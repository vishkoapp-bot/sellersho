package repository

// Tapin data access (gaps 1, 2, 3 — Tapin completion).
//
// Everything Tapin-shaped that needs the database lives here so the
// HTTP handlers stay thin: the platform box catalogue (tapin_boxes,
// migration 202611050008 — Tapin's API has no boxes endpoint), the
// platform default box id, a supplier's stored Tapin credentials and
// origin warehouse geo ids, the destination name -> Tapin geo code
// resolution over tapin_geo_snapshots, and the Tapin registration
// state on supplier_shipments.
//
// The token is only ever READ here (for server-side upstream calls);
// no method in this file returns it to an HTTP response.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/drup/platform/internal/tapin"
)

type TapinRepository struct {
	db *sql.DB
}

func NewTapinRepository(db *sql.DB) *TapinRepository {
	return &TapinRepository{db: db}
}

// --- Box catalogue -------------------------------------------------

// ListBoxes returns the whole catalogue, inactive rows included
// (admin view), ordered for display.
func (r *TapinRepository) ListBoxes(ctx context.Context) ([]tapin.Box, error) {
	return r.queryBoxes(ctx, false)
}

// ListActiveBoxes returns only active boxes (supplier view + picks).
func (r *TapinRepository) ListActiveBoxes(ctx context.Context) ([]tapin.Box, error) {
	return r.queryBoxes(ctx, true)
}

