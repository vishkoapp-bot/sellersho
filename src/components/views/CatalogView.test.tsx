import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { register } from 'node:module';
import { activeStoreSlugOf } from '../../context/StoreContext';
import type { Product, Store } from '../../types';
import { ToastProvider } from '../../context/ToastContext';
import { setFakeStore } from './catalogViewStoreStub';
import type { FakeStoreValue } from './catalogViewStoreStub';

// Install the StoreContext -> stub redirect BEFORE CatalogView is
// loaded (the static imports above are already resolved by then; the
// component is imported dynamically below, so its transitive
// StoreContext imports go through the hook).
register('./catalogViewGuardHooks.mts', import.meta.url);

const { CatalogView } = await import('./CatalogView');

// Central-paint guard (ITEM 2): a shopper with a dropshipper
// currentStore (e.g. mone) opening the central catalog must never see
// the central assortment — not even for one paint before the
// store-guard effect bounces to the storefront. A product in 0
// assortments (the newly approved «تیشرت زنانه», prod-1cc63eae) used to
// flash in the central list and then vanish when the scoped storefront
// list replaced it.
//
// What this harness CAN cover: renderToStaticMarkup renders exactly
// the first paint (effects never run under SSR), so it is the precise
// tool for "no central paint before the bounce". What it CANNOT cover:
// the bounce effect itself firing (setActiveView in useEffect) — that
// needs a DOM + act(), which this harness (tsx --test, no jsdom)
// cannot run. The tests below pin the render side plus the scope
// derivation the effect relies on, and the setActiveView invariant via
// the source pin at the bottom.

const toPersianDigits = (value: number | string): string =>
  String(value).replace(/\d/g, (d) => '۰۱۲۳۴۵۶۷۸۹'[Number(d)]);

const tee: Product = {
  id: 'prod-1cc63eae',
  title: 'تیشرت زنانه',
  description: 'تیشرت زنانه',
  price: 850000,
  originalPrice: 1000000,
  discountPercent: 15,
  category: 'women-tee-shirts',
  stock: 5,
  isActive: true,
  productType: 'simple',
} as unknown as Product;

const other: Product = {
  ...tee,
  id: 'prod-other',
  title: 'شلوار مردانه',
  price: 1200000,
  originalPrice: 1200000,
  discountPercent: 0,
} as unknown as Product;

const centralStore = {
  id: 'central-store',
  slug: 'central',
  name: 'فروشگاه مرکزی',
  ownerType: 'central',
} as unknown as Store;

// The user's own store, carrying NEITHER product — the prod-1cc63eae
// case: approved centrally, in 0 dropshipper assortments.
const moneStore = {
  id: 'store-mone',
  slug: 'mone',
  name: 'mone',
  ownerType: 'dropshipper',
  activeProductIds: [],
} as unknown as Store;

const setActiveViewCalls: Array<{ view: string; navigation?: unknown }> = [];

const makeFakeStore = (currentStore: Store): FakeStoreValue =>
  ({
    products: [tee, other],
    selectedCategory: 'all',
    setSelectedCategory: () => {},
    searchQuery: '',
    setSearchQuery: () => {},
    toPersianDigits,
    formatPrice: (n: number) => String(n),
    categories: [],
    catalogLoading: false,
    catalogError: null,
    openCatalogCategory: () => {},
    catalogBrandRequest: null,
    clearCatalogBrand: () => {},
    setActiveView: ((view: string, navigation?: unknown) => {
      setActiveViewCalls.push({ view, navigation });
    }) as FakeStoreValue['setActiveView'],
    currentStore,
    routeStoreSlug: null,
    openProductDetail: () => {},
    addToCart: () => {},
    cart: [],
  }) as unknown as FakeStoreValue;

const renderCatalog = (currentStore: Store): string => {
  setActiveViewCalls.length = 0;
  setFakeStore(makeFakeStore(currentStore));
  return renderToStaticMarkup(
    <ToastProvider>
      <CatalogView onBack={() => {}} />
    </ToastProvider>,
  );
};

const countProductRows = (html: string): number =>
  (html.match(/data-testid="product-item"/g) ?? []).length;

describe('CatalogView central-paint guard', () => {
  it('renders no product rows for a dropshipper currentStore — no central paint before the bounce', () => {
    const html = renderCatalog(moneStore);
    assert.equal(countProductRows(html), 0);
    assert.doesNotMatch(html, /تیشرت زنانه/);
    assert.doesNotMatch(html, /شلوار مردانه/);
    assert.doesNotMatch(html, /کاتالوگ جامع کالاها/);
    // The render itself must not navigate either (navigation lives in
    // the effect, which SSR never runs — this pins that no render path
    // calls setActiveView).
    assert.deepEqual(setActiveViewCalls, []);
  });

  it('renders the central list for a central currentStore and never bounces', () => {
    const html = renderCatalog(centralStore);
    assert.equal(countProductRows(html), 2);
    assert.match(html, /تیشرت زنانه/);
    assert.match(html, /شلوار مردانه/);
    assert.match(html, /کاتالوگ جامع کالاها/);
    assert.deepEqual(setActiveViewCalls, []);
  });

  it('derives the guard scope from the store: dropshipper suppresses, central does not', () => {
    assert.equal(activeStoreSlugOf({ routeStoreSlug: null, currentStore: moneStore }), 'mone');
    assert.equal(activeStoreSlugOf({ routeStoreSlug: null, currentStore: centralStore }), null);
    // A routed store scope wins even over a central session store.
    assert.equal(activeStoreSlugOf({ routeStoreSlug: 'mone', currentStore: centralStore }), 'mone');
  });

  it("setActiveView('catalog') leaves currentStore untouched", () => {
    // This harness cannot mount the real StoreProvider and drive a
    // state update (no DOM + act()), so the invariant is pinned at the
    // source: setActiveView's body must never call setCurrentStore — a
    // 'catalog' navigation only clears the route scope
    // (routeStoreSlug) and rewrites the URL. If the deeper fix
    // (resetting currentStore on central navigation) is ever
    // implemented, this test must be updated alongside a DOM-level
    // test of the new behaviour.
    const storeContextPath = path.resolve(
      path.dirname(fileURLToPath(import.meta.url)),
      '../../context/StoreContext.tsx',
    );
    const body = extractFunctionBody(
      readFileSync(storeContextPath, 'utf8'),
      'const setActiveView = (view: ActiveView',
    );
    assert.doesNotMatch(body, /setCurrentStore/);
    assert.match(body, /setRouteStoreSlug\(null\)/);
  });
});

// Brace-matched extraction of a top-level const arrow function's body.
// The setActiveView body holds no braces inside string literals, so a
// plain depth count is exact here; a refactor that breaks that (or
// moves/renames the function) fails loudly instead of silently passing.
const extractFunctionBody = (source: string, marker: string): string => {
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `${marker} not found`);
  // The signature itself carries a default object (`= {}`), so the
  // body starts at the first brace AFTER the arrow, not the first
  // brace after the marker.
  const arrow = source.indexOf('=>', start);
  const open = source.indexOf('{', arrow);
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    if (source[i] === '{') depth++;
    else if (source[i] === '}') {
      depth--;
      if (depth === 0) return source.slice(open, i + 1);
    }
  }
  throw new Error(`unbalanced braces after ${marker}`);
};
