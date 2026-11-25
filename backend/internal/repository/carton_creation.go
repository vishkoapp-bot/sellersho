package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrCartonCreationConflict = errors.New("carton creation key belongs to a different payload")
var errCartonDiscoveryChanged = errors.New("carton product discovery changed")

func normalizeCartonRequest(items []CartonShipmentItem, destination []string) ([]CartonShipmentItem, string, string, string, error) {
	if len(destination) < 1 || len(destination) > 2 || strings.TrimSpace(destination[0]) == "" || len(items) == 0 {
		return nil, "", "", "", fmt.Errorf("explicit destination and variant lines are required")
	}
	warehouse := strings.TrimSpace(destination[0])
	key := uuid.NewString()
	if len(destination) == 2 {
		key = strings.TrimSpace(destination[1])
		if key == "" || len(key) > 128 {
			return nil, "", "", "", fmt.Errorf("invalid carton creation key")
		}
	}
	lines := append([]CartonShipmentItem(nil), items...)
	seen := map[string]bool{}
	total := int64(0)
	for i := range lines {
		id, err := uuid.Parse(lines[i].VariantID)
		if err != nil || lines[i].ExpectedQty <= 0 || int64(lines[i].ExpectedQty) > 2147483647 {
			return nil, "", "", "", fmt.Errorf("invalid shipment line quantity or SKU")
		}
		lines[i].VariantID = id.String()
		if seen[lines[i].VariantID] {
			return nil, "", "", "", fmt.Errorf("shipment requires unique SKU lines")
		}
		seen[lines[i].VariantID] = true
		total += int64(lines[i].ExpectedQty)
		if total > 2147483647 {
			return nil, "", "", "", fmt.Errorf("shipment total exceeds integer quantity limit")
		}
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].VariantID < lines[j].VariantID })
	canonical, _ := json.Marshal(struct {
		Version   int
		Warehouse string
		Items     []CartonShipmentItem
	}{1, warehouse, lines})
	digest := sha256.Sum256(canonical)
	return lines, warehouse, key, hex.EncodeToString(digest[:]), nil
}

func cartonReplayTx(ctx context.Context, tx *sql.Tx, supplier, key, fingerprint string) (*CartonShipment, error) {
	var out CartonShipment
	var stored string
	err := tx.QueryRowContext(ctx, `SELECT s.id,s.shipment_code,(SELECT COUNT(*) FROM shipment_items WHERE shipment_id=s.id),s.status,s.created_at,s.request_fingerprint FROM shipments s WHERE s.supplier_id=$1 AND s.creation_key=$2`, supplier, key).Scan(&out.ID, &out.ShipmentCode, &out.ItemCount, &out.Status, &out.CreatedAt, &stored)
	if err != nil {
		return nil, err
	}
	if stored != fingerprint {
		return nil, ErrCartonCreationConflict
	}
	return &out, nil
}

type CartonInboundSource struct {
	Kind                string `json:"source_kind"`
	SupplierWarehouseID string `json:"supplier_warehouse_id"`
	ExternalReference   string `json:"external_reference"`
	Reason              string `json:"reason"`
}

