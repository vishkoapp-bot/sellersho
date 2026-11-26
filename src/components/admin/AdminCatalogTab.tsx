import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ProductFieldReview } from './ProductFieldReview';
import { apiRequest, isMockMode } from '../../services/apiClient';
import { adminApi } from '../../services/adminApi';
import { mapAdminProposal, AdminProposal } from '../../services/adminProposal';
import { useAuth } from '../../context/AuthContext';
import { useProducts } from '../../context/ProductContext';
import type { Product, ProposedProduct } from '../../types';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { ArrowLeft, Eye, Package, RefreshCw, Search } from 'lucide-react';

type ProposalFilter = ProposedProduct['status'] | 'all';
type QueueProposal = ProposedProduct & Partial<Pick<AdminProposal, 'variants'>>;
const proposalStatus = {
  pending: { label: 'در انتظار بررسی', className: 'bg-amber-50 text-amber-900' },
  approved: { label: 'تأییدشده', className: 'bg-emerald-50 text-emerald-800' },
  rejected: { label: 'ردشده', className: 'bg-rose-50 text-rose-800' },
};
const secondaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-bold text-slate-700 hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';
const primaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand';
const filterControl = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-200 bg-white px-3 text-base text-slate-800 focus-visible:outline-2 focus-visible:outline-brand sm:text-sm';

