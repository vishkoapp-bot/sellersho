import React from 'react';
import { createRoot } from 'react-dom/client';
import { AppContent } from '../../../src/App';
import { AuthProvider } from '../../../src/context/AuthContext';
import { ProductProvider } from '../../../src/context/ProductContext';
import { CartProvider } from '../../../src/context/CartContext';
import { SellerOnboardingProvider } from '../../../src/context/SellerOnboardingContext';
import { OrderProvider } from '../../../src/context/OrderContext';
import { FinanceProvider } from '../../../src/context/FinanceContext';
import { WarehouseProvider } from '../../../src/context/WarehouseContext';
import { StoreProvider, useStore } from '../../../src/context/StoreContext';
import { ToastProvider, useToast } from '../../../src/context/ToastContext';
import { CartOrderSuccess } from '../../../src/components/CartOrderSuccess';
import { OrderDetailModal } from '../../../src/components/common/OrderDetailModal';
import { shippingApi } from '../../../src/services/shippingApi';
import type { Order } from '../../../src/types';
import '../../../src/index.css';
import { authApi } from '../../../src/services/authApi';
import { ApiError } from '../../../src/services/apiClient';
import { bannersApi } from '../../../src/services/bannersApi';
import { catalogApi, type ProductDto } from '../../../src/services/catalogApi';
import { storeApi } from '../../../src/services/storeApi';

const params = new URLSearchParams(window.location.search);
history.replaceState(null, '', params.get('route') || '/');
const routeParams = new URLSearchParams(window.location.search);
const drp = routeParams.has('drp');
const role = routeParams.has('admin') ? 'admin' : drp ? 'seller' : 'customer';
const hasSeller = routeParams.has('seller');
const hasSupplier = routeParams.has('supplier');
const fixtureImages = [
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPQkE8GAAEdAKsz/POGAAAAAElFTkSuQmCC',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGOIdfkBAAKbAZrQLohoAAAAAElFTkSuQmCC',
];

const capabilities: string[] = [role];
if (hasSeller) {
  localStorage.setItem('seler_sho_jwt_token', 'fixture-seller-token');
  if (!capabilities.includes('seller')) capabilities.push('seller');
}
if (hasSupplier) {
  localStorage.setItem('seler_sho_jwt_token', 'fixture-supplier-token');
  capabilities.push('supplier');
  localStorage.setItem('sellersho:supplier-propose-draft:v1', JSON.stringify({
    version: 1,
    step: 4,
    images: fixtureImages,
  }));
}

const products = Array.from({ length: 24 }, (_, index) => ({
  id: `batch8-product-${index}`,
  title: index < 2 ? `کالای دیجیتال نمونه ${index}` : `کالای عمومی نمونه ${index}`,
  category: index < 2 ? 'digital' : index === 2 ? 'mobile' : 'home',
  description: 'توضیحات کالای نمونه',
  price: 100000 + index * 2000,
  originalPrice: 100000 + index * 2000,
  stock: 10,
  isOriginal: index < 3,
  isActive: true,
  image: '/templates/fashion/knitwear.webp',
} as ProductDto));
const central = { id: 'fixture-central', slug: 'central', name: 'فروشگاه مرکزی', ownerType: 'central' as const };
const seller = { id: 'fixture-store', slug: 'batch-eight-store', name: 'فروشگاه نمونه', ownerType: 'dropshipper' as const, activeProductIds: products.map(product => product.id), templateId: 'tpl-electronic-1' };
const sellerB = { ...seller, id: 'fixture-store-b', slug: 'second-store', name: 'فروشگاه دوم', tagline: '', signedContractVersion: 3 };
const unowned = { ...seller, id: 'fixture-unowned', slug: 'unowned-store', name: 'فروشگاه دیگر' };
const drpSeller = {
  ...seller, slug: 'mone', name: 'فروشگاه مونه', tagline: 'شعار نمونه', activePlanId: routeParams.has('standard') ? 'standard' : 'trial',
  signedContractVersion: routeParams.has('unknownSigned') ? undefined : routeParams.has('signedOne') ? 1 : 3,
};
const user = { id: 'fixture-user', mobile: '09120000000', fullName: 'کاربر نمونه', role: role, capabilities };

