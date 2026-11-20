package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/observability"
	"github.com/google/uuid"
)

type FinanceRepository struct {
	db *sql.DB
}

// ErrDisputeOrderNotDisputable is returned by CreateDispute when the
// atomic status flip affects 0 rows: the order left the disputable
// states (shipped / delivered_unconfirmed / delivered) between the
// handler's check and the flip — e.g. a concurrent confirm won the
// race — or a second dispute already flipped it. The tx rolls back,
// so no second DISP row is created. Callers map it to 409 (M2, round 2).
var ErrDisputeOrderNotDisputable = errors.New("order cannot be disputed in its current status")

// ErrDisputeIDCollision is returned by CreateDispute when the
// generated dispute id collides with an existing row. Callers map it
// to a JSON 409, never a raw DB error body (M2, round 2).
var ErrDisputeIDCollision = errors.New("dispute id already exists")

func NewFinanceRepository(db *sql.DB) *FinanceRepository {
	return &FinanceRepository{db: db}
}

// GetLedgerEntries returns immutable accounting journal records
func (r *FinanceRepository) GetLedgerEntries(ctx context.Context) ([]domain.LedgerEntry, error) {
	query := `
		SELECT id, date, target_account, target_type, type, amount, reason, operator,
		       COALESCE(related_order_or_dispute, ''), created_at
		FROM ledger_entries
		ORDER BY date DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(
			&e.ID, &e.Date, &e.TargetAccount, &e.TargetType, &e.Type, &e.Amount,
			&e.Reason, &e.Operator, &e.RelatedOrderOrDispute, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// AddLedgerEntry appends an immutable entry to the ledger
func (r *FinanceRepository) AddLedgerEntry(ctx context.Context, e domain.LedgerEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var account FinancialAccount
	switch {
	case e.TargetType == "seller" && strings.HasPrefix(e.TargetAccount, "store_payable:"):
		account = FinancialAccount{Type: "seller", ID: strings.TrimPrefix(e.TargetAccount, "store_payable:")}
	case e.TargetType == "supplier" && strings.HasPrefix(e.TargetAccount, "supplier_payable:"):
		account = FinancialAccount{Type: "supplier", ID: strings.TrimPrefix(e.TargetAccount, "supplier_payable:")}
	}
	if account.ID != "" {
		var set TransactionLockSet
		if err := LockAccountsTx(ctx, tx, &set, account); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.ID, e.Date, e.TargetAccount, e.TargetType, e.Type, e.Amount, e.Reason, e.Operator, e.RelatedOrderOrDispute); err != nil {
		return err
	}
	return tx.Commit()
}

// GetDisputes returns all customer return and delivery disputes
func (r *FinanceRepository) GetDisputes(ctx context.Context) ([]domain.Dispute, error) {
	query := `
		SELECT id, order_id, order_number, type, customer_name, product_title, claim_amount,
		       reason, store_id, COALESCE(supplier_name, ''), status,
		       COALESCE(responsible_party, ''), COALESCE(resolution_action, ''),
		       resolution_date, created_at
		FROM disputes
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []domain.Dispute{}
	for rows.Next() {
		var d domain.Dispute
		if err := rows.Scan(
			&d.ID, &d.OrderID, &d.OrderNumber, &d.Type, &d.CustomerName, &d.ProductTitle,
			&d.ClaimAmount, &d.Reason, &d.StoreID, &d.SupplierName, &d.Status,
			&d.ResponsibleParty, &d.ResolutionAction, &d.ResolutionDate, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range list {
		out, err := r.GetDisputeOutcome(ctx, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Outcome = &out
	}
	return list, nil
}

// CreateDispute creates a dispute record and flags order as delivery_disputed.
// Fix69 A5: the order is mandatory. A supplied order id/number that does
// not resolve to a real order fails closed (no dispute row, no status
// flip) instead of inserting a placeholder dispute and flipping nothing.
// Store/customer/product fall back to the resolved order (and, for the
// product, to the order's first item) when the caller leaves them blank;
// a caller-supplied store that differs from the order's store is a
// mismatch and also fails closed. The dispute type is stored as given;
// handlers validate it against the disputes CHECK values.
func (r *FinanceRepository) CreateDispute(ctx context.Context, d domain.Dispute) error {
 if d.ClaimAmount<=0||validateSignedFinanceMoney(d.ClaimAmount)!=nil{return ErrDisputeInput}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	orderRef := d.OrderID
	if orderRef == "" {
		orderRef = d.OrderNumber
	}
	if orderRef == "" {
		return fmt.Errorf("order reference is required")
	}

	var resolvedID, resolvedNumber string
	var orderStoreID sql.NullString
	var customerName string
	if err := tx.QueryRowContext(ctx, `SELECT id, order_number, store_id, customer_full_name FROM orders WHERE id = $1 OR order_number = $1`, orderRef).Scan(&resolvedID, &resolvedNumber, &orderStoreID, &customerName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("order not found: %s", orderRef)
		}
		return err
	}
	if err := lockOrderFinanceTx(ctx, tx, resolvedID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT order_number,store_id,customer_full_name FROM orders WHERE id=$1`, resolvedID).Scan(&resolvedNumber, &orderStoreID, &customerName); err != nil {
		return err
	}
	if err := markDisputeWriterTx(ctx, tx); err != nil {
		return err
	}
	d.OrderID = resolvedID
	d.OrderNumber = resolvedNumber
	if d.StoreID == "" {
		d.StoreID = orderStoreID.String
	} else if orderStoreID.Valid && orderStoreID.String != "" && d.StoreID != orderStoreID.String {
		return fmt.Errorf("store mismatch: dispute store does not match order store")
	}
	if d.CustomerName == "" {
		d.CustomerName = customerName
	}
	if d.ProductTitle == "" {
		var title sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT p.title FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = $1 LIMIT 1`, resolvedID).Scan(&title)
		d.ProductTitle = title.String
	}
	if d.ProductTitle == "" {
		return fmt.Errorf("product title is required or could not be resolved from the order")
	}

	query := `
		INSERT INTO disputes (id, order_id, order_number, type, customer_name, product_title, claim_amount, reason, store_id, supplier_name, status)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11)
	`
	if _, err := tx.ExecContext(ctx, query,
		d.ID, d.OrderID, d.OrderNumber, d.Type, d.CustomerName, d.ProductTitle,
		d.ClaimAmount, d.Reason, d.StoreID, d.SupplierName, "pending",
	); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return ErrDisputeIDCollision
		}
		return err
	}

	// Update order status to delivery_disputed by resolved id only: the
	// old code flipped by the raw order-number input, so a lookup miss
	// silently flipped nothing while still returning success.
	// M2 (round 2): the flip is atomic — the status predicate makes
	// the UPDATE itself the guard, so a dispute whose handler loaded
	// 'shipped' before a concurrent confirm committed can no longer
	// un-confirm the order, and two concurrent disputes cannot both
	// insert: the loser's UPDATE affects 0 rows and its tx rolls back.
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO dispute_order_evidence(dispute_id,order_id,prior_status,prior_disputed_at,prior_delivered_at,prior_confirmed_at,prior_window_ends_at,prior_profit_released,prior_profit_released_at,prior_referral_released,hold_at,evidence_kind,actor_user_id) SELECT $1,id,status,disputed_at,NULL,confirmed_at,return_window_ends_at,profit_released,profit_released_at,referral_commission_released,$2,'admission',COALESCE(customer_user_id::text,'') FROM orders WHERE id=$3`, d.ID, now, resolvedID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'delivery_disputed', disputed_at = $1 WHERE id = $2 AND status IN ('shipped','delivered_unconfirmed','delivered')`, now, resolvedID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrDisputeOrderNotDisputable
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// P0-6 (best-effort): dispute filed - admins (adjudication).
	notifyDisputeFiled(ctx, r.db, d.ID, resolvedNumber, d.Type, d.ClaimAmount)
	return nil
}

