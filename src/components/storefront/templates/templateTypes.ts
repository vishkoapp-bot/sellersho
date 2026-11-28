import type { Store, Product, Category } from '../../../types';
import type { CategoryGroup } from './StoreSections';
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

export interface TemplateTestimonial {
  name: string;
  text: string;
}

export interface TemplateFaqItem {
  q: string;
  a: string;
}

export interface TemplateTrustItem {
  icon?: string;
  title: string;
  subtitle?: string;
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
  collectionsTitle?: string;
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
  // Editable content keys (seller panel «ظاهر فروشگاه»). All optional;
  // renderers fall back to store data or blueprint defaults, so a store
  // that never opened the editor still renders a complete page.
  announcementText?: string;
  announcementLink?: string;
  heroTitle?: string;
  heroSubtitle?: string;
  heroCtaText?: string;
  heroCtaLink?: string;
  heroImage?: string;
  aboutTitle?: string;
  aboutText?: string;
  aboutImage?: string;
  brandsTitle?: string;
  productsTitle?: string;
  testimonials?: TemplateTestimonial[];
  faqItems?: TemplateFaqItem[];
  trustItems?: TemplateTrustItem[];
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
  { key: 'lookbook', label: 'لوک‌بوک / کالکشن', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'categoryGrid', label: 'گرید دسته‌بندی‌ها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'collections', label: 'مجموعه‌ها', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
  { key: 'campaign', label: 'کمپین', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
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
  productFeed?: {
    store: Store;
    categorySlugs?: string[];
    categoryLabels?: Record<string, string>;
    total: number;
    loading: boolean;
    error: string;
    hasMore: boolean;
    loadingMore: boolean;
    loadMore: () => void;
    retry: () => void;
  };
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
// B3 (Batch 3): a dict carries NO order — Object.entries order is
// whatever the wire/jsonb emitted (alphabetical from Go maps), never
// the seller's order. When a blueprint order is supplied, dict keys
// are canonicalised (aliases first, e.g. categories -> categoryRail)
// and emitted in blueprint order, unknown keys appended after in
// alphabetical order (their stored order is unrecoverable, so
// alphabetical is the honest deterministic choice). Without a
// blueprint order the entries keep wire order (callers that only
// query enabled/disabled do not care about order).
export function normalizeTemplateSections(sections: unknown, blueprintOrder?: string[]): TemplateSection[] | undefined {
  if (sections === undefined || sections === null) return undefined;
  if (Array.isArray(sections)) {
    return sections.filter((entry) => entry && typeof entry === 'object'
      && canonicalSectionKey(sectionKey(entry)) !== 'newsletter') as TemplateSection[];
  }
  if (typeof sections === 'object') {
    const entries: TemplateSection[] = [];
    const seen = new Set<string>();
    for (const [rawKey, value] of Object.entries(sections as Record<string, unknown>)) {
      const key = canonicalSectionKey(rawKey);
      if (!key || key === 'newsletter' || seen.has(key)) continue; // dedup after canonicalisation; first occurrence wins
      seen.add(key);
      entries.push({
        key,
        label: key,
        toggleable: true,
        orderable: true,
        defaultEnabled: true,
        selfHidesWhenEmpty: true,
        enabled: value !== false,
      });
    }
    if (blueprintOrder) {
      const rank = new Map(blueprintOrder.map((k, i) => [k, i] as const));
      entries.sort((a, b) => {
        const ra = rank.get(sectionKey(a));
        const rb = rank.get(sectionKey(b));
        if (ra !== undefined && rb !== undefined) return ra - rb;
        if (ra !== undefined) return -1;
        if (rb !== undefined) return 1;
        return sectionKey(a).localeCompare(sectionKey(b));
      });
    }
    return entries;
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

// ---------------------------------------------------------------------
// Template blueprints: the unique structure + starter content of each
// template. Activating a template applies its blueprint (ordered section
// lineup + default titles) merged UNDER the seller's saved config, so
// switching templates visibly restructures the storefront while the
// seller's own edits always win. Every value here is presentation-only
// and editable later from the panel; nothing is store data.
// ---------------------------------------------------------------------

export interface TemplateBlueprint {
  templateId: string;
  tagline: string;
  sections: string[];
  custom: TemplateCustom;
}

function bp(templateId: string, tagline: string, sections: string[], custom: TemplateCustom): TemplateBlueprint {
  return { templateId, tagline, sections, custom };
}

export const TEMPLATE_BLUEPRINTS: Record<string, TemplateBlueprint> = {
  'tpl-modern-1': bp('tpl-modern-1', 'مینیمال و آرام؛ تمرکز روی خودِ کالا', [
    'announcement', 'hero', 'categoryRail', 'products', 'featured', 'banners',
    'bestsellers', 'trustStrip', 'about', 'testimonials', 'faq',
  ], {
    categoriesTitle: 'دسته‌بندی‌ها',
    featuredTitle: 'منتخب‌های فروشگاه',
    bestSellersTitle: 'پرفروش‌ترین‌ها',
    productsTitle: 'همه کالاهای فروشگاه',
    aboutTitle: 'درباره فروشگاه ما',
  }),
  'tpl-electronic-1': bp('tpl-electronic-1', 'پرانرژی و ریل‌محور؛ مخصوص گجت و کالای دیجیتال', [
    'announcement', 'hero', 'categoryRail', 'deals', 'timedDeals', 'banners',
    'bestsellers', 'brands', 'discounted', 'testimonials', 'faq', 'products',
  ], {
    categoriesTitle: 'خرید بر اساس دسته‌بندی',
    dealsTitle: 'پیشنهاد شگفت‌انگیز',
    discountedTitle: 'تخفیف‌های داغ',
    bestSellersTitle: 'پرفروش‌ترین گجت‌ها',
    brandsTitle: 'برندهای محبوب',
    productsTitle: 'همه کالاها',
  }),
  'tpl-fashion-1': bp('tpl-fashion-1', 'فروشگاه مد؛ طراحی مینیمال و ادیتوریال', [
    'announcement', 'hero', 'categoryRail', 'products', 'lookbook', 'about',
  ], {
    announcementText: 'استایل روزمره، انتخاب‌های ماندگار',
    heroTitle: 'سادگی، به سبک تو',
    heroSubtitle: 'انتخاب‌هایی برای استایل روزمره و خاص؛ با رنگ‌های آرام و جزئیاتی که ماندگار می‌شوند.',
    heroCtaText: 'کشف کالکشن',
    heroCtaLink: '#products',
    heroImage: '/templates/fashion/hero.webp',
    heroAutoplay: false,
    categoriesTitle: 'دسته‌بندی‌ها',
    banners: [
      { image: '/templates/fashion/categories.webp', title: 'پوشاک زنانه', link: '#products' },
      { image: '/templates/fashion/categories.webp', title: 'پوشاک مردانه', link: '#products' },
      { image: '/templates/fashion/categories.webp', title: 'کیف و اکسسوری', link: '#products' },
    ],
    productsTitle: 'تازه رسیده‌ها',
    featuredTitle: 'منتخب استایل',
    collectionsTitle: 'مجموعه‌ها',
    lookbookTitle: 'برای روزهای خنک',
    lookbookSubtitle: 'انتخاب‌هایی گرم، بافت‌هایی لطیف و استایلی ماندگار.',
    lookbookImage: '/templates/fashion/knitwear.webp',
    lookbookLink: '#products',
    brandsTitle: 'برندها',
    aboutTitle: 'درباره فروشگاه',
  }),
  'tpl-digikala-1': bp('tpl-digikala-1', 'مارکت‌پلیس متراکم؛ مثل فروشگاه‌های بزرگ', [
    'announcement', 'hero', 'categoryRail', 'deals', 'timedDeals', 'banners',
    'brands', 'bestsellers', 'discounted', 'topRated', 'campaign', 'faq', 'products',
  ], {
    categoriesTitle: 'دسته‌بندی‌ها',
    dealsTitle: 'پیشنهاد شگفت‌انگیز',
    bestSellersTitle: 'پرفروش‌ترین‌ها',
    discountedTitle: 'کالاهای تخفیف‌دار',
    topRatedTitle: 'پرامتیازترین‌ها',
    brandsTitle: 'برندها',
    productsTitle: 'همه کالاها',
  }),
  'tpl-tech-1': bp('tpl-tech-1', 'حرفه‌ای و خنک؛ کاتالوگ‌محور با گرید کامل', [
    'announcement', 'hero', 'trustStrip', 'categoryRail', 'featured', 'brands',
    'topRated', 'about', 'faq', 'products',
  ], {
    categoriesTitle: 'دسته‌بندی‌ها',
    featuredTitle: 'منتخب تکنولوژی',
    topRatedTitle: 'پرامتیازترین‌ها',
    brandsTitle: 'برندها',
    aboutTitle: 'چرا از ما بخرید؟',
    productsTitle: 'کاتالوگ کامل',
  }),
};

export function blueprintFor(templateId: string): TemplateBlueprint | undefined {
  return TEMPLATE_BLUEPRINTS[templateId];
}

function sectionLabel(key: string): string {
  return DEFAULT_TEMPLATE_SECTIONS.find((d) => sectionKey(d) === key)?.label || key;
}

// Merge a saved config over a template blueprint. Sections: the saved
// order wins when present AND the saved form is an array (the seller's
// drag-to-reorder); a saved DICT has no order, so its keys are ordered
// by the blueprint instead (B3 — a dict's wire order is alphabetical
// from Go maps / jsonb, never the seller's order). Blueprint sections
// missing from the saved list are appended in blueprint order so new
// sections never vanish. Enabled flags come from the saved config when
// present, else from the blueprint lineup (in lineup = enabled, absent
// = disabled).
export function resolveTemplateConfig(templateId: string, saved?: TemplateConfig | null): TemplateConfig {
  const blueprint = blueprintFor(templateId);
  const base: TemplateConfig = saved && typeof saved === 'object' ? saved : {};
  if (!blueprint) return base;
  const savedSections = normalizeTemplateSections(base.sections, blueprint.sections) || [];
  const savedByKey = new Map(savedSections.map((sec) => [canonicalSectionKey(sectionKey(sec)), sec] as const));
  const orderedKeys: string[] = [];
  for (const sec of savedSections) {
    const k = canonicalSectionKey(sectionKey(sec));
    if (!k) continue;
    if (!orderedKeys.includes(k)) orderedKeys.push(k);
  }
  for (const k of blueprint.sections) {
    if (!orderedKeys.includes(k)) orderedKeys.push(k);
  }
  // Apply the shopping-first layout to older Modern configurations and the editor preview.
  const fixedStartSections = templateId === 'tpl-modern-1'
    ? ['announcement', 'hero', 'heroSlides', 'categoryRail', 'products', 'allProducts']
    : [];
  const fixedEndSections = templateId === 'tpl-modern-1'
    ? ['trustStrip', 'about', 'testimonials', 'faq']
    : [];
  const fixedSections = new Set([...fixedStartSections, ...fixedEndSections]);
  const displayKeys = [
    ...fixedStartSections.filter(key => orderedKeys.includes(key)),
    ...orderedKeys.filter(key => !fixedSections.has(key)),
    ...fixedEndSections.filter(key => orderedKeys.includes(key)),
  ];
  const sections: TemplateSection[] = displayKeys.map((k) => {
    const savedSec = savedByKey.get(k);
    return {
      key: k,
      label: sectionLabel(k),
      toggleable: true,
      orderable: !fixedSections.has(k),
      defaultEnabled: blueprint.sections.includes(k),
      selfHidesWhenEmpty: true,
      enabled: savedSec ? savedSec.enabled !== false : blueprint.sections.includes(k),
    };
  });
  const savedCustom = templateCustom(base);
  return {
    accentColor: base.accentColor,
    sections,
    custom: { ...blueprint.custom, ...savedCustom },
  };
}

// Ordered, enabled, canonical section keys for a resolved config.
// A dict-form config with a blueprint order is ordered by that
// blueprint (same rule as resolveTemplateConfig); without one, dict
// keys keep wire order. Resolved configs are arrays and pass through
// in their saved order.
export function orderedEnabledSections(config: TemplateConfig, blueprintOrder?: string[]): string[] {
  const normalized = normalizeTemplateSections(config.sections, blueprintOrder);
  if (!normalized || normalized.length === 0) {
    return DEFAULT_TEMPLATE_SECTIONS.filter((d) => d.defaultEnabled).map((d) => canonicalSectionKey(sectionKey(d)));
  }
  return normalized
    .filter((sec) => sec.enabled !== false)
    .map((sec) => canonicalSectionKey(sectionKey(sec)))
    .filter((k, i, arr) => k !== '' && arr.indexOf(k) === i);
}

export interface FashionMacroEntry {
  title: string;
  image: string | undefined;
  link: string;
  slug: string;
}

// B4: fashion macro tile images. Lives here (CSS-free) instead of
// FashionTemplate.tsx so the node:test harness can pin it — the
// template file imports fashion.css, which tsx cannot load.
export const resolveFashionMacroEntries = (
  groups: CategoryGroup[],
  banners: TemplateBanner[],
  limit: number,
): FashionMacroEntry[] => {
  if (groups.length) {
    return groups.slice(0, limit).map((group) => ({
      title: group.title,
      image: group.image || group.products[0]?.image,
      link: '#products',
      slug: group.slug,
    }));
  }
  return banners.slice(0, limit).map((b) => ({
    title: b.title || 'مشاهده مجموعه',
    image: b.image,
    link: b.link || '#products',
    slug: '',
  }));
};