let release: ((outcome: 'success' | 'error') => void) | undefined;
const fixture = {
  registrations: 0,
  verifications: 0,
  rejectRegistration: false,
  pending: false,
  shipping: 0,
  staticTabScrolls: 0,
  finish: (outcome: 'success' | 'error' = 'success') => release?.(outcome),
};
Object.assign(window, { batch8Fixture: fixture });
const privateCalls: string[] = [];
const pendingReads: (() => void)[] = [];
let ownedReads = 0;
let releaseOwned: (() => void) | undefined;
let releaseContract: (() => void) | undefined;
let latestModalVisible = false;
if (drp) {
  new MutationObserver(() => {
    if (document.body.textContent?.includes('قرارداد همکاری عوض شده است')) latestModalVisible = true;
  }).observe(document.body, { childList: true, subtree: true });
  Object.assign(fixture, {
    privateCalls: () => privateCalls.slice(),
    ownedReads: () => ownedReads,
    ownerReadyToRelease: () => !!releaseOwned,
    releaseOwned: () => releaseOwned?.(),
    contractReadyToRelease: () => !!releaseContract,
    releaseContract: () => releaseContract?.(),
    hadContractModal: () => latestModalVisible,
    pendingReads: () => pendingReads.length,
    releaseOldReads: () => pendingReads.splice(0).forEach(release => release()),
  });
}
const nativeScrollIntoView = HTMLElement.prototype.scrollIntoView;
HTMLElement.prototype.scrollIntoView = function (options) {
  if (this.parentElement?.className.includes('mask-image')) fixture.staticTabScrolls += 1;
  nativeScrollIntoView.call(this, options);
};

storeApi.getAllStores = async () => (drp ? [routeParams.has('samePublic') ? { ...drpSeller, signedContractVersion: undefined } : unowned, central] : [central]) as Awaited<ReturnType<typeof storeApi.getAllStores>>;
storeApi.getOwnedStores = async () => {
  if (drp) ownedReads += 1;
  if (drp && ownedReads === 2 && routeParams.has('failOwner')) throw new ApiError(503, 'بارگذاری فروشگاه ناموفق بود.', null);
  if (drp && ownedReads === 2 && routeParams.has('delayOwner')) {
    await new Promise<void>(resolve => { releaseOwned = resolve; });
  }
  return hasSeller ? (drp ? [drpSeller, sellerB] : [seller]) as Awaited<ReturnType<typeof storeApi.getOwnedStores>> : [];
};
storeApi.getStoreBySlug = async slug => {
  const publicStores = drp
    ? [central, unowned, { ...drpSeller, signedContractVersion: undefined }, { ...sellerB, signedContractVersion: undefined }]
    : [central, seller];
  return publicStores.find(store => store.slug === slug) as Awaited<ReturnType<typeof storeApi.getStoreBySlug>>;
};
storeApi.registerStore = async () => {
  fixture.registrations += 1;
  if (fixture.rejectRegistration) throw new ApiError(401, '{"error":"Unauthorized"}', null);
  return seller as Awaited<ReturnType<typeof storeApi.registerStore>>;
};
authApi.sendOTP = async () => ({ message: 'Fixture OTP' }) as Awaited<ReturnType<typeof authApi.sendOTP>>;
authApi.verifyOTP = async () => {
  fixture.verifications += 1;
  return { token: 'fixture-token', user } as Awaited<ReturnType<typeof authApi.verifyOTP>>;
};
authApi.getMe = async () => {
  await new Promise(resolve => setTimeout(resolve, 100));
  return { ...user, userId: user.id } as Awaited<ReturnType<typeof authApi.getMe>>;
};
if (hasSupplier) {
  authApi.getSupplierProfile = async () => ({
    id: 'fixture-supplier',
    company_name: 'شرکت نمونه',
    warehouse_city: 'تهران',
    warehouse_city_confirmed: true,
    permissions: ['supplier.manage'],
    signedContractVersion: 1,
  });
}
bannersApi.getBanners = async position => {
  if (!['hero', 'secondary', 'promo', 'tile'].includes(position)) return [];
  return [{
    id: `fixture-banner-${position}`,
    title: position === 'hero'
      ? 'تازه‌ترین کالاهای دیجیتال و لوازم هوشمند برای خانه و محل کار را در فروشگاه نمونه پیدا کنید'
      : 'پیشنهادهای ویژه فروشگاه نمونه برای خرید لوازم خانه و وسایل دیجیتال مورد نیاز هر روز شما',
    subtitle: 'مجموعه‌ای از محصولات منتخب را مرور کنید، ویژگی‌ها و قیمت‌ها را کنار هم ببینید و کالای مناسب خانه یا محل کارتان را پیدا کنید.',
    imageUrl: fixtureImages[0],
    linkUrl: '#products-section',
    position,
    sortOrder: 0,
    isActive: true,
  }];
};
catalogApi.getCategories = async () => [
  { id: 'digital', title: 'کالای دیجیتال', slug: 'digital', iconName: 'Smartphone' },
  { id: 'home', title: 'خانه', slug: 'home', iconName: 'Home' },
  { id: 'mobile', title: 'موبایل', slug: 'mobile', parentId: 'digital' },
];
catalogApi.getNiches = async () => [];
catalogApi.getPriceBounds = async () => ({ minPrice: 100000, maxPrice: 600000, count: 41 });
catalogApi.getProductById = async () => products[0];
catalogApi.getProductsPage = async (filter = {}, options = {}) => {
  if (filter.storeSlug) {
    const items = products.slice(0, 8);
    return { items, nextCursor: null, total: items.length, hasMore: false };
  }
  if (filter.suggest) return { items: products.slice(0, 2), nextCursor: null, total: 2, hasMore: false };
  if (filter.category === 'mobile') {
    return { items: products.filter((p) => p.category === 'mobile'), nextCursor: null, total: 1, hasMore: false };
  }
  if (filter.category === 'digital' || filter.search) {
    fixture.pending = true;
    await new Promise<void>((resolve, reject) => {
      release = (outcome) => {
        fixture.pending = false;
        if (outcome === 'error') reject(new ApiError(503, 'Fixture catalog unavailable', null));
        else if (filter.search === 'range-error') reject(new ApiError(400, 'Fixture inverted price range', null));
        else resolve();
      };
      options.signal?.addEventListener('abort', () => {
        fixture.pending = false;
        reject(new DOMException('Aborted', 'AbortError'));
      }, { once: true });
    });
    const items = filter.search === 'missing' ? [] : products.slice(0, 2);
    return { items, nextCursor: null, total: items.length, hasMore: false };
  }
  const total = Number(routeParams.get('total') || 41);
  return { items: products, nextCursor: total > 24 ? 'page-two' : null, total, hasMore: total > 24 };
};

