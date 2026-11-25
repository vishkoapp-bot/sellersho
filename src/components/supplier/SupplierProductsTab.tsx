import { SupplierSectionHeading } from './SupplierSectionHeading';
import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi, isSupplierProductActive, SupplierProductDto, SupplierPriceIncreaseDto, WarrantyPropertyDto } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import { Package, Plus, CheckCircle2, Clock, ShieldCheck, Edit3, XCircle, AlertCircle, Save, Upload, Trash2, Star, Layers, Image as ImageIcon } from 'lucide-react';
import { SupplierGalleryModal } from './SupplierGalleryModal';
import { SupplierVariantsModal, unitCountDiscrepancy } from './SupplierVariantsModal';
import { RetiredPrintedCodesPanel, retiredPrintedCodesOf, stockAdjustmentErrorMessage, type StockAdjustResponse } from './SupplierInventoryTab';
import { toPersianDigits } from '../../utils/formatters';
import { normalizePersianText } from '../../utils/routing';
import { SupplierReviewsModal } from './SupplierReviewsModal';
import { SupplierFeedbackModal } from './SupplierFeedbackModal';
import { SupplierAddonsWidget } from './SupplierAddonsWidget';
import { SupplierProposeForm } from './SupplierProposeForm';
import { SupplierSKUSelect, useSupplierVariants } from './SupplierSKUSelect';

// Reject-notification deep-link decision (DESIGN.md §4.3-4.4), extracted
// as a pure function so the guard / consume-once semantics are
// unit-testable: renderToStaticMarkup never runs effects, so the effect
// below cannot be exercised by a static render. The effect applies the
// outcome verbatim — 'focus' means switch to proposed_offers and (when
// targetId is set) scroll to + open the SupplierFeedbackModal for that
// proposal after a paint; the caller marks the id handled and reports it
// consumed. 'skip' while loading deliberately does NOT consume the id,
// so the deep-link still lands once the proposals arrive.
export type ProposalFocusOutcome =
  | { kind: 'reset' }
  | { kind: 'skip' }
  | { kind: 'focus'; focusId: string; targetId: string | null };

export const resolveProposalFocus = (
  focusProposalId: string | null | undefined,
  proposals: Array<{ id: string | number }>,
  handledId: string | null,
  loading: boolean,
): ProposalFocusOutcome => {
  if (!focusProposalId) return { kind: 'reset' };
  if (handledId === focusProposalId) return { kind: 'skip' };
  if (loading) return { kind: 'skip' };
  const target = proposals.find((prop) => String(prop.id) === String(focusProposalId));
  return { kind: 'focus', focusId: focusProposalId, targetId: target ? String(target.id) : null };
};

// G6: status counts for the proposals tab label. The list is an
// all-status history; only `pending` is "awaiting review".
export const proposalStatusCounts = (offers: { status: string }[]) => ({
  total: offers.length,
  pending: offers.filter((prop) => prop.status === 'pending').length,
  approved: offers.filter((prop) => prop.status === 'approved').length,
  rejected: offers.filter((prop) => prop.status === 'rejected').length,
});

// E1: row status label — an inactive row is «غیرفعال», never «تأییدشده».
// The header count and this badge share isSupplierProductActive, so the
// Products tab and the Overview tab can no longer disagree (41 vs 43).
export const supplierProductStatusLabel = (product: SupplierProductDto): string =>
  isSupplierProductActive(product) ? 'تأییدشده' : 'غیرفعال';

// G4: a stock edit needs an origin for supplier_direct products only;
// warehouse-fulfilment products ship from the central warehouse and their
// stock is not declared through this path.
export const supplierStockNeedsOrigin = (product: Pick<SupplierProductDto, 'fulfillmentMethod' | 'originSupplierWarehouseId'>): boolean =>
  product.fulfillmentMethod === 'supplier_direct' && !product.originSupplierWarehouseId;

// G4: honest origin display — a NULL origin is «مبدأ مشخص نشده» even when
// a profile city exists; the profile city is not an origin (E5 owner gate
// pending) and the backend stock gate rejects NULL origins.
export const resolveSupplierOriginDisplay = (
  product: Pick<SupplierProductDto, 'originSupplierWarehouseId'>,
  originWarehouse: { name: string; cityName: string } | undefined,
): string => originWarehouse ? `${originWarehouse.name} — ${originWarehouse.cityName}` : product.originSupplierWarehouseId ? 'انبار غیرفعال‌شده' : 'مبدأ مشخص نشده';

// G4: the stock endpoint maps ErrStockOriginRequired to HTTP 409 and
// returns its Persian message verbatim in the `error` field, which
// apiRequest surfaces as the ApiError message.
export const isStockOriginRequiredError = (cause: unknown): boolean => {
  if (!(cause instanceof Error)) return false;
  return (cause as { status?: number }).status === 409 && cause.message.includes('انبار فعال متعلق به تأمین‌کننده');
};

export function sumProductUnitCount(counts: Record<string, number> | undefined): number | null {
  const values = Object.values(counts ?? {});
  return values.length ? values.reduce((sum, value) => sum + value, 0) : null;
}

export function productUnitChipState(unitCount: number | null, stock: number): 'unloaded' | 'matched' | 'mismatch' {
  if (unitCount === null) return 'unloaded';
  return unitCount === stock ? 'matched' : 'mismatch';
}

export function ProductUnitChip({ unitCount, stock }: { unitCount: number | null; stock: number }) {
  const state = productUnitChipState(unitCount, stock);
  return (
    <span className={`rounded-lg px-1.5 py-0.5 ${state === 'mismatch' ? 'bg-amber-50 text-amber-800' : 'bg-slate-100 text-slate-600'}`}>
      {unitCount === null ? '—' : `${toPersianDigits(unitCount)} واحد سریال‌دار از ${toPersianDigits(stock)} عدد موجودی`}
      {unitCount !== null && state === 'mismatch' && <strong className="block">{unitCountDiscrepancy(unitCount, stock)}</strong>}
    </span>
  );
}

