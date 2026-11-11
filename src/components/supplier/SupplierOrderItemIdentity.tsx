import React from 'react';
import type { SupplierOrderItemDto } from '../../services/supplierApi';
import { toPersianDigits } from '../../utils/formatters';

export function SupplierOrderItemIdentity({ item, showQuantity = true }: { item: SupplierOrderItemDto; showQuantity?: boolean }) {
  return <div className="min-w-0 text-[11px] leading-6">
    <p className="font-bold text-slate-700 break-words">{item.productTitle}</p>
    {(item.variantId || item.variantName || item.variantSpnc) && <p className="text-slate-500 break-words">
      {item.variantName || 'تنوع خریداری‌شده'}{item.variantSpnc && <> · <span dir="ltr" className="font-mono">{item.variantSpnc}</span></>}
    </p>}
    {showQuantity && <p className="text-slate-500">تعداد: {toPersianDigits(item.quantity)}</p>}
  </div>;
}

export function SupplierOrderLines({ items }: { items?: SupplierOrderItemDto[] }) {
  if (!items?.length) return null;
  return <ul aria-label="کالاها و تنوع‌های خریداری‌شده" className="space-y-2 max-h-64 overflow-auto">
    {items.map((item, index) => <li key={item.orderItemId || `${item.productId}:${item.variantId || ''}:${index}`} className="bg-slate-50 rounded-xl p-2.5"><SupplierOrderItemIdentity item={item} /></li>)}
  </ul>;
}
