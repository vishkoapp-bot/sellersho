package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
)

// This fixture is opt-in only and is executed by the release coordinator inside
// disposable PostgreSQL. It never reads the application's DATABASE_URL.
func productModelV2Fixture(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SKU_COMMERCE_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable migrated SKU_COMMERCE_DATABASE_URL")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "model_test_" + uuid.NewString()[:8]
	if _, err = db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); db.Close() })
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	for _, table := range []string{"products", "product_variants", "categories", "suppliers", "niches", "product_niches", "product_images", "pending_approvals", "proposal_field_reviews", "proposal_review_history", "product_draft_sku_reservations", "inventory_balances", "inventory_movements", "inventory_reservations", "dk_attributes", "dk_attribute_values", "dk_category_attributes", "dk_category_attribute_values", "dk_category_colors", "dk_category_brands", "product_attribute_values", "local_product_attributes", "local_product_attribute_values", "category_local_product_attributes", "category_local_product_attribute_values", "product_attribute_option_extensions", "category_product_attribute_option_extensions", "product_canonical_attribute_values", "platform_settings", "supplier_return_requests", "supplier_return_events", "supplier_return_parcels", "warehouse_storage_positions", "warehouse_storage_receipt_lots"} {
		if _, err = tx.Exec(fmt.Sprintf(`CREATE TABLE %s (LIKE public.%s INCLUDING ALL)`, table, table)); err != nil {
			t.Fatalf("clone %s: %v", table, err)
		}
	}
	// Publication now creates serialized units. Install the real unit schema in
	// this isolated fixture so its constraints are exercised with the SKU writes.
	if _, err = tx.Exec(`CREATE TABLE orders(id VARCHAR(50) PRIMARY KEY);
		CREATE TABLE order_items(id UUID PRIMARY KEY);
		ALTER TABLE inventory_balances DROP COLUMN unit_tracked`); err != nil {
		t.Fatal(err)
	}
	unitMigration, err := os.ReadFile("../../migrations/202611230002_spncx_product_units.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(unitMigration)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE supplier_warehouses (LIKE public.supplier_warehouses INCLUDING ALL);
		INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES('model-source','supplier-test','Model source','Test','Test')`); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{`INSERT INTO suppliers(id,name,approval_status) VALUES('supplier-test','Model fixture supplier','approved')`, `CREATE TRIGGER model_fixture_stock AFTER INSERT OR UPDATE OR DELETE ON inventory_balances FOR EACH ROW EXECUTE FUNCTION public.sync_product_stock_projection()`, `INSERT INTO categories(id,title,slug,icon_name) VALUES('category-test','Test shirts','test-shirts','Package')`, `INSERT INTO dk_category_colors(category_id,dk_color_id,title) VALUES('category-test',1,'Color 1'),('category-test',2,'Color 2')`, `INSERT INTO dk_attributes(dk_attr_id,title,type) VALUES(-1,'Size','checkbox')`, `INSERT INTO dk_attribute_values(dk_attr_id,dk_value_id,title_fa) VALUES(-1,-1,'Size 1'),(-1,-2,'Size 2')`, `INSERT INTO dk_category_attributes VALUES('category-test',-1)`, `INSERT INTO dk_category_attribute_values VALUES('category-test',-1,-1),('category-test',-1,-2)`, `INSERT INTO platform_settings(id,supplier_return_postal_charge) VALUES(1,10)`} {
		if _, err = tx.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return skuScopedDB(t, schema)
}

func TestProductModelV2IsolatedSupplierReturnLifecycleKeepsExactSKU(t *testing.T) {
	db := productModelV2Fixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	d := v2Fixture(t, 2, 1)
	d["supplierWarehouseId"] = "model-source"
	d["title"] = "Return shirts"
	d["description"] = "test"
	d["categorySlug"] = "test-shirts"
	d["productSchema"].(map[string]interface{})["attributes"].([]interface{})[0].(map[string]interface{})["name"] = "رنگ"
	_, product := approveV2FixtureProposal(t, repo, d)
	vs := d["variants"].([]interface{})
	red := vs[0].(map[string]interface{})["id"].(string)
	blue := vs[1].(map[string]interface{})["id"].(string)
	if _, err := db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('old-unallocated',$1,'supplier-test','supplier','supplier','legacy','sellable',7,FALSE)`, product); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateReturnRequestWithIdempotency(ctx, "supplier-test", product, 1, "supplier_collect", "return", "missing-sku", ""); err == nil {
		t.Fatal("tracked return accepted without SKU")
	}
	first, err := repo.CreateReturnRequestForVariantWithIdempotency(ctx, "supplier-test", product, red, 1, "supplier_collect", "return", "cancel-sku", "")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repo.CreateReturnRequestForVariantWithIdempotency(ctx, "supplier-test", product, red, 1, "supplier_collect", "return", "cancel-sku", "")
	if err != nil || replay.ID != first.ID {
		t.Fatalf("return replay: %v", err)
	}
	if _, err = repo.CreateReturnRequestForVariantWithIdempotency(ctx, "supplier-test", product, blue, 1, "supplier_collect", "return", "cancel-sku", ""); err == nil {
		t.Fatal("return replay changed selected SKU")
	}
	for i := 0; i < 2; i++ {
		if err = repo.CancelReturn(ctx, "supplier-test", first.ID, "cancel", ""); err != nil {
			t.Fatal(err)
		}
	}
	second, err := repo.CreateReturnRequestForVariantWithIdempotency(ctx, "supplier-test", product, red, 2, "supplier_collect", "return", "complete-sku", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReviewReturn(ctx, second.ID, "approved", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE supplier_return_requests SET status='pickup_pending' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO supplier_return_parcels(id,return_request_id,parcel_number,carrier_code,payment_method,origin,destination,length_mm,width_mm,height_mm,weight_grams,status,created_by,idempotency_key) VALUES('parcel-test',$1,1,'test','sender_pays','origin','destination',1,1,1,1,'confirmed','test','parcel-test')`, second.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = repo.HandoverReturn(ctx, second.ID, "model-operator", "handover-sku"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err = repo.ReceiveReturn(ctx, second.ID, "", "receive-sku"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err = repo.InspectReturn(ctx, second.ID, "", "inspect-sku", 1, 1, "partial"); err != nil {
			t.Fatal(err)
		}
	}
	var redStock, blueStock, oldStock, damaged int
	if err = db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&redStock); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&blueStock); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT quantity FROM inventory_balances WHERE id='old-unallocated'`).Scan(&oldStock); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='damaged'`, red).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	if redStock != 1 || blueStock != 2 || oldStock != 7 || damaged != 1 {
		t.Fatalf("SKU return changed wrong inventory: red=%d blue=%d old=%d damaged=%d", redStock, blueStock, oldStock, damaged)
	}
}

func approveV2FixtureProposal(t *testing.T, repo *SupplierRepository, data map[string]interface{}) (string, string) {
	t.Helper()
	ctx := context.Background()
	id, err := repo.ProposeProduct(ctx, "supplier-test", data)
	if err != nil {
		t.Fatal(err)
	}
	decisions := []FieldDecision{}
	for key, value := range ReviewLeaves(data) {
		decisions = append(decisions, FieldDecision{Key: key, Decision: "approved", ValueHash: ReviewValueHash(value)})
	}
	if err = repo.SaveFieldReview(ctx, id, "", decisions); err != nil {
		t.Fatal(err)
	}
	product, err := repo.ApproveProposal(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	return id, product
}

func TestProductModelV2IsolatedPublicationPreservesUUIDsAndRegistry(t *testing.T) {
	db := productModelV2Fixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	d := v2Fixture(t, 2, 2)
	d["supplierWarehouseId"] = "model-source"
	d["title"] = "V2 shirts"
	d["description"] = "test"
	d["categorySlug"] = "test-shirts"
	attrs := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})
	attrs[0].(map[string]interface{})["name"] = "رنگ"
	// Request an option for an existing color definition, keeping its identity.
	newOption := "requestedv:" + uuid.NewString()
	attrs[0].(map[string]interface{})["values"].([]interface{})[1].(map[string]interface{})["id"] = newOption
	attrs[0].(map[string]interface{})["values"].([]interface{})[1].(map[string]interface{})["label"] = "New color"
	variants := d["variants"].([]interface{})
	for _, raw := range variants {
		v := raw.(map[string]interface{})
		s := v["selection"].(map[string]interface{})
		if s["color"] == "color:2" {
			s["color"] = newOption
		}
	}
	variants[1].(map[string]interface{})["is_active"] = false
	variants[1].(map[string]interface{})["stock"] = float64(0)
	variants[1].(map[string]interface{})["supplier_base_cost"] = float64(0)
	proposal, product := approveV2FixtureProposal(t, repo, d)
	var version, skuCount, allocated, unallocated, total int
	var kind string
	if err := db.QueryRow(`SELECT schema_version,product_type,stock FROM products WHERE id=$1`, product).Scan(&version, &kind, &total); err != nil {
		t.Fatal(err)
	}
	if version != 2 || kind != "variable" || total != 6 {
		t.Fatalf("product model/projection %d %s %d", version, kind, total)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_variants WHERE product_id=$1 AND inventory_mode='tracked'`, product).Scan(&skuCount); err != nil {
		t.Fatal(err)
	}
	if skuCount != 4 {
		t.Fatalf("unexpected synthetic/missing SKU rows: %d", skuCount)
	}
	// B2 (2026-11-13): publication assigns a real product default —
	// the first submitted variant here (the proposal names none), so
	// exactly one of the four published variants is the default.
	var defaultCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_variants WHERE product_id=$1 AND is_default`, product).Scan(&defaultCount); err != nil {
		t.Fatal(err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default variant, got %d", defaultCount)
	}
	var defaultID string
	if err := db.QueryRow(`SELECT id::text FROM product_variants WHERE product_id=$1 AND is_default`, product).Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if defaultID != variants[0].(map[string]interface{})["id"] {
		t.Fatalf("default variant = %s, want first submitted %s", defaultID, variants[0].(map[string]interface{})["id"])
	}
	for _, raw := range variants {
		v := raw.(map[string]interface{})
		var actual string
		if err := db.QueryRow(`SELECT id::text FROM product_variants WHERE product_id=$1 AND id=$2`, product, v["id"]).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if actual != v["id"] {
			t.Fatal("draft UUID replaced")
		}
	}
	db.QueryRow(`SELECT COUNT(*) FILTER(WHERE variant_id IS NOT NULL),COUNT(*) FILTER(WHERE variant_id IS NULL) FROM inventory_balances WHERE product_id=$1`, product).Scan(&allocated, &unallocated)
	if allocated != 4 || unallocated != 0 {
		t.Fatalf("guessed/unallocated balances created: %d %d", allocated, unallocated)
	}
	definitions, err := NewCatalogRepository(db).GetCategoryProductAttributes(ctx, "test-shirts")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range definitions {
		if a.ID == "color" {
			for _, v := range a.Values {
				if v.ID == newOption {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("requested option not published in category registry")
	}
	// UUID preflight rejects a new draft before creating a second publication.
	if _, err = repo.ProposeProduct(ctx, "supplier-test", d); err == nil {
		t.Fatal("published UUID reused by another draft")
	}
	if _, err = repo.ApproveProposal(ctx, proposal, ""); err == nil {
		t.Fatal("proposal published twice")
	}
	catalog, err := NewCatalogRepository(db).GetProductByID(ctx, product)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ProductType != "variable" || catalog.SchemaVersion != 2 || catalog.ProductSchema == nil {
		t.Fatal("catalog lost canonical model")
	}
}

func TestProductModelV2IsolatedRequestedAxisAndDraftOwnership(t *testing.T) {
	db := productModelV2Fixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	d := v2Fixture(t, 1, 1)
	d["supplierWarehouseId"] = "model-source"
	d["title"] = "Requested size"
	d["description"] = "test"
	d["categorySlug"] = "test-shirts"
	attrs := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})
	attrs[0].(map[string]interface{})["name"] = "رنگ"
	a := attrs[1].(map[string]interface{})
	a["id"] = "requested:" + uuid.NewString()
	a["name"] = "Requested fit"
	a["source"] = "requested"
	delete(a, "registryId")
	option := "requestedv:" + uuid.NewString()
	a["values"] = []interface{}{map[string]interface{}{"id": option, "label": "Loose"}}
	s := d["variants"].([]interface{})[0].(map[string]interface{})["selection"].(map[string]interface{})
	delete(s, "dk:-1")
	s[a["id"].(string)] = option
	id, err := repo.ProposeProduct(ctx, "supplier-test", d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ProposeProduct(ctx, "supplier-test", d); err == nil {
		t.Fatal("same UUID allowed in two pending drafts")
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateProductProposalTx(ctx, tx, d, id); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Rollback()
	decisions := []FieldDecision{}
	for k, v := range ReviewLeaves(d) {
		decisions = append(decisions, FieldDecision{Key: k, Decision: "approved", ValueHash: ReviewValueHash(v)})
	}
	if err = repo.SaveFieldReview(ctx, id, "", decisions); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ApproveProposal(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	defs, err := NewCatalogRepository(db).GetCategoryProductAttributes(ctx, "test-shirts")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, registered := range defs {
		if registered.ID == a["id"] && registered.Source == "local" && len(registered.Values) == 1 && registered.Values[0].ID == option {
			found = true
		}
	}
	if !found {
		t.Fatal("requested axis identity was not retained on publication")
	}
}

func TestProductModelV2IsolatedStablePriceCorrectionKeepsOtherApprovals(t *testing.T) {
	db := productModelV2Fixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	d := v2Fixture(t, 2, 1)
	d["supplierWarehouseId"] = "model-source"
	d["title"] = "Review shirts"
	d["description"] = "test"
	d["categorySlug"] = "test-shirts"
	d["productSchema"].(map[string]interface{})["attributes"].([]interface{})[0].(map[string]interface{})["name"] = "رنگ"
	variants := d["variants"].([]interface{})
	first := variants[0].(map[string]interface{})["id"].(string)
	other := variants[1].(map[string]interface{})["id"].(string)
	priceKey := "/variants/" + first + "/supplier_base_cost"
	stockKey := "/variants/" + other + "/stock"
	id, err := repo.ProposeProduct(ctx, "supplier-test", d)
	if err != nil {
		t.Fatal(err)
	}
	leaves := ReviewLeaves(d)
	if _, ok := leaves[priceKey]; !ok {
		t.Fatal("review price does not use stable SKU UUID")
	}
	decisions := []FieldDecision{}
	for k, v := range leaves {
		decision, reason := "approved", ""
		if k == priceKey {
			decision = "rejected"
			reason = "correct price"
		}
		decisions = append(decisions, FieldDecision{Key: k, Decision: decision, Reason: reason, ValueHash: ReviewValueHash(v)})
	}
	if err = repo.SaveFieldReview(ctx, id, "", decisions); err != nil {
		t.Fatal(err)
	}
	if err = repo.ResubmitGranularReview(ctx, "supplier-test", id, map[string]interface{}{stockKey: float64(8)}); err == nil {
		t.Fatal("supplier changed an approved SKU stock field")
	}
	if err = repo.ResubmitGranularReview(ctx, "supplier-test", id, map[string]interface{}{priceKey: float64(150)}); err != nil {
		t.Fatal(err)
	}
	var decision, hash string
	if err = db.QueryRow(`SELECT decision,COALESCE(value_hash,'') FROM proposal_field_reviews WHERE proposal_id=$1 AND field_key=$2`, id, stockKey).Scan(&decision, &hash); err != nil {
		t.Fatal(err)
	}
	if decision != "approved" || hash != ReviewValueHash(leaves[stockKey]) {
		t.Fatal("unrelated approval invalidated")
	}
	if _, err = repo.ApproveProposal(ctx, id, ""); err == nil {
		t.Fatal("corrected price published without rereview")
	}
	var raw []byte
	if err = db.QueryRow(`SELECT requested_data FROM pending_approvals WHERE id=$1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var corrected map[string]interface{}
	if err = json.Unmarshal(raw, &corrected); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveFieldReview(ctx, id, "", []FieldDecision{{Key: priceKey, Decision: "approved", ValueHash: ReviewValueHash(ReviewLeaves(corrected)[priceKey])}}); err != nil {
		t.Fatal(err)
	}
	product, err := repo.ApproveProposal(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	var cost int64
	if err = db.QueryRow(`SELECT supplier_base_cost FROM product_variants WHERE product_id=$1 AND id=$2`, product, first).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if cost != 150 {
		t.Fatal("corrected price or stable SKU identity lost")
	}
}

