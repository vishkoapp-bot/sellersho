package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/shippingquote"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// OrderRepository handles persistence of orders and line items
type OrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// CanReadOperationalTracking scopes authenticated tracking to the store owner
// or an active supplier owner/member with order-management permission.
func (r *OrderRepository) CanReadOperationalTracking(ctx context.Context, orderID, userID, role string) (bool, error) {
	var allowed bool
	if role == "seller" {
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN stores s ON s.id=o.store_id WHERE o.id=$1 AND s.owner_user_id=$2::uuid)`, orderID, userID).Scan(&allowed)
		return allowed, err
	}
	if role == "supplier" {
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS(
		 SELECT 1 FROM supplier_shipments sh JOIN suppliers s ON s.id=sh.supplier_id
		 WHERE sh.order_id=$1 AND (s.owner_user_id=$2::uuid OR EXISTS(
		 SELECT 1 FROM supplier_members sm WHERE sm.supplier_id=s.id AND sm.user_id=$2::uuid
		 AND sm.is_active AND (sm.permissions ? 'orders.manage' OR sm.permissions ? 'supplier.manage'))))`, orderID, userID).Scan(&allowed)
		return allowed, err
	}
	return false, nil
}

const orderSelectColumns = `
	id, customer_user_id, order_number, store_id, store_name, total_amount, discount_amount,
	payable_amount, payment_method, payment_status, customer_full_name, customer_phone,
	customer_province, customer_city, customer_address, customer_postal_code,
	status, tracking_code, shipping_cost, dropshipper_profit, platform_profit,
	referral_commission, referral_commission_released, referrer_seller_id,
	profit_released, profit_released_at, shipped_at, confirmed_at, disputed_at,
	created_at, updated_at, COALESCE(fulfillment_method, 'warehouse'),
	wallet_amount, gateway_amount, platform_addon_amount, supplier_addon_amount,
	inventory_captured, packaging_length_mm, packaging_width_mm, packaging_height_mm,
	dispute_reason, carrier, tracking_number, shipping_label, packed_at, box_size,
	weight_grams, idempotency_key, return_window_ends_at,
	COALESCE(seller_discount_amount, 0), COALESCE(discount_code, ''), COALESCE(shipping_quote_id, '')`

func orderScanDest(o *domain.Order) []interface{} {
	return []interface{}{
		&o.ID, &o.CustomerUserID, &o.OrderNumber, &o.StoreID, &o.StoreName, &o.TotalAmount, &o.DiscountAmount,
		&o.PayableAmount, &o.PaymentMethod, &o.PaymentStatus, &o.CustomerFullName, &o.CustomerPhone,
		&o.CustomerProvince, &o.CustomerCity, &o.CustomerAddress, &o.CustomerPostalCode,
		&o.Status, &o.TrackingCode, &o.ShippingCost, &o.DropshipperProfit, &o.PlatformProfit,
		&o.ReferralCommission, &o.ReferralCommissionReleased, &o.ReferrerSellerID,
		&o.ProfitReleased, &o.ProfitReleasedAt, &o.ShippedAt, &o.ConfirmedAt, &o.DisputedAt,
		&o.CreatedAt, &o.UpdatedAt, &o.FulfillmentMethod,
		&o.WalletAmount, &o.GatewayAmount, &o.PlatformAddonAmount, &o.SupplierAddonAmount,
		&o.InventoryCaptured, &o.PackagingLengthMM, &o.PackagingWidthMM, &o.PackagingHeightMM,
		&o.DisputeReason, &o.Carrier, &o.TrackingNumber, &o.ShippingLabel, &o.PackedAt, &o.BoxSize,
		&o.WeightGrams, &o.IdempotencyKey, &o.ReturnWindowEndsAt, &o.SellerDiscountAmount, &o.DiscountCode, &o.ShippingQuoteID,
	}
}

// CreateOrder inserts an order and its line items inside an ACID transaction
func (r *OrderRepository) CreateOrder(ctx context.Context, order domain.Order) error {
	return r.CreateOrderWithShipments(ctx, order, nil)
}

// CreateOrderWithShipments persists the order, line items, and direct supplier
// allocations in one ACID transaction.
func (r *OrderRepository) CreateOrderWithShipments(ctx context.Context, order domain.Order, shipments []domain.SupplierShipment) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := r.insertOrderWithShipmentsTx(ctx, tx, order, shipments, false); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// P0-14 (best-effort, separate tx): order placed - fulfilling
	// suppliers + store owner. A notify failure never fails the order.
	notifyOrderPlaced(ctx, r.db, order.ID)
	return nil
}

func (r *OrderRepository) insertOrderWithShipmentsTx(ctx context.Context, tx *sql.Tx, order domain.Order, shipments []domain.SupplierShipment, snapshotTitles bool) error {
	queryOrder := `
		INSERT INTO orders (
			id, customer_user_id, order_number, store_id, store_name, total_amount, discount_amount,
			payable_amount, payment_method, payment_status, customer_full_name, customer_phone,
			customer_province, customer_city, customer_address, customer_postal_code,
			status, tracking_code, shipping_cost, dropshipper_profit, platform_profit,
			referral_commission, referral_commission_released, referrer_seller_id,
			profit_released, created_at, updated_at, fulfillment_method,
			wallet_amount, gateway_amount, platform_addon_amount, supplier_addon_amount,
			inventory_captured, packaging_length_mm, packaging_width_mm, packaging_height_mm,
			dispute_reason, carrier, tracking_number, shipping_label, packed_at, box_size,
			weight_grams, idempotency_key, return_window_ends_at, seller_discount_amount, discount_code,
			shipping_quote_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19,
			$20, $21, $22, $23, $24, $25, $26, $27, $28,
			$29, $30, $31, $32, $33, $34, $35, $36,
			$37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48
		)
	`
	now := time.Now()
	fulfillmentMethod := order.FulfillmentMethod
	if fulfillmentMethod == "" {
		fulfillmentMethod = "warehouse"
	}
	_, err := tx.ExecContext(ctx, queryOrder,
		order.ID, order.CustomerUserID, order.OrderNumber, order.StoreID, order.StoreName,
		order.TotalAmount, order.DiscountAmount, order.PayableAmount, order.PaymentMethod,
		order.PaymentStatus, order.CustomerFullName, order.CustomerPhone, order.CustomerProvince, order.CustomerCity,
		order.CustomerAddress, order.CustomerPostalCode, order.Status, order.TrackingCode,
		order.ShippingCost, order.DropshipperProfit, order.PlatformProfit, order.ReferralCommission,
		order.ReferralCommissionReleased, order.ReferrerSellerID, order.ProfitReleased,
		now, now, fulfillmentMethod,
		order.WalletAmount, order.GatewayAmount, order.PlatformAddonAmount, order.SupplierAddonAmount,
		order.InventoryCaptured, orderNullInt(order.PackagingLengthMM), orderNullInt(order.PackagingWidthMM),
		orderNullInt(order.PackagingHeightMM), orderNullString(order.DisputeReason), orderNullString(order.Carrier),
		orderNullString(order.TrackingNumber), orderNullString(order.ShippingLabel), orderNullTime(order.PackedAt),
		orderNullString(order.BoxSize), orderNullInt(order.WeightGrams), orderNullString(order.IdempotencyKey),
		orderNullTime(order.ReturnWindowEndsAt), order.SellerDiscountAmount, order.DiscountCode, orderNullString(&order.ShippingQuoteID),
	)
	if err != nil {
		return fmt.Errorf("failed to insert order: %w", err)
	}

	queryItem := `
		INSERT INTO order_items (
			id, order_id, product_id, quantity, wholesale_cost,
			platform_fee, seller_margin, final_unit_price, discount_amount,
			variant_id, variant_name, variant_spnc
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	// Legacy repository callers do not claim a modern policy. New priced
	// snapshots append the policy in the same INSERT/transaction.
	queryItemVersion := strings.Replace(queryItem, "variant_id, variant_name, variant_spnc", "variant_id, variant_name, variant_spnc, pricing_policy_version", 1)
	queryItemVersion = strings.Replace(queryItemVersion, "$10, $11, $12)", "$10, $11, $12, $13)", 1)
	for _, item := range order.Items {
		itemID := uuid.New().String()
		itemArgs := []interface{}{itemID, order.ID, item.ProductID, item.Quantity,
			item.WholesaleCost, item.PlatformFee, item.SellerMargin, item.FinalUnitPrice, item.DiscountAmount,
			orderNullString(item.VariantID), orderNullString(item.VariantName), orderNullString(item.VariantSPNC)}
		insertItem := queryItem
		if item.PricingPolicyVersion != "" {
			insertItem = queryItemVersion
			itemArgs = append(itemArgs, item.PricingPolicyVersion)
		}
		_, err := tx.ExecContext(ctx, insertItem, itemArgs...)
		if err != nil {
			return fmt.Errorf("failed to insert order item: %w", err)
		}
		if snapshotTitles {
			if _, err := tx.ExecContext(ctx, `UPDATE order_items SET product_title_snapshot=$2 WHERE id=$1`, itemID, item.ProductTitle); err != nil {
				return fmt.Errorf("snapshot checkout line: %w", err)
			}
		}
		// Gap 13: persist the line's resolved origin in the SAME
		// transaction (migrations 202610240001 + 202610280001).
		// handleCreateOrder always stamps Routing (the single
		// resolver's verdict); direct repository callers that leave
		// it empty (legacy/test construction) write no origin row,
		// and the warehouse queue + return derivation fall back to
		// the product flag for such origin-less items.
		if item.Routing != "" {
			originType := "warehouse"
			if item.Routing == "supplier_direct" {
				originType = "supplier"
			}
			var originSupplierID interface{}
			if item.SupplierID != "" {
				originSupplierID = item.SupplierID
			}
			splits := item.OriginSplits
			if len(splits) == 0 {
				splits = []domain.OrderItemOriginSplit{{WarehouseID: item.WarehouseID, SupplierWarehouseID: item.SupplierWarehouseID, Label: item.OriginLabel, Quantity: item.Quantity, LegIndex: item.LegIndex}}
			}
			total := 0
			for _, split := range splits {
				if split.Quantity <= 0 || split.Quantity > item.Quantity-total {
					return fmt.Errorf("invalid order origin split")
				}
				total += split.Quantity
			}
			if total != item.Quantity {
				return fmt.Errorf("incomplete order origin split")
			}
			for _, split := range splits {
				if _, err := tx.ExecContext(ctx, `
				INSERT INTO order_item_origins (
					id, order_id, order_item_id, leg_index, origin_type,
					supplier_id, warehouse_id, supplier_warehouse_id, origin_label, quantity
				) VALUES ($1, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10)
			`, uuid.New().String(), order.ID, itemID, split.LegIndex, originType,
					originSupplierID, orderNullString(split.WarehouseID),
					orderNullString(split.SupplierWarehouseID), split.Label, split.Quantity); err != nil {
					return fmt.Errorf("failed to insert order item origin: %w", err)
				}
			}
		}
	}

	for _, shipment := range shipments {
		addressJSON, err := json.Marshal(shipment.ShippingAddress)
		if err != nil {
			return fmt.Errorf("failed to marshal supplier shipment address: %w", err)
		}
		status := shipment.Status
		if status == "" {
			status = "pending"
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO supplier_shipments (
				id, order_id, supplier_id, shipping_address, customer_phone, customer_name, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		`, shipment.ID, shipment.OrderID, shipment.SupplierID, addressJSON, shipment.CustomerPhone, shipment.CustomerName, status)
		if err != nil {
			return fmt.Errorf("failed to insert supplier shipment: %w", err)
		}
	}

	return nil
}

// GetOrderByID retrieves an order with its items by ID
func (r *OrderRepository) GetOrderByID(ctx context.Context, id string) (*domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders WHERE id = $1"
	row := r.db.QueryRowContext(ctx, query, id)
	return r.scanOrder(ctx, row)
}

// GetOrderByTrackingCode retrieves an order by its tracking code
func (r *OrderRepository) GetOrderByTrackingCode(ctx context.Context, code string) (*domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders WHERE LOWER(tracking_code) = LOWER($1)"
	row := r.db.QueryRowContext(ctx, query, code)
	return r.scanOrder(ctx, row)
}

// GetOrdersByStoreID retrieves orders for a store
func (r *OrderRepository) GetOrdersByStoreID(ctx context.Context, storeID string) ([]domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders WHERE store_id = $1 ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(orderScanDest(&o)...); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}

