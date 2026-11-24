package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrBackfillDryRunRequired          = errors.New("backfill dry run required")
	ErrBackfillPlanHashMismatch        = errors.New("backfill plan hash mismatch")
	ErrBackfillExcessProductNotFound   = errors.New("backfill excess product not found")
	ErrBackfillNoExcessToDecide        = errors.New("no backfill excess to decide")
	ErrBackfillInvalidExcessDecision   = errors.New("invalid backfill excess decision")
	ErrBackfillProductNotExcluded      = errors.New("backfill product not excluded")
	ErrBackfillReincludeReasonRequired = errors.New("backfill re-include reason required")
	ErrBackfillReincludeOutsideScope   = errors.New("backfill re-include outside approved scope")
)

type BackfillProductDiff struct {
	ProductID     string `json:"product_id"`
	AllocChanged  bool   `json:"alloc_changed"`
	TopUpChanged  bool   `json:"top_up_changed"`
	ExcessChanged bool   `json:"excess_changed"`
}

type BackfillPlanDiff struct {
	PreviousPlanHash *string               `json:"previous_plan_hash"`
	ChangedProducts  []BackfillProductDiff `json:"changed_products"`
}

type storedBackfillPlan struct {
	plan     BackfillProductPlan
	hash     *string
	decision *string
}

func decodeStoredBackfillPlan(id string, raw []byte, stored *storedBackfillPlan) (bool, error) {
	if err := json.Unmarshal(raw, &stored.plan); err != nil {
		return false, fmt.Errorf("decode stored backfill plan for %s: %w", id, err)
	}
	if stored.plan.ProductID == "" {
		return false, nil // B5 state rows can exist with the default empty plan.
	}
	if stored.plan.ProductID != id {
		return false, fmt.Errorf("stored backfill plan for %s has product ID %q", id, stored.plan.ProductID)
	}
	return true, nil
}

// Match B5's reuse of a stored variant UUID when the approved scope matches.
func reuseBackfillVariantID(p *BackfillProductPlan, old BackfillProductPlan) {
	if len(p.NewVariants) != 1 || len(old.NewVariants) != 1 {
		return
	}
	freshID, storedID := p.NewVariants[0].VariantID, old.NewVariants[0].VariantID
	p.NewVariants[0].VariantID = storedID
	for i := range p.Alloc {
		if p.Alloc[i].VariantID == freshID {
			p.Alloc[i].VariantID = storedID
		}
	}
	for i := range p.LotsToCreate {
		if p.LotsToCreate[i].VariantID == freshID {
			p.LotsToCreate[i].VariantID = storedID
		}
	}
}

func diffBackfillPlan(plan *BackfillPlan, previous map[string]storedBackfillPlan) *BackfillPlanDiff {
	if len(previous) == 0 {
		return nil
	}
	diff := &BackfillPlanDiff{ChangedProducts: []BackfillProductDiff{}}
	var foundPrevious bool
	for i := range plan.Products {
		p := &plan.Products[i]
		stored, ok := previous[p.ProductID]
		if !ok {
			continue
		}
		if !foundPrevious {
			diff.PreviousPlanHash = stored.hash
			foundPrevious = true
		}
		if stored.hash != nil && *stored.hash == plan.PlanHash {
			reuseBackfillVariantID(p, stored.plan)
		}
		change := BackfillProductDiff{
			ProductID: p.ProductID, AllocChanged: !slices.Equal(p.Alloc, stored.plan.Alloc),
			TopUpChanged: p.TopUp != stored.plan.TopUp, ExcessChanged: p.Excess != stored.plan.Excess,
		}
		if change.AllocChanged || change.TopUpChanged || change.ExcessChanged {
			diff.ChangedProducts = append(diff.ChangedProducts, change)
		}
	}
	return diff
}

