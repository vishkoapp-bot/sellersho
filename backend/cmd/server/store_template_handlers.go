package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// Team 01 (Phase 2, live parity): store template system (MASTER-AUDIT
// D04, backend part only; frontend template components belong to
// T05/T08).
//
// Live templates (probe + migrations 202610160010, 202610190008,
// 202610190010, 202610190011, 202610190012): tpl-modern-1, tpl-tech-1,
// tpl-electronic-1, tpl-fashion-1, tpl-digikala-1. The model is
// templateId + templateConfig on stores (NOT the source selectedTheme
// model, which must not be mapped onto these ids).
// TODO: the live template registry also carries section presets and
// colour presets (frontend evidence); the store_templates table in
// this tree carries only id/name/description/thumbnail, so presets
// are not returned here until their storage is confirmed.

// handleT01StoreTemplates serves GET /api/v1/store-templates (public).
func handleT01StoreTemplates(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	// FIX 14: live serves a DIRECT array (no {"templates":...} wrapper)
	// in the order modern, electronic, fashion, digikala, tech (probed
	// live 2026-11-03). The table has no sort column, so the live order
	// is expressed as a CASE; unknown future templates sort last by id.
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, name, description, thumbnail_url, is_active
		FROM store_templates WHERE is_active = TRUE
		ORDER BY CASE id
			WHEN 'tpl-modern-1' THEN 0
			WHEN 'tpl-electronic-1' THEN 1
			WHEN 'tpl-fashion-1' THEN 2
			WHEN 'tpl-digikala-1' THEN 3
			WHEN 'tpl-tech-1' THEN 4
			ELSE 5 END, id ASC`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	// Field order matches live's alphabetical object keys.
	type template struct {
		Description  string `json:"description"`
		ID           string `json:"id"`
		IsActive     bool   `json:"isActive"`
		Name         string `json:"name"`
		ThumbnailURL string `json:"thumbnailUrl"`
	}
	templates := []template{}
	for rows.Next() {
		var t template
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.ThumbnailURL, &t.IsActive); err != nil {
			writeInternalError(w, err)
			return
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, templates)
}

// t01SellerStoreID resolves the caller's store: an explicit ?storeId=
// or ?slug= wins, otherwise the caller's first owned store.
func t01SellerStoreID(w http.ResponseWriter, r *http.Request, db *sql.DB, userID string) (string, bool) {
	ctx := r.Context()
	if id := strings.TrimSpace(r.URL.Query().Get("storeId")); id != "" {
		var found string
		if err := db.QueryRowContext(ctx,
			`SELECT id FROM stores WHERE id = $1 AND owner_user_id = $2`, id, userID).Scan(&found); err == nil {
			return found, true
		}
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return "", false
	}
	if slug := strings.TrimSpace(r.URL.Query().Get("slug")); slug != "" {
		var found string
		if err := db.QueryRowContext(ctx,
			`SELECT id FROM stores WHERE slug = $1 AND owner_user_id = $2`, slug, userID).Scan(&found); err == nil {
			return found, true
		}
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return "", false
	}
	ids := t01SellerStoreIDs(ctx, db, userID)
	if len(ids) == 0 {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return "", false
	}
	return ids[0], true
}

// handleT01SellerStoreTemplate serves GET/PUT /api/v1/seller/store/template.
func handleT01SellerStoreTemplate(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	storeID, ok := t01SellerStoreID(w, r, db, userID)
	if !ok {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		var templateID sql.NullString
		var cfg []byte
		if err := db.QueryRowContext(ctx,
			`SELECT template_id, template_config FROM stores WHERE id = $1`, storeID).Scan(&templateID, &cfg); err != nil {
			writeInternalError(w, err)
			return
		}
		if len(cfg) == 0 {
			cfg = []byte("{}")
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"storeId":        storeID,
			"templateId":     templateID.String,
			"templateConfig": json.RawMessage(cfg),
		})
	case http.MethodPut:
		var req struct {
			TemplateID     string          `json:"templateId"`
			TemplateConfig json.RawMessage `json:"templateConfig"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.TemplateID) == "" {
			http.Error(w, `{"error":"templateId is required"}`, http.StatusBadRequest)
			return
		}
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM store_templates WHERE id = $1 AND is_active = TRUE)`, req.TemplateID).Scan(&exists); err != nil || !exists {
			if err != nil {
				writeInternalError(w, err)
				return
			}
			http.Error(w, `{"error":"Unknown template"}`, http.StatusBadRequest)
			return
		}
		cfg := req.TemplateConfig
		if len(cfg) == 0 {
			cfg = []byte("{}")
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE stores SET template_id = $1, template_config = $2::jsonb WHERE id = $3`,
			req.TemplateID, string(cfg), storeID); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"storeId":        storeID,
			"templateId":     req.TemplateID,
			"templateConfig": json.RawMessage(cfg),
		})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01SellerStoreTemplatePublish serves POST
// /api/v1/seller/store/template/publish. It persists the given (or
// currently selected) template as the store's live template.
// TODO: no publish-state column exists in the migrations, so the exact
// live publish semantics (draft vs published) are unverified; publish
// here is the persist step, not a separate state transition.
func handleT01SellerStoreTemplatePublish(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	storeID, ok := t01SellerStoreID(w, r, db, userID)
	if !ok {
		return
	}
	ctx := r.Context()
	var req struct {
		TemplateID     string          `json:"templateId"`
		TemplateConfig json.RawMessage `json:"templateConfig"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	templateID := strings.TrimSpace(req.TemplateID)
	cfg := req.TemplateConfig
	if templateID == "" {
		var current sql.NullString
		var curCfg []byte
		if err := db.QueryRowContext(ctx,
			`SELECT template_id, template_config FROM stores WHERE id = $1`, storeID).Scan(&current, &curCfg); err != nil {
			writeInternalError(w, err)
			return
		}
		templateID = current.String
		cfg = curCfg
	}
	if templateID == "" {
		http.Error(w, `{"error":"templateId is required"}`, http.StatusBadRequest)
		return
	}
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE stores SET template_id = $1, template_config = $2::jsonb WHERE id = $3`,
		templateID, string(cfg), storeID); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"published":  true,
		"storeId":    storeID,
		"templateId": templateID,
	})
}