// ConfirmOrderDelivery confirms order delivery by the actual customer owner.
//
// D34 (live parity): confirmation ONLY moves the order to
// delivered_confirmed and records the return window (D35). It does NOT
// release profit or the referral-commission flag: those move only when
// the return window closes, in ReleaseClosedReturnWindows (the release
// point Team 09's delivery-lock-releaser worker must call) or in the
// worker's own stage 2. The old source behaviour of releasing profit
// in this UPDATE is deliberately removed; the handler message was
// changed with it. The locked referral BONUS row release below is
// retained from source (it is a bonus, not order profit) and is
// idempotent with the release-time backstop in
// ReleaseClosedReturnWindows.
// customerUserID MUST be non-empty and match the order owner. Empty customerUserID will fail immediately (fail-closed).
// Only orders in 'shipped', 'delivered_unconfirmed', or 'delivered' can be confirmed.
// If this is the referred store's first delivered_confirmed order, any locked referral bonus for the referrer is atomically released.
func (r *OrderRepository) ConfirmOrderDelivery(ctx context.Context, idOrNumber, customerUserID string) error {
	trimmedUserID := strings.TrimSpace(customerUserID)
	if trimmedUserID == "" {
		return fmt.Errorf("customerUserID is required for delivery confirmation (fail-closed): %w", sql.ErrNoRows)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id=$1 OR order_number=$1`, idOrNumber).Scan(&financeOrderID); err != nil {
		return err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID); err != nil {
		return err
	}

	now := time.Now()
	// D35: window = confirmed_at + return_window_days from
	// platform_settings (live = 7, migration 202610310007; fallback 7).
	// TODO(U19): the exact live origin event is unverified (3 live rows
	// are exactly confirmed_at + 7 days, 3 are ~7 days 23 hours after
	// confirmed_at, and 2 delivered_confirmed rows have a NULL window).
	// The confirmed rule is implemented; NULL-window orders are NOT
	// given an invented fallback window in the release path.
	windowDays := returnWindowDaysFromTx(ctx, tx)
	windowEnds := now.AddDate(0, 0, windowDays)
	query := `
		UPDATE orders
		SET status = 'delivered_confirmed',
		    confirmed_at = $1,
		    return_window_ends_at = $2,
		    updated_at = $1
		WHERE (id = $3 OR order_number = $3)
		  AND customer_user_id = $4::uuid
		  AND status IN ('shipped', 'delivered_unconfirmed', 'delivered')
		RETURNING store_id
	`
	var orderStoreID string
	err = tx.QueryRowContext(ctx, query, now, windowEnds, idOrNumber, trimmedUserID).Scan(&orderStoreID)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}

	// Atomic release of locked referral bonus if this is the referred store's first confirmed order
	if orderStoreID != "" {
		releaseBonusQuery := `
			UPDATE referral_bonuses
			SET status = 'released',
			    released_at = $1
			WHERE referred_store_id = $2
			  AND status = 'locked'
		`
		_, _ = tx.ExecContext(ctx, releaseBonusQuery, now, orderStoreID)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// P1-3 (best-effort): delivery confirmed - customer + store owner
	// + suppliers. Profit is NOT released here (window close only).
	NotifyDeliveryConfirmed(ctx, r.db, idOrNumber, false)
	return nil
}

// ConfirmOrderDeliverySystem allows an explicit administrative or worker system operation to confirm delivery.
// This is strictly segregated from customer endpoints.
func (r *OrderRepository) ConfirmOrderDeliverySystem(ctx context.Context, idOrNumber string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id=$1 OR order_number=$1`, idOrNumber).Scan(&financeOrderID); err != nil {
		return err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID); err != nil {
		return err
	}

	now := time.Now()
	// D34/D35: staged, exactly like ConfirmOrderDelivery: status +
	// return window only; profit stays locked until the window closes.
	// TODO(U19): see ConfirmOrderDelivery for the unverified origin.
	windowDays := returnWindowDaysFromTx(ctx, tx)
	windowEnds := now.AddDate(0, 0, windowDays)
	query := `
		UPDATE orders
		SET status = 'delivered_confirmed',
		    confirmed_at = $1,
		    return_window_ends_at = $2,
		    updated_at = $1
		WHERE (id = $3 OR order_number = $3)
		  AND status IN ('shipped', 'delivered_unconfirmed', 'delivered')
		RETURNING store_id
	`
	var orderStoreID string
	err = tx.QueryRowContext(ctx, query, now, windowEnds, idOrNumber).Scan(&orderStoreID)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}

	// Atomic release of locked referral bonus if this is the referred store's first confirmed order
	if orderStoreID != "" {
		releaseBonusQuery := `
			UPDATE referral_bonuses
			SET status = 'released',
			    released_at = $1
			WHERE referred_store_id = $2
			  AND status = 'locked'
		`
		_, _ = tx.ExecContext(ctx, releaseBonusQuery, now, orderStoreID)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// P1-3 (best-effort): system-confirmed delivery.
	NotifyDeliveryConfirmed(ctx, r.db, idOrNumber, false)
	return nil
}

// UpdateOrderStatusFromCarrier updates the order status based on carrier events,
// WITHOUT releasing profit or commissions (which strictly require customer confirmation or 20-day lock expiry).
// Fix69 O2: carrier statuses are mapped to orders-status-CHECK-legal values
// only. 'in_transit' moves the order FORWARD to 'shipped' (orders has no
// in_transit state; the old mapping back to 'processing' was backwards),
// and 'failed' maps to 'delivery_disputed', the CHECK-legal delivery-failure
// state (same state finance CreateDispute sets, with disputed_at). The
// shipment-level 'in_transit'/'failed' states stay on supplier_shipments,
// which has no such CHECK.
func (r *OrderRepository) UpdateOrderStatusFromCarrier(ctx context.Context, idOrNumber, status string) error {
	var targetStatus string
	switch status {
	case "delivered":
		targetStatus = "delivered"
	case "shipped":
		targetStatus = "shipped"
	case "in_transit":
		targetStatus = "shipped"
	case "failed":
		targetStatus = "delivery_disputed"
	default:
		return fmt.Errorf("unsupported carrier status transition: %s", status)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id=$1 OR order_number=$1`, idOrNumber).Scan(&financeOrderID); err != nil {
		return err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID); err != nil {
		return err
	}
	query := `
		UPDATE orders
		SET status = $2,
		    shipped_at = CASE WHEN $2 = 'shipped' AND shipped_at IS NULL THEN NOW() ELSE shipped_at END,
		    disputed_at = CASE WHEN $2 = 'delivery_disputed' THEN NOW() ELSE disputed_at END,
		    updated_at = NOW()
		WHERE (id = $1 OR order_number = $1)
		  AND status IN ('processing', 'shipped', 'delivered_unconfirmed', 'delivered')
		  AND ($2 <> 'delivered' OR NOT EXISTS (
			SELECT 1 FROM supplier_shipments
			WHERE order_id = orders.id AND status <> 'delivered'
		  ))
	`
	result, err := tx.ExecContext(ctx, query, idOrNumber, targetStatus)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// P1-5 (best-effort): carrier 'failed' flips the order to
	// delivery_disputed - the customer is told.
	if targetStatus == "delivery_disputed" {
		notifyCarrierFailed(ctx, r.db, idOrNumber)
	}
	return nil
}

// UpdateWarehouseOrderStatusFromCarrier applies an authenticated carrier update
// to warehouse-managed orders, which do not have supplier_shipments rows.
// Fix69 O2: same mapping as the supplier-direct path — carrier
// 'in_transit' maps forward to order 'shipped' (orders has no in_transit
// state; writing it violated the orders CHECK), and 'failed' maps to the
// CHECK-legal 'delivery_disputed'. The old shipped/in_transit guard half
// was dead (an order can never legally sit in 'in_transit'); failed is
// now also accepted from 'packed'/'shipped', the realistic post-dispatch
// failure states, not only from pre-dispatch picking states.
func (r *OrderRepository) UpdateWarehouseOrderStatusFromCarrier(ctx context.Context, trackingNumber, status string) (*domain.Order, error) {
	var targetStatus string
	switch status {
	case "shipped":
		targetStatus = "shipped"
	case "in_transit":
		targetStatus = "shipped"
	case "delivered":
		targetStatus = "delivered"
	case "failed":
		targetStatus = "delivery_disputed"
	default:
		return nil, sql.ErrNoRows
	}

	// Critic fix (Phase 2): a repeated webhook for an order ALREADY in
	// the target state is a no-op success — return the order, notify
	// nobody. The transition UPDATE below has NO `status = $2` leg
	// (the old leg re-matched an already-disputed order and re-notified
	// on every repeated 'failed' webhook); without the pre-check here,
	// removing that leg would turn a duplicate webhook into a 404.
	var existingID, currentStatus string
	if err := r.db.QueryRowContext(ctx, `SELECT id, status FROM orders WHERE tracking_number = $1 AND fulfillment_method IN ('warehouse', 'mixed')`, trackingNumber).Scan(&existingID, &currentStatus); err == nil && currentStatus == targetStatus {
		return r.GetOrderByID(ctx, existingID)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE tracking_number=$1 AND fulfillment_method IN ('warehouse','mixed')`, trackingNumber).Scan(&financeOrderID); err != nil {
		return nil, err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID); err != nil {
		return nil, err
	}
	var orderID string
	query := `
		UPDATE orders
		SET status = $2,
		    shipped_at = CASE WHEN $2 = 'shipped' AND shipped_at IS NULL THEN NOW() ELSE shipped_at END,
		    disputed_at = CASE WHEN $2 = 'delivery_disputed' THEN NOW() ELSE disputed_at END,
		    updated_at = NOW()
		WHERE tracking_number = $1
		  AND fulfillment_method IN ('warehouse', 'mixed')
		  AND (
			(status IN ('processing', 'picking', 'picked', 'packed') AND $2 = 'shipped') OR
			(status = 'shipped' AND $2 = 'delivered') OR
			(status IN ('processing', 'picking', 'picked', 'packed', 'shipped') AND $2 = 'delivery_disputed')
		  )
		RETURNING id
	`
	if err := tx.QueryRowContext(ctx, query, trackingNumber, targetStatus).Scan(&orderID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	order, err := r.GetOrderByID(ctx, orderID)
	if err != nil || order == nil {
		return nil, err
	}
	// P1-5 (best-effort): carrier 'failed' on a warehouse order.
	if targetStatus == "delivery_disputed" {
		notifyCarrierFailed(ctx, r.db, orderID)
	}
	return order, nil
}

func (r *OrderRepository) scanOrder(ctx context.Context, row *sql.Row) (*domain.Order, error) {
	var o domain.Order
	if err := row.Scan(orderScanDest(&o)...); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan order: %w", err)
	}

	itemQuery := `
		SELECT oi.id, oi.order_id, oi.product_id, COALESCE(to_jsonb(oi)->>'product_title_snapshot',p.title,''), oi.quantity,
		       oi.wholesale_cost, oi.platform_fee, oi.seller_margin, oi.final_unit_price, COALESCE(oi.discount_amount, 0),
		       oi.variant_id, oi.variant_name, oi.variant_spnc, COALESCE(to_jsonb(oi)->>'pricing_policy_version', 'legacy-unversioned')
		FROM order_items oi
		LEFT JOIN products p ON oi.product_id = p.id
		WHERE oi.order_id = $1
	`
	rows, err := r.db.QueryContext(ctx, itemQuery, o.ID)
	if err != nil {
		return nil, fmt.Errorf("read order items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it domain.OrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.ProductTitle, &it.Quantity, &it.WholesaleCost, &it.PlatformFee, &it.SellerMargin, &it.FinalUnitPrice, &it.DiscountAmount, &it.VariantID, &it.VariantName, &it.VariantSPNC, &it.PricingPolicyVersion); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		o.Items = append(o.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read order items: %w", err)
	}

	return &o, nil
}

// GetOrdersByCustomerPhone retrieves orders placed by a specific customer phone
func (r *OrderRepository) GetOrdersByCustomerPhone(ctx context.Context, phone string) ([]domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders WHERE customer_phone = $1 ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, phone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(orderScanDest(&o)...); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	// Merge (g): same lines fix as the buyer-history list — attach
	// order lines in one query so list and detail agree. An
	// items-query failure leaves items empty rather than failing
	// the listing.
	if len(orders) > 0 {
		ids := make([]string, 0, len(orders))
		for _, o := range orders {
			ids = append(ids, o.ID)
		}
		if byOrder, ierr := r.loadItemsByOrderIDs(ctx, ids); ierr == nil {
			for i := range orders {
				orders[i].Items = byOrder[orders[i].ID]
			}
		}
	}
	return orders, nil
}

