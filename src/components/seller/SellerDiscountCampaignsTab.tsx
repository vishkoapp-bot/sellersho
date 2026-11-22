import React, { useEffect, useMemo, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi, DiscountCampaign } from '../../services/storeApi';
import {
  Percent,
  Plus,
  RotateCcw,
  Trash2,
  Tag,
  Calendar,
  Layers,
  ChevronDown,
  ChevronUp,
  Search,
  CheckCircle2,
  Sparkles,
  AlertCircle,
  ToggleLeft,
  ToggleRight,
} from 'lucide-react';
import { Product, ProductVariant } from '../../types';
import { cardPriceRangeOf, cardPriceVariantOf } from '../../utils/cardPrice';

const toLocalInput = (d: Date) => {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

const defaultRange = () => ({
  startsAt: toLocalInput(new Date()),
  endsAt: toLocalInput(new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)), // 7 days later
});

const campaignStatus = (c: DiscountCampaign) => {
  const now = Date.now();
  if (!c.isActive) return { label: 'غیرفعال', tone: 'bg-slate-100 text-slate-500' };
  if (new Date(c.startsAt).getTime() > now) return { label: 'شروع‌نشده', tone: 'bg-amber-50 text-amber-600' };
  if (new Date(c.endsAt).getTime() <= now) return { label: 'پایان‌یافته', tone: 'bg-slate-100 text-slate-500' };
  return { label: 'در حال اجرا', tone: 'bg-emerald-50 text-emerald-600' };
};

interface ProductDiscountConfig {
  productId: string;
  // If product has no variants, this value is used
  discountPercent: number;
  // If product has variants, discounts per variant id
  variantDiscounts: Record<string, number>;
}

