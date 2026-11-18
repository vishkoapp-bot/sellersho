import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { apiRequest, isMockMode } from '../services/apiClient';
import { useAuth } from './AuthContext';
import { 
  PlatformSettings, 
  PayoutRequest, 
  AdjustmentRecord, 
  DisputeRecord, 
  AuditLogEntry, 
  PendingApprovalItem,
  PlatformContracts,
  SubscriptionPlan,
  PackagingTariff,
  DeliveryConfirmationSettings
} from '../types';
import { 
  DEFAULT_DEPOT_BASE_QUOTA_BY_CATEGORY, 
  DEFAULT_PACKAGING_TARIFFS 
} from '../utils/pricingEngine';

const INITIAL_SUBSCRIPTION_PLANS: SubscriptionPlan[] = [
  {
    id: 'trial',
    name: 'آزمایشی رایگان',
    durationDays: 14,
    durationLabel: '۱۴ روزه',
    price: 0,
    productLimit: 25,
    allowedCustomDomains: 0,
    maxStores: 1,
    features: ['۲۵ قلم کالا در کاتالوگ', 'ویترین اختصاصی سلر', 'لجستیک و انبارداری متمرکز', 'تسویه حساب هفتگی پایا'],
  },
  {
    id: 'standard',
    name: 'استاندارد ۳ ماهه',
    durationDays: 90,
    durationLabel: '۳ ماهه',
    price: 890000,
    productLimit: 100,
    allowedCustomDomains: 2,
    maxStores: 1,
    features: ['۱۰۰ قلم کالا در کاتالوگ', '۲ دامنه اختصاصی', 'تسویه حساب روزانه پایا'],
  },
  {
    id: 'pro',
    name: 'حرفه‌ای ۶ ماهه',
    durationDays: 180,
    durationLabel: '۶ ماهه',
    price: 1490000,
    productLimit: 300,
    allowedCustomDomains: 5,
    maxStores: 1,
    features: ['۳۰۰ قلم کالا در کاتالوگ', '۵ دامنه اختصاصی', 'ربات تلگرام و اتوماسیون سفارش', 'ویجت تبلیغاتی اختصاصی'],
  },
  {
    id: 'vip',
    name: 'طلایی یک‌ساله VIP',
    durationDays: 365,
    durationLabel: 'یک‌ساله VIP',
    price: 3900000,
    productLimit: 9999,
    allowedCustomDomains: 10,
    maxStores: 10,
    features: ['سقف کاتالوگ حداکثری (۹,۹۹۹ قلم کالا)', '۱۰ دامنه اختصاصی با اعتبارسنجی خودکار DNS', 'ساخت و مدیریت تا ۱۰ فروشگاه با یک حساب', 'کارمزد تسویه صفر', 'مدیر حساب اختصاصی'],
  },
];

const INITIAL_PLATFORM_SETTINGS: PlatformSettings = {
  platformPct: 0,
  centralProfitPct: 10,
  minProfitMargin: 5,
  maxProfitMargin: 35,
  defaultMargin: 20,
  attributionModel: 'last_touch',
  cookieDays: 30,
  autoConfirmDeliveryDays: 3,
  deliveryConfirmation: {
    holdPeriodDays: 20,
    followUpTriggerDays: 10,
  },
  referralProgram: {
    sellerCommissionPercent: 2,
    isEnabled: true,
    subjectToDeliveryLock: true,
  },
  contracts: {
    sellerContractText: 'قرارداد همکاری تجاری دراپ‌شیپینگ دروپ',
    sellerContractVersion: 2,
    supplierContractText: 'قرارداد تأمین و توزیع کالای دروپ',
    supplierContractVersion: 2,
    lastUpdated: '۱۴۰۵/۰۱/۱۵',
  },
  warehouseTariffs: {
    inboundFeePerCarton: 15000,
    storageFeePerCubicDay: 8000,
    pickingPackingFee: 0,
    firstMonthFreeStorage: true,
    depotBufferMultiplier: 1.5,
    baseMinDepotQuota: 100,
    depotBaseQuotaByCategory: DEFAULT_DEPOT_BASE_QUOTA_BY_CATEGORY,
  },
  packagingTariffs: DEFAULT_PACKAGING_TARIFFS,
  subscriptionPlans: INITIAL_SUBSCRIPTION_PLANS,
  supplierPromotions: [],
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
    shebaVerificationApi: {
      apiUrl: 'https://api.jibit.ir/v1/cards/sheba',
      // SEC-01: no credential may ship in the frontend bundle. The Sheba
      // verification integration stays DISABLED until a real key is injected
      // server-side; the previous fake placeholder apiKey has been removed.
      isEnabled: false,
    },
  },
};

