package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SupplierShipmentRepository manages supplier direct drop-shipments
type SupplierShipmentRepository struct {
	db *sql.DB
}

func NewSupplierShipmentRepository(db *sql.DB) *SupplierShipmentRepository {
	return &SupplierShipmentRepository{db: db}
}

// CreateShipment inserts a new direct fulfillment shipment
func (r *SupplierShipmentRepository) CreateShipment(ctx context.Context, shipment domain.SupplierShipment) error {
	addressJSON, err := json.Marshal(shipment.ShippingAddress)
	if err != nil {
		return fmt.Errorf("failed to marshal shipping address: %w", err)
	}

	if shipment.ID == "" {
		shipment.ID = fmt.Sprintf("ship-%d", time.Now().UnixNano()/1e6)
	}
	if shipment.Status == "" {
		shipment.Status = "pending"
	}

	query := `
		INSERT INTO supplier_shipments (
			id, order_id, supplier_id, shipping_address, customer_phone, customer_name, status, shipped_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::varchar, CASE WHEN $7::varchar IN ('shipped','in_transit','delivered') THEN NOW() ELSE NULL END, NOW(), NOW())
	`

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockFulfillmentOrderTx(ctx, tx, shipment.OrderID); err != nil {
		return err
	}
	var orderStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id=$1`, shipment.OrderID).Scan(&orderStatus); err != nil {
		return err
	}
	if orderStatus == "cancelled" {
		return ErrShipmentState
	}
	if shipment.Status == "shipped" || shipment.Status == "in_transit" || shipment.Status == "delivered" {
		if err := assertOrderDispatchAllowedTx(ctx, tx, shipment.OrderID); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, query,
		shipment.ID,
		shipment.OrderID,
		shipment.SupplierID,
		addressJSON,
		shipment.CustomerPhone,
		shipment.CustomerName,
		shipment.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to insert supplier shipment: %w", err)
	}

	return tx.Commit()
}

// GetPendingShipmentsPage returns one cursor page of pending shipments for
// a given supplier (or all if empty/admin), oldest first (work-queue
// order). The cursor is the (created_at ASC, id ASC) keyset. PERF-02
// Step 5: the old unbounded list is gone.
func (r *SupplierShipmentRepository) GetPendingShipmentsPage(ctx context.Context, supplierID string, opts ListPageOptions) (ListPage[domain.SupplierShipment], error) {
	limit := NormalizeListLimit(opts.Limit)
	query := `
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, ''), NULL::timestamptz, false
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE ($1 = '' OR $1 = 'all' OR s.supplier_id = $1 OR s.supplier_id IN (SELECT id FROM suppliers WHERE owner_user_id::text = $1))
		  AND s.status = 'pending'
	`
	args := []interface{}{supplierID}
	if opts.Cursor != "" {
		cursorAt, cursorID, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return ListPage[domain.SupplierShipment]{}, err
		}
		// ASC queue: the next page is rows AFTER the cursor key.
		query += " AND (s.created_at, s.id) > ($2::timestamptz, $3::text)"
		args = append(args, cursorAt, cursorID)
	}
	query += fmt.Sprintf(" ORDER BY s.created_at ASC, s.id ASC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	shipments, err := r.scanShipments(ctx, query, args...)
	if err != nil {
		return ListPage[domain.SupplierShipment]{}, fmt.Errorf("failed to query pending shipments: %w", err)
	}
	hasMore := len(shipments) > limit
	if hasMore {
		shipments = shipments[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := shipments[len(shipments)-1]
		nextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
	}
	return ListPage[domain.SupplierShipment]{Items: shipments, NextCursor: nextCursor, HasMore: hasMore, Limit: limit}, nil
}

// GetPendingShipments returns pending shipments for a given supplier (or all if empty/admin)
// Legacy compat: the first page only, at most MaxListLimit rows.
// HTTP handlers use GetPendingShipmentsPage.
func (r *SupplierShipmentRepository) GetPendingShipments(ctx context.Context, supplierID string) ([]domain.SupplierShipment, error) {
	page, err := r.GetPendingShipmentsPage(ctx, supplierID, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// scanShipments runs a shipment list query and scans the rows.
func (r *SupplierShipmentRepository) scanShipments(ctx context.Context, query string, args ...interface{}) ([]domain.SupplierShipment, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shipments []domain.SupplierShipment
	for rows.Next() {
		var s domain.SupplierShipment
		var addressJSON []byte

		err := rows.Scan(
			&s.ID, &s.OrderID, &s.SupplierID, &addressJSON, &s.CustomerPhone, &s.CustomerName,
			&s.Carrier, &s.TrackingNumber, &s.ShippedAt, &s.DeliveredAt, &s.Status, &s.SupplierNotes,
			&s.CreatedAt, &s.UpdatedAt,
			&s.OrderNumber, &s.DispatchDueAt, &s.DispatchEstimate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan supplier shipment: %w", err)
		}

		_ = json.Unmarshal(addressJSON, &s.ShippingAddress)
		shipments = append(shipments, s)
	}
	if shipments == nil {
		shipments = []domain.SupplierShipment{}
	}
	return shipments, rows.Err()
}

// GetShipmentsPage returns one cursor page of shipments matching supplier
// and optional status, newest first, keyed on (created_at DESC, id DESC).
// PERF-02 Step 5.
func (r *SupplierShipmentRepository) GetShipmentsPage(ctx context.Context, supplierID, status string, opts ListPageOptions) (ListPage[domain.SupplierShipment], error) {
	return r.shipmentsPage(ctx, supplierID, status, "", "", opts)
}

func (r *SupplierShipmentRepository) GetSupplierOrderWorkPage(ctx context.Context, supplierID, work, search string, opts ListPageOptions) (ListPage[domain.SupplierShipment], error) {
	return r.shipmentsPage(ctx, supplierID, "", work, search, opts)
}

func (r *SupplierShipmentRepository) shipmentsPage(ctx context.Context, supplierID, status, work, search string, opts ListPageOptions) (ListPage[domain.SupplierShipment], error) {
	limit := NormalizeListLimit(opts.Limit)
	whereClauses, args := shipmentWhereClauses(supplierID, status)
	deadline := "NULL::timestamptz, false"
	shipmentStatus := "s.status"
	if work != "" {
		whereClauses, args = []string{"s.supplier_id = $1"}, []interface{}{supplierID}
		shipmentStatus = `CASE WHEN s.status IN ('pending','processing') THEN
			CASE WHEN o.status='cancelled' THEN 'cancelled'
			WHEN o.payment_status IS DISTINCT FROM 'paid' THEN 'awaiting_payment' ELSE s.status END
			ELSE s.status END`
		deadline = "CASE WHEN " + supplierOrderReadySQL + " THEN " + supplierDispatchDeadlineSQL("s") + " END, s.dispatch_due_at IS NULL"
		if work == "ready" || work == "overdue" {
			whereClauses = append(whereClauses, supplierOrderReadySQL)
		}
		if work == "overdue" {
			whereClauses = append(whereClauses, supplierDispatchDeadlineSQL("s")+" < now()")
		}
		if work == "shipped" {
			whereClauses = append(whereClauses, "s.status IN ('shipped','in_transit')")
		}
		if work == "delivered" {
			whereClauses = append(whereClauses, "s.status IN ('delivered','delivered_confirmed')")
		}
		if search != "" {
			const normalize = "translate(lower(%s), '۰۱۲۳۴۵۶۷۸۹٠١٢٣٤٥٦٧٨٩يك', '01234567890123456789یک')"
			parameter := fmt.Sprintf(normalize, fmt.Sprintf("$%d", len(args)+1))
			fields := []string{"o.order_number", "s.order_id", "s.customer_name", "s.tracking_number"}
			for i, field := range fields {
				fields[i] = "strpos(" + fmt.Sprintf(normalize, field) + ", " + parameter + ") > 0"
			}
			whereClauses = append(whereClauses, "("+strings.Join(fields, " OR ")+")")
			args = append(args, search)
		}
	}
	if opts.Cursor != "" {
		cursorAt, cursorID, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return ListPage[domain.SupplierShipment]{}, err
		}
		whereClauses = append(whereClauses, fmt.Sprintf("(s.created_at, s.id) < ($%d::timestamptz, $%d::text)", len(args)+1, len(args)+2))
		args = append(args, cursorAt, cursorID)
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + whereClauses[0]
		for i := 1; i < len(whereClauses); i++ {
			whereSQL += " AND " + whereClauses[i]
		}
	}
	query := fmt.Sprintf(`
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, %s, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, ''), %s
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		%s
		ORDER BY s.created_at DESC, s.id DESC LIMIT $%d
	`, shipmentStatus, deadline, whereSQL, len(args)+1)
	args = append(args, limit+1)
	shipments, err := r.scanShipments(ctx, query, args...)
	if err != nil {
		return ListPage[domain.SupplierShipment]{}, fmt.Errorf("failed to query shipments: %w", err)
	}
	hasMore := len(shipments) > limit
	if hasMore {
		shipments = shipments[:limit]
	}
	nextCursor := ""
	if hasMore {
		last := shipments[len(shipments)-1]
		nextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
	}
	return ListPage[domain.SupplierShipment]{Items: shipments, NextCursor: nextCursor, HasMore: hasMore, Limit: limit}, nil
}

// shipmentWhereClauses builds the supplier/status predicates shared by
// GetShipments and GetShipmentsPage. argIdx tracks $ placeholders.
func shipmentWhereClauses(supplierID, status string) ([]string, []interface{}) {
	var whereClauses []string
	var args []interface{}
	argIdx := 1

	if supplierID != "" && supplierID != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("(s.supplier_id = $%d OR s.supplier_id IN (SELECT id FROM suppliers WHERE owner_user_id::text = $%d))", argIdx, argIdx))
		args = append(args, supplierID)
		argIdx++
	}

	if status != "" && status != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("s.status = $%d", argIdx))
		args = append(args, status)
		argIdx++
	}
	return whereClauses, args
}

// GetShipments returns shipments matching supplier and optional status.
// Legacy compat: the first page only, at most MaxListLimit rows.
// HTTP handlers use GetShipmentsPage.
func (r *SupplierShipmentRepository) GetShipments(ctx context.Context, supplierID, status string) ([]domain.SupplierShipment, error) {
	page, err := r.GetShipmentsPage(ctx, supplierID, status, ListPageOptions{Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// GetShipmentByID retrieves a shipment record by its primary key
func (r *SupplierShipmentRepository) GetShipmentByID(ctx context.Context, id string) (*domain.SupplierShipment, error) {
	query := `
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE s.id = $1
	`

	var s domain.SupplierShipment
	var addressJSON []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&s.ID, &s.OrderID, &s.SupplierID, &addressJSON, &s.CustomerPhone, &s.CustomerName,
		&s.Carrier, &s.TrackingNumber, &s.ShippedAt, &s.DeliveredAt, &s.Status, &s.SupplierNotes,
		&s.CreatedAt, &s.UpdatedAt,
		&s.OrderNumber,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	_ = json.Unmarshal(addressJSON, &s.ShippingAddress)
	return &s, nil
}

// GetShipmentByIDSupplier returns a shipment only when it belongs to the
// authenticated supplier. Supplier IDs are part of the authorization boundary.
func (r *SupplierShipmentRepository) GetShipmentByIDSupplier(ctx context.Context, id, supplierID string) (*domain.SupplierShipment, error) {
	query := `
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE s.id = $1 AND s.supplier_id = $2
		`
	return r.scanShipment(ctx, query, id, supplierID)
}

func (r *SupplierShipmentRepository) scanShipment(ctx context.Context, query string, args ...interface{}) (*domain.SupplierShipment, error) {
	var s domain.SupplierShipment
	var addressJSON []byte
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&s.ID, &s.OrderID, &s.SupplierID, &addressJSON, &s.CustomerPhone, &s.CustomerName,
		&s.Carrier, &s.TrackingNumber, &s.ShippedAt, &s.DeliveredAt, &s.Status, &s.SupplierNotes,
		&s.CreatedAt, &s.UpdatedAt, &s.OrderNumber,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(addressJSON, &s.ShippingAddress)
	return &s, nil
}

// GetShipmentByOrderID retrieves the direct shipment record for an order
func (r *SupplierShipmentRepository) GetShipmentByOrderID(ctx context.Context, orderID string) (*domain.SupplierShipment, error) {
	query := `
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE s.order_id = $1
		ORDER BY s.created_at DESC
		LIMIT 1
	`

	var s domain.SupplierShipment
	var addressJSON []byte

	err := r.db.QueryRowContext(ctx, query, orderID).Scan(
		&s.ID, &s.OrderID, &s.SupplierID, &addressJSON, &s.CustomerPhone, &s.CustomerName,
		&s.Carrier, &s.TrackingNumber, &s.ShippedAt, &s.DeliveredAt, &s.Status, &s.SupplierNotes,
		&s.CreatedAt, &s.UpdatedAt,
		&s.OrderNumber,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	_ = json.Unmarshal(addressJSON, &s.ShippingAddress)
	return &s, nil
}

// GetShipmentByOrderIDForUser returns the caller's own direct shipment for an
// order, or nil when the caller has none. The supplier predicate mirrors
// OrderRepository.CanReadOperationalTracking's supplier branch (owner, or an
// active team member with orders.manage / supplier.manage), so a supplier
// party to a multi-supplier order can never be handed another supplier's
// carrier or tracking number through the order-level lookup.
func (r *SupplierShipmentRepository) GetShipmentByOrderIDForUser(ctx context.Context, orderID, userID string) (*domain.SupplierShipment, error) {
	query := `
		SELECT
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE s.order_id = $1 AND (
			s.supplier_id IN (SELECT id FROM suppliers WHERE owner_user_id::text = $2)
			OR EXISTS (
				SELECT 1 FROM supplier_members sm
				WHERE sm.supplier_id = s.supplier_id AND sm.user_id = $2::uuid
				AND sm.is_active AND (sm.permissions ? 'orders.manage' OR sm.permissions ? 'supplier.manage')
			)
		)
		ORDER BY s.created_at DESC
		LIMIT 1
	`
	return r.scanShipment(ctx, query, orderID, userID)
}

// MarkShipped updates shipment status to 'shipped' with carrier and tracking number
func (r *SupplierShipmentRepository) MarkShipped(ctx context.Context, shipmentID, carrier, trackingNumber string, notes *string) error {
	return r.markShippedGuarded(ctx, "", shipmentID, carrier, trackingNumber, notes)
}
func (r *SupplierShipmentRepository) MarkShippedForSupplier(ctx context.Context, supplierID, shipmentID, carrier, trackingNumber string, notes *string) error {
	return r.markShippedGuarded(ctx, supplierID, shipmentID, carrier, trackingNumber, notes)
}
func (r *SupplierShipmentRepository) markShippedGuarded(ctx context.Context, supplierID, shipmentID, carrier, trackingNumber string, notes *string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM supplier_shipments WHERE id=$1 AND ($2='' OR supplier_id=$2)`, shipmentID, supplierID).Scan(&orderID); err != nil {
		return err
	}
	if err := lockFulfillmentOrderTx(ctx, tx, orderID); err != nil {
		return err
	}
	var replay bool
	if err := tx.QueryRowContext(ctx, `SELECT status='shipped' AND COALESCE(tracking_number,'')=$3 AND (shipped_at IS NOT NULL OR delivered_at IS NOT NULL OR status IN ('shipped','in_transit','delivered')) FROM supplier_shipments WHERE id=$1 AND order_id=$2`, shipmentID, orderID, trackingNumber).Scan(&replay); err != nil {
		return err
	}
	// The order stabilizes dispatch/cancellation while the new-dispatch branch
	// acquires earlier financial guards before taking the shipment row lock.
	if err := assertOrderDispatchAllowedTx(ctx, tx, orderID, replay); err != nil {
		return err
	}
	var status, currentTracking string
	if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(tracking_number,'') FROM supplier_shipments WHERE id=$1 AND order_id=$2 AND ($3='' OR supplier_id=$3) FOR UPDATE`, shipmentID, orderID, supplierID).Scan(&status, &currentTracking); err != nil {
		return err
	}
	if status == "shipped" && currentTracking == trackingNumber {
		return tx.Commit()
	}
	if replay {
		return ErrShipmentState
	}
	if status != "pending" && status != "processing" {
		return ErrShipmentState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_shipments SET status='shipped',carrier=$2,tracking_number=$3,shipped_at=now(),supplier_notes=$4,updated_at=now() WHERE id=$1`, shipmentID, carrier, trackingNumber, notes); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateShipmentStatus updates status via tracking number (e.g., from carrier webhook)