func (r *TapinRepository) queryBoxes(ctx context.Context, activeOnly bool) ([]tapin.Box, error) {
	q := `SELECT id, title, length_cm, width_cm, height_cm, max_weight_grams, is_active, sort
	      FROM tapin_boxes`
	if activeOnly {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY sort ASC, id ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list tapin boxes: %w", err)
	}
	defer rows.Close()
	out := []tapin.Box{}
	for rows.Next() {
		var b tapin.Box
		var maxW sql.NullInt64
		if err := rows.Scan(&b.ID, &b.Title, &b.LengthCm, &b.WidthCm, &b.HeightCm, &maxW, &b.IsActive, &b.Sort); err != nil {
			return nil, fmt.Errorf("scan tapin box: %w", err)
		}
		if maxW.Valid {
			v := int(maxW.Int64)
			b.MaxWeightGrams = &v
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ReplaceBoxes upserts the admin-edited catalogue in one transaction.
// Rows are keyed by id; boxes absent from the list are left untouched
// (deactivation is the removal path — a hard delete could orphan the
// tapin_box_id recorded on past shipments).
func (r *TapinRepository) ReplaceBoxes(ctx context.Context, boxes []tapin.Box) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range boxes {
		var maxW interface{}
		if b.MaxWeightGrams != nil {
			maxW = *b.MaxWeightGrams
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tapin_boxes (id, title, length_cm, width_cm, height_cm, max_weight_grams, is_active, sort, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
			ON CONFLICT (id) DO UPDATE SET
				title = EXCLUDED.title, length_cm = EXCLUDED.length_cm,
				width_cm = EXCLUDED.width_cm, height_cm = EXCLUDED.height_cm,
				max_weight_grams = EXCLUDED.max_weight_grams,
				is_active = EXCLUDED.is_active, sort = EXCLUDED.sort,
				updated_at = NOW()`,
			b.ID, b.Title, b.LengthCm, b.WidthCm, b.HeightCm, maxW, b.IsActive, b.Sort); err != nil {
			return fmt.Errorf("upsert tapin box %s: %w", b.ID, err)
		}
	}
	return tx.Commit()
}

// DefaultBoxID reads platform_settings.tapin_default_box_id, falling
// back to '4' (the owner's confirmed default) when unset.
func (r *TapinRepository) DefaultBoxID(ctx context.Context) string {
	var id sql.NullString
	if err := r.db.QueryRowContext(ctx,
		`SELECT tapin_default_box_id FROM platform_settings WHERE id = 1`).Scan(&id); err != nil {
		return "4"
	}
	if !id.Valid || strings.TrimSpace(id.String) == "" {
		return "4"
	}
	return strings.TrimSpace(id.String)
}

// PickBoxForParcel resolves the box for a parcel from the live
// catalogue: PickBox over active boxes; when the catalogue cannot
// produce a pick (empty catalogue / missing default), the platform
// default id itself is the answer, per the owner's box rule.
func (r *TapinRepository) PickBoxForParcel(ctx context.Context, lengthCm, widthCm, heightCm, weightGrams int) (tapin.Box, bool, error) {
	boxes, err := r.ListActiveBoxes(ctx)
	if err != nil {
		return tapin.Box{}, false, err
	}
	def := r.DefaultBoxID(ctx)
	pick, oversize, ok := tapin.PickBox(boxes, lengthCm, widthCm, heightCm, weightGrams, def)
	if !ok {
		return tapin.Box{ID: def, Title: "سایز ۴"}, false, nil
	}
	return pick, oversize, nil
}

// --- Supplier credentials + origin ---------------------------------

// SupplierTapinCredentials is a supplier's stored Tapin connection.
// Connected means BOTH fields are non-empty — the quote and
// registration paths treat a half-connection as not connected.
type SupplierTapinCredentials struct {
	SupplierID string
	ShopID     string
	Token      string
}

func (c SupplierTapinCredentials) Connected() bool {
	return strings.TrimSpace(c.ShopID) != "" && strings.TrimSpace(c.Token) != ""
}

// GetSupplierCredentials loads the stored token + shop id. The token
// is for server-side upstream calls only.
func (r *TapinRepository) GetSupplierCredentials(ctx context.Context, supplierID string) (SupplierTapinCredentials, error) {
	var creds SupplierTapinCredentials
	creds.SupplierID = supplierID
	var shop, token sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT tapin_shop_id, tapin_token FROM suppliers WHERE id = $1`, supplierID).Scan(&shop, &token)
	if err == sql.ErrNoRows {
		return creds, nil
	}
	if err != nil {
		return creds, fmt.Errorf("load supplier tapin credentials: %w", err)
	}
	creds.ShopID = strings.TrimSpace(shop.String)
	creds.Token = strings.TrimSpace(token.String)
	return creds, nil
}

// SupplierOriginWarehouse is the geo identity of a supplier's own
// warehouse: the Tapin province/city ids the warehouse was mapped to
// (supplier_warehouses.tapin_province_id / tapin_city_id, migration
// 202610240001). Zero ids mean "not mapped".
type SupplierOriginWarehouse struct {
	ID              string
	Name            string
	TapinProvinceID int
	TapinCityID     int
}

// GetSupplierWarehouseGeo reads one warehouse's Tapin geo ids.
func (r *TapinRepository) GetSupplierWarehouseGeo(ctx context.Context, warehouseID string) (*SupplierOriginWarehouse, error) {
	var w SupplierOriginWarehouse
	err := r.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(name, ''), COALESCE(tapin_province_id, 0), COALESCE(tapin_city_id, 0)
		 FROM supplier_warehouses WHERE id = $1`, warehouseID).Scan(&w.ID, &w.Name, &w.TapinProvinceID, &w.TapinCityID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load supplier warehouse geo: %w", err)
	}
	return &w, nil
}

// GetSupplierDefaultWarehouseGeo returns the supplier's first active
// warehouse (the same fallback order_repo uses for origin display).
func (r *TapinRepository) GetSupplierDefaultWarehouseGeo(ctx context.Context, supplierID string) (*SupplierOriginWarehouse, error) {
	var w SupplierOriginWarehouse
	err := r.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(name, ''), COALESCE(tapin_province_id, 0), COALESCE(tapin_city_id, 0)
		 FROM supplier_warehouses WHERE supplier_id = $1 AND is_active = TRUE
		 ORDER BY created_at ASC LIMIT 1`, supplierID).Scan(&w.ID, &w.Name, &w.TapinProvinceID, &w.TapinCityID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load supplier default warehouse geo: %w", err)
	}
	return &w, nil
}

// --- Destination geo resolution ------------------------------------

// geoTreeNode is one province node of the tapin_geo_snapshots 'tree'
// payload: [{code, title, cities: [{code, title}]}] — the shape the
// geo handlers document in customer_handlers.go.
type geoTreeNode struct {
	Code   json.RawMessage   `json:"code"`
	Title  string            `json:"title"`
	Name   string            `json:"name"`
	Cities []geoTreeCityNode `json:"cities"`
}

type geoTreeCityNode struct {
	Code  json.RawMessage `json:"code"`
	Title string          `json:"title"`
	Name  string          `json:"name"`
}

func geoCodeInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return v, true
		}
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f), true
	}
	return 0, false
}

