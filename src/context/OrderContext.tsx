import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { Order, CartItem, Store, Product } from '../types';
import { initialOrders } from '../data/initialOrders';
import { BackendOrderDto, orderApi } from '../services/orderApi';
import { stageAddressSelection, takeStagedAddressSelection } from '../services/addressApi';
import { paymentApi } from '../services/paymentApi';
import { isMockMode } from '../services/apiClient';

export interface CreateOrderParams {
  customerInfo: Order['customerInfo'];
  paymentMethod: 'cash';
  discountCode?: string;
  shippingQuoteId?: string;
  // Merge-phase backend (Team 1 gap): see orderApi.CreateOrderPayload.
  addressId?: string;
  saveAddress?: boolean;
  cartItems: CartItem[];
  currentStore?: { id: string; name: string; slug?: string; ownerType?: string; profitMarginPercent?: number };
  customerPhone?: string;
  referredStoreSlug?: string | null;
  stores?: Store[];
}

export interface OrderContextType {
  orders: Order[];
  currentOrder: Order | null;
  isCreatingOrder: boolean;
  trackingOrder: Order | null;
  setTrackingOrder: (order: Order | null) => void;
  isTrackingModalOpen: boolean;
  setIsTrackingModalOpen: (open: boolean) => void;
  placeOrder: (params: CreateOrderParams) => Promise<Order>;
  fetchMyOrders: () => Promise<void>;
  trackOrder: (trackingCode: string) => Promise<Order | null>;
  confirmOrderDelivery: (orderId: string) => void;
  disputeOrderDelivery: (orderId: string, reason?: string) => void;
  adminResolveDeliveryDispute: (
    orderId: string,
    action: 'release_to_seller' | 'refund_to_customer',
    responsibleParty?: string
  ) => void;
  getOrderById: (orderId: string) => Order | undefined;
}

const OrderContext = createContext<OrderContextType | undefined>(undefined);

const mapBackendOrder = (order: BackendOrderDto): Order => ({
  id: order.id,
  orderNumber: order.orderNumber,
  items: (order.items || []).map((item): CartItem => ({
    product: {
      id: item.productId,
      title: item.productTitle || item.productId,
      slug: item.productId,
      category: '',
      supplierId: '',
      supplierName: '',
      originalPrice: item.finalUnitPrice,
      price: item.finalUnitPrice,
      supplierBaseCost: item.wholesaleCost,
      platformMargin: item.platformFee,
      discountPercent: 0,
      cashbackPercent: 0,
      rating: 0,
      reviewsCount: 0,
      salesCount: 0,
      isOriginal: true,
      stock: 0,
      image: '',
      images: [],
      specs: {},
      description: '',
      warehouse: '',
      preparationDays: 0,
      supplierWarranty: '',
    },
    quantity: item.quantity,
    selectedVariantId: item.variantId,
    orderItemId: item.id,
    storeId: order.storeId,
    storeName: order.storeName,
  })),
  totalAmount: order.totalAmount,
  discountAmount: order.discountAmount,
  payableAmount: order.payableAmount,
  paymentMethod: 'cash', // FIX 14: backend coerces to cash; no installments exist.
  customerInfo: { fullName: order.customerFullName, phone: order.customerPhone, province: order.customerProvince, city: order.customerCity, address: order.customerAddress, postalCode: order.customerPostalCode },
  status: order.status,
  trackingCode: order.trackingCode,
  createdAt: order.createdAt,
  shippedAt: order.shippedAt,
  confirmedAt: order.confirmedAt,
  disputedAt: order.disputedAt,
  shippingCost: order.shippingCost,
  shippingQuoteId: order.shippingQuoteId,
  returnWindowEndsAt: order.returnWindowEndsAt,
  storeId: order.storeId,
  storeName: order.storeName,
  dropshipperProfit: order.dropshipperProfit,
  platformProfit: order.platformProfit,
  referralCommission: order.referralCommission,
  referralCommissionReleased: order.referralCommissionReleased,
  referrerSellerId: order.referrerSellerId,
  profitReleased: order.profitReleased,
  profitReleasedAt: order.profitReleasedAt,
});

