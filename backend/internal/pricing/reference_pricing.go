// Package pricing — live-parity reference pricing engine (Phase 2, Team 02).
//
// This file REPLACES the source pricing model for every product/order price
// path. The old model (legacy_pricing.go, formerly pricing.go) computed
// P = RoundTo100((C + F) * (1 + margin)) with an absolute platform fee F.
// The live 2026-11-02 build does not contain that model at all
// (ResolvePricing / ResolvePlatformFeeF occur 0 times in the live binary)
// and instead prices percent-only:
//
//	P = max(referenceCost, RoundTo100(referenceCost * (1 + margin)))
//	referenceCost = max(C, RoundTo100(C * (1 + platformPct/100)))
//	platformFeeF (reported) = referenceCost - C
//	baseCostB (reported, dropshipper view) = referenceCost
//
// FIN-012 versions the nonnegative floor and exact integer rounding. With zero
// platform percent the fee remains zero for costs divisible by 100; other
// costs follow the floor contract in money.go and docs/architecture/money-policy.md.
//
// Proven live examples (MASTER-AUDIT section 3(a), team-02 report section 3.1):
//   - prod-earbuds-pro, C=1,200,000: central 1,320,000 (x1.10), dropshipper
//     1,380,000 (x1.15), platformFeeF=0, baseCostB=1,200,000.
//   - prod-vitamin-serum, C=420,000, product percent override 7%:
//     449,400 = 420,000 x 1.07.
//   - prod-1793551155965 (demo gold), C=58,000,000: central 63,800,000
//     (x1.10), dropshipper 66,700,000 (x1.15), platformFeeF=0.
//   - discountPercent is DERIVED: round((OriginalPrice - P) / OriginalPrice
//   - 100). It is not a seller discount (see D48 note below).
//
// Absolute fee overrides are DEAD in live (D30): supplier_fee_overrides
// holds one historical 120,000 row and product_fee_overrides is empty, and
// the percent-only engine rejects absolute overrides. Those values are
// carried in PlatformPricingSettings only as historical data and are NEVER
// read by any function in this file.
//
// Central price: P = C x 1.10 with central_profit_pct = 10 (D26). Do NOT
// confuse this with central_store_markup_percent = 15, which is stored on
// the settings struct for reference only; its precedence in the ORDER path
// is unverified (TODO U12) and it is never used here.
//
// Discount campaigns (D48): seller_discount_campaigns CRUD exists in live
// but price application is NOT proven (TODO U04). This engine deliberately
// does NOT apply discount campaigns. It also does NOT implement
// seller_discount_codes or a discount-quote endpoint (branch-only, not
// live). orders.discount_amount stays OriginalPrice minus final price; it
// is not a seller discount.
//
// Historical orders: NEVER recompute them with this formula. order_items
// snapshots are the reference for history (live history contains a fixed
// 100,000 fee regime, a 30% percent regime, and a return to fixed — none
// of which is today's formula).
package pricing

import (
	"math/big"

	"github.com/drup/platform/internal/domain"
)