func PersistBackfillDryRunPlan(ctx context.Context, db *sql.DB, plan *BackfillPlan) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backfill dry-run persistence: %w", err)
	}
	defer tx.Rollback()

	previous := make(map[string]storedBackfillPlan, len(plan.Products))
	for i := range plan.Products {
		p := &plan.Products[i]
		var raw []byte
		var status string
		var stored storedBackfillPlan
		err := tx.QueryRowContext(ctx, `SELECT plan, plan_hash, excess_decision, status
			FROM spncx_backfill_state WHERE product_id=$1 FOR UPDATE`, p.ProductID).
			Scan(&raw, &stored.hash, &stored.decision, &status)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read prior backfill plan for %s: %w", p.ProductID, err)
		}
		if status != "completed" {
			p.ExcessDecision = stored.decision
		}
		hasPlan, err := decodeStoredBackfillPlan(p.ProductID, raw, &stored)
		if err != nil {
			return err
		}
		if hasPlan {
			previous[p.ProductID] = stored
		}
	}
	diff := diffBackfillPlan(plan, previous)
	for _, p := range plan.Products {
		raw, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("encode backfill plan for %s: %w", p.ProductID, err)
		}
		var target int
		for _, a := range p.Alloc {
			target += max(0, a.Alloc-a.AlreadyMinted)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO spncx_backfill_state AS s
			(product_id,status,plan,plan_hash,excess_amount,units_target)
			VALUES ($1,'pending',$2::jsonb,$3,$4,$5)
			ON CONFLICT (product_id) DO UPDATE SET
			    plan_hash=EXCLUDED.plan_hash,
			    plan=CASE WHEN s.status='completed' THEN s.plan ELSE EXCLUDED.plan END,
			    excess_amount=CASE WHEN s.status='completed' THEN s.excess_amount ELSE EXCLUDED.excess_amount END,
			    units_target=CASE WHEN s.status='completed' THEN s.units_target ELSE EXCLUDED.units_target END,
			    status=CASE WHEN s.status='failed' THEN 'pending' ELSE s.status END,
			    error=CASE WHEN s.status='failed' THEN NULL ELSE s.error END`,
			p.ProductID, string(raw), plan.PlanHash, p.Excess, target)
		if err != nil {
			return fmt.Errorf("persist backfill plan for %s: %w", p.ProductID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backfill dry-run plan: %w", err)
	}
	plan.DiffVsPreviousDryRun = nil
	if diff != nil {
		plan.DiffVsPreviousDryRun = diff
	}
	return nil
}

type BackfillApproval struct {
	plans map[string]storedBackfillPlan
}

// LoadBackfillApproval checks product existence and the presence of each
// requested approval before the handler spends time computing the live plan.
func LoadBackfillApproval(ctx context.Context, db *sql.DB, productIDs []string) (*BackfillApproval, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin backfill approval read: %w", err)
	}
	defer tx.Rollback()

	ids, err := backfillProductIDsTx(ctx, tx, productIDs)
	if err != nil {
		return nil, fmt.Errorf("list products for backfill approval: %w", err)
	}
	approval := &BackfillApproval{plans: make(map[string]storedBackfillPlan, len(ids))}
	for _, id := range ids {
		var raw []byte
		var stored storedBackfillPlan
		err := tx.QueryRowContext(ctx, `SELECT s.plan, s.plan_hash FROM products p
			LEFT JOIN spncx_backfill_state s ON s.product_id=p.id WHERE p.id=$1`, id).
			Scan(&raw, &stored.hash)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrBackfillProductNotFound, id)
		}
		if err != nil {
			return nil, fmt.Errorf("read backfill approval for %s: %w", id, err)
		}
		if len(raw) == 0 || stored.hash == nil || *stored.hash == "" {
			continue
		}
		hasPlan, err := decodeStoredBackfillPlan(id, raw, &stored)
		if err != nil {
			return nil, err
		}
		if hasPlan {
			approval.plans[id] = stored
		}
	}
	if len(approval.plans) != len(ids) || len(ids) == 0 {
		return nil, ErrBackfillDryRunRequired
	}
	return approval, nil
}

// Compare returns the live diff only after every stored hash matches the live scope.
func (a *BackfillApproval) Compare(plan *BackfillPlan) (*BackfillPlanDiff, error) {
	if len(a.plans) != len(plan.Products) {
		return nil, ErrBackfillPlanHashMismatch
	}
	for _, p := range plan.Products {
		stored, ok := a.plans[p.ProductID]
		if !ok || *stored.hash != plan.PlanHash {
			return nil, ErrBackfillPlanHashMismatch
		}
	}
	return diffBackfillPlan(plan, a.plans), nil
}

type BackfillExcessDecision struct {
	ProductID string     `json:"product_id"`
	Amount    int        `json:"excess_amount"`
	Decision  string     `json:"excess_decision"`
	DecidedBy string     `json:"excess_decided_by"`
	DecidedAt time.Time  `json:"excess_decided_at"`
	ReviewBy  *time.Time `json:"review_by"`
}

func backfillReviewBy(completedAt *time.Time) *time.Time {
	if completedAt == nil {
		return nil
	}
	reviewBy := completedAt.Add(30 * 24 * time.Hour)
	return &reviewBy
}

func BackfillExcessDecisionValid(decision string) bool {
	return decision == "write_off_accepted" || decision == "physical_count_ordered"
}

func BackfillProductExists(ctx context.Context, db *sql.DB, productID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id=$1)`, productID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check backfill product %s: %w", productID, err)
	}
	return exists, nil
}

