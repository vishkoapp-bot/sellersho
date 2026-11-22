import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ProductFieldReview } from './ProductFieldReview';
import { apiRequest, isMockMode } from '../../services/apiClient';
import { adminApi } from '../../services/adminApi';
import { mapAdminProposal, AdminProposal } from '../../services/adminProposal';
import { useAuth } from '../../context/AuthContext';
import { useProducts } from '../../context/ProductContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { Package, Check, X, ShieldCheck, Tag, Search, Filter, Eye, AlertTriangle } from 'lucide-react';

export const AdminCatalogTab: React.FC = () => {
  const {
    products,
    proposedProducts: localProposals,
    approveAndMergeProduct,
    rejectProposedProduct,
    toggleProductOriginality,
    formatPrice,
    toPersianDigits,
    setActiveView,
  } = useStore();
  const { success, warning, error } = useToast();
  const {authToken} = useAuth();
  const {fetchProducts} = useProducts();
  const [serverProposals,setServerProposals] = useState<AdminProposal[]>([]);
  const [loading,setLoading] = useState(false);
  const [loadError,setLoadError] = useState('');
  const [reviewing,setReviewing] = useState(false);
  // D2 (Batch 4a): dedupe semantics — skip-first, abort-only-background.
  // inFlightRef holds the one in-flight proposals fetch (kind + controller).
  // A background (focus/interval) refresh skips while any fetch is in
  // flight, so it can never re-trigger the spinner over a settled state.
  // A user-initiated refresh aborts a stale *background* fetch only — it
  // never aborts the initial load or another user refresh. The in-flight
  // guard is primary; abort is only the supersede path. A generation id
  // keeps a superseded request from clearing the new request's loading.
  const inFlightRef = useRef<{ kind: 'background' | 'foreground'; controller: AbortController; id: number } | null>(null);
  const requestSeqRef = useRef(0);
  const lastFetchAtRef = useRef(0);
  const [hasLoaded,setHasLoaded] = useState(false);
  const refreshProposals = async (opts?: { background?: boolean }) => {
    if (isMockMode() || !authToken) return;
    const inFlight = inFlightRef.current;
    if (inFlight) {
      if (opts?.background) return;
      if (inFlight.kind !== 'background') return;
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
      setServerProposals(rows.map(mapAdminProposal));
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
  const [approvedIds, setApprovedIds] = useState<{ [key: string]: boolean }>({});
  const [rejectingItem, setRejectingItem] = useState<any | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  const [inspectingItem, setInspectingItem] = useState<any | null>(null);
  const [pendingOriginality, setPendingOriginality] = useState<any | null>(null);
  // Proposals saved/published from the field-review modal this session
  // (proposal id -> timestamp). Combined with the queue status below, this
  // marks products the admin has just published, so the catalog list can
  // highlight them as freshly approved.
  const [modalReviewedIds, setModalReviewedIds] = useState<{ [key: string]: number }>({});
  const [catalogSearch, setCatalogSearch] = useState('');
  const [catalogLimit, setCatalogLimit] = useState(10);
  // Pause the 30 s auto-refresh while a review modal / confirm dialog is open,
  // so the list never reorders under the reviewer.
  const modalOpenRef = useRef(false);
  useEffect(() => { modalOpenRef.current = !!inspectingItem || !!rejectingItem; }, [inspectingItem, rejectingItem]);
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
  const proposedProducts = isMockMode() ? localProposals : serverProposals;

  // Requirement 16: Multi-Filter Search Bar
  const [searchQuery, setSearchQuery] = useState('');
  const [supplierFilter, setSupplierFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState<'all' | 'pending' | 'approved'>('pending');

  const supplierOptions = Array.from(new Map(proposedProducts.map(p => [p.supplierId, p.supplierName])).entries());
  const filtersActive = searchQuery.trim() !== '' || supplierFilter !== 'all' || statusFilter !== 'pending';
  const clearFilters = () => { setSearchQuery(''); setSupplierFilter('all'); setStatusFilter('pending'); };

  const pendingApprovals = proposedProducts.filter((p) => {
    if (statusFilter === 'pending' && p.status !== 'pending') return false;
    if (statusFilter === 'approved' && p.status !== 'approved') return false;
    if (supplierFilter !== 'all' && p.supplierId !== supplierFilter) return false;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      const variantIds = ((p as AdminProposal).variants || []).map((v: any) => String(v.id ?? v.sku ?? '')).join(' ');
      return p.title.toLowerCase().includes(q) || (p.barcode || '').includes(q) || p.supplierName.toLowerCase().includes(q) || (p.category || '').toLowerCase().includes(q) || variantIds.toLowerCase().includes(q);
    }
    return true;
  });

  const review = async (item: any, approved: boolean, reason?: string) => {
    if (reviewing) return;
    setReviewing(true);
    try {
      if (isMockMode()) { if (approved) await approveAndMergeProduct(item); else rejectProposedProduct(item, reason || 'Rejected'); }
      else {
        await adminApi.reviewApproval(item.id, {status: approved ? 'approved' : 'rejected', ...(!approved ? {rejectionReason: reason} : {})});
        await refreshProposals(); if (approved) await fetchProducts().catch(()=>{});
      }
      if (approved) setApprovedIds(p=>({...p,[item.id]:true}));
      setInspectingItem(null);setRejectingItem(null);setRejectReason('');
      success(approved ? 'کالا تأیید شد.' : 'کالا رد شد.');
    } catch(e) { error(e instanceof Error ? e.message : 'ثبت نتیجه ناموفق بود.'); }
    finally {setReviewing(false);}
  };
  const handleOpenReview = (item: any) => { setInspectingItem(item); };
  const handleConfirmReject = () => {
    if (!rejectReason.trim()) { error('دلیل رد کالا را بنویسید تا تأمین‌کننده بداند چه چیزی را اصلاح کند.'); return; }
    if (rejectingItem) void review(rejectingItem, false, rejectReason.trim());
  };
  // A queue proposal and its catalog product have different ids; match on
  // the proposal's productId when the server sent one, else on exact title.
  const findCatalogProduct = (item: any) => products.find((p) => (item.productId && p.id === item.productId) || p.title === item.title);
  const viewCatalogProduct = (item: any) => {
    const product = findCatalogProduct(item);
    if (!product) { error('این کالا هنوز در فهرست کاتالوگ دیده نمی‌شود؛ «به‌روزرسانی صف بازبینی» را بزنید و دوباره تلاش کنید.'); return; }
    setActiveView('product_detail', { productId: product.id });
  };
  // Freshly approved = approved from this tab session (whole-product
  // approve, or a modal publish whose queue status has flipped to
  // approved), or approved within the last 24 h when the server reports
  // reviewedAt. No createdAt exists on products, so recency comes from
  // the proposal, never from an invented date.
  const isFreshlyApprovedProduct = (product: any) => proposedProducts.some((q: any) => {
    const matches = (q.productId && q.productId === product.id) || q.title === product.title;
    if (!matches) return false;
    if (approvedIds[q.id]) return true;
    if (modalReviewedIds[q.id] && q.status === 'approved') return true;
    if (q.status === 'approved' && q.reviewedAt) {
      const t = new Date(q.reviewedAt).getTime();
      return !Number.isNaN(t) && Date.now() - t < 24 * 3600 * 1000;
    }
    return false;
  });
  const handleModalUpdated = () => {
    if (inspectingItem) setModalReviewedIds((p) => ({ ...p, [inspectingItem.id]: Date.now() }));
    void refreshProposals();
    void fetchProducts().catch(() => {});
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
    <div className="space-y-3.5 pb-6" aria-busy={loading || reviewing}>
      <button type="button" onClick={()=>void refreshProposals()} disabled={loading} className="min-h-11 rounded-xl border px-3 py-2 text-xs font-bold disabled:opacity-40">به‌روزرسانی صف بازبینی</button>
      {loading && !hasLoaded && <p role="status" className="rounded-xl bg-slate-50 p-3 text-xs leading-7">در حال دریافت درخواست‌ها…</p>}
      {loading && hasLoaded && <p role="status" className="rounded-xl bg-slate-50 p-3 text-xs leading-7">در حال به‌روزرسانی…</p>}
      {loadError && <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs leading-7 text-rose-800"><p className="font-bold">بارگذاری صف بازبینی ناموفق بود.</p><p>{loadError}</p><button type="button" onClick={() => void refreshProposals()} className="min-h-11 rounded-lg bg-rose-600 px-4 py-2 text-xs font-bold text-white">تلاش مجدد</button></div>}
      {!loading && !loadError && pendingApprovals.length===0 && <div className="space-y-2 rounded-xl border border-slate-200 bg-white p-4 text-center"><p className="text-xs leading-7 text-slate-500">درخواستی با این فیلترها وجود ندارد.</p>{filtersActive && <button type="button" onClick={clearFilters} className="min-h-11 rounded-lg border px-4 py-2 text-xs font-bold">پاک کردن فیلترها</button>}</div>}

      {/* Requirement 16: Multi-Filter Search Bar */}
      <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-bold text-slate-800 flex items-center gap-1.5">
            <Filter className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
            <span>جستجو و فیلتر پیشرفته صف تأیید کالا</span>
          </h3>
          <span className="text-xs text-slate-400">
            {toPersianDigits(pendingApprovals.length)} مورد
          </span>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 text-xs">
          <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5">
            <Search className="w-3.5 h-3.5 text-slate-400 ml-1.5"  strokeWidth={1.5}/>
            <input
              type="text"
              placeholder="جستجوی نام، بارکد، دسته یا تأمین‌کننده..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full bg-transparent text-sm text-slate-800 placeholder-slate-400 focus:outline-hidden"
            />
          </div>

          <select
            value={supplierFilter}
            onChange={(e) => setSupplierFilter(e.target.value)}
            className="min-h-11 p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs"
            aria-label="فیلتر تأمین‌کننده"
          >
            <option value="all">همه تأمین‌کنندگان</option>
            {supplierOptions.map(([supplierId, supplierName]) => <option key={supplierId} value={supplierId}>{supplierName}</option>)}
          </select>

          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as any)}
            className="min-h-11 p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs"
            aria-label="فیلتر وضعیت"
          >
            <option value="all">همه وضعیت‌ها</option>
            <option value="pending">در انتظار بررسی</option>
            <option value="approved">تأییدشده</option>
          </select>
        </div>
      </div>

      {/* Pending Approval Queue */}
      <div className="bg-white rounded-2xl p-3.5 border border-blue-200 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Package className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-sm font-bold text-slate-800">صف کالاهای پیشنهادی تأمین‌کنندگان</h3>
          </div>
          <span className="text-[10px] bg-blue-100 text-blue-700 font-bold px-2 py-0.5 rounded-full">
            معیار بازبینی: تطبیق بارکد و کیفیت
          </span>
        </div>
        <p className="text-xs text-slate-500 leading-relaxed">
          بارکد کارخانه به تنهایی اثبات یکسان‌بودن مشخصات نیست؛ مدیر پلتفرم تطبیق مشخصات، گارانتی و کیفیت محتوا را بررسی و تأیید می‌کند (بخش ۴ پروپوزال).
        </p>

        <div className="space-y-2 pt-1">
          {pendingApprovals.map((item) => {
            const isApproved = approvedIds[item.id] || item.status === 'approved';
            const submitted = item.submittedAt || item.createdAt;
            const submittedLabel = submitted ? (() => { try { return new Date(submitted).toLocaleDateString('fa-IR'); } catch { return String(submitted); } })() : '';
            return (
              <div
                key={item.id}
                role="button"
                tabIndex={0}
                aria-label={`بازبینی ${item.title}`}
                onClick={() => setInspectingItem(item)}
                onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setInspectingItem(item); } }}
                className="p-3 bg-slate-50 hover:bg-blue-50/50 cursor-pointer rounded-xl border border-slate-200/80 space-y-2 transition-colors"
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-start gap-2 min-w-0">
                    {item.imageUrl ? <img src={item.imageUrl} alt={`تصویر ${item.title}`} className="w-12 h-12 shrink-0 rounded-lg object-cover border" /> : <Eye className="w-4 h-4 shrink-0 mt-1 text-blue-600"  strokeWidth={1.5}/>}
                    <h4 className="text-sm font-bold text-slate-800 leading-7">{item.title}</h4>
                  </div>
                  <span className="shrink-0 text-[10px] font-mono bg-white px-2 py-0.5 rounded border border-slate-200" dir="ltr">
                    {item.barcode}
                  </span>
                </div>

                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500">
                  <span>تأمین‌کننده: {item.supplierName}</span>
                  {item.category && <span>دسته: {item.category}</span>}
                  {((item as AdminProposal).variants?.length ?? 0) > 0 && <span>{toPersianDigits((item as AdminProposal).variants.length)} تنوع</span>}
                  {submittedLabel && <span>ثبت: {submittedLabel}</span>}
                  <span>قیمت عمده پیشنهادی: <strong className="text-slate-800">{formatPrice(item.proposedCost)} تومان</strong></span>
                </div>

                {isApproved ? (
                  <div className="space-y-2 p-2 bg-emerald-50 text-emerald-700 rounded-lg" onClick={(e) => e.stopPropagation()}>
                    <p className="text-xs font-bold text-center">
                      کالا به کاتالوگ مرکزی اضافه و برای تمام ویترین‌ها فعال شد.
                    </p>
                    <button
                      type="button"
                      onClick={() => viewCatalogProduct(item)}
                      className="w-full min-h-11 py-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-lg flex items-center justify-center gap-1"
                    >
                      <Eye className="w-4 h-4" strokeWidth={1.5}/>
                      <span>مشاهده در کاتالوگ</span>
                    </button>
                  </div>
                ) : (
                  <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-200" onClick={(e) => e.stopPropagation()}>
                    <button
                      disabled={reviewing} onClick={() => handleOpenReview(item)}
                      className="min-h-11 py-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-lg flex items-center justify-center gap-1 disabled:opacity-40"
                    >
                      <Check className="w-4 h-4"  strokeWidth={1.5}/>
                      <span>بازبینی فیلدها</span>
                    </button>
                    <button
                      disabled={reviewing} onClick={() => { setRejectReason(''); setRejectingItem(item); }}
                      className="min-h-11 py-2.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-xs rounded-lg flex items-center justify-center gap-1 disabled:opacity-40"
                    >
                      <X className="w-4 h-4"  strokeWidth={1.5}/>
                      <span>رد کالا</span>
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Active Catalog Overview with Originality Badges (Requirement 23) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between gap-2">
          <h4 className="text-sm font-bold text-slate-800">کاتالوگ فعال و اصالت کالاها (الزام حقوقی بج غیر اصل)</h4>
          <span className="shrink-0 text-xs text-slate-400">نمایش {toPersianDigits(visibleCatalog.length)} از {toPersianDigits(filteredCatalog.length)} محصول</span>
        </div>
        <p className="text-xs text-slate-500">
          برای کالاهای غیر اصل و های‌کپی، وضعیت را تعیین کنید تا بج «غیر اصل» در تمام ویترین‌ها درج شود. فهرست به‌ترتیب تازه‌ترین‌ها نمایش داده می‌شود؛ کالایی که همین حالا تأیید کرده‌اید با برچسب «تازه تأیید شده» مشخص است.
        </p>
        <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5">
          <Search className="w-3.5 h-3.5 text-slate-400 ml-1.5" strokeWidth={1.5}/>
          <input
            type="text"
            placeholder="جستجوی کالا در کاتالوگ (نام، دسته، تأمین‌کننده)..."
            value={catalogSearch}
            onChange={(e) => { setCatalogSearch(e.target.value); setCatalogLimit(10); }}
            aria-label="جستجوی کاتالوگ فعال"
            className="w-full bg-transparent text-sm text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
        </div>

        <div className="space-y-1.5 pt-1">
          {visibleCatalog.length === 0 && <p className="p-3 text-center text-xs text-slate-400">موردی با این جستجو در کاتالوگ نیست.</p>}
          {visibleCatalog.map((p) => {
            const isOrig = p.isOriginal !== false;
            const fresh = isFreshlyApprovedProduct(p);
            return (
              <div key={p.id} className={`p-2 border rounded-xl flex items-center justify-between text-xs ${fresh ? 'bg-emerald-50/60 border-emerald-300' : 'bg-slate-50 border-slate-100'}`}>
                <div className="flex items-center gap-2 min-w-0">
                  <img src={p.image} alt={p.title} className="w-7 h-7 rounded-lg object-cover border" />
                  <span className="font-bold text-slate-800 truncate max-w-[180px]">{p.title}</span>
                  {fresh && <span className="shrink-0 text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-600 text-white">تازه تأیید شده</span>}
                </div>

                <div className="flex items-center gap-2">
                  <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full ${
                    isOrig ? 'bg-emerald-50 text-emerald-700' : 'bg-amber-100 text-amber-800 font-black'
                  }`}>
                    {isOrig ? 'اصل' : 'غیر اصل (های‌کپی)'}
                  </span>
                  <button
                    type="button"
                    onClick={() => setActiveView('product_detail', { productId: p.id })}
                    aria-label={`مشاهده ${p.title} در کاتالوگ`}
                    title="مشاهده در کاتالوگ"
                    className="min-h-9 min-w-9 p-1.5 bg-white border border-slate-200 text-slate-600 rounded-lg"
                  >
                    <Eye className="w-4 h-4" strokeWidth={1.5}/>
                  </button>
                  <button
                    onClick={() => setPendingOriginality(p)}
                    className="min-h-9 px-2 py-1.5 bg-white border border-slate-200 text-slate-700 text-xs rounded-lg"
                  >
                    تغییر
                  </button>
                </div>
              </div>
            );
          })}
        </div>
        {filteredCatalog.length > visibleCatalog.length && (
          <div className="flex gap-2 pt-1">
            <button type="button" onClick={() => setCatalogLimit((n) => n + 10)} className="flex-1 min-h-11 rounded-xl border px-3 py-2 text-xs font-bold">نمایش {toPersianDigits(Math.min(10, filteredCatalog.length - visibleCatalog.length))} مورد بیشتر</button>
            <button type="button" onClick={() => setCatalogLimit(filteredCatalog.length)} className="flex-1 min-h-11 rounded-xl border px-3 py-2 text-xs font-bold">نمایش همه ({toPersianDigits(filteredCatalog.length)})</button>
          </div>
        )}
        {filteredCatalog.length > 10 && visibleCatalog.length === filteredCatalog.length && (
          <button type="button" onClick={() => setCatalogLimit(10)} className="w-full min-h-11 rounded-xl border px-3 py-2 text-xs font-bold">نمایش کمتر</button>
        )}
      </div>

      {/* Requirement 16: Proposed Product Details Modal */}
      {inspectingItem && <ProductFieldReview id={inspectingItem.id} onClose={()=>setInspectingItem(null)} onUpdated={handleModalUpdated} />}

      <ConfirmDialog
        isOpen={!!rejectingItem}
        title="رد کالا و عودت به تأمین‌کننده"
        message={`کالای «${rejectingItem?.title}» رد و برای اصلاح به تأمین‌کننده بازگردانده می‌شود؛ دلیل رد در اعلان داخل پنل تأمین‌کننده و فهرست پیشنهادهای او نمایش داده می‌شود.`}
        confirmText="تأیید رد کالا"
        isDanger={true}
        onConfirm={handleConfirmReject}
        onCancel={() => { setRejectingItem(null); setRejectReason(''); }}
      >
        <label className="block text-xs font-bold text-slate-700">دلیل رد — الزامی؛ همین متن در اعلان داخل پنل تأمین‌کننده و فهرست پیشنهادها نمایش داده می‌شود
          <textarea
            value={rejectReason}
            onChange={(e) => setRejectReason(e.target.value)}
            placeholder="مثلاً: تصویر اصلی کیفیت کافی ندارد؛ وزن کالا ثبت نشده است."
            rows={3}
            className="mt-1 w-full rounded-xl border border-slate-300 p-3 text-sm leading-7 font-normal focus:border-rose-400 outline-none"
          />
        </label>
      </ConfirmDialog>

      <ConfirmDialog
        isOpen={!!pendingOriginality}
        title="تغییر وضعیت اصالت کالا"
        message={pendingOriginality ? `وضعیت اصالت «${pendingOriginality.title}» به «${pendingOriginality.isOriginal !== false ? 'غیر اصل (های‌کپی)' : 'اصل'}» تغییر کند؟ این بج در تمام ویترین‌ها نمایش داده می‌شود.` : ''}
        confirmText="تأیید تغییر"
        onConfirm={() => {
          if (!pendingOriginality) return;
          const p = pendingOriginality;
          const isOrig = p.isOriginal !== false;
          toggleProductOriginality(p.id, !isOrig);
          success(`وضعیت کالای ${p.title} تغییر کرد.`);
          setPendingOriginality(null);
        }}
        onCancel={() => setPendingOriginality(null)}
      />
    </div>
  );
};