export const SellerDiscountCampaignsTab: React.FC = () => {
  const { currentStore, products, niches, formatPrice, toPersianDigits } = useStore();
  const slug = currentStore?.slug || '';
  const [campaigns, setCampaigns] = useState<DiscountCampaign[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSuccess, setFormSuccess] = useState<string | null>(null);

  // Form Fields
  const [name, setName] = useState('');
  const [campaignTargetMode, setCampaignTargetMode] = useState<'custom_products' | 'niche' | 'store'>('custom_products');
  const [selectedNicheId, setSelectedNicheId] = useState('');
  const [nicheDiscountValue, setNicheDiscountValue] = useState('10');
  const [storeDiscountValue, setStoreDiscountValue] = useState('10');
  const [range, setRange] = useState(defaultRange);

  // Multi-Product & Variant Selection
  const [productSearch, setProductSearch] = useState('');
  const [selectedProductIds, setSelectedProductIds] = useState<string[]>([]);
  const [productConfigs, setProductConfigs] = useState<Record<string, ProductDiscountConfig>>({});
  const [expandedProductIds, setExpandedProductIds] = useState<Record<string, boolean>>({});

  // Products available in current store
  const showcaseProducts = useMemo(() => {
    const ids = currentStore?.activeProductIds;
    return ids && ids.length > 0 ? products.filter((p) => ids.includes(p.id)) : products;
  }, [products, currentStore?.activeProductIds]);

  const filteredProducts = useMemo(() => {
    if (!productSearch.trim()) return showcaseProducts;
    const q = productSearch.toLowerCase();
    return showcaseProducts.filter((p) => p.title.toLowerCase().includes(q) || p.category.toLowerCase().includes(q));
  }, [showcaseProducts, productSearch]);

  const reload = () => {
    if (!slug) { setLoading(false); setLoadFailed(true); return; }
    setLoading(true);
    setLoadFailed(false);
    storeApi.getDiscountCampaigns(slug)
      .then((list) => { setCampaigns(list); setLoading(false); })
      .catch(() => { setLoading(false); setLoadFailed(true); });
  };

  useEffect(reload, [slug]);

  // Handle toggling product selection for festival
  const toggleProductSelection = (product: Product) => {
    const isSelected = selectedProductIds.includes(product.id);
    if (isSelected) {
      setSelectedProductIds((prev) => prev.filter((id) => id !== product.id));
    } else {
      setSelectedProductIds((prev) => [...prev, product.id]);
      if (!productConfigs[product.id]) {
        const variants = product.variants || [];
        const variantDiscounts: Record<string, number> = {};
        variants.forEach((v) => { variantDiscounts[v.id] = 10; });
        setProductConfigs((prev) => ({
          ...prev,
          [product.id]: {
            productId: product.id,
            discountPercent: 10,
            variantDiscounts,
          },
        }));
      }
      // Expand by default if it has variants
      if (product.variants && product.variants.length > 0) {
        setExpandedProductIds((prev) => ({ ...prev, [product.id]: true }));
      }
    }
  };

  // Set single product general discount percent
  const setProductBaseDiscount = (productId: string, val: number) => {
    setProductConfigs((prev) => {
      const existing = prev[productId] || { productId, discountPercent: 10, variantDiscounts: {} };
      return {
        ...prev,
        [productId]: { ...existing, discountPercent: Math.max(0, Math.min(99, val)) },
      };
    });
  };

  // Set specific variant discount
  const setVariantDiscount = (productId: string, variantId: string, val: number) => {
    setProductConfigs((prev) => {
      const existing = prev[productId] || { productId, discountPercent: 10, variantDiscounts: {} };
      return {
        ...prev,
        [productId]: {
          ...existing,
          variantDiscounts: {
            ...existing.variantDiscounts,
            [variantId]: Math.max(0, Math.min(99, val)),
          },
        },
      };
    });
  };

  // Apply uniform discount to all variants of a product
  const applyUniformDiscountToVariants = (product: Product, val: number) => {
    const clamped = Math.max(0, Math.min(99, val));
    const variantDiscounts: Record<string, number> = {};
    (product.variants || []).forEach((v) => { variantDiscounts[v.id] = clamped; });
    setProductConfigs((prev) => ({
      ...prev,
      [product.id]: {
        productId: product.id,
        discountPercent: clamped,
        variantDiscounts,
      },
    }));
  };

  const toggleExpand = (productId: string) => {
    setExpandedProductIds((prev) => ({ ...prev, [productId]: !prev[productId] }));
  };

  // Handle festival creation
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setFormSuccess(null);

    if (!name.trim()) {
      setFormError('لطفاً عنوان جشنواره یا نام کمپین را وارد کنید.');
      return;
    }

    const startsAt = new Date(range.startsAt);
    const endsAt = new Date(range.endsAt);
    if (Number.isNaN(startsAt.getTime()) || Number.isNaN(endsAt.getTime()) || endsAt <= startsAt) {
      setFormError('بازهٔ زمانی جشنواره معتبر نیست؛ زمان پایان باید بعد از شروع باشد.');
      return;
    }

    setSubmitting(true);

    try {
      if (campaignTargetMode === 'store') {
        const val = Number(storeDiscountValue);
        if (!Number.isFinite(val) || val <= 0) throw new Error('درصد تخفیف کل فروشگاه باید عددی مثبت باشد.');
        const created = await storeApi.createDiscountCampaign(slug, {
          name: name.trim(),
          scope: 'store',
          targetId: '',
          mode: 'percent',
          value: val,
          startsAt: startsAt.toISOString(),
          endsAt: endsAt.toISOString(),
          isActive: true,
        });
        setCampaigns((prev) => [created, ...prev]);
      } else if (campaignTargetMode === 'niche') {
        const val = Number(nicheDiscountValue);
        if (!selectedNicheId) throw new Error('لطفاً نیچ مورد نظر را انتخاب کنید.');
        if (!Number.isFinite(val) || val <= 0) throw new Error('درصد تخفیف نیچ باید عددی مثبت باشد.');
        const created = await storeApi.createDiscountCampaign(slug, {
          name: name.trim(),
          scope: 'niche',
          targetId: selectedNicheId,
          mode: 'percent',
          value: val,
          startsAt: startsAt.toISOString(),
          endsAt: endsAt.toISOString(),
          isActive: true,
        });
        setCampaigns((prev) => [created, ...prev]);
      } else {
        // Custom Selected Products & Variants
        if (selectedProductIds.length === 0) {
          throw new Error('حداقل یک محصول را برای این جشنواره انتخاب کنید.');
        }

        const promises: Promise<DiscountCampaign>[] = [];

        for (const prodId of selectedProductIds) {
          const product = showcaseProducts.find((p) => p.id === prodId);
          if (!product) continue;
          const config = productConfigs[prodId] || { productId: prodId, discountPercent: 10, variantDiscounts: {} };
          const variants = product.variants || [];

          if (variants.length > 0) {
            // Create a campaign per variant
            for (const v of variants) {
              const variantVal = config.variantDiscounts[v.id] ?? config.discountPercent ?? 10;
              if (variantVal > 0) {
                promises.push(
                  storeApi.createDiscountCampaign(slug, {
                    name: `${name.trim()} (${product.title} - ${v.name})`,
                    scope: 'variant',
                    targetId: v.id,
                    productId: product.id,
                    mode: 'percent',
                    value: variantVal,
                    startsAt: startsAt.toISOString(),
                    endsAt: endsAt.toISOString(),
                    isActive: true,
                  })
                );
              }
            }
          } else {
            // Create a campaign for the product
            const prodVal = config.discountPercent || 10;
            if (prodVal > 0) {
              promises.push(
                storeApi.createDiscountCampaign(slug, {
                  name: `${name.trim()} (${product.title})`,
                  scope: 'product',
                  targetId: product.id,
                  mode: 'percent',
                  value: prodVal,
                  startsAt: startsAt.toISOString(),
                  endsAt: endsAt.toISOString(),
                  isActive: true,
                })
              );
            }
          }
        }

        const createdList = await Promise.all(promises);
        setCampaigns((prev) => [...createdList, ...prev]);
      }

      setFormSuccess(`جشنواره «${name.trim()}» با موفقیت ثبت و تخفیف‌ها فعال شدند.`);
      setName('');
      setSelectedProductIds([]);
      setRange(defaultRange());
    } catch (err: any) {
      setFormError(err.message || 'ساخت کمپین انجام نشد. اگر حاشیه سود قفل باشد امکان ساخت کمپین نیست.');
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggle = async (c: DiscountCampaign) => {
    setFormError(null);
    try {
      const updated = await storeApi.updateDiscountCampaign(slug, c.id, {
        name: c.name,
        scope: c.scope,
        targetId: c.targetId,
        mode: c.mode,
        value: c.value,
        startsAt: c.startsAt,
        endsAt: c.endsAt,
        isActive: !c.isActive,
      });
      setCampaigns((prev) => prev.map((item) => (item.id === c.id ? updated : item)));
    } catch {
      setFormError('تغییر وضعیت کمپین انجام نشد.');
    }
  };

  const handleDelete = async (c: DiscountCampaign) => {
    setFormError(null);
    try {
      await storeApi.deleteDiscountCampaign(slug, c.id);
      setCampaigns((prev) => prev.filter((item) => item.id !== c.id));
    } catch {
      setFormError('حذف کمپین انجام نشد.');
    }
  };

  const targetTitle = (c: DiscountCampaign) => {
    if (c.scope === 'variant') {
      for (const p of products) {
        const v = (p.variants || []).find((x) => x.id === c.targetId);
        if (v) return `${p.title} (${v.name})`;
      }
      return 'واریانت اختصاصی';
    }
    if (c.scope === 'product') {
      return products.find((p) => p.id === c.targetId)?.title || 'محصول اختصاصی';
    }
    if (c.scope === 'niche') {
      return niches.find((n) => n.id === c.targetId)?.title || 'نیچ تخصصی';
    }
    return 'کل فروشگاه';
  };

  if (loading) {
    return (
      <div className="space-y-3 pb-6 animate-pulse" aria-label="در حال بارگذاری کمپین‌ها">
        <div className="bg-white rounded-2xl border border-slate-100 h-40" />
        <div className="bg-white rounded-2xl border border-slate-100 h-24" />
      </div>
    );
  }

  if (loadFailed) {
    return (
      <div className="bg-white rounded-2xl border border-slate-100 p-8 text-center">
        <p className="text-xs font-bold text-slate-700 mb-1">بارگذاری کمپین‌ها انجام نشد</p>
        <p className="text-[11px] text-slate-400 mb-4">اتصال اینترنت را بررسی کنید و دوباره تلاش کنید.</p>
        <button
          type="button"
          onClick={reload}
          className="inline-flex items-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2 text-xs font-bold text-white shadow"
        >
          <RotateCcw className="w-3.5 h-3.5" strokeWidth={1.5} />
          تلاش دوباره
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-4 pb-8">
      {/* Informative Banner */}
      <section className="bg-gradient-to-l from-indigo-50 to-blue-50/60 rounded-2xl border border-indigo-100 p-4">
        <div className="flex items-start gap-3">
          <div className="p-2 rounded-xl bg-indigo-600 text-white shadow-xs shrink-0">
            <Sparkles className="w-4 h-4" />
          </div>
          <div className="space-y-1">
            <h3 className="text-xs font-black text-indigo-950">مدیریت جشنواره و تخفیف‌های هدفمند</h3>
            <p className="text-[11px] leading-5 text-indigo-900/80">
              محصولات یا واریانت‌های مدنظر خود (رنگ، سایز، مدل) را انتخاب کرده و درصد تخفیف دلخواه را اعمال کنید. 
              این تخفیف‌ها به همراه نام جشنواره مستقیماً در ویترین فروشگاه و صفحه محصول به مشتریان نمایش داده می‌شوند.
            </p>
          </div>
        </div>
      </section>

      {/* Main Campaign Builder Form */}
      <section className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4 sm:p-5">
        <div className="flex items-center gap-2 mb-4 border-b border-slate-100 pb-3">
          <Tag className="w-4 h-4 text-brand-deep" />
          <h3 className="text-xs font-black text-slate-800">ایجاد جشنواره یا کمپین تخفیف جدید</h3>
        </div>

        {formError && (
          <div className="mb-4 rounded-xl bg-rose-50 border border-rose-200 p-3 flex items-start gap-2 text-rose-800 text-xs">
            <AlertCircle className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
            <p className="leading-5">{formError}</p>
          </div>
        )}

        {formSuccess && (
          <div className="mb-4 rounded-xl bg-emerald-50 border border-emerald-200 p-3 flex items-start gap-2 text-emerald-800 text-xs">
            <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0 mt-0.5" />
            <p className="leading-5">{formSuccess}</p>
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Campaign Name */}
          <div>
            <label className="block text-[11px] font-bold text-slate-700 mb-1.5">
              نام جشنواره / کمپین <span className="text-rose-500">*</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="مثلاً: جشنواره شگفت‌انگیز پاییزه، فروش ویژه آخر هفته..."
              className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-xs text-slate-800 focus:bg-white focus:border-brand-deep focus:outline-none transition"
            />
          </div>

          {/* Time Window */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] font-bold text-slate-700 mb-1.5 flex items-center gap-1">
                <Calendar className="w-3.5 h-3.5 text-slate-400" />
                <span>تاریخ و ساعت شروع</span>
              </label>
              <input
                type="datetime-local"
                value={range.startsAt}
                onChange={(e) => setRange((prev) => ({ ...prev, startsAt: e.target.value }))}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-800 focus:bg-white focus:outline-none"
              />
            </div>
            <div>
              <label className="block text-[11px] font-bold text-slate-700 mb-1.5 flex items-center gap-1">
                <Calendar className="w-3.5 h-3.5 text-slate-400" />
                <span>تاریخ و ساعت پایان</span>
              </label>
              <input
                type="datetime-local"
                value={range.endsAt}
                onChange={(e) => setRange((prev) => ({ ...prev, endsAt: e.target.value }))}
                className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-xs text-slate-800 focus:bg-white focus:outline-none"
              />
            </div>
          </div>

          {/* Scope Selection */}
          <div>
            <label className="block text-[11px] font-bold text-slate-700 mb-2">نوع تخفیف و هدف‌گذاری</label>
            <div className="grid grid-cols-3 gap-2">
              <button
                type="button"
                onClick={() => setCampaignTargetMode('custom_products')}
                className={`py-2 px-3 rounded-xl border text-xs font-bold transition flex items-center justify-center gap-1.5 ${
                  campaignTargetMode === 'custom_products'
                    ? 'border-indigo-600 bg-indigo-50/70 text-indigo-900 shadow-2xs'
                    : 'border-slate-200 bg-slate-50 text-slate-600 hover:bg-slate-100'
                }`}
              >
                <Layers className="w-3.5 h-3.5" />
                <span>انتخاب محصولات و واریانت‌ها</span>
              </button>
              <button
                type="button"
                onClick={() => setCampaignTargetMode('niche')}
                className={`py-2 px-3 rounded-xl border text-xs font-bold transition flex items-center justify-center gap-1.5 ${
                  campaignTargetMode === 'niche'
                    ? 'border-indigo-600 bg-indigo-50/70 text-indigo-900 shadow-2xs'
                    : 'border-slate-200 bg-slate-50 text-slate-600 hover:bg-slate-100'
                }`}
              >
                <span>یک دسته‌بندی (نیچ)</span>
              </button>
              <button
                type="button"
                onClick={() => setCampaignTargetMode('store')}
                className={`py-2 px-3 rounded-xl border text-xs font-bold transition flex items-center justify-center gap-1.5 ${
                  campaignTargetMode === 'store'
                    ? 'border-indigo-600 bg-indigo-50/70 text-indigo-900 shadow-2xs'
                    : 'border-slate-200 bg-slate-50 text-slate-600 hover:bg-slate-100'
                }`}
              >
                <span>تخفیف عمومی کل فروشگاه</span>
              </button>
            </div>
          </div>

          {/* Whole Store Scope Inputs */}
          {campaignTargetMode === 'store' && (
            <div className="p-3 bg-slate-50 rounded-xl border border-slate-200 space-y-2">
              <label className="block text-[11px] font-bold text-slate-700">درصد تخفیف کل محصولات فروشگاه (%)</label>
              <div className="flex items-center gap-2 max-w-xs">
                <input
                  type="number"
                  min="1"
                  max="90"
                  value={storeDiscountValue}
                  onChange={(e) => setStoreDiscountValue(e.target.value)}
                  className="w-24 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs text-center font-bold"
                />
                <Percent className="w-4 h-4 text-slate-400" />
                <span className="text-[11px] text-slate-500">از حاشیه سود شما کسر می‌شود.</span>
              </div>
            </div>
          )}

          {/* Niche Scope Inputs */}
          {campaignTargetMode === 'niche' && (
            <div className="p-3 bg-slate-50 rounded-xl border border-slate-200 space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className="block text-[11px] font-bold text-slate-700 mb-1">انتخاب نیچ</label>
                  <select
                    value={selectedNicheId}
                    onChange={(e) => setSelectedNicheId(e.target.value)}
                    className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs"
                  >
                    <option value="">انتخاب دسته‌بندی...</option>
                    {niches.map((n) => (
                      <option key={n.id} value={n.id}>{n.title}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-[11px] font-bold text-slate-700 mb-1">درصد تخفیف این نیچ (%)</label>
                  <div className="flex items-center gap-2">
                    <input
                      type="number"
                      min="1"
                      max="90"
                      value={nicheDiscountValue}
                      onChange={(e) => setNicheDiscountValue(e.target.value)}
                      className="w-24 rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs text-center font-bold"
                    />
                    <Percent className="w-4 h-4 text-slate-400" />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* CUSTOM PRODUCTS & VARIANTS SELECTION */}
          {campaignTargetMode === 'custom_products' && (
            <div className="space-y-3 border border-slate-200 rounded-2xl p-3 bg-slate-50/50">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 border-b border-slate-200/80 pb-2.5">
                <div>
                  <h4 className="text-xs font-black text-slate-800">انتخاب کالاها و تنوع‌ها (Variants)</h4>
                  <p className="text-[10px] text-slate-500">
                    کالاهایی که تیک می‌خورند وارد جشنواره می‌شوند. برای کالاهای دارای تنوع، می‌توانید روی هر رنگ/سایز تخفیف جدا بگذارید.
                  </p>
                </div>
                <div className="relative min-w-[200px]">
                  <Search className="w-3.5 h-3.5 text-slate-400 absolute right-2.5 top-2.5" />
                  <input
                    type="text"
                    value={productSearch}
                    onChange={(e) => setProductSearch(e.target.value)}
                    placeholder="جستجوی کالا..."
                    className="w-full rounded-xl border border-slate-200 bg-white pr-8 pl-3 py-1.5 text-[11px] focus:outline-none focus:border-brand-deep"
                  />
                </div>
              </div>

              {/* Product List */}
              <div className="space-y-2 max-h-[420px] overflow-y-auto pr-1">
                {filteredProducts.map((product) => {
                  const isSelected = selectedProductIds.includes(product.id);
                  const variants = product.variants || [];
                  const hasVariants = variants.length > 0;
                  // B7: price shown is the card price (default variant) —
                  // name that variant; the range line covers the rest.
                  const priced = cardPriceVariantOf(product);
                  const priceRange = cardPriceRangeOf(product);
                  const isExpanded = !!expandedProductIds[product.id];
                  const config = productConfigs[product.id] || {
                    productId: product.id,
                    discountPercent: 10,
                    variantDiscounts: {},
                  };

                  return (
                    <div
                      key={product.id}
                      className={`rounded-xl border transition ${
                        isSelected
                          ? 'border-indigo-300 bg-white shadow-2xs'
                          : 'border-slate-200/80 bg-white opacity-85 hover:opacity-100'
                      }`}
                    >
                      {/* Product Header Row */}
                      <div className="p-3 flex items-center justify-between gap-3">
                        <div className="flex items-center gap-3 min-w-0">
                          <input
                            type="checkbox"
                            checked={isSelected}
                            onChange={() => toggleProductSelection(product)}
                            className="h-4 w-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500 cursor-pointer"
                          />
                          <img
                            src={product.image}
                            alt={product.title}
                            className="w-10 h-10 object-cover rounded-lg border border-slate-100 shrink-0"
                          />
                          <div className="min-w-0">
                            <h5 className="text-xs font-bold text-slate-900 truncate">{product.title}</h5>
                            <div className="flex items-center gap-2 mt-0.5 text-[10px] text-slate-500">
                              <span>قیمت: {priced.variant?.label ? `${priced.variant.label} · ` : ''}{formatPrice(priced.triple.price)} تومان</span>
                              {priceRange.hasRange && (
                                <span className="text-slate-400">بازه قیمت: {formatPrice(priceRange.min)} تا {formatPrice(priceRange.max)} تومان</span>
                              )}
                              {hasVariants && (
                                <span className="bg-indigo-50 text-indigo-700 px-1.5 py-0.2 rounded font-semibold">
                                  {toPersianDigits(variants.length)} واریانت
                                </span>
                              )}
                            </div>
                          </div>
                        </div>

                        {/* Controls if selected */}
                        {isSelected && (
                          <div className="flex items-center gap-2 shrink-0">
                            {!hasVariants ? (
                              <div className="flex items-center gap-1.5 bg-slate-50 px-2 py-1 rounded-lg border border-slate-200">
                                <span className="text-[10px] text-slate-600 font-bold">تخفیف:</span>
                                <input
                                  type="number"
                                  min="1"
                                  max="90"
                                  value={config.discountPercent}
                                  onChange={(e) => setProductBaseDiscount(product.id, Number(e.target.value))}
                                  className="w-12 text-center text-xs font-extrabold text-indigo-700 bg-white rounded border border-slate-200 py-0.5"
                                />
                                <span className="text-[10px] text-slate-400">٪</span>
                              </div>
                            ) : (
                              <button
                                type="button"
                                onClick={() => toggleExpand(product.id)}
                                className="inline-flex items-center gap-1 text-[11px] text-indigo-600 hover:text-indigo-800 font-bold px-2 py-1 rounded-lg bg-indigo-50/80 border border-indigo-100"
                              >
                                <span>تنظیم تخفیف واریانت‌ها</span>
                                {isExpanded ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                              </button>
                            )}
                          </div>
                        )}
                      </div>

                      {/* Variants Accordion */}
                      {isSelected && hasVariants && isExpanded && (
                        <div className="border-t border-slate-100 bg-slate-50/70 p-3 rounded-b-xl space-y-2">
                          <div className="flex items-center justify-between pb-2 border-b border-slate-200/60 text-[10px] text-slate-600">
                            <span className="font-bold">واریانت‌های کالا (رنگ / سایز / ویژگی):</span>
                            <div className="flex items-center gap-1.5">
                              <span>اعمال سریع درصد یکسان:</span>
                              <input
                                type="number"
                                min="1"
                                max="90"
                                defaultValue={10}
                                id={`quick-${product.id}`}
                                className="w-12 text-center text-xs font-bold bg-white rounded border border-slate-300 py-0.5"
                              />
                              <button
                                type="button"
                                onClick={() => {
                                  const input = document.getElementById(`quick-${product.id}`) as HTMLInputElement;
                                  if (input) applyUniformDiscountToVariants(product, Number(input.value));
                                }}
                                className="px-2 py-0.5 bg-indigo-600 text-white rounded text-[10px] font-bold hover:bg-indigo-700"
                              >
                                اعمال
                              </button>
                            </div>
                          </div>

                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                            {variants.map((v) => {
                              const val = config.variantDiscounts[v.id] ?? config.discountPercent ?? 10;
                              return (
                                <div
                                  key={v.id}
                                  className="flex items-center justify-between p-2 rounded-lg bg-white border border-slate-200 text-xs"
                                >
                                  <div className="min-w-0 pr-1">
                                    <span className="font-bold text-slate-800 block truncate">{v.name}</span>
                                    <span className="text-[10px] text-slate-400">موجودی: {toPersianDigits(v.stock ?? 0)}</span>
                                  </div>
                                  <div className="flex items-center gap-1 shrink-0">
                                    <input
                                      type="number"
                                      min="1"
                                      max="90"
                                      value={val}
                                      onChange={(e) => setVariantDiscount(product.id, v.id, Number(e.target.value))}
                                      className="w-12 text-center text-xs font-extrabold text-indigo-700 bg-slate-50 border border-slate-200 rounded py-1"
                                    />
                                    <span className="text-[10px] text-slate-500 font-bold">٪</span>
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Submit Button */}
          <button
            type="submit"
            disabled={submitting}
            className="w-full inline-flex items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 py-3 px-4 font-bold text-xs text-white shadow-md hover:from-indigo-700 hover:to-purple-700 transition disabled:opacity-50"
          >
            <Plus className="w-4 h-4" />
            <span>{submitting ? 'در حال ثبت جشنواره...' : 'ایجاد و فعال‌سازی جشنواره'}</span>
          </button>
        </form>
      </section>

      {/* Existing Active Campaigns List */}
      <section className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4 sm:p-5">
        <div className="flex items-center justify-between mb-4 border-b border-slate-100 pb-3">
          <div className="flex items-center gap-2">
            <Percent className="w-4 h-4 text-emerald-600" />
            <h3 className="text-xs font-black text-slate-800">لیست جشنواره‌ها و تخفیف‌های ثبت‌شده</h3>
          </div>
          <span className="text-[10px] font-bold text-slate-500 bg-slate-100 px-2 py-0.5 rounded-full">
            {toPersianDigits(campaigns.length)} مورد
          </span>
        </div>

        {campaigns.length === 0 ? (
          <div className="p-8 text-center text-slate-400 text-xs">
            هنوز جشنواره یا تخفیفی برای این فروشگاه تعریف نشده است.
          </div>
        ) : (
          <div className="space-y-2.5">
            {campaigns.map((c) => {
              const status = campaignStatus(c);
              return (
                <div
                  key={c.id}
                  className="p-3 rounded-xl border border-slate-100 bg-slate-50/50 hover:bg-white hover:border-slate-200 transition flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs"
                >
                  <div className="space-y-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="font-extrabold text-slate-900">{c.name}</span>
                      <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full ${status.tone}`}>
                        {status.label}
                      </span>
                      <span className="text-[10px] bg-indigo-50 text-indigo-700 px-2 py-0.5 rounded-md font-bold">
                        {toPersianDigits(c.value)}٪ تخفیف
                      </span>
                    </div>
                    <div className="text-[11px] text-slate-500 flex items-center gap-2">
                      <span>هدف: {targetTitle(c)}</span>
                      <span>•</span>
                      <span>تا {new Date(c.endsAt).toLocaleDateString('fa-IR')}</span>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0 self-end sm:self-center">
                    <button
                      type="button"
                      onClick={() => handleToggle(c)}
                      className="p-1.5 rounded-lg text-slate-500 hover:bg-slate-200 transition"
                      title={c.isActive ? 'غیرفعال‌سازی' : 'فعال‌سازی'}
                    >
                      {c.isActive ? (
                        <ToggleRight className="w-5 h-5 text-emerald-600" />
                      ) : (
                        <ToggleLeft className="w-5 h-5 text-slate-400" />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleDelete(c)}
                      className="p-1.5 rounded-lg text-rose-500 hover:bg-rose-50 transition"
                      title="حذف کمپین"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>
    </div>
  );
};