// ResolveDispute resolves a dispute and issues compensation without debiting seller wallet if carrier is liable.
// Fix69 A4:
//   - pending guard: the dispute row is locked FOR UPDATE and must still
//     be pending; a second resolution fails closed instead of debiting
//     a second time.
//   - no ledger entry is written for release_to_seller (claim rejected:
//     live toast says exactly that) or when the claim amount is <= 0.
//   - the ledger target type comes from disputePartyLedgerType, which
//     maps both party codes and the Persian labels the panels send,
//     including warehouse; an unmapped party on a ledger-writing
//     resolution fails closed instead of silently defaulting to carrier.
//   - ledger ids are UUIDs (ADJ- + UUID), never ADJ- + millis%10000.
func (r *FinanceRepository) ResolveDispute(ctx context.Context, disputeID, action, party, operator string, claimAmount int64) error {
	_, err := r.ResolveDisputeWithOutcome(ctx, disputeID, DisputeResolutionInput{Action: action, ResponsibleParty: party, ActorUserID: operator, ExpectedClaimAmount: &claimAmount})
	return err
}

// disputePartyLedgerType maps a responsible-party value to a
// ledger_entries target_type. Panels send Persian labels (and, for the
// warehouse action, the refund_warehouse action code); party codes are
// accepted too. The action code is the fallback so a refund_warehouse
// resolution never silently debits the carrier again. 'warehouse' is a
// valid target_type only after migration 202611040011 extends the
// ledger_entries CHECK; without it a warehouse resolution fails closed
// on the CHECK instead of mis-debiting. Unknown parties return false
// (fail closed) rather than defaulting to carrier.
func disputePartyLedgerType(party, action string) (string, bool) {
	switch strings.TrimSpace(party) {
	case "supplier", "تأمین‌کننده", "تأمین‌کننده کالا":
		return "supplier", true
	case "seller", "دراپ‌شیپر", "فروشنده", "سلر":
		return "seller", true
	case "carrier", "شرکت حمل‌ونقل و باربری", "شرکت حمل", "باربری":
		return "carrier", true
	case "warehouse", "انبار", "انبار مرکزی":
		return "warehouse", true
	case "platform", "پلتفرم":
		return "platform", true
	}
	switch action {
	case "refund_supplier":
		return "supplier", true
	case "refund_seller":
		return "seller", true
	case "refund_carrier":
		return "carrier", true
	case "refund_warehouse":
		return "warehouse", true
	}
	return "", false
}

type StoreBalance struct {
	StoreID         string `json:"storeId"`
	Balance         int64  `json:"balance"`
	UnsettledAmount int64  `json:"unsettledAmount"`
	AvailableAmount int64  `json:"availableAmount"`
	// D31 live deductions (0 when seller_early_withdrawals is absent):
	EarlyWithdrawalApprovedFees int64      `json:"earlyWithdrawalApprovedFees"`
	EarlyWithdrawalPendingGross int64      `json:"earlyWithdrawalPendingGross"`
	BonusLocked                 int64      `json:"bonusLocked"`
	BonusAvailable              int64      `json:"bonusAvailable"`
	BonusTotal                  int64      `json:"bonusTotal"`
	ReferralCommissionEarned    int64      `json:"referralCommissionEarned"`
	LastReleasedAt              *time.Time `json:"lastReleasedAt,omitempty"`
	// GAP 6/15 deductions (0 when the source table is absent):
	SellerReturnReversals int64 `json:"sellerReturnReversals"`
	Adjustments           int64 `json:"adjustments"`
}

type StatementEntry struct {
	ID        string    `json:"id"`
	Date      time.Time `json:"date"`
	Type      string    `json:"type"` // credit / debit
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`
	Reference string    `json:"reference"`
}

type StoreStatement struct {
	OpeningAvailable int64 `json:"openingAvailable"`
	ClosingAvailable int64 `json:"closingAvailable"`
	OpeningReserved  int64 `json:"openingReserved"`
	ClosingReserved  int64 `json:"closingReserved"`
	CurrentAvailable int64 `json:"currentAvailable"`

	// OpeningEntry is the explicit reconciled-opening marker (FIN-009
	// D-05): the derived opening amount as its own entry, dated at the
	// month start, so the opening is visible in the response instead
	// of only implied by OpeningAvailable. It is NOT a month flow and
	// is never included in Entries or in the month totals. Nil when
	// the opening is 0. OpeningVerified reports whether any
	// reconciliation evidence backs the opening: FIN-009 records no
	// such evidence, so it is always false here — an unverified
	// opening is flagged, never replaced with invented history.
	// FIN-014 owns the cutover record that can set it true.
	OpeningEntry    *StatementEntry `json:"openingEntry,omitempty"`
	OpeningVerified bool            `json:"openingVerified"`

	// Paging (FIN-009 D-03): TotalEntries is the month's entry count
	// (all pages), NextCursor is the opaque token for the following
	// page (empty on the last page), Limit is the page size that was
	// applied. The unpaged GetStoreStatement returns the whole month:
	// TotalEntries == len(Entries), NextCursor == "", Limit == 0.
	TotalEntries int    `json:"totalEntries"`
	NextCursor   string `json:"nextCursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`

	StoreID     string           `json:"storeId"`
	Month       string           `json:"month"`
	Entries     []StatementEntry `json:"entries"`
	TotalCredit int64            `json:"totalCredit"`
	TotalDebit  int64            `json:"totalDebit"`
	NetBalance  int64            `json:"netBalance"`
}

