package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Team 01 (Phase 2, live parity): admin operational APIs (MASTER-AUDIT
// D15, excluding supplier parts already done by Team 06:
// admin/suppliers/list|records|shipping-methods and admin/store-intel/
// suppliers are NOT duplicated here).
//
// Staff and jobs are priority (they block admin completeness): jobs
// lists job_runs (migration 202610310008 calls it "persisted history
// for the admin jobs tab"); staff has no handler in the extracted
// live symbols, so it is registered with the confirmed path/role and
// a TODO U54 stub, never a guessed implementation.
// Money-out summaries, where present, count only
// completed/transfer_confirmed payout_requests (Team 02, D32).

// --- banners (D15) ----------------------------------------------------------

func handleT01AdminBanners(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, title, subtitle, image_url, link_url, position, sort_order, is_active, starts_at, ends_at
			FROM banners ORDER BY sort_order ASC, id ASC`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, title, imageURL, position string
			var subtitle, linkURL sql.NullString
			var sortOrder int
			var isActive bool
			var starts, ends sql.NullTime
			if err := rows.Scan(&id, &title, &subtitle, &imageURL, &linkURL, &position, &sortOrder, &isActive, &starts, &ends); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "title": title, "subtitle": subtitle.String, "imageUrl": imageURL,
				"linkUrl": linkURL.String, "position": position, "sortOrder": sortOrder,
				"isActive": isActive, "startsAt": t01NullTime(starts), "endsAt": t01NullTime(ends),
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"banners": out})
	case http.MethodPost:
		var req struct {
			Title     string `json:"title"`
			Subtitle  string `json:"subtitle"`
			ImageURL  string `json:"imageUrl"`
			LinkURL   string `json:"linkUrl"`
			Position  string `json:"position"`
			SortOrder int    `json:"sortOrder"`
			IsActive  *bool  `json:"isActive"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.ImageURL) == "" {
			http.Error(w, `{"error":"title and imageUrl are required"}`, http.StatusBadRequest)
			return
		}
		if req.Position == "" {
			req.Position = "hero"
		}
		active := true
		if req.IsActive != nil {
			active = *req.IsActive
		}
		id := uuid.NewString()
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO banners (id, title, subtitle, image_url, link_url, position, sort_order, is_active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, req.Title, nullIfEmpty(req.Subtitle), req.ImageURL, nullIfEmpty(req.LinkURL),
			req.Position, req.SortOrder, active); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

func t01NullTime(t sql.NullTime) interface{} {
	if !t.Valid {
		return nil
	}
	return t.Time.Format(time.RFC3339)
}

func handleT01AdminBannerByID(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		var title, imageURL, position string
		var subtitle, linkURL sql.NullString
		var sortOrder int
		var isActive bool
		err := db.QueryRowContext(ctx,
			`SELECT title, subtitle, image_url, link_url, position, sort_order, is_active
			FROM banners WHERE id = $1`, id).Scan(&title, &subtitle, &imageURL, &linkURL, &position, &sortOrder, &isActive)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Banner not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"id": id, "title": title, "subtitle": subtitle.String, "imageUrl": imageURL,
			"linkUrl": linkURL.String, "position": position, "sortOrder": sortOrder, "isActive": isActive,
		})
	case http.MethodPut, http.MethodPatch:
		var req map[string]json.RawMessage
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		// Partial update over the allowlisted banner columns.
		cols := map[string]string{
			"title": "title", "subtitle": "subtitle", "imageUrl": "image_url",
			"linkUrl": "link_url", "position": "position", "sortOrder": "sort_order", "isActive": "is_active",
		}
		for k, raw := range req {
			col, ok := cols[k]
			if !ok {
				http.Error(w, `{"error":"Unknown banner field"}`, http.StatusBadRequest)
				return
			}
			var v interface{}
			if err := json.Unmarshal(raw, &v); err != nil {
				http.Error(w, `{"error":"Invalid banner field"}`, http.StatusBadRequest)
				return
			}
			if _, err := db.ExecContext(ctx,
				"UPDATE banners SET "+col+" = $1, updated_at = NOW() WHERE id = $2", v, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
	case http.MethodDelete:
		res, err := db.ExecContext(ctx, `DELETE FROM banners WHERE id = $1`, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, `{"error":"Banner not found"}`, http.StatusNotFound)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminBannersSyncSections serves POST
// /api/v1/admin/banners/sync-sections.
// TODO: the live sync semantics (which sections are regenerated from
// banners) are unverified, so this route is registered with the
// confirmed path/role and a stub, never a guessed regeneration.
func handleT01AdminBannersSyncSections(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	t01Stub(w, r, "D15", "Banner-to-section sync semantics are unverified; no sections are modified by this stub.")
}

// --- homepage sections (D15) -------------------------------------------------

func handleT01AdminHomepageSections(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		sections := t01QueryHomepageSections(w, r, db, false)
		if sections == nil {
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"sections": sections})
	case http.MethodPost:
		var req struct {
			SectionType string          `json:"sectionType"`
			Title       string          `json:"title"`
			Config      json.RawMessage `json:"config"`
			SortOrder   int             `json:"sortOrder"`
			IsActive    *bool           `json:"isActive"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.SectionType) == "" {
			http.Error(w, `{"error":"sectionType is required"}`, http.StatusBadRequest)
			return
		}
		active := true
		if req.IsActive != nil {
			active = *req.IsActive
		}
		cfg := req.Config
		if len(cfg) == 0 {
			cfg = []byte("{}")
		}
		id := uuid.NewString()
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO homepage_sections (id, section_type, title, config, sort_order, is_active)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)`,
			id, req.SectionType, nullIfEmpty(req.Title), string(cfg), req.SortOrder, active); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminHomepageSectionByID serves PUT/DELETE
// /api/v1/admin/homepage-sections/<id>.
func handleT01AdminHomepageSectionByID(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		var req struct {
			SectionType string          `json:"sectionType"`
			Title       *string         `json:"title"`
			Config      json.RawMessage `json:"config"`
			SortOrder   *int            `json:"sortOrder"`
			IsActive    *bool           `json:"isActive"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if req.SectionType != "" {
			if _, err := db.ExecContext(ctx, `UPDATE homepage_sections SET section_type = $1, updated_at = NOW() WHERE id = $2`, req.SectionType, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.Title != nil {
			if _, err := db.ExecContext(ctx, `UPDATE homepage_sections SET title = $1, updated_at = NOW() WHERE id = $2`, *req.Title, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if len(req.Config) > 0 {
			if _, err := db.ExecContext(ctx, `UPDATE homepage_sections SET config = $1::jsonb, updated_at = NOW() WHERE id = $2`, string(req.Config), id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.SortOrder != nil {
			if _, err := db.ExecContext(ctx, `UPDATE homepage_sections SET sort_order = $1, updated_at = NOW() WHERE id = $2`, *req.SortOrder, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.IsActive != nil {
			if _, err := db.ExecContext(ctx, `UPDATE homepage_sections SET is_active = $1, updated_at = NOW() WHERE id = $2`, *req.IsActive, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
	case http.MethodDelete:
		res, err := db.ExecContext(ctx, `DELETE FROM homepage_sections WHERE id = $1`, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, `{"error":"Section not found"}`, http.StatusNotFound)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleT01AdminHomepageSectionsReorder serves PUT
// /api/v1/admin/homepage-sections/reorder. It accepts either
// {"order": [<id>, ...]} (positions assigned in array order) or
// {"sections": [{"id": ..., "sortOrder": ...}]}.
// TODO: the exact live reorder body is unverified; both shapes above
// are accepted so neither plausible caller breaks.
func handleT01AdminHomepageSectionsReorder(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodPut, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var body struct {
		Order    []string `json:"order"`
		Sections []struct {
			ID        string `json:"id"`
			SortOrder int    `json:"sortOrder"`
		} `json:"sections"`
	}
	if !t01DecodeJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	if len(body.Order) > 0 {
		for i, id := range body.Order {
			if _, err := db.ExecContext(ctx,
				`UPDATE homepage_sections SET sort_order = $1, updated_at = NOW() WHERE id = $2`, (i+1)*10, id); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": len(body.Order)})
		return
	}
	if len(body.Sections) > 0 {
		for _, s := range body.Sections {
			if _, err := db.ExecContext(ctx,
				`UPDATE homepage_sections SET sort_order = $1, updated_at = NOW() WHERE id = $2`, s.SortOrder, s.ID); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": len(body.Sections)})
		return
	}
	http.Error(w, `{"error":"order or sections is required"}`, http.StatusBadRequest)
}

// handleT01AdminHomepageSectionsHistory serves GET
// /api/v1/admin/homepage-sections/history.
// TODO: no section-history table exists in the migration chain, so
// this route is registered with the confirmed path/role and a stub.
func handleT01AdminHomepageSectionsHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	t01Stub(w, r, "D15", "No homepage-section history table exists in the migrations; history storage is unverified.")
}

// --- site content / static pages (D15) -----------------------------------------

// t01FlattenContent flattens a nested content map back to dotted keys.
func t01FlattenContent(prefix string, node map[string]interface{}, out map[string]string) {
	for k, v := range node {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch child := v.(type) {
		case map[string]interface{}:
			t01FlattenContent(key, child, out)
		case string:
			out[key] = child
		default:
			encoded, _ := json.Marshal(v)
			out[key] = string(encoded)
		}
	}
}

func handleT01AdminSiteContent(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		content := t01SiteContentMap(w, r, db)
		if content == nil {
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"content": content})
	case http.MethodPut, http.MethodPatch:
		var body struct {
			Content map[string]interface{} `json:"content"`
			Key     string                 `json:"key"`
			Value   string                 `json:"value"`
			Type    string                 `json:"valueType"`
		}
		if !t01DecodeJSON(w, r, &body) {
			return
		}
		flat := map[string]string{}
		if body.Content != nil {
			t01FlattenContent("", body.Content, flat)
		} else if body.Key != "" {
			flat[body.Key] = body.Value
		} else {
			http.Error(w, `{"error":"content or key+value is required"}`, http.StatusBadRequest)
			return
		}
		for k, v := range flat {
			valueType := "text"
			if body.Key != "" && body.Type != "" {
				valueType = body.Type
			} else if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") {
				valueType = "json"
			} else if v == "true" || v == "false" {
				// Keep the stored type for known boolean keys; new
				// keys default to text unless the caller says otherwise.
				if _, err := db.ExecContext(r.Context(),
					`INSERT INTO site_content (key, value, value_type) VALUES ($1, $2, 'boolean')
					ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, k, v); err != nil {
					writeInternalError(w, err)
					return
				}
				continue
			}
			if _, err := db.ExecContext(r.Context(),
				`INSERT INTO site_content (key, value, value_type) VALUES ($1, $2, $3)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`, k, v, valueType); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "updated": len(flat)})
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminStaticPages(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		pages := t01QueryStaticPages(w, r, db, false)
		if pages == nil {
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"pages": pages})
	case http.MethodPost:
		var req struct {
			Key       string `json:"key"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			SortOrder int    `json:"sortOrder"`
			IsActive  *bool  `json:"isActive"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Key) == "" || strings.TrimSpace(req.Title) == "" {
			http.Error(w, `{"error":"key and title are required"}`, http.StatusBadRequest)
			return
		}
		active := true
		if req.IsActive != nil {
			active = *req.IsActive
		}
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO static_pages (key, title, content, sort_order, is_active)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (key) DO UPDATE SET title = EXCLUDED.title, content = EXCLUDED.content,
				sort_order = EXCLUDED.sort_order, is_active = EXCLUDED.is_active, updated_at = NOW()`,
			req.Key, req.Title, req.Content, req.SortOrder, active); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "key": req.Key})
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminStaticPageByKey(w http.ResponseWriter, r *http.Request, db *sql.DB, key string) {
	if t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		var title, content string
		var sortOrder int
		var isActive bool
		err := db.QueryRowContext(ctx,
			`SELECT title, content, sort_order, is_active FROM static_pages WHERE key = $1`, key).Scan(&title, &content, &sortOrder, &isActive)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Page not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"key": key, "title": title, "content": content, "sortOrder": sortOrder, "isActive": isActive,
		})
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Title     *string `json:"title"`
			Content   *string `json:"content"`
			SortOrder *int    `json:"sortOrder"`
			IsActive  *bool   `json:"isActive"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if req.Title != nil {
			if _, err := db.ExecContext(ctx, `UPDATE static_pages SET title = $1, updated_at = NOW() WHERE key = $2`, *req.Title, key); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.Content != nil {
			if _, err := db.ExecContext(ctx, `UPDATE static_pages SET content = $1, updated_at = NOW() WHERE key = $2`, *req.Content, key); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.SortOrder != nil {
			if _, err := db.ExecContext(ctx, `UPDATE static_pages SET sort_order = $1, updated_at = NOW() WHERE key = $2`, *req.SortOrder, key); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		if req.IsActive != nil {
			if _, err := db.ExecContext(ctx, `UPDATE static_pages SET is_active = $1, updated_at = NOW() WHERE key = $2`, *req.IsActive, key); err != nil {
				writeInternalError(w, err)
				return
			}
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "key": key})
	case http.MethodDelete:
		res, err := db.ExecContext(ctx, `DELETE FROM static_pages WHERE key = $1`, key)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, `{"error":"Page not found"}`, http.StatusNotFound)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "key": key})
	default:
		writeMethodNotAllowed(w)
	}
}

