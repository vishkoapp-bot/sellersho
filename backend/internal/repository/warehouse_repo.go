package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type WarehouseInbound struct {
	ID                 string          `json:"id"`
	SupplierID         string          `json:"supplierId"`
	SupplierName       string          `json:"supplierName"`
	WarehouseName      string          `json:"warehouseName"`
	ItemsCount         int             `json:"itemsCount"`
	CartonsCount       int             `json:"cartonsCount"`
	Carrier            string          `json:"carrier"`
	TruckPlate         string          `json:"truckPlate"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"createdAt"`
	StartedReceivingAt *time.Time      `json:"startedReceivingAt,omitempty"`
	CompletedAt        *time.Time      `json:"completedAt,omitempty"`
	Items              json.RawMessage `json:"items"`
	ScannedItems       json.RawMessage `json:"scannedItems"`
}

type QCQueueItem struct {
	ReconciliationRequired bool      `json:"reconciliationRequired"`
	ReceiptLineID          string    `json:"receiptLineId"`
	VariantID              string    `json:"variantId,omitempty"`
	PassedQuantity         int       `json:"passedQuantity"`
	FailedQuantity         int       `json:"failedQuantity"`
	ShortageQuantity       int       `json:"shortageQuantity"`
	RemainingQuantity      int       `json:"remainingQuantity"`
	ID                     string    `json:"id"`
	ProductID              string    `json:"productId"`
	ProductTitle           string    `json:"productTitle"`
	InboundID              string    `json:"inboundId"`
	SupplierName           string    `json:"supplierName"`
	ExpectedQuantity       int       `json:"expectedQuantity"`
	ReceivedQuantity       int       `json:"receivedQuantity"`
	CreatedAt              time.Time `json:"createdAt"`
}

type PutawayQueueItem struct {
	ReconciliationRequired bool      `json:"reconciliationRequired"`
	ID                     string    `json:"id"`
	ProductID              string    `json:"productId"`
	VariantID              string    `json:"variantId,omitempty"`
	VariantName            string    `json:"variantName,omitempty"`
	VariantSPNC            string    `json:"variantSpnc,omitempty"`
	Title                  string    `json:"title"`
	InboundID              string    `json:"inboundId"`
	BinLocation            string    `json:"binLocation"`
	Quantity               int       `json:"quantity"`
	Status                 string    `json:"status"`
	CreatedAt              time.Time `json:"createdAt"`
	LengthMM               int       `json:"lengthMm"`
	WidthMM                int       `json:"widthMm"`
	HeightMM               int       `json:"heightMm"`
	WeightGrams            int       `json:"weightGrams"`
	MeasurementStatus      string    `json:"measurementStatus"`
	MeasurementRevision    string    `json:"measurementRevision,omitempty"`
	MeasurementSource      string    `json:"measurementSource"`
}

type ReadyToPickOrder struct {
	ID                string          `json:"id"`
	OrderNumber       string          `json:"orderNumber"`
	StoreName         string          `json:"storeName"`
	CustomerName      string          `json:"customerName"`
	TotalAmount       int64           `json:"totalAmount"`
	Status            string          `json:"status"`
	FulfillmentMethod string          `json:"fulfillmentMethod"`
	Items             json.RawMessage `json:"items"`
	CreatedAt         time.Time       `json:"createdAt"`
	// Invoice/label enrichment (additive): the order's real money figures and
	// the recipient snapshot, so the packing desk can print a real branded
	// invoice and a complete shipping label from the queue DTO alone. The
	// recipient fields are the same customer_* snapshot columns the full
	// order DTO exposes on internal paths; warehouse staff ship to this
	// address, so no extra masking applies here.
	DiscountAmount     int64  `json:"discountAmount"`
	ShippingCost       int64  `json:"shippingCost"`
	PayableAmount      int64  `json:"payableAmount"`
	PaymentStatus      string `json:"paymentStatus"`
	CustomerPhone      string `json:"customerPhone"`
	CustomerProvince   string `json:"customerProvince"`
	CustomerCity       string `json:"customerCity"`
	CustomerAddress    string `json:"customerAddress"`
	CustomerPostalCode string `json:"customerPostalCode"`
	TrackingCode       string `json:"trackingCode"`
}

type WarehouseRepository struct {
	db *sql.DB
}

func NewWarehouseRepository(db *sql.DB) *WarehouseRepository {
	return &WarehouseRepository{db: db}
}