func (r *SupplierRepository) createCartonShipment(ctx context.Context, supplier string, items []CartonShipmentItem, destination ...string) (*CartonShipment, error) {
	return r.CreateCartonShipmentFromSource(ctx, supplier, items, CartonInboundSource{Kind: "transfer"}, destination...)
}
func (r *SupplierRepository) CreateCartonShipmentFromSource(ctx context.Context, supplier string, items []CartonShipmentItem, sourceRequest CartonInboundSource, destination ...string) (*CartonShipment, error) {
	lines, warehouse, key, fingerprint, err := normalizeCartonRequest(items, destination)
	if err != nil {
		return nil, err
	}
	sourceRequest.Kind = strings.TrimSpace(sourceRequest.Kind)
	sourceRequest.SupplierWarehouseID = strings.TrimSpace(sourceRequest.SupplierWarehouseID)
	sourceRequest.ExternalReference = strings.TrimSpace(sourceRequest.ExternalReference)
	sourceRequest.Reason = strings.TrimSpace(sourceRequest.Reason)
	if len(sourceRequest.ExternalReference) > 128 || len(sourceRequest.Reason) > 1000 {
		return nil, ErrInboundProvenance
	}
	if sourceRequest.Kind == "" {
		sourceRequest.Kind = "transfer"
	}
	if sourceRequest.Kind != "transfer" && sourceRequest.Kind != "external_replenishment" {
		return nil, ErrInboundProvenance
	}
	if sourceRequest.Kind == "transfer" && (sourceRequest.ExternalReference != "" || sourceRequest.Reason != "") {
		return nil, ErrInboundProvenance
	}
	if sourceRequest.Kind == "external_replenishment" && (sourceRequest.ExternalReference == "" || sourceRequest.Reason == "" || sourceRequest.SupplierWarehouseID != "") {
		return nil, ErrInboundProvenance
	}
	sourceCanonical, _ := json.Marshal(sourceRequest)
	sourceDigest := sha256.Sum256(append([]byte(fingerprint), sourceCanonical...))
	fingerprint = hex.EncodeToString(sourceDigest[:])
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		out, err := r.createCartonShipmentAttempt(ctx, supplier, warehouse, key, fingerprint, lines, sourceRequest)
		var state interface{ SQLState() string }
		retryable := errors.Is(err, errCartonDiscoveryChanged) || (errors.As(err, &state) && (state.SQLState() == "40001" || state.SQLState() == "40P01" || state.SQLState() == "55P03"))
		if err == nil || !retryable || attempt == 2 || ctx.Err() != nil {
			return out, err
		}
		// The attempt has rolled back before waiting. Fresh discovery/guards use
		// the original key and overall deadline; exhaustion remains an error.
		timer := time.NewTimer(time.Duration(15+time.Now().UnixNano()%20) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable carton retry loop")
}

func (r *SupplierRepository) createCartonShipmentAttempt(ctx context.Context, supplier, warehouse, key, fingerprint string, lines []CartonShipmentItem, sourceRequest CartonInboundSource) (*CartonShipment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout='1500ms'`); err != nil {
		return nil, err
	}
	// A committed replay is authorized by the supplier-scoped lookup and its
	// immutable request fingerprint. It does not recreate today's stock/source.
	if out, err := cartonReplayTx(ctx, tx, supplier, key, fingerprint); err == nil {
		return out, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Compare any committed operation before fresh catalog discovery can fail.
	// These discovery reads acquire no row locks. Every fresh identity is
	// revalidated after the complete principal/product/SKU guard prefix is held.
	products := map[string]string{}
	locks := []TransactionRowLock{{Table: "suppliers", ID: supplier}}
	for _, line := range lines {
		var product string
		if err := tx.QueryRowContext(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, line.VariantID).Scan(&product); err != nil {
			return nil, err
		}
		products[line.VariantID] = product
		locks = append(locks, TransactionRowLock{Table: "products", ID: product}, TransactionRowLock{Table: "product_variants", ID: line.VariantID})
	}
	if err := LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, locks...); err != nil {
		return nil, err
	}
	if out, err := cartonReplayTx(ctx, tx, supplier, key, fingerprint); err == nil {
		return out, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT is_active FROM warehouses WHERE id=$1 FOR SHARE`, warehouse).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("destination warehouse is inactive")
	}
	source := ""
	for _, line := range lines {
		var product, ownedSource, actualSupplier string
		if err := tx.QueryRowContext(ctx, `SELECT v.product_id,COALESCE(p.supplier_id,'') FROM product_variants v JOIN products p ON p.id=v.product_id WHERE v.id=$1`, line.VariantID).Scan(&product, &actualSupplier); err != nil {
			return nil, err
		}
		if product != products[line.VariantID] {
			return nil, errCartonDiscoveryChanged
		}
		if actualSupplier != supplier {
			return nil, sql.ErrNoRows
		}
		if sourceRequest.Kind == "external_replenishment" {
			continue
		}
		err := tx.QueryRowContext(ctx, `SELECT p.id,w.id FROM product_variants v JOIN products p ON p.id=v.product_id JOIN supplier_warehouses w ON w.id=COALESCE(NULLIF($3,''),p.origin_supplier_warehouse_id) AND w.supplier_id=p.supplier_id AND w.is_active WHERE v.id=$1 AND p.supplier_id=$2 FOR SHARE OF w`, line.VariantID, supplier, sourceRequest.SupplierWarehouseID).Scan(&product, &ownedSource)
		if err != nil {
			return nil, fmt.Errorf("explicit owned carton source is required: %w", err)
		}
		if product != products[line.VariantID] {
			return nil, errCartonDiscoveryChanged
		}
		if source != "" && source != ownedSource {
			return nil, fmt.Errorf("carton lines require a single source warehouse")
		}
		source = ownedSource
	}
	out, err := createCartonShipmentTx(ctx, tx, supplier, warehouse, source, key, fingerprint, lines, sourceRequest)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// createCartonShipmentTx writes only through the caller transaction, under its
