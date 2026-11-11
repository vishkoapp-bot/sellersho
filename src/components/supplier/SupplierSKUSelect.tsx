import React, { useEffect, useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import type { SupplierProductDto, SupplierProductVariantDto } from '../../services/supplierApi';
import { skuReviewLabel } from '../../utils/productReview';

export function useSupplierVariants(product: SupplierProductDto | null) {
  const [retry, setRetry] = useState(0);
  const [state, setState] = useState<{ productId: string; rows: SupplierProductVariantDto[]; loading: boolean; error: string }>({ productId: '', rows: [], loading: false, error: '' });
  useEffect(() => {
    if (!product || product.schemaVersion !== 2) return;
    let cancelled = false;
    setState({ productId: product.id, rows: [], loading: true, error: '' });
    supplierApi.getVariants(product.id).then(rows => {
      if (!cancelled) setState({ productId: product.id, rows, loading: false, error: '' });
    }).catch(cause => {
      if (!cancelled) setState({ productId: product.id, rows: [], loading: false, error: cause instanceof Error ? cause.message : 'دریافت تنوع‌ها ناموفق بود.' });
    });
    return () => { cancelled = true; };
  }, [product?.id, product?.schemaVersion, retry]);
  const current = state.productId === product?.id ? state : { rows: [], loading: product?.schemaVersion === 2, error: '' };
  return { ...current, reload: () => setRetry(attempt => attempt + 1) };
}

interface Props {
  product: SupplierProductDto;
  rows: SupplierProductVariantDto[];
  selectedId: string;
  onSelect: (id: string) => void;
  label: string;
  loading?: boolean;
  error?: string;
  retry?: () => void;
  disabled?: boolean;
  available?: (id: string) => number;
}
export function SupplierSKUSelect({ product, rows, selectedId, onSelect, label, loading, error, retry, disabled, available }: Props) {
  return <div className="w-full space-y-2">
    <label className="block text-[11px] font-bold text-slate-700">{label}
      <select aria-label={label} required value={selectedId} disabled={disabled || loading || !!error}
        onChange={event => onSelect(event.target.value)} className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs bg-white">
        <option value="">تنوع دقیق کالا را انتخاب کنید</option>
        {rows.map(row => <option key={row.id} value={row.id} disabled={available ? available(row.id) <= 0 : false}>
          {skuReviewLabel(product, row)}{row.spnc ? ` · ${row.spnc}` : ''}{row.is_active === false ? ' (غیرفعال)' : ''}{available ? ` — ${available(row.id).toLocaleString('fa-IR')} عدد آماده برگشت` : ''}
        </option>)}
      </select>
    </label>
    {loading && <p role="status" className="text-xs text-slate-500">در حال دریافت تنوع‌ها…</p>}
    {error && <p role="alert" className="text-xs text-rose-700">{error} <button type="button" onClick={retry} className="underline">تلاش دوباره</button></p>}
    {!loading && !error && rows.length === 0 && <p className="text-xs text-amber-800">تنوع قابل انتخابی دریافت نشد.</p>}
  </div>;
}
