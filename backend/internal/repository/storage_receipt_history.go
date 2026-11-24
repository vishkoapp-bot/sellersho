package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

var ErrStorageReceiptSource = errors.New("storage receipt source requires reconciliation")

type storageReceiptSource struct {
	Kind, Event, Line, Movement, Balance, Product, Variant, Supplier, Warehouse, Inbound, GoodsClass string
	Quantity                                                                                         int
	PhysicalAt, BillingAt, HeldFrom                                                                  time.Time
}

// Caller preholds the canonical supplier/product/SKU/work and source balances.
// Exact receipt events, not product-level top-ups, own the accounting cohort.
func creditQCStorageReceiptTx(ctx context.Context, tx *sql.Tx, disposition string) error {
	var source storageReceiptSource
	var prior, passed, received int
	var receiptVariant, kind string
	err := tx.QueryRowContext(ctx, `SELECT d.id,l.id,l.product_id,COALESCE(a.variant_id,l.variant_id),l.variant_id,l.source_kind,l.supplier_id,l.warehouse_id,l.shipment_id,d.passed_delta,d.cumulative_disposed-d.passed_delta-d.failed_delta,d.received_quantity,d.created_at
		FROM inbound_qc_dispositions d JOIN inbound_inventory_lines l ON l.id=d.receipt_line_id
		LEFT JOIN inbound_putaway_allocations a ON l.variant_id='' AND a.disposition_id=d.id AND a.receipt_line_id=l.id
			AND a.shipment_id=l.shipment_id AND a.product_id=l.product_id AND a.supplier_id=l.supplier_id AND a.warehouse_id=l.warehouse_id
			AND a.source_location_key='qc-accepted:'||d.id AND a.quantity=d.passed_delta
		WHERE d.id=$1`, disposition).Scan(&source.Event, &source.Line, &source.Product, &source.Variant, &receiptVariant, &kind, &source.Supplier, &source.Warehouse, &source.Inbound, &passed, &prior, &received, &source.BillingAt)
	if err != nil {
		return err
	}
	if passed <= 0 {
		return ErrStorageReceiptSource
	}
	source.Kind, source.GoodsClass, source.HeldFrom = "inbound_qc", "normal", source.BillingAt
	var existing int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(initial_quantity),0) FROM warehouse_storage_receipt_lots WHERE source_kind='inbound_qc' AND source_event_id=$1`, disposition).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		if existing != passed {
			return ErrStorageReceiptSource
		}
		return nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 AND COALESCE(variant_id::text,'')=$2 AND supplier_id=$3 AND warehouse_id=$4 AND owner_type='supplier' AND location_type='warehouse' AND status='qc_accepted' AND location_key=$5 AND quantity=$6`, source.Product, source.Variant, source.Supplier, source.Warehouse, "qc-accepted:"+disposition, passed).Scan(&source.Balance); err != nil {
		return ErrStorageReceiptSource
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,quantity,created_at FROM inventory_movements
		WHERE (reference_type='inbound_transfer' OR (reference_type='inbound_receive' AND $6='external_replenishment'
			AND $5<>'' AND from_status='external' AND from_location_key='external'))
		AND reference_id=$1 AND to_status='quarantine' AND to_location_key=$2 AND product_id=$3 AND supplier_id=$4 AND COALESCE(variant_id::text,'')=$5
		ORDER BY created_at,id COLLATE "C"`, source.Line, "inbound-line:"+source.Line, source.Product, source.Supplier, receiptVariant, kind)
	if err != nil {
		return err
	}
	var lots []storageReceiptSource
	cumulative, total := 0, 0
	for rows.Next() {
		var movement string
		var n int
		var at time.Time
		if err = rows.Scan(&movement, &n, &at); err != nil {
			rows.Close()
			return err
		}
		if n <= 0 || n > 2147483647-cumulative {
			rows.Close()
			return ErrStorageReceiptSource
		}
		low, high := cumulative, cumulative+n
		cumulative = high
		start, end := max(low, prior), min(high, prior+passed)
		if end > start {
			part := source
			part.Movement, part.PhysicalAt, part.Quantity = movement, at, end-start
			lots = append(lots, part)
			total += part.Quantity
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if total != passed || cumulative < received {
		return ErrStorageReceiptSource
	}
	// The old ASN-only FK is optional metadata. Carton identity stays in Line.
	var asn sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT id FROM inbound_shipments WHERE id=$1`, source.Inbound).Scan(&asn); err != nil && err != sql.ErrNoRows {
		return err
	}
	for _, part := range lots {
		part.Inbound = asn.String
		if err = insertStorageReceiptLotTx(ctx, tx, part); err != nil {
			return err
		}
	}
	return nil
}

