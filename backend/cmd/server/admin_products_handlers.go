package main

import (
	"database/sql"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Fix69 (W-ROUTES) R3: admin-guarded /admin/products/ subtree.
//
// Live evidence (live bundles, /opt/drup/dist/assets, read-only):
//   index-MERGED.js: setProductBrand: PATCH
//     /admin/products/<id>/brand body {brand}
//   index-MERGED.js: setProductOriginality: PATCH
//     /admin/products/<id>/originality body {isOriginal}
//   AdminDashboardView-MERGED.js: GET /admin/products/<id>/pricing,
//     read only as V.supplierBaseCost; PUT /admin/products/<id>/pricing
//     body {clear: true} or {markupType, markupValue} where markupType
//     defaults to "percent" in the form state.
// No other /admin/products/<id>/* call exists in the live MERGED
// bundles (full scan of *-MERGED.js: only brand, originality,
// pricing, plus the fixed paths proposals, missing-measurements).
// Storage: products.brand (migration 202610200003),
// products.is_original (initial schema), products.supplier_base_cost
// (initial schema) and products.markup_type/markup_value (migration
// 202610190009, CHECK fixed|percent). The pricing GET/PUT response
// bodies are candidate shapes: the live client only reads
// supplierBaseCost on GET and only needs a 2xx on PUT, so nothing
// beyond that is claimed about live. The bare /admin/products path
// has no proven list handler here: Go mux 301-redirects it to this
// subtree (matching the live probe), and this handler answers 404
// for an empty sub-path. Existing more-specific registrations
// (proposals, approve/, missing-measurements, implicit-agreements)
// keep precedence in the mux and are untouched.

func wr69AdminProducts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/products/")
	rest = strings.TrimSuffix(rest, "/")
	idx := strings.LastIndex(rest, "/")
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	id, action := rest[:idx], rest[idx+1:]
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch action {
	case "brand":
		if !t01MethodOnly(w, r, http.MethodPatch) {
			return
		}
		wr69AdminProductBrand(w, r, db, id)
	case "originality":
		if !t01MethodOnly(w, r, http.MethodPatch) {
			return
		}
		wr69AdminProductOriginality(w, r, db, id)
	case "pricing":
		switch r.Method {
		case http.MethodGet:
			wr69AdminProductPricingGet(w, r, db, id)
		case http.MethodPut:
			wr69AdminProductPricingPut(w, r, db, id)
		default:
			writeMethodNotAllowed(w)
		}
	default:
		http.NotFound(w, r)
	}
}

func wr69AdminProductBrand(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req struct {
		Brand string `json:"brand"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	// Live client trims and caps at 100 characters; empty clears.
	brand := strings.TrimSpace(req.Brand)
	if utf8.RuneCountInString(brand) > 100 {
		http.Error(w, `{"error":"brand must be at most 100 characters"}`, http.StatusBadRequest)
		return
	}
	res, err := db.ExecContext(r.Context(), `UPDATE products SET brand = $1, updated_at = NOW() WHERE id = $2`, brand, id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "brand": brand})
}

func wr69AdminProductOriginality(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req struct {
		IsOriginal *bool `json:"isOriginal"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if req.IsOriginal == nil {
		http.Error(w, `{"error":"isOriginal is required"}`, http.StatusBadRequest)
		return
	}
	res, err := db.ExecContext(r.Context(), `UPDATE products SET is_original = $1, updated_at = NOW() WHERE id = $2`, *req.IsOriginal, id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "isOriginal": *req.IsOriginal})
}

func wr69AdminProductPricingGet(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var baseCost int64
	var markupType sql.NullString
	var markupValue sql.NullFloat64
	err := db.QueryRowContext(r.Context(), `SELECT supplier_base_cost, markup_type, markup_value FROM products WHERE id = $1`, id).Scan(&baseCost, &markupType, &markupValue)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// Per-product margin ceiling (ISO-C): separate table; a missing table
	// (pre-migration database) or missing row both read as null.
	var maxMarginPct interface{}
	var mm float64
	if err := db.QueryRowContext(r.Context(), `SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id = $1`, id).Scan(&mm); err == nil {
		maxMarginPct = mm
	}
	var mt interface{}
	if markupType.Valid {
		mt = markupType.String
	}
	var mv interface{}
	if markupValue.Valid {
		mv = markupValue.Float64
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id": id, "supplierBaseCost": baseCost, "markupType": mt, "markupValue": mv, "maxMarginPct": maxMarginPct,
	})
}

