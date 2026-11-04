import { Product, Store, PlatformSettings, PackagingTariff } from '../types/index';
import { getProductNicheIds } from '../data/niches';

/**
 * Universal Price Rounding:
 * Prices in Tomans are always rounded to the nearest multiple of 100 Tomans.
 */
export const roundTo100 = (val: number): number => {
  return Math.round(val / 100) * 100;
};

/**
 * Percent-only platform share (pct) resolver.
 * Priority: published global platformPct -> pct derived from a
 * store-priced fee the API already reported (platformMargin = R - C
 * for the same product, which also carries a per-product ladder pct)
 * -> 0. Per-product/category ladder maps are server-side only; the
 * client never consults flat fee overrides (historical data only).
 */
export const resolvePlatformPct = (
  product: { id: string; category?: string; supplierBaseCost?: number; platformMargin?: number },
  settings?: PlatformSettings
): number => {
  if (settings?.platformPct !== undefined) {
    return settings.platformPct;
  }
  if (
    product.supplierBaseCost &&
    product.supplierBaseCost > 0 &&
    product.platformMargin !== undefined &&
    product.platformMargin > 0
  ) {
    return (product.platformMargin / product.supplierBaseCost) * 100;
  }
  return 0;
};

/**
 * Resolves the percent base price R = RoundTo100(C * (1 + pct/100)).
 * This is the price the dropshipper effectively buys at; the platform's
 * share is the gap R - C, never a flat fee.
 */
export const resolveBaseCostR = (
  product: Product,
  settings?: PlatformSettings
): number => {
  const pct = resolvePlatformPct(product, settings);
  return roundTo100(product.supplierBaseCost * (1 + pct / 100));
};

export interface PricingResolution {
  wholesaleCost: number; // C
  platformFee: number;   // F = R - C (percent share, never a flat fee)
  baseCost: number;      // R = RoundTo100(C * (1 + pct/100))
  sellerMargin: number;  // D = P - R
  finalPrice: number;    // P = RoundTo100(R * (1 + margin/100))
  originalPrice: number; // Anchor: max(stored original, final) — server rule
  activeTier: 'product' | 'niche' | 'category' | 'default';
  pricingMode: 'percent';
  marginPercent: number; // applied margin percent (input, not profit/final)
}

export interface ResolvedPricingResult {
  price: number;
  originalPrice: number;
  baseCostR: number;
  platformFee: number;
  profitToman: number;
  marginPercent: number;
  activeTier: 'product' | 'niche' | 'category' | 'default';
  pricingMode: 'percent';
}

/**
 * Universal Single-Source Pricing Engine (Priority 2.1) — percent-only.
 *
 * R = RoundTo100(C * (1 + pct/100)); P = RoundTo100(R * (1 + margin/100)).
 * Margin Priority: Admin Lock -> Single Product Override -> Niche Override
 * -> Category Override -> Store Default / Platform Default 20%.
 * Central stores take the platform's centralProfitPct on R (default 10).
 * The anchor price follows the server rule: max(stored original, final).
 * Universal 100-Toman Price Rounding.
 */
export const resolvePricing = (
  product: Product,
  store: Store,
  platformSettings?: PlatformSettings
): PricingResolution => {
  const wholesaleCost = product.supplierBaseCost;
  const pct = resolvePlatformPct(product, platformSettings);
  const baseCost = roundTo100(wholesaleCost * (1 + pct / 100));
  const platformFee = baseCost - wholesaleCost;
  const pNiches = product.nicheIds || getProductNicheIds(product);

  let rawFinalPrice = baseCost;
  let activeTier: 'product' | 'niche' | 'category' | 'default' = 'default';

  const effectiveStoreDefault = store?.customStoreMarginOverride !== undefined
    ? store.customStoreMarginOverride
    : (store?.profitMarginPercent > 0 ? store.profitMarginPercent : (platformSettings?.defaultMargin || 20));

  let marginValue = effectiveStoreDefault;

  if (store?.ownerType === 'dropshipper') {
    if (store.isMarginLockedByAdmin && store.customStoreMarginOverride !== undefined) {
      activeTier = 'default';
      marginValue = store.customStoreMarginOverride;
      rawFinalPrice = baseCost * (1 + marginValue / 100);
    } else {
      // Priority 1: Product-specific override (percent only)
      if (store.productPriceOverrides && store.productPriceOverrides[product.id] !== undefined) {
        activeTier = 'product';
        marginValue = store.productPriceOverrides[product.id];
        rawFinalPrice = baseCost * (1 + marginValue / 100);
      }
      // Priority 2: Niche-specific override (percent only)
      else if (store.nichePriceOverrides) {
        const matchedNicheId = pNiches.find((nId) => store.nichePriceOverrides![nId] !== undefined);
        if (matchedNicheId) {
          activeTier = 'niche';
          marginValue = store.nichePriceOverrides[matchedNicheId];
          rawFinalPrice = baseCost * (1 + marginValue / 100);
        }
      }

      // Priority 3: Category-specific override
      if (activeTier === 'default' && store.categoryPriceOverrides && store.categoryPriceOverrides[product.category] !== undefined) {
        activeTier = 'category';
        marginValue = store.categoryPriceOverrides[product.category];
        rawFinalPrice = baseCost * (1 + marginValue / 100);
      }

      // Priority 4: Default Margin (uses store default or platform default)
      if (activeTier === 'default') {
        rawFinalPrice = baseCost * (1 + marginValue / 100);
      }
    }
  } else {
    // Central store: the platform's own margin on the base price.
    const centralPct = platformSettings?.centralProfitPct ?? 10;
    marginValue = centralPct;
    rawFinalPrice = baseCost * (1 + centralPct / 100);
  }

  const finalPrice = roundTo100(rawFinalPrice);
  const sellerMargin = finalPrice - baseCost;
  // Server rule (not final * 1.15): the anchor is the stored original,
  // raised to the final price when the final price exceeds it.
  const originalPrice = Math.max(product.originalPrice || 0, finalPrice);

  return {
    wholesaleCost,
    platformFee,
    baseCost,
    sellerMargin,
    finalPrice,
    originalPrice,
    activeTier,
    pricingMode: 'percent',
    marginPercent: marginValue,
  };
};

