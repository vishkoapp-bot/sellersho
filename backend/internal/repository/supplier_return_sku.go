package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func validateSupplierReturnReplay(stored SupplierReturnRequest, productID, variantID string, quantity int, method, reason string) error {
	if stored.ProductID != productID || stored.VariantID != variantID || stored.RequestedQuantity != quantity || stored.CollectionMethod != method || stored.Reason != reason {
		return fmt.Errorf("idempotency key already used for a different return request")
	}
	return nil
}

// Empty variant identity preserves the legacy API. A tracked product must never
// reserve a sibling SKU or use its historical unallocated product balance.
func (r *SupplierRepository) CreateReturnRequestForVariantWithIdempotency(ctx context.Context, supplierID, productID, variantID string, quantity int, method, reason, idempotencyKey, actorUserID string) (*SupplierReturnRequest, error) {
	return r.createSupplierReturnTx(ctx, supplierID, productID, variantID, quantity, method, reason, idempotencyKey, actorUserID)
}

var ErrSupplierReturnVariantUnresolved = errors.New("supplier_return_variant_unresolved")

func (r *SupplierRepository) createSupplierReturnTx(ctx context.Context, supplierID, productID, variantID string, quantity int, method, reason, idempotencyKey, actorUserID string) (*SupplierReturnRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if (variantID != "" && uuid.Validate(variantID) != nil) || quantity <= 0 || (method != "supplier_collect" && method != "postal") || strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("invalid SKU return request")
	}
	if idempotencyKey == "" {
		idempotencyKey = "sku-return-" + uuid.NewString()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, TransactionRowLock{Table: "suppliers", ID: supplierID}, TransactionRowLock{Table: "products", ID: productID}); err != nil {
		return nil, err
	}
	var version int
	var title, warehouse string
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,title,COALESCE(warehouse_name,'') FROM products WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, productID, supplierID).Scan(&version, &title, &warehouse); err != nil {
		return nil, err
	}
	if variantID == "" {
		variantID, err = ResolveLegacyPurchaseVariant(ctx, tx, productID, "")
		if err != nil {
			return nil, err
		}
		if variantID == "" {
			return nil, ErrSupplierReturnVariantUnresolved
		}
	}
	if err = lockSKUProductTx(ctx, tx, productID, variantID); err != nil {
		return nil, err
	}

	var mode, name, spnc string
	if err = tx.QueryRowContext(ctx, `SELECT inventory_mode,variant_name,spnc FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&mode, &name, &spnc); err != nil {
		return nil, err
	}
	if version >= 2 && mode != "tracked" {
		return nil, fmt.Errorf("SKU does not have tracked inventory")
	}
	var stored SupplierReturnRequest
	err = tx.QueryRowContext(ctx, `SELECT id,product_id,COALESCE(variant_id::text,''),requested_quantity,collection_method,reason,status,postal_charge,created_at FROM supplier_return_requests WHERE supplier_id=$1 AND idempotency_key=$2 FOR UPDATE`, supplierID, idempotencyKey).Scan(&stored.ID, &stored.ProductID, &stored.VariantID, &stored.RequestedQuantity, &stored.CollectionMethod, &stored.Reason, &stored.Status, &stored.PostalCharge, &stored.CreatedAt)
	if err == nil {
		if err = validateSupplierReturnReplay(stored, productID, variantID, quantity, method, reason); err != nil {
			return nil, err
		}
		stored.SupplierID = supplierID
		stored.ProductTitle = title
		stored.WarehouseName = warehouse
		stored.VariantName = name
		stored.VariantSPNC = spnc
		return &stored, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	type allocation struct {
		id, location, kind, physicalVariant string
		quantity                            int
	}
	// A NULL physical lot is eligible only for the uniquely identified legacy default.
	allowLegacySource := false
	if mode == "legacy" {
		resolved, e := ResolveLegacyPurchaseVariant(ctx, tx, productID, "")
		if e != nil {
			return nil, e
		}
		allowLegacySource = resolved == variantID
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,location_key,location_type,quantity,COALESCE(variant_id::text,'') FROM inventory_balances WHERE product_id=$1 AND supplier_id=$2 AND (variant_id=$3 OR ($4 AND variant_id IS NULL)) AND owner_type='supplier' AND status='sellable' AND quantity>0 ORDER BY id COLLATE "C" FOR UPDATE`, productID, supplierID, variantID, allowLegacySource)
	if err != nil {
		return nil, err
	}
	var allocations []allocation
	eligible := 0
	for rows.Next() {
		var a allocation
		if err = rows.Scan(&a.id, &a.location, &a.kind, &a.quantity, &a.physicalVariant); err != nil {
			rows.Close()
			return nil, err
		}
		allocations = append(allocations, a)
		eligible += a.quantity
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if quantity > eligible {
		return nil, fmt.Errorf("return quantity exceeds eligible inventory for selected SKU")
	}
	postal := int64(0)
	if method == "postal" {
		var policy sql.NullInt64
		if err = tx.QueryRowContext(ctx, `SELECT supplier_return_postal_charge FROM platform_settings WHERE id=1`).Scan(&policy); err != nil {
			return nil, err
		}
		if !policy.Valid {
			return nil, fmt.Errorf("postal return charge policy is not configured")
		}
		postal = policy.Int64
	}
	id := "RET-" + uuid.NewString()
	remaining := quantity
	first := ""
	for i, a := range allocations {
		if remaining == 0 {
			break
		}
		take := a.quantity
		if take > remaining {
			take = remaining
		}
		if first == "" {
			first = a.id
		}
		key := fmt.Sprintf("%s:%s:%d", idempotencyKey, variantID, i)
		if err = moveSupplierReturnLotTx(ctx, tx, productID, supplierID, variantID, a.id, take, "sellable", "return_requested", a.location, a.kind, a.location, id, key+":move", actorUserID, a.physicalVariant == ""); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_reservations(id,product_id,supplier_id,variant_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,'supplier_return',$7,'active',$8)`, "RES-"+uuid.NewString(), productID, supplierID, a.physicalVariant, a.location, take, id, key+":reservation"); err != nil {
			return nil, err
		}
		remaining -= take
	}
	if remaining != 0 || first == "" {
		return nil, fmt.Errorf("SKU return reservation incomplete")
	}
	var created SupplierReturnRequest
	err = tx.QueryRowContext(ctx, `INSERT INTO supplier_return_requests(id,supplier_id,product_id,variant_id,requested_quantity,collection_method,postal_charge,status,reason,idempotency_key,inventory_reference_id) VALUES($1,$2,$3,$4,$5,$6,$7,'awaiting_return_test',$8,$9,$10) RETURNING created_at`, id, supplierID, productID, variantID, quantity, method, postal, reason, idempotencyKey, first).Scan(&created.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO supplier_return_events(id,return_request_id,to_status,reason,actor_user_id,idempotency_key) VALUES($1,$2,'awaiting_return_test',$3,NULLIF($4,''),$5)`, "RET-EVT-"+uuid.NewString(), id, reason, actorUserID, idempotencyKey+":event"); err != nil {
		return nil, err
	}
	created.ID = id
	created.SupplierID = supplierID
	created.ProductID = productID
	created.ProductTitle = title
	created.WarehouseName = warehouse
	created.VariantID = variantID
	created.VariantName = name
	created.VariantSPNC = spnc
	created.RequestedQuantity = quantity
	created.EligibleQuantity = eligible - quantity
	created.CollectionMethod = method
	created.PostalCharge = postal
	created.Status = "awaiting_return_test"
	created.Reason = reason
	if err = syncProductStockTx(ctx, tx, productID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &created, nil
}

// Establish the same product -> SKU -> request -> balances lock order as payment
// capture before an existing return transition locks its request row.
func lockSupplierReturnSKUTx(ctx context.Context, tx *sql.Tx, returnID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var product, variant, supplier string
	err := tx.QueryRowContext(ctx, `SELECT product_id,COALESCE(to_jsonb(rr)->>'variant_id',''),supplier_id FROM supplier_return_requests rr WHERE id=$1`, returnID).Scan(&product, &variant, &supplier)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, TransactionRowLock{Table: "suppliers", ID: supplier}, TransactionRowLock{Table: "products", ID: product}); err != nil {
		return err
	}
	if variant == "" {
		variant, err = ResolveLegacyPurchaseVariant(ctx, tx, product, "")
		if err != nil {
			return err
		}
		if variant == "" {
			return ErrSupplierReturnVariantUnresolved
		}
	}
	if err = lockSKUProductTx(ctx, tx, product, variant); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE supplier_return_requests SET variant_id=$2 WHERE id=$1 AND variant_id IS NULL`, returnID, variant)
	return err
}

func supplierReturnVariantTx(ctx context.Context, tx *sql.Tx, returnID string) (string, error) {
	var variant string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(to_jsonb(rr)->>'variant_id','') FROM supplier_return_requests rr WHERE id=$1`, returnID).Scan(&variant)
	return variant, err
}

