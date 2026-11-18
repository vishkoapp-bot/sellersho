import { Router, Request, Response } from 'express';
import { mockProducts, mockStores, mockCategories, mockSuppliers } from '../data/mockData';
import { Order, Store, PlatformSettings } from '../types/index';
import { resolvePricing, DEFAULT_PACKAGING_TARIFFS } from '../utils/pricingEngine';
import { maskCustomerForRole, ViewerRole } from '../utils/privacyMasker';

export const apiRouter = Router();

// Server-side Platform Settings (Single Source of Truth)
const serverPlatformSettings: PlatformSettings = {
  platformPct: 0,
  centralProfitPct: 10,
  minProfitMargin: 5,
  maxProfitMargin: 35,
  defaultMargin: 20,
  attributionModel: 'last_touch',
  cookieDays: 30,
  deliveryConfirmation: { holdPeriodDays: 20, followUpTriggerDays: 10 },
  referralProgram: { sellerCommissionPercent: 2, isEnabled: true, subjectToDeliveryLock: true },
  contracts: { sellerContractText: '', sellerContractVersion: 2, supplierContractText: '', supplierContractVersion: 2, lastUpdated: '' },
  warehouseTariffs: {
    inboundFeePerCarton: 15000,
    storageFeePerCubicDay: 8000,
    pickingPackingFee: 0,
    firstMonthFreeStorage: true,
    depotBufferMultiplier: 1.5,
    baseMinDepotQuota: 100,
  },
  packagingTariffs: DEFAULT_PACKAGING_TARIFFS,
  subscriptionPlans: [],
  subscriptionPlanLimits: { trial: 25, standard: 100, pro: 300, vip: 9999 },
  adRevenue: {
    centralClickRate: 5000,
    sellerClickShare: 3000,
    platformClickShareOnSeller: 2000,
    centralViewRateCPM: 25000,
    sellerViewShareCPM: 15000,
    platformViewShareOnSellerCPM: 10000,
  },
  integrations: {
    // SEC-01 (critic F-12): the Sheba/Jibit verification integration
    // stays DISABLED in the mock/dev server — never enabled with an
    // empty key. (The real integration lives in FinanceContext, also
    // disabled; apiKey is kept as a type-only optional field.)
    shebaVerificationApi: { apiUrl: '', apiKey: '', isEnabled: false },
  },
};

// In-memory persistent stores and orders
let dynamicStores: Store[] = [...mockStores];
let dynamicOrders: Order[] = [];

// Health check
apiRouter.get('/health', (req: Request, res: Response) => {
  res.json({ status: 'ok', timestamp: new Date().toISOString() });
});

// Priority 1.3: Unified Product Price Resolution across Server & Frontend
apiRouter.get('/products', (req: Request, res: Response) => {
  const { category, search, storeSlug } = req.query;

  const store = dynamicStores.find((s) => s.slug === storeSlug) || dynamicStores[0];

  let result = mockProducts.map((p) => {
    const pricing = resolvePricing(p, store, serverPlatformSettings);
    return {
      ...p,
      price: pricing.finalPrice,
      originalPrice: pricing.originalPrice,
      platformMargin: pricing.platformFee,
    };
  });

  if (category && category !== 'all') {
    result = result.filter((p) => p.category === category);
  }

  if (search && typeof search === 'string') {
    const q = search.toLowerCase();
    result = result.filter(
      (p) =>
        p.title.toLowerCase().includes(q) ||
        p.supplierName.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q)
    );
  }

  res.json(result);
});

// Single product with universal pricing
apiRouter.get('/products/:id', (req: Request, res: Response) => {
  const { storeSlug } = req.query;
  const product = mockProducts.find((p) => p.id === req.params.id);
  if (!product) {
    return res.status(404).json({ error: 'Product not found' });
  }

  const store = dynamicStores.find((s) => s.slug === storeSlug) || dynamicStores[0];
  const pricing = resolvePricing(product, store, serverPlatformSettings);

  res.json({
    ...product,
    price: pricing.finalPrice,
    originalPrice: pricing.originalPrice,
    platformMargin: pricing.platformFee,
  });
});

// Get categories
apiRouter.get('/categories', (req: Request, res: Response) => {
  res.json(mockCategories);
});

// Get suppliers
apiRouter.get('/suppliers', (req: Request, res: Response) => {
  res.json(mockSuppliers);
});

// Get stores
apiRouter.get('/stores', (req: Request, res: Response) => {
  res.json(dynamicStores);
});

