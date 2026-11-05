package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Critic fix F1: variant supplier_base_cost changes go through the SAME
// gap-11 governance matrix as product cost changes, scoped to
// (product, variant), with the variant's EFFECTIVE cost (own cost,
// else the inherited product cost) as the matrix's old cost. These
// tests run against the shared disposable Postgres via the product
// governance fixture (setupPriceGovFixture) plus two seeded variants:
// varA inherits the product cost (own cost NULL), varB carries its
// own cost of 2000 over the product's 1000.

type variantGovFixture struct {
	*priceGovFixture
	varAID string
	varBID string
}

func setupVariantGovFixture(t *testing.T) *variantGovFixture {
	t.Helper()
	f := setupPriceGovFixture(t)
	suffix := f.productID[len("gov-product-"):]
	v := &variantGovFixture{priceGovFixture: f, varAID: uuid.NewString(), varBID: uuid.NewString()}
	if _, err := f.db.Exec(`INSERT INTO product_variants (id, product_id, spnc, variant_name, attributes, is_default, stock, supplier_base_cost) VALUES ($1, $2, $3, 'رنگ قرمز', '{}'::jsonb, true, 5, NULL)`, v.varAID, f.productID, "GOVSPNCA"+suffix[:4]); err != nil {
		t.Fatalf("seed variant A: %v", err)
	}
	if _, err := f.db.Exec(`INSERT INTO product_variants (id, product_id, spnc, variant_name, attributes, is_default, stock, supplier_base_cost) VALUES ($1, $2, $3, 'رنگ آبی', '{}'::jsonb, false, 5, 2000)`, v.varBID, f.productID, "GOVSPNCB"+suffix[:4]); err != nil {
		t.Fatalf("seed variant B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.db.Exec(`DELETE FROM product_variants WHERE product_id = $1`, f.productID)
	})
	return v
}

// resetVariants restores product + variant state: product cost 1000,
// varA inheriting (NULL), varB at 2000, no grants, no increase rows.
func (v *variantGovFixture) resetVariants(t *testing.T) {
	t.Helper()
	v.reset(t)
	if _, err := v.db.Exec(`UPDATE product_variants SET supplier_base_cost = NULL WHERE id = $1`, v.varAID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.Exec(`UPDATE product_variants SET supplier_base_cost = 2000 WHERE id = $1`, v.varBID); err != nil {
		t.Fatal(err)
	}
}

func (v *variantGovFixture) putVariant(variantID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+v.productID+"/variants/"+variantID+"/pricing", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, v.supplierID))
	w := httptest.NewRecorder()
	v.h.HandleUpdateVariantPricing(w, req, v.productID, variantID)
	return w
}

func (v *variantGovFixture) putVariantCost(variantID string, cost int64) *httptest.ResponseRecorder {
	return v.putVariant(variantID, fmt.Sprintf(`{"supplierBaseCost":%d}`, cost))
}

func (v *variantGovFixture) variantCost(t *testing.T, variantID string) sql.NullInt64 {
	t.Helper()
	var cost sql.NullInt64
	if err := v.db.QueryRow(`SELECT supplier_base_cost FROM product_variants WHERE id = $1`, variantID).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	return cost
}

func (v *variantGovFixture) variantRow(t *testing.T, variantID string) repository.SupplierPriceIncrease {
	t.Helper()
	rows, err := v.repo.ListSupplierPriceIncreases(v.ctx, v.supplierID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ProductID == v.productID && row.VariantID != nil && *row.VariantID == variantID {
			return row
		}
	}
	t.Fatalf("no price increase row for variant %s", variantID)
	return repository.SupplierPriceIncrease{}
}