func (r *SupplierShipmentRepository) UpdateShipmentStatus(ctx context.Context, trackingNumber, status string) (*domain.SupplierShipment, error) {
	if status != "shipped" && status != "in_transit" && status != "delivered" && status != "failed" {
		return nil, ErrShipmentState
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM supplier_shipments WHERE tracking_number=$1`, trackingNumber).Scan(&orderID); err != nil {
		return nil, err
	}
	if err := lockOrderFinanceTx(ctx, tx, orderID); err != nil {
		return nil, err
	}
	var alreadyPhysical bool
	if err := tx.QueryRowContext(ctx, `SELECT shipped_at IS NOT NULL OR delivered_at IS NOT NULL OR status IN ('shipped','in_transit','delivered') FROM supplier_shipments WHERE tracking_number=$1 AND order_id=$2 FOR UPDATE`, trackingNumber, orderID).Scan(&alreadyPhysical); err != nil {
		return nil, err
	}
	// Carrier facts for prior custody and failed metadata do not create dispatch.
	if err := assertOrderDispatchAllowedTx(ctx, tx, orderID, alreadyPhysical || status == "failed"); err != nil {
		return nil, err
	}
	var s domain.SupplierShipment
	err = tx.QueryRowContext(ctx, `
		UPDATE supplier_shipments
		SET status = $2,
 shipped_at = CASE WHEN $2 IN ('shipped','in_transit','delivered') OR status IN ('shipped','in_transit','delivered') THEN COALESCE(shipped_at,now()) ELSE shipped_at END,
		    delivered_at = CASE WHEN $2 = 'delivered' THEN COALESCE(delivered_at, NOW()) ELSE delivered_at END,
		    updated_at = NOW()
		WHERE tracking_number = $1 AND order_id=$3
		  AND (
			status = $2 OR
			(status IN ('shipped', 'in_transit') AND $2 IN ('in_transit', 'delivered')) OR
			(status = 'pending' AND $2 = 'shipped') OR
			(status IN ('pending', 'processing') AND $2 = 'failed') OR
			-- Critic fix (Phase 2, fix 1): a carrier failure realistically
			-- arrives AFTER dispatch, when the shipment is shipped /
			-- in_transit. Without this leg a failed webhook for an
			-- in-flight direct shipment matched no row, the handler
			-- fell through to the warehouse path (404), and the
			-- 'failed' wiring in handleWebhookTrackingUpdate was dead
			-- for the only realistic case.
			(status IN ('shipped', 'in_transit') AND $2 = 'failed')
		  )
		RETURNING id, order_id, supplier_id, status
	`, trackingNumber, status, orderID).Scan(&s.ID, &s.OrderID, &s.SupplierID, &s.Status)
	if err != nil {
		return nil, err
	}

	failureActivated := false
	if status == "failed" {
		if failureActivated, err = applyCarrierFailureHoldTx(ctx, tx, orderID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if failureActivated {
		notifyCarrierFailed(ctx, r.db, orderID)
	}
	return &s, nil
}

// LogShipmentAudit records an access or state event in supplier_shipment_audit_logs
func (r *SupplierShipmentRepository) LogShipmentAudit(ctx context.Context, shipmentID, actorUserID, action string) error {
	id := uuid.New().String()
	query := `
		INSERT INTO supplier_shipment_audit_logs (id, shipment_id, actor_user_id, action, timestamp)
		VALUES ($1, $2, $3, $4, NOW())
	`
	_, err := r.db.ExecContext(ctx, query, id, shipmentID, actorUserID, action)
	return err
}

// SupplierWarehouseOrderItem is one of the caller's own lines inside a
// warehouse-routed order. Wholesale cost only: this view never carries the
// dropshipper's margin or the final customer price.
type SupplierWarehouseOrderItem struct {
	OrderItemID   string `json:"orderItemId"`
	ProductID     string `json:"productId"`
	ProductTitle  string `json:"productTitle"`
	VariantID     string `json:"variantId,omitempty"`
	VariantName   string `json:"variantName,omitempty"`
	VariantSPNC   string `json:"variantSpnc,omitempty"`
	Quantity      int    `json:"quantity"`
	WholesaleCost int64  `json:"wholesaleCost"`
	LineTotal     int64  `json:"lineTotal"`
}

// SupplierWarehouseOrder is a paid order fulfilled from the central
// warehouse that contains at least one of the caller's products.
type SupplierWarehouseOrder struct {
	OrderID     string                       `json:"orderId"`
	OrderNumber string                       `json:"orderNumber"`
	Status      string                       `json:"status"`
	Province    string                       `json:"province"`
	City        string                       `json:"city"`
	CreatedAt   time.Time                    `json:"createdAt"`
	Items       []SupplierWarehouseOrderItem `json:"items"`
	ItemsTotal  int64                        `json:"itemsTotal"`
}

// Only the warehouse-origin units of the supplier's own lines are visible.
// Older orders without origin snapshots keep the original ownership check.
const supplierWarehouseLinesSQL = `
	FROM order_items oi JOIN products p ON p.id=oi.product_id
	JOIN LATERAL (
		SELECT CASE WHEN count(*)=0 THEN CASE WHEN p.supplier_id=$1 AND NOT EXISTS (
			SELECT 1 FROM supplier_shipments ss WHERE ss.order_id=oi.order_id AND ss.supplier_id=$1
		) THEN oi.quantity ELSE 0 END
		ELSE COALESCE(sum(x.quantity) FILTER (WHERE x.origin_type='warehouse' AND x.order_id=oi.order_id AND x.supplier_id=$1), 0)
		END AS quantity
		FROM order_item_origins x WHERE x.order_item_id=oi.id
	) warehouse ON warehouse.quantity > 0 AND warehouse.quantity <= oi.quantity
`

func (r *SupplierShipmentRepository) GetSupplierWarehouseOrders(ctx context.Context, supplierID string, limit, offset int) ([]SupplierWarehouseOrder, int, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	const visible = `FROM orders o WHERE o.payment_status='paid'
		AND EXISTS (SELECT 1 ` + supplierWarehouseLinesSQL + ` WHERE oi.order_id=o.id)`
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) "+visible, supplierID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count supplier warehouse orders: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT o.id, o.order_number, o.status, o.customer_province, o.customer_city, o.created_at
		`+visible+`
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT $2 OFFSET $3
	`, supplierID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query supplier warehouse orders: %w", err)
	}
	defer rows.Close()

	orders := make([]SupplierWarehouseOrder, 0)
	ids := make([]string, 0)
	positions := make(map[string]int)
	for rows.Next() {
		var o SupplierWarehouseOrder
		if err := rows.Scan(&o.OrderID, &o.OrderNumber, &o.Status, &o.Province, &o.City, &o.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan supplier warehouse order: %w", err)
		}
		o.Items = []SupplierWarehouseOrderItem{}
		positions[o.OrderID] = len(orders)
		ids = append(ids, o.OrderID)
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate supplier warehouse orders: %w", err)
	}
	if len(ids) > 0 {
		itemRows, err := tx.QueryContext(ctx, `
			SELECT oi.order_id, oi.product_id, COALESCE(NULLIF(oi.product_title_snapshot,''), p.title), warehouse.quantity, oi.wholesale_cost, oi.id::text,
			       COALESCE(oi.variant_id::text,''), COALESCE(oi.variant_name,''), COALESCE(oi.variant_spnc,'')
			`+supplierWarehouseLinesSQL+`
			WHERE oi.order_id = ANY($2::text[])
			ORDER BY oi.id
		`, supplierID, pq.Array(ids))
		if err != nil {
			return nil, 0, fmt.Errorf("query supplier warehouse order items: %w", err)
		}
		for itemRows.Next() {
			var item SupplierWarehouseOrderItem
			var orderID string
			if err := itemRows.Scan(&orderID, &item.ProductID, &item.ProductTitle, &item.Quantity, &item.WholesaleCost, &item.OrderItemID, &item.VariantID, &item.VariantName, &item.VariantSPNC); err != nil {
				itemRows.Close()
				return nil, 0, fmt.Errorf("scan supplier warehouse order item: %w", err)
			}
			item.LineTotal = item.WholesaleCost * int64(item.Quantity)
			i := positions[orderID]
			orders[i].ItemsTotal += item.LineTotal
			orders[i].Items = append(orders[i].Items, item)
		}
		err = itemRows.Err()
		itemRows.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("iterate supplier warehouse order items: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}