func RecordBackfillExcessDecision(ctx context.Context, db *sql.DB, productID, decision, decidedBy string) (*BackfillExcessDecision, error) {
	if !BackfillExcessDecisionValid(decision) {
		return nil, ErrBackfillInvalidExcessDecision
	}
	result := &BackfillExcessDecision{ProductID: productID}
	var completedAt *time.Time
	err := db.QueryRowContext(ctx, `UPDATE spncx_backfill_state
		SET excess_decision=$2, excess_decided_by=$3, excess_decided_at=NOW()
		WHERE product_id=$1 AND excess_amount>0
		RETURNING excess_amount, excess_decision, excess_decided_by, excess_decided_at, completed_at`,
		productID, decision, decidedBy).
		Scan(&result.Amount, &result.Decision, &result.DecidedBy, &result.DecidedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		exists, err := BackfillProductExists(ctx, db, productID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrBackfillExcessProductNotFound
		}
		return nil, ErrBackfillNoExcessToDecide
	}
	if err != nil {
		return nil, fmt.Errorf("record backfill excess decision for %s: %w", productID, err)
	}
	result.ReviewBy = backfillReviewBy(completedAt)
	return result, nil
}

// AuditBackfillReinclude validates the entire request. Failed preflight can
// still surface validation errors, but must leave no audit writes.
func AuditBackfillReinclude(ctx context.Context, db *sql.DB, plan *BackfillPlan, reinclude map[string]string, actor string) error {
	if len(reinclude) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backfill re-include audit: %w", err)
	}
	defer tx.Rollback()

	ids := make([]string, 0, len(reinclude))
	for id := range reinclude {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	record := plan.AllPassed()
	for _, id := range ids {
		var previousReason, previousActor string
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(excluded_reason,''), COALESCE(excluded_by,'')
			FROM spncx_backfill_state WHERE product_id=$1 AND status='excluded' FOR UPDATE`, id).
			Scan(&previousReason, &previousActor)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBackfillProductNotExcluded
		}
		if err != nil {
			return fmt.Errorf("read backfill exclusion for %s: %w", id, err)
		}
		if strings.TrimSpace(reinclude[id]) == "" {
			return ErrBackfillReincludeReasonRequired
		}
		if !slices.ContainsFunc(plan.Products, func(p BackfillProductPlan) bool { return p.ProductID == id }) {
			return ErrBackfillReincludeOutsideScope
		}
		if !record {
			continue
		}
		details, err := json.Marshal(map[string]string{
			"product_id": id, "reason": reinclude[id],
			"previous_excluded_reason": previousReason, "previous_excluded_by": previousActor,
		})
		if err != nil {
			return fmt.Errorf("encode backfill re-include audit for %s: %w", id, err)
		}
		// The compact UUID keeps the prefixed ID within audit_logs.id VARCHAR(50).
		auditID := "SPNCX-REINCLUDE-" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id,operator,action,category,details)
			VALUES ($1,$2,'spncx_backfill_reinclude','warehouse',$3)`, auditID, actor, string(details)); err != nil {
			return fmt.Errorf("audit backfill re-include for %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backfill re-include audit: %w", err)
	}
	return nil
}

