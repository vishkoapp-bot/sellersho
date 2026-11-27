import React from 'react';
import { createRoot } from 'react-dom/client';
import { AppContent } from '../../../src/App';
import { AuthProvider, useAuth } from '../../../src/context/AuthContext';
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
import type { Order, Store } from '../../../src/types';
import type { ActiveView } from '../../../src/context/storeContextTypes';
import '../../../src/index.css';
import { authApi } from '../../../src/services/authApi';
import { ApiError } from '../../../src/services/apiClient';
import { bannersApi } from '../../../src/services/bannersApi';
import { catalogApi, type ProductDto } from '../../../src/services/catalogApi';
import { storeApi, type SellerProductCandidate } from '../../../src/services/storeApi';

const params = new URLSearchParams(window.location.search);
history.replaceState(null, '', params.get('route') || '/');
const routeParams = new URLSearchParams(window.location.search);
const matrixRole = routeParams.get('access-fixture-role');
const matrixCaps = matrixRole === null ? undefined : (routeParams.get('access-fixture-caps') ?? '').split(',').filter(Boolean);
const hasWarehouse = routeParams.has('warehouse');
const hasAdmin = routeParams.has('admin');
const staleWarehouseRole = routeParams.has('stale-warehouse-role');
const adminAccess = routeParams.get('admin');
const adminRole = adminAccess === 'finance' ? 'finance_specialist' : adminAccess === 'super' ? 'super_admin' : 'admin';
const role = matrixRole ?? (hasAdmin ? adminRole : hasWarehouse || routeParams.has('warehouse-otp') || staleWarehouseRole ? 'warehouse_staff' : 'customer');
const hasSeller = routeParams.has('seller') || matrixCaps?.includes('seller');
const hasSupplier = routeParams.has('supplier') || matrixCaps?.includes('supplier');
const fixtureImages = [
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPQkE8GAAEdAKsz/POGAAAAAElFTkSuQmCC',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGOIdfkBAAKbAZrQLohoAAAAAElFTkSuQmCC',
];

