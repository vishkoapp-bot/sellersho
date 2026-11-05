package main

import (
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

func campaignError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrStoreForbidden) || errors.Is(err, repository.ErrStoreInput) {
		sellerError(w, err)
		return
	}
	switch {
	case errors.Is(err, repository.ErrCampaignForbidden):
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
	case errors.Is(err, repository.ErrCampaignInput):
		http.Error(w, `{"error":"Invalid campaign"}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrCampaignInactive):
		http.Error(w, `{"error":"Campaign is inactive"}`, http.StatusConflict)
	case errors.Is(err, repository.ErrCampaignWidgetManaged):
		// F2: the widget campaign row mirrors the deals-widget
		// config; its status is not the switch. Tell the seller
		// where the deal is actually managed, in Persian.
		http.Error(w, `{"error":"کمپین ویجت پیشنهاد شگفت‌انگیز از بخش ویجت‌ها مدیریت می‌شود؛ برای توقف یا فعال‌سازی پیشنهاد از همان بخش اقدام کنید."}`, http.StatusBadRequest)
	default:
		writeInternalError(w, err)
	}
}

func handlePublicSellerCampaigns(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	list, err := repo.PublicSellerCampaigns(r.Context(), slug)
	if err != nil {
		campaignError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func handleSellerAdEvent(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		CampaignID string `json:"campaignId"`
		Type       string `json:"type"`
		Token      string `json:"token"`
		ProductID  string `json:"productId"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&p) != nil {
		campaignError(w, repository.ErrCampaignInput)
		return
	}
	if _, err := uuid.Parse(p.CampaignID); err != nil {
		campaignError(w, repository.ErrCampaignInput)
		return
	}
	if strings.TrimSpace(p.Token) == "" || len(p.Token) > 200 {
		campaignError(w, repository.ErrCampaignInput)
		return
	}
	accepted, err := repo.RecordSellerCampaignEvent(r.Context(), p.CampaignID, p.Type, p.Token, p.ProductID)
	if err != nil {
		campaignError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"accepted": accepted})
}

func handleSellerCampaigns(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug string) {
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		list, err := repo.GetSellerCampaigns(r.Context(), claims.UserID, slug)
		if err != nil {
			campaignError(w, err)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		Name          string                 `json:"name"`
		Type          string                 `json:"type"`
		ProductIDs    []string               `json:"productIds"`
		Configuration map[string]interface{} `json:"configuration"`
		Status        string                 `json:"status"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&p) != nil {
		campaignError(w, repository.ErrCampaignInput)
		return
	}
	if p.Configuration == nil {
		p.Configuration = map[string]interface{}{}
	}
	if p.Status == "" {
		p.Status = "draft"
	}
	c, err := repo.CreateSellerCampaign(r.Context(), claims.UserID, slug, p.Name, strings.ToLower(p.Type), p.ProductIDs, p.Configuration, p.Status)
	if err != nil {
		campaignError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(c)
}

func handleSellerCampaignAction(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug, action string) {
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	w.Header().Set("Content-Type", "application/json")
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/stores/"+slug+"/campaigns/")
	if action == "analytics" {
		id = strings.TrimSuffix(id, "/analytics")
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		a, err := repo.SellerCampaignAnalytics(r.Context(), claims.UserID, slug, id)
		if err != nil {
			campaignError(w, err)
			return
		}
		json.NewEncoder(w).Encode(a)
		return
	}
	if action == "history" {
		id = strings.TrimSuffix(id, "/history")
		items, err := repo.SellerCampaignHistory(r.Context(), claims.UserID, slug, id)
		if err != nil {
			campaignError(w, err)
			return
		}
		json.NewEncoder(w).Encode(items)
		return
	}
	id = strings.TrimSuffix(id, "/")
	if r.Method != http.MethodPatch {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		Status     string   `json:"status"`
		Name       string   `json:"name"`
		ProductIDs []string `json:"productIds"`
	}
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		campaignError(w, repository.ErrCampaignInput)
		return
	}
	if p.Name != "" || p.ProductIDs != nil {
		if err := repo.UpdateSellerCampaign(r.Context(), claims.UserID, slug, id, p.Name, p.ProductIDs, nil, nil); err != nil {
			campaignError(w, err)
			return
		}
	}
	if p.Status == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := repo.SetSellerCampaignStatus(r.Context(), claims.UserID, slug, id, p.Status); err != nil {
		campaignError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
