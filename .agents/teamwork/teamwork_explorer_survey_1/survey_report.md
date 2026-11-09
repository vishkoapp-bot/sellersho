# Comprehensive Survey Report: Fashion Storefront Template (`tpl-fashion-1`) and Storefront Ecosystem in Sellersho

**Date:** 2026-11-07  
**Author:** Teamwork Explorer Survey Agent (`teamwork_explorer_survey_1`)  
**Target:** Sellersho Storefront Subsystem, Fashion Template (`tpl-fashion-1`), and Surrounding Components  

---

## 1. Executive Summary & Architecture Overview

The **Fashion Template (`tpl-fashion-1`)** is one of five official storefront templates in Sellersho (alongside `tpl-modern-1`, `tpl-electronic-1`, `tpl-digikala-1`, and `tpl-tech-1`). Unlike traditional marketplace themes characterized by dense deal grids, countdown timers, and promotional badges, `tpl-fashion-1` implements a **curated, editorial gallery experience** inspired by high-end fashion platforms (specifically following the design language of `aassttiinn.com`).

### Core Architectural Characteristics:
1. **Strict Design Token Discipline (Aastin Tokens):**
   - Canvas/Background: `#faf8f5` (warm editorial off-white/cream)
   - Secondary Tiles: `#f2f2f2` (neutral light grey)
   - Announcement Bar: `#efeae3` (subtle contrasting sand)
   - Cards/Surfaces: `#ffffff`
   - Primary Ink/Text: `#1c1c1a` (deep charcoal black)
   - Muted/Secondary Text: `#737373`
   - Hairline Rules & Borders: `#e5e0d8`
   - Hero Band / Dark Surface: `#3a4441` (dark charcoal slate-green)
   - Corner Radius: Strict `rounded-[2px]` or `rounded-none`. **No bubble pills or large rounded cards (`rounded-2xl` / `rounded-3xl`) exist inside the template.**
2. **Template Identity Whitelist:**
   - E-commerce badge/deal sections (`deals`, `timedDeals`, `discounted`, `campaign`, `topRated`, `bestsellers`, `trustStrip`) are **strictly prohibited** inside `FashionTemplate.tsx` and return `null` unconditionally, even if enabled in saved configurations.
3. **Dynamic Single-Pass Media Ledger (`MediaLedger`):**
   - Prevents image repetition across editorial modules. As the page renders in section order, images used by hero slides, macro tiles, category grids, collections, brand portraits, banners, and spotlight clusters are claimed in a deduplication ledger.
   - The catalog floor (`#products`) is the sole exception, acting as the complete un-deduplicated index.
4. **Isolated Price Presentation:**
   - **Prices appear strictly and exclusively on the catalog floor (`AastinProductCard`)**. Hero, spotlight, collection mosaics, and category cards carry zero prices or discount percentage tags, preserving the editorial lookbook aesthetic.
5. **End-to-End PDP Theming Bridge:**
   - When a user clicks any product in the Fashion storefront, navigation to `ProductDetailView` triggers `resolvePdpTheme`, which activates `fashion = true`. The entire PDP (Gallery, Typography, Variant Selector, Add-to-Cart/Buy-Now buttons, and the `BuyNowAddressModal`) transforms into the Aastin token aesthetic.

---

## 2. In-Depth Examination of `FashionTemplate.tsx`

**File Location:** `src/components/storefront/templates/FashionTemplate.tsx` (1,211 lines)

### 2.1 Component Structure & Section Breakdown

`FashionTemplate` dynamically resolves its section lineup using `resolveTemplateConfig('tpl-fashion-1', config)` and renders them in order based on `orderedEnabledSections(cfg)`.