type backfillStatusCounts struct {
	Pending   int `json:"pending"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Excluded  int `json:"excluded"`
}

func (c *backfillStatusCounts) add(status string, count int) {
	switch status {
	case "pending":
		c.Pending += count
	case "completed":
		c.Completed += count
	case "failed":
		c.Failed += count
	case "excluded":
		c.Excluded += count
	}
}

type backfillStatusSummary struct {
	backfillStatusCounts
	TotalUnitsMinted int `json:"total_units_minted"`
}

type BackfillStatusProduct struct {
	ProductID       string     `json:"product_id"`
	Status          string     `json:"status"`
	UnitsMinted     int        `json:"units_minted"`
	UnitsTarget     int        `json:"units_target"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	Error           *string    `json:"error"`
	ExcludedReason  *string    `json:"excluded_reason"`
	ExcludedBy      *string    `json:"excluded_by"`
	ExcludedAt      *time.Time `json:"excluded_at"`
	FlippedVariants []string   `json:"flipped_variants"`
	ExcessHeld      int        `json:"excess_held"`
	ExcessReviewBy  *time.Time `json:"excess_review_by"`
	ExcessAmount    int        `json:"excess_amount"`
	ExcessDecision  *string    `json:"excess_decision"`
	ExcessDecidedBy *string    `json:"excess_decided_by"`
	ExcessDecidedAt *time.Time `json:"excess_decided_at"`
}

type BackfillStatus struct {
	Running  bool                    `json:"running"`
	Products []BackfillStatusProduct `json:"products"`
	Summary  backfillStatusSummary   `json:"summary"`
}

func probeBackfillRunning(ctx context.Context, db *sql.DB) (bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("get backfill lock probe connection: %w", err)
	}
	defer conn.Close()

	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, backfillAdvisoryLockID).Scan(&acquired); err != nil {
		// A canceled query may still have acquired the session lock.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return false, fmt.Errorf("probe backfill lock: %w", err)
	}
	if !acquired {
		return true, nil
	}
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var unlocked bool
	err = conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, backfillAdvisoryLockID).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		if err == nil {
			err = errors.New("backfill probe lock was not held")
		}
		return false, fmt.Errorf("release backfill probe lock: %w", err)
	}
	return false, nil
}

func ReadBackfillStatus(ctx context.Context, db *sql.DB) (*BackfillStatus, error) {
	running, err := probeBackfillRunning(ctx, db)
	if err != nil {
		return nil, err
	}
	result := &BackfillStatus{Running: running, Products: []BackfillStatusProduct{}}
	rows, err := db.QueryContext(ctx, `SELECT s.product_id,s.status,s.units_minted,s.units_target,s.started_at,s.completed_at,
		s.error,s.excluded_reason,s.excluded_by,s.excluded_at,s.plan->'flipped_variants',
		(SELECT COALESCE(SUM(b.quantity),0) FROM inventory_balances b
		 WHERE b.product_id=s.product_id AND b.status='unreconciled' AND b.location_key='spncx-excess:'||s.product_id),
		s.excess_amount,s.excess_decision,s.excess_decided_by,s.excess_decided_at
		FROM spncx_backfill_state s ORDER BY s.product_id`)
	if err != nil {
		return nil, fmt.Errorf("query backfill status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p BackfillStatusProduct
		var flipped []byte
		if err := rows.Scan(&p.ProductID, &p.Status, &p.UnitsMinted, &p.UnitsTarget, &p.StartedAt, &p.CompletedAt,
			&p.Error, &p.ExcludedReason, &p.ExcludedBy, &p.ExcludedAt, &flipped, &p.ExcessHeld,
			&p.ExcessAmount, &p.ExcessDecision, &p.ExcessDecidedBy, &p.ExcessDecidedAt); err != nil {
			return nil, fmt.Errorf("scan backfill status: %w", err)
		}
		if len(flipped) != 0 {
			if err := json.Unmarshal(flipped, &p.FlippedVariants); err != nil {
				return nil, fmt.Errorf("decode flipped variants for %s: %w", p.ProductID, err)
			}
		}
		if p.FlippedVariants == nil {
			p.FlippedVariants = []string{}
		}
		p.ExcessReviewBy = backfillReviewBy(p.CompletedAt)
		result.Products = append(result.Products, p)
		result.Summary.add(p.Status, 1)
		result.Summary.TotalUnitsMinted += p.UnitsMinted
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read backfill status: %w", err)
	}
	return result, nil
}
