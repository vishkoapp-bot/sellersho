import type { ProductSchema } from './productSchema';
export type { ProductSchema, ProductAttribute, ProductOption, CatalogProductAttribute } from './productSchema';
// FIX 4 (team 04 G1/G2): live variant shape, carried through the
// product model so the detail view can offer a variant selector and
// price/stock overrides exactly like live (variantPrices keyed by id).
export interface ProductVariant {
  selection?: Record<string,string>;
  combinationKey?: string;
  inventoryMode?: 'legacy' | 'tracked';
  stockAuthority?: 'coded-v1';
  isActive?: boolean;
  id: string;
  spnc: string;
  name: string;
  variantName: string;
  attributes: Record<string, unknown>;
  stock: number;
  supplierBaseCost?: number; // per-variant wholesale cost (D28; falls back to the product cost)
  isDefault: boolean;
  minCartQty: number;
  maxCartQty: number | null;
  // Variant systems P0/P1/P2 (Nov 13, 2026): all optional — old DTOs
  // without them keep working. Backend contract: images live in the
  // product_variant_images table (cap 4/variant, images[0] is the hero);
  // physicals are mm/g, NULL = inherit from the product; sale fields
  // use the backend's saleFrom/saleTo naming (P1(c)).
  imageUrl?: string | null; // hero image; derived from images[0] when absent
  images?: string[]; // variant gallery (hero first, max 3 more); empty = product gallery
  weightGrams?: number | null; // null = inherit Product.weightGrams
  lengthMm?: number | null; // dims are all-or-none: all three set or all inherit
  widthMm?: number | null;
  heightMm?: number | null;
  shippingClass?: string | null; // null = inherit product class (stored only, P2(b))
  taxClass?: string | null; // null = inherit product class (stored only, P2(b))
  salePrice?: number | null; // configured sale price (Toman); see VariantPrice for the resolved one
  saleFrom?: string | null;
  saleTo?: string | null;
}

export interface VariantPrice {
  price: number;
  originalPrice: number;
  discountPercent: number;
  campaignName?: string;
  // P1(c) sale: salePrice is the CONFIGURED sale price S; price is
  // already the post-sale final when onSale is true (sale REPLACES the
  // resolved final price, never stacks; floor R enforced server-side).
  salePrice?: number;
  saleFrom?: string | null;
  saleTo?: string | null;
  onSale?: boolean; // true iff the sale determined this final price
}

// P2(a)/P2(c): per-store variant settings, edited only in
// SellerProductPricingModal. visibleInStore=false hides the variant in
// this store only (global isActive untouched); at most one variant per
// (store, product) is the store default.
export interface StoreVariantSetting {
  storeId: string;
  productId: string;
  variantId: string;
  visibleInStore: boolean;
  isStoreDefault: boolean;
}

// FIX 14b: live PDP addon card (see catalogApi ProductAddonDto).
export interface ProductAddon {
  id: string;
  title: string;
  description?: string;
  feePerUnit: number;
  ownerType: string;
}