// preheld supplier/product/SKU guards. Exact persisted source reservation is
// committed atomically with its header and immutable line provenance.
func createCartonShipmentTx(ctx context.Context, tx *sql.Tx, supplier, warehouse, source, key, fingerprint string, lines []CartonShipmentItem, sources ...CartonInboundSource) (*CartonShipment, error) {
	sourceRequest := CartonInboundSource{Kind: "transfer"}
	if len(sources) > 0 {
		sourceRequest = sources[0]
	}
	var out CartonShipment
	inserted := false
	for attempt := 0; attempt < 5; attempt++ {
		code := newCartonShipmentCode()
		if code == "" {
			return nil, fmt.Errorf("could not generate shipment code")
		}
		err := tx.QueryRowContext(ctx, `INSERT INTO shipments(id,shipment_code,supplier_id,status,warehouse_id,supplier_warehouse_id,creation_key,request_fingerprint) VALUES($1,$2,$3,'draft',$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING id,shipment_code,status,created_at`, uuid.NewString(), code, supplier, warehouse, sql.NullString{String: source, Valid: source != ""}, key, fingerprint).Scan(&out.ID, &out.ShipmentCode, &out.Status, &out.CreatedAt)
		if err == nil {
			inserted = true
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		// DO NOTHING leaves the transaction usable after a code collision, and
		// observes the committed winner for an overlapping same-key creation.
		if existing, err := cartonReplayTx(ctx, tx, supplier, key, fingerprint); err == nil {
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if !inserted {
		return nil, fmt.Errorf("could not generate a unique shipment code")
	}
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,$3)`, out.ID, line.VariantID, line.ExpectedQty); err != nil {
			return nil, err
		}
	}
	transferLines := make([]InboundTransferLine, 0, len(lines))
	for _, line := range lines {
		var product string
		if err := tx.QueryRowContext(ctx, `SELECT product_id FROM product_variants WHERE id=$1`, line.VariantID).Scan(&product); err != nil {
			return nil, err
		}
		transferLines = append(transferLines, InboundTransferLine{ProductID: product, VariantID: line.VariantID, Quantity: line.ExpectedQty, SourceKind: sourceRequest.Kind, ExternalReference: sourceRequest.ExternalReference, Reason: sourceRequest.Reason, Source: InventoryOrigin{Type: "supplier", SupplierID: supplier, SupplierWarehouseID: source}})
	}
	if err := reserveInboundTransferTx(ctx, tx, out.ID, supplier, warehouse, transferLines); err != nil {
		return nil, err
	}
	out.ItemCount = len(lines)
	return &out, nil
}
