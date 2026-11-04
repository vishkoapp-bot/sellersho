import type { Store, Product, Category } from '../../../types';
import { filterStoreProducts } from '../../../utils/storeAssortment';

// FIX3 — store template section model, rebuilt from live behaviour
// (live template chunks, read-only evidence), NOT copied live code:
// live templates read store.templateConfig.custom (heroSlides,
// fallback-banner texts, titles/counts) and derive rails / deals /
// brands from the store's own products and categories. The candidate
// model therefore has the same section ids and the same data contract:
//  - heroSlides: seller-configured slides from templateConfig.custom
//  - categoryRail(s): categories with the most products in this store
//  - banners: seller-configured banners from templateConfig.custom
//  - deals: discounted products of this store
//  - timedDeals: the store's public deals widget (sellerDealsWidgetConfig)
//  - brands: distinct supplier names of this store's products
//  - featured / bestsellers: flagged / best-selling products
//  - products / allProducts: the full storefront grid (Tech's only grid)
// Sections keep the previous model's semantics: toggleable, orderable,
// defaultEnabled, selfHidesWhenEmpty. A section renders nothing when
// it is disabled OR when it has no data (self-hiding), so a store
// without configured slides/banners never shows an empty shell.
// The full tpl-modern-1 section order in live is still unrecovered
// (U44); Modern is assembled conservatively (hero + deals + products)
// and that gap is recorded, not invented.
export interface TemplateSection {
  key?: string;
  id?: string;
  label: string;
  toggleable: boolean;
  orderable: boolean;
  anchor?: string;
  defaultEnabled: boolean;
  selfHidesWhenEmpty: boolean;
  enabled?: boolean;
}

export function sectionKey(section: TemplateSection): string {
  return section.key || section.id || '';
}

// Live uses both spellings for the same section: the Digikala chunk
// defines id "categoryRails", the task evidence calls it categoryRail.
const SECTION_ALIASES: Record<string, string> = {
  categoryRails: 'categoryRail',
  categoryRail: 'categoryRail',
  // FIX6 - dict keys from the public API for the same categories block.
  categories: 'categoryRail',
  categoryTiles: 'categoryRail',
};

export function canonicalSectionKey(key: string): string {
  return SECTION_ALIASES[key] || key;
}

export interface TemplateHeroSlide {
  image?: string;
  title?: string;
  subtitle?: string;
  link?: string;
}

export interface TemplateBanner {
  image?: string;
  title?: string;
  link?: string;
}

export interface TemplateCategoryOverride {
  slug: string;
  label?: string;
  name?: string;
  count?: number;
  image?: string;
}

// Presentation-only custom values from templateConfig.custom. These
// are exactly the keys the FIX3 public API allowlist emits, so a
// public visitor only ever sees what the backend already approved.
export interface TemplateCustom {
  heroSlides?: TemplateHeroSlide[];
  banners?: TemplateBanner[];
  heroFallbackBanner1?: string;
  heroFallbackBanner2?: string;
  heroFallbackBanner3?: string;
  heroFallbackSubtitle?: string;
  categoriesTitle?: string;
  categoriesCount?: number;
  categoryRailsMax?: number;
  categoryRailsCount?: number;
  categoryOverrides?: TemplateCategoryOverride[];
  dealsTitle?: string;
  dealsCount?: number;
  discountedTitle?: string;
  discountedCount?: number;
  featuredTitle?: string;
  featuredCount?: number;
  bestSellersTitle?: string;
  bestsellersCount?: number;
  topRatedTitle?: string;
  topRatedCount?: number;
  newestTitle?: string;
  newestCount?: number;
  promoBadge?: string;
  viewAllText?: string;
  heroAutoplay?: boolean;
  lookbookTitle?: string;
  lookbookImage?: string;
  lookbookSubtitle?: string;
  lookbookLink?: string;
}

// FIX6 - the public store API returns templateConfig.sections as a
// dict of booleans (e.g. mone: {deals: true, brands: true, ...}), not
// the array the templates historically expected. The type admits both;
// normalizeTemplateSections() below converts the dict form, and
// sectionEnabled() is the single choke point that uses it, so no
// template or StoreSections component needs its own shape handling.
export interface TemplateConfig {
  accentColor?: string;
  sections?: TemplateSection[] | Record<string, boolean>;
  custom?: TemplateCustom;
}

