// notifications.go — Phase-2 best-effort outbox writers.
//
// Contract (launch-readiness/notifications INVENTORY.md §4): every writer
// here runs AFTER the business transaction commits, in its own separate
// transaction, and a notify failure is logged and never fails the
// business operation — exactly like notifyProposalRejected in
// supplier_repo.go. Recipient resolution is verified in code, never
// guessed:
//   - customer = orders.customer_user_id
//   - supplier owner = suppliers.owner_user_id
//   - store owner (seller) = stores.owner_user_id
//   - admin = users with role 'admin' and is_active, plus users holding
//     an active 'super_admin' capability (user_capabilities; super_admin
//     is a capability, not a users.role value — see the role CHECK in
//     migration 000001 and capability_repo.go GrantSuperAdminAfterOTP)
//   - warehouse staff = users with role 'warehouse_staff' and is_active
//     (role CHECK in migration 000001/000008; seeded staff rows exist)
//
// No email/SMS/push is built here; rows are 'pending' in
// notification_outbox, the readable state (P1-16: no sent-worker exists).
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// LowStockThreshold is the conservative default for P1-10: no threshold
// exists anywhere in the product (no setting, no per-product value), so
// writers notify when a paid order leaves a product at or below 5 units.
// A product decision (per-product thresholds, a platform setting) can
// replace this constant; it is deliberately NOT a new settings column.
const LowStockThreshold = 5

// formatToman renders an integer Toman amount with Latin digit grouping
// (e.g. 1250000 -> "1,250,000 تومان"). Latin digits are used so the
// amount string is searchable/matchable in tests and logs; the Persian
// body text around it stays Persian.
func formatToman(amount int64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	s := strconv.FormatInt(amount, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String() + " تومان"
	}
	return b.String() + " تومان"
}

// notifyBestEffort inserts one outbox row in its own transaction. Every
// failure path logs a structured line (tag identifies the event) and
// returns without error. A blank recipient is a skip, never a guess:
// callers must resolve the recipient first.
func notifyBestEffort(ctx context.Context, db *sql.DB, recipient, kind, title, body string, payload map[string]interface{}, tag string) {
	if db == nil {
		log.Printf("notify_skipped tag=%s kind=%s reason=db_nil", tag, kind)
		return
	}
	if strings.TrimSpace(recipient) == "" {
		log.Printf("notify_skipped tag=%s kind=%s reason=recipient_unresolved", tag, kind)
		return
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("notify_failed tag=%s kind=%s recipient_user_id=%s reason=tx_begin_error error=%v", tag, kind, recipient, err)
		return
	}
	defer tx.Rollback()
	if err := NotifyUser(ctx, tx, recipient, kind, truncateRunes(title, 200), body, payload); err != nil {
		log.Printf("notify_failed tag=%s kind=%s recipient_user_id=%s reason=notify_insert_error error=%v", tag, kind, recipient, err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("notify_failed tag=%s kind=%s recipient_user_id=%s reason=tx_commit_error error=%v", tag, kind, recipient, err)
		return
	}
}

// notifyManyBestEffort notifies each distinct non-blank recipient once.
func notifyManyBestEffort(ctx context.Context, db *sql.DB, recipients []string, kind, title, body string, payload map[string]interface{}, tag string) {
	seen := map[string]bool{}
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		notifyBestEffort(ctx, db, r, kind, title, body, payload, tag)
	}
}

// NotifyBestEffort is the exported form of notifyBestEffort for handlers
// in cmd/server (finance adjustments, ticket replies) that hold a
// *sql.DB directly instead of a repository.
func NotifyBestEffort(ctx context.Context, db *sql.DB, recipient, kind, title, body string, payload map[string]interface{}, tag string) {
	notifyBestEffort(ctx, db, recipient, kind, title, body, payload, tag)
}

// AdminUserIDs returns active admin recipients, verified in code:
// role 'admin', plus any user holding an active 'super_admin'
// capability. The user_capabilities table may predate the status
// column on very old trees, so the capability leg is guarded by
// to_regclass + information_schema and skipped when absent.
func AdminUserIDs(ctx context.Context, db *sql.DB) []string {
	if db == nil {
		return nil
	}
	var ids []string
	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE is_active = TRUE AND role = 'admin'`)
	if err != nil {
		log.Printf("notify_admin_resolve_failed reason=admin_query_error error=%v", err)
		return nil
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("notify_admin_resolve_failed reason=admin_rows_error error=%v", err)
	}
	// super_admin capability leg (guarded: table/column may not exist).
	var hasCap bool
	_ = db.QueryRowContext(ctx, `SELECT to_regclass('public.user_capabilities') IS NOT NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='user_capabilities' AND column_name='status')`).Scan(&hasCap)
	if hasCap {
		crows, err := db.QueryContext(ctx, `SELECT DISTINCT user_id FROM user_capabilities WHERE capability = 'super_admin' AND status = 'active' AND user_id IN (SELECT id FROM users WHERE is_active = TRUE)`)
		if err == nil {
			for crows.Next() {
				var id string
				if err := crows.Scan(&id); err == nil && id != "" {
					ids = append(ids, id)
				}
			}
			crows.Close()
		}
	}
	seen := map[string]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// WarehouseUserIDs returns active warehouse-staff recipients:
// users.role = 'warehouse_staff' OR 'logistics_specialist' (CHECK in
// migrations 000001/000008). Critic fix (Phase 2): logistics_specialist
// was missing — the logistics lead works the same warehouse queues
// (main.go warehouseRoles admits both roles to every /warehouse/*
// route incl. the warehouse inbox, and the dev seeder creates the
// logistics lead as a users row), so inbound/return rows addressed
// only to warehouse_staff never reached them.
func WarehouseUserIDs(ctx context.Context, db *sql.DB) []string {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE is_active = TRUE AND role IN ('warehouse_staff', 'logistics_specialist')`)
	if err != nil {
		log.Printf("notify_warehouse_resolve_failed reason=query_error error=%v", err)
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// FinanceQueueUserIDs returns the recipients who actually work the
// dispute + payout queues, verified in main.go (not assumed):
//   - admins (AdminUserIDs, incl. super_admin capability holders);
//   - role 'finance' / 'finance_specialist': /api/v1/finance/*
//     (financeRoles) and /api/v1/disputes/* resolve routes admit them;
//   - role 'support_specialist': /api/v1/disputes/* read + resolve
//     routes admit them.
//
// Critic fix (Phase 2): dispute-filed / payout-requested rows went to
// admins only, so the specialists who adjudicate disputes and review
// payouts never saw them. The /api/v1/admin/notifications route gate
// in main.go is widened to the same set, so these rows are readable
// by their recipients instead of orphaned. Logistics is deliberately
// NOT in this set: it has read-only dispute access and resolves
// nothing, so it gets no queue mail.
func FinanceQueueUserIDs(ctx context.Context, db *sql.DB) []string {
	ids := AdminUserIDs(ctx, db)
	if db == nil {
		return ids
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE is_active = TRUE AND role IN ('finance', 'finance_specialist', 'support_specialist')`)
	if err != nil {
		log.Printf("notify_finance_queue_resolve_failed reason=query_error error=%v", err)
		return ids
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	seen := map[string]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// supplierOwnerID resolves a supplier's owner user id ("" when the
// supplier is unknown or ownerless — the central record has no owner).
func supplierOwnerID(ctx context.Context, db *sql.DB, supplierID string) string {
	var owner sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT owner_user_id FROM suppliers WHERE id = $1`, supplierID).Scan(&owner); err != nil {
		return ""
	}
	return owner.String
}

// storeOwnerID resolves a store's owner user id (stores.owner_user_id).
func storeOwnerID(ctx context.Context, db *sql.DB, storeID string) string {
	var owner sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT owner_user_id FROM stores WHERE id = $1`, storeID).Scan(&owner); err != nil {
		return ""
	}
	return owner.String
}

// resolveRequesterUserID resolves a pending_approvals.requested_by value
// to a user id. The column holds either a users.id (domain/store-change
// requests) or a suppliers.id (product/warranty proposals); both shapes
// are verified in code. Anything else resolves to "" (skip, log).
func resolveRequesterUserID(ctx context.Context, db *sql.DB, requestedBy string) string {
	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		return ""
	}
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id::text = $1)`, requestedBy).Scan(&exists); err == nil && exists {
		return requestedBy
	}
	return supplierOwnerID(ctx, db, requestedBy)
}