export const AdminCatalogTab: React.FC = () => {
  const {
    products,
    proposedProducts: localProposals,
    rejectProposedProduct,
    toggleProductOriginality,
    formatPrice,
    toPersianDigits,
    setActiveView,
  } = useStore();
  const { success, error } = useToast();
  const {authToken} = useAuth();
  const {fetchProducts} = useProducts();
  const [serverProposals,setServerProposals] = useState<AdminProposal[]>([]);
  const [loading,setLoading] = useState(false);
  const [loadError,setLoadError] = useState('');
  // D2 (Batch 4a): dedupe semantics — skip-first, abort-only-background.
  // inFlightRef holds the one in-flight proposals fetch (kind + controller).
  // A background (focus/interval) refresh skips while any fetch is in
  // flight, so it can never re-trigger the spinner over a settled state.
  // A user-initiated refresh aborts a stale *background* fetch only — it
  // never aborts the initial load or another user refresh. A refresh after
  // a saved decision replaces older reads so they cannot restore stale status. A generation id
  // keeps a superseded request from clearing the new request's loading.
  const inFlightRef = useRef<{ kind: 'background' | 'foreground'; controller: AbortController; id: number } | null>(null);
  const requestSeqRef = useRef(0);
  const lastFetchAtRef = useRef(0);
  const [hasLoaded,setHasLoaded] = useState(false);
  const refreshProposals = async (opts?: { background?: boolean; afterDecision?: boolean }) => {
    if (isMockMode() || !authToken) return;
    const inFlight = inFlightRef.current;
    if (inFlight) {
      if (opts?.background) return;
      if (inFlight.kind !== 'background' && !opts?.afterDecision) return;
      inFlight.controller.abort();
      inFlightRef.current = null;
    }
    const kind = opts?.background ? 'background' : 'foreground';
    const controller = new AbortController();
    const id = ++requestSeqRef.current;
    inFlightRef.current = { kind, controller, id };
    lastFetchAtRef.current = Date.now();
    const startedAt = Date.now();
    setLoading(true);setLoadError('');
    try {
      const rows = await apiRequest<Record<string,any>[]>('/admin/products/proposals',{method:'GET',auth:true,signal: controller.signal});
      if (inFlightRef.current?.id !== id) return;
      setServerProposals(rows.map(row => ({
        ...mapAdminProposal(row),
        createdAt: row.created_at ?? row.createdAt,
        reviewedAt: row.reviewed_at ?? row.reviewedAt,
      })));
      setHasLoaded(true);
    }
    catch(e) {
      if (controller.signal.aborted) return;
      const elapsedSec = Math.max(0, Math.round((Date.now() - startedAt) / 1000));
      const msg = e instanceof Error ? e.message : 'Unable to load proposals';
      setLoadError(`${msg} (پس از ${toPersianDigits(elapsedSec)} ثانیه)`);
    }
    finally { if (inFlightRef.current?.id === id) { inFlightRef.current = null; setLoading(false); } }
  };
  const [inspectingItem, setInspectingItem] = useState<ProposedProduct | null>(null);
  const [pendingOriginality, setPendingOriginality] = useState<Product | null>(null);
  const [publishedIds, setPublishedIds] = useState<Set<string>>(() => new Set());
  const [catalogSearch, setCatalogSearch] = useState('');
  const [catalogLimit, setCatalogLimit] = useState(10);
  const queueHeading = useRef<HTMLHeadingElement>(null);
  // Pause the 30 s auto-refresh while a review modal / confirm dialog is open,
  // so the list never reorders under the reviewer.
  const modalOpenRef = useRef(false);
  useEffect(() => { modalOpenRef.current = !!inspectingItem || !!pendingOriginality; }, [inspectingItem, pendingOriginality]);
  useEffect(()=>{
    void refreshProposals();
    // D2: the 30 s interval is the refresh cadence; window-focus only
    // re-triggers when the last fetch started over 10 s ago, so refocus
    // bursts during a live session cannot restart the spinner.
    const onFocus=()=>{ if (modalOpenRef.current) return; if (Date.now() - lastFetchAtRef.current < 10000) return; void refreshProposals({background:true}); };
    window.addEventListener('focus',onFocus);
    const timer=window.setInterval(onFocus,30000);
    return ()=>{window.removeEventListener('focus',onFocus);window.clearInterval(timer);};
  },[authToken]);
  const proposedProducts: QueueProposal[] = isMockMode() ? localProposals : serverProposals;

  // Requirement 16: Multi-Filter Search Bar
  const [searchQuery, setSearchQuery] = useState('');
  const [supplierFilter, setSupplierFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState<ProposalFilter>('pending');

  const supplierOptions = Array.from(new Map(proposedProducts.map(p => [p.supplierId, p.supplierName])).entries());
  const filtersActive = searchQuery.trim() !== '' || supplierFilter !== 'all' || statusFilter !== 'pending';
  const clearFilters = () => { setSearchQuery(''); setSupplierFilter('all'); setStatusFilter('pending'); };

  const searchTerm = searchQuery.trim().toLowerCase();
  const matchingProposals = proposedProducts.filter((p) => {
    if (statusFilter !== 'all' && p.status !== statusFilter) return false;
    if (supplierFilter !== 'all' && p.supplierId !== supplierFilter) return false;
    if (!searchTerm) return true;
    const variantIds = (p.variants ?? []).map(v => String(v.id ?? v.sku ?? '')).join(' ');
    return [p.title, p.barcode, p.supplierName, p.category, variantIds].some(value => value?.toLowerCase().includes(searchTerm));
  });

  // A queue proposal and its catalog product have different ids; match on
  // the proposal's productId when the server sent one, else on exact title.
  const findCatalogProduct = (item: ProposedProduct) => products.find(p => (item.productId && p.id === item.productId) || p.title === item.title);
  const viewCatalogProduct = (item: ProposedProduct) => {
    const product = findCatalogProduct(item);
    if (!product) { error('این کالا هنوز در فهرست کاتالوگ دیده نمی‌شود؛ «به‌روزرسانی صف بازبینی» را بزنید و دوباره تلاش کنید.'); return; }
    setActiveView('product_detail', { productId: product.id });
  };
  const isFreshlyApprovedProduct = (product: Product) => proposedProducts.some(q => {
    const matches = (q.productId && q.productId === product.id) || q.title === product.title;
    if (!matches) return false;
    if (publishedIds.has(q.id)) return true;
    if (q.status === 'approved' && q.reviewedAt) {
      const t = new Date(q.reviewedAt).getTime();
      return !Number.isNaN(t) && Date.now() - t < 24 * 3600 * 1000;
    }
    return false;
  });
  const handleModalUpdated = (status?: ProposedProduct['status']) => {
    if (inspectingItem && status) {
      const { id } = inspectingItem;
      setServerProposals(rows => rows.map(row => row.id === id ? { ...row, status } : row));
      if (status === 'approved') setPublishedIds(ids => new Set(ids).add(id));
    }
    void refreshProposals({ afterDecision: true });
    void fetchProducts().catch(() => {});
  };
  const rejectProposal = async (item: ProposedProduct, reason: string) => {
    if (isMockMode()) rejectProposedProduct(item, reason);
    else await adminApi.reviewApproval(item.id, { status: 'rejected', rejectionReason: reason });
  };
  // Catalog list keeps the server's order (newest first: the API returns
  // the just-approved product first) and is only filtered by search.
  const filteredCatalog = catalogSearch.trim()
    ? products.filter((p) => {
        const q = catalogSearch.trim().toLowerCase();
        return p.title.toLowerCase().includes(q) || (p.category || '').toLowerCase().includes(q) || (p.supplierName || '').toLowerCase().includes(q);
      })
    : products;
  const visibleCatalog = filteredCatalog.slice(0, catalogLimit);

  return (
    <div className="space-y-4">
      <section aria-labelledby="admin-catalog-queue" className="space-y-4 rounded-2xl border border-slate-200 bg-white p-3 sm:p-5">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="min-w-0">
            <h2 id="admin-catalog-queue" ref={queueHeading} tabIndex={-1} className="text-base font-extrabold leading-7 text-slate-900 focus-visible:outline-brand">صف بازبینی کالا</h2>
            <p className="text-sm leading-6 text-slate-600">مشخصات، تصاویر، بارکد و گارانتی کالا را پیش از انتشار بررسی کنید.</p>
          </div>
          <button type="button" onClick={() => void refreshProposals()} disabled={loading} className={secondaryButton}>
            <RefreshCw aria-hidden="true" className="size-4 shrink-0" /> به‌روزرسانی صف بازبینی
          </button>
        </div>

        <div className="grid min-w-0 grid-cols-2 gap-2.5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)]">
          <label className="col-span-2 min-w-0 space-y-1 text-xs font-bold text-slate-700 lg:col-span-1">
            <span>جستجو</span>
            <span className="relative block">
              <Search aria-hidden="true" className="pointer-events-none absolute start-3 top-3.5 size-4 text-slate-500" />
              <input type="search" aria-label="جستجوی صف بازبینی" placeholder="نام کالا، بارکد یا تأمین‌کننده" value={searchQuery}
                onChange={event => setSearchQuery(event.target.value)} className={filterControl + ' ps-9'} />
            </span>
          </label>
          <label className="min-w-0 space-y-1 text-xs font-bold text-slate-700">
            <span>وضعیت</span>
            <select value={statusFilter} onChange={event => setStatusFilter(event.target.value as ProposalFilter)}
              className={filterControl} aria-label="فیلتر وضعیت">
              <option value="all">همه وضعیت‌ها</option>
              {Object.entries(proposalStatus).map(([status, details]) => <option key={status} value={status}>{details.label}</option>)}
            </select>
          </label>
          <label className="min-w-0 space-y-1 text-xs font-bold text-slate-700">
            <span>تأمین‌کننده</span>
            <select value={supplierFilter} onChange={event => setSupplierFilter(event.target.value)}
              className={filterControl} aria-label="فیلتر تأمین‌کننده">
              <option value="all">همه تأمین‌کنندگان</option>
              {supplierOptions.map(([supplierId, supplierName]) => <option key={supplierId} value={supplierId}>{supplierName}</option>)}
            </select>
          </label>
        </div>

        <div className="flex min-h-11 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-slate-200 pt-2">
          <p role="status" className="text-sm leading-6 text-slate-600">
            {loading ? hasLoaded ? 'در حال به‌روزرسانی…' : 'در حال دریافت درخواست‌ها…' : (!loadError || hasLoaded) && <>{toPersianDigits(matchingProposals.length)} پیشنهاد</>}
          </p>
          {filtersActive && <button type="button" onClick={clearFilters}
            className="min-h-11 rounded-xl px-2 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand">پاک کردن فیلترها</button>}
        </div>
        {loadError && (
          <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm leading-7 text-rose-900">
            <p className="font-bold">بارگذاری صف بازبینی ناموفق بود.</p>
            <p>{loadError}</p>
            <button type="button" onClick={() => void refreshProposals()} className={secondaryButton}>تلاش مجدد</button>
          </div>
        )}
        {!loading && !loadError && matchingProposals.length === 0 &&
          <p className="rounded-xl bg-slate-50 p-4 text-center text-sm leading-7 text-slate-600">درخواستی با این فیلترها وجود ندارد.</p>}

        <div className="space-y-2.5">
          {matchingProposals.map(item => {
            const status = proposalStatus[item.status] ?? proposalStatus.pending;
            const submittedAt = item.submittedAt ?? item.createdAt;
            const variants = item.variants?.length ?? 0;
            const reviewLabel = item.status === 'approved' ? 'مشاهده بازبینی' : 'بازبینی';
            return (
              <article key={item.id} className="min-w-0 space-y-3 rounded-xl border border-slate-200 bg-slate-50/70 p-3 sm:flex sm:items-start sm:justify-between sm:gap-4 sm:space-y-0">
                <div className="flex min-w-0 items-start gap-3">
                  {item.imageUrl
                    ? <img src={item.imageUrl} alt="" loading="lazy" className="size-12 shrink-0 rounded-xl border border-slate-200 bg-white object-cover" />
                    : <span className="grid size-12 shrink-0 place-items-center rounded-xl border border-brand-border bg-brand-bg text-brand-deep"><Package aria-hidden="true" className="size-5" /></span>}
                  <div className="min-w-0 space-y-1">
                    <h3 className="font-extrabold leading-6 [overflow-wrap:anywhere]">{item.title}</h3>
                    <p className="text-sm leading-6 text-slate-600 [overflow-wrap:anywhere]">{item.supplierName}{item.category && <> · {item.category}</>}</p>
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs leading-6 text-slate-600">
                      {item.barcode && <span>بارکد <bdi className="[overflow-wrap:anywhere]" dir="ltr">{item.barcode}</bdi></span>}
                      {variants > 0 && <span>{toPersianDigits(variants)} تنوع</span>}
                      {submittedAt && <time dateTime={submittedAt}>{new Date(submittedAt).toLocaleDateString('fa-IR')}</time>}
                      <span>قیمت عمده <strong className="text-slate-800">{formatPrice(item.proposedCost)} تومان</strong></span>
                    </div>
                  </div>
                </div>
                <div className="flex flex-col items-stretch gap-2 sm:shrink-0 sm:items-end">
                  <span className={'self-start rounded-full px-2.5 py-1 text-xs font-bold sm:self-auto ' + status.className}>{status.label}</span>
                  <button type="button" aria-label={reviewLabel + ' ' + item.title} onClick={() => setInspectingItem(item)}
                    className={item.status === 'pending' ? primaryButton : secondaryButton}>
                    {reviewLabel} <ArrowLeft aria-hidden="true" className="size-4 shrink-0" />
                  </button>
                  {item.status === 'approved' && <button type="button" onClick={() => viewCatalogProduct(item)} className={secondaryButton}>مشاهده در کاتالوگ</button>}
                </div>
              </article>
            );
          })}
        </div>
      </section>

      <section aria-labelledby="admin-active-catalog" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-3 sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id="admin-active-catalog" className="text-base font-extrabold text-slate-900">کاتالوگ فعال</h2>
          <span className="text-xs text-slate-600">نمایش {toPersianDigits(visibleCatalog.length)} از {toPersianDigits(filteredCatalog.length)} کالا</span>
        </div>
        <p className="text-sm leading-6 text-slate-600">وضعیت اصالت کالا در همهٔ ویترین‌ها نمایش داده می‌شود.</p>
        <div className="relative">
          <Search aria-hidden="true" className="pointer-events-none absolute start-3 top-3.5 size-4 text-slate-500" />
          <input type="search" aria-label="جستجوی کاتالوگ فعال" placeholder="نام کالا، دسته یا تأمین‌کننده" value={catalogSearch}
            onChange={event => { setCatalogSearch(event.target.value); setCatalogLimit(10); }} className={filterControl + ' ps-9'} />
        </div>

        <div className="space-y-2">
          {visibleCatalog.length === 0 && <p className="rounded-xl bg-slate-50 p-4 text-center text-sm text-slate-600">موردی با این جستجو در کاتالوگ نیست.</p>}
          {visibleCatalog.map(product => {
            const original = product.isOriginal !== false;
            const fresh = isFreshlyApprovedProduct(product);
            return (
              <article key={product.id} className={'flex min-w-0 flex-wrap items-center justify-between gap-3 rounded-xl border p-3 ' + (fresh ? 'border-emerald-200 bg-emerald-50/50' : 'border-slate-200 bg-slate-50/70')}>
                <div className="flex min-w-0 flex-1 items-center gap-2.5">
                  <img src={product.image} alt="" loading="lazy" className="size-10 shrink-0 rounded-lg border border-slate-200 bg-white object-cover" />
                  <div className="min-w-0">
                    <h3 className="text-sm font-bold leading-6 [overflow-wrap:anywhere]">{product.title}</h3>
                    <div className="mt-1 flex flex-wrap gap-1.5">
                      <span className={'rounded-full px-2 py-0.5 text-xs font-bold ' + (original ? 'bg-emerald-50 text-emerald-800' : 'bg-amber-100 text-amber-900')}>
                        {original ? 'اصل' : 'غیر اصل (های‌کپی)'}
                      </span>
                      {fresh && <span className="rounded-full bg-emerald-700 px-2 py-0.5 text-xs font-bold text-white">تازه تأیید شده</span>}
                    </div>
                  </div>
                </div>
                <div className="flex w-full flex-wrap gap-2 sm:w-auto">
                  <button type="button" onClick={() => setActiveView('product_detail', { productId: product.id })}
                    aria-label={'مشاهده ' + product.title + ' در کاتالوگ'} className={secondaryButton}>
                    <Eye aria-hidden="true" className="size-4 shrink-0" /> مشاهده
                  </button>
                  <button type="button" onClick={() => setPendingOriginality(product)} className={secondaryButton}>تغییر اصالت</button>
                </div>
              </article>
            );
          })}
        </div>
        {filteredCatalog.length > visibleCatalog.length && (
          <div className="flex flex-wrap gap-2">
            <button type="button" onClick={() => setCatalogLimit(limit => limit + 10)} className={secondaryButton}>نمایش {toPersianDigits(Math.min(10, filteredCatalog.length - visibleCatalog.length))} مورد بیشتر</button>
            <button type="button" onClick={() => setCatalogLimit(filteredCatalog.length)} className={secondaryButton}>نمایش همه ({toPersianDigits(filteredCatalog.length)})</button>
          </div>
        )}
        {filteredCatalog.length > 10 && visibleCatalog.length === filteredCatalog.length &&
          <button type="button" onClick={() => setCatalogLimit(10)} className={secondaryButton}>نمایش کمتر</button>}
      </section>

      {inspectingItem && <ProductFieldReview id={inspectingItem.id} initialTitle={inspectingItem.title}
        onClose={status => {
          setInspectingItem(null);
          if ((status && statusFilter !== 'all' && status !== statusFilter) || !matchingProposals.some(item => item.id === inspectingItem.id)) {
            requestAnimationFrame(() => queueHeading.current?.focus({ preventScroll: true }));
          }
        }}
        onUpdated={handleModalUpdated}
        onRejectProduct={inspectingItem.status === 'pending' ? reason => rejectProposal(inspectingItem, reason) : undefined} />}

      {pendingOriginality && (
        <SupplierDialog titleId="admin-originality-title" onDismiss={() => setPendingOriginality(null)}>
          <SupplierDialogHeading id="admin-originality-title" title="تغییر وضعیت اصالت کالا" onClose={() => setPendingOriginality(null)} />
          <p className="text-sm leading-7 text-slate-700">وضعیت اصالت «{pendingOriginality.title}» به «{pendingOriginality.isOriginal !== false ? 'غیر اصل (های‌کپی)' : 'اصل'}» تغییر کند؟ این وضعیت در همهٔ ویترین‌ها نمایش داده می‌شود.</p>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" onClick={() => {
              toggleProductOriginality(pendingOriginality.id, pendingOriginality.isOriginal === false);
              success('وضعیت اصالت کالا تغییر کرد.');
              setPendingOriginality(null);
            }} className={primaryButton}>تأیید تغییر</button>
            <button type="button" onClick={() => setPendingOriginality(null)} className={secondaryButton}>انصراف</button>
          </div>
        </SupplierDialog>
      )}
    </div>
  );
};
