import React, { useEffect, useMemo, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi, DiscountCampaign } from '../../services/storeApi';
import { Percent, Plus, RotateCcw, Trash2 } from 'lucide-react';

// Live parity (SellerDashboardView-MERGED.js, component Fn, ~bytes 288300-300300;
// API index-MERGED.js: GET/POST/PATCH/DELETE /stores/:slug/discount-campaigns).
// MANAGEMENT ONLY, and CREATE + toggle + delete only — live has NO edit flow:
// PATCH is used solely to flip isActive with the full campaign object.
// Scope precedence (live explainer): product beats niche beats whole store.
// Margin-only rule (live explainer, verbatim below): the discount comes
// only out of the seller's own profit — never the platform share, never the
// supplier cost — and a discount percent is shown to customers only once
// the product has 30 days of real price history below the campaign price.
// This tab never touches discount codes (separate tab) or advertising.
const toLocalInput = (d: Date) => {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const defaultRange = () => ({
  startsAt: toLocalInput(new Date()),
  endsAt: toLocalInput(new Date(Date.now() + 10080 * 60 * 1000)),
});

const campaignStatus = (c: DiscountCampaign) => {
  const now = Date.now();
  if (!c.isActive) return { label: 'غیرفعال', tone: 'bg-slate-100 text-slate-500' };
  if (new Date(c.startsAt).getTime() > now) return { label: 'شروع‌نشده', tone: 'bg-amber-50 text-amber-600' };
  if (new Date(c.endsAt).getTime() <= now) return { label: 'پایان‌یافته', tone: 'bg-slate-100 text-slate-500' };
  return { label: 'در حال اجرا', tone: 'bg-emerald-50 text-emerald-600' };
};

export const SellerDiscountCampaignsTab: React.FC = () => {
  const { currentStore, products, niches, formatPrice, toPersianDigits } = useStore();
  const slug = currentStore?.slug || '';
  const [campaigns, setCampaigns] = useState<DiscountCampaign[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [scope, setScope] = useState<'product' | 'variant' | 'niche' | 'store'>('product');
  const [targetId, setTargetId] = useState('');
  const [variantId, setVariantId] = useState('');
  const [mode, setMode] = useState<'percent' | 'fixed'>('percent');
  const [value, setValue] = useState('');
  const [range, setRange] = useState(defaultRange);

  const showcaseProducts = useMemo(() => {
    const ids = currentStore?.activeProductIds;
    return ids && ids.length > 0 ? products.filter((p) => ids.includes(p.id)) : products;
  }, [products, currentStore?.activeProductIds]);

  const reload = () => {
    if (!slug) { setLoading(false); setLoadFailed(true); return; }
    setLoading(true);
    setLoadFailed(false);
    storeApi.getDiscountCampaigns(slug)
      .then((list) => { setCampaigns(list); setLoading(false); })
      .catch(() => { setLoading(false); setLoadFailed(true); });
  };
  useEffect(reload, [slug]);

  useEffect(() => {
    if ((scope === 'product' || scope === 'variant') && showcaseProducts.length > 0 && !showcaseProducts.some((p) => p.id === targetId)) {
      setTargetId(showcaseProducts[0].id);
    }
    if (scope === 'niche' && niches.length > 0 && !niches.some((n) => n.id === targetId)) {
      setTargetId(niches[0].id);
    }
  }, [scope, showcaseProducts, niches]);
  const selectedProduct = useMemo(
    () => showcaseProducts.find((p) => p.id === targetId),
    [showcaseProducts, targetId],
  );

  useEffect(() => {
    if (scope !== 'variant') { setVariantId(''); return; }
    const variants = selectedProduct?.variants || [];
    if (variants.length > 0 && !variants.some((v) => v.id === variantId)) {
      setVariantId(variants[0].id);
    }
  }, [scope, selectedProduct, variantId]);

  const targetTitle = (c: DiscountCampaign) => {
    undefined
    if (c.scope === 'variant') {
      for (const p of products) {
        const v = (p.variants || []).find((x) => x.id === c.targetId);
        if (v) return p.title + ' — ' + v.name;
      }
      return 'واریانت';
    }
    if (c.scope === 'product') return products.find((p) => p.id === c.targetId)?.title || 'محصول';
    return niches.find((n) => n.id === c.targetId)?.title || 'نیچ';
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    const numericValue = Number(value);
    if (!name.trim()) { setFormError('نام کمپین را وارد کنید.'); return; }
    if (!Number.isFinite(numericValue) || numericValue < 0) { setFormError('مقدار حاشیهٔ هدف باید عددی مثبت باشد.'); return; }
    if (scope !== 'store' && scope !== 'variant' && !targetId) { setFormError('هدف کمپین را انتخاب کنید.'); return; }
    if (scope === 'variant' && !variantId) { setFormError('واریانت کمپین را انتخاب کنید.'); return; }
    const startsAt = new Date(range.startsAt);
    const endsAt = new Date(range.endsAt);
    if (Number.isNaN(startsAt.getTime()) || Number.isNaN(endsAt.getTime()) || endsAt <= startsAt) {
      setFormError('بازهٔ زمانی کمپین درست نیست؛ پایان باید بعد از شروع باشد.');
      return;
    }
    setSubmitting(true);
    try {
      const created = await storeApi.createDiscountCampaign(slug, {
        name: name.trim(),
        scope,
        targetId: scope === 'variant' ? variantId : (scope === 'store' ? '' : targetId),
        productId: scope === 'variant' ? targetId : undefined,
        mode,
        value: numericValue,
        startsAt: startsAt.toISOString(),
        endsAt: endsAt.toISOString(),
        isActive: true,
      });
      setCampaigns((prev) => [created, ...prev]);
      setName('');
      setValue('');
      setRange(defaultRange());
    } catch {
      setFormError('ساخت کمپین انجام نشد. اگر حاشیهٔ سود فروشگاه‌تان از طرف مدیریت قفل شده باشد، ساخت کمپین ممکن نیست.');
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
      setCampaigns((prev) => prev.map((item) => item.id === c.id ? updated : item));
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
        <button type="button" onClick={reload} className="inline-flex items-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2 text-xs font-bold text-white">
          <RotateCcw className="w-3.5 h-3.5" strokeWidth={1.5} />
          تلاش دوباره
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-3 pb-6">
      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-3">کمپین تخفیف چطور کار می‌کند؟</h3>
        <ul className="text-[11px] leading-6 text-slate-500 space-y-1">
          <li>• در بازهٔ کمپین، حاشیهٔ سود شما روی هدف انتخابی به مقداری که تعیین می‌کنید می‌رسد؛ تخفیف فقط از سود خودتان کم می‌شود و قیمت پایه و قیمت تأمین‌کننده تغییر نمی‌کند.</li>
          <li>• اگر چند کمپین هم‌زمان باشند، خاص‌ترین هدف برنده است: واریانت، بعد محصول، بعد نیچ، بعد کل فروشگاه. حاشیهٔ هدف را می‌توانید فقط روی یک واریانت مشخص هم بگذارید.</li>
          <li>• درصد تخفیف فقط وقتی به مشتری نمایش داده می‌شود که برای آن محصول در فروشگاه شما دست‌کم ۳۰ روز سابقهٔ قیمت واقعی ثبت شده باشد و قیمت کمپین از میانگین قیمت ۳۰ روز گذشته کمتر باشد. تا آن زمان قیمت واقعاً کم می‌شود، ولی درصدی نمایش داده نمی‌شود و محصول وارد ریل پیشنهادها نمی‌شود.</li>
        </ul>
      </section>

      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-3">کمپین تازه</h3>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="block text-[11px] font-bold text-slate-600 mb-1">نام کمپین</label>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="مثلاً: جشنوارهٔ آخر ماه" className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs" />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="block text-[11px] font-bold text-slate-600 mb-1">هدف کمپین</label>
              <select value={scope} onChange={(e) => setScope(e.target.value as 'product' | 'variant' | 'niche' | 'store')} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">
                <option value="product">یک محصول</option>
                <option value="variant">یک واریانت</option>
                <option value="niche">یک نیچ</option>
                <option value="store">کل فروشگاه</option>
              </select>
            </div>
            {(scope === 'product' || scope === 'variant') && (
              <div>
                <label className="block text-[11px] font-bold text-slate-600 mb-1">محصول</label>
                <select value={targetId} onChange={(e) => setTargetId(e.target.value)} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">
                  {showcaseProducts.map((p) => <option key={p.id} value={p.id}>{p.title}</option>)}
                </select>
              </div>
            )}
            {scope === 'variant' && (
              <div>
                <label className="block text-[11px] font-bold text-slate-600 mb-1">واریانت</label>
                <select value={variantId} onChange={(e) => setVariantId(e.target.value)} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">
                  {(selectedProduct?.variants || []).map((v) => <option key={v.id} value={v.id}>{v.name}</option>)}
                </select>
              </div>
            )}
            {scope === 'niche' && (
              <div>
                <label className="block text-[11px] font-bold text-slate-600 mb-1">نیچ</label>
                <select value={targetId} onChange={(e) => setTargetId(e.target.value)} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">
                  {niches.map((n) => <option key={n.id} value={n.id}>{n.title}</option>)}
                </select>
              </div>
            )}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="block text-[11px] font-bold text-slate-600 mb-1">نوع حاشیهٔ هدف</label>
              <select value={mode} onChange={(e) => setMode(e.target.value as 'percent' | 'fixed')} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">
                <option value="percent">درصدی از قیمت پایه</option>
                <option value="fixed">مبلغ ثابت (تومان)</option>
              </select>
            </div>
            <div>
              <label className="block text-[11px] font-bold text-slate-600 mb-1">{mode === 'percent' ? 'حاشیهٔ هدف (درصد)' : 'حاشیهٔ هدف (تومان)'}</label>
              <input value={value} onChange={(e) => setValue(e.target.value)} inputMode="decimal" placeholder={mode === 'percent' ? 'مثلاً: ۵' : 'مثلاً: ۵۰۰۰۰'} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs" />
            </div>
          </div>
          <p className="text-[11px] leading-5 text-slate-500">حاشیهٔ هدف یعنی در مدت کمپین، سود شما روی هر فروش دقیقاً همین مقدار می‌شود؛ می‌تواند تا صفر هم باشد.</p>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="block text-[11px] font-bold text-slate-600 mb-1">شروع</label>
              <input type="datetime-local" value={range.startsAt} onChange={(e) => setRange((r) => ({ ...r, startsAt: e.target.value }))} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs" dir="ltr" />
            </div>
            <div>
              <label className="block text-[11px] font-bold text-slate-600 mb-1">پایان</label>
              <input type="datetime-local" value={range.endsAt} onChange={(e) => setRange((r) => ({ ...r, endsAt: e.target.value }))} className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs" dir="ltr" />
            </div>
          </div>
          {formError && <p className="text-[11px] font-bold text-rose-500">{formError}</p>}
          <button type="submit" disabled={submitting} className="w-full inline-flex items-center justify-center gap-1.5 rounded-xl bg-brand-deep py-2.5 text-xs font-bold text-white disabled:opacity-50">
            <Plus className="w-4 h-4" strokeWidth={1.5} />
            {submitting ? 'در حال ساخت…' : 'ساخت کمپین'}
          </button>
        </form>
      </section>

      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-3">کمپین‌های من</h3>
        {campaigns.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">هنوز کمپینی نساخته‌اید.</p>
        ) : (
          <ul className="divide-y divide-slate-50">
            {campaigns.map((c) => {
              const status = campaignStatus(c);
              return (
                <li key={c.id} className="py-3 flex items-start gap-3">
                  <span className="w-8 h-8 rounded-xl bg-brand-soft border border-brand-border flex items-center justify-center shrink-0">
                    <Percent className="w-4 h-4 text-brand-deep" strokeWidth={1.5} />
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-[11px] font-bold text-slate-800 truncate">{c.name}</p>
                    <p className="text-[10px] text-slate-400 mt-0.5">{targetTitle(c)} — حاشیهٔ هدف: {c.mode === 'percent' ? `${toPersianDigits(c.value)}٪ از قیمت پایه` : `${formatPrice(c.value)} تومان`}</p>
                    <p className="text-[10px] text-slate-400 mt-0.5">از {new Date(c.startsAt).toLocaleString('fa-IR')} تا {new Date(c.endsAt).toLocaleString('fa-IR')}</p>
                  </div>
                  <span className={`text-[9px] font-bold px-2 py-1 rounded-full shrink-0 ${status.tone}`}>{status.label}</span>
                  <button type="button" onClick={() => void handleToggle(c)} className="text-[10px] font-bold text-slate-500 hover:text-slate-800 shrink-0">{c.isActive ? 'غیرفعال‌کردن' : 'فعال‌کردن'}</button>
                  <button type="button" onClick={() => void handleDelete(c)} className="text-rose-400 hover:text-rose-600 shrink-0" aria-label="حذف کمپین">
                    <Trash2 className="w-4 h-4" strokeWidth={1.5} />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
};
