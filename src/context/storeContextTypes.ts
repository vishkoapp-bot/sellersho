import {
  Product,
  ProductAddon,
  Store,
  CartItem,
  Order,
  PayoutRequest,
  StoreBuilderConfig,
  StoreSocialLinks,
  PlatformSettings,
  AdjustmentRecord,
  AuditLogEntry,
  DisputeRecord,
  SupplierSession,
  WarehouseSession,
  ProposedProduct,
  InboundShipment,
  QCDiscrepancyReport,
  AdCampaign,
  Niche,
  StoreNameChangeRequest,
  StoreSlugChangeRequest,
  CustomDomainRequest,
  PreMadeWidget,
  PackagingTariff,
  SubscriptionPlan,
  SupplierPromotion,
  PlatformContracts,
  DeliveryConfirmationSettings,
  PendingApprovalItem,
} from '../types';
import { DisputeDeliveryResponse } from '../services/orderApi';

// Worker A (buy-now): live direct-buy state types (index-MERGED.js
// StoreProvider). The payload mirrors live's startDirectBuy arg
// {product, qty, variantRef, addonIds, addonSnapshot, store}, adapted to
// the candidate line shape (variantId instead of a variant object).
export interface DirectBuyStore {
  id: string;
  name: string;
  slug?: string;
}
export interface DirectBuy {
  product: Product;
  qty: number;
  variantId?: string;
  variantLabel?: string;
  addonIds: string[];
  addonSnapshot: ProductAddon[];
  store: DirectBuyStore;
}
export interface DirectBuyAddress {
  id: string;
  label?: string;
  receiverFullName: string;
  receiverPhone?: string;
  provinceName: string;
  provinceCode?: unknown;
  cityName: string;
  cityCode?: unknown;
  postalCode?: string;
  addressDetail: string;
  isDefault?: boolean;
}
export type ActiveView = 
  | 'customer'
  | 'product_detail'
  | 'catalog'
  | 'categories_tree'
  | 'customer_profile'
  | 'customer_orders'
  | 'customer_wishlist'
  | 'static_page'
  | 'seller_dashboard'
  | 'seller_storefront'
  | 'supplier_dashboard'
  | 'warehouse_panel'
  | 'supplier_landing'
  | 'dropshipper_landing'
  | 'admin_panel'
  | 'not_found'; // Team 08 (D45/D46): NEW live view for unknown URLs

export type StaticPageKey = 'about' | 'terms' | 'privacy' | 'faq' | 'license';

export type AdminRole = 
  | 'admin'
  | 'super_admin'
  | 'finance_specialist' 
  | 'logistics_specialist' 
  | 'support_specialist';

export interface AdminSession {
  token: string;
  username: string;
  fullName: string;
  role: AdminRole;
  roleLabel: string;
  loginAt: string;
}

export type SellerAccountStatus = 'active' | 'onboarding' | 'suspended';

export interface SellerSession {
  token: string;
  mobile: string;
  fullName: string;
  status: SellerAccountStatus;
  storeId?: string;
  suspensionReason?: string;
  loginAt: string;
}

export interface StoreContextType {
  // Consumer components render only after the provider has selected a store.
  currentStore: Store;
  setCurrentStore: (store: Store) => void;
  stores: Store[];
  addStore: (store: Store) => void;
  storeStatuses: { [storeId: string]: 'active' | 'suspended' };
  toggleStoreStatus: (storeId: string) => 'active' | 'suspended';
  products: Product[];
  categories: import('../types').Category[];
  niches: Niche[];
  catalogLoading: boolean;
  catalogError: string | null;
  cart: CartItem[];
  addToCart: (product: Product, quantity?: number, color?: string, variantId?: string, addonIds?: string[]) => void;
  removeFromCart: (key: string) => void;
  updateQuantity: (key: string, quantity: number) => void;
  clearCart: () => void;
  cartTotal: number;
  cartDiscount: number;
  cartPayable: number;
  cartAddonsTotal: number;
  cartItemsCount: number;
  searchQuery: string;
  setSearchQuery: (query: string) => void;
  selectedCategory: string;
  setSelectedCategory: (catId: string) => void;
  selectedProduct: Product | null;
  setSelectedProduct: (prod: Product | null) => void;
  
