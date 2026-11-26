import { markedUpPrice } from '../../utils/money';
import React, { useEffect, useRef, useState } from 'react';
import type { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { SellerProductPricingModal } from './SellerProductPricingModal';
import { SellerVariantWorkspace } from './SellerVariantWorkspace';
import { OFFICIAL_NICHES, getProductNicheIds } from '../../data/niches';
import { resolvePricing, resolvePlatformPct } from '../../utils/pricingEngine';
import { storeApi, type SellerProductCandidate, type SellerVariantEffectiveMargin } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { catalogApi, type ProductDto } from '../../services/catalogApi';
import { mapBackendProduct } from '../../context/ProductContext';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { SupplierEmptyState } from '../supplier/SupplierResourceState';
import { Search, Check, SlidersHorizontal, Layers, ChevronDown, Package, X } from 'lucide-react';

const searchBox = 'flex min-h-11 items-center gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 focus-within:border-brand focus-within:ring-1 focus-within:ring-brand';
const searchInput = 'min-h-11 min-w-0 flex-1 bg-transparent text-sm text-slate-800 placeholder:text-slate-500 focus:outline-hidden';
const secondaryAction = 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white px-3 text-sm font-bold text-slate-700 hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

// C5 (Batch 4b): cost-presence helpers. mapBackendProduct zeroes a
// missing baseCostB (ProductContext: supplierBaseCost is 0 when
// baseCostB is null), so post-mapping a missing cost is indistinguishable
// from a genuine 0. Presence is therefore captured from the raw scoped
// DTOs BEFORE mapping: the cost is missing iff for EVERY variant
// v.supplierBaseCost == null AND the product baseCostB == null
// (null/undefined only — NOT !cost, so a genuine 0 is a number and
// still renders computed 0/0/0).
export interface CatalogCostPresence {
  product: boolean;
  hasVariants?: true;
  variants: Record<string, boolean>;
}

export function buildCatalogCostPresence(items: ProductDto[]): Record<string, CatalogCostPresence> {
  const out: Record<string, CatalogCostPresence> = {};
  for (const d of items || []) {
    const variants: Record<string, boolean> = {};
    for (const v of d.variants || []) variants[v.id] = v.supplierBaseCost != null;
    out[d.id] = { product: d.baseCostB != null, variants };
    if (d.hasVariants) out[d.id].hasVariants = true;
  }
  return out;
}

// Effective cost of one variant row: the variant's own cost when the
// scoped DTO carried it, else the product's cost when the DTO carried
// that, else null (missing). No trailing `?? 0`: a missing cost must
// surface as «قیمت تأمین‌کننده در دسترس نیست», never as 0.
export function catalogVariantCost(
  variant: { supplierBaseCost?: number | null },
  productCost: number | null,
  variantKnown: boolean,
): number | null {
  if (variantKnown && variant.supplierBaseCost != null) return variant.supplierBaseCost;
  return productCost;
}

// Missing-only predicate for a row: the cost is missing iff NO cost is
// known for it — neither the product's nor any variant's. A missing
// presence entry (e.g. a row from the central fallback list, which by
// design carries no baseCostB) counts as missing, never as 0.
export function catalogRowCostMissing(
  product: { variants?: Array<{ id: string; supplierBaseCost?: number | null }> },
  presence?: CatalogCostPresence,
): boolean {
  if (!presence) return true;
  if (presence.product) return false;
  const variants = product.variants || [];
  return !variants.some((v) => presence.variants[v.id]);
}

export function shouldRefetchCatalogAfterConfirm(
  productDiffCount: number,
  putResult: { ok: boolean } | null,
): boolean {
  return productDiffCount > 0 && putResult !== null && putResult.ok === true;
}

// Scoped-fetch outcome -> the tab-level error state. A rejected walk,
// an incomplete walk, and a non-array resolve are all the same visible
// failure state ('walk'); a walk the server answered without the
// inactive carve-out is the re-login state ('inactive'). A COMPLETE
// walk is a success even when the assortment is genuinely empty —
// an empty + complete walk proceeds to setScopedProducts([]) and the
// empty state (pre-Batch 4 behavior), never the retry error card.
export function resolveScopedCatalogOutcome(
  result: { items?: unknown; complete?: boolean; includeInactiveApplied?: boolean } | null | undefined,
): 'ok' | 'walk' | 'inactive' {
  if (!result || !Array.isArray(result.items)) return 'walk';
  if (!result.complete) return 'walk';
  if (result.includeInactiveApplied !== true) return 'inactive';
  return 'ok';
}

export function addedProductIds(pending: string[], saved: string[]): string[] {
  const savedIds = new Set(saved);
  return pending.filter((id) => !savedIds.has(id));
}

export function removedProductIds(saved: string[], pending: string[]): string[] {
  return addedProductIds(saved, pending);
}

export function unionUndoProductIds(freshIds: string[], removedIds: string[]): string[] {
  return [...new Set([...freshIds, ...removedIds])];
}

export function staleAddedIds(addedIds: string[], eligibleIds: string[]): string[] {
  return addedProductIds(addedIds, eligibleIds);
}

// Product changes stay in the draft until the seller confirms or cancels.
export const SellerCatalogTab: React.FC<{ initialWorkFilter?: 'attention' }> = ({ initialWorkFilter }) => {
  const {
    products,
    currentStore,
    setCurrentStore,
    updateStoreActiveProducts,
    updateStoreSelectedNiches,
    platformSettings,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error } = useToast();

  const [nicheSearch, setNicheSearch] = useState('');
  const [extraSearch, setExtraSearch] = useState('');
  const [showExtraSection, setShowExtraSection] = useState(false);
  const [pricingProduct, setPricingProduct] = useState<Product | null>(null);
  const [workFilter, setWorkFilter] = useState(initialWorkFilter);
  useEffect(() => { setWorkFilter(initialWorkFilter); }, [initialWorkFilter]);
  const [showNicheHelp, setShowNicheHelp] = useState(false);
  const [saving, setSaving] = useState(false);
  const [nicheSaving, setNicheSaving] = useState(false);
  const [confirmBaseline, setConfirmBaseline] = useState<string[] | null>(null);
  const [undoableRemoval, setUndoableRemoval] = useState<string[] | null>(null);
  const [undoSaving, setUndoSaving] = useState(false);
  const [pickerQuery, setPickerQuery] = useState('');
  const [pickerResults, setPickerResults] = useState<SellerProductCandidate[]>([]);
  const [pickerLoading, setPickerLoading] = useState(true);
  const [pickerError, setPickerError] = useState<string | null>(null);
  const [pickerHasMore, setPickerHasMore] = useState(false);
  const [pickerCursor, setPickerCursor] = useState<string | null>(null);
  const [pickerTitleById, setPickerTitleById] = useState<Record<string, string>>({});
  const pickerRequestRef = useRef(0);
  const nicheSavingRef = useRef(false);
  const suppressPendingResyncRef = useRef(false);
  const productsBusy = saving || undoSaving;

  // Fix80: the context catalogue is the UNSCOPED central list, which by
  // design carries no baseCostB (live parity) — resolvePricing on it
  // yields 0/0/0. The seller's own store view (?storeSlug=) DOES carry
  // baseCostB (= C); fetch it for this tab's rows, falling back to the
  // context list while it loads or on failure.
  const [scopedProducts, setScopedProducts] = useState<Product[] | null>(null);
  // C5: cost presence captured from the raw scoped DTOs before mapping
  // (see buildCatalogCostPresence) — keyed by product id, so a missing
  // cost renders «قیمت تأمین‌کننده در دسترس نیست» instead of 0/0/0.
  const [costPresence, setCostPresence] = useState<Record<string, CatalogCostPresence>>({});
  // M-1/M-2: the scoped walk can fail mid-way, or answer without the
  // inactive carve-out applied. scopedError records which: 'walk'
  // (incomplete walk) or 'inactive' (asked for inactive, server did
  // not apply it — expired token / lost ownership). While it is set
  // the tab keeps the PREVIOUS scoped list (or an empty error state
  // when there is none) and never falls back to the central list.
  const [scopedError, setScopedError] = useState<'walk' | 'inactive' | null>(null);
  const [walkRetry, setWalkRetry] = useState(0);
  const storeSlug = currentStore?.ownerType === 'dropshipper' ? currentStore?.slug : undefined;
  // Refetch when the pricing modal closes too: visibility/default changes
  // saved there must show in the list immediately (same staleness trap
  // as the variant percents below). The list is only blanked when the
  // store itself changes, never on a plain refetch.
  const prevSlugRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!storeSlug) return;
    if (pricingProduct) return;
    const slugChanged = prevSlugRef.current !== storeSlug;
    prevSlugRef.current = storeSlug;
    if (slugChanged) { setScopedProducts(null); setScopedError(null); setCostPresence({}); }
    let cancelled = false;
    // PERF-01: the seller manages the WHOLE assortment here, so this
    // admin surface walks all pages (bounded requests, cursor-driven)
    // instead of one unbounded fetch. Public surfaces never do this.
    catalogApi.fetchAllProductPages({ storeSlug, includeInactive: true })
      .then(({ items, complete, includeInactiveApplied }) => {
        if (cancelled) return;
        // C5: a non-array resolve is the same failure state as a
        // rejected or incomplete walk (resolveScopedCatalogOutcome).
        // An empty + complete walk is NOT an error — it is a genuinely
        // empty assortment and proceeds to setScopedProducts([]).
        // M-1/M-2: a failed walk leaves the previous scoped list in
        // place (or the error state below when there is none) — the
        // central catalogue is never shown as the store's assortment.
        const outcome = resolveScopedCatalogOutcome({ items, complete, includeInactiveApplied });
        if (outcome !== 'ok') { setScopedError(outcome); return; }
        setScopedError(null);
        setCostPresence(buildCatalogCostPresence(items));
        setScopedProducts(items.map(mapBackendProduct));
      })
      .catch(() => { if (!cancelled) setScopedError('walk'); });
    return () => { cancelled = true; };
  }, [storeSlug, pricingProduct, walkRetry]);
  // M-1: while the walk has errored and no scoped list was ever
  // loaded, show an empty error state — never the central list.
  const displayProducts = scopedProducts ?? (scopedError ? [] : products);
  // C5: while the scoped fetch is still pending, the tab is loading —
  // the central list (which carries no baseCostB by design) must not
  // flash as rows of 0/0/0. A previous scoped list stays visible on
  // refetch; only the first load, with nothing yet, shows loading.
  const scopedLoading = !!storeSlug && scopedProducts === null && !scopedError;

  const loadPickerPage = async (slug: string, query: string, cursor?: string) => {
    const request = ++pickerRequestRef.current;
    setPickerLoading(true);
    setPickerError(null);
    try {
      const response = await storeApi.getSellerProductCandidates({
        storeSlug: slug, q: query, ...(cursor ? { cursor } : {}),
      });
      if (request !== pickerRequestRef.current) return;
      if (!Array.isArray(response.products)) throw new Error('Invalid product candidates response');
      setPickerResults((previous) => cursor
        ? [...new Map([...previous, ...response.products].map((product) => [product.id, product])).values()]
        : response.products);
      setPickerCursor(response.nextCursor);
      setPickerHasMore(response.hasMore);
    } catch {
      if (request === pickerRequestRef.current) setPickerError('دریافت فهرست کالاها ناموفق بود');
    } finally {
      if (request === pickerRequestRef.current) setPickerLoading(false);
    }
  };

  useEffect(() => {
    if (!showExtraSection || !storeSlug) return;
    setPickerLoading(true);
    setPickerError(null);
    setPickerResults([]);
    setPickerHasMore(false);
    setPickerCursor(null);
    const timer = window.setTimeout(() => {
      void loadPickerPage(storeSlug, pickerQuery);
    }, 300);
    return () => {
      window.clearTimeout(timer);
      pickerRequestRef.current += 1;
    };
  }, [showExtraSection, storeSlug, pickerQuery, walkRetry]);

  // The context updates optimistically. Keep comparing against the server
  // baseline until the PUT succeeds or a server read supplies a fresh one.
  const savedProductIds = confirmBaseline ?? currentStore.activeProductIds ?? [];
  const savedNicheIds = currentStore.selectedNicheIds || [];

  const [pendingProductIds, setPendingProductIds] = useState<string[]>(savedProductIds);
  const [pendingNicheIds, setPendingNicheIds] = useState<string[]>(savedNicheIds);

  // Re-sync the draft only when the SAVED values actually change (e.g. after
  // a confirm persists), never on unrelated store updates.
  const savedProductsKey = savedProductIds.join(',');
  const savedNichesKey = savedNicheIds.join(',');
  useEffect(() => {
    if (confirmBaseline !== null) return;
    if (suppressPendingResyncRef.current) {
      suppressPendingResyncRef.current = false;
      return;
    }
    setPendingProductIds(savedProductIds);
  }, [savedProductsKey, confirmBaseline]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setPendingNicheIds(savedNicheIds); }, [savedNichesKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const productDiffCount =
    addedProductIds(pendingProductIds, savedProductIds).length +
    removedProductIds(savedProductIds, pendingProductIds).length;
  const hasChanges = productDiffCount > 0;

  useEffect(() => {
    if (saving && !hasChanges) setSaving(false);
  }, [saving, hasChanges]);

  const toggleNiche = async (nicheId: string) => {
    if (nicheSavingRef.current || productsBusy) return;
    let next: string[];
    if (pendingNicheIds.includes(nicheId)) {
      if (pendingNicheIds.length === 1) return; // Live label: "یک یا چند تا"
      next = pendingNicheIds.filter((id) => id !== nicheId);
    } else {
      next = [...pendingNicheIds, nicheId];
    }
    if (!currentStore) return;
    const serverAcked = currentStore.selectedNicheIds || [];
    setPendingNicheIds(next);
    if (!currentStore.slug || isMockMode()) {
      updateStoreSelectedNiches(next);
      return;
    }
    nicheSavingRef.current = true;
    setNicheSaving(true);
    try {
      const returned = await storeApi.saveStoreSettings(currentStore.slug, { selectedNicheIds: next });
      const selected = returned.selectedNicheIds ?? next;
      setPendingNicheIds(selected);
      updateStoreSelectedNiches(selected);
    } catch {
      setPendingNicheIds(serverAcked);
      error('ذخیره نیچ انجام نشد؛ دوباره تلاش کن');
    } finally {
      nicheSavingRef.current = false;
      setNicheSaving(false);
    }
  };

  const toggleProductSelect = (id: string) => {
    if (productsBusy) return;
    setPendingProductIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  };

  const pickerRows = pickerResults.filter((product) => !pendingProductIds.includes(product.id));
  const addPickerProduct = ({ id, title }: SellerProductCandidate) => {
    if (productsBusy) return;
    setPendingProductIds((previous) => previous.includes(id) ? previous : [...previous, id]);
    setPickerTitleById((previous) => ({ ...previous, [id]: title }));
  };

  const findOwnedStore = async () => {
    const owned = await storeApi.getOwnedStores();
    return owned.find((store) => store.id === currentStore.id);
  };

  const restoreProducts = async (preserveDraft: boolean) => {
    try {
      const freshStore = await findOwnedStore();
      if (!freshStore) return false;
      suppressPendingResyncRef.current = preserveDraft;
      setCurrentStore(freshStore);
      return true;
    } catch {
      return false;
    }
  };

  const revalidateAddedProducts = async (ids: string[]) => {
    if (!ids.length || !storeSlug || isMockMode()) return [];
    try {
      const { products: eligible } = await storeApi.getSellerProductCandidates({ storeSlug, ids });
      return staleAddedIds(ids, eligible.map((product) => product.id));
    } catch (err) {
      console.warn('Failed to re-validate seller product candidates:', err);
      return [];
    }
  };

  const dropStaleProducts = (stale: string[]) => {
    if (!stale.length) return;
    const staleIds = new Set(stale);
    setPendingProductIds((previous) => previous.filter((id) => !staleIds.has(id)));
    for (const id of staleIds) {
      const title = pickerTitleById[id] ?? displayProducts.find((product) => product.id === id)?.title ?? id;
      error(`«${title}» دیگر قابل افزودن نیست و از تغییرات حذف شد`);
    }
  };

  const handleConfirm = async () => {
    if (productsBusy || nicheSavingRef.current) return;
    setSaving(true);
    const savedBefore = [...savedProductIds];
    const pendingBefore = [...pendingProductIds];
    const previousBaseline = confirmBaseline;
    setConfirmBaseline(savedBefore);
    let baselineAfterSave: string[] | null = previousBaseline;
    try {
      const added = addedProductIds(pendingBefore, savedBefore);
      const stale = await revalidateAddedProducts(added);
      dropStaleProducts(stale);
      const remaining = pendingBefore.filter((id) => !stale.includes(id));
      const removed = removedProductIds(savedBefore, remaining);
      const remainingDiff = addedProductIds(remaining, savedBefore).length + removed.length;
      if (remainingDiff === 0) return;

      const result = await updateStoreActiveProducts(remaining);
      if (result.ok) {
        baselineAfterSave = null;
        setUndoableRemoval(removed.length > 0 ? removed : null);
        if (shouldRefetchCatalogAfterConfirm(remainingDiff, result)) setWalkRetry((n) => n + 1);
      } else {
        // If the rollback read fails, the context still has the optimistic
        // list. Retain the acknowledged baseline here so the draft can retry.
        baselineAfterSave = await restoreProducts(true) ? null : savedBefore;
        const newlyStale = (await revalidateAddedProducts(added)).filter((id) => remaining.includes(id));
        dropStaleProducts(newlyStale);
        if (!newlyStale.length) error('ذخیره کالاها انجام نشد؛ دوباره تلاش کن');
      }
    } finally {
      setConfirmBaseline(baselineAfterSave);
      setSaving(false);
    }
  };

  const handleUndo = async () => {
    if (!currentStore || !undoableRemoval || productsBusy || nicheSavingRef.current) return;
    setUndoSaving(true);
    try {
      const freshStore = await findOwnedStore();
      if (!freshStore) throw new Error('Owned store missing');
      const union = unionUndoProductIds(freshStore.activeProductIds || [], undoableRemoval);
      const result = await updateStoreActiveProducts(union);
      if (!result.ok) {
        if (await restoreProducts(false)) setConfirmBaseline(null);
        error('بازگردانی انجام نشد؛ دوباره تلاش کن');
        return;
      }
      setConfirmBaseline(null);
      setWalkRetry((n) => n + 1);
      setUndoableRemoval(null);
      success('کالاهای حذف‌شده بازگردانده شدند');
    } catch {
      error('بازگردانی انجام نشد؛ دوباره تلاش کن');
    } finally {
      setUndoSaving(false);
    }
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
  // G10: derive niches exactly like §1 above (raw nicheIds || [] let a
  // product with empty nicheIds and a category-derived niche appear in
  // BOTH sections). Belt-and-braces: also exclude ids already in §1.
  const nicheProductIds = new Set(nicheProducts.map((p) => p.id));
  const outsideNicheProducts = displayProducts.filter((p) => {
    if (nicheProductIds.has(p.id)) return false;
    const pNiches = p.nicheIds && p.nicheIds.length > 0 ? p.nicheIds : getProductNicheIds(p);
    const isOutside = !pNiches.some((nId) => pendingNicheIds.includes(nId));
    const matchesSearch = !extraSearch || p.title.includes(extraSearch);
    return isOutside && matchesSearch;
  });

  const [variantMargins, setVariantMargins] = useState<{
    storeId: string;
    byProduct: Map<string, Map<string, SellerVariantEffectiveMargin>>;
  }>();
  const [marginRefresh, setMarginRefresh] = useState(0);
  const storeId = currentStore.id;
  const effectiveByProduct = variantMargins?.storeId === storeId ? variantMargins.byProduct : undefined;
  useEffect(() => {
    // Re-read both sources when the modal closes, including after clearing a margin.
    if (!storeId || isMockMode()) return;
    const controller = new AbortController();
    const effective = storeApi.getSellerVariantEffectiveMargins(storeId, controller.signal);
    const overrides = storeApi.getSellerVariantOverrides(storeId);
    void effective.catch(() => []).then((rows) => {
      if (controller.signal.aborted) return;
      const byProduct = new Map<string, Map<string, SellerVariantEffectiveMargin>>();
      for (const row of rows) {
        const variants = byProduct.get(row.productId) ?? new Map<string, SellerVariantEffectiveMargin>();
        variants.set(row.variantId, row);
        byProduct.set(row.productId, variants);
      }
      setVariantMargins({ storeId, byProduct });
    });
    void Promise.all([effective, overrides]).then(([rows, saved]) => {
      if (controller.signal.aborted) return;
      const ovMap = new Map(saved.filter(row => row.mode === 'percent').map(row => [row.variantId, row.value] as const));
      for (const row of rows) {
        if (row.tier !== 'variant' || row.locked || ovMap.get(row.variantId) === row.marginPct) continue;
        console.warn('Seller variant effective margin differs from saved override', {
          storeId, productId: row.productId, variantId: row.variantId,
          effective: row.marginPct, saved: ovMap.get(row.variantId),
        });
      }
    }).catch(() => {});
    return () => controller.abort();
  }, [storeId, marginRefresh, walkRetry]);

  const renderRow = (product: Product) => {
    const isPending = pendingProductIds.includes(product.id);
    const isSaved = savedProductIds.includes(product.id);
    const isNew = isPending && !isSaved;
    const isRemoved = !isPending && isSaved;
    const rowVariants: NonNullable<Product['variants']> = product.variants ?? [];
    const effective = effectiveByProduct?.get(product.id);
    const presence = costPresence[product.id];
    const isVariantProduct = product.productType === 'variable' || rowVariants.length > 0 || presence?.hasVariants
      || !!effective?.size || !!products.find((p) => p.id === product.id)?.variants?.length;
    const pricing = isVariantProduct ? undefined : resolvePricing(product, currentStore, platformSettings);
    let baseCostR = pricing?.baseCost ?? 0;
    let displayFinal = pricing?.finalPrice ?? 0;
    let displayProfit = pricing?.sellerMargin ?? 0;
    const fa = (n: number) => n.toLocaleString('fa-IR', { maximumFractionDigits: 2 });
    let displayPctText = pricing ? `${fa(pricing.marginPercent)}٪` : '';
    let fromPrefix = '';
    let unverified = false;
    let costMissing = catalogRowCostMissing(product, presence);

    // Variants never inherit a product override. An unset variant uses the
    // max allowed margin, with the per-product ceiling binding when set.
    if (isVariantProduct) {
      const effProductCost = presence?.product ? product.supplierBaseCost : null;
      const pctR = resolvePlatformPct(product, platformSettings);
      const rowsV = rowVariants.flatMap((v) => {
        const resolved = effective?.get(v.id);
        const costV = catalogVariantCost(v, effProductCost, presence?.variants[v.id] ?? false);
        const baseV = resolved?.referenceCost ?? (costV == null ? null : markedUpPrice(costV, pctR));
        if (baseV == null) return [];
        const pct = resolved?.marginPct ?? platformSettings.maxProfitMargin ?? 35;
        const finV = resolved?.finalPrice ?? markedUpPrice(baseV, pct);
        if (!resolved) unverified = true;
        return [{ pct, finV, profitV: finV - baseV, baseV }];
      });
      costMissing = rowsV.length === 0;
      if (rowsV.length > 0) {
        const pcts = rowsV.map((r) => r.pct);
        const fins = rowsV.map((r) => r.finV);
        const minPct = Math.min(...pcts);
        const maxPct = Math.max(...pcts);
        baseCostR = Math.min(...rowsV.map((r) => r.baseV));
        displayFinal = Math.min(...fins);
        displayProfit = Math.min(...rowsV.map((r) => r.profitV));
        if (Math.max(...fins) !== displayFinal || maxPct !== minPct) fromPrefix = 'از ';
        displayPctText = minPct === maxPct ? `${fa(minPct)}٪` : `${fa(minPct)}٪ تا ${fa(maxPct)}٪`;
      }
    }

    return (
      <div
        key={product.id}
        onClick={() => toggleProductSelect(product.id)}
        className={`flex cursor-pointer flex-wrap items-center gap-3 rounded-2xl border p-3 transition-colors sm:p-4 ${isPending ? 'border-brand-border' : 'border-slate-200'} ${isRemoved ? 'bg-amber-50/50' : 'bg-white hover:border-brand-border'}`}
      >
        <div className="flex min-w-0 grow basis-full items-start gap-3 sm:basis-64">
          <img
            src={product.image}
            alt=""
            loading="lazy"
            className="size-12 shrink-0 rounded-xl border border-slate-100 bg-slate-50 object-contain p-1 sm:size-14"
          />
          <div className="min-w-0">
            <h4 className="mb-1 break-words text-sm font-extrabold leading-6 text-[#261b41]">
              {product.title}
            </h4>
            {costMissing ? (
              <div className="text-xs leading-5 text-slate-600">
                <span>قیمت تأمین‌کننده در دسترس نیست</span>
              </div>
            ) : (
              <dl className="flex flex-wrap gap-x-3 gap-y-1 text-xs leading-5 text-slate-600">
                <div className="flex gap-1"><dt>خرید</dt><dd className="font-bold text-slate-800">{formatPrice(baseCostR)} تومان</dd></div>
                <div className="flex gap-1"><dt>فروش</dt><dd className="font-bold text-slate-800">{fromPrefix}{formatPrice(displayFinal)} تومان</dd></div>
                <div className="flex gap-1 text-emerald-800"><dt>سود تو</dt><dd className="font-bold">{formatPrice(displayProfit)} تومان ({displayPctText})</dd></div>
              </dl>
            )}
            {!costMissing && isVariantProduct && <p className="mt-0.5 text-xs leading-5 text-slate-600">پیش از تخفیف{unverified ? ' · تأیید نشده' : ''}</p>}
            {(() => {
              // P2(a)/P2(c) summary only (no controls here): the scoped
              // list omits hidden variants, the central list carries all
              // of them, so the difference IS the hidden count.
              if (isMockMode() || !scopedProducts) return null;
              const central = products.find((p) => p.id === product.id);
              const centralVariants: any[] = (central as any)?.variants ?? [];
              if (centralVariants.length === 0) return null;
              const scopedVariants: any[] = (product as any).variants ?? [];
              const hiddenCount = Math.max(0, centralVariants.length - scopedVariants.length);
              const defaultVariant = centralVariants.find((v) => v.isDefault);
              const storeDefaultId = (product as any).storeDefaultVariantId ?? null;
              const storeDefault = storeDefaultId ? centralVariants.find((v) => v.id === storeDefaultId) : null;
              const defaultDiffers = !!storeDefault && !!defaultVariant && storeDefaultId !== defaultVariant.id;
              if (hiddenCount <= 0 && !defaultDiffers) return null;
              return (
                <p className="pt-1 text-xs leading-5 text-slate-600">
                  {hiddenCount > 0 ? `${toPersianDigits(hiddenCount)} تنوع از ${toPersianDigits(centralVariants.length)} تنوع در فروشگاهت خاموش است` : ''}
                  {hiddenCount > 0 && defaultDiffers ? ' · ' : ''}
                  {defaultDiffers ? `پیش‌فرض فروشگاه: ${storeDefault.variantName || storeDefault.name}` : ''}
                </p>
              );
            })()}
            {(isNew || isRemoved) && (
              <span className="mt-1 inline-block rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs font-bold text-amber-800">
                {isNew ? 'در انتظار افزودن' : 'در انتظار حذف'}
              </span>
            )}
          </div>
        </div>

        <div className="flex w-full flex-wrap items-center justify-between gap-2 border-t border-slate-100 pt-2 sm:w-auto sm:justify-start sm:border-0 sm:pt-0">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setPricingProduct(product);
            }}
            className={secondaryAction}
            title="تنظیم سود این کالا"
            aria-label={`تنظیم سود ${product.title}`}
            aria-haspopup="dialog"
          >
            <SlidersHorizontal className="size-4" strokeWidth={1.5} aria-hidden="true" />
            <span>تنظیم سود</span>
          </button>

          <button
            type="button"
            role="switch"
            aria-checked={isPending}
            aria-label={`نمایش ${product.title} در فروشگاه`}
            title={isPending ? 'فعال در فروشگاه — برای غیرفعال کردن بزن' : 'غیرفعال — برای فعال کردن در فروشگاه بزن'}
            onClick={(e) => {
              // Without stopPropagation the click bubbles to the row's own
              // onClick and toggles twice — the switch looked dead.
              e.stopPropagation();
              toggleProductSelect(product.id);
            }}
            className="flex min-h-11 items-center gap-2 rounded-xl px-2 text-xs font-bold transition-colors hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand"
          >
            <span className={isPending ? 'text-brand-deep' : 'text-slate-600'}>
              {isPending ? 'فعال' : 'غیرفعال'}
            </span>
            <span
              dir="ltr"
              className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full px-0.5 transition-colors ${
                isPending ? 'bg-brand-deep' : 'bg-slate-400'
              }`}
            >
              <span
                className={`inline-block h-5 w-5 rounded-full bg-white shadow motion-safe:transition-transform ${
                  isPending ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </span>
          </button>
        </div>
      </div>
    );
  };

  const refreshVariantRows = () => setWalkRetry(value => value + 1);
  const storeForWork = storeSlug ? { id: storeId, slug: storeSlug, name: currentStore.name } : undefined;
  if (storeForWork && workFilter === 'attention') {
    return <div className="space-y-4">
      <SupplierSectionHeading title="تنوع‌های نیازمند رسیدگی" description="وضعیت فروش و نمایش هر تنوع را بررسی کن." icon={<Layers />} />
      <SellerVariantWorkspace key={storeId + '-attention'} store={storeForWork} attentionOnly
        onBack={() => setWorkFilter(undefined)} onChanged={refreshVariantRows} />
    </div>;
  }

  return (
    <div className={`space-y-4 ${hasChanges ? 'pb-36 [&_button]:scroll-mb-36 [&_input]:scroll-mb-36' : 'pb-6'}`}>
      <SupplierSectionHeading title="انتخاب کالا برای فروشگاهت" description="کالاهای ویترین، سود و نحوه نمایش تنوع‌ها را مدیریت کن." icon={<Package />}
        action={<span className="rounded-full border border-brand-border bg-brand-soft px-3 py-1 text-xs font-bold text-brand-deep">
          {toPersianDigits(savedProductIds.length)} کالا در فروشگاه
        </span>} />
      {storeForWork && <SellerVariantWorkspace key={storeId} store={storeForWork} onChanged={refreshVariantRows} />}

      {scopedError && (
        <div role="alert" className="space-y-2 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
          <p className="font-extrabold">{scopedError === 'inactive' ? 'فهرست کالاهای فروشگاه کامل بارگذاری نشد.' : 'دریافت قیمت‌های تأمین‌کننده ناموفق بود'}</p>
          <p>{scopedError === 'inactive'
            ? 'دسترسی به کالاهای غیرفعال برقرار نشد. اگر نشستت منقضی شده است، دوباره وارد شو.'
            : scopedProducts
              ? 'فهرست قبلی نمایش داده شده است؛ ممکن است تازه‌ترین تغییرات در آن نباشد.'
              : 'کالاها نمایش داده نمی‌شوند تا فهرست فروشگاه دوباره خوانده شود.'}</p>
          <button type="button" onClick={() => { setScopedError(null); setWalkRetry((n) => n + 1); }} className={secondaryAction}>تلاش مجدد</button>
        </div>
      )}

      {undoableRemoval && (
        <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-brand-border bg-brand-bg p-3 text-sm text-brand-deep">
          <span role="status" className="grow">{toPersianDigits(undoableRemoval.length)} کالا از فروشگاه حذف شد</span>
          <button type="button" onClick={handleUndo} disabled={productsBusy || nicheSaving} className={secondaryAction}>
            بازگردانی
          </button>
          <button type="button" aria-label="بستن پیام حذف کالا" onClick={() => setUndoableRemoval(null)} className="grid size-11 place-items-center rounded-xl hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">
            <X aria-hidden="true" size={18} />
          </button>
        </div>
      )}

      <section aria-labelledby="seller-niche-heading" className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h3 id="seller-niche-heading" className="text-sm font-extrabold text-[#261b41]">موضوع‌های فروشگاه</h3>
            <p className="mt-1 text-sm leading-6 text-slate-600">می‌توانی چند موضوع انتخاب کنی. تغییر موضوع‌ها همان لحظه ذخیره می‌شود.</p>
          </div>
          <button type="button" onClick={() => setShowNicheHelp(value => !value)} aria-expanded={showNicheHelp}
            aria-controls="seller-niche-help" className={secondaryAction}>
            «نیچ» یعنی چه؟
          </button>
        </div>
        <p id="seller-niche-help" hidden={!showNicheHelp} className="rounded-xl bg-brand-bg p-3 text-sm leading-6 text-slate-700">
          نیچ یعنی یک موضوع تخصصی مثل ساعت هوشمند یا پاوربانک. با انتخاب موضوع، کالاهای مرتبط را برای افزودن به فروشگاه می‌بینی.
        </p>

          <div className="flex flex-wrap gap-2" role="group" aria-labelledby="seller-niche-heading">
            {OFFICIAL_NICHES.map((niche) => {
              const isSelected = pendingNicheIds.includes(niche.id);
              return (
                <button
                  key={niche.id}
                  type="button"
                  onClick={() => toggleNiche(niche.id)}
                  disabled={nicheSaving || productsBusy}
                  aria-pressed={isSelected}
                  className={`flex min-h-11 items-center gap-1.5 rounded-xl border px-3 py-2 text-sm font-bold transition-colors focus-visible:ring-2 focus-visible:ring-brand disabled:cursor-wait disabled:opacity-60 ${
                    isSelected
                      ? 'border-brand bg-brand text-white'
                      : 'border-slate-200 bg-slate-50 text-slate-700 hover:bg-brand-bg'
                  }`}
                >
                  <span>{niche.title}</span>
                  {isSelected && <Check aria-hidden="true" className="size-4" strokeWidth={1.5} />}
                </button>
              );
            })}
          </div>
      </section>

      {scopedLoading ? (
        <p role="status" className="rounded-xl border border-[#e6e2f0] bg-white p-4 text-sm text-slate-700">در حال دریافت قیمت‌های تأمین‌کننده…</p>
      ) : (
        <>
      <section aria-labelledby="seller-niche-products" className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-2 px-0.5">
          <div>
            <h3 id="seller-niche-products" className="text-base font-extrabold text-[#261b41]">کالاهای نیچ‌های تو</h3>
            <p className="mt-1 text-sm leading-6 text-slate-600">بعد از تغییر وضعیت کالاها، «تأیید و ذخیره» را بزن تا تغییر در ویترین اعمال شود.</p>
          </div>
          <span className="text-sm font-bold text-slate-600">{toPersianDigits(nicheProducts.length)} کالا</span>
        </div>
        <label className={searchBox}>
          <Search aria-hidden="true" className="size-4 shrink-0 text-slate-600" strokeWidth={1.75} />
          <input type="search" aria-label="جست‌وجوی کالاهای موضوع‌های انتخاب‌شده" placeholder="جستجوی کالا در نیچ‌هایی که انتخاب کردی…"
            value={nicheSearch} onChange={event => setNicheSearch(event.target.value)} className={searchInput} />
        </label>

        {nicheProducts.length === 0 ? (
          <SupplierEmptyState icon={<Package />} title="کالایی پیدا نشد"
            description={nicheSearch ? 'عبارت دیگری جست‌وجو کن یا موضوع دیگری را هم انتخاب کن.' : 'از بخش موضوع‌های فروشگاه، موضوع دیگری را انتخاب کن.'}
            action={nicheSearch && <button type="button" onClick={() => setNicheSearch('')} className={secondaryAction}>پاک کردن جستجو</button>} />
        ) : (
          nicheProducts.map(renderRow)
        )}
      </section>

      <section className="rounded-[22px] border border-[#e6e2f0] bg-white shadow-sm">
        <button
          type="button"
          onClick={() => setShowExtraSection(value => !value)}
          aria-expanded={showExtraSection}
          aria-controls="seller-extra-catalog"
          className="flex w-full items-center justify-between gap-3 rounded-[22px] p-4 text-start transition-colors hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand sm:p-5"
        >
          <span className="flex min-w-0 items-center gap-3">
            <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep"><Package aria-hidden="true" size={18} strokeWidth={1.75} /></span>
            <span className="min-w-0">
              <span className="block text-sm font-extrabold leading-6 text-[#261b41]">
                بقیه کالاها (خارج از نیچ‌های تو)
              </span>
              <span className="block text-sm leading-6 text-slate-600">
                جست‌وجو و افزودن از کاتالوگ پلتفرم
              </span>
            </span>
          </span>
          <ChevronDown aria-hidden="true" className={`size-4 shrink-0 text-slate-600 motion-safe:transition-transform ${showExtraSection ? 'rotate-180' : ''}`} strokeWidth={1.75} />
        </button>

        {showExtraSection && (
          <div id="seller-extra-catalog" className="space-y-5 border-t border-slate-100 p-4 sm:p-5">
            {storeSlug && (
              <section aria-labelledby="seller-add-platform-product" className="space-y-3 rounded-2xl border border-slate-200 p-3 sm:p-4">
                <div>
                  <h4 id="seller-add-platform-product" className="text-sm font-extrabold text-[#261b41]">افزودن کالا از پلتفرم</h4>
                  <p className="mt-1 text-sm leading-6 text-slate-600">برای ثبت کالاهای افزوده‌شده، «تأیید و ذخیره» را بزن.</p>
                </div>
                <label className={searchBox}>
                  <Search aria-hidden="true" className="size-4 shrink-0 text-slate-600" strokeWidth={1.75} />
                  <input type="search" aria-label="جستجوی همه کالاهای پلتفرم" placeholder="جستجو در همه کالاهای پلتفرم…"
                    value={pickerQuery} onChange={event => setPickerQuery(event.target.value)} className={searchInput} />
                </label>
                {pickerRows.map((product) => (
                  <div key={product.id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-slate-50 p-3 text-sm">
                    <div className="min-w-0 flex-1 basis-36">
                      <p className="break-words font-bold text-slate-800">{product.title}</p>
                      {!product.isActive && (
                        <span className="mt-1 inline-block rounded-lg bg-amber-50 px-2 py-1 text-xs leading-5 text-amber-800">
                          غیرفعال — تا فعال نشود در فروشگاه نمایش داده نمی‌شود
                        </span>
                      )}
                    </div>
                    <button
                      type="button"
                      aria-label={`افزودن ${product.title} به فروشگاه`}
                      onClick={() => addPickerProduct(product)}
                      disabled={productsBusy}
                      className={secondaryAction}
                    >
                      افزودن
                    </button>
                  </div>
                ))}
                {pickerLoading && <p role="status" className="text-sm text-slate-600">در حال جستجو…</p>}
                {pickerError && (
                  <p role="alert" className="flex flex-wrap items-center gap-2 text-sm text-rose-800">
                    {pickerError}
                    <button
                      type="button"
                      onClick={() => void loadPickerPage(storeSlug, pickerQuery, pickerCursor ?? undefined)}
                      className={secondaryAction}
                    >
                      تلاش مجدد
                    </button>
                  </p>
                )}
                {!pickerLoading && !pickerError && pickerRows.length === 0 && (
                  <p role="status" className="text-sm text-slate-600">در همه کالاهای پلتفرم کالایی با این جستجو پیدا نشد</p>
                )}
                {pickerHasMore && (
                  <button
                    type="button"
                    disabled={pickerLoading || !!pickerError || !pickerCursor}
                    onClick={() => { if (pickerCursor) void loadPickerPage(storeSlug, pickerQuery, pickerCursor); }}
                    className={secondaryAction}
                  >
                    نمایش بیشتر
                  </button>
                )}
              </section>
            )}

            <section aria-labelledby="seller-outside-niche-products" className="space-y-3">
              <h4 id="seller-outside-niche-products" className="text-sm font-extrabold text-[#261b41]">کالاهای خارج از موضوع‌های انتخاب‌شده</h4>
              <label className={searchBox}>
                <Search aria-hidden="true" className="size-4 shrink-0 text-slate-600" strokeWidth={1.75} />
                <input type="search" aria-label="جست‌وجوی کالاهای خارج از موضوع‌های انتخاب‌شده" placeholder="جستجو در بقیه کالاها…"
                  value={extraSearch} onChange={event => setExtraSearch(event.target.value)} className={searchInput} />
              </label>
              {outsideNicheProducts.length === 0 ? (
                <p role="status" className="rounded-xl bg-slate-50 px-3 py-4 text-center text-sm text-slate-600">کالایی پیدا نشد</p>
              ) : (
                outsideNicheProducts.slice(0, 10).map(renderRow)
              )}
            </section>
          </div>
        )}
      </section>
        </>
      )}

      {/* Live's fixed bottom confirm bar, only while the draft differs. */}
      {hasChanges && (
        <div className="pointer-events-none fixed inset-x-0 bottom-0 z-30 px-3 pb-[max(.75rem,env(safe-area-inset-bottom))]">
          <div className="pointer-events-auto mx-auto max-w-[504px] space-y-2 rounded-2xl border border-amber-200 bg-white p-3 shadow-lg">
            <div role="status" className="flex flex-wrap items-center gap-x-1.5 text-amber-900">
              <span className="text-sm font-extrabold">
                {productDiffCount > 0 ? `${toPersianDigits(productDiffCount)} تغییر در انتظار ذخیره` : 'تغییر در انتظار ذخیره'}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={handleConfirm}
                disabled={productsBusy || nicheSaving}
                className="flex min-h-12 flex-1 items-center justify-center rounded-xl bg-brand px-3 text-sm font-extrabold text-white transition-colors hover:bg-brand-deep focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:cursor-wait disabled:opacity-60"
              >
                {saving ? 'در حال ذخیره…' : 'تأیید و ذخیره'}
              </button>
              <button
                type="button"
                onClick={handleCancel}
                disabled={productsBusy}
                className={secondaryAction + ' min-h-12'}
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
        effectiveMargins={pricingProduct ? effectiveByProduct?.get(pricingProduct.id) : undefined}
        hasVariants={!!(pricingProduct && costPresence[pricingProduct.id]?.hasVariants)}
        onClose={() => {
          setPricingProduct(null);
          setMarginRefresh((n) => n + 1);
        }}
      />
    </div>
  );
};
