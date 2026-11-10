package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type WarehouseBin struct {
	PositionX       *float64  `json:"positionX,omitempty"`
	PositionY       *float64  `json:"positionY,omitempty"`
	PositionZ       *float64  `json:"positionZ,omitempty"`
	BinCode         string    `json:"binCode" db:"bin_code"`
	HallName        string    `json:"hallName" db:"hall_name"`
	Aisle           string    `json:"aisle" db:"aisle"`
	Shelf           string    `json:"shelf" db:"shelf"`
	CapacityPercent int       `json:"capacityPercent" db:"capacity_percent"`
	ProductID       *string   `json:"productId" db:"product_id"`
	ProductTitle    string    `json:"productTitle,omitempty"`
	Quantity        int       `json:"quantity" db:"quantity"`
	UpdatedAt       time.Time `json:"updatedAt" db:"updated_at"`
	WarehouseName   string    `json:"warehouseName" db:"warehouse_name"`
	CapacityVolume  int64     `json:"capacityVolumeCm3" db:"capacity_volume_cm3"`
	OccupiedVolume  int64     `json:"occupiedVolumeCm3" db:"occupied_volume_cm3"`
	ReservedVolume  int64     `json:"reservedVolumeCm3" db:"reserved_volume_cm3"`
	RemainingVolume int64     `json:"remainingVolumeCm3" db:"remaining_volume_cm3"`
	MaxWeightGrams  *int64    `json:"maxWeightGrams,omitempty" db:"max_weight_grams"`
	OccupiedWeight  int64     `json:"occupiedWeightGrams" db:"occupied_weight_grams"`
	CapacityStatus  string    `json:"capacityStatus" db:"capacity_status"`
	UnavailableWhy  string    `json:"unavailableReason,omitempty" db:"unavailable_reason"`
}

type InboundShipment struct {
	ID            string          `json:"id" db:"id"`
	SupplierID    string          `json:"supplierId" db:"supplier_id"`
	SupplierName  string          `json:"supplierName" db:"supplier_name"`
	WarehouseName string          `json:"warehouseName" db:"warehouse_name"`
	ItemsCount    int             `json:"itemsCount" db:"items_count"`
	CartonsCount  int             `json:"cartonsCount" db:"cartons_count"`
	Carrier       string          `json:"carrier" db:"carrier"`
	TruckPlate    string          `json:"truckPlate" db:"truck_plate"`
	Status        string          `json:"status" db:"status"`
	CreatedAt     time.Time       `json:"createdAt" db:"created_at"`
	ReceivedAt    *time.Time      `json:"receivedAt" db:"received_at"`
	DispatchAt    *time.Time      `json:"dispatchAt" db:"dispatch_at"`
	DispatchedAt  *time.Time      `json:"dispatchedAt" db:"dispatched_at"`
	Items         json.RawMessage `json:"items" db:"items"`
	ScannedItems  json.RawMessage `json:"scannedItems" db:"scanned_items"`
}

type ShippingManifest struct {
	ID            string     `json:"id" db:"id"`
	Carrier       string     `json:"carrier" db:"carrier"`
	DriverName    string     `json:"driverName" db:"driver_name"`
	PackagesCount int        `json:"packagesCount" db:"packages_count"`
	Status        string     `json:"status" db:"status"`
	CreatedAt     time.Time  `json:"createdAt" db:"created_at"`
	DispatchedAt  *time.Time `json:"dispatchedAt" db:"dispatched_at"`
}

type WMSRepository struct {
	db *sql.DB
}

type WarehouseOverview struct {
	WarehouseName     string `json:"warehouseName"`
	CapacityVolumeCm3 int64  `json:"capacityVolumeCm3"`
	OccupiedVolumeCm3 int64  `json:"occupiedVolumeCm3"`
	ReservedVolumeCm3 int64  `json:"reservedVolumeCm3"`
	CapacityPercent   int    `json:"capacityPercent"`
	InboundPending    int    `json:"inboundPending"`
	VerifiedMovements int    `json:"verifiedMovements"`
}

func NewWMSRepository(db *sql.DB) *WMSRepository {
	return &WMSRepository{db: db}
}