  // Dedicated Product Detail & Catalog Navigation
  currentProductDetail: Product | null;
  openProductDetail: (product: Product) => void;
  openCatalogCategory: (categorySlug: string, query?: string) => void;
  catalogBrandRequest: { brand: string; nonce: number } | null;
  openCatalogBrand: (brand: string) => void;
  clearCatalogBrand: () => void;

  // Static Pages
  staticPageKey: StaticPageKey;
  openStaticPage: (key: StaticPageKey) => void;

  // Customer Wishlist (per store: every dropshipper store is a
  // separate site with its own wishlist; wishlistIds is the CURRENT
  // store's list, wishlists the full per-store map)
  wishlistIds: string[];
  wishlists: Record<string, string[]>;
  toggleWishlist: (productId: string, store?: { id: string; slug?: string }) => void;
  isWishlisted: (productId: string, store?: { id: string; slug?: string }) => boolean;

  // Customer Account & Profile
  customerUser: {
    fullName: string;
    mobile: string;
    nationalCode: string;
    isLoggedIn: boolean;
  };
  setCustomerUser: React.Dispatch<React.SetStateAction<any>>;
  logoutCustomer: () => void;

  // Admin Session Guard (Multi-role: Super Admin & Finance Specialist)
  adminSession: AdminSession | null;
  setAdminSession: React.Dispatch<React.SetStateAction<AdminSession | null>>;
  logoutAdmin: () => void;

  // Seller Session Guard (Mobile + OTP + Onboarding/Suspended Guard)
  sellerSession: SellerSession | null;
  setSellerSession: React.Dispatch<React.SetStateAction<SellerSession | null>>;
  logoutSeller: () => void;

  // Supplier Session Guard (Company Code / Login)
  supplierSession: SupplierSession | null;
  setSupplierSession: React.Dispatch<React.SetStateAction<SupplierSession | null>>;
  logoutSupplier: () => void;

  // Warehouse Session Guard (Staff ID / Warehouse Code)
  warehouseSession: WarehouseSession | null;
  setWarehouseSession: React.Dispatch<React.SetStateAction<WarehouseSession | null>>;
  logoutWarehouse: () => void;

  // Enterprise Platform Settings (Single Source of Truth)
  platformSettings: PlatformSettings;
  updatePlatformSettings: (settings: Partial<PlatformSettings>) => void;

  // Dynamic Catalog Approvals & Supplier Discrepancies
  proposedProducts: ProposedProduct[];
  submitProposedProduct: (offer: Omit<ProposedProduct, 'id' | 'status' | 'createdAt'>) => void;
  approveAndMergeProduct: (item: ProposedProduct | any) => void;
  rejectProposedProduct: (item: ProposedProduct | any, reason: string) => void;
  addProduct: (product: Product) => void;

  // Supplier Wholesale Cost ($C) Live Updates
  updateWholesaleCost: (productId: string, newCostC: number) => void;

  // Inbound Shipments & Warehouse Integration
  inboundShipments: InboundShipment[];
  createInboundShipment: (shipment: Omit<InboundShipment, 'id' | 'date' | 'status' | 'statusText'>) => InboundShipment;
  receiveInboundShipment: (shipmentId: string) => void;
  completePutaway: (shipmentId: string, binLocation: string, batchNumber?: string) => void;
  completeOrderPicking: (orderId: string) => void;
  completeOrderPacking: (orderId: string, weightGrams: number, boxSize: string) => void;
  dispatchOrdersToShipping: (orderIds: string[], carrierName: string) => { manifestId: string; trackingCodes: string[] };
  processWarehouseReturn: (disputeId: string, decision: 'restock' | 'damaged', notes?: string) => void;

  // Quality Control (QC) Discrepancy Flow
  qcDiscrepancies: QCDiscrepancyReport[];
  submitQCDiscrepancy: (report: Omit<QCDiscrepancyReport, 'id' | 'date' | 'status'>) => void;
  acceptQCDiscrepancy: (reportId: string) => void;
  disputeQCDiscrepancy: (reportId: string, objectionNote: string) => void;

  // Supplier Ad Bidding Network
  adCampaigns: AdCampaign[];
  createAdCampaign: (campaign: Omit<AdCampaign, 'id' | 'spentToday' | 'validClicks' | 'status'>) => void;
  toggleAdCampaign: (campaignId: string) => void;

  // Official Double-Entry Adjustments & Reversals
  adjustmentRecords: AdjustmentRecord[];
  createAdjustmentRecord: (record: Omit<AdjustmentRecord, 'id' | 'date'>) => void;

