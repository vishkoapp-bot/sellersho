import React, { useState, useEffect } from 'react';
import { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { OFFICIAL_NICHES, getProductNicheIds } from '../../data/niches';
import { roundTo100, resolveBaseCostR, resolvePlatformPct, resolvePricing } from '../../utils/pricingEngine';
import { X, Calculator, Check, ArrowRight, Layers, AlertTriangle } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  product: Product | null;
  isOpen: boolean;
  onClose: () => void;
}

interface ModalContentProps {
  product: Product;
  onClose: () => void;
}

const SellerProductPricingModalContent: React.FC<ModalContentProps> = ({ product, onClose }) => {
  const {
    products,
    currentStore,
    updateStoreProductPriceOverrideWithMode,
    updateStoreNichePriceOverride,
    platformSettings,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error } = useToast();

  const [showNicheConfirmModal, setShowNicheConfirmModal] = useState(false);

  // Percent base price R = C x (1+pct/100) (server percent engine).
  const baseCostR = resolveBaseCostR(product, platformSettings);
  const productPct = resolvePlatformPct(product, platformSettings);

  // Live: store-level admin margin lock (candidate context carries only the
  // store flag; live also has per-product / per-niche locks — see report).
  const isLocked = !!currentStore.isMarginLockedByAdmin;
  const variantCount = product.variants?.length ?? 0;
  const baseUnknown = !isFinite(baseCostR) || baseCostR <= 0;

  // Identify product niche
  const pNicheIds = product.nicheIds && product.nicheIds.length > 0 ? product.nicheIds : getProductNicheIds(product);
  const productNiche = OFFICIAL_NICHES.find((n) => pNicheIds.includes(n.id)) || OFFICIAL_NICHES[0];

  // Products in this niche for bulk impact preview
  const nicheProducts = products.filter((p) => {
    const ids = p.nicheIds && p.nicheIds.length > 0 ? p.nicheIds : getProductNicheIds(p);
    return ids.includes(productNiche.id);
  });

  // Current product pricing state
  const existingOverride = currentStore.productPriceOverrides?.[product.id];

  const [profitVal, setProfitVal] = useState<string>(
    existingOverride !== undefined
      ? existingOverride.toString()
      : (currentStore.profitMarginPercent || platformSettings.defaultMargin || 20).toString()
  );
  const [applyToNiche, setApplyToNiche] = useState(false);

  const numVal = Number(profitVal);

  // Requirement 1: Negative profit rejection (percent-only floor from
  // platform settings; the server enforces the same range on save).
  const minAllowed = Math.max(0, platformSettings.minProfitMargin ?? 5);
  // ISO-C margin model: global ceiling from platform settings (a
  // per-product ceiling, when set by the platform, is enforced
  // server-side and surfaced by the save error path).
  const maxAllowed = platformSettings.maxProfitMargin ?? 35;
  const isProfitValid = profitVal !== '' && !isNaN(numVal) && numVal >= minAllowed && numVal <= maxAllowed;

  // The allowed margin band comes from the server's pricing settings
  // (platformSettings), so the seller always sees the real range before
  // saving. Save failures are reported by kind: a 400 names the range, a
  // 403 is the admin lock, anything else is a server error — never blame
  // the range for a server failure.
  const rangeText = `از ${toPersianDigits(minAllowed)}٪ تا ${toPersianDigits(maxAllowed)}٪`;
  const saveErrorText = (status?: number): string => {
    if (status === 400) return `ذخیره انجام نشد؛ درصد سود باید در بازه مجاز پلتفرم باشد (${rangeText}).`;
    if (status === 403) return 'ذخیره انجام نشد؛ قیمت‌گذاری این فروشگاه توسط مدیر پلتفرم قفل شده است.';
    return 'ذخیره انجام نشد؛ خطای سرور رخ داد. چند لحظه دیگر دوباره تلاش کنید.';
  };

  // Priority 2.1: Use Canonical resolvePricing from pricingEngine
  const simulatedStore = {
    ...currentStore,
    productPriceOverrides: { ...(currentStore.productPriceOverrides || {}), [product.id]: numVal },
  };
  const simulatedPricing = resolvePricing(product, simulatedStore as any, platformSettings);
  const finalPriceP = isProfitValid ? simulatedPricing.finalPrice : roundTo100(baseCostR);
  const netProfitToman = finalPriceP - baseCostR;
  // Displayed margin is the applied input margin (server definition), not
  // profit/final — so the number shown matches the number typed.
  const effectiveMarginPercent = isProfitValid ? simulatedPricing.marginPercent : 0;

  const handleInitialSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (isLocked) return;
    if (!isProfitValid) {
      if (!isNaN(numVal) && numVal > maxAllowed) {
        error(`حداکثر درصد سود مجاز ${toPersianDigits(maxAllowed)}٪ است.`);
      } else {
        error(`حداقل درصد سود مجاز ${toPersianDigits(minAllowed)}٪ است و مقادیر منفی پذیرفته نیستند.`);
      }
      return;
    }

    if (applyToNiche && productNiche) {
      setShowNicheConfirmModal(true);
      return;
    }

    // Awaited: the toast must not claim success when the server rejects
    // (floor/ceiling/lock); on rejection the context reverts the value.
    const res = await updateStoreProductPriceOverrideWithMode(product.id, numVal);
    if (!res.ok) {
      error(saveErrorText(res.status));
      return;
    }
    if (numVal <= 0) {
      success(`سود سفارشی کالای «${product.title}» حذف شد؛ از این پس حاشیه سود پیش‌فرض فروشگاه اعمال می‌شود.`);
    } else {
      success(`سود ${toPersianDigits(numVal)}٪ برای کالای «${product.title}» ذخیره شد؛ قیمت نهایی پس از اعلام مبنای فروش در ویترین اعمال می‌شود.`);
    }
    onClose();
  };

  const handleConfirmNicheBulkApply = async () => {
    const resNiche = await updateStoreNichePriceOverride(productNiche.id, numVal);
    const resProduct = await updateStoreProductPriceOverrideWithMode(product.id, numVal);
    if (!resNiche.ok || !resProduct.ok) {
      error(saveErrorText((!resNiche.ok ? resNiche.status : resProduct.status)));
      return;
    }
    success(
      numVal <= 0
        ? `نرخ سود نیچ «${productNiche.title}» حذف شد؛ از این پس سود جداگانه هر کالا — یا حاشیه سود پیش‌فرض فروشگاه — اعمال می‌شود.`
        : `نرخ سود نیچ «${productNiche.title}» ذخیره شد؛ روی کالاهایی که سود جداگانه دارند اثری ندارد.`
    );
    setShowNicheConfirmModal(false);
    onClose();
  };

  // Per-variant margins (percent-only): the dropshipper sets a profit
  // PERCENT per variant; the supplier sets the variant's own price
  // (supplierBaseCost). Empty input = follow the product margin above.
  // Fetched once per modal open (the content remounts per product via
  // key); the product-margin save must not clobber typed variant values.
  const [variantInputs, setVariantInputs] = useState<Record<string, string>>({});
  const [variantSaved, setVariantSaved] = useState<Record<string, string>>({});
  const [variantSaving, setVariantSaving] = useState(false);

  useEffect(() => {
    if (variantCount === 0 || isMockMode() || !currentStore?.id) return;
    storeApi.getSellerVariantOverrides(currentStore.id)
      .then((rows) => {
        const next: Record<string, string> = {};
        for (const row of rows) {
          if (row.productId === product.id && product.variants?.some((v) => v.id === row.variantId)) {
            next[row.variantId] = String(row.value);
          }
        }
        setVariantSaved(next);
        setVariantInputs(next);
      })
      .catch(() => {});
  }, []);

  const handleSaveVariantMargins = async () => {
    if (isLocked || variantSaving) return;
    const variants = product.variants || [];
    for (const v of variants) {
      const raw = variantInputs[v.id];
      if (raw === undefined || raw === '') continue;
      const n = Number(raw);
      if (isNaN(n) || n < 0) {
        error(`درصد سود تنوع «${v.variantName || v.name}» باید عددی بزرگ‌تر یا مساوی صفر باشد.`);
        return;
      }
    }
    if (!currentStore?.id) { error('فروشگاه فعال مشخص نیست.'); return; }
    setVariantSaving(true);
    const nextSaved = { ...variantSaved };
    let firstError: string | null = null;
    for (const v of variants) {
      const raw = variantInputs[v.id];
      const saved = variantSaved[v.id];
      if ((raw ?? '') === (saved ?? '')) continue;
      try {
        if (raw === undefined || raw === '' || Number(raw) === 0) {
          if (saved !== undefined) {
            await storeApi.deleteSellerVariantOverride(currentStore.id, product.id, v.id);
            delete nextSaved[v.id];
          }
        } else {
          await storeApi.setSellerVariantOverride({ storeId: currentStore.id, productId: product.id, variantId: v.id, value: Number(raw), mode: 'percent' });
          nextSaved[v.id] = raw;
        }
      } catch (err: any) {
        // Revert this row on server rejection (same rule as the product modal).
        setVariantInputs((prev) => ({ ...prev, [v.id]: saved ?? '' }));
        if (saved === undefined) delete nextSaved[v.id]; else nextSaved[v.id] = saved;
        if (!firstError) firstError = err?.message || 'ذخیره سود تنوع انجام نشد.';
      }
    }
    setVariantSaved(nextSaved);
    setVariantSaving(false);
    if (firstError) { error(firstError); return; }
    success('سود تنوع‌ها ذخیره شد.');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[400px] w-full p-4 shadow-2xl border border-slate-100 flex max-h-[calc(100dvh-1.5rem)] flex-col gap-3 overflow-hidden"
      >
        <div className="flex shrink-0 items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-blue-50 text-blue-600 flex items-center justify-center">
              <Calculator className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">سود تو از این کالا</h3>
              <span className="text-[10px] text-slate-400 block truncate max-w-[200px]">{product.title}</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        {isLocked && (
          <div className="p-2.5 bg-amber-50 border border-amber-200 rounded-xl text-[11px] font-bold text-amber-800">
            قیمت‌گذاری این فروشگاه از طرف مدیریت قفل شده است؛ تغییر سود در حال حاضر ممکن نیست.
          </div>
        )}

        <form onSubmit={handleInitialSubmit} className="-mx-1 min-h-0 flex-1 space-y-3 overflow-y-auto overscroll-contain px-1 pb-1 text-xs">
          {/* Section 3.1: Single unified base price (R) */}
          <div className="bg-slate-50 p-3 rounded-xl border border-slate-200/80 flex items-center justify-between gap-3">
            <span className="text-slate-600 font-bold text-[11px]">قیمت پایه:</span>
            <span className="shrink-0 whitespace-nowrap font-extrabold text-slate-900 font-mono text-sm">{formatPrice(baseCostR)} تومان</span>
          </div>

          {variantCount >= 2 && (
            <p className="rounded-xl bg-slate-50 border border-slate-200 p-2.5 text-[11px] leading-5 text-slate-600">
              این کالا ۲ تنوع یا بیشتر دارد؛ سود سطح کالا در قیمت فروش اثری ندارد و قیمت از سود هر تنوع حساب می‌شود.
            </p>
          )}

          {variantCount > 0 && (
            <div className="space-y-2 p-3 bg-slate-50 border border-slate-200 rounded-xl">
              <p className="text-[11px] font-bold text-slate-800">سود هر تنوع (درصد روی قیمت پایه همان تنوع)</p>
              <p className="text-[10px] leading-5 text-slate-500">
                قیمت هر تنوع را تأمین‌کننده تعیین می‌کند؛ تو فقط درصد سود همان تنوع را می‌گذاری. خالی یعنی پیرو سود کالا.
              </p>
              <p className="text-[10px] leading-5 text-slate-500">
                بازه مجاز سود برای هر تنوع هم همان بازه پلتفرم است: {rangeText}
              </p>
              {(product.variants || []).map((v) => {
                const costV = v.supplierBaseCost ?? product.supplierBaseCost;
                const baseV = roundTo100(costV * (1 + productPct / 100));
                const typed = variantInputs[v.id] ?? '';
                const marginV = typed !== '' && !isNaN(Number(typed)) ? Number(typed) : (isNaN(numVal) ? 0 : numVal);
                const finalV = roundTo100(baseV * (1 + marginV / 100));
                return (
                  <div key={v.id} className="space-y-2 bg-white border border-slate-200 rounded-xl p-2.5">
                    <div className="flex items-center gap-2">
                      <span className="flex-1 min-w-0 truncate text-[11px] font-bold text-slate-800">{v.variantName || v.name}</span>
                      <input
                        type="number"
                        min={0}
                        disabled={isLocked || variantSaving}
                        value={typed}
                        onChange={(e) => setVariantInputs((prev) => ({ ...prev, [v.id]: e.target.value }))}
                        placeholder={!isNaN(numVal) ? toPersianDigits(numVal) : '۲۰'}
                        className="w-20 shrink-0 bg-slate-50 border border-slate-200 rounded-lg px-2 py-1.5 text-center text-xs font-bold text-emerald-600 font-mono focus:outline-hidden"
                        aria-label={`درصد سود تنوع ${v.variantName || v.name}`}
                      />
                      <span className="shrink-0 text-[10px] font-bold text-slate-400">٪</span>
                    </div>
                    {baseV > 0 ? (
                      <div className="flex items-center justify-between gap-2 border-t border-slate-100 pt-1.5 text-[10px] text-slate-500">
                        <span className="whitespace-nowrap">پایه: <span className="font-mono font-bold text-slate-700">{formatPrice(baseV)} ت</span></span>
                        <span className="whitespace-nowrap">نهایی: <span className="font-mono font-bold text-slate-700">{formatPrice(finalV)} ت</span></span>
                      </div>
                    ) : (
                      <p className="border-t border-slate-100 pt-1.5 text-[10px] text-slate-500">قیمت تنوع هنوز اعلام نشده است.</p>
                    )}
                  </div>
                );
              })}
              <button
                type="button"
                disabled={isLocked || variantSaving}
                onClick={handleSaveVariantMargins}
                className="w-full py-2 bg-slate-700 hover:bg-slate-800 text-white font-bold text-xs rounded-xl disabled:opacity-50"
              >
                {variantSaving ? 'در حال ذخیره…' : 'ذخیره سود تنوع‌ها'}
              </button>
            </div>
          )}

          {/* Section 3.2: Percent-only profit input */}
          <div>

            <div className={`relative flex items-center rounded-xl px-3 py-2 border transition-all ${
              !isProfitValid
                ? 'bg-rose-50/50 border-rose-300 ring-1 ring-rose-200'
                : 'bg-slate-50 border-slate-200 focus-within:bg-white focus-within:border-blue-500'
            }`}>
              <input
                type="number"
                min={minAllowed}
                disabled={isLocked}
                value={profitVal}
                onChange={(e) => setProfitVal(e.target.value)}
                placeholder='۲۰'
                className="w-full bg-transparent text-xs font-bold text-emerald-600 font-mono focus:outline-hidden"
                required
              />
              <span className="text-[10px] text-slate-400 font-bold mr-1 shrink-0">
                درصد سود روی مبنا
              </span>
            </div>

            <p className="mt-1 text-[10px] leading-5 text-slate-500">
              بازه مجاز سود در پلتفرم: {rangeText}
            </p>
            {/* Requirement 1: Real-time validation warning */}
            {!isProfitValid && profitVal !== '' && (
              <div className="flex items-center gap-1 text-[10px] text-rose-600 mt-1">
                <AlertTriangle className="w-3 h-3 shrink-0"  strokeWidth={1.5}/>
                <span>
                  {!isNaN(numVal) && numVal > maxAllowed
                    ? `حداکثر درصد سود مجاز ${toPersianDigits(maxAllowed)}٪ است.`
                    : `سود منفی مجاز نیست؛ حداقل درصد سود مصوب پلتفرم ${toPersianDigits(minAllowed)}٪ است.`}
                </span>
              </div>
            )}
          </div>

          {/* Section 3.3.b: Niche Bulk Checkbox */}
          <div className="p-2.5 bg-blue-50/50 border border-blue-100 rounded-xl flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Layers className="w-4 h-4 text-blue-600 shrink-0"  strokeWidth={1.5}/>
              <div>
                <span className="text-[11px] font-bold text-slate-800 block">
                  اعمال روی کل نیچ «{productNiche.title}»
                </span>
                <span className="text-[9px] text-slate-500">
                  شامل {toPersianDigits(nicheProducts.length)} کالا در این نیچ (نیازمند تایید دومرحله‌ای)
                </span>
              </div>
            </div>
            <input
              type="checkbox"
              disabled={isLocked}
              checked={applyToNiche}
              onChange={(e) => setApplyToNiche(e.target.checked)}
              className="w-4 h-4 accent-blue-600 cursor-pointer"
            />
          </div>

          {baseUnknown ? (
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-1">
              {isProfitValid ? (
                <>
                  <div className="flex items-center justify-between">
                    <span className="text-[10px] text-slate-700 font-bold">سود انتخابی شما:</span>
                    <span className="text-base font-black text-slate-800 font-mono">`٪${toPersianDigits(numVal)}`</span>
                  </div>
                  <p className="text-[10px] leading-5 text-slate-600">قیمت نهایی را فعلاً نمی‌شود نشان داد، چون قیمت خرید کالا هنوز اعلام نشده است. وقتی اعلام شود، همین سودی که این‌جا می‌گذاری خودکار روی آن حساب می‌شود.</p>
                </>
              ) : (
                <p className="text-[10px] leading-5 text-slate-600">مقدار سود معتبر وارد کنید تا ذخیره شود.</p>
              )}
            </div>
          ) : (
          <div className="p-3 bg-emerald-50/70 border border-emerald-200 rounded-xl space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-[10px] text-emerald-800 font-bold">قیمت نهایی مصرف‌کننده (رند ۱۰۰ ت):</span>
              <span className="text-base font-black text-emerald-800 font-mono">{formatPrice(finalPriceP)} تومان</span>
            </div>
            <div className="flex items-center justify-between text-[10px] text-emerald-700 pt-1 border-t border-emerald-200/60">
              <span>سود خالص شما در هر فروش: <strong className="font-mono">+{formatPrice(netProfitToman)} ت</strong></span>
              <span>حاشیه سود: ٪{toPersianDigits(effectiveMarginPercent)}</span>
            </div>
          </div>

          )}
          <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
            <button
              type="button"
              onClick={onClose}
              className="py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
            >
              انصراف
            </button>
            <button
              type="submit"
              disabled={!isProfitValid || isLocked}
              className="py-2.5 bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs disabled:opacity-50 disabled:cursor-not-allowed transition-all"
            >
              {applyToNiche ? 'مرحله بعد: بازبینی و تایید' : 'ذخیره و اعمال در ویترین'}
            </button>
          </div>
        </form>

        {/* Requirement 3: Two-Step Confirmation Modal for Bulk Niche Pricing */}
        {showNicheConfirmModal && (
          <div className="fixed inset-0 z-60 flex items-center justify-center p-3 bg-slate-900/70 backdrop-blur-xs select-none">
            <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-amber-200 space-y-3 text-xs max-h-[calc(100dvh-1.5rem)] overflow-y-auto overscroll-contain">
              <div className="flex items-center gap-2 text-amber-600 border-b border-slate-100 pb-2">
                <AlertTriangle className="w-5 h-5 shrink-0"  strokeWidth={1.5}/>
                <h4 className="font-bold text-slate-900">این سود روی همه کالاهای این نیچ اعمال شود؟</h4>
              </div>

              <div className="space-y-2 text-slate-600 text-[11px] leading-relaxed">
                <p>
                  شما در حال اعمال نرخ سود{' '}
                  <strong className="text-blue-700 font-bold">
                    `${toPersianDigits(numVal)}٪`
                  </strong>{' '}
                  روی کل نیچ تخصصی <strong className="text-slate-900">«{productNiche.title}»</strong> هستید.
                </p>

                <div className="p-2.5 bg-amber-50/70 border border-amber-200/80 rounded-xl space-y-1">
                  <span className="font-bold text-amber-900 block text-[10px]">
                    کالاهای تحت تأثیر ({toPersianDigits(nicheProducts.length)} محصول):
                  </span>
                  <div className="max-h-28 overflow-y-auto no-scrollbar space-y-1 pt-1">
                    {nicheProducts.map((p) => (
                      <div key={p.id} className="flex justify-between text-[10px] border-b border-amber-200/40 pb-0.5">
                        <span className="truncate max-w-[190px]">{p.title}</span>
                        <span className="text-slate-500 font-mono shrink-0">مبنا: {formatPrice(resolveBaseCostR(p, platformSettings))} ت</span>
                      </div>
                    ))}
                  </div>
                </div>

                <p className="p-2 bg-slate-50 border border-slate-200 rounded-xl text-[10px] leading-5 text-slate-600">
                  <strong>دقت کن:</strong> این نرخ فقط روی کالاهایی اثر می‌گذارد که سود جداگانه برای خود کالا یا تنوعش تعیین نشده باشد؛ سودهای جداگانه سر جایشان می‌مانند و هر وقت خواستی می‌توانی سود هر کالا را جداگانه عوض کنی.
                </p>
              </div>

              <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
                <button
                  type="button"
                  onClick={() => setShowNicheConfirmModal(false)}
                  className="py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
                >
                  انصراف و بازگشت
                </button>
                <button
                  type="button"
                  onClick={handleConfirmNicheBulkApply}
                  className="py-2 bg-amber-600 hover:bg-amber-700 text-white font-bold text-xs rounded-xl shadow-xs"
                >
                  تأیید نهایی و اعمال گروهی
                </button>
              </div>
            </div>
          </div>
        )}
      </motion.div>
    </div>
  );
};


export const SellerProductPricingModal: React.FC<Props> = ({ product, isOpen, onClose }) => {
  if (!isOpen || !product) return null;
  return <SellerProductPricingModalContent key={product.id} product={product} onClose={onClose} />;
};