const capabilities: string[] = matrixCaps ?? (staleWarehouseRole ? ['customer', 'seller', 'supplier'] : [role]);
if (matrixRole && !routeParams.has('access-fixture-otp')) localStorage.setItem('seler_sho_jwt_token', 'fixture-matrix-token');
if (hasAdmin || staleWarehouseRole) localStorage.setItem('seler_sho_jwt_token', 'fixture-existing-token');
if (hasWarehouse) localStorage.setItem('seler_sho_jwt_token', 'fixture-warehouse-token');
if (hasSeller) {
  localStorage.setItem('seler_sho_jwt_token', 'fixture-seller-token');
  if (!matrixCaps) capabilities.push('seller');
}
if (hasSupplier) {
  if (!routeParams.has('access-fixture-otp')) localStorage.setItem('seler_sho_jwt_token', 'fixture-supplier-token');
  if (!matrixCaps) capabilities.push('supplier');
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
if (routeParams.has('promo-badges')) {
  Object.assign(products[0], {
    campaignName: 'پیشنهاد شگفت‌انگیز پایان هفته برای کالاهای دیجیتال',
    originalPrice: 125000,
    discountPercent: 20,
    isOriginal: false,
  });
}
if (routeParams.has('pdp-flow')) {
  const stock = routeParams.has('sold-out') ? 0 : 4;
  Object.assign(products[0], {
    productType: 'variable',
    stock,
    image: fixtureImages[0],
    images: fixtureImages,
    variants: [
      { id: 'pdp-stone', name: 'سنگی', spnc: 'PDP-STONE', attributes: {}, minCartQty: 2, stock },
      { id: 'pdp-forest', name: 'سبز', spnc: 'PDP-FOREST', attributes: {}, minCartQty: 1, stock: Math.min(stock, 1) },
    ],
    variantPrices: {
      'pdp-stone': { price: 100000, originalPrice: 100000, discountPercent: 0 },
      'pdp-forest': { price: 150000, originalPrice: 180000, discountPercent: 17 },
    },
  });
}
if (routeParams.has('legacy-parent')) {
  const product = products[0];
  Object.assign(product, {
    productType: 'legacy',
    title: 'فیلتر روغن موتور',
    variants: [
      { id: 'legacy-parent', name: 'فیلتر روغن موتور', spnc: 'LEGACY-PARENT', isDefault: true, stock: 150, attributes: {} },
      { id: 'legacy-sport', name: 'اسپرت', spnc: 'LEGACY-SPORT', stock: 60, attributes: { type: 'اسپرت' } },
      { id: 'legacy-standard', name: 'استاندارد', spnc: 'LEGACY-STANDARD', stock: 90, attributes: { type: 'استاندارد' } },
    ],
    variantPrices: {
      'legacy-parent': { price: 100000, originalPrice: 100000, discountPercent: 0 },
      'legacy-sport': { price: 150000, originalPrice: 150000, discountPercent: 0 },
      'legacy-standard': { price: 120000, originalPrice: 120000, discountPercent: 0 },
    },
  });
}
const central = { id: 'fixture-central', slug: 'central', name: 'فروشگاه مرکزی', ownerType: 'central' as const };
const seller = {
  id: 'fixture-store', slug: 'batch-eight-store', name: 'فروشگاه نمونه', ownerType: 'dropshipper',
  activeProductIds: products.map(product => product.id), templateId: routeParams.has('fashion') ? 'tpl-fashion-1' : 'tpl-electronic-1',
  ...(routeParams.has('fashion') ? {
    templateConfig: {
      custom: {
        heroTitle: 'لباس‌هایی برای هر روز',
        heroSubtitle: 'پوشاک و اکسسوری برای استایل روزمره',
        heroImage: '/templates/fashion/hero.webp',
        lookbookTitle: 'بافت‌های سبک برای روزهای خنک',
        lookbookImage: '/templates/fashion/knitwear.webp',
      },
    },
  } : {}),
  signedContractVersion: routeParams.has('renew-contract') ? 1 : 2,
  activePlanId: routeParams.has('multi-store') ? 'vip' : undefined,
} as Store & { activeProductIds: string[] };
const secondStore = routeParams.has('multi-store')
  ? { ...seller, id: 'fixture-second-store', slug: 'second-store', name: 'فروشگاه دوم' }
  : null;
const drp33 = routeParams.get('drp33');
const drp3132 = routeParams.get('drp3132');
const variantProduct: ProductDto = {
  ...products[0],
  title: 'کالای تنوع‌دار DRP-33',
  productType: 'variable',
  nicheIds: ['niche-audio'],
  baseCostB: 100000,
  variants: [{
    id: 'drp33-variant', name: 'تنوع نمونه', variantName: 'تنوع نمونه',
    spnc: 'DRP33', attributes: {}, isDefault: true, stock: 10,
    supplierBaseCost: 100000, minCartQty: 1, maxCartQty: null,
  }],
};
const plainProduct: ProductDto = {
  ...products[1], title: 'کالای ساده DRP-33', nicheIds: ['niche-audio'], baseCostB: 100000,
};
if (drp33) {
  Object.assign(seller, {
    selectedNicheIds: ['niche-audio'],
    productPriceOverrides: { [variantProduct.id]: 15, [plainProduct.id]: 15 },
  });
}
let drp33Override = ['override', 'locked', 'failure', 'missing'].includes(drp33 ?? '') ? 15 : undefined;
let drp33Visible = false;
const user = { id: 'fixture-user', mobile: '09120000000', fullName: routeParams.has('legacy-account-name')
  ? sessionStorage.getItem('fixture-account-name') ?? 'کاربر جدید' : 'کاربر نمونه', role: role, capabilities };

let release: ((outcome: 'success' | 'error') => void) | undefined;
let holdInitialProfile = routeParams.has('holdInitialProfile');
let rejectInitialProfile = routeParams.has('rejectInitialProfile');
let resolveInitialProfile: (() => void) | undefined;
const fixture = {
  registrations: 0,
  verifications: 0,
  initialProfilePending: () => resolveInitialProfile !== undefined,
  finishInitialProfile: () => {
    const finish = resolveInitialProfile;
    resolveInitialProfile = undefined;
    finish?.();
  },
  rejectRegistration: false,
  pending: false,
  shipping: 0,
  staticTabScrolls: 0,
  effectiveMarginReads: 0,
  variantOverrideClears: 0,
  finish: (outcome: 'success' | 'error' = 'success') => release?.(outcome),
};
if (drp3132 !== null) {
  const nicheStorageKey = 'batch8:drp31:niches';
  seller.selectedNicheIds = JSON.parse(localStorage.getItem(nicheStorageKey) ?? '["niche-smartwatch"]');
  if (drp3132 === 'undo') seller.activeProductIds.push('drp32-headphone');

  const candidates: SellerProductCandidate[] = [
    { id: 'drp32-headphone', title: 'هدفون بی‌سیم تست', imageUrl: fixtureImages[0], isActive: false },
    { id: 'drp32-speaker', title: 'اسپیکر بلوتوثی تست', imageUrl: fixtureImages[0], isActive: true },
    { id: 'drp32-watch', title: 'ساعت مچی تست', imageUrl: fixtureImages[0], isActive: true },
  ];
  const candidateDtos: ProductDto[] = candidates.map(({ imageUrl, ...product }) => ({
    ...products[0], ...product, baseCostB: 50000, nicheIds: ['niche-audio'],
    category: 'digital', stock: 10, image: imageUrl,
  }));
  const drpFixture = Object.assign(fixture, {
    settingsPuts: [] as Array<{ slug: string; config: Record<string, unknown> }>,
    productsPuts: [] as Array<{ slug: string; productIds: string[] }>,
    candidateCalls: [] as Array<Parameters<typeof storeApi.getSellerProductCandidates>[0]>,
    settingsInFlight: 0,
    settingsMaxInFlight: 0,
    failNextSettingsPut: false,
    failNextProductsPut: false,
    staleCandidateIds: [] as string[],
    staleCandidateIdsOnProductsFailure: [] as string[],
    settingsDelayMs: 0,
    simulateExternalAdd: (id: string) => {
      if (!seller.activeProductIds.includes(id)) seller.activeProductIds = [...seller.activeProductIds, id];
    },
  });

  catalogApi.fetchAllProductPages = async () => ({
    items: [...products, ...candidateDtos].filter((product) => seller.activeProductIds.includes(product.id)),
    complete: true,
    includeInactiveApplied: true,
  });
  storeApi.saveStoreSettings = async (slug, config) => {
    drpFixture.settingsPuts.push({ slug, config });
    drpFixture.settingsInFlight += 1;
    drpFixture.settingsMaxInFlight = Math.max(drpFixture.settingsMaxInFlight, drpFixture.settingsInFlight);
    try {
      if (drpFixture.settingsDelayMs) await new Promise((resolve) => setTimeout(resolve, drpFixture.settingsDelayMs));
      if (drpFixture.failNextSettingsPut) {
        drpFixture.failNextSettingsPut = false;
        throw new ApiError(400, '{"error":"Invalid input"}', null);
      }
      seller.selectedNicheIds = config.selectedNicheIds as string[];
      localStorage.setItem(nicheStorageKey, JSON.stringify(seller.selectedNicheIds));
      return { ...seller };
    } finally {
      drpFixture.settingsInFlight -= 1;
    }
  };
  storeApi.saveStoreProducts = async (slug, productIds) => {
    drpFixture.productsPuts.push({ slug, productIds: [...productIds] });
    if (drpFixture.failNextProductsPut) {
      drpFixture.failNextProductsPut = false;
      drpFixture.staleCandidateIds.push(...drpFixture.staleCandidateIdsOnProductsFailure);
      drpFixture.staleCandidateIdsOnProductsFailure = [];
      throw new ApiError(400, '{"error":"Invalid input"}', null);
    }
    seller.activeProductIds = [...productIds];
    return { ...seller };
  };
  storeApi.getSellerProductCandidates = async (params) => {
    drpFixture.candidateCalls.push(params);
    const available = candidates.filter((product) => !seller.activeProductIds.includes(product.id));
    if (params.ids) {
      const eligible = available.filter(({ id }) => params.ids?.includes(id) && !drpFixture.staleCandidateIds.includes(id));
      return { success: true, products: eligible, nextCursor: null, hasMore: false };
    }
    const query = params.q;
    const matches = query ? available.filter(({ title }) => title.includes(query)) : available;
    if (drp3132 === 'paging' && params.cursor === 'drp32-cursor-1') {
      return { success: true, products: matches.slice(2), nextCursor: null, hasMore: false };
    }
    if (drp3132 === 'paging' && matches.length > 2) {
      return { success: true, products: matches.slice(0, 2), nextCursor: 'drp32-cursor-1', hasMore: true };
    }
    return { success: true, products: matches, nextCursor: null, hasMore: false };
  };
}
Object.assign(window, { batch8Fixture: fixture });
const nativeScrollIntoView = HTMLElement.prototype.scrollIntoView;
HTMLElement.prototype.scrollIntoView = function (options) {
  if (this.parentElement?.className.includes('mask-image')) fixture.staticTabScrolls += 1;
  nativeScrollIntoView.call(this, options);
};

storeApi.getAllStores = async () => [central] as Awaited<ReturnType<typeof storeApi.getAllStores>>;
storeApi.getOwnedStores = async () => hasSeller
  ? [drp3132 === null ? seller : { ...seller }, ...(secondStore ? [secondStore] : [])]
  : [];
storeApi.getStoreBySlug = async slug => [central, seller, secondStore].find(store => store?.slug === slug) as Awaited<ReturnType<typeof storeApi.getStoreBySlug>>;
if (routeParams.has('renew-contract')) {
  storeApi.saveStoreSettings = async (_slug, config) => {
    seller.signedContractVersion = Number(config.signedContractVersion);
    return seller;
  };
}
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
  if (holdInitialProfile) {
    holdInitialProfile = false;
    await new Promise<void>(resolve => { resolveInitialProfile = resolve; });
  } else {
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  if (rejectInitialProfile) {
    rejectInitialProfile = false;
    throw new ApiError(0, 'Fixture session unavailable');
  }
  return { ...user, userId: user.id } as Awaited<ReturnType<typeof authApi.getMe>>;
};
authApi.updateProfileName = async fullName => {
  sessionStorage.setItem('fixture-account-name', fullName);
  user.fullName = fullName;
  return { ...user, userId: user.id };
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
  if (position === 'hero' && routeParams.has('editorial')) return [{
    id: 'editorial-digital', title: 'دیجیتال برای هر روز', subtitle: 'موبایل، هدفون و لوازم جانبی',
    imageUrl: '/static/uploads/banners/selersho-editorial-digital-v1.webp',
    linkUrl: '/?category=digital', position, sortOrder: 0, isActive: true,
  }];
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
    if (routeParams.has('fashion')) {
      const items = options.cursor ? products.slice(8, 16) : products.slice(0, 8);
      return { items, nextCursor: options.cursor ? null : 'fashion-page-2', total: 16, hasMore: !options.cursor };
    }
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

if (drp33) {
  const scopedVariantProduct: ProductDto = drp33 === 'hidden'
    ? { ...variantProduct, productType: 'legacy', hasVariants: true, variants: [] }
    : variantProduct;
  catalogApi.fetchAllProductPages = async () => ({
    items: [scopedVariantProduct, plainProduct], complete: true, includeInactiveApplied: true,
  });
  catalogApi.getProductById = async (id, storeSlug) =>
    id === plainProduct.id ? plainProduct : storeSlug ? scopedVariantProduct : variantProduct;
}

const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => {
  const url = String(input);
  if (!url.includes('/api/')) return nativeFetch(input, init);
  if (drp33 === 'workspace' && url.includes('/seller/variant-effective-margins?') && url.includes('workspace=1')) {
    const variant = variantProduct.variants![0];
    const margin = drp33Override ?? 35;
    return Response.json({ workItems: [{
      productId: variantProduct.id, title: variantProduct.title, needsAttention: !drp33Visible,
      variants: [{
        id: variant.id, name: variant.name, spnc: variant.spnc, active: true, visible: drp33Visible, available: true,
        price: 100000 + margin * 1000, effectiveMargin: margin, margin: drp33Override ?? null,
        minimumMargin: 5, maximumMargin: 35, marginLocked: false,
      }],
    }] });
  }
  if (drp33 === 'workspace' && url.includes('/seller/stores/') && url.endsWith('/visibility')) {
    drp33Visible = JSON.parse(String(init?.body)).isVisible;
    return Response.json({ isVisible: drp33Visible });
  }
  if (drp33 && url.includes('/seller/variant-effective-margins?')) {
    fixture.effectiveMarginReads += 1;
    if (drp33 === 'failure') return Response.json({ error: 'Fixture pricing unavailable' }, { status: 503 });
    const ceilingPct = drp33 === 'ceiling-high' ? 40 : drp33 === 'ceiling' ? 25 : 35;
    const locked = drp33 === 'locked';
    const marginPct = locked ? 18 : drp33Override ?? ceilingPct;
    return Response.json({
      items: drp33 === 'missing' || drp33 === 'hidden' ? [] : [{
        productId: variantProduct.id, variantId: variantProduct.variants![0].id,
        ceilingPct, locked, marginPct, tier: locked || drp33Override != null ? 'variant' : 'max-default',
        referenceCost: 100000, finalPrice: 100000 + marginPct * 1000,
      }],
    });
  }
  if (drp33 && url.includes('/seller/variant-overrides')) {
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body));
      drp33Override = url.includes('workspace=1') ? body.margin ?? undefined : body.value;
    } else if (init?.method === 'DELETE') {
      drp33Override = undefined;
      fixture.variantOverrideClears += 1;
    }
    return Response.json({ overrides: drp33Override == null ? [] : [{
      storeId: seller.id, productId: variantProduct.id, variantId: variantProduct.variants![0].id,
      value: drp33Override, mode: 'percent',
    }] });
  }
  if (drp33 && url.endsWith('/seller/pricing-settings')) {
    return Response.json({ platformPct: 0, minProfitMargin: 5, maxProfitMargin: 35 });
  }
  if (url.includes('/seller/variant-effective-margins?')) return Response.json({ items: [] });
  if (url.includes('/seller/variant-overrides')) return Response.json({ overrides: [] });
  const body = url.endsWith('/homepage-sections')
    ? { sections: [
        { id: 'grid', sectionType: 'product_grid', config: {}, sortOrder: 0, isActive: true },
        { id: 'secondary', sectionType: 'secondary_slider', config: {}, sortOrder: 1, isActive: true },
        { id: 'trust', sectionType: 'trust_features', config: {}, sortOrder: 2, isActive: true },
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

type FixtureOrderItems = 'flat' | 'missing' | 'both';

function fixtureOrder(shippingCost: number, items?: FixtureOrderItems): Order {
  const flatItem = { productId: 'product-bracelet', productTitle: 'دستبند طلا ۱۸ عیار', quantity: 1, variantName: 'ساده' };
  const deletedItem = { productId: 'deleted-product', quantity: 1 };
  const rawItems = items === 'both' ? [flatItem, deletedItem]
    : items === 'flat' ? [flatItem] : items === 'missing' ? [deletedItem] : [];
  return {
    id: 'fixture-order', orderNumber: 'FIXTURE', items: rawItems as unknown as Order['items'], totalAmount: 100000,
    discountAmount: 0, payableAmount: 100000 + shippingCost, paymentMethod: 'cash', status: 'pending',
    trackingCode: '', createdAt: '2026-01-01', shippingCost, storeId: central.id, storeName: central.name,
    dropshipperProfit: 0, platformProfit: 0,
    customerInfo: { fullName: user.fullName, phone: user.mobile, province: 'تهران', city: 'تهران', address: 'نشانی نمونه', postalCode: '1234567890' },
  };
}

if (routeParams.has('tracking-orders')) {
  const previousFetch = window.fetch.bind(window);
  window.fetch = async (input, options) => {
    const path = new URL(String(input), window.location.href).pathname;
    if (path === '/api/v1/orders') {
      const base = { ...fixtureOrder(0), customerFullName: 'خریدار آزمایشی', customerProvince: 'تهران', customerCity: 'تهران' };
      return Response.json([
        { ...base, id: 'seller-tracking', storeId: seller.id, storeName: seller.name, trackingCode: 'OTHER-900', status: 'shipped' },
        { ...base, id: 'central-tracking', trackingCode: 'DEMO-125', status: 'pending' },
      ]);
    }
    if (path === '/api/v1/orders/central-tracking/shipping') return Response.json({ carrier: 'شرکت حمل نمونه' });
    return previousFetch(input, options);
  };
}

if (routeParams.has('fixture-orders')) {
  const fetchCatalogFixture = window.fetch.bind(window);
  window.fetch = async (input, options) => {
    const url = new URL(String(input), window.location.href);
    if (url.pathname === `/api/v1/stores/${seller.slug}/orders`) {
      return Response.json({ orders: [{ ...fixtureOrder(0, 'both'), storeId: seller.id, storeName: seller.name }], hasMore: false });
    }
    return fetchCatalogFixture(input, options);
  };
}

if (hasWarehouse) {
  const originalFetch = window.fetch.bind(window);
  let received = false;
  const inboundId = 'fixture-inbound-carton';
  const lineId = 'fixture-line-from-carton';
  const warehouseReceipts: unknown[] = [];
  let qualityPassed = false;
  let reserved = false;
  let placed = false;
  Object.assign(fixture, { warehouseReceipts });
  const queue = () => [
    { id: 'fixture-other-line', receiptLineId: 'fixture-other-line', variantId: 'other-sku',
      productId: 'other-product', productTitle: 'کالای محمولهٔ دیگر', inboundId: 'other-inbound' },
    ...(received && !qualityPassed ? [{ id: lineId, receiptLineId: lineId, variantId: 'carton-sku',
      productId: 'carton-product', productTitle: 'کالای همان کارتن', inboundId }] : []),
  ].map(item => ({ ...item, supplierName: 'تأمین‌کننده نمونه', expectedQuantity: 1, receivedQuantity: 1,
    passedQuantity: 0, failedQuantity: 0, shortageQuantity: 0, remainingQuantity: 1, createdAt: '2026-11-20T00:00:00Z' }));
  const putaway = () => [
    { id: 'other-task', productId: 'other-product', title: 'کالای محمولهٔ دیگر', inboundId: 'other-inbound', status: 'pending', binLocation: '' },
    ...(qualityPassed && !placed ? [{ id: 'carton-task', productId: 'carton-product', title: 'کالای همان کارتن',
      inboundId, status: reserved ? 'assigned' : 'pending', binLocation: reserved ? 'BIN-57' : '' }] : []),
  ].map(task => ({ ...task, quantity: 1, measurementStatus: 'verified', lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 500 }));
  window.fetch = async (input, options) => {
    const { pathname } = new URL(String(input), window.location.href);
    if (pathname === '/api/v1/warehouse/overview') {
      return Response.json({ warehouseName: 'انبار شهر نمونه', capacityVolumeCm3: 100000, occupiedVolumeCm3: 15000,
        reservedVolumeCm3: 10000, capacityPercent: 25, inboundPending: 7, verifiedMovements: 11 });
    }
    if (pathname === '/api/v1/warehouse/shipments/CARTON-57') {
      return Response.json({ shipment: { shipment_code: 'CARTON-57', supplier_name: 'تأمین‌کننده نمونه',
        status: received ? 'received' : 'sent', items: [{ variant_id: 'carton-sku', variant_name: 'بسته نمونه',
          product_title: 'کالای همان کارتن', spnc: 'SPNC-57', expected_qty: 1, received_qty: received ? 1 : 0 }] } });
    }
    if (pathname === '/api/v1/warehouse/shipments/CARTON-57/receive') {
      warehouseReceipts.push(JSON.parse(String(options?.body)));
      received = true;
      return Response.json({ success: true, status: 'received', shipment_code: 'CARTON-57',
        items: [{ line_id: lineId, quantity: 1, unit_codes: [], mismatched: [] }] });
    }
    if (pathname === '/api/v1/warehouse/qc/queue') return Response.json({ success: true, queue: queue() });
    if (pathname === '/api/v1/warehouse/qc/carton-product/complete') {
      const payload = JSON.parse(String(options?.body));
      if (payload.receiptLineId !== lineId || payload.inboundId !== inboundId || payload.passedQuantity !== 1) {
        return Response.json({ error: 'wrong receipt line' }, { status: 400 });
      }
      qualityPassed = true;
      return Response.json({ success: true, status: 'completed', unit_codes: [] });
    }
    if (pathname === '/api/v1/warehouse/putaway/queue') return Response.json({ success: true, queue: putaway() });
    if (pathname === '/api/v1/warehouse/putaway/tasks/carton-task/assign') {
      reserved = JSON.parse(String(options?.body)).binLocation === 'BIN-57';
      return Response.json({ success: reserved }, { status: reserved ? 200 : 400 });
    }
    if (pathname === '/api/v1/warehouse/putaway/carton-task/confirm') {
      placed = reserved;
      return Response.json({ success: placed }, { status: placed ? 200 : 400 });
    }
    if (pathname === '/api/v1/warehouse/bins') return Response.json([
      { binCode: 'BIN-57', warehouseName: 'انبار شهر نمونه', hallName: 'سالن نمونه', aisle: '۱', shelf: '۱',
        quantity: placed ? 1 : 0, capacityStatus: 'verified', capacityVolumeCm3: 25000, remainingVolumeCm3: 20000,
        ...(placed ? { productId: 'carton-product', productTitle: 'کالای همان کارتن' } : {}) },
      { binCode: 'BIN-OTHER', warehouseName: 'انبار شهر نمونه', quantity: 0, capacityStatus: 'verified', remainingVolumeCm3: 20000 },
    ]);
    if (pathname.startsWith('/api/v1/warehouse/orders/ready-to-')) return Response.json({ success: true, orders: [] });
    if (pathname === '/api/v1/warehouse/notifications') return Response.json({ notifications: [], unreadCount: 0 });
    if (pathname === '/api/v1/warehouse/return-backs' || pathname === '/api/v1/warehouses/returns' ||
      pathname === '/api/v1/warehouse/returns' ||
      pathname === '/api/v1/warehouse/inbounds') return Response.json([]);
    return originalFetch(input, options);
  };
}

if (hasAdmin || matrixCaps) {
  const originalFetch = window.fetch.bind(window);
  const adminReads: string[] = [];
  Object.assign(fixture, { adminReads });
  window.fetch = async (input, options) => {
    const { pathname, searchParams: query } = new URL(String(input), window.location.href);
    if (matrixCaps && ['/api/v1/supplier/products', '/api/v1/supplier/products/proposals'].includes(pathname)) return Response.json([]);
    if (!pathname.startsWith('/api/v1/admin/')) return originalFetch(input, options);
    adminReads.push(pathname);
    if (pathname === '/api/v1/admin/notifications') return Response.json({ notifications: [], unreadCount: 0 });
    if (pathname === '/api/v1/admin/overview/leaders') return Response.json({ data: { suppliers: [], stores: [] } });
    if (pathname === '/api/v1/admin/jobs') return Response.json({ jobs: [] });
    if (pathname === '/api/v1/admin/overview/trends' || pathname === '/api/v1/admin/overview/alerts') return Response.json({ data: [] });
    if (pathname === '/api/v1/admin/orders') {
      if (!routeParams.has('fixture-admin-orders')) return Response.json({ items: [], hasMore: false });
      if (new Headers(options?.headers).get('Authorization') !== 'Bearer fixture-existing-token') {
        return Response.json({ error: 'Unauthorized' }, { status: 401 });
      }
      const order = {
        id: '123', orderNumber: 'ORD-123', storeName: 'فروشگاه آزمایشی', province: 'تهران', city: 'تهران',
        status: 'packed', fulfillmentMethod: 'mixed', createdAt: '2026-11-10T12:00:00Z', trackingCode: 'TRK-123',
        trackingNumber: '59999999', supplierShipments: [{ id: 'leg-a', status: 'shipped', carrier: 'پست', trackingNumber: '712345' }],
      };
      if (query.get('search')) {
        const matches = query.get('search') === '۷۱۲۳۴۵' && query.get('status') === 'preparing';
        return Response.json({ items: matches ? [{ ...order, id: '712', orderNumber: 'ORD-712' }] : [] });
      }
      return Response.json(query.get('cursor') === 'next-page'
        ? { items: [{ ...order, id: '456', orderNumber: 'ORD-456' }] }
        : { items: [order], nextCursor: 'next-page' });
    }
    return Response.json({});
  };
}

function FixtureControls() {
  const store = useStore();
  const { currentUser, isSessionReady } = useAuth();
  const toast = useToast();
  const [order, setOrder] = React.useState<Order | null>(null);
  const [showDetails, setShowDetails] = React.useState(false);
  Object.assign(fixture, {
    toast: toast.showToast,
    openWorkspace: (view: ActiveView) => store.setActiveView(view),
    state: () => ({ view: store.activeView, catalogTotal: store.catalogTotal, productCount: store.products.length,
      userId: currentUser?.id, isSessionReady, storeSlug: store.currentStore.slug,
      stores: store.stores.map(entry => entry.id) }),
    openCheckout: (shipping: number) => {
      fixture.shipping = shipping;
      store.startDirectBuy({ product: store.products[0], qty: 1, addonIds: [], addonSnapshot: [], store: central });
      store.setDirectBuyAddress({ id: 'fixture-address', receiverFullName: 'کاربر نمونه', receiverPhone: user.mobile,
        postalCode: '1234567890', provinceName: 'تهران', cityName: 'تهران', addressDetail: 'نشانی نمونه' });
      store.setIsCartOpen(true);
    },
    showOrder: (shippingCost: number, detail: boolean, items?: FixtureOrderItems) => {
      setShowDetails(detail);
      setOrder(fixtureOrder(shippingCost, items));
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
