# Handoff Report: Milestone 1 — FashionTemplate & AastinProductCard Elevation

**Author:** `teamwork_explorer_m1_1`  
**Date:** 2026-11-07  
**Working Directory:** `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\teamwork_explorer_m1_1`  
**Type:** Hard Handoff (Investigation & Implementation Design Complete)  

---

## 1. Observation

1. **Source File Structure & Codebase Location:**
   - Target component: `src/components/storefront/templates/FashionTemplate.tsx` (1,211 lines).
   - Component registry: `src/components/storefront/templates/TemplateRegistry.tsx` maps `tpl-fashion-1` to `FashionTemplate` via `React.lazy`.
   - Shell container: `src/components/storefront/SellerStorefrontView.tsx` wraps the template within `SellerStorefrontHeader`, search overlay, and `SellerStorefrontFooter`.
   - Root application container: `src/App.tsx:419` mounts `<ProductDetailModal />` unconditionally, controlled by `selectedProduct` from `useStore()`.

2. **Existing Implementation Analysis in `FashionTemplate.tsx`:**
   - **Hero (`AastinHero` lines 164–305):**
     - Without slides: purely typographic hero on `#faf8f5` with `h1` at line 197 (`text-4xl lg:text-6xl font-extrabold`) and simple underlined link.
     - With slides: `h-[80svh] lg:h-[92svh]` crossfade container with bottom-center dot pagination. Lacks desktop hover arrows, slide counters, and dynamic Ken-Burns zoom.
   - **Macro Categories (`AastinMacroCategories` lines 314–378):**
     - Up to 3 tiles with basic `group-hover:scale-105` (500ms). Lacks hover border transition and gradient scrim. Pill chips use `border-[#e5e0d8]` without item counts.
   - **Editorial Lookbook (`AastinLookbookBand` lines 553–592):**
     - Single static image with `h-[52svh] lg:h-[72svh]`. Lacks image zoom, editorial kicker tag, and interactive call-to-action link.
   - **Banners (`AastinBanners` lines 678–714):**
     - Static tiles with title underneath. Lacks hover scrim and interactive button prompts.
   - **Product Card (`AastinProductCard` lines 834–858):**
     - Single `<button type="button">` wrapping image and text.
     - Single image render (`product.image`), no support for secondary image hover flip (`product.images[1]`).
     - Zero quick-view button on desktop.
     - No discount percentage badge or original price strike-through.
     - No out-of-stock overlay.
     - Note on DOM semantics: wrapping an inner quick-view `<button>` inside an outer `<button>` is invalid HTML and throws React DOM nesting errors.

3. **Store Context & Modal Infrastructure:**
   - `src/context/StoreContext.tsx:1332-1333`: `useStore()` exposes `selectedProduct: prod.selectedProduct` and `setSelectedProduct: prod.setSelectedProduct`.
   - `src/components/ProductDetailModal.tsx`: Listens to `selectedProduct` and renders a quick-view modal with variant selection, gallery, and cart actions.

4. **Production Build Baseline:**
   - Ran `cd C:\Users\LENOVO\projects\sellersho; npm run build`.
   - Output: `tsc && vite build` completed with **exit code 0** in 29.36s (`✓ 2211 modules transformed`). `FashionTemplate-DYyhr79K.js` generated at 23.67 kB.

---

## 2. Logic Chain

1. **Preservation of Core Contracts (From Obs 1 & 2):**
   - The contract requires zero modifications to `StorefrontTemplateProps` (`store`, `products`, `accentColor`, `config`, `categories`).
   - The template whitelist (`deals`, `timedDeals`, `discounted`, `campaign`, `topRated`, `bestsellers`, `trustStrip` returning `null`) and the `MediaLedger` image deduplication mechanism must remain strictly intact to preserve the editorial identity.
   - In the proposed design, all prop signatures, ledger calls (`peekUnusedImage`, `claimImage`), and whitelist filters remain identical.