// PlatformPricingSettings is the pricing-relevant subset of platform
// settings, loaded from platform_settings (id = 1) plus the platform_pct
// ladder tables by repository.PricingRepository. It intentionally does
// NOT reuse domain.PlatformSettings: that struct lives in
// backend/internal/domain/models.go, which is outside Team 02 ownership,
// and it does not carry the percent-engine columns.
type PlatformPricingSettings struct {
	// Percent engine (D24/D26).
	PlatformPct      float64 `json:"platformPct"`      // global platform_pct
	CentralProfitPct float64 `json:"centralProfitPct"` // central_profit_pct, live = 10
	IncreaseCapPct   float64 `json:"increaseCapPct"`   // increase_cap_pct
	IncreaseLocked   bool    `json:"increaseLocked"`   // increase_locked
	MaxProfitMargin  float64 `json:"maxProfitMargin"`  // max_profit_margin
	MinProfitMargin  float64 `json:"minProfitMargin"`  // min_profit_margin, live = 5

	// Store default margin chain.
	DefaultMargin float64 `json:"defaultMargin"` // default_margin, live = 20

	// Ladder overrides (D24): product > category > global.
	ProductPlatformPcts  map[string]float64 `json:"productPlatformPcts,omitempty"`
	CategoryPlatformPcts map[string]float64 `json:"categoryPlatformPcts,omitempty"`
	// Per-product margin ceilings (ISO-C margin model): when a product has
	// a ceiling row, it REPLACES the global MaxProfitMargin for that product.
	ProductMarginCeilings map[string]float64 `json:"productMarginCeilings,omitempty"`

	// Stored but NOT used as price inputs (see notes on each field).
	// TODO U12: central_store_markup_percent = 15 in live, but the
	// storefront price uses central_profit_pct = 10; precedence in the
	// order path is unverified, so this value is never applied here.
	CentralStoreMarkupPercent float64 `json:"centralStoreMarkupPercent,omitempty"`
	// TODO U13: increase_cap_pct / increase_locked / market ceilings /
	// platform-pct exceptions are not applied to storefront prices here;
	// their tables are empty in live and no behaviour was proven.
	// TODO U27: the "12% central" figure seen in one audit note is the
	// same x1.10 phenomenon viewed against OriginalPrice (63,800,000 /
	// 72,500,000 = 88%), NOT a separate discount rule. No 12% rule exists
	// in this file.

	// Referral (D33): live has referral_is_enabled = false. The flag is
	// respected by callers; the central commission base (fee + margin)
	// is the source formula and is NOT proven for live re-enablement.
	ReferralIsEnabled         bool    `json:"referralIsEnabled"`
	ReferralCommissionPercent float64 `json:"referralCommissionPercent"`

	// Settlement settings DATA only (manual engine boundary): these values
	// are read from the live row (settlement_day = 10,
	// early_withdrawal_fee_pct = 5.00, set 2026-11-03) but no settlement
	// cycle has ever run in live, so they are settings data, not proven
	// live behaviour. No auto monthly-sweep worker is created by this
	// package, and the early-withdrawal minimum is NOT hard-coded here:
	// live wallet_min_withdrawal = 0 and the branch-only 1,000,000 figure
	// is deliberately not treated as live behaviour (TODO U03).
	SettlementDay         *int    `json:"settlementDay,omitempty"`
	EarlyWithdrawalFeePct float64 `json:"earlyWithdrawalFeePct,omitempty"`
	WalletMinWithdrawal   int64   `json:"walletMinWithdrawal"`
	WalletSpendEnabled    bool    `json:"walletSpendEnabled"`
	WalletWithdrawEnabled bool    `json:"walletWithdrawEnabled"`

	// Percent-only: the historical absolute overrides (D30) are NOT
	// part of price inputs. They live only in the FeeOverridesHistory
	// read path (GetFeeOverridesHistory); the pricing load path never
	// reads them, and write paths reject fixed values.
}

// DefaultPlatformPricingSettings returns the fallback used only when the
// settings row cannot be loaded at all. It mirrors the live migration
// defaults (platform_pct = 0, central_profit_pct = 10, default margin 20)
// and the observed live row (referral disabled). It is a fallback, not an
// admin value: a successfully loaded row always wins over this.
func DefaultPlatformPricingSettings() *PlatformPricingSettings {
	return &PlatformPricingSettings{
		PlatformPct:           0,
		CentralProfitPct:      10,
		IncreaseCapPct:        10,
		IncreaseLocked:        true,
		MaxProfitMargin:       35,
		MinProfitMargin:       5,
		DefaultMargin:         20,
		ReferralIsEnabled:     false,
		WalletMinWithdrawal:   0,
		WalletSpendEnabled:    true,
		WalletWithdrawEnabled: true,
	}
}

// OrDefault returns s, or the fallback above when s is nil.
func OrDefault(s *PlatformPricingSettings) *PlatformPricingSettings {
	if s == nil {
		return DefaultPlatformPricingSettings()
	}
	return s
}

