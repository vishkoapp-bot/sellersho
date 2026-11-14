package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/drup/platform/internal/adguard"
	"github.com/drup/platform/internal/testdb"
	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func operationsDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("OPERATIONS_TEST_DATABASE_URL must point at a disposable ops_test database")
	}
	db, e := testdb.Open(t, url)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func opsExec(t *testing.T, db *sql.DB, q string, args ...interface{}) {
	t.Helper()
	if _, e := db.Exec(q, args...); e != nil {
		t.Fatalf("%s: %v", q, e)
	}
}

type opsFixture struct{ user, supplier, store, slug, product string }

func opsFixtureNew(t *testing.T, db *sql.DB) opsFixture {
	t.Helper()
	suffix := uuid.NewString()[:8]
	f := opsFixture{uuid.NewString(), "os-" + suffix, "ot-" + suffix, "ops-" + suffix, "op-" + suffix}
	opsExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'ops test','supplier')`, f.user, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9))
	opsExec(t, db, `INSERT INTO suppliers(id,name,owner_user_id,approval_status) VALUES($1,'ops supplier',$2,'approved')`, f.supplier, f.user)
	opsExec(t, db, `INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,status) VALUES($1,'ops store',$2,'ops',$3,'standard','active')`, f.store, f.slug, f.user)
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,warehouse_name) VALUES($1,'ops product',$1,$2,2000,1000,0,100,100,100,100,100,'verified','ops warehouse')`, f.product, f.supplier)
	if err := NewSupplierRepository(db).RecordProductMeasurement(context.Background(), f.product, f.user, 100, 100, 100, 100, "measured operations fixture"); err != nil {
		t.Fatal(err)
	}
	return f
}
func opsOrder(t *testing.T, db *sql.DB, f opsFixture, paid bool) (string, string) {
	t.Helper()
	id := "oo-" + uuid.NewString()[:12]
	payment := "pending"
	if paid {
		payment = "paid"
	}
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status) VALUES($1,$1,$2,'ops',2000,2000,'ops','09123456789','x','x','x','1234512345',$1,$3,$4)`, id, f.store, f.user, payment)
	opsExec(t, db, `INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,1,1000,0,1000,2000)`, uuid.NewString(), id, f.product)
	token := "tok-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES($1,$2,$2,$3,2000,$4)`, "pay-"+uuid.NewString(), id, token, payment)
	return id, token
}

