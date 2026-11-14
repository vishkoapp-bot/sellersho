package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

// Automatic monthly settlement + early withdrawal (Team Settlement,
// Phase 3, on top of feature/live-parity).
//
// Provenance: the core of this file is ported from the undeployed
// branch feature/monthly-settlement (commit 48809c0), which was the
// ONLY source for the automatic layer. The MANUAL monthly engine in
// live-parity (backend/cmd/server/finance_parity_handlers.go:
// handleT01MonthlyRun/List/Detail, handleT01EarlyWithdrawalsList and
// handleT01EarlyWithdrawalReview) is NOT duplicated and NOT deleted:
// this file reuses its tables, item statuses (pending_payout,
// skipped_no_bank_details), fee-free monthly items, and payout
// semantics, and only adds what live-parity lacks: the automatic
// sweep, the seller wallet breakdown, and the seller early-withdrawal
// request path.
//
// Adaptations to live-parity (deliberate, each documented at the
// function where it applies):
//   - The sweep reads balances through GetStoreBalance, exactly like
//     the manual run, instead of the branch's sellerAvailableSQL-only
//     read. GetStoreBalance already carries the live D31 deductions
//     (approved early-withdrawal fees and pending early gross, with a
//     to_regclass guard), plus bonus/referral components.
//   - The sweep store filter is the manual engine's
//     owner_type != 'central' (not the branch's
//     owner_type = 'dropshipper'), and the payout recipient_name
//     is the store name (manual engine), not the store id (branch).
//   - Cycle totals count skipped stores and their amounts, like the
//     manual engine does; the branch counted only swept stores.
//     Cycles produced by either trigger therefore share one
//     definition of totalStores/totalGross/totalNet.
//   - The branch's ReviewEarlyWithdrawal is NOT ported: live-parity
//     already reviews early withdrawals (approved payout, NO ledger
//     entry at approval, D32: the ledger debit is written by the
//     existing transfer-confirmation flow). Porting the branch
//     version would have reintroduced ledger-at-approval with the
//     non-unique PAY-LED-<millis%100000> id that D32 removed.
//   - The branch's /api/v1/admin/settlement/settings and
//     /api/v1/finance/monthly-settlements|early-withdrawals routes
//     are NOT ported: live-parity already serves the same data at
//     /api/v1/admin/settings/settlement and
//     /api/v1/finance/settlements/monthly* and
//     /api/v1/finance/early-withdrawals*.
//   - The branch's finance_repo/seller_services balance changes are
//     NOT ported: live-parity already subtracts pending early gross
//     and approved early fees in GetStoreBalance and
//     sellerAvailableSQL (D31).
//
// User decisions applied here (NOT live-verified facts — see
// MASTER-AUDIT U02/U03): the automatic sweep runs on the
// admin-configured settlement_day (default 10, changeable in the
// super-admin panel); early withdrawal requires at least
// 1,000,000 تومان, charges the admin-configured
// early_withdrawal_fee_pct, and finance must review it within 24
// hours (deadlineAt = created_at + 24h, pending rows only). Day and
// fee are NEVER hard-coded in business logic: both are read from the
// platform_settings singleton (id = 1) on every run/request, and
// NULL means unset — the sweep stays inert and early withdrawal is
// refused until an admin sets them. The candidate branch works with
// its own settings row; it does NOT assume the live database values.
//
// No real bank transfer exists in this codebase, and none is
// invented here: "automatic to Sheba" means the sweep automatically
// creates a payout_requests row in status 'pending' for
// finance/bank execution, exactly like the manual engine. Monthly
// payouts are fee-free; only early withdrawals charge a fee.

const (
	// EarlyWithdrawalMinimumToman is the user-approved minimum for an
	// early (off-cycle) withdrawal request. The monthly sweep has no
	// minimum: any positive balance is swept, so small balances are
	// never stuck forever. U03: this number comes from the branch /
	// user decision only; it is NOT verified against live behaviour.
	EarlyWithdrawalMinimumToman int64 = 1000000
	// EarlyWithdrawalSLAHours is the user-approved review SLA for an
	// early withdrawal: finance settles it within 24 hours.
	EarlyWithdrawalSLAHours = 24
)

var (
	ErrSettlementNotConfigured = errors.New("settlement settings are not configured")
	ErrEarlyWithdrawalTooSmall = errors.New("early withdrawal is below the minimum amount")
	ErrEarlyWithdrawalFeeUnset = errors.New("early withdrawal fee percent is not configured")
)

