package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Team 01 (Phase 2, live parity): public content, subscription plans,
// and platform constants (MASTER-AUDIT D01, D02, D03).
//
// Confirmed probe shapes (Phase-1 team report §3.1):
//   GET /banners            -> 200, banner list (position "hero" seen)
//   GET /homepage-sections  -> 200, sections[] with sectionType values
//     such as trust_features and secondary_slider
//   GET /site-content       -> 200, content.footer.about_text and friends
//   GET /static-pages       -> 200, pages[] with key values such as about
//   GET /marketing-settings -> 200, referralCommissionPercent 2,
//     trialCatalogQuota 25
//   GET /subscription-plans -> 200 [] (live plans are all inactive)
//   GET /public/return-window-days -> 200 {"days":7}
//   GET /platform/contracts -> 200 {sellerContractVersion: 3,
//     supplierContractVersion: 3}
// The banners wrapper key is NOT probe-confirmed (the probe only
// describes a list); {"banners": [...]} is used for consistency with
// the sections[]/pages[] wrappers and is marked TODO below.
// Site-content has a 5-minute cache in the live frontend; that cache is
// a frontend concern and is not implemented in this API.

// --- banners (D01) -------------------------------------------------------

// FIX 14: live banner shape, probed 2026-11-03: subtitle and linkUrl
// are ALWAYS present ("" when empty, never omitted), createdAt and
// updatedAt are present, and rows group by position (hero, promo,
// secondary, tile) with sort_order inside each position.
type t01Banner struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle"`
	ImageURL  string    `json:"imageUrl"`
	LinkURL   string    `json:"linkUrl"`
	Position  string    `json:"position"`
	SortOrder int       `json:"sortOrder"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// handleT01Banners serves GET /api/v1/banners (public, active only).
// TODO: the exact live wrapper key is unverified; {"banners": [...]} is
// used here. Optional ?position= filters by position.
func handleT01Banners(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	position := strings.TrimSpace(r.URL.Query().Get("position"))
	q := `SELECT id, title, subtitle, image_url, link_url, position, sort_order, is_active, created_at, updated_at
		FROM banners WHERE is_active = TRUE`
	args := []interface{}{}
	if position != "" {
		q += ` AND position = $1`
		args = append(args, position)
	}
	q += ` ORDER BY position ASC, sort_order ASC, id ASC`
	rows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	banners := []t01Banner{}
	for rows.Next() {
		var b t01Banner
		var subtitle, linkURL sql.NullString
		if err := rows.Scan(&b.ID, &b.Title, &subtitle, &b.ImageURL, &linkURL, &b.Position, &b.SortOrder, &b.IsActive, &b.CreatedAt, &b.UpdatedAt); err != nil {
			writeInternalError(w, err)
			return
		}
		b.Subtitle = subtitle.String
		b.LinkURL = linkURL.String
		banners = append(banners, b)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"banners": banners, "success": true})
}

// --- homepage sections (D01) ----------------------------------------------

type t01HomepageSection struct {
	ID          string          `json:"id"`
	SectionType string          `json:"sectionType"`
	Title       *string         `json:"title,omitempty"`
	Config      json.RawMessage `json:"config"`
	SortOrder   int             `json:"sortOrder"`
	IsActive    bool            `json:"isActive"`
}

func t01QueryHomepageSections(w http.ResponseWriter, r *http.Request, db *sql.DB, activeOnly bool) []t01HomepageSection {
	q := `SELECT id, section_type, title, config, sort_order, is_active FROM homepage_sections`
	if activeOnly {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY sort_order ASC, id ASC`
	rows, err := db.QueryContext(r.Context(), q)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	defer rows.Close()
	sections := []t01HomepageSection{}
	for rows.Next() {
		var s t01HomepageSection
		var cfg []byte
		if err := rows.Scan(&s.ID, &s.SectionType, &s.Title, &cfg, &s.SortOrder, &s.IsActive); err != nil {
			writeInternalError(w, err)
			return nil
		}
		// FIX 9: an empty-string title becomes nil, so omitempty omits
		// the title key exactly like live (live omits the key for its
		// title-less sections; it does not emit an empty string).
		if s.Title != nil && *s.Title == "" {
			s.Title = nil
		}
		if len(cfg) == 0 {
			cfg = []byte("{}")
		}
		s.Config = json.RawMessage(cfg)
		sections = append(sections, s)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return nil
	}
	return sections
}

