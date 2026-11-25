import { SupplierSectionHeading } from './SupplierSectionHeading';
import { SupplierDialog } from './SupplierDialog';
import { ContextSupportButton } from '../support/ContextSupportButton';
import { Boxes, Search } from 'lucide-react';
import React, { useCallback, useEffect, useId, useRef, useState } from 'react';
import { useToast } from '../../context/ToastContext';
import { supplierApi, SupplierProductDto, SupplierReturnRequestDto, SupplierInventoryLocationDto, SupplierProductVariantDto } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import { SupplierSKUSelect, useSupplierVariants } from './SupplierSKUSelect';
import { supplierInventoryLabel, supplierReturnAttemptScope, supplierReturnQuantity } from '../../utils/supplierSkuInventory';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import { ApiError } from '../../services/apiClient';
import { extractSpncxErrorCode } from '../../services/spncxAdminApi';
import { unitsApi, type ListUnitsParams, type ListUnitsResponse } from '../../services/unitsApi';
import { UNIT_STATUS_LABELS } from '../../utils/unitStatus';
import { SupplierVariantWorkspace } from './SupplierVariantWorkspace';
import { SupplierStorageAging } from './SupplierStorageAging';

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

export interface StockAdjustResponse {
  product_id: string;
  variant_id: string | null;
  previous_stock: number;
  stock: number;
  delta: number;
  retired_codes: string[];
  retired_printed_codes: string[];
  coverage_gap: Record<string, unknown> | null;
}

export function retiredPrintedCodesOf(response: unknown): string[] {
  if (!response || typeof response !== 'object' || !('retired_printed_codes' in response)) return [];
  const codes = response.retired_printed_codes;
  return Array.isArray(codes) ? codes.filter((code): code is string => typeof code === 'string') : [];
}

export function stockAdjustmentErrorMessage(cause: unknown, fallback: string): string {
  const code = extractSpncxErrorCode(cause);
  if (cause instanceof ApiError && cause.status === 409 && code === 'stock_below_held') {
    return `موجودی رزروشده مانع کاهش موجودی است. (${code})`;
  }
  if (code) return `${fallback} (${code})`;
  return cause instanceof Error ? cause.message : fallback;
}

