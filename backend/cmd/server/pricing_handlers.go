package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
)

// Pricing handlers (Team 02, live parity). This file owns only the
// pricing part of the admin settings family (D14): platform, pricing,
// margins, platform-pct and the fee-overrides HISTORY read path. The
// full admin settings family beyond pricing belongs to the API/identity
// teams and is deliberately not duplicated here.
//
// Deliberately NOT in this file (D20 / settlement boundary):
//   - admin/settings/base-fee: deleted in live, must not return.
//   - admin/settings/settlement and any monthly-settlement route or
//     auto-sweep: the live manual settlement engine belongs to the
//     finance scope; this package only reads settlement_day /
//     early_withdrawal_fee_pct as settings data, never acts on them.
//   - seller_discount_codes / discount-quote: branch-only, not live.
//   - absolute fee-override WRITES: dead in live (D30); only the
//     historical read path exists here.

// loadPricingSettings loads the percent-engine settings, falling back to
// the live migration defaults only when the row cannot be loaded at all.
// The error is returned alongside the fallback so callers can log it;
// price paths use the fallback rather than failing the request.
func loadPricingSettings(ctx context.Context, pricingRepo *repository.PricingRepository) (*pricing.PlatformPricingSettings, error) {
	if pricingRepo == nil {
		return pricing.DefaultPlatformPricingSettings(), nil
	}
	s, err := pricingRepo.GetPlatformPricingSettings(ctx)
	if err != nil {
		return pricing.DefaultPlatformPricingSettings(), err
	}
	return s, nil
}

// loadStoreMarginLocks returns a store's margin locks, or nil when the
// pricing repo is unavailable (pricing then proceeds without locks).
// Locks are loaded once per request by the product/order handlers.
func loadStoreMarginLocks(ctx context.Context, pricingRepo *repository.PricingRepository, storeID string) []pricing.MarginLock {
	if pricingRepo == nil || storeID == "" {
		return nil
	}
	locks, err := pricingRepo.ListStoreMarginLocks(ctx, storeID)
	if err != nil {
		return nil
	}
	return locks
}

func writePricingJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// handleAdminPricingSettings implements GET/PUT
// /api/v1/admin/settings/pricing. GET returns the percent-engine settings
// (plus settlement settings data and the historical fee overrides,
// clearly separated). PUT writes only the live binary's five pricing
// columns (platform_pct, central_profit_pct, increase_cap_pct,
// increase_locked, max_profit_margin).
func handleAdminPricingSettings(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, err := pricingRepo.GetPlatformPricingSettings(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		history, _ := pricingRepo.GetFeeOverridesHistory(r.Context())
		writePricingJSON(w, map[string]interface{}{
			"settings": s,
			// Historical absolute overrides, NOT price inputs (D30).
			"feeOverridesHistory": history,
			"notes": map[string]string{
				"centralStoreMarkupPercent": "stored only; storefront central price uses centralProfitPct (TODO U12 for the order path)",
				"settlement":                "settings data only; no settlement cycle has run in live and no auto worker is created here",
			},
		})
	case http.MethodPut, http.MethodPost:
		var req pricing.PlatformPricingSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		if req.PlatformPct < 0 || req.PlatformPct > 1000 {
			http.Error(w, `{"error":"platformPct must be between 0 and 1000"}`, http.StatusBadRequest)
			return
		}
		if req.CentralProfitPct < 0 || req.CentralProfitPct > 100 {
			http.Error(w, `{"error":"centralProfitPct must be between 0 and 100"}`, http.StatusBadRequest)
			return
		}
		if req.IncreaseCapPct < 0 || req.MaxProfitMargin < 0 {
			http.Error(w, `{"error":"increaseCapPct and maxProfitMargin must be >= 0"}`, http.StatusBadRequest)
			return
		}
		if err := pricingRepo.UpdatePlatformPricingSettings(r.Context(), &req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleAdminMargins implements GET /api/v1/admin/settings/margins: the
// margins subset of the pricing settings family.
func handleAdminMargins(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	s, err := pricingRepo.GetPlatformPricingSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writePricingJSON(w, map[string]interface{}{
		"defaultMargin":             s.DefaultMargin,
		"maxProfitMargin":           s.MaxProfitMargin,
		"centralProfitPct":          s.CentralProfitPct,
		"centralStoreMarkupPercent": s.CentralStoreMarkupPercent,
		"centralMarkupNote":         "centralStoreMarkupPercent is stored only; central storefront price uses centralProfitPct (TODO U12)",
	})
}

// handleAdminPlatformPct implements the live /api/v1/admin/platform-pct
// family (D14/D24): GET lists both ladder tables; POST sets one ladder
// row ({"scope":"product"|"category","id": "...","pct": n}); DELETE with
// ?productId= or ?categorySlug= removes one row.
func handleAdminPlatformPct(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		lists, err := pricingRepo.ListPlatformPcts(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, lists)
	case http.MethodPost, http.MethodPut:
		var req struct {
			Scope string  `json:"scope"`
			ID    string  `json:"id"`
			Pct   float64 `json:"pct"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		if req.Pct < 0 || req.Pct > 1000 {
			http.Error(w, `{"error":"pct must be between 0 and 1000"}`, http.StatusBadRequest)
			return
		}
		var err error
		switch req.Scope {
		case "product":
			err = pricingRepo.SetProductPlatformPct(r.Context(), req.ID, req.Pct)
		case "category":
			err = pricingRepo.SetCategoryPlatformPct(r.Context(), req.ID, req.Pct)
		default:
			http.Error(w, `{"error":"scope must be product or category"}`, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	case http.MethodDelete:
		productID := r.URL.Query().Get("productId")
		categorySlug := r.URL.Query().Get("categorySlug")
		var err error
		switch {
		case productID != "":
			err = pricingRepo.DeleteProductPlatformPct(r.Context(), productID)
		case categorySlug != "":
			err = pricingRepo.DeleteCategoryPlatformPct(r.Context(), categorySlug)
		default:
			http.Error(w, `{"error":"productId or categorySlug required"}`, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleAdminFeeOverridesHistory implements GET
// /api/v1/admin/settings/fee-overrides and GET
// /api/v1/admin/settings/product-fee-override as HISTORY reads only
// (D30). There is deliberately no write endpoint here: absolute fee
// overrides are dead in the live percent engine.
func handleAdminFeeOverridesHistory(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	history, err := pricingRepo.GetFeeOverridesHistory(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writePricingJSON(w, history)
}

// handleAdminMarginLocks implements GET/POST/DELETE
// /api/v1/admin/margin-locks?storeId=... (D29). POST body:
// {"storeId","scope":"product"|"niche"|"variant","scopeId","mode":"freeze"|"admin_set","value": n?}.
// DELETE uses ?storeId=&scope=&scopeId=. Both tables are 0 rows in live;
// this implements the mechanism (including snapshot/restore on delete).
func handleAdminMarginLocks(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodGet:
		storeID := r.URL.Query().Get("storeId")
		if storeID == "" {
			http.Error(w, `{"error":"storeId required"}`, http.StatusBadRequest)
			return
		}
		locks, err := pricingRepo.ListStoreMarginLocks(r.Context(), storeID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if locks == nil {
			locks = []pricing.MarginLock{}
		}
		writePricingJSON(w, map[string]interface{}{"locks": locks})
	case http.MethodPost, http.MethodPut:
		var req struct {
			StoreID string   `json:"storeId"`
			Scope   string   `json:"scope"`
			ScopeID string   `json:"scopeId"`
			Mode    string   `json:"mode"`
			Value   *float64 `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StoreID == "" || req.ScopeID == "" {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		if err := pricingRepo.SetStoreMarginLock(r.Context(), req.StoreID, pricing.MarginLock{Scope: req.Scope, ScopeID: req.ScopeID, Mode: req.Mode, Value: req.Value}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	case http.MethodDelete:
		storeID := r.URL.Query().Get("storeId")
		scope := r.URL.Query().Get("scope")
		scopeID := r.URL.Query().Get("scopeId")
		if storeID == "" || scope == "" || scopeID == "" {
			http.Error(w, `{"error":"storeId, scope and scopeId required"}`, http.StatusBadRequest)
			return
		}
		if err := pricingRepo.DeleteStoreMarginLock(r.Context(), storeID, scope, scopeID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleSellerPricingSettings serves the published percent-pricing rules
// a seller's panel needs to simulate prices exactly like the server:
// GET /api/v1/seller/pricing-settings (seller role only). It returns only
// the five published numbers — no ladder maps, no historical fee values.
// A settings-load failure fails closed (500) rather than serving fallback
// defaults as if they were the platform's rules.
func handleSellerPricingSettings(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if pricingRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	s, err := pricingRepo.GetPlatformPricingSettings(r.Context())
	if err != nil {
		http.Error(w, `{"error":"Pricing settings unavailable"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"platformPct":     s.PlatformPct,
		"centralProfitPct": s.CentralProfitPct,
		"minProfitMargin": s.MinProfitMargin,
		"maxProfitMargin": s.MaxProfitMargin,
		"defaultMargin":   s.DefaultMargin,
	})
}

// handleSellerVariantOverrides implements the seller variant-override
// mechanism (D28) at /api/v1/seller/variant-overrides. GET lists the
// caller's own store's overrides (?storeId=); PUT sets one
// ({"storeId","productId","variantId","value","mode":"percent"}); DELETE uses
// ?storeId=&productId=&variantId=. Ownership is checked against the
// caller's stores; the legacy boolean margin lock is read compatibility
// only and does not gate this endpoint (new-model locks do, at price
// resolution time). No fake data is seeded: the live table is 0 rows.
func handleSellerVariantOverrides(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository, storeRepo *repository.StoreRepository) {
	w.Header().Set("Content-Type", "application/json")
	if pricingRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	owns := func(storeID string) bool {
		stores, err := storeRepo.GetStoresByOwner(r.Context(), claims.UserID)
		if err != nil {
			return false
		}
		for _, s := range stores {
			if s.ID == storeID {
				return true
			}
		}
		return false
	}
	switch r.Method {
	case http.MethodGet:
		storeID := r.URL.Query().Get("storeId")
		if storeID == "" || !owns(storeID) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		overrides, err := pricingRepo.ListStoreVariantOverrides(r.Context(), storeID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if overrides == nil {
			overrides = []pricing.StoreVariantOverride{}
		}
		writePricingJSON(w, map[string]interface{}{"overrides": overrides})
	case http.MethodPut, http.MethodPost:
		var req pricing.StoreVariantOverride
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StoreID == "" || req.ProductID == "" || req.VariantID == "" {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
		if !owns(req.StoreID) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		if err := pricingRepo.SetStoreVariantOverride(r.Context(), req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	case http.MethodDelete:
		storeID := r.URL.Query().Get("storeId")
		productID := r.URL.Query().Get("productId")
		variantID := r.URL.Query().Get("variantId")
		if storeID == "" || productID == "" || variantID == "" || !owns(storeID) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		if err := pricingRepo.DeleteStoreVariantOverride(r.Context(), storeID, productID, variantID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writePricingJSON(w, map[string]interface{}{"ok": true})
	default:
		writeMethodNotAllowed(w)
	}
}
