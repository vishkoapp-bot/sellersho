package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

func TestSupplierVariantWorkspaceReplaysRejectedPriceRequest(t *testing.T) {
	v := setupVariantGovFixture(t)
	v.resetVariants(t)
	v.setGovernance(t, 10, true)
	const payload = `{"supplierBaseCost":1200,"reason":"updated supplier tariff","expected":{"cost":null,"productCost":1000}}`
	request := func(key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+v.productID+"/variants/"+v.varAID+"/pricing", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", key)
		ctx := context.WithValue(req.Context(), supplierIDKey, v.supplierID)
		ctx = context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: "workspace-price-supplier"})
		w := httptest.NewRecorder()
		v.h.HandleUpdateVariantPricing(w, req.WithContext(ctx), v.productID, v.varAID)
		return w
	}
	const key = "workspace-price-test-1"
	w := request(key, payload)
	if w.Code != http.StatusAccepted {
		t.Fatalf("queued request HTTP=%d body=%s", w.Code, w.Body)
	}
	id := decodePricingPut(t, w).RequestID
	if retry := request(key, payload); retry.Code != http.StatusAccepted || decodePricingPut(t, retry).RequestID != id {
		t.Fatalf("pending replay HTTP=%d body=%s", retry.Code, retry.Body)
	}
	if _, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, id, false, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if retry := request(key, payload); retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), "variant_workspace_price_rejected") {
		t.Fatalf("rejected replay HTTP=%d body=%s", retry.Code, retry.Body)
	}
	if n := v.variantRowCount(t, v.varAID); n != 1 {
		t.Fatalf("rejected replay made %d rows, want 1", n)
	}
	if retry := request(key, strings.Replace(payload, "1200", "1250", 1)); retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), "variant_workspace_conflict") {
		t.Fatalf("changed replay HTTP=%d body=%s", retry.Code, retry.Body)
	}
	const freshKey = "workspace-price-test-2"
	fresh := request(freshKey, payload)
	if fresh.Code != http.StatusAccepted {
		t.Fatalf("fresh request HTTP=%d body=%s", fresh.Code, fresh.Body)
	}
	freshID := decodePricingPut(t, fresh).RequestID
	if freshID == id {
		t.Fatal("a new operation must get a new request after rejection")
	}
	if _, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, freshID, true, "admin-1"); err != nil {
		t.Fatal(err)
	}
	if retry := request(freshKey, payload); retry.Code != http.StatusOK || decodePricingPut(t, retry).RequestID != freshID {
		t.Fatalf("approved replay HTTP=%d body=%s", retry.Code, retry.Body)
	}
	if n := v.variantRowCount(t, v.varAID); n != 2 {
		t.Fatalf("approved replay made %d rows, want 2", n)
	}
}

func TestSupplierVariantPriceClassifiesAgainstLockedCost(t *testing.T) {
	v := setupVariantGovFixture(t)
	v.resetVariants(t)
	v.setGovernance(t, 10, true)
	ctx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	defer cancel()
	holder, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	var pid int
	if err := holder.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, `UPDATE product_variants SET supplier_base_cost=1000 WHERE id=$1`, v.varBID); err != nil {
		t.Fatal(err)
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() { response <- v.putVariantCost(v.varBID, 1500) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := v.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("variant edit never waited for the existing writer")
		}
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-response:
		if w.Code != http.StatusAccepted {
			t.Fatalf("edit became a governed increase after the concurrent decrease: HTTP=%d body=%s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("variant edit did not finish after releasing the writer")
	}
	if got := v.variantCost(t, v.varBID); !got.Valid || got.Int64 != 1000 {
		t.Fatalf("unapproved change replaced the concurrent write: %v", got)
	}
}

func TestSupplierVariantClearApprovalRequiresReviewedParentCost(t *testing.T) {
	v := setupVariantGovFixture(t)
	v.resetVariants(t)
	v.setGovernance(t, 10, true)
	if _, err := v.db.Exec(`UPDATE product_variants SET supplier_base_cost=750 WHERE id=$1`, v.varAID); err != nil {
		t.Fatal(err)
	}
	queued := v.putVariant(v.varAID, `{"clear":true}`)
	if queued.Code != http.StatusAccepted {
		t.Fatalf("queued clear HTTP=%d body=%s", queued.Code, queued.Body)
	}
	id := decodePricingPut(t, queued).RequestID
	if _, err := v.db.Exec(`UPDATE products SET supplier_base_cost=1300 WHERE id=$1`, v.productID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, id, true, "admin-1"); !errors.Is(err, repository.ErrPriceIncreaseStaleCost) {
		t.Fatalf("approve with changed inheritance target error=%v", err)
	}
	if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 750 {
		t.Fatalf("drifted clear changed the SKU cost: %v", got)
	}
	if row := v.variantRow(t, v.varAID); row.Status != "pending" {
		t.Fatalf("drifted clear changed the review status: %s", row.Status)
	}
}