// GetPendingInbounds returns shipments waiting to be received
func (r *WarehouseRepository) GetPendingInbounds(ctx context.Context) ([]WarehouseInbound, error) {
	query := `
		SELECT id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count,
		       COALESCE(carrier, ''), COALESCE(truck_plate, ''), status, created_at,
		       started_receiving_at, completed_at,
		       COALESCE(items, '[]'::jsonb), COALESCE(scanned_items, '[]'::jsonb)
		FROM inbound_shipments
		WHERE status IN ('pending', 'in_transit')
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []WarehouseInbound
	for rows.Next() {
		var s WarehouseInbound
		if err := rows.Scan(
			&s.ID, &s.SupplierID, &s.SupplierName, &s.WarehouseName, &s.ItemsCount, &s.CartonsCount,
			&s.Carrier, &s.TruckPlate, &s.Status, &s.CreatedAt,
			&s.StartedReceivingAt, &s.CompletedAt,
			&s.Items, &s.ScannedItems,
		); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
}

// StartReceiving transitions inbound status to 'receiving'. The status
// predicate is fail-closed: an inbound that is already 'received' can never
// be regressed back to 'receiving' (that would reopen a closed receipt and
// risk double-crediting inventory on a second completion).
func (r *WarehouseRepository) StartReceiving(ctx context.Context, inboundID, staffUserID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx, `
		UPDATE inbound_shipments
		SET status = 'receiving', started_receiving_at = $1
		WHERE id = $2 AND status IN ('pending', 'in_transit', 'receiving')
	`, now, inboundID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("محموله ورودی یافت نشد یا دریافت آن قبلاً تکمیل شده است: %s", inboundID)
	}

	logID := fmt.Sprintf("LOG-INB-%d", time.Now().UnixMilli()%100000)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO inbound_receiving_logs (id, inbound_id, staff_user_id, action, created_at)
		VALUES ($1, $2, $3, 'started', $4)
	`, logID, inboundID, staffUserID, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ScanItem records a barcode/serial scan against an inbound shipment.
// Hardened (Fix 7): the scan is reconciled against the declared manifest —
// the product must be a declared line of this inbound and the quantity must
// be positive. Decorative scans of undeclared products are rejected
// fail-closed instead of being silently appended.
func (r *WarehouseRepository) ScanItem(ctx context.Context, inboundID string, itemData map[string]interface{}, staffUserID string) (int, error) {
	productID, _ := itemData["productId"].(string)
	if productID == "" {
		productID, _ = itemData["product_id"].(string)
	}
	quantity := int(asFloat(itemData["quantity"]))
	if strings.TrimSpace(productID) == "" {
		return 0, fmt.Errorf("شناسه کالا در اسکن نامعتبر است")
	}
	if quantity <= 0 {
		return 0, fmt.Errorf("تعداد اسکن‌شده باید مثبت باشد")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var status string
	var items []byte
	if err := tx.QueryRowContext(ctx, `SELECT status, COALESCE(items,'[]'::jsonb) FROM inbound_shipments WHERE id=$1 FOR UPDATE`, inboundID).Scan(&status, &items); err == sql.ErrNoRows {
		return 0, fmt.Errorf("محموله ورودی یافت نشد: %s", inboundID)
	} else if err != nil {
		return 0, err
	}
	if status != "receiving" && status != "pending" && status != "in_transit" {
		return 0, fmt.Errorf("این محموله در وضعیت «%s» قابل اسکن نیست", status)
	}
	declared := map[string]bool{}
	var entries []struct {
		ProductID       string `json:"productId"`
		ProductIDLegacy string `json:"product_id"`
	}
	if err := json.Unmarshal(items, &entries); err != nil {
		return 0, err
	}
	for _, e := range entries {
		pid := e.ProductID
		if pid == "" {
			pid = e.ProductIDLegacy
		}
		if pid != "" {
			declared[pid] = true
		}
	}
	if !declared[productID] {
		return 0, fmt.Errorf("کالای اسکن‌شده در مانیفست این محموله نیست: %s", productID)
	}

	itemBytes, err := json.Marshal(itemData)
	if err != nil {
		return 0, err
	}

	logID := fmt.Sprintf("SCAN-%d", time.Now().UnixMilli()%1000000)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO inbound_receiving_logs (id, inbound_id, staff_user_id, action, scanned_item, created_at)
		VALUES ($1, $2, $3, 'item_scanned', $4, NOW())
	`, logID, inboundID, staffUserID, itemBytes)
	if err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE inbound_shipments
		SET scanned_items = COALESCE(scanned_items, '[]'::jsonb) || $1::jsonb
		WHERE id = $2
	`, itemBytes, inboundID)
	if err != nil {
		return 0, err
	}

	var count int
	_ = tx.QueryRowContext(ctx, `
		SELECT jsonb_array_length(COALESCE(scanned_items, '[]'::jsonb))
		FROM inbound_shipments
		WHERE id = $1
	`, inboundID).Scan(&count)

	return count, tx.Commit()
}

// CompleteInbound completes inbound receiving and queues items for QC
func (r *WarehouseRepository) CompleteInbound(ctx context.Context, inboundID, staffUserID string) error {
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := lockInboundShipmentTx(ctx, tx, inboundID); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM inbound_shipments WHERE id=$1`, inboundID).Scan(&current); err != nil {
		return err
	}
	if current != "receiving" && current != "pending" && current != "in_transit" && current != "received" {
		return ErrInboundProvenance
	}
	var before int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(received_quantity),0) FROM inbound_inventory_lines WHERE shipment_id=$1`, inboundID).Scan(&before); err != nil {
		return err
	}
	if err := applyInboundInventoryTx(ctx, tx, inboundID, staffUserID); err != nil {
		return err
	}
	var outstanding, received int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(dispatched_quantity-received_quantity),0),COALESCE(SUM(received_quantity),0) FROM inbound_inventory_lines WHERE shipment_id=$1`, inboundID).Scan(&outstanding, &received); err != nil {
		return err
	}
	status := "received"
	if outstanding > 0 {
		status = "receiving"
	}
	// A completed replay has already validated every cumulative quantity above.
	// Acknowledge without rewriting timestamps or appending another completion.
	if before == received && ((current == "received" && outstanding == 0) || (current == "receiving" && outstanding > 0)) {
		return tx.Commit()
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE inbound_shipments SET status=$2::text,completed_at=CASE WHEN $2::text='received' THEN COALESCE(completed_at,$3) ELSE completed_at END WHERE id=$1`, inboundID, status, now); err != nil {
		return err
	}

	logID := fmt.Sprintf("LOG-INB-%d", time.Now().UnixMilli()%100000)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO inbound_receiving_logs (id, inbound_id, staff_user_id, action, created_at)
		VALUES ($1, $2, $3, 'completed', $4)
	`, logID, inboundID, staffUserID, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetQCQueue returns items pending quality check inspection
func (r *WarehouseRepository) GetQCQueue(ctx context.Context) ([]QCQueueItem, error) {
	query := `
        WITH queue(id,product_id,title,inbound_id,supplier_name,expected,received,created_at,line_id,variant_id,passed,failed,shortage,remaining,reconciliation_required) AS (
            SELECT l.id,l.product_id,COALESCE(p.title,''),l.shipment_id,h.supplier_name,
                   l.quantity,l.received_quantity,h.created_at,l.id,l.variant_id,
                   COALESCE(d.quantity_passed,0),COALESCE(d.quantity_failed,0),
                   COALESCE(d.quantity_shortage,0),l.received_quantity-COALESCE(d.cumulative_disposed,0),
                   EXISTS(SELECT 1 FROM qc_results q WHERE q.inbound_id=l.shipment_id AND q.product_id=l.product_id AND NOT EXISTS(SELECT 1 FROM inbound_qc_dispositions evidence WHERE evidence.id=q.id))
            FROM inbound_inventory_lines l
            JOIN products p ON p.id=l.product_id
            JOIN (
                SELECT id::text AS id,COALESCE(supplier_name,'') AS supplier_name,created_at FROM inbound_shipments WHERE status IN ('receiving','received')
                UNION ALL
                SELECT id::text,'' AS supplier_name,created_at FROM shipments WHERE status IN ('received','partially_received')
            ) h ON h.id=l.shipment_id
            LEFT JOIN LATERAL (
                SELECT quantity_passed,quantity_failed,quantity_shortage,cumulative_disposed
                FROM inbound_qc_dispositions WHERE receipt_line_id=l.id ORDER BY cumulative_disposed DESC LIMIT 1
            ) d ON TRUE
            WHERE l.received_quantity>COALESCE(d.cumulative_disposed,0)
            UNION ALL
            -- Preserve unallocated history as an explicit, unactionable exception.
            -- Scan quantities are evidence to review, never stock to credit.
            SELECT 'legacy:'||i.id||':'||COALESCE(item->>'productId',item->>'product_id'),
                   COALESCE(item->>'productId',item->>'product_id'),COALESCE(p.title,''),i.id,COALESCE(i.supplier_name,''),
                   COALESCE((item->>'quantity')::int,0),scanned.received_quantity,i.created_at,'','',0,0,0,scanned.received_quantity,TRUE
            FROM inbound_shipments i
            CROSS JOIN LATERAL jsonb_array_elements(COALESCE(i.items,'[]'::jsonb)) item
            LEFT JOIN products p ON p.id=COALESCE(item->>'productId',item->>'product_id')
            CROSS JOIN LATERAL (
                SELECT COALESCE(SUM(COALESCE((scan->>'quantity')::int,0)),0)::int AS received_quantity
                FROM jsonb_array_elements(COALESCE(i.scanned_items,'[]'::jsonb)) scan
                WHERE COALESCE(scan->>'productId',scan->>'product_id')=COALESCE(item->>'productId',item->>'product_id')
            ) scanned
            WHERE i.status IN ('receiving','received') AND NOT EXISTS(
                SELECT 1 FROM inbound_inventory_lines l WHERE l.shipment_id=i.id AND l.product_id=COALESCE(item->>'productId',item->>'product_id'))
            UNION ALL
            SELECT 'legacy:'||s.id::text||':'||si.variant_id::text,COALESCE(v.product_id,''),COALESCE(p.title,''),s.id::text,'',
                   si.expected_qty,si.received_qty,s.created_at,'',si.variant_id::text,0,0,0,si.received_qty,TRUE
            FROM shipments s JOIN shipment_items si ON si.shipment_id=s.id
            LEFT JOIN product_variants v ON v.id=si.variant_id LEFT JOIN products p ON p.id=v.product_id
            WHERE s.status IN ('received','partially_received') AND si.received_qty>0 AND NOT EXISTS(
                SELECT 1 FROM inbound_inventory_lines l WHERE l.shipment_id=s.id::text AND l.variant_id=si.variant_id::text)
        )
        SELECT * FROM queue ORDER BY created_at DESC,id COLLATE "C" LIMIT 20
    `
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []QCQueueItem
	for rows.Next() {
		var item QCQueueItem
		if err := rows.Scan(
			&item.ID, &item.ProductID, &item.ProductTitle,
			&item.InboundID, &item.SupplierName, &item.ExpectedQuantity, &item.ReceivedQuantity, &item.CreatedAt,
			&item.ReceiptLineID, &item.VariantID, &item.PassedQuantity, &item.FailedQuantity, &item.ShortageQuantity, &item.RemainingQuantity, &item.ReconciliationRequired,
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, nil
}

// RecordQCPass accepts cumulative all-passed counts through the same durable
// receipt-line identity as CompleteQC. Accepted stock awaits exact putaway.
func (r *WarehouseRepository) RecordQCPass(ctx context.Context, productID, inboundID, inspectorUserID string, qty int, receiptLineID ...string) error {
	return r.recordInboundDisposition(ctx, productID, inboundID, inspectorUserID, qty, 0, 0, "", receiptLineID...)
}

// CompleteQC accepts cumulative disposition counts, never a second source debit.
func (r *WarehouseRepository) CompleteQC(ctx context.Context, productID, inboundID, inspectorUserID string, passed, failed, shortage int, reason string, receiptLineID ...string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("QC reason is required")
	}
	return r.recordInboundDisposition(ctx, productID, inboundID, inspectorUserID, passed, failed, shortage, strings.TrimSpace(reason), receiptLineID...)
}

func (r *WarehouseRepository) recordInboundDisposition(ctx context.Context, productID, shipmentID, actor string, passed, failed, shortage int, reason string, receiptLineID ...string) error {
	if len(receiptLineID) > 1 || (len(receiptLineID) == 1 && strings.TrimSpace(receiptLineID[0]) == "") {
		return ErrInboundProvenance
	}
	if strings.TrimSpace(productID) == "" || strings.TrimSpace(shipmentID) == "" || passed < 0 || failed < 0 || shortage < 0 || int64(passed)+int64(failed) <= 0 || int64(passed)+int64(failed) > 2147483647 || int64(shortage) > 2147483647 {
		return ErrInboundProvenance
	}
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockInboundShipmentTx(ctx, tx, shipmentID); err != nil {
		return err
	}
	lines, err := inboundLinesTx(ctx, tx, shipmentID)
	if err != nil {
		return err
	}
	// The legacy endpoint names a product, not a SKU. Never spread a QC
	// decision across multiple SKU lines or infer one from today's catalog.
	var selected *inboundPersistedLine
	for i := range lines {
		if lines[i].Product != productID || (len(receiptLineID) == 1 && lines[i].ID != receiptLineID[0]) {
			continue
		}
		if selected != nil {
			return ErrInboundProvenance
		}
		selected = &lines[i]
	}
	if selected == nil {
		return ErrInboundProvenance
	}
	if err := dispositionInboundLineTx(ctx, tx, *selected, actor, passed, failed, shortage, reason); err != nil {
		return err
	}
	return tx.Commit()
}

func applyInboundInventoryTx(ctx context.Context, tx *sql.Tx, inboundID, actor string) error {
	lines, err := inboundLinesTx(ctx, tx, inboundID)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return ErrInboundProvenance
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(scanned_items,'[]'::jsonb) FROM inbound_shipments WHERE id=$1`, inboundID).Scan(&raw); err != nil {
		return err
	}
	var scans []struct {
		Product  string `json:"productId"`
		Legacy   string `json:"product_id"`
		Quantity int    `json:"quantity"`
	}
	if err := json.Unmarshal(raw, &scans); err != nil {
		return err
	}
	quantities := map[string]int{}
	known := map[string]bool{}
	for _, l := range lines {
		if l.Variant != "" {
			return ErrInboundProvenance
		}
		known[l.Product] = true
	}
	for _, scan := range scans {
		pid := scan.Product
		if pid == "" {
			pid = scan.Legacy
		}
		if !known[pid] || scan.Quantity <= 0 || scan.Quantity > 2147483647-quantities[pid] {
			return ErrInboundProvenance
		}
		quantities[pid] += scan.Quantity
	}
	if len(quantities) == 0 {
		return ErrInboundProvenance
	}
	for _, l := range lines {
		if err := receiveInboundLineTx(ctx, tx, l.ID, quantities[l.Product], actor); err != nil {
			return err
		}
	}
	return nil
}

