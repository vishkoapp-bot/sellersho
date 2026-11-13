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
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Gap 11 DB-gated governance tests: the full decision matrix through the
// real PUT handler, the admin review flow with its stale-cost guard, the
// exception-ticket endpoint and the grant lifecycle. They run against the
// shared disposable Postgres (setupTestPostgresDB) and restore the
// platform_settings governance columns on cleanup.

type priceGovFixture struct {
	db          *sql.DB
	repo        *repository.SupplierRepository
	pricingRepo *repository.PricingRepository
	h           *SupplierHandlers
	supplierID  string
	supplierNm  string
	ownerID     string
	productID   string
	ctx         context.Context
}

func setupPriceGovFixture(t *testing.T) *priceGovFixture {
	t.Helper()
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	pricingRepo := repository.NewPricingRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Gov Test "+suffix, "Owner", "gov-"+suffix, "TestGovPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	productID := "gov-product-" + suffix
	var origCap float64
	var origLocked bool
	var origEarlyWithdrawal sql.NullFloat64
	if err := db.QueryRow(`SELECT increase_cap_pct, increase_locked, early_withdrawal_fee_pct FROM platform_settings WHERE id = 1`).Scan(&origCap, &origLocked, &origEarlyWithdrawal); err != nil {
		t.Fatal(err)
	}
	// GetPlatformPricingSettings cannot scan a NULL early_withdrawal_fee_pct
	// (pre-existing loader behaviour, unrelated to gap 11); prod's row
	// carries a value, so mirror prod for the duration of the test and
	// restore the original (possibly NULL) value on cleanup.
	if !origEarlyWithdrawal.Valid {
		if _, err := db.Exec(`UPDATE platform_settings SET early_withdrawal_fee_pct = 0 WHERE id = 1`); err != nil {
			t.Fatal(err)
		}
	}
	f := &priceGovFixture{
		db: db, repo: repo, pricingRepo: pricingRepo,
		supplierID: supplier.ID, supplierNm: supplier.Name, ownerID: owner.ID,
		productID: productID, ctx: ctx,
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`UPDATE platform_settings SET increase_cap_pct = $1, increase_locked = $2, early_withdrawal_fee_pct = $3 WHERE id = 1`, origCap, origLocked, origEarlyWithdrawal)
		_, _ = db.Exec(`DELETE FROM supplier_price_increases WHERE product_id = $1`, productID)
		_, _ = db.Exec(`DELETE FROM product_price_exceptions WHERE product_id = $1`, productID)
		_, _ = db.Exec(`DELETE FROM support_tickets WHERE requester_user_id = $1`, owner.ID)
		_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, productID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
		db.Close()
	})
	if err := repo.CreateDirectProduct(ctx, domain.Product{ID: productID, Slug: productID, Title: "Gov product", CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 10}); err != nil {
		t.Fatal(err)
	}
	f.h = NewSupplierHandlers(repo, auth.NewJWTService("gov-test"), false).WithPricingRepository(pricingRepo)
	return f
}

func (f *priceGovFixture) setGovernance(t *testing.T, capPct float64, locked bool) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE platform_settings SET increase_cap_pct = $1, increase_locked = $2 WHERE id = 1`, capPct, locked); err != nil {
		t.Fatal(err)
	}
}

// reset restores the product to cost 1000 with no agreement flag, no
// grant and no increase rows.
func (f *priceGovFixture) reset(t *testing.T) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE products SET supplier_base_cost = 1000, implicit_agreement = FALSE, implicit_agreement_until = NULL WHERE id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM supplier_price_increases WHERE product_id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM product_price_exceptions WHERE product_id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
}

func (f *priceGovFixture) putPrice(newCost int64) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"new_supplier_base_cost":%d}`, newCost)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+f.productID+"/pricing", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, f.supplierID))
	w := httptest.NewRecorder()
	f.h.HandleUpdatePricing(w, req)
	return w
}

func (f *priceGovFixture) cost(t *testing.T) int64 {
	t.Helper()
	var cost int64
	if err := f.db.QueryRow(`SELECT supplier_base_cost FROM products WHERE id = $1`, f.productID).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	return cost
}

func (f *priceGovFixture) latestRow(t *testing.T) repository.SupplierPriceIncrease {
	t.Helper()
	rows, err := f.repo.ListSupplierPriceIncreases(f.ctx, f.supplierID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ProductID == f.productID {
			return row
		}
	}
	t.Fatalf("no price increase row for product %s", f.productID)
	return repository.SupplierPriceIncrease{}
}