const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => {
  const url = String(input);
  if (!url.includes('/api/')) return nativeFetch(input, init);
  if (drp && url.endsWith('/platform/contracts')) {
    if (routeParams.has('delayContract')) await new Promise<void>(resolve => { releaseContract = resolve; });
    return Response.json(routeParams.has('unknownActive') ? {} : { sellerContractVersion: 3, sellerContractText: 'متن واقعی قرارداد ۳' });
  }
  const storeRequest = drp && url.match(/\/api\/v1\/stores\/([^/]+)\/(orders|payouts|balance|wallet|early-withdrawals|returns|services)(?:\?|$)/);
  if (storeRequest) {
    const [, slug, endpoint] = storeRequest;
    privateCalls.push(slug + ':' + endpoint + (init?.method === 'POST' ? ':' + String(init.body) : ''));
    if (slug !== 'mone' && slug !== sellerB.slug) return Response.json({ error: 'forbidden unowned store' }, { status: 403 });
    if (routeParams.has('delayOldOrders') && slug === 'mone' && endpoint === 'orders') {
      await new Promise<void>(resolve => pendingReads.push(resolve));
    }
    if (routeParams.has('delayOldBalance') && slug === 'mone' && endpoint === 'balance') {
      await new Promise<void>(resolve => pendingReads.push(resolve));
      return Response.json({ error: 'موجودی فروشگاه مونه بارگذاری نشد' }, { status: 503 });
    }
    if (endpoint === 'orders') return Response.json({ orders: [{ id: slug, orderNumber: slug === 'mone' ? 'FIRST-ORDER' : 'SECOND-ORDER', status: 'pending', items: [] }] });
    if (endpoint === 'balance') return Response.json({ availableAmount: slug === 'mone' ? 890000 : 123000 });
    if (endpoint === 'returns') return Response.json([
      { id: 'canceled', return_code: 'CANCELLED-RETURN', status: 'cancelled', items: [
        { product_title: 'unchecked', qc_status: 'pending' }, { product_title: 'missing' },
        { product_title: 'checked', qc_status: 'ok' }, { product_title: 'damage', qc_status: 'defective' },
      ] },
      { id: 'pending', return_code: 'OPEN-RETURN', status: 'requested', items: [{ product_title: 'incoming', qc_status: 'pending' }] },
    ]);
    if (endpoint === 'services') return Response.json({ id: drpSeller.id, activePlanId: 'standard', planExpiresAt: '2027-02-24', amount: 890000, durationDays: 90 });
    return Response.json([]);
  }
  const body = url.endsWith('/homepage-sections')
    ? { sections: [
        { id: 'grid', sectionType: 'product_grid', config: {}, sortOrder: 0, isActive: true },
        { id: 'secondary', sectionType: 'secondary_slider', config: {}, sortOrder: 1, isActive: true },
      ] }
    : url.endsWith('/static-pages')
    ? { pages: ['about', 'terms', 'privacy', 'faq', 'license'].map((key, sortOrder) => ({
        key, sortOrder, isActive: true, title: key, content: 'متن نمونه',
      })) }
    : {};
  return Response.json(body);
};