// Recipient naming (D32, unified): store payouts ALWAYS use
// recipient_type = "seller" and recipient_name = storeID, in both (corrected at integration: migration 000003 CHECK allows only seller/supplier)
// the owner path and the public path. Supplier payouts keep
// recipient_type = "supplier". The old owner path used "seller"; that
// inconsistency is removed by this change.
type SettlementRequest struct {
	ID                  string     `json:"id"`
	StoreID             *string    `json:"storeId,omitempty"`
	SupplierID          *string    `json:"supplierId,omitempty"`
	RecipientType       string     `json:"recipientType"`
	RecipientName       string     `json:"recipientName"`
	TransferReference   *string    `json:"transferReference,omitempty"`
	TransferConfirmedBy *string    `json:"transferConfirmedBy,omitempty"`
	TransferConfirmedAt *time.Time `json:"transferConfirmedAt,omitempty"`
	Shaba               string     `json:"shaba"`
	BankName            string     `json:"bankName"`
	Amount              int64      `json:"amount"`
	Status              string     `json:"status"`
	AvailableBalance    int64      `json:"availableBalance"`
	RejectionReason     *string    `json:"rejectionReason,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	ProcessedAt         *time.Time `json:"processedAt,omitempty"`
}

type RevenueReport struct {
	Period          string `json:"period"`
	TotalRevenue    int64  `json:"totalRevenue"`
	PlatformFees    int64  `json:"platformFees"`
	SellerPayouts   int64  `json:"sellerPayouts"`
	SupplierPayouts int64  `json:"supplierPayouts"`
	// SellerProfit is the dropshipper profit EARNED in the period
	// (SUM(dropshipper_profit) over non-cancelled orders). It was
	// previously reported under the sellerPayouts name; sellerPayouts
	// now means money actually paid out (D32: payout_requests in
	// completed/transfer_confirmed only).
	SellerProfit int64 `json:"sellerProfit"`
}

// parseRevenuePeriod parses "YYYY-Qn" (calendar quarter) or "YYYY-MM"
// (calendar month) into [start, end). Anything else is an
// "invalid period" error, mapped to 400 by the handler.
func parseRevenuePeriod(period string) (time.Time, time.Time, error) {
	if len(period) == 7 && period[4] == '-' && period[5] == 'Q' {
		var year, q int
		if _, err := fmt.Sscanf(period, "%d-Q%d", &year, &q); err != nil || q < 1 || q > 4 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid period %q: expected YYYY-Qn", period)
		}
		start := time.Date(year, time.Month((q-1)*3+1), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 3, 0), nil
	}
	if len(period) == 7 && period[4] == '-' {
		start, err := time.Parse("2006-01", period)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid period %q: expected YYYY-MM", period)
		}
		return start, start.AddDate(0, 1, 0), nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("invalid period %q: expected YYYY-Qn or YYYY-MM", period)
}

// GetLedgerFilteredPage returns one cursor page of ledger entries in a
// bounded date window, newest first, keyed on (date DESC, id DESC).
// Window contract (PERF-02 Step 6): dates are YYYY-MM-DD; both empty
// defaults to the last DefaultReportWindowDays days; a window over
// MaxReportWindowDays is rejected. The old unbounded, dateless full-table
// scan (100,002 rows + a 6,472 kB sort spill to disk) is gone.
func (r *FinanceRepository) GetLedgerFilteredPage(ctx context.Context, dateFrom, dateTo string, opts ListPageOptions) (ListPage[domain.LedgerEntry], error) {
	limit := NormalizeListLimit(opts.Limit)
	if dateFrom == "" && dateTo == "" {
		dateFrom, dateTo = DefaultReportWindow()
	}
	if err := ValidateReportWindow(dateFrom, dateTo); err != nil {
		return ListPage[domain.LedgerEntry]{}, err
	}
	query := `
		SELECT id, date, target_account, target_type, type, amount, reason, operator,
		       COALESCE(related_order_or_dispute, ''), created_at
		FROM ledger_entries
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1
	if dateFrom != "" {
		query += fmt.Sprintf(" AND date >= $%d", argIdx)
		args = append(args, dateFrom)
		argIdx++
	}
	if dateTo != "" {
		query += fmt.Sprintf(" AND date <= $%d", argIdx)
		args = append(args, dateTo)
		argIdx++
	}
	if opts.Cursor != "" {
		cursorAt, cursorID, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return ListPage[domain.LedgerEntry]{}, err
		}
		query += fmt.Sprintf(" AND (date, id) < ($%d::timestamptz, $%d::text)", argIdx, argIdx+1)
		args = append(args, cursorAt, cursorID)
		argIdx += 2
	}
	query += fmt.Sprintf(" ORDER BY date DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListPage[domain.LedgerEntry]{}, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(
			&e.ID, &e.Date, &e.TargetAccount, &e.TargetType, &e.Type, &e.Amount,
			&e.Reason, &e.Operator, &e.RelatedOrderOrDispute, &e.CreatedAt,
		); err != nil {
			return ListPage[domain.LedgerEntry]{}, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return ListPage[domain.LedgerEntry]{}, err
	}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := entries[len(entries)-1]
		nextCursor = EncodeCreatedAtCursor(last.Date, last.ID)
	}
	return ListPage[domain.LedgerEntry]{Items: entries, NextCursor: nextCursor, HasMore: hasMore, Limit: limit}, nil
}

