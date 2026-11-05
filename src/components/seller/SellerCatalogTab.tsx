import React, { useEffect, useState } from 'react';
import type { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { SellerProductPricingModal } from './SellerProductPricingModal';
import { OFFICIAL_NICHES, getProductNicheIds } from '../../data/niches';
import { resolvePricing, resolvePlatformPct, roundTo100 } from '../../utils/pricingEngine';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { catalogApi } from '../../services/catalogApi';
import { mapBackendProduct } from '../../context/ProductContext';
import { Search, Check, SlidersHorizontal, Sparkles, Layers, ChevronDown } from 'lucide-react';

// Live parity ("انتخاب کالا"): picking is a DRAFT. Tapping a product or a
// niche only changes pending state; nothing is applied to the storefront
// until the fixed bottom bar's "تأیید و ذخیره" is pressed, and "انصراف"
// discards the draft. Pending rows carry live's "جدید — ذخیره نشده" /
// "حذف می‌شود — ذخیره نشده" badges. Saved values come from the store only
// (no invented first-8-products fallback).
export const SellerCatalogTab: React.FC = () => {
  const {
    products,
    currentStore,
    updateStoreActiveProducts,
    updateStoreSelectedNiches,
    platformSettings,
    formatPrice,
    toPersianDigits,
  } = useStore();

  const [nicheSearch, setNicheSearch] = useState('');
  const [extraSearch, setExtraSearch] = useState('');
  const [showExtraSection, setShowExtraSection] = useState(false);
  const [pricingProduct, setPricingProduct] = useState<any | null>(null);
  const [showNicheHelp, setShowNicheHelp] = useState(false);
  const [saving, setSaving] = useState(false);

  // Fix80: the context catalogue is the UNSCOPED central list, which by
  // design carries no baseCostB (live parity) — resolvePricing on it
  // yields 0/0/0. The seller's own store view (?storeSlug=) DOES carry
  // baseCostB (= C); fetch it for this tab's rows, falling back to the
  // context list while it loads or on failure.
  const [scopedProducts, setScopedProducts] = useState<Product[] | null>(null);
  const storeSlug = currentStore?.ownerType === 'dropshipper' ? currentStore?.slug : undefined;
  useEffect(() => {
    if (!storeSlug) return;
    setScopedProducts(null);
    let cancelled = false;
    catalogApi.getProducts({ storeSlug })
      .then((data) => {
        if (!cancelled && Array.isArray(data) && data.length > 0) {
          setScopedProducts(data.map(mapBackendProduct));
        }
      })
      .catch(() => { /* keep the context list as fallback */ });
    return () => { cancelled = true; };
  }, [storeSlug]);
  const displayProducts = scopedProducts ?? products;

  const savedProductIds = currentStore.activeProductIds || [];
  const savedNicheIds = currentStore.selectedNicheIds || [];

  const [pendingProductIds, setPendingProductIds] = useState<string[]>(savedProductIds);
  const [pendingNicheIds, setPendingNicheIds] = useState<string[]>(savedNicheIds);

  // Re-sync the draft only when the SAVED values actually change (e.g. after
  // a confirm persists), never on unrelated store updates.
  const savedProductsKey = savedProductIds.join(',');
  const savedNichesKey = savedNicheIds.join(',');
  useEffect(() => { setPendingProductIds(savedProductIds); }, [savedProductsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setPendingNicheIds(savedNicheIds); }, [savedNichesKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const productDiffCount =
    pendingProductIds.filter((id) => !savedProductIds.includes(id)).length +
    savedProductIds.filter((id) => !pendingProductIds.includes(id)).length;
  const nicheDiff =
    pendingNicheIds.length !== savedNicheIds.length ||
    pendingNicheIds.some((id) => !savedNicheIds.includes(id));
  const hasChanges = productDiffCount > 0 || nicheDiff;

  useEffect(() => {
    if (saving && !hasChanges) setSaving(false);
  }, [saving, hasChanges]);

  const toggleNiche = (nicheId: string) => {
    let next: string[];
    if (pendingNicheIds.includes(nicheId)) {
      if (pendingNicheIds.length === 1) return; // Live label: "یک یا چند تا"
      next = pendingNicheIds.filter((id) => id !== nicheId);
    } else {
      next = [...pendingNicheIds, nicheId];
    }
    setPendingNicheIds(next);
  };

  const toggleProductSelect = (id: string) => {
    setPendingProductIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  };

  const handleConfirm = () => {
    setSaving(true);
    if (nicheDiff) updateStoreSelectedNiches(pendingNicheIds);
    if (productDiffCount > 0) updateStoreActiveProducts(pendingProductIds);
  };

  const handleCancel = () => {
    setPendingProductIds(savedProductIds);
    setPendingNicheIds(savedNicheIds);
    setSaving(false);
  };

  // 1. Primary Niche Products strictly aligned with official niches
  const nicheProducts = displayProducts.filter((p) => {
    const pNiches = p.nicheIds && p.nicheIds.length > 0 ? p.nicheIds : getProductNicheIds(p);
    const isInSelectedNiches = pNiches.some((nId) => pendingNicheIds.includes(nId));
    const matchesSearch = !nicheSearch || p.title.includes(nicheSearch);
    return isInSelectedNiches && matchesSearch;
  });

  // 2. Extra (Outside Niches) Products
  const outsideNicheProducts = displayProducts.filter((p) => {
    const pNiches = p.nicheIds || [];
    const isOutside = !pNiches.some((nId) => pendingNicheIds.includes(nId));
    const matchesSearch = !extraSearch || p.title.includes(extraSearch);
    return isOutside && matchesSearch;
  });

  // Per-variant seller percents for this store (productId -> variantId -> pct).
  // Variant products price from these (backend: variant override wins), so
  // the list must show them — not the product-level fallback percent.
  const [variantPctByProduct, setVariantPctByProduct] = useState<Record<string, Record<string, number>>>({});
  useEffect(() => {
    if (!currentStore?.id || isMockMode()) return;
    let cancelled = false;
    storeApi.getSellerVariantOverrides(currentStore.id)
      .then((rows) => {
        if (cancelled) return;
        const map: Record<string, Record<string, number>> = {};
        for (const r of rows) {
          (map[r.productId] = map[r.productId] || {})[r.variantId] = r.value;
        }
        setVariantPctByProduct(map);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [currentStore?.id]);

  const renderRow = (product: any, purple: boolean) => {
    const isPending = pendingProductIds.includes(product.id);
    const isSaved = savedProductIds.includes(product.id);
    const isNew = isPending && !isSaved;
    const isRemoved = !isPending && isSaved;
    const pricing = resolvePricing(product, currentStore, platformSettings);
    const baseCostR = pricing.baseCost;
    const finalPrice = pricing.finalPrice;
    const profitToman = pricing.sellerMargin;

    // Variant products: the row shows the VARIANT pricing (what is actually
    // charged), using each variant's effective percent (its own override,
    // else the product fallback). Uniform -> exact numbers; mixed -> «از».
    let displayFinal = finalPrice;
    let displayProfit = profitToman;
    let displayPctText = `${pricing.marginPercent.toLocaleString('fa-IR', { maximumFractionDigits: 2 })}٪`;
    let fromPrefix = '';
    const rowVariants: Array<{ id: string; supplierBaseCost?: number }> = (product as any).variants || [];
    if (rowVariants.length > 0) {
      const ovMap = variantPctByProduct[product.id] || {};
      const pctR = resolvePlatformPct(product, platformSettings);
      const rowsV = rowVariants.map((v) => {
        const costV = v.supplierBaseCost ?? product.supplierBaseCost ?? 0;
        const baseV = roundTo100(costV * (1 + pctR / 100));
        const pct = ovMap[v.id] ?? pricing.marginPercent;
        const finV = roundTo100(baseV * (1 + pct / 100));
        return { pct, finV, profitV: finV - baseV, baseV };
      }).filter((r) => r.baseV > 0);
      if (rowsV.length > 0) {
        const pcts = rowsV.map((r) => r.pct);
        const fins = rowsV.map((r) => r.finV);
        const profs = rowsV.map((r) => r.profitV);
        const minPct = Math.min(...pcts);
        const maxPct = Math.max(...pcts);
        displayFinal = Math.min(...fins);
        displayProfit = Math.min(...profs);
        if (Math.max(...fins) !== displayFinal || maxPct !== minPct) fromPrefix = 'از ';
        const fa = (n: number) => n.toLocaleString('fa-IR', { maximumFractionDigits: 2 });
        displayPctText = minPct === maxPct ? `${fa(minPct)}٪` : `${fa(minPct)}٪ تا ${fa(maxPct)}٪`;
      }
    }

    return (
      <div
        key={product.id}
        onClick={() => toggleProductSelect(product.id)}
        className={`p-2.5 rounded-2xl border transition-all flex items-center justify-between cursor-pointer select-none ${isRemoved ? 'opacity-60' : ''} ${
          isPending
            ? purple
              ? 'bg-purple-50/40 border-purple-400 ring-1 ring-purple-100 shadow-2xs'
              : 'bg-blue-50/40 border-[#007dfa] ring-1 ring-blue-100 shadow-2xs'
            : 'bg-white border-slate-200/90 hover:bg-slate-50'
        }`}
      >
        <div className="flex items-center gap-2.5 min-w-0">
          <img
            src={product.image}
            alt={product.title}
            className="w-12 h-12 rounded-xl object-contain bg-slate-50 border border-slate-100 p-1 shrink-0"
          />
          <div className="min-w-0">
            <h4 className="text-xs font-bold text-slate-800 truncate mb-1">
              {product.title}
            </h4>
            <div className="flex items-center gap-2 text-[10px] text-slate-500">
              <span>خرید: <strong className="text-slate-800">{formatPrice(baseCostR)}</strong></span>
              <span>•</span>
              <span>فروش: <strong className="text-slate-800">{fromPrefix}{formatPrice(displayFinal)}</strong></span>
              <span>•</span>
              <span className="text-emerald-600 font-bold">
                سود تو: +{formatPrice(displayProfit)} ت
                <span className="text-emerald-700"> ({displayPctText})</span>
              </span>
            </div>
            {(isNew || isRemoved) && (
              <span className="flex w-fit items-center gap-1 mt-1 text-[10px] font-bold bg-amber-50 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full">
                {isNew ? 'جدید — ذخیره نشده' : 'حذف می‌شود — ذخیره نشده'}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setPricingProduct(product);
            }}
            className="min-h-[44px] px-2.5 rounded-xl bg-slate-100 hover:bg-blue-50 hover:text-blue-600 text-slate-600 flex items-center gap-1 transition-colors"
            title="تنظیم سود این کالا"
          >
            <SlidersHorizontal className="w-4 h-4" strokeWidth={1.5} />
            <span className="text-[11px] font-bold">سود</span>
          </button>

          <button
            type="button"
            role="switch"
            aria-checked={isPending}
            aria-label={isPending ? `غیرفعال کردن ${product.title} در فروشگاه` : `فعال کردن ${product.title} در فروشگاه`}
            title={isPending ? 'فعال در فروشگاه — برای غیرفعال کردن بزن' : 'غیرفعال — برای فعال کردن در فروشگاه بزن'}
            onClick={() => toggleProductSelect(product.id)}
            className="min-h-[44px] px-1.5 rounded-xl flex items-center gap-2 transition-colors hover:bg-slate-50"
          >
            <span className={`text-[11px] font-bold ${isPending ? 'text-emerald-600' : 'text-slate-400'}`}>
              {isPending ? 'فعال' : 'غیرفعال'}
            </span>
            <span
              dir="ltr"
              className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full px-0.5 transition-colors ${
                isPending ? (purple ? 'bg-purple-600' : 'bg-[#007dfa]') : 'bg-slate-300'
              }`}
            >
              <span
                className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform ${
                  isPending ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </span>
          </button>
        </div>
      </div>
    );
  };

  return (
    <div className={`space-y-3.5 ${hasChanges ? 'pb-32' : 'pb-6'}`}>
      {/* Live intro + niche help. */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <p className="text-[11px] text-slate-600 leading-6">
          اینجا کالاهایی را انتخاب می‌کنی که در فروشگاهت فروخته شوند. اول موضوع‌های فروشگاهت (نیچ) را انتخاب کن، بعد روی هر کالا بزن تا انتخاب شود یا از انتخاب دربیاید؛ در پایان دکمه «تأیید و ذخیره» را بزن تا تغییرات در فروشگاهت اعمال شود.
        </p>
        <button
          type="button"
          onClick={() => setShowNicheHelp((v) => !v)}
          aria-expanded={showNicheHelp}
          className="text-[11px] font-bold text-blue-700"
        >
          «نیچ» یعنی چه؟
        </button>
        {showNicheHelp && (
          <div className="space-y-1 text-[11px] leading-6 text-slate-600">
            <p>نیچ یعنی یک موضوع تخصصی؛ مثلاً «ساعت هوشمند» یا «پاوربانک». وقتی یک نیچ را انتخاب می‌کنی، کالاهای آن موضوع آماده‌اند تا با یک لمس وارد فروشگاهت شوند.</p>
            <p>اگر فقط ساعت هوشمند می‌فروشی، فقط نیچ «ساعت هوشمند» را روشن کن.</p>
          </div>
        )}
      </div>

      {/* 1. Header & Active Products Count */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <Layers className="w-4 h-4 text-blue-600" strokeWidth={1.5} />
            <h3 className="text-xs font-bold text-slate-800">انتخاب کالا برای فروشگاهت</h3>
          </div>
          <span className="text-[10px] text-blue-700 bg-blue-50 border border-blue-200 px-2 py-0.5 rounded-full font-bold">
            {toPersianDigits(savedProductIds.length)} کالا در فروشگاهت هست
          </span>
        </div>

        {/* Section 2.2: Niche Selector Chips (Multi-Select) */}
        <div>
          <label className="text-[11px] font-bold text-slate-600 block mb-1.5">
            نیچ‌های فروشگاهت (موضوع‌هایی که می‌فروشی) — یک یا چند تا:
          </label>
          <div className="flex flex-wrap gap-1.5">
            {OFFICIAL_NICHES.map((niche) => {
              const isSelected = pendingNicheIds.includes(niche.id);
              return (
                <button
                  key={niche.id}
                  type="button"
                  onClick={() => toggleNiche(niche.id)}
                  className={`px-3 py-2.5 min-h-[44px] rounded-xl text-xs font-bold transition-all flex items-center gap-1.5 ${
                    isSelected
                      ? 'bg-blue-600 text-white shadow-xs scale-102'
                      : 'bg-slate-100 text-slate-700 hover:bg-slate-200'
                  }`}
                >
                  <span>{niche.title}</span>
                  {isSelected && <Check className="w-3.5 h-3.5" strokeWidth={1.5} />}
                </button>
              );
            })}
          </div>
        </div>

        {/* Niche Search */}
        <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5">
          <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5" strokeWidth={1.5} />
          <input
            type="text"
            placeholder="جستجوی کالا در نیچ‌هایی که انتخاب کردی…"
            value={nicheSearch}
            onChange={(e) => setNicheSearch(e.target.value)}
            className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
        </div>
      </div>

      {/* 2. Products of Selected Niches */}
      <div className="space-y-2">
        <div className="flex items-center justify-between px-1 text-[11px]">
          <span className="font-bold text-slate-700">کالاهای نیچ‌های تو ({toPersianDigits(nicheProducts.length)} کالا)</span>
          <span className="text-slate-400 text-[10px]">روی کالا بزن تا انتخاب شود یا از انتخاب دربیاید؛ در پایان «تأیید و ذخیره» را بزن</span>
        </div>

        <div className="rounded-xl bg-slate-50 border border-slate-200 px-3 py-2 text-[11px] text-slate-600 leading-relaxed">
          قیمت خرید هر کالا از پلتفرم، قیمت فروش به مشتری و سود تو از هر فروش زیر هر کالا نوشته شده است؛ برای کالایی که قیمت تأمین‌کننده ندارد هنوز اعلام نشده است.
        </div>

        {nicheProducts.length === 0 ? (
          <div className="bg-white rounded-2xl p-8 text-center space-y-2">
            <p className="text-xs font-bold text-slate-700">کالایی پیدا نشد</p>
            <p className="text-[11px] text-slate-500 leading-5">
              {nicheSearch
                ? 'در نیچ‌هایی که انتخاب کردی، کالایی با این جستجو نیست. جستجو را پاک کن یا یک نیچ دیگر هم انتخاب کن.'
                : 'یک نیچ دیگر هم از بالا انتخاب کن.'}
            </p>
            {nicheSearch && (
              <button
                type="button"
                onClick={() => setNicheSearch('')}
                className="text-[11px] font-bold text-blue-700"
              >
                پاک کردن جستجو
              </button>
            )}
          </div>
        ) : (
          nicheProducts.map((product) => renderRow(product, false))
        )}
      </div>

      {/* Section 2.3: بقیه کالاها (خارج از نیچ‌های تو) */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs overflow-hidden">
        <button
          type="button"
          onClick={() => setShowExtraSection(!showExtraSection)}
          className="w-full p-3.5 flex items-center justify-between hover:bg-slate-50 text-right transition-colors"
        >
          <div className="flex items-center gap-2">
            <Sparkles className="w-4 h-4 text-purple-600" strokeWidth={1.5} />
            <div>
              <span className="text-xs font-bold text-slate-800 block">
                بقیه کالاها (خارج از نیچ‌های تو)
              </span>
              <span className="text-[10px] text-slate-400">
                از همه کالاهای پلتفرم، هر کدام را خواستی به فروشگاهت اضافه کن
              </span>
            </div>
          </div>
          <ChevronDown className={`w-4 h-4 text-slate-400 transition-transform ${showExtraSection ? 'rotate-180' : ''}`} strokeWidth={1.5} />
        </button>

        {showExtraSection && (
          <div className="p-3.5 pt-0 space-y-2.5 border-t border-slate-100">
            <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5 mt-2">
              <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5" strokeWidth={1.5} />
              <input
                type="text"
                placeholder="جستجو در بقیه کالاها…"
                value={extraSearch}
                onChange={(e) => setExtraSearch(e.target.value)}
                className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
              />
            </div>

            <div className="space-y-2">
              {outsideNicheProducts.length === 0 ? (
                <p className="text-center text-xs text-slate-400 py-4">کالایی پیدا نشد</p>
              ) : (
                outsideNicheProducts.slice(0, 10).map((product) => renderRow(product, true))
              )}
            </div>
          </div>
        )}
      </div>

      {/* Live's fixed bottom confirm bar, only while the draft differs. */}
      {hasChanges && (
        <div className="fixed bottom-0 inset-x-0 z-30 px-3.5 pb-3 pointer-events-none">
          <div className="max-w-[504px] mx-auto pointer-events-auto bg-white border border-amber-200 rounded-2xl shadow-lg p-3 space-y-2.5">
            <div className="flex items-center gap-1.5 text-amber-800">
              <span className="text-xs font-bold">
                {productDiffCount > 0 ? `${toPersianDigits(productDiffCount)} تغییر در انتظار ذخیره` : 'تغییر در انتظار ذخیره'}
              </span>
              <span className="text-[10px] text-amber-700">(هنوز در فروشگاهت اعمال نشده)</span>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={handleConfirm}
                disabled={saving}
                className="flex-1 min-h-[48px] rounded-xl bg-blue-700 hover:bg-blue-800 text-white text-sm font-extrabold flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60 disabled:cursor-wait"
              >
                {saving ? 'در حال ذخیره…' : 'تأیید و ذخیره'}
              </button>
              <button
                type="button"
                onClick={handleCancel}
                disabled={saving}
                className="min-h-[48px] px-4 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-600 text-xs font-bold flex items-center justify-center gap-1 transition-colors disabled:opacity-60"
              >
                انصراف
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Section 3: Pricing Modal with B-only and Percent/Amount Switch */}
      <SellerProductPricingModal
        isOpen={!!pricingProduct}
        product={pricingProduct}
        onClose={() => setPricingProduct(null)}
      />
    </div>
  );
};
