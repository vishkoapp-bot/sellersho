package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
)

type FinanceRepository struct {
	db *sql.DB
}

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
	query := `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.ExecContext(ctx, query,
		e.ID, e.Date, e.TargetAccount, e.TargetType, e.Type, e.Amount, e.Reason, e.Operator, e.RelatedOrderOrDispute,
	)
	return err
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

	var list []domain.Dispute
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
		return err
	}

	// Update order status to delivery_disputed by resolved id only: the
	// old code flipped by the raw order-number input, so a lookup miss
	// silently flipped nothing while still returning success.
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'delivery_disputed', disputed_at = $1 WHERE id = $2`, now, resolvedID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("order not found: %s", orderRef)
	}

	return tx.Commit()
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM disputes WHERE id = $1 FOR UPDATE`, disputeID).Scan(&status); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("dispute not found: %s", disputeID)
		}
		return err
	}
	if status != "pending" {
		return fmt.Errorf("dispute already resolved: %s", disputeID)
	}

	now := time.Now()
	updateDispute := `
		UPDATE disputes
		SET status = 'resolved', resolution_action = $1, responsible_party = $2, resolution_date = $3
		WHERE id = $4 AND status = 'pending'
	`
	res, err := tx.ExecContext(ctx, updateDispute, action, party, now, disputeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("dispute already resolved: %s", disputeID)
	}

	// No ledger entry for a rejected claim or a non-positive amount.
	if action == "release_to_seller" || claimAmount <= 0 {
		return tx.Commit()
	}

	targetType, ok := disputePartyLedgerType(party, action)
	if !ok {
		return fmt.Errorf("unknown responsible party: %s", party)
	}

	ledgerID := "ADJ-" + uuid.NewString()
	insertLedger := `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, $2, $3, $4, 'debit', $5, $6, $7, $8)
	`
	reason := fmt.Sprintf("صدور حکم داوری پرونده %s به مسئولیت %s", disputeID, party)
	if _, err := tx.ExecContext(ctx, insertLedger, ledgerID, now, party, targetType, claimAmount, reason, operator, disputeID); err != nil {
		return err
	}

	return tx.Commit()
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

// GetLedgerFiltered returns ledger entries with date bounds
func (r *FinanceRepository) GetLedgerFiltered(ctx context.Context, dateFrom, dateTo string) ([]domain.LedgerEntry, error) {
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
	query += " ORDER BY date DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
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

// GetStoreBalance calculates real-time store financial balance including referral bonuses and 2% referral commissions
func (r *FinanceRepository) GetStoreBalance(ctx context.Context, storeID string) (*StoreBalance, error) {
	var totalSales int64
	var pendingSettlements int64

	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(payable_amount), 0)
		FROM orders
		WHERE store_id = $1 AND status != 'cancelled'
	`, storeID).Scan(&totalSales); err != nil {
		return nil, err
	}

	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM payout_requests
		WHERE store_id = $1 AND status = 'pending'
	`, storeID).Scan(&pendingSettlements); err != nil {
		return nil, err
	}

	// 1. Calculate referral bonus totals (by status)
	var bonusLocked, bonusAvailable int64
	var lastReleasedAt *time.Time

	if err := r.db.QueryRowContext(ctx, `
		SELECT 
			COALESCE(SUM(CASE WHEN status = 'locked' THEN amount ELSE 0 END), 0) as locked,
			COALESCE(SUM(CASE WHEN status = 'released' THEN amount ELSE 0 END), 0) as released,
			MAX(released_at) as last_rel
		FROM referral_bonuses
		WHERE referrer_store_id = $1
	`, storeID).Scan(&bonusLocked, &bonusAvailable, &lastReleasedAt); err != nil {
		return nil, err
	}

	// 2. Calculate released 2% referral commission earned from subordinate orders
	var referralCommissionEarned int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(referral_commission), 0)
		FROM orders
		WHERE store_id IN (SELECT id FROM stores WHERE referrer_seller_id = $1)
		  AND referrer_seller_id = $1
		  AND referral_commission_released = TRUE
		  AND status != 'cancelled'
	`, storeID).Scan(&referralCommissionEarned); err != nil {
		return nil, err
	}

	var releasedProfit int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(dropshipper_profit), 0)
		FROM orders
		WHERE store_id = $1 AND profit_released = TRUE AND status IN ('delivered','delivered_confirmed')
	`, storeID).Scan(&releasedProfit); err != nil {
		return nil, err
	}

	// Only immutable released profit is withdrawable; never infer profit from revenue.
	salesProfit := releasedProfit
	totalBalance := salesProfit + bonusAvailable + bonusLocked + referralCommissionEarned

	// Available for withdrawal includes salesProfit + released bonus + earned referral commission minus pending payouts
	// (Locked bonus is EXCLUDED from available amount)
	var settledPayouts int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id=$1 AND status IN ('approved','paid','processing','completed','transfer_confirmed')),0) + COALESCE((SELECT SUM(amount) FROM seller_service_purchases WHERE store_id=$1),0)`, storeID).Scan(&settledPayouts); err != nil {
		return nil, err
	}
	// D31 live balance formula: source core MINUS the two
	// early-withdrawal deductions, guarded so a database without the
	// seller_early_withdrawals table (pre-202611020002) still works.
	// Approved withdrawals deduct their fee_amount; pending withdrawals
	// deduct their full gross_amount (the same profit must not be swept
	// again by a monthly settlement while a withdrawal is pending).
	var earlyApprovedFees, earlyPendingGross int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT CASE WHEN to_regclass('public.seller_early_withdrawals') IS NOT NULL
			THEN COALESCE((SELECT SUM(fee_amount) FROM seller_early_withdrawals WHERE store_id = $1 AND status = 'approved'), 0)
			ELSE 0 END
	`, storeID).Scan(&earlyApprovedFees); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT CASE WHEN to_regclass('public.seller_early_withdrawals') IS NOT NULL
			THEN COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id = $1 AND status = 'pending'), 0)
			ELSE 0 END
	`, storeID).Scan(&earlyPendingGross); err != nil {
		return nil, err
	}
	available := (salesProfit + bonusAvailable + referralCommissionEarned) - pendingSettlements - settledPayouts - earlyApprovedFees - earlyPendingGross
	if available < 0 {
		available = 0
	}

	return &StoreBalance{
		StoreID:                     storeID,
		Balance:                     totalBalance,
		UnsettledAmount:             pendingSettlements,
		AvailableAmount:             available,
		EarlyWithdrawalApprovedFees: earlyApprovedFees,
		EarlyWithdrawalPendingGross: earlyPendingGross,
		BonusLocked:                 bonusLocked,
		BonusAvailable:              bonusAvailable,
		BonusTotal:                  bonusLocked + bonusAvailable,
		ReferralCommissionEarned:    referralCommissionEarned,
		LastReleasedAt:              lastReleasedAt,
	}, nil
}

