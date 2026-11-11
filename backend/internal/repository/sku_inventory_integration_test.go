package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// These tests NEVER use DATABASE_URL or any host application credentials.
// The release coordinator supplies a disposable migrated PostgreSQL database.
// Each test uses its own schema so concurrency tests exercise actual row locks.
func skuInventoryFixture(t *testing.T) (*sql.DB, string, string, string) {
	t.Helper()
	dsn := os.Getenv("SKU_COMMERCE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SKU_COMMERCE_DATABASE_URL to a disposable migrated database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "sku_test_" + uuid.NewString()[:8]
	if _, err := db.Exec(`CREATE SCHEMA ` + schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SET LOCAL search_path TO ` + schema + `,public`); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE products(id VARCHAR(50) PRIMARY KEY,supplier_id VARCHAR(50),stock INT NOT NULL,schema_version INT NOT NULL DEFAULT 0,product_type TEXT NOT NULL DEFAULT 'legacy',updated_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE product_variants(id UUID PRIMARY KEY,product_id TEXT,stock INT NOT NULL,inventory_mode TEXT NOT NULL DEFAULT 'legacy',is_active BOOL NOT NULL DEFAULT TRUE,min_cart_qty INT DEFAULT 1,max_cart_qty INT)`,
		`CREATE TABLE inventory_balances(id VARCHAR(80) PRIMARY KEY,product_id VARCHAR(50),supplier_id VARCHAR(50),variant_id UUID,owner_type VARCHAR(20),location_type VARCHAR(20),location_key VARCHAR(150),status VARCHAR(30),quantity INT NOT NULL CHECK(quantity>=0),reconciled BOOL,updated_at TIMESTAMPTZ DEFAULT NOW(),bin_code VARCHAR(30))`,
		`CREATE UNIQUE INDEX legacy_balance_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`,
		`CREATE UNIQUE INDEX sku_balance_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`,
		`CREATE TABLE inventory_movements(id VARCHAR(80),product_id VARCHAR(50),supplier_id VARCHAR(50),variant_id UUID,quantity INT,from_status VARCHAR(30),to_status VARCHAR(30),from_location_key VARCHAR(150),to_location_key VARCHAR(150),reference_type VARCHAR(40),reference_id VARCHAR(80),idempotency_key VARCHAR(150) UNIQUE,actor_user_id VARCHAR(80),metadata JSONB)`,
		`CREATE TABLE order_items(id UUID PRIMARY KEY,order_id TEXT,product_id TEXT,variant_id UUID,quantity INT)`,
		`CREATE TABLE orders(id TEXT PRIMARY KEY,order_number TEXT,status TEXT,payment_status TEXT,payment_expires_at TIMESTAMPTZ,inventory_captured BOOL DEFAULT FALSE,updated_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE payment_transactions(id TEXT,order_id TEXT,order_number TEXT,gateway_token TEXT UNIQUE,rrn TEXT,card_mask TEXT,amount BIGINT,status TEXT,created_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE order_inventory_captures(order_item_id TEXT PRIMARY KEY,order_id TEXT,product_id TEXT,variant_id UUID,quantity INT,variant_quantity INT,restored_quantity INT NOT NULL DEFAULT 0,inventory_mode TEXT,allocations JSONB)`,
		`CREATE TABLE order_inventory_returns(return_item_id TEXT PRIMARY KEY,order_item_id TEXT,quantity INT)`,
		`CREATE TABLE shipment_items(shipment_id UUID,variant_id UUID,expected_qty INT,received_qty INT DEFAULT 0,inventory_received_qty INT DEFAULT 0,qc_status TEXT DEFAULT 'pending')`,
		`CREATE TABLE putaway_tasks(id TEXT,product_id TEXT,variant_id UUID,inventory_source_location_key TEXT,quantity INT,status TEXT,created_at TIMESTAMPTZ)`,
		`CREATE TRIGGER sync_fixture_stock AFTER INSERT OR UPDATE OR DELETE ON inventory_balances FOR EACH ROW EXECUTE FUNCTION public.sync_product_stock_projection()`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	red, blue := uuid.NewString(), uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products VALUES('p','s',0,2,'variable',NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode) VALUES($1,'p',1,'tracked'),($2,'p',3,'tracked')`, red, blue); err != nil {
		t.Fatal(err)
	}
	if err := SeedTrackedSKUInventoryTx(context.Background(), tx, "p", "s"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db, schema, red, blue
}

func skuScopedDB(t *testing.T, schema string) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SKU_COMMERCE_DATABASE_URL")
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid disposable database URL")
		}
		q := u.Query()
		q.Set("search_path", schema+",public")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema + ",public"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("open disposable schema database")
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSKUInventoryPaymentCallbackIsAtomicAndIdempotent(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	for _, order := range []string{"pay-one", "pay-two"} {
		if _, err := tx.Exec(`INSERT INTO orders(id,status,payment_status) VALUES($1,'processing','pending')`, order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,$2,'p',$3,1)`, uuid.NewString(), order, red); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$1,$1,$1,1000,'pending')`, order); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewOrderRepository(scoped)
	type result struct {
		order string
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, order := range []string{"pay-one", "pay-two"} {
		wg.Add(1)
		go func(order string) {
			defer wg.Done()
			_, err := repo.MarkPaymentPaid(context.Background(), order, "rrn", "masked")
			results <- result{order, err}
		}(order)
	}
	wg.Wait()
	close(results)
	winner, loser := "", ""
	for r := range results {
		if r.err == nil {
			if winner != "" {
				t.Fatal("both callbacks captured the last SKU unit")
			}
			winner = r.order
		} else if errors.Is(r.err, ErrInsufficientInventory) {
			loser = r.order
		} else {
			t.Fatalf("unexpected payment error: %v", r.err)
		}
	}
	if winner == "" || loser == "" {
		t.Fatal("expected one paid and one insufficient inventory callback")
	}
	if _, err := repo.MarkPaymentPaid(context.Background(), winner, "rrn", "masked"); err != nil {
		t.Fatal(err)
	}
	var stock, captured int
	var state string
	if err := scoped.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM order_inventory_captures`).Scan(&captured); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT status FROM payment_transactions WHERE gateway_token=$1`, loser).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if stock != 0 || captured != 1 || state != "pending" {
		t.Fatalf("non-atomic/replayed payment stock=%d captures=%d loser=%s", stock, captured, state)
	}
	if err := repo.CancelOrder(context.Background(), winner, "", true); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 1 {
		t.Fatalf("paid cancellation restored %d units, want 1", stock)
	}
}

func skuTestTx(t *testing.T, db *sql.DB, schema string) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`SET LOCAL search_path TO ` + schema + `,public`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	return tx
}

func TestSKUInventoryExactCaptureCancelAndLegacyIsolation(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	ctx := context.Background()
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('unallocated','p','s','supplier','supplier','legacy','sellable',7,TRUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'o','p',$2,1)`, id, red); err != nil {
		t.Fatal(err)
	}
	if handled, err := captureOrderInventoryTx(ctx, tx, "o"); err != nil || !handled {
		t.Fatalf("capture %v %v", handled, err)
	}
	var r, b, unallocated int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b)
	tx.QueryRow(`SELECT quantity FROM inventory_balances WHERE id='unallocated'`).Scan(&unallocated)
	if r != 0 || b != 3 || unallocated != 7 {
		t.Fatalf("capture changed wrong pool red=%d blue=%d unallocated=%d", r, b, unallocated)
	}
	for i := 0; i < 2; i++ {
		if handled, err := restoreOrderInventoryTx(ctx, tx, "o"); err != nil || !handled {
			t.Fatalf("restore %v %v", handled, err)
		}
	}
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r)
	if r != 1 {
		t.Fatalf("replayed restore stock=%d", r)
	}
}

