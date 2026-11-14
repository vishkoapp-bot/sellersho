import { parsePercent } from '../../utils/money';
import React, { useState, useEffect } from 'react';
import { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { catalogApi } from '../../services/catalogApi';
import { mapBackendProduct } from '../../context/ProductContext';
import { activeProductVariants, resolvePurchase, sellerVariantPreview } from '../../utils/productPurchase';
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
  const [scopedPricingProduct, setScopedPricingProduct] = useState<Product | null>(null);
  // P2(a)/P2(c): full (unscoped) variant list + per-store settings.
  // Visibility is derived by comparing the store-scoped DTO against
  // the central DTO (a hidden variant is absent from the scoped list);
  // there is no bulk GET endpoint for these settings (backend P1-P2
  // spec), so nothing here invents one.
  const isCentralStore = !currentStore || currentStore.ownerType !== 'dropshipper';
  const [centralPricingProduct, setCentralPricingProduct] = useState<Product | null>(null);
  const [scopedFetchDone, setScopedFetchDone] = useState(false);
  const [centralFetchDone, setCentralFetchDone] = useState(false);
  const [visibilityInputs, setVisibilityInputs] = useState<Record<string, boolean>>({});
  const [visibilitySaved, setVisibilitySaved] = useState<Record<string, boolean>>({});
  const [defaultInput, setDefaultInput] = useState<string | null>(null);
  const [defaultSaved, setDefaultSaved] = useState<string | null>(null);
  const variantSettingsReady = !isMockMode() && !isCentralStore && scopedFetchDone && centralFetchDone;
  const pricingProduct = scopedPricingProduct ?? product;
  const pricingVariants = activeProductVariants(centralPricingProduct ?? pricingProduct);
  const scopedVariantIds = new Set(activeProductVariants(pricingProduct).map((v) => v.id));
  useEffect(() => {
    if (isMockMode() || !currentStore?.slug) { setScopedFetchDone(true); return; }
    let cancelled = false;
    catalogApi.getProductById(product.id, currentStore.slug)
      .then((dto) => { if (!cancelled) setScopedPricingProduct(mapBackendProduct(dto)); })
      .catch(() => {})
      .finally(() => { if (!cancelled) setScopedFetchDone(true); });
    return () => { cancelled = true; };
  }, [product.id, currentStore?.slug]);
  useEffect(() => {
    if (isMockMode() || isCentralStore) { setCentralPricingProduct(null); setCentralFetchDone(true); return; }
    let cancelled = false;
    catalogApi.getProductById(product.id)
      .then((dto) => { if (!cancelled) setCentralPricingProduct(mapBackendProduct(dto)); })
      .catch(() => { /* full list falls back to the passed product */ })
      .finally(() => { if (!cancelled) setCentralFetchDone(true); });
    return () => { cancelled = true; };
  }, [product.id]);
  // Snapshot the settings once both DTOs have landed (the modal is
  // keyed per product, so this runs once per open).
  useEffect(() => {
    if (!variantSettingsReady) return;
    const vis: Record<string, boolean> = {};
    for (const v of pricingVariants) vis[v.id] = scopedVariantIds.has(v.id);
    setVisibilitySaved(vis);
    setVisibilityInputs(vis);
    const def = pricingProduct.storeDefaultVariantId ?? null;
    setDefaultSaved(def);
    setDefaultInput(def);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [variantSettingsReady]);

  // Percent base price R = C x (1+pct/100) (server percent engine).
  const baseCostR = resolveBaseCostR(pricingProduct, platformSettings);
  const productPct = resolvePlatformPct(pricingProduct, platformSettings);

  // Live: store-level admin margin lock (candidate context carries only the
  // store flag; live also has per-product / per-niche locks — see report).
  const isLocked = !!currentStore.isMarginLockedByAdmin;
  const variantCount = pricingVariants.length;
  const visibleVariantCount = pricingVariants.filter((v) => visibilityInputs[v.id] !== false).length;
  const allVariantsHidden = variantSettingsReady && variantCount > 0 && visibleVariantCount === 0;
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

  // No store-wide default margin exists anymore: with no per-product
  // percent the input starts EMPTY and the maximum allowed margin
  // applies automatically (see fallbackPct below).
  const [profitVal, setProfitVal] = useState<string>(
    existingOverride !== undefined ? existingOverride.toString() : ''
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
  let validPrecision = false;
  try { parsePercent(numVal); validPrecision = true; } catch { /* invalid draft */ }
  const isProfitValid = profitVal !== '' && validPrecision && numVal >= minAllowed && numVal <= maxAllowed;
  // Wherever no percent is set, the maximum allowed margin applies
  // automatically (platform max; a per-product ceiling, when the
  // platform sets one, is enforced server-side and surfaced via the
  // save error path). Used for the plain-product preview; per-variant
  // rows fall back to the max directly (an unset variant never inherits
  // the product percent — backend parity).
  const fallbackPct = profitVal !== '' && !isNaN(numVal) ? numVal : maxAllowed;
  const previewValid = profitVal === '' || isProfitValid;

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
    productPriceOverrides: { ...(currentStore.productPriceOverrides || {}), [product.id]: previewValid ? fallbackPct : maxAllowed },
  };
  const simulatedPricing = resolvePricing(pricingProduct, simulatedStore as any, platformSettings);
  const finalPriceP = previewValid ? simulatedPricing.finalPrice : baseCostR;
  const netProfitToman = finalPriceP - baseCostR;
  // Displayed margin is the applied input margin (server definition), not
  // profit/final — so the number shown matches the number typed.
  const effectiveMarginPercent = previewValid ? simulatedPricing.marginPercent : 0;

  const handleInitialSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    // Variant products have no product-level margin: their prices come
    // from the per-variant percents only (saved by the variant button).
    if (variantCount > 0) { onClose(); return; }
    if (isLocked) return;
    if (profitVal === '') {
      if (applyToNiche) {
        error('برای اعمال گروهی روی نیچ، ابتدا درصد سود را وارد کن.');
        return;
      }
      // No percent for this product: an existing custom percent is
      // removed (saving 0 deletes the override); either way the maximum
      // allowed margin applies automatically from now on.
      if (existingOverride !== undefined) {
        const res = await updateStoreProductPriceOverrideWithMode(product.id, 0);
        if (!res.ok) {
          error(saveErrorText(res.status));
          return;
        }
        success(`سود سفارشی کالای «${product.title}» حذف شد؛ از این پس حداکثر سود مجاز پلتفرم به‌صورت خودکار اعمال می‌شود.`);
      } else {
        success(`برای کالای «${product.title}» درصدی تعیین نشده است؛ حداکثر سود مجاز پلتفرم به‌صورت خودکار اعمال می‌شود.`);
      }
      onClose();
      return;
    }
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
      success(`سود سفارشی کالای «${product.title}» حذف شد؛ از این پس حداکثر سود مجاز پلتفرم به‌صورت خودکار اعمال می‌شود.`);
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
        ? `نرخ سود نیچ «${productNiche.title}» حذف شد؛ از این پس سود جداگانه هر کالا — یا حداکثر سود مجاز پلتفرم به‌صورت خودکار — اعمال می‌شود.`
        : `نرخ سود نیچ «${productNiche.title}» ذخیره شد؛ روی کالاهایی که سود جداگانه دارند اثری ندارد.`
    );
    setShowNicheConfirmModal(false);
    onClose();
  };

  // Per-variant margins (percent-only): the dropshipper sets a profit
  // PERCENT per variant; the supplier sets the variant's own price
  // (supplierBaseCost). Empty input = the product percent if one is
  // set, otherwise the maximum allowed margin, automatically.
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
    const variants = pricingVariants;
    for (const v of variants) {
      const raw = variantInputs[v.id];
      if (raw === undefined || raw === '') continue;
      const n = Number(raw);
      if (!Number.isFinite(n) || (n !== 0 && (n < minAllowed || n > maxAllowed))) {
        error(`درصد سود تنوع «${v.variantName || v.name}» باید در بازه مجاز پلتفرم باشد (صفر برای حذف سود سفارشی).`);
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
    // P2(a) visibility: only changed rows are PUT; a failed row reverts
    // to its saved value (same firstError pattern as the margin loop).
    const nextVisibilitySaved = { ...visibilitySaved };
    for (const v of variants) {
      if (!variantSettingsReady) break;
      const next = visibilityInputs[v.id];
      const saved = visibilitySaved[v.id];
      if (next === undefined || next === saved) continue;
      try {
        await storeApi.setSellerVariantVisibility(currentStore.slug, product.id, v.id, next); // F2: /seller/stores/{slug}/… resolves by SLUG only (GetStoreBySlug) — passing the store id ("store-mone") 404s. Margin overrides above correctly keep .id (backend checks ownership by ID).
        nextVisibilitySaved[v.id] = next;
      } catch (err: any) {
        setVisibilityInputs((prev) => ({ ...prev, [v.id]: saved ?? true }));
        nextVisibilitySaved[v.id] = saved ?? true;
        if (!firstError) firstError = err?.message || 'ذخیره نمایش تنوع انجام نشد.';
      }
    }
    setVisibilitySaved(nextVisibilitySaved);
    // P2(c) store default: if the chosen default is now hidden, the
    // store falls back to the first visible purchasable variant (or no
    // store default at all when nothing is visible).
    if (variantSettingsReady) {
      let defaultToSave = defaultInput;
      if (defaultToSave) {
        const defVariant = variants.find((v) => v.id === defaultToSave);
        const defVisible = defVariant ? (visibilityInputs[defVariant.id] ?? true) : false;
        if (!defVariant || !defVisible) {
          const fallback = variants.find((v) => (visibilityInputs[v.id] ?? true) && resolvePurchase(pricingProduct, v.id) != null);
          defaultToSave = fallback?.id ?? null;
        }
      }
      if (defaultToSave !== defaultSaved) {
        try {
          if (defaultToSave) await storeApi.setSellerStoreDefaultVariant(currentStore.slug, product.id, defaultToSave); // F2: slug, not id (see visibility save above)
          else await storeApi.deleteSellerStoreDefaultVariant(currentStore.slug, product.id); // F2: slug, not id
          setDefaultSaved(defaultToSave);
        } catch (err: any) {
          setDefaultInput(defaultSaved);
          if (!firstError) firstError = err?.message || 'ذخیره پیش‌فرض فروشگاه انجام نشد.';
        }
      }
    }
    setVariantSaving(false);
    if (firstError) { error(firstError); return; }
    success('تنظیمات تنوع‌ها ذخیره شد.');
  };

  // Summary for variant products: computed from each variant's EFFECTIVE
  // percent (typed ?? saved ?? auto max) and its own base price —
  // the same numbers the backend charges with (variant override wins).
  const variantSummary = (() => {
    if (variantCount === 0) return null;
    const rowsV = pricingVariants.map((v) => {
      const typed = variantInputs[v.id] ?? '';
      const eff = typed !== '' && Number.isFinite(Number(typed)) && Number(typed) > 0 ? Number(typed) : maxAllowed;
      const preview = sellerVariantPreview(pricingProduct, v, productPct, eff);
      return { ...preview, eff };
    }).filter((row) => row.finalPrice != null && row.finalPrice > 0);
    if (rowsV.length === 0) return null;
    const pcts = rowsV.map((r) => r.eff);
    const finals = rowsV.map((r) => r.finalPrice!);
    const profits = rowsV.map((r) => r.profit ?? 0);
    return {
      hasCosts: rowsV.every((row) => row.baseCost != null),
      minPct: Math.min(...pcts), maxPct: Math.max(...pcts),
      minFinal: Math.min(...finals), maxFinal: Math.max(...finals),
      minProfit: Math.min(...profits), maxProfit: Math.max(...profits),
    };
  })();

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

          {variantCount > 0 && (
            <p className="rounded-xl bg-slate-50 border border-slate-200 p-2.5 text-[11px] leading-5 text-slate-600">
              این کالا تنوع دارد؛ قیمتش فقط از سود هر تنوع حساب می‌شود و سود جداگانه‌ای در سطح کالا ندارد. درصد هر تنوع را همین‌جا تعیین کن.
            </p>
          )}

          {variantCount > 0 && (
            <div className="space-y-2 p-3 bg-slate-50 border border-slate-200 rounded-xl">
              <p className="text-[11px] font-bold text-slate-800">سود هر تنوع (درصد روی قیمت پایه همان تنوع)</p>
              <p className="text-[10px] leading-5 text-slate-500">
                قیمت هر تنوع را تأمین‌کننده تعیین می‌کند؛ تو فقط درصد سود همان تنوع را می‌گذاری. تنوعی که خالی بماند، درصد محصول به آن نمی‌رسد؛ به‌صورت خودکار حداکثر سود مجاز پلتفرم ({toPersianDigits(maxAllowed)}٪) برای همان تنوع اعمال می‌شود.
              </p>
              <p className="text-[10px] leading-5 text-slate-500">
                بازه مجاز سود برای هر تنوع هم همان بازه پلتفرم است: {rangeText}
              </p>
              {pricingVariants.map((v) => {
                const typed = variantInputs[v.id] ?? '';
                const marginV = typed !== '' && Number.isFinite(Number(typed)) && Number(typed) > 0 ? Number(typed) : maxAllowed;
                const preview = sellerVariantPreview(pricingProduct, v, productPct, marginV);
                const baseV = preview.baseCost;
                const finalV = preview.finalPrice;
                const isVisible = visibilityInputs[v.id] ?? true;
                const isStoreDefault = defaultInput === v.id;
                return (
                  <div key={v.id} className={`space-y-2 bg-white border border-slate-200 rounded-xl p-2.5 ${!isVisible ? 'opacity-60' : ''}`}>
                    <div className="flex items-center gap-2">
                      <span className="flex-1 min-w-0 truncate text-[11px] font-bold text-slate-800">{v.variantName || v.name}</span>
                      <input
                        type="number"
                        min={0}
                        disabled={isLocked || variantSaving}
                        value={typed}
                        onChange={(e) => setVariantInputs((prev) => ({ ...prev, [v.id]: e.target.value }))}
                        placeholder={toPersianDigits(maxAllowed)}
                        className="w-20 shrink-0 bg-slate-50 border border-slate-200 rounded-lg px-2 py-1.5 text-center text-xs font-bold text-emerald-600 font-mono focus:outline-hidden"
                        aria-label={`درصد سود تنوع ${v.variantName || v.name}`}
                      />
                      <span className="shrink-0 text-[10px] font-bold text-slate-400">٪</span>
                    </div>
                    {!isCentralStore && !isMockMode() && (variantSettingsReady ? (
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 border-t border-slate-100 pt-1.5 text-[10px] text-slate-600">
                        <label className="flex items-center gap-2">
                          <button
                            type="button"
                            role="switch"
                            aria-checked={isVisible}
                            aria-label={`نمایش تنوع ${v.variantName || v.name} در فروشگاه`}
                            disabled={variantSaving}
                            onClick={() => setVisibilityInputs((prev) => ({ ...prev, [v.id]: !(prev[v.id] ?? true) }))}
                            className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ${isVisible ? 'bg-emerald-500' : 'bg-slate-300'}`}
                          >
                            <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${isVisible ? 'translate-x-5 rtl:-translate-x-5' : 'translate-x-1 rtl:-translate-x-1'}`} />
                          </button>
                          <span className="font-bold">در فروشگاه من</span>
                        </label>
                        <label className="flex items-center gap-1.5">
                          <input
                            type="radio"
                            name="store-default-variant"
                            checked={isStoreDefault}
                            disabled={variantSaving || !isVisible}
                            onChange={() => setDefaultInput(v.id)}
                            aria-label={`پیش‌فرض فروشگاه: ${v.variantName || v.name}`}
                          />
                          <span className="font-bold">پیش‌فرض فروشگاه</span>
                        </label>
                        {isStoreDefault && <span className="text-slate-400">پیش‌فرض فروشگاه</span>}
                      </div>
                    ) : (
                      <p className="border-t border-slate-100 pt-1.5 text-[10px] text-slate-400">در حال بارگذاری تنظیمات تنوع…</p>
                    ))}
                    {baseV != null && finalV != null ? (
                      <div className="flex items-center justify-between gap-2 border-t border-slate-100 pt-1.5 text-[10px] text-slate-500">
                        <span className="whitespace-nowrap">پایه: <span className="font-mono font-bold text-slate-700">{formatPrice(baseV)} ت</span></span>
                        <span className="whitespace-nowrap">نهایی: <span className="font-mono font-bold text-slate-700">{formatPrice(finalV)} ت</span></span>
                      </div>
                    ) : (
                      <p className="border-t border-slate-100 pt-1.5 text-[10px] text-slate-500">
                        {finalV != null ? `قیمت فعلی فروشگاه: ${formatPrice(finalV)} تومان. پیش‌نمایش سود پس از دریافت مبنای مجاز نمایش داده می‌شود.` : 'قیمت تنوع هنوز اعلام نشده است.'}
                      </p>
                    )}
                  </div>
                );
              })}
              {allVariantsHidden && (
                <p role="alert" className="rounded-xl bg-amber-50 border border-amber-200 p-2.5 text-[11px] leading-5 text-amber-800">
                  همه‌ی تنوع‌های این کالا در فروشگاهت خاموش شدند؛ کالا در فروشگاهت نمایش داده نمی‌شود.
                </p>
              )}
              {!isCentralStore && !isMockMode() && defaultInput && (
                <button type="button" disabled={variantSaving} onClick={() => setDefaultInput(null)} className="text-[10px] font-bold text-slate-500 underline underline-offset-2">حذف پیش‌فرض فروشگاه (بازگشت به پیش‌فرض کالا)</button>
              )}
              <button
                type="button"
                disabled={isLocked || variantSaving}
                onClick={handleSaveVariantMargins}
                className="w-full py-2 bg-slate-700 hover:bg-slate-800 text-white font-bold text-xs rounded-xl disabled:opacity-50"
              >
                {variantSaving ? 'در حال ذخیره…' : 'ذخیره تنظیمات تنوع‌ها'}
              </button>
            </div>
          )}

          {/* Section 3.2: Percent-only profit input (single-variant / plain products only) */}
          {variantCount === 0 && (
          <div>

            <div className={`relative flex items-center rounded-xl px-3 py-2 border transition-all ${
              !previewValid
                ? 'bg-rose-50/50 border-rose-300 ring-1 ring-rose-200'
                : 'bg-slate-50 border-slate-200 focus-within:bg-white focus-within:border-blue-500'
            }`}>
              <input
                type="number"
                min={minAllowed}
                disabled={isLocked}
                value={profitVal}
                onChange={(e) => setProfitVal(e.target.value)}
                placeholder={`خودکار: حداکثر مجاز (${toPersianDigits(maxAllowed)}٪)`}
                className="w-full bg-transparent text-xs font-bold text-emerald-600 font-mono focus:outline-hidden"
              />
              <span className="text-[10px] text-slate-400 font-bold mr-1 shrink-0">
                درصد سود این کالا
              </span>
            </div>

            <p className="mt-1 text-[10px] leading-5 text-slate-500">
              بازه مجاز سود در پلتفرم: {rangeText}
            </p>
            {profitVal === '' && (
              <p className="mt-1 text-[10px] leading-5 text-slate-500">
                این کالا درصد جداگانه ندارد؛ حداکثر سود مجاز به‌صورت خودکار اعمال می‌شود. برای سود دلخواه، درصد را همین‌جا بنویس و ذخیره کن.
              </p>
            )}
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

          )}

          {/* Section 3.3.b: Niche Bulk Checkbox (plain products only) */}
          {variantCount === 0 && (<>
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

          </>)}

          {variantCount > 0 && variantSummary ? (
            <div className="p-3 bg-emerald-50/70 border border-emerald-200 rounded-xl space-y-1">
              {variantSettingsReady && (
                <p className="text-[10px] text-emerald-800 font-bold">تنوع‌های فعال در فروشگاهت: {toPersianDigits(visibleVariantCount)} از {toPersianDigits(variantCount)}{defaultSaved ? ` · پیش‌فرض: ${pricingVariants.find((v) => v.id === defaultSaved)?.variantName || pricingVariants.find((v) => v.id === defaultSaved)?.name || ''}` : ''}</p>
              )}
              <div className="flex items-center justify-between">
                <span className="text-[10px] text-emerald-800 font-bold">{variantSummary.hasCosts ? 'قیمت نهایی تنوع‌ها (رند ۱۰۰ ت):' : 'قیمت فعلی تنوع‌ها در فروشگاه:'}</span>
                <span className="text-base font-black text-emerald-800 font-mono">
                  {variantSummary.minFinal === variantSummary.maxFinal
                    ? `${formatPrice(variantSummary.minFinal)} تومان`
                    : `از ${formatPrice(variantSummary.minFinal)} تومان`}
                </span>
              </div>
              {variantSummary.hasCosts && <div className="flex items-center justify-between text-[10px] text-emerald-700 pt-1 border-t border-emerald-200/60">
                <span>سود شما در هر فروش: <strong className="font-mono">+{formatPrice(variantSummary.minProfit)} ت{variantSummary.maxProfit !== variantSummary.minProfit ? ' به بالا' : ''}</strong></span>
                <span>حاشیه سود: {variantSummary.minPct === variantSummary.maxPct ? `٪${toPersianDigits(variantSummary.minPct)}` : `٪${toPersianDigits(variantSummary.minPct)} تا ٪${toPersianDigits(variantSummary.maxPct)}`}</span>
              </div>}
            </div>
          ) : variantCount > 0 ? (
            <p className="text-xs text-slate-500">قیمت تنوع‌ها هنوز اعلام نشده است.</p>
          ) : baseUnknown ? (
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-1">
              {previewValid ? (
                <>
                  <div className="flex items-center justify-between">
                    <span className="text-[10px] text-slate-700 font-bold">{profitVal === '' ? 'سود خودکار (حداکثر مجاز):' : 'سود انتخابی شما:'}</span>
                    <span className="text-base font-black text-slate-800 font-mono">`٪${toPersianDigits(fallbackPct)}`</span>
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
          {variantCount > 0 ? (
            <button
              type="button"
              onClick={onClose}
              className="w-full py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
            >
              بستن
            </button>
          ) : (
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
              disabled={!previewValid || isLocked}
              className="py-2.5 bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs disabled:opacity-50 disabled:cursor-not-allowed transition-all"
            >
              {applyToNiche ? 'مرحله بعد: بازبینی و تایید' : 'ذخیره و اعمال در ویترین'}
            </button>
          </div>
          )}
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