func geoNodeName(title, name string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	return strings.TrimSpace(name)
}

// ResolveDestinationGeo maps destination province/city NAMES to
// Tapin geo ids using the platform's stored Tapin geography
// (tapin_geo_snapshots 'tree'). Returns ok=false when the snapshot
// is absent or either name is unmatched — the caller decides what an
// unmapped destination means (the quote path fails closed for
// Tapin-connected legs; it never guesses a code).
func (r *TapinRepository) ResolveDestinationGeo(ctx context.Context, provinceName, cityName string) (provinceID, cityID int, ok bool, err error) {
	var payload []byte
	qerr := r.db.QueryRowContext(ctx,
		`SELECT payload FROM tapin_geo_snapshots WHERE kind = 'tree'`).Scan(&payload)
	if qerr == sql.ErrNoRows || len(payload) == 0 {
		return 0, 0, false, nil
	}
	if qerr != nil {
		return 0, 0, false, fmt.Errorf("load tapin geo snapshot: %w", qerr)
	}
	var nodes []geoTreeNode
	if uerr := json.Unmarshal(payload, &nodes); uerr != nil {
		return 0, 0, false, nil
	}
	provinceName = strings.TrimSpace(provinceName)
	cityName = strings.TrimSpace(cityName)
	for _, node := range nodes {
		if geoNodeName(node.Title, node.Name) != provinceName {
			continue
		}
		pid, pok := geoCodeInt(node.Code)
		if !pok {
			return 0, 0, false, nil
		}
		for _, city := range node.Cities {
			if geoNodeName(city.Title, city.Name) == cityName {
				cid, cok := geoCodeInt(city.Code)
				if !cok {
					return 0, 0, false, nil
				}
				return pid, cid, true, nil
			}
		}
		return 0, 0, false, nil
	}
	return 0, 0, false, nil
}

// --- Shipment registration state -----------------------------------

// ShipmentTapinState is the Tapin-relevant slice of a supplier
// shipment row, used by the registration idempotency guard and the
// label lookup.
type ShipmentTapinState struct {
	ID                     string
	Status                 string
	Carrier                string
	TrackingNumber         string
	TapinService           string
	TapinBoxID             string
	TapinRegisterAttempted bool
	PackagingLengthMM      int
	PackagingWidthMM       int
	PackagingHeightMM      int
}

const tapinStateColumns = `id, status, carrier, tracking_number, tapin_service, tapin_box_id,
	tapin_register_attempted_at, packaging_length_mm, packaging_width_mm, packaging_height_mm`

func scanTapinState(row interface {
	Scan(dest ...interface{}) error
}) (*ShipmentTapinState, error) {
	var s ShipmentTapinState
	var carrier, tracking, service, box sql.NullString
	var attempted sql.NullTime
	var pl, pw, ph sql.NullInt64
	err := row.Scan(&s.ID, &s.Status, &carrier, &tracking, &service, &box, &attempted, &pl, &pw, &ph)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load shipment tapin state: %w", err)
	}
	s.Carrier = carrier.String
	s.TrackingNumber = tracking.String
	s.TapinService = service.String
	s.TapinBoxID = box.String
	s.TapinRegisterAttempted = attempted.Valid
	s.PackagingLengthMM = int(pl.Int64)
	s.PackagingWidthMM = int(pw.Int64)
	s.PackagingHeightMM = int(ph.Int64)
	return &s, nil
}