// orderNotifyCtx carries everything the order writers need, resolved
// after the business transaction commits (rows still exist).
type orderNotifyCtx struct {
	OrderID          string
	OrderNumber      string
	CustomerID       string
	StoreID          string
	StoreOwnerID     string
	SupplierOwnerIDs []string
	Status           string
	PayableAmount    int64
}

func loadOrderNotifyCtx(ctx context.Context, db *sql.DB, orderIDOrNumber string) (*orderNotifyCtx, error) {
	var o orderNotifyCtx
	var customer sql.NullString
	var store sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT id, order_number, customer_user_id, store_id, status, COALESCE(payable_amount, 0) FROM orders WHERE id = $1 OR order_number = $1`, orderIDOrNumber).Scan(&o.OrderID, &o.OrderNumber, &customer, &store, &o.Status, &o.PayableAmount); err != nil {
		return nil, err
	}
	o.CustomerID = customer.String
	o.StoreID = store.String
	if o.StoreID != "" {
		o.StoreOwnerID = storeOwnerID(ctx, db, o.StoreID)
	}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT s.owner_user_id FROM order_items oi JOIN products p ON p.id = oi.product_id JOIN suppliers s ON s.id = p.supplier_id WHERE oi.order_id = $1 AND s.owner_user_id IS NOT NULL`, o.OrderID)
	if err == nil {
		for rows.Next() {
			var owner sql.NullString
			if err := rows.Scan(&owner); err == nil && owner.Valid && owner.String != "" {
				o.SupplierOwnerIDs = append(o.SupplierOwnerIDs, owner.String)
			}
		}
		rows.Close()
	}
	return &o, nil
}

func (o *orderNotifyCtx) payload(event string) map[string]interface{} {
	return map[string]interface{}{
		"order_id":     o.OrderID,
		"order_number": o.OrderNumber,
		"event":        event,
		"store_id":     o.StoreID,
	}
}

// storeOwnerAlreadyNotified reports whether the store owner is already
// among the supplier recipients (one person owns both the store and
// the supplying record). Critic fix (Phase 2): the order writers used
// to notify the suppliers and then the store owner unconditionally,
// so a supplier who is also the store owner got two identical rows
// for one event. Callers skip the store-owner call when this is true
// — the supplier row already tells them, with the more actionable body.
func (o *orderNotifyCtx) storeOwnerAlreadyNotified() bool {
	if o.StoreOwnerID == "" {
		return false
	}
	for _, s := range o.SupplierOwnerIDs {
		if s == o.StoreOwnerID {
			return true
		}
	}
	return false
}

// notifyOrderPlaced (P0-14): order placed — fulfilling suppliers + store owner.
func notifyOrderPlaced(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=order_placed order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	title := fmt.Sprintf("سفارش جدید %s ثبت شد", o.OrderNumber)
	body := fmt.Sprintf("سفارش %s در فروشگاه شما ثبت شد. برای آماده‌سازی و ارسال، به بخش سفارش‌ها بروید.", o.OrderNumber)
	notifyManyBestEffort(ctx, db, o.SupplierOwnerIDs, NotifKindOrderPlaced, title, body, o.payload("order_placed"), "order_placed_supplier")
	if o.StoreOwnerID != "" && !o.storeOwnerAlreadyNotified() {
		notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindOrderPlaced, title, fmt.Sprintf("سفارش %s در فروشگاه شما ثبت شد.", o.OrderNumber), o.payload("order_placed"), "order_placed_store")
	}
}

// notifyPaymentConfirmed (P0-14): payment confirmed — fulfilment is
// unblocked. Suppliers + store owner.
func notifyPaymentConfirmed(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=payment_confirmed order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	title := fmt.Sprintf("پرداخت سفارش %s تایید شد", o.OrderNumber)
	body := fmt.Sprintf("پرداخت سفارش %s تایید شد و آماده‌سازی آن آزاد شد. مبلغ قابل پرداخت: %s.", o.OrderNumber, formatToman(o.PayableAmount))
	notifyManyBestEffort(ctx, db, o.SupplierOwnerIDs, NotifKindPaymentConfirmed, title, body, o.payload("payment_confirmed"), "payment_confirmed_supplier")
	if o.StoreOwnerID != "" && !o.storeOwnerAlreadyNotified() {
		notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindPaymentConfirmed, title, body, o.payload("payment_confirmed"), "payment_confirmed_store")
	}
}

// notifyPaymentFailed (P1-9): gateway verification failed — customer.
// The failure is recorded on payment_transactions (status='failed') by
// RecordPaymentFailure before this is called.
func notifyPaymentFailed(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=payment_failed order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	if o.CustomerID == "" {
		log.Printf("notify_skipped tag=payment_failed order_id=%s reason=customer_unresolved", orderID)
		return
	}
	notifyBestEffort(ctx, db, o.CustomerID, NotifKindPaymentFailed,
		fmt.Sprintf("پرداخت سفارش %s ناموفق بود", o.OrderNumber),
		fmt.Sprintf("پرداخت سفارش %s توسط درگاه تایید نشد و مبلغی از حساب شما کسر نشده است. می‌توانید دوباره برای پرداخت اقدام کنید.", o.OrderNumber),
		o.payload("payment_failed"), "payment_failed")
}

// notifyOrderCancelled (P0-5): cancel — fulfilling suppliers (stop
// preparing) + store owner. The customer is NOT notified here: a
// customer/admin cancel is already known to its initiator; the
// system expiry-cancel notifies the customer separately
// (notifyExpiredOrder).
func notifyOrderCancelled(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=order_cancelled order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	title := fmt.Sprintf("سفارش %s لغو شد", o.OrderNumber)
	notifyManyBestEffort(ctx, db, o.SupplierOwnerIDs, NotifKindOrderCancelled, title, fmt.Sprintf("سفارش %s لغو شد؛ آماده‌سازی آن را متوقف کنید.", o.OrderNumber), o.payload("order_cancelled"), "order_cancelled_supplier")
	if o.StoreOwnerID != "" && !o.storeOwnerAlreadyNotified() {
		notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindOrderCancelled, title, fmt.Sprintf("سفارش %s لغو شد.", o.OrderNumber), o.payload("order_cancelled"), "order_cancelled_store")
	}
}