// Shared legacy-shaped interface resolves one actual immutable QC event. A line
// with several accepted batches must identify the exact acceptance timestamp;
// its physical receipt allocation is still derived from recorded receipt units.
func creditStorageReceiptLotTx(ctx context.Context, tx *sql.Tx, receiptLineID, productID, supplierID, variantID, warehouseID string, qty int, physicalReceivedAt, billingStartAt time.Time) error {
	var disposition string
	rows, err := tx.QueryContext(ctx, `SELECT d.id FROM inbound_qc_dispositions d JOIN inbound_inventory_lines l ON l.id=d.receipt_line_id WHERE l.id=$1 AND l.product_id=$2 AND l.supplier_id=$3 AND l.variant_id=$4 AND l.warehouse_id=$5 AND d.passed_delta=$6 AND d.created_at=$7`, receiptLineID, productID, supplierID, variantID, warehouseID, qty, billingStartAt)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		if err = rows.Scan(&disposition); err != nil {
			rows.Close()
			return err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrStorageReceiptSource
	}
	if err = creditQCStorageReceiptTx(ctx, tx, disposition); err != nil {
		return err
	}
	// PostgreSQL timestamps have microsecond precision. Compare through the same
	// database type, rather than reject a valid Go clock with finer nanoseconds.
	var physicalMatches bool
	if err = tx.QueryRowContext(ctx, `SELECT MIN(physical_received_at) IS NOT DISTINCT FROM $2::timestamptz FROM warehouse_storage_receipt_lots WHERE source_kind='inbound_qc' AND source_event_id=$1`, disposition, physicalReceivedAt).Scan(&physicalMatches); err != nil || !physicalMatches {
		return ErrStorageReceiptSource
	}
	return nil
}

