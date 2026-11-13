package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Team 04 live parity: product list/detail (D21, D28, D41, D42, D43, D46,
// specs/gallery shared gap). This section REPLACES the old ProductResponse
// and handleProductsList. Nothing outside this section of handlers.go is
// touched by Team 04.
// ---------------------------------------------------------------------------

// ProductVariantResponse is one variant as returned inside a product
// (live T04 section 2). product_variants has no min/max-cart columns, so
// the live payload constants are used: minCartQty 1, maxCartQty null.
// The variant's supplier_base_cost is deliberately NOT exposed here: it
// is a cost input to pricing, not a public field (same reasoning as the
// removal of baseCostB from the product response, D21).
type ProductVariantResponse struct {
	Selection        map[string]string `json:"selection"`
	InventoryMode    string            `json:"inventoryMode"`
	IsActive         bool              `json:"isActive"`
	CombinationKey   string            `json:"combinationKey,omitempty"`
	SupplierBaseCost *int64            `json:"supplierBaseCost,omitempty"`
	// FIX 14b: field order is live's (probed 2026-11-03 on the bracelet
	// detail): spnc 4th, isDefault before stock. Go encodes struct
	// fields in declaration order, so this order IS the wire order.
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	VariantName string                 `json:"variantName"`
	Spnc        string                 `json:"spnc"`
	Attributes  map[string]interface{} `json:"attributes"`
	IsDefault   bool                   `json:"isDefault"`
	Stock       int                    `json:"stock"`
	MinCartQty  int                    `json:"minCartQty"`
	MaxCartQty  *int                   `json:"maxCartQty"`
	Images      []string               `json:"images,omitempty"`
	WeightGrams *int                   `json:"weightGrams,omitempty"`
	LengthMM    *int                   `json:"lengthMm,omitempty"`
	WidthMM     *int                   `json:"widthMm,omitempty"`
	HeightMM    *int                   `json:"heightMm,omitempty"`
	// P2(b): EFFECTIVE classes (variant ?? product ?? 'standard'),
	// resolved in buildProductResponse; always present.
	ShippingClass string `json:"shippingClass"`
	TaxClass      string `json:"taxClass"`
}

// VariantPriceResponse is one entry of variantPrices, keyed by variant
// ID (UUID) exactly as live serves it (FIX 4, team 04 G1 / team 02 G6).
type VariantPriceResponse struct {
	Price           int64      `json:"price"`
	OriginalPrice   int64      `json:"originalPrice"`
	DiscountPercent int        `json:"discountPercent"`
	CampaignName    string     `json:"campaignName,omitempty"`
	// P1(c): configured sale price + window (echoed whenever set, any
	// window state); OnSale is true iff the sale determined this price.
	SalePrice *int64     `json:"salePrice,omitempty"`
	SaleFrom  *time.Time `json:"saleFrom,omitempty"`
	SaleTo    *time.Time `json:"saleTo,omitempty"`
	OnSale    bool       `json:"onSale,omitempty"`
}

// ProductResponse is the FROZEN live list shape (D21): 21 always-present
// keys, in this order, plus three appended omitempty extensions:
// priceMin/priceMax (P1(a)) and storeDefaultVariantId (P2(c)). The
// appended keys sit at the end so the FIX-14 supplier-key positions
// never shift. The following source fields are deliberately
// absent and must not be re-added: supplierId, supplierName (hardcoded
// supplier names), baseCostB (cost leak; live central responses omit it
// and the dropshipper-view baseCostB=C is an engine-internal value, not
// a response key), profitToman, marginPercent, installmentPrice
// (installments were deleted by user decision).
type ProductResponse struct {
	ProductType   string                `json:"productType"`
	SchemaVersion int                   `json:"schemaVersion"`
	ProductSchema *domain.ProductSchema `json:"productSchema,omitempty"`
	ID            string                `json:"id"`
	Title         string                `json:"title"`
	Slug          string                `json:"slug"`
	Category      string                `json:"category"`
	// FIX 14: live list serves the three supplier keys ONLY for an
	// existing NON-central store view, at exactly these positions
	// (supplierId/supplierName after category, baseCostB after price).
	SupplierID       *string                         `json:"supplierId,omitempty"`
	SupplierName     *string                         `json:"supplierName,omitempty"`
	OriginalPrice    int64                           `json:"originalPrice"`
	DiscountEligible bool                            `json:"discountEligible"`
	DiscountPercent  int                             `json:"discountPercent"`
	CampaignName     string                          `json:"campaignName,omitempty"`
	Price            int64                           `json:"price"`
	BaseCostB        *int64                          `json:"baseCostB,omitempty"`
	PlatformFeeF     int64                           `json:"platformFeeF"`
	IsOriginal       bool                            `json:"isOriginal"`
	Stock            int                             `json:"stock"`
	Image            string                          `json:"image"`
	Description      string                          `json:"description"`
	Warehouse        string                          `json:"warehouse"`
	NicheIDs         []string                        `json:"nicheIds"`
	IsActive         bool                            `json:"isActive"`
	Brand            string                          `json:"brand"`
	Variants         []ProductVariantResponse        `json:"variants"`
	VariantPrices    map[string]VariantPriceResponse `json:"variantPrices"`
	// P1(a): per-store price range over priced ACTIVE variants
	// (stock deliberately NOT a filter — the range states price truth,
	// not availability truth). Simple products: min == max. Zero-variant
	// legacy products: both = product-level price. Variable products
	// with no priced variant: both omitted (nil), never 0 and never
	// the product price. P2(c): this store view's default variant.
	PriceMin             *int64  `json:"priceMin,omitempty"`
	PriceMax             *int64  `json:"priceMax,omitempty"`
	StoreDefaultVariantID *string `json:"storeDefaultVariantId,omitempty"`
}

// ProductDetailResponse is the live detail shape (D21): the 19 list keys
// minus brand, plus supplierWarranty and storeCount (20 live keys),
// followed by three FLAGGED Phase 2 extensions that are NOT part of the
// live shape: specs and images (specs/gallery shared gap, MASTER
// section 1: present in the DB in both source and live, in neither API)
// and spnNumber (D46). Team 08 must not treat the extension keys as
// live-proven, and a future live-parity check must compare only the
// first 20 keys against live. P1(a)/P2(c) append priceMin, priceMax and
// storeDefaultVariantId at the end (same semantics as the list shape).
type ProductDetailResponse struct {
	ProductType   string                `json:"productType"`
	SchemaVersion int                   `json:"schemaVersion"`
	ProductSchema *domain.ProductSchema `json:"productSchema,omitempty"`
	// FIX 14b: live's detail mapper also reads addons + addonSectionTitle
	// (live bundle); both are omitempty, so products without addons --
	// all 40 current live products -- serve no addon keys, like live.
	Addons            []repository.PDPAddon `json:"addons,omitempty"`
	AddonSectionTitle string                `json:"addonSectionTitle,omitempty"`
	// Live detail keys are alphabetical (live builds the detail from a
	// map); this struct is ordered to match live's byte order.
	// FIX 4 (team 04 G3 / team 02 G5): live serves the three supplier
	// keys ONLY on an existing non-central store view (?storeSlug=...).
	// They stay absent (omitempty nil) on the central view, matching
	// live central responses.
	BaseCostB        *int64 `json:"baseCostB,omitempty"`
	Category         string `json:"category"`
	Description      string `json:"description"`
	DiscountEligible bool   `json:"discountEligible"`
	DiscountPercent  int    `json:"discountPercent"`
	CampaignName     string `json:"campaignName,omitempty"`
	ID               string `json:"id"`
	Image            string `json:"image"`
	// Flagged extensions (see type comment above).
	Images           []string                        `json:"images,omitempty"`
	IsActive         bool                            `json:"isActive"`
	IsOriginal       bool                            `json:"isOriginal"`
	NicheIDs         []string                        `json:"nicheIds"`
	OriginalPrice    int64                           `json:"originalPrice"`
	PlatformFeeF     int64                           `json:"platformFeeF"`
	Price            int64                           `json:"price"`
	Slug             string                          `json:"slug"`
	Specs            map[string]interface{}          `json:"specs,omitempty"`
	SpnNumber        *int64                          `json:"spnNumber"`
	Stock            int                             `json:"stock"`
	StoreCount       int                             `json:"storeCount"`
	SupplierID       *string                         `json:"supplierId,omitempty"`
	SupplierName     *string                         `json:"supplierName,omitempty"`
	SupplierWarranty string                          `json:"supplierWarranty"`
	Title            string                          `json:"title"`
	VariantPrices    map[string]VariantPriceResponse `json:"variantPrices"`
	Variants         []ProductVariantResponse        `json:"variants"`
	Warehouse        string                          `json:"warehouse"`
	PriceMin         *int64                          `json:"priceMin,omitempty"`
	PriceMax         *int64                          `json:"priceMax,omitempty"`
	StoreDefaultVariantID *string                    `json:"storeDefaultVariantId,omitempty"`
}

// deriveDiscountPercent derives discountPercent exactly as Team 02
// documents for live: (OriginalPrice - P) / OriginalPrice, rounded to a
// whole percent. It is NOT a seller discount campaign (D48, U04): no
// campaign is applied anywhere in this file.
func deriveDiscountPercent(originalPrice, price int64) int {
	if originalPrice <= 0 || price >= originalPrice {
		return 0
	}
	return int(math.Round(float64(originalPrice-price) / float64(originalPrice) * 100))
}

// normalizeSearchTerm normalises a search term for logging (D43,
// logSearchEvent/normalizeSearchTerm): trim, collapse whitespace runs,
// and map Arabic Yeh/Kaf to Persian Yeh/Kaf. Digits are NOT converted:
// the logged term keeps the user's digits, and the query itself still
// runs against the raw term. The exact live normalisation is unproven
// beyond the symbol name, so this is the documented, minimal rule.
func normalizeSearchTerm(term string) string {
	collapsed := strings.Join(strings.Fields(term), " ")
	var b strings.Builder
	for _, r := range collapsed {
		switch r {
		case 'ي':
			b.WriteRune('ی')
		case 'ك':
			b.WriteRune('ک')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizePersianDigits converts Persian and Arabic-Indic digits to
// Latin digits. Used only for parsing spn- numbers (D46), never to
// rewrite slugs or search terms.
func normalizePersianDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseSpnSegment parses one "spn-<n>" API path segment (D46). Persian
// and Arabic digits are accepted and normalised to Latin digits.
func parseSpnSegment(segment string) (int64, error) {
	if !strings.HasPrefix(segment, "spn-") {
		return 0, fmt.Errorf("not an spn segment")
	}
	digits := normalizePersianDigits(strings.TrimPrefix(segment, "spn-"))
	if digits == "" {
		return 0, fmt.Errorf("empty spn number")
	}
	return strconv.ParseInt(digits, 10, 64)
}

// BuildCanonicalProductURL builds the canonical shared product URL
// (D46): /product/spn-<n>/<slug>/, with ?spnc=<n> appended when a
// variant number is given. When the product has no spn_number (legacy
// rows predate migration 202610280006 in a test database), it falls
// back to /product/<id>. This is URL construction only; the frontend
// router itself belongs to Team 08.
func BuildCanonicalProductURL(p domain.Product, variantSpncNumber *int) string {
	if p.SpnNumber == nil {
		return "/product/" + p.ID
	}
	url := "/product/spn-" + strconv.FormatInt(*p.SpnNumber, 10) + "/" + p.Slug + "/"
	if variantSpncNumber != nil {
		url += "?spnc=" + strconv.Itoa(*variantSpncNumber)
	}
	return url
}

// ParseCanonicalProductPath parses a frontend canonical product path
// (D46): /product/spn-<n>/<slug>/ (trailing slash optional, Persian or
// Arabic digits accepted). It returns the spn number and the slug, or
// ok=false for any other shape. The ?spnc= query value is parsed by the
// caller, not here, because it is a query parameter, not part of the
// path.
func ParseCanonicalProductPath(path string) (spn int64, slug string, ok bool) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] != "product" {
		return 0, "", false
	}
	n, err := parseSpnSegment(parts[1])
	if err != nil {
		return 0, "", false
	}
	if len(parts) >= 3 {
		slug = parts[2]
	}
	return n, slug, true
}

// DefaultVariantForResponse returns the deterministic default variant:
// the first is_default variant in repo order (is_default DESC,
// created_at ASC, id ASC); if none is marked default, the first variant.
// TODO U32: live has 51 products with variants but only 50 is_default
// rows, so at least one product has no default (or one has two). The
// live fallback for that case is unproven; this deterministic rule must
// not be read as live behaviour.
func DefaultVariantForResponse(variants []domain.ProductVariant) *domain.ProductVariant {
	if len(variants) == 0 {
		return nil
	}
	for i := range variants {
		if variants[i].InventoryMode == "tracked" && !variants[i].IsActive {
			continue
		}
		if variants[i].IsDefault {
			return &variants[i]
		}
	}
	for i := range variants {
		if variants[i].InventoryMode != "tracked" || variants[i].IsActive {
			return &variants[i]
		}
	}
	return nil
}

// StoreDefaultVariantForResponse returns the store's default variant
// from an already store-filtered variant list (P2(a) filtering applied
// by the caller), or nil when there is no row / the row's variant is
// not in the list. Purchasability (price/stock) is NOT checked here —
// the caller applies it. DefaultVariantForResponse is left unchanged:
// its tests and semantics are product-level.
func StoreDefaultVariantForResponse(variants []domain.ProductVariant, storeDefaultVariantID string) *domain.ProductVariant {
	if storeDefaultVariantID == "" {
		return nil
	}
	for i := range variants {
		if variants[i].ID == storeDefaultVariantID {
			return &variants[i]
		}
	}
	return nil
}

// buildProductResponse renders one product in the frozen 19-key list
// shape. discountEligible is true for every listed product: live
// returned true for all 40 active central products and no eligibility
// rule beyond that observation was proven, so no narrower rule is
// invented here.
func buildProductResponse(p domain.Product, res pricing.ReferenceResolution, variants []domain.ProductVariant, variantPrices map[string]VariantPriceResponse, storeDefaultVariantID ...string) ProductResponse {
	if variants == nil {
		variants = []domain.ProductVariant{}
	}
	vresp := make([]ProductVariantResponse, 0, len(variants))
	for _, v := range variants {
		attrs := v.Attributes
		if attrs == nil {
			attrs = map[string]interface{}{}
		}
		mode, active := v.InventoryMode, v.IsActive
		if mode == "" {
			mode = "legacy"
			active = true
		}
		selection := v.Selection
		if selection == nil {
			selection = map[string]string{}
		}
		minQty := v.MinCartQty
		if minQty < 1 {
			minQty = 1
		}
		shipClass := ""
		if v.ShippingClass != nil {
			shipClass = *v.ShippingClass
		}
		taxClass := ""
		if v.TaxClass != nil {
			taxClass = *v.TaxClass
		}
		vresp = append(vresp, ProductVariantResponse{
			Selection: selection, InventoryMode: mode, IsActive: active, CombinationKey: v.CombinationKey,
			ID:          v.ID,
			Spnc:        v.Spnc,
			Name:        v.Name,
			VariantName: v.Name,
			Attributes:  attrs,
			Stock:       v.Stock,
			IsDefault:   v.IsDefault,
			MinCartQty:  minQty,
			MaxCartQty:  v.MaxCartQty,
			Images:      v.Images,
			WeightGrams: v.WeightGrams,
			LengthMM:    v.LengthMM,
			WidthMM:     v.WidthMM,
			HeightMM:    v.HeightMM,
			// P2(b): effective classes, resolved once here.
			ShippingClass: domain.EffectiveShippingClass(shipClass, p.ShippingClass),
			TaxClass:      domain.EffectiveTaxClass(taxClass, p.TaxClass),
		})
	}
	if variantPrices == nil {
		variantPrices = map[string]VariantPriceResponse{}
	}
	// P1(a): price range over priced ACTIVE variants in the rendered
	// list. Stock is deliberately NOT a filter (see struct comment).
	// The IsActive filter comes from the rendered list (the legacy
	// normalisation above already handled mode == "" rows), not from
	// the map alone: priceVariantsForResponse still prices legacy
	// inactive variants.
	var priceMin, priceMax *int64
	for _, vr := range vresp {
		if !vr.IsActive {
			continue
		}
		vp, ok := variantPrices[vr.ID]
		if !ok || vp.Price <= 0 {
			continue
		}
		if priceMin == nil || vp.Price < *priceMin {
			v := vp.Price
			priceMin = &v
		}
		if priceMax == nil || vp.Price > *priceMax {
			v := vp.Price
			priceMax = &v
		}
	}
	if priceMin == nil && len(variants) == 0 {
		// Zero-variant legacy product: the product-level price is the
		// only price that exists.
		v := res.FinalPrice
		priceMin, priceMax = &v, &v
	}
	var storeDefaultID *string
	if len(storeDefaultVariantID) > 0 && storeDefaultVariantID[0] != "" {
		// Only advertise a store default that is actually in the
		// (store-filtered) variant list; a hidden/inactive default
		// falls through silently (the row is not auto-deleted).
		for _, v := range variants {
			if v.ID == storeDefaultVariantID[0] {
				s := storeDefaultVariantID[0]
				storeDefaultID = &s
				break
			}
		}
	}
	if p.NicheIDs == nil {
		p.NicheIDs = []string{}
	}
	return ProductResponse{
		ProductType: productTypeForResponse(p), SchemaVersion: p.SchemaVersion, ProductSchema: p.ProductSchema,
		ID:               p.ID,
		Title:            p.Title,
		Slug:             p.Slug,
		Category:         p.CategorySlug,
		OriginalPrice:    res.OriginalPrice,
		DiscountEligible: true,
		DiscountPercent:  deriveDiscountPercent(res.OriginalPrice, res.FinalPrice),
		Price:            res.FinalPrice,
		PlatformFeeF:     res.PlatformFee,
		IsOriginal:       p.IsOriginal,
		Stock:            p.Stock,
		Image:            p.ImageURL,
		Description:      p.Description,
		Warehouse:        p.WarehouseName,
		NicheIDs:         p.NicheIDs,
		IsActive:         p.IsActive,
		Brand:            p.Brand,
		Variants:         vresp,
		VariantPrices:    variantPrices,
		PriceMin:         priceMin,
		PriceMax:         priceMax,
		StoreDefaultVariantID: storeDefaultID,
	}
}

// priceVariantsForResponse prices every variant with Team 02's resolver
// (D28). A variant's own supplier_base_cost wins when set; otherwise the
// product cost is used. Variant percent overrides from
// store_variant_overrides win over the product resolution; fixed-mode
// overrides fall back inside the resolver (TODO U10, owned by Team 02).
// The store's timed-deals widget discount (when the product is in a
// live widget campaign) is then applied per variant for display.
// This function never touches pricing internals: it only calls the
// resolver Team 02 supplies.
func priceVariantsForResponse(p domain.Product, variants []domain.ProductVariant, store domain.Store, settings *pricing.PlatformPricingSettings, locks []pricing.MarginLock, overrides map[string]pricing.StoreVariantOverride, campaigns []domain.SellerDiscountCampaign) map[string]VariantPriceResponse {
	out := make(map[string]VariantPriceResponse, len(variants))
	widgetCfg := loadDealsWidgetConfig(&store)
	for _, v := range variants {
		if v.InventoryMode == "tracked" && !v.IsActive {
			continue
		}
		cost := p.SupplierBaseCost
		if v.SupplierBaseCost != nil {
			cost = *v.SupplierBaseCost
		}
		var ov *pricing.StoreVariantOverride
		if o, ok := overrides[p.ID+"/"+v.ID]; ok {
			oc := o
			ov = &oc
		}
		res := pricing.ResolveReferencePricingForStoreProductVariantWithCost(p, cost, v.ID, store, settings, locks, ov)
		res = applyDealsWidgetDiscountForDisplay(&store, widgetCfg, p.ID, res)
		vname := ""
		if camp := pickDisplayCampaign(campaigns, p, v.ID); camp != nil && !displayCampaignLocked(store, locks, p, v.ID) {
			d := applyDisplayCampaign(res, camp)
			res = d.Resolution
			vname = d.CampaignName
		}
		// P1(c): sale AFTER campaigns (shared helper — the charge
		// path calls the same one). Lowest wins; floor R; a sale
		// below R suspends. When the sale wins, no campaign name.
		onSale := false
		if v.SalePrice != nil {
			var saleRes pricing.ReferenceResolution
			saleRes, onSale = pricing.ApplyVariantSale(res, v.SalePrice, time.Now(), v.SaleFrom, v.SaleTo)
			res = saleRes
			if onSale {
				vname = ""
			}
		}
		out[v.ID] = VariantPriceResponse{
			Price:           res.FinalPrice,
			OriginalPrice:   res.OriginalPrice,
			DiscountPercent: deriveDiscountPercent(res.OriginalPrice, res.FinalPrice),
			CampaignName:    vname,
			SalePrice:       v.SalePrice,
			SaleFrom:        v.SaleFrom,
			SaleTo:          v.SaleTo,
			OnSale:          onSale,
		}
	}
	return out
}

// loadVariantOverridesForStore loads a store's variant overrides once
// per request into a productID/variantID-keyed map. A nil pricing repo
// or a load error yields no overrides (product resolution applies),
// never a failed request.
func loadVariantOverridesForStore(ctx context.Context, pricingRepo *repository.PricingRepository, storeID string) map[string]pricing.StoreVariantOverride {
	out := map[string]pricing.StoreVariantOverride{}
	if pricingRepo == nil || storeID == "" {
		return out
	}
	list, err := pricingRepo.ListStoreVariantOverrides(ctx, storeID)
	if err != nil {
		return out
	}
	for _, o := range list {
		out[o.ProductID+"/"+o.VariantID] = o
	}
	return out
}

// resolveListStore resolves the store for a catalog listing exactly as
// before: the requested storeSlug, falling back to central, falling
// back to nil (central pricing via an empty store value).
func resolveListStore(ctx context.Context, storeRepo *repository.StoreRepository, storeSlug string) *domain.Store {
	if storeRepo == nil {
		return nil
	}
	store, _ := storeRepo.GetStoreBySlug(ctx, storeSlug)
	if store == nil {
		store, _ = storeRepo.GetStoreBySlug(ctx, "central")
	}
	return store
}

// handleProductsList prices every product with the live percent engine
// (pricing.ResolveReferencePricing). The legacy absolute-fee engine is
// never called here. InstallmentPrice is deliberately absent: live has
// no installmentPrice in the product response and installments were
// deleted by user decision, so no installment field is produced here.
// List filtering is live behaviour (D42): only active products are
// listed (enforced in the repository); brand filtering is an EXACT
// match (D41), never partial.
// Store views are the exception that makes a storefront work: when
// ?storeSlug= names an existing non-central dropshipper store, the
// list is THAT store's own assortment (its store_products set, the
// same IDs the store DTO exposes as activeProductIds), priced with
// the store's resolved margins below. The unscoped catalog is never
// leaked into a store view, and assortment membership replaces the
// global is_active gate there — without this, a dropshipper whose
// picks are globally inactive renders an empty store.
func handleProductsList(w http.ResponseWriter, r *http.Request, ctx context.Context, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, pricingRepo *repository.PricingRepository, supplierRepos ...*repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")

	category := r.URL.Query().Get("category")
	// FIX 4 (team 04 G4): live honours ?limit=; an absent, zero,
	// negative or unparsable value means "no limit" (full active list).
	limit := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}
	niche := r.URL.Query().Get("niche")
	search := r.URL.Query().Get("search")
	sort := r.URL.Query().Get("sort")
	storeSlug := r.URL.Query().Get("storeSlug")
	brand := r.URL.Query().Get("brand")

	var isOriginal *bool
	if origStr := r.URL.Query().Get("is_original"); origStr != "" {
		val, _ := strconv.ParseBool(origStr)
		isOriginal = &val
	}

	inStockOnly := false
	if inStockStr := r.URL.Query().Get("in_stock"); inStockStr != "" {
		inStockOnly, _ = strconv.ParseBool(inStockStr)
	}

	if catalogRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var supplierRepo *repository.SupplierRepository
	if len(supplierRepos) > 0 {
		supplierRepo = supplierRepos[0]
	}

	filter := repository.ProductFilter{
		CategorySlug: category,
		NicheSlug:    niche,
		Brand:        brand,
		IsOriginal:   isOriginal,
		InStockOnly:  inStockOnly,
		Search:       search,
		SortBy:       sort,
	}

	// The explicitly requested store (no central fallback): an existing
	// non-central dropshipper store scopes the listing to its own
	// assortment. reqStore is reused for the FIX 14 supplier fields
	// below, which key on the same condition.
	var reqStore *domain.Store
	if storeSlug != "" {
		reqStore, _ = storeRepo.GetStoreBySlug(ctx, storeSlug)
	}
	if reqStore != nil && reqStore.Slug != "central" && reqStore.OwnerType == "dropshipper" {
		filter.StoreScoped = true
		filter.StoreProductIDs = reqStore.ActiveProductIDs
	}

	products, err := catalogRepo.GetProducts(ctx, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	settings, _ := loadPricingSettings(ctx, pricingRepo)
	store := resolveListStore(ctx, storeRepo, storeSlug)

	// D43 logged search: best effort, never fails the listing. The
	// public list is catalog-wide, so the shown result count is both
	// the store result count and the catalog result count here; the
	// store-assortment split for seller search-insight is a separate
	// concern and is not conflated with a guessed number.
	if search != "" {
		effectiveSlug := storeSlug
		if store != nil {
			effectiveSlug = store.Slug
		}
		if effectiveSlug == "" {
			effectiveSlug = "central"
		}
		_ = catalogRepo.LogSearchEvent(ctx, normalizeSearchTerm(search), effectiveSlug, len(products), len(products))
	}

	ids := make([]string, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	// P2(a): a non-central store view loads store-filtered variants
	// (hidden variants never reach pricing or the response). Central
	// is never filtered. P2(c): the store's default variants, batched.
	storeIDForVariants := ""
	if store != nil && store.Slug != "central" {
		storeIDForVariants = store.ID
	}
	variantsByProduct, _ := catalogRepo.GetVariantsByProductIDsForStore(ctx, ids, storeIDForVariants)
	defaultsByProduct, _ := catalogRepo.GetStoreVariantDefaults(ctx, storeIDForVariants, ids)

	var storeVal domain.Store
	var locks []pricing.MarginLock
	var overrides map[string]pricing.StoreVariantOverride
	if store != nil {
		storeVal = *store
		locks = loadStoreMarginLocks(ctx, pricingRepo, store.ID)
		overrides = loadVariantOverridesForStore(ctx, pricingRepo, store.ID)
	} else {
		overrides = map[string]pricing.StoreVariantOverride{}
	}
	displayCampaigns := loadActiveDiscountCampaignsForDisplay(ctx, storeRepo, store)

	// FIX 4 (team 04 G5): a nil slice encodes as JSON null, matching
	// live empty results. Callers must tolerate null (the candidate
	// frontend normalises it to [] in catalogApi.getProducts).
	// FIX 14: live supplier rule, proven by public API probes
	// 2026-11-03: the list carries supplierId/supplierName/baseCostB
	// ONLY when ?storeSlug= names an existing NON-central store
	// (vishkostor: all 40 items carry them; central or no slug or an
	// unknown slug: none). The fallback central store used for pricing
	// must NOT trigger the fields.
	// AUTH-KEYED (owner secrecy rule): the store view alone no longer
	// suffices -- the fields are the supplier's cost book and are
	// filled per item only for a caller related to the store or the
	// product (see canSeeSupplierFields); everyone else gets the
	// central shape, platformFeeF included (zeroed below).
	viewStore := reqStore != nil && reqStore.Slug != "central"
	var viewer supplierFieldsViewer
	if viewStore {
		viewer = resolveSupplierFieldsViewer(ctx, supplierRepo)
	}
	supplierNameByID := map[string]string{}
	widgetCfg := loadDealsWidgetConfig(store)
	var resp []ProductResponse
	for _, p := range products {
		res := pricing.ResolveReferencePricing(p, storeVal, settings, locks)
		res = applyDealsWidgetDiscountForDisplay(store, widgetCfg, p.ID, res)
		displayCampaignName := ""
		if camp := pickDisplayCampaign(displayCampaigns, p, ""); camp != nil && !displayCampaignLocked(storeVal, locks, p, "") {
			d := applyDisplayCampaign(res, camp)
			res = d.Resolution
			displayCampaignName = d.CampaignName
		}
		variants := variantsByProduct[p.ID]
		item := buildProductResponse(p, res, variants, priceVariantsForResponse(p, variants, storeVal, settings, locks, overrides, displayCampaigns), defaultsByProduct[p.ID])
		item.CampaignName = displayCampaignName
		if viewStore && viewer.canSeeSupplierFields(reqStore, p.SupplierID) {
			sid := p.SupplierID
			item.SupplierID = &sid
			if name, ok := supplierNameByID[p.SupplierID]; !ok {
				name, _ = catalogRepo.GetSupplierNameByID(ctx, p.SupplierID)
				supplierNameByID[p.SupplierID] = name
				item.SupplierName = &name
			} else {
				item.SupplierName = &name
			}
			b := p.SupplierBaseCost
			item.BaseCostB = &b
			exposeScopedVariantCosts(&item, variants, p.SupplierBaseCost)
		} else {
			item.PlatformFeeF = 0
		}
		resp = append(resp, item)
	}
	if limit > 0 && limit < len(resp) {
		resp = resp[:limit]
	}

	json.NewEncoder(w).Encode(resp)
}

// assembleProductDetail builds the 20-key live detail shape (the
// specs/images extensions are omitted like live; spnNumber stays as
// the flagged extension). It deliberately does NOT
// filter on is_active: live returns 200 for an inactive product's
// detail (D42, live-confirmed-200, not a Team 04 choice).
func assembleProductDetail(ctx context.Context, catalogRepo *repository.CatalogRepository, pricingRepo *repository.PricingRepository, storeRepo *repository.StoreRepository, store *domain.Store, settings *pricing.PlatformPricingSettings, p domain.Product, showSupplier bool) (ProductDetailResponse, error) {
	var storeVal domain.Store
	var locks []pricing.MarginLock
	var overrides map[string]pricing.StoreVariantOverride
	if store != nil {
		storeVal = *store
		locks = loadStoreMarginLocks(ctx, pricingRepo, store.ID)
		overrides = loadVariantOverridesForStore(ctx, pricingRepo, store.ID)
	} else {
		overrides = map[string]pricing.StoreVariantOverride{}
	}
	displayCampaigns := loadActiveDiscountCampaignsForDisplay(ctx, storeRepo, store)
	res := pricing.ResolveReferencePricing(p, storeVal, settings, locks)
	res = applyDealsWidgetDiscountForDisplay(store, loadDealsWidgetConfig(store), p.ID, res)
	pdpCampaignName := ""
	if camp := pickDisplayCampaign(displayCampaigns, p, ""); camp != nil && !displayCampaignLocked(storeVal, locks, p, "") {
		d := applyDisplayCampaign(res, camp)
		res = d.Resolution
		pdpCampaignName = d.CampaignName
	}
	// P2(a)/P2(c): store-filtered variants + the store's default for
	// this product (central: unfiltered, no default).
	storeIDForVariants := ""
	if store != nil && store.Slug != "central" {
		storeIDForVariants = store.ID
	}
	variants, _ := catalogRepo.GetProductVariantsForStore(ctx, p.ID, storeIDForVariants)
	storeDefaultID := ""
	if defaults, derr := catalogRepo.GetStoreVariantDefaults(ctx, storeIDForVariants, []string{p.ID}); derr == nil {
		storeDefaultID = defaults[p.ID]
	}
	base := buildProductResponse(p, res, variants, priceVariantsForResponse(p, variants, storeVal, settings, locks, overrides, displayCampaigns), storeDefaultID)
	base.CampaignName = pdpCampaignName
	storeCount, _ := catalogRepo.GetProductStoreCount(ctx, p.ID)
	// FIX 12: live's detail DTO has exactly 20 keys — no images/specs
	// keys at all (live detail for the bracelet carries only `image`).
	// Omitting them makes the spn route render exactly like the prod
	// route (single image, empty specs), which is live's prod-page
	// rendering. The DB values are untouched; this is the response
	// shape only.
	var images []string
	var specs map[string]interface{}
	// FIX 14: supplier fields only for an existing non-central store
	// view (see handleProductDetail). The supplier name lookup is
	// best-effort: an unknown supplier yields "" exactly as live
	// serves for sup-selersho-central.
	var supplierID, supplierName *string
	var baseCostB *int64
	// FIX 14b: PDP addons for the viewing store (see GetPDPAddons).
	// A nil store means the central view. The section title is served
	// only when at least one addon resolves.
	storeID := ""
	isCentral := true
	if store != nil {
		storeID = store.ID
		isCentral = storeVal.OwnerType == "central"
	}
	pdpAddons, addonSectionTitle, _ := catalogRepo.GetPDPAddons(ctx, storeID, isCentral, p.ID, p.CategorySlug, p.SupplierID, res.FinalPrice)
	if len(pdpAddons) == 0 {
		addonSectionTitle = ""
	}
	if showSupplier {
		exposeScopedVariantCosts(&base, variants, p.SupplierBaseCost)
		sid := p.SupplierID
		supplierID = &sid
		name, _ := catalogRepo.GetSupplierNameByID(ctx, p.SupplierID)
		supplierName = &name
		b := p.SupplierBaseCost
		baseCostB = &b
	}
	// The two non-pointer cost fields follow the same gate as the
	// pointer fields above: without a related caller (see
	// canSeeSupplierFields) platformFeeF is zeroed and supplierWarranty
	// is emptied, matching the central shape.
	platformFeeF := base.PlatformFeeF
	supplierWarranty := p.SupplierWarranty
	if !showSupplier {
		platformFeeF = 0
		supplierWarranty = ""
	}
	return ProductDetailResponse{
		ProductType: base.ProductType, SchemaVersion: base.SchemaVersion, ProductSchema: base.ProductSchema,
		ID:                base.ID,
		Title:             base.Title,
		Slug:              base.Slug,
		Category:          base.Category,
		OriginalPrice:     base.OriginalPrice,
		DiscountEligible:  base.DiscountEligible,
		DiscountPercent:   base.DiscountPercent,
		Price:             base.Price,
		PlatformFeeF:      platformFeeF,
		IsOriginal:        base.IsOriginal,
		Stock:             base.Stock,
		Image:             base.Image,
		Description:       base.Description,
		Warehouse:         base.Warehouse,
		SupplierWarranty:  supplierWarranty,
		NicheIDs:          base.NicheIDs,
		IsActive:          base.IsActive,
		StoreCount:        storeCount,
		Variants:          base.Variants,
		VariantPrices:     base.VariantPrices,
		PriceMin:          base.PriceMin,
		PriceMax:          base.PriceMax,
		StoreDefaultVariantID: base.StoreDefaultVariantID,
		Specs:             specs,
		Images:            images,
		Addons:            pdpAddons,
		AddonSectionTitle: addonSectionTitle,
		SpnNumber:         p.SpnNumber,
		SupplierID:        supplierID,
		SupplierName:      supplierName,
		BaseCostB:         baseCostB,
	}, nil
}

// handleProductDetail serves GET /api/v1/products/<id|slug> and the
// server-side spn- lookup (D46 decision: implement the lookup, so
// shared spn- links resolve instead of 404ing). The spn- form is
// /api/v1/products/spn-<n> (the frontend canonical path
// /product/spn-<n>/<slug>/?spnc=... is a SPA route owned by Team 08;
// the ?spnc= value selects a variant client-side from the variants
// array and is not a server filter: an unknown spnc still returns the
// product, it never 404s). The literal "price-bounds" path is routed
// here too as a safety net if the exact route registration is lost in
// integration merging.
func handleProductDetail(w http.ResponseWriter, r *http.Request, ctx context.Context, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, pricingRepo *repository.PricingRepository, subPath string, supplierRepos ...*repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")

	trimmed := strings.Trim(strings.TrimSuffix(subPath, "/"), "/")
	if trimmed == "price-bounds" {
		handleProductPriceBounds(w, r, ctx, catalogRepo, storeRepo, pricingRepo)
		return
	}
	// FIX 14: live serves GET /api/v1/products/<id>/reviews with
	// {"items":[],"rating":0,"reviewsCount":0,"total":0} for the bracelet
	// (probed live 2026-11-03); the candidate previously returned the
	// product detail for this path because only the first segment was
	// read. The reviews tab in the live frontend reads this route.
	if parts := strings.Split(trimmed, "/"); len(parts) == 2 && parts[1] == "reviews" && r.Method == http.MethodGet {
		handleProductReviews(w, r, ctx, catalogRepo, parts[0])
		return
	}
	// A slug segment after an spn- segment (defensive: the API path is
	// just spn-<n>, but a copied frontend path must not break it).
	key := strings.Split(trimmed, "/")[0]

	if catalogRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var supplierRepo *repository.SupplierRepository
	if len(supplierRepos) > 0 {
		supplierRepo = supplierRepos[0]
	}

	var p *domain.Product
	var err error
	if strings.HasPrefix(key, "spn-") {
		var spn int64
		spn, err = parseSpnSegment(key)
		if err == nil {
			p, err = catalogRepo.GetProductBySpn(ctx, spn)
		}
	} else {
		p, err = catalogRepo.GetProductByID(ctx, key)
	}
	if err != nil || p == nil {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	// Gap 12: the PUBLIC detail hides a suspended supplier's product
	// exactly like a missing one (both the id/slug and spn- paths land
	// here). GetProductByID itself stays ungated — order history,
	// warehouse, admin and supplier reads still resolve the product.
	if suspended, serr := catalogRepo.IsSupplierSuspended(ctx, p.SupplierID); serr == nil && suspended {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}

	settings, _ := loadPricingSettings(ctx, pricingRepo)
	storeSlug := r.URL.Query().Get("storeSlug")
	store := resolveListStore(ctx, storeRepo, storeSlug)
	// FIX 14: live detail rule, proven by public API probes 2026-11-03:
	// supplier fields appear ONLY for an existing NON-central store --
	// never for central even when explicitly requested, never for an
	// unknown slug. The old (storeSlug != "") rule leaked baseCostB
	// (e.g. bracelet 58,000,000) in the central view.
	// AUTH-KEYED (owner secrecy rule): as in the list, the store view
	// alone no longer suffices -- the caller must own the store,
	// supply the product, or be an admin (see canSeeSupplierFields).
	showSupplier := false
	if storeSlug != "" {
		if reqStore, _ := storeRepo.GetStoreBySlug(ctx, storeSlug); reqStore != nil && reqStore.Slug != "central" {
			showSupplier = resolveSupplierFieldsViewer(ctx, supplierRepo).canSeeSupplierFields(reqStore, p.SupplierID)
		}
	}

	detail, derr := assembleProductDetail(ctx, catalogRepo, pricingRepo, storeRepo, store, settings, *p, showSupplier)
	if derr != nil {
		http.Error(w, derr.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(detail)
}

// handleProductReviews serves GET /api/v1/products/<id|spn->/reviews
// (live shape, probed 2026-11-03). Non-empty rating/count semantics
// are best-effort: rating is the 1-decimal mean of published ratings,
// reviewsCount and total are the published count; the empty case
// matches live exactly (items [], rating 0, reviewsCount 0, total 0).
func handleProductReviews(w http.ResponseWriter, r *http.Request, ctx context.Context, catalogRepo *repository.CatalogRepository, key string) {
	w.Header().Set("Content-Type", "application/json")
	if catalogRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var p *domain.Product
	var err error
	if strings.HasPrefix(key, "spn-") {
		var spn int64
		if spn, err = parseSpnSegment(key); err == nil {
			p, err = catalogRepo.GetProductBySpn(ctx, spn)
		}
	} else {
		p, err = catalogRepo.GetProductByID(ctx, key)
	}
	if err != nil || p == nil {
		http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
		return
	}
	reviews, err := catalogRepo.GetProductReviews(ctx, p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if reviews == nil {
		reviews = []repository.ProductReview{}
	}
	var sum int64
	for _, rv := range reviews {
		sum += int64(rv.Rating)
	}
	var rating float64
	if len(reviews) > 0 {
		rating = math.Round(float64(sum)/float64(len(reviews))*10) / 10
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"items":        reviews,
		"rating":       rating,
		"reviewsCount": len(reviews),
		"total":        len(reviews),
	})
}

// productPriceBoundsResponse is the D05 shape: minPrice/maxPrice/count
// over active products only, with an exact category filter.
type productPriceBoundsResponse struct {
	MinPrice int64 `json:"minPrice"`
	MaxPrice int64 `json:"maxPrice"`
	Count    int   `json:"count"`
}

// computePriceBounds is the pure D05 computation over resolved (engine)
// prices. An empty set yields 0/0/0; no live empty-set shape was proven,
// so no other sentinel is invented.
func computePriceBounds(prices []int64) productPriceBoundsResponse {
	if len(prices) == 0 {
		return productPriceBoundsResponse{MinPrice: 0, MaxPrice: 0, Count: 0}
	}
	min, max := prices[0], prices[0]
	for _, v := range prices[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return productPriceBoundsResponse{MinPrice: min, MaxPrice: max, Count: len(prices)}
}

// handleProductPriceBounds implements GET /products/price-bounds (D05).
// Bounds are computed from the live percent engine's final prices over
// active products only (GetProducts enforces is_active), with the same
// exact category_slug match as the list. The live probe
// {198000, 63800000, 40} is a shape example, never hardcoded data.
// Storefront exception, mirroring handleProductsList: a ?storeSlug=
// naming an existing non-central dropshipper store scopes the priced
// set to that store's own assortment and applies its deals-widget
// campaign prices, so the bounds match the storefront card prices.
func handleProductPriceBounds(w http.ResponseWriter, r *http.Request, ctx context.Context, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, pricingRepo *repository.PricingRepository) {
	w.Header().Set("Content-Type", "application/json")
	if catalogRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	storeSlug := r.URL.Query().Get("storeSlug")
	filter := repository.ProductFilter{CategorySlug: r.URL.Query().Get("category")}
	// Mirror handleProductsList's store resolution (the explicitly
	// requested store, no central fallback): an existing non-central
	// dropshipper store scopes the priced set to its own assortment,
	// so the bounds describe the same products the storefront lists.
	// Central/unknown slugs keep the global active-catalog path,
	// byte-identical to before.
	var reqStore *domain.Store
	if storeSlug != "" {
		reqStore, _ = storeRepo.GetStoreBySlug(ctx, storeSlug)
	}
	if reqStore != nil && reqStore.Slug != "central" && reqStore.OwnerType == "dropshipper" {
		filter.StoreScoped = true
		filter.StoreProductIDs = reqStore.ActiveProductIDs
	}
	products, err := catalogRepo.GetProducts(ctx, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	settings, _ := loadPricingSettings(ctx, pricingRepo)
	store := resolveListStore(ctx, storeRepo, storeSlug)
	var storeVal domain.Store
	var locks []pricing.MarginLock
	if store != nil {
		storeVal = *store
		locks = loadStoreMarginLocks(ctx, pricingRepo, store.ID)
	}
	widgetCfg := loadDealsWidgetConfig(store)
	prices := make([]int64, 0, len(products))
	for _, p := range products {
		res := pricing.ResolveReferencePricing(p, storeVal, settings, locks)
		res = applyDealsWidgetDiscountForDisplay(store, widgetCfg, p.ID, res)
		prices = append(prices, res.FinalPrice)
	}
	json.NewEncoder(w).Encode(computePriceBounds(prices))
}

func handleOwnedStoresList(w http.ResponseWriter, r *http.Request, ctx context.Context, storeRepo *repository.StoreRepository) {
	w.Header().Set("Content-Type", "application/json")
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	stores, err := storeRepo.GetStoresByOwner(ctx, claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(stores)
}

func handleStoresList(w http.ResponseWriter, r *http.Request, ctx context.Context, storeRepo *repository.StoreRepository) {
	w.Header().Set("Content-Type", "application/json")
	if storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	stores, err := storeRepo.GetAllStores(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// FIX 9: the public list returns only active stores, like live
	// (live returns 4 active; the single-store endpoint still serves
	// suspended stores per D45). This is a code filter only; the DB
	// keeps all rows.
	active := stores[:0]
	for i := range stores {
		if stores[i].Status == "active" {
			active = append(active, stores[i])
		}
	}
	// Team 05 (Phase 2, D52, as amended by FIX3): the public list
	// emits the same public keys per store as the single-store
	// response (12 live keys + templateId/templateConfig/logo).
	json.NewEncoder(w).Encode(team05PublicStoreList(active))
}

func authorizeStoreRead(w http.ResponseWriter, r *http.Request, jwtService *auth.JWTService, storeRepo *repository.StoreRepository, slug string) bool {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	claims, err := jwtService.ValidateToken(strings.TrimPrefix(authHeader, "Bearer "))
	if err != nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	if err := jwtService.RefreshCapabilities(r.Context(), claims); err != nil {
		http.Error(w, `{"error":"Authorization state unavailable"}`, http.StatusServiceUnavailable)
		return false
	}
	// ISO-E: AuthMiddleware denies blocked/inactive users on every guarded
	// route, but this helper re-validates the token itself (the /stores/
	// sub-routes are dispatched inside an otherwise unguarded handler), so
	// a blocked seller could previously still read their own store orders,
	// referrals and returns through /stores/<slug>/orders|referrals|
	// referral-notifications|returns. Apply the same gate, with the same
	// strings/statuses as AuthMiddleware. A nil/non-implementing reader
	// keeps the previous behaviour (IsUserBlocked returns false, nil).
	blocked, err := jwtService.IsUserBlocked(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, `{"error":"Authorization state unavailable"}`, http.StatusServiceUnavailable)
		return false
	}
	if blocked {
		http.Error(w, `{"error":"Account is disabled"}`, http.StatusForbidden)
		return false
	}
	store, err := storeRepo.GetStoreBySlug(r.Context(), slug)
	if err != nil || store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return false
	}
	if !claims.HasDirectCapability("admin") && (store.OwnerUserID == nil || *store.OwnerUserID != claims.UserID) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return false
	}
	return true
}

func handleRegisterStore(w http.ResponseWriter, r *http.Request, ctx context.Context, storeRepo *repository.StoreRepository, capabilityRepos ...*repository.CapabilityRepository) {
	w.Header().Set("Content-Type", "application/json")
	if storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	var req struct {
		StoreName           string  `json:"storeName"`
		Slug                string  `json:"slug"`
		OwnerName           string  `json:"ownerName"`
		ProfitMarginPercent float64 `json:"profitMarginPercent"`
		ReferralCode        string  `json:"referralCode"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if req.StoreName == "" || req.Slug == "" {
		http.Error(w, `{"error":"Missing required fields"}`, http.StatusBadRequest)
		return
	}

	cleanSlug := strings.ToLower(strings.TrimSpace(req.Slug))
	existing, _ := storeRepo.GetStoreBySlug(ctx, cleanSlug)
	if existing != nil {
		http.Error(w, `{"error":"Store slug already taken"}`, http.StatusConflict)
		return
	}

	ownedStores, err := storeRepo.GetStoresByOwner(ctx, claims.UserID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	maxStores := 1
	activePlanID := "trial"
	for _, store := range ownedStores {
		if store.ActivePlanID == "vip" {
			maxStores = 10
			activePlanID = "vip"
			break
		}
	}
	if len(ownedStores) >= maxStores {
		http.Error(w, fmt.Sprintf(`{"error":"Store limit reached for your plan (%d stores)"}`, maxStores), http.StatusConflict)
		return
	}

	// Cryptographically secure referral code generation (>=8 chars, random uppercase alphanum)
	randSuffix := strings.ToUpper(uuid.New().String()[:8])
	uniqueReferralCode := fmt.Sprintf("DRUP-%s", randSuffix)

	newStore := domain.Store{
		ID:                    fmt.Sprintf("store-%s", uuid.New().String()),
		Name:                  req.StoreName,
		Slug:                  cleanSlug,
		OwnerType:             "dropshipper",
		OwnerName:             claims.FullName,
		OwnerUserID:           &claims.UserID,
		ProfitMarginPercent:   req.ProfitMarginPercent,
		ActivePlanID:          activePlanID,
		ReferralCode:          uniqueReferralCode,
		SignedContractVersion: 2,
	}
	if req.ProfitMarginPercent <= 0 {
		newStore.ProfitMarginPercent = 12.0
	}

	// Resolve referral code if provided (avoid self-referral)
	referrerStoreID := ""
	if strings.TrimSpace(req.ReferralCode) != "" {
		refStore, _ := storeRepo.GetStoreByReferralCode(ctx, req.ReferralCode)
		if refStore != nil {
			// Prevent self-referral
			if refStore.OwnerUserID == nil || *refStore.OwnerUserID != claims.UserID {
				referrerStoreID = refStore.ID
				newStore.ReferrerSellerID = &refStore.ID
			}
		}
	}

	if err := storeRepo.CreateStoreWithBonus(ctx, newStore, referrerStoreID); err != nil {
		writeInternalError(w, err)
		return
	}
	if len(capabilityRepos) > 0 && capabilityRepos[0] != nil {
		if err := capabilityRepos[0].Grant(ctx, claims.UserID, "seller", "store_ownership", newStore.ID, nil); err != nil {
			writeInternalError(w, err)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newStore)
}

func handleStoreOverrides(w http.ResponseWriter, r *http.Request, ctx context.Context, slug string, storeRepo *repository.StoreRepository) {
	w.Header().Set("Content-Type", "application/json")
	if storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}

	store, err := storeRepo.GetStoreBySlug(ctx, slug)
	if err != nil || store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if !claims.HasCapability("admin") && (store.OwnerUserID == nil || *store.OwnerUserID != claims.UserID) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	var req struct {
		Type                string  `json:"type"` // "product" | "niche" | "store_margin"
		TargetID            string  `json:"targetId"`
		Value               float64 `json:"value"`
		Mode                string  `json:"mode"` // "percent" only; fixed is rejected for product/niche sets
		ProfitMarginPercent float64 `json:"profitMarginPercent"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid payload"}`, http.StatusBadRequest)
		return
	}

	// Percent-only: product/niche sets must carry mode "percent" (empty
	// means percent). Deletes (value <= 0) ignore mode. This covers the
	// admin direct-setter path; the seller path is double-covered by
	// SaveOwnedPricing. Fixed values are rejected, never stored.
	if (req.Type == "product" || req.Type == "niche") && req.Value > 0 {
		if req.Mode == "" {
			req.Mode = "percent"
		} else if req.Mode != "percent" {
			http.Error(w, `{"error":"Fixed margin mode is not supported; margins are percent-only"}`, http.StatusBadRequest)
			return
		}
	}

	// ISO-C: seller calls route through SaveOwnedPricing so the admin
	// margin lock, the [floor, ceiling] range (including per-product
	// ceilings), and value sanity are enforced server-side. The old
	// direct setters performed no validation at all. Admin-capability
	// callers keep the direct setters: setting a margin as the platform
	// is a platform action, like an admin_set lock.
	isAdmin := claims.HasCapability("admin")
	savePricing := func(kind, target, mode string, value float64) error {
		if isAdmin {
			switch kind {
			case "store_margin":
				return storeRepo.SetStoreMargin(ctx, store.ID, value)
			case "product":
				return storeRepo.SetStoreProductOverride(ctx, store.ID, target, value, mode)
			case "niche":
				return storeRepo.SetStoreNicheOverride(ctx, store.ID, target, value, mode)
			}
			return nil
		}
		return storeRepo.SaveOwnedPricing(ctx, claims.UserID, slug, kind, target, mode, value)
	}
	writePricingErr := func(err error) {
		if errors.Is(err, repository.ErrStoreForbidden) {
			http.Error(w, `{"error":"Store pricing is locked by admin"}`, http.StatusForbidden)
		} else if errors.Is(err, repository.ErrStoreInput) {
			http.Error(w, `{"error":"Invalid pricing value"}`, http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	if req.Type == "store_margin" || (req.ProfitMarginPercent > 0 && req.Type == "") {
		margin := req.Value
		if req.ProfitMarginPercent > 0 {
			margin = req.ProfitMarginPercent
		}
		if err := savePricing("store_margin", "", "percent", margin); err != nil {
			writePricingErr(err)
			return
		}
	} else if req.Type == "product" {
		if err := savePricing("product", req.TargetID, req.Mode, req.Value); err != nil {
			writePricingErr(err)
			return
		}
	} else if req.Type == "niche" {
		if err := savePricing("niche", req.TargetID, req.Mode, req.Value); err != nil {
			writePricingErr(err)
			return
		}
	} else {
		http.Error(w, `{"error":"Invalid pricing type"}`, http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"storeId": store.ID,
		"message": "Pricing override saved successfully",
	})
}
