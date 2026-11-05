import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { 
  InboundShipment, 
  QCDiscrepancyReport, 
  ProposedProduct, 
  AdCampaign, 
  SupplierPromotion,
  Product
} from '../types';
import { apiRequest, isMockMode } from '../services/apiClient';
import { useProducts } from './ProductContext';

export interface WarehouseContextType {
  inboundShipments: InboundShipment[];
  createInboundShipment: (shipment: Omit<InboundShipment, 'id' | 'date' | 'status' | 'statusText'>) => InboundShipment;
  receiveInboundShipment: (shipmentId: string) => void;
  completePutaway: (shipmentId: string, binLocation: string, batchNumber?: string) => void;
  completeOrderPicking: (orderId: string) => void;
  completeOrderPacking: (orderId: string, weightGrams: number, boxSize: string) => void;
  dispatchOrdersToShipping: (orderIds: string[], carrierName: string) => { manifestId: string; trackingCodes: string[] };
  processWarehouseReturn: (disputeId: string, decision: 'restock' | 'damaged', notes?: string) => void;
  qcDiscrepancies: QCDiscrepancyReport[];
  submitQCDiscrepancy: (report: Omit<QCDiscrepancyReport, 'id' | 'date' | 'status'>) => void;
  acceptQCDiscrepancy: (reportId: string) => void;
  disputeQCDiscrepancy: (reportId: string, objectionNote: string) => void;
  proposedProducts: ProposedProduct[];
  submitProposedProduct: (offer: Omit<ProposedProduct, 'id' | 'status' | 'createdAt'>) => void;
  approveAndMergeProduct: (item: ProposedProduct | any) => void;
  rejectProposedProduct: (item: ProposedProduct | any, reason: string) => void;
  updateWholesaleCost: (productId: string, newCostC: number) => void;
  adCampaigns: AdCampaign[];
  createAdCampaign: (campaign: Omit<AdCampaign, 'id' | 'spentToday' | 'validClicks' | 'status'>) => void;
  toggleAdCampaign: (campaignId: string) => void;
  supplierPromotions: SupplierPromotion[];
  createSupplierPromotion: (promo: Omit<SupplierPromotion, 'id'>) => void;
  deleteSupplierPromotion: (promoId: string) => void;
}

const WarehouseContext = createContext<WarehouseContextType | undefined>(undefined);

const INITIAL_INBOUND_SHIPMENTS: InboundShipment[] = [
  {
    id: 'INB-9941',
    supplierId: 'sup-technolife',
    supplierName: 'تکنولایف',
    warehouse: 'انبار مرکزی تهران',
    itemsCount: 100,
    cartonsCount: 8,
    date: '۱۴۰۵/۰۷/۰۱',
    status: 'in_transit',
    statusText: 'در راه به انبار مرکزی (باربری تیپاکس)',
    type: 'inbound',
  },
  {
    id: 'INB-9920',
    supplierId: 'sup-technolife',
    supplierName: 'تکنولایف',
    warehouse: 'انبار مرکزی تهران',
    itemsCount: 200,
    cartonsCount: 15,
    date: '۱۴۰۵/۰۶/۲۵',
    status: 'received',
    statusText: 'تحویل و جانمایی کامل در قفسه‌ها',
    type: 'inbound',
  },
];

const INITIAL_QC_DISCREPANCIES: QCDiscrepancyReport[] = [
  {
    id: 'DISC-4091',
    inboundId: 'INB-9941',
    supplierId: 'sup-technolife',
    productTitle: 'پاوربانک شیائومی Redmi PB200LZM ۲۰۰۰۰',
    expectedCount: 100,
    receivedCount: 97,
    passedQC: 95,
    rejectedDamaged: 2,
    shortage: 3,
    date: '۱۴۰۵/۰۶/۲۹',
    inspectorNote: '۳ عدد کسری در کارتن شماره ۴؛ ۲ عدد دارای جعبه آسیب‌دیده با ضربه در حمل باربری.',
    status: 'pending_review',
  },
];

