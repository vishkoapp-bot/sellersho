package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type AdminTrackedOrder struct {
	ID                string                    `json:"id"`
	OrderNumber       string                    `json:"orderNumber"`
	StoreName         string                    `json:"storeName"`
	Province          string                    `json:"province"`
	City              string                    `json:"city"`
	Status            string                    `json:"status"`
	FulfillmentMethod string                    `json:"fulfillmentMethod"`
	TrackingCode      string                    `json:"trackingCode"`
	TrackingNumber    string                    `json:"trackingNumber,omitempty"`
	Carrier           string                    `json:"carrier,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	PackedAt          *time.Time                `json:"packedAt,omitempty"`
	ShippedAt         *time.Time                `json:"shippedAt,omitempty"`
	ConfirmedAt       *time.Time                `json:"confirmedAt,omitempty"`
	SupplierShipments []AdminTrackedSupplierLeg `json:"supplierShipments"`
}

type AdminTrackedSupplierLeg struct {
	ID             string     `json:"id"`
	Carrier        string     `json:"carrier"`
	TrackingNumber string     `json:"trackingNumber"`
	Status         string     `json:"status"`
	ShippedAt      *time.Time `json:"shippedAt,omitempty"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
}

var adminOrderTrackingStatus = map[string]string{
	"":                  "TRUE",
	"pending":           "o.status = 'pending'",
	"preparing":         "o.status IN ('processing', 'picking', 'picked', 'packed')",
	"shipped":           "o.status = 'shipped'",
	"delivered":         "o.status IN ('delivered', 'delivered_unconfirmed', 'delivered_confirmed')",
	"delivery_disputed": "o.status = 'delivery_disputed'",
	"cancelled":         "o.status = 'cancelled'",
}

var ErrInvalidAdminOrderStatus = errors.New("invalid order status filter")

// ListAdminOrderTracking returns order and shipment data for the tracking list.
// Customer contact data, address details, and internal payment fields are excluded from the projection.
func (r *OrderRepository) ListAdminOrderTracking(ctx context.Context, opts ListPageOptions, status string, references []string) (ListPage[AdminTrackedOrder], error) {
	page := ListPage[AdminTrackedOrder]{Items: []AdminTrackedOrder{}, Limit: NormalizeListLimit(opts.Limit)}
	statusFilter, ok := adminOrderTrackingStatus[status]
	if !ok {
		return page, ErrInvalidAdminOrderStatus
	}
	args := []any{page.Limit + 1}
	conditions := []string{statusFilter}
	if len(references) > 0 {
		args = append(args, pq.Array(references))
		conditions = append(conditions, `o.id IN (
			SELECT id FROM orders WHERE id = ANY($2::varchar[]) OR order_number = ANY($2::varchar[])
				OR tracking_code = ANY($2::varchar[]) OR tracking_number = ANY($2::varchar[])
			UNION SELECT order_id FROM supplier_shipments WHERE tracking_number = ANY($2::varchar[])
		)`)
	}
	if opts.Cursor != "" {
		at, id, err := DecodeCreatedAtCursor(opts.Cursor)
		if err != nil {
			return page, err
		}
		conditions = append(conditions, fmt.Sprintf("(o.created_at, o.id) < ($%d, $%d)", len(args)+1, len(args)+2))
		args = append(args, at, id)
	}
	query := `SELECT recent.id, recent.order_number, recent.store_name, recent.customer_province, recent.customer_city,
			recent.status, COALESCE(recent.fulfillment_method, 'warehouse'), recent.tracking_code, COALESCE(recent.tracking_number, ''),
			COALESCE(recent.carrier, ''), recent.created_at, recent.packed_at, recent.shipped_at, recent.confirmed_at,
			COALESCE((SELECT jsonb_agg(jsonb_build_object(
				'id', s.id, 'status', COALESCE(s.status, ''), 'carrier', COALESCE(s.carrier, ''),
				'trackingNumber', COALESCE(s.tracking_number, ''), 'shippedAt', s.shipped_at, 'deliveredAt', s.delivered_at
			) ORDER BY s.created_at, s.id) FROM supplier_shipments s WHERE s.order_id = recent.id), '[]'::jsonb)
		FROM (
			SELECT id, order_number, store_name, customer_province, customer_city, status, fulfillment_method, tracking_code,
				tracking_number, carrier, created_at, packed_at, shipped_at, confirmed_at
			FROM orders o WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY o.created_at DESC, o.id DESC LIMIT $1
		) recent ORDER BY recent.created_at DESC, recent.id DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var order AdminTrackedOrder
		var shipments []byte
		if err := rows.Scan(&order.ID, &order.OrderNumber, &order.StoreName, &order.Province, &order.City, &order.Status,
			&order.FulfillmentMethod, &order.TrackingCode, &order.TrackingNumber, &order.Carrier,
			&order.CreatedAt, &order.PackedAt, &order.ShippedAt, &order.ConfirmedAt, &shipments); err != nil {
			return page, err
		}
		if err := json.Unmarshal(shipments, &order.SupplierShipments); err != nil {
			return page, err
		}
		page.Items = append(page.Items, order)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > page.Limit {
		page.Items = page.Items[:page.Limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = EncodeCreatedAtCursor(last.CreatedAt, last.ID)
		page.HasMore = true
	}
	return page, nil
}