export const DEFAULT_TEMPLATE_SECTIONS: TemplateSection[] = [
  { key: 'announcement', label: 'نوار اطلاع‌رسانی', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'hero', label: 'بنر اصلی', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'heroSlides', label: 'اسلایدهای بنر اصلی', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'categoryRail', label: 'ریل دسته‌های پُرمحصول', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'banners', label: 'بنرها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'deals', label: 'پیشنهادهای شگفت‌انگیز', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'timedDeals', label: 'پیشنهاد زمان‌دار', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'brands', label: 'ریل برندها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'featured', label: 'کالاهای منتخب', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'bestsellers', label: 'پرفروش‌ترین‌ها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'trustStrip', label: 'نوار اعتماد', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'discounted', label: 'کالاهای تخفیف‌دار', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'about', label: 'درباره فروشگاه', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'topRated', label: 'پرامتیازترین‌ها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'promo', label: 'بنر تبلیغاتی', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'campaign', label: 'کمپین', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'newsletter', label: 'خبرنامه', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'testimonials', label: 'نظرات مشتریان', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'faq', label: 'پرسش‌های متداول', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'products', label: 'همه کالاها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'allProducts', label: 'همه کالاها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
];

export interface StorefrontTemplateProps {
  store: Store;
  products: Product[];
  accentColor: string;
  config: TemplateConfig;
  categories?: Category[];
}

// Products actually active in this storefront (live public store response
// carries activeProductIds). FIX 10: when a store records a non-empty
// activeProductIds list, the storefront shows exactly the intersection
// with the catalogue - even when that intersection is empty (live shows
// its empty state for mone/vishkostor/sahara1, whose assigned ids are not
// in the central catalogue). Falling back to the full catalogue here is
// what made every preview storefront render the general 40-item grid.
// ISO-A (user-ordered storefront separation): that fallback is kept
// ONLY for the central store (which records [] by design and owns the
// whole catalogue). For a dropshipper, an empty/undefined list means
// zero published products, not "no restriction": falling back to the
// full central catalogue would make every central product buyable in
// a dropshipper context that publishes nothing, the exact preview
// leak class (order creation then 400s "is not published by this
// store"). A dropshipper with no recorded list therefore gets [].
export function storefrontProducts(store: Store, products: Product[]): Product[] {
  // Live parity for the empty-array case (live Modern/Tech filter:
  // `!ae || ae.includes(id)` - an empty array yields an empty grid,
  // never the full catalogue). Central stores keep the full catalogue.
  return filterStoreProducts(store, products);
}

// FIX6 - normalize the API dict form ({deals: true, brands: false})
// into the array form the section model expects. Arrays pass through
// (non-object entries dropped); anything else means 'no override' and
// the caller falls back to defaults. Unknown dict keys pass through
// unchanged; templates only ever query keys they know.
export function normalizeTemplateSections(sections: unknown): TemplateSection[] | undefined {
  if (sections === undefined || sections === null) return undefined;
  if (Array.isArray(sections)) {
    return sections.filter((entry) => entry && typeof entry === 'object') as TemplateSection[];
  }
  if (typeof sections === 'object') {
    return Object.entries(sections as Record<string, unknown>).map(([key, value]) => ({
      key,
      label: key,
      toggleable: true,
      orderable: true,
      defaultEnabled: true,
      selfHidesWhenEmpty: true,
      enabled: value !== false,
    }));
  }
  return undefined;
}

export function sectionEnabled(config: TemplateConfig, key: string): boolean {
  const wanted = canonicalSectionKey(key);
  const section = normalizeTemplateSections(config.sections)?.find((s) => canonicalSectionKey(sectionKey(s)) === wanted);
  if (section) return section.enabled !== false;
  return DEFAULT_TEMPLATE_SECTIONS.find((s) => canonicalSectionKey(sectionKey(s)) === wanted)?.defaultEnabled ?? true;
}

export function templateCustom(config: TemplateConfig): TemplateCustom {
  const custom = (config as { custom?: unknown }).custom;
  if (custom && typeof custom === 'object' && !Array.isArray(custom)) {
    return custom as TemplateCustom;
  }
  return {};
}

// Live heroSlides filter, rebuilt: a slide counts only when it has a
// non-empty image or title; anything else self-hides.
export function heroSlidesOf(config: TemplateConfig): TemplateHeroSlide[] {
  const slides = templateCustom(config).heroSlides;
  if (!Array.isArray(slides)) return [];
  return slides.filter(
    (s) =>
      s &&
      typeof s === 'object' &&
      ((typeof s.image === 'string' && s.image.trim() !== '') ||
        (typeof s.title === 'string' && s.title.trim() !== '')),
  );
}

export function bannersOf(config: TemplateConfig): TemplateBanner[] {
  const banners = templateCustom(config).banners;
  if (!Array.isArray(banners)) return [];
  return banners.filter(
    (b) => b && typeof b === 'object' && typeof b.image === 'string' && b.image.trim() !== '',
  );
}

export function customText(config: TemplateConfig, key: keyof TemplateCustom, fallback: string): string {
  const value = templateCustom(config)[key];
  return typeof value === 'string' && value.trim() !== '' ? value : fallback;
}

export function customCount(config: TemplateConfig, key: keyof TemplateCustom, fallback: number): number {
  const value = templateCustom(config)[key];
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}
