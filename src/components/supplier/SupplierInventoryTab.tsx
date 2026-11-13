import React, { useEffect, useState } from 'react';
import { useToast } from '../../context/ToastContext';
import { supplierApi, SupplierProductDto, SupplierReturnRequestDto, SupplierInventoryLocationDto, SupplierProductVariantDto } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { SupplierSKUSelect, useSupplierVariants } from './SupplierSKUSelect';
import { supplierInventoryLabel, supplierReturnAttemptScope, supplierReturnQuantity } from '../../utils/supplierSkuInventory';
import { formatPrice, toPersianDigits } from '../../utils/formatters';

// FIX 42: rebuilt to the live bundle SupplierDashboardView-MERGED.js (Zl component).
// Divergences fixed vs the previous candidate tab: wrong h2 (it even said
// "canonical"), missing help box, missing central stock-share box, wrong
// search placeholder, missing empty states, wrong grid labels (live:
// آماده فروش / کنار گذاشته برای سفارش / درخواست برگشت / در حال بررسی
// کیفیت / در راه انبار / هنوز شمرده نشده), raw English statuses in location
// lines (live maps them through Xl) and wrong unreconciled note, a dead
// "adjustment legacy" button (it set state but rendered no form at all),
// wrong returns section (live: «برگرداندن‌هایی که خواسته‌ای» with Persian
// quantities and Yl status labels, no id), and a wrong return modal
// (live: «برگرداندن کالا: …» with labeled fields, «خودم می‌آیم تحویل
// می‌گیرم» / «با پست برمی‌گردد» options, and its own validation/toast
// strings). The stock-share box renders only on a successful
// /supplier/stock-share response, exactly like live's conditional box;
// the candidate backend still returns the U07 stub error for that route,
// so the box stays hidden until a real stock-share source exists —
// matching live's error branch, not a UI divergence.
// NOTE: live's number inputs use its __sepF/__sepW Persian formatting;
// the small helpers below reproduce it (Persian digits + ٬ separators).
//
// REDESIGN (Nov 13, 2026, ITEM 4): the tab was a flat list of grey
// 10px spans — a zero-sellable product looked identical to a healthy one,
// the edit form pushed the list down, search matched titles only, and the
// returns summary sat below the whole list. The redesign keeps every
// API call, payload, toast string and validation message, and only
// restructures the presentation: a status pill + hero sellable figure per
// card, per-variant stock rows from the already-loaded locations, the edit
// form moved into the same fixed-modal pattern as the return form,
// sticky search + status filter chips with counts, and the returns summary
// above the list. Decisions that tests must pin live in pure exported
// helpers below; the presentational SupplierInventoryView takes all data
// as props so it renders under renderToStaticMarkup (the container's
// useToast/useSupplierResource hooks cannot run in that harness).
const STATUS_LABELS: Record<string, string> = {
  sellable: 'آماده فروش',
  sales_reserved: 'کنار گذاشته برای سفارش',
  return_requested: 'درخواست برگشت',
  quarantine: 'در حال بررسی کیفیت',
  in_transit: 'در راه انبار',
};

const RETURN_STATUS_LABELS: Record<string, string> = {
  requested: 'ثبت شده',
  pending: 'ثبت شده',
  approved: 'قبول شد',
  rejected: 'رد شد',
  completed: 'انجام شد',
};

// --- Pure helpers (exported for SupplierInventoryTab.test.tsx) -------------

// No platform-wide low-stock threshold exists in code (SupplierOverviewTab
// only sorts products by stock), so the tab defines its own: sellable <= 5
// is "low". This is a display threshold only — nothing is sent to the server.
export const LOW_STOCK_THRESHOLD = 5;
export type StockStatus = 'in' | 'low' | 'out';
export const STOCK_STATUS_LABEL: Record<StockStatus, string> = { in: 'موجود', low: 'کم', out: 'ناموجود' };
export const STOCK_STATUS_PILL: Record<StockStatus, string> = {
  in: 'bg-emerald-50 text-emerald-700 border-emerald-200',
  low: 'bg-amber-50 text-amber-700 border-amber-200',
  out: 'bg-rose-50 text-rose-700 border-rose-200',
};
export const stockStatus = (sellable: number): StockStatus =>
  sellable <= 0 ? 'out' : sellable <= LOW_STOCK_THRESHOLD ? 'low' : 'in';