// ISO-B: backend order-creation errors are English strings carrying a
// raw product id (order_handlers.go); the drawer toast would otherwise
// show them verbatim to the buyer. Map the known store/address cases
// to human Persian copy. Unknown messages pass through unchanged so a
// genuinely new backend error stays visible instead of being masked.
const mapOrderCreateError = (message: string): string => {
  if (message.includes('is not published by this store')) {
    return 'این کالا در فروشگاه انتخاب‌شده منتشر نشده است؛ لطفاً سبد را با کالاهای همین فروشگاه تکمیل کنید یا همان کالا را از فروشگاه مرکزی سفارش بدهید.';
  }
  if (message.includes('Store catalog is unavailable')) {
    return 'کاتالوگ فروشگاه در حال حاضر در دسترس نیست؛ لطفاً چند لحظه دیگر دوباره تلاش کنید.';
  }
  if (message.includes('Address not found')) {
    return 'آدرس ذخیره‌شده انتخاب‌شده یافت نشد؛ لطفاً آدرس دیگری انتخاب کنید.';
  }
  if (message.includes('Discount code is invalid or expired')) {
    return 'کد تخفیف نامعتبر یا منقضی شده است.';
  }
  if (message.includes('Store not found')) {
    return 'فروشگاه انتخاب‌شده یافت نشد.';
  }
  if (message.includes('Missing required order information')) {
    return 'اطلاعات سفارش ناقص است؛ لطفاً اطلاعات گیرنده را کامل کنید.';
  }
  if (message.includes('Wallet payment is not available')) {
    return 'پرداخت با کیف پول در حال حاضر فعال نیست.';
  }
  if (message.includes('Database not initialized')) {
    return 'خطای سرور؛ لطفاً چند لحظه دیگر دوباره تلاش کنید.';
  }
  return message;
};

// Live parity (index-MERGED.js hS): a fresh uuid per submit.
const newIdempotencyKey = (): string =>
  typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `idem-${Date.now()}-${Math.random().toString(36).slice(2)}`;

