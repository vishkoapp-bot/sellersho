import { markedUpPrice, parsePercent } from '../../utils/money';
import React, { useState, useEffect } from 'react';
import { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, type SellerVariantEffectiveMargin } from '../../services/storeApi';
import { catalogApi } from '../../services/catalogApi';
import { mapBackendProduct } from '../../context/ProductContext';
import { activeProductVariants, resolvePurchase, sellerVariantPreview } from '../../utils/productPurchase';
import { isMockMode } from '../../services/apiClient';
import { OFFICIAL_NICHES, getProductNicheIds } from '../../data/niches';
import { roundTo100, resolveBaseCostR, resolvePlatformPct, resolvePricing } from '../../utils/pricingEngine';
import { Layers, AlertTriangle } from 'lucide-react';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';

const secondaryAction = 'inline-flex min-h-11 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 font-bold text-slate-700 hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const primaryAction = 'inline-flex min-h-11 items-center justify-center rounded-xl bg-brand px-3 py-2 text-center font-extrabold text-white hover:bg-brand-deep focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50';

interface Props {
  product: Product | null;
  hasVariants?: boolean;
  isOpen: boolean;
  onClose: () => void;
  effectiveMargins?: ReadonlyMap<string, SellerVariantEffectiveMargin>;
}

interface ModalContentProps {
  product: Product;
  hasVariants?: boolean;
  onClose: () => void;
  effectiveMargins?: ReadonlyMap<string, SellerVariantEffectiveMargin>;
}

