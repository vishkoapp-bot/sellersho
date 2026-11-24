package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type SPNCXPrintRequest struct {
	ProductID string   `json:"productId"`
	VariantID string   `json:"variantId"`
	Codes     []string `json:"codes"`
	Reprint   bool     `json:"reprint"`
}

// SPNCXLabelJob supplies already-issued codes and the symbology for a
// renderer. No label renderer or printer can issue a unit code.
type SPNCXLabelJob struct {
	ID            string    `json:"id"`
	ProductID     string    `json:"productId"`
	VariantID     string    `json:"variantId"`
	Codes         []string  `json:"codes"`
	BarcodeFormat string    `json:"barcodeFormat"`
	Reprint       bool      `json:"reprint"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (r *SPNCXRepository) PrintLabels(ctx context.Context, access SPNCXAccess, key string, request SPNCXPrintRequest) (SPNCXLabelJob, error) {
	var job SPNCXLabelJob
	if !access.allowed() {
		return job, ErrSPNCXNotFound
	}
	key = strings.TrimSpace(key)
	product := strings.TrimSpace(request.ProductID)
	sku, err := uuid.Parse(strings.TrimSpace(request.VariantID))
	if err != nil || product == "" || len(product) > 50 || key == "" || len(key) > 128 || len(request.Codes) == 0 || len(request.Codes) > 100 {
		return job, ErrSPNCXInput
	}
	codes := make([]string, 0, len(request.Codes))
	seen := map[string]bool{}
	for _, raw := range request.Codes {
		code, ok := normalizeSPNCX(raw)
		if !ok || seen[code] {
			return job, ErrSPNCXInput
		}
		seen[code] = true
		codes = append(codes, code)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()

	// Unit writers take the product guard before SKU, lot, and unit locks.
	// Keep that order and lock all matching units in sequence order.
	var guard string
	err = tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id=$1 FOR SHARE`, product).Scan(&guard)
	if errors.Is(err, sql.ErrNoRows) {
		return job, ErrSPNCXNotFound
	}
	if err != nil {
		return job, err
	}
	predicate, scope := access.predicate("$4", true, true)
	rows, err := tx.QueryContext(ctx, `SELECT u.id,u.code,u.printed_at IS NOT NULL,u.status,COALESCE(b.id,''),COALESCE(b.status,''),COALESCE(b.quantity,0),
			COALESCE(`+spncxLabelLotMatchesSKU+`,FALSE)
		FROM product_units u JOIN products p ON p.id=u.product_id
		LEFT JOIN product_variants v ON v.id=u.variant_id AND v.product_id=p.id
		LEFT JOIN inventory_balances b ON b.id=u.balance_id AND b.product_id=p.id
		WHERE u.product_id=$1 AND u.variant_id=$2 AND u.code=ANY($3) AND (`+predicate+`)
		ORDER BY u.sequence,u.id FOR UPDATE OF u`, append([]any{product, sku.String(), pq.Array(codes)}, scope...)...)
	if err != nil {
		return job, err
	}
	var unitIDs, ordered []string
	var inconsistent bool
	lotCounts := map[string]int{}
	for rows.Next() {
		var id, code, status, lot, lotStatus string
		var printed, exactPlacement bool
		var quantity int
		if err := rows.Scan(&id, &code, &printed, &status, &lot, &lotStatus, &quantity, &exactPlacement); err != nil {
			rows.Close()
			return job, err
		}
		ordered = append(ordered, code)
		unitIDs = append(unitIDs, id)
		lotCounts[lot]++
		inconsistent = inconsistent || !exactPlacement || printed != request.Reprint || status != "in_stock" || lotStatus != "sellable" || lotCounts[lot] > quantity
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return job, err
	}
	if len(unitIDs) != len(codes) {
		return job, ErrSPNCXNotFound
	}

	job = SPNCXLabelJob{ProductID: product, VariantID: sku.String(), Codes: ordered, BarcodeFormat: "CODE128", Reprint: request.Reprint}
	var priorCodes []byte
	var priorProduct, priorSKU, priorActor, priorRole, priorSupplier string
	var priorReprint bool
	loadPrior := func() error {
		return tx.QueryRowContext(ctx, `SELECT id,product_id,COALESCE(variant_id::text,''),COALESCE(actor_user_id,''),reprint,unit_codes,created_at,
			COALESCE(payload->>'role',''),COALESCE(payload->>'supplier','') FROM spncx_print_jobs WHERE idempotency_key=$1`, key).
			Scan(&job.ID, &priorProduct, &priorSKU, &priorActor, &priorReprint, &priorCodes, &job.CreatedAt, &priorRole, &priorSupplier)
	}
	replay := func() (SPNCXLabelJob, error) {
		if priorProduct != product || priorSKU != sku.String() || priorActor != access.actor || priorReprint != request.Reprint || priorRole != access.role || priorSupplier != access.supplier {
			return SPNCXLabelJob{}, ErrSPNCXPrintConflict
		}
		var prior []string
		if err := json.Unmarshal(priorCodes, &prior); err != nil {
			return SPNCXLabelJob{}, err
		}
		if !slices.Equal(prior, ordered) {
			return SPNCXLabelJob{}, ErrSPNCXPrintConflict
		}
		if err := tx.Commit(); err != nil {
			return SPNCXLabelJob{}, err
		}
		return job, nil
	}
	if err = loadPrior(); err == nil {
		return replay()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SPNCXLabelJob{}, err
	}
	if inconsistent {
		return SPNCXLabelJob{}, ErrSPNCXPrintConflict
	}
	if !request.Reprint {
		// Apply the exact discovery rule to initial jobs, including manually
		// typed codes. The shared product guard excludes inventory mutations
		// and selected unit locks serialize print state. Rank every in-stock
		// unit on the affected lots, including previously printed units.
		lots := make([]string, 0, len(lotCounts))
		for lot := range lotCounts {
			lots = append(lots, lot)
		}
		var eligible int
		err := tx.QueryRowContext(ctx, `WITH ranked AS (`+spncxRankedSellableUnits+`
			AND p.id=$1 AND v.id=$2 AND b.id=ANY($3::text[]))
			SELECT count(*) FROM ranked WHERE unit_id=ANY($4::uuid[]) AND `+spncxFirstPrintEligible,
			product, sku.String(), pq.Array(lots), pq.Array(unitIDs)).Scan(&eligible)
		if err != nil {
			return SPNCXLabelJob{}, err
		}
		if eligible != len(unitIDs) {
			return SPNCXLabelJob{}, ErrSPNCXPrintConflict
		}
	}

	encoded, _ := json.Marshal(ordered)
	payload, _ := json.Marshal(map[string]string{"role": access.role, "supplier": access.supplier})
	err = tx.QueryRowContext(ctx, `INSERT INTO spncx_print_jobs(idempotency_key,product_id,variant_id,unit_codes,payload,actor_user_id,reprint,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()) ON CONFLICT(idempotency_key) DO NOTHING RETURNING id,created_at`,
		key, product, sku.String(), string(encoded), string(payload), access.actor, request.Reprint).Scan(&job.ID, &job.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := loadPrior(); err != nil {
			return SPNCXLabelJob{}, err
		}
		return replay()
	}
	if err != nil {
		return SPNCXLabelJob{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE product_units
		SET printed_by=CASE WHEN printed_at IS NULL THEN $2 ELSE printed_by END,
			printed_at=COALESCE(printed_at,$3),last_printed_at=$3,print_count=print_count+1,updated_at=$3
		WHERE id=ANY($1::uuid[])`, pq.Array(unitIDs), access.actor, job.CreatedAt); err != nil {
		return SPNCXLabelJob{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO spncx_print_job_units(job_id,unit_id,code)
		SELECT $1,id,code FROM product_units WHERE id=ANY($2::uuid[])`, job.ID, pq.Array(unitIDs)); err != nil {
		return SPNCXLabelJob{}, err
	}
	event := "printed"
	if request.Reprint {
		event = "reprinted"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id,created_at)
		SELECT id,code,$2,status,status,'spncx_print_job',$3,$4,$5 FROM product_units WHERE id=ANY($1::uuid[])`,
		pq.Array(unitIDs), event, job.ID, access.actor, job.CreatedAt); err != nil {
		return SPNCXLabelJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return SPNCXLabelJob{}, err
	}
	return job, nil
}

// SPNCXUnprinted labels are already-issued, not a reservation or a print
// job. Both discovery and printing apply the same current-custody scope.
type SPNCXUnprinted struct {
	ProductID string   `json:"productId,omitempty"`
	VariantID string   `json:"variantId,omitempty"`
	Codes     []string `json:"codes"`
	HasMore   bool     `json:"hasMore"`
}

var spncxSKUCode = regexp.MustCompile(`^[0-9A-Z-]{1,12}$`)

const spncxLabelLotMatchesSKU = `((v.inventory_mode='tracked' AND b.variant_id=v.id) OR (v.inventory_mode='legacy' AND b.variant_id IS NULL))`

// A unit beyond the lot's physical capacity is not available for a new
// first label even if its historical ledger row still says in_stock.
const spncxRankedSellableUnits = `
		SELECT u.id AS unit_id,u.product_id,u.variant_id,u.code,u.printed_at,u.sequence,b.quantity,
			ROW_NUMBER() OVER(PARTITION BY b.id ORDER BY u.sequence,u.id) AS position
		FROM product_variants v JOIN products p ON p.id=v.product_id
		JOIN product_units u ON u.variant_id=v.id AND u.product_id=p.id
		JOIN inventory_balances b ON b.id=u.balance_id AND b.product_id=p.id
		WHERE u.status='in_stock' AND b.status='sellable' AND b.quantity>0
		  AND ` + spncxLabelLotMatchesSKU + `
`

const spncxFirstPrintEligible = "printed_at IS NULL AND position<=quantity"

func (r *SPNCXRepository) UnprintedLabels(ctx context.Context, access SPNCXAccess, spnc string, limit int) (SPNCXUnprinted, error) {
	result := SPNCXUnprinted{Codes: []string{}}
	if !access.allowed() {
		return result, ErrSPNCXNotFound
	}
	spnc = strings.ToUpper(strings.TrimSpace(spnc))
	if !spncxSKUCode.MatchString(spnc) || limit < 1 || limit > 100 {
		return result, ErrSPNCXInput
	}
	predicate, scope := access.predicate("$3", true, false)
	rows, err := r.db.QueryContext(ctx, `WITH ranked AS (`+spncxRankedSellableUnits+`
		AND v.spnc=$1 AND (`+predicate+`))
	SELECT product_id,variant_id,code FROM ranked WHERE `+spncxFirstPrintEligible+`
	ORDER BY sequence,code LIMIT $2`, append([]any{spnc, limit + 1}, scope...)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&result.ProductID, &result.VariantID, &code); err != nil {
			return SPNCXUnprinted{}, err
		}
		result.Codes = append(result.Codes, code)
	}
	if err := rows.Err(); err != nil {
		return SPNCXUnprinted{}, err
	}
	if len(result.Codes) > limit {
		result.Codes = result.Codes[:limit]
		result.HasMore = true
	}
	return result, nil
}