export function RetiredPrintedCodesPanel({ codes, onAcknowledge }: { codes: string[]; onAcknowledge: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const [copyStatus, setCopyStatus] = useState('');
  useEffect(() => {
    const panel = dialog.current;
    panel?.showModal();
    return () => panel?.close();
  }, []);

  const copyAll = async () => {
    if (typeof document === 'undefined' || typeof navigator === 'undefined') return;
    const text = codes.join('\n');
    try {
      await navigator.clipboard.writeText(text);
      setCopyStatus('کدها کپی شدند.');
      return;
    } catch {
      // Plain HTTP and denied clipboard permissions can still use selection-based copying.
    }
    const previousFocus = document.activeElement;
    const input = document.createElement('textarea');
    input.value = text;
    input.readOnly = true;
    input.className = 'fixed opacity-0 pointer-events-none';
    dialog.current?.appendChild(input);
    try {
      input.select();
      setCopyStatus(document.execCommand('copy') ? 'کدها کپی شدند.' : 'کپی کدها انجام نشد.');
    } catch {
      setCopyStatus('کپی کدها انجام نشد.');
    } finally {
      input.remove();
      if (previousFocus instanceof HTMLElement) previousFocus.focus();
    }
  };

  return (
    <dialog ref={dialog} onCancel={event => event.preventDefault()} aria-labelledby={titleId} dir="rtl"
      className="m-auto w-[calc(100%-2rem)] max-w-lg space-y-3 rounded-2xl bg-white p-4 text-slate-900 backdrop:bg-slate-950/60">
      <h3 id={titleId} className="text-sm font-bold text-amber-800">این برچسب‌های چاپ‌شده دیگر معتبر نیستند — آن‌ها را از قفسه/کارتن جدا و معدوم کنید:</h3>
      <ul className="max-h-64 space-y-1 overflow-auto rounded-lg bg-amber-50 p-3">
        {codes.map((code, index) => <li key={index} dir="ltr" className="font-mono break-all text-xs">{code}</li>)}
      </ul>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <button type="button" onClick={copyAll} className="rounded-lg border px-3 py-2 font-bold">کپی همه کدها</button>
        <button type="button" onClick={onAcknowledge} className="rounded-lg bg-brand-deep px-3 py-2 font-bold text-white">متوجه شدم</button>
        <span role="status">{copyStatus}</span>
      </div>
    </dialog>
  );
}

// The stock-save contract, extracted so tests can pin the exact
// updateStock payload: absolute total, counted reason and exact SKU when needed.
// The
// component wraps this with the saving flag, toast, and reloads.
export async function saveStockEdit(
  api: Pick<typeof supplierApi, 'updateStock'>,
  product: SupplierProductDto,
  stock: string,
  stockVariantId: string,
  variantRows: SupplierProductVariantDto[],
  reason = '',
): Promise<StockAdjustResponse> {
  const value = Number(stock);
  if (stock.trim() === '' || !Number.isSafeInteger(value) || value < 0 || value > 2147483647) {
    throw new Error('موجودی را با عدد وارد کن (از صفر تا ۲۱۴۷۴۸۳۶۴۷).');
  }
  if (product.schemaVersion === 2 && (!stockVariantId || !variantRows.some(row => row.id === stockVariantId))) {
    throw new Error('تنوع دقیق کالا را انتخاب کنید.');
  }
  if (!reason.trim()) throw new Error('علت اصلاح موجودی را وارد کنید.');
  return await api.updateStock(product.id, value, reason.trim(), product.schemaVersion === 2 ? stockVariantId : undefined) as unknown as StockAdjustResponse;
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

type InventoryLocation = SupplierInventoryLocationDto & { balanceId?: string };

export function loadLotUnits(
  listUnits: (params: ListUnitsParams) => Promise<ListUnitsResponse>,
  balanceId: string,
  cursor?: string,
): Promise<ListUnitsResponse> {
  return listUnits({ balance_id: balanceId, limit: 200, ...(cursor ? { cursor } : {}) });
}

interface LotUnitsDrillDownProps {
  allocation?: InventoryLocation['allocation'];
  balanceId?: string;
  page?: ListUnitsResponse;
  expanded?: boolean;
  loading?: boolean;
  error?: string;
  onToggle?: () => void;
  onLoadMore?: () => void;
}

export function LotUnitsDrillDown({
  allocation, balanceId, page, expanded, loading, error, onToggle, onLoadMore,
}: LotUnitsDrillDownProps) {
  const listId = useId();
  if (allocation === 'unallocated') return <span>بدون واحد سریال‌دار</span>;
  if (!balanceId) return <span>—</span>;
  return (
    <div className="mt-1 space-y-1">
      <button type="button" onClick={onToggle} aria-expanded={!!expanded} aria-controls={listId} className="font-bold text-brand-deep underline">
        {page ? `${toPersianDigits(page.units.length)}${page.next_cursor !== null ? '+' : ''} واحد` : '— واحد'}
      </button>
      {error && <p role="alert" className="text-rose-700">{error}</p>}
      <div id={listId} hidden={!expanded}>
        {expanded && (
          <>
            {page && <ul className="space-y-1">
              {page.units.map(unit => (
                <li key={unit.code} className="flex flex-wrap items-center gap-x-2">
                  <span dir="ltr" className="font-mono break-all">{unit.code}</span>
                  <span>{UNIT_STATUS_LABELS[unit.status] || unit.status}</span>
                  <span>{unit.print_count > 0 ? `${toPersianDigits(unit.print_count)} بار چاپ شده` : 'هنوز چاپ نشده'}</span>
                </li>
              ))}
            </ul>}
            {loading && <p role="status">در حال دریافت واحدها…</p>}
            {(page?.next_cursor != null || (!page && error)) && (
              <button type="button" onClick={onLoadMore} disabled={loading} className="font-bold text-brand-deep underline">
                {error ? 'تلاش دوباره' : 'نمایش بیشتر'}
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function InventoryLotUnits({ balanceId, enabled }: { balanceId: string; enabled: boolean }) {
  const [page, setPage] = useState<ListUnitsResponse>();
  const [expanded, setExpanded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const fetchPage = useCallback(async (cursor?: string, isActive: () => boolean = () => true) => {
    setLoading(true);
    setError('');
    try {
      const result = await loadLotUnits(unitsApi.listUnits, balanceId, cursor);
      if (isActive()) setPage(previous => cursor && previous ? { ...result, units: [...previous.units, ...result.units] } : result);
    } catch (cause) {
      const code = extractSpncxErrorCode(cause);
      if (isActive()) setError(code ? `دریافت واحدهای این محل انجام نشد. (${code})` : 'دریافت واحدهای این محل انجام نشد.');
    } finally {
      if (isActive()) setLoading(false);
    }
  }, [balanceId]);

  useEffect(() => {
    if (!enabled) return;
    let active = true;
    void fetchPage(undefined, () => active);
    return () => { active = false; };
  }, [enabled, fetchPage]);

  return <LotUnitsDrillDown balanceId={balanceId} page={page} expanded={expanded} loading={loading} error={error}
    onToggle={() => setExpanded(value => !value)} onLoadMore={() => { if (!loading) void fetchPage(page?.next_cursor ?? undefined); }} />;
}

export interface SupplierInventoryEditState {
  product: SupplierProductDto;
  variantId: string;
  stock: string;
  reason: string;
}

export interface SupplierInventoryViewProps {
  items: SupplierProductDto[];
  totalCount: number;
  locations: InventoryLocation[];
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
  onEditReasonChange: (reason: string) => void;
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
    statusCounts, editing, editVariants, saving, onEditVariantChange, onEditStockChange, onEditReasonChange,
    onSaveEdit, onCancelEdit, onOpenEdit, onOpenReturn, returning, returnVariants,
    returnQuantity, returnVariantId, returnMethod, returnReason, returnSaving,
    onReturnVariantChange, onReturnQuantityChange, onReturnMethodChange, onReturnReasonChange,
    onSubmitReturn, onCancelReturn, returnableOf,
  } = props;
  const [openedLocationDetails, setOpenedLocationDetails] = useState<Record<string, boolean>>({});
  const grouped = locations.reduce<Record<string, InventoryLocation[]>>((groups, location) => {
    (groups[location.productId] ||= []).push(location);
    return groups;
  }, {});

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="موجودی کالاها" description="موجودی آمادهٔ فروش را ببین یا کالا و تنوع موردنظر را برای اصلاح موجودی پیدا کن." icon={<Boxes />} />
      <details className="rounded-2xl border border-slate-100 bg-white px-4 py-3 text-[11px] leading-relaxed text-slate-600">
        <summary className="cursor-pointer font-bold text-slate-700">راهنمای موجودی</summary>
        <p className="pt-2">اینجا برای هر کالا می‌بینی چند تا از آن کجاست: آماده فروش، کنار گذاشته برای سفارش، یا در راه انبار. موجودی را هم از همین‌جا عوض می‌کنی.</p>
      </details>
      {shareLoaded && shareData && Number.isFinite(shareData.centralUnits) && Number.isFinite(shareData.supplierUnits) && (
        <div className="space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-sm leading-6 shadow-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="font-extrabold text-[#261b41]">سهم انبار مرکزی</span>
            <span className="rounded-full bg-emerald-600 px-2 py-0.5 text-[10px] font-bold text-white">ارسال سریع</span>
            {shareData.centralStockShare !== null && Number.isFinite(shareData.centralStockShare) ? (
              <span className="font-extrabold text-slate-900 font-mono">{toPersianDigits(Math.round(shareData.centralStockShare * 100))}٪</span>
            ) : (
              <span className="text-slate-400">بدون موجودی قابل فروش</span>
            )}
          </div>
          <p className="text-slate-500">{toPersianDigits(shareData.centralUnits)} واحد در انبار مرکزی • {toPersianDigits(shareData.supplierUnits)} واحد نزد شما</p>
          <p className="font-bold text-emerald-800 text-[10px]">کالایی که در انبار مرکزی دپو شده باشد، با «ارسال سریع» فروش می‌رود و امتیازت بالاتر می‌رود.</p>
        </div>
      )}
      <div className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-3 shadow-sm sm:p-4">
        <div className="relative">
          <Search aria-hidden="true" className="pointer-events-none absolute start-3 top-3.5 size-4 text-slate-500" />
          <input
          type="search"
          aria-label="جستجوی موجودی"
          value={query}
          onChange={event => onQueryChange(event.target.value)}
          placeholder="اسم، کد کالا یا تنوع را بنویس"
          className="min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] ps-9 pe-3 text-sm focus:bg-white focus-visible:ring-2 focus-visible:ring-brand"
          />
        </div>
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="فیلتر وضعیت موجودی">
          {(['all', 'in', 'low', 'out'] as InventoryStatusFilter[]).map(filter => (
            <button
              key={filter}
              type="button"
              aria-pressed={statusFilter === filter}
              onClick={() => onStatusFilterChange(filter)}
              className={`min-h-11 rounded-xl border px-3 text-xs font-bold focus-visible:ring-2 focus-visible:ring-brand ${statusFilter === filter ? 'border-[#382660] bg-[#382660] text-white' : 'border-[#e6e2f0] bg-[#f8f6fc] text-[#382660] hover:bg-brand-soft'}`}
            >
              {filter === 'all' ? 'همه' : STOCK_STATUS_LABEL[filter]} ({loading ? '—' : toPersianDigits(statusCounts[filter])})
            </button>
          ))}
        </div>
        {loading ? (
          <p className="text-[10px] text-slate-400">در حال دریافت…</p>
        ) : (
          <p className="text-[10px] text-slate-400">نمایش {toPersianDigits(items.length)} از {toPersianDigits(totalCount)} کالا</p>
        )}
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
                <div className="flex flex-wrap items-center gap-2">
                  {product && <a href={`#inventory-${item.productId}`} className="shrink-0 rounded-lg bg-brand-soft px-2.5 py-1.5 text-[11px] font-bold text-brand-deep">مشاهده کالا</a>}
                  <ContextSupportButton role="supplier" kind="return" referenceType="stock_return" referenceId={item.id} />
                </div>
              </div>
            );
          })}
        </section>
      )}
      {items.length === 0 && !loading && !error && (
        <SupplierEmptyState icon={<Boxes />} title={query.trim() ? 'کالایی با این اسم پیدا نشد' : statusFilter !== 'all' ? 'کالایی با این وضعیت نیست' : 'هنوز کالای تأییدشده‌ای نداری'}
          description={!query.trim() && statusFilter === 'all' ? 'وقتی مدیر کالایت را تأیید کند، موجودی‌اش اینجا نمایش داده می‌شود.' : undefined}
          action={(query || statusFilter !== 'all') && <button type="button" onClick={() => { onQueryChange(''); onStatusFilterChange('all'); }} className="min-h-11 rounded-xl bg-brand-soft px-4 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن فیلترها</button>} />
      )}
      {!loading && items.map(item => {
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
          <section key={item.id} id={`inventory-${item.id}`} className="scroll-mt-24 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <div className="flex gap-3">
              {item.imageUrl ? (
                <img src={item.imageUrl} alt="" width={48} height={48} loading="lazy" className="size-12 shrink-0 rounded-xl border border-[#e6e2f0] object-cover" />
              ) : (
                <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl border bg-slate-50 text-[10px] text-slate-400">بدون عکس</div>
              )}
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-start gap-2">
                  <h3 className="min-w-0 break-words text-sm font-extrabold leading-6 text-[#261b41]">{item.title}</h3>
                  <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-bold ${STOCK_STATUS_PILL[status]}`}>{STOCK_STATUS_LABEL[status]}</span>
                </div>
                <p className="break-all text-xs text-slate-600">کد کالا: {item.id}{item.categorySlug ? ` · ${item.categorySlug}` : ''}</p>
                {item.originalPrice > 0 && <p className="text-[10px] text-slate-500">قیمت: {formatPrice(item.originalPrice)} تومان</p>}
              </div>
              <div className="shrink-0 rounded-xl bg-[#f4effa] px-2.5 py-2 text-center">
                <p className={`text-xl font-extrabold tabular-nums leading-6 ${status === 'out' ? 'text-rose-600' : status === 'low' ? 'text-amber-600' : 'text-slate-900'}`}>{toPersianDigits(sellable)}</p>
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
                      className="min-h-11 shrink-0 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"
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
            <details className="rounded-xl bg-slate-50 px-3 py-2" onToggle={event => {
              if (event.currentTarget.open && !openedLocationDetails[item.id]) {
                setOpenedLocationDetails(previous => ({ ...previous, [item.id]: true }));
              }
            }}>
              <summary className="cursor-pointer text-[11px] font-bold text-slate-600">جزئیات وضعیت انبار</summary>
              <div className="grid grid-cols-2 gap-2 pt-2 text-xs text-slate-600 sm:grid-cols-3">
                <span className="font-bold text-emerald-700">آماده فروش: <b>{toPersianDigits(sums.sellable || 0)}</b></span>
                <span>کنار گذاشته برای سفارش: <b>{toPersianDigits(sums.sales_reserved || 0)}</b></span>
                <span>درخواست برگشت: <b>{toPersianDigits(sums.return_requested || 0)}</b></span>
                <span>در حال بررسی کیفیت: <b>{toPersianDigits(sums.quarantine || 0)}</b></span>
                <span>در راه انبار: <b>{toPersianDigits(sums.in_transit || 0)}</b></span>
                <span>هنوز شمرده نشده: <b>{toPersianDigits(unreconciled)}</b></span>
              </div>
              {itemLocations.map(location => (
                <div key={location.balanceId || `${location.locationKey}-${location.status}-${location.variantId || 'unallocated'}`} className="pt-1 text-[10px] text-slate-500">
                  <p>{supplierInventoryLabel(location)} · {location.locationKey} · {STATUS_LABELS[location.status] || location.status} · {toPersianDigits(location.quantity)} عدد {location.reconciled ? '' : '· انبار هنوز ثبت نکرده'}</p>
                  {location.allocation !== 'unallocated' && location.balanceId
                    ? <InventoryLotUnits balanceId={location.balanceId} enabled={!!openedLocationDetails[item.id]} />
                    : <LotUnitsDrillDown allocation={location.allocation} />}
                </div>
              ))}
            </details>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                disabled={saving}
                onClick={() => onOpenEdit(item)}
                className="min-h-11 flex-1 rounded-xl bg-brand-soft px-3 py-2 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"
              >
                عوض‌کردن موجودی
              </button>
              <button
                type="button"
                onClick={() => onOpenReturn(item)}
                className="min-h-11 flex-1 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-bold text-amber-800 hover:bg-amber-100 focus-visible:ring-2 focus-visible:ring-amber-700"
              >
                برگرداندن کالا
              </button>
            </div>
          </section>
        );
      })}
      {editing && (
        <SupplierDialog titleId="supplier-stock-dialog-title" onDismiss={saving ? undefined : onCancelEdit}>
          <form onSubmit={onSaveEdit} className="space-y-3">
            <h3 id="supplier-stock-dialog-title" className="text-base font-extrabold">تغییر موجودی {editing.product.title}</h3>
            {editing.product.schemaVersion === 2 && <SupplierSKUSelect product={editing.product} rows={editVariants.rows} selectedId={editing.variantId}
              label="تنوع برای اصلاح موجودی" loading={editVariants.loading} error={editVariants.error} retry={editVariants.reload} disabled={saving}
              onSelect={id => onEditVariantChange(id)} /> }
            <label className="block text-[11px] font-bold text-slate-600">
              موجودی کل قابل فروش (عدد)
              <input
                type="text"
                inputMode="numeric"
                min="0"
                value={formatNumber(editing.stock)}
                onChange={event => onEditStockChange(parseDigits(event.target.value))}
                className="mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand"
              />
            </label>
            <p className="text-[11px] text-slate-500">این تعداد شامل موجودی قابل فروش در انبارهای دیگر هم هست.</p>
            <label className="block text-xs font-bold text-slate-700">علت اصلاح موجودی<input aria-label="علت اصلاح موجودی" value={editing.reason} onChange={event => onEditReasonChange(event.target.value)} maxLength={500} className="mt-1 min-h-11 w-full rounded-xl border border-slate-300 px-3 py-2 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand" /></label>
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
        </SupplierDialog>
      )}
      {returning && (
        <SupplierDialog titleId="supplier-return-dialog-title" onDismiss={returnSaving ? undefined : onCancelReturn}>
          <form onSubmit={onSubmitReturn} className="space-y-3">
            <h3 id="supplier-return-dialog-title" className="text-base font-extrabold">برگرداندن {returning.title}</h3>
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
        </SupplierDialog>
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
  const [stockReason, setStockReason] = useState('');
  const [stockVariantId, setStockVariantId] = useState('');
  const [saving, setSaving] = useState(false);
  const [retiredPrintedCodes, setRetiredPrintedCodes] = useState<string[]>([]);
  const [returns, setReturns] = useState<SupplierReturnRequestDto[]>([]);
  const [locations, setLocations] = useState<InventoryLocation[]>([]);
  // E7: the locations fetch is a second, independent load — track it
  // explicitly so the view never renders data-looking zeros while either
  // fetch is still in flight.
  const [locationsLoading, setLocationsLoading] = useState(true);
  const [locationsError, setLocationsError] = useState('');
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

  // E7: one loader for the returns+locations pair, so retry re-triggers
  // both fetches. Settlement (success or error) always clears the loading
  // flag — a locations failure must surface as an error with retry, never
  // as silent all-zero stock.
  const loadLocations = useCallback(() => {
    setLocationsLoading(true);
    setLocationsError('');
    Promise.all([supplierApi.getReturns(), supplierApi.getInventoryLocations()]).then(([returnItems, inventory]) => {
      setReturns(returnItems);
      setLocations(inventory.inventory || []);
    }).catch((err: any) => {
      setLocationsError(err?.message || 'دریافت موجودی انبار انجام نشد.');
    }).finally(() => {
      setLocationsLoading(false);
    });
  }, []);

  useEffect(() => {
    loadLocations();
    supplierApi.getStockShare().then((data: any) => {
      setShareData(data);
      setShareLoaded(true);
    }).catch(() => {
      setShareLoaded(true);
    });
  }, [loadLocations]);

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
    setStockReason('');
    setStockVariantId(variantId || '');
    // v2 stock comes from the variant row (pre-filled by the effect above
    // once rows load, or by the picker below); v1 pre-fills immediately.
    setStock(item.schemaVersion === 2 ? '' : String(item.stock));
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!editing || saving || retiredPrintedCodes.length > 0) return;
    setSaving(true);
    try {
      const response = await saveStockEdit(supplierApi, editing, stock, stockVariantId, stockVariants.rows, stockReason);
      if (response.delta < 0) {
        const codes = retiredPrintedCodesOf(response);
        if (codes.length > 0) setRetiredPrintedCodes(codes);
      }
      setEditing(null);
      success('موجودی ذخیره شد.');
      await resource.reload();
      supplierApi.getInventoryLocations().then(data => setLocations(data.inventory || [])).catch(() => {});
    } catch (cause: unknown) {
      error(stockAdjustmentErrorMessage(cause, 'ذخیره انجام نشد.'));
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

  // E7 retry semantics: exactly one products reload (resource.reload) and
  // exactly one locations reload (loadLocations) per retry — the two
  // loaders are independent, so neither double-fires.
  const retryAll = () => {
    void resource.reload();
    loadLocations();
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
    <div className="space-y-3">
      {retiredPrintedCodes.length > 0 && <RetiredPrintedCodesPanel codes={retiredPrintedCodes} onAcknowledge={() => setRetiredPrintedCodes([])} />}
      <SupplierVariantWorkspace onChanged={retryAll} onRetired={codes => setRetiredPrintedCodes(previous => [...new Set([...previous, ...codes])])} />
      <SupplierStorageAging />
      <SupplierInventoryView
      items={items}
      totalCount={allProducts.length}
      locations={locations}
      returns={returns}
      productsById={productsById}
      shareData={shareData}
      shareLoaded={shareLoaded}
      loading={resource.loading || locationsLoading}
      error={resource.error || locationsError}
      retry={retryAll}
      query={query}
      onQueryChange={setQuery}
      statusFilter={statusFilter}
      onStatusFilterChange={setStatusFilter}
      statusCounts={statusCounts}
      editing={editing ? { product: editing, variantId: stockVariantId, stock, reason: stockReason } : null}
      editVariants={stockVariants}
      saving={saving}
      onEditVariantChange={id => { setStockVariantId(id); setStock(variantPrefillStock(stockVariants.rows, id)); }}
      onEditStockChange={setStock}
      onEditReasonChange={setStockReason}
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
    </div>
  );
};
