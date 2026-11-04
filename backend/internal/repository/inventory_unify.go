// inventory_unify.go — canonical-inventory helpers.
//
// This file recreates the helpers migration 202610170001 cites as
// "internal/repository/inventory_unify.go :: syncProductStockTx" and migration
// 202610180005 cites as "repository.NotifyUser". The file was referenced by
// supplier_repo.go and warehouse_repo.go but was never committed in any branch
// (checked: main and the three un-deployed feature branches), so the helpers
// below are reconstructions, not recovered live code:
//
//   - syncProductStockTx mirrors the migration's own trigger
//     sync_product_stock_projection() exactly: re-project products.stock from
//     SUM(sellable) only when the product has at least one balance row;
//     otherwise it is a no-op and legacy products.stock stays authoritative.
//   - canonicalCoveredTx is the predicate ensureCanonicalCoverageTx needs:
//     "covered" means the product already has at least one balance row.
//   - NotifyUser inserts one 'pending' row into notification_outbox, as
//     migration 202610180005 specifies; the kind constant's stored value is
//     unverified (no stored kind was observed) — only the two Pack/Ship call
//     sites use it, via this constant, so a correction is a one-line change.
//
// TODO (team-00, live-parity): verify the three semantics above against the
// live build's behaviour in a non-live environment before relying on them
// beyond making the tree compile/vet clean.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
)

// NotifKindOrderStatus is the notification kind for order-status updates
// (packing/shipping). Its stored string value is unverified; see file header.
const NotifKindOrderStatus = "order_status"

// syncProductStockTx re-syncs the products.stock projection from the canonical
// inventory_balances ledger: SUM(quantity) over status='sellable' rows. It is
// a no-op for products with no balance rows, so legacy products keep their
// directly-entered products.stock (see migration 202610170001 and the
// syncProductStockTx comments at its call sites).
func syncProductStockTx(ctx context.Context, tx *sql.Tx, productID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE products p
		SET stock = s.sellable, updated_at = NOW()
		FROM (SELECT COALESCE(SUM(quantity), 0)::int AS sellable
		      FROM inventory_balances
		      WHERE product_id = $1 AND status = 'sellable') s
		WHERE p.id = $1
		  AND EXISTS (SELECT 1 FROM inventory_balances WHERE product_id = $1)`,
		productID)
	return err
}

// canonicalCoveredTx reports whether a product has already entered the
// canonical inventory ledger (at least one inventory_balances row).
func canonicalCoveredTx(ctx context.Context, tx *sql.Tx, productID string) (bool, error) {
	var covered bool
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM inventory_balances WHERE product_id = $1)`,
		productID).Scan(&covered)
	return covered, err
}

// NotifyUser appends one pending notification to the transactional outbox
// (notification_outbox, migration 202610180005) inside the caller's
// transaction. Delivery (pending -> sent) is a background worker's job, not
// this function's. A failed insert fails the caller's transaction closed,
// by design.
// notifPayloadWithStore copies base and adds the order's store_id when
// the order carries one, so a notification row's payload attributes the
// event to its store (previously Pack/Ship payloads carried no store at
// all, making per-store attribution impossible). A NULL store_id is
// omitted rather than emitted as an empty/wrong store.
func notifPayloadWithStore(base map[string]interface{}, storeID sql.NullString) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	if storeID.Valid && storeID.String != "" {
		base["store_id"] = storeID.String
	}
	return base
}

func NotifyUser(ctx context.Context, tx *sql.Tx, recipientUserID, kind, title, body string, payload map[string]interface{}) error {
	payloadJSON := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadJSON = b
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO notification_outbox (recipient_user_id, kind, title, body, payload)
		 VALUES ($1, $2, $3, $4, $5::jsonb)`,
		recipientUserID, kind, title, body, string(payloadJSON))
	return err
}