type SettlementSettings struct {
	SettlementDay         *int     `json:"settlementDay,omitempty"`
	EarlyWithdrawalFeePct *float64 `json:"earlyWithdrawalFeePct,omitempty"`
}

type WalletBreakdown struct {
	StoreID           string              `json:"storeId"`
	PendingProfit     int64               `json:"pendingProfit"`
	ReleasedProfit    int64               `json:"releasedProfit"`
	AvailableAmount   int64               `json:"availableAmount"`
	PendingEarlyGross int64               `json:"pendingEarlyGross"`
	Settings          *SettlementSettings `json:"settlementSettings,omitempty"`
}

type MonthlySettlementCycle struct {
	Period        string    `json:"period"`
	SettlementDay int       `json:"settlementDay"`
	Status        string    `json:"status"`
	TotalStores   int       `json:"totalStores"`
	TotalGross    int64     `json:"totalGross"`
	TotalNet      int64     `json:"totalNet"`
	SkippedStores int       `json:"skippedStores"`
	CreatedAt     time.Time `json:"createdAt"`
}

type MonthlySettlementItem struct {
	ID              string    `json:"id"`
	CyclePeriod     string    `json:"cyclePeriod"`
	StoreID         string    `json:"storeId"`
	GrossAmount     int64     `json:"grossAmount"`
	FeeAmount       int64     `json:"feeAmount"`
	NetAmount       int64     `json:"netAmount"`
	PayoutRequestID *string   `json:"payoutRequestId,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type EarlyWithdrawal struct {
	ID              string     `json:"id"`
	StoreID         string     `json:"storeId"`
	GrossAmount     int64      `json:"grossAmount"`
	FeePct          float64    `json:"feePct"`
	FeeAmount       int64      `json:"feeAmount"`
	NetAmount       int64      `json:"netAmount"`
	Shaba           string     `json:"shaba"`
	BankName        string     `json:"bankName"`
	Status          string     `json:"status"`
	RejectionReason *string    `json:"rejectionReason,omitempty"`
	PayoutRequestID *string    `json:"payoutRequestId,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	ProcessedAt     *time.Time `json:"processedAt,omitempty"`
	// DeadlineAt is derived, not stored: created_at + 24h, for the
	// pending (not yet reviewed) state only.
	DeadlineAt *time.Time `json:"deadlineAt,omitempty"`
}

func (e *EarlyWithdrawal) fillDeadline() {
	if e.Status == "pending" {
		d := e.CreatedAt.Add(EarlyWithdrawalSLAHours * time.Hour)
		e.DeadlineAt = &d
	}
}

func (r *FinanceRepository) GetSettlementSettings(ctx context.Context) (*SettlementSettings, error) {
	var s SettlementSettings
	var day sql.NullInt64
	var fee sql.NullFloat64
	if err := r.db.QueryRowContext(ctx, `SELECT settlement_day, early_withdrawal_fee_pct FROM platform_settings WHERE id = 1`).Scan(&day, &fee); err != nil {
		return nil, err
	}
	if day.Valid {
		v := int(day.Int64)
		s.SettlementDay = &v
	}
	if fee.Valid {
		v := fee.Float64
		s.EarlyWithdrawalFeePct = &v
	}
	return &s, nil
}

func (r *FinanceRepository) GetWalletBreakdown(ctx context.Context, storeID string) (*WalletBreakdown, error) {
	bal, err := r.GetStoreBalance(ctx, storeID)
	if err != nil {
		return nil, err
	}
	var pending, released int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(dropshipper_profit),0) FROM orders WHERE store_id=$1 AND profit_released=FALSE AND status != 'cancelled'`, storeID).Scan(&pending); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(dropshipper_profit),0) FROM orders WHERE store_id=$1 AND profit_released=TRUE AND status IN ('delivered','delivered_confirmed')`, storeID).Scan(&released); err != nil {
		return nil, err
	}
	// Guarded like GetStoreBalance (D31): a database without the
	// seller_early_withdrawals table (pre-202611020002) still works.
	var earlyPending int64
	if err := r.db.QueryRowContext(ctx, `SELECT CASE WHEN to_regclass('public.seller_early_withdrawals') IS NOT NULL THEN COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='pending'),0) ELSE 0 END`, storeID).Scan(&earlyPending); err != nil {
		return nil, err
	}
	settings, err := r.GetSettlementSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &WalletBreakdown{
		StoreID: storeID, PendingProfit: pending, ReleasedProfit: released,
		AvailableAmount: bal.AvailableAmount, PendingEarlyGross: earlyPending, Settings: settings,
	}, nil
}