func (f *priceGovFixture) rowCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM supplier_price_increases WHERE product_id = $1`, f.productID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type pricingPutResponse struct {
	Success   bool    `json:"success"`
	Applied   bool    `json:"applied"`
	AutoAppr  bool    `json:"autoApproved"`
	Status    string  `json:"status"`
	RequestID string  `json:"requestId"`
	Reason    string  `json:"reason"`
	Pct       float64 `json:"pct"`
	CapPct    float64 `json:"capPct"`
	Message   string  `json:"message"`
	Err       string  `json:"error"`
}

func decodePricingPut(t *testing.T, w *httptest.ResponseRecorder) pricingPutResponse {
	t.Helper()
	var resp pricingPutResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return resp
}

func TestPriceIncreaseGovernanceMatrix(t *testing.T) {
	f := setupPriceGovFixture(t)

	t.Run("locked below cap queues pending, cost unchanged", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, true)
		w := f.putPrice(1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Status != "pending_review" || resp.Reason != "locked" || resp.RequestID == "" || resp.Message == "" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := f.cost(t); got != 1000 {
			t.Fatalf("cost changed while pending: %d", got)
		}
		row := f.latestRow(t)
		if row.Status != "pending" || row.ID != resp.RequestID || row.OldReference != 1000 || row.NewReference != 1050 {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("duplicate pending is 409", func(t *testing.T) {
		w := f.putPrice(1060)
		if w.Code != http.StatusConflict {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if got := f.cost(t); got != 1000 {
			t.Fatalf("cost changed on duplicate: %d", got)
		}
	})

	t.Run("review approve applies and stamps reviewed_at; second review conflicts", func(t *testing.T) {
		row := f.latestRow(t)
		reviewed, err := f.pricingRepo.ReviewPriceIncrease(f.ctx, row.ID, true, "admin-1")
		if err != nil {
			t.Fatal(err)
		}
		if reviewed.Status != "approved" || reviewed.ReviewedAt == nil {
			t.Fatalf("unexpected reviewed row: %+v", reviewed)
		}
		if got := f.cost(t); got != 1050 {
			t.Fatalf("approve did not apply: %d", got)
		}
		if _, err := f.pricingRepo.ReviewPriceIncrease(f.ctx, row.ID, true, "admin-1"); !errors.Is(err, repository.ErrPriceIncreaseNotPending) {
			t.Fatalf("second review err = %v", err)
		}
	})

	t.Run("unlocked below cap auto-applies with approved auto row", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, false)
		w := f.putPrice(1050)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if !resp.Applied || !resp.AutoAppr || resp.RequestID == "" {
			t.Fatalf("unexpected applied body: %+v", resp)
		}
		if got := f.cost(t); got != 1050 {
			t.Fatalf("auto-apply did not land: %d", got)
		}
		row := f.latestRow(t)
		if row.Status != "approved" || !row.AutoApproved || row.IsException || row.ReviewedAt == nil {
			t.Fatalf("unexpected auto row: %+v", row)
		}
	})

	t.Run("unlocked above cap queues", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, false)
		w := f.putPrice(1500)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Reason != "above_cap" || resp.CapPct != 10 {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := f.cost(t); got != 1000 {
			t.Fatalf("cost changed while pending: %d", got)
		}
	})

	t.Run("grant auto-applies while locked under grant cap", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, true)
		if err := f.pricingRepo.UpsertProductPriceException(f.ctx, f.productID, 30, nil, "admin-1"); err != nil {
			t.Fatal(err)
		}
		w := f.putPrice(1200)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if !resp.Applied || !resp.AutoAppr {
			t.Fatalf("unexpected grant applied body: %+v", resp)
		}
		if got := f.cost(t); got != 1200 {
			t.Fatalf("grant apply did not land: %d", got)
		}
		row := f.latestRow(t)
		if !row.IsException || row.ExceptionCapPct == nil || *row.ExceptionCapPct != 30 || row.Status != "approved" {
			t.Fatalf("unexpected grant row: %+v", row)
		}
	})

	t.Run("grant cap exceeded queues as exception_cap", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, true)
		if err := f.pricingRepo.UpsertProductPriceException(f.ctx, f.productID, 30, nil, "admin-1"); err != nil {
			t.Fatal(err)
		}
		w := f.putPrice(1500)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Reason != "exception_cap" || resp.CapPct != 30 {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		if got := f.cost(t); got != 1000 {
			t.Fatalf("cost changed while pending: %d", got)
		}
		row := f.latestRow(t)
		if row.Status != "pending" || !row.IsException {
			t.Fatalf("unexpected row: %+v", row)
		}
	})

	t.Run("implicit agreement queues for review even unlocked below cap", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, false)
		if _, err := f.db.Exec(`UPDATE products SET implicit_agreement = TRUE, implicit_agreement_until = NULL WHERE id = $1`, f.productID); err != nil {
			t.Fatal(err)
		}
		w := f.putPrice(1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if resp.Reason != "agreement" {
			t.Fatalf("unexpected 202 body: %+v", resp)
		}
		row := f.latestRow(t)
		if row.Status != "pending" || !row.AgreementReview {
			t.Fatalf("unexpected row: %+v", row)
		}
		if got := f.cost(t); got != 1000 {
			t.Fatalf("cost changed while pending: %d", got)
		}
	})

	t.Run("decrease while locked applies directly with no row", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, true)
		w := f.putPrice(900)
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		if got := f.cost(t); got != 900 {
			t.Fatalf("decrease did not apply: %d", got)
		}
		if n := f.rowCount(t); n != 0 {
			t.Fatalf("decrease wrote %d governance rows", n)
		}
	})

	t.Run("review approve with drifted cost conflicts and row stays pending; reject then closes it", func(t *testing.T) {
		f.reset(t)
		f.setGovernance(t, 10, true)
		w := f.putPrice(1050)
		if w.Code != http.StatusAccepted {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		resp := decodePricingPut(t, w)
		if _, err := f.db.Exec(`UPDATE products SET supplier_base_cost = 1100 WHERE id = $1`, f.productID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pricingRepo.ReviewPriceIncrease(f.ctx, resp.RequestID, true, "admin-1"); !errors.Is(err, repository.ErrPriceIncreaseStaleCost) {
			t.Fatalf("stale approve err = %v", err)
		}
		row := f.latestRow(t)
		if row.Status != "pending" {
			t.Fatalf("row left pending state after stale approve: %+v", row)
		}
		reviewed, err := f.pricingRepo.ReviewPriceIncrease(f.ctx, resp.RequestID, false, "admin-1")
		if err != nil {
			t.Fatal(err)
		}
		if reviewed.Status != "rejected" || reviewed.ReviewedAt == nil {
			t.Fatalf("unexpected rejected row: %+v", reviewed)
		}
		if got := f.cost(t); got != 1100 {
			t.Fatalf("reject changed the cost: %d", got)
		}
	})
}

func TestPriceIncreaseExceptionTicket(t *testing.T) {
	f := setupPriceGovFixture(t)

	post := func(productID string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"productId":%q,"newReference":2000,"message":"need room"}`, productID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/price-increases/exception-ticket", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, f.supplierID))
		w := httptest.NewRecorder()
		f.h.HandleSupplierPriceIncreaseExceptionTicket(w, req)
		return w
	}

	w := post(f.productID)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TicketID string `json:"ticketId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.TicketID == "" {
		t.Fatalf("bad ticket response %q: %v", w.Body.String(), err)
	}
	var section, role, subject string
	if err := f.db.QueryRow(`SELECT section, requester_role, subject FROM support_tickets WHERE id = $1`, resp.TicketID).Scan(&section, &role, &subject); err != nil {
		t.Fatal(err)
	}
	if section != "finance" || role != "supplier" {
		t.Fatalf("ticket routing wrong: section=%q role=%q", section, role)
	}
	if subject != "درخواست استثنای افزایش قیمت — Gov product" {
		t.Fatalf("unexpected subject: %q", subject)
	}

	if w := post("not-my-product"); w.Code != http.StatusNotFound {
		t.Fatalf("foreign product ticket got %d: %s", w.Code, w.Body.String())
	}
}

func TestPriceExceptionGrantLifecycle(t *testing.T) {
	f := setupPriceGovFixture(t)

	ex, err := f.pricingRepo.GetProductPriceException(f.ctx, f.productID)
	if err != nil || ex != nil {
		t.Fatalf("expected no grant, got %+v, %v", ex, err)
	}
	if err := f.pricingRepo.UpsertProductPriceException(f.ctx, f.productID, 20, nil, "admin-1"); err != nil {
		t.Fatal(err)
	}
	ex, err = f.pricingRepo.GetProductPriceException(f.ctx, f.productID)
	if err != nil || ex == nil || ex.ExceptionCapPct != 20 {
		t.Fatalf("grant not stored: %+v, %v", ex, err)
	}
	ticket := "ticket-123"
	if err := f.pricingRepo.UpsertProductPriceException(f.ctx, f.productID, 45, &ticket, "admin-2"); err != nil {
		t.Fatal(err)
	}
	ex, err = f.pricingRepo.GetProductPriceException(f.ctx, f.productID)
	if err != nil || ex == nil || ex.ExceptionCapPct != 45 || ex.TicketID == nil || *ex.TicketID != "ticket-123" || ex.GrantedBy != "admin-2" {
		t.Fatalf("grant upsert did not replace: %+v, %v", ex, err)
	}
	if err := f.pricingRepo.UpsertProductPriceException(f.ctx, "missing-product", 10, nil, "admin-1"); !errors.Is(err, repository.ErrProductNotFound) {
		t.Fatalf("grant for missing product err = %v", err)
	}
	if err := f.pricingRepo.DeleteProductPriceException(f.ctx, f.productID, "admin-1"); err != nil {
		t.Fatal(err)
	}
	ex, err = f.pricingRepo.GetProductPriceException(f.ctx, f.productID)
	if err != nil || ex != nil {
		t.Fatalf("grant survived revoke: %+v, %v", ex, err)
	}
	if err := f.pricingRepo.DeleteProductPriceException(f.ctx, f.productID, "admin-1"); !errors.Is(err, repository.ErrPriceExceptionNotFound) {
		t.Fatalf("second revoke err = %v", err)
	}
}

func TestImplicitAgreementActiveWindow(t *testing.T) {
	f := setupPriceGovFixture(t)

	active, err := f.pricingRepo.IsImplicitAgreementActive(f.ctx, f.productID)
	if err != nil || active {
		t.Fatalf("no flag must be inactive: %v %v", active, err)
	}
	if _, err := f.db.Exec(`UPDATE products SET implicit_agreement = TRUE, implicit_agreement_until = NULL WHERE id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if active, err = f.pricingRepo.IsImplicitAgreementActive(f.ctx, f.productID); err != nil || !active {
		t.Fatalf("open-ended agreement must be active: %v %v", active, err)
	}
	if _, err := f.db.Exec(`UPDATE products SET implicit_agreement_until = NOW() - INTERVAL '1 day' WHERE id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if active, err = f.pricingRepo.IsImplicitAgreementActive(f.ctx, f.productID); err != nil || active {
		t.Fatalf("expired agreement must be inactive: %v %v", active, err)
	}
	if _, err := f.db.Exec(`UPDATE products SET implicit_agreement_until = NOW() + INTERVAL '30 days' WHERE id = $1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if active, err = f.pricingRepo.IsImplicitAgreementActive(f.ctx, f.productID); err != nil || !active {
		t.Fatalf("future agreement must be active: %v %v", active, err)
	}
}

