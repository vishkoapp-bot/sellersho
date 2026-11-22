import React, { useEffect, useMemo, useState } from 'react';
import { Copy, Search, Check } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { buildAdBannerLink } from '../../utils/attributionTokens';
import { CATEGORY_TO_OFFICIAL_NICHES_MAP, getProductNicheIds, OFFICIAL_NICHES } from '../../data/niches';
import { cardPriceVariantOf } from '../../utils/cardPrice';

const sizes = [
  { id: 'square', title: 'مربعی', width: 300, height: 300 },
  { id: 'landscape', title: 'افقی', width: 600, height: 314 },
  { id: 'story', title: 'عمودی', width: 300, height: 600 },
  { id: 'leaderboard', title: 'عریض', width: 728, height: 90 },
];
const escapeHtml = (value: string) => value.replace(/[&<>\"']/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]!));

// Live parity (SellerDashboardView-MERGED.js, ad-library section In, offsets
// ~206517-214200): live titles/describes the library in the informal
// second person, and — unlike the previous candidate build — the copied
// banner link is NOT origin-relative with a hand-built ad_token. Live
// builds it as buildAdBannerLink(store URL, slug, `ad_library_<size>`)
// plus &productId=<id>, plus &campaignId=<id> when an ACTIVE banner
// campaign exists (fetched on mount). Copy below is live's, verbatim.
export const SellerAdLibrary: React.FC = () => {
  const { currentStore, products, niches, formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const taxonomy = niches.length ? niches : OFFICIAL_NICHES;
  const ownNiches = useMemo(() => {
    if (currentStore.selectedNicheIds?.length) return currentStore.selectedNicheIds;
    const categories = currentStore.activeCategories || [];
    const mapped = categories.flatMap((category) => CATEGORY_TO_OFFICIAL_NICHES_MAP[category] || []);
    if (mapped.length) return [...new Set(mapped)];
    return [...new Set(products.filter((product) => currentStore.activeProductIds?.includes(product.id)).flatMap(getProductNicheIds))];
  }, [currentStore, products]);
  const [nicheFilter, setNicheFilter] = useState('all');
  const [sizeFilter, setSizeFilter] = useState('all');
  const [productFilter, setProductFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [copiedId, setCopiedId] = useState('');
  const [activeBannerCampaignId, setActiveBannerCampaignId] = useState<string | null>(null);
  useEffect(() => {
    if (!currentStore.slug) return;
    storeApi.getSellerCampaigns(currentStore.slug).then((list) => {
      const active = (list || []).find((c) => c.type === 'banner' && c.status === 'active');
      setActiveBannerCampaignId(active ? active.id : null);
    }).catch(() => setActiveBannerCampaignId(null));
  }, [currentStore.slug]);
  const eligibleProducts = products.filter((product) => product.stock > 0);
  const sortedProducts = [...eligibleProducts].sort((a, b) => Number(getProductNicheIds(b).some((id) => ownNiches.includes(id))) - Number(getProductNicheIds(a).some((id) => ownNiches.includes(id))) || a.title.localeCompare(b.title, 'fa'));
  const filteredProducts = sortedProducts.filter((product) =>
    (nicheFilter === 'all' || (nicheFilter === 'mine' ? getProductNicheIds(product).some((id) => ownNiches.includes(id)) : getProductNicheIds(product).includes(nicheFilter))) &&
    (productFilter === 'all' || product.id === productFilter) &&
    product.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const selectedSizes = sizes.filter((size) => sizeFilter === 'all' || size.id === sizeFilter);
  const [limit, setLimit] = useState(12);
  const banners = filteredProducts.flatMap((product) => selectedSizes.map((size) => ({ product, size, id: `${product.id}-${size.id}` })));

  const copy = async (product: (typeof products)[number], size: (typeof sizes)[number], id: string) => {
    const storeUrl = `https://sellershow.ir/store/${currentStore.slug}`;
    let target = buildAdBannerLink(storeUrl, currentStore.slug, `ad_library_${size.id}`);
    target += `&productId=${encodeURIComponent(product.id)}`;
    if (activeBannerCampaignId) target += `&campaignId=${encodeURIComponent(activeBannerCampaignId)}`;
    const image = new URL(product.image, window.location.origin);
    if (!['http:', 'https:'].includes(image.protocol)) { error('نشانی تصویر کالا معتبر نیست.'); return; }
    // B7: the embed price is the card price (default variant) — label
    // the variant it came from, escaped like every other embed value.
    const priced = cardPriceVariantOf(product);
    const code = `<a href="${escapeHtml(target)}" target="_blank" rel="noopener noreferrer" dir="rtl" aria-label="${escapeHtml(product.title)}" style="display:flex;flex-direction:${size.id === 'leaderboard' ? 'row' : 'column'};align-items:center;justify-content:center;gap:12px;width:${size.width}px;max-width:100%;height:${size.height}px;box-sizing:border-box;padding:12px;background:#f8fafc;border:1px solid #cbd5e1;border-radius:16px;text-decoration:none;color:#0f172a;font-family:Tahoma,sans-serif;overflow:hidden"><img src="${escapeHtml(image.href)}" alt="${escapeHtml(product.title)}" style="width:${size.id === 'leaderboard' ? '64px' : '60%'};height:${size.id === 'leaderboard' ? '64px' : '55%'};object-fit:contain"/><span style="font-weight:bold;font-size:14px">${escapeHtml(product.title)}</span>${priced.variant?.label ? `<span style="color:#475569;font-size:12px">${escapeHtml(priced.variant.label)}</span>` : ''}<span style="color:#047857;font-size:13px">${escapeHtml(formatPrice(priced.triple.price))} تومان</span><span style="background:#2563eb;color:white;padding:8px 12px;border-radius:8px;font-size:12px">مشاهده کالا</span></a>`;
    try { await navigator.clipboard.writeText(code); setCopiedId(id); success('کد بنر همین کالا و اندازه کپی شد.'); } catch { error('کپی کد بنر انجام نشد؛ دسترسی مرورگر به حافظه موقت (کلیپ‌بورد) را بررسی کنید.'); }
  };

  return <section className="space-y-3 rounded-2xl border border-slate-200 bg-white p-3.5">
    <h3 className="text-sm font-bold text-slate-900">بنرهای آماده برای کالاها</h3>
    <p className="text-xs leading-6 text-slate-500">بنر کالاهای موضوع‌های فروشگاهت اول فهرست می‌آید. برای هر کالا چهار اندازه بنر آماده است؛ کدش را کپی کن و در سایت یا وبلاگت بگذار. هر کس روی بنر بزند، وارد فروشگاه تو می‌شود و خریدش برای تو حساب می‌شود.</p>
    <p className="text-[11px] text-emerald-700">موضوع‌های فروشگاهت: {ownNiches.map((id) => taxonomy.find((niche) => niche.id === id)?.title).filter(Boolean).join('، ') || 'هنوز موضوعی انتخاب نشده؛ از بخش «انتخاب کالا» انتخاب کن.'}</p>
    <div className="grid gap-2 sm:grid-cols-2">
      <label className="text-xs">نیچ بنر<select aria-label="نیچ بنر" value={nicheFilter} onChange={(event) => { setNicheFilter(event.target.value); setLimit(12); }} className="mt-1 w-full rounded-xl border bg-slate-50 p-2"><option value="all">همه نیچ‌ها؛ نیچ من اول</option><option value="mine">فقط نیچ‌های فروشگاه من</option>{taxonomy.map((niche) => <option key={niche.id} value={niche.id}>{ownNiches.includes(niche.id) ? '★ ' : ''}{niche.title}</option>)}</select></label>
      <label className="text-xs">اندازه بنر<select aria-label="اندازه بنر" value={sizeFilter} onChange={(event) => { setSizeFilter(event.target.value); setLimit(12); }} className="mt-1 w-full rounded-xl border bg-slate-50 p-2"><option value="all">تمام اندازه‌ها</option>{sizes.map((size) => <option key={size.id} value={size.id}>{size.title} · {size.width}×{size.height}</option>)}</select></label>
      <label className="text-xs">کالای بنر<select aria-label="کالای بنر" value={productFilter} onChange={(event) => { setProductFilter(event.target.value); setLimit(12); }} className="mt-1 w-full rounded-xl border bg-slate-50 p-2"><option value="all">تمام کالاهای موجود</option>{sortedProducts.map((product) => <option key={product.id} value={product.id}>{product.title}</option>)}</select></label>
      <label className="text-xs">جست‌وجوی کالا<div className="mt-1 flex items-center gap-2 rounded-xl border bg-slate-50 px-2"><Search className="h-4 w-4 text-slate-400" strokeWidth={1.5} /><input aria-label="جست‌وجوی کالای بنر" value={search} onChange={(event) => { setSearch(event.target.value); setLimit(12); }} className="min-w-0 w-full bg-transparent p-2" /></div></label>
    </div>
    <div className="flex justify-between text-xs text-slate-500"><span>{toPersianDigits(banners.length)} بنر</span><button type="button" onClick={() => { setNicheFilter('all'); setSizeFilter('all'); setProductFilter('all'); setSearch(''); setLimit(12); }} className="text-brand-deep">پاک‌کردن فیلترها</button></div>
    {banners.length === 0 && <p className="rounded-xl bg-slate-50 p-6 text-center text-xs">بنری با این فیلترها پیدا نشد.</p>}
    <div className="grid gap-3 sm:grid-cols-2">{banners.slice(0, limit).map(({ product, size, id }) => { const priced = cardPriceVariantOf(product); return (<article key={id} className="space-y-2 rounded-xl border border-slate-200 p-3">
      <div className="flex flex-wrap justify-between gap-1 text-[10px]"><span>{size.title} · {size.width}×{size.height}</span>{getProductNicheIds(product).some((niche) => ownNiches.includes(niche)) && <span className="rounded-full bg-emerald-50 px-2 text-emerald-700">مناسب نیچ شما</span>}</div>
      <div className="flex justify-center rounded-xl bg-slate-100 p-2"><div style={{ aspectRatio: `${size.width}/${size.height}`, width: size.id === 'story' ? '50%' : '100%' }} className={`flex overflow-hidden items-center justify-center gap-1 rounded-lg bg-white p-2 ${size.id === 'leaderboard' ? 'flex-row' : 'flex-col'}`}><img loading="lazy" src={product.image} alt={product.title} className={`${size.id === 'leaderboard' ? 'h-full w-12' : 'h-1/2 w-2/3'} object-contain`} /><span className="line-clamp-2 text-center text-[10px] font-bold">{product.title}</span><span className="text-[9px] text-emerald-700">{priced.variant?.label ? `${priced.variant.label} · ` : ''}{formatPrice(priced.triple.price)} تومان</span></div></div>
      <button type="button" onClick={() => copy(product, size, id)} className="flex w-full items-center justify-center gap-2 rounded-xl bg-brand-deep p-2 text-xs font-bold text-white">{copiedId === id ? <Check className="h-4 w-4" strokeWidth={1.5} /> : <Copy className="h-4 w-4" strokeWidth={1.5} />} کپی کد بنر</button>
    </article>); })}</div>
    {limit < banners.length && <button type="button" onClick={() => setLimit(limit + 12)} className="w-full rounded-xl bg-slate-100 p-2 text-xs font-bold">نمایش بنرهای بیشتر</button>}
  </section>;
};
