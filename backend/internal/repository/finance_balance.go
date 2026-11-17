package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/drup/platform/internal/pricing"
)

// FinancialAccount identifies an existing typed principal row.
type FinancialAccount struct{ Type, ID string }

var ErrFinanceCommandConflict = errors.New("financial request key has a different payload")
var ErrFinanceAccountChanged = errors.New("financial account discovery changed; retry the whole request")
var ErrFinancePrincipalUnresolved = errors.New("immutable financial principal requires reconciliation")

type financeCommand struct {
	account                     FinancialAccount
	operation, key, fingerprint string
}

func newFinanceCommand(account FinancialAccount, operation string, keys []string, payload interface{}) (financeCommand, error) {
	c := financeCommand{account: account, operation: operation}
	if len(keys) > 1 {
		return c, ErrStoreInput
	}
	if len(keys) == 1 {
		c.key = strings.TrimSpace(keys[0])
	}
	if len(c.key) > 128 {
		return c, ErrStoreInput
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return c, err
	}
	sum := sha256.Sum256(encoded)
	c.fingerprint = hex.EncodeToString(sum[:])
	return c, nil
}

// The caller holds the typed account guard and revalidated ownership. Request
// identity and immutable result commit with the reservation/entitlement. A lost
// response can therefore replay after balance/configuration changes safely.
func (c financeCommand) replayTx(ctx context.Context, tx *sql.Tx, result interface{}) (bool, error) {
	if c.key == "" {
		return false, nil
	}
	var fingerprint string
	var encoded []byte
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,result FROM finance_command_requests WHERE principal_type=$1 AND principal_id=$2 AND operation=$3 AND client_key=$4`, c.account.Type, c.account.ID, c.operation, c.key).Scan(&fingerprint, &encoded)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fingerprint != c.fingerprint {
		return false, ErrFinanceCommandConflict
	}
	if result != nil {
		if err := json.Unmarshal(encoded, result); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (c financeCommand) recordTx(ctx context.Context, tx *sql.Tx, result interface{}) error {
	if c.key == "" {
		return nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO finance_command_requests(principal_type,principal_id,operation,client_key,fingerprint,result) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, c.account.Type, c.account.ID, c.operation, c.key, c.fingerprint, string(encoded))
	return err
}

// LockOrderAccountsTx acquires order, payment and affected account guards before
// worker/direct HTTP money mutations. Never call after work or inventory locks.
func LockOrderAccountsTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	return lockOrderFinanceTx(ctx, tx, orderID)
}

// LockAccountsTx uses caller-owned locks and transaction. Order/payment guards
// must already be held when applicable; never acquire an earlier class afterwards.
func LockAccountsTx(ctx context.Context, tx *sql.Tx, set *TransactionLockSet, accounts ...FinancialAccount) error {
	locks := make([]TransactionRowLock, 0, len(accounts))
	for _, a := range accounts {
		table := ""
		switch a.Type {
		case "seller":
			table = "stores"
		case "supplier":
			table = "suppliers"
		case "customer", "wallet":
			table = "users"
		default:
			return fmt.Errorf("%w: unknown account type", ErrTransactionLockOrder)
		}
		locks = append(locks, TransactionRowLock{Table: table, ID: a.ID})
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return LockTransactionRowsTx(bounded, tx, set, locks...)
}

const sellerAvailableSQL = `SELECT (
 COALESCE((SELECT SUM(dropshipper_profit) FROM orders WHERE store_id=$1 AND profit_released=TRUE AND status IN ('delivered','delivered_confirmed')),0)
 + COALESCE((SELECT SUM(amount) FROM referral_bonuses WHERE referrer_store_id=$1 AND status='released'),0)
 + COALESCE((SELECT SUM(referral_commission) FROM orders WHERE referrer_seller_id=$1 AND referral_commission_released=TRUE AND status<>'cancelled'),0)
 - COALESCE((SELECT SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END) FROM customer_return_financial_events WHERE store_id=$1 AND component='seller_profit'),0)
 + COALESCE((SELECT SUM(CASE WHEN type='credit' THEN amount ELSE -amount END) FROM ledger_entries WHERE target_account='store_payable:' || $1 AND target_type='seller' AND reason LIKE 'سند تعدیل%'),0)
 - COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id=$1 AND status IN ('pending','approved','paid','processing','completed','transfer_confirmed')),0)
 - COALESCE((SELECT SUM(amount) FROM seller_service_purchases WHERE store_id=$1),0)
 - CASE WHEN to_regclass('seller_early_withdrawals') IS NOT NULL THEN COALESCE((SELECT SUM(fee_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='approved'),0) ELSE 0 END
 - CASE WHEN to_regclass('seller_early_withdrawals') IS NOT NULL THEN COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='pending'),0) ELSE 0 END)`

// BalanceTx reads signed availability through the caller transaction. All
// principals must be preheld with LockAccountsTx; this helper takes no new locks.
func BalanceTx(ctx context.Context, tx *sql.Tx, account FinancialAccount) (int64, error) {
	switch account.Type {
	case "seller":
		var available int64
		err := tx.QueryRowContext(ctx, sellerAvailableSQL, account.ID).Scan(&available)
		if err == nil {
			err = validateSignedFinanceMoney(available)
		}
		return available, err
	case "supplier":
		var balance SupplierBalance
		err := scanSupplierBalance(ctx, tx, account.ID, &balance)
		return balance.Available, err
	default:
		return 0, fmt.Errorf("%w: unsupported balance type", ErrTransactionLockOrder)
	}
}

// Display and debit gate use one canonical expression in one MVCC snapshot.
func scanStoreBalance(ctx context.Context, q queryRower, storeID string) (*StoreBalance, error) {
	b := &StoreBalance{StoreID: storeID}
	err := q.QueryRowContext(ctx, `SELECT
  (`+sellerAvailableSQL+`),
  COALESCE((SELECT SUM(amount) FROM payout_requests WHERE store_id=$1 AND status='pending'),0),
  COALESCE((SELECT SUM(amount) FROM referral_bonuses WHERE referrer_store_id=$1 AND status='locked'),0),
  COALESCE((SELECT SUM(amount) FROM referral_bonuses WHERE referrer_store_id=$1 AND status='released'),0),
  (SELECT MAX(released_at) FROM referral_bonuses WHERE referrer_store_id=$1),
  COALESCE((SELECT SUM(referral_commission) FROM orders WHERE referrer_seller_id=$1 AND referral_commission_released=TRUE AND status<>'cancelled'),0),
  COALESCE((SELECT SUM(fee_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='approved'),0),
  COALESCE((SELECT SUM(gross_amount) FROM seller_early_withdrawals WHERE store_id=$1 AND status='pending'),0),
  COALESCE((SELECT SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END) FROM customer_return_financial_events WHERE store_id=$1 AND component='seller_profit'),0),
  COALESCE((SELECT SUM(CASE WHEN type='credit' THEN amount ELSE -amount END) FROM ledger_entries WHERE target_account='store_payable:' || $1 AND target_type='seller' AND reason LIKE 'سند تعدیل%'),0),
  COALESCE((SELECT SUM(dropshipper_profit) FROM orders WHERE store_id=$1 AND profit_released=TRUE AND status IN ('delivered','delivered_confirmed')),0)
 `, storeID).Scan(&b.AvailableAmount, &b.UnsettledAmount, &b.BonusLocked, &b.BonusAvailable, &b.LastReleasedAt, &b.ReferralCommissionEarned, &b.EarlyWithdrawalApprovedFees, &b.EarlyWithdrawalPendingGross, &b.SellerReturnReversals, &b.Adjustments, &b.Balance)
	if err != nil {
		return nil, err
	}
	for _, amount := range []int64{b.AvailableAmount, b.UnsettledAmount, b.BonusLocked, b.BonusAvailable, b.ReferralCommissionEarned, b.EarlyWithdrawalApprovedFees, b.EarlyWithdrawalPendingGross, b.SellerReturnReversals, b.Adjustments} {
		if err := validateSignedFinanceMoney(amount); err != nil {
			return nil, err
		}
	}
	b.BonusTotal, err = signedBalanceSum([]int64{b.BonusLocked, b.BonusAvailable}, nil)
	if err != nil {
		return nil, err
	}
	b.Balance, err = signedBalanceSum([]int64{b.Balance, b.BonusTotal, b.ReferralCommissionEarned, b.Adjustments}, []int64{b.SellerReturnReversals})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Signed balances permit debt; checked nonnegative price arithmetic must not be
// reused to floor it. Arbitrary precision intermediates also avoid subtraction
// overflow when a signed adjustment itself is negative.
func signedBalanceSum(credits, debits []int64) (int64, error) {
	total := new(big.Int)
	for _, amount := range credits {
		total.Add(total, big.NewInt(amount))
	}
	for _, amount := range debits {
		total.Sub(total, big.NewInt(amount))
	}
	if !total.IsInt64() {
		return 0, pricing.ErrInvalidMoney
	}
	amount := total.Int64()
	if err := validateSignedFinanceMoney(amount); err != nil {
		return 0, err
	}
	return amount, nil
}

func validateSignedFinanceMoney(amount int64) error {
	if amount > pricing.MaxTransportMoney || amount < -pricing.MaxTransportMoney {
		return pricing.ErrInvalidMoney
	}
	return nil
}

// lockOrderFinanceTx discovers the complete financial prefix before later
// return/inventory rows. The order is held first, so its account identities are
// reread while stable; callers must never invoke this after a work/stock lock.
func lockOrderFinanceTx(ctx context.Context, tx *sql.Tx, orderID string, wallet ...bool) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var set TransactionLockSet
	if err := LockTransactionRowsTx(bounded, tx, &set, TransactionRowLock{Table: "orders", ID: orderID}); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM payment_transactions WHERE order_id=$1`, orderID)
	if err != nil {
		return err
	}
	var payments []TransactionRowLock
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		payments = append(payments, TransactionRowLock{Table: "payment_transactions", ID: id})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := LockTransactionRowsTx(bounded, tx, &set, payments...); err != nil {
		return err
	}
	accounts, err := discoverOrderFinanceAccountsTx(bounded, tx, orderID, len(wallet) > 0 && wallet[0])
	if err != nil {
		return err
	}
	if err := LockAccountsTx(bounded, tx, &set, accounts...); err != nil {
		return err
	}
	if err := lockOrderCouponTx(bounded, tx, &set, orderID); err != nil {
		return err
	}
	// Supplier parity still uses the existing product ownership term. Freeze the
	// parent identities before posting, then reject rediscovery rather than append
	// a newly discovered earlier account after a product/return guard.
	rows, err = tx.QueryContext(bounded, `SELECT DISTINCT product_id,variant_id::text FROM order_items WHERE order_id=$1`, orderID)
	if err != nil {
		return err
	}
	var parents []TransactionRowLock
	for rows.Next() {
		var product string
		var variant sql.NullString
		if err := rows.Scan(&product, &variant); err != nil {
			rows.Close()
			return err
		}
		parents = append(parents, TransactionRowLock{Table: "products", ID: product})
		if variant.Valid && variant.String != "" {
			parents = append(parents, TransactionRowLock{Table: "product_variants", ID: variant.String})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err := LockTransactionRowsTx(bounded, tx, &set, parents...); err != nil {
		return err
	}
	revalidated, err := discoverOrderFinanceAccountsTx(bounded, tx, orderID, len(wallet) > 0 && wallet[0])
	if err != nil {
		return err
	}
	expected := make(map[FinancialAccount]bool, len(accounts))
	for _, account := range accounts {
		expected[account] = true
	}
	if len(expected) != len(revalidated) {
		return ErrFinanceAccountChanged
	}
	for _, account := range revalidated {
		if !expected[account] {
			return ErrFinanceAccountChanged
		}
	}
	return nil
}

func discoverOrderFinanceAccountsTx(ctx context.Context, tx *sql.Tx, orderID string, wallet bool) ([]FinancialAccount, error) {
	rows, err := tx.QueryContext(ctx, `SELECT 'seller',store_id FROM orders WHERE id=$1 AND COALESCE(store_id,'')<>''
 UNION SELECT 'seller',referrer_seller_id FROM orders WHERE id=$1 AND COALESCE(referrer_seller_id,'')<>''
 UNION SELECT 'seller',b.referrer_store_id FROM referral_bonuses b JOIN orders o ON o.store_id=b.referred_store_id WHERE o.id=$1 AND COALESCE(b.referrer_store_id,'')<>''
 UNION SELECT 'supplier',p.supplier_id FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.order_id=$1 AND COALESCE(p.supplier_id,'')<>''
 UNION SELECT 'supplier',o.supplier_id FROM order_item_origins o JOIN order_items oi ON oi.id=o.order_item_id WHERE oi.order_id=$1 AND COALESCE(o.supplier_id,'')<>''`, orderID)
	if err != nil {
		return nil, err
	}
	var accounts []FinancialAccount
	for rows.Next() {
		var account FinancialAccount
		if err := rows.Scan(&account.Type, &account.ID); err != nil {
			rows.Close()
			return nil, err
		}
		accounts = append(accounts, account)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if wallet {
		var customer sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT customer_user_id::text FROM orders WHERE id=$1`, orderID).Scan(&customer); err != nil {
			return nil, err
		}
		if customer.Valid && customer.String != "" {
			accounts = append(accounts, FinancialAccount{Type: "customer", ID: customer.String})
		}
	}
	return accounts, nil
}

func lockPayoutAccountsTx(ctx context.Context, tx *sql.Tx, id string) error {
	var store, supplier sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT store_id,supplier_id FROM payout_requests WHERE id=$1`, id).Scan(&store, &supplier); err != nil {
		return err
	}
	var accounts []FinancialAccount
	if store.Valid && store.String != "" {
		accounts = append(accounts, FinancialAccount{Type: "seller", ID: store.String})
	}
	if supplier.Valid && supplier.String != "" {
		accounts = append(accounts, FinancialAccount{Type: "supplier", ID: supplier.String})
	}
	var set TransactionLockSet
	return LockAccountsTx(ctx, tx, &set, accounts...)
}

// immutableOrderItemSupplierTx consumes the persisted checkout/capture origin,
// never current catalog ownership. A partial/conflicting legacy source is a
// reconciliation gate, not permission to guess which supplier should be debited.
func immutableOrderItemSupplierTx(ctx context.Context, tx *sql.Tx, itemID string) (string, error) {
	var orderID string
	var quantity int
	if err := tx.QueryRowContext(ctx, `SELECT order_id,COALESCE(quantity,0) FROM order_items WHERE id=$1`, itemID).Scan(&orderID, &quantity); err != nil {
		return "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT COALESCE(order_id,''),COALESCE(supplier_id,''),COALESCE(quantity,0) FROM order_item_origins WHERE order_item_id=$1 ORDER BY id`, itemID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var supplier string
	total, count := 0, 0
	empty := false
	for rows.Next() {
		var originOrder, party string
		var part int
		if err := rows.Scan(&originOrder, &party, &part); err != nil {
			return "", err
		}
		if originOrder != orderID || part <= 0 || part > quantity-total {
			return "", ErrFinancePrincipalUnresolved
		}
		total += part
		count++
		if party == "" {
			empty = true
		} else if supplier != "" && supplier != party {
			return "", ErrFinancePrincipalUnresolved
		} else {
			supplier = party
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if quantity <= 0 || count == 0 || total != quantity || empty || supplier == "" {
		return "", ErrFinancePrincipalUnresolved
	}
	return supplier, nil
}

// SumSignedFinanceBalances preserves debt in cross-account read aggregates.
func SumSignedFinanceBalances(amounts ...int64) (int64, error) { return signedBalanceSum(amounts, nil) }
