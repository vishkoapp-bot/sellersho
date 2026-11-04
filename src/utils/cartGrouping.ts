import { CartItem } from '../types';

export interface CartSupplierGroup {
  key: string;
  supplierId: string;
  supplierName: string;
  items: CartItem[];
  itemCount: number;
  subtotal: number;
  originalSubtotal: number;
  discount: number;
}

export const getCartSupplierGroups = (cart: CartItem[]): CartSupplierGroup[] => {
  const groups = new Map<string, CartSupplierGroup>();

  cart.forEach((item) => {
    const supplierId = item.product.supplierId || item.product.supplierName || 'unknown-supplier';
    const supplierName = item.product.supplierName || 'تأمین‌کننده نامشخص';
    const existing = groups.get(supplierId);
    const itemSubtotal = item.product.price * item.quantity;
    const itemOriginalSubtotal = (item.product.originalPrice || item.product.price) * item.quantity;

    if (existing) {
      existing.items.push(item);
      existing.itemCount += item.quantity;
      existing.subtotal += itemSubtotal;
      existing.originalSubtotal += itemOriginalSubtotal;
      existing.discount += itemOriginalSubtotal - itemSubtotal;
      return;
    }

    groups.set(supplierId, {
      key: supplierId,
      supplierId,
      supplierName,
      items: [item],
      itemCount: item.quantity,
      subtotal: itemSubtotal,
      originalSubtotal: itemOriginalSubtotal,
      discount: itemOriginalSubtotal - itemSubtotal,
    });
  });

  return Array.from(groups.values());
};