export const SupplierProductsTab: React.FC<{ focusProposalId?: string | null; onFocusConsumed?: () => void; onPrepareShipment?: (productId: string) => void; openProposal?: boolean; showProposals?: boolean }> = ({ focusProposalId, onFocusConsumed, onPrepareShipment, openProposal, showProposals }) => {
  const {
    supplierSession,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error, warning } = useToast();
  const productsResource = useSupplierResource(supplierApi.getProducts);
  const proposalsResource = useSupplierResource(supplierApi.getProposals);
  const [saving, setSaving] = useState(false);
  const [showAddForm, setShowAddForm] = useState(!!openProposal);
  const [productQuery, setProductQuery] = useState('');
  const proposalFormRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (showAddForm) {
      proposalFormRef.current?.focus({ preventScroll: true });
      proposalFormRef.current?.scrollIntoView({ block: 'start' });
    }
  }, [showAddForm]);
  const [subTab, setSubTab] = useState<'active_products' | 'proposed_offers' | 'my_challenges'>(showProposals ? 'proposed_offers' : 'active_products');
  const loadChallenges = useCallback(() => subTab === 'my_challenges' ? supplierApi.getMyExclusivityChallenges() : Promise.resolve(null), [subTab]);
  const challenges = useSupplierResource(loadChallenges);
  const [myWarehouses, setMyWarehouses] = useState<any[]>([]);
  const [profileCity, setProfileCity] = useState('');
  const [profileCityConfirmed, setProfileCityConfirmed] = useState(false);
  const [originEditingProduct, setOriginEditingProduct] = useState<SupplierProductDto | null>(null);
  const [galleryProduct, setGalleryProduct] = useState<SupplierProductDto | null>(null);
  const [variantsProduct, setVariantsProduct] = useState<SupplierProductDto | null>(null);
  const [unitCountsByProduct, setUnitCountsByProduct] = useState<Record<string, Record<string, number>>>({});
  const [retiredPrintedCodes, setRetiredPrintedCodes] = useState<string[]>([]);
  const storeUnitCounts = useCallback((productId: string, counts: Record<string, number>) => {
    setUnitCountsByProduct(previous => ({ ...previous, [productId]: counts }));
  }, []);
  const invalidateUnitCounts = (productId: string) => {
    setUnitCountsByProduct(previous => {
      if (!Object.hasOwn(previous, productId)) return previous;
      const next = { ...previous };
      delete next[productId];
      return next;
    });
  };
  const [reviewsProduct, setReviewsProduct] = useState<SupplierProductDto | null>(null);
  const [feedbackProposalId, setFeedbackProposalId] = useState<string | null>(null);
  // Reject-notification deep-link (DESIGN.md §4.3-4.4): the drawer hands
  // this tab a proposal id; the tab switches to proposed_offers, scrolls
  // the card into view with a temporary rose ring, and auto-opens the
  // existing SupplierFeedbackModal (the resubmit flow) for it. No second
  // edit path is built; the focus is consumed once so re-renders never
  // re-open the modal.
  const [highlightedProposalId, setHighlightedProposalId] = useState<string | null>(null);
  const handledFocusRef = useRef<string | null>(null);
  const scrollToProposal = (id: string) => {
    setHighlightedProposalId(id);
    document.getElementById(`proposal-card-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    window.setTimeout(() => setHighlightedProposalId((cur) => (cur === id ? null : cur)), 5000);
  };
  const [originWarehouseId, setOriginWarehouseId] = useState('');
  const [bulkWarehouseId, setBulkWarehouseId] = useState('');
  const [bulkSaving, setBulkSaving] = useState(false);
  const [bulkResult, setBulkResult] = useState<string | null>(null);
  useEffect(() => {
    supplierApi.getMyWarehouses().then(setMyWarehouses).catch(() => {});
    supplierApi.getProfile().then((pr: any) => { setProfileCity(pr.warehouse_city || pr.warehouseCity || ''); setProfileCityConfirmed(!!(pr.warehouse_city_confirmed || pr.warehouseCityConfirmed)); }).catch(() => {});
  }, []);
  const [warranties, setWarranties] = useState<WarrantyPropertyDto[]>([]);

  const [editingProduct, setEditingProduct] = useState<SupplierProductDto | null>(null);
  const [editedCostC, setEditedCostC] = useState('');

  // state های جدید برای ویرایش موجودی و همگام‌سازی لحظه‌ای
  const [editingStockProduct, setEditingStockProduct] = useState<SupplierProductDto | null>(null);
  const [stockOriginBlockedId, setStockOriginBlockedId] = useState<string | null>(null);
  const [editedStock, setEditedStock] = useState('');
  const [stockReason, setStockReason] = useState('');
  const [stockVariantId, setStockVariantId] = useState('');
  const stockVariants = useSupplierVariants(editingStockProduct);
  const [editingSupplyProduct, setEditingSupplyProduct] = useState<SupplierProductDto | null>(null);
  const [editedSupplyOption, setEditedSupplyOption] = useState<'3hour_local' | '1day' | '3day'>('1day');
  const supplierProducts = productsResource.error ? [] : productsResource.data || [];
  const query = normalizePersianText(productQuery.trim().toLowerCase());
  const visibleProducts = query ? supplierProducts.filter(product => [product.title, product.brand, product.slug].some(value => value && normalizePersianText(value.toLowerCase()).includes(query))) : supplierProducts;
  // E1: the header counts ACTIVE products only (same predicate as Overview).
  const activeSupplierProducts = supplierProducts.filter(isSupplierProductActive);
  const inactiveSupplierProductsCount = supplierProducts.length - activeSupplierProducts.length;
  // G4: supplier_direct rows with no origin cannot take a stock edit.
  const nullOriginDirectCount = supplierProducts.filter(supplierStockNeedsOrigin).length;
  const supplierProposedOffers = proposalsResource.error ? [] : proposalsResource.data || [];
  const rejectedProposals = supplierProposedOffers.filter((prop) => prop.status === 'rejected');
  // G6: the tab label counts PENDING proposals only — the list is an
  // all-status history (approved/rejected rows carry their own badges
  // and resubmit actions), so labelling its length as "awaiting
  // review" overstated the queue (46 vs the 3 actually pending).
  const proposalCounts = proposalStatusCounts(supplierProposedOffers);
  useEffect(() => {
    const outcome = resolveProposalFocus(focusProposalId, supplierProposedOffers, handledFocusRef.current, proposalsResource.loading);
    if (outcome.kind === 'reset') { handledFocusRef.current = null; return; }
    if (outcome.kind === 'skip') return;
    handledFocusRef.current = outcome.focusId;
    setSubTab('proposed_offers');
    if (outcome.targetId !== null) {
      const targetId = outcome.targetId;
      // Let the proposed_offers list render before scrolling/opening.
      window.setTimeout(() => { scrollToProposal(targetId); setFeedbackProposalId(targetId); }, 50);
    }
    onFocusConsumed?.();
  }, [focusProposalId, proposalsResource.loading, supplierProposedOffers, onFocusConsumed]);
  const canManageStock = supplierSession?.permissions?.some(permission => permission === 'supplier.manage' || permission === 'inventory.manage');
  React.useEffect(() => { supplierApi.getWarranties().then(setWarranties).catch(() => undefined); }, []);

  // Gap 11 — governed price increases: products with a pending review row
  // get a «در حال بررسی» chip; the list also refreshes after a 202 save.
  const [priceIncreases, setPriceIncreases] = useState<SupplierPriceIncreaseDto[]>([]);
  const reloadPriceIncreases = () => { supplierApi.getPriceIncreases().then(setPriceIncreases).catch(() => undefined); };
  useEffect(() => { reloadPriceIncreases(); }, []);
  const pendingPriceProductIds = new Set(priceIncreases.filter((row) => row.status === 'pending').map((row) => row.productId));


  const handleSaveWholesaleCost = async (productId: string) => {
    if (saving) return;
    const cost = Number(editedCostC);
    if (!Number.isSafeInteger(cost) || cost <= 0) {
      error('قیمت عمده را درست وارد کن.');
      return;
    }

    setSaving(true);
    try {
      const res = await supplierApi.updatePricing(productId, cost);
      if (res && res.status === 'pending_review') {
        // Gap 11 — the increase was queued for management review (HTTP 202).
        warning(res.message || 'درخواست افزایش قیمت برای بررسی مدیریت ثبت شد.');
        setEditingProduct(null); setEditedCostC('');
        reloadPriceIncreases();
        await productsResource.reload();
      } else {
        success(`قیمت عمده کالا به ${formatPrice(cost)} تومان تغییر کرد.`); setEditingProduct(null); setEditedCostC(''); await productsResource.reload();
      }
    }
    catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'ذخیره قیمت انجام نشد.'); }
    finally { setSaving(false); }
  };

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="کالاها و تنوع‌ها" description="مدل‌ها و وضعیت کالاها را ببین یا کالای جدیدی برای بررسی پیشنهاد بده." icon={<Package />}
        action={<button type="button" aria-expanded={showAddForm} onClick={() => setShowAddForm(open => !open)}
          className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2">
          {!showAddForm && <Plus aria-hidden="true" className="size-4" />} {showAddForm ? 'بستن فرم پیشنهاد' : 'پیشنهاد کالای جدید'}
        </button>} />
      <SupplierResourceState loading={productsResource.loading || proposalsResource.loading} error={productsResource.error || proposalsResource.error} retry={() => { void productsResource.reload(); void proposalsResource.reload(); }} />
      {showAddForm && (
        <section ref={proposalFormRef} tabIndex={-1} aria-label="فرم پیشنهاد کالای جدید" className="scroll-mt-20">
          <SupplierProposeForm
            warranties={warranties}
            myWarehouses={myWarehouses}
            profileCity={profileCity}
            profileCityConfirmed={profileCityConfirmed}
            sessionToken={supplierSession?.token}
            onSubmitted={async () => { setShowAddForm(false); setSubTab('proposed_offers'); await proposalsResource.reload(); await productsResource.reload(); }}
            onChallengeSubmitted={() => { setShowAddForm(false); if (subTab === 'my_challenges') void challenges.reload(); else setSubTab('my_challenges'); }}
          />
        </section>
      )}
      <div role="group" aria-label="بخش‌های کالاها" className="grid gap-2 rounded-[20px] border border-[#e6e2f0] bg-white p-2 shadow-sm sm:grid-cols-3">
        {([
          { id: 'active_products', label: `کالاهای تأییدشده (${toPersianDigits(supplierProducts.length)})`, note: inactiveSupplierProductsCount > 0 ? `${toPersianDigits(inactiveSupplierProductsCount)} غیرفعال` : '' },
          { id: 'proposed_offers', label: `پیشنهادهای کالا (${toPersianDigits(proposalCounts.total)})`, note: proposalCounts.pending > 0 ? `${toPersianDigits(proposalCounts.pending)} در انتظار بررسی` : '' },
          { id: 'my_challenges', label: 'درخواست‌های من', note: '' },
        ] as const).map(({ id, label, note }) => (
          <button key={id} type="button" aria-pressed={subTab === id} onClick={() => setSubTab(id)}
            className={`min-h-11 rounded-xl px-3 py-2 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand ${subTab === id ? 'bg-[#382660] text-white' : 'bg-[#f7f4fc] text-[#382660] hover:bg-brand-soft'}`}>
            {label}{note && <span className="ms-1.5 text-xs font-medium">· {note}</span>}
          </button>
        ))}
      </div>
      <details className="rounded-2xl border border-[#e6e2f0] bg-white px-4 py-3 text-sm leading-7 text-slate-700">
        <summary className="cursor-pointer font-bold text-[#261b41]">قیمت عمده یعنی چه؟</summary>
        <p className="mt-2">قیمت عمده مبلغی است که برای فروش هر واحد از کالایت دریافت می‌کنی. قیمت فروش به مشتری جدا تعیین می‌شود.</p>
      </details>

      {subTab === 'active_products' && (
        <div className="space-y-2.5">
          {supplierProducts.length > 0 && (
            <div className="rounded-2xl border border-[#e6e2f0] bg-white p-4">
              <label className="block text-sm font-bold text-[#261b41]">جستجوی کالاها
                <input type="search" value={productQuery} onChange={event => setProductQuery(event.target.value)} placeholder="نام یا برند کالا" className="mt-1 block min-h-11 w-full rounded-xl border border-slate-300 px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand sm:max-w-md" />
              </label>
            </div>
          )}
          {(nullOriginDirectCount > 0 || bulkResult) && myWarehouses.some(w => w.isActive) && (
            <div className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-2">
              <p className="text-[11px] font-bold text-slate-700">مبدأ پیش‌فرض برای کالاهای بدون مبدأ</p>
              <p className="text-[10px] leading-5 text-slate-500">{nullOriginDirectCount > 0 ? `${toPersianDigits(nullOriginDirectCount)} کالا مبدأ ندارند و ویرایش موجودی‌شان بسته است؛ یک انبار را انتخاب کن تا برای همه‌شان مبدأ بگیرد.` : 'همه کالاهای ارسال مستقیم مبدأ دارند.'}</p>
              <div className="flex flex-wrap items-center gap-2">
                <select aria-label="انبار مبدأ کالاها" value={bulkWarehouseId} onChange={(e) => setBulkWarehouseId(e.target.value)} className="min-h-11 min-w-0 flex-1 rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-xs focus-visible:ring-2 focus-visible:ring-brand">
                  <option value="">انتخاب انبار…</option>
                  {myWarehouses.filter((w: any) => w.isActive).map((w: any) => (<option key={w.id} value={w.id}>{w.name} — {w.cityName}</option>))}
                </select>
                <button
                  type="button"
                  disabled={bulkSaving || nullOriginDirectCount === 0 || !bulkWarehouseId}
                  onClick={async () => {
                    if (bulkSaving || !bulkWarehouseId) return;
                    setBulkSaving(true);
                    setBulkResult(null);
                    try {
                      const res = await supplierApi.bulkAssignProductOrigin(bulkWarehouseId);
                      setBulkResult(res.assigned > 0 ? `${toPersianDigits(res.assigned)} کالا مبدأ گرفت.` : 'کالای جدیدی مبدأ نگرفت؛ همه مبدأ دارند.');
                      success('مبدأ کالاها تعیین شد.');
                      await productsResource.reload();
                    } catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'تعیین مبدأ انجام نشد.'); } finally { setBulkSaving(false); }
                  }}
                  className="min-h-11 rounded-xl bg-brand-deep px-3 text-xs font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
                >
                  تعیین مبدأ برای کالاهای بدون مبدأ
                </button>
              </div>
              {bulkResult && <p className="text-[10px] text-emerald-700">{bulkResult}</p>}
            </div>
          )}
          {!productsResource.loading && !productsResource.error && !showAddForm && supplierProducts.length === 0 && <SupplierEmptyState icon={<Package />} title="هنوز کالای تأییدشده‌ای نداری." description="کالا را پیشنهاد بده و نتیجهٔ بررسی را در بخش «پیشنهادهای کالا» پیگیری کن." />}
          {query && supplierProducts.length > 0 && visibleProducts.length === 0 && (
            <SupplierEmptyState icon={<Package />} title="کالایی با این نام یا برند پیدا نشد."
              action={<button type="button" onClick={() => setProductQuery('')} className="min-h-11 rounded-xl bg-brand-soft px-4 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن جستجو</button>} />
          )}
          {visibleProducts.map((product) => {
            const effectiveCost = product.supplierBaseCost;
            const effectiveStock = product.stock;
      const effectiveSupplyOption = product.supplyOption || '1day';
      const productActive = isSupplierProductActive(product);
      const originWarehouse = myWarehouses.find((w: any) => w.id === product.originSupplierWarehouseId);
      const originDisplay = resolveSupplierOriginDisplay(product, originWarehouse);

            return (
              <div
                key={product.id}
                className={`space-y-3 rounded-[22px] border border-[#e6e2f0] p-4 shadow-sm ${productActive ? 'bg-white' : 'bg-[#f8f6fc]'}`}
              >
                <div className="flex items-center gap-2.5">
                  {product.imageUrl ? (
                    <img
                      src={product.imageUrl}
                      alt="" width={48} height={48} loading="lazy"
                      className="w-12 h-12 rounded-xl object-contain bg-slate-50 border border-slate-100 p-1 shrink-0"
                    />
                  ) : (
                    <div className="w-12 h-12 rounded-xl bg-slate-50 border border-slate-100 flex items-center justify-center shrink-0" aria-label={product.title}>
                      <Package className="w-5 h-5 text-slate-300" strokeWidth={1.5} />
                    </div>
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-start justify-between gap-1.5">
                      <h4 className="min-w-0 break-words text-sm font-extrabold text-[#261b41]">
                        {product.title}
                      </h4>
                      <span className="flex items-center gap-1 shrink-0">
                        {pendingPriceProductIds.has(product.id) && (
                          <span className="text-[9px] bg-amber-50 text-amber-700 font-bold px-1.5 py-0.2 rounded-full border border-amber-200">
                            در حال بررسی
                          </span>
                        )}
                        <span className={productActive ? "text-[9px] bg-emerald-50 text-emerald-700 font-bold px-1.5 py-0.2 rounded-full border border-emerald-200" : "text-[9px] bg-slate-100 text-slate-500 font-bold px-1.5 py-0.2 rounded-full border border-slate-200"}>
                          {supplierProductStatusLabel(product)}
                        </span>
                      </span>
                    </div>
                    <div className="flex flex-wrap items-center gap-2 text-[10px] text-slate-500 mt-1">
                      <span>قیمت عمده تو: <strong className="text-slate-800">{formatPrice(effectiveCost)} تومان</strong></span>
                      <span>•</span>
                      <span>موجودی: <strong className="text-brand-deep">{toPersianDigits(effectiveStock)} عدد</strong></span>
                      <ProductUnitChip unitCount={sumProductUnitCount(unitCountsByProduct[product.id])} stock={effectiveStock} />
                      {product.brand ? (<><span>•</span><span>برند: <strong className="text-slate-800">{product.brand}</strong></span></>) : null}
                      {product.fulfillmentMethod ? (<><span>•</span><span>{product.fulfillmentMethod === 'supplier_direct' ? 'ارسال مستقیم از تو' : 'ارسال از انبار مرکزی'}</span></>) : null}
                      {product.fulfillmentMethod === 'supplier_direct' ? (<><span>•</span><span>مبدأ: {originDisplay}</span></>) : null}
                    </div>
                  </div>
                </div>

              
              {product.fulfillmentMethod === 'supplier_direct' && (originEditingProduct?.id === product.id ? (
                <div className="pt-2 border-t border-slate-100 flex items-center gap-2">
                  <select value={originWarehouseId} onChange={(e) => setOriginWarehouseId(e.target.value)} className="min-h-11 min-w-0 flex-1 rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-xs focus-visible:ring-2 focus-visible:ring-brand">
                    <option value="">— بدون مبدأ (برای ویرایش موجودی اول انبار انتخاب کن) —</option>
                    {myWarehouses.filter((w: any) => w.isActive).map((w: any) => (<option key={w.id} value={w.id}>{w.name} — {w.cityName}</option>))}
                  </select>
                  <button type="button" disabled={saving} onClick={async () => { if (saving) return; setSaving(true); try { await supplierApi.updateProductOrigin(product.id, originWarehouseId); success('مبدأ کالا تغییر کرد.'); setOriginEditingProduct(null); setStockOriginBlockedId(null); await productsResource.reload(); } catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'تغییر مبدأ انجام نشد.'); } finally { setSaving(false); } }} className="px-2.5 py-1.5 bg-brand-deep text-white font-bold text-[10px] rounded-lg">ذخیره مبدأ</button>
                  <button type="button" onClick={() => setOriginEditingProduct(null)} className="px-2 py-1.5 bg-slate-100 text-slate-600 font-bold text-[10px] rounded-lg">انصراف</button>
                </div>
              ) : (
                <div className="mt-1">
                  <button type="button" onClick={() => { setOriginEditingProduct(product); setOriginWarehouseId(product.originSupplierWarehouseId || ''); }} className="text-[10px] text-brand-deep font-bold hover:underline">تغییر مبدأ ارسال</button>
                </div>
              ))}
              {(product.reviewsCount ?? 0) > 0 && (
                <div className="flex items-center gap-1.5 text-[10px] text-slate-500 mt-1">
                  <Star className="w-3 h-3 text-amber-400 fill-amber-400" strokeWidth={1.5} />
                  <span><strong className="text-slate-800">{toPersianDigits(product.rating ?? 0)}</strong> از ۵ ({toPersianDigits(product.reviewsCount ?? 0)} نظر خریداران)</span>
                  <button type="button" onClick={() => setReviewsProduct(product)} className="text-brand-deep font-bold hover:underline">مشاهده نظرات</button>
                </div>
              )}
              {editingProduct?.id === product.id ? (
                <div className="pt-2 border-t border-slate-100 flex items-center gap-2">
                  <input
                    type="text" inputMode="numeric"
                    value={editedCostC}
                    onChange={(e) => setEditedCostC(e.target.value)}
                    placeholder="قیمت عمده جدید (تومان)"
                    className="flex-1 text-xs p-1.5 rounded-lg bg-slate-50 border border-slate-200 font-bold font-mono"
                  />
                  <button
                    type="button"
                    disabled={saving}
                    onClick={() => handleSaveWholesaleCost(product.id)}
                    className="px-2.5 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[10px] rounded-lg flex items-center gap-1"
                  >
                    <Save className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>ذخیره قیمت</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setEditingProduct(null)}
                    className="px-2 py-1.5 bg-slate-100 text-slate-600 font-bold text-[10px] rounded-lg"
                  >
                    انصراف
                  </button>
                </div>
              ) : (
                <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-[10px] text-slate-400">
                  <span>{product.supplierWarranty}</span>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {onPrepareShipment && (
                      <button type="button" onClick={() => onPrepareShipment(product.id)} className="rounded-md bg-brand-soft px-2 py-1 font-bold text-brand-deep">
                        افزودن به محموله
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => setGalleryProduct(product)}
                      className="text-brand-deep hover:text-brand-deep font-bold flex items-center gap-1 bg-brand-soft px-2 py-0.5 rounded-md"
                    >
                      <ImageIcon className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>گالری تصاویر</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => { invalidateUnitCounts(product.id); setVariantsProduct(product); }}
                      className="inline-flex min-h-10 items-center gap-1 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"
                    >
                      <Layers className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>مدل‌های کالا</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setEditingProduct(product);
                        setEditedCostC(effectiveCost.toString());
                      }}
                      className="inline-flex min-h-10 items-center gap-1 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"
                    >
                      <Edit3 className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>عوض‌کردن قیمت عمده</span>
                    </button>
                  </div>
                </div>
              )}

              {/* بخش ویرایش موجودی */}
              {canManageStock && (editingStockProduct?.id === product.id ? (
                <div className="pt-2 border-t border-slate-100 flex flex-wrap items-center gap-2">
                  {product.schemaVersion === 2 && <SupplierSKUSelect product={product} rows={stockVariants.rows} selectedId={stockVariantId}
                    label="تنوع برای اصلاح موجودی" loading={stockVariants.loading} error={stockVariants.error} retry={stockVariants.reload} disabled={saving}
                    onSelect={id => { setStockVariantId(id); const variant = stockVariants.rows.find(row => row.id === id); setEditedStock(variant ? String(variant.stock) : ''); }} />}
                  {(supplierStockNeedsOrigin(product) || stockOriginBlockedId === product.id) && (
                    <div className="w-full rounded-xl border border-amber-200 bg-amber-50 p-2.5 text-[11px] leading-5 text-amber-800">
                      <p>برای ویرایش موجودی، اول مبدأ ارسال این کالا را مشخص کن.</p>
                      <button type="button" onClick={() => { setOriginEditingProduct(product); setOriginWarehouseId(product.originSupplierWarehouseId || ''); }} className="mt-1 font-bold text-brand-deep hover:underline">تعیین مبدأ ارسال</button>
                    </div>
                  )}
                  <input
                    aria-label="موجودی کل قابل فروش" type="text" inputMode="numeric"
                    value={editedStock}
                    onChange={(e) => setEditedStock(e.target.value)}
                    placeholder="موجودی کل قابل فروش"
                    className="flex-1 text-xs p-1.5 rounded-lg bg-slate-50 border border-slate-200 font-bold font-mono"
                  />
                  <input aria-label="علت اصلاح موجودی" value={stockReason} onChange={event => setStockReason(event.target.value)} maxLength={500} placeholder="علت اصلاح موجودی" className="flex-1 text-xs p-1.5 rounded-lg bg-slate-50 border border-slate-200" />
                  <p className="w-full text-[10px] text-slate-500">این تعداد شامل موجودی قابل فروش در انبارهای دیگر هم هست.</p>
                  <button
                    type="button"
                    disabled={saving}
                    onClick={async () => {
                      const newStock = Number(editedStock.replace(/[۰-۹]/g, digit => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(digit))));
                      if (product.schemaVersion === 2 && (!stockVariantId || !stockVariants.rows.some(row => row.id === stockVariantId))) { error('تنوع دقیق کالا را انتخاب کنید.'); return; }
                      if (editedStock.trim() === '' || !Number.isSafeInteger(newStock) || newStock < 0 || newStock > 2147483647) {
                        error('موجودی باید عدد صحیح از صفر تا ۲۱۴۷۴۸۳۶۴۷ باشد.');
                        return;
                      }
                      if (!stockReason.trim()) { error('علت اصلاح موجودی را وارد کنید.'); return; }

                      if (supplierStockNeedsOrigin(product)) { setStockOriginBlockedId(product.id); return; }
                      if (saving || retiredPrintedCodes.length > 0) return;
                      setSaving(true);
                      try {
                        const response = await supplierApi.updateStock(product.id, newStock, stockReason.trim(), product.schemaVersion === 2 ? stockVariantId : undefined) as unknown as StockAdjustResponse;
                        if (response.delta !== 0) invalidateUnitCounts(product.id);
                        if (response.delta < 0) {
                          const codes = retiredPrintedCodesOf(response);
                          if (codes.length > 0) setRetiredPrintedCodes(codes);
                        }
                        success('موجودی ذخیره شد.');
                        setEditingStockProduct(null);
                        setStockOriginBlockedId(null);
                        setEditedStock('');
                        setStockReason('');
                        await productsResource.reload();
                      } catch (cause: unknown) {
                        if (isStockOriginRequiredError(cause)) {
                          setStockOriginBlockedId(product.id);
                        } else {
                          error(stockAdjustmentErrorMessage(cause, 'ذخیره موجودی انجام نشد.'));
                        }
                      }
                      finally { setSaving(false); }
                    }}
                    className="px-3 py-1.5 rounded-lg bg-green-600 text-white text-xs font-bold hover:bg-green-700"
                  >
                    ✓ ثبت موجودی
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setEditingStockProduct(null);
                      setStockOriginBlockedId(null);
                      setEditedStock('');
                      setStockReason('');
                    }}
                    className="px-2 py-1.5 rounded-lg bg-slate-200 text-slate-700 text-xs font-bold hover:bg-slate-300"
                  >
                    انصراف
                  </button>
                </div>
              ) : (
                <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
                  <span className="text-[10px] text-slate-600">
                    موجودی فعلی: <strong className="text-brand-deep">{toPersianDigits(effectiveStock)} عدد</strong>
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      setEditingStockProduct(product);
                      setStockOriginBlockedId(null);
                      setStockReason('');
                      setStockVariantId('');
                      setEditedStock(product.schemaVersion === 2 ? '' : effectiveStock.toString());
                    }}
                    className="text-green-600 hover:text-green-800 font-bold flex items-center gap-1 bg-green-50 px-2 py-0.5 rounded-md text-[10px]"
                  >
                    <Package className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>ویرایش موجودی</span>
                  </button>
                </div>
              ))}

              {editingSupplyProduct?.id === product.id ? (
                <div className="pt-2 border-t border-slate-100 space-y-2">
                  <div className="grid grid-cols-3 gap-1.5 text-[10px]">
                    {([['3hour_local', '۳ ساعته'], ['1day', '۱ روزه'], ['3day', '۳ روزه']] as const).map(([value, label]) => (
                      <label key={value} className={`cursor-pointer rounded-lg border p-2 text-center ${editedSupplyOption === value ? 'border-brand bg-brand-soft text-brand-deep' : 'border-slate-200 bg-slate-50 text-slate-600'}`}>
                        <input type="radio" name={`supply-${product.id}`} value={value} checked={editedSupplyOption === value} onChange={() => setEditedSupplyOption(value)} className="sr-only" />
                        {label}
                      </label>
                    ))}
                  </div>
                  <div className="flex gap-2">
                    <button type="button" disabled={saving} onClick={async () => { if (saving) return; setSaving(true); try { await supplierApi.updateSupplyOption(product.id, editedSupplyOption); success('سرعت آماده‌سازی ذخیره شد.'); setEditingSupplyProduct(null); await productsResource.reload(); } catch (e: any) { error(e.message || 'ذخیره انجام نشد.'); } finally { setSaving(false); } }} className="flex-1 rounded-lg bg-brand-deep text-white text-[10px] font-bold py-1.5">ذخیره</button>
                    <button type="button" onClick={() => setEditingSupplyProduct(null)} className="rounded-lg bg-slate-100 text-slate-600 text-[10px] font-bold px-3">انصراف</button>
                  </div>
                </div>
              ) : (
                <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
                  <span className="text-[10px] text-slate-600">آماده‌سازی سفارش: <strong className="text-brand-deep">{effectiveSupplyOption === '3hour_local' ? '۳ ساعته' : effectiveSupplyOption === '3day' ? '۳ روزه' : '۱ روزه'}</strong></span>
                  <button type="button" onClick={() => { setEditingSupplyProduct(product); setEditedSupplyOption(effectiveSupplyOption as '3hour_local' | '1day' | '3day'); }} className="text-brand-deep hover:text-brand-deep font-bold text-[10px] bg-brand-soft px-2 py-1 rounded-md">عوض‌کردن</button>
                </div>
              )}

            </div>
            );
          })}
        </div>
      )}

      {/* Sub-Tab 2: Proposed Product Offers Sent to Admin */}
      {subTab === 'proposed_offers' && (
        <div className="space-y-2.5">
          {rejectedProposals.length > 0 && (
            <div className="rounded-2xl border border-rose-200 bg-rose-50 p-3 flex items-center justify-between gap-2 text-xs">
              <p className="text-rose-800 font-bold leading-5">{toPersianDigits(rejectedProposals.length)} پیشنهاد رد شده — علت را بخوانید، ویرایش کنید و دوباره بفرستید.</p>
              <button type="button" onClick={() => scrollToProposal(String(rejectedProposals[0].id))} className="shrink-0 min-h-[36px] px-3 rounded-xl bg-rose-600 hover:bg-rose-700 text-white text-[11px] font-bold transition">مشاهده اولین ردشده</button>
            </div>
          )}
          {!proposalsResource.loading && !proposalsResource.error && (supplierProposedOffers.length === 0 ? (
            !showAddForm && <SupplierEmptyState icon={<Package />} title="هنوز پیشنهادی نفرستاده‌ای." description="با «پیشنهاد کالای جدید» شروع کن. وضعیت پیشنهادها و نتیجهٔ بررسی اینجا نمایش داده می‌شود." />
          ) : (
            supplierProposedOffers.map((prop) => (
              <div key={prop.id} id={`proposal-card-${prop.id}`} className={`bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2 text-xs transition-shadow ${highlightedProposalId === String(prop.id) ? 'ring-2 ring-rose-400 border-rose-300' : ''}`}>
                <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
                  <div>
                    <span className="font-bold text-slate-900">{prop.title}</span>
                    <span className="text-[10px] text-slate-400 block mt-0.5 font-mono">
                      {prop.id} • بارکد: {prop.barcode}
                    </span>
                  </div>
                  {prop.status === 'approved' ? (
                    <span className="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1">
                      <CheckCircle2 className="w-3 h-3 text-emerald-600"  strokeWidth={1.5}/>
                      <span>تأیید شده و در حال فروش</span>
                    </span>
                  ) : prop.status === 'rejected' ? (
                    <span className="text-[10px] bg-rose-50 text-rose-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1">
                      <XCircle className="w-3 h-3 text-rose-600"  strokeWidth={1.5}/>
                      <span>مدیر رد کرده</span>
                    </span>
                  ) : (
                    <span className="text-[10px] bg-amber-50 text-amber-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1">
                      <Clock className="w-3 h-3 text-amber-600"  strokeWidth={1.5}/>
                      <span>مدیر هنوز بررسی نکرده</span>
                    </span>
                  )}
                </div>

                <div className="flex justify-between text-[11px] text-slate-600">
                    <span>قیمت عمده‌ای که گفتی: <strong>{formatPrice(prop.supplierBaseCost || 0)} تومان</strong></span>
                    <span>تاریخ: {new Date(prop.created_at).toLocaleDateString('fa-IR')}</span>
                </div>

                {prop.rejection_reason && (
                  <div className="p-2 bg-rose-50 text-rose-800 rounded-xl text-[10px] flex items-center gap-1">
                    <AlertCircle className="w-3.5 h-3.5 text-rose-600 shrink-0"  strokeWidth={1.5}/>
                    <span>علت عدم تأیید: {prop.rejection_reason}</span>
                  </div>
                )}
                {(prop.status === 'rejected' || prop.status === 'pending') && (
                  <div className="space-y-1.5">
                    <button type="button" onClick={() => setFeedbackProposalId(String(prop.id))} className="w-full py-1.5 min-h-[40px] text-[11px] font-bold rounded-xl border border-violet-200 text-violet-700 hover:bg-violet-50 transition">دیدن نظر مدیر درباره هر بخش</button>
                    {prop.status === 'rejected' && (
                      <button type="button" onClick={() => setFeedbackProposalId(String(prop.id))} className="w-full py-1.5 min-h-[40px] text-[11px] font-bold rounded-xl bg-rose-600 hover:bg-rose-700 text-white transition">ویرایش و ارسال مجدد</button>
                    )}
                  </div>
                )}
              </div>
            ))
          ))}
        </div>
      )}

      {/* Sub-Tab 3: My Exclusivity Challenges */}
      {subTab === 'my_challenges' && (
        <div className="space-y-2.5">
          <SupplierResourceState loading={challenges.loading || (!challenges.data && !challenges.error)} error={challenges.error} retry={challenges.reload} />
          {!challenges.loading && !challenges.error && challenges.data && (challenges.data.length === 0 ? (
            <SupplierEmptyState icon={<ShieldCheck />} title="هنوز درخواست بررسی انحصار ثبت نکرده‌ای." description="اگر هنگام پیشنهاد کالا، گزینهٔ ثبت درخواست بررسی انحصار نمایش داده شد، نتیجهٔ درخواستت را اینجا ببین." />
          ) : (
            challenges.data.map((ch: any) => {
              const variantNames: string[] = Array.isArray(ch.variantNames) ? ch.variantNames : (typeof ch.variantNames === 'string' && ch.variantNames ? (() => { try { const parsed = JSON.parse(ch.variantNames); return Array.isArray(parsed) ? parsed : []; } catch { return []; } })() : []);
              return (
                <div key={ch.id} className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2 text-xs">
                  <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
                    <span className="font-bold text-slate-900">{ch.productTitle}</span>
                    {ch.status === 'approved' ? (
                      <span className="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1"><CheckCircle2 className="w-3 h-3" strokeWidth={1.5} /><span>تأیید شده</span></span>
                    ) : ch.status === 'rejected' ? (
                      <span className="text-[10px] bg-rose-50 text-rose-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1"><XCircle className="w-3 h-3" strokeWidth={1.5} /><span>رد شده</span></span>
                    ) : (
                      <span className="text-[10px] bg-amber-50 text-amber-700 font-bold px-2 py-0.5 rounded-full flex items-center gap-1"><Clock className="w-3 h-3" strokeWidth={1.5} /><span>در انتظار بررسی</span></span>
                    )}
                  </div>
                  <div className="space-y-1 text-[11px] text-slate-600">
                    <p>{ch.branch === 'same_variant' ? (<>شاخه: همین تنوع{ch.variantName ? (<> — «{ch.variantName}»</>) : null}</>) : (<>شاخه: تنوع‌های جدید{variantNames.length > 0 ? (<> — {variantNames.join('، ')}</>) : null}</>)}</p>
                    <p>قیمت پیشنهادی تو: <strong className="text-slate-800">{formatPrice(ch.proposedPrice)} تومان</strong></p>
                    <p>تاریخ ثبت: {new Date(ch.createdAt).toLocaleDateString('fa-IR')}</p>
                    {ch.status === 'approved' && <p className="text-emerald-700 leading-5">کارشناس درخواستت را تأیید کرده است؛ حالا می‌توانی همین کالا را از بخش «پیشنهاد کالای جدید» ثبت کنی. این تأیید فقط برای همین ترکیب کالا (و در شاخهٔ همین تنوع، برای همان تنوع) معتبر است.</p>}
                  </div>
                  {ch.status === 'rejected' && ch.rejectionReason && (
                    <div className="p-2 bg-rose-50 text-rose-800 rounded-xl text-[10px] flex items-center gap-1">
                      <AlertCircle className="w-3.5 h-3.5 text-rose-600 shrink-0"  strokeWidth={1.5}/>
                      <span>دلیل رد: {ch.rejectionReason}</span>
                    </div>
                  )}
                </div>
              );
            })
          ))}
        </div>
      )}
      <SupplierAddonsWidget ownProducts={supplierProducts.map(item => ({ id: item.id, title: item.title }))} />

      {galleryProduct && (
        <SupplierGalleryModal productId={galleryProduct.id} productTitle={galleryProduct.title} onClose={() => setGalleryProduct(null)} onChanged={() => productsResource.reload()} />
      )}
      {variantsProduct && (
        <SupplierVariantsModal key={variantsProduct.id} productId={variantsProduct.id} productTitle={variantsProduct.title} productBaseCost={variantsProduct.supplierBaseCost}
          onClose={() => setVariantsProduct(null)} onUnitCountsLoaded={storeUnitCounts}
          onChanged={() => { invalidateUnitCounts(variantsProduct.id); void productsResource.reload(); }} />
      )}
      {retiredPrintedCodes.length > 0 && <RetiredPrintedCodesPanel codes={retiredPrintedCodes} onAcknowledge={() => setRetiredPrintedCodes([])} />}
      {reviewsProduct && (
        <SupplierReviewsModal productId={reviewsProduct.id} productTitle={reviewsProduct.title} rating={reviewsProduct.rating ?? 0} reviewsCount={reviewsProduct.reviewsCount ?? 0} onClose={() => setReviewsProduct(null)} />
      )}
      {feedbackProposalId && (
        <SupplierFeedbackModal proposalId={feedbackProposalId} onClose={() => setFeedbackProposalId(null)} onResubmitted={() => proposalsResource.reload()} />
      )}
    </div>
  );
};
