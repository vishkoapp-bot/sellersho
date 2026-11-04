package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

func handleSaveStoreWidget(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug string) {
	store, err := repo.GetStoreBySlug(r.Context(), slug)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !claims.HasDirectCapability("admin") && (store.OwnerUserID == nil || *store.OwnerUserID != claims.UserID) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}
	var config struct {
		Title          string   `json:"title"`
		ProductIDs     []string `json:"productIds"`
		CountdownHours int      `json:"countdownHours"`
		EndsAt         string   `json:"endsAt"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&config) != nil || strings.TrimSpace(config.Title) == "" || len(config.Title) > 300 || config.CountdownHours < 1 || config.CountdownHours > 168 || len(config.ProductIDs) < 1 || len(config.ProductIDs) > 100 {
		http.Error(w, `{"error":"Invalid widget configuration"}`, http.StatusBadRequest)
		return
	}
	if valid, err := repo.WidgetProductsExist(r.Context(), config.ProductIDs); err != nil {
		writeInternalError(w, err)
		return
	} else if !valid {
		http.Error(w, `{"error":"Unknown product selected"}`, http.StatusBadRequest)
		return
	}
	config.Title = strings.TrimSpace(config.Title)
	config.EndsAt = time.Now().Add(time.Duration(config.CountdownHours) * time.Hour).UTC().Format(time.RFC3339)
	raw, err := json.Marshal(config)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := repo.SaveOwnedWidgetConfig(r.Context(), claims.UserID, slug, raw); err != nil {
		campaignError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