// handleT01HomepageSections serves GET /api/v1/homepage-sections
// (public, active only) with the confirmed {"sections": [...]} shape.
func handleT01HomepageSections(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	sections := t01QueryHomepageSections(w, r, db, true)
	if sections == nil {
		return
	}
	// FIX 14: live adds success:true (probed 2026-11-03).
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"sections": sections, "success": true})
}

// --- site content (D01) ----------------------------------------------------

// t01SiteContentMap reads site_content rows and nests dotted keys
// (footer.about_text -> content.footer.about_text), parsing values by
// value_type: "json" is decoded, "boolean" becomes a bool, anything
// else stays a string.
func t01SiteContentMap(w http.ResponseWriter, r *http.Request, db *sql.DB) map[string]interface{} {
	rows, err := db.QueryContext(r.Context(),
		`SELECT key, value, value_type FROM site_content ORDER BY key`)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	defer rows.Close()
	content := map[string]interface{}{}
	for rows.Next() {
		var key, value, valueType string
		if err := rows.Scan(&key, &value, &valueType); err != nil {
			writeInternalError(w, err)
			return nil
		}
		var parsed interface{} = value
		switch valueType {
		case "json":
			var decoded interface{}
			if err := json.Unmarshal([]byte(value), &decoded); err == nil {
				parsed = decoded
			}
		case "boolean":
			parsed = value == "true" || value == "1"
		}
		parts := strings.Split(key, ".")
		node := content
		for i, p := range parts {
			if i == len(parts)-1 {
				node[p] = parsed
			} else {
				next, ok := node[p].(map[string]interface{})
				if !ok {
					next = map[string]interface{}{}
					node[p] = next
				}
				node = next
			}
		}
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return nil
	}
	return content
}

// handleT01SiteContent serves GET /api/v1/site-content (public).
// FIX 14: live serves FLAT dotted keys with STRING values inside
// content (footer.about_text, header.search_placeholders as a JSON
// string, trust_features as a JSON string) plus success:true --
// probed live 2026-11-03 (21 keys). The nested/parsed map stays for
// the admin handler only (t01SiteContentMap is untouched).
func handleT01SiteContent(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	rows, err := db.QueryContext(r.Context(),
		`SELECT key, value FROM site_content ORDER BY key`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer rows.Close()
	content := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			writeInternalError(w, err)
			return
		}
		content[key] = value
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"content": content, "success": true})
}

// --- static pages (D01) -----------------------------------------------------

type t01StaticPage struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	SortOrder int    `json:"sortOrder"`
	IsActive  bool   `json:"isActive"`
}

