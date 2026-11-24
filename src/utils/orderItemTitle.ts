export function orderItemTitle(item: {
  product?: { title?: unknown } | null;
  productTitle?: unknown;
  product_title?: unknown;
}): string {
  // Persisted seller order lines carry productTitle, but local cart-shaped
  // orders still carry the full product and older payloads use snake_case.
  for (const candidate of [item.product?.title, item.productTitle, item.product_title]) {
    if (typeof candidate === 'string' && candidate.trim()) return candidate.trim();
  }
  return 'کالای سفارشی';
}