| Section Key | Component Name | Description & Visual Treatment | Data Source |
|---|---|---|---|
| `announcement` | `AastinAnnouncement` (lines 131–153) | Slim full-bleed top bar (`#efeae3`, min-h 36px/40px). Stacked flush with hero without gap. Supports external links (`target="_blank"`). | `custom.announcementText` $\to$ `store.tagline` $\to$ `custom.announcementLink` |
| `hero` / `heroSlides` | `AastinHero` (lines 164–305) | **With Slides:** Full-bleed 80svh (mobile) / 92svh (desktop) slider with crossfade, white H1 at bottom-right, 44px hit-target dots, hover/focus pause, and `prefers-reduced-motion` support.<br>**Without Slides:** Typographic hero with store name H1, subtitle, and underlined CTA link on `#faf8f5`. | `heroSlidesOf(cfg)` $\to$ `custom.heroTitle`, `custom.heroSubtitle`, `custom.heroCtaText`, `custom.heroCtaLink` |
| `categoryRail` | `AastinMacroCategories` (lines 314–378) | Up to 3 top categories rendered as large square/cinematic tiles (`aspect-[16/10]` or `aspect-square`, `rounded-[2px]`), remaining categories as pill chips. Selecting a tile/chip updates `selectedCategory` and jumps to `#products`. | `groupProductsByCategory(items, categories, custom.categoryOverrides)` |
| `about` | `AastinStory` (lines 386–431) | Editorial brand story split: title (`aboutTitle`) and multi-paragraph text alongside a large square image (`aboutImage`). Self-hides if empty. | `custom.aboutText` $\to$ `store.description` / `shortDescription`, `custom.aboutImage` |
| `categoryGrid` | `AastinCategoryGrid` (lines 439–480) | Displays remaining category groups not claimed by macro entrance, capped at 3 or 6 (complete rows of 3). Square tiles on `#f2f2f2` with centered label below. | Unclaimed category groups from `categoryGroups` |
| `collections` | `AastinCollections` (lines 491–545) | Mosaic cards constructed from distinct product images per category: 4 images (2×2), 3 images (1 wide + 2 squares), or 2 images. Requires $\ge 2$ distinct unused images. Lone collection renders as a centered spotlight (max 680px). | Category groups with unused distinct product images |
| `lookbook` | `AastinLookbookBand` (lines 553–592) | Editorial full-bleed/contained still (52svh mobile, 72svh desktop) with overlaid title and subtitle. Renders quiet pale panel if image is absent. | `custom.lookbookImage`, `custom.lookbookTitle`, `custom.lookbookSubtitle`, `custom.lookbookLink` |
| `brands` | `AastinBrandTiles` (lines 601–672) | Distinct product brands with real product imagery. Requires $\ge 2$ brands. Up to 4 portrait tiles (`aspect-[3/4]`), remainder as text chips with item counts. Clicking brand raises `openCatalogBrand(brand)`. | `products` grouped by `brandOf(p)` |
| `banners` / `promo` | `AastinBanners` (lines 678–714) | High-impact sharp banner tiles (`aspect-[16/10]`, `aspect-[4/3]`, or `aspect-[21/9]`). Titles rendered underneath (`TileLabel`), never overlaid. | `bannersOf(cfg)` |
| `featured` | `AastinSpotlight` (lines 723–758) | Featured products presented as pure imagery without text, badges, or prices. 4 items form a 2×2 grid; other counts make the first tile large (`col-span-2`, `row-span-2`) with adjacent squares. | `products.filter(p => p.featured)` (ledger-filtered) |
| `testimonials` | `AastinTestimonials` (lines 764–784) | Quiet quote figures with large Persian quotation mark (`«`), quotation body, and author caption. Self-hides if no quotes exist. | `custom.testimonials` array |
| `faq` | `AastinFaq` (lines 791–827) | Minimalist hairline FAQ rows (`divide-y divide-[#e5e0d8] border-y border-[#e5e0d8]`). No shadows or boxed cards. | `custom.faqItems` $\to$ fallback fetch `GET /static-pages` (`faq` key) |
| `newsletter` | `AastinNewsletter` (lines 865–929) | Full-bleed dark band (`#3a4441`). Title, subtitle, underline-style phone input, and submit button. Submits `POST /newsletter/subscribe` with `{ mobile, storeSlug }`. | `custom.newsletterTitle`, `custom.newsletterSubtitle`, `store.slug` |
| `products` / `allProducts` | Catalog Floor & `AastinProductCard` (lines 829–858, 1134–1171) | The definitive store catalog. Responsive grid: 1 col ($\le 380$px), 2 cols ($\le 880$px), 3 cols, or 4 cols. Integrates category, brand, and text search filters. | `storefrontProducts(store, products)` filtered by category, brand, and query |

### 2.2 The Media Ledger Mechanism (Deduplication)

Lines 97–113 of `FashionTemplate.tsx`:
```typescript
interface MediaLedger {
  used: Set<string>;
  macroTaken: Set<string>;
}
```
- Before any section renders an image, it calls `peekUnusedImage(ledger, candidates)`.
- If an image is selected, `claimImage(ledger, image)` adds the URL to `ledger.used`.
- If no unused candidate image exists for a category or collection, that card drops out or falls back cleanly rather than duplicating an image already seen in the hero or macro tiles.
- The catalog floor (`AastinProductCard`) is deliberately exempt, functioning as the comprehensive catalog index.