// GetLedgerFiltered returns ledger entries with date bounds. Legacy
// compat: the first page only, at most MaxListLimit rows, default 31-day
// window when no dates are given. HTTP handlers use
// GetLedgerFilteredPage.
func (r *FinanceRepository) GetLedgerFiltered(ctx context.Context, dateFrom, dateTo string) ([]domain.LedgerEntry, error) {
	page, err := r.GetLedgerFilteredPage(ctx, dateFrom, dateTo, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// GetStoreBalance calculates real-time store financial balance including referral bonuses and 2% referral commissions
func (r *FinanceRepository) GetStoreBalance(ctx context.Context, storeID string) (*StoreBalance, error) {
	// FIN-013: per-query finance latency for the one-store balance
	// read (audit §2.6 Q2) — one histogram sample per call, outcome
	// only, no amounts or identifiers. Wired here, the single entry
	// point for the standalone balance read (balance endpoint, seller
	// panel, settlement sweep), not inside scanStoreBalance — the
	// statement path calls scanStoreBalance as a sub-step of statement
	// generation and must not be counted as a standalone balance read.
	started := time.Now()
	balance, err := scanStoreBalance(ctx, r.db, storeID)
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	observability.Default.Observe("sql", "store_balance", outcome, time.Since(started))
	return balance, err
}

// GetStorePayoutsPage returns one cursor page of a store's payout requests,
// newest first, keyed on (created_at DESC, id DESC). PERF-02 Step 6.
func (r *FinanceRepository) GetStorePayoutsPage(ctx context.Context, ownerUserID, storeID string, opts ListPageOptions) (ListPage[SettlementRequest], error) {
	limit := NormalizeListLimit(opts.Limit)
	var owns bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stores WHERE id = $1 AND owner_user_id = $2)`, storeID, ownerUserID).Scan(&owns); err != nil {
		return ListPage[SettlementRequest]{}, err
	}
	if !owns {
		return ListPage[SettlementRequest]{}, ErrStoreForbidden
	}
	query := `
		SELECT id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status,
		       COALESCE(available_balance,0), COALESCE(rejection_reason,''), created_at, processed_at,
		       transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests WHERE store_id = $1`
	args := []interface{}{storeID}
	if opts.Cursor != "" {
		cursorAt, cursorID, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return ListPage[SettlementRequest]{}, err
		}
		query += " AND (created_at, id) < ($2::timestamptz, $3::text)"
		args = append(args, cursorAt, cursorID)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListPage[SettlementRequest]{}, err
	}
	defer rows.Close()
	result := []SettlementRequest{}
	for rows.Next() {
		var item SettlementRequest
		if err := rows.Scan(&item.ID, &item.StoreID, &item.RecipientType, &item.RecipientName, &item.Shaba, &item.BankName, &item.Amount, &item.Status, &item.AvailableBalance, &item.RejectionReason, &item.CreatedAt, &item.ProcessedAt, &item.TransferReference, &item.TransferConfirmedBy, &item.TransferConfirmedAt); err != nil {
			return ListPage[SettlementRequest]{}, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return ListPage[SettlementRequest]{}, err
	}
	hasMore := len(result) > limit
	if hasMore {
		result = result[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := result[len(result)-1]
		nextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
	}
	return ListPage[SettlementRequest]{Items: result, NextCursor: nextCursor, HasMore: hasMore, Limit: limit}, nil
}

// GetStorePayouts returns a store's payout requests. Legacy compat: the
// first page only, at most MaxListLimit rows. HTTP handlers use
// GetStorePayoutsPage.
func (r *FinanceRepository) GetStorePayouts(ctx context.Context, ownerUserID, storeID string) ([]SettlementRequest, error) {
	page, err := r.GetStorePayoutsPage(ctx, ownerUserID, storeID, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (r *FinanceRepository) RequestOwnedStorePayout(ctx context.Context, ownerUserID, storeID, shaba, bankName string, amount int64, clientKeys ...string) (*SettlementRequest, error) {
	if amount <= 0 {
		return nil, ErrStoreInput
	}
	command, err := newFinanceCommand(FinancialAccount{Type: "seller", ID: storeID}, "payout-request-v1", clientKeys, []interface{}{ownerUserID, shaba, bankName, amount})
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var accountLocks TransactionLockSet
	if err := LockAccountsTx(ctx, tx, &accountLocks, FinancialAccount{Type: "seller", ID: storeID}); err != nil {
		return nil, err
	}
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2`, storeID, ownerUserID).Scan(&lockedID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrStoreForbidden
		}
		return nil, err
	}
	var original SettlementRequest
	if replay, err := command.replayTx(ctx, tx, &original); err != nil {
		return nil, err
	} else if replay {
		return &original, nil
	}
	available, balanceErr := BalanceTx(ctx, tx, FinancialAccount{Type: "seller", ID: storeID})
	if err := balanceErr; err != nil {
		return nil, err
	}
	if amount > available {
		// FIN-013: the rejection itself is the signal — a payout above
		// signed availability is refused here, never partially paid.
		// The counter carries only the bounded reason, never the
		// amounts or the account.
		observability.Default.RecordFinanceReservationRejected("exceeds_availability")
		return nil, ErrStoreInput
	}
	id := "pay-req-" + uuid.NewString()
	var item SettlementRequest
	if err := tx.QueryRowContext(ctx, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at) VALUES($1,$2,'seller',$2,$3,$4,$5,'pending',$6,NOW()) RETURNING id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at`, id, storeID, shaba, bankName, amount, available).Scan(&item.ID, &item.StoreID, &item.RecipientType, &item.RecipientName, &item.Shaba, &item.BankName, &item.Amount, &item.Status, &item.AvailableBalance, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err := command.recordTx(ctx, tx, item); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// P1-6 (best-effort): payout requested - admins.
	notifyPayoutRequested(ctx, r.db, item.ID, "تسویه فروشگاه", item.Amount, storeID)
	return &item, nil
}

// RequestStorePayout discovers the owner, then delegates locked authorization and creation.
func (r *FinanceRepository) RequestStorePayout(ctx context.Context, storeID, shaba, bankName string, amount int64, clientKeys ...string) (*SettlementRequest, error) {
	var owner string
	if err := r.db.QueryRowContext(ctx, `SELECT owner_user_id FROM stores WHERE id=$1`, storeID).Scan(&owner); err != nil {
		return nil, err
	}
	return r.RequestOwnedStorePayout(ctx, owner, storeID, shaba, bankName, amount, clientKeys...)
}

// statementPostedSQL is the canonical posted-flow union behind
// GetStoreStatement. Every arm carries (id, occurred, direction, amount,
// reason); the month filter, the undated guard and the projections all
// read this one definition, so the balance, the statement and the
// treasury hold cannot drift apart. The sort key is (occurred DESC, id,
// reason): a total order — the two legs of one released order share
// (occurred, id) and are ordered deterministically by reason — and the
// paging cursor carries exactly that key.
func statementPostedSQL() string {
	return `SELECT id::text,profit_released_at occurred,'credit' direction,dropshipper_profit amount,'released_profit' reason FROM orders WHERE store_id=$1 AND profit_released AND status IN ('delivered','delivered_confirmed')
 UNION ALL SELECT id::text,released_at,'credit',amount,'referral_bonus' FROM referral_bonuses WHERE referrer_store_id=$1 AND status='released'
 UNION ALL SELECT id::text,profit_released_at,'credit',referral_commission,'referral_commission' FROM orders WHERE referrer_seller_id=$1 AND referral_commission_released AND status<>'cancelled'
 UNION ALL SELECT r.id,r.created_at,'debit',r.amount,'referral_commission_reversal' FROM referral_commission_reversals r WHERE r.referrer_store_id=$1 AND EXISTS(SELECT 1 FROM orders o WHERE o.id=r.order_id AND o.referral_commission_released AND o.status<>'cancelled')
 UNION ALL SELECT id::text,created_at,CASE WHEN operation='debit' THEN 'debit' ELSE 'credit' END,amount,'seller_return' FROM customer_return_financial_events WHERE store_id=$1 AND component='seller_profit'
 UNION ALL SELECT r.id,r.created_at,'debit',r.amount,'seller_return' FROM seller_profit_reversals r WHERE r.store_id=$1 AND EXISTS(SELECT 1 FROM orders o WHERE o.id=r.order_id AND o.profit_released AND o.status IN ('delivered','delivered_confirmed'))
 UNION ALL SELECT id::text,date,type,amount,'finance_adjustment' FROM ledger_entries WHERE target_account='store_payable:' || $1 AND target_type='seller' AND reason LIKE 'سند تعدیل%'
 UNION ALL SELECT id::text,COALESCE(processed_at,created_at),'debit',amount,'payout' FROM payout_requests WHERE store_id=$1 AND status IN ('paid','processing','completed','transfer_confirmed')
 UNION ALL SELECT id::text,created_at,'debit',amount,'service' FROM seller_service_purchases WHERE store_id=$1
 UNION ALL SELECT id::text,processed_at,'debit',fee_amount,'early_fee' FROM seller_early_withdrawals WHERE store_id=$1 AND status='approved'`
}

// statementReasonLabels maps union reason keys to display strings.
var statementReasonLabels = map[string]string{
	"released_profit":              "سود آزادشده سفارش",
	"referral_bonus":               "پاداش معرف آزادشده",
	"referral_commission":          "کمیسیون معرفی آزادشده",
	"referral_commission_reversal": "برگشت کمیسیون معرفی",
	"seller_return":                "برگشت سود فروشنده",
	"finance_adjustment":           "سند تعدیل حساب",
	"payout":                       "تسویه و واریز به حساب شبا",
	"service":                      "خرید سرویس",
	"early_fee":                    "کارمزد برداشت زودهنگام",
}

// StatementPageOptions bounds one page of a store statement.
type StatementPageOptions struct {
	// Limit caps the entries in one page (default 50, clamped to 1..200).
	Limit int
	// Cursor is the opaque page token returned as NextCursor by the
	// previous page; empty starts at the first page.
	Cursor string
}

// statementPageLimit normalises the requested page size.
func statementPageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

// encodeStatementCursor builds the opaque cursor for the last entry of a
// page from its sort key (occurred, id, reason). The key is raw (the
// union's reason key, not the display label), so the predicate below
// compares the same values the ORDER BY sorts by.
func encodeStatementCursor(occurred time.Time, id, reason string) string {
	raw := occurred.Format(time.RFC3339Nano) + "\n" + id + "\n" + reason
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeStatementCursor parses a page cursor back into its sort key.
func decodeStatementCursor(cursor string) (time.Time, string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("invalid statement cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "\n", 3)
	if len(parts) != 3 {
		return time.Time{}, "", "", fmt.Errorf("invalid statement cursor")
	}
	occurred, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("invalid statement cursor: %w", err)
	}
	return occurred, parts[1], parts[2], nil
}

// statementOpeningEntry builds the explicit opening marker for a
// statement month (FIN-009 D-05). The amount stays derived (the
// projection sum); the entry makes it explicit in the response. Nil
// when the opening is 0. The entry is never a month flow and is never
// included in Entries or the month totals.
func statementOpeningEntry(month string, start time.Time, opening int64) *StatementEntry {
	if opening == 0 {
		return nil
	}
	typ := "credit"
	amount := opening
	if opening < 0 {
		typ = "debit"
		amount = -opening
	}
	return &StatementEntry{
		ID: "opening:" + month, Date: start, Type: typ, Amount: amount,
		Reason: "مانده ابتدای دوره", Reference: "opening",
	}
}

// statementCutoffs computes (openingAvailable, openingReserved,
// closingAvailable, closingReserved) for [start, end) in one
// repeatable-read snapshot.
func statementCutoffs(ctx context.Context, tx *sql.Tx, storeID string, start, end time.Time) (openingAvailable, openingReserved, closingAvailable, closingReserved int64, err error) {
	posted := statementPostedSQL()
	reserve := `COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id=$1 AND created_at<$2 AND ((status IN ('pending','approved')) OR (COALESCE(processed_at,created_at)>=$2 AND status IN ('rejected','paid','processing','completed','transfer_confirmed')))),0)+COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND created_at<$2 AND (status='pending' OR (processed_at>=$2 AND status IN ('approved','rejected')))),0)`
	projection := func(cutoff time.Time) (int64, int64, error) {
		var net, reserved int64
		err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT SUM(CASE WHEN direction='credit' THEN amount ELSE -amount END) FROM (`+posted+`) e WHERE occurred<$2),0),`+reserve, storeID, cutoff).Scan(&net, &reserved)
		if err != nil {
			return 0, 0, err
		}
		available, err := signedBalanceSum([]int64{net}, []int64{reserved})
		return available, reserved, err
	}
	openingAvailable, openingReserved, err = projection(start)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	closingAvailable, closingReserved, err = projection(end)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return openingAvailable, openingReserved, closingAvailable, closingReserved, nil
}