func TestAdminPriceIncreaseQueueAndReviewHandlers(t *testing.T) {
	f := setupPriceGovFixture(t)
	f.reset(t)
	f.setGovernance(t, 10, true)
	w := f.putPrice(1050)
	if w.Code != http.StatusAccepted {
		t.Fatalf("setup pending got %d: %s", w.Code, w.Body.String())
	}
	adminCtx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "admin-9"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/price-increases?status=pending", nil)
	req = req.WithContext(adminCtx)
	rec := httptest.NewRecorder()
	f.h.HandleAdminPriceIncreases(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue got %d: %s", rec.Code, rec.Body.String())
	}
	var list []repository.AdminPriceIncrease
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var found *repository.AdminPriceIncrease
	for i := range list {
		if list[i].ProductID == f.productID {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("pending row missing from admin queue: %s", rec.Body.String())
	}
	if found.ProductTitle != "Gov product" || found.SupplierID != f.supplierID || found.SupplierName != f.supplierNm {
		t.Fatalf("queue row fields wrong: %+v", found)
	}

	reviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/price-increases/"+found.ID+"/review", strings.NewReader(`{"approved":true}`))
	reviewReq = reviewReq.WithContext(adminCtx)
	reviewRec := httptest.NewRecorder()
	f.h.HandleAdminReviewPriceIncrease(reviewRec, reviewReq, found.ID)
	if reviewRec.Code != http.StatusOK {
		t.Fatalf("review got %d: %s", reviewRec.Code, reviewRec.Body.String())
	}
	if got := f.cost(t); got != 1050 {
		t.Fatalf("admin approve did not apply: %d", got)
	}

	againReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/price-increases/"+found.ID+"/review", strings.NewReader(`{"approved":true}`))
	againReq = againReq.WithContext(adminCtx)
	againRec := httptest.NewRecorder()
	f.h.HandleAdminReviewPriceIncrease(againRec, againReq, found.ID)
	if againRec.Code != http.StatusConflict {
		t.Fatalf("second review got %d: %s", againRec.Code, againRec.Body.String())
	}
}