### 2.3 Whitelisted vs. Suppressed Sections

In lines 1110–1121, `FashionTemplate` explicitly enforces:
```typescript
case 'deals':
case 'timedDeals':
case 'discounted':
case 'campaign':
case 'topRated':
case 'bestsellers':
case 'trustStrip':
  // Template-identity whitelist: the shared app-style sections
  // never render inside tpl-fashion-1, even when a saved config enables them.
  return null;
```
This guarantees brand integrity even if a seller activates marketplace widgets in the management panel.

---

## 3. Storefront Component Ecosystem Survey

### 3.1 Layout Shell & Container Hierarchy
The storefront is hosted inside `SellerStorefrontView.tsx` (`src/components/storefront/SellerStorefrontView.tsx`):
1. **Header:** `SellerStorefrontHeader` (sticky top bar).
2. **Search Overlay:** Search drawer toggled by header icon or search query.
3. **Template Body:** `StorefrontTemplate` (wrapping `FashionTemplate` via `TemplateRegistry.tsx`).
4. **Footer:** `SellerStorefrontFooter` (bottom section + fixed mobile navigation).

### 3.2 Detailed Component Inventory

| Component | Path | Role & Behavior | Fashion Compatibility Assessment |
|---|---|---|---|
| `SellerStorefrontView` | `src/components/storefront/SellerStorefrontView.tsx` | Root controller for merchant storefronts. Handles store-scoped product pricing via `catalogApi.getProducts({ storeSlug })`, template resolution, and empty-catalog state (`به‌زودی محصولات جدید می‌رسند`). | **High:** Fully wires store-scoped catalog and template config. |
| `SellerStorefrontHeader` | `src/components/storefront/SellerStorefrontHeader.tsx` | Sticky top navigation with store logo, name, tagline, search button, wishlist button, cart button, and profile button. | **Subpar / Inconsistent:** Only supports `variant="modern"` and `variant="digikala"`. `tpl-fashion-1` receives `modern` header (beige `#FAF8F4` with teal `#0D7377` round logo circle and rounded-full icon buttons), which clashes with the sharp `#1c1c1a` / `#faf8f5` editorial aesthetic. |
| `SellerStorefrontFooter` | `src/components/storefront/SellerStorefrontFooter.tsx` | Dark footer (`#1C1917`) with `rounded-t-[32px]`, store contact links, trust links, and in-page "All Products" modal (`allProductsOpen`). | **Subpar / Inconsistent:** Footer uses rounded-t-[32px] and teal accents (`#0D7377`). The "All Products" modal renders standard `ProductCard` with blue prices and round corners instead of `AastinProductCard`. |
| `StorefrontBottomNav` | `src/components/storefront/SellerStorefrontFooter.tsx:163` | Fixed mobile/tablet bottom navigation bar (Home, Categories, Account, Contact). | **Moderate:** Functional, but uses round buttons and hardcoded teal `#0D7377` active highlights. |
| `ProductCard` (Shared) | `src/components/ProductCard.tsx` | General marketplace product card with rounded-2xl frame, discount badges, add-to-cart button, rating stars, and blue accents. | **N/A for template root:** `FashionTemplate` correctly avoids this and uses `AastinProductCard`. However, modals in the footer and related products in PDP still use `ProductCard`. |
| `AastinProductCard` | `src/components/storefront/templates/FashionTemplate.tsx:834` | Dedicated editorial product card: square tile (`rounded-[2px]`), title, brand line, price in Toman. No distracting buttons or discount badges. | **Excellent:** Flawless embodiment of Aastin aesthetic. |
| `StoreSections` | `src/components/storefront/templates/StoreSections.tsx` | Shared section building blocks (`HeroSlider`, `CategoryRail`, `DealsSection`, `BannerRow`, `BrandsRail`, etc.). | **Partial:** `FashionTemplate` reuses helper logic (`groupProductsByCategory`, `CategoryGroup`), but intentionally overrides visual presentation with native `Aastin*` components. |
| `TemplateProductGrid` | `src/components/storefront/templates/TemplateProductGrid.tsx` | Standard 2-to-4 column grid wrapping `ProductCard`. | **Bypassed:** Not used by `FashionTemplate` (which uses its own responsive flex/grid wrapper for `AastinProductCard`). |
| `BuyNowAddressModal` | `src/components/BuyNowAddressModal.tsx` | Quick purchase address entry drawer/modal. | **High:** Implements explicit `fashion?: boolean` prop; renders square inputs, `#faf8f5` background, `#3a4441` button, and `#1c1c1a` typography when `fashion={true}`. |
| `CartDrawer` | `src/components/CartDrawer.tsx` | Slide-out cart drawer with checkout steps, shipping calculation, and address form. | **Neutral:** Generic platform UI (blue `#007dfa`, rounded-xl). Not themed per template. |
| `CustomerWishlistView` | `src/components/views/CustomerWishlistView.tsx` | Customer saved items view. | **Neutral:** Uses standard `ProductCard` grid. |
| `ProductDetailView` | `src/components/views/ProductDetailView.tsx` | Product detail page (PDP). | **High:** Uses `resolvePdpTheme` to activate `fashion=true` for `tpl-fashion-1`. Gallery, variant chips, and buttons are styled with Aastin tokens. |

