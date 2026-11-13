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

// NotifKindProposalRejected is the notification kind for a super-admin
// rejection of a supplier's proposed product (RejectProposal). It is
// addressed to the proposing supplier's owner user.
const NotifKindProposalRejected = "proposal_rejected"

// Phase-2 notification kinds (launch-readiness/notifications INVENTORY.md
// §3). Every kind below is written by a best-effort writer in
// notifications.go (separate tx, structured log on skip/failure — a
// notify failure never rolls back the business transaction). Payloads
// stay minimal: ids + title/body + reason/amount where relevant.
// The legacy order_status kind above is reused for pack/ship rows so
// the existing customer reader keeps working.
const (
	NotifKindOrderPlaced           = "order_placed"
	NotifKindPaymentConfirmed      = "payment_confirmed"
	NotifKindPaymentFailed         = "payment_failed"
	NotifKindOrderCancelled        = "order_cancelled"
	NotifKindDisputeFiled          = "dispute_filed"
	NotifKindDisputeResolved       = "dispute_resolved"
	NotifKindRefundIssued          = "refund_issued"
	NotifKindFinanceAdjustment     = "finance_adjustment"
	NotifKindPayoutReviewed        = "payout_reviewed"
	NotifKindPayoutTransfer        = "payout_transfer_confirmed"
	NotifKindPayoutRequested       = "payout_requested"
	NotifKindReturnApproved        = "return_approved"
	NotifKindReturnRejected        = "return_rejected"
	NotifKindReturnQCCharged       = "return_qc_charged"
	NotifKindReturnCODRefused      = "return_cod_refused"
	NotifKindSettlementSkipped     = "settlement_skipped"
	NotifKindSettlementCreated     = "settlement_created"
	NotifKindProposalApproved      = "proposal_approved"
	NotifKindApprovalRejected      = "approval_rejected"
	NotifKindDeliveryConfirmed     = "delivery_confirmed"
	NotifKindProfitReleased        = "profit_released"
	NotifKindReturnRequested       = "return_requested"
	NotifKindReturnShipped         = "return_shipped"
	NotifKindSupplierAccepted      = "supplier_order_accepted"
	NotifKindCarrierFailed         = "carrier_failed"
	NotifKindSupplierApproved      = "supplier_approved"
	NotifKindSupplierRejected      = "supplier_rejected"
	NotifKindSupplierReturnReview  = "supplier_return_reviewed"
	NotifKindSupplierParcelCharge  = "supplier_parcel_charge"
	NotifKindLowStock              = "low_stock"
	NotifKindTicketReply           = "ticket_reply"
	NotifKindSupplierRegistered    = "supplier_registered"
	NotifKindStorageFeeCharged     = "storage_fee_charged"
	NotifKindInboundCreated        = "inbound_created"
	NotifKindInboundDispatched     = "inbound_dispatched"
	NotifKindSupplierReturnTransit = "supplier_return_in_transit"
)

// syncProductStockTx re-syncs the products.stock projection from the canonical
// inventory_balances ledger: SUM(quantity) over status='sellable' rows. It is
// a no-op for products with no balance rows, so legacy products keep their
// directly-entered products.stock (see migration 202610170001 and the
// syncProductStockTx comments at its call sites).
func syncProductStockTx(ctx context.Context, tx *sql.Tx, productID string) error {
	skuAccounting, err := hasSKUInventoryTx(ctx, tx)
	if err != nil {
		return err
	}
	if !skuAccounting {
		_, err := tx.ExecContext(ctx, `UPDATE products p SET stock=s.sellable,updated_at=NOW() FROM (SELECT COALESCE(SUM(quantity),0)::int sellable FROM inventory_balances WHERE product_id=$1 AND status='sellable') s WHERE p.id=$1 AND EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=$1)`, productID)
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE products p
		SET stock = s.sellable, updated_at = NOW()
		FROM (SELECT COALESCE(SUM(ib.quantity), 0)::int AS sellable
		      FROM inventory_balances ib
		      LEFT JOIN product_variants pv ON pv.id=ib.variant_id
		      WHERE ib.product_id = $1 AND ib.status = 'sellable'
		        AND ((ib.variant_id IS NULL AND NOT EXISTS(SELECT 1 FROM products WHERE id=$1 AND schema_version>=2)) OR pv.is_active)) s
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
// transaction. A failed insert fails the caller's transaction closed,
// by design.
//
// REAL OUTBOX CONTRACT (P1-16, 2026-11-13 — supersedes the migration
// header and any comment describing a delivery worker): there is NO
// background worker and NO pending -> sent transition anywhere in this
// tree. 'pending' IS the readable state: the read endpoints return
// pending rows, and marking read flips 'pending' (or 'sent', if a row
// ever carries it) -> 'read'. 'sent' exists only as a CHECK-allowed
// status value in the schema; nothing writes it and nothing should be
// built to write it. Do not build a worker that flips pending -> sent:
// there is no external delivery (no push/SMS/email) for it to mark.
// (The migration SQL file 202610180005 is a historical record and is
// intentionally left unedited; this comment is the live contract.)
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