  // Arbitration, Disputes & 7-Day Returns
  disputes: DisputeRecord[];
  resolveDisputeAndAdjust: (
    disputeId: string,
    responsibleParty: string,
    actionType?: 'release_to_seller' | 'refund_carrier' | 'refund_warehouse' | 'refund_supplier' | 'refund_seller'
  ) => void;

  // Enterprise Audit Trail
  auditLogs: AuditLogEntry[];
  logAuditEvent: (action: string, category: AuditLogEntry['category'], details: string) => void;

  // Centralized Dropshipper Store State Mutators (Root State Injection)
  updateStoreActiveProducts: (productIds: string[]) => void;
  toggleCategoryActiveProducts: (categorySlug: string, enable: boolean) => void;
  updateStoreProductPriceOverride: (productId: string, profitD: number) => void;
  updateStoreCategoryPriceOverride: (categorySlug: string, marginPercent: number) => void;
  updateStoreBuilderConfig: (config: StoreBuilderConfig, publishImmediately?: boolean) => void;
  revertStoreBuilderConfig: () => void;
  updateStoreCustomDomain: (domain: string) => void;
  updateStorePlan: (planId: 'trial' | 'standard' | 'pro' | 'vip') => void;

  // Store Requests & Domain
  storeNameChangeRequests: StoreNameChangeRequest[];
  requestStoreNameChange: (storeId: string, requestedName: string) => void;
  approveStoreNameChange: (requestId: string) => void;
  rejectStoreNameChange: (requestId: string) => void;
  storeSlugChangeRequests: StoreSlugChangeRequest[];
  requestStoreSlugChange: (storeId: string, requestedSlug: string) => void;
  approveStoreSlugChange: (requestId: string) => void;
  rejectStoreSlugChange: (requestId: string) => void;
  customDomainRequests: CustomDomainRequest[];
  requestCustomDomain: (storeId: string, domain: string) => void;
  approveCustomDomain: (requestId: string) => void;
  rejectCustomDomain: (requestId: string) => void;

  // Unified Pending Approval Queue (Priority 2.2)
  unifiedPendingApprovals: PendingApprovalItem[];
  approveUnifiedItem: (item: PendingApprovalItem) => void;
  rejectUnifiedItem: (item: PendingApprovalItem, reason?: string) => void;

  // Niche Management & Multi-level Pricing
  updateStoreSelectedNiches: (nicheIds: string[]) => void;
  updateStoreProductPriceOverrideWithMode: (productId: string, profitD: number) => Promise<{ ok: boolean; status?: number }>;
  updateStoreNichePriceOverride: (nicheId: string, margin: number) => Promise<{ ok: boolean; status?: number }>;
  
  // Marketing & Available Widgets
  preMadeWidgets: PreMadeWidget[];
  createPreMadeWidget: (widget: Omit<PreMadeWidget, 'id' | 'createdAt'>) => void;
  updateSellerDealsWidgetConfig: (config: Store['sellerDealsWidgetConfig']) => void;
  updateCentralDealsFilter: (filter: 'my_niches' | 'manual' | 'all', productIds?: string[]) => void;

  // Sheba Name Verification
  verifyShebaOwnerName: (shaba: string, expectedName: string) => Promise<{ success: boolean; ownerName: string; bankName: string; error?: string }>;

  // Shared Payout Queue (connecting Seller & Supplier with FinancePayoutsTab)
  payoutRequests: PayoutRequest[];
  requestPayout: (storeId: string, amount: number, shaba: string, bankName: string) => boolean;
  approvePayout: (payoutId: string) => void;
  rejectPayout: (payoutId: string) => void;

  // Persistent Referral Attribution (Chapter 7)
  referredStoreSlug: string | null;
  setReferredStoreSlug: (slug: string | null) => void;

  // Order Delivery Confirmation & Dispute System (Section 4)
  confirmOrderDelivery: (orderId: string) => Promise<void>;
  disputeOrderDelivery: (orderId: string, reason?: string) => Promise<DisputeDeliveryResponse | null>;
  busyOrderId: string | null;
  adminResolveDeliveryDispute: (
    orderId: string,
    action: 'release_to_seller' | 'refund_to_customer',
    responsibleParty?: string
  ) => void;