func TestOperationsOrderExpiryAndPaymentRace(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	r := NewOrderRepository(db)
	expired, expiredToken := opsOrder(t, db, f, false)
	paid, token := opsOrder(t, db, f, false)
	warehouse := seedCanonicalWarehouseStock(t, db, f.product, f.supplier, 100)
	seedCanonicalWarehouseOrderOrigins(t, db, expired, warehouse)
	seedCanonicalWarehouseOrderOrigins(t, db, paid, warehouse)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, expired)
	var wg sync.WaitGroup
	var expiryErr, paymentErr error
	rrn := "rrn-" + uuid.NewString()[:20]
	wg.Add(2)
	go func() { defer wg.Done(); _, expiryErr = r.ExpireUnpaidOrders(ctx) }()
	go func() { defer wg.Done(); _, paymentErr = r.MarkPaymentPaid(ctx, token, rrn, "mask") }()
	wg.Wait()
	if expiryErr != nil || paymentErr != nil {
		t.Fatalf("expiry=%v payment=%v", expiryErr, paymentErr)
	}
	if _, e := r.MarkPaymentPaid(ctx, expiredToken, "rrn", "mask"); !errors.Is(e, ErrPaymentExpired) {
		t.Fatalf("resurrected expired payment: %v", e)
	}
	if _, e := r.MarkPaymentPaid(ctx, token, rrn, "mask"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.ExpireUnpaidOrders(ctx); e != nil {
		t.Fatal(e)
	}
	var stock int
	var status string
	db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock)
	db.QueryRow(`SELECT payment_status FROM orders WHERE id=$1`, paid).Scan(&status)
	if stock != 99 || status != "paid" {
		t.Fatalf("stock=%d status=%s", stock, status)
	}
}
func TestOperationsQCDeadline(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	r := NewSupplierRepository(db)
	id := "qc-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO qc_discrepancies(id,supplier_id,product_id,expected_quantity,received_quantity,defect_count,reason,status,response_due_at) VALUES($1,$2,$3,2,1,0,'shortage','open',NOW()-INTERVAL '1 second')`, id, f.supplier, f.product)
	if e := r.RespondDiscrepancy(ctx, f.supplier, id, "response", ""); !errors.Is(e, ErrQCResponseExpired) {
		t.Fatal(e)
	}
	if _, e := EscalateQCDeadlines(ctx, db); e != nil {
		t.Fatal(e)
	}
	if _, e := EscalateQCDeadlines(ctx, db); e != nil {
		t.Fatal(e)
	}
	var escalated bool
	db.QueryRow(`SELECT escalated_at IS NOT NULL AND status='open' FROM qc_discrepancies WHERE id=$1`, id).Scan(&escalated)
	if !escalated {
		t.Fatal("not escalated")
	}
	opsExec(t, db, `UPDATE qc_discrepancies SET response_due_at=NOW()+INTERVAL '48 hours',escalated_at=NULL WHERE id=$1`, id)
	if e := r.RespondDiscrepancy(ctx, "not-owner", id, "response", ""); e == nil {
		t.Fatal("ownership bypass")
	}
	if e := r.RespondDiscrepancy(ctx, f.supplier, id, "response", ""); e != nil {
		t.Fatal(e)
	}
	if e := r.RespondDiscrepancy(ctx, f.supplier, id, "overwrite", ""); e == nil {
		t.Fatal("response overwritten")
	}
}
func TestOperationsSLAThresholdAndManualOverride(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	id, _ := opsOrder(t, db, f, true)
	add := func(name, status string) {
		shipped := "NULL"
		if status == "shipped" {
			shipped = "NOW()-INTERVAL '2 days'"
		}
		opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,created_at,shipped_at) VALUES($1,$2,$3,'{}','09123456789','ops',$4,NOW()-INTERVAL '3 days',`+shipped+`)`, name, id, f.supplier, status)
	}
	if _, e := RefreshSupplierSLA(ctx, db); e != nil {
		t.Fatal(e)
	}
	var eligible bool
	db.QueryRow(`SELECT eligible FROM supplier_sla_metrics WHERE supplier_id=$1`, f.supplier).Scan(&eligible)
	if eligible {
		t.Fatal("no history grant")
	}
	for i := 0; i < 49; i++ {
		add(fmt.Sprintf("sla-%s-%d", f.supplier, i), "shipped")
	}
	add("sla-fail-"+f.supplier, "failed")
	if _, e := RefreshSupplierSLA(ctx, db); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT eligible FROM supplier_sla_metrics WHERE supplier_id=$1`, f.supplier).Scan(&eligible)
	if eligible {
		t.Fatal("98 is not ABOVE 98")
	}
	add("sla-extra-"+f.supplier, "shipped")
	if _, e := RefreshSupplierSLA(ctx, db); e != nil {
		t.Fatal(e)
	}
	var status, actor string
	db.QueryRow(`SELECT status,decided_by FROM supplier_shipping_methods WHERE owner_user_id=$1 AND method='direct'`, f.user).Scan(&status, &actor)
	if status != "enabled" || actor != "system:sla" {
		t.Fatal(status, actor)
	}
	opsExec(t, db, `UPDATE supplier_shipping_methods SET status='disabled',decided_by='manual-admin' WHERE owner_user_id=$1 AND method='direct'`, f.user)
	if _, e := RefreshSupplierSLA(ctx, db); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT status FROM supplier_shipping_methods WHERE owner_user_id=$1 AND method='direct'`, f.user).Scan(&status)
	if status != "disabled" {
		t.Fatal("overrode admin")
	}
}
func TestOperationsAdDeduplication(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	campaign := uuid.NewString()
	opsExec(t, db, `INSERT INTO seller_campaigns(id,store_id,name,campaign_type,status,product_ids) VALUES($1,$2,'ops','link','active','[]')`, campaign, f.store)
	r := NewStoreRepository(db)
	ctx := context.Background()
	ev := adguard.Evidence{IPHash: "ip-a", FingerprintHash: "fp-a"}
	ok, e := r.RecordSellerCampaignEvent(ctx, campaign, "click", "token-a", "", ev)
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	for _, ev := range []adguard.Evidence{{IPHash: "ip-a", FingerprintHash: "fp-b"}, {IPHash: "ip-b", FingerprintHash: "fp-a"}} {
		if ok, e = r.RecordSellerCampaignEvent(ctx, campaign, "click", uuid.NewString(), "", ev); e != nil || ok {
			t.Fatal(ok, e)
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM seller_campaign_events WHERE campaign_id=$1`, campaign).Scan(&n)
	if n != 1 {
		t.Fatal(n)
	}
	opsExec(t, db, `UPDATE ad_fraud_keys SET expires_at=NOW()-INTERVAL '1 second' WHERE campaign_id=$1`, campaign)
	if ok, e = r.RecordSellerCampaignEvent(ctx, campaign, "click", "token-later", "", ev); e != nil || !ok {
		t.Fatal(ok, e)
	}
}
func TestOperationsNearestBinCapacity(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "ops-warehouse-" + f.slug
	opsExec(t, db, `UPDATE products SET warehouse_name=$2 WHERE id=$1`, f.product, warehouse)
	task := "put-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO putaway_tasks(id,product_id,quantity,status) VALUES($1,$2,2,'pending')`, task, f.product)
	near, far := "bn-"+uuid.NewString()[:8], "bf-"+uuid.NewString()[:8]
	for i, bin := range []string{near, far} {
		opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status,position_x,position_y,position_z) VALUES($1,'h','a','s',$3,10000,1000,'verified',$2,0,0)`, bin, i*10+1, warehouse)
	}
	r := NewWarehouseRepository(db)
	ctx := context.Background()
	rows, e := r.SuggestBins(ctx, task, warehouseopt.Point{})
	if e != nil || len(rows) < 2 || rows[0].BinCode != near {
		t.Fatal(rows, e)
	}
	opsExec(t, db, `UPDATE warehouse_bins SET occupied_weight_grams=900 WHERE bin_code=$1`, near)
	rows, e = r.SuggestBins(ctx, task, warehouseopt.Point{})
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range rows {
		if b.BinCode == near {
			t.Fatal("overweight bin suggested")
		}
	}
	if e = r.AssignPutawayTask(ctx, task, near, f.user); e == nil {
		t.Fatal("assignment bypassed weight gate")
	}
	if e = r.AssignPutawayTask(ctx, task, far, f.user); e != nil {
		t.Fatal(e)
	}
	var volume int
	db.QueryRow(`SELECT volume_cm3 FROM putaway_capacity_reservations WHERE task_id=$1`, task).Scan(&volume)
	if volume != 2000 {
		t.Fatalf("wrong units: %d", volume)
	}
	opsExec(t, db, `UPDATE products SET weight_grams=200 WHERE id=$1`, f.product)
	if e = r.ConfirmPutaway(ctx, task, f.user); e == nil {
		t.Fatal("changed measurements bypassed reservation")
	}
	opsExec(t, db, `UPDATE products SET warehouse_name='other warehouse' WHERE id=$1`, f.product)
	if e = r.AssignPutawayTask(ctx, task, far, f.user); e == nil {
		t.Fatal("cross-warehouse assignment accepted")
	}
}

type operationsDNS struct{ token string }

func TestOperationsPickingRouteCanonicalQuantities(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	one, _ := opsOrder(t, db, f, true)
	two, _ := opsOrder(t, db, f, true)
	near, far := "rn-"+uuid.NewString()[:8], "rf-"+uuid.NewString()[:8]
	for i, bin := range []string{near, far} {
		opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,warehouse_name,position_x,position_y,position_z) VALUES($1,'h','a','s','ops warehouse',$2,0,0)`, bin, i*7+1)
		opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,bin_code,status,quantity,reconciled) VALUES($1,$2,$3,'supplier','bin',$4,$4,'sellable',1,TRUE)`, "ib-"+uuid.NewString(), f.product, f.supplier, bin)
	}
	repo := NewWarehouseRepository(db)
	route, e := repo.OptimizePicking(context.Background(), []string{one, two}, "ops warehouse", warehouseopt.Point{})
	if e != nil || len(route.Stops) != 2 || len(route.MissingBins) != 0 || route.Distance != 16 {
		t.Fatalf("%+v %v", route, e)
	}
	three, _ := opsOrder(t, db, f, true)
	route, e = repo.OptimizePicking(context.Background(), []string{one, two, three}, "ops warehouse", warehouseopt.Point{})
	if e != nil || len(route.MissingBins) != 1 {
		t.Fatalf("phantom stock allocated: %+v %v", route, e)
	}
	unpaid, _ := opsOrder(t, db, f, false)
	if _, e = repo.OptimizePicking(context.Background(), []string{unpaid}, "ops warehouse", warehouseopt.Point{}); e == nil {
		t.Fatal("unpaid order planned")
	}
}

func TestOperationsAutomaticSLARevocation(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	order, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,created_at,shipped_at) VALUES($1,$2,$3,'{}','09123456789','ops','shipped',NOW()-INTERVAL '3 days',NOW()-INTERVAL '2 days')`, "ss-"+uuid.NewString(), order, f.supplier)
	if _, e := RefreshSupplierSLA(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	var status string
	db.QueryRow(`SELECT status FROM supplier_shipping_methods WHERE owner_user_id=$1 AND method='direct'`, f.user).Scan(&status)
	if status != "enabled" {
		t.Fatal(status)
	}
	opsExec(t, db, `UPDATE supplier_shipments SET status='failed' WHERE supplier_id=$1`, f.supplier)
	if _, e := RefreshSupplierSLA(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	db.QueryRow(`SELECT status FROM supplier_shipping_methods WHERE owner_user_id=$1 AND method='direct'`, f.user).Scan(&status)
	if status != "disabled" {
		t.Fatal("automatic grant not revoked")
	}
}

func (f operationsDNS) LookupTXT(context.Context, string) ([]string, error) {
	return []string{f.token}, nil
}
func (f operationsDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("203.0.113.8")}}, nil
}
func TestOperationsDomainApprovalOwnershipAndDisable(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	r := NewStoreRepository(db)
	ctx := context.Background()
	domain := f.slug + ".example.com"
	d, e := r.RequestDomain(ctx, f.user, f.slug, domain)
	if e != nil {
		t.Fatal(e)
	}
	dns := operationsDNS{d.Token}
	if e = r.VerifyOwnedDomain(ctx, f.user, domain, "203.0.113.8", dns); e == nil {
		t.Fatal("unapproved domain activated")
	}
	opsExec(t, db, `UPDATE pending_approvals SET status='approved' WHERE target_id=$1 AND entity_type='custom_domain'`, f.store)
	if e = r.VerifyOwnedDomain(ctx, "wrong", domain, "203.0.113.8", dns); e == nil {
		t.Fatal("other owner verified")
	}
	if e = r.VerifyOwnedDomain(ctx, f.user, domain, "203.0.113.8", dns); e != nil {
		t.Fatal(e)
	}
	if slug, e := r.AllowedDomain(ctx, domain); e != nil || slug != f.slug {
		t.Fatal(slug, e)
	}
	if e = r.DisableOwnedDomain(ctx, f.user, domain); e != nil {
		t.Fatal(e)
	}
	if _, e = r.AllowedDomain(ctx, domain); e == nil {
		t.Fatal("disabled domain allowed")
	}
	d2, e := r.RequestDomain(ctx, f.user, f.slug, domain)
	if e != nil || d2.Token == d.Token {
		t.Fatal("domain re-registration failed or reused proof", e)
	}
	if e = r.VerifyOwnedDomain(ctx, f.user, domain, "203.0.113.8", operationsDNS{d2.Token}); e == nil {
		t.Fatal("re-registration bypassed fresh approval")
	}
}