// --- generic admin lists (D15) ---------------------------------------------------
// These lists return real table rows via t01ListRows; the exact live
// response envelope for each is unverified (TODO at each call site in
// main.go registration comments and in the final report).

func handleT01AdminGenericList(w http.ResponseWriter, r *http.Request, db *sql.DB, table, key, orderBy string) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, _, err := t01ListRows(r.Context(), db, table, orderBy, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{key: rows})
}

// --- categories / warehouses (D15) -------------------------------------------------

func handleT01AdminCategories(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleT01AdminGenericList(w, r, db, "categories", "categories", "ORDER BY id ASC")
	case http.MethodPost:
		var req struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Slug     string `json:"slug"`
			IconName string `json:"iconName"`
			Gradient string `json:"gradient"`
			ImageURL string `json:"imageUrl"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Slug) == "" {
			http.Error(w, `{"error":"title and slug are required"}`, http.StatusBadRequest)
			return
		}
		id := req.ID
		if id == "" {
			id = "cat-" + uuid.NewString()
		}
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO categories (id, title, slug, icon_name, gradient, image_url)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, slug = EXCLUDED.slug,
				icon_name = EXCLUDED.icon_name, gradient = EXCLUDED.gradient, image_url = EXCLUDED.image_url`,
			id, req.Title, req.Slug, req.IconName, nullIfEmpty(req.Gradient), nullIfEmpty(req.ImageURL)); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminWarehouses(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, name, tapin_province_id, tapin_city_id, province_name, city_name, priority, is_active
			FROM warehouses ORDER BY priority ASC, id ASC`)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, name, provinceName, cityName string
			var tapinProvince, tapinCity, priority int
			var isActive bool
			if err := rows.Scan(&id, &name, &tapinProvince, &tapinCity, &provinceName, &cityName, &priority, &isActive); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "name": name, "tapinProvinceId": tapinProvince, "tapinCityId": tapinCity,
				"provinceName": provinceName, "cityName": cityName, "priority": priority, "isActive": isActive,
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"warehouses": out})
	case http.MethodPost:
		var req struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			TapinProvince int    `json:"tapinProvinceId"`
			TapinCity     int    `json:"tapinCityId"`
			ProvinceName  string `json:"provinceName"`
			CityName      string `json:"cityName"`
			Priority      int    `json:"priority"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		id := req.ID
		if id == "" {
			id = "wh-" + uuid.NewString()
		}
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO warehouses (id, name, tapin_province_id, tapin_city_id, province_name, city_name, priority)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, tapin_province_id = EXCLUDED.tapin_province_id,
				tapin_city_id = EXCLUDED.tapin_city_id, province_name = EXCLUDED.province_name,
				city_name = EXCLUDED.city_name, priority = EXCLUDED.priority`,
			id, req.Name, req.TapinProvince, req.TapinCity, req.ProvinceName, req.CityName, req.Priority); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// --- tickets / disputes / returns / reviews (D15) ----------------------------------

func handleT01AdminTicketReply(w http.ResponseWriter, r *http.Request, db *sql.DB, ticketID string) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Body       string `json:"body"`
		IsInternal bool   `json:"isInternal"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		http.Error(w, `{"error":"body is required"}`, http.StatusBadRequest)
		return
	}
	author := t01ClaimsUserID(r)
	if _, err := db.ExecContext(r.Context(),
		`INSERT INTO support_ticket_messages (ticket_id, author_user_id, body, is_internal)
		VALUES ($1, $2::uuid, $3, $4)`, ticketID, author, req.Body, req.IsInternal); err != nil {
		writeInternalError(w, err)
		return
	}
	if _, err := db.ExecContext(r.Context(),
		`UPDATE support_tickets SET updated_at = NOW() WHERE id = $1`, ticketID); err != nil {
		writeInternalError(w, err)
		return
	}
	// P1-11 (best-effort, writer only): ticket reply - the requester
	// when an admin replied, admins when the requester replied.
	// Internal notes notify nobody. (The false SMS copy is W3's.)
	repository.NotifyTicketReply(r.Context(), db, ticketID, author, req.IsInternal, req.Body)
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "ticketId": ticketID})
}

