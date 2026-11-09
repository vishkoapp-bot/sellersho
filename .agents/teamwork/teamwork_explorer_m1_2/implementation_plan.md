# Implementation Plan: Storefront Shell Harmonization for Fashion Template (`tpl-fashion-1`)

**Author:** `teamwork_explorer_m1_2`  
**Milestone:** Milestone 1 — Storefront Shell Components  
**Target Components:**
1. `src/components/storefront/SellerStorefrontHeader.tsx`
2. `src/components/storefront/SellerStorefrontView.tsx`
3. `src/components/storefront/SellerStorefrontFooter.tsx`  
*(Additional integration point: `src/App.tsx` for store-scoped PDP header)*  
**Date:** 2026-11-07  

---

## 1. Executive Summary & Design Rationale

The Sellersho Fashion Storefront Template (`tpl-fashion-1`) implements a high-end editorial design language inspired by `aassttiinn.com`:
- **Canvas:** `#faf8f5` (warm cream/off-white)
- **Ink / Typography:** `#1c1c1a` (deep charcoal black)
- **Dividers & Borders:** `#e5e0d8` (hairline rule)
- **Dark Editorial Surface / Footer Band:** `#3a4441` (charcoal slate-green)
- **Geometry:** Architectural sharp corners (`rounded-[2px]` or `rounded-none`).

### The Architectural Problem:
Previously, both the storefront header (`SellerStorefrontHeader`) and footer (`SellerStorefrontFooter`) rendered the generic "modern" marketplace shell:
1. **Header Conflict:** The header rendered a beige `#FAF8F4` background with a bright teal `#0D7377` round circular logo badge, rounded-full pill buttons, and inconsistent padding that clashed severely with the editorial lookbook aesthetic.
2. **Footer Conflict:** The footer rendered with a dark `#1C1917` surface, an intrusive `rounded-t-[32px]` arch, an 8px top margin (`mt-2`), and teal `#0D7377` circular icon frames. When placed directly beneath `FashionTemplate`'s `#3a4441` newsletter band, the `rounded-t-[32px]` arch cut into the page with an awkward cutout and background void.
3. **Container Mismatch:** The header and footer used arbitrary or hardcoded paddings rather than aligning with `FashionTemplate`'s canonical container grid (`max-w-[1440px] px-4 lg:px-10`).

### The Solution:
This plan defines the concrete, type-safe implementation specifications to seamlessly integrate `SellerStorefrontHeader`, `SellerStorefrontView`, and `SellerStorefrontFooter` with `tpl-fashion-1`.

---

## 2. Component 1: `src/components/storefront/SellerStorefrontHeader.tsx`

### 2.1 Interface & Props Specification
Update `Props` to support the `'fashion'` variant:

```typescript
interface Props {
  onOpenSearch: () => void;
  variant?: 'modern' | 'digikala' | 'fashion';
  searchQuery?: string;
  onSearchChange?: (q: string) => void;
}
```

### 2.2 Dedicated `variant === 'fashion'` Architecture
When `variant === 'fashion'`, render a dedicated glassmorphic editorial header:

1. **Backdrop & Container:**
   - Outer element: `<header className="sticky top-0 z-30 bg-[#faf8f5]/90 backdrop-blur-md border-b border-[#e5e0d8]">`
   - Inner container: `<div className="mx-auto w-full max-w-[1440px] flex items-center justify-between px-4 lg:px-10 py-2.5">`
   - Perfectly aligns with `FashionTemplate`'s `CONTAINER` constant (`max-w-[1440px] px-4 lg:px-10`).