export const OrderProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [orders, setOrders] = useState<Order[]>(() => isMockMode() ? initialOrders : []);
  const [currentOrder, setCurrentOrder] = useState<Order | null>(null);
  const [isCreatingOrder, setIsCreatingOrder] = useState<boolean>(false);
  const [trackingOrder, setTrackingOrder] = useState<Order | null>(null);
  const [isTrackingModalOpen, setIsTrackingModalOpen] = useState(false);

  const fetchMyOrders = async () => {
    if (isMockMode()) return;
    try {
      const data = await orderApi.getCustomerOrders();
      setOrders((data || []).map(mapBackendOrder));
    } catch (err: any) {
      console.warn('Failed to fetch orders:', err.message);
    }
  };

  const placeOrder = async (params: CreateOrderParams): Promise<Order> => {
    const { customerInfo, paymentMethod, cartItems, currentStore, customerPhone, referredStoreSlug, stores, discountCode, shippingQuoteId, addressId, saveAddress } = params;
    // ISO-B store attribution: the order belongs to the store the cart
    // lines were added under, NOT to whichever store the session
    // happens to point at when checkout is pressed (StoreDropdown
    // switches, a restored localStorage cart, or loadStores defaulting
    // to stores[0] can all move currentStore after the lines were
    // added). A mixed or storeless cart falls back to currentStore.
    // A missing slug NEVER falls back to 'central' (see the
    // fail-closed guard in the backend branch below): central skips
    // the published-only check, so a fallback would silently bypass
    // validation and misattribute the order. Direct-buy
    // lines carry directBuy.store's id, so this resolves to the same
    // store the StoreContext wrapper already resolved.
    const firstCartStoreId = cartItems[0]?.storeId;
    const cartStoreId =
      cartItems.length > 0 && cartItems.every((item) => item.storeId === firstCartStoreId)
        ? firstCartStoreId
        : undefined;
    const orderStore = (cartStoreId ? stores?.find((s) => s.id === cartStoreId) : undefined) ?? currentStore;
    setIsCreatingOrder(true);
    try {
      if (!isMockMode()) {
        // Staged cart-checkout address (CartCheckoutForm/addressApi),
        // consumed once. An explicit addressId (direct-buy modal) wins
        // outright, including over a stale staged saveAddress: the
        // modal's address was already saved by the modal itself, so
        // re-saving the order address would duplicate it.
        // Settled model (user decision): showcase lines are published
        // BY CONSTRUCTION once ISO-A's display fix lands, and the
        // published-only validation must stay. Fail closed on a
        // missing slug instead of falling back to 'central': central
        // skips the published-only check, so a slug-less order (e.g.
        // a direct-buy store without a slug, or an unresolvable cart
        // store) would silently bypass validation and misattribute
        // the order. The backend's own unknown-slug → central
        // fallback (order_handlers.go:214-216) remains a backend-side
        // hazard: the frontend can no longer trigger it with an empty
        // slug, but a stale non-empty slug still could.
        const orderSlug = orderStore?.slug;
        if (!orderSlug) {
          throw new Error('فروشگاه سفارش مشخص نیست؛ لطفاً صفحه را بازخوانی کنید و دوباره تلاش کنید.');
        }
        const stagedAddress = takeStagedAddressSelection();
        const effectiveAddressId = addressId ?? stagedAddress?.addressId;
        const effectiveSaveAddress = addressId ? saveAddress : (saveAddress ?? stagedAddress?.saveAddress);
        let backendOrder: BackendOrderDto;
        try {
          backendOrder = await orderApi.createOrder({
            storeId: orderSlug,
            customerInfo: {
              fullName: customerInfo.fullName,
              phone: customerPhone || customerInfo.phone || '09123456789',
              province: customerInfo.province,
              city: customerInfo.city,
              address: customerInfo.address,
              postalCode: customerInfo.postalCode,
            },
            paymentMethod,
            items: cartItems.map((item) => ({ productId: item.product.id, variantId: item.selectedVariantId, quantity: item.quantity, addonIds: item.selectedAddonIds })),
            discountCode,
            shippingQuoteId,
            addressId: effectiveAddressId,
            saveAddress: effectiveSaveAddress,
            idempotencyKey: newIdempotencyKey(),
          });
        } catch (err: any) {
          // Preserve the staged selection for a retry: the form only
          // re-stages on a field change, so a bare retry would
          // otherwise silently lose the saved-address choice.
          stageAddressSelection(stagedAddress);
          throw new Error(mapOrderCreateError(err?.message || ''));
        }

        // The order already exists at this point and its success
        // screen states payment is still pending ("به‌زودی"). A
        // token/callback failure must therefore NOT turn a created
        // order into a failure toast (the old code threw, the drawer
        // kept the cart, and a retry created a second pending order).
        // Live's rule is the same: a registered order is "awaiting
        // payment", never presented as paid and never hidden.
        try {
          const paymentToken = await paymentApi.getShaparakToken({
            orderId: backendOrder.id,
            orderNumber: backendOrder.orderNumber,
            amount: backendOrder.payableAmount,
            redirectUrl: `${window.location.origin}/payment/callback`,
          });

          if (import.meta.env.DEV || import.meta.env.VITE_USE_MOCK === 'true') {
            await paymentApi.processShaparakCallback({
              token: paymentToken.token,
              orderNumber: backendOrder.orderNumber,
            });
          }
        } catch (tokenErr: any) {
          console.warn('Payment token step failed; order remains pending payment:', tokenErr?.message || tokenErr);
        }

        const order: Order = {
          id: backendOrder.id,
          customerInfo: {
            fullName: backendOrder.customerFullName,
            phone: backendOrder.customerPhone,
            province: backendOrder.customerProvince,
            city: backendOrder.customerCity,
            address: backendOrder.customerAddress,
            postalCode: backendOrder.customerPostalCode,
          },
          orderNumber: backendOrder.orderNumber,
          items: [...cartItems],
          totalAmount: backendOrder.totalAmount,
          discountAmount: backendOrder.discountAmount,
          payableAmount: backendOrder.payableAmount,
          paymentMethod: 'cash', // FIX 14: backend coerces to cash; no installments exist.
          status: backendOrder.status,
          trackingCode: backendOrder.trackingCode,
          createdAt: backendOrder.createdAt,
          shippingCost: backendOrder.shippingCost,
          shippingQuoteId: backendOrder.shippingQuoteId,
          returnWindowEndsAt: backendOrder.returnWindowEndsAt,
          storeId: backendOrder.storeId,
          storeName: backendOrder.storeName,
          dropshipperProfit: backendOrder.dropshipperProfit,
          platformProfit: backendOrder.platformProfit,
          referralCommission: backendOrder.referralCommission,
          referralCommissionReleased: backendOrder.referralCommissionReleased,
          referrerSellerId: backendOrder.referrerSellerId,
          profitReleased: backendOrder.profitReleased,
        };

        setOrders((prev) => [order, ...prev]);
        setCurrentOrder(order);
        setTrackingOrder(order);
        return order;
      }

      const randomNum = Math.floor(100000 + Math.random() * 900000);
      const trackingCode = `TRK-${randomNum}`;
      const orderNumber = `ORD-${Date.now().toString().slice(-6)}`;

      let totalDropshipperProfit = 0;
      let totalPlatformProfit = 0;

      const referringStore =
        orderStore?.ownerType === 'central' && referredStoreSlug && stores
          ? stores.find((s) => s.slug === referredStoreSlug || s.id === referredStoreSlug)
          : null;

      cartItems.forEach((item) => {
        const baseCost = (item.product.supplierBaseCost || 0) * item.quantity;
        const platformFee = (item.product.platformMargin || 0) * item.quantity;
        const sellingPrice = item.product.price * item.quantity;

        if (orderStore?.ownerType === 'dropshipper') {
          const profit = Math.max(0, sellingPrice - (baseCost + platformFee));
          totalDropshipperProfit += profit;
          totalPlatformProfit += platformFee;
        } else if (referringStore) {
          const marginPercent = referringStore.profitMarginPercent || 10;
          const dropshipMargin = Math.round(baseCost * (marginPercent / 100));
          totalDropshipperProfit += dropshipMargin;
          totalPlatformProfit += Math.max(0, (sellingPrice - baseCost) - dropshipMargin);
        } else {
          totalPlatformProfit += sellingPrice - baseCost;
        }
      });

      const targetStoreId = cartItems[0]?.storeId || currentStore?.id || 'store-central';
      const targetStoreName =
        cartItems[0]?.storeName || (referringStore ? `${referringStore.name} (معرف)` : orderStore?.name || 'مرکزی');

      const cartTotal = cartItems.reduce((sum, item) => sum + (item.product.originalPrice || item.product.price) * item.quantity, 0);
      const cartPayable = cartItems.reduce((sum, item) => sum + item.product.price * item.quantity, 0);

      const newOrder: Order = {
        id: `ord-${Date.now()}`,
        orderNumber,
        customerInfo: {
          fullName: customerInfo.fullName || 'مشتری گرامی',
          phone: customerPhone || '09123456789',
          province: customerInfo.province || 'تهران',
          city: customerInfo.city || 'تهران',
          address: customerInfo.address || 'نشانی ثبت‌نشده',
          postalCode: customerInfo.postalCode || '۱۱۱۱۱۱۱۱۱۱',
        },
        items: [...cartItems],
        totalAmount: cartTotal,
        discountAmount: Math.max(0, cartTotal - cartPayable),
        payableAmount: cartPayable,
        paymentMethod,
        status: 'processing',
        trackingCode,
        createdAt: new Date().toLocaleDateString('fa-IR'),
        shippingCost: 45000,
        storeId: targetStoreId,
        storeName: targetStoreName,
        dropshipperProfit: totalDropshipperProfit,
        platformProfit: totalPlatformProfit,
        profitReleased: false,
      };

      setOrders((prev) => [newOrder, ...prev]);
      setCurrentOrder(newOrder);
      setTrackingOrder(newOrder);
      return newOrder;
    } finally {
      setIsCreatingOrder(false);
    }
  };

  const trackOrder = async (trackingCode: string): Promise<Order | null> => {
    const local = orders.find((o) => o.trackingCode === trackingCode || o.orderNumber === trackingCode);
    if (local) return local;
    if (!isMockMode()) {
      try {
        const backendOrder = await orderApi.trackOrder(trackingCode);
        return {
          id: backendOrder.id,
          customerInfo: {
            fullName: backendOrder.customerFullName,
            phone: backendOrder.customerPhone,
            province: backendOrder.customerProvince,
            city: backendOrder.customerCity,
            address: backendOrder.customerAddress,
            postalCode: backendOrder.customerPostalCode,
          },
          orderNumber: backendOrder.orderNumber,
          items: [],
          totalAmount: backendOrder.totalAmount,
          discountAmount: backendOrder.discountAmount,
          payableAmount: backendOrder.payableAmount,
          paymentMethod: 'cash', // FIX 14: backend coerces to cash; no installments exist.
          status: backendOrder.status,
          trackingCode: backendOrder.trackingCode,
          createdAt: backendOrder.createdAt,
          shippingCost: backendOrder.shippingCost,
          shippingQuoteId: backendOrder.shippingQuoteId,
          returnWindowEndsAt: backendOrder.returnWindowEndsAt,
          storeId: backendOrder.storeId,
          storeName: backendOrder.storeName,
          dropshipperProfit: backendOrder.dropshipperProfit,
          platformProfit: backendOrder.platformProfit,
          referralCommission: backendOrder.referralCommission,
          referralCommissionReleased: backendOrder.referralCommissionReleased,
          referrerSellerId: backendOrder.referrerSellerId,
          profitReleased: backendOrder.profitReleased,
        };
      } catch {
        return null;
      }
    }
    return null;
  };

  const confirmOrderDelivery = (orderId: string) => {
    setOrders((prev) =>
      prev.map((o) => (o.id === orderId ? { ...o, status: 'delivered', profitReleased: true } : o))
    );
  };

  const disputeOrderDelivery = (orderId: string, reason?: string) => {
    setOrders((prev) =>
      prev.map((o) => (o.id === orderId ? { ...o, status: 'delivery_disputed', disputeReason: reason } : o))
    );
  };

  const adminResolveDeliveryDispute = (
    orderId: string,
    action: 'release_to_seller' | 'refund_to_customer',
    responsibleParty?: string
  ) => {
    setOrders((prev) =>
      prev.map((o) => {
        if (o.id === orderId) {
          return {
            ...o,
            status: action === 'release_to_seller' ? 'delivered' : 'cancelled',
            profitReleased: action === 'release_to_seller',
            disputeResolvedAt: new Date().toLocaleDateString('fa-IR'),
            disputeResolutionAction: action,
            disputeResponsibleParty: responsibleParty,
          };
        }
        return o;
      })
    );
  };

  const getOrderById = (orderId: string) => orders.find((o) => o.id === orderId);

  const value: OrderContextType = {
    orders,
    currentOrder,
    isCreatingOrder,
    trackingOrder,
    setTrackingOrder,
    isTrackingModalOpen,
    setIsTrackingModalOpen,
    placeOrder,
    fetchMyOrders,
    trackOrder,
    confirmOrderDelivery,
    disputeOrderDelivery,
    adminResolveDeliveryDispute,
    getOrderById,
  };

  return <OrderContext.Provider value={value}>{children}</OrderContext.Provider>;
};

export const useOrders = (): OrderContextType => {
  const context = useContext(OrderContext);
  if (!context) {
    throw new Error('useOrders must be used within an OrderProvider');
  }
  return context;
};