func insertStorageReceiptLotTx(ctx context.Context, tx *sql.Tx, source storageReceiptSource) error {
	if source.Quantity <= 0 || source.PhysicalAt.IsZero() || source.BillingAt.Before(source.PhysicalAt) || source.HeldFrom.Before(source.BillingAt) {
		return ErrStorageReceiptSource
	}
	var settings StorageFeeSettings
	if err := tx.QueryRowContext(ctx, `SELECT storage_fee_enabled,storage_fee_free_days,storage_fee_per_kg_day,storage_fee_per_m3_day FROM platform_settings WHERE id=1`).Scan(&settings.Enabled, &settings.FreeDays, &settings.PerKgDay, &settings.PerM3Day); err != nil {
		return err
	}
	invalidPolicy := settings.FreeDays < 0 || settings.FreeDays > 36500 || settings.PerKgDay < 0 || settings.PerKgDay > pricing.MaxTransportMoney || settings.PerM3Day < 0 || settings.PerM3Day > pricing.MaxTransportMoney
	var physical SKUPhysical
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(length_mm,0),COALESCE(width_mm,0),COALESCE(height_mm,0),COALESCE(weight_grams,0),verified,COALESCE(revision,''),source FROM inventory_effective_measurements WHERE product_id=$1 AND variant_id=$2`, source.Product, source.Variant).Scan(&physical.LengthMM, &physical.WidthMM, &physical.HeightMM, &physical.WeightGrams, &physical.Verified, &physical.Revision, &physical.Source); err != nil {
		return err
	}
	status, reason := "unresolved_measurement", "warehouse measurement was not verified at QC acceptance"
	var revision, l, w, h, g, unit any
	if physical.Verified && !invalidPolicy {
		rate, err := storageFeeUnitDayToman(int(physical.WeightGrams), int(physical.LengthMM), int(physical.WidthMM), int(physical.HeightMM), settings)
		if err != nil {
			invalidPolicy = true
		} else {
			status, reason = "verified", ""
			revision, l, w, h, g, unit = physical.Revision, physical.LengthMM, physical.WidthMM, physical.HeightMM, physical.WeightGrams, rate
		}
	}
	if invalidPolicy {
		status, reason = "unresolved_policy", "receipt-time storage tariff is invalid or exceeds the monetary bound"
		settings.FreeDays, settings.PerKgDay, settings.PerM3Day = 0, 0, 0
	}
	id := "WSP-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(source.Kind+":"+source.Event+":"+source.Movement)).String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,inbound_id,goods_class,qty,stocked_at,original_stocked_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$8)`, id, source.Product, source.Supplier, source.Warehouse, source.Inbound, source.GoodsClass, source.Quantity, source.BillingAt); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO warehouse_storage_receipt_lots(id,source_kind,source_event_id,receipt_line_id,receipt_movement_id,source_balance_id,product_id,variant_id,supplier_id,warehouse_id,initial_quantity,physical_received_at,billing_start_at,held_from,measurement_revision_id,length_mm,width_mm,height_mm,weight_grams,free_days,per_kg_day_toman,per_m3_day_toman,unit_day_toman,history_status,unresolved_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`, id, source.Kind, source.Event, source.Line, source.Movement, source.Balance, source.Product, source.Variant, source.Supplier, source.Warehouse, source.Quantity, source.PhysicalAt, source.BillingAt, source.HeldFrom, revision, l, w, h, g, settings.FreeDays, settings.PerKgDay, settings.PerM3Day, unit, status, reason)
	return err
}

// Called after return QC and conserved physical restock, in that same transaction.
// Only proven central-warehouse, sellable return items can own new fee history.
func creditCustomerReturnStorageTx(ctx context.Context, tx *sql.Tx, returnID string) error {
	var sellable bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM return_items ri JOIN order_inventory_captures c ON c.order_item_id=ri.order_item_id WHERE ri.return_id=$1 AND ri.qc_status='ok')`, returnID).Scan(&sellable); err != nil {
		return err
	}
	if !sellable {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT r.return_item_id::text,m.id,r.source_balance_id,r.product_id,r.variant_id,r.supplier_id,r.warehouse_id,r.qty,m.created_at,r.received_at FROM warehouse_customer_return_bin_sources r JOIN inventory_movements m ON m.idempotency_key=CASE WHEN r.variant_id<>'' THEN 'sku-return:'||r.return_item_id::text ELSE 'crstk:'||r.return_id::text||':'||r.return_item_id::text||':sellable' END WHERE r.return_id=$1 AND r.source_proven AND EXISTS(SELECT 1 FROM order_inventory_captures WHERE order_item_id=(SELECT order_item_id FROM return_items WHERE id=r.return_item_id)) ORDER BY r.return_item_id`, returnID)
	if err != nil {
		return err
	}
	var sources []storageReceiptSource
	for rows.Next() {
		var s storageReceiptSource
		if err = rows.Scan(&s.Event, &s.Movement, &s.Balance, &s.Product, &s.Variant, &s.Supplier, &s.Warehouse, &s.Quantity, &s.PhysicalAt, &s.BillingAt); err != nil {
			rows.Close()
			return err
		}
		s.Kind, s.GoodsClass, s.Line, s.HeldFrom = "customer_return", "returned", s.Event, s.BillingAt
		sources = append(sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, s := range sources {
		if err = insertStorageReceiptLotTx(ctx, tx, s); err != nil {
			return err
		}
	}
	return nil
}