export interface Product {
  productType?: 'legacy' | 'simple' | 'variable';
  schemaVersion?: number;
  productSchema?: ProductSchema;
  id: string;
  title: string;
  slug: string;
  spnNumber?: number;
  returnWindowEndsAt?: string | null;
  category: string;
  supplierId: string;
  supplierName: string;
  originalPrice: number; // in Tomans
  price: number; // central or current store price in Tomans
  supplierBaseCost: number; // C in proposal (wholesale cost)
  platformMargin: number; // F in proposal (central platform share)
  discountPercent: number;
  campaignName?: string;
  cashbackPercent: number; // 3% or 7%
  rating: number; // 4.8
  reviewsCount: number;
  salesCount: number;
  stock: number;
  image: string;
  images: string[];
  specs: { [key: string]: string };
  variants?: ProductVariant[];
  variantPrices?: Record<string, VariantPrice>;
  addons?: ProductAddon[];
  addonSectionTitle?: string;
  attributeValues?: { attributeId: number; valueId: number }[];
  brand?: string;
  isActive?: boolean;
  discountEligible?: boolean;
  storeCount?: number;
  // Variant systems P0/P1/P2: product-level physicals (the variant
  // fallback target — the propose form requires them, but they were
  // dropped by mapBackendProduct until now) and the per-store price
  // range + store default (P1(a)/P2(c), *int64 omitempty server-side).
  weightGrams?: number;
  lengthMm?: number;
  widthMm?: number;
  heightMm?: number;
  priceMin?: number;
  priceMax?: number;
  storeDefaultVariantId?: string | null;
  // Resolved sale window of the currently selected variant (set by
  // productForVariant from its VariantPrice entry; drives the PDP
  // countdown). Not a stored product field.
  saleFrom?: string | null;
  saleTo?: string | null;
  description: string;
  warehouse: string; // e.g. "انبار مرکزی تهران"
  preparationDays: number;
  supplierWarranty: string;
  supplyOption?: '3hour_local' | '1day' | '3day';
  featured?: boolean;
  specialDeal?: boolean;
  nicheIds?: string[];
  isOriginal?: boolean; // Requirement 23: false means non-original / های‌کپی
}

export interface Supplier {
  id: string;
  name: string;
  logo: string;
  rating: number;
  totalProducts: number;
  deliveryScore: number;
  warehouseCity: string;
  description: string;
  badge: string;
}

export interface StoreSocialLinks {
  instagram?: string;
  telegram?: string;
  whatsapp?: string;
  eitaa?: string;
  bale?: string;
}

export interface StoreBuilderConfig {
  selectedTheme: string;
  accentColor: string;
  widgets: {
    heroBanner: boolean;
    dealsSlider: boolean;
    categoriesGrid: boolean;
    storeDealsWidget: boolean;
    trustBar: boolean;
    contactInfo: boolean;
  };
  socialLinks?: StoreSocialLinks;
  publishedAt?: string;
  draftConfig?: Partial<StoreBuilderConfig>;
}

export interface Niche {
  id: string;
  title: string;
  slug: string;
  categorySlug: string;
  badge?: string;
  description?: string;
}

export interface StoreNameChangeRequest {
  id: string;
  storeId: string;
  currentName: string;
  requestedName: string;
  sellerMobile: string;
  requestDate: string;
  status: 'pending' | 'approved' | 'rejected';
}

export interface StoreSlugChangeRequest {
  id: string;
  storeId: string;
  currentSlug: string;
  requestedSlug: string;
  sellerMobile: string;
  requestDate: string;
  status: 'pending' | 'approved' | 'rejected';
}

export type ApprovalEntityType = 
  | 'store_name' 
  | 'store_slug' 
  | 'custom_domain' 
  | 'proposed_product' 
  | 'payout';

export interface PendingApprovalItem {
  id: string;
  entityType: ApprovalEntityType;
  targetId: string;
  title: string;
  requestedData: Record<string, unknown>;
  requestedBy: string;
  status: 'pending' | 'approved' | 'rejected';
  createdAt: string;
  reviewedAt?: string;
  rejectionReason?: string;
}

export interface CustomDomainRequest {
  id: string;
  storeId: string;
  storeName: string;
  requestedDomain: string;
  requestDate: string;
  status: 'pending' | 'approved' | 'rejected';
  nameserver1?: string;
  nameserver2?: string;
  verifiedAt?: string;
}

export interface PreMadeWidget {
  id: string;
  title: string;
  type: 'central_deals' | 'seller_deals' | 'best_sellers' | 'html_banner';
  description: string;
  bannerImage?: string;
  previewImageUrl?: string;
  targetCategory?: string;
  targetNicheIds?: string[];
  recommendedProductIds?: string[];
  bannerSize?: 'square' | 'landscape' | 'story' | 'leaderboard';
  bannerWidth?: number;
  bannerHeight?: number;
  htmlCodeSnippet?: string;
  createdAt: string;
}