2. **Brand & Logo Presentation (Eliminating Teal Circle):**
   - Click handler: `onClick={goToStoreHome}` with `title={`خانه ${currentStore.name}`}`.
   - When merchant has an image logo (`currentStore.logo && !logoFailed`):
     - Frame: `w-10 h-10 rounded-[2px] overflow-hidden border border-[#e5e0d8] bg-white flex items-center justify-center shrink-0`
     - `<img src={currentStore.logo} alt={currentStore.name} className="w-full h-full object-cover" onError={() => setLogoFailed(true)} />`
   - When no image logo exists (fallback monogram):
     - Frame: `w-10 h-10 rounded-[2px] bg-[#1c1c1a] text-[#faf8f5] flex items-center justify-center font-black text-base shrink-0 select-none`
     - Renders `{initial}` (the first letter of store name) in cream on charcoal. **No teal `#0D7377` circle!**
   - Store Name: `<h1 className="font-black text-base lg:text-lg tracking-tight leading-tight truncate text-[#1c1c1a]">{currentStore.name}</h1>`
   - Tagline: `{tagline !== '' && <span className="text-[11px] font-medium text-[#737373] mt-0.5 block line-clamp-1">{tagline}</span>}`

3. **Minimal Monochrome Icon Action Buttons:**
   - Touch Target: `w-11 h-11 flex items-center justify-center rounded-[2px]` (exact 44px × 44px hit-target for Apple HIG / accessibility).
   - Color: `text-[#1c1c1a] hover:bg-[#efeae3]/80 transition-colors`.
   - **Search Button:**
     - `onClick={onOpenSearch}`, `title="جستجو در محصولات"`, `<Search className="w-5 h-5" strokeWidth={1.5} />`
   - **Wishlist Button:**
     - `onClick={() => setActiveView('customer_wishlist')}`, `title="علاقه‌مندی‌ها"`, `<Heart className="w-5 h-5" strokeWidth={1.5} />`
   - **Cart Button:**
     - `onClick={() => setIsCartOpen(true)}`, `title="سبد خرید"`, `<ShoppingCart className="w-5 h-5" strokeWidth={1.5} />`
     - Cart badge: Sleek charcoal pill with cream digits:
       `<span className="absolute top-1.5 right-1.5 text-[#faf8f5] bg-[#1c1c1a] font-bold text-[9px] min-w-4 h-4 px-0.5 rounded-full flex items-center justify-center" style={{ boxShadow: '0 0 0 2px #faf8f5' }}>{cartItemsCount > 99 ? '۹۹+' : toPersianDigits(cartItemsCount)}</span>`
   - **Account Button:**
     - `onClick={() => setActiveView('customer_profile')}`, `title={(customerUser?.isLoggedIn || customerUser?.mobile) ? 'حساب کاربری' : 'ورود به حساب کاربری'}`, `<User className="w-5 h-5" strokeWidth={1.5} />`

### 2.3 Exact Code Replacement for `SellerStorefrontHeader.tsx`

```tsx
<<<<
interface Props {
  onOpenSearch: () => void;
  variant?: 'modern' | 'digikala';
  searchQuery?: string;
  onSearchChange?: (q: string) => void;
}
====
interface Props {
  onOpenSearch: () => void;
  variant?: 'modern' | 'digikala' | 'fashion';
  searchQuery?: string;
  onSearchChange?: (q: string) => void;
}
>>>>
```

Insert before the final `modern` return statement:

```tsx
  if (variant === 'fashion') {
    return (
      <header className="sticky top-0 z-30 bg-[#faf8f5]/90 backdrop-blur-md border-b border-[#e5e0d8]">
        <div className="mx-auto w-full max-w-[1440px] flex items-center justify-between px-4 lg:px-10 py-2.5">
          <div
            className="flex items-center gap-3 min-w-0 cursor-pointer"
            onClick={goToStoreHome}
            title={`خانه ${currentStore.name}`}
          >
            {currentStore.logo && !logoFailed ? (
              <div className="w-10 h-10 rounded-[2px] overflow-hidden border border-[#e5e0d8] bg-white flex items-center justify-center shrink-0">
                <img
                  src={currentStore.logo}
                  alt={currentStore.name}
                  className="w-full h-full object-cover"
                  onError={() => setLogoFailed(true)}
                />
              </div>
            ) : (
              <div className="w-10 h-10 rounded-[2px] bg-[#1c1c1a] text-[#faf8f5] flex items-center justify-center font-black text-base shrink-0 select-none">
                {initial}
              </div>
            )}
            <div className="min-w-0">
              <h1 className="font-black text-base lg:text-lg tracking-tight leading-tight truncate text-[#1c1c1a]">
                {currentStore.name}
              </h1>
              {tagline !== '' && (
                <span className="text-[11px] font-medium text-[#737373] mt-0.5 block line-clamp-1">
                  {tagline}
                </span>
              )}
            </div>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            <button
              onClick={onOpenSearch}
              className="w-11 h-11 flex items-center justify-center rounded-[2px] text-[#1c1c1a] hover:bg-[#efeae3]/80 transition-colors"
              title="جستجو در محصولات"
              aria-label="جستجو در محصولات"
            >
              <Search className="w-5 h-5" strokeWidth={1.5} />
            </button>
            <button
              onClick={() => setActiveView('customer_wishlist')}
              className="w-11 h-11 flex items-center justify-center rounded-[2px] text-[#1c1c1a] hover:bg-[#efeae3]/80 transition-colors"
              title="علاقه‌مندی‌ها"
              aria-label="علاقه‌مندی‌ها"
            >
              <Heart className="w-5 h-5" strokeWidth={1.5} />
            </button>
            <button
              onClick={() => setIsCartOpen(true)}
              className="relative w-11 h-11 flex items-center justify-center rounded-[2px] text-[#1c1c1a] hover:bg-[#efeae3]/80 transition-colors"
              title="سبد خرید"
              aria-label="سبد خرید"
            >
              <ShoppingCart className="w-5 h-5" strokeWidth={1.5} />
              {cartItemsCount > 0 && (
                <span
                  className="absolute top-1.5 right-1.5 text-[#faf8f5] bg-[#1c1c1a] font-bold text-[9px] min-w-4 h-4 px-0.5 rounded-full flex items-center justify-center"
                  style={{ boxShadow: '0 0 0 2px #faf8f5' }}
                >
                  {cartItemsCount > 99 ? '۹۹+' : toPersianDigits(cartItemsCount)}
                </span>
              )}
            </button>
            <button
              onClick={() => setActiveView('customer_profile')}
              className="w-11 h-11 flex items-center justify-center rounded-[2px] text-[#1c1c1a] hover:bg-[#efeae3]/80 transition-colors"
              title={(customerUser?.isLoggedIn || customerUser?.mobile) ? 'حساب کاربری' : 'ورود به حساب کاربری'}
              aria-label={(customerUser?.isLoggedIn || customerUser?.mobile) ? 'حساب کاربری' : 'ورود به حساب کاربری'}
            >
              <User className="w-5 h-5" strokeWidth={1.5} />
            </button>
          </div>
        </div>
      </header>
    );
  }
```

---

## 3. Component 2: `src/components/storefront/SellerStorefrontView.tsx`

### 3.1 Template Resolution & Variant Propagation
In `SellerStorefrontView.tsx`, the active template is identified via `resolvedTemplateId`. We update both the empty-storefront and active template branches to pass `variant="fashion"` when `resolvedTemplateId === 'tpl-fashion-1'`.

### 3.2 Exact Code Changes for `SellerStorefrontView.tsx`

#### Change 1: Empty Storefront Branch (lines 220–226 & line 259)
```tsx
<<<<
    if (items.length === 0) {
      return (
        <div className="flex-1 flex flex-col bg-white">
          <SellerStorefrontHeader
            onOpenSearch={() => setIsSearchOpen(true)}
            variant={resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
          />
====
    if (items.length === 0) {
      return (
        <div className={`flex-1 flex flex-col ${resolvedTemplateId === 'tpl-fashion-1' ? 'bg-[#faf8f5]' : 'bg-white'}`}>
          <SellerStorefrontHeader
            onOpenSearch={() => setIsSearchOpen(true)}
            variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
          />
>>>>
```
And in line 259:
```tsx
<<<<
          <SellerStorefrontFooter />
====
          <SellerStorefrontFooter variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
>>>>
```

