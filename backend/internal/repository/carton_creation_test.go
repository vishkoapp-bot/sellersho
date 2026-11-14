package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func cartonFixture(t *testing.T) (*sql.DB, *SupplierRepository, string, string) {
	t.Helper()
	_, schema, red, blue := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	for _, q := range []string{

		`DROP INDEX carton_creation_key_unique`,
		`ALTER TABLE shipments DROP COLUMN creation_key, DROP COLUMN request_fingerprint`,
		`CREATE TABLE inventory_reservations(id TEXT PRIMARY KEY)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	db = inboundFixtureDB(t, schema)
	return db, NewSupplierRepository(db), red, blue
}

func assertCartonCounts(t *testing.T, db *sql.DB, headers, lines int) {
	t.Helper()
	var h, l, res, movements int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM shipments),(SELECT COUNT(*) FROM shipment_items),(SELECT COUNT(*) FROM inventory_reservations),(SELECT COUNT(*) FROM inventory_movements)`).Scan(&h, &l, &res, &movements); err != nil {
		t.Fatal(err)
	}
	if h != headers || l != lines || res != 0 || movements != lines {
		t.Fatalf("writes: headers=%d lines=%d reservations=%d movements=%d; want %d/%d/0/booking-lines", h, l, res, movements, headers, lines)
	}
}

func TestCartonCreationRejectsBeforeWrites(t *testing.T) {
	for _, kind := range []string{"overflow-second-line", "cumulative-overflow", "duplicate", "foreign"} {
		t.Run(kind, func(t *testing.T) {
			db, repo, red, blue := cartonFixture(t)
			items := []CartonShipmentItem{{red, 1}, {blue, 2147483648}}
			switch kind {
			case "cumulative-overflow":
				items = []CartonShipmentItem{{red, 2147483647}, {blue, 1}}
			case "duplicate":
				items = []CartonShipmentItem{{red, 1}, {red, 1}}
			case "foreign":
				if _, err := db.Exec(`UPDATE products SET supplier_id='other'`); err != nil {
					t.Fatal(err)
				}
				items = []CartonShipmentItem{{red, 1}}
			}
			if _, err := repo.CreateCartonShipment(context.Background(), "s", items, "w1"); err == nil {
				t.Fatal("invalid carton accepted")
			}
			assertCartonCounts(t, db, 0, 0)
		})
	}
}

func TestCartonCreationRollsBackAfterFirstLine(t *testing.T) {
	db, repo, red, blue := cartonFixture(t)
	if _, err := db.Exec(`CREATE FUNCTION fail_second_carton_line() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM shipment_items WHERE shipment_id=NEW.shipment_id) THEN RAISE EXCEPTION 'injected second line failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_second BEFORE INSERT ON shipment_items FOR EACH ROW EXECUTE FUNCTION fail_second_carton_line()`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}, {blue, 1}}, "w1"); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("expected injected failure, got %v", err)
	}
	assertCartonCounts(t, db, 0, 0)
}

func TestCartonCreationReplayConcurrentAndResponseLoss(t *testing.T) {
	db, repo, red, blue := cartonFixture(t)
	items := []CartonShipmentItem{{red, 1}, {blue, 2}}
	type result struct {
		shipment *CartonShipment
		err      error
	}
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := repo.CreateCartonShipment(context.Background(), "s", items, "w1", "same-key")
			results <- result{s, e}
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if id != "" && id != r.shipment.ID {
			t.Fatal("same key created multiple shipments")
		}
		id = r.shipment.ID
	}
	// The first response can be lost; a later retry, including reordered lines,
	// must return the durable shipment rather than create another physical claim.
	replay, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{items[1], items[0]}, "w1", "same-key")
	if err != nil || replay.ID != id {
		t.Fatalf("response loss replay: %+v %v", replay, err)
	}
	if _, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 2}}, "w1", "same-key"); err == nil {
		t.Fatal("changed payload accepted")
	}
	if _, err := repo.CreateCartonShipment(context.Background(), "s", items, "w2", "same-key"); err == nil {
		t.Fatal("changed destination accepted")
	}
	assertCartonCounts(t, db, 1, 2)
}

