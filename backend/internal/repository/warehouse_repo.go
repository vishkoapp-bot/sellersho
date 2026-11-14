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
	ID               string    `json:"id"`
	ProductID        string    `json:"productId"`
	ProductTitle     string    `json:"productTitle"`
	InboundID        string    `json:"inboundId"`
	SupplierName     string    `json:"supplierName"`
	ExpectedQuantity int       `json:"expectedQuantity"`
	ReceivedQuantity int       `json:"receivedQuantity"`
	CreatedAt        time.Time `json:"createdAt"`
}

type PutawayQueueItem struct {
	ID                string    `json:"id"`
	ProductID         string    `json:"productId"`
	VariantID         string    `json:"variantId,omitempty"`
	VariantName       string    `json:"variantName,omitempty"`
	VariantSPNC       string    `json:"variantSpnc,omitempty"`
	Title             string    `json:"title"`
	InboundID         string    `json:"inboundId"`
	BinLocation       string    `json:"binLocation"`
	Quantity          int       `json:"quantity"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	LengthMM          int       `json:"lengthMm"`
	WidthMM           int       `json:"widthMm"`
	HeightMM          int       `json:"heightMm"`
	WeightGrams       int       `json:"weightGrams"`
	MeasurementStatus string    `json:"measurementStatus"`
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx, `
		UPDATE inbound_shipments
		SET status = 'received', completed_at = $1
		WHERE id = $2 AND status IN ('receiving', 'pending', 'in_transit')
	`, now, inboundID)
	if err != nil {
		return err
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("محموله ورودی یافت نشد: %s", inboundID)
	}
	if err := applyInboundInventoryTx(ctx, tx, inboundID, staffUserID); err != nil {
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
		SELECT i.id || ':' || COALESCE(item->>'productId', item->>'product_id'),
		       COALESCE(item->>'productId', item->>'product_id'), COALESCE(p.title, ''),
		       i.id, i.supplier_name, COALESCE((item->>'quantity')::int, 0),
		       COALESCE(scanned.received_quantity, 0), i.created_at
		FROM inbound_shipments i
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(i.items, '[]'::jsonb)) item
		JOIN products p ON p.id = COALESCE(item->>'productId', item->>'product_id')
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(COALESCE((scan->>'quantity')::int, 0)), 0) AS received_quantity
			FROM jsonb_array_elements(COALESCE(i.scanned_items, '[]'::jsonb)) scan
			WHERE COALESCE(scan->>'productId', scan->>'product_id') = COALESCE(item->>'productId', item->>'product_id')
		) scanned ON TRUE
		WHERE i.status = 'received'
		  AND NOT EXISTS (
			-- Fix P3-15: a 'passed' qc_result means the item already went
			-- through QC (RecordQCPass is now a full pass with inventory
			-- movement). Keeping it in the queue let the dual QC paths run
			-- on the same item and double-credit inventory.
			SELECT 1 FROM qc_results q
			WHERE q.inbound_id = i.id
			  AND q.product_id = COALESCE(item->>'productId', item->>'product_id')
			  AND q.status IN ('passed', 'completed')
		  )
		ORDER BY i.created_at DESC
		LIMIT 20
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
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, nil
}

// RecordQCPass records successful quality inspection and enqueues putaway.
// (Fix P3-15) Hardened against the dual-QC double-credit hazard:
//  1. Idempotency guard: an existing 'passed'/'completed' qc_result for the
//     item makes a re-run a silent no-op — a QC pass can never credit the
//     same item twice (no duplicate putaway task, no second movement). The
//     shipment row is locked FOR UPDATE so concurrent passes serialize.
//  2. Inventory movement: this used to leave goods stranded in 'physical'
//     forever (never sellable). It now performs the same physical→sellable
//     movement as CompleteQC, with a DETERMINISTIC movement idempotency key
//     ("qc-pass:<inbound>:<product>") so even a guard bypass could not
//     double-credit balances.
//
// debitSupplierSellableForQCReceiptTx transfers only the inbound's persisted
// birth source. Unknown legacy origins or incomplete source stock require
// reconciliation; today's product origin never supplies missing provenance.
func debitSupplierSellableForQCReceiptTx(ctx context.Context, tx *sql.Tx, productID, inboundID string, qty int, referenceType, referenceID, actorUserID, toLocationKey string) error {
	if qty <= 0 {
		return nil
	}
	var supplier, source string
	if err := tx.QueryRowContext(ctx, `SELECT i.supplier_id,w.id FROM inbound_shipments i JOIN products p ON p.id=$2 AND p.supplier_id=i.supplier_id JOIN supplier_warehouses w ON w.id=i.supplier_warehouse_id AND w.supplier_id=i.supplier_id WHERE i.id=$1`, inboundID, productID).Scan(&supplier, &source); err != nil {
		return fmt.Errorf("%w: inbound source reconciliation required: %w", ErrInsufficientInventory, err)
	}
	location := "supplier-warehouse:" + source
	key := "inbound-qc-source:" + inboundID + ":" + productID
	var recorded int
	var from, to string
	err := tx.QueryRowContext(ctx, `SELECT quantity,from_location_key,to_location_key FROM inventory_movements WHERE idempotency_key=$1`, key).Scan(&recorded, &from, &to)
	if err == nil {
		if recorded != qty || from != location || to != toLocationKey {
			return fmt.Errorf("%w: inbound source event changed", ErrInsufficientInventory)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var balance string
	var available int
	if err := tx.QueryRowContext(ctx, `SELECT id,quantity FROM inventory_balances WHERE product_id=$1 AND supplier_id=$2 AND supplier_warehouse_id=$3 AND warehouse_id IS NULL AND variant_id IS NULL AND owner_type='supplier' AND location_type='supplier' AND location_key=$4 AND status='sellable' FOR UPDATE`, productID, supplier, source, location).Scan(&balance, &available); err != nil {
		return fmt.Errorf("%w: inbound source stock is missing: %w", ErrInsufficientInventory, err)
	}
	if available < qty {
		return fmt.Errorf("%w: inbound source stock is incomplete", ErrInsufficientInventory)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW() WHERE id=$1`, balance, qty); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,$4,'sellable','sellable',$5,$6,$7,$8,$9,NULLIF($10,''),'{}')`, "IM-"+uuid.NewString(), productID, supplier, qty, location, toLocationKey, referenceType, referenceID, key, actorUserID)
	return err
}

func (r *WarehouseRepository) RecordQCPass(ctx context.Context, productID, inboundID, inspectorUserID string, qty int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if qty <= 0 {
		return fmt.Errorf("positive QC quantity required")
	}
	// Serialize concurrent passes on the shipment row (mirrors CompleteQC).
	var supplierID string
	var warehouseID string
	var received int
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,items_count,COALESCE(warehouse_id,'') FROM inbound_shipments WHERE id=$1 AND status='received' FOR UPDATE`, inboundID).Scan(&supplierID, &received, &warehouseID); err != nil {
		return err
	}
	if qty > received {
		return fmt.Errorf("QC quantity exceeds physically received quantity")
	}
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM qc_results WHERE inbound_id=$1 AND product_id=$2 AND status IN ('passed','completed')`, inboundID, productID).Scan(&existing); err == nil {
		return tx.Commit()
	}

	resID := "QC-" + uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO qc_results (id, product_id, inbound_id, inspector_user_id, status, quantity_passed, created_at)
		VALUES ($1, $2, $3, $4, 'passed', $5, NOW())
	`, resID, productID, inboundID, inspectorUserID, qty)
	if err != nil {
		return err
	}

	qcCredited, err := qcSellableAlreadyCreditedTx(ctx, tx, productID, inboundID)
	if err != nil {
		return err
	}
	if err := moveInventoryTx(ctx, tx, productID, supplierID, qty, "physical", "sellable", "inbound:"+inboundID, "warehouse:"+inboundID, "qc_pass", resID, "qc-pass:"+inboundID+":"+productID, inspectorUserID, `{}`); err != nil {
		return err
	}

	// P0 2026-10-20: true-transfer debit — QC-pass mints sellable at the
	// warehouse, so the supplier's canonical sellable row is debited in the
	// same tx (cf. C3 in the SKU receive path); otherwise every pass
	// inflates the pool. Runs after the move so its products.stock re-sync
	// (inside moveInventoryTx) already covered the credit; re-sync again
	// for the debit leg.
	if err := debitSupplierSellableForQCReceiptTx(ctx, tx, productID, inboundID, qty, "inbound_qc_pass_transfer", resID, inspectorUserID, "warehouse:"+inboundID); err != nil {
		return err
	}
	if err := syncProductStockTx(ctx, tx, productID); err != nil {
		return err
	}

	putID := "PUT-" + uuid.NewString()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO putaway_tasks (id, product_id, inbound_id, bin_location, quantity, status, created_at,warehouse_id)
		VALUES ($1, $2, $3::text, NULL, $4, 'pending', NOW(),(SELECT warehouse_id FROM inbound_shipments WHERE id=$3::text))
	`, putID, productID, inboundID, qty)
	if err != nil {
		return err
	}

	// Storage-fee position (gap 7 lifecycle): this pass minted the
	// sellable warehouse stock above, so the same quantity joins the
	// position ledger in this transaction (skipped when an earlier
	// QC path already credited this inbound's sellable move).
	if !qcCredited {
		if err := creditStoragePositionTx(ctx, tx, productID, supplierID, warehouseID, inboundID, qty); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *WarehouseRepository) CompleteQC(ctx context.Context, productID, inboundID, inspectorUserID string, passed, failed, shortage int, reason string) error {
	if strings.TrimSpace(productID) == "" || strings.TrimSpace(inboundID) == "" || passed < 0 || failed < 0 || shortage < 0 || passed+failed == 0 || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("valid QC quantities and reason are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var supplierID string
	var warehouseID string
	var received int
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,items_count,COALESCE(warehouse_id,'') FROM inbound_shipments WHERE id=$1 AND status='received' FOR UPDATE`, inboundID).Scan(&supplierID, &received, &warehouseID); err != nil {
		return err
	}
	if passed+failed > received {
		return fmt.Errorf("QC quantities exceed physically received quantity")
	}
	var existing string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM qc_results WHERE inbound_id=$1 AND product_id=$2 AND status='completed'`, inboundID, productID).Scan(&existing); err == nil {
		return tx.Commit()
	}
	qcID := "QC-" + uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO qc_results (id,product_id,inbound_id,inspector_user_id,status,defect_reason,quantity_passed,quantity_failed,created_at) VALUES ($1,$2,$3,$4,'completed',$5,$6,$7,NOW())`, qcID, productID, inboundID, inspectorUserID, reason, passed, failed); err != nil {
		return err
	}
	if shortage > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO qc_discrepancies (id,inbound_id,supplier_id,product_id,expected_quantity,received_quantity,defect_count,reason,status,created_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8,'open',NOW()`, "DISP-QC-"+uuid.NewString(), inboundID, supplierID, productID, received+shortage, received, failed, reason); err != nil {
			return err
		}
	}
	qcCredited, err := qcSellableAlreadyCreditedTx(ctx, tx, productID, inboundID)
	if err != nil {
		return err
	}
	if passed > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_tasks (id,product_id,inbound_id,bin_location,quantity,status,created_at,warehouse_id) VALUES ($1,$2,$3::text,NULL,$4,'pending',NOW(),(SELECT warehouse_id FROM inbound_shipments WHERE id=$3::text))`, "PUT-"+uuid.NewString(), productID, inboundID, passed); err != nil {
			return err
		}
		if err := moveInventoryTx(ctx, tx, productID, supplierID, passed, "physical", "sellable", "inbound:"+inboundID, "warehouse:"+inboundID, "qc_pass", qcID, "qc:"+qcID+":passed", inspectorUserID, `{"failed":%d,"shortage":%d}`, failed, shortage); err != nil {
			return err
		}
		// P0 2026-10-20: true-transfer debit, same as RecordQCPass above —
		// only the passed units become sellable, so only they are debited
		// from the supplier's canonical sellable row (failed units go to
		// 'damaged' and never enter the sellable pool).
		if err := debitSupplierSellableForQCReceiptTx(ctx, tx, productID, inboundID, passed, "inbound_qc_pass_transfer", qcID, inspectorUserID, "warehouse:"+inboundID); err != nil {
			return err
		}
		if err := syncProductStockTx(ctx, tx, productID); err != nil {
			return err
		}
		// Storage-fee position (gap 7 lifecycle): same rule as
		// RecordQCPass — the passed quantity joins the position
		// ledger in this transaction, once per inbound QC credit.
		if !qcCredited {
			if err := creditStoragePositionTx(ctx, tx, productID, supplierID, warehouseID, inboundID, passed); err != nil {
				return err
			}
		}
	}
	if failed > 0 {
		if err := moveInventoryTx(ctx, tx, productID, supplierID, failed, "physical", "damaged", "inbound:"+inboundID, "quarantine:"+inboundID, "qc_failed", qcID, "qc:"+qcID+":failed", inspectorUserID, `{"reason":%q}`, reason); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func applyInboundInventoryTx(ctx context.Context, tx *sql.Tx, inboundID, actor string) error {
	var supplierID string
	var items []byte
	var scanned []byte
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id,COALESCE(items,'[]'::jsonb),COALESCE(scanned_items,'[]'::jsonb) FROM inbound_shipments WHERE id=$1 FOR UPDATE`, inboundID).Scan(&supplierID, &items, &scanned); err != nil {
		return err
	}
	var entries []struct {
		ProductID       string `json:"productId"`
		ProductIDLegacy string `json:"product_id"`
		Quantity        int    `json:"quantity"`
	}
	if err := json.Unmarshal(items, &entries); err != nil {
		return err
	}
	// Aggregate verified scans per product (both key spellings). Only
	// physically scanned units may enter canonical inventory — the declared
	// manifest quantities are never booked blindly (Fix 7).
	scannedQty := map[string]int{}
	var scans []struct {
		ProductID       string `json:"productId"`
		ProductIDLegacy string `json:"product_id"`
		Quantity        int    `json:"quantity"`
	}
	if err := json.Unmarshal(scanned, &scans); err != nil {
		return err
	}
	totalScanned := 0
	for _, s := range scans {
		pid := s.ProductID
		if pid == "" {
			pid = s.ProductIDLegacy
		}
		if pid == "" || s.Quantity <= 0 {
			continue
		}
		scannedQty[pid] += s.Quantity
		totalScanned += s.Quantity
	}
	// Fail-closed: completing a receipt with zero verified scans books
	// nothing. The warehouse must scan what physically arrived.
	if totalScanned == 0 {
		return fmt.Errorf("هیچ کالایی اسکن نشده است؛ تکمیل دریافت بدون اسکن واقعی مجاز نیست")
	}
	for _, entry := range entries {
		productID := entry.ProductID
		if productID == "" {
			productID = entry.ProductIDLegacy
		}
		if productID == "" || entry.Quantity <= 0 {
			continue
		}
		verified := scannedQty[productID]
		// Fail-closed: more scanned than declared is a data anomaly —
		// crediting it would corrupt canonical inventory. Nothing is
		// booked; the whole completion rolls back for investigation.
		if verified > entry.Quantity {
			return fmt.Errorf("تعداد اسکن‌شده کالای %s (%d) از تعداد اعلامی مانیفست (%d) بیشتر است؛ مغایرت باید بررسی شود", productID, verified, entry.Quantity)
		}
		if verified > 0 {
			// Canonical-coverage bootstrap (Team 4, 2026-10-19): a product
			// that has never touched the canonical ledger still carries its
			// stock in the legacy products.stock column. moveInventoryTx
			// below would flip it to canonical authority with a zero
			// sellable balance, and syncProductStockTx would then WIPE the
			// legacy stock (e.g. 50 -> 0). Migrate the legacy quantity into
			// the canonical sellable balance first, in the same transaction,
			// so no unit is ever lost in the transition.
			if err := ensureCanonicalCoverageTx(ctx, tx, productID, supplierID); err != nil {
				return err
			}
			if err := ensureInboundSourceBalanceTx(ctx, tx, productID, supplierID, inboundID, verified); err != nil {
				return err
			}
			if err := moveInventoryTx(ctx, tx, productID, supplierID, verified, "in_transit", "physical", "inbound:"+inboundID, "inbound:"+inboundID, "inbound_received", inboundID, "inbound:"+inboundID+":"+productID, actor, `{}`); err != nil {
				return err
			}
		}
		// Shortage (declared but unscanned) never becomes phantom stock:
		// only the verified quantity was booked above, and the shortfall
		// is filed as an open QC discrepancy for the supplier/finance
		// trail instead of silently vanishing.
		if verified < entry.Quantity {
			if err := fileInboundShortageTx(ctx, tx, inboundID, supplierID, productID, entry.Quantity, verified); err != nil {
				return err
			}
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

func ensureInboundSourceBalanceTx(ctx context.Context, tx *sql.Tx, productID, supplierID, inboundID string, quantity int) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled)
		VALUES ($1,$2,$3,'supplier','in_transit',$4,'in_transit',$5,TRUE)
		ON CONFLICT (product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO NOTHING`,
		"INV-"+uuid.NewString(), productID, supplierID, "inbound:"+inboundID, quantity)
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
	if qty <= 0 {
		qty = 1
	}
	resID := fmt.Sprintf("QC-FAIL-%d", time.Now().UnixMilli()%100000)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO qc_results (id, product_id, inbound_id, inspector_user_id, status, defect_reason, quantity_failed, created_at)
		VALUES ($1, $2, $3, $4, 'failed', $5, $6, NOW())
	`, resID, productID, inboundID, inspectorUserID, reason, qty)
	return err
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
		       pt.quantity, pt.status, pt.created_at, COALESCE(p.length_mm,0), COALESCE(p.width_mm,0), COALESCE(p.height_mm,0), COALESCE(p.weight_grams,0), COALESCE(p.measurement_status,'unverified'),
		       COALESCE(pt.variant_id::text,''), COALESCE(pv.variant_name,''), COALESCE(pv.spnc,'')
		FROM putaway_tasks pt
		LEFT JOIN products p ON pt.product_id = p.id
		LEFT JOIN product_variants pv ON pv.id=pt.variant_id
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
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, nil
}

func (r *WarehouseRepository) AssignPutawayTask(ctx context.Context, taskID, binLocation, staffUserID string) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(binLocation) == "" || strings.TrimSpace(staffUserID) == "" {
		return fmt.Errorf("task, bin and actor are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var productID string
	var quantity, lengthMM, widthMM, heightMM, weightGrams int
	var measurementStatus, status, sourceWarehouse string
	if err := tx.QueryRowContext(ctx, `SELECT pt.product_id,pt.quantity,pt.status,COALESCE(p.length_mm,0),COALESCE(p.width_mm,0),COALESCE(p.height_mm,0),COALESCE(p.weight_grams,0),COALESCE(p.measurement_status,'unverified'),COALESCE((SELECT warehouse_name FROM inbound_shipments WHERE id=pt.inbound_id),p.warehouse_name,'') FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1 FOR UPDATE`, taskID).Scan(&productID, &quantity, &status, &lengthMM, &widthMM, &heightMM, &weightGrams, &measurementStatus, &sourceWarehouse); err != nil {
		return err
	}
	if status == "completed" {
		return tx.Commit()
	}
	if quantity <= 0 || measurementStatus != "verified" || lengthMM <= 0 || widthMM <= 0 || heightMM <= 0 || weightGrams <= 0 {
		return fmt.Errorf("task measurements are not verified")
	}
	volume, requiredWeight, err := warehouseopt.Capacity(int64(lengthMM), int64(widthMM), int64(heightMM), int64(weightGrams), int64(quantity))
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
	var reserved, reservedWeight int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_cm3),0),COALESCE(SUM(weight_grams),0) FROM putaway_capacity_reservations WHERE bin_code=$1 AND status='active' AND task_id<>$2`, binLocation, taskID).Scan(&reserved, &reservedWeight); err != nil {
		return err
	}
	if occupied+reserved+volume > capacity {
		return fmt.Errorf("insufficient bin volume capacity")
	}
	if maxWeight <= 0 || occupiedWeight+reservedWeight+requiredWeight > maxWeight {
		return fmt.Errorf("bin weight capacity exceeded")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE putaway_tasks SET bin_location=$1,status='assigned',assigned_to=$2 WHERE id=$3 AND status IN ('pending','assigned')`, binLocation, staffUserID, taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_capacity_reservations (id,task_id,bin_code,volume_cm3,weight_grams,status,idempotency_key) VALUES ($1,$2,$3,$4,$5,'active',$6) ON CONFLICT (task_id) DO UPDATE SET bin_code=EXCLUDED.bin_code,volume_cm3=EXCLUDED.volume_cm3,weight_grams=EXCLUDED.weight_grams,status='active'`, "CAP-"+uuid.NewString(), taskID, binLocation, volume, requiredWeight, "putaway:"+taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// AssignPutaway allocates a shelf bin location for putaway
func (r *WarehouseRepository) AssignPutaway(ctx context.Context, productID, binLocation string, quantity int, staffUserID string) (string, error) {
	if quantity <= 0 || strings.TrimSpace(binLocation) == "" || strings.TrimSpace(staffUserID) == "" {
		return "", fmt.Errorf("product, bin, quantity and actor are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var lengthMM, widthMM, heightMM, weightGrams int
	var measurementStatus, sourceWarehouse string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(length_mm,0),COALESCE(width_mm,0),COALESCE(height_mm,0),COALESCE(weight_grams,0),measurement_status,COALESCE(warehouse_name,'') FROM products WHERE id=$1 FOR UPDATE`, productID).Scan(&lengthMM, &widthMM, &heightMM, &weightGrams, &measurementStatus, &sourceWarehouse); err != nil {
		return "", err
	}
	if measurementStatus != "verified" || lengthMM <= 0 || widthMM <= 0 || heightMM <= 0 || weightGrams <= 0 {
		return "", fmt.Errorf("product measurements are not verified")
	}
	volume, requiredWeight, err := warehouseopt.Capacity(int64(lengthMM), int64(widthMM), int64(heightMM), int64(weightGrams), int64(quantity))
	if err != nil {
		return "", err
	}
	taskID := "PUT-TSK-" + uuid.NewString()
	var capacity, occupied, maxWeight, occupiedWeight int64
	var capacityStatus, binWarehouse string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(capacity_volume_cm3,0),occupied_volume_cm3,COALESCE(max_weight_grams,0),occupied_weight_grams,capacity_status,COALESCE(warehouse_name,'') FROM warehouse_bins WHERE bin_code=$1 FOR UPDATE`, binLocation).Scan(&capacity, &occupied, &maxWeight, &occupiedWeight, &capacityStatus, &binWarehouse); err != nil {
		return "", err
	}
	if capacityStatus != "verified" || capacity <= 0 {
		return "", fmt.Errorf("bin capacity is not verified")
	}
	if sourceWarehouse == "" || sourceWarehouse != binWarehouse {
		return "", fmt.Errorf("قفسه باید در انبار همان کالا باشد")
	}
	var reserved, reservedWeight int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_cm3),0),COALESCE(SUM(weight_grams),0) FROM putaway_capacity_reservations WHERE bin_code=$1 AND status='active'`, binLocation).Scan(&reserved, &reservedWeight); err != nil {
		return "", err
	}
	if occupied+reserved+volume > capacity {
		return "", fmt.Errorf("insufficient bin volume capacity")
	}
	if maxWeight <= 0 || occupiedWeight+reservedWeight+requiredWeight > maxWeight {
		return "", fmt.Errorf("bin weight capacity exceeded")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_tasks (id,product_id,bin_location,quantity,status,assigned_to,created_at) VALUES ($1,$2,$3,$4,'assigned',$5,NOW())`, taskID, productID, binLocation, quantity, staffUserID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO putaway_capacity_reservations (id,task_id,bin_code,volume_cm3,weight_grams,status,idempotency_key) VALUES ($1,$2,$3,$4,$5,'active',$6)`, "CAP-"+uuid.NewString(), taskID, binLocation, volume, requiredWeight, "putaway:"+taskID); err != nil {
		return "", err
	}
	return taskID, tx.Commit()
}

// ConfirmPutaway finishes putaway and increases physical bin inventory
func (r *WarehouseRepository) ConfirmPutaway(ctx context.Context, taskID, actor string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	var binCode string
	var productID, supplierID, inboundID, variantID, sourceLocation string
	var quantity int
	var volume, weight int64
	var lengthMM, widthMM, heightMM, weightGrams int64
	var measurementStatus string
	var taskStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM putaway_tasks WHERE id=$1 FOR UPDATE`, taskID).Scan(&taskStatus); err != nil {
		return err
	}
	if taskStatus == "completed" {
		return tx.Commit()
	}
	if err = tx.QueryRowContext(ctx, `SELECT bin_code,volume_cm3,weight_grams FROM putaway_capacity_reservations WHERE task_id=$1 AND status='active' FOR UPDATE`, taskID).Scan(&binCode, &volume, &weight); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT pt.product_id,COALESCE(p.supplier_id,''),COALESCE(pt.inbound_id,''),pt.quantity,COALESCE(p.length_mm,0),COALESCE(p.width_mm,0),COALESCE(p.height_mm,0),COALESCE(p.weight_grams,0),p.measurement_status,COALESCE(pt.variant_id::text,''),COALESCE(pt.inventory_source_location_key,'warehouse:'||pt.inbound_id) FROM putaway_tasks pt JOIN products p ON p.id=pt.product_id WHERE pt.id=$1 AND pt.status IN ('pending','assigned') FOR UPDATE`, taskID).Scan(&productID, &supplierID, &inboundID, &quantity, &lengthMM, &widthMM, &heightMM, &weightGrams, &measurementStatus, &variantID, &sourceLocation); err != nil {
		return err
	}
	currentVolume, currentWeight, err := warehouseopt.Capacity(lengthMM, widthMM, heightMM, weightGrams, int64(quantity))
	if err != nil {
		return err
	}
	if measurementStatus != "verified" || currentVolume != volume || currentWeight != weight {
		return fmt.Errorf("ابعاد یا وزن کالا تغییر کرده است؛ تخصیص قفسه را دوباره انجام دهید")
	}
	if _, err = tx.ExecContext(ctx, `SELECT occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1 FOR UPDATE`, binCode); err != nil {
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
	if variantID != "" {
		if err := putawayTrackedSKUInventoryTx(ctx, tx, productID, supplierID, variantID, quantity, sourceLocation, binCode, taskID, actor); err != nil {
			return err
		}
	} else if err := moveInventoryTx(ctx, tx, productID, supplierID, quantity, "sellable", "sellable", "warehouse:"+inboundID, "bin:"+binCode, "putaway_confirmed", taskID, "putaway:"+taskID, actor, `{}`); err != nil {
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