func wr69AdminProductPricingPut(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	var req struct {
		Clear          bool     `json:"clear"`
		MarkupType     string   `json:"markupType"`
		MarkupValue    *float64 `json:"markupValue"`
		MaxMarginPct   *float64 `json:"maxMarginPct"`
		ClearMaxMargin bool     `json:"clearMaxMargin"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	// Per-product margin ceiling (ISO-C margin model), independent of the
	// markup fields: maxMarginPct upserts a row, clearMaxMargin deletes
	// it (global MaxProfitMargin binds again). A ceiling-only request
	// (no markup fields) is valid and returns without touching markup.
	ceilingTouched := req.ClearMaxMargin || req.MaxMarginPct != nil
	if req.MaxMarginPct != nil && (math.IsNaN(*req.MaxMarginPct) || math.IsInf(*req.MaxMarginPct, 0) || *req.MaxMarginPct < 0 || *req.MaxMarginPct > 1000) {
		http.Error(w, `{"error":"maxMarginPct must be between 0 and 1000"}`, http.StatusBadRequest)
		return
	}
	if ceilingTouched {
		var prodExists bool
		if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, id).Scan(&prodExists); err != nil {
			writeInternalError(w, err)
			return
		}
		if !prodExists {
			http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
			return
		}
		if req.ClearMaxMargin {
			if _, err := db.ExecContext(r.Context(), `DELETE FROM product_margin_ceilings WHERE product_id = $1`, id); err != nil {
				writeInternalError(w, err)
				return
			}
		} else if req.MaxMarginPct != nil {
			if _, err := db.ExecContext(r.Context(), `INSERT INTO product_margin_ceilings (product_id, max_margin_pct, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (product_id) DO UPDATE SET max_margin_pct = EXCLUDED.max_margin_pct, updated_at = NOW()`, id, *req.MaxMarginPct); err != nil {
				writeInternalError(w, err)
				return
			}
		}
	}
	if req.Clear {
		res, err := db.ExecContext(r.Context(), `UPDATE products SET markup_type = NULL, markup_value = NULL, updated_at = NOW() WHERE id = $1`, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "cleared": true})
		return
	}
	if req.MarkupType == "" && req.MarkupValue == nil && !req.Clear && !ceilingTouched {
		http.Error(w, `{"error":"nothing to update"}`, http.StatusBadRequest)
		return
	}
	if req.MarkupType == "" && req.MarkupValue != nil {
		http.Error(w, `{"error":"markupType must be fixed or percent"}`, http.StatusBadRequest)
		return
	}
	if req.MarkupType != "" && req.MarkupType != "fixed" && req.MarkupType != "percent" {
		http.Error(w, `{"error":"markupType must be fixed or percent"}`, http.StatusBadRequest)
		return
	}
	if req.MarkupValue != nil && *req.MarkupValue < 0 {
		http.Error(w, `{"error":"markupValue must be >= 0"}`, http.StatusBadRequest)
		return
	}
	if req.MarkupType == "" {
		// Ceiling-only request: markup untouched.
		var mmOut interface{}
		if req.MaxMarginPct != nil {
			mmOut = *req.MaxMarginPct
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "maxMarginPct": mmOut, "clearedMaxMargin": req.ClearMaxMargin})
		return
	}
	var mv interface{}
	if req.MarkupValue != nil {
		mv = *req.MarkupValue
	}
	res, err := db.ExecContext(r.Context(), `UPDATE products SET markup_type = $1, markup_value = $2, updated_at = NOW() WHERE id = $3`, req.MarkupType, mv, id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "markupType": req.MarkupType, "markupValue": mv})
}