const SellerProductPricingModalContent: React.FC<ModalContentProps> = ({ product, onClose, effectiveMargins, hasVariants }) => {
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
  const platformPct = resolvePlatformPct(pricingProduct, platformSettings);

  // Live: store-level admin margin lock (candidate context carries only the
  // store flag; live also has per-product / per-niche locks — see report).
  const isLocked = !!currentStore.isMarginLockedByAdmin;
  const variantCount = pricingVariants.length;
  const isVariantProduct = !!hasVariants || product.productType === 'variable' || pricingProduct.productType === 'variable'
    || centralPricingProduct?.productType === 'variable' || !!product.variants?.length
    || variantCount > 0 || !!effectiveMargins?.size || !!products.find((p) => p.id === product.id)?.variants?.length;
  const showPlainPricing = !isVariantProduct && (isCentralStore || isMockMode() || centralFetchDone);
  const baseCostR = showPlainPricing ? resolveBaseCostR(pricingProduct, platformSettings) : 0;
  const visibleVariantCount = pricingVariants.filter((v) => visibilityInputs[v.id] !== false).length;
  const allVariantsHidden = variantSettingsReady && variantCount > 0 && visibleVariantCount === 0;
  const baseUnknown = !isFinite(baseCostR) || baseCostR <= 0;

  // Identify product niche
  const pNicheIds = product.nicheIds && product.nicheIds.length > 0 ? product.nicheIds : getProductNicheIds(product);
  const productNiche = OFFICIAL_NICHES.find((n) => pNicheIds.includes(n.id)) || OFFICIAL_NICHES[0];

  // Products in this niche for bulk impact preview
  const nicheProducts = products.filter((p) => {
    const ids = p.nicheIds && p.nicheIds.length > 0 ? p.nicheIds : getProductNicheIds(p);
    return p.productType !== 'variable' && !p.variants?.length && ids.includes(productNiche.id);
  });

  // Current product pricing state
  const existingOverride = isVariantProduct ? undefined : currentStore.productPriceOverrides?.[product.id];

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
  const effectiveCeiling = effectiveMargins?.values().next().value?.ceilingPct ?? maxAllowed;
  let validPrecision = false;
  try { parsePercent(numVal); validPrecision = true; } catch { /* invalid draft */ }
  const isProfitValid = profitVal !== '' && validPrecision && numVal >= minAllowed && numVal <= maxAllowed;
  // Wherever no percent is set, the maximum allowed margin applies
  // automatically (platform max; a per-product ceiling, when the
  // platform sets one, is enforced server-side and surfaced via the
  // save error path). Used for the plain-product preview.
  const fallbackPct = profitVal !== '' && !isNaN(numVal) ? numVal : maxAllowed;
  const previewValid = profitVal === '' || isProfitValid;

  // The allowed margin band comes from the server's pricing settings
  // (platformSettings), so the seller always sees the real range before
  // saving. Save failures are reported by kind: a 400 names the range, a
  // 403 is the admin lock, anything else is a server error — never blame
  // the range for a server failure.
  const rangeText = `از ${toPersianDigits(minAllowed)}٪ تا ${toPersianDigits(isVariantProduct ? effectiveCeiling : maxAllowed)}٪`;
  const saveErrorText = (status?: number): string => {
    if (status === 400) return `ذخیره انجام نشد؛ درصد سود باید در بازه مجاز پلتفرم باشد (${rangeText}).`;
    if (status === 403) return 'ذخیره انجام نشد؛ قیمت‌گذاری این فروشگاه توسط مدیر پلتفرم قفل شده است.';
    return 'ذخیره انجام نشد؛ خطای سرور رخ داد. چند لحظه دیگر دوباره تلاش کنید.';
  };

  // Priority 2.1: Use Canonical resolvePricing from pricingEngine
  const simulatedPricing = showPlainPricing ? resolvePricing(pricingProduct, {
    ...currentStore,
    productPriceOverrides: { ...(currentStore.productPriceOverrides || {}), [product.id]: previewValid ? fallbackPct : maxAllowed },
  }, platformSettings) : undefined;
  const finalPriceP = previewValid && simulatedPricing ? simulatedPricing.finalPrice : baseCostR;
  const netProfitToman = finalPriceP - baseCostR;
  // Displayed margin is the applied input margin (server definition), not
  // profit/final — so the number shown matches the number typed.
  const effectiveMarginPercent = previewValid ? simulatedPricing?.marginPercent ?? 0 : 0;

  const handleInitialSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (isVariantProduct) {
      if (variantCount > 0) await handleSaveVariantMargins();
      return;
    }
    if (!showPlainPricing) return;
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
  // (supplierBaseCost). As of Nov 5, an unset variant uses its effective
  // ceiling and never inherits a parent override. Drafts load per modal.
  const [variantInputs, setVariantInputs] = useState<Record<string, string>>({});
  const [variantSaved, setVariantSaved] = useState<Record<string, string>>({});
  const [variantSaving, setVariantSaving] = useState(false);

  useEffect(() => {
    if (!isVariantProduct || isMockMode() || !currentStore?.id) return;
    let cancelled = false;
    storeApi.getSellerVariantOverrides(currentStore.id)
      .then((rows) => {
        if (cancelled) return;
        const next: Record<string, string> = {};
        for (const row of rows) {
          if (row.productId === product.id && row.mode === 'percent') {
            next[row.variantId] = String(row.value);
          }
        }
        setVariantSaved(next);
        setVariantInputs(next);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [isVariantProduct, currentStore.id, product.id]);

  const handleSaveVariantMargins = async () => {
    if (isLocked || variantSaving) return;
    const variants = pricingVariants;
    for (const v of variants) {
      const raw = variantInputs[v.id];
      if (raw === undefined || raw === '') continue;
      const n = Number(raw);
      if (!Number.isFinite(n) || (n !== 0 && (n < minAllowed || n > effectiveCeiling))) {
        error(`درصد سود تنوع «${v.variantName || v.name}» باید در بازه ${rangeText} باشد (صفر برای حذف سود سفارشی).`);
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

  const previewVariant = (variant: typeof pricingVariants[number]) => {
    const resolved = effectiveMargins?.get(variant.id);
    const draft = variantInputs[variant.id];
    const typed = Number(draft);
    const draftMargin = Number.isFinite(typed) && typed > 0 ? Math.min(typed, effectiveCeiling) : effectiveCeiling;
    const margin = resolved && (resolved.locked || draft === undefined) ? resolved.marginPct : draftMargin;
    const baseCost = resolved?.referenceCost;
    if (baseCost == null) return { ...sellerVariantPreview(pricingProduct, variant, platformPct, margin), margin };
    try {
      const resolvedPrice = resolved?.marginPct === margin ? resolved.finalPrice : undefined;
      const finalPrice = resolvedPrice ?? markedUpPrice(baseCost, margin);
      return { baseCost, finalPrice, profit: finalPrice - baseCost, margin };
    } catch {
      return { baseCost, margin };
    }
  };

  const variantSummary = (() => {
    if (variantCount === 0) return null;
    const rowsV = pricingVariants.map(previewVariant).filter((row) => row.finalPrice != null && row.finalPrice > 0);
    if (rowsV.length === 0) return null;
    const pcts = rowsV.map((r) => r.margin);
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
    <SupplierDialog titleId="seller-profit-title" onDismiss={variantSaving ? undefined : onClose} wide={isVariantProduct}>
        <SupplierDialogHeading id="seller-profit-title" title="تنظیم سود کالا" detail={product.title} onClose={onClose} busy={variantSaving} />

        {isLocked && (
          <div role="status" className="mb-3 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
            مدیر پلتفرم قیمت‌گذاری این فروشگاه را قفل کرده است. تغییر سود فعلاً ممکن نیست.
          </div>
        )}

        <form onSubmit={handleInitialSubmit} className="space-y-4 text-sm">
          {/* Section 3.1: Single unified base price (R) */}
          {showPlainPricing && <div className="bg-slate-50 p-3 rounded-xl border border-slate-200/80 flex items-center justify-between gap-3">
            <span className="font-bold text-slate-700">قیمت پایه</span>
            <span className="font-extrabold tabular-nums text-slate-900">{baseUnknown ? 'اعلام نشده' : `${formatPrice(baseCostR)} تومان`}</span>
          </div>}

          {variantCount > 0 && (
            <div className="space-y-3">
              <h4 className="text-sm font-extrabold text-[#261b41]">سود و وضعیت نمایش تنوع‌ها</h4>
              <p className="text-sm leading-6 text-slate-600">
                بازه مجاز سود هر تنوع {rangeText} است. برای فیلد خالی، حداکثر سود مجاز کالا ({toPersianDigits(effectiveCeiling)}٪) اعمال می‌شود.
              </p>
              {pricingVariants.map((v) => {
                const typed = variantInputs[v.id] ?? '';
                const preview = previewVariant(v);
                const baseV = preview.baseCost;
                const finalV = preview.finalPrice;
                const isVisible = visibilityInputs[v.id] ?? true;
                const isStoreDefault = defaultInput === v.id;
                return (
                  <div key={v.id} className={`space-y-2 rounded-2xl border border-slate-200 p-3 ${isVisible ? 'bg-white' : 'bg-slate-50'}`}>
                    <div className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 break-words font-extrabold text-[#261b41]">{v.variantName || v.name}</span>
                      <input
                        type="number"
                        inputMode="decimal"
                        step="any"
                        disabled={isLocked || variantSaving}
                        value={typed}
                        onChange={(e) => setVariantInputs((prev) => ({ ...prev, [v.id]: e.target.value }))}
                        placeholder={toPersianDigits(effectiveCeiling)}
                        className="min-h-11 w-20 shrink-0 rounded-xl border border-slate-200 bg-white px-2 text-center font-bold tabular-nums text-slate-800 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
                        aria-label={`درصد سود تنوع ${v.variantName || v.name}`}
                      />
                      <span aria-hidden="true" className="shrink-0 text-xs font-bold text-slate-600">٪</span>
                    </div>
                    {!isCentralStore && !isMockMode() && (variantSettingsReady ? (
                      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-slate-100 pt-1.5 text-xs text-slate-700">
                          <button
                            type="button"
                            role="switch"
                            aria-checked={isVisible}
                            aria-label={`نمایش تنوع ${v.variantName || v.name} در فروشگاه`}
                            disabled={variantSaving}
                            onClick={() => setVisibilityInputs((prev) => ({ ...prev, [v.id]: !(prev[v.id] ?? true) }))}
                            className="flex min-h-11 items-center gap-2 rounded-xl px-2 font-bold hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
                          >
                            <span dir="ltr" aria-hidden="true" className={`inline-flex h-6 w-11 shrink-0 items-center rounded-full px-0.5 ${isVisible ? 'bg-brand-deep' : 'bg-slate-400'}`}>
                              <span className={`size-5 rounded-full bg-white shadow motion-safe:transition-transform ${isVisible ? 'translate-x-5' : ''}`} />
                            </span>
                            در فروشگاه من
                          </button>
                        <label className="flex min-h-11 items-center gap-2 rounded-xl px-2">
                          <input
                            type="radio"
                            name="store-default-variant"
                            checked={isStoreDefault}
                            disabled={variantSaving || !isVisible}
                            onChange={() => setDefaultInput(v.id)}
                            aria-label={`پیش‌فرض فروشگاه: ${v.variantName || v.name}`}
                            className="size-4 shrink-0 accent-brand-deep focus-visible:ring-2 focus-visible:ring-brand"
                          />
                          <span className="font-bold">پیش‌فرض فروشگاه</span>
                        </label>
                      </div>
                    ) : (
                      <p className="border-t border-slate-100 pt-1.5 text-xs text-slate-600">در حال بارگذاری تنظیمات تنوع…</p>
                    ))}
                    {baseV != null && finalV != null ? (
                      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-slate-100 pt-2 text-xs text-slate-600">
                        <span>قیمت پایه <strong className="tabular-nums text-slate-800">{formatPrice(baseV)} تومان</strong></span>
                        <span>قیمت فروش <strong className="tabular-nums text-slate-800">{formatPrice(finalV)} تومان</strong></span>
                      </div>
                    ) : (
                      <p className="border-t border-slate-100 pt-2 text-xs leading-5 text-slate-600">
                        {finalV != null ? `قیمت فعلی فروشگاه: ${formatPrice(finalV)} تومان. پیش‌نمایش سود پس از دریافت مبنای مجاز نمایش داده می‌شود.` : 'قیمت تنوع هنوز اعلام نشده است.'}
                      </p>
                    )}
                  </div>
                );
              })}
              {allVariantsHidden && (
                <p role="alert" className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
                  همه‌ی تنوع‌های این کالا در فروشگاهت خاموش شدند؛ کالا در فروشگاهت نمایش داده نمی‌شود.
                </p>
              )}
              {!isCentralStore && !isMockMode() && defaultInput && (
                <button type="button" disabled={variantSaving} onClick={() => setDefaultInput(null)} className={secondaryAction}>بازگشت به تنوع پیش‌فرض کالا</button>
              )}
            </div>
          )}

          {/* Section 3.2: Percent-only profit input (single-variant / plain products only) */}
          {showPlainPricing && (
          <div className="space-y-1.5">
            <label htmlFor="seller-product-margin" className="block font-bold text-slate-800">درصد سود این کالا</label>
            <div className={`flex min-h-11 items-center gap-2 rounded-xl border px-3 focus-within:ring-2 focus-within:ring-brand ${
              previewValid ? 'border-slate-200 bg-slate-50' : 'border-rose-300 bg-rose-50'}`}>
              <input
                id="seller-product-margin"
                aria-describedby={previewValid ? 'seller-product-margin-range' : 'seller-product-margin-error'}
                aria-invalid={!previewValid}
                type="number"
                inputMode="decimal"
                step="any"
                min={minAllowed}
                disabled={isLocked}
                value={profitVal}
                onChange={(e) => setProfitVal(e.target.value)}
                placeholder={toPersianDigits(maxAllowed)}
                className="min-h-11 min-w-0 flex-1 bg-transparent font-bold tabular-nums text-slate-800 focus:outline-hidden"
              />
              <span aria-hidden="true" className="shrink-0 font-bold text-slate-600">٪</span>
            </div>

            <p id="seller-product-margin-range" className="text-sm leading-6 text-slate-600">
              بازه مجاز سود {rangeText} است.
            </p>
            {profitVal === '' && (
              <p className="text-sm leading-6 text-slate-600">
                اگر این فیلد را خالی بگذاری، حداکثر سود مجاز به‌صورت خودکار اعمال می‌شود.
              </p>
            )}
            {/* Requirement 1: Real-time validation warning */}
            {!isProfitValid && profitVal !== '' && (
              <p id="seller-product-margin-error" className="flex items-start gap-2 text-sm leading-6 text-rose-800">
                <AlertTriangle aria-hidden="true" className="mt-1 size-4 shrink-0" strokeWidth={1.75} />
                درصد سود باید {rangeText} باشد.
              </p>
            )}
          </div>

          )}

          {/* Section 3.3.b: Niche Bulk Checkbox (plain products only) */}
          {showPlainPricing && (<>
          <label className="flex min-h-11 cursor-pointer items-center justify-between gap-3 rounded-xl border border-brand-border bg-brand-bg p-3 focus-within:ring-2 focus-within:ring-brand">
            <span className="flex min-w-0 items-start gap-2">
              <Layers aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-brand-deep" strokeWidth={1.75} />
              <span>
                <span className="block font-bold text-slate-800">
                  اعمال روی کالاهای سادهٔ نیچ «{productNiche.title}»
                </span>
                <span className="block text-xs leading-6 text-slate-600">
                  {toPersianDigits(nicheProducts.length)} کالا برای بازبینی در مرحله بعد
                </span>
              </span>
            </span>
            <input
              type="checkbox"
              disabled={isLocked}
              checked={applyToNiche}
              onChange={(e) => setApplyToNiche(e.target.checked)}
              className="size-4 shrink-0 accent-brand-deep"
            />
          </label>

          </>)}

          {variantCount > 0 && variantSummary ? (
            <div className="space-y-2 rounded-xl border border-emerald-200 bg-emerald-50 p-3">
              {variantSettingsReady && (
                <p className="text-xs leading-6 text-emerald-900">تنوع‌های فعال در فروشگاهت: {toPersianDigits(visibleVariantCount)} از {toPersianDigits(variantCount)}{defaultSaved ? ` · پیش‌فرض: ${pricingVariants.find((v) => v.id === defaultSaved)?.variantName || pricingVariants.find((v) => v.id === defaultSaved)?.name || ''}` : ''}</p>
              )}
              <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
                <span className="font-bold text-emerald-900">{variantSummary.hasCosts ? 'قیمت فروش تنوع‌ها' : 'قیمت فعلی تنوع‌ها در فروشگاه'}</span>
                <span className="text-base font-extrabold tabular-nums text-emerald-900">
                  {variantSummary.minFinal === variantSummary.maxFinal
                    ? `${formatPrice(variantSummary.minFinal)} تومان`
                    : `از ${formatPrice(variantSummary.minFinal)} تومان`}
                </span>
              </div>
              {variantSummary.hasCosts && <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-emerald-200 pt-2 text-xs leading-5 text-emerald-900">
                <span>سود تو از هر فروش <strong className="tabular-nums">{formatPrice(variantSummary.minProfit)} تومان{variantSummary.maxProfit !== variantSummary.minProfit ? ' به بالا' : ''}</strong></span>
                <span>حاشیه سود {variantSummary.minPct === variantSummary.maxPct ? `${toPersianDigits(variantSummary.minPct)}٪` : `${toPersianDigits(variantSummary.minPct)}٪ تا ${toPersianDigits(variantSummary.maxPct)}٪`}</span>
              </div>}
            </div>
          ) : isVariantProduct ? (
            <p className="text-sm text-slate-600">قیمت تنوع‌ها هنوز اعلام نشده است.</p>
          ) : !showPlainPricing ? null : baseUnknown ? (
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-1">
              {previewValid ? (
                <>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-bold text-slate-700">{profitVal === '' ? 'سود خودکار (حداکثر مجاز)' : 'سود انتخابی تو'}</span>
                    <span className="text-base font-extrabold tabular-nums text-slate-800">{toPersianDigits(fallbackPct)}٪</span>
                  </div>
                  <p className="text-sm leading-6 text-slate-600">بعد از اعلام قیمت خرید، سودی که اینجا تعیین می‌کنی روی آن اعمال می‌شود.</p>
                </>
              ) : (
                <p className="text-sm leading-6 text-slate-600">برای ذخیره، درصد سود را در بازه مجاز وارد کن.</p>
              )}
            </div>
          ) : (
          <div className="space-y-2 rounded-xl border border-emerald-200 bg-emerald-50 p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-bold text-emerald-900">قیمت فروش به مشتری</span>
              <span className="text-base font-extrabold tabular-nums text-emerald-900">{formatPrice(finalPriceP)} تومان</span>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-emerald-200 pt-2 text-xs leading-5 text-emerald-900">
              <span>سود تو از هر فروش <strong className="tabular-nums">{formatPrice(netProfitToman)} تومان</strong></span>
              <span>حاشیه سود {toPersianDigits(effectiveMarginPercent)}٪</span>
            </div>
          </div>

          )}
          <div className="flex flex-wrap justify-end gap-2 border-t border-slate-100 pt-3">
            <button type="button" onClick={onClose} disabled={variantSaving} className={secondaryAction}>
              {showPlainPricing ? 'انصراف' : 'بستن'}
            </button>
            {showPlainPricing && <button type="submit" disabled={!previewValid || isLocked} className={primaryAction}>
              {applyToNiche ? 'بازبینی و تأیید' : 'ذخیره و اعمال در ویترین'}
            </button>}
            {variantCount > 0 && <button type="submit" disabled={isLocked || variantSaving} className={primaryAction}>
              {variantSaving ? 'در حال ذخیره…' : 'ذخیره تنظیمات تنوع‌ها'}
            </button>}
          </div>
        </form>

        {/* Requirement 3: Two-Step Confirmation Modal for Bulk Niche Pricing */}
        {showPlainPricing && showNicheConfirmModal && (
          <SupplierDialog titleId="seller-niche-profit-title" onDismiss={() => setShowNicheConfirmModal(false)}>
              <SupplierDialogHeading id="seller-niche-profit-title" title="بازبینی سود گروهی" onClose={() => setShowNicheConfirmModal(false)} />
              <div className="space-y-3 text-sm leading-6 text-slate-600">
                <p>
                  نرخ سود <strong className="text-brand-deep">{toPersianDigits(numVal)}٪</strong> روی کالاهای سادهٔ موضوع <strong className="text-slate-900">«{productNiche.title}»</strong> اعمال می‌شود.
                </p>

                <div className="space-y-2 rounded-xl border border-brand-border bg-brand-bg p-3">
                  <p className="font-bold text-[#261b41]">کالاهای این موضوع ({toPersianDigits(nicheProducts.length)} کالا)</p>
                  <ul className="max-h-36 divide-y divide-brand-border overflow-y-auto">
                    {nicheProducts.map((p) => (
                      <li key={p.id} className="flex flex-wrap justify-between gap-x-2 py-1.5 text-xs leading-5">
                        <span className="min-w-0 break-words">{p.title}</span>
                        <span className="tabular-nums text-slate-600">قیمت پایه {formatPrice(resolveBaseCostR(p, platformSettings))} تومان</span>
                      </li>
                    ))}
                  </ul>
                </div>

                <p className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
                  این نرخ فقط برای کالاهای ساده‌ای اعمال می‌شود که سود جداگانه ندارند.
                </p>
              </div>

              <div className="mt-3 flex flex-wrap justify-end gap-2 border-t border-slate-100 pt-3 text-sm">
                <button
                  type="button"
                  onClick={() => setShowNicheConfirmModal(false)}
                  className={secondaryAction}
                >
                  انصراف و بازگشت
                </button>
                <button
                  type="button"
                  onClick={handleConfirmNicheBulkApply}
                  className={primaryAction}
                >
                  تأیید نهایی و اعمال گروهی
                </button>
              </div>
          </SupplierDialog>
        )}
    </SupplierDialog>
  );
};


export const SellerProductPricingModal: React.FC<Props> = ({ product, isOpen, onClose, effectiveMargins, hasVariants }) => {
  if (!isOpen || !product) return null;
  return <SellerProductPricingModalContent key={product.id} product={product} onClose={onClose} effectiveMargins={effectiveMargins} hasVariants={hasVariants} />;
};