// Get store by slug
apiRouter.get('/stores/:slug', (req: Request, res: Response) => {
  const store = dynamicStores.find((s) => s.slug === req.params.slug);
  if (!store) {
    return res.status(404).json({ error: 'Store not found' });
  }
  res.json(store);
});

// Register new dropshipper seller store
apiRouter.post('/sellers/register', (req: Request, res: Response) => {
  const { storeName, slug, ownerName, profitMargin, categories } = req.body;

  if (!storeName || !slug || !ownerName) {
    return res.status(400).json({ error: 'Missing required store fields' });
  }

  const existing = dynamicStores.find((s) => s.slug === slug.toLowerCase());
  if (existing) {
    return res.status(409).json({ error: 'Store slug already taken' });
  }

  const newStore: Store = {
    id: `store-${Date.now()}`,
    name: storeName,
    slug: slug.toLowerCase().trim(),
    ownerType: 'dropshipper',
    ownerName,
    tagline: `ویترین اختصاصی ${storeName} با ضمانت ارسال انبار مرکزی`,
    profitMarginPercent: Number(profitMargin) || 12,
    description: `فروشگاه مستقل ${storeName} تحت بستر یکپارچه دراپ‌شیپینگ.`,
    activeCategories: categories || ['digital', 'fashion'],
    totalSales: 0,
    monthlyRevenueEstimate: 0,
  };

  dynamicStores.push(newStore);
  res.status(201).json(newStore);
});

// Place order with Priority 1.2 (Privacy DTO for seller role) and Priority 1.3 (Unified Pricing)
apiRouter.post('/orders', (req: Request, res: Response) => {
  const { items, customerInfo, paymentMethod, storeId } = req.body;

  if (!items || !customerInfo || !storeId) {
    return res.status(400).json({ error: 'Missing order parameters' });
  }

  const store = dynamicStores.find((s) => s.id === storeId) || dynamicStores[0];
  const randomNum = Math.floor(100000 + Math.random() * 900000);
  const trackingCode = `TRK-${randomNum}`;
  const orderNumber = `ORD-${Date.now().toString().slice(-6)}`;

  let totalAmount = 0;
  let payableAmount = 0;
  let dropshipperProfit = 0;
  let platformProfit = 0;

  const resolvedItems = items.map((item: any) => {
    const qty = item.quantity || 1;
    const pricing = resolvePricing(item.product, store, serverPlatformSettings);
    const itemSellingPrice = pricing.finalPrice * qty;
    const itemPlatformFee = pricing.platformFee * qty;
    const itemWholesale = pricing.wholesaleCost * qty;

    totalAmount += pricing.originalPrice * qty;
    payableAmount += itemSellingPrice;

    if (store.ownerType === 'dropshipper') {
      dropshipperProfit += pricing.sellerMargin * qty;
      platformProfit += itemPlatformFee;
    } else {
      platformProfit += (itemSellingPrice - itemWholesale);
    }

    return {
      ...item,
      product: {
        ...item.product,
        price: pricing.finalPrice,
        originalPrice: pricing.originalPrice,
        platformMargin: pricing.platformFee,
      },
    };
  });

  const internalOrder: Order = {
    id: `ord-${Date.now()}`,
    orderNumber,
    items: resolvedItems,
    totalAmount,
    discountAmount: totalAmount - payableAmount,
    payableAmount,
    paymentMethod: paymentMethod || 'cash',
    customerInfo, // Preserved internally for warehouse packing & shipping
    status: 'processing',
    trackingCode,
    createdAt: new Date().toISOString(),
    storeId: store.id,
    storeName: store.name,
    dropshipperProfit,
    platformProfit,
  };

  dynamicOrders.push(internalOrder);

  // Priority 1.2: Enforce Privacy DTO when responder role is 'seller' or caller requested
  const callerRole = (req.headers['x-user-role'] || req.query.role || req.body.callerRole || 'customer') as ViewerRole;
  const responseDto = {
    ...internalOrder,
    customerInfo: maskCustomerForRole(internalOrder.customerInfo, callerRole),
  };

  res.status(201).json(responseDto);
});

// Track order with Role-based DTO masking
apiRouter.get('/orders/track/:trackingCode', (req: Request, res: Response) => {
  const paramCode = String(req.params.trackingCode).toLowerCase();
  const order = dynamicOrders.find(
    (o) => o.trackingCode.toLowerCase() === paramCode
  );
  if (!order) {
    return res.status(404).json({ error: 'Order not found' });
  }

  const callerRole = (req.headers['x-user-role'] || req.query.role || 'public') as ViewerRole;
  res.json({
    ...order,
    customerInfo: maskCustomerForRole(order.customerInfo, callerRole),
  });
});