export interface FinanceContextType {
  balance: number;
  payoutRequests: PayoutRequest[];
  requestPayout: (storeId: string, amount: number, shaba: string, bankName: string) => boolean;
  approvePayout: (payoutId: string) => void;
  rejectPayout: (payoutId: string) => void;
  adjustmentRecords: AdjustmentRecord[];
  createAdjustmentRecord: (record: Omit<AdjustmentRecord, 'id' | 'date'>) => void;
  disputes: DisputeRecord[];
  resolveDisputeAndAdjust: (
    disputeId: string,
    responsibleParty: string,
    actionType?: 'release_to_seller' | 'refund_carrier' | 'refund_warehouse' | 'refund_supplier' | 'refund_seller'
  ) => void;
  verifyShebaOwnerName: (shaba: string, expectedName: string) => Promise<{ success: boolean; ownerName: string; bankName: string; error?: string }>;
  executeSupplierMonthlyPayouts: () => void;
  platformSettings: PlatformSettings;
  updatePlatformSettings: (settings: Partial<PlatformSettings>) => void;
  updateContracts: (contracts: Partial<PlatformContracts>) => void;
  updateSubscriptionPlans: (plans: SubscriptionPlan[]) => void;
  updateTrialCatalogQuota: (quota: number) => void;
  updateDeliveryConfirmationSettings: (settings: DeliveryConfirmationSettings) => void;
  updateWarehouseTariffs: (tariffs: Partial<PlatformSettings['warehouseTariffs']>) => void;
  updatePackagingTariffs: (tariffs: PackagingTariff[]) => void;
  unifiedPendingApprovals: PendingApprovalItem[];
  approveUnifiedItem: (item: PendingApprovalItem) => void;
  rejectUnifiedItem: (item: PendingApprovalItem, reason?: string) => void;
  auditLogs: AuditLogEntry[];
  logAuditEvent: (action: string, category: AuditLogEntry['category'], details: string) => void;
}

const FinanceContext = createContext<FinanceContextType | undefined>(undefined);

const INITIAL_PAYOUT_REQUESTS: PayoutRequest[] = [
  {
    id: 'pay-req-001',
    storeId: 'store-bibi',
    recipientType: 'دراپ‌شیپر',
    recipientName: 'بهاره رضوانی',
    amount: 14500000,
    clearedProfit: 14500000,
    requestDate: '۱۴۰۵/۰۶/۲۹',
    status: 'pending',
    shaba: 'IR820120000000001234567890',
    bankName: 'بانک ملت',
  },
  {
    id: 'pay-req-002',
    storeId: 'sup-technolife',
    recipientType: 'تأمین‌کننده',
    recipientName: 'شرکت بازرگانی فناوری نوین (تکنولایف)',
    amount: 38400000,
    clearedProfit: 38400000,
    requestDate: '۱۴۰۵/۰۶/۳۰',
    status: 'pending',
    shaba: 'IR550170000000112233445566',
    bankName: 'بانک ملی ایران',
  },
];

const INITIAL_DISPUTES: DisputeRecord[] = [
  {
    id: 'DISP-9102',
    orderNumber: 'ORD-9021',
    type: 'return_7day',
    customerName: 'سارا کاویانی',
    productTitle: 'پاوربانک شیائومی Redmi PB200LZM ۲۰۰۰۰',
    claimAmount: 850000,
    reason: 'درخواست مرجوعی ۷ روزه به دلیل عدم تطابق رنگ',
    storeId: 'store-bibi',
    storeName: 'فروشگاه بی‌بی استایل',
    supplierName: 'تکنولایف',
    time: '۱۴۰۵/۰۶/۲۸',
    status: 'pending',
  },
];

const INITIAL_ADJUSTMENTS: AdjustmentRecord[] = [
  {
    id: 'ADJ-1001',
    date: '۱۴۰۵/۰۶/۲۵',
    targetAccount: 'لجستیک و خسارت باربری (حساب واسط)',
    targetType: 'carrier',
    type: 'debit',
    amount: 350000,
    reason: 'جبران خسارت شکستگی فیزیکی هندزفری حین جابجایی پیک اسنپ‌باکس',
    operator: 'سارا کمالی (امور مالی)',
    relatedOrderOrDispute: 'ORD-8910',
  },
];

const INITIAL_AUDIT_LOGS: AuditLogEntry[] = [
  {
    id: 'log-001',
    timestamp: '۱۴۰۵/۰۶/۲۵ ۱۰:۱۵',
    operator: 'super_admin',
    action: 'تغییر کارمزد پایه',
    category: 'commission',
    details: 'نرخ کارمزد پایه سامانه از ۱۰۰,۰۰۰ به ۱۱۰,۰۰۰ ریال تغییر یافت.',
  },
];

