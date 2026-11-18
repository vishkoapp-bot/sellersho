package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type SupplierCampaign struct {
	ID          string `json:"id"`
	Name        string `json:"campaign_name"`
	Budget      int64  `json:"budget"`
	Bid         int64  `json:"cpc_bid"`
	Spent       int64  `json:"spent_budget"`
	Impressions int    `json:"impressions"`
	Clicks      int    `json:"clicks"`
	Status      string `json:"status"`
}

func (r *SupplierRepository) GetCampaigns(ctx context.Context, supplierID string) ([]SupplierCampaign, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, campaign_name, budget, cpc_bid, spent_budget, impressions, clicks, status FROM supplier_campaigns WHERE supplier_id = $1 ORDER BY created_at DESC`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SupplierCampaign{}
	for rows.Next() {
		var item SupplierCampaign
		if err := rows.Scan(&item.ID, &item.Name, &item.Budget, &item.Bid, &item.Spent, &item.Impressions, &item.Clicks, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) SetCampaignStatus(ctx context.Context, supplierID, id, status string) error {
	if status != "active" && status != "paused" {
		return fmt.Errorf("invalid campaign status")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE supplier_campaigns SET status = $1 WHERE id = $2 AND supplier_id = $3`, status, id, supplierID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type SupplierBalance struct {
	Earned          int64 `json:"earned"`
	Reserved        int64 `json:"reserved"`
	Paid            int64 `json:"paid"`
	ReturnCharges   int64 `json:"returnCharges"`
	ReturnClawbacks int64 `json:"returnClawbacks"`
	Adjustments     int64 `json:"adjustments"`
	StorageFees     int64 `json:"storageFees"`
	Available       int64 `json:"available"`
}

// Canonical supplier-balance terms (GAP 15): the per-supplier balance
// IS the platform's one definition of "owed to a supplier" — it is the
// figure the payout gate recomputes under lock — so every surface
// (panel, payout gate, treasury aggregate) is built from these shared
// fragments and can never drift into a second definition. $SUP is
// replaced by the supplier-id expression: "$1" for the per-supplier
// query, "s.id" inside SumAllSuppliersAvailable.
const (
	// Earned: per-item wholesale-cost snapshots over paid + delivered
	// orders. No release hold — supplier money is owed from delivery.
	supplierOriginAuthority = `(SELECT CASE WHEN COUNT(*)>0 AND SUM(src.quantity)=oi.quantity AND BOOL_AND(COALESCE(src.quantity,0)>0 AND COALESCE(src.order_id,'')=oi.order_id AND COALESCE(src.supplier_id,'')<>'') AND COUNT(DISTINCT src.supplier_id)=1 THEN MIN(src.supplier_id) END FROM order_item_origins src WHERE src.order_item_id=oi.id)`
	supplierTermEarned      = `COALESCE((SELECT SUM(oi.wholesale_cost::numeric * oi.quantity) FROM order_items oi JOIN orders o ON o.id=oi.order_id WHERE ` + supplierOriginAuthority + ` = $SUP AND o.payment_status='paid' AND o.status IN ('delivered','delivered_confirmed')),0)`
	// Reserved: payout requests still awaiting review.
	supplierTermReserved = `COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $SUP AND status = 'pending'), 0)`
	// Paid: payout requests that left (or are leaving) the platform.
	supplierTermPaid = `COALESCE((SELECT SUM(amount) FROM payout_requests WHERE supplier_id = $SUP AND status IN ('approved','paid','processing','completed','transfer_confirmed')), 0)`
	// ReturnCharges, supplier stock-return events leg (signed:
	// debit +, anything else -).
	supplierTermReturnEvents = `COALESCE((SELECT SUM(CASE WHEN operation = 'debit' THEN amount ELSE -amount END) FROM supplier_return_financial_events WHERE supplier_id = $SUP), 0)`
	// ReturnCharges, customer-return shipping leg: defective-item
	// shares of the return fare charged at receive (migration
	// 202610250202). The charges cannot live in
	// supplier_return_financial_events (its return_request_id FKs to
	// supplier_return_requests), so they are summed here instead.
	supplierTermReturnShippingCharges = `COALESCE((SELECT SUM(amount) FROM customer_return_shipping_charges WHERE supplier_id = $SUP AND bearer = 'supplier'), 0)`
	// ReturnClawbacks (GAP 6): product-cost reversals written when a
	// customer return is refunded. The orders join is REQUIRED:
	// Earned reads the whole order, so once an order is cancelled
	// (e.g. by a dispute refund) Earned already excludes it —
	// counting its item clawbacks too would reverse the cost twice.
	supplierTermReturnClawbacks = `COALESCE((SELECT SUM(CASE WHEN e.operation = 'debit' THEN e.amount ELSE -e.amount END) FROM customer_return_financial_events e JOIN orders o ON o.id = e.order_id WHERE e.supplier_id = $SUP AND e.component = 'product_cost' AND o.status <> 'cancelled'), 0)`
	// Adjustments (GAP 15): finance adjustment documents
	// (wr69FinanceAdjustmentCreate) already write ledger_entries
	// payable legs; this term finally reads them — credit +, debit -,
	// per the form's own labels. The read source is ledger_entries,
	// NOT finance_adjustments: the ledger legs are written
	// unconditionally and are append-only, while the audit table's
	// write is existence-guarded. Dispute-resolution debits can never
	// leak in: their target_account is a free-form party string (not
	// 'supplier_payable:<id>') and their reason lacks the prefix.
	supplierTermAdjustments = `COALESCE((SELECT SUM(CASE WHEN type = 'credit' THEN amount ELSE -amount END) FROM ledger_entries WHERE target_account = 'supplier_payable:' || $SUP AND target_type = 'supplier' AND reason LIKE 'سند تعدیل%'), 0)`
	// StorageFees (GAP 7): assessed warehouse storage-fee charges
	// (migration 202610280005) written by the storage-fee engine
	// (RunStorageFeeAssessment); deducted from Available so the
	// supplier sees Available drop by exactly the charged total.
	supplierTermStorageFees = `COALESCE((SELECT SUM(amount_toman) FROM warehouse_storage_fee_charges WHERE supplier_id = $SUP), 0)`
)

func supplierTermFor(fragment, supplierExpr string) string {
	return strings.ReplaceAll(fragment, "$SUP", supplierExpr)
}

// balanceTableFlags records which optional money tables the connected
// database actually has, so balance SQL only ever references existing
// tables (the long-standing guard pattern of this balance: a database
// predating a migration reads the terms it supports).
type balanceTableFlags struct {
	financialEvents bool // supplier_return_financial_events
	shippingCharges bool // customer_return_shipping_charges
	returnEvents    bool // customer_return_financial_events
	storageFees     bool // warehouse_storage_fee_charges
}

func probeBalanceTables(ctx context.Context, q queryRower) (balanceTableFlags, error) {
	var f balanceTableFlags
	err := q.QueryRowContext(ctx, `SELECT
		to_regclass('supplier_return_financial_events') IS NOT NULL,
		to_regclass('customer_return_shipping_charges') IS NOT NULL,
		to_regclass('customer_return_financial_events') IS NOT NULL,
		to_regclass('warehouse_storage_fee_charges') IS NOT NULL`).Scan(&f.financialEvents, &f.shippingCharges, &f.returnEvents, &f.storageFees)
	return f, err
}

func (f balanceTableFlags) returnChargesExpr(supplierExpr string) string {
	if !f.financialEvents {
		return "0"
	}
	expr := supplierTermFor(supplierTermReturnEvents, supplierExpr)
	if f.shippingCharges {
		expr += " + " + supplierTermFor(supplierTermReturnShippingCharges, supplierExpr)
	}
	return expr
}

func (f balanceTableFlags) returnClawbacksExpr(supplierExpr string) string {
	if !f.returnEvents {
		return "0"
	}
	return supplierTermFor(supplierTermReturnClawbacks, supplierExpr)
}

func (f balanceTableFlags) storageFeesExpr(supplierExpr string) string {
	if !f.storageFees {
		return "0"
	}
	return supplierTermFor(supplierTermStorageFees, supplierExpr)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func scanSupplierBalance(ctx context.Context, q queryRower, supplierID string, balance *SupplierBalance) error {
	flags, err := probeBalanceTables(ctx, q)
	if err != nil {
		return err
	}
	var unresolved int64
	query := "SELECT " + strings.Join([]string{
		supplierTermFor(supplierTermEarned, "$1"),
		supplierTermFor(supplierTermReserved, "$1"),
		supplierTermFor(supplierTermPaid, "$1"),
		flags.returnChargesExpr("$1"),
		flags.returnClawbacksExpr("$1"),
		supplierTermFor(supplierTermAdjustments, "$1"),
		flags.storageFeesExpr("$1"),
		unresolvedSupplierEarningsExpr("$1"),
	}, ", ")
	if err := q.QueryRowContext(ctx, query, supplierID).Scan(&balance.Earned, &balance.Reserved, &balance.Paid, &balance.ReturnCharges, &balance.ReturnClawbacks, &balance.Adjustments, &balance.StorageFees, &unresolved); err != nil {
		return err
	}
	if unresolved > 0 {
		return ErrFinancePrincipalUnresolved
	}
	for _, amount := range []int64{balance.Earned, balance.Reserved, balance.Paid, balance.ReturnCharges, balance.ReturnClawbacks, balance.Adjustments, balance.StorageFees} {
		if err := validateSignedFinanceMoney(amount); err != nil {
			return err
		}
	}
	balance.Available, err = signedBalanceSum([]int64{balance.Earned, balance.Adjustments}, []int64{balance.Reserved, balance.Paid, balance.ReturnCharges, balance.ReturnClawbacks, balance.StorageFees})
	return err
}

func (r *SupplierRepository) GetSettlementBalance(ctx context.Context, supplierID string) (*SupplierBalance, error) {
	var balance SupplierBalance
	if err := scanSupplierBalance(ctx, r.db, supplierID, &balance); err != nil {
		return nil, err
	}
	return &balance, nil
}

// SumAllSuppliersAvailableSplit aggregates every supplier's canonical
// signed availability in ONE query (FIN-008 D-13/G-17), split the same
// way as the store aggregate: positive is the liability, recovery is
// the separately reported receivable, never netted (D-10). Unresolved
// supplier earnings fail the aggregate, never zero (C-04).
//
// The signed-sum SumAllSuppliersAvailable below is deliberately KEPT
// unchanged: it is the panel identity (owedToSuppliers == Σ panel
// Available, signed) pinned by the GAP-15 tests. Treasury must NOT use
// it — treasury uses this split variant.
func SumAllSuppliersAvailableSplit(ctx context.Context, q TreasuryQuerier) (positive, recovery int64, err error) {
	flags, err := probeBalanceTables(ctx, q)
	if err != nil {
		return 0, 0, err
	}
	available := "(" + supplierTermFor(supplierTermEarned, "s.id") + ")" +
		" - (" + supplierTermFor(supplierTermReserved, "s.id") + ")" +
		" - (" + supplierTermFor(supplierTermPaid, "s.id") + ")" +
		" - (" + flags.returnChargesExpr("s.id") + ")" +
		" - (" + flags.returnClawbacksExpr("s.id") + ")" +
		" + (" + supplierTermFor(supplierTermAdjustments, "s.id") + ")" +
		" - (" + flags.storageFeesExpr("s.id") + ")"
	var unresolved int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN avail > 0 THEN avail ELSE 0 END),0),`+
		`COALESCE(SUM(CASE WHEN avail < 0 THEN -avail ELSE 0 END),0),`+unresolvedSupplierEarningsExpr("")+
		` FROM (SELECT `+available+` AS avail FROM suppliers s) t`).Scan(&positive, &recovery, &unresolved); err != nil {
		return 0, 0, err
	}
	if unresolved > 0 {
		return 0, 0, ErrFinancePrincipalUnresolved
	}
	if err := validateSignedFinanceMoney(positive); err != nil {
		return 0, 0, err
	}
	if err := validateSignedFinanceMoney(recovery); err != nil {
		return 0, 0, err
	}
	if _, err := signedBalanceSum([]int64{positive}, []int64{recovery}); err != nil {
		return 0, 0, err
	}
	return positive, recovery, nil
}

// SumAllSuppliersAvailable is the treasury's canonical "owed to
// suppliers" (GAP 15): the signed sum of every supplier's Available,
// assembled from the same fragments as the per-supplier balance.
// Negative balances net against the total (no per-supplier flooring),
// keeping the identity owedToSuppliers == Σ panel Available exact.
//
// NOTE (FIN-008 D-10): this signed identity is the PANEL identity and
// is kept for it. The treasury cash position must NOT use it —
// negatives netted here would inflate safeToSpend. Treasury uses
// SumAllSuppliersAvailableSplit, which reports positives as the
// liability and negatives as a separate recovery receivable.
func (r *SupplierRepository) SumAllSuppliersAvailable(ctx context.Context) (int64, error) {
	flags, err := probeBalanceTables(ctx, r.db)
	if err != nil {
		return 0, err
	}
	available := "(" + supplierTermFor(supplierTermEarned, "s.id") + ")" +
		" - (" + supplierTermFor(supplierTermReserved, "s.id") + ")" +
		" - (" + supplierTermFor(supplierTermPaid, "s.id") + ")" +
		" - (" + flags.returnChargesExpr("s.id") + ")" +
		" - (" + flags.returnClawbacksExpr("s.id") + ")" +
		" + (" + supplierTermFor(supplierTermAdjustments, "s.id") + ")" +
		" - (" + flags.storageFeesExpr("s.id") + ")"
	var total, unresolved int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(`+available+`),0),`+unresolvedSupplierEarningsExpr("")+` FROM suppliers s`).Scan(&total, &unresolved); err != nil {
		return 0, err
	}
	if unresolved > 0 {
		return 0, ErrFinancePrincipalUnresolved
	}
	return total, validateSignedFinanceMoney(total)
}

// Current catalog ownership is only a candidate for reporting ambiguity, never
// authority for earned money. The treasury refuses every unresolved amount;
// per-account reads also block if the source names that account or no financial
// party can be identified at all. Nothing silently turns unknown funds into 0.
func unresolvedSupplierEarningsExpr(supplierExpr string) string {
	candidate := ""
	if supplierExpr != "" {
		candidate = ` AND (p.supplier_id=` + supplierExpr + ` OR EXISTS(SELECT 1 FROM order_item_origins candidate WHERE candidate.order_item_id=oi.id AND candidate.supplier_id=` + supplierExpr + `) OR COALESCE(p.supplier_id,'')='')`
	}
	return `COALESCE((SELECT COUNT(*) FROM order_items oi JOIN orders o ON o.id=oi.order_id LEFT JOIN products p ON p.id=oi.product_id WHERE oi.wholesale_cost>0 AND o.payment_status='paid' AND o.status IN ('delivered','delivered_confirmed') AND ` + supplierOriginAuthority + ` IS NULL` + candidate + `),0)`
}