// notifyExpiredOrder (P0-5): system expiry-cancel of an unpaid order — customer.
func notifyExpiredOrder(ctx context.Context, db *sql.DB, orderID, orderNumber, customerID string) {
	if customerID == "" {
		// Resolve from the (cancelled) order row as a fallback.
		if o, err := loadOrderNotifyCtx(ctx, db, orderID); err == nil {
			customerID = o.CustomerID
			orderNumber = o.OrderNumber
		}
	}
	if customerID == "" {
		log.Printf("notify_skipped tag=order_expired order_id=%s reason=customer_unresolved", orderID)
		return
	}
	notifyBestEffort(ctx, db, customerID, NotifKindOrderCancelled,
		fmt.Sprintf("سفارش %s به دلیل عدم پرداخت لغو شد", orderNumber),
		fmt.Sprintf("مهلت پرداخت سفارش %s به پایان رسید و سفارش به‌صورت خودکار لغو شد. برای خرید دوباره، سفارش جدید ثبت کنید.", orderNumber),
		map[string]interface{}{"order_id": orderID, "order_number": orderNumber, "event": "order_expired", "store_id": ""}, "order_expired")
}

// NotifyDeliveryConfirmed (P1-3): delivery confirmed (manual or 20-day
// auto-confirm) — customer + store owner + suppliers. Profit is NOT
// released by confirmation; the release notifies separately.
func NotifyDeliveryConfirmed(ctx context.Context, db *sql.DB, orderID string, auto bool) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=delivery_confirmed order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	event := "delivery_confirmed"
	how := "تایید شد"
	if auto {
		event = "delivery_auto_confirmed"
		how = "به‌صورت خودکار تایید شد"
	}
	title := fmt.Sprintf("تحویل سفارش %s %s", o.OrderNumber, how)
	if o.CustomerID != "" {
		notifyBestEffort(ctx, db, o.CustomerID, NotifKindDeliveryConfirmed, title, fmt.Sprintf("تحویل سفارش %s %s. اگر مشکلی در کالا هست، تا پایان مهلت مرجوعی از بخش مرجوعی اقدام کنید.", o.OrderNumber, how), o.payload(event), "delivery_confirmed_customer")
	}
	notifyManyBestEffort(ctx, db, o.SupplierOwnerIDs, NotifKindDeliveryConfirmed, title, fmt.Sprintf("تحویل سفارش %s %s. سود فروشنده پس از پایان مهلت مرجوعی آزاد می‌شود.", o.OrderNumber, how), o.payload(event), "delivery_confirmed_supplier")
	if o.StoreOwnerID != "" && !o.storeOwnerAlreadyNotified() {
		notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindDeliveryConfirmed, title, fmt.Sprintf("تحویل سفارش %s %s. سود شما پس از پایان مهلت مرجوعی آزاد می‌شود.", o.OrderNumber, how), o.payload(event), "delivery_confirmed_store")
	}
}

// notifyProfitReleased (P1-3): return window closed, profit released —
// store owner + suppliers.
func notifyProfitReleased(ctx context.Context, db *sql.DB, orderID string, profit int64) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=profit_released order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	title := fmt.Sprintf("سود سفارش %s آزاد شد", o.OrderNumber)
	body := fmt.Sprintf("مهلت مرجوعی سفارش %s پایان یافت و سود آن (%s) آزاد شد و به موجودی قابل برداشت شما اضافه شد.", o.OrderNumber, formatToman(profit))
	if o.StoreOwnerID != "" {
		notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindProfitReleased, title, body, o.payload("profit_released"), "profit_released_store")
	}
	// Dedupe (fix 8): the store row above already told the store owner
	// (with the amount); drop them from the supplier list so one person
	// never gets two profit rows for one release.
	suppliers := make([]string, 0, len(o.SupplierOwnerIDs))
	for _, s := range o.SupplierOwnerIDs {
		if s != o.StoreOwnerID {
			suppliers = append(suppliers, s)
		}
	}
	notifyManyBestEffort(ctx, db, suppliers, NotifKindProfitReleased, title, fmt.Sprintf("مهلت مرجوعی سفارش %s پایان یافت و سود فروشنده آن آزاد شد.", o.OrderNumber), o.payload("profit_released"), "profit_released_supplier")
}

// notifyCarrierFailed (P1-5): carrier reported failure (order flips to
// delivery_disputed) — customer.
func notifyCarrierFailed(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=carrier_failed order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	if o.CustomerID == "" {
		log.Printf("notify_skipped tag=carrier_failed order_id=%s reason=customer_unresolved", orderID)
		return
	}
	notifyBestEffort(ctx, db, o.CustomerID, NotifKindCarrierFailed,
		fmt.Sprintf("ارسال سفارش %s با مشکل مواجه شد", o.OrderNumber),
		fmt.Sprintf("شرکت حمل‌ونقل ارسال سفارش %s را ناموفق اعلام کرد؛ پرونده آن برای بررسی به پشتیبانی ارجاع شد. نتیجه بررسی به شما اطلاع داده می‌شود.", o.OrderNumber),
		o.payload("carrier_failed"), "carrier_failed")
}

// notifySupplierAccepted (P1-5): supplier accepted a direct order — customer.
func notifySupplierAccepted(ctx context.Context, db *sql.DB, orderID, supplierName string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=supplier_accepted order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	if o.CustomerID == "" {
		log.Printf("notify_skipped tag=supplier_accepted order_id=%s reason=customer_unresolved", orderID)
		return
	}
	notifyBestEffort(ctx, db, o.CustomerID, NotifKindSupplierAccepted,
		fmt.Sprintf("سفارش %s توسط فروشنده پذیرفته شد", o.OrderNumber),
		fmt.Sprintf("سفارش %s پذیرفته شد و در حال آماده‌سازی است.", o.OrderNumber),
		o.payload("supplier_accepted"), "supplier_accepted")
}

