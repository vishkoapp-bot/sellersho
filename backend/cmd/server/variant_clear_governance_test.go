package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Critic fix F1 follow-up (migration 202611050010): a variant
// clear-to-inherit is NOT always a lowering operation. When the
// variant's own cost sits BELOW the product's cost, clearing raises
// the variant's effective cost to the product's — that raise goes
// through the same governance matrix as a set-increase, and approval
// applies AS A CLEAR (variant cost back to NULL). Clears that lower
// or match the effective cost (own cost at/above the product's, or
// nothing to clear) stay on the direct path with no row. Fixture:
// product cost 1000, varA inheriting (NULL), varB own cost set per
// subtest via setVariantOwnCost.

func (v *variantGovFixture) setVariantOwnCost(t *testing.T, variantID string, cost int64) {
	t.Helper()
	if _, err := v.db.Exec(`UPDATE product_variants SET supplier_base_cost = $1 WHERE id = $2`, cost, variantID); err != nil {
		t.Fatal(err)
	}
}

func (v *variantGovFixture) setProductCost(t *testing.T, cost int64) {
	t.Helper()
	if _, err := v.db.Exec(`UPDATE products SET supplier_base_cost = $1 WHERE id = $2`, cost, v.productID); err != nil {
		t.Fatal(err)
	}
}

func TestVariantClearGovernance(t *testing.T) {
	v := setupVariantGovFixture(t)

	t.Run("locked clear raising effective cost queues with clear intent; approve applies as clear", func(t *testing.T) {
		v.resetVariants(t)
		v.setVariantOwnCost(t, v.varBID, 600) // below the product's 1000
		v.setGovernance(t, 10, true)
		w := v.putVariant(v.varBID, `{"clear":true}`)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Status != "pending_review" || resp.Reason != "locked" || resp.RequestID == "" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := v.variantCost(t, v.varBID); !got.Valid || got.Int64 != 600 {
			t.Fatalf("variant cost changed while pending: %v", got)
		}
		row := v.variantRow(t, v.varBID)
		if row.Status != "pending" || row.ID != resp.RequestID || !row.ClearOnApprove ||
			row.OldReference != 600 || row.NewReference != 1000 {
			t.Fatalf("unexpected row (want pending clear-intent 600 -> 1000): %+v", row)
		}

		reviewed, err := v.pricingRepo.ReviewPriceIncrease(v.ctx, resp.RequestID, true, "admin-1")
		if err != nil {
			t.Fatal(err)
		}
		if reviewed.Status != "approved" || !reviewed.ClearOnApprove {
			t.Fatalf("reviewed row = %+v", reviewed)
		}
		if got := v.variantCost(t, v.varBID); got.Valid {
			t.Fatalf("variant cost after approved clear = %d, want NULL (inheriting)", got.Int64)
		}
		if got := v.cost(t); got != 1000 {
			t.Fatalf("product cost moved by a variant clear approval: %d", got)
		}
	})

	t.Run("duplicate governed clear while pending is 409", func(t *testing.T) {
		v.resetVariants(t)
		v.setVariantOwnCost(t, v.varBID, 600)
		v.setGovernance(t, 10, true)
		if w := v.putVariant(v.varBID, `{"clear":true}`); w.Code != http.StatusAccepted {
			t.Fatalf("first clear: got %d: %s", w.Code, w.Body.String())
		}
		if w := v.putVariant(v.varBID, `{"clear":true}`); w.Code != http.StatusConflict {
			t.Fatalf("duplicate clear: got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varBID); !got.Valid || got.Int64 != 600 {
			t.Fatalf("variant cost changed behind a pending clear: %v", got)
		}
	})

	t.Run("unlocked clear within cap applies immediately as NULL", func(t *testing.T) {
		v.resetVariants(t)
		v.setProductCost(t, 1050)
		v.setVariantOwnCost(t, v.varBID, 1000) // clear = +5% to the product cost
		v.setGovernance(t, 10, false)
		w := v.putVariant(v.varBID, `{"clear":true}`)
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
		if got := v.variantCost(t, v.varBID); got.Valid {
			t.Fatalf("variant cost after auto-approved clear = %d, want NULL (inheriting)", got.Int64)
		}
		row := v.variantRow(t, v.varBID)
		if row.Status != "approved" || !row.AutoApproved || !row.ClearOnApprove ||
			row.OldReference != 1000 || row.NewReference != 1050 {
			t.Fatalf("unexpected row (want approved auto clear-intent 1000 -> 1050): %+v", row)
		}
	})

	t.Run("clear lowering effective cost stays direct even while locked", func(t *testing.T) {
		v.resetVariants(t) // varB own cost 2000, above the product's 1000
		v.setGovernance(t, 10, true)
		w := v.putVariant(v.varBID, `{"clear":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varBID); got.Valid {
			t.Fatalf("variant B cost = %d, want NULL (inheriting)", got.Int64)
		}
		if n := v.variantRowCount(t, v.varBID); n != 0 {
			t.Fatalf("lowering clear created %d increase rows", n)
		}
	})

	t.Run("clear with no own cost stays direct even while locked", func(t *testing.T) {
		v.resetVariants(t) // varA already inherits (NULL)
		v.setGovernance(t, 10, true)
		w := v.putVariant(v.varAID, `{"clear":true}`)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if got := v.variantCost(t, v.varAID); got.Valid {
			t.Fatalf("variant A cost = %d, want NULL (inheriting)", got.Int64)
		}
		if n := v.variantRowCount(t, v.varAID); n != 0 {
			t.Fatalf("no-op clear created %d increase rows", n)
		}
	})
}