func (r *FinanceRepository) RequestEarlyWithdrawal(ctx context.Context, ownerUserID, storeID string, gross int64, shaba, bankName string) (*EarlyWithdrawal, error) {
	if gross > pricing.MaxTransportMoney {
		return nil, pricing.ErrInvalidMoney
	}
	if gross < EarlyWithdrawalMinimumToman {
		return nil, ErrEarlyWithdrawalTooSmall
	}
	if shaba == "" || bankName == "" {
		return nil, fmt.Errorf("shaba and bank_name are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var lockedID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2 AND owner_type='dropshipper' FOR UPDATE`, storeID, ownerUserID).Scan(&lockedID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrStoreForbidden
		}
		return nil, err
	}
	// sellerAvailableSQL (live-parity, D31) already subtracts pending
	// early withdrawals, pending/approved payouts and service
	// purchases, so the same profit cannot be requested twice.
	var available int64
	if err := tx.QueryRowContext(ctx, sellerAvailableSQL, storeID).Scan(&available); err != nil {
		return nil, err
	}
	if gross > available {
		return nil, ErrStoreInsufficientBalance
	}
	var feePct sql.NullFloat64
	if err := tx.QueryRowContext(ctx, `SELECT early_withdrawal_fee_pct FROM platform_settings WHERE id=1`).Scan(&feePct); err != nil {
		return nil, err
	}
	if !feePct.Valid {
		return nil, ErrEarlyWithdrawalFeeUnset
	}
	feeAmount, err := pricing.TruncatedPercent(gross, feePct.Float64)
	if err != nil || feeAmount > gross || feePct.Float64 > 100 {
		return nil, pricing.ErrInvalidMoney
	}
	net := gross - feeAmount
	if net <= 0 {
		return nil, fmt.Errorf("net amount must be positive")
	}
	id := "early-" + uuid.NewString()
	var out EarlyWithdrawal
	if err := tx.QueryRowContext(ctx, `INSERT INTO seller_early_withdrawals(id,store_id,gross_amount,fee_pct,fee_amount,net_amount,shaba,bank_name,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',NOW()) RETURNING id,store_id,gross_amount,fee_pct,fee_amount,net_amount,shaba,bank_name,status,rejection_reason,payout_request_id,created_at,processed_at`, id, storeID, gross, feePct.Float64, feeAmount, net, shaba, bankName).Scan(&out.ID, &out.StoreID, &out.GrossAmount, &out.FeePct, &out.FeeAmount, &out.NetAmount, &out.Shaba, &out.BankName, &out.Status, &out.RejectionReason, &out.PayoutRequestID, &out.CreatedAt, &out.ProcessedAt); err != nil {
		return nil, err
	}
	out.fillDeadline()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// P1-6 (best-effort): early-withdrawal requested - admins.
	notifyPayoutRequested(ctx, r.db, out.ID, "برداشت زودهنگام", out.GrossAmount, storeID)
	return &out, nil
}

