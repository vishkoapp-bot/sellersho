package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/google/uuid"
)

func TestSPNCXBackfillAdminHTTP(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(stock, lotQuantity int) (string, string) {
		t.Helper()
		suffix := strings.ToUpper(uuid.NewString()[:8])
		id, lot := "b6-"+suffix, uuid.NewString()
		exec(`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,
			stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,warehouse_name,origin_supplier_warehouse_id)
			VALUES($1,'Backfill product',$1,$2,2000,1000,0,$3,100,100,100,100,'verified','Audit warehouse',$4)`, id, f.supplier, stock, f.warehouse)
		exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode,spnc,variant_name)
			VALUES($1,$2,$3,'legacy',$4,'Backfill variant')`, uuid.NewString(), id, stock, suffix)
		exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,
			status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',
			'supplier-warehouse:'||$4,'sellable',$5,$4)`, lot, id, f.supplier, f.warehouse, lotQuantity)
		exec(`UPDATE products SET stock=$2 WHERE id=$1`, id, stock)
		return id, lot
	}
	product, lot := seed(3, 3)
	excess, _ := seed(2, 4)
	vetoed, _ := seed(1, 1)
	missing := "b6-missing"
	service := auth.NewJWTService(strings.Repeat("s", 32))
	tokenFor := func(capability string) string {
		t.Helper()
		token, err := service.GenerateTokenWithCapabilities(f.user, "09123456789", "Backfill admin", "admin", []string{capability}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	const prefix = "/api/v1/admin/spncx/"
	const reconciliation = "/api/v1/admin/inventory/reconciliation"
	mux := http.NewServeMux()
	guard := RequireCapabilities(service, "super_admin")
	mux.HandleFunc(prefix, guard(func(w http.ResponseWriter, r *http.Request) { handleAdminSpncxSubtree(w, r, db) }))
	mux.HandleFunc(reconciliation, guard(func(w http.ResponseWriter, r *http.Request) { handleAdminReconciliation(w, r, db) }))
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	superAdmin, admin := tokenFor("super_admin"), tokenFor("admin")
	for _, route := range []struct{ method, path string }{
		{"POST", prefix + "backfill"},
		{"GET", prefix + "backfill/status"},
		{"POST", prefix + "backfill/rollback"},
		{"POST", prefix + "backfill/excess-decision"},
		{"GET", reconciliation},
	} {
		checkSPNCXStatus(t, call(route.method, route.path, "", ""), 401, `{"error":"Unauthorized"}`)
		checkSPNCXStatus(t, call(route.method, route.path, admin, ""), 403, `{"error":"Forbidden"}`)
		wrong := "POST"
		if route.method == "POST" {
			wrong = "GET"
		}
		checkSPNCXStatus(t, call(wrong, route.path, superAdmin, ""), 404, "404 page not found")
	}
	for _, path := range []string{"", "backfill/nope", "backfill/", "backfill/status/"} {
		checkSPNCXStatus(t, call("GET", prefix+path, superAdmin, ""), 404, "404 page not found")
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		return call("POST", prefix+"backfill"+path, superAdmin, body)
	}
	postPlan := func(dry bool, id, fields string) *httptest.ResponseRecorder {
		return post("", fmt.Sprintf(`{"dry_run":%t,"product_ids":[%q]%s}`, dry, id, fields))
	}
	checkError := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		checkSPNCXJSON(t, w, status, fmt.Sprintf(`{"error":%q}`, code))
	}
	decode := func(w *httptest.ResponseRecorder, target any) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("got HTTP %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), target); err != nil {
			t.Fatal(err)
		}
	}
	checkError(post("", `{}`), 400, "invalid_request")
	checkError(post("", `{"dry_run":true}{ }`), 400, "invalid_request")
	checkError(postPlan(true, missing, ""), 404, "product_not_found")
	checkError(postPlan(false, product, ""), 409, "dry_run_required")
	checkError(postPlan(false, missing, ""), 404, "product_not_found")
	checkError(post("/rollback", fmt.Sprintf(`{"product_id":%q}`, missing)), 404, "product_not_found")
	checkError(post("/rollback", fmt.Sprintf(`{"product_id":%q}`, product)), 409, "rollback_not_completed")
	checkError(post("/rollback", fmt.Sprintf(`{"product_id":%q,"all":true}`, product)), 400, "invalid_request")

	unitCount := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM product_units`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := unitCount()
	var plan map[string]json.RawMessage
	decode(postPlan(true, product, ""), &plan)
	keys := strings.Fields("dry_run plan_hash basis total_units product_count group_totals products global_assertions demo_test_products inactive_products pre_cutover_orders diff_vs_previous_dry_run")
	for _, key := range keys {
		if _, ok := plan[key]; !ok {
			t.Errorf("missing dry-run field %s", key)
		}
	}
	var products []struct {
		ProductID string `json:"product_id"`
	}
	if err := json.Unmarshal(plan["products"], &products); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := json.Unmarshal(plan["plan_hash"], &hash); err != nil {
		t.Fatal(err)
	}
	if len(plan) != len(keys) || string(plan["dry_run"]) != "true" || string(plan["basis"]) != `"products.stock"` ||
		!strings.HasPrefix(hash, "sha256:") || len(products) != 1 || products[0].ProductID != product || unitCount() != before {
		t.Fatalf("unexpected dry-run response: %s", plan)
	}
	var persisted bool
	if err := db.QueryRow(`SELECT plan_hash=$2 AND NOT (plan ? 'plan_hash') FROM spncx_backfill_state WHERE product_id=$1`,
		product, hash).Scan(&persisted); err != nil || !persisted {
		t.Fatalf("plan persistence: %t, %v", persisted, err)
	}

	decide := func(id, decision string) *httptest.ResponseRecorder {
		return post("/excess-decision", fmt.Sprintf(`{"product_id":%q,"excess_decision":%q,"excess_decided_by":"spoof"}`, id, decision))
	}
	checkError(decide("", ""), 422, "invalid_excess_decision")
	checkError(post("/excess-decision", `{"product_id":true,"excess_decision":123}`), 422, "invalid_excess_decision")
	checkError(decide("", "write_off_accepted"), 400, "invalid_request")
	checkError(decide(missing, "write_off_accepted"), 404, "product_not_found")
	checkError(decide(excess, "write_off_accepted"), 409, "no_excess_to_decide")
	checkError(decide(product, "write_off_accepted"), 409, "no_excess_to_decide")
	decode(postPlan(true, excess, ""), &map[string]any{})
	var decision struct {
		DecidedBy string `json:"excess_decided_by"`
		Amount    int    `json:"excess_amount"`
	}
	decode(decide(excess, "write_off_accepted"), &decision)
	checkError(post("", fmt.Sprintf(`{"dry_run":false,"product_ids":[%q,%q]}`, product, excess)), 409, "plan_hash_mismatch")
	if decision.DecidedBy != f.user || decision.Amount != 2 {
		t.Fatalf("excess decision: %+v", decision)
	}
	exec(`UPDATE inventory_balances SET quantity=2 WHERE id=$1`, lot)
	var variant string
	if err := db.QueryRow(`SELECT id FROM product_variants WHERE product_id=$1`, product).Scan(&variant); err != nil {
		t.Fatal(err)
	}
	// The projection changes P from 3 to 2 while the legacy variant stays at 3.
	checkSPNCXJSON(t, postPlan(false, product, ""), 409, fmt.Sprintf(
		`{"error":"plan_diff_requires_reapproval","diff":{"previous_plan_hash":%q,"changed_products":[{"product_id":%q,"alloc_changed":true,"top_up_changed":false,"excess_changed":false}]}}`, hash, product))
	run := postPlan(false, product, `,"confirm_plan_diff":true`)
	checkSPNCXJSON(t, run, 200, fmt.Sprintf(
		`{"dry_run":false,"plan_hash":%q,"completed":1,"skipped_completed":0,"failed":0,"excluded_skipped":0,"total_minted":2,"products":[{"product_id":%q,"status":"completed","units_minted":2,"units_target":2,"flipped_variants":[%q],"excess":0}]}`, hash, product, variant))
	checkSPNCXJSON(t, postPlan(false, product, ""), 200, fmt.Sprintf(
		`{"dry_run":false,"plan_hash":%q,"completed":0,"skipped_completed":1,"failed":0,"excluded_skipped":0,"total_minted":0,"products":[{"product_id":%q,"status":"skipped_completed","units_minted":0,"units_target":0,"flipped_variants":[],"excess":0}]}`, hash, product))

	var report struct {
		GeneratedAt    time.Time             `json:"generated_at"`
		Checks         []struct{ ID string } `json:"checks"`
		HeldLots       *[]json.RawMessage    `json:"held_lots"`
		CoverageGaps   *[]json.RawMessage    `json:"coverage_gaps"`
		FailedCaptures *[]json.RawMessage    `json:"failed_captures"`
	}
	decode(call("GET", reconciliation, superAdmin, ""), &report)
	var checkIDs []string
	for _, check := range report.Checks {
		checkIDs = append(checkIDs, check.ID)
	}
	if strings.Join(checkIDs, " ") != "V0 V1 V1b V1c V2 V3 V4 V5 V6 V6b V7 V8 V9 V10 V11 V12" ||
		report.GeneratedAt.IsZero() || report.HeldLots == nil || report.CoverageGaps == nil || report.FailedCaptures == nil {
		t.Fatalf("unexpected reconciliation report: %+v", report)
	}
	decode(call("GET", prefix+"backfill/status", superAdmin, ""), &map[string]any{})

	var printed string
	if err := db.QueryRow(`SELECT code FROM product_units WHERE product_id=$1 ORDER BY sequence LIMIT 1`, product).Scan(&printed); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE product_units SET printed_at=NOW(),print_count=1 WHERE code=$1`, printed)
	exec(`INSERT INTO product_unit_events(unit_id,code,event_type,reference_type,reference_id)
		SELECT id,code,'printed','label_print','b6-print' FROM product_units WHERE code=$1`, printed)
	rollback := func(confirm bool) *httptest.ResponseRecorder {
		return post("/rollback", fmt.Sprintf(`{"product_id":%q,"confirm_printed_destruction":%t}`, product, confirm))
	}
	checkSPNCXJSON(t, rollback(false), 409, fmt.Sprintf(
		`{"error":"rollback_needs_printed_confirmation","printed_codes_to_destroy":[%q]}`, printed))
	checkSPNCXJSON(t, post("/rollback", `{"all":true}`), 200, fmt.Sprintf(
		`{"all":true,"results":[{"product_id":%q,"rolled_back":false,"printed_codes_to_destroy":[%q],"restored_snapshot":false,"error":"rollback_needs_printed_confirmation"}]}`, product, printed))
	checkSPNCXJSON(t, rollback(true), 200, fmt.Sprintf(
		`{"product_id":%q,"rolled_back":true,"printed_codes_to_destroy":[%q],"restored_snapshot":true}`, product, printed))

	exec(`INSERT INTO spncx_backfill_state(product_id,status,units_target,excluded_reason,excluded_by,excluded_at)
		VALUES($1,'excluded',0,'owner veto',$2,NOW())`, vetoed, f.user)
	decode(postPlan(true, vetoed, ""), &map[string]any{})
	checkError(postPlan(false, vetoed, fmt.Sprintf(`,"reinclude":{%q:""}`, vetoed)), 422, "reinclude_reason_required")
	checkError(postPlan(false, vetoed, fmt.Sprintf(`,"reinclude":{%q:"not excluded"}`, excess)), 422, "product_not_excluded")
	var reincluded struct{ Completed int }
	decode(postPlan(false, vetoed, fmt.Sprintf(`,"reinclude":{%q:"owner veto lifted"}`, vetoed)), &reincluded)
	var audited bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM audit_logs WHERE action='spncx_backfill_reinclude'
		AND category='warehouse' AND operator=$1 AND details::jsonb->>'product_id'=$2
		AND details::jsonb->>'reason'='owner veto lifted' AND details::jsonb->>'previous_excluded_reason'='owner veto'
		AND details::jsonb->>'previous_excluded_by'=$1)`, f.user, vetoed).Scan(&audited); err != nil || !audited || reincluded.Completed != 1 {
		t.Fatalf("re-include: audited=%t, run=%+v, err=%v", audited, reincluded, err)
	}
}
