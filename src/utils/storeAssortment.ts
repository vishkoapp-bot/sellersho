import type { Store, Product } from '../types';

// ISO-A (model (a), user-decided): a dropshipper showcase shows and sells
// ONLY its published products (store_products / activeProductIds).
// The central store sells the full catalogue. This mirrors the server
// order rule exactly: order_handlers.go checks publication ONLY when the
// order's store OwnerType == "dropshipper", and the pricing engine only
// applies a dropshipper margin for OwnerType == "dropshipper".
export function isDropshipperStore(store: Store | null | undefined): boolean {
  return store?.ownerType === 'dropshipper';
}

// Single choke point for "is this product published by this store?".
// Central / unknown stores: everything is published (central catalogue).
// Dropshipper with a real array: strict membership. Dropshipper with no
// array (transient / malformed store object only — the public store API
// always emits an array): fail CLOSED, i.e. not published. This is a
// deliberate separation hardening: live's grid filter (`!ae ||
// ae.includes(id)`) falls back to the full catalogue when the list is
// undefined, which is only reachable for malformed store objects here
// (resolved stores always carry an array; suspended stores render the
// NotFound view, never a template).
export function isProductPublishedInStore(
  store: Store | null | undefined,
  productId: string,
): boolean {
  if (!isDropshipperStore(store)) return true;
  const ids = store?.activeProductIds;
  if (!Array.isArray(ids)) return false;
  return ids.includes(productId);
}

export function filterStoreProducts(
  store: Store | null | undefined,
  products: Product[],
): Product[] {
  if (!isDropshipperStore(store)) return products;
  return products.filter((p) => isProductPublishedInStore(store, p.id));
}
