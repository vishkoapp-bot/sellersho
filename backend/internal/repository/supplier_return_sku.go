package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
	if variantID == "" {
		return r.CreateReturnRequestWithIdempotency(ctx, supplierID, productID, quantity, method, reason, idempotencyKey, actorUserID)
	}
	if uuid.Validate(variantID) != nil || quantity <= 0 || (method != "supplier_collect" && method != "postal") || strings.TrimSpace(reason) == "" {
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
	var version int
	var title, warehouse string
	if err = tx.QueryRowContext(ctx, `SELECT schema_version,title,COALESCE(warehouse_name,'') FROM products WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, productID, supplierID).Scan(&version, &title, &warehouse); err != nil {
		return nil, err
	}
	if version != 2 {
		return nil, fmt.Errorf("SKU-specific return requires a tracked product")
	}
	var mode, name, spnc string
	if err = tx.QueryRowContext(ctx, `SELECT inventory_mode,variant_name,spnc FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variantID, productID).Scan(&mode, &name, &spnc); err != nil {
		return nil, err
	}
	if mode != "tracked" {
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
		id, location, kind string
		quantity           int
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,location_key,location_type,quantity FROM inventory_balances WHERE product_id=$1 AND supplier_id=$2 AND variant_id=$3 AND owner_type='supplier' AND status='sellable' AND quantity>0 ORDER BY reconciled DESC,updated_at,id FOR UPDATE`, productID, supplierID, variantID)
	if err != nil {
		return nil, err
	}
	var allocations []allocation
	eligible := 0
	for rows.Next() {
		var a allocation
		if err = rows.Scan(&a.id, &a.location, &a.kind, &a.quantity); err != nil {
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
		if err = moveSKUInventoryTx(ctx, tx, productID, supplierID, variantID, take, "sellable", "return_requested", a.location, a.kind, a.location, "supplier_return", id, key+":move", actorUserID); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_reservations(id,product_id,supplier_id,variant_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,'supplier_return',$7,'active',$8)`, "RES-"+uuid.NewString(), productID, supplierID, variantID, a.location, take, id, key+":reservation"); err != nil {
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
	var product, variant string
	err := tx.QueryRowContext(ctx, `SELECT product_id,COALESCE(to_jsonb(rr)->>'variant_id','') FROM supplier_return_requests rr WHERE id=$1`, returnID).Scan(&product, &variant)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if variant == "" {
		return nil
	}
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE`, product).Scan(&locked); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, `SELECT id::text FROM product_variants WHERE id=$1 AND product_id=$2 FOR UPDATE`, variant, product).Scan(&locked)
}

func supplierReturnVariantTx(ctx context.Context, tx *sql.Tx, returnID string) (string, error) {
	var variant string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(to_jsonb(rr)->>'variant_id','') FROM supplier_return_requests rr WHERE id=$1`, returnID).Scan(&variant)
	return variant, err
}

func moveSupplierReturnReservationsSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, consume bool) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil || variant == "" {
		return false, err
	}
	type reservation struct {
		id, location string
		quantity     int
	}
	var reservations []reservation
	rows, err := tx.QueryContext(ctx, `SELECT id,location_key,quantity FROM inventory_reservations WHERE reference_type='supplier_return' AND reference_id=$1 AND variant_id=$2 AND status='active' ORDER BY id FOR UPDATE`, returnID, variant)
	if err != nil {
		return true, err
	}
	for rows.Next() {
		var a reservation
		if err = rows.Scan(&a.id, &a.location, &a.quantity); err != nil {
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
		var kind string
		if err = tx.QueryRowContext(ctx, `SELECT location_type FROM inventory_balances WHERE product_id=$1 AND variant_id=$2 AND location_key=$3 AND status='return_requested' FOR UPDATE`, productID, variant, a.location).Scan(&kind); err != nil {
			return true, err
		}
		status, destination, next, action := "sellable", a.location, "released", "release"
		if consume {
			status = "in_transit"
			destination = "return:" + returnID
			kind = "in_transit"
			next = "consumed"
			action = "handover"
		}
		if err = moveSKUInventoryTx(ctx, tx, productID, supplierID, variant, a.quantity, "return_requested", status, a.location, kind, destination, "supplier_return", returnID, returnID+":"+action+":"+a.id, actor); err != nil {
			return true, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE inventory_reservations SET status=$2::text,released_at=CASE WHEN $2::text='released' THEN NOW() ELSE released_at END WHERE id=$1 AND status='active'`, a.id, next); err != nil {
			return true, err
		}
		total += a.quantity
	}
	if consume {
		if err = decrementStoragePositionsFIFO(ctx, tx, productID, supplierID, "", total); err != nil {
			return true, err
		}
	}
	return true, syncProductStockTx(ctx, tx, productID)
}

func receiveSupplierReturnSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, quantity int) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil || variant == "" {
		return false, err
	}
	return true, moveSKUInventoryTx(ctx, tx, productID, supplierID, variant, quantity, "in_transit", "quarantine", "return:"+returnID, "warehouse", "return:"+returnID, "supplier_return", returnID, "receive:"+returnID, actor)
}

func inspectSupplierReturnSKUTx(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actor string, accepted, failed int) (bool, error) {
	variant, err := supplierReturnVariantTx(ctx, tx, returnID)
	if err != nil || variant == "" {
		return false, err
	}
	for _, part := range []struct {
		quantity    int
		status, key string
	}{{accepted, "sellable", "accepted"}, {failed, "damaged", "failed"}} {
		if part.quantity == 0 {
			continue
		}
		if err = moveSKUInventoryTx(ctx, tx, productID, supplierID, variant, part.quantity, "quarantine", part.status, "return:"+returnID, "warehouse", "return:"+returnID, "supplier_return", returnID, "inspect:"+returnID+":"+part.key, actor); err != nil {
			return true, err
		}
	}
	return true, nil
}