func TestSKUInventoryLegacyUntrackedNeverGainsSKUUnits(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	sku := uuid.NewString()
	item := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products VALUES('legacy','s',5,0,'legacy',NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode) VALUES($1,'legacy',0,'legacy')`, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'legacy-order','legacy',$2,1)`, item, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "legacy-order"); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	var variantCaptured int
	if err := tx.QueryRow(`SELECT allocations,variant_quantity FROM order_inventory_captures WHERE order_item_id=$1`, item).Scan(&raw, &variantCaptured); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" || variantCaptured != 0 {
		t.Fatalf("guessed legacy SKU allocation %s / %d", raw, variantCaptured)
	}
	if _, err := restoreOrderInventoryTx(ctx, tx, "legacy-order"); err != nil {
		t.Fatal(err)
	}
	var stock, productStock int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sku).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT stock FROM products WHERE id='legacy'`).Scan(&productStock); err != nil {
		t.Fatal(err)
	}
	if stock != 0 || productStock != 5 {
		t.Fatalf("legacy cancellation minted SKU units stock=%d product=%d", stock, productStock)
	}
}

func TestSKUInventoryStockAdjustmentProtectsOtherLocationsAndSKUs(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	if err := moveSKUInventoryTx(ctx, tx, "p", "s", red, 1, "sellable", "sellable", "supplier:s", "warehouse", "warehouse:w", "test", "receipt", "stock-test-transfer", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 0, "reduce supplier declaration", "s"); err == nil {
		t.Fatal("aggregate stock edit removed warehouse-held units")
	}
	for i := 0; i < 2; i++ {
		if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 3, "replenish supplier declaration", "s"); err != nil {
			t.Fatal(err)
		}
	}
	var r, b, held int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT quantity FROM inventory_balances WHERE variant_id=$1 AND location_key='warehouse:w'`, red).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if r != 3 || b != 3 || held != 1 {
		t.Fatalf("wrong SKU/location adjusted red=%d blue=%d held=%d", r, b, held)
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "foreign", red, 5, "foreign supplier attempt", "s"); err == nil {
		t.Fatal("foreign supplier adjusted stock")
	}
}