// ResolvePlatformPct implements the live percent ladder (D24):
// product (platform_pct_products) > category (platform_pct_categories) >
// global (platform_pct). The returned source is "product", "category" or
// "global". Today both ladder tables are empty in live, so the global
// platform_pct = 0 applies and the reported platform fee is 0.
func ResolvePlatformPct(productID, categorySlug string, settings *PlatformPricingSettings) (pct float64, source string) {
	settings = OrDefault(settings)
	if settings.ProductPlatformPcts != nil {
		if v, ok := settings.ProductPlatformPcts[productID]; ok {
			return v, "product"
		}
	}
	if categorySlug != "" && settings.CategoryPlatformPcts != nil {
		if v, ok := settings.CategoryPlatformPcts[categorySlug]; ok {
			return v, "category"
		}
	}
	return settings.PlatformPct, "global"
}

// EffectiveMarginCeiling returns the ceiling that binds a seller-set
// margin for one product (ISO-C margin model). A per-product ceiling
// row, when present, REPLACES the global MaxProfitMargin for that
// product; otherwise the global value binds. Per-product ceilings are
// platform-set via the admin product-pricing endpoint.
func EffectiveMarginCeiling(productID string, settings *PlatformPricingSettings) float64 {
	settings = OrDefault(settings)
	if settings.ProductMarginCeilings != nil {
		if v, ok := settings.ProductMarginCeilings[productID]; ok {
			return v
		}
	}
	return settings.MaxProfitMargin
}

// capSellerMargin clamps a seller-set margin to the binding ceiling.
// Admin-set locks (tier "product-locked") are exempt: the lock IS the
// platform's explicit decision for that scope. Central profit is never
// passed through here.
func capSellerMargin(margin float64, tier, productID string, settings *PlatformPricingSettings) float64 {
	if tier == "product-locked" {
		return margin
	}
	if ceiling := EffectiveMarginCeiling(productID, settings); margin > ceiling {
		return ceiling
	}
	return margin
}

// MarginLock is one row of store_margin_locks (D29). Modes are exactly
// "freeze" and "admin_set" (the only modes confirmed in the live binary);
// no other lock semantics are invented here.
type MarginLock struct {
	Scope   string   `json:"scope"`   // "product" | "niche" | "variant"
	ScopeID string   `json:"scopeId"` // product_id, niche_id or variant_id
	Mode    string   `json:"mode"`    // "freeze" | "admin_set"
	Value   *float64 `json:"value"`   // required for admin_set, nil for freeze
}

// StoreVariantOverride is one row of store_variant_overrides (D28).
type StoreVariantOverride struct {
	StoreID   string  `json:"storeId"`
	ProductID string  `json:"productId"`
	VariantID string  `json:"variantId"`
	Value     float64 `json:"value"` // percent (percent-only)
	Mode      string  `json:"mode"`  // "percent" only; fixed is rejected at write time
}

// ReferenceResolution is the complete live-model price breakdown.
type ReferenceResolution struct {
	MoneyPolicyVersion string `json:"moneyPolicyVersion"`
	PricingError       string `json:"pricingError,omitempty"`

	WholesaleCost     int64   `json:"wholesaleCost"` // C
	PlatformPct       float64 `json:"platformPct"`
	PlatformPctSource string  `json:"platformPctSource"`
	ReferenceCost     int64   `json:"referenceCost"` // R = RoundTo100(C * (1+pct/100))
	PlatformFee       int64   `json:"platformFee"`   // F = R - C; always nonnegative
	BaseCost          int64   `json:"baseCost"`      // live baseCostB = R, with cost floor
	SellerMargin      int64   `json:"sellerMargin"`  // D = P - R (dropshipper margin, or central profit for central stores)
	FinalPrice        int64   `json:"finalPrice"`    // P
	OriginalPrice     int64   `json:"originalPrice"`
	ActiveTier        string  `json:"activeTier"`    // "central" | "product" | "product-locked" | "niche" | "variant" | "default" | "max-default"
	PricingMode       string  `json:"pricingMode"`   // "percent" only (percent-only platform)
	MarginPercent     float64 `json:"marginPercent"` // applied margin percent
	// AdminLocked is true when an admin margin lock determined the
	// margin: a product or variant admin_set lock, or the legacy admin
	// store lock. It is never serialized — the tier string alone cannot
	// carry this fact, because the variant admin-lock branch reports
	// ActiveTier "variant" (the same string as a seller variant
	// override) and the legacy store lock reports "default". Discount
	// layers must leave a locked resolution's price untouched.
	AdminLocked bool `json:"-"`
	// UnverifiedNote names the TODO that limits this resolution, if any
	// (currently only the U10 niche fallback; fixed rows are inert under
	// percent-only). Empty means the resolution used only proven rules.
	UnverifiedNote string `json:"unverifiedNote,omitempty"`
}