---

## 4. Template Lifecycle: Registration, Resolution, Customization & Preview

### 4.1 Backend Data & API Layer
- **Database Schema:** `store_templates` table (`id`, `name`, `description`, `thumbnail_url`, `is_active`). In migration scripts, `tpl-fashion-1` is registered with:
  - ID: `tpl-fashion-1`
  - Name: `قالب فشن (Fashion / Aastin)`
  - Order: Modern (0) $\to$ Electronic (1) $\to$ **Fashion (2)** $\to$ Digikala (3) $\to$ Tech (4)
- **Store Record:** `stores` table stores `template_id` (varchar) and `template_config` (jsonb).
- **Public & Seller Endpoints:**
  - `GET /api/v1/store-templates`: returns all active templates.
  - `GET /api/v1/seller/store/template?slug=...`: fetches seller's configured template and JSON config.
  - `PUT /api/v1/seller/store/template`: saves draft template ID and configuration.
  - `POST /api/v1/seller/store/template/publish`: marks configuration as published/live.
  - `GET /api/v1/stores/{slug}`: public store response includes `templateId` and whitelisted presentation keys from `templateConfig`.

### 4.2 Frontend Template Registry (`TemplateRegistry.tsx`)
- Registered template IDs: `STORE_TEMPLATE_IDS = ['tpl-modern-1', 'tpl-tech-1', 'tpl-electronic-1', 'tpl-fashion-1', 'tpl-digikala-1']`.
- Lazy Loading: `FashionTemplate` is lazily imported via `React.lazy(() => import('./FashionTemplate'))` and wrapped in `React.Suspense` with a Persian loading fallback.
- Accent Color: `TEMPLATE_ACCENTS['tpl-fashion-1'] = '#FF3B30'` (used as fallback when merchant does not specify a custom accent).

### 4.3 Template Blueprints & Legacy Migrations (`templateTypes.ts`)
- **Blueprint Definition:**
  ```typescript
  'tpl-fashion-1': bp('tpl-fashion-1', 'روشن و ادیتوریال؛ گالری کیوریت‌شده به سبک آستین', [
    'announcement', 'hero', 'categoryRail', 'about', 'categoryGrid', 'collections',
    'lookbook', 'brands', 'banners', 'featured', 'testimonials', 'products', 'faq', 'newsletter',
  ], {
    categoriesTitle: 'دسته‌بندی‌ها',
    collectionsTitle: 'مجموعه‌ها',
    featuredTitle: 'منتخب استایل',
    lookbookTitle: 'کالکشن جدید',
    lookbookSubtitle: 'جدیدترین‌های این فصل را ببینید.',
    brandsTitle: 'برندها',
    aboutTitle: 'درباره فروشگاه',
    productsTitle: 'همه کالاها',
    heroAutoplay: true,
    newsletterTitle: 'از کالاهای تازه باخبر شوید',
    newsletterSubtitle: 'با ثبت شماره موبایل، از رسیدن کالکشن‌های تازه و موجود شدن دوباره کالاها باخبر می‌شوید.',
  })
  ```