func TestSKUInventoryMissingForeignAndZeroSKUReject(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'missing','p',NULL,1)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "missing"); err == nil {
		t.Fatal("V2 purchase omitted SKU")
	}
	foreign := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode) VALUES($1,'foreign-product',5,'tracked')`, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'foreign','p',$2,1)`, uuid.NewString(), foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "foreign"); err == nil {
		t.Fatal("foreign product SKU accepted")
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 0, "sold out fixture", "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'zero','p',$2,1)`, uuid.NewString(), red); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "zero"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("zero tracked stock accepted: %v", err)
	}
}

func TestSKUInventoryFailedQCRemainsQuarantined(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	shipment := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := ReceiveTrackedSKUShipmentItemTx(ctx, tx, shipment, red, 1, "failed"); err != nil {
			t.Fatal(err)
		}
	}
	var sellable, quarantine, tasks int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&sellable); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='quarantine'`, red).Scan(&quarantine); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM putaway_tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if sellable != 0 || quarantine != 1 || tasks != 0 {
		t.Fatalf("failed QC became sellable stock=%d quarantine=%d tasks=%d", sellable, quarantine, tasks)
	}
}

func TestSKUInventoryConcurrentLastUnitOnlyOneCapture(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	for _, order := range []string{"one", "two"} {
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,$2,'p',$3,1)`, uuid.NewString(), order, red); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, order := range []string{"one", "two"} {
		wg.Add(1)
		go func(order string) {
			defer wg.Done()
			tx, err := db.Begin()
			if err != nil {
				results <- err
				return
			}
			defer tx.Rollback()
			if _, err = tx.Exec(`SET LOCAL search_path TO ` + schema + `,public`); err == nil {
				_, err = captureOrderInventoryTx(context.Background(), tx, order)
			}
			if err == nil {
				err = tx.Commit()
			}
			results <- err
		}(order)
	}
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInsufficientInventory) {
			rejected++
		} else {
			t.Fatalf("unexpected capture error %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("success=%d rejected=%d", success, rejected)
	}
}