// DiscountPercent derives the display discount from OriginalPrice (D21/D26):
// round((OriginalPrice - P) / OriginalPrice * 100), floored at 0. It is a
// derived ratio, not a discount granted to anybody.
func DiscountPercent(originalPrice, finalPrice int64) int64 {
	if originalPrice <= 0 || finalPrice >= originalPrice {
		return 0
	}
	if finalPrice < 0 {
		return 0
	}
	n := new(big.Int).Mul(big.NewInt(originalPrice-finalPrice), big.NewInt(100))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(originalPrice), rem)
	if new(big.Int).Lsh(rem, 1).Cmp(big.NewInt(originalPrice)) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}

// applyDropshipperOverride applies one seller override to the current
// margin selection. Only percent overrides are applied. Percent-only:
// a "fixed" override can only come from a legacy row (write paths reject
// fixed); it is never silently converted to a percent or an amount.
// It returns the new margin, tier, mode and, when the override could not
// be applied, a note explaining the fallback.
func applyDropshipperOverride(currentMargin float64, currentTier string, value float64, mode, tier string) (float64, string, string, string) {
	if mode == "fixed" {
		// Percent-only defensive branch: legacy fixed rows are inert.
		// Do not guess their arithmetic. Keep the previous selection.
		return currentMargin, currentTier, "percent", "U10: fixed-mode override present but not applied; previous margin kept"
	}
	return value, tier, "percent", ""
}

// selectDropshipperMarginForProduct resolves the dropshipper margin percent.
// Precedence (proven parts first, unverified parts explicitly TODO):
//  1. admin_set margin lock for the product (D29) — replaces the seller
//     override for that scope. A freeze lock keeps the seller value, so
//     resolution falls through to the seller override below.
//  2. product percent override (proven: 7% and 15% rows in live).
//  3. admin custom store margin override / legacy boolean lock
//     (read compatibility only, D29): when IsMarginLockedByAdmin is set
//     with a CustomStoreMarginOverride, that value wins over the store
//     default, as in the source lock behaviour.
//  4. max-allowed fallback (owner rule, 2026-11-05): there is NO
//     store-wide default profit anymore. When nothing above set a
//     percent, the margin is the maximum allowed for the product — its
//     per-product ceiling row when present, else settings
//     MaxProfitMargin (tier "max-default"). Store ProfitMarginPercent
//     and settings DefaultMargin no longer influence pricing, and
//     CustomStoreMarginOverride reaches pricing only through the
//     tier-3 legacy ADMIN lock above, never as a seller default.
//
// Niche overrides are NOT applied: TODO U10 (the niche table is empty in
// live and no niche behaviour was observed). If a niche override exists
// and nothing more specific won, the resolution notes U10 and uses the
// max-allowed fallback instead of guessing niche precedence.
// Ceiling (ISO-C margin model): the margin selected here is capped at
// compute time by capSellerMargin in ResolveReferencePricing (and in the
// variant-override branch): a per-product ceiling row replaces the global
// MaxProfitMargin for that product; admin_set locks are exempt.
// The locked return reports whether an admin lock (steps 1 or 3)
// determined the margin; it becomes ReferenceResolution.AdminLocked.
func selectDropshipperMarginForProduct(product domain.Product, store domain.Store, settings *PlatformPricingSettings, locks []MarginLock) (margin float64, tier string, mode string, note string, locked bool) {
	settings = OrDefault(settings)

	// Tier 4 (owner rule, 2026-11-05): the max-allowed fallback — the
	// per-product ceiling row when present, else the global
	// MaxProfitMargin. No store default participates anymore.
	def := EffectiveMarginCeiling(product.ID, settings)

	// 1. admin_set product lock for this product.
	for _, l := range locks {
		if l.Scope == "product" && l.ScopeID == product.ID && l.Mode == "admin_set" && l.Value != nil {
			return *l.Value, "product-locked", "percent", "", true
		}
	}

	// 2. product percent override.
	if store.ProductPriceOverrides != nil {
		if v, ok := store.ProductPriceOverrides[product.ID]; ok {
			mode := "percent"
			if store.ProductPriceOverrideModes != nil {
				if m, ok2 := store.ProductPriceOverrideModes[product.ID]; ok2 && m != "" {
					mode = m
				}
			}
			m, t, md, n := applyDropshipperOverride(def, "default", v, mode, "product")
			if n == "" {
				return m, t, md, "", false
			}
			// Fixed override: TODO U10 — fall through to the
			// max-allowed fallback (or the legacy lock below) and
			// keep the note.
			note = n
			_ = m
			_ = t
			_ = md
			// Continue to lock/fallback below with the note attached.
			if store.IsMarginLockedByAdmin && store.CustomStoreMarginOverride != nil {
				return *store.CustomStoreMarginOverride, "default", "percent", note, true
			}
			return def, "max-default", "percent", note, false
		}
	}

	// 3. legacy boolean lock, read compatibility only (D29).
	if store.IsMarginLockedByAdmin && store.CustomStoreMarginOverride != nil {
		return *store.CustomStoreMarginOverride, "default", "percent", "", true
	}

	// 4. max-allowed fallback (tier "max-default"). Niche overrides, if
	// any, are deliberately not applied (TODO U10).
	if len(store.NichePriceOverrides) > 0 {
		for _, nid := range product.NicheIDs {
			if _, ok := store.NichePriceOverrides[nid]; ok {
				return def, "max-default", "percent", "U10: niche override present but not applied; max allowed margin used", false
			}
		}
	}
	return def, "max-default", "percent", "", false
}