// Sellable total for a product — the same sum the old six-span grid showed
// (all owners, status === 'sellable'), so the hero figure and the pill
// never disagree with the breakdown underneath.
export const sellableOf = (locations: SupplierInventoryLocationDto[], productId: string): number =>
  locations
    .filter(location => location.productId === productId && location.status === 'sellable')
    .reduce((sum, location) => sum + Math.max(0, location.quantity), 0);

export interface VariantSellableRow { variantId: string; label: string; sellable: number; }

// Per-variant sellable rows, derived from the already-loaded inventory
// locations (variantName/variantSpnc ride on the location rows) — no extra
// fetch. Variants with no warehouse location yet simply have no row; their
// stock is still editable through the edit form's variant picker.
export const variantSellableRows = (locations: SupplierInventoryLocationDto[], productId: string): VariantSellableRow[] => {
  const byVariant = new Map<string, VariantSellableRow>();
  for (const location of locations) {
    if (location.productId !== productId || !location.variantId) continue;
    const row = byVariant.get(location.variantId) || { variantId: location.variantId, label: supplierInventoryLabel(location), sellable: 0 };
    if (location.status === 'sellable') row.sellable += Math.max(0, location.quantity);
    byVariant.set(location.variantId, row);
  }
  return [...byVariant.values()];
};

export type InventoryStatusFilter = 'all' | StockStatus;

// Search matches title, product id (SKU), category, and variant labels —
// variant names/SPNCs from the locations, plus any variant rows already
// loaded for the edit/return forms (passed in by the container).
export const filterInventoryItems = (
  items: SupplierProductDto[],
  query: string,
  status: InventoryStatusFilter,
  locations: SupplierInventoryLocationDto[],
  variantLabelsByProduct: Record<string, string[]> = {},
): SupplierProductDto[] => {
  const q = query.trim().toLowerCase();
  return items.filter(item => {
    if (status !== 'all' && stockStatus(sellableOf(locations, item.id)) !== status) return false;
    if (!q) return true;
    const haystacks = [
      item.title, item.id, item.slug, item.categorySlug,
      ...(variantLabelsByProduct[item.id] || []),
      ...locations.filter(location => location.productId === item.id)
        .flatMap(location => [location.variantName || '', location.variantSpnc || '']),
    ];
    return haystacks.some(text => (text || '').toLowerCase().includes(q));
  });
};

export const countByStockStatus = (
  items: SupplierProductDto[],
  locations: SupplierInventoryLocationDto[],
): Record<InventoryStatusFilter, number> => {
  const counts: Record<InventoryStatusFilter, number> = { all: items.length, in: 0, low: 0, out: 0 };
  for (const item of items) counts[stockStatus(sellableOf(locations, item.id))] += 1;
  return counts;
};

export const formatNumber = (digits: string): string => {
  if (digits === '') return '';
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬');
  return toPersianDigits(grouped);
};

export const parseDigits = (value: string): string => {
  const persian = '۰۱۲۳۴۵۶۷۸۹';
  let out = '';
  for (const ch of value) {
    const pi = persian.indexOf(ch);
    if (pi >= 0) out += String(pi);
    else if (ch >= '0' && ch <= '9') out += ch;
  }
  return out;
};

// The stock the edit form opens pre-filled with for a variant — the
// variant row's own stock, not the location-derived sellable figure (the
// two can differ: sellable is what is allocated in a warehouse).
export const variantPrefillStock = (rows: SupplierProductVariantDto[], variantId: string): string => {
  const row = rows.find(candidate => candidate.id === variantId);
  return row ? String(row.stock ?? '') : '';
};

export const formatReturnDate = (iso: string): string => {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  try {
    return date.toLocaleDateString('fa-IR');
  } catch {
    return iso.slice(0, 10);
  }
};

// The stock-save contract, extracted so tests can pin the exact
// updateStock payload: same validation messages and the same
// updateStock(id, value, undefined, variantId?) call as before. The
// component wraps this with the saving flag, toast, and reloads.
export async function saveStockEdit(
  api: Pick<typeof supplierApi, 'updateStock'>,
  product: SupplierProductDto,
  stock: string,
  stockVariantId: string,
  variantRows: SupplierProductVariantDto[],
): Promise<void> {
  const value = Number(stock);
  if (stock.trim() === '' || !Number.isSafeInteger(value) || value < 0) {
    throw new Error('موجودی را با عدد وارد کن (نمی‌تواند منفی باشد).');
  }
  if (product.schemaVersion === 2 && (!stockVariantId || !variantRows.some(row => row.id === stockVariantId))) {
    throw new Error('تنوع دقیق کالا را انتخاب کنید.');
  }
  await api.updateStock(product.id, value, undefined, product.schemaVersion === 2 ? stockVariantId : undefined);
}