export const resolveStoreProductPricing = (
  product: Product,
  store: Store,
  settings: PlatformSettings
): ResolvedPricingResult => {
  const res = resolvePricing(product, store, settings);
  return {
    price: res.finalPrice,
    originalPrice: res.originalPrice,
    baseCostR: res.baseCost,
    platformFee: res.platformFee,
    profitToman: res.sellerMargin,
    marginPercent: res.marginPercent,
    activeTier: res.activeTier,
    pricingMode: res.pricingMode,
  };
};

/**
 * Requirement 7: Packaging & Shipping Tariffs by Weight & Distance
 */
export const DEFAULT_PACKAGING_TARIFFS: PackagingTariff[] = [
  {
    id: 'pack-light-intra',
    title: 'مرسوله سبک درون‌استانی (تا ۵۰۰ گرم)',
    minWeightGrams: 0,
    maxWeightGrams: 500,
    destinationType: 'intra_province',
    boxSize: 'پاکت حباب‌دار A4 / کارتن سایز ۱',
    cost: 35000,
  },
  {
    id: 'pack-light-inter',
    title: 'مرسوله سبک برون‌استانی (تا ۵۰۰ گرم)',
    minWeightGrams: 0,
    maxWeightGrams: 500,
    destinationType: 'inter_province',
    boxSize: 'پاکت حباب‌دار A4 / کارتن سایز ۱',
    cost: 48000,
  },
  {
    id: 'pack-med-intra',
    title: 'مرسوله متوسط درون‌استانی (۵۰۰ تا ۲۰۰۰ گرم)',
    minWeightGrams: 501,
    maxWeightGrams: 2000,
    destinationType: 'intra_province',
    boxSize: 'کارتن پستی سایز ۲',
    cost: 52000,
  },
  {
    id: 'pack-med-inter',
    title: 'مرسوله متوسط برون‌استانی (۵۰۰ تا ۲۰۰۰ گرم)',
    minWeightGrams: 501,
    maxWeightGrams: 2000,
    destinationType: 'inter_province',
    boxSize: 'کارتن پستی سایز ۲',
    cost: 68000,
  },
  {
    id: 'pack-heavy-intra',
    title: 'مرسوله سنگین درون‌استانی (بیش از ۲ کیلوگرم)',
    minWeightGrams: 2001,
    maxWeightGrams: 50000,
    destinationType: 'intra_province',
    boxSize: 'کارتن استاندارد سایز ۳ / پالت',
    cost: 75000,
  },
  {
    id: 'pack-heavy-inter',
    title: 'مرسوله سنگین برون‌استانی (بیش از ۲ کیلوگرم)',
    minWeightGrams: 2001,
    maxWeightGrams: 50000,
    destinationType: 'inter_province',
    boxSize: 'کارتن استاندارد سایز ۳ / پالت',
    cost: 95000,
  },
];

export const resolvePackagingAndShippingFee = (
  weightGrams: number,
  destinationProvince: string,
  originProvince: string = 'تهران',
  settings?: PlatformSettings
): { cost: number; tariffTitle: string; boxSize: string; isInterProvince: boolean } => {
  const tariffs = settings?.packagingTariffs && settings.packagingTariffs.length > 0
    ? settings.packagingTariffs
    : DEFAULT_PACKAGING_TARIFFS;

  const dest = (destinationProvince || '').trim();
  const orig = (originProvince || '').trim();
  const isInterProvince = dest !== '' && dest !== orig;
  const destinationType = isInterProvince ? 'inter_province' : 'intra_province';

  const matched = tariffs.find(
    (t) =>
      t.destinationType === destinationType &&
      weightGrams >= t.minWeightGrams &&
      weightGrams <= t.maxWeightGrams
  ) || tariffs[0];

  return {
    cost: roundTo100(matched.cost),
    tariffTitle: matched.title,
    boxSize: matched.boxSize,
    isInterProvince,
  };
};

