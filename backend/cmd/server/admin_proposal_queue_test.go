package main

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/repository"
	"net/http/httptest"
	"testing"
)

func TestAdminProposalQueueReadsSupplierSubmissions(t *testing.T) {
	db := supplierFlowDB(t)
	fixture := supplierFlowSeed(t, db)
	repo := repository.NewSupplierRepository(db)
	id, err := repo.ProposeProduct(context.Background(), fixture.supplier, map[string]interface{}{"title": "Admin queue regression", "supplierBaseCost": float64(1234)})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM pending_approvals WHERE id=$1`, id)
	h := NewSupplierHandlers(repo, nil, false)
	w := httptest.NewRecorder()
	h.HandleGetProposals(w, httptest.NewRequest("GET", "/api/v1/admin/products/proposals", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var rows []map[string]interface{}
	if err = json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["id"] == id {
			return
		}
	}
	t.Fatal("supplier submission missing from admin queue")
}