func (r *WMSRepository) GetOverview(ctx context.Context) (*WarehouseOverview, error) {
	var overview WarehouseOverview
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(warehouse_name),'انبار مرکزی تهران'),COALESCE(SUM(capacity_volume_cm3),0),COALESCE(SUM(occupied_volume_cm3),0),
		COALESCE((SELECT SUM(volume_cm3) FROM putaway_capacity_reservations WHERE status='active'),0),
		COALESCE((SELECT COUNT(*) FROM inbound_shipments WHERE status IN ('pending','in_transit','receiving')),0),
		COALESCE((SELECT COUNT(*) FROM inventory_movements WHERE reference_type IN ('inbound_received','qc_pass','putaway_confirmed')),0)
		FROM warehouse_bins`).Scan(&overview.WarehouseName, &overview.CapacityVolumeCm3, &overview.OccupiedVolumeCm3, &overview.ReservedVolumeCm3, &overview.InboundPending, &overview.VerifiedMovements)
	if err != nil {
		return nil, err
	}
	denominator := overview.CapacityVolumeCm3
	if denominator > 0 {
		overview.CapacityPercent = int((overview.OccupiedVolumeCm3 + overview.ReservedVolumeCm3) * 100 / denominator)
	}
	return &overview, nil
}

func (r *WMSRepository) GetBins(ctx context.Context) ([]WarehouseBin, error) {
	query := `
		SELECT b.bin_code, b.hall_name, b.aisle, b.shelf, b.capacity_percent,
		       b.product_id, COALESCE(p.title, ''), b.quantity, b.updated_at,
		       COALESCE(b.warehouse_name,''), COALESCE(b.capacity_volume_cm3,0),
		       COALESCE(b.occupied_volume_cm3,0), COALESCE((SELECT SUM(volume_cm3) FROM putaway_capacity_reservations r WHERE r.bin_code=b.bin_code AND r.status='active'),0),
		       COALESCE(b.capacity_volume_cm3,0)-COALESCE(b.occupied_volume_cm3,0)-COALESCE((SELECT SUM(volume_cm3) FROM putaway_capacity_reservations r WHERE r.bin_code=b.bin_code AND r.status='active'),0),
		       b.max_weight_grams, COALESCE(b.occupied_weight_grams,0), b.capacity_status,
		       CASE WHEN b.capacity_status <> 'verified' THEN 'ظرفیت فیزیکی تأیید نشده' WHEN COALESCE(b.capacity_volume_cm3,0)-COALESCE(b.occupied_volume_cm3,0)-COALESCE((SELECT SUM(volume_cm3) FROM putaway_capacity_reservations r WHERE r.bin_code=b.bin_code AND r.status='active'),0) <= 0 THEN 'ظرفیت باقی‌مانده کافی نیست' ELSE '' END, b.position_x,b.position_y,b.position_z
		FROM warehouse_bins b
		LEFT JOIN products p ON b.product_id = p.id
		ORDER BY b.bin_code ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bins []WarehouseBin
	for rows.Next() {
		var b WarehouseBin
		if err := rows.Scan(
			&b.BinCode, &b.HallName, &b.Aisle, &b.Shelf, &b.CapacityPercent,
			&b.ProductID, &b.ProductTitle, &b.Quantity, &b.UpdatedAt, &b.WarehouseName, &b.CapacityVolume, &b.OccupiedVolume, &b.ReservedVolume, &b.RemainingVolume, &b.MaxWeightGrams, &b.OccupiedWeight, &b.CapacityStatus, &b.UnavailableWhy,
			&b.PositionX, &b.PositionY, &b.PositionZ,
		); err != nil {
			return nil, err
		}
		bins = append(bins, b)
	}
	return bins, nil
}

func (r *WMSRepository) UpdateBinCapacity(ctx context.Context, binCode, actor string, capacityVolume, maxWeight int64, verify bool) error {
	if capacityVolume <= 0 {
		return fmt.Errorf("capacity volume must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var occupied, reserved int64
	if err := tx.QueryRowContext(ctx, `SELECT occupied_volume_cm3,COALESCE((SELECT SUM(volume_cm3) FROM putaway_capacity_reservations r WHERE r.bin_code=$1 AND r.status='active'),0) FROM warehouse_bins WHERE bin_code=$1 FOR UPDATE`, binCode).Scan(&occupied, &reserved); err != nil {
		return err
	}
	if capacityVolume < occupied+reserved {
		return fmt.Errorf("capacity cannot be below occupied and reserved volume")
	}
	status := "unverified"
	if verify {
		status = "verified"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE warehouse_bins SET capacity_volume_cm3=$1,max_weight_grams=NULLIF($2,0),capacity_status=$3,updated_at=NOW() WHERE bin_code=$4`, capacityVolume, maxWeight, status, binCode); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id,timestamp,operator,action,category,details) VALUES ($1,NOW(),$2,$3,'warehouse_capacity',$4)`, "LOG-CAP-"+fmt.Sprintf("%d", time.Now().UnixNano()), actor, "update_bin_capacity", fmt.Sprintf(`{"binCode":"%s","capacityVolume":%d,"maxWeight":%d,"status":"%s"}`, binCode, capacityVolume, maxWeight, status)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *WMSRepository) GetInboundShipments(ctx context.Context) ([]InboundShipment, error) {
	query := `SELECT id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count, carrier, truck_plate, status, created_at, received_at, COALESCE(items,'[]'::jsonb), COALESCE(scanned_items,'[]'::jsonb) FROM inbound_shipments ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []InboundShipment
	for rows.Next() {
		var s InboundShipment
		if err := rows.Scan(&s.ID, &s.SupplierID, &s.SupplierName, &s.WarehouseName, &s.ItemsCount, &s.CartonsCount, &s.Carrier, &s.TruckPlate, &s.Status, &s.CreatedAt, &s.ReceivedAt, &s.Items, &s.ScannedItems); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
}

func (r *WMSRepository) GetManifests(ctx context.Context) ([]ShippingManifest, error) {
	query := `SELECT id, carrier, driver_name, packages_count, status, created_at, dispatched_at FROM shipping_manifests ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ShippingManifest
	for rows.Next() {
		var m ShippingManifest
		if err := rows.Scan(&m.ID, &m.Carrier, &m.DriverName, &m.PackagesCount, &m.Status, &m.CreatedAt, &m.DispatchedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, nil
}

func (r *WMSRepository) DispatchManifest(ctx context.Context, id string) error {
	now := time.Now()
	_, err := r.db.ExecContext(ctx, `UPDATE shipping_manifests SET status = 'handed_over', dispatched_at = $1 WHERE id = $2`, now, id)
	return err
}