func (v *variantGovFixture) variantRowCount(t *testing.T, variantID string) int {
	t.Helper()
	var n int
	if err := v.db.QueryRow(`SELECT COUNT(*) FROM supplier_price_increases WHERE product_id = $1 AND variant_id = $2`, v.productID, variantID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestVariantPriceIncreaseGovernanceMatrix(t *testing.T) {
	v := setupVariantGovFixture(t)

	t.Run("locked increase queues pending, variant cost unchanged", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		w := v.putVariantCost(v.varAID, 1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Status != "pending_review" || resp.Reason != "locked" || resp.RequestID == "" || resp.Message == "" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := v.variantCost(t, v.varAID); got.Valid {
			t.Fatalf("variant cost changed while pending: %d", got.Int64)
		}
		row := v.variantRow(t, v.varAID)
		if row.Status != "pending" || row.ID != resp.RequestID || row.OldReference != 1000 || row.NewReference != 1050 {
			t.Fatalf("unexpected row (old must be the inherited effective cost 1000): %+v", row)
		}
		if row.VariantName != "رنگ قرمز" {
			t.Fatalf("supplier queue row variant name = %q, want رنگ قرمز", row.VariantName)
		}
	})

	t.Run("duplicate pending for same variant is 409, other scopes unaffected", func(t *testing.T) {
		// varA still has the pending row from the previous subtest.
		if w := v.putVariantCost(v.varAID, 1060); w.Code != http.StatusConflict {
			t.Fatalf("duplicate variant pending: got %d: %s", w.Code, w.Body.String())
		}
		// A pending row for varB is a different scope: it queues.
		if w := v.putVariantCost(v.varBID, 2100); w.Code != http.StatusAccepted {
			t.Fatalf("other variant while pending: got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("product-level pending does not block a variant increase", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		if w := v.putPrice(1050); w.Code != http.StatusAccepted {
			t.Fatalf("product put: got %d: %s", w.Code, w.Body.String())
		}
		if w := v.putVariantCost(v.varAID, 1050); w.Code != http.StatusAccepted {
			t.Fatalf("variant put behind product pending: got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unlocked within cap applies to the variant, approved row scoped", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, false)
		w := v.putVariantCost(v.varAID, 1050)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
		if resp["applied"] != true || resp["autoApproved"] != true || resp["requestId"] == nil || resp["requestId"] == "" {
			t.Fatalf("applied response missing governance fields: %v", resp)
		}
		if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 1050 {
			t.Fatalf("variant cost = %v, want 1050", got)
		}
		if got := v.cost(t); got != 1000 {
			t.Fatalf("product cost moved by a variant apply: %d", got)
		}
		row := v.variantRow(t, v.varAID)
		if row.Status != "approved" || !row.AutoApproved {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("unlocked above cap queues with above_cap", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, false)
		w := v.putVariantCost(v.varAID, 1200)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if resp := decodePricingPut(t, w); resp.Reason != "above_cap" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := v.variantCost(t, v.varAID); got.Valid {
			t.Fatalf("variant cost changed while pending: %d", got.Int64)
		}
	})

	t.Run("exception grant applies while locked", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		if err := v.pricingRepo.UpsertProductPriceException(v.ctx, v.productID, 50, nil, "admin-1"); err != nil {
			t.Fatal(err)
		}
		w := v.putVariantCost(v.varAID, 1400)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 1400 {
			t.Fatalf("variant cost = %v, want 1400 via grant", got)
		}
		row := v.variantRow(t, v.varAID)
		if row.Status != "approved" || !row.IsException {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("implicit agreement queues even unlocked below cap", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, false)
		if _, err := v.db.Exec(`UPDATE products SET implicit_agreement = TRUE, implicit_agreement_until = NULL WHERE id = $1`, v.productID); err != nil {
			t.Fatal(err)
		}
		w := v.putVariantCost(v.varAID, 1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if resp := decodePricingPut(t, w); resp.Reason != "agreement" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		row := v.variantRow(t, v.varAID)
		if row.Status != "pending" || !row.AgreementReview {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("decrease and equal-to-effective apply directly with no row", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true) // locked: decreases must still pass
		w := v.putVariantCost(v.varBID, 1500)
		if w.Code != http.StatusOK {
			t.Fatalf("decrease: got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varBID); !got.Valid || got.Int64 != 1500 {
			t.Fatalf("variant B cost = %v, want 1500", got)
		}
		if n := v.variantRowCount(t, v.varBID); n != 0 {
			t.Fatalf("decrease created %d increase rows", n)
		}
		// Equal to the effective (inherited) cost is not an increase.
		w = v.putVariantCost(v.varAID, 1000)
		if w.Code != http.StatusOK {
			t.Fatalf("equal-to-effective: got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 1000 {
			t.Fatalf("variant A cost = %v, want explicit 1000", got)
		}
		if n := v.variantRowCount(t, v.varAID); n != 0 {
			t.Fatalf("equal-to-effective created %d increase rows", n)
		}
	})

	t.Run("clear to inherit stays direct even while locked", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		w := v.putVariant(v.varBID, `{"clear":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("clear: got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varBID); got.Valid {
			t.Fatalf("variant B cost = %d, want NULL (inheriting)", got.Int64)
		}
		if n := v.variantRowCount(t, v.varBID); n != 0 {
			t.Fatalf("clear created %d increase rows", n)
		}
	})
}

func TestVariantPriceIncreaseReviewFlow(t *testing.T) {
	v := setupVariantGovFixture(t)

	t.Run("admin queue names the variant; approve applies to the variant only", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		w := v.putVariantCost(v.varAID, 1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)

		queue, err := v.pricingRepo.ListAdminPriceIncreases(v.ctx, "pending")
		if err != nil {
			t.Fatal(err)
		}
		var found *repository.AdminPriceIncrease
		for i := range queue {
			if queue[i].ID == resp.RequestID {
				found = &queue[i]
			}
		}
		if found == nil {
			t.Fatalf("pending variant request %s not in admin queue", resp.RequestID)
		}
		if found.VariantID == nil || *found.VariantID != v.varAID || found.VariantName != "رنگ قرمز" {
			t.Fatalf("admin queue row variant = %v / %q, want %s / رنگ قرمز", found.VariantID, found.VariantName, v.varAID)
		}

		reviewed, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, resp.RequestID, true, "admin-1")
		if err != nil {
			t.Fatal(err)
		}
		if reviewed.Status != "approved" {
			t.Fatalf("reviewed row = %+v", reviewed)
		}
		if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 1050 {
			t.Fatalf("variant cost after approve = %v, want 1050", got)
		}
		if got := v.cost(t); got != 1000 {
			t.Fatalf("product cost moved by a variant approval: %d", got)
		}
	})

	t.Run("stale effective cost blocks approval, row stays pending", func(t *testing.T) {
		v.resetVariants(t)
		v.setGovernance(t, 10, true)
		w := v.putVariantCost(v.varAID, 1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		// The variant's effective cost moves after filing (own cost set).
		if _, err := v.db.Exec(`UPDATE product_variants SET supplier_base_cost = 1100 WHERE id = $1`, v.varAID); err != nil {
			t.Fatal(err)
		}
		if _, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, resp.RequestID, true, "admin-1"); !errors.Is(err, repository.ErrPriceIncreaseStaleCost) {
			t.Fatalf("stale approve err = %v, want ErrPriceIncreaseStaleCost", err)
		}
		row := v.variantRow(t, v.varAID)
		if row.Status != "pending" {
			t.Fatalf("row after stale approve = %+v, want still pending", row)
		}
		if got := v.variantCost(t, v.varAID); !got.Valid || got.Int64 != 1100 {
			t.Fatalf("variant cost = %v, want untouched 1100", got)
		}
	})
}