// fileInboundShortageTx records an open QC discrepancy for a declared-vs-
// scanned shortfall found at inbound completion. The missing units were NOT
// credited to inventory; this row is the paper trail.
func fileInboundShortageTx(ctx context.Context, tx *sql.Tx, inboundID, supplierID, productID string, expected, received int) error {
	id := "DISP-QC-" + uuid.NewString()[:8]
	_, err := tx.ExecContext(ctx, `
		INSERT INTO qc_discrepancies (id, inbound_id, supplier_id, product_id, expected_quantity, received_quantity, defect_count, reason, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 0, $7, 'open', NOW())
	`, id, inboundID, supplierID, productID, expected, received, "کسری دریافتی نسبت به مانیفست (ثبت خودکار هنگام تکمیل دریافت)")
	return err
}

// ensureCanonicalCoverageTx migrates a legacy product into the canonical
// inventory ledger on its first warehouse receipt. The product's existing
// products.stock is seeded as a reconciled sellable balance (attributed to
// the receiving supplier and clearly marked as a legacy migration), with a
// matching audit movement. Idempotent: a second call is a no-op because the
// product is already covered.
func ensureCanonicalCoverageTx(ctx context.Context, tx *sql.Tx, productID, supplierID string) error {
	covered, err := canonicalCoveredTx(ctx, tx, productID)
	if err != nil || covered {
		return err
	}
	var legacyStock int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(stock,0) FROM products WHERE id=$1`, productID).Scan(&legacyStock); err != nil {
		return err
	}
	if legacyStock <= 0 {
		return nil
	}
	movementID := "IM-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
		VALUES ($1,$2,$3,$4,'legacy','sellable','legacy_stock','warehouse:legacy_migration','legacy_stock_migration',$5,$6,'system','{}'::jsonb)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		movementID, productID, supplierID, legacyStock, productID, "legacy-migrate:"+productID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled)
		VALUES ($1,$2,$3,'supplier','warehouse','warehouse:legacy_migration','sellable',$4,TRUE)
		ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO NOTHING`,
		"INV-"+uuid.NewString(), productID, supplierID, legacyStock)
	return err
}

func upsertInventoryMovementTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, quantity int, fromStatus, toStatus, fromLocation, toLocation, referenceType, referenceID, idempotency, actor, metadataFormat string, metadataArgs ...interface{}) error {
	if quantity <= 0 {
		return nil
	}
	metadata := fmt.Sprintf(metadataFormat, metadataArgs...)
	result, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, quantity, fromStatus, toStatus, fromLocation, toLocation, referenceType, referenceID, idempotency, actor, metadata)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier',CASE WHEN $4::text LIKE 'supplier_warehouse:%' THEN 'supplier' ELSE 'warehouse' END,$4,$5,$6,TRUE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, toLocation, toStatus, quantity)
	return err
}

func moveInventoryTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, quantity int, fromStatus, toStatus, fromLocation, toLocation, referenceType, referenceID, idempotency, actor, metadataFormat string, metadataArgs ...interface{}) error {
	if quantity <= 0 {
		return nil
	}
	if strings.HasPrefix(toLocation, "bin:") {
		var matches bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_balances s JOIN warehouse_bins b ON b.warehouse_id=s.warehouse_id WHERE s.product_id=$1 AND s.supplier_id=$2 AND s.variant_id IS NULL AND s.location_key=$3 AND s.status=$4 AND b.bin_code=$5 AND s.warehouse_id IS NOT NULL)`, productID, supplierID, fromLocation, fromStatus, strings.TrimPrefix(toLocation, "bin:")).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("putaway requires matching immutable warehouse identities")
		}
	}
	metadata := fmt.Sprintf(metadataFormat, metadataArgs...)
	result, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, quantity, fromStatus, toStatus, fromLocation, toLocation, referenceType, referenceID, idempotency, actor, metadata)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return err
	}
	var sourceID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 AND supplier_id=$2 AND variant_id IS NULL AND location_key=$3 AND status=$4 FOR UPDATE`, productID, supplierID, fromLocation, fromStatus).Scan(&sourceID); err != nil {
		return fmt.Errorf("canonical source balance unavailable: %w", err)
	}
	result, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE id=$2 AND quantity >= $1`, quantity, sourceID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return fmt.Errorf("insufficient canonical source balance")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier',CASE WHEN $4::text LIKE 'supplier_warehouse:%' THEN 'supplier' ELSE 'warehouse' END,$4,$5,$6,TRUE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, toLocation, toStatus, quantity)
	if err != nil {
		return err
	}
	// Unified inventory: keep the products.stock projection in sync with the
	// canonical balances (previously warehouse moves never updated it).
	return syncProductStockTx(ctx, tx, productID)
}

// RecordQCFail records quality defect
func (r *WarehouseRepository) RecordQCFail(ctx context.Context, productID, inboundID, inspectorUserID, reason string, qty int) error {
	// This obsolete endpoint cannot identify a cumulative pass/fail partition or
	// exact receipt line. CompleteQC records failures through conserved disposition.
	return ErrInboundLifecycleClosed
}

// ErrQCDiscrepancyMissingIdentity is returned when a QC discrepancy is filed
// without a supplier or product identity. Disputes are financial/legal
// records: they must never accuse a guessed supplier/product (audit C8).
var ErrQCDiscrepancyMissingIdentity = errors.New("supplierId و productId برای ثبت مغایرت کیفی الزامی است")

// CreateQCDiscrepancy creates formal dispute record for supplier
func (r *WarehouseRepository) CreateQCDiscrepancy(ctx context.Context, d map[string]interface{}) (string, error) {
	id := fmt.Sprintf("DISP-QC-%d", time.Now().UnixMilli()%100000)
	inboundID, _ := d["inboundId"].(string)
	supplierID, _ := d["supplierId"].(string)
	if supplierID == "" {
		return "", fmt.Errorf("%w: supplierId", ErrQCDiscrepancyMissingIdentity)
	}
	productID, _ := d["productId"].(string)
	if productID == "" {
		return "", fmt.Errorf("%w: productId", ErrQCDiscrepancyMissingIdentity)
	}
	expectedQty := int(asFloat(d["expectedQuantity"]))
	receivedQty := int(asFloat(d["receivedQuantity"]))
	defectCount := int(asFloat(d["defectCount"]))
	reason, _ := d["reason"].(string)
	if reason == "" {
		reason = "مغایرت کیفی و شکستگی اقلام"
	}

	query := `
		INSERT INTO qc_discrepancies (id, inbound_id, supplier_id, product_id, expected_quantity, received_quantity, defect_count, reason, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'open', NOW())
	`
	_, err := r.db.ExecContext(ctx, query, id, inboundID, supplierID, productID, expectedQty, receivedQty, defectCount, reason)
	if err != nil {
		return "", err
	}
	return id, nil
}

// GetPutawayQueue returns putaway tasks
func (r *WarehouseRepository) GetPutawayQueue(ctx context.Context) ([]PutawayQueueItem, error) {
	query := `
		SELECT pt.id, pt.product_id, COALESCE(p.title, 'کالای بدون عنوان'),
		       COALESCE(pt.inbound_id, ''), COALESCE(pt.bin_location, ''),
		       pt.quantity, pt.status, pt.created_at, COALESCE(m.length_mm,0), COALESCE(m.width_mm,0), COALESCE(m.height_mm,0), COALESCE(m.weight_grams,0), COALESCE(m.measurement_status,'unverified'),
		       COALESCE(pt.variant_id::text,''), COALESCE(pv.variant_name,''), COALESCE(pv.spnc,''),
		       NOT EXISTS(SELECT 1 FROM inbound_putaway_allocations a WHERE a.task_id=pt.id) OR m.product_id IS NULL,COALESCE(m.revision,''),COALESCE(m.source,'')
		FROM putaway_tasks pt
		LEFT JOIN products p ON pt.product_id = p.id
		LEFT JOIN product_variants pv ON pv.id=pt.variant_id
		LEFT JOIN inventory_effective_measurements m ON m.product_id=pt.product_id AND m.variant_id=COALESCE(pt.variant_id::text,'')
		WHERE pt.status IN ('pending', 'assigned')
		ORDER BY pt.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PutawayQueueItem
	for rows.Next() {
		var item PutawayQueueItem
		if err := rows.Scan(
			&item.ID, &item.ProductID, &item.Title,
			&item.InboundID, &item.BinLocation, &item.Quantity, &item.Status, &item.CreatedAt, &item.LengthMM, &item.WidthMM, &item.HeightMM, &item.WeightGrams, &item.MeasurementStatus,
			&item.VariantID, &item.VariantName, &item.VariantSPNC,
			&item.ReconciliationRequired,
			&item.MeasurementRevision, &item.MeasurementSource,
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// Accepted history without a task remains visible but cannot be guessed into
	// a new physical placement. Counts here are recorded evidence, not availability.
	history, err := r.db.QueryContext(ctx, `SELECT r.task_id,r.product_id,COALESCE(p.title,''),r.quantity FROM inbound_putaway_reconciliation r LEFT JOIN products p ON p.id=r.product_id WHERE r.exception='accepted_history_without_exact_task' ORDER BY r.task_id COLLATE "C" LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer history.Close()
	for history.Next() {
		item := PutawayQueueItem{ReconciliationRequired: true, Status: "reconciliation_required"}
		if err = history.Scan(&item.ID, &item.ProductID, &item.Title, &item.Quantity); err != nil {
			return nil, err
		}
		item.ID = "reconcile:" + item.ID
		list = append(list, item)
	}
	if err = history.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (r *WarehouseRepository) AssignPutawayTask(ctx context.Context, taskID, binLocation, staffUserID string) error {
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(binLocation) == "" || strings.TrimSpace(staffUserID) == "" {
		return fmt.Errorf("task, bin and actor are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPutawayParentsTx(ctx, tx, taskID); err != nil {
		return err
	}
	var productID, variantID, status, sourceWarehouse string
	var quantity int
	if err := tx.QueryRowContext(ctx, `SELECT pt.product_id,pt.quantity,pt.status,COALESCE(pt.variant_id::text,''),COALESCE((SELECT w.name FROM inbound_putaway_allocations a JOIN warehouses w ON w.id=a.warehouse_id WHERE a.task_id=pt.id),(SELECT warehouse_name FROM inbound_shipments WHERE id=pt.inbound_id),p.warehouse_name,'') FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1 FOR UPDATE OF pt,p`, taskID).Scan(&productID, &quantity, &status, &variantID, &sourceWarehouse); err != nil {
		return err
	}
	if status == "completed" {
		return tx.Commit()
	}
	physical, err := loadEffectiveSKUPhysicalTx(ctx, tx, productID, variantID)
	if err != nil {
		return err
	}
	if !physical.Verified {
		return ErrSKUPhysicalUnverified
	}
	volume, requiredWeight, err := warehouseopt.Capacity(physical.LengthMM, physical.WidthMM, physical.HeightMM, physical.WeightGrams, int64(quantity))
	if err != nil {
		return err
	}
	var capacity, occupied, maxWeight, occupiedWeight int64
	var capacityStatus, binWarehouse string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(capacity_volume_cm3,0),occupied_volume_cm3,COALESCE(max_weight_grams,0),occupied_weight_grams,capacity_status,COALESCE(warehouse_name,'') FROM warehouse_bins WHERE bin_code=$1 FOR UPDATE`, binLocation).Scan(&capacity, &occupied, &maxWeight, &occupiedWeight, &capacityStatus, &binWarehouse); err != nil {
		return err
	}
	if capacityStatus != "verified" || capacity <= 0 {
		return fmt.Errorf("bin capacity is not verified")
	}
	if sourceWarehouse == "" || sourceWarehouse != binWarehouse {
		return fmt.Errorf("قفسه باید در انبار همان محموله باشد")
	}
	var exactWarehouse string
	if err := tx.QueryRowContext(ctx, `SELECT warehouse_id FROM inbound_putaway_allocations WHERE task_id=$1`, taskID).Scan(&exactWarehouse); err == nil {
		var binWarehouseID string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(warehouse_id,'') FROM warehouse_bins WHERE bin_code=$1`, binLocation).Scan(&binWarehouseID); err != nil {
			return err
		}
		if binWarehouseID != exactWarehouse {
			return ErrInboundProvenance
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	var reserved, reservedWeight int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_cm3),0),COALESCE(SUM(weight_grams),0) FROM putaway_capacity_reservations WHERE bin_code=$1 AND status='active' AND task_id<>$2`, binLocation, taskID).Scan(&reserved, &reservedWeight); err != nil {
		return err
	}
	if occupied < 0 || occupied > capacity || reserved < 0 || reserved > capacity-occupied || volume > capacity-occupied-reserved {
		return fmt.Errorf("insufficient bin volume capacity")
	}
	if maxWeight <= 0 || occupiedWeight < 0 || occupiedWeight > maxWeight || reservedWeight < 0 || reservedWeight > maxWeight-occupiedWeight || requiredWeight > maxWeight-occupiedWeight-reservedWeight {
		return fmt.Errorf("bin weight capacity exceeded")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE putaway_tasks SET bin_location=$1,status='assigned',assigned_to=$2 WHERE id=$3 AND status IN ('pending','assigned')`, binLocation, staffUserID, taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_capacity_reservations (id,task_id,bin_code,volume_cm3,weight_grams,status,idempotency_key,measurement_revision_id) VALUES ($1,$2,$3,$4,$5,'active',$6,$7) ON CONFLICT (task_id) DO UPDATE SET bin_code=EXCLUDED.bin_code,volume_cm3=EXCLUDED.volume_cm3,weight_grams=EXCLUDED.weight_grams,status='active',released_at=NULL,measurement_revision_id=EXCLUDED.measurement_revision_id`, "CAP-"+uuid.NewString(), taskID, binLocation, volume, requiredWeight, "putaway:"+taskID, physical.Revision); err != nil {
		return err
	}
	return tx.Commit()
}

// AssignPutaway allocates a shelf bin location for putaway
func (r *WarehouseRepository) AssignPutaway(ctx context.Context, productID, binLocation string, quantity int, actor string) (string, error) {
	if quantity <= 0 || strings.TrimSpace(actor) == "" || strings.TrimSpace(binLocation) == "" {
		return "", ErrInboundProvenance
	}
	rows, err := r.db.QueryContext(ctx, `SELECT pt.id FROM putaway_tasks pt JOIN inbound_putaway_allocations a ON a.task_id=pt.id WHERE pt.product_id=$1 AND pt.variant_id IS NULL AND pt.quantity=$2 AND pt.status IN ('pending','assigned') ORDER BY pt.id COLLATE "C" LIMIT 2`, productID, quantity)
	if err != nil {
		return "", err
	}
	var tasks []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		tasks = append(tasks, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(tasks) != 1 {
		return "", ErrInboundProvenance
	}
	if err = r.AssignPutawayTask(ctx, tasks[0], binLocation, actor); err != nil {
		return "", err
	}
	return tasks[0], nil
}

// ConfirmPutaway finishes putaway and increases physical bin inventory
func (r *WarehouseRepository) ConfirmPutaway(ctx context.Context, taskID, actor string) error {
	ctx, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	if strings.TrimSpace(actor) == "" {
		return ErrInboundProvenance
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockPutawayParentsTx(ctx, tx, taskID); err != nil {
		return err
	}
	allocation, err := loadExactPutawayTx(ctx, tx, taskID)
	if err != nil {
		return err
	}

	now := time.Now()
	var binCode string
	var productID, supplierID, inboundID, variantID, sourceLocation string
	var quantity int
	var volume, weight int64
	var measurementRevision string
	var taskStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM putaway_tasks WHERE id=$1 FOR UPDATE`, taskID).Scan(&taskStatus); err != nil {
		return err
	}
	if taskStatus == "completed" {
		return tx.Commit()
	}
	if taskStatus != "assigned" {
		return ErrInboundProvenance
	}
	if err = tx.QueryRowContext(ctx, `SELECT bin_code,volume_cm3,weight_grams,COALESCE(measurement_revision_id,'') FROM putaway_capacity_reservations WHERE task_id=$1 AND status='active'`, taskID).Scan(&binCode, &volume, &weight, &measurementRevision); err != nil {
		return err
	}
	if err = lockPutawayPhysicalTx(ctx, tx, taskID, binCode, allocation); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT pt.product_id,COALESCE(p.supplier_id,''),COALESCE(pt.inbound_id,''),pt.quantity,COALESCE(pt.variant_id::text,''),COALESCE(pt.inventory_source_location_key,'warehouse:'||pt.inbound_id) FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1 AND pt.status IN ('pending','assigned') FOR UPDATE`, taskID).Scan(&productID, &supplierID, &inboundID, &quantity, &variantID, &sourceLocation); err != nil {
		return err
	}
	physical, err := loadEffectiveSKUPhysicalTx(ctx, tx, productID, variantID)
	if err != nil {
		return err
	}
	currentVolume, currentWeight, err := warehouseopt.Capacity(physical.LengthMM, physical.WidthMM, physical.HeightMM, physical.WeightGrams, int64(quantity))
	if err != nil {
		return err
	}
	if !physical.Verified || physical.Revision != measurementRevision || currentVolume != volume || currentWeight != weight {
		return fmt.Errorf("ابعاد یا وزن کالا تغییر کرده است؛ تخصیص قفسه را دوباره انجام دهید")
	}
	if _, err = tx.ExecContext(ctx, `SELECT occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1 FOR UPDATE`, binCode); err != nil {
		return err
	}
	var capacity, maxWeight, occupied, occupiedWeight, reserved, reservedWeight int64
	var capacityStatus string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(capacity_volume_cm3,0),COALESCE(max_weight_grams,0),occupied_volume_cm3,occupied_weight_grams,capacity_status FROM warehouse_bins WHERE bin_code=$1`, binCode).Scan(&capacity, &maxWeight, &occupied, &occupiedWeight, &capacityStatus); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_cm3),0),COALESCE(SUM(weight_grams),0) FROM putaway_capacity_reservations WHERE bin_code=$1 AND status='active'`, binCode).Scan(&reserved, &reservedWeight); err != nil {
		return err
	}
	if capacityStatus != "verified" || capacity <= 0 || maxWeight <= 0 || occupied < 0 || occupiedWeight < 0 || occupied > capacity || occupiedWeight > maxWeight || reserved < volume || reservedWeight < weight || reserved > capacity-occupied || reservedWeight > maxWeight-occupiedWeight {
		return fmt.Errorf("bin capacity changed; reassign putaway")
	}
	if err = consumeExactPutawayTx(ctx, tx, taskID, binCode, actor, allocation); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, "PLACE-"+uuid.NewString(), taskID, allocation.product, allocation.variant, allocation.supplier, allocation.warehouse, binCode, quantity, volume, weight, physical.Revision); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE putaway_tasks SET status='completed',completed_at=$1 WHERE id=$2 AND status IN ('pending','assigned')`, now, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE warehouse_bins SET quantity=quantity+$1,occupied_volume_cm3=occupied_volume_cm3+$2,occupied_weight_grams=occupied_weight_grams+$3,updated_at=$4 WHERE bin_code=$5`, quantity, volume, weight, now, binCode); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE putaway_capacity_reservations SET status='consumed',released_at=$1 WHERE task_id=$2 AND status='active'`, now, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// getOrdersByStatuses returns customer orders in any of the given warehouse
// statuses, with real product titles and real bin locations (NULL when the
// product has no bin assignment — the UI must omit the bin in that case
// rather than fabricate one).
//
// MONEY GATE (critical): only orders with payment_status='paid' are ever
// returned. handleCreateOrder sets payment_status='pending' on creation, so
// without this predicate the warehouse could pick/pack/ship orders nobody
// paid for. Unpaid orders never enter the queue — the warehouse UI shows an
// honest empty state for them, not a fabricated row.
func (r *WarehouseRepository) getOrdersByStatuses(ctx context.Context, statuses []string) ([]ReadyToPickOrder, error) {
	// ROUTING GATE (Oct 18, 2026): a paid order can only enter the warehouse
	// queues when the warehouse actually has work on it — order-level
	// fulfillment_method 'warehouse' or 'mixed'. Supplier-direct-only orders
	// are fulfilled by the supplier; listing them here caused double
	// fulfillment (warehouse AND supplier both shipping).
	//
	// Per-item routing (gap 13) reads the PERSISTED origin: order
	// creation resolves every line once through
	// routing.ResolveItemRouting (the single routing truth shared
	// with the shipping quote) and writes one order_item_origins row
	// per line, so the queue never re-derives routing from the
	// supplier_shipping_methods toggles — a later toggle flip cannot
	// move an already-placed order between fulfilers. Origin-less
	// (legacy) items fall back to the product's fulfillment_method
	// flag only. Only warehouse-routed items enter the pick list;
	// each carries its 'fulfillmentMethod' indicator so mixed orders
	// are explicit in the UI. The per-item LATERAL keeps exactly one
	// origin per item (the lowest leg_index) even if a legacy line
	// was split across several origin rows.
	query := `
		SELECT o.id, o.order_number, o.store_name, o.customer_full_name, o.total_amount, o.status,
		       COALESCE(o.fulfillment_method, 'warehouse'),
		       COALESCE(wh.items, '[]'::jsonb) as items,
		       o.created_at,
		       o.discount_amount, o.shipping_cost, o.payable_amount, o.payment_status,
		       COALESCE(o.customer_phone, ''), COALESCE(o.customer_province, ''),
		       COALESCE(o.customer_city, ''), COALESCE(o.customer_address, ''),
		       COALESCE(o.customer_postal_code, ''), COALESCE(o.tracking_code, '')
		FROM orders o
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object(
			           'orderItemId', oi.id,
			           'productId', oi.product_id,
			           'productTitle', p.title,
			           'variantId', oi.variant_id,
			           'variantName', oi.variant_name,
			           'variantSpnc', oi.variant_spnc,
			           'quantity', COALESCE(oio.warehouse_quantity,oi.quantity),
                           'originReconciliationRequired',COALESCE(oio.unresolved,TRUE),
			           'unitPrice', oi.final_unit_price,
			           'fulfillmentMethod', r.routing,
			           'binLocation', CASE WHEN COALESCE(pv.inventory_mode,'legacy')='tracked' THEN (
			               SELECT substring(ib.location_key FROM 5)
			               FROM order_inventory_captures ic
			               CROSS JOIN LATERAL jsonb_array_elements(ic.allocations) a
			               JOIN inventory_balances ib ON ib.id=a->>'balanceId'
			               WHERE ic.order_item_id=oi.id::text AND ib.variant_id=oi.variant_id AND ib.location_type='bin'
			               ORDER BY ib.id LIMIT 1
			           ) ELSE (
			               SELECT wb.bin_code FROM warehouse_bins wb
			               WHERE wb.product_id = oi.product_id
			               ORDER BY wb.updated_at DESC LIMIT 1
			           ) END
			       )) FILTER (WHERE r.routing = 'warehouse') AS items
			FROM order_items oi
			LEFT JOIN products p ON p.id = oi.product_id
			LEFT JOIN product_variants pv ON pv.id=oi.variant_id
			LEFT JOIN LATERAL (
				SELECT CASE WHEN bool_or(origin_type='warehouse') THEN 'warehouse' WHEN bool_or(origin_type='supplier') THEN 'supplier' END AS origin_type,
                SUM(quantity) FILTER(WHERE origin_type='warehouse') AS warehouse_quantity,
                bool_or(origin_type='legacy' OR (origin_type='warehouse' AND warehouse_id IS NULL) OR (origin_type='supplier' AND supplier_warehouse_id IS NULL)) AS unresolved
                FROM order_item_origins WHERE order_item_id=oi.id
			) oio ON true
			CROSS JOIN LATERAL (
				SELECT CASE
					WHEN oio.origin_type = 'supplier' THEN 'supplier_direct'
					WHEN oio.origin_type = 'warehouse' THEN 'warehouse'
					ELSE COALESCE(p.fulfillment_method, 'warehouse')
				END AS routing
			) r
			WHERE oi.order_id = o.id
		) wh ON true
		WHERE o.status = ANY($1)
		  AND o.payment_status = 'paid'
		  AND COALESCE(o.fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
		ORDER BY o.created_at DESC
		LIMIT 50
	`
	rows, err := r.db.QueryContext(ctx, query, pq.Array(statuses))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ReadyToPickOrder
	for rows.Next() {
		var o ReadyToPickOrder
		if err := rows.Scan(
			&o.ID, &o.OrderNumber, &o.StoreName, &o.CustomerName, &o.TotalAmount, &o.Status,
			&o.FulfillmentMethod, &o.Items, &o.CreatedAt,
			&o.DiscountAmount, &o.ShippingCost, &o.PayableAmount, &o.PaymentStatus,
			&o.CustomerPhone, &o.CustomerProvince,
			&o.CustomerCity, &o.CustomerAddress,
			&o.CustomerPostalCode, &o.TrackingCode,
		); err != nil {
			return nil, err
		}
		list = append(list, o)
	}
	return list, nil
}

// GetOrdersReadyToPick returns customer orders pending warehouse picking
func (r *WarehouseRepository) GetOrdersReadyToPick(ctx context.Context) ([]ReadyToPickOrder, error) {
	return r.getOrdersByStatuses(ctx, []string{"processing", "pending", "picking"})
}

// GetOrdersReadyToPack returns orders that finished picking and wait for packing
func (r *WarehouseRepository) GetOrdersReadyToPack(ctx context.Context) ([]ReadyToPickOrder, error) {
	return r.getOrdersByStatuses(ctx, []string{"picked"})
}

// GetOrdersReadyToShip returns packed orders waiting for carrier handover
func (r *WarehouseRepository) GetOrdersReadyToShip(ctx context.Context) ([]ReadyToPickOrder, error) {
	return r.getOrdersByStatuses(ctx, []string{"packed"})
}

// StartPickingOrder marks order in picking status
func (r *WarehouseRepository) StartPickingOrder(ctx context.Context, orderID, staffUserID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Resolve the canonical order UUID: callers may pass the order number,
	// but pick_tasks.order_id is an FK to orders(id) — inserting the raw
	// parameter broke start-picking for order-number callers (FK violation).
	var orderUUID string
	err = tx.QueryRowContext(ctx, `
		UPDATE orders SET status = 'picking'
		WHERE (id = $1 OR order_number = $1)
		  AND payment_status = 'paid'
		  AND COALESCE(fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
		  AND status IN ('pending', 'processing', 'picking')
		RETURNING id
	`, orderID).Scan(&orderUUID)
	if err == sql.ErrNoRows {
		// Fail-closed: the order is missing, not paid yet, OR it is a
		// supplier-direct-only order with no warehouse work. Such orders
		// must never enter picking even via a direct API call (double
		// fulfillment: supplier AND warehouse both shipping).
		return fmt.Errorf("سفارش یافت نشد، هنوز پرداخت نشده است یا روش ارسال آن مستقیم (بدون انبار) است: %s", orderID)
	}
	if err != nil {
		return err
	}

	taskID := fmt.Sprintf("PICK-%d", time.Now().UnixMilli()%100000)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO pick_tasks (id, order_id, picker_user_id, status, started_at)
		VALUES ($1, $2, $3, 'picking', NOW())
		ON CONFLICT (id) DO UPDATE SET status = 'picking', started_at = NOW()
	`, taskID, orderUUID, staffUserID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// PickItem records an item picked from shelf
func (r *WarehouseRepository) PickItem(ctx context.Context, orderID string, itemData map[string]interface{}) error {
	itemBytes, err := json.Marshal(itemData)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE pick_tasks
		SET picked_items = picked_items || $1::jsonb
		WHERE order_id = $2
	`, itemBytes, orderID)
	return err
}

// CompletePickingOrder marks order picked. Hardened like StartPickingOrder:
// paid + warehouse-routed + currently picking, otherwise fail-closed. An
// unpaid, supplier-direct-only, or out-of-sequence order can never be driven
// to picked via a direct API call.
func (r *WarehouseRepository) CompletePickingOrder(ctx context.Context, orderID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	var orderUUID string
	err = tx.QueryRowContext(ctx, `
		UPDATE orders SET status = 'picked'
		WHERE (id = $1 OR order_number = $1)
		  AND payment_status = 'paid'
		  AND COALESCE(fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
		  AND status = 'picking'
		RETURNING id
	`, orderID).Scan(&orderUUID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("سفارش یافت نشد، هنوز پرداخت نشده است، روش ارسال آن مستقیم است یا در مرحله برداشت (picking) نیست: %s", orderID)
	}
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `UPDATE pick_tasks SET status = 'picked', completed_at = $1 WHERE order_id = $2`, now, orderUUID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// PackOrder generates tracking number and packaging label. weightGrams is the
// actual scale weight recorded by warehouse staff (0 = unknown, not persisted).
// boxSize is the packaging box the staff selected (persisted for the shipping
// label / manifest trail; empty when the client did not send one).
func (r *WarehouseRepository) PackOrder(ctx context.Context, orderID string, weightGrams int, boxSize string) (string, string, error) {
	now := time.Now()
	trackingNumber := fmt.Sprintf("TRK-POST-%d", now.UnixNano()%1000000000)
	// The printable label is rendered client-side by the warehouse print
	// system; no external label URL is fabricated here (the old cdn.drup.ir
	// link was dead).
	shippingLabel := ""

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()

	boxSize = strings.TrimSpace(boxSize)
	if len(boxSize) > 80 {
		boxSize = boxSize[:80]
	}
	query := `
		UPDATE orders
		SET status = 'packed', tracking_number = $1, tracking_code = $1, shipping_label = $2, packed_at = $3, box_size = NULLIF($4, '')
		WHERE (id = $5 OR order_number = $5)
		  AND payment_status = 'paid'
		  AND COALESCE(fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
		  AND status = 'picked'
		RETURNING id, order_number, customer_user_id::text, store_id
	`
	args := []interface{}{trackingNumber, shippingLabel, now, boxSize, orderID}
	if weightGrams > 0 {
		query = `
			UPDATE orders
			SET status = 'packed', tracking_number = $1, tracking_code = $1, shipping_label = $2, packed_at = $3, weight_grams = $4, box_size = NULLIF($5, '')
			WHERE (id = $6 OR order_number = $6)
			  AND payment_status = 'paid'
			  AND COALESCE(fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
			  AND status = 'picked'
			RETURNING id, order_number, customer_user_id::text, store_id
		`
		args = []interface{}{trackingNumber, shippingLabel, now, weightGrams, boxSize, orderID}
	}
	var updatedID, orderNumber, customerUserID string
	var storeID sql.NullString
	err = tx.QueryRowContext(ctx, query, args...).Scan(&updatedID, &orderNumber, &customerUserID, &storeID)
	if err == sql.ErrNoRows {
		// Fail-closed: unpaid, supplier-direct-only, missing, or
		// out-of-sequence (not picked) orders can never be packed.
		return "", "", fmt.Errorf("سفارش یافت نشد، هنوز پرداخت نشده است، روش ارسال آن مستقیم است یا در مرحله برداشت نهایی (picked) نیست: %s", orderID)
	}
	if err != nil {
		return "", "", err
	}
	// 20261019: notify the customer that packing is done — same transaction.
	if err := NotifyUser(ctx, tx, customerUserID, NotifKindOrderStatus,
		"سفارش شما بسته‌بندی شد",
		fmt.Sprintf("سفارش %s بسته‌بندی شد و آماده ارسال است.", orderNumber),
		notifPayloadWithStore(map[string]interface{}{"order_id": updatedID, "order_number": orderNumber, "event": "packed"}, storeID)); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return trackingNumber, shippingLabel, nil
}

// ShipOrder hands off the package to carrier. FIXWAVE P2 Fix 12: the status
// flip and the customer notification are one transaction — the customer is
// told about the shipment only when the shipment actually happened.
func (r *WarehouseRepository) ShipOrder(ctx context.Context, orderID, carrier, trackingNumber string) error {
	now := time.Now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var canonicalID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE id=$1 OR order_number=$1`, orderID).Scan(&canonicalID); err != nil {
		return err
	}
	if err := assertOrderDispatchAllowedTx(ctx, tx, canonicalID); err != nil {
		return err
	}
	if err := lockWarehouseDispatchInventoryTx(ctx, tx, canonicalID); err != nil {
		return err
	}
	var updatedID, orderNumber, customerUserID string
	var storeID sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE orders
		SET status = 'shipped', carrier = $1,
		    tracking_number = COALESCE(NULLIF($2, ''), tracking_number),
		    tracking_code = COALESCE(NULLIF($2, ''), tracking_code),
		    shipped_at = $3
		WHERE (id = $4 OR order_number = $4)
		  AND payment_status = 'paid'
		  AND COALESCE(fulfillment_method, 'warehouse') IN ('warehouse', 'mixed')
		  AND status = 'packed'
		  AND (COALESCE(fulfillment_method, 'warehouse') <> 'mixed' OR NOT EXISTS (
			SELECT 1 FROM supplier_shipments
			WHERE order_id = orders.id AND status NOT IN ('shipped', 'in_transit', 'delivered')
		  ))
		RETURNING id, order_number, customer_user_id::text, store_id
	`, carrier, trackingNumber, now, orderID).Scan(&updatedID, &orderNumber, &customerUserID, &storeID)
	if err == sql.ErrNoRows {
		// Fail-closed: unpaid, supplier-direct-only, missing, or
		// out-of-sequence (not packed) orders can never ship.
		return fmt.Errorf("%w: order %s is unpaid, cancelled, not packed, or has pending direct legs", ErrShipmentState, orderID)
	}
	if err != nil {
		return err
	}
	// Storage-fee positions (gap 7 lifecycle): the goods physically
	// leave the warehouse at this carrier handover, so the order's
	// warehouse-routed quantities leave the position ledger (FIFO)
	// in the same transaction.
	if err := consumeShippedOrderPositionsTx(ctx, tx, updatedID); err != nil {
		return err
	}
	if err := NotifyUser(ctx, tx, customerUserID, NotifKindOrderStatus,
		"سفارش شما ارسال شد",
		fmt.Sprintf("سفارش %s به اداره پست تحویل داده شد و در راه است.%s", orderNumber, trackingLabel(trackingNumber)),
		notifPayloadWithStore(map[string]interface{}{"order_id": updatedID, "order_number": orderNumber, "event": "shipped", "tracking": trackingNumber}, storeID)); err != nil {
		return err
	}
	return tx.Commit()
}

// trackingLabel renders the tracking suffix only when a tracking number
// actually exists — never a fabricated one.
func trackingLabel(tracking string) string {
	if strings.TrimSpace(tracking) == "" {
		return ""
	}
	return " کد رهگیری: " + strings.TrimSpace(tracking)
}

func asFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
