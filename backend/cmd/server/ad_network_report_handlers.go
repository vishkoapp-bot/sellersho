package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/drup/platform/internal/repository"
)

func handleAdNetworkReport(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	days := 7
	if raw := r.URL.Query().Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || (value != 7 && value != 30 && value != 90) {
			http.Error(w, "Report window must be 7, 30 or 90 days", http.StatusBadRequest)
			return
		}
		days = value
	}
	if repo == nil {
		http.Error(w, "Report unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	report, err := repo.GetAdNetworkReport(ctx, days)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, report)
}