#### Change 2: Standard Storefront Branch (lines 264–273 & line 287)
```tsx
<<<<
    return (
      <div className="flex-1 flex flex-col bg-white">
        {/* FIX 13: per-template header — Digikala's own header (2 buttons
            + always-visible search) only in the template path; the
            empty path above always uses the Modern header. */}
        <SellerStorefrontHeader
          onOpenSearch={() => setIsSearchOpen(true)}
          variant={resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
        />
====
    return (
      <div className={`flex-1 flex flex-col ${resolvedTemplateId === 'tpl-fashion-1' ? 'bg-[#faf8f5]' : 'bg-white'}`}>
        {/* Per-template header: Fashion editorial header, Digikala header, or Modern default */}
        <SellerStorefrontHeader
          onOpenSearch={() => setIsSearchOpen(true)}
          variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
        />
>>>>
```
And in line 287:
```tsx
<<<<
        <SellerStorefrontFooter />
====
        <SellerStorefrontFooter variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
>>>>
```

---

## 4. Component 3: `src/components/storefront/SellerStorefrontFooter.tsx`

### 4.1 Interface & Props Specification
Update `SellerStorefrontFooter` to accept optional `variant`:

```typescript
interface Props {
  variant?: 'modern' | 'digikala' | 'fashion';
}
```

Resolve fashion mode robustly:
```typescript
const isFashion = variant === 'fashion' || currentStore.templateId === 'tpl-fashion-1';
```

### 4.2 Seamless Join with `FashionTemplate`'s `#3a4441` Band
1. **Remove `rounded-t-[32px]` and `mt-2`:**
   - Modern: `className="rounded-t-[32px] mt-2"`
   - Fashion: `className="mt-0 rounded-none"`
2. **Harmonize Background Color:**
   - Modern: `backgroundColor: '#1C1917'`
   - Fashion: `backgroundColor: '#3a4441'`  
     *(Matches `FashionTemplate`'s `AastinNewsletter` background `#3a4441` exactly. When the newsletter is rendered, the footer joins without any seam, gap, or color clash!)*
3. **Container Alignment:**
   - Modern: `px-6 pt-8 pb-[84px]`
   - Fashion: `px-6 pt-8 pb-[84px] mx-auto w-full max-w-[1440px] lg:px-10`  
     *(Matches `FashionTemplate`'s 1440px container)*
4. **Eliminate Teal `#0D7377` Accents:**
   - Store Icon Frame: In fashion, `rounded-[2px] bg-white/10 text-white` (replaces `${ACCENT}26` round circle).
   - Phone & Instagram Buttons: In fashion, `rounded-[2px] border border-white/20 bg-white/5 hover:border-white/40 text-stone-200` with white icons (`min-h-[44px]`).
5. **Touch Targets for Nav Links:**
   - Navigation links (`خانه`, `محصولات`, `تماس`, `درباره ما`) styled with `min-h-[44px] inline-flex items-center` for standard touch targets.
6. **Harmonize `StorefrontBottomNav`:**
   - Inside `StorefrontBottomNav`, resolve `const isFashion = currentStore?.templateId === 'tpl-fashion-1';`.
   - On fashion template:
     - Outer nav: `backgroundColor: '#faf8f5'`, `borderColor: '#e5e0d8'`
     - Active tab: `backgroundColor: '#1c1c1a'`, `color: '#faf8f5'`, `borderRadius: '2px'` (sharp monochrome active tab instead of teal round pill).

### 4.3 Exact Code Replacement for `SellerStorefrontFooter.tsx`

```tsx
<<<<
const ACCENT = '#0D7377';

export const SellerStorefrontFooter: React.FC = () => {
  const { currentStore, openStaticPage, toPersianDigits, products } = useStore();
====
const ACCENT = '#0D7377';

interface Props {
  variant?: 'modern' | 'digikala' | 'fashion';
}

export const SellerStorefrontFooter: React.FC<Props> = ({ variant = 'modern' }) => {
  const { currentStore, openStaticPage, toPersianDigits, products } = useStore();
  const isFashion = variant === 'fashion' || currentStore.templateId === 'tpl-fashion-1';
>>>>
```

Footer wrapper and contents:
```tsx
<<<<
    <footer className="rounded-t-[32px] mt-2" style={{ backgroundColor: '#1C1917' }}>
      <div className="px-6 pt-8 pb-[84px]">
        <div className="flex items-center gap-2.5">
          <div
            className="w-10 h-10 rounded-full flex items-center justify-center"
            style={{ backgroundColor: `${ACCENT}26` }}
          >
            <Store className="w-5 h-5" strokeWidth={1.5} style={{ color: ACCENT }} />
          </div>
          <h2 className="text-lg font-black text-white">{currentStore.name}</h2>
        </div>
====
    <footer
      className={isFashion ? 'mt-0 rounded-none' : 'rounded-t-[32px] mt-2'}
      style={{ backgroundColor: isFashion ? '#3a4441' : '#1C1917' }}
    >
      <div className={`px-6 pt-8 pb-[84px] ${isFashion ? 'mx-auto w-full max-w-[1440px] lg:px-10' : ''}`}>
        <div className="flex items-center gap-2.5">
          <div
            className={`w-10 h-10 flex items-center justify-center shrink-0 ${
              isFashion ? 'rounded-[2px] bg-white/10 text-white' : 'rounded-full'
            }`}
            style={isFashion ? undefined : { backgroundColor: `${ACCENT}26` }}
          >
            <Store className="w-5 h-5" strokeWidth={1.5} style={isFashion ? { color: '#ffffff' } : { color: ACCENT }} />
          </div>
          <h2 className="text-lg font-black text-white">{currentStore.name}</h2>
        </div>
>>>>
```

Phone & Instagram row:
```tsx
<<<<
        {(phone !== '' || instagram !== '') && (
          <div className="flex flex-wrap gap-2 mt-5">
            {phone !== '' && (
              <a
                href={`tel:${phone}`}
                className="inline-flex items-center gap-2 rounded-full border border-white/15 px-4 py-2 text-[13px] text-stone-200 transition-colors hover:border-white/30"
              >
                <Phone className="w-4 h-4" style={{ color: ACCENT }}  strokeWidth={1.5}/>
                <span dir="ltr">{phone}</span>
              </a>
            )}
            {instagram !== '' && (
              <a
                href={`https://instagram.com/${instagramHandle}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-white/15 px-4 py-2 text-[13px] text-stone-200 transition-colors hover:border-white/30"
              >
                <Instagram className="w-4 h-4" style={{ color: ACCENT }}  strokeWidth={1.5}/>
                <span dir="ltr">@{instagramHandle}</span>
              </a>
            )}
          </div>
        )}