// GetShipmentTapinState loads the caller's shipment for an order
// (latest row, mirroring SubmitShipping's selection).
func (r *TapinRepository) GetShipmentTapinState(ctx context.Context, supplierID, orderID string) (*ShipmentTapinState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+tapinStateColumns+`
		FROM supplier_shipments
		WHERE order_id = $1 AND supplier_id = $2
		ORDER BY created_at DESC LIMIT 1`, orderID, supplierID)
	return scanTapinState(row)
}

// GetShipmentTapinStateByID loads a shipment by its own id (label
// endpoint), scoped to the owning supplier.
func (r *TapinRepository) GetShipmentTapinStateByID(ctx context.Context, supplierID, shipmentID string) (*ShipmentTapinState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+tapinStateColumns+`
		FROM supplier_shipments
		WHERE id = $1 AND supplier_id = $2`, shipmentID, supplierID)
	return scanTapinState(row)
}

// MarkTapinRegisterAttempted stamps the unknown-outcome guard
// (migration 202610250012) BEFORE an upstream registration call.
func (r *TapinRepository) MarkTapinRegisterAttempted(ctx context.Context, shipmentID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE supplier_shipments SET tapin_register_attempted_at = NOW(), updated_at = NOW() WHERE id = $1`, shipmentID)
	if err != nil {
		return fmt.Errorf("stamp tapin register attempt: %w", err)
	}
	return nil
}

// ClearTapinRegisterAttempted clears the guard after a DEFINITE
// upstream rejection (an envelope answer proves nothing was
// registered, so a retry is safe).
func (r *TapinRepository) ClearTapinRegisterAttempted(ctx context.Context, shipmentID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE supplier_shipments SET tapin_register_attempted_at = NULL, updated_at = NOW() WHERE id = $1`, shipmentID)
	if err != nil {
		return fmt.Errorf("clear tapin register attempt: %w", err)
	}
	return nil
}

// RecordTapinRegistration persists a successful registration's
// metadata and clears the unknown-outcome guard.
func (r *TapinRepository) RecordTapinRegistration(ctx context.Context, shipmentID, service, boxID string, upstreamStatus int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE supplier_shipments
		SET tapin_service = $2, tapin_box_id = $3, tapin_registered_at = NOW(),
		    tapin_last_status = $4, tapin_register_attempted_at = NULL, updated_at = NOW()
		WHERE id = $1`, shipmentID, service, boxID, strconv.Itoa(upstreamStatus))
	if err != nil {
		return fmt.Errorf("record tapin registration: %w", err)
	}
	return nil
}

// --- Registration inputs -------------------------------------------

// SupplierOrderLine is one of the caller's own order lines, with the
// catalog facts the registration payload needs.
type SupplierOrderLine struct {
	ProductID         string
	Title             string
	Quantity          int
	FinalUnitPrice    int64 // Toman, as charged on the order
	WeightGrams       int   // 0 = unrecorded; caller applies the 500g fallback
	OriginWarehouseID string
}

// ListSupplierOrderLines returns the order lines belonging to the
// supplier (products are supplier-owned), in stable order.
func (r *TapinRepository) ListSupplierOrderLines(ctx context.Context, orderID, supplierID string) ([]SupplierOrderLine, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT oi.product_id, COALESCE(p.title, ''), oi.quantity, oi.final_unit_price,
		       COALESCE(p.weight_grams, 0), COALESCE(p.origin_supplier_warehouse_id, '')
		FROM order_items oi
		JOIN products p ON p.id = oi.product_id
		WHERE oi.order_id = $1 AND p.supplier_id = $2
		ORDER BY oi.product_id ASC`, orderID, supplierID)
	if err != nil {
		return nil, fmt.Errorf("list supplier order lines: %w", err)
	}
	defer rows.Close()
	out := []SupplierOrderLine{}
	for rows.Next() {
		var l SupplierOrderLine
		if err := rows.Scan(&l.ProductID, &l.Title, &l.Quantity, &l.FinalUnitPrice, &l.WeightGrams, &l.OriginWarehouseID); err != nil {
			return nil, fmt.Errorf("scan supplier order line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