// statementFinish fills the cutoff-derived fields, the current canonical
// balance and the explicit opening marker, then commits. The opening
// stays unverified: FIN-009 records no reconciliation evidence for it
// (FIN-014 owns the cutover record that can set OpeningVerified true).
func statementFinish(ctx context.Context, tx *sql.Tx, result *StoreStatement, start, end time.Time) error {
	var err error
	result.OpeningAvailable, result.OpeningReserved, result.ClosingAvailable, result.ClosingReserved, err = statementCutoffs(ctx, tx, result.StoreID, start, end)
	if err != nil {
		return err
	}
	// Includes the current canonical signed amount explicitly, without confusing it
	// with a historical cutoff when later events have already posted.
	balance, err := scanStoreBalance(ctx, tx, result.StoreID)
	if err != nil {
		return err
	}
	result.CurrentAvailable = balance.AvailableAmount
	result.OpeningEntry = statementOpeningEntry(result.Month, start, result.OpeningAvailable)
	result.OpeningVerified = false
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// statementMonthWindow parses a YYYY-MM statement month into the
// [start, end) window of Asia/Tehran midnights (FIN-009 Q-02: TEHRAN —
// owner decision Nov 19, superseding the FIN-001 UTC month). start is
// 00:00 Asia/Tehran on the 1st of the month; end is 00:00 Asia/Tehran
// on the 1st of the next month (start.AddDate(0, 1, 0) in that
// location). The occurred values are timestamptz, so comparing them
// against these absolute instants is correct regardless of session
// timezone. The opening/closing projection cutoffs in
// statementCutoffs flow from the same start/end, so no cutoff is left
// in UTC; the opening entry is likewise dated at the Tehran start.
//
// The location is loaded with time.LoadLocation — the same mechanism
// as the storage-fee precedent (storage_fee_repo.go
// loadTehranLocation) — but a load failure is returned as an error
// here, never a silent fallback to UTC: a Tehran month computed in UTC
// would silently move the 00:00–03:29 Tehran window on the 1st into
// the previous month. The load cannot fail in practice: time/tzdata
// is embedded in the binary (imported by storage_history_policy.go in
// this package) and the system zoneinfo database also carries
// Asia/Tehran (verified in the test environment).
func statementMonthWindow(month string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("statement month timezone Asia/Tehran unavailable: %w", err)
	}
	start, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid month: %w", err)
	}
	return start, start.AddDate(0, 1, 0), nil
}

