package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// Fingerprint the caller's normalized immutable intent BEFORE resolving saved
// addresses or current catalog/quote facts. A committed replay needs neither.
func normalizeCheckoutRequest(req *CreateOrderReq) string {
	req.StoreID = strings.TrimSpace(req.StoreID)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.PaymentMethod = strings.TrimSpace(req.PaymentMethod)
	if req.PaymentMethod == "" {
		req.PaymentMethod = "cash"
	}
	req.AddressID = strings.TrimSpace(req.AddressID)
	req.ShippingQuoteID = strings.TrimSpace(req.ShippingQuoteID)
	req.DiscountCode = strings.TrimSpace(req.DiscountCode)
	c := &req.CustomerInfo
	c.FullName = strings.TrimSpace(c.FullName)
	c.Phone = strings.TrimSpace(c.Phone)
	c.Province = strings.TrimSpace(c.Province)
	c.City = strings.TrimSpace(c.City)
	c.Address = strings.TrimSpace(c.Address)
	c.PostalCode = strings.TrimSpace(c.PostalCode)
	for i := range req.Items {
		it := &req.Items[i]
		it.ProductID = strings.TrimSpace(it.ProductID)
		it.VariantID = strings.TrimSpace(it.VariantID)
		for j := range it.AddonIDs {
			it.AddonIDs[j] = strings.TrimSpace(it.AddonIDs[j])
		}
		sort.Strings(it.AddonIDs)
	}
	sort.SliceStable(req.Items, func(i, j int) bool {
		a, _ := json.Marshal(req.Items[i])
		b, _ := json.Marshal(req.Items[j])
		return string(a) < string(b)
	})
	copy := *req
	copy.IdempotencyKey = ""
	data, _ := json.Marshal(copy)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func writeCheckoutError(w http.ResponseWriter, err error) {
	for _, e := range []error{repository.ErrCouponExhausted, repository.ErrCouponInvalid, repository.ErrCouponChanged, repository.ErrCouponEvidence, repository.ErrCouponLifecycleRequired} {
		if errors.Is(err, e) {
			http.Error(w, `{"error":"`+e.Error()+`","code":"`+e.Error()+`"}`, http.StatusConflict)
			return
		}
	}
	if errors.Is(err, repository.ErrCheckoutConflict) || errors.Is(err, repository.ErrCheckoutAbandoned) || errors.Is(err, repository.ErrCheckoutLegacy) {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusConflict)
		return
	}
	writeInternalError(w, err)
}

func handleCheckoutAttempt(w http.ResponseWriter, r *http.Request, repo *repository.OrderRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil || claims.UserID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, 401)
		return
	}
	if repo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, 503)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/checkout-attempts/")
	if len(key) == 0 || len(key) > 128 || strings.Contains(key, "/") || key != strings.TrimSpace(key) {
		http.Error(w, `{"error":"Invalid checkout attempt"}`, 400)
		return
	}
	var a *repository.CheckoutAttempt
	var err error
	switch r.Method {
	case http.MethodGet:
		a, err = repo.GetCheckoutAttempt(r.Context(), claims.UserID, key)
	case http.MethodPost:
		var body struct {
			Action string `json:"action"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body) != nil || body.Action != "abandon" {
			http.Error(w, `{"error":"Invalid checkout action"}`, 400)
			return
		}
		a, err = repo.AbandonCheckoutAttempt(r.Context(), claims.UserID, key)
	default:
		writeMethodNotAllowed(w)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, `{"error":"Checkout attempt not found"}`, 404)
		return
	}
	if err != nil {
		writeCheckoutError(w, err)
		return
	}
	json.NewEncoder(w).Encode(a)
}