func TestCartonCreationCodeCollisionRetriesAtomically(t *testing.T) {
	db, repo, red, _ := cartonFixture(t)
	if _, err := db.Exec(`INSERT INTO shipments(id,shipment_code,status) VALUES('00000000-0000-4000-8000-000000000001','COLLIDE1','draft'); CREATE SEQUENCE carton_code_attempt; CREATE FUNCTION collide_first_carton_code() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF nextval('carton_code_attempt')=1 THEN NEW.shipment_code='COLLIDE1'; END IF; RETURN NEW; END $$; CREATE TRIGGER collide_first BEFORE INSERT ON shipments FOR EACH ROW EXECUTE FUNCTION collide_first_carton_code()`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1"); err != nil {
		t.Fatal(err)
	}
	assertCartonCounts(t, db, 2, 1)
}

func TestCartonCreationRetriesWholeTransactionAfterLockTimeout(t *testing.T) {
	db, repo, red, _ := cartonFixture(t)
	owner, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback()
	if _, err := owner.Exec(`SELECT id FROM suppliers WHERE id='s' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1", "retry-key")
		done <- err
	}()
	time.Sleep(1800 * time.Millisecond)
	assertCartonCounts(t, db, 0, 0)
	if err := owner.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("whole transaction retry failed: %v", err)
	}
	assertCartonCounts(t, db, 1, 1)
}

func TestCartonMigrationHistoricalExceptionsAreReadOnly(t *testing.T) {
	db, repo, red, _ := cartonFixture(t)
	carton, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1", "historical")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipments(id,shipment_code,supplier_id,status,warehouse_id,supplier_warehouse_id) VALUES('00000000-0000-4000-8000-000000000001','ORPHAN','s','draft','w1','sw1'),('00000000-0000-4000-8000-000000000002','MATCH','s','draft','w1','sw1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES('00000000-0000-4000-8000-000000000002',$1,1)`, red); err != nil {
		t.Fatal(err)
	}
	var orphan, claims int
	if err := db.QueryRow(`SELECT COUNT(*) FILTER(WHERE reason='orphan_header'),COUNT(*) FILTER(WHERE reason='matching_physical_claim_review') FROM carton_creation_reconciliation`).Scan(&orphan, &claims); err != nil || orphan != 1 || claims != 2 {
		t.Fatalf("historical candidates: %d/%d %v", orphan, claims, err)
	}
	if _, err := db.Exec(`DROP INDEX carton_variant_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty,received_qty) VALUES($1,$2,7,3)`, carton.ID, red); err != nil {
		t.Fatal(err)
	}
	preflight, err := os.ReadFile("../../../scripts/carton-creation-preflight.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(preflight)); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err == nil || !strings.Contains(err.Error(), "explicit reconciliation") {
		t.Fatalf("duplicate history migration should refuse: %v", err)
	}
	var h, l, received int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM shipments),(SELECT COUNT(*) FROM shipment_items),(SELECT SUM(received_qty) FROM shipment_items)`).Scan(&h, &l, &received); err != nil || h != 3 || l != 3 || received != 3 {
		t.Fatalf("history changed: %d/%d/%d %v", h, l, received, err)
	}
}

func TestCartonMigrationDownUpAndUniqueIdentity(t *testing.T) {
	db, repo, red, _ := cartonFixture(t)
	down, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	carton, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, carton.ID, red); err == nil {
		t.Fatal("duplicate variant constraint absent")
	}
	if _, err := db.Exec(`INSERT INTO shipments(id,shipment_code,supplier_id,status,creation_key,request_fingerprint) SELECT gen_random_uuid(),'OTHER','s','draft',creation_key,request_fingerprint FROM shipments WHERE id=$1`, carton.ID); err == nil {
		t.Fatal("supplier key uniqueness absent")
	}
	assertCartonCounts(t, db, 1, 1)
}