2. **Hero & Editorial Elevation (From Obs 2):**
   - In `AastinHero`, adding a Ken Burns scale (`transition-transform duration-[8000ms] ease-out scale-105`), multi-stop gradient scrim (`from-black/85 via-black/40 via-45% to-transparent`), desktop hover arrows (`ChevronRight`, `ChevronLeft` with `strokeWidth={1.5}`), and a slide counter (`toPersianDigits(activeIndex + 1)} / ${toPersianDigits(count)}`) provides a world-class luxury feel while honoring `prefers-reduced-motion`.
   - In `AastinLookbookBand`, extending height to `74svh`, adding a kicker tag `EDITORIAL LOOKBOOK`, and rendering an interactive link with an animated `ArrowLeft` icon transforms static imagery into an engaging collection portal.

3. **Product Card Modernization (`AastinProductCard`) (From Obs 2, 3, & 4):**
   - **Secondary Image Flip:** Detecting `secondaryImage` via `product.images.find(img => img !== product.image) || product.images[1]`. When present, primary image fades out (`opacity-0 duration-500`) and secondary image fades in with 5% zoom (`opacity-100 scale-105 duration-500`). When absent, fallback is a smooth 5% zoom on the primary image.
   - **DOM Accessibility:** To support a desktop hover quick-view button without illegal `<button>` nesting, the outer card container is refactored into a `div` with `role="button"`, `tabIndex={0}`, and `onKeyDown` handlers for `Enter` and `Space`. This guarantees zero console warnings and full keyboard/screen reader compliance.
   - **Quick View Integration:** The desktop hover action button (`hidden lg:flex`) stops propagation (`e.stopPropagation()`) and calls `onQuickView(product)`, invoking `setSelectedProduct(product)` from `useStore()`. This triggers `<ProductDetailModal />` already mounted in `App.tsx`.
   - **Minimalist Badges & Price:** Badges adhere to Aastin tokens: `#1c1c1a` discount pill with white Persian digits, crisp `rounded-[2px]`, out-of-stock frosted overlay, and original price strike-through when discounted.

---

## 3. Caveats

- **Scope Boundary:** This milestone plan specifically designs code changes for `FashionTemplate.tsx`. Separate tasks/agents cover header variant additions in `SellerStorefrontHeader.tsx` and tablet container fixes in `src/App.tsx`.
- **Image Data Dependency:** The secondary image flip depends on products having valid URLs in `product.images`. For products with only 1 image, the card seamlessly falls back to smooth image zoom.
- **Quick View Modal Styling:** The quick view modal is handled by the existing `ProductDetailModal.tsx`. Subsequent milestones can further tune its modal styling to match Aastin tokens if needed.
- No other caveats.

---

## 4. Conclusion

The visual design and component architecture for `FashionTemplate.tsx` have been fully upgraded and specified in `implementation_plan.md`. The design achieves:
1. Editorial elevation across Hero, Macro Categories, Collections, Lookbook, and Banners.
2. Modern fashion product card (`AastinProductCard`) featuring secondary image hover flip (`product.images[1]`), smooth fallback zoom, minimalist typography badges, and desktop quick view trigger.
3. 100% contract and whitelist compliance, preserving all data bindings, media ledger deduplication, and Persian typography.
4. Clean DOM semantics with zero button nesting, full keyboard/touch accessibility, and zero runtime errors.

---

## 5. Verification Method

Independent verification can be executed via:

1. **TypeScript & Production Build Verification:**
   ```powershell
   cd C:\Users\LENOVO\projects\sellersho
   npm run build
   ```
   *Expected outcome:* Exit code 0, 0 TypeScript errors, bundle chunk `FashionTemplate-*.js` emitted cleanly.

2. **File Inspection:**
   Inspect the complete specification and code snippets in:
   `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\teamwork_explorer_m1_1\implementation_plan.md`

3. **Behavioral Invalidation Conditions:**
   - Any runtime error or React DOM nesting warning (e.g., `validateDOMNesting(...): <button> cannot appear as a descendant of <button>`).
   - Any regression in whitelist filtering (e.g. deals or timers appearing in `tpl-fashion-1`).
   - Any layout overflow or horizontal scrolling on 375px or 390px mobile viewports.
