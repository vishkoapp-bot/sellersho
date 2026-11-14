package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/bank"
	"github.com/drup/platform/internal/repository"
)

func sellerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrStoreForbidden):
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, nil)
	case errors.Is(err, repository.ErrStoreInsufficientBalance):
		// Wallet-funded purchase (subscription/SMS credit) the store
		// cannot afford yet — say so in Persian instead of the generic
		// English "Invalid input" this used to collapse into.
		http.Error(w, `{"error":"موجودی کیف پول فروشگاه برای این خرید کافی نیست."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrSMSServiceUnavailable):
		http.Error(w, `{"error":"بسته‌های پیامک در حال حاضر قابل خرید نیستند؛ مسیر ارسال پیامک هنوز فعال نشده است."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrUnknownSellerService):
		http.Error(w, `{"error":"این اشتراک یا خدمت در حال حاضر قابل خرید نیست."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrStoreInput):
		http.Error(w, `{"error":"Invalid input"}`, http.StatusBadRequest)
	default:
		writeInternalError(w, err)
	}
}

func handleSellerStore(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, fin *repository.FinanceRepository, slug, action string) {
	w.Header().Set("Content-Type", "application/json")
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	store, err := repo.OwnedStore(r.Context(), claims.UserID, slug)
	if err != nil {
		sellerError(w, err)
		return
	}
	switch action {
	case "services":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Service string `json:"service"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&payload) != nil {
			http.Error(w, `{"error":"اطلاعات ارسالی نامعتبر است."}`, http.StatusBadRequest)
			return
		}
		err = fin.PurchaseSellerService(r.Context(), claims.UserID, store.ID, payload.Service)
	case "settings":
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w)
			return
		}
		var config map[string]interface{}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&config) != nil {
			http.Error(w, `{"error":"Invalid settings"}`, 400)
			return
		}
		err = repo.SaveStoreSettings(r.Context(), claims.UserID, slug, config)
	case "products":
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			ProductIDs []string `json:"productIds"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil || payload.ProductIDs == nil {
			http.Error(w, `{"error":"Product IDs required"}`, 400)
			return
		}
		err = repo.SaveStoreProducts(r.Context(), claims.UserID, slug, payload.ProductIDs)
	case "report":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		start, end := r.URL.Query().Get("start"), r.URL.Query().Get("end")
		for _, v := range []string{start, end} {
			if v != "" {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					http.Error(w, `{"error":"Invalid date"}`, 400)
					return
				}
			}
		}
		if start != "" && end != "" && start > end {
			http.Error(w, `{"error":"Invalid date range"}`, 400)
			return
		}
		report, err := repo.SellerReport(r.Context(), claims.UserID, slug, start, end)
		if err != nil {
			sellerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(report)
		return
	case "balance":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		balance, err := fin.GetStoreBalance(r.Context(), store.ID)
		if err != nil {
			sellerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(balance)
		return
	case "payouts":
		if r.Method == http.MethodGet {
			items, err := fin.GetStorePayouts(r.Context(), claims.UserID, store.ID)
			if err != nil {
				sellerError(w, err)
				return
			}
			json.NewEncoder(w).Encode(items)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Amount int64  `json:"amount"`
			Shaba  string `json:"shaba"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil || payload.Amount <= 0 {
			http.Error(w, `{"error":"Invalid payout"}`, 400)
			return
		}
		check := bank.VerifySheba(payload.Shaba)
		if !check.IsValid {
			http.Error(w, `{"error":"Invalid IBAN"}`, 400)
			return
		}
		item, err := fin.RequestOwnedStorePayout(r.Context(), claims.UserID, store.ID, strings.ToUpper(strings.TrimSpace(payload.Shaba)), check.BankName, payload.Amount)
		if err != nil {
			sellerError(w, err)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(item)
		return
	case "wallet":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		breakdown, err := fin.GetWalletBreakdown(r.Context(), store.ID)
		if err != nil {
			sellerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(breakdown)
		return
	case "early-withdrawals":
		if r.Method == http.MethodGet {
			items, err := fin.ListEarlyWithdrawals(r.Context(), store.ID)
			if err != nil {
				sellerError(w, err)
				return
			}
			json.NewEncoder(w).Encode(items)
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Amount int64  `json:"amount"`
			Shaba  string `json:"shaba"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil || payload.Amount <= 0 {
			http.Error(w, `{"error":"Invalid early withdrawal"}`, 400)
			return
		}
		check := bank.VerifySheba(payload.Shaba)
		if !check.IsValid {
			http.Error(w, `{"error":"Invalid IBAN"}`, 400)
			return
		}
		item, err := fin.RequestEarlyWithdrawal(r.Context(), claims.UserID, store.ID, payload.Amount, strings.ToUpper(strings.TrimSpace(payload.Shaba)), check.BankName)
		if err != nil {
			settlementSellerError(w, err)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(item)
		return
	case "discount-codes":
		// Team Discounts (Phase 3): seller discount-code CRUD for the
		// owned store (GET/POST/PUT on the collection). Campaign CRUD
		// lives in the live-parity Team 05 handler and is NOT
		// duplicated here. The toggle uses PUT on the collection with
		// {id, isActive} (old-branch shape), unlike the Team 05
		// campaign PATCH /:id shape, because handleSellerStore
		// receives no id path part.
		if r.Method == http.MethodGet {
			items, err := repo.ListDiscountCodes(r.Context(), store.ID)
			if err != nil {
				sellerError(w, err)
				return
			}
			if items == nil {
				items = []repository.DiscountCode{}
			}
			json.NewEncoder(w).Encode(items)
			return
		}
		if r.Method == http.MethodPut {
			var payload struct {
				ID       string `json:"id"`
				IsActive bool   `json:"isActive"`
			}
			if decodeMoneyJSON(r.Body, &payload) != nil || payload.ID == "" {
				http.Error(w, `{"error":"Invalid request"}`, 400)
				return
			}
			if err := repo.SetDiscountCodeActive(r.Context(), store.ID, payload.ID, payload.IsActive); err != nil {
				sellerError(w, err)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Code     string    `json:"code"`
			Mode     string    `json:"mode"`
			Value    float64   `json:"value"`
			StartsAt time.Time `json:"startsAt"`
			EndsAt   time.Time `json:"endsAt"`
			MaxUses  *int      `json:"maxUses"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil || payload.Code == "" {
			http.Error(w, `{"error":"Invalid code"}`, 400)
			return
		}
		item, err := repo.CreateDiscountCode(r.Context(), store.ID, repository.DiscountCode{Code: payload.Code, Mode: payload.Mode, Value: payload.Value, StartsAt: payload.StartsAt, EndsAt: payload.EndsAt, MaxUses: payload.MaxUses})
		if err != nil {
			sellerError(w, err)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(item)
		return
	case "change-requests":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var payload struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil {
			http.Error(w, `{"error":"Invalid request"}`, 400)
			return
		}
		err = repo.RequestStoreChange(r.Context(), claims.UserID, slug, payload.Kind, payload.Value)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		sellerError(w, err)
		return
	}
	updated, err := repo.GetStoreBySlug(r.Context(), slug)
	if err != nil {
		sellerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func handleSellerTickets(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if r.Method == http.MethodPost {
		var payload struct {
			Subject string `json:"subject"`
			Message string `json:"message"`
		}
		if decodeMoneyJSON(r.Body, &payload) != nil {
			http.Error(w, `{"error":"Invalid ticket"}`, 400)
			return
		}
		if err := repo.CreateSellerTicket(r.Context(), claims.UserID, payload.Subject, payload.Message); err != nil {
			sellerError(w, err)
			return
		}
	} else if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	items, err := repo.SellerTickets(r.Context(), claims.UserID)
	if err != nil {
		sellerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(items)
}
