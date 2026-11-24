package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSPNCXInput         = errors.New("invalid SPNCX request")
	ErrSPNCXNotFound      = errors.New("SPNCX unit not found")
	ErrSPNCXPrintConflict = errors.New("SPNCX print conflict")
	spncxCodePattern      = regexp.MustCompile(`^[0-9A-Z-]+-X[0-9]{4}$`)
)

type SPNCXRepository struct{ db *sql.DB }

func NewSPNCXRepository(db *sql.DB) *SPNCXRepository { return &SPNCXRepository{db: db} }

// SPNCXAccess is derived only from the authenticated API principal, not
// supplied by the caller in a print or lookup request.
type SPNCXAccess struct{ role, actor, supplier string }

func SPNCXAdminAccess(actor string) SPNCXAccess { return SPNCXAccess{role: "admin", actor: actor} }
func SPNCXSupplierAccess(actor, supplier string) SPNCXAccess {
	return SPNCXAccess{role: "supplier", actor: actor, supplier: supplier}
}
func SPNCXWarehouseAccess(staff string) SPNCXAccess {
	return SPNCXAccess{role: "warehouse", actor: staff}
}

func (a SPNCXAccess) allowed() bool {
	if strings.TrimSpace(a.actor) == "" || len(a.actor) > 50 {
		return false
	}
	switch a.role {
	case "admin":
		return true
	case "supplier":
		return a.supplier != "" && len(a.supplier) <= 50
	case "warehouse":
		_, err := uuid.Parse(a.actor)
		return err == nil
	default:
		return false
	}
}

// Lookups authorize supplier ownership, current warehouse custody, or the
// exact un-restored sale allocation when the unit moved to a sold ledger lot.
// Print mutations never use historical custody as authority.
func (a SPNCXAccess) predicate(param string, custodyOnly, lockScope bool) (string, []any) {
	switch a.role {
	case "admin":
		return "TRUE", nil
	case "supplier":
		owned := "p.supplier_id=" + param
		if custodyOnly {
			owned += ` AND b.supplier_id=p.supplier_id AND b.location_type='supplier' AND b.warehouse_id IS NULL
				AND EXISTS(SELECT 1 FROM supplier_warehouses sw WHERE sw.id=b.supplier_warehouse_id AND sw.supplier_id=p.supplier_id AND sw.is_active)`
		}
		return owned, []any{a.supplier}
	case "warehouse":
		site := "site.id=b.warehouse_id"
		if !custodyOnly {
			site = `((u.status<>'sold' AND ` + site + `) OR (b.status='sold' AND u.status='sold' AND EXISTS (
				SELECT 1 FROM order_inventory_captures cap
				CROSS JOIN LATERAL jsonb_array_elements(cap.allocations) alloc
				JOIN order_item_origins o ON o.id=alloc->>'originId' AND o.order_item_id::text=cap.order_item_id
				WHERE cap.order_item_id=sale.order_item_id::text AND cap.order_id=sale.order_id AND (alloc->'unit_codes') ? u.code
				  AND o.origin_type='warehouse' AND o.warehouse_id=site.id
			)))`
		}
		pred := `EXISTS(SELECT 1 FROM warehouses site JOIN users staff ON staff.id=` + param + `
			WHERE site.is_active AND ` + warehouseStaffCityMatch + ` AND ` + site
		if lockScope {
			pred += " FOR SHARE OF site,staff"
		}
		return pred + ")", []any{a.actor}
	default:
		return "FALSE", nil
	}
}

func normalizeSPNCX(code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	return code, len(code) <= 20 && spncxCodePattern.MatchString(code)
}

// SPNCXTrace is staff-only. No buyer identity, raw event metadata, or staff
// account details are copied into it, and catalog DTOs do not import it.
type SPNCXTrace struct {
	Code             string            `json:"code"`
	ProductID        string            `json:"productId"`
	VariantID        string            `json:"variantId"`
	Status           string            `json:"status"`
	IssuedAt         time.Time         `json:"issuedAt"`
	PrintedAt        *time.Time        `json:"printedAt"`
	LastPrintedAt    *time.Time        `json:"lastPrintedAt"`
	PrintCount       int               `json:"printCount"`
	History          []SPNCXTraceEvent `json:"history"`
	HistoryTruncated bool              `json:"historyTruncated"`
	CurrentOrderID   *string           `json:"currentOrderId,omitempty"`
}

type SPNCXTraceEvent struct {
	EventType     string    `json:"eventType"`
	FromStatus    *string   `json:"fromStatus"`
	ToStatus      *string   `json:"toStatus"`
	ReferenceType string    `json:"referenceType"`
	ReferenceID   *string   `json:"referenceId"`
	At            time.Time `json:"at"`
}

func (r *SPNCXRepository) LookupUnit(ctx context.Context, access SPNCXAccess, code string) (SPNCXTrace, error) {
	var trace SPNCXTrace
	if !access.allowed() {
		return trace, ErrSPNCXNotFound
	}
	code, ok := normalizeSPNCX(code)
	if !ok {
		return trace, ErrSPNCXInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return trace, err
	}
	defer tx.Rollback()
	predicate, scope := access.predicate("$2", false, false)
	var id string
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.code,u.product_id,u.variant_id,u.status,u.issued_at,u.printed_at,u.last_printed_at,u.print_count,sale.order_id
		FROM product_units u JOIN products p ON p.id=u.product_id
		LEFT JOIN inventory_balances b ON b.id=u.balance_id AND b.product_id=p.id
		LEFT JOIN LATERAL (
			SELECT ou.order_id,ou.order_item_id FROM order_item_units ou
			WHERE u.status='sold' AND ou.unit_id=u.id AND ou.restored_at IS NULL
			ORDER BY ou.captured_at DESC,ou.order_item_id DESC LIMIT 1
		) sale ON true
		WHERE u.code=$1 AND (`+predicate+`)`, append([]any{code}, scope...)...).Scan(&id, &trace.Code, &trace.ProductID, &trace.VariantID, &trace.Status, &trace.IssuedAt, &trace.PrintedAt, &trace.LastPrintedAt, &trace.PrintCount, &trace.CurrentOrderID)
	if errors.Is(err, sql.ErrNoRows) {
		return SPNCXTrace{}, ErrSPNCXNotFound
	}
	if err != nil {
		return SPNCXTrace{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT event_type,from_status,to_status,reference_type,reference_id,created_at
		FROM product_unit_events WHERE unit_id=$1 ORDER BY created_at DESC,id DESC LIMIT 201`, id)
	if err != nil {
		return SPNCXTrace{}, err
	}
	defer rows.Close()
	trace.History = []SPNCXTraceEvent{}
	for rows.Next() {
		var item SPNCXTraceEvent
		if err := rows.Scan(&item.EventType, &item.FromStatus, &item.ToStatus, &item.ReferenceType, &item.ReferenceID, &item.At); err != nil {
			return SPNCXTrace{}, err
		}
		if access.role == "warehouse" {
			// A physical unit can move between warehouses; previous work
			// references do not automatically become visible at the new site.
			item.ReferenceID = nil
		}
		trace.History = append(trace.History, item)
	}
	if err := rows.Err(); err != nil {
		return SPNCXTrace{}, err
	}
	rows.Close()
	if len(trace.History) > 200 {
		trace.HistoryTruncated = true
		trace.History = trace.History[:200]
	}
	if err := tx.Commit(); err != nil {
		return SPNCXTrace{}, err
	}
	return trace, nil
}
