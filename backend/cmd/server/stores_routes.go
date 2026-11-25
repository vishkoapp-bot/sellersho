package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/privacy"
	"github.com/drup/platform/internal/repository"
)

func handleStoresBlock(jwtService *auth.JWTService, storeRepo *repository.StoreRepository, finRepo *repository.FinanceRepository, catalogRepo *repository.CatalogRepository, pricingRepo *repository.PricingRepository, orderRepo *repository.OrderRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/stores/")
		if subPath == "" {
			handleStoresList(w, r, ctx, storeRepo)
			return
		}
		if subPath == "register" {
			if r.Method == http.MethodPost {
				RequireRoles(jwtService, "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
					handleRegisterStore(w, r, r.Context(), storeRepo)
				})(w, r)
				return
			}
			writeMethodNotAllowed(w)
			return
		}

		parts := strings.Split(strings.TrimSuffix(subPath, "/"), "/")
		slug := parts[0]
		if len(parts) >= 2 && parts[1] == "campaigns" {
			RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
				if len(parts) == 2 {
					handleSellerCampaigns(w, r, storeRepo, slug)
					return
				}
				if len(parts) == 3 {
					handleSellerCampaignAction(w, r, storeRepo, slug, "")
					return
				}
				if len(parts) == 4 && (parts[3] == "analytics" || parts[3] == "history") {
					handleSellerCampaignAction(w, r, storeRepo, slug, parts[3])
					return
				}
				http.NotFound(w, r)
			})(w, r)
			return
		}
		// Team 05 (Phase 2, D48/D47): seller discount-campaign CRUD
		// and the seller subscribers list. Both are seller-owned
		// routes; the dashboard tabs that consume them belong to
		// Team 08.
		if len(parts) >= 2 && parts[1] == "discount-campaigns" {
			RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
				id := ""
				if len(parts) == 3 {
					id = parts[2]
				}
				if len(parts) > 3 {
					http.NotFound(w, r)
					return
				}
				handleTeam05StoreDiscountCampaigns(w, r, storeRepo, slug, id)
			})(w, r)
			return
		}
		if len(parts) == 2 && parts[1] == "subscribers" {
			RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
				handleTeam05StoreSubscribers(w, r, storeRepo, slug)
			})(w, r)
			return
		}
		if len(parts) == 2 && parts[1] == "public-campaigns" {
			handlePublicSellerCampaigns(w, r, storeRepo, slug)
			return
		}
		// Team Discounts (Phase 3): public discount quote (no auth,
		// read-only, never consumes a code use). Campaign CRUD above
		// is live-parity Team 05 and is reused unchanged.
		if len(parts) == 2 && parts[1] == "discount-quote" {
			handleDiscountQuote(w, r, catalogRepo, storeRepo, pricingRepo, slug)
			return
		}
		if len(parts) == 3 && parts[1] == "discount-codes" {
			RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					http.NotFound(w, r)
					return
				}
				handleSellerStore(w, r, storeRepo, finRepo, slug, parts[1], parts[2])
			})(w, r)
			return
		}
		if len(parts) == 2 {
			switch parts[1] {
			case "settings", "products", "report", "balance", "wallet", "statement", "payouts", "early-withdrawals", "discount-codes", "change-requests", "services":
				RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
					handleSellerStore(w, r, storeRepo, finRepo, slug, parts[1], "")
				})(w, r)
				return
			}
		}
		if len(parts) == 2 && parts[1] == "ad-widget" {
			if r.Method != http.MethodPut {
				writeMethodNotAllowed(w)
				return
			}
			RequireRoles(jwtService, "seller", "admin")(func(w http.ResponseWriter, r *http.Request) { handleSaveStoreWidget(w, r, storeRepo, slug) })(w, r)
			return
		}

		if len(parts) > 1 && parts[1] == "pricing" && r.Method == http.MethodPut {
			RequireRoles(jwtService, "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
				handleStoreOverrides(w, r, r.Context(), slug, storeRepo)
			})(w, r)
			return
		}

		if len(parts) > 1 && parts[1] == "referrals" {
			if !authorizeStoreRead(w, r, jwtService, storeRepo, slug) {
				return
			}
			store, _ := storeRepo.GetStoreBySlug(ctx, slug)
			if store == nil {
				http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
				return
			}
			referred, err := storeRepo.GetReferredStoresWithStatus(ctx, store.ID)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			json.NewEncoder(w).Encode(referred)
			return
		}

		if len(parts) > 1 && parts[1] == "referral-notifications" {
			if !authorizeStoreRead(w, r, jwtService, storeRepo, slug) {
				return
			}
			store, _ := storeRepo.GetStoreBySlug(ctx, slug)
			if store == nil {
				http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
				return
			}
			notifs, err := storeRepo.GetReferralBonusNotifications(ctx, store.ID)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			json.NewEncoder(w).Encode(notifs)
			return
		}

		if len(parts) == 2 && parts[1] == "work-queue" {
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w)
				return
			}
			if !authorizeStoreRead(w, r, jwtService, storeRepo, slug) {
				return
			}
			store, err := storeRepo.GetStoreBySlug(ctx, slug)
			if err != nil || store == nil {
				http.NotFound(w, r)
				return
			}
			handleSellerWorkCounts(w, r, orderRepo, store.ID)
			return
		}

		if len(parts) > 1 && parts[1] == "orders" {
			if !authorizeStoreRead(w, r, jwtService, storeRepo, slug) {
				return
			}
			store, _ := storeRepo.GetStoreBySlug(ctx, slug)
			if store == nil {
				http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
				return
			}
			opts, perr := parseListPage(r)
			if perr != nil {
				http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
				return
			}
			work := r.URL.Query().Get("work")
			if work != "" && work != "follow-up" {
				t01WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "فیلتر سفارش معتبر نیست."})
				return
			}
			page, err := orderRepo.GetOrdersByStoreIDPage(ctx, store.ID, opts, work)
			if err != nil {
				if isInvalidCursorError(err) {
					http.Error(w, `{"error":"cursor is invalid"}`, http.StatusBadRequest)
					return
				}
				writeInternalError(w, err)
				return
			}
			orders := page.Items
			for i := range orders {
				masked := privacy.MaskCustomerForRole(privacy.CustomerInfo{
					FullName:   orders[i].CustomerFullName,
					Phone:      orders[i].CustomerPhone,
					Province:   orders[i].CustomerProvince,
					City:       orders[i].CustomerCity,
					Address:    orders[i].CustomerAddress,
					PostalCode: orders[i].CustomerPostalCode,
				}, "seller")
				orders[i].CustomerFullName = masked.FullName
				orders[i].CustomerPhone = masked.Phone
				orders[i].CustomerAddress = masked.Address
				orders[i].CustomerPostalCode = masked.PostalCode
			}
			if orders == nil {
				orders = []domain.Order{}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"orders": orders, "nextCursor": page.NextCursor, "hasMore": page.HasMore, "limit": page.Limit})
			return
		}

		if len(parts) > 1 && parts[1] == "returns" {
			if !authorizeStoreRead(w, r, jwtService, storeRepo, slug) {
				return
			}
			store, _ := storeRepo.GetStoreBySlug(ctx, slug)
			if store == nil {
				http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
				return
			}
			handleStoreReturns(w, r, ctx, store.ID, orderRepo)
			return
		}

		if len(parts) > 1 {
			http.NotFound(w, r)
			return
		}

		// Single store info (Team 05, D52): public, emits the live
		// public keys plus the 3 FIX3 public storefront fields
		// (templateId, templateConfig, logo) -- never the full Store
		// object. FIX 14 corrects the old D45 assumption: live returns
		// 404 "Store is suspended" for suspended slugs (probed live
		// 2026-11-03 for all 6 suspended slugs), so this endpoint does
		// too; the public LIST stays active-only (4 stores).
		store, err := storeRepo.GetStoreBySlug(ctx, slug)
		if err != nil || store == nil {
			http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
			return
		}
		if store.Status == "suspended" {
			http.Error(w, `{"error":"Store is suspended"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(team05PublicStoreView(store))
	}
}