// ResolveReferencePricing is the live percent engine (D23/D24/D26/D27).
// Central stores (any OwnerType other than "dropshipper", including a nil
// store) price at central_profit_pct; dropshippers price at the margin
// selected by selectDropshipperMarginForProduct, on the reference cost R,
// NOT on C + F. central_store_markup_percent is never consulted here
// (TODO U12 for the order path).
func ResolveReferencePricing(product domain.Product, store domain.Store, settings *PlatformPricingSettings, locks []MarginLock) ReferenceResolution {
	settings = OrDefault(settings)
	if product.OriginalPrice < 0 || product.OriginalPrice > MaxTransportMoney || product.SupplierBaseCost > MaxTransportMoney {
		return invalidResolution(product.SupplierBaseCost)
	}
	pct, pctSource := ResolvePlatformPct(product.ID, product.CategorySlug, settings)
	referenceCost, err := MarkedUpPrice(product.SupplierBaseCost, pct)
	if err != nil {
		return invalidResolution(product.SupplierBaseCost)
	}

	var margin float64
	var tier, mode, note string
	var locked bool
	if store.OwnerType == "dropshipper" {
		margin, tier, mode, note, locked = selectDropshipperMarginForProduct(product, store, settings, locks)
		margin = capSellerMargin(margin, tier, product.ID, settings)
	} else {
		margin = settings.CentralProfitPct
		tier = "central"
		mode = "percent"
	}

	finalPrice, err := MarkedUpPrice(referenceCost, margin)
	if err != nil {
		return invalidResolution(product.SupplierBaseCost)
	}
	if finalPrice > MaxTransportMoney {
		return invalidResolution(product.SupplierBaseCost)
	}
	originalPrice := product.OriginalPrice
	if finalPrice > originalPrice {
		originalPrice = finalPrice
	}
	return ReferenceResolution{
		MoneyPolicyVersion: MoneyPolicyVersion,
		WholesaleCost:      product.SupplierBaseCost,
		PlatformPct:        pct,
		PlatformPctSource:  pctSource,
		ReferenceCost:      referenceCost,
		PlatformFee:        referenceCost - product.SupplierBaseCost,
		BaseCost:           referenceCost,
		SellerMargin:       finalPrice - referenceCost,
		FinalPrice:         finalPrice,
		OriginalPrice:      originalPrice,
		ActiveTier:         tier,
		PricingMode:        mode,
		MarginPercent:      margin,
		AdminLocked:        locked,
		UnverifiedNote:     note,
	}
}

