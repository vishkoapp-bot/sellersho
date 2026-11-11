type Location = { productId: string; variantId?: string; ownerType: string; status: string; quantity: number };
export const supplierReturnQuantity = (locations: Location[], productId: string, variantId?: string, requiresSKU = false): number => {
  if (requiresSKU && !variantId) return 0;
  return locations.filter(location => location.productId === productId && location.ownerType === 'supplier'
    && location.status === 'sellable' && (requiresSKU ? location.variantId === variantId : !location.variantId))
    .reduce((sum, location) => sum + Math.max(0, location.quantity), 0);
};
export const supplierReturnAttemptScope = (productId: string, variantId: string | undefined, quantity: number, method: string, reason: string): string =>
  JSON.stringify([productId, variantId ?? '', quantity, method, reason.trim()]);
export const supplierInventoryLabel = (item: { variantId?: string; variantName?: string; variantSpnc?: string }): string =>
  item.variantId ? [item.variantName || 'تنوع مشخص‌شده', item.variantSpnc].filter(Boolean).join(' · ') : 'موجودی قدیمی بدون تخصیص به تنوع';
