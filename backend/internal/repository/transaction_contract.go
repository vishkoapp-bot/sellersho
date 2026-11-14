package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// ErrTransactionLockOrder means the discovery/preheld-lock contract was violated.
// It is a programming error, not a retryable database conflict.
var ErrTransactionLockOrder = errors.New("transaction lock contract violated")

// TransactionRowLock identifies one existing row. Table is an allowlisted,
// unqualified relation name; IDs are bound SQL parameters, never identifiers.
type TransactionRowLock struct {
	Table string
	ID    string
}

// Every product precedes every SKU (including products with multiple SKUs).
// Account guards are typed: seller/store, supplier, then wallet/customer. Within the last
// two classes table order is explicit, then IDs sort bytewise, not by discovery
// order, FIFO timestamps, SQL planner order, or localized display names.
var transactionLockTables = map[string]int{
	"orders":               100,
	"payment_transactions": 200,
	"stores":               300, "suppliers": 301, "users": 302,
	"products":           400,
	"product_variants":   500,
	"supplier_shipments": 600, "inbound_shipments": 601, "shipments": 602,
	"shipment_items": 603, "customer_returns": 604, "return_items": 605,
	"supplier_return_requests": 606, "supplier_return_parcels": 607,
	"putaway_tasks": 608, "order_inventory_captures": 609,
	"disputes": 610, "payout_requests": 611, "seller_early_withdrawals": 612,
	"monthly_settlement_cycles": 613, "monthly_settlement_items": 614,
	"pick_tasks": 615, "supplier_return_back_requests": 616,
	"inventory_balances": 700, "inventory_reservations": 701,
	"warehouse_bins": 702, "putaway_capacity_reservations": 703,
	// Rank 704 is reserved for INV-06 placement rows once their schema exists.
	"warehouse_storage_positions": 705,
}

func transactionLockColumn(table string) string {
	switch table {
	case "warehouse_bins":
		return "bin_code"
	case "order_inventory_captures":
		return "order_item_id"
	case "monthly_settlement_cycles":
		return "period"
	default:
		return "id"
	}
}

func transactionLockLess(a, b TransactionRowLock) bool {
	if transactionLockTables[a.Table] != transactionLockTables[b.Table] {
		return transactionLockTables[a.Table] < transactionLockTables[b.Table]
	}
	return a.ID < b.ID
}

func normalizeTransactionLocks(locks []TransactionRowLock) ([]TransactionRowLock, error) {
	result := append([]TransactionRowLock(nil), locks...)
	for _, lock := range result {
		if _, ok := transactionLockTables[lock.Table]; !ok || lock.ID == "" {
			return nil, fmt.Errorf("%w: unknown relation or empty identity", ErrTransactionLockOrder)
		}
	}
	sort.Slice(result, func(i, j int) bool { return transactionLockLess(result[i], result[j]) })
	unique := result[:0]
	for _, lock := range result {
		if len(unique) == 0 || unique[len(unique)-1] != lock {
			unique = append(unique, lock)
		}
	}
	return unique, nil
}

// TransactionLockSet belongs to one caller-owned transaction and one goroutine.
// Its zero value is ready to use. It must observe every row lock in that
// transaction: locks obtained by other SQL/helpers are not magically detected.
// Discover and acquire the full plan first, then invoke preheld-lock helpers.
type TransactionLockSet struct {
	tx       *sql.Tx
	held     map[TransactionRowLock]bool
	last     TransactionRowLock
	poisoned bool
}

// LockTransactionRowsTx acquires a sorted plan without opening a transaction or
// using *sql.DB. The caller supplies a deadline and must roll back on ANY error.
// A failed acquisition poisons the set; it cannot be reused for partial progress.
// Missing rows fail closed with sql.ErrNoRows. Revalidate authorization, ownership,
// state and immutable event payload through the same tx after acquiring locks.
// The allowlist is a contract aid for adopted callers, not an interceptor for
// arbitrary SQL in legacy callers. See docs/architecture/transaction-contract.md.
func LockTransactionRowsTx(ctx context.Context, tx *sql.Tx, set *TransactionLockSet, locks ...TransactionRowLock) error {
	if ctx == nil || tx == nil || set == nil {
		return fmt.Errorf("%w: context, transaction and lock set are required", ErrTransactionLockOrder)
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return fmt.Errorf("%w: acquisition requires a context deadline", ErrTransactionLockOrder)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if set.poisoned || (set.tx != nil && set.tx != tx) {
		return fmt.Errorf("%w: failed or different transaction", ErrTransactionLockOrder)
	}
	plan, err := normalizeTransactionLocks(locks)
	if err != nil {
		return err
	}
	// Validate the entire extension before touching the database. Reusing an
	// already-held guard is allowed; discovering an earlier new guard is not.
	for _, lock := range plan {
		if !set.held[lock] && set.last.Table != "" && transactionLockLess(lock, set.last) {
			return fmt.Errorf("%w: earlier guard discovered after later guard", ErrTransactionLockOrder)
		}
	}
	if set.held == nil {
		set.held = make(map[TransactionRowLock]bool)
	}
	set.tx = tx
	for _, lock := range plan {
		if set.held[lock] {
			continue
		}
		var id string
		// Table comes exclusively from transactionLockTables, not user input.
		column := transactionLockColumn(lock.Table)
		if err := tx.QueryRowContext(ctx, `SELECT `+column+` FROM `+lock.Table+` WHERE `+column+`=$1 FOR UPDATE`, lock.ID).Scan(&id); err != nil {
			set.poisoned = true
			return fmt.Errorf("acquire transaction row guard: %w", err)
		}
		set.held[lock] = true
		set.last = lock
	}
	return nil
}
