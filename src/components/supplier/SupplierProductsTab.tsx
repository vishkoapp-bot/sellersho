import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi, SupplierProductDto, SupplierPriceIncreaseDto, WarrantyPropertyDto } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { Package, Plus, CheckCircle2, Clock, ShieldCheck, Edit3, XCircle, AlertCircle, Save, Upload, Trash2, Star, Layers, Image as ImageIcon } from 'lucide-react';
import { SupplierGalleryModal } from './SupplierGalleryModal';
import { SupplierVariantsModal } from './SupplierVariantsModal';
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

export const SupplierProductsTab: React.FC<{ focusProposalId?: string | null; onFocusConsumed?: () => void }> = ({ focusProposalId, onFocusConsumed }) => {
  const {
    supplierSession,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error, warning } = useToast();
  const productsResource = useSupplierResource(supplierApi.getProducts);
  const proposalsResource = useSupplierResource(supplierApi.getProposals);
  const [saving, setSaving] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [subTab, setSubTab] = useState<'active_products' | 'proposed_offers' | 'my_challenges'>('active_products');
  const [challenges, setChallenges] = useState<any[]>([]);
  const [challengesLoading, setChallengesLoading] = useState(false);
  const [challengesLoaded, setChallengesLoaded] = useState(false);
  const [myWarehouses, setMyWarehouses] = useState<any[]>([]);
  const [profileCity, setProfileCity] = useState('');
  const [profileCityConfirmed, setProfileCityConfirmed] = useState(false);
  const [originEditingProduct, setOriginEditingProduct] = useState<SupplierProductDto | null>(null);
  const [galleryProduct, setGalleryProduct] = useState<SupplierProductDto | null>(null);
  const [variantsProduct, setVariantsProduct] = useState<SupplierProductDto | null>(null);
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
  useEffect(() => {
    supplierApi.getMyWarehouses().then(setMyWarehouses).catch(() => {});
    supplierApi.getProfile().then((pr: any) => { setProfileCity(pr.warehouse_city || pr.warehouseCity || ''); setProfileCityConfirmed(!!(pr.warehouse_city_confirmed || pr.warehouseCityConfirmed)); }).catch(() => {});
  }, []);
  useEffect(() => {
    if (subTab !== 'my_challenges' || challengesLoaded) return;
    setChallengesLoading(true);
    supplierApi.getMyExclusivityChallenges().then((list) => { setChallenges(list); setChallengesLoaded(true); }).catch(() => { setChallengesLoaded(true); }).finally(() => setChallengesLoading(false));
  }, [subTab, challengesLoaded]);
  const [warranties, setWarranties] = useState<WarrantyPropertyDto[]>([]);

  const [editingProduct, setEditingProduct] = useState<SupplierProductDto | null>(null);
  const [editedCostC, setEditedCostC] = useState('');

  // state های جدید برای ویرایش موجودی و همگام‌سازی لحظه‌ای
  const [editingStockProduct, setEditingStockProduct] = useState<SupplierProductDto | null>(null);
  const [editedStock, setEditedStock] = useState('');
  const [stockVariantId, setStockVariantId] = useState('');
  const stockVariants = useSupplierVariants(editingStockProduct);
  const [editingSupplyProduct, setEditingSupplyProduct] = useState<SupplierProductDto | null>(null);
  const [editedSupplyOption, setEditedSupplyOption] = useState<'3hour_local' | '1day' | '3day'>('1day');
  const supplierProducts = productsResource.error ? [] : productsResource.data || [];
  const supplierProposedOffers = proposalsResource.error ? [] : proposalsResource.data || [];
  const rejectedProposals = supplierProposedOffers.filter((prop) => prop.status === 'rejected');
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
    <div className="space-y-3.5 pb-6">
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">
        <p className="font-bold text-slate-700">قیمت عمده یعنی چه؟</p>
        <p>کالای تازه را با دکمه «پیشنهاد کالای جدید» بفرست؛ مدیر اول بررسی می‌کند و اگر تأیید شود، کالایت به فروش می‌رسد. قیمت عمده همان پولی است که پلتفرم برای هر دانه از کالایت به تو می‌دهد. قیمت فروش به مشتری جدا حساب می‌شود و لازم نیست تو تعیینش کنی.</p>
        <p className="text-slate-500">مثال: قیمت عمده را ۱۰۰ هزار تومان می‌گذاری؛ هر دانه که فروش برود، ۱۰۰ هزار تومان به حساب تو می‌آید.</p>
      </div>
      <SupplierResourceState loading={productsResource.loading || proposalsResource.loading} error={productsResource.error || proposalsResource.error} retry={() => { void productsResource.reload(); void proposalsResource.reload(); }} />
      {/* FIX 50 — live addons widget (Ql), above the products header. */}
      <SupplierAddonsWidget ownProducts={supplierProducts.map((item) => ({ id: item.id, title: item.title }))} />
      {/* Header and Add Offer */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-xs font-bold text-slate-800">کالاهای تو</h3>
          <span className="text-[10px] text-slate-400 block mt-0.5">
            پیشنهادهای تازه و کالاهای تأییدشده
          </span>
        </div>
        <button
          onClick={() => setShowAddForm(!showAddForm)}
          className="flex items-center gap-1 bg-brand-deep text-white text-[11px] font-bold min-h-[44px] px-3.5 rounded-xl shadow-xs"
        >
          <Plus className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>پیشنهاد کالای جدید</span>
        </button>
      </div>

      {/* Navigation Sub-Tabs */}
      <div className="flex bg-slate-100 p-1 rounded-xl text-xs font-bold gap-1">
        <button
          type="button"
          onClick={() => setSubTab('active_products')}
          className={`flex-1 min-h-[44px] px-2 rounded-lg transition-all ${
            subTab === 'active_products' ? 'bg-white text-brand-deep shadow-2xs' : 'text-slate-600'
          }`}
        >
          کالاهای تأییدشده ({toPersianDigits(supplierProducts.length)})
        </button>
        <button
          type="button"
          onClick={() => setSubTab('proposed_offers')}
          className={`flex-1 min-h-[44px] px-2 rounded-lg transition-all ${
            subTab === 'proposed_offers' ? 'bg-white text-brand-deep shadow-2xs' : 'text-slate-600'
          }`}
        >
        پیشنهادهای در انتظار بررسی ({toPersianDigits(supplierProposedOffers.length)})
        </button>
        <button
          type="button"
          onClick={() => setSubTab('my_challenges')}
          className={`flex-1 min-h-[44px] px-2 rounded-lg transition-all flex items-center justify-center ${
            subTab === 'my_challenges' ? 'bg-white text-brand-deep shadow-2xs' : 'text-slate-600'
          }`}
        >
          درخواست‌های من
        </button>
      </div>

      {showAddForm && (
        <SupplierProposeForm
          warranties={warranties}
          myWarehouses={myWarehouses}
          profileCity={profileCity}
          profileCityConfirmed={profileCityConfirmed}
          sessionToken={supplierSession?.token}
          onSubmitted={async () => { setShowAddForm(false); setSubTab('proposed_offers'); await proposalsResource.reload(); await productsResource.reload(); }}
          onChallengeSubmitted={() => { setShowAddForm(false); setSubTab('my_challenges'); }}
        />
      )}

      {subTab === 'active_products' && (
        <div className="space-y-2.5">
          {!productsResource.loading && !productsResource.error && supplierProducts.length === 0 && <div className="rounded-2xl border bg-white p-6 text-center space-y-2">
            <Package className="w-10 h-10 text-slate-300 mx-auto"  strokeWidth={1.5}/>
            <p className="text-xs font-bold text-slate-700">هنوز کالای تأییدشده‌ای نداری.</p>
            <p className="text-[11px] leading-5 text-slate-400">اول کالایت را پیشنهاد بده؛ مدیر بررسی می‌کند و وقتی تأیید شد، اینجا می‌آید و به فروش می‌رسد.</p>
            <button type="button" onClick={() => setShowAddForm(true)} className="min-h-[44px] px-4 rounded-xl bg-brand-deep text-white text-xs font-bold">پیشنهاد کالای جدید</button>
          </div>}
          {supplierProducts.map((product) => {
            const effectiveCost = product.supplierBaseCost;
            const effectiveStock = product.stock;
      const effectiveSupplyOption = product.supplyOption || '1day';
      const originWarehouse = myWarehouses.find((w: any) => w.id === product.originSupplierWarehouseId);
      const originDisplay = originWarehouse ? `${originWarehouse.name} — ${originWarehouse.cityName}` : product.originSupplierWarehouseId ? 'انبار غیرفعال‌شده' : profileCityConfirmed && profileCity ? `شهر انبار پروفایل (${profileCity})` : 'انبار تأمین‌کننده';

            return (
              <div
                key={product.id}
                className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2"
              >
                <div className="flex items-center gap-2.5">
                  {product.imageUrl ? (
                    <img
                      src={product.imageUrl}
                      alt={product.title}
                      className="w-12 h-12 rounded-xl object-contain bg-slate-50 border border-slate-100 p-1 shrink-0"
                    />
                  ) : (
                    <div className="w-12 h-12 rounded-xl bg-slate-50 border border-slate-100 flex items-center justify-center shrink-0" aria-label={product.title}>
                      <Package className="w-5 h-5 text-slate-300" strokeWidth={1.5} />
                    </div>
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between">
                      <h4 className="text-xs font-bold text-slate-800 truncate">
                        {product.title}
                      </h4>
                      <span className="flex items-center gap-1 shrink-0">
                        {pendingPriceProductIds.has(product.id) && (
                          <span className="text-[9px] bg-amber-50 text-amber-700 font-bold px-1.5 py-0.2 rounded-full border border-amber-200">
                            در حال بررسی
                          </span>
                        )}
                        <span className="text-[9px] bg-emerald-50 text-emerald-700 font-bold px-1.5 py-0.2 rounded-full border border-emerald-200">
                          تأییدشده
                        </span>
                      </span>
                    </div>
                    <div className="flex items-center gap-2 text-[10px] text-slate-500 mt-1">
                      <span>قیمت عمده تو: <strong className="text-slate-800">{formatPrice(effectiveCost)} تومان</strong></span>
                      <span>•</span>
                      <span>موجودی: <strong className="text-brand-deep">{toPersianDigits(effectiveStock)} عدد</strong></span>
                      {product.brand ? (<><span>•</span><span>برند: <strong className="text-slate-800">{product.brand}</strong></span></>) : null}
                      {product.fulfillmentMethod ? (<><span>•</span><span>{product.fulfillmentMethod === 'supplier_direct' ? 'ارسال مستقیم از تو' : 'ارسال از انبار مرکزی'}</span></>) : null}
                      {product.fulfillmentMethod === 'supplier_direct' ? (<><span>•</span><span>مبدأ: {originDisplay}</span></>) : null}
                    </div>
                  </div>
                </div>

              
              {product.fulfillmentMethod === 'supplier_direct' && (originEditingProduct?.id === product.id ? (
                <div className="pt-2 border-t border-slate-100 flex items-center gap-2">
                  <select value={originWarehouseId} onChange={(e) => setOriginWarehouseId(e.target.value)} className="flex-1 text-[10px] p-1.5 rounded-lg bg-slate-50 border border-slate-200 outline-none">
                    {profileCityConfirmed ? (<option value="">شهر انبار پروفایل{profileCity ? ` (${profileCity})` : ''}</option>) : (<option value="">— بدون مبدأ مشخص —</option>)}
                    {myWarehouses.filter((w: any) => w.isActive).map((w: any) => (<option key={w.id} value={w.id}>{w.name} — {w.cityName}</option>))}
                  </select>
                  <button type="button" disabled={saving} onClick={async () => { if (saving) return; setSaving(true); try { await supplierApi.updateProductOrigin(product.id, originWarehouseId); success('مبدأ کالا تغییر کرد.'); setOriginEditingProduct(null); await productsResource.reload(); } catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'تغییر مبدأ انجام نشد.'); } finally { setSaving(false); } }} className="px-2.5 py-1.5 bg-brand-deep text-white font-bold text-[10px] rounded-lg">ذخیره مبدأ</button>
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
                  <div className="flex items-center gap-1.5">
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
                      onClick={() => setVariantsProduct(product)}
                      className="text-brand-deep hover:text-brand-deep font-bold flex items-center gap-1 bg-brand-soft px-2 py-1 rounded-md"
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
                      className="text-brand-deep hover:text-brand-deep font-bold flex items-center gap-1 bg-brand-soft px-2 py-1 rounded-md"
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
                  <input
                    aria-label="موجودی جدید کالا" type="text" inputMode="numeric"
                    value={editedStock}
                    onChange={(e) => setEditedStock(e.target.value)}
                    placeholder="موجودی جدید"
                    className="flex-1 text-xs p-1.5 rounded-lg bg-slate-50 border border-slate-200 font-bold font-mono"
                  />
                  <button
                    type="button"
                    disabled={saving}
                    onClick={async () => {
                      const newStock = Number(editedStock.replace(/[۰-۹]/g, digit => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(digit))));
                      if (product.schemaVersion === 2 && (!stockVariantId || !stockVariants.rows.some(row => row.id === stockVariantId))) { error('تنوع دقیق کالا را انتخاب کنید.'); return; }
                      if (editedStock.trim() === '' || !Number.isSafeInteger(newStock) || newStock < 0) {
                        error('موجودی باید عدد مثبت باشد.');
                        return;
                      }

                      if (saving) return;
                      setSaving(true);
                      try { await supplierApi.updateStock(product.id, newStock, undefined, product.schemaVersion === 2 ? stockVariantId : undefined); success('موجودی ذخیره شد.'); setEditingStockProduct(null); setEditedStock(''); await productsResource.reload(); }
                      catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'ذخیره موجودی انجام نشد.'); }
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
                      setEditedStock('');
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
          {supplierProposedOffers.length === 0 ? (
            <div className="bg-white rounded-2xl p-6 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">هنوز پیشنهادی نفرستاده‌ای.</p>
              <p className="text-[11px] leading-5 text-slate-400">با دکمه «پیشنهاد کالای جدید» بالای همین صفحه شروع کن. پیشنهادت اینجا می‌آید و وضعیتش را می‌بینی: در انتظار بررسی، تأییدشده یا ردشده.</p>
            </div>
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
          )}
        </div>
      )}

      {/* Sub-Tab 3: My Exclusivity Challenges */}
      {subTab === 'my_challenges' && (
        <div className="space-y-2.5">
          {challengesLoading ? (
            <div className="bg-white rounded-2xl p-6 text-center text-xs text-slate-400">در حال دریافت درخواست‌ها…</div>
          ) : challenges.length === 0 ? (
            <div className="bg-white rounded-2xl p-6 text-center space-y-1.5">
              <ShieldCheck className="w-8 h-8 mx-auto text-slate-300" strokeWidth={1.5} />
              <p className="text-xs font-bold text-slate-600">هنوز درخواست چالشی ثبت نکرده‌ای.</p>
              <p className="text-[11px] leading-5 text-slate-400">وقتی پیشنهاد کالایی به‌خاطر ثبت قبلی همان کالا توسط تأمین‌کننده‌ای دیگر رد شود، همان‌جا گزینهٔ ثبت چالش نمایش داده می‌شود و درخواستت اینجا قرار می‌گیرد.</p>
            </div>
          ) : (
            challenges.map((ch: any) => {
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
          )}
        </div>
      )}
      {galleryProduct && (
        <SupplierGalleryModal productId={galleryProduct.id} productTitle={galleryProduct.title} onClose={() => setGalleryProduct(null)} onChanged={() => productsResource.reload()} />
      )}
      {variantsProduct && (
        <SupplierVariantsModal productId={variantsProduct.id} productTitle={variantsProduct.title} productBaseCost={variantsProduct.supplierBaseCost} onClose={() => setVariantsProduct(null)} onChanged={() => productsResource.reload()} />
      )}
      {reviewsProduct && (
        <SupplierReviewsModal productId={reviewsProduct.id} productTitle={reviewsProduct.title} rating={reviewsProduct.rating ?? 0} reviewsCount={reviewsProduct.reviewsCount ?? 0} onClose={() => setReviewsProduct(null)} />
      )}
      {feedbackProposalId && (
        <SupplierFeedbackModal proposalId={feedbackProposalId} onClose={() => setFeedbackProposalId(null)} onResubmitted={() => proposalsResource.reload()} />
      )}
    </div>
  );
};