func TestProductModelV2IsolatedLegacyInboundCannotAllocateTrackedProduct(t *testing.T) {
	db := productModelV2Fixture(t)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	d := v2Fixture(t, 1, 1)
	d["supplierWarehouseId"] = "model-source"
	d["title"] = "Inbound shirts"
	d["description"] = "test"
	d["categorySlug"] = "test-shirts"
	d["productSchema"].(map[string]interface{})["attributes"].([]interface{})[0].(map[string]interface{})["name"] = "رنگ"
	_, product := approveV2FixtureProposal(t, repo, d)
	_, err := repo.CreateInbound(ctx, "supplier-test", map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": product, "quantity": float64(1), "carton_count": float64(1)}}})
	if !errors.Is(err, ErrInboundInput) {
		t.Fatalf("legacy inbound accepted tracked product or failed after validation: %v", err)
	}
	var unallocated int
	if err = db.QueryRow(`SELECT COUNT(*) FROM inventory_balances WHERE product_id=$1 AND variant_id IS NULL`, product).Scan(&unallocated); err != nil {
		t.Fatal(err)
	}
	if unallocated != 0 {
		t.Fatal("legacy inbound introduced unallocated SKU stock")
	}
}

func TestBatch9PublicationPreservesExplicitDefault(t *testing.T) {
	db := productModelV2Fixture(t)
	data := v2Fixture(t, 2, 1)
	data["supplierWarehouseId"] = "model-source"
	data["title"] = "Batch 9 shirts"
	data["description"] = "test"
	data["categorySlug"] = "test-shirts"
	data["productSchema"].(map[string]interface{})["attributes"].([]interface{})[0].(map[string]interface{})["name"] = "رنگ"
	chosen := data["variants"].([]interface{})[1].(map[string]interface{})
	chosen["is_default"] = true
	_, product := approveV2FixtureProposal(t, NewSupplierRepository(db), data)
	var defaultID string
	if err := db.QueryRow(`SELECT id::text FROM product_variants WHERE product_id = $1 AND is_default`, product).Scan(&defaultID); err != nil {
		t.Fatal(err)
	}
	if defaultID != chosen["id"] {
		t.Fatalf("stored default = %s, want %s", defaultID, chosen["id"])
	}
}