// GetStoreStatement separates posted flows from reservations. Historical cutoffs
// project the immutable event dates and current state records; missing financial
// event dates require reconciliation rather than an invented posting period.
func (r *FinanceRepository) GetStoreStatement(ctx context.Context, storeID, month string) (*StoreStatement, error) {
	start, end, err := statementMonthWindow(month)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	posted := statementPostedSQL()
	var undated int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+posted+`) e WHERE amount<>0 AND occurred IS NULL`, storeID).Scan(&undated); err != nil {
		return nil, err
	}
	if undated > 0 {
		return nil, ErrFinancePrincipalUnresolved
	}
	result := &StoreStatement{StoreID: storeID, Month: month, Entries: []StatementEntry{}}
	rows, err := tx.QueryContext(ctx, `SELECT id,occurred,direction,amount,reason FROM (`+posted+`) e WHERE amount>0 AND occurred>=$2 AND occurred<$3 ORDER BY occurred DESC,id,reason`, storeID, start, end)
	if err != nil {
		return nil, err
	}
	var credits, debits []int64
	for rows.Next() {
		var e StatementEntry
		if err := rows.Scan(&e.ID, &e.Date, &e.Type, &e.Amount, &e.Reason); err != nil {
			rows.Close()
			return nil, err
		}
		e.Reason = statementReasonLabels[e.Reason]
		e.Reference = e.ID
		result.Entries = append(result.Entries, e)
		if e.Type == "credit" {
			credits = append(credits, e.Amount)
		} else {
			debits = append(debits, e.Amount)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result.TotalCredit, err = signedBalanceSum(credits, nil)
	if err != nil {
		return nil, err
	}
	result.TotalDebit, err = signedBalanceSum(debits, nil)
	if err != nil {
		return nil, err
	}
	result.NetBalance, err = signedBalanceSum(credits, debits)
	if err != nil {
		return nil, err
	}
	// Unpaged: the whole month is one page.
	result.TotalEntries = len(result.Entries)
	if err := statementFinish(ctx, tx, result, start, end); err != nil {
		return nil, err
	}
	return result, nil
}

// GetStoreStatementPage returns one page of a store statement (FIN-009
// D-03). Entries are ordered by (occurred DESC, id, reason) — a total
// order, so the same-order profit/commission legs always come out in
// the same relative order — and the cursor carries exactly that key, so
// paging round-trips every entry exactly once. TotalCredit, TotalDebit,
// NetBalance and TotalEntries are MONTH totals (all pages), not page
// totals; the opening/closing/current fields are the same projections
// GetStoreStatement returns.
func (r *FinanceRepository) GetStoreStatementPage(ctx context.Context, storeID, month string, opts StatementPageOptions) (*StoreStatement, error) {
	start, end, err := statementMonthWindow(month)
	if err != nil {
		return nil, err
	}
	limit := statementPageLimit(opts.Limit)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	posted := statementPostedSQL()
	var undated int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+posted+`) e WHERE amount<>0 AND occurred IS NULL`, storeID).Scan(&undated); err != nil {
		return nil, err
	}
	if undated > 0 {
		return nil, ErrFinancePrincipalUnresolved
	}
	result := &StoreStatement{StoreID: storeID, Month: month, Entries: []StatementEntry{}, Limit: limit}
	// Month totals (all pages) in one aggregate pass.
	var totalCredit, totalDebit int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN direction='credit' THEN amount ELSE 0 END),0),COALESCE(SUM(CASE WHEN direction='debit' THEN amount ELSE 0 END),0) FROM (`+posted+`) e WHERE amount>0 AND occurred>=$2 AND occurred<$3`, storeID, start, end).Scan(&result.TotalEntries, &totalCredit, &totalDebit); err != nil {
		return nil, err
	}
	result.TotalCredit = totalCredit
	result.TotalDebit = totalDebit
	if result.NetBalance, err = signedBalanceSum([]int64{totalCredit}, []int64{totalDebit}); err != nil {
		return nil, err
	}
	// Page rows: strictly after the cursor in (occurred DESC, id, reason)
	// order. limit+1 rows detect whether a next page exists.
	args := []interface{}{storeID, start, end, limit + 1}
	predicate := ""
	if opts.Cursor != "" {
		cursorOccurred, cursorID, cursorReason, err := decodeStatementCursor(opts.Cursor)
		if err != nil {
			return nil, err
		}
		predicate = ` AND (occurred<$5 OR (occurred=$5 AND (id>$6 OR (id=$6 AND reason>$7))))`
		args = append(args, cursorOccurred, cursorID, cursorReason)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,occurred,direction,amount,reason FROM (`+posted+`) e WHERE amount>0 AND occurred>=$2 AND occurred<$3`+predicate+` ORDER BY occurred DESC,id,reason LIMIT $4`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e StatementEntry
		var rawReason string
		if err := rows.Scan(&e.ID, &e.Date, &e.Type, &e.Amount, &rawReason); err != nil {
			rows.Close()
			return nil, err
		}
		if len(result.Entries) == limit {
			// limit+1-th row: a next page exists; the cursor is the
			// last entry of THIS page. Its raw reason key rides in
			// Reference until the cursor is built below.
			last := result.Entries[limit-1]
			result.NextCursor = encodeStatementCursor(last.Date, last.ID, last.Reference)
			rows.Close()
			break
		}
		e.Reason = statementReasonLabels[rawReason]
		e.Reference = rawReason
		result.Entries = append(result.Entries, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range result.Entries {
		result.Entries[i].Reference = result.Entries[i].ID
	}
	if err := statementFinish(ctx, tx, result, start, end); err != nil {
		return nil, err
	}
	return result, nil
}

