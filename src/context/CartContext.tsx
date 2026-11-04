import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { Product, CartItem } from '../types';
import { cartItemKey, cartKeyOf } from '../utils/cartKey';


export interface CartContextType {
  cart: CartItem[];
  cartTotal: number;
  cartDiscount: number;
  cartPayable: number;
  cartItemsCount: number;
  itemCount: number;
  isCartOpen: boolean;
  setIsCartOpen: (open: boolean) => void;
  setActiveCartStore: (storeId: string) => void;
  addToCart: (product: Product, quantity?: number, color?: string, currentStore?: { id: string; name: string }, variantId?: string, addonIds?: string[]) => void;
  removeFromCart: (key: string) => void;
  updateQuantity: (key: string, quantity: number) => void;
  clearCart: () => void;
  getCartItem: (key: string) => CartItem | undefined;
  calculateTotal: () => number;
}

const CartContext = createContext<CartContextType | undefined>(undefined);

export const CartProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  // Fix72 (live parity, index-MERGED.js cart provider): carts are a
  // RECORD keyed by store id, not one global array. The visible cart
  // is carts[activeStoreId]; switching stores switches the cart,
  // other stores' carts are preserved (never merged, never cleared).
  // Persistence: 'drup_carts' (map). A legacy 'drup_cart' array is
  // migrated once, grouped by item.storeId || 'store-central', and
  // the legacy key is removed — exactly like live's loader.
  const [carts, setCarts] = useState<Record<string, CartItem[]>>(() => {
    if (typeof window === 'undefined') return {};
    try {
      const savedMap = localStorage.getItem('drup_carts');
      if (savedMap) {
        const parsed = JSON.parse(savedMap);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed;
      }
    } catch {}
    try {
      const legacy = localStorage.getItem('drup_cart');
      if (legacy) {
        const items = JSON.parse(legacy);
        if (Array.isArray(items)) {
          const grouped: Record<string, CartItem[]> = {};
          for (const item of items) {
            const sid = (item as CartItem).storeId || 'store-central';
            (grouped[sid] || (grouped[sid] = [])).push(item as CartItem);
          }
          localStorage.removeItem('drup_cart');
          return grouped;
        }
      }
    } catch {}
    return {};
  });
  const [activeStoreId, setActiveStoreId] = useState<string>('store-central');
  const cart: CartItem[] = carts[activeStoreId] ?? [];

  const [isCartOpen, setIsCartOpen] = useState(false);

  // Sync the per-store map to localStorage (live: setItem('drup_carts')).
  useEffect(() => {
    if (typeof window !== 'undefined') {
      try { localStorage.setItem('drup_carts', JSON.stringify(carts)); } catch {}
    }
  }, [carts]);

  const setActiveCartStore = (storeId: string) => {
    if (storeId) setActiveStoreId(storeId);
  };

  const addToCart = (
    product: Product,
    quantity = 1,
    color?: string,
    currentStore?: { id: string; name: string },
    variantId?: string,
    addonIds?: string[]
  ) => {
    if ((product.stock || 0) <= 0) return;

    // Live parity: the item joins its own store's cart, and that
    // store becomes the visible cart. No conflict modal, no replace:
    // other stores' carts are untouched.
    const storeId = currentStore?.id || activeStoreId || 'store-central';
    const storeName = currentStore?.name || 'فروشگاه مرکزی';

    // FIX 5: key by product::variant, so two variants of one
    // product are two lines instead of merging into one.
    // FIX 14b: live's key adds the sorted addon-ids suffix.
    const key = cartKeyOf(product.id, variantId, addonIds);
    setActiveStoreId(storeId);
    setCarts((prev) => {
      const storeCart = prev[storeId] ?? [];
      const existing = storeCart.find((item) => cartItemKey(item) === key);
      const nextStoreCart = existing
        ? storeCart.map((item) =>
            cartItemKey(item) === key
              ? { ...item, quantity: item.quantity + quantity }
              : item
          )
        : [
            ...storeCart,
            {
              product,
              quantity,
              selectedColor: color,
              selectedVariantId: variantId,
              selectedAddonIds: addonIds,
              storeId,
              storeName,
            },
          ];
      return { ...prev, [storeId]: nextStoreCart };
    });
    setIsCartOpen(true);
  };

  // Callers pass a cart key (product::variant); a bare product id
  // still matches the first line of that product, for legacy callers.
  const matchesKey = (item: CartItem, key: string) =>
    cartItemKey(item) === key || item.product.id === key;

  const removeFromCart = (key: string) => {
    setCarts((prev) => ({
      ...prev,
      [activeStoreId]: (prev[activeStoreId] ?? []).filter((item) => !matchesKey(item, key)),
    }));
  };

  const updateQuantity = (key: string, quantity: number) => {
    if (quantity <= 0) {
      removeFromCart(key);
      return;
    }
    setCarts((prev) => ({
      ...prev,
      [activeStoreId]: (prev[activeStoreId] ?? []).map((item) =>
        matchesKey(item, key) ? { ...item, quantity } : item
      ),
    }));
  };

  const clearCart = () => {
    // Fix72: clears ONLY the visible (active) store's cart; other
    // stores' carts are preserved.
    setCarts((prev) => ({ ...prev, [activeStoreId]: [] }));
  };

  const getCartItem = (key: string) => cart.find((item) => matchesKey(item, key));

  const cartTotal = cart.reduce((sum, item) => sum + (item.product.originalPrice || item.product.price) * item.quantity, 0);
  // FIX 14b: live's cart fee helper sums product.addons feePerUnit over
  // the line's selected addon ids; the payable must include them, or
  // the drawer total would understate what the backend charges.
  const cartAddonTotal = (item: CartItem): number => {
    const ids = item.selectedAddonIds;
    if (!ids || ids.length === 0 || !item.product.addons) return 0;
    const byId = new Map(item.product.addons.map((a) => [a.id, a.feePerUnit || 0]));
    return ids.reduce((sum, id) => sum + (byId.get(id) ?? 0), 0);
  };
  const cartPayable = cart.reduce((sum, item) => sum + (item.product.price + cartAddonTotal(item)) * item.quantity, 0);
  const cartDiscount = Math.max(0, cartTotal - cartPayable);
  const cartItemsCount = cart.reduce((sum, item) => sum + item.quantity, 0);

  const calculateTotal = () => cartPayable;

  const value: CartContextType = {
    cart,
    cartTotal,
    cartDiscount,
    cartPayable,
    cartItemsCount,
    itemCount: cartItemsCount,
    isCartOpen,
    setIsCartOpen,
    setActiveCartStore,
    addToCart,
    removeFromCart,
    updateQuantity,
    clearCart,
    getCartItem,
    calculateTotal,
  };

  return <CartContext.Provider value={value}>{children}</CartContext.Provider>;
};

export const useCart = (): CartContextType => {
  const context = useContext(CartContext);
  if (!context) {
    throw new Error('useCart must be used within a CartProvider');
  }
  return context;
};