- **Legacy Migration Expansion:** Legacy configurations stored before the section split had a single `lookbook` key bundling category grids and collections. Line 399 defines:
  ```typescript
  'tpl-fashion-1': { lookbook: ['categoryGrid', 'collections', 'lookbook'] }
  ```
  `resolveTemplateConfig` expands this legacy key at its original index, guaranteeing backward compatibility.

### 4.4 Seller Customization & Live Emulator
- **Customization Panel (`SellerTemplateTab.tsx`):**
  - Section reordering (Up/Down arrow buttons).
  - Section visibility toggle (Switch component).
  - Per-section content editors (text fields, textareas, image URL inputs, slide list editors, banner list editors, quote editors, FAQ item editors).
  - Draft state persists automatically in browser `localStorage` under `tpl-draft:${slug}:${templateId}`.
- **Live Emulator (`TemplatePreviewEmulator.tsx`):**
  - Mobile emulator: 390px viewport inside a realistic smartphone frame with notch.
  - Desktop emulator: 1280px stage scaled proportionally via CSS transform.
  - Changes in the draft config immediately update the preview without network delay.

---

## 5. Props, Data Models, State Handlers & Dynamic Bindings

### 5.1 Input Props (`StorefrontTemplateProps`)
```typescript
export interface StorefrontTemplateProps {
  store: Store;              // Store model: name, slug, tagline, description, logo, etc.
  products: Product[];       // Catalog products (store-scoped or central)
  accentColor: string;       // Resolved accent color (#FF3B30 or custom hex)
  config: TemplateConfig;    // Seller configuration ({ sections, custom, accentColor })
  categories?: Category[];   // Global category tree
}
```

### 5.2 Context Hooks & Storefront State Handlers
Inside `FashionTemplate.tsx`, the component accesses global store state via `useStore()`:
1. `openProductDetail: (product: Product) => void`:
   - Called when a user clicks any `AastinProductCard` on the catalog floor or any tile in `AastinSpotlight`.
   - Transitions `activeView` to `'product_detail'` while propagating `{ storeSlug, productId }`.
2. `selectedCategory: string` & `setSelectedCategory: (slug: string) => void`:
   - Triggered by clicking macro category tiles, category chips, or category grid cards.
   - Filters the catalog floor and smooth-scrolls to `#products`.
3. `openCatalogBrand: (brand: string) => void`, `catalogBrandRequest`, `clearCatalogBrand`:
   - Triggered by clicking brand tiles or brand chips in `AastinBrandTiles`.
   - Filters the catalog floor by brand and updates the floor heading.
4. `searchQuery: string` & `setSearchQuery: (query: string) => void`:
   - Bound to the search input in the header. Filters items on the catalog floor matching title or description.
5. `clearFloorFilter: () => void`:
   - Resets active category (`'all'`), brand (`clearCatalogBrand()`), and search query (`''`).

---

## 6. Detailed Gap Analysis: Implemented vs. Missing / Subpar

### 6.1 What is Implemented Exceptionally Well
1. **Faithful Editorial Design Execution:** Visual fidelity to `aassttiinn.com` is immaculate. The warm paper canvas (`#faf8f5`), hairline borders (`#e5e0d8`), dark ink (`#1c1c1a`), and sharp `rounded-[2px]` geometry look distinctly premium.
2. **Media Ledger Deduplication:** The single-pass image ledger prevents duplicate imagery across the hero, macro categories, category grid, collections, brands, banners, and spotlight.
3. **Whitelisted Integrity:** Suppressing marketplace deal bars and countdowns ensures the template never looks like a generic discount storefront.
4. **PDP Integration:** `ProductDetailView`, `ProductDetailGallery`, `ProductVariantSelector`, and `BuyNowAddressModal` all honor the `fashion` flag and adapt their styling seamlessly.
5. **Robust Backward Compatibility:** Legacy configuration bundle expansion ensures older merchant stores do not break.

### 6.2 Identified Gaps and Subpar Areas