// notifyLowStock (P1-10): product at/below the conservative threshold
// after a paid order — supplier owner + store owner, per product.
// Critic fix (Phase 2): notify ONLY on the crossing — the order must
// have moved the product from above the threshold to at/below it.
// The pre-order stock is reconstructed as current stock + this
// order's quantity (notifyLowStock runs right after payment capture
// decrements stock). An order placed while stock is already ≤5
// notifies nobody: re-notifying on every paid order while stock sits
// low was pure spam.
// Variant stock IS included (cheap: one join on order_items.
// variant_id): a variant that crossed the same threshold gets its
// own row naming the variant. The variant leg is best-effort — if
// the product_variants table is absent (older test DDLs), it logs
// and skips while the product-level leg still runs.
func notifyLowStock(ctx context.Context, db *sql.DB, orderID string) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=low_stock order_id=%s reason=order_lookup_error error=%v", orderID, err)
		return
	}
	rows, err := db.QueryContext(ctx, `SELECT p.id, p.title, COALESCE(p.stock, 0), COALESCE(p.supplier_id, ''), COALESCE(SUM(oi.quantity), 0) FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = $1 GROUP BY p.id, p.title, p.stock, p.supplier_id`, o.OrderID)
	if err != nil {
		log.Printf("notify_skipped tag=low_stock order_id=%s reason=low_stock_query_error error=%v", orderID, err)
		return
	}
	for rows.Next() {
		var productID, title, supplierID string
		var stock, qty int
		if err := rows.Scan(&productID, &title, &stock, &supplierID, &qty); err != nil {
			continue
		}
		if stock > LowStockThreshold || stock+qty <= LowStockThreshold {
			continue // no crossing on this order: still healthy, or already low before it
		}
		payload := map[string]interface{}{"product_id": productID, "order_id": o.OrderID, "order_number": o.OrderNumber, "event": "low_stock", "store_id": o.StoreID, "stock": stock}
		t := fmt.Sprintf("موجودی «%s» رو به پایان است", truncateRunes(title, 60))
		b := fmt.Sprintf("موجودی کالای «%s» به %d عدد رسید. برای جلوگیری از اتمام موجودی، آن را شارژ کنید.", title, stock)
		supOwner := ""
		if supplierID != "" {
			supOwner = supplierOwnerID(ctx, db, supplierID)
			notifyBestEffort(ctx, db, supOwner, NotifKindLowStock, t, b, payload, "low_stock_supplier")
		}
		if o.StoreOwnerID != "" && o.StoreOwnerID != supOwner {
			notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindLowStock, t, b, payload, "low_stock_store")
		}
	}
	rows.Close()
	// Variant leg: same crossing rule per ordered variant.
	vrows, verr := db.QueryContext(ctx, `SELECT pv.id, COALESCE(pv.variant_name, ''), COALESCE(pv.stock, 0), COALESCE(SUM(oi.quantity), 0), p.title, COALESCE(p.supplier_id, '') FROM order_items oi JOIN product_variants pv ON pv.id = oi.variant_id JOIN products p ON p.id = oi.product_id WHERE oi.order_id = $1 AND oi.variant_id IS NOT NULL GROUP BY pv.id, pv.variant_name, pv.stock, p.title, p.supplier_id`, o.OrderID)
	if verr != nil {
		log.Printf("notify_skipped tag=low_stock_variant order_id=%s reason=variant_query_error error=%v", orderID, verr)
		return
	}
	defer vrows.Close()
	for vrows.Next() {
		var variantID, variantName, productTitle, supplierID string
		var stock, qty int
		if err := vrows.Scan(&variantID, &variantName, &stock, &qty, &productTitle, &supplierID); err != nil {
			continue
		}
		if stock > LowStockThreshold || stock+qty <= LowStockThreshold {
			continue
		}
		payload := map[string]interface{}{"product_id": "", "variant_id": variantID, "order_id": o.OrderID, "order_number": o.OrderNumber, "event": "low_stock", "store_id": o.StoreID, "stock": stock}
		label := variantName
		if label == "" {
			label = productTitle
		}
		t := fmt.Sprintf("موجودی واریانت «%s» رو به پایان است", truncateRunes(label, 60))
		b := fmt.Sprintf("موجودی واریانت «%s» از کالای «%s» به %d عدد رسید. برای جلوگیری از اتمام موجودی، آن را شارژ کنید.", label, productTitle, stock)
		supOwner := ""
		if supplierID != "" {
			supOwner = supplierOwnerID(ctx, db, supplierID)
			notifyBestEffort(ctx, db, supOwner, NotifKindLowStock, t, b, payload, "low_stock_variant_supplier")
		}
		if o.StoreOwnerID != "" && o.StoreOwnerID != supOwner {
			notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindLowStock, t, b, payload, "low_stock_variant_store")
		}
	}
}

// notifyDisputeFiled (P0-6): dispute filed — the dispute queue
// (admins + finance/support specialists who adjudicate; see
// FinanceQueueUserIDs).
func notifyDisputeFiled(ctx context.Context, db *sql.DB, disputeID, orderNumber, disputeType string, claimAmount int64) {
	admins := FinanceQueueUserIDs(ctx, db)
	if len(admins) == 0 {
		log.Printf("notify_skipped tag=dispute_filed dispute_id=%s reason=no_admin_recipients", disputeID)
		return
	}
	payload := map[string]interface{}{"dispute_id": disputeID, "order_number": orderNumber, "event": "dispute_filed", "claim_amount": claimAmount}
	notifyManyBestEffort(ctx, db, admins, NotifKindDisputeFiled,
		fmt.Sprintf("اختلاف جدید برای سفارش %s ثبت شد", orderNumber),
		fmt.Sprintf("مشتری برای سفارش %s اختلاف ثبت کرد (نوع: %s، مبلغ ادعا: %s). برای داوری به بخش اختلاف‌ها بروید.", orderNumber, disputeType, formatToman(claimAmount)),
		payload, "dispute_filed")
}

// notifyDisputeResolved (P0-7): dispute resolved — customer (verdict +
// reason, incl. claim-rejected) and the debited/liable party. Carrier /
// warehouse / platform liability has no user recipient and is skipped
// (logged); admins already know the verdict from the panel.
func notifyDisputeResolved(ctx context.Context, db *sql.DB, disputeID, orderID, action, party string, claimAmount int64) {
	o, err := loadOrderNotifyCtx(ctx, db, orderID)
	if err != nil {
		log.Printf("notify_skipped tag=dispute_resolved dispute_id=%s reason=order_lookup_error error=%v", disputeID, err)
		return
	}
	verdict := "به نفع شما حل شد"
	if action == "release_to_seller" {
		verdict = "ادعای شما رد شد"
	}
	payload := map[string]interface{}{"dispute_id": disputeID, "order_id": o.OrderID, "order_number": o.OrderNumber, "event": "dispute_resolved", "action": action, "party": party, "claim_amount": claimAmount, "store_id": o.StoreID}
	if o.CustomerID != "" {
		body := fmt.Sprintf("پرونده اختلاف سفارش %s %s. طرف مسئول: %s.", o.OrderNumber, verdict, party)
		if action == "release_to_seller" {
			body = fmt.Sprintf("پرونده اختلاف سفارش %s بررسی شد و ادعای شما رد شد؛ مبلغی بازپرداخت نمی‌شود. طرف مسئول: %s.", o.OrderNumber, party)
		} else if claimAmount > 0 {
			body = fmt.Sprintf("پرونده اختلاف سفارش %s %s و بازپرداخت مبلغ %s ثبت شد. طرف مسئول: %s.", o.OrderNumber, verdict, formatToman(claimAmount), party)
		}
		notifyBestEffort(ctx, db, o.CustomerID, NotifKindDisputeResolved, fmt.Sprintf("نتیجه اختلاف سفارش %s", o.OrderNumber), body, payload, "dispute_resolved_customer")
	}
	// Debited/liable party: supplier -> order suppliers; seller -> store owner.
	switch strings.TrimSpace(party) {
	case "supplier", "تأمین‌کننده", "تأمین‌کننده کالا":
		notifyManyBestEffort(ctx, db, o.SupplierOwnerIDs, NotifKindDisputeResolved,
			fmt.Sprintf("در پرونده اختلاف سفارش %s مسئول شناخته شدید", o.OrderNumber),
			fmt.Sprintf("در پرونده اختلاف سفارش %s مسئولیت با تأمین‌کننده اعلام شد (مبلغ ادعا: %s). جزئیات را در پنل تأمین‌کننده ببینید.", o.OrderNumber, formatToman(claimAmount)),
			payload, "dispute_resolved_supplier")
	case "seller", "دراپ‌شیپر", "فروشنده", "سلر":
		// No dedupe guard here (fix 8): no supplier row is sent in this
		// branch, so the store-owner row is the seller's only notice.
		if o.StoreOwnerID != "" {
			notifyBestEffort(ctx, db, o.StoreOwnerID, NotifKindDisputeResolved,
				fmt.Sprintf("در پرونده اختلاف سفارش %s مسئول شناخته شدید", o.OrderNumber),
				fmt.Sprintf("در پرونده اختلاف سفارش %s مسئولیت با فروشنده اعلام شد (مبلغ ادعا: %s).", o.OrderNumber, formatToman(claimAmount)),
				payload, "dispute_resolved_seller")
		}
	default:
		log.Printf("notify_skipped tag=dispute_resolved_liable dispute_id=%s party=%s reason=no_user_recipient_for_party", disputeID, party)
	}
}