export interface Store {
  id: string;
  name: string;
  slug: string;
  ownerType: 'central' | 'dropshipper';
  ownerName: string;
  tagline: string;
  shortDescription?: string;
  city?: string;
  logo?: string;
  profitMarginPercent: number; // 0% for central, or e.g. 10% for dropshipper
  description: string;
  activeCategories: string[];
  totalSales: number;
  monthlyRevenueEstimate: number;
  activeProductIds?: string[];
  selectedNicheIds?: string[];
  productPriceOverrides?: { [productId: string]: number };
  productPriceOverrideModes?: { [productId: string]: 'percent' | 'fixed' };
  categoryPriceOverrides?: { [categorySlug: string]: number };
  nichePriceOverrides?: { [nicheId: string]: number };
  nichePriceOverrideModes?: { [nicheId: string]: 'percent' | 'fixed' };
  footerText?: string;
  sellerDealsWidgetConfig?: {
    title: string;
    productIds: string[];
    countdownHours: number;
    endsAt: string;
    // Campaign discount per product, carved out of the seller's own
    // resolved margin (percent, capped at that product's margin).
    discounts?: Record<string, number>;
  };
  centralDealsFilter?: 'my_niches' | 'manual' | 'all';
  centralDealsProductIds?: string[];
  builderConfig?: StoreBuilderConfig;
  // Public widgets-only subset of builderConfig (emitted by the
  // public store DTO as builderWidgets): the six section toggles
  // for real visitors. The owner preview uses builderConfig.widgets,
  // which takes precedence whenever it is present.
  builderWidgets?: StoreBuilderConfig['widgets'];
  customDomain?: string;
  activePlanId?: 'trial' | 'standard' | 'pro' | 'vip';
  planExpiresAt?: string;
  smsCredits?: number;
  referrerSellerId?: string; // Requirement 8: Seller referral relationship
  referralCode?: string; // Unique code for seller to share
  customStoreMarginOverride?: number; // Requirement 13: Admin manual override for store default margin
  isMarginLockedByAdmin?: boolean; // Requirement 13: Admin lock preventing seller modifications
  signedContractVersion?: number; // Item 7: Version of the contract signed by this seller
  status?: string; // Live public store status (active | suspended, D45)
  ownerPhone?: string; // Live store_suspended page field; rendered only when the API provides it (TODO U42)
  templateId?: string; // Live storefront template id (D04), NOT builder selectedTheme
  templateConfig?: Record<string, unknown>;
}

export interface PayoutRequest {
  id: string;
  storeId?: string;
  recipientType: 'دراپ‌شیپر' | 'تأمین‌کننده';
  recipientName: string;
  shaba: string;
  bankName: string;
  amount: number;
  clearedProfit: number;
  requestDate: string;
  status: 'pending' | 'approved' | 'rejected';
}

export interface CartItem {
  product: Product;
  quantity: number;
  selectedColor?: string;
  // FIX 5 (from Fix 4): the variant this line was bought in. The cart
  // key is product::variant (see utils/cartKey), so two variants of
  // one product are two lines; orderItemId is the backend order-line
  // id, needed for a live-shaped return request.
  selectedVariantId?: string;
  // FIX 14b: live cart lines carry addonIds; the key gains the sorted
  // suffix (see utils/cartKey), so different addons are two lines.
  selectedAddonIds?: string[];
  orderItemId?: string;
  storeId?: string;
  storeName?: string;
}