| Area | Current Implementation | Identified Issue / Subpar Behavior | Severity |
|---|---|---|---|
| **Header Theming** | Uses `SellerStorefrontHeader` with `variant="modern"`. | Header has beige `#FAF8F4` background, circular `#0D7377` teal logo icon, and rounded-full icon buttons. It visually clashes with the sharp `#1c1c1a` / `#faf8f5` / `rounded-none` editorial theme of `FashionTemplate`. | **High** |
| **Footer Theming** | Uses `SellerStorefrontFooter`. | Footer uses `rounded-t-[32px]` curve and teal `#0D7377` accents. This undermines the architectural minimalism and sharp borders of the template. | **Medium** |
| **Modal Consistency** | "All Products" modal in footer and PDP Related Products section render standard `ProductCard`. | Renders rounded cards with blue badges, add-to-cart buttons, and discount ribbons, violating the editorial image-only / clean price rule. | **Medium** |
| **Seller Panel Feedback on Whitelist** | `SellerTemplateTab` displays editors for `deals`, `timedDeals`, `discounted`, `bestsellers`, `trustStrip`. | Sellers can configure these sections in the editor, but `FashionTemplate` returns `null`. There is no visual badge or note in the editor informing the seller that Fashion template omits these sections by design. | **Medium** |
| **Catalog Floor Sorting / Filtering** | Floor only has text search and active category/brand filter. | Lacks an editorial filter/sort bar (e.g., sort by newest, price high-to-low, or price low-to-high). | **Low** |
| **Cart Drawer Aesthetic** | `CartDrawer` uses platform blue `#007dfa` and rounded-xl styles. | When opening the cart from a Fashion storefront, the user experiences a visual disconnect between the high-end editorial page and the standard platform drawer. | **Low** |
| **FAQ Layout Shift** | `AastinFaq` runs an async fetch to `/static-pages` in `useEffect` when seller has no custom items. | Causes slight layout shift upon page load if network latency occurs. | **Low** |

---

## 7. Concrete Recommendations & Improvement Proposals

1. **Introduce a `variant="fashion"` for `SellerStorefrontHeader`:**
   - Background `#faf8f5`, border `#e5e0d8`, sharp rectangular logo (`rounded-[2px]`), `#1c1c1a` text, and square icon buttons (`rounded-none hover:bg-[#f2f2f2]`).
2. **Introduce a `variant="fashion"` for `SellerStorefrontFooter`:**
   - Remove `rounded-t-[32px]` in favor of a straight border-t `border-[#e5e0d8]`. Replace `#0D7377` accents with `#1c1c1a` or `--tpl-accent`.
3. **Harmonize Modal Product Cards:**
   - Update `allProductsOpen` in `SellerStorefrontFooter` and the related items carousel in `ProductDetailView` to render `AastinProductCard` when the active template is `tpl-fashion-1`.
4. **Seller Panel Section Badge & Tooltip:**
   - In `SellerTemplateTab.tsx`, add an informative badge next to sections like `deals`, `bestsellers`, and `trustStrip` when `selectedId === 'tpl-fashion-1'`:  
     *«این بخش در قالب ادیتوریال فشن جهت حفظ ظاهر مینیمال نمایش داده نمی‌شود.»*
5. **Catalog Floor Sorting Dropdown:**
   - Add a subtle, borderless Persian sort dropdown on `#products` (جدیدترین، ارزان‌ترین، گران‌ترین) that harmonizes with Aastin typography.

---

## 8. Summary Table of Template Sections & Bindings

```
[Template Root: FashionTemplate.tsx (bg-[#faf8f5], text-[#1c1c1a])]
 │
 ├── [announcement] ── AastinAnnouncement (bg-[#efeae3], text-[#1c1c1a])
 ├── [hero] ────────── AastinHero (Full-bleed 80/92svh slider or typographic H1)
 ├── [categoryRail] ── AastinMacroCategories (Top 3 visual tiles + pill chips)
 ├── [about] ───────── AastinStory (Editorial story split + square image)
 ├── [categoryGrid] ── AastinCategoryGrid (Remainder category square tiles)
 ├── [collections] ─── AastinCollections (Product image mosaics: 2x2, 1+2, 2x1)
 ├── [lookbook] ────── AastinLookbookBand (Tall editorial collection still 52/72svh)
 ├── [brands] ──────── AastinBrandTiles (Portrait brand tiles 3:4 + chips)
 ├── [banners] ─────── AastinBanners (Large sharp banner tiles + sub-labels)
 ├── [featured] ────── AastinSpotlight (Pure product imagery cluster, zero prices)
 ├── [testimonials] ── AastinTestimonials (Customer quote cards with « marks)
 ├── [faq] ─────────── AastinFaq (Hairline-separated QA list, no boxes)
 ├── [newsletter] ──── AastinNewsletter (Full-bleed dark charcoal band #3a4441)
 └── [products] ────── Catalog Floor (AastinProductCard grid, search/category/brand filters, ONLY place with prices)
```

*(End of Survey Report)*