// notifyRefundIssued (P0-8): refund state change / wallet credit-debit —
// customer, amount + destination. Wording is honest about what the code
// proves: a wallet credit says "به کیف پول شما بازگشت"; a dispute/order
// status flip with no wallet movement says only that the refund was
// "ثبت شد" and never promises a PSP reversal (U21 caveat).
func notifyRefundIssued(ctx context.Context, db *sql.DB, orderID, orderNumber, customerID string, amount int64, walletCredit bool, extra string) {
	if customerID == "" {
		if o, err := loadOrderNotifyCtx(ctx, db, orderID); err == nil {
			customerID = o.CustomerID
			orderNumber = o.OrderNumber
		}
	}
	if customerID == "" {
		log.Printf("notify_skipped tag=refund_issued order_id=%s reason=customer_unresolved", orderID)
		return
	}
	payload := map[string]interface{}{"order_id": orderID, "order_number": orderNumber, "event": "refund_issued", "amount": amount, "wallet_credit": walletCredit}
	var body string
	if walletCredit {
		body = fmt.Sprintf("مبلغ %s بابت سفارش %s به کیف پول شما بازگشت.%s", formatToman(amount), orderNumber, extra)
	} else {
		body = fmt.Sprintf("بازپرداخت مبلغ %s بابت سفارش %s ثبت شد؛ وضعیت پرداخت سفارش به «بازپرداخت‌شده» تغییر کرد.%s", formatToman(amount), orderNumber, extra)
	}
	notifyBestEffort(ctx, db, customerID, NotifKindRefundIssued,
		fmt.Sprintf("بازپرداخت سفارش %s", orderNumber), body, payload, "refund_issued")
}

// NotifyFinanceAdjustment (P0-9): finance adjustment — adjusted supplier
// owner or store owner (amount, reason, docId). Called from the
// adjustment handler after the ledger legs commit.
func NotifyFinanceAdjustment(ctx context.Context, db *sql.DB, targetKind, targetID, direction string, amount int64, reason, docID string) {
	var recipient string
	if targetKind == "supplier" {
		recipient = supplierOwnerID(ctx, db, targetID)
	} else {
		recipient = storeOwnerID(ctx, db, targetID)
	}
	if recipient == "" {
		log.Printf("notify_skipped tag=finance_adjustment doc_id=%s target_kind=%s target_id=%s reason=recipient_unresolved", docID, targetKind, targetID)
		return
	}
	dirFa := "بدهکار (کاهش مانده)"
	if direction == "credit" {
		dirFa = "بستانکار (افزایش مانده)"
	}
	payload := map[string]interface{}{"doc_id": docID, "target_kind": targetKind, "target_id": targetID, "event": "finance_adjustment", "direction": direction, "amount": amount, "reason": reason}
	notifyBestEffort(ctx, db, recipient, NotifKindFinanceAdjustment,
		fmt.Sprintf("سند تعدیل مالی %s", docID),
		fmt.Sprintf("سند تعدیل مالی %s برای شما صادر شد: %s به مبلغ %s. علت: %s", docID, dirFa, formatToman(amount), reason),
		payload, "finance_adjustment")
}

// notifyPayoutReviewed (P0-10): payout reviewed (approved/rejected +
// reason) — store/supplier owner. The two ids are mutually exclusive
// by construction (seller payouts set store_id with supplier_id NULL;
// supplier payouts set supplier_id with store_id NULL —
// supplier_repo.go RequestSettlement, finance_repo.go,
// settlement_repo.go), so the old store-over-supplier preference
// could never drop a recipient in practice; both are notified anyway
// so a future creator that sets both cannot silently lose one.
func notifyPayoutReviewed(ctx context.Context, db *sql.DB, payoutID, status, reason string, amount int64, storeID, supplierID string) {
	var recipients []string
	if storeID != "" {
		recipients = append(recipients, storeOwnerID(ctx, db, storeID))
	}
	if supplierID != "" {
		recipients = append(recipients, supplierOwnerID(ctx, db, supplierID))
	}
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=payout_reviewed payout_id=%s reason=recipient_unresolved", payoutID)
		return
	}
	verdict := "تایید شد"
	if status == "rejected" {
		verdict = "رد شد"
	}
	payload := map[string]interface{}{"payout_id": payoutID, "event": "payout_reviewed", "status": status, "amount": amount, "reason": reason, "store_id": storeID, "supplier_id": supplierID}
	body := fmt.Sprintf("درخواست تسویه شما به مبلغ %s %s.", formatToman(amount), verdict)
	if status == "rejected" && strings.TrimSpace(reason) != "" {
		body = fmt.Sprintf("درخواست تسویه شما به مبلغ %s رد شد. علت: %s", formatToman(amount), reason)
	} else if status == "approved" {
		body = fmt.Sprintf("درخواست تسویه شما به مبلغ %s تایید شد و برای واریز بانکی در نوبت قرار گرفت.", formatToman(amount))
	}
	notifyManyBestEffort(ctx, db, recipients, NotifKindPayoutReviewed, fmt.Sprintf("نتیجه بررسی تسویه (%s)", formatToman(amount)), body, payload, "payout_reviewed")
}

// notifyPayoutTransferConfirmed (P0-10): transfer confirmed — store/
// supplier owner (amount + reference). Same mutual-exclusivity note
// as notifyPayoutReviewed: both are notified if both ids are ever set.
func notifyPayoutTransferConfirmed(ctx context.Context, db *sql.DB, payoutID, transferReference string, amount int64, storeID, supplierID string) {
	var recipients []string
	if storeID != "" {
		recipients = append(recipients, storeOwnerID(ctx, db, storeID))
	}
	if supplierID != "" {
		recipients = append(recipients, supplierOwnerID(ctx, db, supplierID))
	}
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=payout_transfer_confirmed payout_id=%s reason=recipient_unresolved", payoutID)
		return
	}
	payload := map[string]interface{}{"payout_id": payoutID, "event": "payout_transfer_confirmed", "amount": amount, "transfer_reference": transferReference, "store_id": storeID, "supplier_id": supplierID}
	notifyManyBestEffort(ctx, db, recipients, NotifKindPayoutTransfer,
		fmt.Sprintf("واریز تسویه %s انجام شد", formatToman(amount)),
		fmt.Sprintf("مبلغ %s به حساب شبای شما واریز شد. شماره مرجع واریز: %s", formatToman(amount), transferReference),
		payload, "payout_transfer_confirmed")
}

// notifyPayoutRequested (P1-6): new payout / early-withdrawal request —
// the payout queue (admins + finance specialists who review payouts;
// see FinanceQueueUserIDs).
func notifyPayoutRequested(ctx context.Context, db *sql.DB, payoutID, kindLabel string, amount int64, storeID string) {
	admins := FinanceQueueUserIDs(ctx, db)
	if len(admins) == 0 {
		log.Printf("notify_skipped tag=payout_requested payout_id=%s reason=no_admin_recipients", payoutID)
		return
	}
	payload := map[string]interface{}{"payout_id": payoutID, "event": "payout_requested", "amount": amount, "store_id": storeID}
	notifyManyBestEffort(ctx, db, admins, NotifKindPayoutRequested,
		fmt.Sprintf("درخواست تسویه جدید (%s)", formatToman(amount)),
		fmt.Sprintf("درخواست %s به مبلغ %s ثبت شد و در انتظار بررسی مالی است.", kindLabel, formatToman(amount)),
		payload, "payout_requested")
}