export const FinanceProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { currentUser, authToken } = useAuth();
  const pricingCapabilities = currentUser?.capabilities ?? (currentUser ? [currentUser.role] : []);
  const canReadSellerPricing = pricingCapabilities.some((capability) => ['seller', 'admin', 'super_admin'].includes(capability));
  const [balance, setBalance] = useState<number>(0);
  const [payoutRequests, setPayoutRequests] = useState<PayoutRequest[]>(INITIAL_PAYOUT_REQUESTS);
  const [adjustmentRecords, setAdjustmentRecords] = useState<AdjustmentRecord[]>(INITIAL_ADJUSTMENTS);
  const [disputes, setDisputes] = useState<DisputeRecord[]>(INITIAL_DISPUTES);
  const [auditLogs, setAuditLogs] = useState<AuditLogEntry[]>(INITIAL_AUDIT_LOGS);
  const [platformSettings, setPlatformSettings] = useState<PlatformSettings>(INITIAL_PLATFORM_SETTINGS);

  // Contracts come from the server (GET /platform/contracts); the INITIAL
  // values are only a fallback when the request fails or mock mode is on.
  useEffect(() => {
    if (isMockMode()) return;
    apiRequest<{ sellerContractText?: string; sellerContractVersion?: number; supplierContractText?: string; supplierContractVersion?: number }>('/platform/contracts', { method: 'GET' })
      .then((c) => setPlatformSettings((prev) => ({
        ...prev,
        contracts: {
          ...prev.contracts,
          ...(c.sellerContractText ? { sellerContractText: c.sellerContractText } : {}),
          ...(typeof c.sellerContractVersion === 'number' ? { sellerContractVersion: c.sellerContractVersion } : {}),
          ...(c.supplierContractText ? { supplierContractText: c.supplierContractText } : {}),
          ...(typeof c.supplierContractVersion === 'number' ? { supplierContractVersion: c.supplierContractVersion } : {}),
        },
      })))
      .catch(() => {});
  }, []);
  // Seller pricing rules come from the server (GET /seller/pricing-settings);
  // Fetch only after an authorized session is ready. Supplier-only users
  // and guests must not send requests to a seller-restricted endpoint. Without
  // this, the seller modal/tab would simulate against stale mock settings.
  useEffect(() => {
    if (isMockMode() || !authToken || !canReadSellerPricing) return;
    const controller = new AbortController();
    apiRequest<{ platformPct?: number; centralProfitPct?: number; minProfitMargin?: number; maxProfitMargin?: number; defaultMargin?: number }>('/seller/pricing-settings', { method: 'GET', auth: true, signal: controller.signal })
      .then((s) => setPlatformSettings((prev) => ({
        ...prev,
        ...(typeof s.platformPct === 'number' ? { platformPct: s.platformPct } : {}),
        ...(typeof s.centralProfitPct === 'number' ? { centralProfitPct: s.centralProfitPct } : {}),
        ...(typeof s.minProfitMargin === 'number' ? { minProfitMargin: s.minProfitMargin } : {}),
        ...(typeof s.maxProfitMargin === 'number' ? { maxProfitMargin: s.maxProfitMargin } : {}),
        ...(typeof s.defaultMargin === 'number' ? { defaultMargin: s.defaultMargin } : {}),
      })))
      .catch(() => {});
    return () => controller.abort();
  }, [authToken, canReadSellerPricing]);
  const [unifiedPendingApprovals, setUnifiedPendingApprovals] = useState<PendingApprovalItem[]>([]);

  const requestPayout = (storeId: string, amount: number, shaba: string, bankName: string): boolean => {
    const newRequest: PayoutRequest = {
      id: `pay-req-${Date.now().toString().slice(-4)}`,
      storeId,
      recipientType: 'دراپ‌شیپر',
      recipientName: storeId,
      amount,
      clearedProfit: amount,
      requestDate: new Date().toLocaleDateString('fa-IR'),
      status: 'pending',
      shaba,
      bankName,
    };
    setPayoutRequests((prev) => [newRequest, ...prev]);
    return true;
  };

  const approvePayout = (payoutId: string) => {
    setPayoutRequests((prev) =>
      prev.map((req) => (req.id === payoutId ? { ...req, status: 'approved' } : req))
    );
  };

  const rejectPayout = (payoutId: string) => {
    setPayoutRequests((prev) =>
      prev.map((req) => (req.id === payoutId ? { ...req, status: 'rejected' } : req))
    );
  };

  const createAdjustmentRecord = (record: Omit<AdjustmentRecord, 'id' | 'date'>) => {
    const newRecord: AdjustmentRecord = {
      ...record,
      id: `ADJ-${Date.now().toString().slice(-4)}`,
      date: new Date().toLocaleDateString('fa-IR'),
    };
    setAdjustmentRecords((prev) => [newRecord, ...prev]);
  };

  const resolveDisputeAndAdjust = (
    disputeId: string,
    responsibleParty: string,
    actionType: 'release_to_seller' | 'refund_carrier' | 'refund_warehouse' | 'refund_supplier' | 'refund_seller' = 'release_to_seller'
  ) => {
    setDisputes((prev) =>
      prev.map((d) =>
        d.id === disputeId
          ? {
              ...d,
              responsibleParty,
              resolutionAction: actionType,
              resolutionDate: new Date().toLocaleDateString('fa-IR'),
            }
          : d
      )
    );
  };
  const verifyShebaOwnerName = async (
    shaba: string,
    expectedName: string
  ): Promise<{ success: boolean; ownerName: string; bankName: string; error?: string }> => {
    const clean = shaba.replace(/\s+/g, '');
    if (clean.length < 24) {
      return { success: false, ownerName: '', bankName: '', error: 'شماره شبا نامعتبر است' };
    }
    return { success: true, ownerName: expectedName || 'صاحب حساب شبا', bankName: 'بانک عامل' };
  };

  const executeSupplierMonthlyPayouts = () => {};

  const updatePlatformSettings = (settings: Partial<PlatformSettings>) => {
    setPlatformSettings((prev) => ({ ...prev, ...settings }));
  };

  const updateContracts = (contracts: Partial<PlatformContracts>) => {
    setPlatformSettings((prev) => ({
      ...prev,
      contracts: { ...prev.contracts, ...contracts },
    }));
  };

  const updateSubscriptionPlans = (plans: SubscriptionPlan[]) => {
    setPlatformSettings((prev) => ({ ...prev, subscriptionPlans: plans }));
  };

  const updateTrialCatalogQuota = (quota: number) => {
    setPlatformSettings((prev) => ({
      ...prev,
      subscriptionPlanLimits: { ...prev.subscriptionPlanLimits, trial: quota },
    }));
  };



  const updateDeliveryConfirmationSettings = (settings: DeliveryConfirmationSettings) => {
    setPlatformSettings((prev) => ({ ...prev, deliveryConfirmation: settings }));
  };

  const updateWarehouseTariffs = (tariffs: Partial<PlatformSettings['warehouseTariffs']>) => {
    setPlatformSettings((prev) => ({
      ...prev,
      warehouseTariffs: { ...prev.warehouseTariffs, ...tariffs },
    }));
  };

  const updatePackagingTariffs = (tariffs: PackagingTariff[]) => {
    setPlatformSettings((prev) => ({ ...prev, packagingTariffs: tariffs }));
  };

  const approveUnifiedItem = (item: PendingApprovalItem) => {
    setUnifiedPendingApprovals((prev) => prev.filter((i) => i.id !== item.id));
  };

  const rejectUnifiedItem = (item: PendingApprovalItem, reason?: string) => {
    setUnifiedPendingApprovals((prev) => prev.filter((i) => i.id !== item.id));
  };

  const logAuditEvent = (action: string, category: AuditLogEntry['category'], details: string) => {
    const entry: AuditLogEntry = {
      id: `log-${Date.now().toString().slice(-4)}`,
      timestamp: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
      operator: 'admin',
      action,
      category,
      details,
    };
    setAuditLogs((prev) => [entry, ...prev]);
  };

  const value: FinanceContextType = {
    balance,
    payoutRequests,
    requestPayout,
    approvePayout,
    rejectPayout,
    adjustmentRecords,
    createAdjustmentRecord,
    disputes,
    resolveDisputeAndAdjust,
    verifyShebaOwnerName,
    executeSupplierMonthlyPayouts,
    platformSettings,
    updatePlatformSettings,
    updateContracts,
    updateSubscriptionPlans,
    updateTrialCatalogQuota,
    updateDeliveryConfirmationSettings,
    updateWarehouseTariffs,
    updatePackagingTariffs,
    unifiedPendingApprovals,
    approveUnifiedItem,
    rejectUnifiedItem,
    auditLogs,
    logAuditEvent,
  };

  return <FinanceContext.Provider value={value}>{children}</FinanceContext.Provider>;
};

export const useFinance = (): FinanceContextType => {
  const context = useContext(FinanceContext);
  if (!context) {
    throw new Error('useFinance must be used within a FinanceProvider');
  }
  return context;
};
