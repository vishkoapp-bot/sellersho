package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// Team 05 (Phase 2, live parity): stores / dropshippers, backend part
// (MASTER-AUDIT D45, D47, D48, D52, D04-backend-check).
//
// Scope notes, all deliberate:
//   - D52 (as amended by FIX3, step-zero decision): the PUBLIC store
//     response is the 12 live keys PLUS 3 public storefront fields:
//     templateId, templateConfig (sanitized, presentation keys only)
//     and logo. Live's own storefront code reads templateId from the
//     public store-by-slug response (index-MERGED.js: it fetches the
//     store by slug with no auth and renders H4[templateId] ?? Modern),
//     and the template catalogue itself is public (/store-templates).
//     A storefront template + logo are the store's public presentation
//     (what any visitor sees), not owner-private data. Profit/margin,
//     owner data, phone, sales, referral codes, custom domains and
//     builderConfig are still never emitted here.
//     Widgets-only exception (F5): builderWidgets IS emitted —
//     only the six boolean widget toggles inside
//     builderConfig.widgets (see team05PublicBuilderWidgets).
//     They are presentation data (they decide which public
//     sections a visitor sees); every other builderConfig key
//     stays private under D52.
//     Central-deals exception (F1): centralDealsFilter and
//     centralDealsProductIds ARE emitted when set — the public
//     storefront's deals slider filters by them for real
//     visitors, so they are presentation data of the same
//     class. Both keys are omitted while unset, which the
//     storefront reads as the default niche mode.
//   - D48: discount campaigns are MANAGEMENT ONLY. No campaign query
//     exists in any price path in this tree, and none is added here:
//     price application of campaigns is unproven in live (U04).
//   - D45: GET /stores/<slug> is public and serves suspended stores
//     too; the real stores.status column is loaded by
//     loadPanelConfig (seller_panel_repo.go). The SPA router and the
//     loading/invalid/suspended STATES belong to Team 08.
//   - D47: only the backend list for the subscribers tab is built
//     here; the tab UI belongs to Team 08. Backend routes never
//     depend on dashboard tab order or labels (U35).
//   - D51: the server view recorder already exists (Team 01,
//     handleT01ViewEvent, POST /api/v1/views/events). The frontend
//     storefront-view call site lives in router-owned code, so it is
//     specified for Team 08 in the Team 05 report, not added to the
//     storeApi surface owned by this team.
//   - Not changed here, deliberately: store registration logic and
//     plan caps (U37), the ?ref= contract (U38, source behaviour
//     kept as-is), any server slug reserved-list rule (U39: the
//     server still only lowercases/trims/checks duplicates), the
//     change-request approval flow (U40), and the suspended-page
//     ownerPhone source (U42: unverified, so no phone source is
//     invented and no ownerPhone key is added to the public view).

// team05PublicStore is the live public shape (MASTER-AUDIT D52) plus
// the 3 FIX3 public storefront fields. Keep this struct in sync with
// the live probe for the 12 base keys, not with domain.Store. The 3
// added keys are presentation-only: templateId (one of the 5 public
// template ids), templateConfig (sanitized by
// team05PublicTemplateConfig: accent, section toggles and custom
// presentation keys only) and logo (the store's public brand image).
// Never add owner, margin, sales, phone, referral, domain or builder
// fields here — the only exceptions are builderWidgets, which
// carries ONLY the six boolean widget toggles from
// builderConfig.widgets (presentation data, F5), and the
// centralDealsFilter/centralDealsProductIds pair, which the
// public deals slider consumes (F1); builderConfig itself
// stays excluded.
type team05PublicStore struct {
	// FIX 14b: field order is live's struct order (probed 2026-11-03 on
	// /stores/mone: id, slug, name, tagline, description,
	// shortDescription, status, ownerType, activeProductIds,
	// selectedNicheIds, activeCategories, sellerDealsWidgetConfig).
	// The 3 FIX3 presentation-only keys are not live keys; they stay at
	// the end so the live base order is byte-identical.
	ID                      string          `json:"id"`
	Slug                    string          `json:"slug"`
	Name                    string          `json:"name"`
	Tagline                 string          `json:"tagline"`
	Description             string          `json:"description"`
	ShortDescription        string          `json:"shortDescription"`
	Status                  string          `json:"status"`
	OwnerType               string          `json:"ownerType"`
	ActiveProductIDs        []string        `json:"activeProductIds"`
	SelectedNicheIDs        []string        `json:"selectedNicheIds"`
	ActiveCategories        []string        `json:"activeCategories"`
	SellerDealsWidgetConfig json.RawMessage `json:"sellerDealsWidgetConfig"`
	TemplateID              string          `json:"templateId"`
	TemplateConfig          json.RawMessage `json:"templateConfig"`
	Logo                    string          `json:"logo"`
	BuilderWidgets          *team05PublicBuilderWidgets `json:"builderWidgets,omitempty"`
	// F1: the central-deals filter pair — presentation data the
	// public storefront deals slider consumes; omitted while
	// unset (unset == the default niche mode downstream).
	CentralDealsFilter      string          `json:"centralDealsFilter,omitempty"`
	CentralDealsProductIDs  []string        `json:"centralDealsProductIds,omitempty"`
}