func handleT01AdminReviewByID(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if !t01MethodOnly(w, r, http.MethodPut, http.MethodPatch) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Status) == "" {
		http.Error(w, `{"error":"status is required"}`, http.StatusBadRequest)
		return
	}
	res, err := db.ExecContext(r.Context(),
		`UPDATE product_reviews SET status = $1 WHERE id = $2`, req.Status, id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, `{"error":"Review not found"}`, http.StatusNotFound)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "status": req.Status})
}

// --- payouts / payments / customer withdrawals (D15) ---------------------------------

func handleT01AdminPayouts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status,
			COALESCE(available_balance, 0), COALESCE(rejection_reason, ''), created_at, processed_at,
			transfer_reference, transfer_confirmed_by, transfer_confirmed_at
		FROM payout_requests ORDER BY created_at DESC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, storeID, recipientType, recipientName, shaba, bankName, status, rejection string
		var amount, available int64
		var created time.Time
		var processed sql.NullTime
		var transferRef, transferBy sql.NullString
		var transferAt sql.NullTime
		if err := rows.Scan(&id, &storeID, &recipientType, &recipientName, &shaba, &bankName, &amount, &status,
			&available, &rejection, &created, &processed, &transferRef, &transferBy, &transferAt); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "storeId": storeID, "recipientType": recipientType, "recipientName": recipientName,
			"shaba": shaba, "bankName": bankName, "amount": amount, "status": status,
			"availableBalance": available, "rejectionReason": rejection,
			"createdAt": created.Format(time.RFC3339), "processedAt": t01NullTime(processed),
			"transferReference": transferRef.String, "transferConfirmedBy": transferBy.String,
			"transferConfirmedAt": t01NullTime(transferAt),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	// NOTE (D32): callers must not treat "approved" as paid; only
	// completed/transfer_confirmed count as money out.
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"payouts": out})
}

