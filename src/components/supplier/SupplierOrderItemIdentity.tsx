import React, { useCallback, useEffect, useState } from 'react';
import type { SupplierOrderItemDto } from '../../services/supplierApi';
import { toPersianDigits } from '../../utils/formatters';
import { ApiError } from '../../services/apiClient';
import { extractSpncxErrorCode } from '../../services/spncxAdminApi';
import { unitsApi, type ListUnitsParams, type ListUnitsResponse } from '../../services/unitsApi';

export function loadOrderItemUnits(
  listUnits: (params: ListUnitsParams) => Promise<ListUnitsResponse>,
  orderItemId: string,
  cursor?: string,
): Promise<ListUnitsResponse> {
  return listUnits({ order_item_id: orderItemId, limit: 200, ...(cursor ? { cursor } : {}) });
}

interface OrderUnitCodesListProps {
  codes: string[];
  nextCursor?: string | null;
  expanded?: boolean;
  loadingMore?: boolean;
  error?: string;
  onToggle?: () => void;
  onLoadMore?: () => void;
}

export function OrderUnitCodesList({
  codes, nextCursor, expanded, loadingMore, error, onToggle, onLoadMore,
}: OrderUnitCodesListProps) {
  if (!codes.length && nextCursor == null && !error) return null;
  const collapsible = codes.length > 3;
  const count = `${toPersianDigits(codes.length)}${nextCursor != null ? '+' : ''} واحد`;
  return (
    <div className="text-[10px] text-slate-600">
      {collapsible ? (
        <button type="button" aria-expanded={!!expanded} onClick={onToggle} className="font-bold text-brand-deep underline">
          {`${count}: ${expanded ? 'پنهان کردن کدها' : 'نمایش کدها'}`}
        </button>
      ) : nextCursor != null && <p>{count}</p>}
      {(!collapsible || expanded) && (
        <>
          {codes.length > 0 && <ul className="flex flex-wrap gap-x-2">
            {codes.map((code, index) => <li key={index} dir="ltr" className="font-mono break-all">{code}</li>)}
          </ul>}
          {nextCursor != null && <button type="button" disabled={loadingMore} onClick={onLoadMore} className="font-bold text-brand-deep underline">
            {loadingMore ? 'در حال دریافت…' : 'نمایش بیشتر'}
          </button>}
        </>
      )}
      {error && <p role="alert" className="text-rose-700">{error}</p>}
    </div>
  );
}

function CapturedOrderItemUnits({ orderItemId }: { orderItemId: string }) {
  const [page, setPage] = useState<ListUnitsResponse>();
  const [expanded, setExpanded] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');
  const fetchPage = useCallback(async (cursor?: string, isActive: () => boolean = () => true) => {
    setError('');
    if (cursor) setLoadingMore(true);
    try {
      const result = await loadOrderItemUnits(unitsApi.listUnits, orderItemId, cursor);
      if (isActive()) setPage(previous => cursor && previous ? { ...result, units: [...previous.units, ...result.units] } : result);
    } catch (cause) {
      if (!isActive()) return;
      if (cause instanceof ApiError && (cause.status === 404 || cause.status === 501)) return;
      const code = extractSpncxErrorCode(cause);
      setError(code ? `دریافت کدهای واحد انجام نشد. (${code})` : 'دریافت کدهای واحد انجام نشد.');
    } finally {
      if (cursor && isActive()) setLoadingMore(false);
    }
  }, [orderItemId]);

  useEffect(() => {
    let active = true;
    void fetchPage(undefined, () => active);
    return () => { active = false; };
  }, [fetchPage]);

  return <OrderUnitCodesList codes={page?.units.map(unit => unit.code) ?? []} nextCursor={page?.next_cursor}
    expanded={expanded} loadingMore={loadingMore} error={error} onToggle={() => setExpanded(value => !value)}
    onLoadMore={() => {
      if (!loadingMore && page?.next_cursor != null) {
        setExpanded(true);
        void fetchPage(page.next_cursor);
      }
    }} />;
}

export function SupplierOrderItemIdentity({ item, showQuantity = true }: { item: SupplierOrderItemDto; showQuantity?: boolean }) {
  return <div className="min-w-0 text-[11px] leading-6">
    <p className="font-bold text-slate-700 break-words">{item.productTitle}</p>
    {(item.variantId || item.variantName || item.variantSpnc) && <p className="text-slate-500 break-words">
      {item.variantName || 'تنوع خریداری‌شده'}{item.variantSpnc && <> · <span dir="ltr" className="font-mono">{item.variantSpnc}</span></>}
    </p>}
    {showQuantity && <p className="text-slate-500">تعداد: {toPersianDigits(item.quantity)}</p>}
    {item.orderItemId && <CapturedOrderItemUnits key={item.orderItemId} orderItemId={item.orderItemId} />}
  </div>;
}

export function SupplierOrderLines({ items }: { items?: SupplierOrderItemDto[] }) {
  if (!items?.length) return null;
  return <ul aria-label="کالاها و تنوع‌های خریداری‌شده" className="space-y-2 max-h-64 overflow-auto">
    {items.map((item, index) => <li key={item.orderItemId || `${item.productId}:${item.variantId || ''}:${index}`} className="bg-slate-50 rounded-xl p-2.5"><SupplierOrderItemIdentity item={item} /></li>)}
  </ul>;
}
