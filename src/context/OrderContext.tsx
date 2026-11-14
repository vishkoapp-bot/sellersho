import React, { createContext, useContext, useState, useEffect, useLayoutEffect, useRef, ReactNode } from 'react';
import { Order, CartItem, Store, Product } from '../types';
import { BackendOrderDto, DisputeDeliveryResponse, orderApi } from '../services/orderApi';
import { stageAddressSelection, takeStagedAddressSelection } from '../services/addressApi';
import { paymentApi } from '../services/paymentApi';
import { isMockMode, tokenStore } from '../services/apiClient';
import { useAuth } from './AuthContext';

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
  fetchMyOrders: () => Promise<Order[]>; // returns the fresh list so callers can check post-refetch state
  trackOrder: (trackingCode: string) => Promise<Order | null>;
  confirmOrderDelivery: (orderId: string) => Promise<void>;
  disputeOrderDelivery: (orderId: string, reason?: string) => Promise<DisputeDeliveryResponse | null>;
  busyOrderId: string | null;
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
    return 'این کالا در این فروشگاه عرضه نمی‌شود؛ لطفاً آن را از سبد حذف کنید یا کالاهای همین فروشگاه را انتخاب کنید';
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
  // P1-3: shipping-quote failures from the backend gate/recompute.
  // shipping_quote_required is the new fail-closed gate (no quoteId);
  // the other two already existed at the quote endpoint.
  if (message.includes('shipping_quote_required')) {
    return 'هزینه ارسال محاسبه نشده است؛ لطفاً هزینه ارسال را محاسبه کنید و دوباره تلاش کنید.';
  }
  if (message.includes('shipping_quote_changed')) {
    return 'هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی و دوباره تلاش کنید.';
  }
  if (message.includes('shipping_unavailable')) {
    return 'قیمت ارسال در دسترس نیست؛ لطفاً چند لحظه دیگر دوباره تلاش کنید.';
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
  const { currentUser, authToken, isSessionReady } = useAuth();
  // Private order, tracking, checkout/address and panel state below this boundary
  // belongs to one verified immutable account/tenant. A new key discards it before
  // children render. CartProvider is above this boundary: guest product carts are
  // deliberately retained, never order history, contacts or saved addresses.
  const scope = JSON.stringify([authToken ? currentUser?.id ?? null : null, currentUser?.storeId ?? null, isSessionReady]);
  return <OrderSessionProvider key={scope}>{children}</OrderSessionProvider>;
};

const OrderSessionProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { currentUser, authToken, isSessionReady } = useAuth();
  const mounted = useRef(true);
  const captureSession = () => {
    const generation = tokenStore.getGeneration();
    const credential = authToken;
    return () => mounted.current && generation === tokenStore.getGeneration() && credential === tokenStore.getToken();
  };
  useLayoutEffect(() => {
    mounted.current = true;
    stageAddressSelection(null);
    localStorage.removeItem('drup_orders'); // discard unused legacy unscoped history
    return () => { mounted.current = false; stageAddressSelection(null); };
  }, []);
  // Demo data has no immutable customer IDs. It is never an ownership source.
  const [orders, setOrders] = useState<Order[]>([]);
  const [currentOrder, setCurrentOrder] = useState<Order | null>(null);
  const [isCreatingOrder, setIsCreatingOrder] = useState<boolean>(false);
  const [trackingOrder, setTrackingOrder] = useState<Order | null>(null);
  const [isTrackingModalOpen, setIsTrackingModalOpen] = useState(false);
  const [busyOrderId, setBusyOrderId] = useState<string | null>(null);
  const creatingOperations = useRef(new Set<symbol>());
  const deliveryOperations = useRef(new Map<symbol, string>());
  const beginCreation = () => {
    const operation = Symbol();
    creatingOperations.current.add(operation);
    setIsCreatingOrder(true);
    return () => {
      creatingOperations.current.delete(operation);
      // Busy state belongs to mounted operations, not credential generations.
      // A rejected replacement login invalidates data responses but keeps this
      // account mounted; every settled operation must still release its own flag.
      if (mounted.current) setIsCreatingOrder(creatingOperations.current.size > 0);
    };
  };
  const beginDelivery = (orderId: string) => {
    const operation = Symbol();
    deliveryOperations.current.set(operation, orderId);
    setBusyOrderId(orderId);
    return () => {
      deliveryOperations.current.delete(operation);
      if (mounted.current) setBusyOrderId([...deliveryOperations.current.values()].at(-1) ?? null);
    };
  };
  const ordersRef = useRef<Order[]>(orders);
  useEffect(() => { ordersRef.current = orders; }, [orders]);

  const fetchMyOrders = async (): Promise<Order[]> => {
    const isCurrent = captureSession();
    if (!isCurrent() || !isSessionReady || !currentUser?.id || !authToken) return [];
    if (isMockMode()) return ordersRef.current;
    try {
      const data = await orderApi.getCustomerOrders();
      if (!isCurrent()) return [];
      const mapped = (data || []).map(mapBackendOrder);
      setOrders(mapped);
      return mapped;
    } catch (err: any) {
      if (!isCurrent()) return [];
      console.warn('Failed to fetch orders:', err.message);
      return ordersRef.current;
    }
  };

  const placeOrder = async (params: CreateOrderParams): Promise<Order> => {
    const isCurrent = captureSession();
    const requireCurrent = () => { if (!isCurrent()) throw new Error('Session changed during order request'); };
    requireCurrent();
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
    const finishCreation = beginCreation();
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
          requireCurrent();
          stageAddressSelection(stagedAddress);
          throw new Error(mapOrderCreateError(err?.message || ''));
        }
        requireCurrent();

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
          requireCurrent();

          if (import.meta.env.DEV || import.meta.env.VITE_USE_MOCK === 'true') {
            await paymentApi.processShaparakCallback({
              token: paymentToken.token,
              orderNumber: backendOrder.orderNumber,
            });
          }
        } catch (tokenErr: any) {
          console.warn('Payment token step failed; order remains pending payment:', tokenErr?.message || tokenErr);
        }
        requireCurrent();

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
      finishCreation();
    }
  };

  const trackOrder = async (trackingCode: string): Promise<Order | null> => {
    const isCurrent = captureSession();
    if (!isCurrent()) return null;
    const local = orders.find((o) => o.trackingCode === trackingCode || o.orderNumber === trackingCode);
    if (local) return local;
    if (!isMockMode()) {
      try {
        const backendOrder = await orderApi.trackOrder(trackingCode);
        if (!isCurrent()) return null;
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

  // P0-1: confirm / dispute hit the real backend; the server is the
  // source of truth. Confirm does NOT release profit — it only starts
  // the 7-day return window (profit releases when the window closes).
  // Local state is therefore never set to legacy 'delivered' and
  // profitReleased is never set true here; the refetch carries the
  // server status (delivered_confirmed) and timestamps.
  const confirmOrderDelivery = async (orderId: string) => {
    const isCurrent = captureSession();
    if (!isCurrent()) return;
    const order = ordersRef.current.find((o) => o.id === orderId || o.orderNumber === orderId);
    if (!order || [...deliveryOperations.current.values()].includes(orderId)) return;
    if (isMockMode()) {
      // Demo mode has no backend: mirror the server transition locally.
      setOrders((prev) => prev.map((o) => (o.id === order.id ? { ...o, status: 'delivered_confirmed' } : o)));
      return;
    }
    const finishDelivery = beginDelivery(orderId);
    try {
      await orderApi.confirmDelivery(order.orderNumber || order.id);
      if (!isCurrent()) return;
      await fetchMyOrders();
    } catch (err: any) {
      if (!isCurrent()) return;
      // Idempotent double-submit: the backend 404s a second confirm.
      // Refetch; if the order is now delivered_confirmed, the first
      // call already won and the 404 is swallowed as already-confirmed.
      const fresh = await fetchMyOrders();
      if (fresh.find((o) => o.id === order.id)?.status === 'delivered_confirmed') return;
      throw err; // call site shows the error toast; local state untouched
    } finally {
      finishDelivery();
    }
  };

  const disputeOrderDelivery = async (orderId: string, reason?: string): Promise<DisputeDeliveryResponse | null> => {
    const isCurrent = captureSession();
    if (!isCurrent()) return null;
    const order = ordersRef.current.find((o) => o.id === orderId || o.orderNumber === orderId);
    if (!order || [...deliveryOperations.current.values()].includes(orderId)) return null;
    const disputeReason = (reason ?? '').trim() || 'خریدار اعلام کرد بسته پستی هنوز به دست ایشان نرسیده است.';
    if (isMockMode()) {
      // Demo mode has no backend: mirror the server transition locally.
      // No dispute id exists in mock mode, so the toast prints none.
      setOrders((prev) => prev.map((o) => (o.id === order.id ? { ...o, status: 'delivery_disputed', disputeReason } : o)));
      return null;
    }
    const finishDelivery = beginDelivery(orderId);
    try {
      const res = await orderApi.disputeDelivery(order.orderNumber || order.id, {
        type: 'delivery_undelivered',
        claimAmount: Math.round(order.payableAmount),
        reason: disputeReason,
        ...(order.items?.[0]?.product?.title ? { productTitle: order.items[0].product.title } : {}),
      });
      if (!isCurrent()) return null;
      await fetchMyOrders();
      if (!isCurrent()) return null;
      return res;
    } finally {
      finishDelivery();
    }
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
    busyOrderId,
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
