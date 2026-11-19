package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQUAL01WarehouseAssignmentAndSelf(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	id, site := uuid.NewString(), uuid.NewString()
	city := "City-" + site[:8]
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1,'09000049201','City fixture','warehouse_staff')`, id)
	t.Cleanup(func() { exec(`DELETE FROM users WHERE id=$1`, id) })
	t.Cleanup(func() { exec(`DELETE FROM audit_logs WHERE operator=$1 AND action='set_staff_warehouse_city'`, id) })
	exec(`INSERT INTO user_capabilities(user_id,capability,status) VALUES($1,'warehouse_staff','active') ON CONFLICT(user_id,capability) DO NOTHING`, id)
	exec(`INSERT INTO warehouses(id,name,city_name,province_name,tapin_city_id,tapin_province_id) VALUES($1,$2,$3,'Fixture',1,1)`, site, site, city)
	t.Cleanup(func() { exec(`DELETE FROM warehouses WHERE id=$1`, site) })
	jwt := auth.NewJWTService("warehouse-city-generated-fixture-key")
	reader := &middlewareCapabilityReader{}
	jwt.SetCapabilityReader(reader)
	sign := func(role string) string {
		t.Helper()
		s, e := jwt.GenerateTokenWithCapabilities(id, "", "", role, []string{role}, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	staffToken, adminToken, superToken := sign("warehouse_staff"), sign("admin"), sign("super_admin")
	assign := RequireCapabilities(jwt, "super_admin")(func(w http.ResponseWriter, r *http.Request) { handleWarehouseStaffCity(w, r, db) })
	self := requireWarehouseCityRoles(jwt, "warehouse_staff", "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01WarehouseMe(w, r, db) })
	invoke := func(handler http.HandlerFunc, method, path, token, body, cap string, want int) *httptest.ResponseRecorder {
		t.Helper()
		reader.capabilities = []string{cap}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body)
		}
		return w
	}
	endpoint := "/api/v1/admin/staff/" + id + "/warehouse-city"
	assertCity := func(want string) {
		t.Helper()
		w := invoke(self, http.MethodGet, "/api/v1/warehouse/me?warehouseCity=caller-selector", staffToken, "", "warehouse_staff", 200)
		var got struct{ WarehouseCity string }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.WarehouseCity != want {
			t.Fatalf("warehouse city=%q want %q: %v", got.WarehouseCity, want, err)
		}
	}
	assertCity("")
	body := `{"city":"` + city + `"}`
	invoke(assign, http.MethodPut, endpoint, adminToken, body, "admin", 403)
	invoke(assign, http.MethodPut, endpoint, superToken, body+`{}`, "super_admin", 400)
	invoke(assign, http.MethodPut, endpoint, superToken, `{"city":"not-catalogued"}`, "super_admin", 404)
	invoke(assign, http.MethodPut, endpoint, superToken, body, "super_admin", 204)
	assertCity(city)
	invoke(assign, http.MethodPut, endpoint, superToken, `{"city":""}`, "super_admin", 204)
	assertCity("")
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE operator=$1
		AND action='set_staff_warehouse_city' AND details::jsonb->>'userId'=$1`, id).Scan(&events); err != nil || events != 2 {
		t.Fatalf("assignment and revocation audit events = %d, err=%v", events, err)
	}
}

func TestQUAL01WarehouseRoleBoundary(t *testing.T) {
	const actor = "warehouse-boundary-fixture"
	jwt := auth.NewJWTService("warehouse-role-generated-fixture-key")
	reader := &middlewareCapabilityReader{}
	jwt.SetCapabilityReader(reader)
	token, err := jwt.GenerateTokenWithCapabilities(actor, "", "", "warehouse_staff", []string{"warehouse_staff"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		roles  []string
		global bool
		want   int
		staff  string
	}{
		{"staff", []string{"warehouse_staff"}, false, 204, actor},
		{"staff and logistics", []string{"warehouse_staff", "logistics_specialist"}, false, 204, actor},
		{"staff and admin", []string{"warehouse_staff", "admin"}, true, 403, ""},
		{"logistics only", []string{"logistics_specialist"}, true, 204, ""},
		{"supplier only", []string{"supplier"}, false, 403, ""},
		{"revoked", nil, false, 403, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader.capabilities = tc.roles
			gate := requireWarehouseCityRoles(jwt, "warehouse_staff", "logistics_specialist", "admin")
			if tc.global {
				gate = requireGlobalWarehouseRoles(jwt, "logistics_specialist", "admin")
			}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/warehouse/inbounds?staffId=other", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			gate(func(w http.ResponseWriter, r *http.Request) {
				if actual := repository.WarehouseStaffID(r.Context()); actual != tc.staff {
					t.Errorf("staff principal = %q, want %q", actual, tc.staff)
				}
				w.WriteHeader(http.StatusNoContent)
			})(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}
