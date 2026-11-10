package main

import (
	"database/sql"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/warehouseopt"
	"net/http"
	"strings"
	"time"
)

func decodePosition(w http.ResponseWriter, r *http.Request, p interface{}) bool {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(p) == nil
}
func handleBinSuggestions(w http.ResponseWriter, r *http.Request, repo *repository.WarehouseRepository, task string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p warehouseopt.Point
	if !decodePosition(w, r, &p) {
		http.Error(w, "مختصات معتبر لازم است", 400)
		return
	}
	out, e := repo.SuggestBins(r.Context(), task, p)
	if e != nil {
		http.Error(w, e.Error(), 409)
		return
	}
	t01WriteJSON(w, 200, out)
}
func handlePickingRoute(w http.ResponseWriter, r *http.Request, repo *repository.WarehouseRepository) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var p struct {
		OrderIDs  []string           `json:"orderIds"`
		Warehouse string             `json:"warehouse"`
		Start     warehouseopt.Point `json:"start"`
	}
	if !decodePosition(w, r, &p) {
		http.Error(w, "Invalid route", 400)
		return
	}
	out, e := repo.OptimizePicking(r.Context(), p.OrderIDs, p.Warehouse, p.Start)
	if e != nil {
		http.Error(w, e.Error(), 409)
		return
	}
	t01WriteJSON(w, 200, out)
}
func handleBinPosition(w http.ResponseWriter, r *http.Request, repo *repository.WarehouseRepository) {
	if r.Method != http.MethodPut {
		writeMethodNotAllowed(w)
		return
	}
	var p warehouseopt.Point
	if !decodePosition(w, r, &p) {
		http.Error(w, "Invalid coordinates", 400)
		return
	}
	bin := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/bins/"), "/position")
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if e := repo.SetBinPosition(r.Context(), bin, p, claims.UserID); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	t01WriteJSON(w, 200, map[string]bool{"success": true})
}
func handleSupplierSLA(w http.ResponseWriter, r *http.Request, db *sql.DB, repo *repository.SupplierRepository) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	id := getSupplierIDFromContext(r, repo)
	var n, good int
	var score float64
	var eligible bool
	var assessed time.Time
	e := db.QueryRowContext(r.Context(), `SELECT samples,on_time,score,eligible,assessed_at FROM supplier_sla_metrics WHERE supplier_id=$1`, id).Scan(&n, &good, &score, &eligible, &assessed)
	if e != nil && e != sql.ErrNoRows {
		writeInternalError(w, e)
		return
	}
	t01WriteJSON(w, 200, map[string]interface{}{"samples": n, "onTime": good, "score": score, "eligible": eligible, "assessedAt": assessed, "windowDays": 90, "threshold": 98})
}