func t01QueryStaticPages(w http.ResponseWriter, r *http.Request, db *sql.DB, activeOnly bool) []t01StaticPage {
	q := `SELECT key, title, content, sort_order, is_active FROM static_pages`
	if activeOnly {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY sort_order ASC, key ASC`
	rows, err := db.QueryContext(r.Context(), q)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	defer rows.Close()
	pages := []t01StaticPage{}
	for rows.Next() {
		var p t01StaticPage
		if err := rows.Scan(&p.Key, &p.Title, &p.Content, &p.SortOrder, &p.IsActive); err != nil {
			writeInternalError(w, err)
			return nil
		}
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return nil
	}
	return pages
}

// handleT01StaticPages serves GET /api/v1/static-pages (public, active
// only) with the confirmed {"pages": [...]} shape (pages carry key).
func handleT01StaticPages(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	pages := t01QueryStaticPages(w, r, db, true)
	if pages == nil {
		return
	}
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"pages": pages, "success": true})
}

// --- marketing settings (D01) ----------------------------------------------

// handleT01MarketingSettings serves GET /api/v1/marketing-settings
// (public). FIX 14: live shape, probed 2026-11-03:
// {"settings":{"referralCommissionPercent":2,"referralIsEnabled":false,
// "trialCatalogQuota":25},"success":true}.
func handleT01MarketingSettings(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"settings": map[string]interface{}{
			"referralCommissionPercent": t01PlatformFloat(ctx, db, "referral_commission_percent", 2),
			"referralIsEnabled":         t01PlatformBool(ctx, db, "referral_is_enabled", false),
			"trialCatalogQuota":         t01PlatformInt(ctx, db, "trial_catalog_quota", 25),
		},
		"success": true,
	})
}

// --- subscription plans (D02) ------------------------------------------------

// NOTE (D02): plan seed data and prices are owned by the identity team;
// these handlers only read the subscription_plans table. The public
// probe returns 200 [] because every live plan row is inactive, so the
// public handler filters to is_active plans.
type t01SubscriptionPlan struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	DurationDays         int             `json:"durationDays"`
	DurationLabel        string          `json:"durationLabel"`
	Price                int64           `json:"price"`
	ProductLimit         int             `json:"productLimit"`
	AllowedCustomDomains int             `json:"allowedCustomDomains"`
	MaxStores            int             `json:"maxStores"`
	Features             json.RawMessage `json:"features"`
	SortOrder            int             `json:"sortOrder"`
	IsActive             bool            `json:"isActive"`
}

func t01QueryPlans(w http.ResponseWriter, r *http.Request, db *sql.DB, activeOnly bool) []t01SubscriptionPlan {
	q := `SELECT id, name, duration_days, duration_label, price, product_limit,
		allowed_custom_domains, max_stores, features, sort_order, is_active
		FROM subscription_plans`
	if activeOnly {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY sort_order ASC, id ASC`
	rows, err := db.QueryContext(r.Context(), q)
	if err != nil {
		writeInternalError(w, err)
		return nil
	}
	defer rows.Close()
	plans := []t01SubscriptionPlan{}
	for rows.Next() {
		var p t01SubscriptionPlan
		var features []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.DurationDays, &p.DurationLabel, &p.Price,
			&p.ProductLimit, &p.AllowedCustomDomains, &p.MaxStores, &features, &p.SortOrder, &p.IsActive); err != nil {
			writeInternalError(w, err)
			return nil
		}
		if len(features) == 0 {
			features = []byte("[]")
		}
		p.Features = json.RawMessage(features)
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, err)
		return nil
	}
	return plans
}

// handleT01PublicPlans serves GET /api/v1/subscription-plans (public).
// The live probe returns a bare [] array.
func handleT01PublicPlans(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	plans := t01QueryPlans(w, r, db, true)
	if plans == nil {
		return
	}
	t01WriteJSON(w, http.StatusOK, plans)
}

// handleT01AdminPlans serves GET /api/v1/admin/subscription-plans.
func handleT01AdminPlans(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	plans := t01QueryPlans(w, r, db, false)
	if plans == nil {
		return
	}
	t01WriteJSON(w, http.StatusOK, plans)
}

// --- platform constants (D03) ------------------------------------------------

// handleT01ReturnWindowDays serves GET /api/v1/public/return-window-days
// with the exact probe shape {"days":7}. The value is read from
// platform_settings.return_window_days (migration 202610310007) and
// falls back to 7 when the column or row is missing, matching the live
// handler behaviour documented in that migration.
func handleT01ReturnWindowDays(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	days := t01PlatformInt(r.Context(), db, "return_window_days", 7)
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{"days": days})
}

// handleT01PlatformContracts serves GET /api/v1/platform/contracts.
// FIX 14: live also returns the contract TEXTS (probed 2026-11-03:
// sellerContractText and supplierContractText, both "test" on live).
// Texts are read from platform_settings like the versions; a staging
// value that differs from live's is a data difference, not a shape gap.
func handleT01PlatformContracts(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	ctx := r.Context()
	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sellerContractText":      t01PlatformText(ctx, db, "seller_contract_text", "test"),
		"sellerContractVersion":   t01PlatformInt(ctx, db, "seller_contract_version", 3),
		"supplierContractText":    t01PlatformText(ctx, db, "supplier_contract_text", "test"),
		"supplierContractVersion": t01PlatformInt(ctx, db, "supplier_contract_version", 3),
	})
}