// notifySettlementSkipped (P0-13): monthly settlement skipped for missing
// bank details — store owner.
func notifySettlementSkipped(ctx context.Context, db *sql.DB, storeID, period string, amount int64) {
	recipient := storeOwnerID(ctx, db, storeID)
	if recipient == "" {
		log.Printf("notify_skipped tag=settlement_skipped store_id=%s period=%s reason=recipient_unresolved", storeID, period)
		return
	}
	payload := map[string]interface{}{"store_id": storeID, "event": "settlement_skipped", "period": period, "amount": amount}
	notifyBestEffort(ctx, db, recipient, NotifKindSettlementSkipped,
		"تسویه ماهانه شما انجام نشد",
		fmt.Sprintf("تسویه دوره %s به مبلغ %s به دلیل نبود اطلاعات بانکی (شبا) انجام نشد و مبلغ در حساب شما باقی ماند. برای دریافت تسویه، اطلاعات بانکی فروشگاه را تکمیل کنید.", period, formatToman(amount)),
		payload, "settlement_skipped")
}

// notifySettlementCreated (P1-6): monthly cycle created a payout for the
// seller — store owner.
func notifySettlementCreated(ctx context.Context, db *sql.DB, storeID, period, payoutID string, amount int64) {
	recipient := storeOwnerID(ctx, db, storeID)
	if recipient == "" {
		log.Printf("notify_skipped tag=settlement_created store_id=%s period=%s reason=recipient_unresolved", storeID, period)
		return
	}
	payload := map[string]interface{}{"store_id": storeID, "event": "settlement_created", "period": period, "payout_id": payoutID, "amount": amount}
	notifyBestEffort(ctx, db, recipient, NotifKindSettlementCreated,
		fmt.Sprintf("تسویه ماهانه %s ثبت شد", period),
		fmt.Sprintf("تسویه ماهانه دوره %s به مبلغ %s برای شما ثبت شد و در انتظار بررسی مالی است.", period, formatToman(amount)),
		payload, "settlement_created")
}

// notifyProposalApproved (P1-1): symmetric with proposal_rejected —
// supplier owner. P1-17: proposal_rejected payload gains "reason".
func notifyProposalApproved(ctx context.Context, db *sql.DB, proposalID, productTitle, supplierID, productID, notes string) {
	recipient := supplierOwnerID(ctx, db, supplierID)
	if recipient == "" {
		log.Printf("notify_skipped tag=proposal_approved proposal_id=%s supplier_id=%s reason=recipient_unresolved", proposalID, supplierID)
		return
	}
	payload := map[string]interface{}{"proposal_id": proposalID, "event": "proposal_approved", "product_title": productTitle, "product_id": productID, "reason": notes}
	body := fmt.Sprintf("پیشنهاد کالای «%s» تایید شد و کالا در کاتالوگ منتشر شد.", productTitle)
	if strings.TrimSpace(notes) != "" {
		body = fmt.Sprintf("پیشنهاد کالای «%s» تایید شد و کالا در کاتالوگ منتشر شد. یادداشت مدیر: %s", productTitle, notes)
	}
	notifyBestEffort(ctx, db, recipient, NotifKindProposalApproved,
		fmt.Sprintf("پیشنهاد «%s» تایید شد", truncateRunes(productTitle, 60)), body, payload, "proposal_approved")
}

// notifyApprovalRejected (P1-2): generic (non-product) approval rejection
// — the requester (user id, or supplier owner when requested_by is a
// supplier id). Product proposals are excluded: they go through
// RejectProposal/notifyProposalRejected.
func notifyApprovalRejected(ctx context.Context, db *sql.DB, approvalID, entityType, title, requestedBy, reason string) {
	recipient := resolveRequesterUserID(ctx, db, requestedBy)
	if recipient == "" {
		log.Printf("notify_skipped tag=approval_rejected approval_id=%s requested_by=%s reason=recipient_unresolved", approvalID, requestedBy)
		return
	}
	payload := map[string]interface{}{"approval_id": approvalID, "event": "approval_rejected", "entity_type": entityType, "reason": reason}
	body := fmt.Sprintf("درخواست «%s» تایید نشد.", title)
	if strings.TrimSpace(reason) != "" {
		body = fmt.Sprintf("درخواست «%s» تایید نشد. علت: %s", title, reason)
	}
	notifyBestEffort(ctx, db, recipient, NotifKindApprovalRejected, fmt.Sprintf("درخواست «%s» رد شد", truncateRunes(title, 60)), body, payload, "approval_rejected")
}

// notifySupplierRegistered (P1-12): supplier registration — admins. The
// smallest honest fix: an outbox row for admins (the admin read path
// is W2's; the row is readable once it exists).
func notifySupplierRegistered(ctx context.Context, db *sql.DB, supplierID, companyName, ownerUserID string) {
	admins := AdminUserIDs(ctx, db)
	if len(admins) == 0 {
		log.Printf("notify_skipped tag=supplier_registered supplier_id=%s reason=no_admin_recipients", supplierID)
		return
	}
	payload := map[string]interface{}{"supplier_id": supplierID, "event": "supplier_registered", "company_name": companyName}
	notifyManyBestEffort(ctx, db, admins, NotifKindSupplierRegistered,
		fmt.Sprintf("تأمین‌کننده جدید ثبت شد: %s", truncateRunes(companyName, 60)),
		fmt.Sprintf("تأمین‌کننده «%s» ثبت‌نام کرد و حساب آن فعال شد.", companyName),
		payload, "supplier_registered")
}

// notifyLegacySupplierDecision (P1-7): legacy supplier approve/reject —
// supplier owner.
func notifyLegacySupplierDecision(ctx context.Context, db *sql.DB, supplierID, ownerUserID string, approved bool, reason string) {
	if ownerUserID == "" {
		ownerUserID = supplierOwnerID(ctx, db, supplierID)
	}
	if ownerUserID == "" {
		log.Printf("notify_skipped tag=legacy_supplier_decision supplier_id=%s reason=recipient_unresolved", supplierID)
		return
	}
	payload := map[string]interface{}{"supplier_id": supplierID, "event": "supplier_approved", "reason": reason}
	kind := NotifKindSupplierApproved
	title := "حساب تأمین‌کننده شما تایید شد"
	body := "حساب تأمین‌کننده شما تایید و فعال شد؛ می‌توانید وارد پنل تأمین‌کننده شوید."
	if !approved {
		kind = NotifKindSupplierRejected
		payload["event"] = "supplier_rejected"
		title = "حساب تأمین‌کننده شما رد شد"
		body = "حساب تأمین‌کننده شما تایید نشد."
		if strings.TrimSpace(reason) != "" {
			body = fmt.Sprintf("حساب تأمین‌کننده شما تایید نشد. علت: %s", reason)
		}
	}
	notifyBestEffort(ctx, db, ownerUserID, kind, title, body, payload, "legacy_supplier_decision")
}