  // Automated Technical Nameserver Domain Verification (Section 6)
  verifyAndConnectDomainAutomatically: (storeId: string, domain: string) => Promise<{ success: boolean; message: string }>;

  // Wallet Purchases & Plan Renewal (Section 7)
  purchaseServiceWithWallet: (storeId: string, serviceType: 'plan_renewal' | 'sms_500' | 'sms_2000' | 'catalog_upgrade', cost: number, details?: string) => boolean;

  // Social Links (Section 9)
  updateStoreSocialLinks: (socialLinks: StoreSocialLinks) => void;

  // Admin Store Detail & Margin Override (Section 13)
  updateStoreAdminMarginOverride: (storeId: string, margin: number, isLocked: boolean) => void;

  // Digital Contracts (Section 14)
  updateContracts: (contracts: Partial<PlatformContracts>) => void;

  // Subscription Plans Management (Section 20 & Item 4)
  updateSubscriptionPlans: (plans: SubscriptionPlan[]) => void;
  updateTrialCatalogQuota: (quota: number) => void;

  // Pre-made Widgets Edit (Section 22)
  updatePreMadeWidget: (id: string, updated: Partial<PreMadeWidget>) => void;

  // Product Authenticity (Section 23)
  toggleProductOriginality: (productId: string, isOriginal: boolean) => void;

  // Supplier Promotions (Section 12)
  createSupplierPromotion: (promo: Omit<SupplierPromotion, 'id'>) => void;
  deleteSupplierPromotion: (promoId: string) => void;

  // Delivery confirmation settings & Warehouse tariffs (Sections 4, 17, 19)
  updateDeliveryConfirmationSettings: (settings: DeliveryConfirmationSettings) => void;
  updateWarehouseTariffs: (tariffs: Partial<PlatformSettings['warehouseTariffs']>) => void;
  executeSupplierMonthlyPayouts: () => void;

  // 3-Tier Platform Fee (F) Overrides (Principle 4 & Req 6)

  // Weight & Distance Packaging Tariffs (Req 7)
  updatePackagingTariffs: (tariffs: PackagingTariff[]) => void;

  isCartOpen: boolean;
  setIsCartOpen: (open: boolean) => void;
  // Worker A (buy-now): live direct-buy state.
  directBuy: DirectBuy | null;
  startDirectBuy: (directBuy: DirectBuy) => void;
  clearDirectBuy: () => void;
  directBuyAddress: DirectBuyAddress | null;
  setDirectBuyAddress: (address: DirectBuyAddress | null) => void;
  isSellerModalOpen: boolean;
  setIsSellerModalOpen: (open: boolean) => void;
  isTrackingModalOpen: boolean;
  setIsTrackingModalOpen: (open: boolean) => void;
  trackingOrder: Order | null;
  setTrackingOrder: (order: Order | null) => void;
  orders: Order[];
  placeOrder: (customerInfo: Order['customerInfo'], paymentMethod: 'cash', discountCode?: string, shippingQuoteId?: string, direct?: { lines: CartItem[]; store?: DirectBuyStore }, address?: { addressId?: string; saveAddress?: boolean }) => Promise<Order>;
  activeView: ActiveView;
  setActiveView: (view: ActiveView, navigation?: { productId?: string; staticPageKey?: StaticPageKey; storeSlug?: string; sellerTab?: string; searchQuery?: string; categorySlug?: string }) => void;
  // Team 08 (D45/D46) pathname-router / storefront resolve states.
  // Router/storefront states ONLY; cart/order logic is untouched here.
  storefrontSlug: string | null;
  storefrontLoading: boolean;
  storefrontResolved: boolean;
  storefrontInvalid: boolean;
  storefrontSuspended: boolean;
  storefrontUnsubscribeSlug: string | null;
  storefrontProductDetail: Product | null;
  openStorefront: (slug?: string) => void;
  sellerDashboardTab: string;
  setSellerDashboardTab: (tab: string) => void;
  routeProductPending: boolean;
  routeProductInvalid: boolean;
  // Store scope of a product opened from inside a seller storefront
  // (null = unscoped product route, i.e. the central PDP).
  routeStoreSlug: string | null;
  // P0-2: routed-store resolution gate (see StoreContext).
  routeStoreResolved: boolean;
  routeStoreInvalid: boolean;
  formatPrice: (price: number) => string;
  toPersianDigits: (strOrNum: string | number) => string;
}