// Preserve NULL physical identity while reserving or releasing a legacy lot.
// Other movements (including the request's dedicated transit bucket) keep the
// existing SKU identity. No previously recorded reservation is reclassified.
func moveSupplierReturnLotTx(ctx context.Context, tx *sql.Tx, product, supplier, variant, sourceID string, qty int, from, to, location, kind, destination, returnID, key, actor string, keepLegacy bool) error {
	if !keepLegacy {
		return moveSKUInventoryFromLotTx(ctx, tx, product, supplier, variant, sourceID, qty, from, to, location, kind, destination, "supplier_return", returnID, key, actor)
	}
	if destination != location {
		return fmt.Errorf("refusing to relocate unallocated return custody")
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,'supplier_return',$9,$10,NULLIF($11,''),'{}') ON CONFLICT(idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), product, supplier, variant, qty, from, to, location, returnID, key, actor)
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil || n == 0 {
		return e
	}
	res, err = tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW()
		WHERE id=$1 AND quantity >=$2 AND product_id=$3 AND supplier_id=$4 AND variant_id IS NULL
		AND owner_type='supplier' AND status=$5 AND location_key=$6 AND location_type=$7`, sourceID, qty, product, supplier, from, location, kind)
	if err != nil {
		return err
	}
	if n, e := res.RowsAffected(); e != nil || n != 1 {
		if e != nil {
			return e
		}
		return ErrInsufficientInventory
	}
	var held string
	// Only cancellation back into an existing, authoritative same-bin bucket
	// may tolerate missing legacy metadata. Never overwrite the destination,
	// merge contradictory origins, or upgrade an unreconciled quantity.
	if err = tx.QueryRowContext(ctx, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,bin_code,warehouse_id,supplier_warehouse_id)
		SELECT $1,product_id,supplier_id,owner_type,location_type,location_key,$2,$3,reconciled,bin_code,warehouse_id,supplier_warehouse_id FROM inventory_balances WHERE id=$4
		ON CONFLICT(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL
		DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()
		WHERE inventory_balances.supplier_id IS NOT DISTINCT FROM EXCLUDED.supplier_id
		AND inventory_balances.reconciled = EXCLUDED.reconciled
		AND (
			(inventory_balances.bin_code,inventory_balances.warehouse_id,inventory_balances.supplier_warehouse_id)
			IS NOT DISTINCT FROM (EXCLUDED.bin_code,EXCLUDED.warehouse_id,EXCLUDED.supplier_warehouse_id)
			OR ($2='sellable' AND EXCLUDED.location_type='bin'
				AND EXCLUDED.supplier_warehouse_id IS NULL AND inventory_balances.supplier_warehouse_id IS NULL
				AND (EXCLUDED.bin_code IS NULL OR EXCLUDED.bin_code=inventory_balances.bin_code)
				AND (EXCLUDED.warehouse_id IS NULL OR EXCLUDED.warehouse_id=inventory_balances.warehouse_id)
				AND EXISTS (SELECT 1 FROM warehouse_bins b WHERE b.bin_code=inventory_balances.bin_code
					AND b.warehouse_id=inventory_balances.warehouse_id
					AND EXCLUDED.location_key IN (b.bin_code,'bin:'||b.bin_code)))
		)
		RETURNING id`, "INV-"+uuid.NewString(), to, qty, sourceID).Scan(&held); err != nil {
		return err
	}
	_, err = moveLotUnitsTx(ctx, tx, product, variant, sourceID, held, unitStatusForLot(from, location), unitStatusForLot(to, destination), qty, "supplier_return", "supplier_return", returnID, actor)
	return err
}

func moveSupplierReturnReservationsSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, consume bool) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil {
		return false, err
	}
	if variant == "" {
		return false, ErrSupplierReturnVariantUnresolved
	}
	type reservation struct {
		id, location, physicalVariant string
		quantity                      int
	}
	var reservations []reservation
	// Physical balances precede inventory_reservations in the shared CORE order.
	balanceRows, err := tx.QueryContext(ctx, `SELECT id FROM inventory_balances WHERE product_id=$1 AND supplier_id=$3 AND (variant_id=$2 OR variant_id IS NULL) ORDER BY id COLLATE "C" FOR UPDATE`, productID, variant, supplierID)
	if err != nil {
		return true, err
	}
	for balanceRows.Next() {
	}
	err = balanceRows.Err()
	balanceRows.Close()
	if err != nil {
		return true, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,location_key,quantity,COALESCE(variant_id::text,'') FROM inventory_reservations WHERE reference_type='supplier_return' AND reference_id=$1 AND product_id=$3 AND supplier_id=$4 AND (variant_id=$2 OR variant_id IS NULL) AND status='active' ORDER BY id FOR UPDATE`, returnID, variant, productID, supplierID)
	if err != nil {
		return true, err
	}
	for rows.Next() {
		var a reservation
		if err = rows.Scan(&a.id, &a.location, &a.quantity, &a.physicalVariant); err != nil {
			rows.Close()
			return true, err
		}
		reservations = append(reservations, a)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return true, err
	}
	rows.Close()
	if len(reservations) == 0 {
		return true, fmt.Errorf("SKU return reservation missing or already consumed")
	}
	total := 0
	for _, a := range reservations {
		var kind, balanceID string
		if err = tx.QueryRowContext(ctx, `SELECT b.location_type,b.id FROM inventory_balances b
			WHERE b.product_id=$1 AND b.supplier_id=$4 AND b.variant_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid AND b.location_key=$3 AND b.owner_type='supplier' AND b.status='return_requested' AND b.quantity >=$5
			AND NOT EXISTS (SELECT 1 FROM inventory_balances x WHERE x.product_id=b.product_id AND x.supplier_id=b.supplier_id AND x.variant_id IS NOT DISTINCT FROM b.variant_id AND x.location_key=b.location_key AND x.owner_type=b.owner_type AND x.status=b.status AND x.id<>b.id)
			FOR UPDATE OF b`, productID, a.physicalVariant, a.location, supplierID, a.quantity).Scan(&kind, &balanceID); err != nil {
			return true, err
		}
		status, destination, next, action := "sellable", a.location, "released", "release"
		if consume {
			status = "in_transit"
			destination = "supplier-return-transit:" + returnID
			kind = "in_transit"
			next = "consumed"
			action = "handover"
		}
		if err = moveSupplierReturnLotTx(ctx, tx, productID, supplierID, variant, balanceID, a.quantity, "return_requested", status, a.location, kind, destination, returnID, returnID+":"+action+":"+a.id, actor, !consume && a.physicalVariant == ""); err != nil {
			return true, err
		}
		// A reservation identifies its original physical lot. Keep a historical
		// NULL SKU here: the moved destination is SKU-linked, but occupancy must
		// still join the original unallocated source when recording departure.
		if _, err = tx.ExecContext(ctx, `UPDATE inventory_reservations SET status=$2::text,released_at=CASE WHEN $2::text='released' THEN NOW() ELSE released_at END WHERE id=$1 AND status='active'`, a.id, next); err != nil {
			return true, err
		}
		total += a.quantity
	}
	if consume {
		if err = releaseSupplierReturnOccupancyTx(ctx, tx, returnID, actor); err != nil {
			return true, err
		}
		known, err := hasKnownStorageProductTx(ctx, tx, productID)
		if err != nil {
			return true, err
		}
		if known {
			err = consumeStorageBusinessEventTx(ctx, tx, "supplier_return", returnID, actor)
		} else {
			err = decrementStoragePositionsFIFO(ctx, tx, productID, supplierID, "", total)
		}
		if err != nil {
			return true, err
		}
	}
	return true, syncProductStockTx(ctx, tx, productID)
}

func receiveSupplierReturnSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, quantity int) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil {
		return false, err
	}
	if variant == "" {
		return false, ErrSupplierReturnVariantUnresolved
	}
	source := "supplier-return-transit:" + returnID
	balance, err := resolveSupplierReturnLotTx(ctx, tx, productID, supplierID, variant, source, returnID, "in_transit")
	if err != nil {
		return true, err
	}
	return true, moveSKUInventoryFromLotTx(ctx, tx, productID, supplierID, variant, balance, quantity, "in_transit", "quarantine", source, "warehouse", "supplier-return:"+returnID, "supplier_return", returnID, "receive:"+returnID, actor)

}

func inspectSupplierReturnSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, accepted, failed int) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil {
		return false, err
	}
	if variant == "" {
		return false, ErrSupplierReturnVariantUnresolved
	}
	location := "supplier-return:" + returnID
	balance, err := resolveSupplierReturnLotTx(ctx, tx, productID, supplierID, variant, location, returnID, "quarantine")
	if err != nil {
		return true, err
	}
	for _, part := range []struct {
		quantity    int
		status, key string
	}{{accepted, "sellable", "accepted"}, {failed, "damaged", "failed"}} {
		if part.quantity == 0 {
			continue
		}
		if err = moveSKUInventoryFromLotTx(ctx, tx, productID, supplierID, variant, balance, part.quantity, "quarantine", part.status, location, "warehouse", location, "supplier_return", returnID, "inspect:"+returnID+":"+part.key, actor); err != nil {
			return true, err
		}
	}
	return true, nil
}

func resolveSupplierReturnLotTx(ctx context.Context, tx *sql.Tx, product, supplier, variant, location, returnID, status string) (string, error) {
	id, err := inventoryLotIDTx(ctx, tx, product, variant, location, status)
	if err != sql.ErrNoRows {
		return id, err
	}
	err = tx.QueryRowContext(ctx, `UPDATE inventory_balances SET variant_id=$3,location_key=$4,updated_at=NOW()
		WHERE product_id=$1 AND supplier_id=$2 AND (variant_id IS NULL OR variant_id=$3) AND location_key=$5 AND status=$6 RETURNING id`, product, supplier, variant, location, "return:"+returnID, status).Scan(&id)
	return id, err
}