// resolveVariantAtMargin builds the resolution for one variant priced at
// an explicit margin percent on the variant's own cost, under the normal
// platform-pct ladder. variantProduct must already carry the variant cost
// as SupplierBaseCost.
func resolveVariantAtMargin(variantProduct domain.Product, variantCost int64, margin float64, tier string, settings *PlatformPricingSettings) ReferenceResolution {
	pct, pctSource := ResolvePlatformPct(variantProduct.ID, variantProduct.CategorySlug, settings)
	referenceCost, err := MarkedUpPrice(variantCost, pct)
	if err != nil {
		return invalidResolution(variantCost)
	}
	finalPrice, err := MarkedUpPrice(referenceCost, margin)
	if err != nil {
		return invalidResolution(variantCost)
	}
	if finalPrice > MaxTransportMoney || variantProduct.OriginalPrice < 0 || variantProduct.OriginalPrice > MaxTransportMoney {
		return invalidResolution(variantCost)
	}
	originalPrice := variantProduct.OriginalPrice
	if finalPrice > originalPrice {
		originalPrice = finalPrice
	}
	return ReferenceResolution{
		MoneyPolicyVersion: MoneyPolicyVersion,
		WholesaleCost:      variantCost,
		PlatformPct:        pct,
		PlatformPctSource:  pctSource,
		ReferenceCost:      referenceCost,
		PlatformFee:        referenceCost - variantCost,
		BaseCost:           referenceCost,
		SellerMargin:       finalPrice - referenceCost,
		FinalPrice:         finalPrice,
		OriginalPrice:      originalPrice,
		ActiveTier:         tier,
		PricingMode:        "percent",
		MarginPercent:      margin,
	}
}

// ResolveReferencePricingForStoreProductVariantWithCost prices one variant
// (D28) from the variant's own supplier_base_cost. A percent variant
// override (store_variant_overrides, keyed by variant_id) wins for that
// variant. When NO percent variant override exists — no override row, or
// a legacy fixed row, which is inert under percent-only — the variant
// does NOT inherit the product override and there is no store default:
// a dropshipper variant prices at the MAX ALLOWED margin for the product
// (per-product ceiling when present, else MaxProfitMargin), computed on
// the variant cost with the normal platform pct (owner rule,
// 2026-11-05). Variant admin_set locks behave like product locks for
// that variant, and a product admin_set lock binds its variants; both
// keep winning over every seller value. Central stores keep central
// pricing via the product resolution.
func ResolveReferencePricingForStoreProductVariantWithCost(product domain.Product, variantCost int64, variantID string, store domain.Store, settings *PlatformPricingSettings, locks []MarginLock, variantOverride *StoreVariantOverride) ReferenceResolution {
	settings = OrDefault(settings)
	// A variant admin_set lock replaces every seller override for it.
	for _, l := range locks {
		if l.Scope == "variant" && l.ScopeID == variantID && l.Mode == "admin_set" && l.Value != nil {
			variantProduct := product
			variantProduct.SupplierBaseCost = variantCost
			base := ResolveReferencePricing(variantProduct, store, settings, nil)
			// Recompute with the locked margin, keeping the same pct.
			if base.PricingError != "" {
				return base
			}
			finalPrice, err := MarkedUpPrice(base.ReferenceCost, *l.Value)
			if err != nil || finalPrice > MaxTransportMoney {
				return invalidResolution(variantCost)
			}
			base.SellerMargin = finalPrice - base.ReferenceCost
			base.FinalPrice = finalPrice
			base.ActiveTier = "variant"
			base.PricingMode = "percent"
			base.MarginPercent = *l.Value
			base.AdminLocked = true
			if finalPrice > base.OriginalPrice {
				base.OriginalPrice = finalPrice
			}
			return base
		}
	}
	// A product admin_set lock binds variants of that product too: a
	// variant override must not bypass the product lock (ISO-C).
	for _, l := range locks {
		if l.Scope == "product" && l.ScopeID == product.ID && l.Mode == "admin_set" && l.Value != nil {
			variantProduct := product
			variantProduct.SupplierBaseCost = variantCost
			base := ResolveReferencePricing(variantProduct, store, settings, nil)
			// Recompute with the locked margin, keeping the same pct.
			if base.PricingError != "" {
				return base
			}
			finalPrice, err := MarkedUpPrice(base.ReferenceCost, *l.Value)
			if err != nil || finalPrice > MaxTransportMoney {
				return invalidResolution(variantCost)
			}
			base.SellerMargin = finalPrice - base.ReferenceCost
			base.FinalPrice = finalPrice
			base.ActiveTier = "product-locked"
			base.PricingMode = "percent"
			base.MarginPercent = *l.Value
			base.AdminLocked = true
			if finalPrice > base.OriginalPrice {
				base.OriginalPrice = finalPrice
			}
			return base
		}
	}
	if variantOverride != nil && variantOverride.VariantID == variantID {
		variantProduct := product
		variantProduct.SupplierBaseCost = variantCost
		if variantOverride.Mode == "fixed" {
			// Percent-only defensive branch: legacy fixed rows are inert.
			if store.OwnerType == "dropshipper" {
				// Owner rule (2026-11-05): with no percent variant
				// override, the variant resolves at the MAX ALLOWED
				// margin for the product on the variant cost — never
				// the product resolution (U10: the fixed row itself
				// is still not applied).
				res := resolveVariantAtMargin(variantProduct, variantCost, EffectiveMarginCeiling(product.ID, settings), "max-default", settings)
				res.UnverifiedNote = "U10: fixed-mode variant override present but not applied; max allowed margin used"
				return res
			}
			res := ResolveReferencePricing(variantProduct, store, settings, locks)
			res.UnverifiedNote = "U10: fixed-mode variant override present but not applied; product resolution used"
			return res
		}
		cappedValue := capSellerMargin(variantOverride.Value, "variant", variantProduct.ID, settings)
		return resolveVariantAtMargin(variantProduct, variantCost, cappedValue, "variant", settings)
	}
	variantProduct := product
	variantProduct.SupplierBaseCost = variantCost
	if store.OwnerType == "dropshipper" {
		// Owner rule (2026-11-05): no variant percent override — the
		// variant prices at the MAX ALLOWED margin for the product,
		// computed on the variant cost. It does NOT inherit the product
		// override, and no store default exists anymore.
		return resolveVariantAtMargin(variantProduct, variantCost, EffectiveMarginCeiling(product.ID, settings), "max-default", settings)
	}
	return ResolveReferencePricing(variantProduct, store, settings, locks)
}