// NotifyTicketReply (P1-11): support-ticket reply — customer direction
// (admin replied to the requester) or admin direction (requester
// replied). Internal notes notify nobody. The false SMS copy fix is
// W3's; this is only the outbox writer.
func NotifyTicketReply(ctx context.Context, db *sql.DB, ticketID, authorUserID string, isInternal bool, body string) {
	if isInternal {
		return
	}
	var requester sql.NullString
	var subject string
	if err := db.QueryRowContext(ctx, `SELECT requester_user_id, COALESCE(subject, '') FROM support_tickets WHERE id = $1`, ticketID).Scan(&requester, &subject); err != nil {
		log.Printf("notify_skipped tag=ticket_reply ticket_id=%s reason=ticket_lookup_error error=%v", ticketID, err)
		return
	}
	snippet := strings.TrimSpace(body)
	if len([]rune(snippet)) > 120 {
		snippet = string([]rune(snippet)[:120]) + "…"
	}
	payload := map[string]interface{}{"ticket_id": ticketID, "event": "ticket_reply", "subject": subject}
	if requester.Valid && requester.String != "" && requester.String == authorUserID {
		// Requester replied: tell the admins.
		admins := AdminUserIDs(ctx, db)
		if len(admins) == 0 {
			log.Printf("notify_skipped tag=ticket_reply ticket_id=%s reason=no_admin_recipients", ticketID)
			return
		}
		notifyManyBestEffort(ctx, db, admins, NotifKindTicketReply,
			fmt.Sprintf("پاسخ جدید در تیکت «%s»", truncateRunes(subject, 60)),
			fmt.Sprintf("مشتری در تیکت «%s» پاسخ جدید ثبت کرد: %s", subject, snippet),
			payload, "ticket_reply_admin")
		return
	}
	if !requester.Valid || requester.String == "" {
		log.Printf("notify_skipped tag=ticket_reply ticket_id=%s reason=requester_unresolved", ticketID)
		return
	}
	notifyBestEffort(ctx, db, requester.String, NotifKindTicketReply,
		fmt.Sprintf("پاسخ پشتیبانی در تیکت «%s»", truncateRunes(subject, 60)),
		fmt.Sprintf("پشتیبانی در تیکت «%s» پاسخ داد: %s", subject, snippet),
		payload, "ticket_reply_customer")
}

// notifyReturnRequested (P1-4): customer return filed — warehouse staff
// + admins (they work the QC queue).
func notifyReturnRequested(ctx context.Context, db *sql.DB, returnID, orderNumber, reason string) {
	payload := map[string]interface{}{"return_id": returnID, "order_number": orderNumber, "event": "return_requested", "reason": reason}
	recipients := append(WarehouseUserIDs(ctx, db), AdminUserIDs(ctx, db)...)
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=return_requested return_id=%s reason=no_recipients", returnID)
		return
	}
	notifyManyBestEffort(ctx, db, recipients, NotifKindReturnRequested,
		fmt.Sprintf("درخواست مرجوعی جدید برای سفارش %s", orderNumber),
		fmt.Sprintf("مشتری برای سفارش %s درخواست مرجوعی ثبت کرد. علت: %s", orderNumber, reason),
		payload, "return_requested")
}

// notifyReturnDecision (P0-11): return approved/rejected — customer,
// including the admin_note reason on rejection.
func notifyReturnDecision(ctx context.Context, db *sql.DB, returnID, orderNumber, customerID, status, adminNote string) {
	if customerID == "" {
		log.Printf("notify_skipped tag=return_decision return_id=%s reason=customer_unresolved", returnID)
		return
	}
	payload := map[string]interface{}{"return_id": returnID, "order_number": orderNumber, "event": "return_" + status, "reason": adminNote}
	if status == "approved" {
		notifyBestEffort(ctx, db, customerID, NotifKindReturnApproved,
			fmt.Sprintf("مرجوعی سفارش %s تایید شد", orderNumber),
			fmt.Sprintf("درخواست مرجوعی سفارش %s تایید شد؛ کالا را طبق دستورالعمل مرجوعی ارسال کنید.", orderNumber),
			payload, "return_approved")
		return
	}
	body := fmt.Sprintf("درخواست مرجوعی سفارش %s تایید نشد.", orderNumber)
	if strings.TrimSpace(adminNote) != "" {
		body = fmt.Sprintf("درخواست مرجوعی سفارش %s تایید نشد. علت: %s", orderNumber, adminNote)
	}
	notifyBestEffort(ctx, db, customerID, NotifKindReturnRejected,
		fmt.Sprintf("مرجوعی سفارش %s رد شد", orderNumber), body, payload, "return_rejected")
}

// notifyReturnShipped (P1-4): customer shipped the return — destination:
// supplier owner when the destination is a supplier warehouse, else
// warehouse staff (central).
func notifyReturnShipped(ctx context.Context, db *sql.DB, returnID, orderNumber, tracking string, destSupplierWarehouseID string) {
	payload := map[string]interface{}{"return_id": returnID, "order_number": orderNumber, "event": "return_shipped", "tracking_number": tracking}
	if destSupplierWarehouseID != "" {
		var supplierID string
		if err := db.QueryRowContext(ctx, `SELECT supplier_id FROM supplier_warehouses WHERE id = $1`, destSupplierWarehouseID).Scan(&supplierID); err == nil && supplierID != "" {
			recipient := supplierOwnerID(ctx, db, supplierID)
			if recipient != "" {
				notifyBestEffort(ctx, db, recipient, NotifKindReturnShipped,
					fmt.Sprintf("مرجوعی سفارش %s ارسال شد", orderNumber),
					fmt.Sprintf("مشتری مرجوعی سفارش %s را ارسال کرد. کد رهگیری: %s", orderNumber, tracking),
					payload, "return_shipped_supplier")
				return
			}
		}
		log.Printf("notify_skipped tag=return_shipped return_id=%s reason=destination_supplier_unresolved", returnID)
	}
	recipients := WarehouseUserIDs(ctx, db)
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=return_shipped return_id=%s reason=no_warehouse_recipients", returnID)
		return
	}
	notifyManyBestEffort(ctx, db, recipients, NotifKindReturnShipped,
		fmt.Sprintf("مرجوعی سفارش %s ارسال شد", orderNumber),
		fmt.Sprintf("مشتری مرجوعی سفارش %s را ارسال کرد. کد رهگیری: %s", orderNumber, tracking),
		payload, "return_shipped_warehouse")
}

// notifyReturnQCCharged (P0-12): return QC received — charged suppliers
// (amount + return code + per-item split, from the persisted charge rows).
func notifyReturnQCCharged(ctx context.Context, db *sql.DB, returnID, orderNumber, returnCode string) {
	rows, err := db.QueryContext(ctx, `SELECT supplier_id, COALESCE(SUM(amount), 0), COUNT(*) FROM customer_return_shipping_charges WHERE return_id = $1 AND bearer = 'supplier' GROUP BY supplier_id`, returnID)
	if err != nil {
		log.Printf("notify_skipped tag=return_qc_charged return_id=%s reason=charges_query_error error=%v", returnID, err)
		return
	}
	defer rows.Close()
	any := false
	for rows.Next() {
		var supplierID string
		var amount int64
		var items int
		if err := rows.Scan(&supplierID, &amount, &items); err != nil {
			continue
		}
		any = true
		recipient := supplierOwnerID(ctx, db, supplierID)
		if recipient == "" {
			log.Printf("notify_skipped tag=return_qc_charged return_id=%s supplier_id=%s reason=recipient_unresolved", returnID, supplierID)
			continue
		}
		payload := map[string]interface{}{"return_id": returnID, "order_number": orderNumber, "event": "return_qc_charged", "return_code": returnCode, "amount": amount, "items": items}
		notifyBestEffort(ctx, db, recipient, NotifKindReturnQCCharged,
			fmt.Sprintf("هزینه حمل مرجوعی %s از شما کسر شد", returnCode),
			fmt.Sprintf("در مرجوعی %s (سفارش %s) هزینه حمل %s بابت %d قلم کالا از حساب شما کسر شد.", returnCode, orderNumber, formatToman(amount), items),
			payload, "return_qc_charged")
	}
	if !any {
		log.Printf("notify_skipped tag=return_qc_charged return_id=%s reason=no_supplier_charges", returnID)
	}
}