func (r *FinanceRepository) ListEarlyWithdrawals(ctx context.Context, storeID string) ([]EarlyWithdrawal, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,store_id,gross_amount,fee_pct,fee_amount,net_amount,shaba,bank_name,status,rejection_reason,payout_request_id,created_at,processed_at FROM seller_early_withdrawals WHERE store_id=$1 ORDER BY created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EarlyWithdrawal{}
	for rows.Next() {
		var e EarlyWithdrawal
		if err := rows.Scan(&e.ID, &e.StoreID, &e.GrossAmount, &e.FeePct, &e.FeeAmount, &e.NetAmount, &e.Shaba, &e.BankName, &e.Status, &e.RejectionReason, &e.PayoutRequestID, &e.CreatedAt, &e.ProcessedAt); err != nil {
			return nil, err
		}
		e.fillDeadline()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *FinanceRepository) ListMonthlyCycles(ctx context.Context) ([]MonthlySettlementCycle, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_at FROM monthly_settlement_cycles ORDER BY period DESC LIMIT 24`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonthlySettlementCycle{}
	for rows.Next() {
		var c MonthlySettlementCycle
		if err := rows.Scan(&c.Period, &c.SettlementDay, &c.Status, &c.TotalStores, &c.TotalGross, &c.TotalNet, &c.SkippedStores, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *FinanceRepository) ListMonthlyItems(ctx context.Context, period string) ([]MonthlySettlementItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status,created_at FROM monthly_settlement_items WHERE cycle_period=$1 ORDER BY created_at DESC`, period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MonthlySettlementItem{}
	for rows.Next() {
		var it MonthlySettlementItem
		if err := rows.Scan(&it.ID, &it.CyclePeriod, &it.StoreID, &it.GrossAmount, &it.FeeAmount, &it.NetAmount, &it.PayoutRequestID, &it.Status, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// daysInMonth returns the number of days in the given year/month.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// effectiveSettlementDay clamps an admin-configured day to the month
// length (e.g. day 31 in February runs on the last day of February),
// so a configured day can never silently skip a month.
func effectiveSettlementDay(day, year int, month time.Month) int {
	if dim := daysInMonth(year, month); day > dim {
		return dim
	}
	return day
}

// settlementWindowPeriod resolves the settlement period whose window
// contains today, or ("", 0, false) outside any window. The window is
// the settlement day plus the 3 following days (D1 catch-up, N=3):
// e.g. day 10 sweeps on days 10, 11, 12 and 13. Window days that spill
// into the next month still sweep the PREVIOUS month's period, never
// the new month's. Days are server-local (now's own location, as the
// previous day-equality check did). Windows of adjacent months cannot
// overlap: a previous month's window ends at most on day 3 of the
// current month, while a current month's window starts on the
// (clamped) configured day.
func settlementWindowPeriod(now time.Time, configuredDay int) (string, int, bool) {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	eff := effectiveSettlementDay(configuredDay, today.Year(), today.Month())
	start := time.Date(today.Year(), today.Month(), eff, 0, 0, 0, 0, loc)
	if !today.Before(start) && !today.After(start.AddDate(0, 0, 3)) {
		return today.Format("2006-01"), eff, true
	}
	prevRef := today.AddDate(0, -1, 0)
	effPrev := effectiveSettlementDay(configuredDay, prevRef.Year(), prevRef.Month())
	startPrev := time.Date(prevRef.Year(), prevRef.Month(), effPrev, 0, 0, 0, 0, loc)
	if !today.Before(startPrev) && !today.After(startPrev.AddDate(0, 0, 3)) {
		return startPrev.Format("2006-01"), effPrev, true
	}
	return "", 0, false
}

// RunMonthlySettlement performs the free automatic monthly sweep.
// It returns (nil, nil) when today is outside the settlement window
// (the settlement day plus the 3 following days, D1 catch-up), and
// (nil, ErrSettlementNotConfigured) when the admin has not set a day.
// It is idempotent: a cycle for the current period can only be created
// once (period is the primary key), and a second run returns the
// existing cycle without sweeping again.
//
// Reconciliation with the manual engine (handleT01MonthlyRun): the
// store filter (owner_type != 'central'), the store-name
// recipient, the fee-free pending payout, the
// skipped_no_bank_details item, and the cycle totals (which count
// skipped stores and their amounts) all match the manual run, so a
// cycle means the same thing whichever trigger created it. Balances
// are read through GetStoreBalance before the write transaction,
// exactly like the manual run (same read/write race window; a
// balance change between the read and the cycle insert is not
// re-checked, and a later cycle never re-sweeps the same period).
func (r *FinanceRepository) RunMonthlySettlement(ctx context.Context, now time.Time, createdBy string) (*MonthlySettlementCycle, error) {
	settings, err := r.GetSettlementSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.SettlementDay == nil {
		return nil, ErrSettlementNotConfigured
	}
	// D1 catch-up (user decision, N=3): the sweep runs on the
	// settlement day and during the 3 following days. Window days
	// that spill into the next month still sweep the PREVIOUS
	// month's period, never the new month's.
	period, effectiveDay, ok := settlementWindowPeriod(now, *settings.SettlementDay)
	if !ok {
		return nil, nil
	}
	type candidate struct {
		id, name  string
		available int64
	}
	storeRows, err := r.db.QueryContext(ctx, `SELECT id, name FROM stores WHERE owner_type != 'central' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var candidates []candidate
	for storeRows.Next() {
		var c candidate
		if err := storeRows.Scan(&c.id, &c.name); err != nil {
			storeRows.Close()
			return nil, err
		}
		bal, err := r.GetStoreBalance(ctx, c.id)
		if err != nil {
			storeRows.Close()
			return nil, err
		}
		c.available = bal.AvailableAmount
		candidates = append(candidates, c)
	}
	storeRows.Close()
	if err := storeRows.Err(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// FIX 14: serialize concurrent sweep runs for this period (the
	// cycle-PK already enforces one settlement per month; the lock
	// closes the check-then-insert race so a duplicate run waits
	// instead of failing on the PK). This changes no catch-up or
	// balance-read semantics -- both are flagged for Richard's
	// decision in the Fix 14 report, not invented here.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(91827364)`); err != nil {
		return nil, err
	}
	var existing MonthlySettlementCycle
	err = tx.QueryRowContext(ctx, `SELECT period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_at FROM monthly_settlement_cycles WHERE period=$1`, period).Scan(&existing.Period, &existing.SettlementDay, &existing.Status, &existing.TotalStores, &existing.TotalGross, &existing.TotalNet, &existing.SkippedStores, &existing.CreatedAt)
	if err == nil {
		// Already swept this month: do nothing, do not sweep twice.
		return &existing, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO monthly_settlement_cycles(period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_by) VALUES($1,$2,'completed',0,0,0,0,$3)`, period, effectiveDay, createdBy); err != nil {
		return nil, err
	}
	totalStores, skippedStores := 0, 0
	var totalGross, totalNet int64
	// P0-13 / P1-6 notify lists, resolved during the loop and sent
	// only after the cycle commits (best-effort, separate tx each).
	type settlementNotify struct {
		storeID  string
		amount   int64
		payoutID string
	}
	var skippedNotifies, createdNotifies []settlementNotify
	for _, c := range candidates {
		if c.available <= 0 {
			continue
		}
		var shaba, bankName string
		err := tx.QueryRowContext(ctx, `SELECT shaba, bank_name FROM payout_requests WHERE store_id = $1 ORDER BY created_at DESC LIMIT 1`, c.id).Scan(&shaba, &bankName)
		if err == sql.ErrNoRows || shaba == "" {
			// Never invent bank details: record the skip, keep balance.
			if _, err := tx.ExecContext(ctx, `INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status) VALUES($1,$2,$3,$4,0,$4,NULL,'skipped_no_bank_details')`, "msi-"+uuid.NewString(), period, c.id, c.available); err != nil {
				return nil, err
			}
			skippedStores++
			totalStores++
			totalGross += c.available
			totalNet += c.available
			skippedNotifies = append(skippedNotifies, settlementNotify{storeID: c.id, amount: c.available})
			continue
		}
		if err != nil {
			return nil, err
		}
		// Pending payout for the full (fee-free) amount. It stays
		// 'pending' for the existing finance review /
		// transfer-confirmation flow (D32); no ledger entry is written
		// here, because approval alone never writes the ledger, and
		// no bank transfer is performed by this code.
		payoutID := "pay-req-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,available_balance,created_at) VALUES($1,$2,'seller',$3,$4,$5,$6,'pending',$6,NOW())`, payoutID, c.id, c.name, shaba, bankName, c.available); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status) VALUES($1,$2,$3,$4,0,$4,$5,'pending_payout')`, "msi-"+uuid.NewString(), period, c.id, c.available, payoutID); err != nil {
			return nil, err
		}
		totalStores++
		totalGross += c.available
		totalNet += c.available
		createdNotifies = append(createdNotifies, settlementNotify{storeID: c.id, amount: c.available, payoutID: payoutID})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE monthly_settlement_cycles SET total_stores=$1, total_gross=$2, total_net=$3, skipped_stores=$4 WHERE period=$5`, totalStores, totalGross, totalNet, skippedStores, period); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// P0-13 (best-effort): skipped for missing bank details - store
	// owner, telling them to add bank details. P1-6: sellers whose
	// cycle created a payout for them.
	for _, n := range skippedNotifies {
		notifySettlementSkipped(ctx, r.db, n.storeID, period, n.amount)
	}
	for _, n := range createdNotifies {
		notifySettlementCreated(ctx, r.db, n.storeID, period, n.payoutID, n.amount)
	}
	return &MonthlySettlementCycle{Period: period, SettlementDay: effectiveDay, Status: "completed", TotalStores: totalStores, TotalGross: totalGross, TotalNet: totalNet, SkippedStores: skippedStores}, nil
}