func (r *FinanceRepository) GetStorePayouts(ctx context.Context, ownerUserID, storeID string) ([]SettlementRequest, error) {
	var owns bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stores WHERE id = $1 AND owner_user_id = $2)`, storeID, ownerUserID).Scan(&owns); err != nil {
		return nil, err
	}
	if !owns {
		return nil, ErrStoreForbidden
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status,
		       COALESCE(available_balance,0), COALESCE(rejection_reason,''), created_at, processed_at,
		       transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests WHERE store_id = $1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SettlementRequest{}
	for rows.Next() {
		var item SettlementRequest
		if err := rows.Scan(&item.ID, &item.StoreID, &item.RecipientType, &item.RecipientName, &item.Shaba, &item.BankName, &item.Amount, &item.Status, &item.AvailableBalance, &item.RejectionReason, &item.CreatedAt, &item.ProcessedAt, &item.TransferReference, &item.TransferConfirmedBy, &item.TransferConfirmedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *FinanceRepository) RequestOwnedStorePayout(ctx context.Context, ownerUserID, storeID, shaba, bankName string, amount int64) (*SettlementRequest, error) {
	if amount <= 0 {
		return nil, ErrStoreInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, storeID, ownerUserID).Scan(&lockedID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrStoreForbidden
		}
		return nil, err
	}
	var available int64
	if err := tx.QueryRowContext(ctx, sellerAvailableSQL, storeID).Scan(&available); err != nil {
		return nil, err
	}
	if amount > available {
		return nil, ErrStoreInput
	}
	id := "pay-req-" + uuid.NewString()
	var item SettlementRequest
	if err := tx.QueryRowContext(ctx, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at) VALUES($1,$2,'seller',$2,$3,$4,$5,'pending',$6,NOW()) RETURNING id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at`, id, storeID, shaba, bankName, amount, available).Scan(&item.ID, &item.StoreID, &item.RecipientType, &item.RecipientName, &item.Shaba, &item.BankName, &item.Amount, &item.Status, &item.AvailableBalance, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

// RequestStorePayout checks real available balance from GetStoreBalance before creating a payout request (fail-closed)
func (r *FinanceRepository) RequestStorePayout(ctx context.Context, storeID, shaba, bankName string, amount int64) (*SettlementRequest, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("invalid payout amount: %d", amount)
	}

	bal, err := r.GetStoreBalance(ctx, storeID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve store balance: %w", err)
	}
	if amount > bal.AvailableAmount {
		return nil, fmt.Errorf("requested amount (%d) exceeds available balance (%d)", amount, bal.AvailableAmount)
	}

	reqID := fmt.Sprintf("pay-req-%s", uuid.New().String())
	query := `
		INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, available_balance, created_at)
		VALUES ($1, $2, 'seller', $2, $3, $4, $5, 'pending', $6, NOW())
		RETURNING id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, available_balance, created_at
	`
	var s SettlementRequest
	err = r.db.QueryRowContext(ctx, query, reqID, storeID, shaba, bankName, amount, bal.AvailableAmount).Scan(
		&s.ID, &s.StoreID, &s.RecipientType, &s.RecipientName, &s.Shaba, &s.BankName, &s.Amount, &s.Status, &s.AvailableBalance, &s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert payout request: %w", err)
	}
	return &s, nil
}
func (r *FinanceRepository) GetStoreStatement(ctx context.Context, storeID, month string) (*StoreStatement, error) {
	// Fix69 A2: built from real rows only. The old version returned the
	// same hardcoded credit 3,500,000 (ORD-SET-101) and debit 1,000,000
	// (PAYOUT-501) for every store and month; no such rows exist.
	// Credits are dropshipper profit released in the month plus referral
	// bonuses released in the month. Debits are money-out payouts
	// (approved alone is NOT money out per D32, pending/rejected never
	// are) plus seller service purchases in the month. Ledger entries
	// are deliberately NOT added on top: payout debits already appear
	// in ledger_entries at transfer confirmation (ConfirmManualTransfer)
	// and profit-release credits already appear there too, so adding
	// both sources would double-count. An empty month returns an empty
	// entry list with zero totals, never fake rows.
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return nil, fmt.Errorf("invalid month: want YYYY-MM, got %q", month)
	}
	end := start.AddDate(0, 1, 0)
	entries := []StatementEntry{}

	// Credits: released dropshipper profit (same status set as
	// GetStoreBalance), dated by profit_released_at.
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, order_number, dropshipper_profit, profit_released_at
		FROM orders
		WHERE store_id = $1 AND profit_released = TRUE AND status IN ('delivered', 'delivered_confirmed')
		  AND profit_released_at >= $2 AND profit_released_at < $3 AND dropshipper_profit > 0
		ORDER BY profit_released_at DESC
	`, storeID, start, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e StatementEntry
		var orderNumber string
		var profit int64
		if err := rows.Scan(&e.ID, &orderNumber, &profit, &e.Date); err != nil {
			rows.Close()
			return nil, err
		}
		e.Type = "credit"
		e.Amount = profit
		e.Reason = fmt.Sprintf("سود آزادشده سفارش %s", orderNumber)
		e.Reference = orderNumber
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Credits: referral bonuses released in the month.
	brows, err := r.db.QueryContext(ctx, `
		SELECT id, amount, released_at
		FROM referral_bonuses
		WHERE referrer_store_id = $1 AND status = 'released'
		  AND released_at >= $2 AND released_at < $3 AND amount > 0
		ORDER BY released_at DESC
	`, storeID, start, end)
	if err != nil {
		return nil, err
	}
	for brows.Next() {
		var e StatementEntry
		if err := brows.Scan(&e.ID, &e.Amount, &e.Date); err != nil {
			brows.Close()
			return nil, err
		}
		e.Type = "credit"
		e.Reason = "پاداش معرف آزادشده"
		e.Reference = e.ID
		entries = append(entries, e)
	}
	if err := brows.Err(); err != nil {
		brows.Close()
		return nil, err
	}
	brows.Close()

	// Debits: money-out payouts, dated by processed_at when set
	// (transfer confirmation) and otherwise by created_at.
	prows, err := r.db.QueryContext(ctx, `
		SELECT id, amount, COALESCE(processed_at, created_at)
		FROM payout_requests
		WHERE store_id = $1 AND status IN ('paid', 'processing', 'completed', 'transfer_confirmed')
		  AND COALESCE(processed_at, created_at) >= $2 AND COALESCE(processed_at, created_at) < $3 AND amount > 0
		ORDER BY COALESCE(processed_at, created_at) DESC
	`, storeID, start, end)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var e StatementEntry
		if err := prows.Scan(&e.ID, &e.Amount, &e.Date); err != nil {
			prows.Close()
			return nil, err
		}
		e.Type = "debit"
		e.Reason = "تسویه و واریز به حساب شبا"
		e.Reference = e.ID
		entries = append(entries, e)
	}
	if err := prows.Err(); err != nil {
		prows.Close()
		return nil, err
	}
	prows.Close()

	// Debits: seller service purchases in the month.
	srows, err := r.db.QueryContext(ctx, `
		SELECT id, service_type, amount, created_at
		FROM seller_service_purchases
		WHERE store_id = $1 AND created_at >= $2 AND created_at < $3 AND amount > 0
		ORDER BY created_at DESC
	`, storeID, start, end)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var e StatementEntry
		var serviceType string
		if err := srows.Scan(&e.ID, &serviceType, &e.Amount, &e.Date); err != nil {
			srows.Close()
			return nil, err
		}
		e.Type = "debit"
		e.Reason = fmt.Sprintf("خرید سرویس %s", serviceType)
		e.Reference = serviceType
		entries = append(entries, e)
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return nil, err
	}
	srows.Close()

	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Date.After(entries[j].Date) })

	var totalCredit int64
	var totalDebit int64
	for _, e := range entries {
		if e.Type == "credit" {
			totalCredit += e.Amount
		} else {
			totalDebit += e.Amount
		}
	}

	return &StoreStatement{
		StoreID:     storeID,
		Month:       start.Format("2006-01"),
		Entries:     entries,
		TotalCredit: totalCredit,
		TotalDebit:  totalDebit,
		NetBalance:  totalCredit - totalDebit,
	}, nil
}

// GetPendingSettlements returns payout requests awaiting finance approval
func (r *FinanceRepository) GetPendingSettlements(ctx context.Context) ([]SettlementRequest, error) {
	query := `
		SELECT id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name,
		       amount, status, rejection_reason, created_at, processed_at,
		       transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests
		WHERE status = 'pending'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	var amount int64
	var recName string
	var recType string

	err = tx.QueryRowContext(ctx, `
		SELECT amount, recipient_name, recipient_type
		FROM payout_requests
		WHERE id = $1 AND status = 'pending'
		FOR UPDATE
	`, id).Scan(&amount, &recName, &recType)
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

	return tx.Commit()
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

	now := time.Now()
	var amount int64
	var recName string
	var recType string

	err = tx.QueryRowContext(ctx, `
		SELECT amount, recipient_name, recipient_type
		FROM payout_requests
		WHERE id = $1 AND status = 'approved'
		FOR UPDATE
	`, id).Scan(&amount, &recName, &recType)
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

	if amount > 0 {
		ledgerID := "PAY-LED-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
			VALUES ($1, $2, $3, $4, 'debit', $5, $6, $7, $8)
		`, ledgerID, now, recName, recType, amount, "تسویه پایا بانکی تایید شده", operator, id); err != nil {
			return err
		}
	}

	return tx.Commit()
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