func handleT01AdminCustomerWithdrawals(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	// Preserve the existing review/transfer wire fields, but never expose
	// retry identities or future storage-only columns.
	rows, err := db.QueryContext(r.Context(), `
		SELECT id, customer_id, customer_name, customer_mobile, account_holder_name,
		       sheba, bank_name, amount, status, rejection_reason, reviewed_by, reviewed_at,
		       transfer_reference, transfer_date, transfer_confirmed_by, transfer_confirmed_at,
		       created_at, updated_at
		FROM customer_withdrawal_requests ORDER BY created_at DESC, id DESC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	fields, err := rows.Columns()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(fields))
		dest := make([]any, len(fields))
		for i := range dest {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			writeInternalError(w, err)
			return
		}
		item := make(map[string]any, len(fields))
		for i, field := range fields {
			switch value := values[i].(type) {
			case []byte:
				item[field] = string(value)
			case time.Time:
				item[field] = value.Format(time.RFC3339)
			default:
				item[field] = value
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]any{"withdrawals": out})
}

// handleT01AdminCustomerWithdrawalReview serves POST
// /api/v1/admin/customer-withdrawals/<id>/review with
// {status: "approved"|"rejected", reason?}. Per migration
// 202610200002 §3.4, holds live in customer_wallets.locked_balance
// and NO journal exists until the paid transition: approval therefore
// writes no ledger entry, and rejection releases the lock.
func handleT01AdminCustomerWithdrawalReview(w http.ResponseWriter, r *http.Request, db *sql.DB, id string) {
	if !t01MethodOnly(w, r, http.MethodPost) || t01DBNil(w, db) {
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !t01DecodeJSON(w, r, &req) {
		return
	}
	if req.Status != "approved" && req.Status != "rejected" {
		http.Error(w, `{"error":"status must be approved or rejected"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	var customerID string
	var amount int64
	var oldStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT customer_id, amount, status FROM customer_withdrawal_requests WHERE id = $1 FOR UPDATE`,
		id).Scan(&customerID, &amount, &oldStatus)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"Withdrawal not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if oldStatus != "pending" {
		http.Error(w, `{"error":"Withdrawal is not pending"}`, http.StatusConflict)
		return
	}
	reviewer := t01ClaimsUserID(r)
	if req.Status == "rejected" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE customer_withdrawal_requests SET status = 'rejected', rejection_reason = $1,
				reviewed_by = $2, reviewed_at = NOW(), updated_at = NOW() WHERE id = $3`,
			nullIfEmpty(req.Reason), reviewer, id); err != nil {
			writeInternalError(w, err)
			return
		}
		var released int64
		err := tx.QueryRowContext(ctx,
			`UPDATE customer_wallets SET locked_balance = locked_balance - $1
			 WHERE customer_id = $2::uuid AND locked_balance >= $1 RETURNING locked_balance`, amount, customerID).Scan(&released)
		if err == sql.ErrNoRows {
			http.Error(w, `{"error":"Withdrawal hold is missing or insufficient"}`, http.StatusConflict)
			return
		}
		if err != nil {
			writeInternalError(w, err)
			return
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE customer_withdrawal_requests SET status = 'approved',
				reviewed_by = $1, reviewed_at = NOW(), updated_at = NOW() WHERE id = $2`,
			reviewer, id); err != nil {
			writeInternalError(w, err)
			return
		}
		// TODO: the paid transition (transfer confirmation + ledger
		// debit) is a separate step; approval alone writes no journal
		// (migration 202610200002 §3.4, D32).
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id, "status": req.Status})
}

// --- newsletter subscribers / supplier promotions / products (D15) ------------------

func handleT01AdminNewsletterSubscribers(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	q := `SELECT id, store_id, mobile, email, status, source, created_at, unsubscribed_at
		FROM newsletter_subscribers WHERE 1 = 1`
	args := []interface{}{}
	if storeID := strings.TrimSpace(r.URL.Query().Get("storeId")); storeID != "" {
		args = append(args, storeID)
		q += ` AND store_id = $` + strconv.Itoa(len(args))
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		args = append(args, status)
		q += ` AND status = $` + strconv.Itoa(len(args))
	}
	q += ` ORDER BY created_at DESC LIMIT ` + strconv.Itoa(t01Limit(r))
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, storeID, mobile, status, source string
		var email sql.NullString
		var created time.Time
		var unsub sql.NullTime
		if err := rows.Scan(&id, &storeID, &mobile, &email, &status, &source, &created, &unsub); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "storeId": storeID, "mobile": mobile, "email": email.String,
			"status": status, "source": source, "createdAt": created.Format(time.RFC3339),
			"unsubscribedAt": t01NullTime(unsub),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"subscribers": out})
}

func handleT01AdminSupplierPromotions(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if t01DBNil(w, db) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, title, supplier_id, discount_type, discount_value, valid_until, status, created_at
			FROM supplier_promotions ORDER BY created_at DESC LIMIT $1`, t01Limit(r))
		if err != nil {
			writeInternalError(w, err)
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, title, supplierID, discountType, validUntil, status string
			var discountValue int64
			var created time.Time
			if err := rows.Scan(&id, &title, &supplierID, &discountType, &discountValue, &validUntil, &status, &created); err != nil {
				writeInternalError(w, err)
				return
			}
			out = append(out, map[string]interface{}{
				"id": id, "title": title, "supplierId": supplierID, "discountType": discountType,
				"discountValue": discountValue, "validUntil": validUntil, "status": status,
				"createdAt": created.Format(time.RFC3339),
			})
		}
		if err := rows.Err(); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{"promotions": out})
	case http.MethodPost:
		var req struct {
			Title         string `json:"title"`
			SupplierID    string `json:"supplierId"`
			DiscountType  string `json:"discountType"`
			DiscountValue int64  `json:"discountValue"`
			ValidUntil    string `json:"validUntil"`
		}
		if !t01DecodeJSON(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Title) == "" || (req.DiscountType != "platform_fee_discount" && req.DiscountType != "warehouse_free_storage") {
			http.Error(w, `{"error":"title and a valid discountType are required"}`, http.StatusBadRequest)
			return
		}
		supplierID := req.SupplierID
		if supplierID == "" {
			supplierID = "all"
		}
		id := "promo-" + uuid.NewString()
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO supplier_promotions (id, title, supplier_id, discount_type, discount_value, valid_until)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			id, req.Title, supplierID, req.DiscountType, req.DiscountValue, req.ValidUntil); err != nil {
			writeInternalError(w, err)
			return
		}
		t01WriteJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