export interface ReturnValidationInput {
  requiresSKU: boolean;
  variantId: string;
  variantIds: string[];
  quantity: number;
  max: number;
  reason: string;
}
export const validateReturnRequest = (input: ReturnValidationInput): string | null => {
  if (input.requiresSKU && (!input.variantId || !input.variantIds.includes(input.variantId))) {
    return 'تنوع دقیق کالای برگشتی را انتخاب کنید.';
  }
  if (!Number.isSafeInteger(input.quantity) || input.quantity <= 0 || input.quantity > input.max || !input.reason.trim()) {
    return 'تعداد را درست وارد کن (از موجودی آماده فروش بیشتر نباشد) و علت برگرداندن را بنویس.';
  }
  return null;
};

// --- Presentational view ---------------------------------------------------

export interface SupplierInventoryEditState {
  product: SupplierProductDto;
  variantId: string;
  stock: string;
}

export interface SupplierInventoryViewProps {
  items: SupplierProductDto[];
  totalCount: number;
  locations: SupplierInventoryLocationDto[];
  returns: SupplierReturnRequestDto[];
  productsById: Record<string, SupplierProductDto>;
  shareData: { centralStockShare: number | null; centralUnits: number; supplierUnits: number } | null;
  shareLoaded: boolean;
  loading: boolean;
  error: string;
  retry: () => void;
  query: string;
  onQueryChange: (query: string) => void;
  statusFilter: InventoryStatusFilter;
  onStatusFilterChange: (filter: InventoryStatusFilter) => void;
  statusCounts: Record<InventoryStatusFilter, number>;
  editing: SupplierInventoryEditState | null;
  editVariants: { rows: SupplierProductVariantDto[]; loading: boolean; error: string; reload: () => void };
  saving: boolean;
  onEditVariantChange: (variantId: string) => void;
  onEditStockChange: (stock: string) => void;
  onSaveEdit: (event: React.FormEvent) => void;
  onCancelEdit: () => void;
  onOpenEdit: (product: SupplierProductDto, variantId?: string) => void;
  onOpenReturn: (product: SupplierProductDto) => void;
  returning: SupplierProductDto | null;
  returnVariants: { rows: SupplierProductVariantDto[]; loading: boolean; error: string; reload: () => void };
  returnQuantity: string;
  returnVariantId: string;
  returnMethod: 'supplier_collect' | 'postal';
  returnReason: string;
  returnSaving: boolean;
  onReturnVariantChange: (variantId: string) => void;
  onReturnQuantityChange: (quantity: string) => void;
  onReturnMethodChange: (method: 'supplier_collect' | 'postal') => void;
  onReturnReasonChange: (reason: string) => void;
  onSubmitReturn: (event: React.FormEvent) => void;
  onCancelReturn: () => void;
  // Supplier-owned sellable for the return max-guard (ownerType filter),
  // as opposed to sellableOf, which is the all-owner hero figure.
  returnableOf: (productId: string, variantId?: string, requiresSKU?: boolean) => number;
}