// notifyReturnCODRefused (P0-12): COD parcel refused at receive — customer.
func notifyReturnCODRefused(ctx context.Context, db *sql.DB, returnID, orderNumber, customerID string) {
	if customerID == "" {
		log.Printf("notify_skipped tag=return_cod_refused return_id=%s reason=customer_unresolved", returnID)
		return
	}
	payload := map[string]interface{}{"return_id": returnID, "order_number": orderNumber, "event": "return_cod_refused"}
	notifyBestEffort(ctx, db, customerID, NotifKindReturnCODRefused,
		fmt.Sprintf("مرجوعی سفارش %s پس‌کرایه بود و تحویل گرفته نشد", orderNumber),
		fmt.Sprintf("مرسوله مرجوعی سفارش %s به‌صورت پس‌کرایه (پرداخت در مقصد) ارسال شده بود و انبار آن را تحویل نگرفت. مرجوعی را با پرداخت پیش‌کرایه دوباره ارسال کنید.", orderNumber),
		payload, "return_cod_refused")
}

// notifyInboundCreated / notifyInboundDispatched (P0-15): inbound
// shipment created / dispatched by a supplier — warehouse staff.
func notifyInbound(ctx context.Context, db *sql.DB, inboundID, supplierName string, dispatched bool) {
	recipients := WarehouseUserIDs(ctx, db)
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=inbound inbound_id=%s reason=no_warehouse_recipients", inboundID)
		return
	}
	kind := NotifKindInboundCreated
	event := "inbound_created"
	title := fmt.Sprintf("محموله ورودی جدید از %s", supplierName)
	body := fmt.Sprintf("تأمین‌کننده %s یک محموله ورودی ثبت کرد. برای تحویل و کنترل کیفیت به بخش ورودی‌ها بروید.", supplierName)
	if dispatched {
		kind = NotifKindInboundDispatched
		event = "inbound_dispatched"
		title = fmt.Sprintf("محموله ورودی %s در راه انبار است", supplierName)
		body = fmt.Sprintf("تأمین‌کننده %s محموله ورودی را ارسال کرد و در راه انبار است.", supplierName)
	}
	payload := map[string]interface{}{"inbound_id": inboundID, "event": event, "supplier_name": supplierName}
	notifyManyBestEffort(ctx, db, recipients, kind, title, body, payload, "inbound")
}

// notifySupplierReturnReview (P1-8): supplier return-review decision — supplier.
func notifySupplierReturnReview(ctx context.Context, db *sql.DB, returnID, supplierID, status, reason string) {
	recipient := supplierOwnerID(ctx, db, supplierID)
	if recipient == "" {
		log.Printf("notify_skipped tag=supplier_return_review return_id=%s supplier_id=%s reason=recipient_unresolved", returnID, supplierID)
		return
	}
	payload := map[string]interface{}{"return_id": returnID, "event": "supplier_return_reviewed", "status": status, "reason": reason}
	verdict := "تایید شد"
	if status == "rejected" {
		verdict = "رد شد"
	}
	body := fmt.Sprintf("درخواست مرجوعی کالای شما %s.", verdict)
	if strings.TrimSpace(reason) != "" {
		body = fmt.Sprintf("درخواست مرجوعی کالای شما %s. علت: %s", verdict, reason)
	}
	notifyBestEffort(ctx, db, recipient, NotifKindSupplierReturnReview,
		fmt.Sprintf("نتیجه بررسی مرجوعی: %s", verdict), body, payload, "supplier_return_review")
}

// notifySupplierParcelCharge (P1-8): supplier-paid parcel charge confirmed
// — supplier (amount + parcel).
func notifySupplierParcelCharge(ctx context.Context, db *sql.DB, returnID, parcelID, supplierID string, amount int64) {
	recipient := supplierOwnerID(ctx, db, supplierID)
	if recipient == "" {
		log.Printf("notify_skipped tag=supplier_parcel_charge return_id=%s supplier_id=%s reason=recipient_unresolved", returnID, supplierID)
		return
	}
	payload := map[string]interface{}{"return_id": returnID, "parcel_id": parcelID, "event": "supplier_parcel_charge", "amount": amount}
	notifyBestEffort(ctx, db, recipient, NotifKindSupplierParcelCharge,
		"هزینه حمل مرسوله مرجوعی تایید شد",
		fmt.Sprintf("هزینه حمل مرسوله مرجوعی به مبلغ %s تایید و از حساب شما کسر شد.", formatToman(amount)),
		payload, "supplier_parcel_charge")
}

// notifySupplierReturnTransit (P0-15): supplier return handed over and in
// transit to its destination — warehouse staff.
func notifySupplierReturnTransit(ctx context.Context, db *sql.DB, returnID, supplierID string) {
	recipients := WarehouseUserIDs(ctx, db)
	if len(recipients) == 0 {
		log.Printf("notify_skipped tag=supplier_return_transit return_id=%s reason=no_warehouse_recipients", returnID)
		return
	}
	payload := map[string]interface{}{"return_id": returnID, "event": "supplier_return_in_transit", "supplier_id": supplierID}
	notifyManyBestEffort(ctx, db, recipients, NotifKindSupplierReturnTransit,
		"مرجوعی تأمین‌کننده در راه است",
		"یک مرجوعی تأمین‌کننده تحویل پست شد و در راه مقصد است؛ برای تحویل آن آماده باشید.",
		payload, "supplier_return_transit")
}

// notifyStorageFeeCharged (P0-16): storage fee charged — supplier owner,
// per supplier per run (total + positions count). Only fires when the
// flag-gated assessment actually charges (RunStorageFeeAssessment).
func notifyStorageFeeCharged(ctx context.Context, db *sql.DB, supplierID string, total int64, positions int) {
	recipient := supplierOwnerID(ctx, db, supplierID)
	if recipient == "" {
		log.Printf("notify_skipped tag=storage_fee_charged supplier_id=%s reason=recipient_unresolved", supplierID)
		return
	}
	payload := map[string]interface{}{"supplier_id": supplierID, "event": "storage_fee_charged", "amount": total, "positions": positions}
	notifyBestEffort(ctx, db, recipient, NotifKindStorageFeeCharged,
		"هزینه انبارداری از شما کسر شد",
		fmt.Sprintf("هزینه انبارداری %d جایگاه کالای شما به مبلغ %s محاسبه و از موجودی شما کسر شد. جزئیات را در بخش مالی پنل تأمین‌کننده ببینید.", positions, formatToman(total)),
		payload, "storage_fee_charged")
}