====
        {(phone !== '' || instagram !== '') && (
          <div className="flex flex-wrap gap-2 mt-5">
            {phone !== '' && (
              <a
                href={`tel:${phone}`}
                className={`inline-flex items-center gap-2 min-h-[44px] px-4 py-2 text-[13px] text-stone-200 transition-colors ${
                  isFashion
                    ? 'rounded-[2px] border border-white/20 bg-white/5 hover:border-white/40'
                    : 'rounded-full border border-white/15 hover:border-white/30'
                }`}
              >
                <Phone className="w-4 h-4" style={isFashion ? { color: '#ffffff' } : { color: ACCENT }} strokeWidth={1.5} />
                <span dir="ltr">{phone}</span>
              </a>
            )}
            {instagram !== '' && (
              <a
                href={`https://instagram.com/${instagramHandle}`}
                target="_blank"
                rel="noreferrer"
                className={`inline-flex items-center gap-2 min-h-[44px] px-4 py-2 text-[13px] text-stone-200 transition-colors ${
                  isFashion
                    ? 'rounded-[2px] border border-white/20 bg-white/5 hover:border-white/40'
                    : 'rounded-full border border-white/15 hover:border-white/30'
                }`}
              >
                <Instagram className="w-4 h-4" style={isFashion ? { color: '#ffffff' } : { color: ACCENT }} strokeWidth={1.5} />
                <span dir="ltr">@{instagramHandle}</span>
              </a>
            )}
          </div>
        )}
