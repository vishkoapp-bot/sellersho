package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
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
			id, order_id, supplier_id, shipping_address, customer_phone, customer_name, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`

	_, err = r.db.ExecContext(ctx, query,
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

	return nil
}

// GetPendingShipments returns pending shipments for a given supplier (or all if empty/admin)
func (r *SupplierShipmentRepository) GetPendingShipments(ctx context.Context, supplierID string) ([]domain.SupplierShipment, error) {
	query := `
		SELECT 
			s.id, s.order_id, s.supplier_id, s.shipping_address, s.customer_phone, s.customer_name,
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		WHERE ($1 = '' OR $1 = 'all' OR s.supplier_id = $1 OR s.supplier_id IN (SELECT id FROM suppliers WHERE owner_user_id::text = $1))
		  AND s.status = 'pending'
		ORDER BY s.created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending shipments: %w", err)
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
			&s.OrderNumber,
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
	return shipments, nil
}

// GetShipments returns shipments matching supplier and optional status
func (r *SupplierShipmentRepository) GetShipments(ctx context.Context, supplierID, status string) ([]domain.SupplierShipment, error) {
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
			s.carrier, s.tracking_number, s.shipped_at, s.delivered_at, s.status, s.supplier_notes,
			s.created_at, s.updated_at,
			COALESCE(o.order_number, '')
		FROM supplier_shipments s
		LEFT JOIN orders o ON s.order_id = o.id
		%s
		ORDER BY s.created_at DESC
	`, whereSQL)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query shipments: %w", err)
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
			&s.OrderNumber,
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
	return shipments, nil
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
	query := `
		UPDATE supplier_shipments
		SET status = 'shipped',
		    carrier = $2,
		    tracking_number = $3,
		    shipped_at = NOW(),
		    supplier_notes = $4,
		    updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, shipmentID, carrier, trackingNumber, notes)
	if err != nil {
		return fmt.Errorf("failed to update shipment status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *SupplierShipmentRepository) MarkShippedForSupplier(ctx context.Context, supplierID, shipmentID, carrier, trackingNumber string, notes *string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE supplier_shipments
		SET status = 'shipped', carrier = $3, tracking_number = $4,
		    shipped_at = NOW(), supplier_notes = $5, updated_at = NOW()
		WHERE id = $1 AND supplier_id = $2 AND status IN ('pending', 'processing')
	`, shipmentID, supplierID, carrier, trackingNumber, notes)
	if err != nil {
		return fmt.Errorf("failed to update shipment status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 1 {
		return nil
	}

	var currentStatus, currentTracking string
	err = r.db.QueryRowContext(ctx, `SELECT status, COALESCE(tracking_number, '') FROM supplier_shipments WHERE id = $1 AND supplier_id = $2`, shipmentID, supplierID).Scan(&currentStatus, &currentTracking)
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if currentStatus == "shipped" && currentTracking == trackingNumber {
		return nil
	}
	return ErrShipmentState
}

// UpdateShipmentStatus updates status via tracking number (e.g., from carrier webhook)
func (r *SupplierShipmentRepository) UpdateShipmentStatus(ctx context.Context, trackingNumber, status string) (*domain.SupplierShipment, error) {
	if status != "shipped" && status != "in_transit" && status != "delivered" && status != "failed" {
		return nil, ErrShipmentState
	}
	var s domain.SupplierShipment
	err := r.db.QueryRowContext(ctx, `
		UPDATE supplier_shipments
		SET status = $2,
		    delivered_at = CASE WHEN $2 = 'delivered' THEN COALESCE(delivered_at, NOW()) ELSE delivered_at END,
		    updated_at = NOW()
		WHERE tracking_number = $1
		  AND (
			status = $2 OR
			(status IN ('shipped', 'in_transit') AND $2 IN ('in_transit', 'delivered')) OR
			(status = 'pending' AND $2 = 'shipped') OR
			(status IN ('pending', 'processing') AND $2 = 'failed')
		  )
		RETURNING id, order_id, supplier_id, status
	`, trackingNumber, status).Scan(&s.ID, &s.OrderID, &s.SupplierID, &s.Status)
	if err != nil {
		return nil, err
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
