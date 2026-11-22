import React from 'react';
import { createRoot } from 'react-dom/client';
import '../../../src/index.css';
import { AuthProvider } from '../../../src/context/AuthContext';
import { ProductProvider } from '../../../src/context/ProductContext';
import { CartProvider } from '../../../src/context/CartContext';
import { OrderProvider } from '../../../src/context/OrderContext';
import { FinanceProvider } from '../../../src/context/FinanceContext';
import { WarehouseProvider } from '../../../src/context/WarehouseContext';
import { StoreProvider } from '../../../src/context/StoreContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { AdminDashboardView } from '../../../src/components/admin/AdminDashboardView';
import { storeApi } from '../../../src/services/storeApi';
import { catalogApi } from '../../../src/services/catalogApi';

const h: any = {};
(window as any).adminScrollHarness = h;
// Only API boundaries are synthetic. All contexts and the view are production code.
// The admin session itself comes from localStorage seeded by the test
// (drup_current_user with super_admin + drup_auth_token), exactly like a
// saved super_admin session — no OTP, no network login.
// Two fixture stores: StoreProvider blocks its children until a current
// store is selected, so the stubbed list must be non-empty (same stores
// as the catalog fixture); the stores-list tab therefore renders rows.
const stores: any[] = [
  { id: 'central-store', slug: 'central', name: 'Central', ownerType: 'central', ownerName: 'پلتفرم', activeProductIds: [] },
  { id: 'store-mone', slug: 'mone', name: 'mone', ownerType: 'dropshipper', ownerName: 'مالک مون', activeProductIds: [] },
];
(storeApi as any).getAllStores = async () => stores;
(storeApi as any).getOwnedStores = async () => stores;
(storeApi as any).getStoreBySlug = async (slug: string) => stores.find((s) => s.slug === slug) || null;
(catalogApi as any).getProducts = async () => [];
(catalogApi as any).getProductsPage = async () => ({ items: [], nextCursor: null, hasMore: false, total: 0 });
(catalogApi as any).getCategories = async () => [];
(catalogApi as any).getNiches = async () => [];
(catalogApi as any).getPriceBounds = async () => ({ minPrice: 0, maxPrice: 0, count: 0 });
const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => String(input).includes('/api/')
  ? new Response('{}', { headers: { 'Content-Type': 'application/json' } })
  : nativeFetch(input, init);

h.activeTab = () => {
  const badge = [...document.querySelectorAll('span')].find((s) => /^\/admin\//.test(s.textContent || ''));
  return badge ? (badge.textContent || '').replace('/admin/', '') : null;
};
h.scrollY = () => window.scrollY;
h.scrollToBottom = () => window.scrollTo(0, document.body.scrollHeight);
h.clickTab = (label: string) => {
  const btn = [...document.querySelectorAll('aside button')].find((b) => (b.textContent || '').trim() === label);
  if (!btn) throw new Error('nav button not found: ' + label);
  (btn as HTMLElement).click();
};
h.bodyText = () => document.body.textContent || '';
h.tabHeadingVisible = (label: string) => {
  const el = [...document.querySelectorAll('main h3')].find((e) => (e.textContent || '').trim() === label);
  if (!el) return false;
  const r = el.getBoundingClientRect();
  return r.top >= 0 && r.top < window.innerHeight;
};

createRoot(document.getElementById('root')!).render(
  <AuthProvider>
    <ProductProvider>
      <CartProvider>
        <OrderProvider>
          <FinanceProvider>
            <WarehouseProvider>
              <StoreProvider>
                <ToastProvider>
                  <AdminDashboardView onBackToCustomerSite={() => {}} />
                </ToastProvider>
              </StoreProvider>
            </WarehouseProvider>
          </FinanceProvider>
        </OrderProvider>
      </CartProvider>
    </ProductProvider>
  </AuthProvider>,
);
