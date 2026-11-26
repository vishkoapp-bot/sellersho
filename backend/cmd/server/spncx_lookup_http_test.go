package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// lookupHTTPSeed stages one tracked variant + sellable lot + two in-stock
// units (codes SPNC-X0001/X0002) for a supplierFlow fixture product, plus one
// minted event for the first unit. Unit rows are inserted directly: the
// mint helper lives in the repository package and is not reachable here.
func lookupHTTPSeed(t *testing.T, db *sql.DB, f supplierFlowFixture, spnc string) (variant, lot string) {
	t.Helper()
	variant = uuid.NewString()
	lot = uuid.NewString()
	if _, err := db.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode,spnc,variant_name)
		VALUES($1,$2,2,'tracked',$3,'Lookup variant')`, variant, f.product, spnc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_name,bin_code)
		VALUES($1,$2,$3,$4,'supplier','supplier','supplier-warehouse:'||$5,'sellable',2,'Audit warehouse',NULL)`,
		lot, f.product, f.supplier, variant, f.warehouse); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int{1, 2} {
		code := fmt.Sprintf("%s-X%04d", spnc, seq)
		if _, err := db.Exec(`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
			VALUES($1,$2,$3,$4,'in_stock',$5)`, code, variant, f.product, seq, lot); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id)
		SELECT id,code,'minted',NULL,'in_stock','fixture',NULL,NULL FROM product_units WHERE code=$1`, spnc+"-X0001"); err != nil {
		t.Fatal(err)
	}
	return variant, lot
}

// checkSPNCXStatus asserts status + exact body for the guard rejections,
// which http.Error renders as text/plain (so checkSPNCXJSON's
// application/json requirement does not apply to them).
func checkSPNCXStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if recorder.Code != status || strings.TrimSpace(recorder.Body.String()) != body {
		t.Fatalf("got HTTP %d %s, want HTTP %d %s", recorder.Code, recorder.Body, status, body)
	}
}

func TestSPNCXLookupHTTP(t *testing.T) {
	db := supplierFlowDB(t)
	a := supplierFlowSeed(t, db)
	b := supplierFlowSeed(t, db)
	aVariant, _ := lookupHTTPSeed(t, db, a, "A0C00001")
	lookupHTTPSeed(t, db, b, "B0C00001")
	supplierRepo := repository.NewSupplierRepository(db)

	// No capability reader: token capabilities ([role]) govern, exactly as
	// the other role-matrix tests in this package do. The supplier tokens
	// resolve an active supplier id through the repository.
	jwtService := auth.NewJWTService(strings.Repeat("s", 32))
	tokenFor := func(user, role string) string {
		token, err := jwtService.GenerateToken(user, "09123456789", "Lookup test", role, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	guard := RequireRoles(jwtService, "admin", "warehouse_staff", "logistics_specialist", "supplier")
	dispatch := guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/units" {
			HandleListUnits(w, r, db, supplierRepo)
			return
		}
		HandleGetUnit(w, r, db, supplierRepo, strings.TrimPrefix(r.URL.Path, "/api/v1/units/"))
	})
	call := func(method, path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		dispatch(recorder, req)
		return recorder
	}

	// Unauthenticated → 401; wrong role → 403.
	checkSPNCXStatus(t, call(http.MethodGet, "/api/v1/units/A0C00001-X0001", ""),
		401, `{"error":"Unauthorized"}`)
	checkSPNCXStatus(t, call(http.MethodGet, "/api/v1/units/A0C00001-X0001", tokenFor(a.user, "customer")),
		403, `{"error":"Forbidden"}`)

	// Owning supplier resolves their own unit with the full §7.1 shape.
	recorder := call(http.MethodGet, "/api/v1/units/A0C00001-X0001", tokenFor(a.user, "supplier"))
	var unit repository.UnitLookupResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &unit); err != nil || recorder.Code != http.StatusOK ||
		unit.Code != "A0C00001-X0001" || unit.Status != "in_stock" ||
		unit.Variant.ID != aVariant || unit.Variant.SPNC != "A0C00001" ||
		unit.Product.ID != a.product || unit.Location == nil ||
		unit.Location.LotStatus != "sellable" || unit.Order != nil ||
		len(unit.History) != 1 || unit.History[0].EventType != "minted" {
		t.Fatalf("own lookup: HTTP %d %s (%v)", recorder.Code, recorder.Body, err)
	}

	// Cross-supplier and unknown codes are the same 404 with the same body.
	for _, code := range []string{"B0C00001-X0001", "A0C00001-X9999"} {
		checkSPNCXJSON(t, call(http.MethodGet, "/api/v1/units/"+code, tokenFor(a.user, "supplier")),
			404, `{"error":"unit_not_found"}`)
	}
	// The other supplier resolves their own unit; admin and warehouse see all.
	recorder = call(http.MethodGet, "/api/v1/units/B0C00001-X0001", tokenFor(b.user, "supplier"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("supplier b own lookup: HTTP %d %s", recorder.Code, recorder.Body)
	}
	for _, role := range []string{"admin", "warehouse_staff"} {
		recorder = call(http.MethodGet, "/api/v1/units/B0C00001-X0001", tokenFor(a.user, role))
		if recorder.Code != http.StatusOK {
			t.Fatalf("role %s lookup of another supplier's unit: HTTP %d %s", role, recorder.Code, recorder.Body)
		}
	}
	// A supplier caller with no supplier profile owns nothing: 404, not 403.
	checkSPNCXJSON(t, call(http.MethodGet, "/api/v1/units/A0C00001-X0001",
		tokenFor(uuid.NewString(), "supplier")), 404, `{"error":"unit_not_found"}`)

	// Wrong method → 405.
	checkSPNCXJSON(t, call(http.MethodPost, "/api/v1/units/A0C00001-X0001", tokenFor(a.user, "supplier")),
		405, `{"error":"Method not allowed"}`)

	// List: supplier sees only their own units; variant filter + cursor page.
	recorder = call(http.MethodGet, "/api/v1/units", tokenFor(a.user, "supplier"))
	var list repository.ListUnitsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || recorder.Code != http.StatusOK ||
		len(list.Units) != 2 || list.NextCursor != nil {
		t.Fatalf("supplier list: HTTP %d %s (%v)", recorder.Code, recorder.Body, err)
	}
	for _, u := range list.Units {
		if u.ProductID != a.product {
			t.Fatalf("supplier list leaked another supplier's unit: %+v", u)
		}
	}
	recorder = call(http.MethodGet, "/api/v1/units?variant_id="+aVariant+"&limit=1", tokenFor(a.user, "supplier"))
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || recorder.Code != http.StatusOK ||
		len(list.Units) != 1 || list.NextCursor == nil {
		t.Fatalf("list page 1: HTTP %d %s (%v)", recorder.Code, recorder.Body, err)
	}
	recorder = call(http.MethodGet, "/api/v1/units?variant_id="+aVariant+"&limit=1&cursor="+*list.NextCursor,
		tokenFor(a.user, "supplier"))
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || recorder.Code != http.StatusOK ||
		len(list.Units) != 1 || list.NextCursor != nil {
		t.Fatalf("list page 2: HTTP %d %s (%v)", recorder.Code, recorder.Body, err)
	}

	// Invalid limit / cursor → 422 with the verbatim code in body.error.
	checkSPNCXJSON(t, call(http.MethodGet, "/api/v1/units?limit=abc", tokenFor(a.user, "supplier")),
		422, `{"error":"invalid_limit"}`)
	checkSPNCXJSON(t, call(http.MethodGet, "/api/v1/units?cursor=!!!", tokenFor(a.user, "supplier")),
		422, `{"error":"invalid_cursor"}`)

	if _, err := db.Exec(`UPDATE suppliers SET approval_status='suspended' WHERE id=$1`, a.supplier); err != nil {
		t.Fatal(err)
	}
	checkSPNCXJSON(t, call(http.MethodGet, "/api/v1/units/A0C00001-X0001", tokenFor(a.user, "supplier")),
		404, `{"error":"unit_not_found"}`)
	recorder = call(http.MethodGet, "/api/v1/units", tokenFor(a.user, "supplier"))
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil || recorder.Code != http.StatusOK || len(list.Units) != 0 {
		t.Fatalf("suspended supplier list: HTTP %d %s (%v)", recorder.Code, recorder.Body, err)
	}
}
