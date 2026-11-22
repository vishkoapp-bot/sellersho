import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { AuthProvider } from '../../../src/context/AuthContext';
import { ProductProvider } from '../../../src/context/ProductContext';
import { CartProvider } from '../../../src/context/CartContext';
import { OrderProvider } from '../../../src/context/OrderContext';
import { FinanceProvider } from '../../../src/context/FinanceContext';
import { WarehouseProvider } from '../../../src/context/WarehouseContext';
import { StoreProvider, useStore } from '../../../src/context/StoreContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { Header } from '../../../src/components/Header';
import { catalogApi } from '../../../src/services/catalogApi';
import { storeApi } from '../../../src/services/storeApi';

// A2 header fixture: the real Header mounted on the full provider
// tree, with a DEFERRED suggestion read. Only API boundaries are
// synthetic; all contexts and the Header are production code.
const h: any = { calls: [], pending: null };
(window as any).headerHarness = h;
const stores: any[] = [
  { id: 'central-store', slug: 'central', name: 'Central', ownerType: 'central', activeProductIds: ['prod-1cc63eae', 'prod-other'] },
  { id: 'store-mone', slug: 'mone', name: 'mone', ownerType: 'dropshipper', activeProductIds: [] },
];
const products: any[] = [
  { id: 'prod-1cc63eae', title: 'تیشرت زنانه', description: 'تیشرت زنانه', price: 850000, originalPrice: 1000000, discountPercent: 15, category: 'women-tee-shirts', stock: 5, isActive: true },
  { id: 'prod-other', title: 'شلوار مردانه', description: 'شلوار مردانه', price: 1200000, originalPrice: 1200000, discountPercent: 0, category: 'women-tee-shirts', stock: 5, isActive: true },
].map(product => ({ ...product, slug: product.id, supplierId: 'fixture-supplier', supplierName: 'Fixture', platformFeeF: 0, image: '/templates/fashion/knitwear.webp', images: ['/templates/fashion/knitwear.webp'] }));
storeApi.getAllStores = async () => stores;
storeApi.getOwnedStores = async () => stores;
storeApi.getStoreBySlug = async slug => stores.find(store => store.slug === slug);
catalogApi.getProducts = async (params = {}) => params.storeSlug === 'mone' ? [] : products;
catalogApi.getCategories = async () => [];
catalogApi.getNiches = async () => [];
catalogApi.getPriceBounds = async () => ({ minPrice: 850000, maxPrice: 1200000, count: 2 });
// A2: a suggestion read (suggest=1) returns a promise that settles
// ONLY when the test resolves it, and rejects the moment its
// AbortSignal fires — so the test can observe the timer-cancel half
// (no call at all) and the in-flight-abort half (call aborted)
// separately. Ordinary page reads answer immediately, so
// ProductContext's first page load and the catalog navigation after
// Enter both proceed.
catalogApi.getProductsPage = async (params: any = {}, opts: any = {}) => {
  if (!params.suggest) {
    const items = products.filter((p: any) =>
      (params.minPrice == null || p.price >= params.minPrice) &&
      (params.maxPrice == null || p.price <= params.maxPrice));
    return { items, nextCursor: null, hasMore: false, total: items.length };
  }
  const call: any = { params: { ...params }, aborted: false, settled: false, at: Date.now() };
  h.calls.push(call);
  return new Promise((resolve, reject) => {
    const onAbort = () => { call.aborted = true; reject(new DOMException('The user aborted a request.', 'AbortError')); };
    if (opts?.signal?.aborted) { onAbort(); return; }
    opts?.signal?.addEventListener('abort', onAbort, { once: true });
    h.pending = { call, resolve: (items: any[]) => { call.settled = true; resolve({ items, nextCursor: null, hasMore: false, total: items.length }); } };
  });
};
const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => String(input).includes('/api/')
  ? new Response('{}', { headers: { 'Content-Type': 'application/json' } })
  : nativeFetch(input, init);

function Probe() {
  const store = useStore();
  const [show, setShow] = useState(false);
  h.state = () => ({ slug: store.currentStore.slug, view: store.activeView, products: store.products.length, loading: store.catalogLoading, query: store.searchQuery });
  h.open = () => setShow(true);
  // Resolve the pending suggestion fetch with the given DTOs (the
  // control half: a resolving fetch opens the dropdown while the
  // input still holds focus).
  h.resolveSuggestion = (items: any[]) => h.pending?.resolve(items);
  h.suggestCalls = () => h.calls.filter((c: any) => c.params.suggest === true);
  h.dropdownOpen = () => {
    const input = document.querySelector('header input[type="text"]') as HTMLInputElement | null;
    const wrap = input?.closest('div.relative');
    return !!wrap?.querySelector('div.absolute.top-full');
  };
  h.dropdownText = () => {
    const input = document.querySelector('header input[type="text"]') as HTMLInputElement | null;
    const wrap = input?.closest('div.relative');
    return wrap?.querySelector('div.absolute.top-full')?.textContent ?? '';
  };
  return show ? <Header /> : <div id="header-ready" />;
}
createRoot(document.getElementById('root')!).render(
  <AuthProvider><ProductProvider><CartProvider><OrderProvider><FinanceProvider><WarehouseProvider><ToastProvider>
    <StoreProvider><Probe /></StoreProvider>
  </ToastProvider></WarehouseProvider></FinanceProvider></OrderProvider></CartProvider></ProductProvider></AuthProvider>,
);
