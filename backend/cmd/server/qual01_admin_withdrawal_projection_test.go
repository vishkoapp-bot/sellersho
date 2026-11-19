package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestQual01AdminWithdrawalProjection(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES($1,'09000049202','Withdrawal fixture')`, id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM users WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
	})
	if _, err := db.Exec(`INSERT INTO customer_withdrawal_requests
		(id,customer_id,customer_name,customer_mobile,account_holder_name,sheba,bank_name,amount,request_key_sha256,created_at)
		VALUES ($2,$1,'Fixture','09000049202','Fixture','IR000000000000000000000000','Fixture',120,decode(repeat('01',32),'hex'),'2099-01-01T00:00:00Z'),
		       ($3,$1,'Fixture','09000049202','Fixture','IR000000000000000000000000','Fixture',150,decode(repeat('02',32),'hex'),'2099-01-01T00:00:00Z')`,
		id, "a-"+id, "b-"+id); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/admin/customer-withdrawals?limit=1"
	w := httptest.NewRecorder()
	handleT01AdminCustomerWithdrawals(w, httptest.NewRequest(http.MethodGet, path, nil), db)
	var response struct{ Withdrawals []map[string]any }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Withdrawals) != 1 {
		t.Fatalf("status=%d response=%s", w.Code, w.Body)
	}
	row := response.Withdrawals[0]
	var got []string
	for key := range row {
		got = append(got, key)
	}
	slices.Sort(got)
	want := []string{"account_holder_name", "amount", "bank_name", "created_at", "customer_id", "customer_mobile", "customer_name", "id", "rejection_reason", "reviewed_at", "reviewed_by", "sheba", "status", "transfer_confirmed_at", "transfer_confirmed_by", "transfer_date", "transfer_reference", "updated_at"}
	if !slices.Equal(got, want) || row["id"] != "b-"+id || row["amount"] != float64(150) || row["reviewed_at"] != nil {
		t.Fatalf("unexpected projection: %v", row)
	}
	w = httptest.NewRecorder()
	handleT01AdminCustomerWithdrawals(w, httptest.NewRequest(http.MethodPost, path, nil), db)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST returned %d", w.Code)
	}
}