func TestSKUInventoryDuplicateLinesAndInactiveReject(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'duplicate','p',$2,1)`, uuid.NewString(), red); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "duplicate"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("duplicate lines bypassed stock: %v", err)
	}
	if _, err := tx.Exec(`DELETE FROM order_items`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE product_variants SET is_active=FALSE WHERE id=$1`, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($2,'inactive','p',$1,1)`, red, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "inactive"); err == nil {
		t.Fatal("captured inactive SKU")
	}
}

func TestSKUInventoryReceiptAndReturnRemainExactAndIdempotent(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	shipment := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if handled, err := ReceiveTrackedSKUShipmentItemTx(ctx, tx, shipment, red, 1, "passed"); err != nil || !handled {
			t.Fatalf("receipt %v %v", handled, err)
		}
	}
	var r, b, tasks int
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r)
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b)
	tx.QueryRow(`SELECT COUNT(*) FROM putaway_tasks`).Scan(&tasks)
	if r != 0 || b != 3 || tasks != 1 {
		t.Fatalf("receipt duplicated/mixed stock red=%d blue=%d tasks=%d", r, b, tasks)
	}
	if err := putawayTrackedSKUInventoryTx(ctx, tx, "p", "s", red, 1, "shipment:"+shipment, "A-1", "return-fixture-putaway", ""); err != nil {
		t.Fatal(err)
	}
	orderItem := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'returned','p',$2,1)`, orderItem, red); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "returned"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if handled, err := restoreCapturedReturnTx(ctx, tx, "return-item", orderItem, "p", "s", 1, "ok", "warehouse:w", ""); err != nil || !handled {
			t.Fatalf("return %v %v", handled, err)
		}
	}
	tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r)
	if r != 1 {
		t.Fatalf("return replay minted stock=%d", r)
	}
	if _, err := restoreCapturedReturnTx(ctx, tx, "second-return-item", orderItem, "p", "s", 1, "ok", "warehouse:w", ""); err == nil {
		t.Fatal("returned more than captured")
	}
	var raw []byte
	if err := tx.QueryRow(`SELECT allocations FROM order_inventory_captures WHERE order_item_id=$1`, orderItem).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var allocations []skuAllocation
	if err := json.Unmarshal(raw, &allocations); err != nil || len(allocations) != 1 {
		t.Fatalf("missing exact capture allocation %s %v", raw, err)
	}
}

func TestSKUInventoryReceiptCannotSellBeforeExactPutaway(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	shipment := uuid.NewString()
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 2, "receipt fixture", "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,2)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveTrackedSKUShipmentItemTx(ctx, tx, shipment, red, 2, "passed"); err != nil {
		t.Fatal(err)
	}
	item := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'staged','p',$2,1)`, item, red); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "staged"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("staging became purchasable: %v", err)
	}
	if err := putawayTrackedSKUInventoryTx(ctx, tx, "p", "s", red, 2, "shipment:"+shipment, "A-1", "putaway-stage", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "staged"); err != nil {
		t.Fatalf("putaway SKU not purchasable: %v", err)
	}
	var physical, sellable int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='physical'`, red).Scan(&physical); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&sellable); err != nil {
		t.Fatal(err)
	}
	if physical != 0 || sellable != 1 {
		t.Fatalf("stranded/oversold putaway physical=%d sellable=%d", physical, sellable)
	}
	var source string
	if err := tx.QueryRow(`SELECT ib.location_key FROM order_inventory_captures ic CROSS JOIN LATERAL jsonb_array_elements(ic.allocations) a JOIN inventory_balances ib ON ib.id=a->>'balanceId' WHERE ic.order_item_id=$1`, item).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "bin:A-1" {
		t.Fatalf("captured non-bin allocation: %s", source)
	}
}
