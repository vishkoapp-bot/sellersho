package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestAdminOrderTrackingPage(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	jwt := auth.NewJWTService(strings.Repeat("s", 32))
	repo := repository.NewOrderRepository(db)
	handler := RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminOrderTracking(w, r, repo)
	})
	tokenFor := func(role string) string {
		t.Helper()
		token, err := jwt.GenerateToken(uuid.NewString(), "09123456789", "Admin order test", role, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	request := func(method, token, query string, status int) repository.ListPage[repository.AdminTrackedOrder] {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/admin/orders"+query, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != status {
			t.Fatalf("got HTTP %d, want %d: %s", w.Code, status, w.Body.String())
		}
		var page repository.ListPage[repository.AdminTrackedOrder]
		if status == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Items == nil {
				t.Fatalf("invalid page: %s (%v)", w.Body.String(), err)
			}
			for _, sensitive := range []string{"fixture-private-name", "09000000000", "fixture-private-address", "0000000000"} {
				if strings.Contains(w.Body.String(), sensitive) {
					t.Fatal("response contains customer contact details")
				}
			}
		}
		return page
	}
	request(http.MethodGet, "", "", http.StatusUnauthorized)
	for _, role := range []string{"customer", "seller", "supplier", "finance_specialist", "support_specialist", "logistics_specialist"} {
		request(http.MethodGet, tokenFor(role), "", http.StatusForbidden)
	}
	admin := tokenFor("admin")
	super := tokenFor("super_admin")
	request(http.MethodPost, admin, "", http.StatusMethodNotAllowed)
	for _, query := range []string{"?cursor=bad", "?limit=-1", "?status=invalid"} {
		request(http.MethodGet, admin, query, http.StatusBadRequest)
	}

	orderTime := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	seed := func(status, method string, at time.Time) string {
		t.Helper()
		id := "AO-" + strings.ToUpper(uuid.NewString()[:8])
		_, err := db.Exec(`INSERT INTO orders
			(id, order_number, store_id, store_name, customer_full_name, customer_phone, customer_province, customer_city, customer_address,
			customer_postal_code, tracking_code, payable_amount, total_amount, payment_status, status, fulfillment_method, created_at)
			VALUES ($1,$1,(SELECT id FROM stores ORDER BY id LIMIT 1),'Fixture Store','fixture-private-name','09000000000','Tehran','Tehran',
			'fixture-private-address','0000000000',$5,100000,100000,'paid',$2,$3,$4)`, id, status, method, at, "TRK-"+id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	newest := seed("packed", "mixed", orderTime)
	second := seed("processing", "warehouse", orderTime.Add(-time.Minute))
	seed("shipped", "warehouse", orderTime.Add(time.Minute))
	if _, err := db.Exec(`UPDATE orders SET tracking_number='۸۱۲۳۴۵' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO supplier_shipments (id, order_id, supplier_id, shipping_address, customer_phone, customer_name, carrier, tracking_number, status, shipped_at)
		VALUES ($1,$2,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'{}','09000000000','fixture-private-name','Post','712345','shipped',NOW())`, "shipment-"+newest, newest)
	if err != nil {
		t.Fatal(err)
	}

	first := request(http.MethodGet, admin, "?status=preparing&limit=1", http.StatusOK)
	if len(first.Items) != 1 || first.Items[0].ID != newest || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	if shipments := first.Items[0].SupplierShipments; len(shipments) != 1 || shipments[0].TrackingNumber != "712345" || shipments[0].Carrier != "Post" {
		t.Fatalf("expected shipment tracking details: %+v", shipments)
	}
	page2 := request(http.MethodGet, super, "?status=preparing&limit=1&cursor="+url.QueryEscape(first.NextCursor), http.StatusOK)
	if len(page2.Items) != 1 || page2.Items[0].ID != second {
		t.Fatalf("unexpected second page: %+v", page2)
	}
	for _, search := range []string{strings.ToLower(newest), "TRK-" + newest, "۷۱۲۳۴۵"} {
		result := request(http.MethodGet, admin, "?search="+url.QueryEscape(search), http.StatusOK)
		if len(result.Items) != 1 || result.Items[0].ID != newest || result.HasMore {
			t.Fatalf("unexpected search for %q: %+v", search, result)
		}
	}
	if got := request(http.MethodGet, admin, "?search="+url.QueryEscape("۸۱۲۳۴۵"), http.StatusOK); len(got.Items) != 1 || got.Items[0].ID != second {
		t.Fatalf("legacy Persian tracking number was not found: %+v", got)
	}
	if got := request(http.MethodGet, super, "?search=712345&status=shipped", http.StatusOK); len(got.Items) != 0 {
		t.Fatalf("search ignored status filter: %+v", got)
	}
}