// GetPendingSettlementsPage returns one cursor page of payout requests
// awaiting finance approval, newest first, keyed on (created_at DESC,
// id DESC). PERF-02 Step 6.
func (r *FinanceRepository) GetPendingSettlementsPage(ctx context.Context, opts ListPageOptions) (ListPage[SettlementRequest], error) {
	limit := NormalizeListLimit(opts.Limit)
	query := `
		SELECT id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name,
		       amount, status, rejection_reason, created_at, processed_at,
		       transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests
		WHERE status = 'pending'
	`
	args := []interface{}{}
	if opts.Cursor != "" {
		cursorAt, cursorID, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return ListPage[SettlementRequest]{}, err
		}
		query += " AND (created_at, id) < ($1::timestamptz, $2::text)"
		args = append(args, cursorAt, cursorID)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListPage[SettlementRequest]{}, err
	}
	defer rows.Close()

	var list []SettlementRequest
	for rows.Next() {
		var s SettlementRequest
		if err := rows.Scan(
			&s.ID, &s.StoreID, &s.SupplierID, &s.RecipientType, &s.RecipientName, &s.Shaba, &s.BankName,
			&s.Amount, &s.Status, &s.RejectionReason, &s.CreatedAt, &s.ProcessedAt,
			&s.TransferReference, &s.TransferConfirmedBy, &s.TransferConfirmedAt,
		); err != nil {
			return ListPage[SettlementRequest]{}, err
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return ListPage[SettlementRequest]{}, err
	}
	hasMore := len(list) > limit
	if hasMore {
		list = list[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := list[len(list)-1]
		nextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
	}
	return ListPage[SettlementRequest]{Items: list, NextCursor: nextCursor, HasMore: hasMore, Limit: limit}, nil
}

// GetPendingSettlements returns payout requests awaiting finance approval.
// Legacy compat: the first page only, at most MaxListLimit rows. HTTP
// handlers use GetPendingSettlementsPage.
func (r *FinanceRepository) GetPendingSettlements(ctx context.Context) ([]SettlementRequest, error) {
	page, err := r.GetPendingSettlementsPage(ctx, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// GetSettlementDetails returns a single payout request
func (r *FinanceRepository) GetSettlementDetails(ctx context.Context, id string) (*SettlementRequest, error) {
	query := `
		SELECT id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name,
		       amount, status, rejection_reason, created_at, processed_at,
		       transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests
		WHERE id = $1
	`
	var s SettlementRequest
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.StoreID, &s.SupplierID, &s.RecipientType, &s.RecipientName, &s.Shaba, &s.BankName,
		&s.Amount, &s.Status, &s.RejectionReason, &s.CreatedAt, &s.ProcessedAt,
		&s.TransferReference, &s.TransferConfirmedBy, &s.TransferConfirmedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// ReviewSettlement updates status of payout and logs to ledger
func (r *FinanceRepository) ReviewSettlement(ctx context.Context, id, status, reason, operator string) error {
	if status != "approved" && status != "rejected" {
		return ErrStoreInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPayoutAccountsTx(ctx, tx, id); err != nil {
		return err
	}

	now := time.Now()
	var amount int64
	var recName string
	var recType string

	var payoutStoreID, payoutSupplierID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT amount, recipient_name, recipient_type, store_id, supplier_id
		FROM payout_requests
		WHERE id = $1 AND status = 'pending'
		FOR UPDATE
	`, id).Scan(&amount, &recName, &recType, &payoutStoreID, &payoutSupplierID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("settlement is missing or already reviewed")
	}
	if err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE payout_requests
		SET status = $1, rejection_reason = NULLIF($2, ''), processed_at = $3
		WHERE id = $4
	`, status, reason, now, id)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return fmt.Errorf("settlement was not pending")
	}

	// P0-10 (best-effort, after the commit below): payout reviewed -
	// store/supplier owner (approved/rejected + reason).
	reviewedStatus, reviewedReason := status, reason
	reviewedAmount := amount
	reviewedStore, reviewedSupplier := payoutStoreID.String, payoutSupplierID.String

	// D32: "approved" is ONLY a manual-transfer marker. No ledger
	// entry is written here — money has not moved yet. The ledger debit
	// is recorded by ConfirmManualTransfer when the transfer is
	// confirmed (status = transfer_confirmed). The old code wrote the
	// debit at approval with a non-unique PAY-LED-<millis%100000> id;
	// both defects are removed by this change. Note: amount/recName/
	// recType are still read (and validated) above so that a review of
	// a missing row fails closed; a missing row fails closed.
	_ = amount
	_ = recName
	_ = recType

	if err := tx.Commit(); err != nil {
		return err
	}
	notifyPayoutReviewed(ctx, r.db, id, reviewedStatus, reviewedReason, reviewedAmount, reviewedStore, reviewedSupplier)
	return nil
}

// ConfirmManualTransfer records a real manual bank transfer (D32, live
// handler p3bPayoutConfirmManualTransfer pattern). Only a payout in
// status "approved" can be confirmed. It sets status =
// transfer_confirmed with the transfer reference, transfer_confirmed_by
// = 'admin' (exactly as in the live SQL string), and transfer/processed
// timestamps, and writes the ledger debit HERE — not at approval —
// with a UNIQUE id (PAY-LED- + UUID, never the old millis%100000
// pattern). Views count only completed/transfer_confirmed as money-out
// (TODO U16: payout_requests is 0 rows in live, so this flow is
// unexercised in live and is implemented from the confirmed state
// machine, not from a guessed PSP transfer).
// ConfirmManualTransfer is ConfirmManualTransferWithDate with no
// transfer_date (the /finance/settlements transfer-confirmed path,
// which carries no date, keeps this exact signature and behaviour).
func (r *FinanceRepository) ConfirmManualTransfer(ctx context.Context, id, transferReference, operator string) error {
	return r.ConfirmManualTransferWithDate(ctx, id, transferReference, operator, nil)
}

// ConfirmManualTransferWithDate records a real manual bank transfer (D32)
// in ONE transaction: approved-only status move, transfer_date (when
// non-nil) and the ledger debit commit or roll back together. A nil
// transferDate leaves the column untouched, exactly as the old method did.
func (r *FinanceRepository) ConfirmManualTransferWithDate(ctx context.Context, id, transferReference, operator string, transferDate *time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPayoutAccountsTx(ctx, tx, id); err != nil {
		return err
	}

	now := time.Now()
	var amount int64
	var recName string
	var recType string

	var confirmStoreID, confirmSupplierID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT amount, recipient_name, recipient_type, store_id, supplier_id
		FROM payout_requests
		WHERE id = $1 AND status = 'approved'
		FOR UPDATE
	`, id).Scan(&amount, &recName, &recType, &confirmStoreID, &confirmSupplierID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("payout is missing or not approved")
	}
	if err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE payout_requests
		SET status = 'transfer_confirmed', transfer_reference = $1, transfer_confirmed_by = 'admin', transfer_confirmed_at = NOW(), processed_at = $2
		WHERE id = $3
	`, transferReference, now, id)
	if err != nil {
		return err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return fmt.Errorf("payout was not approved")
	}

	// W2: transfer_date inside the same transaction (was a second,
	// post-commit UPDATE in the admin handler, so a failure there left
	// a confirmed payout with no date). Any error here rolls the whole
	// confirmation back via the deferred Rollback.
	if transferDate != nil {
		dateResult, dateErr := tx.ExecContext(ctx, "UPDATE payout_requests SET transfer_date = $1 WHERE id = $2", *transferDate, id)
		if dateErr != nil {
			return dateErr
		}
		if dateRows, dateRowsErr := dateResult.RowsAffected(); dateRowsErr != nil || dateRows != 1 {
			return fmt.Errorf("payout transfer_date was not stored")
		}
	}

	confirmedAmount, confirmedStore, confirmedSupplier := amount, confirmStoreID.String, confirmSupplierID.String
	if amount > 0 {
		ledgerID := "PAY-LED-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
			VALUES ($1, $2, $3, $4, 'debit', $5, $6, $7, $8)
		`, ledgerID, now, recName, recType, amount, "تسویه پایا بانکی تایید شده", operator, id); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// P0-10 (best-effort): transfer confirmed - store/supplier owner
	// (amount + reference).
	notifyPayoutTransferConfirmed(ctx, r.db, id, transferReference, confirmedAmount, confirmedStore, confirmedSupplier)
	return nil
}

// GetRevenueReport aggregates revenue, platform fees, seller payouts and
// supplier payouts for exactly one period ("YYYY-Qn" calendar quarter or
// "YYYY-MM" calendar month). Order figures cover non-cancelled orders
// created in the period; payout figures cover payout_requests actually
// paid out (D32: completed/transfer_confirmed) created in the period.
// The previous implementation ignored the period entirely (all-time
// sums) and labelled earned dropshipper profit as sellerPayouts.
func (r *FinanceRepository) GetRevenueReport(ctx context.Context, period string) (*RevenueReport, error) {
	start, end, err := parseRevenuePeriod(period)
	if err != nil {
		return nil, err
	}

	var totalRevenue int64
	var platformFees int64
	var sellerProfit int64

	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(payable_amount), 0),
		       COALESCE(SUM(platform_profit), 0),
		       COALESCE(SUM(dropshipper_profit), 0)
		FROM orders
		WHERE status != 'cancelled' AND created_at >= $1 AND created_at < $2
	`, start, end).Scan(&totalRevenue, &platformFees, &sellerProfit); err != nil {
		return nil, err
	}

	var sellerPayouts int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM payout_requests
		WHERE recipient_type = 'seller' AND status IN ('completed', 'transfer_confirmed')
		  AND created_at >= $1 AND created_at < $2
	`, start, end).Scan(&sellerPayouts); err != nil {
		return nil, err
	}

	var supplierPayouts int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM payout_requests
		WHERE recipient_type = 'supplier' AND status IN ('completed', 'transfer_confirmed')
		  AND created_at >= $1 AND created_at < $2
	`, start, end).Scan(&supplierPayouts); err != nil {
		return nil, err
	}

	return &RevenueReport{
		Period:          period,
		TotalRevenue:    totalRevenue,
		PlatformFees:    platformFees,
		SellerPayouts:   sellerPayouts,
		SupplierPayouts: supplierPayouts,
		SellerProfit:    sellerProfit,
	}, nil
}