// loadItemsByOrderIDs returns order lines grouped by order id, with the
// product title resolved via LEFT JOIN. COALESCE keeps a line whose
// product row is gone (the frontend falls back to the product id for
// the title); this mirrors scanOrder's item query, batched for lists.
func (r *OrderRepository) loadItemsByOrderIDs(ctx context.Context, orderIDs []string) (map[string][]domain.OrderItem, error) {
	if len(orderIDs) == 0 {
		return map[string][]domain.OrderItem{}, nil
	}
	ph := make([]string, 0, len(orderIDs))
	args := make([]interface{}, 0, len(orderIDs))
	for i, id := range orderIDs {
		ph = append(ph, fmt.Sprintf("$%d", i+1))
		args = append(args, id)
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT oi.id, oi.order_id, oi.product_id, COALESCE(p.title, ''), oi.quantity,
		       oi.wholesale_cost, oi.platform_fee, oi.seller_margin, oi.final_unit_price, COALESCE(oi.discount_amount, 0),
		       oi.variant_id, oi.variant_name, oi.variant_spnc, COALESCE(to_jsonb(oi)->>'pricing_policy_version', 'legacy-unversioned')
		FROM order_items oi
		LEFT JOIN products p ON oi.product_id = p.id
		WHERE oi.order_id IN (`+strings.Join(ph, ", ")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byOrder := map[string][]domain.OrderItem{}
	for rows.Next() {
		var it domain.OrderItem
		if err := rows.Scan(
			&it.ID, &it.OrderID, &it.ProductID, &it.ProductTitle, &it.Quantity,
			&it.WholesaleCost, &it.PlatformFee, &it.SellerMargin, &it.FinalUnitPrice, &it.DiscountAmount,
			&it.VariantID, &it.VariantName, &it.VariantSPNC, &it.PricingPolicyVersion,
		); err != nil {
			return nil, err
		}
		byOrder[it.OrderID] = append(byOrder[it.OrderID], it)
	}
	return byOrder, rows.Err()
}

func (r *OrderRepository) GetOrdersByCustomerUserID(ctx context.Context, userID string) ([]domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders WHERE customer_user_id = $1::uuid ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(orderScanDest(&o)...); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	// ISO-D W4: the buyer-history list previously returned orders without
	// lines (unlike scanOrder, used by the detail path, which loads
	// order_items). History cards therefore rendered empty items after a
	// refresh (mapBackendOrder maps a nil items slice to []). Load all
	// lines in one query and attach them, so list and detail agree.
	// An items-query failure leaves items empty (the previous behavior)
	// rather than failing the whole history listing.
	if len(orders) > 0 {
		ids := make([]string, 0, len(orders))
		for _, o := range orders {
			ids = append(ids, o.ID)
		}
		if byOrder, ierr := r.loadItemsByOrderIDs(ctx, ids); ierr == nil {
			for i := range orders {
				orders[i].Items = byOrder[orders[i].ID]
			}
		}
	}
	if err := r.attachRefundSummaries(ctx, orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// GetAllOrders retrieves all orders ordered by creation date
func (r *OrderRepository) GetAllOrders(ctx context.Context) ([]domain.Order, error) {
	query := "SELECT " + orderSelectColumns + " FROM orders ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(orderScanDest(&o)...); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	// Merge (g): same lines fix as the buyer-history list — attach
	// order lines in one query so list and detail agree. An
	// items-query failure leaves items empty rather than failing
	// the listing.
	if len(orders) > 0 {
		ids := make([]string, 0, len(orders))
		for _, o := range orders {
			ids = append(ids, o.ID)
		}
		if byOrder, ierr := r.loadItemsByOrderIDs(ctx, ids); ierr == nil {
			for i := range orders {
				orders[i].Items = byOrder[orders[i].ID]
			}
		}
	}
	return orders, nil
}

func (r *OrderRepository) GetOrderByNumber(ctx context.Context, orderNumber string) (*domain.Order, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+orderSelectColumns+" FROM orders WHERE order_number = $1", orderNumber)
	return r.scanOrder(ctx, row)
}

func (r *OrderRepository) SetPaymentStatus(ctx context.Context, orderID, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE orders SET payment_status = $2, updated_at = NOW() WHERE id = $1`, orderID, status)
	return err
}

// UpdateOrderFulfillment updates status, tracking code, and shipped_at for an order
func (r *OrderRepository) UpdateOrderFulfillment(ctx context.Context, orderID, status, trackingCode string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var canonicalID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id=$1 OR order_number=$1`, orderID).Scan(&canonicalID); err != nil {
		return err
	}
	if err := lockFulfillmentOrderTx(ctx, tx, canonicalID); err != nil {
		return err
	}
	if status == "shipped" || status == "delivered" {
		if err := assertOrderDispatchAllowedTx(ctx, tx, canonicalID); err != nil {
			return err
		}
	}
	now := time.Now()
	query := `
		UPDATE orders
		SET status = $1,
		    tracking_code = CASE WHEN $2 <> '' THEN $2 ELSE tracking_code END,
		    shipped_at = CASE WHEN $1 = 'shipped' AND shipped_at IS NULL THEN $3 ELSE shipped_at END,
		    updated_at = $3
		WHERE (id = $4 OR order_number = $4) AND status <> 'cancelled'
	`
	res, err := tx.ExecContext(ctx, query, status, trackingCode, now, canonicalID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrShipmentState
	}
	return tx.Commit()
}

type PaymentTransaction struct {
	ID           string
	OrderID      string
	OrderNumber  string
	GatewayToken string
	RRN          sql.NullString
	CardMask     sql.NullString
	Amount       int64
	Status       string
	CreatedAt    time.Time
}

func (r *OrderRepository) CreatePaymentTransaction(ctx context.Context, tx PaymentTransaction) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT DO NOTHING
	`, tx.ID, tx.OrderID, tx.OrderNumber, tx.GatewayToken, tx.Amount)
	if err != nil {
		return err
	}
	return nil
}

func (r *OrderRepository) GetPaymentTransaction(ctx context.Context, gatewayToken string) (*PaymentTransaction, error) {
	var tx PaymentTransaction
	err := r.db.QueryRowContext(ctx, `
		SELECT id, order_id, order_number, gateway_token, rrn, card_mask, amount, status, created_at
		FROM payment_transactions WHERE gateway_token = $1
	`, gatewayToken).Scan(&tx.ID, &tx.OrderID, &tx.OrderNumber, &tx.GatewayToken, &tx.RRN, &tx.CardMask, &tx.Amount, &tx.Status, &tx.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

func (r *OrderRepository) GetPaymentTransactionByOrderID(ctx context.Context, orderID string) (*PaymentTransaction, error) {
	var tx PaymentTransaction
	err := r.db.QueryRowContext(ctx, `
		SELECT id, order_id, order_number, gateway_token, rrn, card_mask, amount, status, created_at
		FROM payment_transactions
		WHERE order_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, orderID).Scan(&tx.ID, &tx.OrderID, &tx.OrderNumber, &tx.GatewayToken, &tx.RRN, &tx.CardMask, &tx.Amount, &tx.Status, &tx.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

// RecordPaymentFailure records a gateway verification failure on
// the payment transaction (status='failed', a CHECK-legal value per
// migration 202610250006) and on the order, then best-effort notifies
// the customer (P1-9). Idempotent: a non-pending transaction is left
// alone and no duplicate row is written.
func (r *OrderRepository) RecordPaymentFailure(ctx context.Context, gatewayToken string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID, status string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM payment_transactions WHERE gateway_token=$1`, gatewayToken).Scan(&orderID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if err := lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return err
	}
	var inquiry bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_payment_verifications WHERE order_id=$1 AND payment_id IN (SELECT id FROM payment_transactions WHERE gateway_token=$2))`, orderID, gatewayToken).Scan(&inquiry); err != nil {
		return err
	}
	if inquiry {
		return tx.Commit()
	}
	if err := tx.QueryRowContext(ctx, `SELECT order_id, status FROM payment_transactions WHERE gateway_token = $1 FOR UPDATE`, gatewayToken).Scan(&orderID, &status); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if status != "pending" {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions SET status = 'failed' WHERE gateway_token = $1 AND status = 'pending'`, gatewayToken); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET payment_status = 'failed', updated_at = NOW() WHERE id = $1 AND payment_status = 'pending'`, orderID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	notifyPaymentFailed(ctx, r.db, orderID)
	return nil
}

func (r *OrderRepository) MarkPaymentPaid(ctx context.Context, gatewayToken, rrn, cardMask string) (*PaymentTransaction, error) {
	dbTx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer dbTx.Rollback()
	// Lock the order BEFORE the payment, matching the expiry job. A callback
	// may never resurrect an order that the two-minute sweep cancelled.
	var lockedOrderID, orderStatus, orderPayment string
	var deadline *time.Time
	if err := dbTx.QueryRowContext(ctx, `SELECT o.id,o.status,o.payment_status,o.payment_expires_at FROM orders o JOIN payment_transactions p ON p.order_id=o.id WHERE p.gateway_token=$1 FOR UPDATE OF o`, gatewayToken).Scan(&lockedOrderID, &orderStatus, &orderPayment, &deadline); err != nil {
		return nil, err
	}
	if err := lockOrderFinanceTx(ctx, dbTx, lockedOrderID, true); err != nil {
		return nil, err
	}

	var payment PaymentTransaction
	err = dbTx.QueryRowContext(ctx, `
		SELECT id, order_id, order_number, gateway_token, rrn, card_mask, amount, status, created_at
		FROM payment_transactions WHERE gateway_token = $1 FOR UPDATE
	`, gatewayToken).Scan(
		&payment.ID, &payment.OrderID, &payment.OrderNumber, &payment.GatewayToken,
		&payment.RRN, &payment.CardMask, &payment.Amount, &payment.Status, &payment.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The caller has confirmed payment. Inventory admission may fail, but the
	// money fact must survive cancellation/expiry. The legacy signature does not
	// certify a structured provider receipt: its liability stays unknown until
	// independently verified. Never turn a historical refunded marker into cash.
	if orderStatus == "cancelled" || (orderPayment != "paid" && (orderPayment != "pending" || (deadline != nil && !time.Now().Before(*deadline)))) {
		if payment.Status != "refunded" {
			if err := recordLatePaidCancellationTx(ctx, dbTx, &payment, rrn, cardMask); err != nil {
				return nil, err
			}
			if err := dbTx.Commit(); err != nil {
				return nil, err
			}
		}
		return nil, ErrPaymentExpired
	}
	var compensation bool
	if err := dbTx.QueryRowContext(ctx, `SELECT compensation_capture FROM payment_transactions WHERE id=$1`, payment.ID).Scan(&compensation); err != nil {
		return nil, err
	}
	if compensation {
		return nil, ErrPaymentExpired
	}
	if payment.Status == "paid" {
		return &payment, nil
	}
	if payment.Status != "pending" {
		return nil, ErrPaymentExpired
	}

	handledCapture, err := captureOrderInventoryTx(ctx, dbTx, payment.OrderID)
	if err != nil {
		return nil, err
	}
	if !handledCapture {
		itemRows, err := dbTx.QueryContext(ctx, `
		SELECT product_id, quantity, variant_id
		FROM order_items
		WHERE order_id = $1
	`, payment.OrderID)
		if err != nil {
			return nil, err
		}
		defer itemRows.Close()

		type inventoryItem struct {
			productID string
			quantity  int
			variantID sql.NullString
		}
		var items []inventoryItem
		for itemRows.Next() {
			var item inventoryItem
			if err := itemRows.Scan(&item.productID, &item.quantity, &item.variantID); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if err := itemRows.Err(); err != nil {
			return nil, err
		}
		if err := itemRows.Close(); err != nil {
			return nil, err
		}
		for _, item := range items {
			result, err := dbTx.ExecContext(ctx, `
			UPDATE products
			SET stock = stock - $2
			WHERE id = $1 AND stock >= $2
		`, item.productID, item.quantity)
			if err != nil {
				return nil, err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			if rows != 1 {
				return nil, ErrInsufficientInventory
			}
			// Fix69 O4: decrement the variant stock alongside the product
			// stock. Guarded by stock >= qty and deliberately NOT fatal
			// on zero rows: a 0-stock variant is untracked (see the
			// create-time gate in handleCreateOrder), so it must not
			// fail a payment whose product stock was sufficient.
			// Product stock stays the authoritative accounting.
			if item.variantID.Valid && item.variantID.String != "" {
				if _, err := dbTx.ExecContext(ctx, `
				UPDATE product_variants
				SET stock = stock - $2
				WHERE id = $1 AND stock >= $2
			`, item.variantID.String, item.quantity); err != nil {
					return nil, err
				}
			}
		}
	}

	if _, err = dbTx.ExecContext(ctx, `
		UPDATE payment_transactions
		SET rrn = $2, card_mask = $3, status = 'paid'
		WHERE gateway_token = $1 AND status = 'pending'
	`, gatewayToken, rrn, cardMask); err != nil {
		return nil, err
	}
	if _, err = dbTx.ExecContext(ctx, `
		UPDATE orders SET payment_status = 'paid', inventory_captured = TRUE, updated_at = NOW() WHERE id = $1
	`, payment.OrderID); err != nil {
		return nil, err
	}
	if err = dbTx.Commit(); err != nil {
		return nil, err
	}
	// P0-14 (best-effort): payment confirmed - fulfilment unblocked
	// for suppliers + store owner. P1-10: low-stock check on the
	// just-decremented products. Notify failures never fail payment.
	notifyPaymentConfirmed(ctx, r.db, payment.OrderID)
	notifyLowStock(ctx, r.db, payment.OrderID)

	payment.RRN = sql.NullString{String: rrn, Valid: true}
	payment.CardMask = sql.NullString{String: cardMask, Valid: true}
	payment.Status = "paid"
	return &payment, nil
}

// ---- Team 03 (live parity, D34/D35/D36/D37/D38) ----

func orderNullString(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func orderNullInt(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

func orderNullTime(p *time.Time) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// GetReturnWindowDays reads platform_settings.return_window_days
// (migration 202610310007, live = 7) and falls back to 7 when the row
// or column is missing, matching the live public handler behaviour.
func (r *OrderRepository) GetReturnWindowDays(ctx context.Context) int {
	var days int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(return_window_days, 7) FROM platform_settings ORDER BY id LIMIT 1`).Scan(&days)
	if err != nil || days < 1 || days > 365 {
		return 7
	}
	return days
}

func returnWindowDaysFromTx(ctx context.Context, tx *sql.Tx) int {
	var days int
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(return_window_days, 7) FROM platform_settings ORDER BY id LIMIT 1`).Scan(&days)
	if err != nil || days < 1 || days > 365 {
		return 7
	}
	return days
}

// ReleaseClosedReturnWindows is the D34 release point: it flips
// profit_released for delivered_confirmed orders whose recorded return
// window has closed, and writes the live ledger pair for each release
// (seller/credit + platform/debit, operator system:delivery-lock-releaser).
//
// INTEGRATION NOTE (Team 09 worker): worker/scheduler.go stage 2 flips
// the same flags WITHOUT writing this ledger pair, and this file is
// out of Team 03 scope, so the worker was not edited here. Both paths
// are guarded by profit_released = FALSE, so at most one of them wins
// for a given order and the pair is never double-written; but the
// ledger is only guaranteed when the worker's stage-2 UPDATE is
// replaced by a call to this method. That one-line wiring is left to
// the supervisor at integration — do NOT add a second release path
// that writes ledger rows independently.
//
// TODO(U19/U26): orders with a NULL return_window_ends_at are skipped,
// exactly like the worker. No fallback window (for example
// confirmed_at + 7 days) is invented: live has delivered_confirmed
// rows with a NULL window that are already released, so the NULL
// rule is unverified. TODO(U26): no staged threshold beyond the
// 20-day auto-confirm and the window close is implemented here.
func (r *OrderRepository) ReleaseClosedReturnWindows(ctx context.Context) (int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, store_id, dropshipper_profit
		FROM orders
		WHERE status = 'delivered_confirmed'
		  AND profit_released = FALSE
		  AND return_window_ends_at IS NOT NULL
		  AND return_window_ends_at <= NOW()
		ORDER BY created_at ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type releaseCandidate struct {
		id, storeID string
		profit      int64
	}
	var candidates []releaseCandidate
	for rows.Next() {
		var c releaseCandidate
		if err := rows.Scan(&c.id, &c.storeID, &c.profit); err != nil {
			return 0, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	var released int64
	for _, c := range candidates {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return released, err
		}
		if err := lockOrderFinanceTx(ctx, tx, c.id); err != nil {
			tx.Rollback()
			return released, err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE orders
			SET profit_released = TRUE,
			    profit_released_at = NOW(),
			    referral_commission_released = TRUE,
			    updated_at = NOW()
			WHERE id = $1 AND profit_released = FALSE
		`, c.id)
		if err != nil {
			tx.Rollback()
			return released, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			// Won by a concurrent releaser; its ledger (if any) is
			// that releaser's responsibility, never written twice.
			tx.Rollback()
			continue
		}
		if c.profit > 0 {
			now := time.Now()
			creditID := "REL-" + uuid.NewString()
			debitID := "REL-" + uuid.NewString()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
				VALUES ($1, $2, $3, 'seller', 'credit', $4, 'Staged profit release after return window closed', 'system:delivery-lock-releaser', $5)
			`, creditID, now, c.storeID, c.profit, c.id); err != nil {
				tx.Rollback()
				return released, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
				VALUES ($1, $2, 'platform', 'platform', 'debit', $3, 'Staged profit release after return window closed', 'system:delivery-lock-releaser', $4)
			`, debitID, now, c.profit, c.id); err != nil {
				tx.Rollback()
				return released, err
			}
		}
		// Idempotent backstop for the locked referral bonus of the
		// referred store (source released it at confirmation; the
		// status filter makes a second release a no-op).
		_, _ = tx.ExecContext(ctx, `
			UPDATE referral_bonuses
			SET status = 'released', released_at = NOW()
			WHERE referred_store_id = $1 AND status = 'locked'
		`, c.storeID)
		if err := tx.Commit(); err != nil {
			return released, err
		}
		released++
		// P1-3 (best-effort): profit released - store owner + suppliers.
		notifyProfitReleased(ctx, r.db, c.id, c.profit)
	}
	return released, nil
}

