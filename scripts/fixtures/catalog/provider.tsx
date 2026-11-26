import React, { useLayoutEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { AuthProvider } from '../../../src/context/AuthContext';
import { ProductProvider } from '../../../src/context/ProductContext';
import { CartProvider } from '../../../src/context/CartContext';
import { OrderProvider } from '../../../src/context/OrderContext';
import { FinanceProvider } from '../../../src/context/FinanceContext';
import { WarehouseProvider } from '../../../src/context/WarehouseContext';
import { StoreProvider, useStore } from '../../../src/context/StoreContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { CatalogView } from '../../../src/components/views/CatalogView';
import { PriceRangeSlider } from '../../../src/components/PriceRangeSlider';
import { ApiError } from '../../../src/services/apiClient';
import { catalogApi } from '../../../src/services/catalogApi';
import { storeApi } from '../../../src/services/storeApi';

const h: any = { paints: [] };
(window as any).catalogHarness = h;
const stores: any[] = [
  { id: 'central-store', slug: 'central', name: 'Central', ownerType: 'central', activeProductIds: ['prod-1cc63eae', 'prod-other'] },
  { id: 'store-mone', slug: 'mone', name: 'mone', ownerType: 'dropshipper', activeProductIds: [] },
];
const products: any[] = [
  { id: 'prod-1cc63eae', title: 'تیشرت زنانه', description: 'تیشرت زنانه', price: 850000, originalPrice: 1000000, discountPercent: 15, category: 'women-tee-shirts', stock: 5, isActive: true },
  { id: 'prod-other', title: 'شلوار مردانه', description: 'شلوار مردانه', price: 1200000, originalPrice: 1200000, discountPercent: 0, category: 'women-tee-shirts', stock: 5, isActive: true },
].map(product => ({ ...product, slug: product.id, supplierId: 'fixture-supplier', supplierName: 'Fixture', platformFeeF: 0, image: '/templates/fashion/knitwear.webp', images: ['/templates/fashion/knitwear.webp'] }));
// Only API boundaries are synthetic. All contexts and the view are production code.
storeApi.getAllStores = async () => stores;
storeApi.getOwnedStores = async () => stores;
storeApi.getStoreBySlug = async slug => stores.find(store => store.slug === slug);
catalogApi.getProducts = async (params = {}) => params.storeSlug === 'mone' ? [] : products;
catalogApi.getCategories = async () => [];
catalogApi.getNiches = async () => [];
catalogApi.getPriceBounds = async () => ({ minPrice: 850000, maxPrice: 1200000, count: 2 });
// A4: the paged list read — the ONLY product-list read fetchProducts uses.
// (The old unpaged getProducts stub above is dead since PERF-01, and the
// window.fetch stub returning '{}' made page 1 throw on page.items.map,
// so the fixture never loaded products through the real path.) Filters the
// fixture products by the requested price range and answers an inverted
// pair with the same 400 the backend half returns, so A8 can drive the
// real 400 path end to end.
catalogApi.getProductsPage = async (params: any = {}) => {
  if (params.minPrice != null && params.maxPrice != null && params.minPrice > params.maxPrice) {
    throw new ApiError(400, '{"error":"invalid catalog page request"}', { error: 'invalid catalog page request' });
  }
  const items = products.filter((p: any) =>
    (params.minPrice == null || p.price >= params.minPrice) &&
    (params.maxPrice == null || p.price <= params.maxPrice));
  return { items, nextCursor: null, hasMore: false, total: items.length };
};
const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => String(input).includes('/api/')
  ? new Response('{}', { headers: { 'Content-Type': 'application/json' } })
  : nativeFetch(input, init);

function Probe() {
  const store = useStore();
  const [show, setShow] = useState(false);
  h.state = () => ({ slug: store.currentStore.slug, view: store.activeView, products: store.products.length, loading: store.catalogLoading, priceRangeError: store.catalogPriceRangeError });
  h.select = (slug: string) => store.setActiveView('seller_storefront', { storeSlug: slug });
  h.open = () => { h.paints = []; store.setActiveView('catalog'); setShow(true); };
  // A4: bypass the CatalogView effect guard and push a filter straight into
  // the store (simulates a stale bundle / external client for A8).
  h.forceFilter = (patch: { minPrice?: number | null; maxPrice?: number | null }) => store.setCatalogFilter(patch);
  h.navigateCatalog = () => store.openCatalogCategory('women-tee-shirts');
  useLayoutEffect(() => {
    if (show) h.paints.push({ rows: document.querySelectorAll('[data-testid="product-item"]').length,
      heading: document.body.textContent?.includes('کاتالوگ جامع کالاها') || false, view: store.activeView });
  });
  return show ? <CatalogView onBack={() => {}} /> : <div id="catalog-ready" />;
}

// A4: standalone PriceRangeSlider mount (bounds 198000–63800000, spec §8.2)
// so A1/A2/A4/A5/A6 drive real typing/blur/pointer/keyboard events against
// the real component, independent of the CatalogView mount.
function SliderProbe() {
  const [value, setValue] = useState<[number, number]>([198000, 63800000]);
  const changesRef = useRef<Array<[number, number]>>([]);
  h.slider = {
    value: () => value,
    changes: () => [...changesRef.current],
    set: (v: [number, number]) => setValue(v),
  };
  return (
    <div data-testid="standalone-slider">
      <PriceRangeSlider
        boundMin={198000}
        boundMax={63800000}
        value={value}
        onChange={(v) => { changesRef.current.push(v); setValue(v); }}
      />
    </div>
  );
}
createRoot(document.getElementById('root')!).render(
  <AuthProvider><ProductProvider><CartProvider><OrderProvider><FinanceProvider><WarehouseProvider><ToastProvider>
    <StoreProvider><Probe /></StoreProvider>
    <SliderProbe />
  </ToastProvider></WarehouseProvider></FinanceProvider></OrderProvider></CartProvider></ProductProvider></AuthProvider>,
);