/**
 * Requirement 3: Category-based Supplier Baseline Depot Quota
 * Determines the baseline storage quota floor for new suppliers (with 0 sales history)
 * based on item size/category (small/light: 100, medium: 50, bulky/large: 20).
 */
export const DEFAULT_DEPOT_BASE_QUOTA_BY_CATEGORY: Record<string, number> = {
  small_light: 100,
  medium: 50,
  bulky_large: 20,
  digital: 100,
  beauty: 100,
  gold: 100,
  fashion: 50,
  culture: 50,
  travel: 50,
  supermarket: 50,
  appliances: 20,
  car: 20,
};

export const resolveSupplierBaselineQuota = (
  categorySlug?: string,
  settings?: PlatformSettings
): number => {
  const mapping = settings?.warehouseTariffs?.depotBaseQuotaByCategory || DEFAULT_DEPOT_BASE_QUOTA_BY_CATEGORY;
  if (categorySlug && mapping[categorySlug] !== undefined) {
    return mapping[categorySlug];
  }
  return settings?.warehouseTariffs?.baseMinDepotQuota || 100;
};

export const calculateSupplierDepotQuota = (
  supplier: { avgMonthlySales: number; category?: string },
  settings?: PlatformSettings
): { allowedCap: number; isBaseFloor: boolean; baseFloor: number } => {
  const multiplier = settings?.warehouseTariffs?.depotBufferMultiplier || 1.5;
  const baseFloor = resolveSupplierBaselineQuota(supplier.category, settings);

  if (supplier.avgMonthlySales > 0) {
    const formulaCap = Math.round(supplier.avgMonthlySales * multiplier);
    return {
      allowedCap: Math.max(baseFloor, formulaCap),
      isBaseFloor: formulaCap < baseFloor,
      baseFloor,
    };
  }

  // New suppliers with 0 sales history: strictly use the category-specific floor
  return {
    allowedCap: baseFloor,
    isBaseFloor: true,
    baseFloor,
  };
};
// Item 3: Analyze potential supply-demand tension between novice suppliers and trial-plan sellers in overlapping niches
export interface QuotaMismatchAlert {
  categorySlug: string;
  categoryTitle: string;
  noviceSupplierCount: number;
  totalDepotQuota: number;
  trialSellersCount: number;
  riskLevel: 'high' | 'medium' | 'low';
  message: string;
}

export const checkSupplierTrialSellersMismatch = (
  stores: Array<{ activePlanId?: string; selectedNicheIds?: string[]; activeCategories?: string[] }>,
  suppliers: Array<any>,
  categories: Array<{ slug: string; title: string }>,
  settings?: PlatformSettings
): QuotaMismatchAlert[] => {
  const alerts: QuotaMismatchAlert[] = [];

  categories.forEach((cat) => {
    // 1. Identify novice suppliers in this category (avgMonthlySales === 0 or no sales history)
    const catNoviceSuppliers = suppliers.filter(
      (s) => (s.category === cat.slug || (!s.avgMonthlySales && !s.category)) && (!s.avgMonthlySales || s.avgMonthlySales === 0)
    );

    // 2. Identify active trial sellers targeting this category
    const catTrialSellers = stores.filter(
      (s) => (s.activePlanId === 'trial' || !s.activePlanId) && (s.activeCategories?.includes(cat.slug) || s.selectedNicheIds?.includes(cat.slug))
    );

    if (catNoviceSuppliers.length > 0 && catTrialSellers.length >= 2) {
      const quotaPerSupplier = resolveSupplierBaselineQuota(cat.slug, settings);
      const totalAllowedDepot = catNoviceSuppliers.length * quotaPerSupplier;
      const trialDemandEstimate = catTrialSellers.length * (settings?.warehouseTariffs?.trialPlanCatalogQuota || 20);

      const isHighRisk = totalAllowedDepot < trialDemandEstimate;

      alerts.push({
        categorySlug: cat.slug,
        categoryTitle: cat.title,
        noviceSupplierCount: catNoviceSuppliers.length,
        totalDepotQuota: totalAllowedDepot,
        trialSellersCount: catTrialSellers.length,
        riskLevel: isHighRisk ? 'high' : 'medium',
        message: isHighRisk
          ? `ممکن است سقف دپوی تأمین‌کنندگان تازه‌وارد (${totalAllowedDepot} قلم) برای پوشش تقاضای کاتالوگ ${catTrialSellers.length} سلر پلن آزمایشی کافی نباشد.`
          : `تراکم بالای سلرهای آزمایشی در این دسته‌بندی؛ نظارت بر موجودی انبار توصیه می‌شود.`,
      });
    }
  });

  return alerts;
};