const INITIAL_PROPOSED_PRODUCTS: ProposedProduct[] = [
  {
    id: 'prop-101',
    title: 'ایرپاد پرو ۲ مدل الترا کپی Master Copy',
    proposedCost: 450000,
    category: 'digital',
    nicheIds: ['mobile-accessories'],
    supplierId: 'sup-technolife',
    supplierName: 'تکنولایف',
    barcode: '6261234567890',
    warranty: 'گارانتی ۶ ماهه تعویض',
    status: 'pending',
    createdAt: '۱۴۰۵/۰۶/۳۰',
  },
];

const INITIAL_AD_CAMPAIGNS: AdCampaign[] = [
  {
    id: 'AD-2041',
    supplierId: 'sup-technolife',
    title: 'پروموشن کلیکی پاوربانک ردمی ۲۰۰۰۰',
    model: 'کلیک معتبر (CPC)',
    bid: 5000,
    dailyBudget: 500000,
    spentToday: 215000,
    validClicks: 43,
    status: 'active',
  },
];

export const WarehouseProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { addProduct, fetchProducts } = useProducts();
  const [inboundShipments, setInboundShipments] = useState<InboundShipment[]>(INITIAL_INBOUND_SHIPMENTS);
  const [qcDiscrepancies, setQcDiscrepancies] = useState<QCDiscrepancyReport[]>(INITIAL_QC_DISCREPANCIES);
  const [proposedProducts, setProposedProducts] = useState<ProposedProduct[]>(() => {
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem('drup_proposed_products');
        if (saved) return JSON.parse(saved);
      } catch {}
    }
    return INITIAL_PROPOSED_PRODUCTS;
  });
  const [adCampaigns, setAdCampaigns] = useState<AdCampaign[]>(INITIAL_AD_CAMPAIGNS);
  const [supplierPromotions, setSupplierPromotions] = useState<SupplierPromotion[]>([]);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      try {
        localStorage.setItem('drup_proposed_products', JSON.stringify(proposedProducts));
      } catch {}
    }

    // Automatically sync and merge any approved proposals into the products catalog
    proposedProducts.forEach((item) => {
      if (item.status === 'approved') {
        const prodId = item.productId || (item.id.startsWith('prod-') ? item.id : `prod-${item.id.replace('prop-', '')}`);
        const cost = item.proposedCost || 100000;
        const newProduct: Product = {
          id: prodId,
          title: item.title,
          slug: `product-${prodId}`,
          category: item.category || 'digital',
          supplierId: item.supplierId ?? '',
          supplierName: item.supplierName ?? '',
          originalPrice: Math.round(cost * 1.25),
          price: Math.round(cost * 1.2),
          supplierBaseCost: cost,
          platformMargin: Math.round(cost * 0.1),
          discountPercent: 5,
          cashbackPercent: 0,
          rating: 5,
          reviewsCount: 0,
          salesCount: 0,
          stock: item.stock ?? 50,
          image: item.imageUrl || 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/default.png',
          images: [item.imageUrl || 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/default.png'],
          specs: item.specs || {},
          description: item.description || '',
          warehouse: item.warehouse || 'انبار مرکزی تهران',
          preparationDays: item.preparationDays || 1,
          supplierWarranty: item.warranty || 'ضمانت اصالت و سلامت کالا',
          featured: false,
          specialDeal: false,
          nicheIds: item.nicheIds || ['niche-tws'],
          isOriginal: item.isOriginal ?? true,
        };
        addProduct(newProduct);
      }
    });
  }, [proposedProducts]);

  const createInboundShipment = (
    shipment: Omit<InboundShipment, 'id' | 'date' | 'status' | 'statusText'>
  ): InboundShipment => {
    const newShipment: InboundShipment = {
      ...shipment,
      id: `INB-${Math.floor(1000 + Math.random() * 9000)}`,
      date: new Date().toLocaleDateString('fa-IR'),
      status: 'in_transit',
      statusText: 'در انتظار تأیید و ارسال به انبار',
    };
    setInboundShipments((prev) => [newShipment, ...prev]);
    return newShipment;
  };

  const receiveInboundShipment = (shipmentId: string) => {
    setInboundShipments((prev) =>
      prev.map((s) =>
        s.id === shipmentId
          ? { ...s, status: 'received', statusText: 'دریافت شده در بارانداز، در صف QC' }
          : s
      )
    );
  };

  const completePutaway = (shipmentId: string, binLocation: string, batchNumber?: string) => {
    setInboundShipments((prev) =>
      prev.map((s) =>
        s.id === shipmentId
          ? {
              ...s,
              status: 'putaway_completed',
              statusText: `جانمایی در قفسه ${binLocation}`,
              binLocation,
              batchNumber,
            }
          : s
      )
    );
  };

  const completeOrderPicking = (orderId: string) => {};
  const completeOrderPacking = (orderId: string, weightGrams: number, boxSize: string) => {};

  const dispatchOrdersToShipping = (
    orderIds: string[],
    carrierName: string
  ): { manifestId: string; trackingCodes: string[] } => {
    const manifestId = `MAN-${Math.floor(100 + Math.random() * 900)}`;
    const trackingCodes = orderIds.map((id) => `TRK-${carrierName}-${id.slice(-4)}`);
    return { manifestId, trackingCodes };
  };
  const submitProposedProduct = (offer: Omit<ProposedProduct, 'id' | 'status' | 'createdAt'>) => {
    const tempId = `prop-${Date.now().toString().slice(-4)}`;
    const newProp: ProposedProduct = {
      ...offer,
      id: tempId,
      status: 'pending',
      createdAt: new Date().toLocaleDateString('fa-IR'),
      submittedAt: new Date().toISOString(),
    };
    setProposedProducts((prev) => [newProp, ...prev]);

    if (!isMockMode()) {
      apiRequest<any>('/supplier/products/propose', {
        method: 'POST',
        auth: true,
        body: JSON.stringify({
          title: offer.title,
          supplierBaseCost: offer.proposedCost,
          categorySlug: offer.category,
          nicheIds: offer.nicheIds,
          barcode: offer.barcode,
          warranty: offer.warranty,
          imageUrl: offer.imageUrl,
          image_url: offer.imageUrl,
          description: offer.description,
          specs: offer.specs,
          stock: offer.stock,
          warehouse: offer.warehouse,
          preparationDays: offer.preparationDays,
          isOriginal: offer.isOriginal,
          supplierId: offer.supplierId,
          supplierName: offer.supplierName,
        }),
      })
        .then((resp) => {
          if (resp && resp.proposal_id) {
            setProposedProducts((prev) =>
              prev.map((p) => (p.id === tempId ? { ...p, id: resp.proposal_id, productId: resp.product_id } : p))
            );
          }
        })
        .catch((err) => console.warn('Failed to propose product to backend:', err.message));
    }
  };

  const approveAndMergeProduct = async (item: ProposedProduct | any) => {
    // 1. Mark as approved in proposedProducts
    setProposedProducts((prev) =>
      prev.map((p) => (p.id === item.id ? { ...p, status: 'approved' } : p))
    );

    // 2. Call backend approval API if not in mock mode
    let backendProductID = item.productId;
    if (!isMockMode()) {
      try {
        const resp: any = await apiRequest(`/admin/products/approve/${item.id}`, {
          method: 'POST',
          auth: true,
          body: JSON.stringify({ approved: true, notes: 'تأیید شده از پنل مدیریت' }),
        });
        if (resp && resp.product_id) {
          backendProductID = resp.product_id;
        }
      } catch (err: any) {
        console.warn('Backend approval call notice:', err.message);
      }
    }

    // 3. Construct complete Product object
    const finalProdId = backendProductID || item.productId || (item.id.startsWith('prod-') ? item.id : `prod-${item.id.replace('prop-', '')}`);
    const cost = item.proposedCost || 100000;
    const newProduct: Product = {
      id: finalProdId,
      title: item.title,
      slug: `product-${Date.now()}`,
      category: item.category || 'digital',
      supplierId: item.supplierId ?? '',
      supplierName: item.supplierName ?? '',
      originalPrice: Math.round(cost * 1.25),
      price: Math.round(cost * 1.2),
      supplierBaseCost: cost,
      platformMargin: Math.round(cost * 0.1),
      discountPercent: 5,
      cashbackPercent: 0,
      rating: 5,
      reviewsCount: 0,
      salesCount: 0,
      stock: item.stock ?? 50,
      image: item.imageUrl || 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/default.png',
      images: [item.imageUrl || 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/default.png'],
      specs: item.specs || {},
      description: item.description || '',
      warehouse: item.warehouse || 'انبار مرکزی تهران',
      preparationDays: item.preparationDays || 1,
      supplierWarranty: item.warranty || 'ضمانت اصالت و سلامت کالا',
      featured: false,
      specialDeal: false,
      nicheIds: item.nicheIds || ['niche-tws'],
      isOriginal: item.isOriginal ?? true,
    };

    // 4. Merge into products catalog
    addProduct(newProduct);

    // 5. Refresh from backend database if running against live API
    if (!isMockMode()) {
      fetchProducts().catch(() => {});
    }
  };

  const rejectProposedProduct = (item: ProposedProduct | any, reason: string) => {
    setProposedProducts((prev) =>
      prev.map((p) => (p.id === item.id ? { ...p, status: 'rejected', rejectionReason: reason } : p))
    );
  };

  const updateWholesaleCost = (productId: string, newCostC: number) => {};

  const createAdCampaign = (campaign: Omit<AdCampaign, 'id' | 'spentToday' | 'validClicks' | 'status'>) => {
    const newCamp: AdCampaign = {
      ...campaign,
      id: `AD-${Date.now().toString().slice(-4)}`,
      spentToday: 0,
      validClicks: 0,
      status: 'active',
    };
    setAdCampaigns((prev) => [newCamp, ...prev]);
  };

  const toggleAdCampaign = (campaignId: string) => {
    setAdCampaigns((prev) =>
      prev.map((c) => (c.id === campaignId ? { ...c, status: c.status === 'active' ? 'paused' : 'active' } : c))
    );
  };

  const createSupplierPromotion = (promo: Omit<SupplierPromotion, 'id'>) => {
    const newPromo: SupplierPromotion = {
      ...promo,
      id: `PROMO-${Date.now().toString().slice(-4)}`,
    };
    setSupplierPromotions((prev) => [newPromo, ...prev]);
  };

  const deleteSupplierPromotion = (promoId: string) => {
    setSupplierPromotions((prev) => prev.filter((p) => p.id !== promoId));
  };
  const processWarehouseReturn = (disputeId: string, decision: 'restock' | 'damaged', notes?: string) => {};

  const submitQCDiscrepancy = (report: Omit<QCDiscrepancyReport, 'id' | 'date' | 'status'>) => {
    const newReport: QCDiscrepancyReport = {
      ...report,
      id: `DISC-${Date.now().toString().slice(-4)}`,
      date: new Date().toLocaleDateString('fa-IR'),
      status: 'pending_review',
    };
    setQcDiscrepancies((prev) => [newReport, ...prev]);
  };

  const acceptQCDiscrepancy = (reportId: string) => {
    setQcDiscrepancies((prev) =>
      prev.map((r) => (r.id === reportId ? { ...r, status: 'accepted' } : r))
    );
  };

  const disputeQCDiscrepancy = (reportId: string, objectionNote: string) => {
    setQcDiscrepancies((prev) =>
      prev.map((r) => (r.id === reportId ? { ...r, status: 'disputed', supplierObjectionNote: objectionNote } : r))
    );
  };


  const value: WarehouseContextType = {
    inboundShipments,
    createInboundShipment,
    receiveInboundShipment,
    completePutaway,
    completeOrderPicking,
    completeOrderPacking,
    dispatchOrdersToShipping,
    processWarehouseReturn,
    qcDiscrepancies,
    submitQCDiscrepancy,
    acceptQCDiscrepancy,
    disputeQCDiscrepancy,
    proposedProducts,
    submitProposedProduct,
    approveAndMergeProduct,
    rejectProposedProduct,
    updateWholesaleCost,
    adCampaigns,
    createAdCampaign,
    toggleAdCampaign,
    supplierPromotions,
    createSupplierPromotion,
    deleteSupplierPromotion,
  };

  return <WarehouseContext.Provider value={value}>{children}</WarehouseContext.Provider>;
};

export const useWarehouse = (): WarehouseContextType => {
  const context = useContext(WarehouseContext);
  if (!context) {
    throw new Error('useWarehouse must be used within a WarehouseProvider');
  }
  return context;
};