export interface Order {
  refund?: import('../services/orderApi').OrderRefundSummary;
  id: string;
  orderNumber: string;
  items: CartItem[];
  totalAmount: number;
  discountAmount: number;
  payableAmount: number;
  paymentMethod: 'cash';
  customerInfo: {
    fullName: string;
    phone: string;
    province: string;
    city: string;
    address: string;
    postalCode: string;
  };
  status: 'pending' | 'processing' | 'shipped' | 'delivered_unconfirmed' | 'delivered_confirmed' | 'delivered' | 'delivery_disputed' | 'cancelled';
  trackingCode: string;
  createdAt: string;
  shippedAt?: string;
  confirmedAt?: string;
  disputedAt?: string;
  disputeReason?: string;
  deliveryDate?: string;
  shippingCost?: number;
  shippingQuoteId?: string;
  returnWindowEndsAt?: string;
  storeId: string;
  storeName: string;
  dropshipperProfit: number;
  platformProfit: number;
  referralCommission?: number; // Commission paid to referrer seller from platformProfit (F)
  referralCommissionReleased?: boolean; // Item 2: Tied to delivery lock (released only when order is delivered_confirmed)
  referralCommissionReleasedAt?: string;
  referrerSellerId?: string;
  daysSinceShipped?: number;
  needsFollowUp?: boolean; // Flagged when daysSinceShipped >= followUpTriggerDays (day 10)
  profitReleased?: boolean; // Cleared and withdrawable in seller wallet
  profitReleasedAt?: string;
  adminReviewStatus?: 'pending_review' | 'resolved_released' | 'resolved_refunded';
}

export interface Category {
  id: string;
  title: string;
  slug: string;
  iconName: string;
  parentId?: string | null; // Live category tree (D39): parent from Team 04's API
  count: number;
  gradient: string;
  image: string;
}

export interface Banner {
  id: string;
  title: string;
  subtitle: string;
  link: string;
  bgGradient: string;
  tag: string;
  image: string;
}

export interface PackagingTariff {
  id: string;
  title: string;
  minWeightGrams: number;
  maxWeightGrams: number;
  destinationType: 'intra_province' | 'inter_province';
  boxSize: string;
  cost: number;
}

export interface SubscriptionPlan {
  id: string;
  name: string;
  durationDays: number;
  durationLabel: string;
  price: number;
  productLimit: number;
  allowedCustomDomains: number;
  maxStores: number;
  features: string[];
}

export interface SupplierPromotion {
  id: string;
  title: string;
  supplierId: string; // 'all' or specific supplierId
  discountType: 'platform_fee_discount' | 'warehouse_free_storage';
  discountValue: number;
  validUntil: string;
  status: 'active' | 'expired';
}

export interface DeliveryConfirmationSettings {
  holdPeriodDays: number; // default: 20
  followUpTriggerDays: number; // default: 10
}

export interface ReferralProgramSettings {
  sellerCommissionPercent: number; // default: 2% (from platform fee F)
  isEnabled: boolean;
  subjectToDeliveryLock: boolean; // Item 2: commission tied to 20-day delivery confirmation lock
}

export interface PlatformContracts {
  sellerContractText: string;
  sellerContractVersion?: number; // Unknown until the live contract response supplies it.
  supplierContractText: string;
  supplierContractVersion: number; // Item 7: contract versioning
  lastUpdated: string;
}

export interface PlatformSettings {
  platformPct?: number; // global platform share percent (server-sourced for sellers)
  centralProfitPct?: number; // central store margin percent (server-sourced for sellers)
  minProfitMargin: number;
  maxProfitMargin: number;
  defaultMargin?: number;
  attributionModel: 'last_touch' | 'first_touch';
  cookieDays: number;
  autoConfirmDeliveryDays?: number;
  deliveryConfirmation: DeliveryConfirmationSettings;
  referralProgram: ReferralProgramSettings;
  contracts: PlatformContracts;
  warehouseTariffs: {
    inboundFeePerCarton: number;
    storageFeePerCubicDay: number;
    pickingPackingFee: number;
    firstMonthFreeStorage: boolean;
    depotBufferMultiplier: number;
    baseMinDepotQuota: number;
    depotBaseQuotaByCategory?: Record<string, number>;
    trialPlanCatalogQuota?: number; // Item 3: trial plan catalog quota
  };
  packagingTariffs?: PackagingTariff[];
  subscriptionPlans: SubscriptionPlan[];
  supplierPromotions?: SupplierPromotion[];
  subscriptionPlanLimits: {
    trial: number;
    standard: number;
    pro: number;
    vip: number;
  };
  centralCampaignBudget?: number;
  centralCampaignBudgetUsed?: number;
  adRevenue: {
    centralClickRate: number;
    sellerClickShare: number;
    platformClickShareOnSeller: number;
    centralViewRateCPM: number;
    sellerViewShareCPM: number;
    platformViewShareOnSellerCPM: number;
  };
  integrations: {
    shebaVerificationApi: {
      apiUrl: string;
      // SEC-01: optional on purpose — no API key may ever be set as a
      // frontend default. A real key, if ever needed, must be injected
      // server-side and never shipped in the bundle.
      apiKey?: string;
      isEnabled: boolean;
    };
  };
}