var ErrReturnWindowClosed = fmt.Errorf("return window has closed")

// CancelOrder cancels a not-yet-shipped order. Profit fields are
// deliberately PRESERVED (not zeroed): live cancelled rows keep their
// values. TODO(U18): whether preservation is the live rule or just
// the observed data is unverified. TODO(U20): the origin of the 13
// live cancelled orders is unverified (admin? dispute? seed?); this
// is the customer/admin cancel path, not a claim about that origin.
// Inventory is restored ONLY when inventory_captured is true
// (migration 202610250009): restoring on any other order would mint
// phantom stock for orders whose stock was never captured.
func (r *OrderRepository) CancelOrder(ctx context.Context, idOrNumber, customerUserID string, isAdmin bool) error {
	return r.cancelOrder(ctx, idOrNumber, customerUserID, isAdmin, "")
}

func (r *OrderRepository) cancelOrder(ctx context.Context, idOrNumber, customerUserID string, isAdmin bool, admissionPaymentID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID, status string
	var captured bool
	var q string
	var args []interface{}
	if isAdmin {
		q = `SELECT id, status, inventory_captured FROM orders WHERE id = $1 OR order_number = $1 FOR UPDATE`
		args = []interface{}{idOrNumber}
	} else {
		if strings.TrimSpace(customerUserID) == "" {
			return fmt.Errorf("customerUserID is required for order cancellation (fail-closed): %w", sql.ErrNoRows)
		}
		q = `SELECT id, status, inventory_captured FROM orders WHERE (id = $1 OR order_number = $1) AND customer_user_id = $2::uuid FOR UPDATE`
		args = []interface{}{idOrNumber, customerUserID}
	}
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&orderID, &status, &captured); err != nil {
		return err
	}
	if admissionPaymentID != "" {
		var confirmed bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=$1 AND order_id=$2)`, admissionPaymentID, orderID).Scan(&confirmed); err != nil {
			return err
		}
		if !confirmed {
			return ErrRefundPrincipalUnresolved
		}
		if captured {
			return errPaymentAlreadyCaptured
		}
	}
	if err := assertOrderCancellationAllowedTx(ctx, tx, orderID); err != nil {
		return err
	}
	if status == "cancelled" {
		return tx.Commit()
	}
	if err := lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return err
	}
	if err := lockCancellationInventoryTx(ctx, tx, orderID); err != nil {
		return err
	}
	switch status {
	case "processing", "pending", "picking", "picked", "packed":
	default:
		return fmt.Errorf("%w: %s", ErrOrderCancellationState, status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'cancelled', updated_at = NOW() WHERE id = $1`, orderID); err != nil {
		return err
	}
	if captured {
		handledRestore, err := restoreOrderInventoryTx(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if !handledRestore {
			skuAccounting, err := hasSKUInventoryTx(ctx, tx)
			if err != nil {
				return err
			}
			itemRows, err := tx.QueryContext(ctx, `SELECT id::text, product_id, quantity, variant_id FROM order_items WHERE order_id = $1`, orderID)
			if err != nil {
				return err
			}
			type stockItem struct {
				itemID    string
				productID string
				qty       int
				variantID sql.NullString
			}
			var items []stockItem
			for itemRows.Next() {
				var it stockItem
				if err := itemRows.Scan(&it.itemID, &it.productID, &it.qty, &it.variantID); err != nil {
					itemRows.Close()
					return err
				}
				items = append(items, it)
			}
			itemRows.Close()
			if err := itemRows.Err(); err != nil {
				return err
			}
			for _, it := range items {
				covered := false
				if skuAccounting {
					covered, err = canonicalCoveredTx(ctx, tx, it.productID)
					if err != nil {
						return err
					}
				}
				if covered {
					var supplierID string
					if err := tx.QueryRowContext(ctx, `SELECT COALESCE(supplier_id,'') FROM products WHERE id=$1 FOR UPDATE`, it.productID).Scan(&supplierID); err != nil {
						return err
					}
					// Historical capture proved product quantity, not the SKU or
					// original lot. Restore to an explicit unallocated holding lot.
					if err := upsertInventoryMovementTx(ctx, tx, it.productID, supplierID, it.qty, "sold", "sellable", "customer", "legacy-cancel:"+orderID, "order_cancel", orderID, "legacy-cancel:"+it.itemID, customerUserID, `{}`); err != nil {
						return err
					}
				} else if _, err := tx.ExecContext(ctx, `UPDATE products SET stock = stock + $2 WHERE id = $1`, it.productID, it.qty); err != nil {
					return err
				}
				// Fix69 O4: restore the variant stock alongside the
				// product stock, inside the same inventory_captured
				// gate. See the create-time note: this mirrors the
				// product restore exactly, including its gate.
				if !skuAccounting && it.variantID.Valid && it.variantID.String != "" {
					if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock = stock + $2 WHERE id = $1`, it.variantID.String, it.qty); err != nil {
						return err
					}
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET inventory_captured=false WHERE id=$1`, orderID); err != nil {
		return err
	}
	if err := synchronizeVerifiedPaymentFactsTx(ctx, tx, orderID); err != nil {
		return err
	}
	if err := ensureCancellationRefundsTx(ctx, tx, orderID, customerUserID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// P0-5 (best-effort): cancelled - fulfilling suppliers (stop
	// preparing) + store owner.
	notifyOrderCancelled(ctx, r.db, orderID)
	return nil
}

// ApplyDisputeRefundEffect is a compatibility projection. A resolved verdict
// reserves principal in ResolveDispute's transaction; it cannot assert a bank
// refund, cancel a partially dispatched order or restore physical inventory.
func (r *OrderRepository) ApplyDisputeRefundEffect(ctx context.Context, disputeID string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var orderID, action, status string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(order_id,''),COALESCE(resolution_action,''),status FROM disputes WHERE id=$1`, disputeID).Scan(&orderID, &action, &status)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(action, "refund") || orderID == "" {
		return false, nil
	}
	if err = lockOrderFinanceTx(ctx, tx, orderID, true); err != nil {
		return false, err
	}
	var stable bool
	if err = tx.QueryRowContext(ctx, `SELECT order_id=$2 AND resolution_action=$3 AND status='resolved' FROM disputes WHERE id=$1 FOR UPDATE`, disputeID, orderID, action).Scan(&stable); err != nil {
		return false, err
	}
	if !stable {
		return false, ErrRefundPrincipalUnresolved
	}
	var modern bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_refund_obligations WHERE order_id=$1 AND purpose='dispute' AND business_id=$2)`, orderID, disputeID).Scan(&modern); err != nil {
		return false, err
	}
	if !modern {
		// Preserve uncertain old decisions as review holds, never new cash payouts.
		sources, err := refundRecordedSourcesTx(ctx, tx, orderID)
		if err != nil {
			return false, err
		}
		if err = reserveLegacyRefundEvidenceTx(ctx, tx, orderID, "system:dispute-review", sources); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// ---- Customer returns engine (D36) ----
// The customer_returns / return_items tables exist via migrations
// 202610160006, 202610190005 (cancelled status) and 202610250201 /
// 202610250202 (warehouse, prepaid, return shipping). No Go handler
// or repository existed for them in source; this engine is the live
// behaviour reconstructed from the audit evidence: GET /api/v1/returns
// answers 401 (not 404) in live, live has 4 customer_returns rows all
// linked to delivered_confirmed orders (1 requested, 1 refunded, 2
// cancelled), and the migration comment names the flow "7-day window,
// admin approve/reject, warehouse QC receive, wallet refund via
// balanced ledger". This is NOT the supplier-to-warehouse return
// system (supplier_return_requests / returns_operations_repo.go),
// which already exists in source and is untouched here.

type CustomerReturnItemRequest struct {
	OrderItemID string
	Qty         int
}

// CustomerReturnOptions carries the live return-form fields that sit
// on the return itself, not on its lines. WarehouseID is retained for
// request-shape compatibility ONLY: since the Oct 28, 2026 owner rule
// the return destination is DERIVED server-side from the order's
// shipment origins (see deriveReturnDestination) and a client-sent
// warehouse is never taken as authority. Prepaid is whether the
// customer prepaid the return postage.
type CustomerReturnOptions struct {
	WarehouseID string
	Prepaid     bool
}

type CustomerReturnItemQC struct {
	ReturnItemID string
	QCStatus     string // 'ok' | 'defective'
}

type CustomerReturnItem struct {
	ID          string     `json:"id"`
	ReturnID    string     `json:"returnId"`
	OrderItemID string     `json:"orderItemId"`
	VariantID   *string    `json:"variantId,omitempty"`
	Qty         int        `json:"qty"`
	QCStatus    string     `json:"qcStatus"`
	ReceivedAt  *time.Time `json:"receivedAt,omitempty"`
}

type CustomerReturn struct {
	ID                             string               `json:"id"`
	OrderID                        string               `json:"orderId"`
	CustomerID                     string               `json:"customerId"`
	Status                         string               `json:"status"`
	Reason                         string               `json:"reason"`
	AdminNote                      string               `json:"adminNote"`
	WarehouseID                    string               `json:"warehouseId,omitempty"`
	DestinationSupplierWarehouseID string               `json:"destinationSupplierWarehouseId,omitempty"`
	DestinationName                string               `json:"destinationName,omitempty"`
	DestinationCity                string               `json:"destinationCity,omitempty"`
	DestinationType                string               `json:"destinationType"`
	Prepaid                        bool                 `json:"prepaid"`
	TrackingNumber                 string               `json:"trackingNumber,omitempty"`
	ShippedAt                      *time.Time           `json:"shippedAt,omitempty"`
	ReturnCode                     *string              `json:"returnCode,omitempty"`
	ReturnShippingAmount           *int64               `json:"returnShippingAmount,omitempty"`
	ReturnShippingBearer           *string              `json:"returnShippingBearer,omitempty"`
	CreatedAt                      time.Time            `json:"createdAt"`
	UpdatedAt                      time.Time            `json:"updatedAt"`
	Items                          []CustomerReturnItem `json:"items,omitempty"`
}

const customerReturnSelectColumns = `
	id, order_id, customer_id, status, reason, admin_note,
	COALESCE(warehouse_id, ''), COALESCE(prepaid, FALSE), COALESCE(tracking_number, ''),
	shipped_at, return_code, return_shipping_amount, return_shipping_bearer,
	created_at, updated_at,
	COALESCE(destination_supplier_warehouse_id, '')`

func customerReturnScanDest(cr *CustomerReturn) []interface{} {
	return []interface{}{
		&cr.ID, &cr.OrderID, &cr.CustomerID, &cr.Status, &cr.Reason, &cr.AdminNote,
		&cr.WarehouseID, &cr.Prepaid, &cr.TrackingNumber,
		&cr.ShippedAt, &cr.ReturnCode, &cr.ReturnShippingAmount, &cr.ReturnShippingBearer,
		&cr.CreatedAt, &cr.UpdatedAt, &cr.DestinationSupplierWarehouseID,
	}
}

// fillReturnDestinationNames resolves DestinationType and
// DestinationName for already-scanned returns: a recorded destination
// supplier warehouse makes the destination that supplier's warehouse
// (type 'supplier'); anything else is a central destination whose
// name comes from the warehouses table when one is recorded. Name
// lookups are batched per table, so a list read costs at most two
// extra queries regardless of its size.
func (r *OrderRepository) fillReturnDestinationNames(ctx context.Context, crs []*CustomerReturn) error {
	centralIDs := map[string]bool{}
	supplierIDs := map[string]bool{}
	for _, cr := range crs {
		if cr.DestinationSupplierWarehouseID != "" {
			cr.DestinationType = "supplier"
			supplierIDs[cr.DestinationSupplierWarehouseID] = true
		} else {
			cr.DestinationType = "central"
			if cr.WarehouseID != "" {
				centralIDs[cr.WarehouseID] = true
			}
		}
	}
	centralNames, err := loadWarehouseDisplayByID(ctx, r.db, "warehouses", centralIDs)
	if err != nil {
		return err
	}
	supplierNames, err := loadWarehouseDisplayByID(ctx, r.db, "supplier_warehouses", supplierIDs)
	if err != nil {
		return err
	}
	for _, cr := range crs {
		if cr.DestinationType == "supplier" {
			d := supplierNames[cr.DestinationSupplierWarehouseID]
			cr.DestinationName, cr.DestinationCity = d.name, d.city
		} else {
			d := centralNames[cr.WarehouseID]
			cr.DestinationName, cr.DestinationCity = d.name, d.city
		}
	}
	return nil
}

type warehouseDisplay struct{ name, city string }

// loadWarehouseDisplayByID maps warehouse ids to display name/city
// for one of the two warehouse tables. table is only ever one of the
// two literals passed by fillReturnDestinationNames — never request
// data. Both warehouse tables carry name + city_name columns.
func loadWarehouseDisplayByID(ctx context.Context, db *sql.DB, table string, ids map[string]bool) (map[string]warehouseDisplay, error) {
	out := map[string]warehouseDisplay{}
	if len(ids) == 0 {
		return out, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := db.QueryContext(ctx, "SELECT id, name, city_name FROM "+table+" WHERE id = ANY($1::text[])", pq.Array(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var d warehouseDisplay
		if err := rows.Scan(&id, &d.name, &d.city); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

func (r *OrderRepository) loadReturnItems(ctx context.Context, returnID string) ([]CustomerReturnItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, return_id, order_item_id, variant_id, qty, qc_status, received_at
		FROM return_items WHERE return_id = $1 ORDER BY id ASC
	`, returnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []CustomerReturnItem
	for rows.Next() {
		var it CustomerReturnItem
		if err := rows.Scan(&it.ID, &it.ReturnID, &it.OrderItemID, &it.VariantID, &it.Qty, &it.QCStatus, &it.ReceivedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *OrderRepository) scanCustomerReturn(ctx context.Context, row *sql.Row) (*CustomerReturn, error) {
	var cr CustomerReturn
	if err := row.Scan(customerReturnScanDest(&cr)...); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	items, err := r.loadReturnItems(ctx, cr.ID)
	if err != nil {
		return nil, err
	}
	cr.Items = items
	if err := r.fillReturnDestinationNames(ctx, []*CustomerReturn{&cr}); err != nil {
		return nil, err
	}
	return &cr, nil
}

func (r *OrderRepository) resolveOrderByIDOrNumber(ctx context.Context, idOrNumber string) (*domain.Order, error) {
	order, err := r.GetOrderByID(ctx, idOrNumber)
	if err != nil {
		return nil, err
	}
	if order != nil {
		return order, nil
	}
	return r.GetOrderByNumber(ctx, idOrNumber)
}

// ---- Return destination derivation (owner rule, Oct 28 2026) ----
//
// A customer return goes back to where the order shipped FROM: the
// supplier's warehouse when the parcel shipped from there, the
// central warehouse otherwise. The destination is never taken from
// the client (the return form's old free-text warehouse input is
// ignored as authority); it is derived from the persisted allocation
// in order_item_origins (migrations 202610240001 + 202610280001),
// with the product-level fallback set for legacy orders that predate
// origin rows — the same fallbacks handleOrderReturnQuote prices from
// (products.fulfillment_method / origin_supplier_warehouse_id /
// warehouse_name).

// ReturnDestinationInfo is the derived destination as surfaced to the
// customer: the return-quote payload carries it for the selected item
// set, and the created return confirms it.
type ReturnDestinationInfo struct {
	Type                string `json:"type"` // 'central' | 'supplier'
	WarehouseID         string `json:"warehouseId,omitempty"`
	SupplierWarehouseID string `json:"supplierWarehouseId,omitempty"`
	Name                string `json:"name"`
	City                string `json:"city,omitempty"`
	Province            string `json:"province,omitempty"`
	Address             string `json:"address,omitempty"`
}

// returnDestination is the internal, comparable form of a derived
// destination: exactly one of the two ids is set (an empty central id
// means a central destination with no specific warehouse recorded).
type returnDestination struct {
	warehouseID         string
	supplierWarehouseID string
}

func (d returnDestination) key() string {
	if d.supplierWarehouseID != "" {
		return "supplier:" + d.supplierWarehouseID
	}
	return "central:" + d.warehouseID
}

// returnDestQuerier is the subset of *sql.DB / *sql.Tx the derivation
// reads through, so creation can derive inside its own transaction
// while the quote path derives on the plain handle.
type returnDestQuerier interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// centralConsolidationWarehouseID is the consolidation point mixed or
// unroutable returns are sent to: the highest-priority active central
// warehouse (the ListCentralWarehouses ordering), "" when the
// platform has no active central warehouse yet.
func centralConsolidationWarehouseID(ctx context.Context, q returnDestQuerier) (string, error) {
	var id string
	err := q.QueryRowContext(ctx, `
		SELECT id FROM warehouses WHERE is_active = TRUE ORDER BY priority ASC, id ASC LIMIT 1
	`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// supplierReturnDestination resolves where a supplier-shipped item
// goes back to: the product's recorded origin warehouse, else the
// supplier's single active warehouse. Anything else (no warehouse at
// all, or several with none recorded for the product) is genuinely
// ambiguous: ambiguous=true and the caller routes the return to
// central consolidation instead of guessing a supplier address.
func supplierReturnDestination(ctx context.Context, q returnDestQuerier, supplierID, productOriginWarehouseID string) (returnDestination, bool, error) {
	if productOriginWarehouseID != "" {
		return returnDestination{supplierWarehouseID: productOriginWarehouseID}, false, nil
	}
	if supplierID == "" {
		return returnDestination{}, true, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id FROM supplier_warehouses WHERE supplier_id = $1 AND is_active = TRUE ORDER BY created_at ASC
	`, supplierID)
	if err != nil {
		return returnDestination{}, false, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return returnDestination{}, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return returnDestination{}, false, err
	}
	if len(ids) == 1 {
		return returnDestination{supplierWarehouseID: ids[0]}, false, nil
	}
	return returnDestination{}, true, nil
}

type returnItemProductInfo struct {
	supplierID        string
	fulfillmentMethod string
	originWarehouseID string // products.origin_supplier_warehouse_id
	warehouseName     string // products.warehouse_name
}

// productFallbackDestination resolves an item with no usable origin
// row (legacy order, or a 'legacy' origin) from the product itself.
func productFallbackDestination(ctx context.Context, q returnDestQuerier, info returnItemProductInfo) (returnDestination, bool, error) {
	if info.fulfillmentMethod == "supplier_direct" {
		return supplierReturnDestination(ctx, q, info.supplierID, info.originWarehouseID)
	}
	if info.warehouseName != "" {
		var id string
		err := q.QueryRowContext(ctx, `
			SELECT id FROM warehouses WHERE name = $1 ORDER BY is_active DESC, priority ASC, id ASC LIMIT 1
		`, info.warehouseName).Scan(&id)
		if err != nil && err != sql.ErrNoRows {
			return returnDestination{}, false, err
		}
		if err == nil {
			return returnDestination{warehouseID: id}, false, nil
		}
	}
	// Legacy warehouse-routed stock was one global pool («انبار مرکزی»):
	// a central destination with no specific warehouse recorded.
	return returnDestination{}, false, nil
}

// deriveReturnDestination resolves the single destination for a set
// of returned order items. Every origin row of every returned item
// contributes a destination; exactly one distinct, unambiguous
// destination wins. Several distinct destinations — or a supplier
// destination that cannot be pinned down — route the return to the
// central consolidation warehouse, with the per-item origins
// preserved on order_item_origins for the warehouse/supplier views.
// The second return value reports the genuinely-ambiguous supplier
// case, which creation records in the return's admin_note.
func (r *OrderRepository) deriveReturnDestination(ctx context.Context, q returnDestQuerier, orderID string, orderItemIDs []string) (returnDestination, bool, error) {
	dests := map[string]returnDestination{}
	ambiguous := false
	add := func(d returnDestination) { dests[d.key()] = d }
	for _, itemID := range orderItemIDs {
		var info returnItemProductInfo
		err := q.QueryRowContext(ctx, `
			SELECT COALESCE(p.supplier_id, ''), COALESCE(p.fulfillment_method, 'warehouse'),
			       COALESCE(p.origin_supplier_warehouse_id, ''), COALESCE(p.warehouse_name, '')
			FROM order_items oi JOIN products p ON p.id = oi.product_id
			WHERE oi.id::text = $1 AND oi.order_id = $2
		`, itemID, orderID).Scan(&info.supplierID, &info.fulfillmentMethod, &info.originWarehouseID, &info.warehouseName)
		if err == sql.ErrNoRows {
			// Unknown item: CreateCustomerReturn's own item loop
			// rejects it right after; derivation stays total and
			// treats it as an unrecorded central item.
			add(returnDestination{})
			continue
		}
		if err != nil {
			return returnDestination{}, false, err
		}
		rows, err := q.QueryContext(ctx, `
			SELECT origin_type, COALESCE(warehouse_id, ''), COALESCE(supplier_warehouse_id, ''), COALESCE(supplier_id, '')
			FROM order_item_origins WHERE order_id = $1 AND order_item_id::text = $2 ORDER BY leg_index ASC
		`, orderID, itemID)
		if err != nil {
			return returnDestination{}, false, err
		}
		type originRow struct{ originType, warehouseID, supplierWarehouseID, supplierID string }
		var origins []originRow
		for rows.Next() {
			var o originRow
			if err := rows.Scan(&o.originType, &o.warehouseID, &o.supplierWarehouseID, &o.supplierID); err != nil {
				rows.Close()
				return returnDestination{}, false, err
			}
			origins = append(origins, o)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return returnDestination{}, false, err
		}
		rows.Close()
		if len(origins) == 0 {
			d, amb, err := productFallbackDestination(ctx, q, info)
			if err != nil {
				return returnDestination{}, false, err
			}
			ambiguous = ambiguous || amb
			add(d)
			continue
		}
		for _, o := range origins {
			switch {
			case o.supplierWarehouseID != "":
				add(returnDestination{supplierWarehouseID: o.supplierWarehouseID})
			case o.warehouseID != "":
				add(returnDestination{warehouseID: o.warehouseID})
			case o.originType == "supplier":
				supplierID := o.supplierID
				if supplierID == "" {
					supplierID = info.supplierID
				}
				d, amb, err := supplierReturnDestination(ctx, q, supplierID, info.originWarehouseID)
				if err != nil {
					return returnDestination{}, false, err
				}
				ambiguous = ambiguous || amb
				add(d)
			default:
				d, amb, err := productFallbackDestination(ctx, q, info)
				if err != nil {
					return returnDestination{}, false, err
				}
				ambiguous = ambiguous || amb
				add(d)
			}
		}
	}
	if len(dests) == 1 && !ambiguous {
		for _, d := range dests {
			return d, false, nil
		}
	}
	consolidation, err := centralConsolidationWarehouseID(ctx, q)
	if err != nil {
		return returnDestination{}, false, err
	}
	return returnDestination{warehouseID: consolidation}, ambiguous, nil
}

// loadReturnDestinationInfo decorates a derived destination with the
// warehouse's display name/city (and address for supplier warehouses,
// whose table carries one; central warehouses have no address
// column). A warehouse row deleted after derivation degrades to an
// unnamed destination of the right type rather than an error.
func (r *OrderRepository) loadReturnDestinationInfo(ctx context.Context, q returnDestQuerier, dest returnDestination) (*ReturnDestinationInfo, error) {
	info := &ReturnDestinationInfo{Type: "central", WarehouseID: dest.warehouseID, SupplierWarehouseID: dest.supplierWarehouseID}
	if dest.supplierWarehouseID != "" {
		info.Type = "supplier"
		err := q.QueryRowContext(ctx, `
			SELECT name, city_name, province_name, COALESCE(address, '')
			FROM supplier_warehouses WHERE id = $1
		`, dest.supplierWarehouseID).Scan(&info.Name, &info.City, &info.Province, &info.Address)
		if err == sql.ErrNoRows {
			return info, nil
		}
		return info, err
	}
	if dest.warehouseID != "" {
		err := q.QueryRowContext(ctx, `
			SELECT name, city_name, province_name FROM warehouses WHERE id = $1
		`, dest.warehouseID).Scan(&info.Name, &info.City, &info.Province)
		if err == sql.ErrNoRows {
			return info, nil
		}
		return info, err
	}
	return info, nil
}

// DeriveReturnDestination is the read-side entry point (return
// quote): it derives the destination for the given returned-item set
// without persisting anything.
func (r *OrderRepository) DeriveReturnDestination(ctx context.Context, orderID string, orderItemIDs []string) (*ReturnDestinationInfo, error) {
	dest, _, err := r.deriveReturnDestination(ctx, r.db, orderID, orderItemIDs)
	if err != nil {
		return nil, err
	}
	return r.loadReturnDestinationInfo(ctx, r.db, dest)
}

// CreateCustomerReturn registers a customer return for a
// delivered_confirmed order. Filing after a recorded return window
// closes is rejected; a NULL window does not reject (TODO U19 — the
// window origin and the NULL-window rule are unverified).
// TODO(U25): the filing-rejection rule itself is unproven in live
// (no live return_7day dispute exists); if live proves late filings
// are accepted, remove the ErrReturnWindowClosed rejection, not the
// window read itself.
func (r *OrderRepository) CreateCustomerReturn(ctx context.Context, customerID, orderIDOrNumber, reason string, items []CustomerReturnItemRequest, opts CustomerReturnOptions) (*CustomerReturn, error) {
	order, err := r.resolveOrderByIDOrNumber(ctx, orderIDOrNumber)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, sql.ErrNoRows
	}
	if strings.TrimSpace(customerID) != "" {
		if order.CustomerUserID == nil || *order.CustomerUserID != customerID {
			return nil, sql.ErrNoRows
		}
	}
	if order.Status != "delivered_confirmed" {
		return nil, fmt.Errorf("customer returns require a delivered_confirmed order (status is %s)", order.Status)
	}
	if order.ReturnWindowEndsAt != nil && time.Now().After(*order.ReturnWindowEndsAt) {
		return nil, ErrReturnWindowClosed
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one return item is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize concurrent filings for the same order (supervisor
	// LOW): the over-return guard below sums quantities across the
	// order's live returns, and two filings racing in parallel
	// transactions could each pass it against the same snapshot.
	// Locking the parent order row for the filing's duration makes
	// the check atomic per order.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM orders WHERE id = $1 FOR UPDATE`, order.ID); err != nil {
		return nil, err
	}
	returnID := uuid.NewString()
	now := time.Now()
	// Destination (owner rule, Oct 28 2026): derived from the returned
	// items' shipment origins — never from the client. opts.WarehouseID
	// stays in the request shape for compatibility and is ignored
	// here, so a contradicting or free-text value can neither reroute
	// the parcel nor fail the filing.
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.OrderItemID)
	}
	dest, ambiguous, err := r.deriveReturnDestination(ctx, tx, order.ID, itemIDs)
	if err != nil {
		return nil, err
	}
	warehouseArg := sql.NullString{}
	if dest.warehouseID != "" {
		warehouseArg = sql.NullString{String: dest.warehouseID, Valid: true}
	}
	supplierWarehouseArg := sql.NullString{}
	if dest.supplierWarehouseID != "" {
		supplierWarehouseArg = sql.NullString{String: dest.supplierWarehouseID, Valid: true}
	}
	adminNote := ""
	if ambiguous {
		adminNote = "مقصد مرجوعی: انبار تأمین‌کننده از سوابق ارسال مشخص نشد؛ انبار مرکزی به‌عنوان مقصد تجمیع تعیین شد."
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO customer_returns (id, order_id, customer_id, status, reason, admin_note, warehouse_id, destination_supplier_warehouse_id, prepaid, created_at, updated_at)
		VALUES ($1, $2, $3::uuid, 'requested', $4, $5, $6, $7, $8, $9, $9)
	`, returnID, order.ID, customerID, reason, adminNote, warehouseArg, supplierWarehouseArg, opts.Prepaid, now); err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Qty <= 0 {
			return nil, fmt.Errorf("return quantity must be greater than zero")
		}
		var orderedQty int
		var variantID sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT quantity, variant_id FROM order_items WHERE id = $1 AND order_id = $2`, item.OrderItemID, order.ID).Scan(&orderedQty, &variantID); err != nil {
			return nil, err
		}
		if item.Qty > orderedQty {
			return nil, fmt.Errorf("return quantity exceeds ordered quantity for item %s", item.OrderItemID)
		}
		// Over-return guard (GAP 6): quantities already claimed by
		// other live returns of this order item count against the
		// ordered quantity. The per-filing check above alone lets
		// two filings each claim the full quantity, doubling the
		// refund, the supplier cost clawback and the restock. Rows
		// inserted earlier in THIS filing are visible inside the
		// transaction and count too.
		var alreadyReturned int
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(ri.qty), 0)
			FROM return_items ri
			JOIN customer_returns cr ON cr.id = ri.return_id
			WHERE ri.order_item_id = $1 AND cr.status NOT IN ('rejected', 'cancelled')
		`, item.OrderItemID).Scan(&alreadyReturned); err != nil {
			return nil, err
		}
		if alreadyReturned+item.Qty > orderedQty {
			return nil, fmt.Errorf("return quantity exceeds ordered quantity for item %s", item.OrderItemID)
		}
		var variantArg interface{}
		if variantID.Valid {
			variantArg = variantID.String
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO return_items (id, return_id, order_item_id, variant_id, qty, qc_status)
			VALUES ($1, $2, $3, $4, $5, 'pending')
		`, uuid.NewString(), returnID, item.OrderItemID, variantArg, item.Qty); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// P1-4 (best-effort): return filed - warehouse staff + admins.
	notifyReturnRequested(ctx, r.db, returnID, order.OrderNumber, reason)
	return r.GetCustomerReturn(ctx, returnID)
}

func (r *OrderRepository) GetCustomerReturn(ctx context.Context, id string) (*CustomerReturn, error) {
	row := r.db.QueryRowContext(ctx, "SELECT "+customerReturnSelectColumns+" FROM customer_returns WHERE id = $1", id)
	return r.scanCustomerReturn(ctx, row)
}

func (r *OrderRepository) listCustomerReturnsQuery(ctx context.Context, query string, args ...interface{}) ([]CustomerReturn, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomerReturn
	for rows.Next() {
		var cr CustomerReturn
		if err := rows.Scan(customerReturnScanDest(&cr)...); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := r.loadReturnItems(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	ptrs := make([]*CustomerReturn, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	if err := r.fillReturnDestinationNames(ctx, ptrs); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCustomerReturns lists a customer's own returns; an empty
// customerID lists all returns (admin use only — enforced by the
// handler, not by this query).
func (r *OrderRepository) ListCustomerReturns(ctx context.Context, customerID string) ([]CustomerReturn, error) {
	if strings.TrimSpace(customerID) == "" {
		return r.listCustomerReturnsQuery(ctx, "SELECT "+customerReturnSelectColumns+" FROM customer_returns ORDER BY created_at DESC")
	}
	return r.listCustomerReturnsQuery(ctx, "SELECT "+customerReturnSelectColumns+" FROM customer_returns WHERE customer_id = $1::uuid ORDER BY created_at DESC", customerID)
}

// ListCustomerReturnsByStore lists returns for orders of one store
// (D10: stores/:slug/returns).
func (r *OrderRepository) ListCustomerReturnsByStore(ctx context.Context, storeID string) ([]CustomerReturn, error) {
	return r.listCustomerReturnsQuery(ctx, `
		SELECT cr.id, cr.order_id, cr.customer_id, cr.status, cr.reason, cr.admin_note,
		       COALESCE(cr.warehouse_id, ''), COALESCE(cr.prepaid, FALSE), COALESCE(cr.tracking_number, ''),
		       cr.shipped_at, cr.return_code, cr.return_shipping_amount, cr.return_shipping_bearer,
		       cr.created_at, cr.updated_at,
		       COALESCE(cr.destination_supplier_warehouse_id, '')
		FROM customer_returns cr
		JOIN orders o ON o.id = cr.order_id
		WHERE o.store_id = $1
		ORDER BY cr.created_at DESC
	`, storeID)
}

// ListCustomerReturnsForWarehouse lists returns on their way back to
// (or at) the warehouse: approved, received, or refunded with proven unshelved
// units. A financial refund must not hide outstanding physical work. This is the engine
// behind D10 warehouses/returns; Team 01's /warehouse/return-backs
// stub route is left untouched and is not re-registered here.
//
// The optional destinationID narrows the queue to one destination —
// a central warehouse id or a supplier warehouse id — so each
// warehouse's queue is its own (supplier-destined parcels surface
// under their supplier warehouse id). Omitted or "" keeps the
// unfiltered queue, the pre-filter behaviour.
func (r *OrderRepository) ListCustomerReturnsForWarehouse(ctx context.Context, destinationID ...string) ([]CustomerReturn, error) {
	dest := ""
	if len(destinationID) > 0 {
		dest = destinationID[0]
	}
	return r.listCustomerReturnsQuery(ctx, `
		SELECT `+customerReturnSelectColumns+`
		FROM customer_returns
		WHERE (status IN ('approved', 'received') OR (status='refunded' AND EXISTS(SELECT 1 FROM warehouse_customer_return_bin_sources s WHERE s.return_id=customer_returns.id AND s.source_proven AND s.available_quantity>0)))
		  AND ($1 = '' OR warehouse_id = $1 OR destination_supplier_warehouse_id = $1)
		ORDER BY created_at DESC
	`, dest)
}

func (r *OrderRepository) transitionCustomerReturn(ctx context.Context, id, fromStatus, toStatus, adminNote string) error {
	var q string
	var args []interface{}
	if adminNote != "" {
		q = `UPDATE customer_returns SET status = $1, admin_note = $2, updated_at = NOW() WHERE id = $3 AND status = $4`
		args = []interface{}{toStatus, adminNote, id, fromStatus}
	} else {
		q = `UPDATE customer_returns SET status = $1, updated_at = NOW() WHERE id = $2 AND status = $3`
		args = []interface{}{toStatus, id, fromStatus}
	}
	res, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *OrderRepository) ApproveCustomerReturn(ctx context.Context, id, adminNote string) error {
	cr, _ := r.GetCustomerReturn(ctx, id)
	if err := r.transitionCustomerReturn(ctx, id, "requested", "approved", adminNote); err != nil {
		return err
	}
	// P0-11 (best-effort): return approved - customer.
	if cr != nil {
		orderNum := cr.OrderID
		if o, oerr := loadOrderNotifyCtx(ctx, r.db, cr.OrderID); oerr == nil {
			orderNum = o.OrderNumber
		}
		notifyReturnDecision(ctx, r.db, id, orderNum, cr.CustomerID, "approved", adminNote)
	}
	return nil
}

func (r *OrderRepository) RejectCustomerReturn(ctx context.Context, id, adminNote string) error {
	cr, _ := r.GetCustomerReturn(ctx, id)
	if err := r.transitionCustomerReturn(ctx, id, "requested", "rejected", adminNote); err != nil {
		return err
	}
	// P0-11 (best-effort): return rejected - customer, incl. the
	// admin_note reason.
	if cr != nil {
		orderNum := cr.OrderID
		if o, oerr := loadOrderNotifyCtx(ctx, r.db, cr.OrderID); oerr == nil {
			orderNum = o.OrderNumber
		}
		notifyReturnDecision(ctx, r.db, id, orderNum, cr.CustomerID, "rejected", adminNote)
	}
	return nil
}

func (r *OrderRepository) CancelCustomerReturn(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE customer_returns SET status = 'cancelled', updated_at = NOW() WHERE id = $1 AND status IN ('requested', 'approved')`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ShipCustomerReturn marks an approved return as shipped by the
// customer (FIX 14, live's ship action). Tracking number is optional.
// FIX 14b: prepaid is *bool so an omitted flag leaves the stored value
// untouched, while a live-shaped request carrying prepaid persists it.
func (r *OrderRepository) ShipCustomerReturn(ctx context.Context, id, trackingNumber string, prepaid *bool) error {
	var res sql.Result
	var err error
	if prepaid == nil {
		res, err = r.db.ExecContext(ctx, `UPDATE customer_returns SET status = 'shipped', shipped_at = NOW(), tracking_number = NULLIF($2, ''), updated_at = NOW() WHERE id = $1 AND status = 'approved'`, id, trackingNumber)
	} else {
		res, err = r.db.ExecContext(ctx, `UPDATE customer_returns SET status = 'shipped', shipped_at = NOW(), tracking_number = NULLIF($2, ''), prepaid = $3, updated_at = NOW() WHERE id = $1 AND status = 'approved'`, id, trackingNumber, *prepaid)
	}
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	// P1-4 (best-effort): customer shipped the return - destination
	// (supplier warehouse owner, else warehouse staff), with tracking.
	if cr, cerr := r.GetCustomerReturn(ctx, id); cerr == nil && cr != nil {
		orderNum := cr.OrderID
		if o, oerr := loadOrderNotifyCtx(ctx, r.db, cr.OrderID); oerr == nil {
			orderNum = o.OrderNumber
		}
		notifyReturnShipped(ctx, r.db, id, orderNum, trackingNumber, cr.DestinationSupplierWarehouseID)
	}
	return nil
}

// ReceiveCustomerReturn is the warehouse QC step: approved|shipped ->
// received, with a per-item QC verdict, plus stage 1 of the
// return-shipping settlement (migration 202610250202, owner policy
// Oct 25 2026 — see return_shipping_settlement.go): in the same
// transaction the return parcel is priced with the outbound shipping
// engine, every received item's share of the fare is fixed by its QC
// verdict, supplier-borne shares are charged (charge row + platform
// ledger debit, feeding SupplierBalance.ReturnCharges), a return_code
// is generated when missing, and return_shipping_amount /
// return_shipping_bearer are recorded on the return. The customer
// side of the split moves at refund time (RefundCustomerReturn).
//
// A post-paid (COD) parcel is refused before any QC is recorded: the
// cod_refused flag is committed and ErrReturnCODRefused is returned —
// no receipt, no charges, no wallet movement, and the return stays
// visible in the warehouse list.
func (r *OrderRepository) ReceiveCustomerReturn(ctx context.Context, id string, qc []CustomerReturnItemQC, actorUserID string) (*ReturnShippingSettlement, error) {
	if len(qc) == 0 {
		return nil, fmt.Errorf("at least one QC verdict is required")
	}
	for _, item := range qc {
		if item.QCStatus != "ok" && item.QCStatus != "defective" {
			return nil, fmt.Errorf("unsupported QC verdict %q", item.QCStatus)
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM customer_returns WHERE id=$1`, id).Scan(&financeOrderID); err != nil {
		return nil, err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID); err != nil {
		return nil, err
	}
	var status string
	var orderID string
	var prepaid bool
	var codRefused bool
	var returnCode sql.NullString
	var recordedShippingAmount sql.NullInt64
	var recordedShippingBearer sql.NullString
	var shippingSettledAt sql.NullTime
	var destWarehouseID, destSupplierWarehouseID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT status, order_id, COALESCE(prepaid, FALSE), COALESCE(cod_refused, FALSE), return_code,
			return_shipping_amount, return_shipping_bearer, return_shipping_settled_at,
			warehouse_id, destination_supplier_warehouse_id
		FROM customer_returns WHERE id = $1 FOR UPDATE
	`, id).Scan(&status, &orderID, &prepaid, &codRefused, &returnCode, &recordedShippingAmount, &recordedShippingBearer, &shippingSettledAt, &destWarehouseID, &destSupplierWarehouseID); err != nil {
		return nil, err
	}
	// FIX 14: a shipped return is receivable too (ship precedes
	// receive in the live flow); the approved-only shortcut is kept.
	if status != "approved" && status != "shipped" {
		return nil, sql.ErrNoRows
	}
	if !prepaid {
		// Critic fix (Phase 2): guard on cod_refused = FALSE — a retry
		// after a refusal must NOT re-notify. The row is FOR UPDATE
		// locked above, so concurrent retries serialize on this flag.
		res, err := tx.ExecContext(ctx, `UPDATE customer_returns SET cod_refused = TRUE, updated_at = NOW() WHERE id = $1 AND cod_refused = FALSE`, id)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Already refused (and already notified) — same error, no
			// second notification row.
			return nil, ErrReturnCODRefused
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		// P0-12 (best-effort): COD parcel refused - customer.
		if o, oerr := loadOrderNotifyCtx(ctx, r.db, orderID); oerr == nil {
			notifyReturnCODRefused(ctx, r.db, id, o.OrderNumber, o.CustomerID)
		}
		return nil, ErrReturnCODRefused
	}
	// Completeness (critic fix F2): the verdict list must cover
	// EVERY item of the return. A partial list previously flipped
	// the return to 'received' anyway, and the refund + money
	// reversals then paid over items that were never QC'd.
	verdicts := make(map[string]bool, len(qc))
	for _, item := range qc {
		verdicts[item.ReturnItemID] = true
	}
	itemRows, err := tx.QueryContext(ctx, `SELECT id::text FROM return_items WHERE return_id = $1`, id)
	if err != nil {
		return nil, err
	}
	var uncovered int
	for itemRows.Next() {
		var rid string
		if err := itemRows.Scan(&rid); err != nil {
			itemRows.Close()
			return nil, err
		}
		if !verdicts[rid] {
			uncovered++
		}
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		return nil, err
	}
	itemRows.Close()
	if uncovered > 0 {
		return nil, ErrReturnIncompleteQC
	}
	for _, item := range qc {
		res, err := tx.ExecContext(ctx, `UPDATE return_items SET qc_status = $1, received_at = clock_timestamp() WHERE id = $2 AND return_id = $3`, item.QCStatus, item.ReturnItemID, id)
		if err != nil {
			return nil, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return nil, sql.ErrNoRows
		}
	}

	// Restock (GAP 6): the goods are physically back and their
	// verdicts are recorded, so stock truth moves NOW, at receive —
	// not at refund, which may lag or never be pressed.
	storageSourcesAvailable, err := r.restockReceivedReturnTx(ctx, tx, id, orderID, destWarehouseID, destSupplierWarehouseID, actorUserID)
	if err != nil {
		return nil, err
	}

	settlement := &ReturnShippingSettlement{}
	if recordedShippingAmount.Valid || shippingSettledAt.Valid {
		// Belt-and-braces idempotency: a settlement was already
		// recorded for this return. The status guard above is the
		// primary replay block (backed by the charges' UNIQUE
		// idempotency_key); never charge twice — report the stored
		// settlement, whose totals are the persisted charge rows.
		settlement.ReturnShippingAmount = recordedShippingAmount.Int64
		settlement.ReturnShippingBearer = recordedShippingBearer.String
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0) FROM customer_return_shipping_charges WHERE return_id = $1 AND bearer = 'supplier'
		`, id).Scan(&settlement.SupplierBorneTotal); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0) FROM customer_return_shipping_charges WHERE return_id = $1 AND bearer = 'customer'
		`, id).Scan(&settlement.CustomerBorneTotal); err != nil {
			return nil, err
		}
	} else {
		rates, err := shippingquote.LoadRates(ctx, tx)
		if err != nil {
			return nil, err
		}
		shares, totalFare, err := computeReturnShippingShares(ctx, tx, id, rates)
		if err != nil {
			return nil, err
		}
		settlement.ReturnShippingAmount = totalFare
		settlement.ReturnShippingBearer = returnShippingBearerOf(shares)
		for _, share := range shares {
			if share.Bearer == "supplier" {
				settlement.SupplierBorneTotal += share.Amount
				// A defective item whose product has no supplier
				// cannot be charged (the charges table keys on
				// supplier_id); its share stays platform-borne and
				// produces no row.
				if share.Amount > 0 && share.SupplierID != "" {
					if err := insertSupplierReturnShippingCharge(ctx, tx, id, share, actorUserID); err != nil {
						return nil, err
					}
				}
			} else {
				settlement.CustomerBorneTotal += share.Amount
				// Fix #2: the customer's share is persisted NOW,
				// at the receive-time amount, as a bearer='customer'
				// charge row. No money moves yet (collection happens
				// at refund from the wallet); the refund stage
				// consumes this row and rewrites it to the amount
				// actually debited, so a tariff edit between receive
				// and refund can never re-price the return.
				if share.Amount > 0 {
					if err := insertCustomerReturnShippingCharge(ctx, tx, id, share, actorUserID); err != nil {
						return nil, err
					}
				}
			}
		}
		if !returnCode.Valid {
			var seq int64
			if err := tx.QueryRowContext(ctx, `SELECT nextval('customer_return_code_seq')`).Scan(&seq); err != nil {
				return nil, err
			}
			code := fmt.Sprintf("RET-%d", seq)
			returnCode = sql.NullString{String: code, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE customer_returns
			SET return_shipping_amount = $2, return_shipping_bearer = $3, return_code = $4, updated_at = NOW()
			WHERE id = $1
		`, id, settlement.ReturnShippingAmount, settlement.ReturnShippingBearer, returnCode.String); err != nil {
			return nil, err
		}
	}

	res, err := tx.ExecContext(ctx, `UPDATE customer_returns SET status = 'received', updated_at = NOW() WHERE id = $1 AND status IN ('approved', 'shipped')`, id)
	if err != nil {
		return nil, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, sql.ErrNoRows
	}
	if storageSourcesAvailable && destWarehouseID.Valid && !destSupplierWarehouseID.Valid {
		if err := creditCustomerReturnStorageTx(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// P0-12 (best-effort): QC received - charged suppliers (amount +
	// return code + per-item split, from the persisted charge rows).
	if o, oerr := loadOrderNotifyCtx(ctx, r.db, orderID); oerr == nil {
		code := ""
		if cr, cerr := r.GetCustomerReturn(ctx, id); cerr == nil && cr != nil && cr.ReturnCode != nil {
			code = *cr.ReturnCode
		}
		notifyReturnQCCharged(ctx, r.db, id, o.OrderNumber, code)
	}
	return settlement, nil
}

// The result reports whether the canonical captured-inventory model can prove
// new storage sources. Historical unallocated stock remains outside fee history.
// restockReceivedReturnTx credits a just-received customer return's
// items back into inventory inside the receive transaction (GAP 6).
// QC 'ok' units become sellable again through the canonical ledger,
// credited to the holding lot custreturn:<returnID> (the returns
// precedent: supplier stock-returns credit return:<id> lots); the
// products.stock projection and the variant stock follow. QC
// 'defective' units are credited to quarantine instead: accounted,
// physically at the destination and recoverable by the supplier
// through the back-request flow, but never sellable and never in
// products.stock. The credit is NOT aimed at the origin pool:
// order_item_origins rows are almost never present in this tree and,
// when present, carry warehouse ids rather than the per-inbound
// location_key the balance rows are keyed by, so the exact origin
// balance row is unidentifiable; per-item origin data goes into the
// movement metadata only.
//
// Two guards keep the write honest: the parent order's
// inventory_captured must be set (mirrors CancelOrder — never mint
// stock that was never captured), and the whole block is a no-op on
// a database without the canonical inventory tables.
func (r *OrderRepository) restockReceivedReturnTx(ctx context.Context, tx *sql.Tx, returnID, orderID string, destWarehouseID, destSupplierWarehouseID sql.NullString, actorUserID string) (bool, error) {
	var hasInventory bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('inventory_movements') IS NOT NULL AND to_regclass('inventory_balances') IS NOT NULL`).Scan(&hasInventory); err != nil {
		return false, err
	}
	if !hasInventory {
		return false, nil
	}
	var captured bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(inventory_captured, FALSE) FROM orders WHERE id = $1`, orderID).Scan(&captured); err != nil {
		return false, err
	}
	if !captured {
		return false, nil
	}
	destination := ""
	if destSupplierWarehouseID.Valid && destSupplierWarehouseID.String != "" {
		destination = "supplier_warehouse:" + destSupplierWarehouseID.String
	} else if destWarehouseID.Valid && destWarehouseID.String != "" {
		destination = "warehouse:" + destWarehouseID.String
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT ri.id::text, ri.order_item_id, ri.qty, ri.qc_status, ri.variant_id, oi.product_id, COALESCE(p.supplier_id, ''),
			COALESCE((SELECT oio.origin_label FROM order_item_origins oio WHERE oio.order_item_id::text = ri.order_item_id LIMIT 1), '')
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE ri.return_id = $1
	`, returnID)
	if err != nil {
		return false, err
	}
	type receivedItem struct {
		id          string
		orderItemID string
		qty         int
		qcStatus    string
		variantID   sql.NullString
		productID   string
		supplierID  string
		originLabel string
	}
	var items []receivedItem
	for rows.Next() {
		var it receivedItem
		if err := rows.Scan(&it.id, &it.orderItemID, &it.qty, &it.qcStatus, &it.variantID, &it.productID, &it.supplierID, &it.originLabel); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	skuAccounting, err := hasSKUInventoryTx(ctx, tx)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		handled, err := restoreCapturedReturnTx(ctx, tx, it.id, it.orderItemID, it.productID, it.supplierID, it.qty, it.qcStatus, destination, actorUserID)
		if err != nil {
			return false, err
		}
		if handled {
			continue
		}
		toStatus, suffix := "sellable", "sellable"
		if it.qcStatus == "defective" {
			toStatus, suffix = "quarantine", "quarantine"
		}
		covered, err := canonicalCoveredTx(ctx, tx, it.productID)
		if err != nil {
			return false, err
		}
		if !covered {
			// First canonical row for a legacy product: seed its
			// existing products.stock into the ledger first, or the
			// sellable projection would silently erase it.
			if err := ensureCanonicalCoverageTx(ctx, tx, it.productID, it.supplierID); err != nil {
				return false, err
			}
		}
		metadata := fmt.Sprintf(`{"qc":%q,"destination":%q,"origin":%q}`, it.qcStatus, destination, it.originLabel)
		if err := upsertInventoryMovementTx(ctx, tx, it.productID, it.supplierID, it.qty,
			"customer_return", toStatus, "customer", "custreturn:"+returnID,
			"customer_return", returnID,
			"crstk:"+returnID+":"+it.id+":"+suffix, actorUserID, "%s", metadata); err != nil {
			return false, err
		}
		if it.qcStatus == "ok" {
			if err := syncProductStockTx(ctx, tx, it.productID); err != nil {
				return false, err
			}
			if !skuAccounting && it.variantID.Valid && it.variantID.String != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock = stock + $2 WHERE id = $1`, it.variantID.String, it.qty); err != nil {
					return false, err
				}
			}
		}
	}
	return skuAccounting, nil
}

// recordReturnReversalsTx writes the GAP 6 money reversals for a
// refunded customer return, inside the refund transaction: per
// refunded item, the supplier's product cost (order_items
// wholesale_cost snapshot) and — only when the order's store is a
// dropshipper store — the seller's margin (seller_margin snapshot).
// The platform fee deliberately gets NO event: it was the platform's
// own share of a now-unwound sale and is absorbed implicitly (an
// event would double-count against treasury platformEarned, which
// is computed from orders, not events). Events are written
// unconditionally: a party that already withdrew the money simply
// goes negative, the established pattern for return charges. The
// whole block is a no-op on a database without the events table.
func (r *OrderRepository) recordReturnReversalsTx(ctx context.Context, tx *sql.Tx, returnID, orderID, actorUserID string) error {
	var hasReturnEvents bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('customer_return_financial_events') IS NOT NULL`).Scan(&hasReturnEvents); err != nil {
		return err
	}
	if !hasReturnEvents {
		return nil
	}
	var storeID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT store_id FROM orders WHERE id = $1`, orderID).Scan(&storeID); err != nil {
		return err
	}
	storeOwnerType := ""
	if storeID.Valid && storeID.String != "" {
		// A missing store row means "not a dropshipper store" (no
		// seller margin to reverse), never a failed refund.
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(owner_type, '') FROM stores WHERE id = $1`, storeID.String).Scan(&storeOwnerType); err != nil && err != sql.ErrNoRows {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT ri.id::text, ri.qty, oi.wholesale_cost, oi.seller_margin, oi.id::text
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		WHERE ri.return_id = $1 AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
	`, returnID)
	if err != nil {
		return err
	}
	type refundedItem struct {
		id          string
		qty         int
		unitCost    int64
		unitMargin  int64
		orderItemID string
	}
	var items []refundedItem
	for rows.Next() {
		var it refundedItem
		if err := rows.Scan(&it.id, &it.qty, &it.unitCost, &it.unitMargin, &it.orderItemID); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, it := range items {
		supplierID := ""
		if it.unitCost > 0 {
			var err error
			supplierID, err = immutableOrderItemSupplierTx(ctx, tx, it.orderItemID)
			if err != nil {
				return err
			}
			if supplierID == "" {
				return ErrFinancePrincipalUnresolved
			}
		}
		if supplierID != "" {
			if amount := it.unitCost * int64(it.qty); amount > 0 {
				reason := fmt.Sprintf("برگشت هزینه کالا — مرجوعی مشتری %s / قلم %s", returnID, it.id)
				if err := insertReturnReversalTx(ctx, tx, returnID, it.id, orderID,
					supplierID, "", "product_cost", "supplier", supplierID,
					amount, "crcb:"+returnID+":"+it.id, reason, actorUserID); err != nil {
					return err
				}
			}
		}
		if storeOwnerType == "dropshipper" && storeID.Valid && storeID.String != "" {
			if amount := it.unitMargin * int64(it.qty); amount > 0 {
				reason := fmt.Sprintf("برگشت سود فروشنده — مرجوعی مشتری %s / قلم %s", returnID, it.id)
				if err := insertReturnReversalTx(ctx, tx, returnID, it.id, orderID,
					"", storeID.String, "seller_profit", "seller", storeID.String,
					amount, "crsp:"+returnID+":"+it.id, reason, actorUserID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// insertReturnReversalTx writes one GAP 6 reversal pair: a platform
// ledger debit against the party (mirroring
// ConfirmReturnParcelCharge's ledger shape), then the
// customer_return_financial_events row the balances read back.
func insertReturnReversalTx(ctx context.Context, tx *sql.Tx, returnID, returnItemID, orderID, supplierID, storeID, component, ledgerTargetType, ledgerTargetAccount string, amount int64, idemKey, reason, actorUserID string) error {
	ledgerID := "RET-LED-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, NOW(), $2, $3, 'debit', $4, $5, $6, $7)
	`, ledgerID, ledgerTargetAccount, ledgerTargetType, amount, reason, actorUserID, returnID); err != nil {
		return err
	}
	var supplierArg, storeArg interface{}
	if supplierID != "" {
		supplierArg = supplierID
	}
	if storeID != "" {
		storeArg = storeID
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO customer_return_financial_events (id, return_id, return_item_id, order_id, supplier_id, store_id, component, operation, amount, ledger_entry_id, idempotency_key, reason, actor_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'debit', $8, $9, $10, $11, $12)
	`, "CRF-"+uuid.NewString(), returnID, returnItemID, orderID, supplierArg, storeArg, component, amount, ledgerID, idemKey, reason, actorUserID)
	return err
}

// RefundCustomerReturn moves a QC-received return to refunded and
// credits the customer's wallet by the returned items' snapshot
// prices — ALL received items, defective included (owner decision
// D1, Nov 5 2026): a defective item is the supplier's breach and the
// customer is made whole in full, while an ok (taste) item is
// covered by the standing return promise and the customer's only
// penalty is the return-shipping share settled below. The order
// itself is deliberately NOT changed: live shows a refunded return
// on an order that stayed delivered_confirmed with profit released.
// The money reversal is event-based instead (GAP 6): per refunded
// item, the supplier's product cost and — on dropshipper orders —
// the seller's margin reverse through ledger debits +
// customer_return_financial_events rows, which the balances net
// out; the order row is never mutated.
// TODO(U21): whether the live refund also moved real PSP money is
// unverified; this implements the wallet refund named in the
// migration comment and no PSP call.
func (r *OrderRepository) RefundCustomerReturn(ctx context.Context, id string, actorUserID string) (int64, *ReturnShippingSettlement, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	var financeOrderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM customer_returns WHERE id=$1`, id).Scan(&financeOrderID); err != nil {
		return 0, nil, err
	}
	if err := lockOrderFinanceTx(ctx, tx, financeOrderID, true); err != nil {
		return 0, nil, err
	}
	var customerID string
	var orderID string
	var status string
	var shippingAmount sql.NullInt64
	var shippingBearer sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT customer_id::text, order_id, status, return_shipping_amount, return_shipping_bearer
		FROM customer_returns WHERE id = $1 FOR UPDATE
	`, id).Scan(&customerID, &orderID, &status, &shippingAmount, &shippingBearer); err != nil {
		return 0, nil, err
	}
	var originalCustomer sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT customer_user_id::text FROM orders WHERE id=$1`, financeOrderID).Scan(&originalCustomer); err != nil {
		return 0, nil, err
	}
	if orderID != financeOrderID || !originalCustomer.Valid || customerID != originalCustomer.String {
		return 0, nil, ErrFinancePrincipalUnresolved
	}
	if status != "received" {
		return 0, nil, fmt.Errorf("only a received return can be refunded (status is %s)", status)
	}
	// Defence in depth (GAP 6): re-verify the over-return invariant
	// before any money moves. CreateCustomerReturn enforces it at
	// filing; a filing that predates the guard (or raced it) must
	// not pay out here.
	var overItems int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM (
			SELECT ri.order_item_id
			FROM return_items ri
			JOIN customer_returns cr ON cr.id = ri.return_id
			JOIN order_items oi ON oi.id::text = ri.order_item_id
			WHERE cr.order_id = $1 AND cr.status NOT IN ('rejected', 'cancelled')
			GROUP BY ri.order_item_id, oi.quantity
			HAVING SUM(ri.qty) > oi.quantity
		) over_items
	`, orderID).Scan(&overItems); err != nil {
		return 0, nil, err
	}
	if overItems > 0 {
		return 0, nil, fmt.Errorf("returned quantity exceeds ordered quantity for order %s", orderID)
	}
	var amount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(oi.final_unit_price * ri.qty), 0)
		FROM return_items ri JOIN order_items oi ON oi.id::text = ri.order_item_id
		WHERE ri.return_id = $1 AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
	`, id).Scan(&amount); err != nil {
		return 0, nil, err
	}
	creditWallet := func(sum int64) error {
		if sum <= 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO customer_wallets (customer_id, balance) VALUES ($1::uuid, $2)
			ON CONFLICT (customer_id) DO UPDATE SET balance = customer_wallets.balance + EXCLUDED.balance, updated_at = NOW()
		`, customerID, sum)
		return err
	}
	if err := r.recordReturnReversalsTx(ctx, tx, id, orderID, actorUserID); err != nil {
		return 0, nil, err
	}
	reservation, err := reserveOrderRefundTx(ctx, tx, orderID, "customer_return", id, "wallet", actorUserID, amount, "")
	if err != nil {
		return 0, nil, err
	}
	if err = creditReservedWalletRefundsTx(ctx, tx, reservation.IDs); err != nil {
		return 0, nil, err
	}

	// Stage 2 of the return-shipping settlement (migration
	// 202610250202): runs only when stage 1 recorded a shipping
	// amount at receive. Sequencing follows the policy: the product
	// refund above is credited FIRST, then (a) the supplier-borne
	// shipping total is credited (the customer prepaid the courier
	// and is made whole — the credited total is exactly the sum
	// charged to suppliers at receive, so credits match debits), and
	// (b) the customer-borne total is debited, clamped to the
	// post-credit balance so the wallet never goes below zero; the
	// clamped shortfall is written off (reported, never collected,
	// never carried as debt). The bearer='customer' charge rows
	// record the ACTUALLY debited amounts, distributed over the
	// customer's items pro-rata to their shares.
	//
	// Fix #2: the shares themselves were computed ONCE at receive
	// and persisted as bearer='customer' rows at the receive-time
	// amounts; this stage consumes those rows and finalizes their
	// amounts to what was actually debited. (The pre-fix code
	// recomputed the shares here at CURRENT tariff rates, so a
	// tariff edit between receive and refund re-priced the return
	// and diverged from the recorded return_shipping_amount.) A
	// return received before the persistence existed has no
	// customer rows: it takes the legacy recompute below, once.
	var settlement *ReturnShippingSettlement
	if shippingAmount.Valid {
		settlement = &ReturnShippingSettlement{
			ReturnShippingAmount: shippingAmount.Int64,
			ReturnShippingBearer: shippingBearer.String,
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(amount), 0) FROM customer_return_shipping_charges WHERE return_id = $1 AND bearer = 'supplier'
		`, id).Scan(&settlement.SupplierBorneTotal); err != nil {
			return 0, nil, err
		}
		type customerShareRow struct {
			chargeID     string // persisted bearer='customer' row id; "" on the legacy path
			returnItemID string
			supplierID   string
			amount       int64
		}
		var customerShares []customerShareRow
		chargeRows, err := tx.QueryContext(ctx, `
			SELECT id, return_item_id::text, supplier_id, amount
			FROM customer_return_shipping_charges
			WHERE return_id = $1 AND bearer = 'customer'
			ORDER BY return_item_id ASC
		`, id)
		if err != nil {
			return 0, nil, err
		}
		for chargeRows.Next() {
			var row customerShareRow
			if err := chargeRows.Scan(&row.chargeID, &row.returnItemID, &row.supplierID, &row.amount); err != nil {
				chargeRows.Close()
				return 0, nil, err
			}
			settlement.CustomerBorneTotal += row.amount
			customerShares = append(customerShares, row)
		}
		if err := chargeRows.Err(); err != nil {
			chargeRows.Close()
			return 0, nil, err
		}
		chargeRows.Close()
		if len(customerShares) == 0 {
			// Legacy path: this return was received before receive
			// started persisting bearer='customer' rows, so its
			// receive-time shares exist nowhere. Recompute them the
			// way the pre-fix refund did (current rates); the
			// finalization below INSERTs their rows as it did then.
			rates, err := shippingquote.LoadRates(ctx, tx)
			if err != nil {
				return 0, nil, err
			}
			shares, _, err := computeReturnShippingShares(ctx, tx, id, rates)
			if err != nil {
				return 0, nil, err
			}
			for _, share := range shares {
				if share.Bearer == "customer" {
					settlement.CustomerBorneTotal += share.Amount
					customerShares = append(customerShares, customerShareRow{
						returnItemID: share.ReturnItemID, supplierID: share.SupplierID, amount: share.Amount,
					})
				}
			}
		}
		if err := creditWallet(settlement.SupplierBorneTotal); err != nil {
			return 0, nil, err
		}
		var balance int64
		if err := tx.QueryRowContext(ctx, `SELECT balance FROM customer_wallets WHERE customer_id = $1::uuid`, customerID).Scan(&balance); err != nil {
			if err != sql.ErrNoRows {
				return 0, nil, err
			}
			balance = 0
		}
		debit := settlement.CustomerBorneTotal
		if debit > balance {
			debit = balance
		}
		if debit > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE customer_wallets SET balance = balance - $2, updated_at = NOW() WHERE customer_id = $1::uuid
			`, customerID, debit); err != nil {
				return 0, nil, err
			}
			// Deterministic order for remainder absorption (the
			// persisted read already orders by return item id; the
			// legacy recompute emits the same order — this sort is
			// the safeguard the pre-fix code applied too).
			sort.Slice(customerShares, func(i, j int) bool { return customerShares[i].returnItemID < customerShares[j].returnItemID })
			weights := make([]int64, len(customerShares))
			for i, share := range customerShares {
				weights[i] = share.amount
			}
			debited := allocateProRata(debit, weights)
			for i, share := range customerShares {
				if share.chargeID != "" {
					// Finalize the persisted row to the amount
					// actually debited: unchanged needs no write, a
					// reduced amount is updated, and an item debited
					// nothing loses its row (a row may never claim
					// an uncollected amount, and the table forbids
					// amount = 0; the pre-fix end state for such an
					// item was likewise no row).
					switch {
					case debited[i] == share.amount:
					case debited[i] > 0:
						if _, err := tx.ExecContext(ctx, `
							UPDATE customer_return_shipping_charges SET amount = $1 WHERE id = $2
						`, debited[i], share.chargeID); err != nil {
							return 0, nil, err
						}
					default:
						if _, err := tx.ExecContext(ctx, `
							DELETE FROM customer_return_shipping_charges WHERE id = $1
						`, share.chargeID); err != nil {
							return 0, nil, err
						}
					}
					continue
				}
				if debited[i] <= 0 || share.supplierID == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO customer_return_shipping_charges (id, return_id, return_item_id, supplier_id, bearer, amount, ledger_entry_id, idempotency_key, reason, actor_user_id)
					VALUES ($1, $2, $3, $4, 'customer', $5, NULL, $6, $7, $8)
				`, uuid.NewString(), id, share.returnItemID, share.supplierID, debited[i],
					"crsc:"+id+":"+share.returnItemID, returnShippingReasonTaste, actorUserID); err != nil {
					return 0, nil, err
				}
			}
		} else {
			// Nothing could be collected (the post-credit balance
			// was zero): persisted customer rows must not keep
			// claiming amounts that were never debited.
			for _, share := range customerShares {
				if share.chargeID == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `
					DELETE FROM customer_return_shipping_charges WHERE id = $1
				`, share.chargeID); err != nil {
					return 0, nil, err
				}
			}
		}
		settlement.CustomerDebited = debit
		settlement.CustomerWrittenOff = settlement.CustomerBorneTotal - debit
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_returns
		SET status = 'refunded',
			return_shipping_settled_at = CASE WHEN $2 THEN NOW() ELSE return_shipping_settled_at END,
			updated_at = NOW()
		WHERE id = $1 AND status = 'received'
	`, id, shippingAmount.Valid); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	// P0-8 (best-effort): wallet refund - customer, amount + wallet
	// destination. The wallet credit above is real (customer_wallets
	// upsert), so the wording states a wallet credit, not a PSP
	// reversal. Total credited = product refund + supplier-borne
	// shipping - customer-borne shipping debited.
	totalCredited := amount
	extra := ""
	if settlement != nil {
		totalCredited += settlement.SupplierBorneTotal - settlement.CustomerDebited
		if settlement.CustomerDebited > 0 {
			extra = fmt.Sprintf(" هزینه حمل سهم شما (%s) از کیف پول کسر شد.", formatToman(settlement.CustomerDebited))
		}
	}
	if o, oerr := loadOrderNotifyCtx(ctx, r.db, orderID); oerr == nil {
		notifyRefundIssued(ctx, r.db, orderID, o.OrderNumber, customerID, totalCredited, true, extra)
	} else {
		notifyRefundIssued(ctx, r.db, orderID, "", customerID, totalCredited, true, extra)
	}
	return amount, settlement, nil
}

// ReturnQuote estimates the refund for selected order items (or all
// items when no selection is given) from their snapshot prices.
// TODO(U21): no return-shipping amount is included: the live quote
// shape and any shipping component are unverified, so shipping is
// not guessed into the quote.
func (r *OrderRepository) ReturnQuote(ctx context.Context, orderIDOrNumber string, orderItemIDs []string) (int64, *domain.Order, error) {
	order, err := r.resolveOrderByIDOrNumber(ctx, orderIDOrNumber)
	if err != nil {
		return 0, nil, err
	}
	if order == nil {
		return 0, nil, sql.ErrNoRows
	}
	var amount int64
	if len(orderItemIDs) == 0 {
		for _, it := range order.Items {
			amount += it.FinalUnitPrice * int64(it.Quantity)
		}
		return amount, order, nil
	}
	byID := make(map[string]domain.OrderItem, len(order.Items))
	for _, it := range order.Items {
		byID[it.ID] = it
	}
	for _, itemID := range orderItemIDs {
		it, ok := byID[itemID]
		if !ok {
			return 0, nil, sql.ErrNoRows
		}
		amount += it.FinalUnitPrice * int64(it.Quantity)
	}
	return amount, order, nil
}
