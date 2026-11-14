import React, { useLayoutEffect, useState } from 'react';
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
const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => String(input).includes('/api/')
  ? new Response('{}', { headers: { 'Content-Type': 'application/json' } })
  : nativeFetch(input, init);

function Probe() {
  const store = useStore();
  const [show, setShow] = useState(false);
  h.state = () => ({ slug: store.currentStore.slug, view: store.activeView, products: store.products.length, loading: store.catalogLoading });
  h.select = (slug: string) => store.setCurrentStore(stores.find(value => value.slug === slug));
  h.open = () => { h.paints = []; store.setActiveView('catalog'); setShow(true); };
  h.navigateCatalog = () => store.setActiveView('catalog');
  useLayoutEffect(() => {
    if (show) h.paints.push({ rows: document.querySelectorAll('[data-testid="product-item"]').length,
      heading: document.body.textContent?.includes('کاتالوگ جامع کالاها') || false, view: store.activeView });
  });
  return show ? <CatalogView onBack={() => {}} /> : <div id="catalog-ready" />;
}
createRoot(document.getElementById('root')!).render(
  <AuthProvider><ProductProvider><CartProvider><OrderProvider><FinanceProvider><WarehouseProvider><ToastProvider>
    <StoreProvider><Probe /></StoreProvider>
  </ToastProvider></WarehouseProvider></FinanceProvider></OrderProvider></CartProvider></ProductProvider></AuthProvider>,
);