shippingApi.quote = async () => ({
  success: true,
  quoteId: 'fixture-quote',
  totalShipping: fixture.shipping,
  legs: [{ legIndex: 0, originId: 'fixture-origin', originLabel: 'انبار نمونه', fare: fixture.shipping, weightGrams: 500, boxLabel: 'جعبه' }],
  allocations: [{ productId: products[0].id, quantity: 1, legIndex: 0 }],
});

function FixtureControls() {
  const store = useStore();
  const toast = useToast();
  const [order, setOrder] = React.useState<Order | null>(null);
  const [showDetails, setShowDetails] = React.useState(false);
  Object.assign(fixture, {
    toast: toast.showToast,
    state: () => ({ view: store.activeView, catalogTotal: store.catalogTotal, productCount: store.products.length,
      stores: store.stores.map(entry => entry.id) }),
    openCheckout: (shipping: number) => {
      fixture.shipping = shipping;
      store.startDirectBuy({ product: store.products[0], qty: 1, addonIds: [], addonSnapshot: [], store: central });
      store.setDirectBuyAddress({ id: 'fixture-address', receiverFullName: 'کاربر نمونه', receiverPhone: user.mobile,
        postalCode: '1234567890', provinceName: 'تهران', cityName: 'تهران', addressDetail: 'نشانی نمونه' });
      store.setIsCartOpen(true);
    },
    showOrder: (shippingCost: number, detail: boolean) => {
      setShowDetails(detail);
      setOrder({
        id: 'fixture-order', orderNumber: 'FIXTURE', items: [], totalAmount: 100000,
        discountAmount: 0, payableAmount: 100000 + shippingCost, paymentMethod: 'cash', status: 'pending',
        trackingCode: '', createdAt: '2026-01-01', shippingCost, storeId: central.id, storeName: central.name,
        dropshipperProfit: 0, platformProfit: 0,
        customerInfo: { fullName: user.fullName, phone: user.mobile, province: 'تهران', city: 'تهران', address: 'نشانی نمونه', postalCode: '1234567890' },
      });
    },
    showCheckoutError: () => toast.error('خطای آزمایشی ثبت سفارش'),
  });
  if (!order) return null;
  return <section id="fixture-order">
    {showDetails ? <OrderDetailModal order={order} isOpen onClose={() => setOrder(null)} />
      : <CartOrderSuccess order={order} onClose={() => setOrder(null)} />}
  </section>;
}

createRoot(document.getElementById('root')!).render(
  <AuthProvider><ProductProvider><CartProvider><SellerOnboardingProvider><OrderProvider><FinanceProvider><WarehouseProvider>
    <StoreProvider><ToastProvider><AppContent /><FixtureControls /></ToastProvider></StoreProvider>
  </WarehouseProvider></FinanceProvider></OrderProvider></SellerOnboardingProvider></CartProvider></ProductProvider></AuthProvider>,
);