// team05PublicBuilderWidgets is the widgets-only subset of
// builderConfig that the public store DTO may emit (F5): the six
// boolean toggles that decide which builder sections a real
// visitor sees. Everything else in builderConfig (theme, accent,
// social links, drafts) remains owner-private under D52.
type team05PublicBuilderWidgets struct {
	HeroBanner       bool `json:"heroBanner"`
	DealsSlider      bool `json:"dealsSlider"`
	CategoriesGrid   bool `json:"categoriesGrid"`
	StoreDealsWidget bool `json:"storeDealsWidget"`
	TrustBar         bool `json:"trustBar"`
	ContactInfo      bool `json:"contactInfo"`
}

// team05PublicBuilderWidgetsFrom extracts the widgets subset from
// a raw builderConfig document. A missing/unparseable document or
// a missing widgets object yields nil (the key is omitted and the
// storefront keeps its all-visible default); inside widgets, only
// an explicit false turns a section off, matching the builder
// default that every widget starts enabled.
func team05PublicBuilderWidgetsFrom(raw json.RawMessage) *team05PublicBuilderWidgets {
	var doc struct {
		Widgets map[string]json.RawMessage `json:"widgets"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil || doc.Widgets == nil {
		return nil
	}
	on := func(key string) bool {
		v, ok := doc.Widgets[key]
		if !ok {
			return true
		}
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return true
		}
		return b
	}
	return &team05PublicBuilderWidgets{
		HeroBanner:       on("heroBanner"),
		DealsSlider:      on("dealsSlider"),
		CategoriesGrid:   on("categoriesGrid"),
		StoreDealsWidget: on("storeDealsWidget"),
		TrustBar:         on("trustBar"),
		ContactInfo:      on("contactInfo"),
	}
}

// team05PublicTemplateConfigAllowlist is the FIX3 least-privilege
// filter for template_config: only keys that change what a public
// visitor sees (accent, section on/off toggles, and custom slide /
// banner / title / count presentation values). Anything else a seller
// may have stored in template_config is dropped, never emitted.
var team05PublicTemplateCustomAllowlist = map[string]bool{
	"heroSlides": true, "banners": true,
	"heroFallbackBanner1": true, "heroFallbackBanner2": true,
	"heroFallbackBanner3": true, "heroFallbackSubtitle": true,
	"categoriesTitle": true, "categoriesCount": true,
	"categoriesViewAll": true, "categoryOverrides": true,
	"categoryRailsMax": true, "categoryRailsCount": true,
	"dealsTitle": true, "dealsCount": true,
	"featuredTitle": true, "featuredCount": true,
	"bestSellersTitle": true, "bestsellersCount": true,
	"topRatedTitle": true, "topRatedCount": true,
	"newestTitle": true, "newestCount": true,
	"promoTilesCount": true, "promoBadge": true,
	"viewAllText": true, "heroAutoplay": true,
	"lookbookTitle": true, "lookbookImage": true,
	"lookbookSubtitle": true, "lookbookLink": true,
	"campaignImage": true, "campaignLink": true, "campaignEndsAt": true,
	"trustItems": true, "aboutImage": true,
	// Editable-appearance keys (seller panel section editor): the
	// storefront renders these on the public page, so they are
	// presentation values of exactly the same class as the keys above.
	"announcementText": true, "announcementLink": true,
	"heroTitle": true, "heroSubtitle": true, "heroCtaText": true,
	"heroCtaLink": true, "heroImage": true,
	"aboutTitle": true, "aboutText": true,
	"brandsTitle": true, "productsTitle": true,
	"testimonials": true, "faqItems": true,
	"newsletterTitle": true, "newsletterSubtitle": true,
}

// team05PublicTemplateConfig returns the public-safe subset of a raw
// template_config document: {accentColor, sections, custom?} with
// custom filtered through the allowlist above. Invalid or empty input
// yields {} (never null), so the storefront can render defaults.
func team05PublicTemplateConfig(raw json.RawMessage) json.RawMessage {
	var doc map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
		return json.RawMessage(`{}`)
	}
	out := map[string]json.RawMessage{}
	if v, ok := doc["accentColor"]; ok {
		out["accentColor"] = v
	}
	if v, ok := doc["sections"]; ok {
		out["sections"] = v
	}
	if customRaw, ok := doc["custom"]; ok {
		var custom map[string]json.RawMessage
		if json.Unmarshal(customRaw, &custom) == nil {
			filtered := map[string]json.RawMessage{}
			for k, v := range custom {
				if team05PublicTemplateCustomAllowlist[k] {
					filtered[k] = v
				}
			}
			if b, err := json.Marshal(filtered); err == nil {
				out["custom"] = b
			}
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// team05PublicStoreView converts a loaded store to the public shape.
// Empty lists are emitted as [] (never null) so the storefront can
// render without nil checks.
func team05PublicStoreView(s *domain.Store) team05PublicStore {
	if s == nil {
		return team05PublicStore{
			ActiveCategories: []string{},
			ActiveProductIDs: []string{},
			SelectedNicheIDs: []string{},
			TemplateConfig:   json.RawMessage(`{}`),
		}
	}
	out := team05PublicStore{
		ActiveCategories:        s.ActiveCategories,
		ActiveProductIDs:        s.ActiveProductIDs,
		BuilderWidgets:          team05PublicBuilderWidgetsFrom(s.BuilderConfig),
		CentralDealsFilter:      s.CentralDealsFilter,
		CentralDealsProductIDs:  s.CentralDealsProductIDs,
		Description:             s.Description,
		ID:                      s.ID,
		Logo:                    s.Logo,
		Name:                    s.Name,
		OwnerType:               s.OwnerType,
		SelectedNicheIDs:        s.SelectedNicheIDs,
		SellerDealsWidgetConfig: s.SellerDealsWidgetConfig,
		ShortDescription:        s.ShortDescription,
		Slug:                    s.Slug,
		Status:                  s.Status,
		Tagline:                 s.Tagline,
		TemplateConfig:          team05PublicTemplateConfig(s.TemplateConfig),
		TemplateID:              s.TemplateID,
	}
	if out.ActiveCategories == nil {
		out.ActiveCategories = []string{}
	}
	if out.ActiveProductIDs == nil {
		out.ActiveProductIDs = []string{}
	}
	if out.SelectedNicheIDs == nil {
		out.SelectedNicheIDs = []string{}
	}
	return out
}

// team05PublicStoreList converts a store list to public views; a nil
// input becomes an empty array, never null.
func team05PublicStoreList(stores []domain.Store) []team05PublicStore {
	out := make([]team05PublicStore, 0, len(stores))
	for i := range stores {
		out = append(out, team05PublicStoreView(&stores[i]))
	}
	return out
}

func team05SellerClaims(w http.ResponseWriter, r *http.Request) (*auth.UserClaims, bool) {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return nil, false
	}
	return claims, true
}

// handleTeam05StoreDiscountCampaigns serves the seller discount
// campaign CRUD (MASTER-AUDIT D48, backend only). Routes: GET and
// POST on /stores/<slug>/discount-campaigns, PATCH and DELETE on
// /stores/<slug>/discount-campaigns/<id>.
// Listing is scoped to the store and ordered by created_at DESC;
// deletion is scoped by id AND store_id, matching the live handler
// pattern (p4aStoreDiscountCampaigns). The caller must be the
// store-owning seller (OwnedStore); the frontend tab and its
// service calls belong to Team 08. Campaigns are stored and listed
// only; they are never consulted when pricing an order (U04).
func handleTeam05StoreDiscountCampaigns(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug, id string) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := team05SellerClaims(w, r)
	if !ok {
		return
	}
	store, err := repo.OwnedStore(r.Context(), claims.UserID, slug)
	if err != nil {
		sellerError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			http.NotFound(w, r)
			return
		}
		campaigns, err := repo.ListStoreDiscountCampaigns(r.Context(), store.ID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if campaigns == nil {
			campaigns = []domain.SellerDiscountCampaign{}
		}
		json.NewEncoder(w).Encode(campaigns)
	case http.MethodPost:
		if id != "" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Name     string    `json:"name"`
			Scope    string    `json:"scope"`
			TargetID string    `json:"targetId"`
			Mode     string    `json:"mode"`
			Value    float64   `json:"value"`
			StartsAt time.Time `json:"startsAt"`
			EndsAt   time.Time `json:"endsAt"`
			IsActive bool      `json:"isActive"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}
		// FIX 14: margin-lock check at CREATION time (previously only
		// application was blocked, so a locked seller could create a
		// campaign that silently never applied).
		blocked, berr := repo.DiscountCampaignCreationBlocked(r.Context(), store.ID, payload.Scope, payload.TargetID)
		if berr != nil {
			writeInternalError(w, berr)
			return
		}
		if blocked {
			http.Error(w, `{"error":"Margin is locked by admin for this target"}`, http.StatusForbidden)
			return
		}
		created, err := repo.CreateStoreDiscountCampaign(r.Context(), domain.SellerDiscountCampaign{
			ID:       "dcamp-" + uuid.NewString(),
			StoreID:  store.ID,
			Name:     payload.Name,
			Scope:    payload.Scope,
			TargetID: payload.TargetID,
			Mode:     payload.Mode,
			Value:    payload.Value,
			StartsAt: payload.StartsAt,
			EndsAt:   payload.EndsAt,
			IsActive: payload.IsActive,
		})
		if err != nil {
			sellerError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(created)
	case http.MethodPatch:
		if id == "" {
			http.NotFound(w, r)
			return
		}
		var payload domain.SellerDiscountCampaignPatch
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}
		found, err := repo.UpdateStoreDiscountCampaign(r.Context(), store.ID, id, payload)
		if err != nil {
			sellerError(w, err)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		updated, err := repo.GetStoreDiscountCampaign(r.Context(), store.ID, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		json.NewEncoder(w).Encode(updated)
	case http.MethodDelete:
		if id == "" {
			http.NotFound(w, r)
			return
		}
		found, err := repo.DeleteStoreDiscountCampaign(r.Context(), store.ID, id)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id})
	default:
		writeMethodNotAllowed(w)
	}
}

// handleTeam05StoreSubscribers serves the backend list for the
// seller subscribers tab (MASTER-AUDIT D47, backend only).
// Route: GET /stores/<slug>/subscribers, with an optional
// ?status=active|unsubscribed filter.
// It reads the same newsletter_subscribers table Team 01 used for
// the admin list, scoped to the seller's own store and ordered by
// created_at DESC. The unsubscribe_token column is never returned:
// it is the only credential the public unsubscribe endpoint needs.
// The tab UI belongs to Team 08.
func handleTeam05StoreSubscribers(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository, slug string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	claims, ok := team05SellerClaims(w, r)
	if !ok {
		return
	}
	store, err := repo.OwnedStore(r.Context(), claims.UserID, slug)
	if err != nil {
		sellerError(w, err)
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	subscribers, err := repo.ListStoreSubscribers(r.Context(), store.ID, status, t01Limit(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if subscribers == nil {
		subscribers = []domain.StoreSubscriber{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"subscribers": subscribers})
}