>>>>
```

Navigation links touch targets:
```tsx
<<<<
        <nav aria-label="ناوبری فروشگاه" className="flex items-center gap-5 mt-6">
          <button onClick={scrollToTop} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            خانه
          </button>
          <button onClick={() => setAllProductsOpen(true)} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            محصولات
          </button>
          <button onClick={scrollToBottom} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            تماس
          </button>
        </nav>
        <nav aria-label="لینک‌های اعتماد" className="flex flex-wrap items-center gap-x-5 gap-y-2 mt-5">
          <button onClick={goToStoreAbout} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            درباره ما
          </button>
          <button onClick={() => openStaticPage('terms')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            شرایط استفاده
          </button>
          <button onClick={() => openStaticPage('privacy')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            حریم خصوصی
          </button>
          <button onClick={() => openStaticPage('faq')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            پرسش‌های متداول
          </button>
        </nav>
====
        <nav aria-label="ناوبری فروشگاه" className="flex items-center gap-5 mt-6">
          <button onClick={scrollToTop} className="min-h-[44px] inline-flex items-center text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            خانه
          </button>
          <button onClick={() => setAllProductsOpen(true)} className="min-h-[44px] inline-flex items-center text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            محصولات
          </button>
          <button onClick={scrollToBottom} className="min-h-[44px] inline-flex items-center text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            تماس
          </button>
        </nav>
        <nav aria-label="لینک‌های اعتماد" className="flex flex-wrap items-center gap-x-5 gap-y-2 mt-5">
          <button onClick={goToStoreAbout} className="min-h-[36px] inline-flex items-center text-[11px] text-stone-400 transition-colors hover:text-stone-200">
            درباره ما
          </button>
          <button onClick={() => openStaticPage('terms')} className="min-h-[36px] inline-flex items-center text-[11px] text-stone-400 transition-colors hover:text-stone-200">
            شرایط استفاده
          </button>
          <button onClick={() => openStaticPage('privacy')} className="min-h-[36px] inline-flex items-center text-[11px] text-stone-400 transition-colors hover:text-stone-200">
            حریم خصوصی
          </button>
          <button onClick={() => openStaticPage('faq')} className="min-h-[36px] inline-flex items-center text-[11px] text-stone-400 transition-colors hover:text-stone-200">
            پرسش‌های متداول
          </button>
        </nav>
>>>>
```

`StorefrontBottomNav` styling:
```tsx
<<<<
export const StorefrontBottomNav: React.FC = () => {
  const { setActiveView, currentStore } = useStore();
  const [active, setActive] = React.useState<'home' | 'categories' | 'account' | 'contact'>('home');
====
export const StorefrontBottomNav: React.FC = () => {
  const { setActiveView, currentStore } = useStore();
  const [active, setActive] = React.useState<'home' | 'categories' | 'account' | 'contact'>('home');
  const isFashion = currentStore?.templateId === 'tpl-fashion-1';
>>>>
```
And inside `StorefrontBottomNav` JSX:
```tsx
<<<<
    <nav
      className="fixed bottom-0 z-30 border-t border-x lg:hidden"
      style={{
        left: 0,
        right: 0,
        marginLeft: 'auto',
        marginRight: 'auto',
        width: '100%',
        maxWidth: 504,
        backgroundColor: '#ffffff',
        borderColor: '#ECE4D6',
        boxShadow: '0 -6px 24px rgba(28, 25, 23, 0.08)',
        paddingBottom: 'env(safe-area-inset-bottom)',
      }}
    >
      <div className="flex items-stretch justify-center px-2 py-1.5">
        {tabs.map(({ id, label, Icon }) => {
          const isActive = active === id;
          return (
            <button
              key={id}
              onClick={() => go(id)}
              className="flex-1 min-w-0 flex flex-col items-center justify-center gap-1 px-1 py-2 rounded-2xl transition-all duration-200 ease-out active:scale-95"
              style={{
                backgroundColor: isActive ? `${ACCENT}16` : 'transparent',
                color: isActive ? ACCENT : '#A8A29E',
              }}
            >
              <Icon className="w-[22px] h-[22px]" strokeWidth={isActive ? 2 : 1.5} />
              <span className="text-[10px] font-bold leading-none truncate max-w-full">{label}</span>
            </button>
          );
        })}
      </div>
    </nav>
====
    <nav
      className="fixed bottom-0 z-30 border-t border-x lg:hidden"
      style={{
        left: 0,
        right: 0,
        marginLeft: 'auto',
        marginRight: 'auto',
        width: '100%',
        maxWidth: 504,
        backgroundColor: isFashion ? '#faf8f5' : '#ffffff',
        borderColor: isFashion ? '#e5e0d8' : '#ECE4D6',
        boxShadow: '0 -6px 24px rgba(28, 25, 23, 0.08)',
        paddingBottom: 'env(safe-area-inset-bottom)',
      }}
    >
      <div className="flex items-stretch justify-center px-2 py-1.5">
        {tabs.map(({ id, label, Icon }) => {
          const isActive = active === id;
          return (
            <button
              key={id}
              onClick={() => go(id)}
              className="flex-1 min-w-0 flex flex-col items-center justify-center gap-1 px-1 py-2 transition-all duration-200 ease-out active:scale-95"
              style={{
                borderRadius: isFashion ? '2px' : '16px',
                backgroundColor: isActive ? (isFashion ? '#1c1c1a' : `${ACCENT}16`) : 'transparent',
                color: isActive ? (isFashion ? '#faf8f5' : ACCENT) : isFashion ? '#737373' : '#A8A29E',
              }}
            >
              <Icon className="w-[22px] h-[22px]" strokeWidth={isActive ? 2 : 1.5} />
              <span className="text-[10px] font-bold leading-none truncate max-w-full">{label}</span>
            </button>
          );
        })}
      </div>
    </nav>
>>>>
```

---

## 5. Additional Integration Point: `src/App.tsx` (PDP Header)

In `src/App.tsx`, lines 285–288:
When a user navigates from a store-scoped fashion storefront to a Product Detail Page (`product_detail`), `SellerStorefrontHeader` is rendered above the PDP.

Update line 287 from:
```tsx
variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
```
to:
```tsx
variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
```
This guarantees that visiting a product inside a fashion merchant store retains the sleek glassmorphic fashion header throughout the entire purchase journey.

---

## 6. Responsive Validation & Viewport Matrix

| Viewport Width | Device Target | Header Behavior | Footer Behavior | Bottom Nav Behavior |
|---|---|---|---|---|
| **375px** | iPhone SE / Compact Mobile | Full-bleed with `px-4`, store name truncated cleanly, 44px hit-targets for all icon buttons. | Full-bleed with `px-6`, contact pills wrap cleanly, touch targets $\ge 44$px. | Visible (`lg:hidden`), 4 tabs with 2px active indicator, safe-area inset padded. |
| **390px** | iPhone 12/13/14/15/16 | Full-bleed with `px-4`, comfortable spacing between title and icons. | Full-bleed with `px-6`, smooth stacking on top of bottom nav with `pb-[84px]`. | Visible (`lg:hidden`), comfortable 44px hit height. |
| **768px** | iPad Portrait / Android Tablets | Fluid container, glassmorphic blur active, title scales up to `lg:text-lg`. | Seamless join with `#3a4441` newsletter band, straight border without arch cutout. | Visible (`lg:hidden`), centered up to 504px. |
| **1280px+** | Desktop & High-Res Screens | Contained to `max-w-[1440px] px-10`, perfectly aligned with `FashionTemplate` sections. | Contained to `max-w-[1440px] px-10`, multi-column links. | Automatically hidden (`lg:hidden`). |

---

## 7. Verification Method

1. **Static Type Checking & Compilation:**
   ```powershell
   cd C:\Users\LENOVO\projects\sellersho; npm run build
   ```
   Must exit with code 0. Verifies TypeScript compiler (`tsc`) accepts `'fashion'` variant and all prop signatures.
2. **Visual Inspection Criteria:**
   - Navigating to `/stores/test-fashion` or any store configured with `tpl-fashion-1`:
     - Top header has `bg-[#faf8f5]/90 backdrop-blur-md` and hairline `#e5e0d8` border.
     - Store monogram/logo is sharp square (`rounded-[2px]`), NO teal `#0D7377` round circle exists.
     - Cart badge is `#1c1c1a` with white numbers.
     - Footer sits directly below the `#3a4441` newsletter band with NO gap (`mt-0`) and NO 32px rounded arch (`rounded-none`).
     - Footer background is `#3a4441`, joining seamlessly with the newsletter.
     - Mobile bottom nav active indicator uses `#1c1c1a` instead of teal `#0D7377`.