export interface AdjustmentRecord {
  id: string;
  date: string;
  targetAccount: string;
  targetType: 'seller' | 'supplier' | 'carrier';
  type: 'debit' | 'credit';
  amount: number;
  reason: string;
  operator: string;
  relatedOrderOrDispute?: string;
}

export interface AuditLogEntry {
  id: string;
  timestamp: string;
  operator: string;
  action: string;
  category: 'commission' | 'catalog' | 'store_status' | 'warehouse' | 'finance' | 'subscription' | 'dispute' | 'supplier';
  details: string;
}

export interface DisputeRecord {
  id: string;
  orderNumber: string;
  type: 'return_7day' | 'delivery_undelivered';
  customerName: string;
  productTitle: string;
  claimAmount: number;
  reason: string;
  storeId: string;
  storeName: string;
  supplierName: string;
  time: string;
  status: 'pending' | 'resolved';
  responsibleParty?: string;
  resolutionDate?: string;
  resolutionAction?: 'release_to_seller' | 'refund_carrier' | 'refund_warehouse' | 'refund_supplier' | 'refund_seller';
}

export interface SupplierSession {
	permissions?: string[];
  token: string;
  supplierId: string;
  companyName: string;
  managerName: string;
  warehouseCity: string;
  warehouseCityConfirmed?: boolean;
  loginAt: string;
  signedContractVersion?: number; // Item 7: contract versioning
}

export interface ProposedProduct {
  id: string;
  productId?: string;
  title: string;
  proposedCost: number;
  category: string;
  nicheIds?: string[]; // Priority 1.4: official niche taxonomy selection
  supplierId: string;
  supplierName: string;
  barcode: string;
  warranty: string;
  imageUrl?: string;
  description?: string;
  specs?: Record<string, string>;
  stock?: number;
  warehouse?: string;
  preparationDays?: number;
  isOriginal?: boolean;
  status: 'pending' | 'approved' | 'rejected';
  submittedAt?: string;
  reviewedAt?: string;
  rejectionReason?: string;
  createdAt?: string;
}

export interface InboundShipment {
  id: string;
  supplierId: string;
  supplierName: string;
  warehouse: string;
  itemsCount: number;
  cartonsCount: number;
  carrier?: string;
  truckPlate?: string;
  date: string;
  status: 'in_transit' | 'received' | 'putaway_completed';
  statusText: string;
  type: 'inbound' | 'return';
  productId?: string;
  productTitle?: string;
  binLocation?: string;
  batchNumber?: string;
}

export interface WarehouseSession {
  token: string;
  staffId: string;
  staffName: string;
  warehouseId: string;
  warehouseName: string;
  role: 'warehouse_staff' | 'logistics_specialist' | 'qc_inspector' | 'supervisor';
  loginAt: string;
}

export interface QCDiscrepancyReport {
  id: string;
  inboundId: string;
  supplierId: string;
  supplierName?: string;
  productTitle: string;
  expectedCount: number;
  receivedCount: number;
  passedQC: number;
  rejectedDamaged: number;
  shortage: number;
  date: string;
  inspectorNote: string;
  status: 'pending_review' | 'accepted' | 'disputed';
}

export interface AdCampaign {
  id: string;
  supplierId: string;
  title: string;
  model: 'کلیک معتبر (CPC)' | 'هزار نمایش (CPM)';
  bid: number;
  dailyBudget: number;
  spentToday: number;
  validClicks: number;
  status: 'active' | 'paused';
}
