package main

import (
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/adguard"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
	"net"
	"net/http"
	"os"
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

func adClientIP(r *http.Request) string {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	for _, raw := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		_, network, e := net.ParseCIDR(strings.TrimSpace(raw))
		if e == nil && network.Contains(net.ParseIP(ip)) {
			if forwarded := net.ParseIP(r.Header.Get("X-Real-IP")); forwarded != nil {
				return forwarded.String()
			}
		}
	}
	return ip
}

func handleAdChallenge(w http.ResponseWriter, r *http.Request, guard *adguard.Service, repo *repository.StoreRepository) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		CampaignID string `json:"campaignId"`
		adguard.Signals
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&p) != nil {
		http.Error(w, "Invalid challenge", 400)
		return
	}
	if _, e := uuid.Parse(p.CampaignID); e != nil {
		http.Error(w, "Invalid campaign", 400)
		return
	}
	allowed, limitErr := repo.AllowAdChallenge(r.Context(), guard.Hash(adClientIP(r)))
	if limitErr != nil {
		writeInternalError(w, limitErr)
		return
	}
	if !allowed {
		http.Error(w, "Ad challenge rate limit", http.StatusTooManyRequests)
		return
	}
	token, e := guard.Issue(p.CampaignID, adClientIP(r), r.UserAgent(), p.Signals)
	if e != nil {
		if e = repo.RejectAdEvent(r.Context(), p.CampaignID, "challenge", "", e.Error(), guard.Hash(adClientIP(r))); e != nil {
			writeInternalError(w, e)
			return
		}
		http.Error(w, "درخواست تبلیغ نامعتبر است", 403)
		return
	}
	t01WriteJSON(w, 200, map[string]string{"challenge": token})
}

func handleSellerAdEvent(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, guard *adguard.Service) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		CampaignID string `json:"campaignId"`
		Type       string `json:"type"`
		Token      string `json:"token"`
		ProductID  string `json:"productId"`
		adguard.Proof
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
	evidence, err := guard.Verify(p.CampaignID, p.Type, adClientIP(r), r.UserAgent(), p.Proof)
	if err != nil {
		if e := repo.RejectAdEvent(r.Context(), p.CampaignID, p.Type, p.Token, err.Error(), guard.Hash(adClientIP(r))); e != nil {
			writeInternalError(w, e)
			return
		}
		t01WriteJSON(w, 200, map[string]interface{}{"accepted": false, "reason": err.Error()})
		return
	}
	accepted, err := repo.RecordSellerCampaignEvent(r.Context(), p.CampaignID, p.Type, p.Token, p.ProductID, evidence)
	if err != nil {
		campaignError(w, err)
		return
	}
	if !accepted {
		if e := repo.RejectAdEvent(r.Context(), p.CampaignID, p.Type, p.Token, "duplicate_click", evidence.IPHash); e != nil {
			writeInternalError(w, e)
			return
		}
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
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeMethodNotAllowed(w)
			return
		}
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
	if err := repo.PatchSellerCampaign(r.Context(), claims.UserID, slug, id, p.Name, p.ProductIDs, p.Status); err != nil {
		campaignError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