export const SupplierInventoryView: React.FC<SupplierInventoryViewProps> = (props) => {
  const {
    items, totalCount, locations, returns, productsById, shareData, shareLoaded,
    loading, error, retry, query, onQueryChange, statusFilter, onStatusFilterChange,
    statusCounts, editing, editVariants, saving, onEditVariantChange, onEditStockChange,
    onSaveEdit, onCancelEdit, onOpenEdit, onOpenReturn, returning, returnVariants,
    returnQuantity, returnVariantId, returnMethod, returnReason, returnSaving,
    onReturnVariantChange, onReturnQuantityChange, onReturnMethodChange, onReturnReasonChange,
    onSubmitReturn, onCancelReturn, returnableOf,
  } = props;
  const grouped = locations.reduce<Record<string, SupplierInventoryLocationDto[]>>((groups, location) => {
    (groups[location.productId] ||= []).push(location);
    return groups;
  }, {});

  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">موجودی کالاهای تو</h2>
      <details className="rounded-2xl border border-slate-100 bg-white px-4 py-3 text-[11px] leading-relaxed text-slate-600">
        <summary className="cursor-pointer font-bold text-slate-700">راهنمای موجودی</summary>
        <p className="pt-2">اینجا برای هر کالا می‌بینی چند تا از آن کجاست: آماده فروش، کنار گذاشته برای سفارش، یا در راه انبار. موجودی را هم از همین‌جا عوض می‌کنی.</p>
      </details>
      {shareLoaded && shareData && (
        <div className="rounded-2xl border-2 border-emerald-200 bg-emerald-50/70 p-3 text-[11px] leading-5">
          <div className="flex items-center justify-between gap-2">
            <span className="font-bold text-slate-800">سهم انبار مرکزی</span>
            <span className="rounded-full bg-emerald-600 px-2 py-0.5 text-[10px] font-bold text-white">ارسال سریع</span>
            {shareData.centralStockShare !== null ? (
              <span className="font-extrabold text-slate-900 font-mono">{toPersianDigits(Math.round(shareData.centralStockShare * 100))}٪</span>
            ) : (
              <span className="text-slate-400">بدون موجودی قابل فروش</span>
            )}
          </div>
          <p className="text-slate-500">{toPersianDigits(shareData.centralUnits)} واحد در انبار مرکزی • {toPersianDigits(shareData.supplierUnits)} واحد نزد شما</p>
          <p className="font-bold text-emerald-800 text-[10px]">کالایی که در انبار مرکزی دپو شده باشد، با «ارسال سریع» فروش می‌رود و امتیازت بالاتر می‌رود.</p>
        </div>
      )}
      <div className="sticky top-0 z-10 space-y-2 rounded-2xl border bg-white/95 p-2 shadow-sm backdrop-blur">
        <input
          aria-label="جستجوی موجودی"
          value={query}
          onChange={event => onQueryChange(event.target.value)}
          placeholder="اسم، کد کالا یا تنوع را بنویس"
          className="w-full rounded-xl border p-2.5 min-h-[44px] text-xs"
        />
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="فیلتر وضعیت موجودی">
          {(['all', 'in', 'low', 'out'] as InventoryStatusFilter[]).map(filter => (
            <button
              key={filter}
              type="button"
              aria-pressed={statusFilter === filter}
              onClick={() => onStatusFilterChange(filter)}
              className={`rounded-full border px-3 py-1.5 min-h-[32px] text-[11px] font-bold ${statusFilter === filter ? 'bg-brand-deep text-white border-brand-deep' : 'bg-white text-slate-600 border-slate-200'}`}
            >
              {filter === 'all' ? 'همه' : STOCK_STATUS_LABEL[filter]} ({toPersianDigits(statusCounts[filter])})
            </button>
          ))}
        </div>
        <p className="text-[10px] text-slate-400">نمایش {toPersianDigits(items.length)} از {toPersianDigits(totalCount)} کالا</p>
      </div>
      <SupplierResourceState loading={loading} error={error} retry={retry} />
      {returns.length > 0 && (
        <section className="rounded-2xl border bg-white p-3 space-y-2">
          <h3 className="text-xs font-bold">برگرداندن‌هایی که خواسته‌ای</h3>
          {returns.map(item => {
            const product = productsById[item.productId];
            const title = item.productTitle || product?.title || 'کالا';
            return (
              <div key={item.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-50 pb-2 last:border-0 last:pb-0">
                <div className="min-w-0">
                  <p className="text-[11px] font-bold text-slate-700">{title}</p>
                  <p className="text-[10px] text-slate-500">
                    {item.variantId && <span>{supplierInventoryLabel(item)} · </span>}{toPersianDigits(item.requestedQuantity)} عدد · {RETURN_STATUS_LABELS[item.status] || item.status}
                    {formatReturnDate(item.createdAt) && <span> · {formatReturnDate(item.createdAt)}</span>}
                  </p>
                </div>
                {product && (
                  <a href={`#inventory-${item.productId}`} className="shrink-0 rounded-lg bg-brand-soft px-2.5 py-1.5 text-[11px] font-bold text-brand-deep">مشاهده کالا</a>
                )}
              </div>
            );
          })}
        </section>
      )}
      {items.length === 0 && !loading && !error && (
        <div className="rounded-2xl border bg-white p-8 text-center space-y-2">
          <p className="text-xs font-bold text-slate-600">
            {query.trim() ? 'کالایی با این اسم پیدا نشد' : statusFilter !== 'all' ? 'کالایی با این وضعیت نیست' : 'هنوز کالای تأییدشده‌ای نداری'}
          </p>
          <p className="text-[10px] text-slate-400 leading-relaxed">
            {query.trim() ? 'اسم دیگری را امتحان کن.' : statusFilter !== 'all' ? 'فیلتر وضعیت را عوض کن.' : 'وقتی مدیر کالایت را تأیید کند، موجودی‌اش اینجا می‌آید.'}
          </p>
        </div>
      )}
      {items.map(item => {
        const itemLocations = grouped[item.id] || [];
        const sums = itemLocations.reduce<Record<string, number>>((result, location) => {
          result[location.status] = (result[location.status] || 0) + location.quantity;
          return result;
        }, {});
        const sellable = sums.sellable || 0;
        const status = stockStatus(sellable);
        const unreconciled = itemLocations.filter(location => !location.reconciled).reduce((sum, location) => sum + location.quantity, 0);
        const variants = item.schemaVersion === 2 ? variantSellableRows(locations, item.id) : [];
        return (
          <section key={item.id} id={`inventory-${item.id}`} className="scroll-mt-24 space-y-2 rounded-2xl border bg-white p-3">
            <div className="flex gap-3">
              {item.imageUrl ? (
                <img src={item.imageUrl} alt={item.title} className="h-12 w-12 shrink-0 rounded-xl border object-cover" />
              ) : (
                <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl border bg-slate-50 text-[10px] text-slate-400">بدون عکس</div>
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-start justify-between gap-2">
                  <h3 className="text-xs font-bold leading-5">{item.title}</h3>
                  <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-bold ${STOCK_STATUS_PILL[status]}`}>{STOCK_STATUS_LABEL[status]}</span>
                </div>
                <p className="text-[10px] text-slate-400">کد کالا: {item.id}{item.categorySlug ? ` · ${item.categorySlug}` : ''}</p>
                {item.originalPrice > 0 && <p className="text-[10px] text-slate-500">قیمت: {formatPrice(item.originalPrice)} تومان</p>}
              </div>
              <div className="shrink-0 text-left">
                <p className={`text-lg font-extrabold leading-6 ${status === 'out' ? 'text-rose-600' : status === 'low' ? 'text-amber-600' : 'text-slate-900'}`}>{toPersianDigits(sellable)}</p>
                <p className="text-[10px] text-slate-400">آماده فروش</p>
              </div>
            </div>
            {item.schemaVersion === 2 && variants.length > 0 && (
              <ul className="divide-y divide-slate-50 rounded-xl border border-slate-100">
                {variants.map(row => (
                  <li key={row.variantId} className="flex flex-wrap items-center justify-between gap-2 px-2 py-1.5 text-[11px]">
                    <span className="min-w-0 flex-1 font-bold text-slate-700">{row.label}</span>
                    <span className="text-slate-500">آماده فروش: <b className="text-slate-800">{toPersianDigits(row.sellable)}</b></span>
                    <button
                      type="button"
                      aria-label={`تغییر موجودی ${row.label}`}
                      onClick={() => onOpenEdit(item, row.variantId)}
                      className="shrink-0 rounded-lg bg-brand-soft px-2.5 py-1.5 min-h-[32px] text-[11px] font-bold text-brand-deep"
                    >
                      تغییر
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {item.schemaVersion === 2 && variants.length === 0 && (
              <p className="text-[10px] leading-relaxed text-slate-400">هنوز تنوعی از این کالا در انبار ثبت نشده؛ با «عوض‌کردن موجودی» تنوع را انتخاب کن.</p>
            )}
            <details className="rounded-xl bg-slate-50 px-3 py-2">
              <summary className="cursor-pointer text-[11px] font-bold text-slate-600">جزئیات وضعیت انبار</summary>
              <div className="grid grid-cols-3 gap-1 pt-2 text-[10px] text-slate-600">
                <span className="font-bold text-emerald-700">آماده فروش: <b>{toPersianDigits(sums.sellable || 0)}</b></span>
                <span>کنار گذاشته برای سفارش: <b>{toPersianDigits(sums.sales_reserved || 0)}</b></span>
                <span>درخواست برگشت: <b>{toPersianDigits(sums.return_requested || 0)}</b></span>
                <span>در حال بررسی کیفیت: <b>{toPersianDigits(sums.quarantine || 0)}</b></span>
                <span>در راه انبار: <b>{toPersianDigits(sums.in_transit || 0)}</b></span>
                <span>هنوز شمرده نشده: <b>{toPersianDigits(unreconciled)}</b></span>
              </div>
              {itemLocations.map(location => (
                <p key={`${location.locationKey}-${location.status}-${location.variantId || 'unallocated'}`} className="pt-1 text-[10px] text-slate-500">
                  {supplierInventoryLabel(location)} · {location.locationKey} · {STATUS_LABELS[location.status] || location.status} · {toPersianDigits(location.quantity)} عدد {location.reconciled ? '' : '· انبار هنوز ثبت نکرده'}
                </p>
              ))}
            </details>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                disabled={saving}
                onClick={() => onOpenEdit(item)}
                className="flex-1 rounded-lg bg-brand-soft px-3 py-2 min-h-[40px] text-xs font-bold text-brand-deep"
              >
                عوض‌کردن موجودی
              </button>
              <button
                type="button"
                onClick={() => onOpenReturn(item)}
                className="flex-1 rounded-lg bg-amber-50 px-3 py-2 min-h-[40px] text-xs font-bold text-amber-700"
              >
                برگرداندن کالا
              </button>
            </div>
          </section>
        );
      })}
      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-3">
          <form onSubmit={onSaveEdit} className="w-full max-w-sm space-y-3 rounded-2xl bg-white p-4">
            <h3 className="text-sm font-bold">تغییر موجودی: {editing.product.title}</h3>
            {editing.product.schemaVersion === 2 && <SupplierSKUSelect product={editing.product} rows={editVariants.rows} selectedId={editing.variantId}
              label="تنوع برای اصلاح موجودی" loading={editVariants.loading} error={editVariants.error} retry={editVariants.reload} disabled={saving}
              onSelect={id => onEditVariantChange(id)} /> }
            <label className="block text-[11px] font-bold text-slate-600">
              موجودی جدید (عدد)
              <input
                autoFocus
                type="text"
                inputMode="numeric"
                min="0"
                value={formatNumber(editing.stock)}
                onChange={event => onEditStockChange(parseDigits(event.target.value))}
                className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs bg-white font-normal"
              />
            </label>
            <div className="flex gap-2">
              <button
                disabled={saving || (editing.product.schemaVersion === 2 && (!editing.variantId || editVariants.loading || !!editVariants.error))}
                className="flex-1 rounded-lg bg-brand-deep px-3 py-2 min-h-[44px] text-xs font-bold text-white disabled:opacity-40"
              >
                {saving ? 'ذخیره…' : 'ذخیره موجودی'}
              </button>
              <button
                type="button"
                disabled={saving}
                onClick={onCancelEdit}
                className="flex-1 rounded-lg bg-white border px-3 py-2 min-h-[44px] text-xs"
              >
                انصراف
              </button>
            </div>
          </form>
        </div>
      )}
      {returning && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-3">
          <form onSubmit={onSubmitReturn} className="w-full max-w-sm space-y-3 rounded-2xl bg-white p-4">
            <h3 className="text-sm font-bold">برگرداندن کالا: {returning.title}</h3>
            {returning.schemaVersion === 2 && variantSellableRows(locations, returning.id).length > 0 && (
              <ul className="divide-y divide-slate-50 rounded-xl border border-slate-100 text-[11px]">
                {variantSellableRows(locations, returning.id).map(row => (
                  <li key={row.variantId} className="flex items-center justify-between gap-2 px-2 py-1.5">
                    <span className="font-bold text-slate-700">{row.label}</span>
                    <span className="text-slate-500">آماده برگشت: <b className="text-slate-800">{toPersianDigits(returnableOf(returning.id, row.variantId, true))}</b></span>
                  </li>
                ))}
              </ul>
            )}
            {returning.schemaVersion === 2 && <SupplierSKUSelect product={returning} rows={returnVariants.rows} selectedId={returnVariantId}
              label="تنوع کالای برگشتی" loading={returnVariants.loading} error={returnVariants.error} retry={returnVariants.reload} disabled={returnSaving}
              available={id => returnableOf(returning.id, id, true)} onSelect={id => onReturnVariantChange(id)} />}
            <p className="text-xs text-slate-500">آماده برگشت: {toPersianDigits(returnableOf(returning.id, returnVariantId || undefined, returning.schemaVersion === 2))} عدد</p>
            <label className="block text-[11px] font-bold text-slate-600">
              چند عدد برمی‌گردد؟
              <input
                aria-label="تعداد برگشتی"
                type="text"
                inputMode="numeric"
                min="1"
                max={returnableOf(returning.id, returnVariantId || undefined, returning.schemaVersion === 2)}
                value={formatNumber(returnQuantity)}
                onChange={event => onReturnQuantityChange(parseDigits(event.target.value))}
                className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs font-normal"
              />
            </label>
            <label className="block text-[11px] font-bold text-slate-600">
              چطور برمی‌گردد؟
              <select
                value={returnMethod}
                onChange={event => onReturnMethodChange(event.target.value as 'supplier_collect' | 'postal')}
                className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs font-normal"
              >
                <option value="supplier_collect">خودم می‌آیم تحویل می‌گیرم</option>
                <option value="postal">با پست برمی‌گردد</option>
              </select>
            </label>
            <label className="block text-[11px] font-bold text-slate-600">
              چرا برمی‌گردد؟
              <textarea
                aria-label="علت برگرداندن"
                value={returnReason}
                onChange={event => onReturnReasonChange(event.target.value)}
                className="mt-1 w-full rounded-lg border p-2 text-xs font-normal"
                placeholder="علت برگرداندن را کوتاه بنویس"
              />
            </label>
            <div className="flex gap-2">
              <button disabled={returnSaving || (returning.schemaVersion === 2 && (!returnVariantId || returnVariants.loading || !!returnVariants.error))} className="flex-1 rounded-lg bg-amber-600 p-2 min-h-[44px] text-xs font-bold text-white disabled:opacity-40">{returnSaving ? 'در حال ثبت…' : 'ثبت برگرداندن'}</button>
              <button type="button" disabled={returnSaving} onClick={onCancelReturn} className="flex-1 rounded-lg border p-2 min-h-[44px] text-xs">انصراف</button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};

// --- Container: data + state, unchanged API surface -------------------------

export const SupplierInventoryTab: React.FC = () => {
  const { success, error } = useToast();
  const resource = useSupplierResource(supplierApi.getProducts);
  const [query, setQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<InventoryStatusFilter>('all');
  const [editing, setEditing] = useState<SupplierProductDto | null>(null);
  const [stock, setStock] = useState('');
  const [stockVariantId, setStockVariantId] = useState('');
  const [saving, setSaving] = useState(false);
  const [returns, setReturns] = useState<SupplierReturnRequestDto[]>([]);
  const [locations, setLocations] = useState<SupplierInventoryLocationDto[]>([]);
  const [returning, setReturning] = useState<SupplierProductDto | null>(null);
  const [returnQuantity, setReturnQuantity] = useState('');
  const [returnVariantId, setReturnVariantId] = useState('');
  const [returnSaving, setReturnSaving] = useState(false);
  const stockVariants = useSupplierVariants(editing);
  const returnVariants = useSupplierVariants(returning);
  const [returnMethod, setReturnMethod] = useState<'supplier_collect' | 'postal'>('supplier_collect');
  const [returnReason, setReturnReason] = useState('');
  const [returnIdempotencyKeys, setReturnIdempotencyKeys] = useState<Record<string, string>>({});
  const [shareData, setShareData] = useState<{ centralStockShare: number | null; centralUnits: number; supplierUnits: number } | null>(null);
  const [shareLoaded, setShareLoaded] = useState(false);

  useEffect(() => {
    Promise.all([supplierApi.getReturns(), supplierApi.getInventoryLocations()]).then(([returnItems, inventory]) => {
      setReturns(returnItems);
      setLocations(inventory.inventory || []);
    }).catch(() => undefined);
    supplierApi.getStockShare().then((data: any) => {
      setShareData(data);
      setShareLoaded(true);
    }).catch(() => {
      setShareLoaded(true);
    });
  }, []);

  // Variant pre-fill: opening "تغییر" on a variant row pre-selects the
  // variant, but its stock only arrives with the variant rows — fill the
  // input as soon as the rows land, unless the user already typed.
  useEffect(() => {
    if (!editing || editing.schemaVersion !== 2 || !stockVariantId || stock !== '') return;
    const prefill = variantPrefillStock(stockVariants.rows, stockVariantId);
    if (prefill !== '') setStock(prefill);
  }, [editing, stockVariantId, stock, stockVariants.rows]);

  const openEdit = (item: SupplierProductDto, variantId?: string) => {
    setEditing(item);
    setStockVariantId(variantId || '');
    // v2 stock comes from the variant row (pre-filled by the effect above
    // once rows load, or by the picker below); v1 pre-fills immediately.
    setStock(item.schemaVersion === 2 ? '' : String(item.stock));
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!editing || saving) return;
    setSaving(true);
    try {
      await saveStockEdit(supplierApi, editing, stock, stockVariantId, stockVariants.rows);
      setEditing(null);
      success('موجودی ذخیره شد.');
      await resource.reload();
      supplierApi.getInventoryLocations().then(data => setLocations(data.inventory || [])).catch(() => {});
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ذخیره انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const canonicalSellable = (productId: string, variantId?: string, requiresSKU = false) => supplierReturnQuantity(locations, productId, variantId, requiresSKU);

// crypto.randomUUID exists only in secure contexts (HTTPS/localhost); the live and preview origins are plain HTTP, so fall back instead of throwing. Uniqueness is all the call sites need.
const newUuid = (): string => {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch { /* fall through to the fallback */ }
  return 'id-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
};

  const openReturnForm = (item: SupplierProductDto) => {
    setReturning(item);
    setReturnQuantity('1');
    setReturnVariantId('');
    setReturnReason('');
  };

  const submitReturn = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!returning || returnSaving) return;
    const quantity = Number(returnQuantity);
    const requiresSKU = returning.schemaVersion === 2;
    const validationError = validateReturnRequest({
      requiresSKU,
      variantId: returnVariantId,
      variantIds: returnVariants.rows.map(row => row.id),
      quantity,
      max: canonicalSellable(returning.id, requiresSKU ? returnVariantId : undefined, requiresSKU),
      reason: returnReason,
    });
    if (validationError) { error(validationError); return; }
    const selectedVariantId = requiresSKU ? returnVariantId : undefined;
    const scope = supplierReturnAttemptScope(returning.id, selectedVariantId, quantity, returnMethod, returnReason);
    const idempotencyKey = returnIdempotencyKeys[scope] || `return-${returning.id}-${selectedVariantId || 'unallocated'}-${newUuid()}`;
    setReturnIdempotencyKeys(current => ({ ...current, [scope]: idempotencyKey }));
    setReturnSaving(true);
    try {
      const created = await supplierApi.requestReturn(returning.id, quantity, returnMethod, returnReason.trim(), idempotencyKey, selectedVariantId);
      setReturns(current => [created, ...current.filter((item) => item.id !== created.id)]);
      setReturnIdempotencyKeys(current => { const next = { ...current }; delete next[scope]; return next; });
      supplierApi.getInventoryLocations().then(data => setLocations(data.inventory || [])).catch(() => {});
      void resource.reload();
      setReturning(null);
      setReturnQuantity('');
      setReturnReason('');
      success('درخواست برگرداندن کالا ثبت شد.');
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت درخواست انجام نشد.');
    } finally { setReturnSaving(false); }
  };

  const allProducts = resource.data || [];
  const variantLabels: Record<string, string[]> = {};
  if (editing && stockVariants.rows.length) variantLabels[editing.id] = stockVariants.rows.flatMap(row => [row.variant_name, row.spnc]);
  if (returning && returnVariants.rows.length) variantLabels[returning.id] = [...(variantLabels[returning.id] || []), ...returnVariants.rows.flatMap(row => [row.variant_name, row.spnc])];
  const queryFiltered = filterInventoryItems(allProducts, query, 'all', locations, variantLabels);
  const statusCounts = countByStockStatus(queryFiltered, locations);
  const items = statusFilter === 'all' ? queryFiltered : queryFiltered.filter(item => stockStatus(sellableOf(locations, item.id)) === statusFilter);
  const productsById = Object.fromEntries(allProducts.map(item => [item.id, item]));

  return (
    <SupplierInventoryView
      items={items}
      totalCount={allProducts.length}
      locations={locations}
      returns={returns}
      productsById={productsById}
      shareData={shareData}
      shareLoaded={shareLoaded}
      loading={resource.loading}
      error={resource.error}
      retry={resource.reload}
      query={query}
      onQueryChange={setQuery}
      statusFilter={statusFilter}
      onStatusFilterChange={setStatusFilter}
      statusCounts={statusCounts}
      editing={editing ? { product: editing, variantId: stockVariantId, stock } : null}
      editVariants={stockVariants}
      saving={saving}
      onEditVariantChange={id => { setStockVariantId(id); setStock(variantPrefillStock(stockVariants.rows, id)); }}
      onEditStockChange={setStock}
      onSaveEdit={save}
      onCancelEdit={() => setEditing(null)}
      onOpenEdit={openEdit}
      onOpenReturn={openReturnForm}
      returning={returning}
      returnVariants={returnVariants}
      returnQuantity={returnQuantity}
      returnVariantId={returnVariantId}
      returnMethod={returnMethod}
      returnReason={returnReason}
      returnSaving={returnSaving}
      onReturnVariantChange={setReturnVariantId}
      onReturnQuantityChange={setReturnQuantity}
      onReturnMethodChange={setReturnMethod}
      onReturnReasonChange={setReturnReason}
      onSubmitReturn={submitReturn}
      onCancelReturn={() => setReturning(null)}
      returnableOf={canonicalSellable}
    />
  );
};