func handleT01AdminProductsMissingMeasurements(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, title, length_mm, width_mm, height_mm, weight_grams, measurement_status
		FROM products
		WHERE length_mm IS NULL OR width_mm IS NULL OR height_mm IS NULL OR weight_grams IS NULL
		ORDER BY id ASC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, title, status string
		var length, width, height, weight sql.NullInt64
		if err := rows.Scan(&id, &title, &length, &width, &height, &weight, &status); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "title": title,
			"lengthMm": t01NullInt(length), "widthMm": t01NullInt(width),
			"heightMm": t01NullInt(height), "weightGrams": t01NullInt(weight),
			"measurementStatus": status,
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"products": out})
}

func t01NullInt(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func handleT01AdminProductsImplicitAgreements(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	// Same predicate as Team 06's supplier-side query
	// (ListSupplierImplicitAgreementProducts), without a supplier scope.
	rows, err := db.QueryContext(r.Context(),
		`SELECT id, title, supplier_id, implicit_agreement_until FROM products
		WHERE implicit_agreement = TRUE ORDER BY title ASC LIMIT $1`, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, title, supplierID string
		var until sql.NullTime
		if err := rows.Scan(&id, &title, &supplierID, &until); err != nil {
			writeInternalError(w, err)
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "title": title, "supplierId": supplierID,
			"implicitAgreementUntil": t01NullTime(until),
		})
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"products": out})
}