// ProfitSplit decomposes a resolved price into its money parts.
// PlatformFeeAmount is the platform-pct portion (R - C). For a
// dropshipper, the remaining margin (P - R) is the dropshipper's; for a
// central store it is the platform's central profit, so PlatformTotal is
// the pct portion plus the central profit. The identity
// P = C + PlatformFeeAmount + DropshipperMargin + CentralProfit always
// holds (with exactly one of the last two non-zero).
//
// TODO U14: the live ORDER-path profit split when platform_pct = 0 was
// not proven (no order could be created during the audit). This function
// is the arithmetic decomposition of the storefront price, not a proven
// live order-accounting rule; order code that uses it must carry the U14
// note, and historical order_items snapshots must never be rewritten.
type ProfitSplit struct {
	PlatformFeeAmount int64 `json:"platformFeeAmount"`
	DropshipperMargin int64 `json:"dropshipperMargin"`
	CentralProfit     int64 `json:"centralProfit"`
	PlatformTotal     int64 `json:"platformTotal"`
}

// ReferenceProfitSplit implements the live-named split on a resolution.
func ReferenceProfitSplit(res ReferenceResolution, isCentral bool) ProfitSplit {
	if isCentral {
		return ProfitSplit{
			PlatformFeeAmount: res.PlatformFee,
			DropshipperMargin: 0,
			CentralProfit:     res.SellerMargin,
			PlatformTotal:     res.PlatformFee + res.SellerMargin,
		}
	}
	return ProfitSplit{
		PlatformFeeAmount: res.PlatformFee,
		DropshipperMargin: res.SellerMargin,
		CentralProfit:     0,
		PlatformTotal:     res.PlatformFee,
	}
}

// invalidResolution quarantines unsupported legacy inputs. Mutation callers must reject PricingError.
func invalidResolution(cost int64) ReferenceResolution {
	return ReferenceResolution{WholesaleCost: cost, MoneyPolicyVersion: MoneyPolicyVersion, PricingError: ErrInvalidMoney.Error()}
}
