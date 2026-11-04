import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { AdCampaignStatsModal } from '../common/AdCampaignStatsModal';
import { SellerCentralDeals } from './SellerCentralDeals';
import { SellerAdLibrary } from './SellerAdLibrary';
import { SellerCampaign, storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import {
  Megaphone,
  MousePointerClick,
  ShieldCheck,
  Check,
  Copy,
  Clock,
  Flame,
  TrendingUp,
  Sparkles,
  Code2,
  Info,
} from 'lucide-react';

export const SellerAdsWidgetTab: React.FC = () => {
  const {
    products,
    orders,
    currentStore,
    platformSettings,
    updateStoreBuilderConfig,
    updateSellerDealsWidgetConfig,
    updateCentralDealsFilter,
    preMadeWidgets,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error } = useToast();
  const [productSearch, setProductSearch] = useState('');
  const [selectedOnly, setSelectedOnly] = useState(false);
  const [saving, setSaving] = useState(false);

  const [showStatsModal, setShowStatsModal] = useState(false);
  const [analytics, setAnalytics] = useState({ impressions: 0, clicks: 0, ctr: 0 });
  const [hasAnalytics, setHasAnalytics] = useState(false);
  const [campaigns, setCampaigns] = useState<SellerCampaign[]>([]);
  const [campaignError, setCampaignError] = useState('');
  const [campaignLoading, setCampaignLoading] = useState(false);
  const [newCampaignName, setNewCampaignName] = useState('');
  const [newCampaignType, setNewCampaignType] = useState<'banner'|'link'>('banner');
  const [creatingCampaign, setCreatingCampaign] = useState(false);
  useEffect(() => {
    if (isMockMode() || !currentStore.slug) return;
    setCampaignLoading(true);
    storeApi.getSellerCampaigns(currentStore.slug).then(async (loaded) => {
      setCampaigns(loaded);
      const widget = loaded.find((campaign) => campaign.type === 'widget');
      if (!widget) return;
      const result = await storeApi.getSellerCampaignAnalytics(currentStore.slug, widget.id);
      setAnalytics(result); setHasAnalytics(true);
    }).catch(() => { setHasAnalytics(false); setCampaignError('گرفتن برنامه‌های تبلیغاتی انجام نشد.'); }).finally(() => setCampaignLoading(false));
  }, [currentStore.slug]);

  const widgets = currentStore.builderConfig?.widgets || {
    heroBanner: true,
    dealsSlider: true,
    categoriesGrid: true,
    storeDealsWidget: true,
    trustBar: true,
    contactInfo: true,
  };

  const isCentralDealsActive = widgets.dealsSlider;
  const isSellerDealsActive = widgets.storeDealsWidget;

  // Section 4.1 & 4.5: Ad Revenue model and weighted average
  const sellerClickRate = platformSettings.adRevenue?.sellerClickShare || 3000;
  const validClicksCount = analytics.clicks;
  const totalAdRevenue = 0;
  const averageClickShare = sellerClickRate;

  // Section 4.3.b: Seller Deals Config
  const sellerDealsConfig = currentStore.sellerDealsWidgetConfig || {
    title: 'پیشنهاد شگفت‌انگیز ویژه فروشگاه ما',
    productIds: [],
    countdownHours: 24,
    endsAt: '۲۳:۵۹:۵۹',
  };

  const [customDealsHours, setCustomDealsHours] = useState(sellerDealsConfig.countdownHours.toString());
  const [selectedDealsProducts, setSelectedDealsProducts] = useState<string[]>(sellerDealsConfig.productIds);
  const [dealTitle, setDealTitle] = useState(sellerDealsConfig.title);
  const selectableProducts = products.filter((product) => !currentStore.activeProductIds || currentStore.activeProductIds.includes(product.id));
  const visibleProducts = selectableProducts.filter((product) => product.title.toLowerCase().includes(productSearch.trim().toLowerCase()) && (!selectedOnly || selectedDealsProducts.includes(product.id)));

  // Section 4.4: Central Deals 3-way filter
  const centralFilter = currentStore.centralDealsFilter || 'my_niches';

  // Section 4.3.c: Best Sellers derived dynamically from real platform orders
  const bestSellerProductIds = Array.from(
    new Set(orders.flatMap((o) => o.items.map((i) => i.product.id)))
  );
  const bestSellersCount = bestSellerProductIds.length;

  const toggleWidget = (key: 'dealsSlider' | 'storeDealsWidget') => {
    updateStoreBuilderConfig({
      selectedTheme: currentStore.builderConfig?.selectedTheme || 'general',
      accentColor: currentStore.builderConfig?.accentColor || '#007dfa',
      widgets: {
        ...widgets,
        [key]: !widgets[key],
      },
    }, true);
  };

  const handleSaveSellerDeals = async () => {
    if (!dealTitle.trim() || !Number.isInteger(Number(customDealsHours)) || Number(customDealsHours) < 1 || Number(customDealsHours) > 168) { error('عنوان و زمان معتبر بین ۱ تا ۱۶۸ ساعت وارد کنید.'); return; }
    if (selectedDealsProducts.length === 0) { error('حداقل یک کالا انتخاب کنید.'); return; }
    const config = {
      title: dealTitle.trim(),
      productIds: selectedDealsProducts,
      countdownHours: Number(customDealsHours) || 24,
      endsAt: new Date(Date.now() + Number(customDealsHours) * 3600000).toISOString(),
    };
    if (saving) return;
    setSaving(true);
    try {
      const saved = isMockMode() ? config : await storeApi.saveAdWidget(currentStore.slug, config);
      updateSellerDealsWidgetConfig(saved);
      setCampaigns(await storeApi.getSellerCampaigns(currentStore.slug));
      success('تنظیمات ویجت شگفت‌انگیز اختصاصی فروشگاه ذخیره شد.');
    } catch (err: unknown) { error((err as any)?.status === 400 ? 'برخی از کالاهای انتخاب‌شده در کاتالوگ فروشگاه نیستند؛ اول از تب «انتخاب کالا» آن‌ها را به فروشگاه اضافه کنید.' : err instanceof Error ? err.message : 'ذخیره ویجت انجام نشد.'); }
    finally { setSaving(false); }
  };

  const createCampaign = async () => {
    if (!newCampaignName.trim() || creatingCampaign) { error('اول اسم برنامه تبلیغاتی را بنویس.'); return; }
    setCreatingCampaign(true);
    try {
      const campaign = await fetch('/api/v1/stores/' + encodeURIComponent(currentStore.slug) + '/campaigns', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('seler_sho_jwt_token') || ''}` }, body: JSON.stringify({ name: newCampaignName.trim(), type: newCampaignType, productIds: selectedDealsProducts, status: 'active' }) }).then(async (response) => { if (!response.ok) throw new Error('ثبت کمپین انجام نشد.'); return response.json(); });
      setCampaigns((items) => [campaign, ...items]); setNewCampaignName(''); success('برنامه تبلیغاتی ساخته و فعال شد.');
    } catch (cause: any) { error(cause.message || 'ساختن برنامه انجام نشد. دوباره تلاش کن.'); } finally { setCreatingCampaign(false); }
  };

  const handleCopyHtmlSnippet = (code: string) => {
    // Item 1: Ad tokens (CPC) strictly separated from store referral cookies (CPS)
    const personalized = code
      .replace(/ref=\{\{STORE_SLUG\}\}/g, `ad_token=${currentStore.slug}_banner`)
      .replace(/\{\{STORE_SLUG\}\}/g, `${currentStore.slug}_banner`);
    if (navigator.clipboard) {
      navigator.clipboard.writeText(personalized);
      success('کد HTML بنر با توکن رهگیری تبلیغاتی (ad_token) در حافظه کپی شد.');
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="rounded-2xl border border-brand-border bg-brand-soft/50 px-3.5 py-2.5"><div className="flex items-start gap-2"><Info className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} /><p className="flex-1 text-xs leading-6 text-slate-700">این‌جا برای فروشگاهت «پیشنهاد ویژه» می‌سازی، بنر تبلیغاتی آماده می‌گیری و می‌بینی چند نفر روی تبلیغ‌هایت زده‌اند. تازه‌کاری؟ فقط بخش اول کافی است؛ بقیه را بعداً هم می‌توانی یاد بگیری.</p></div></div>
      <p className="rounded-xl bg-amber-50 p-3 text-xs leading-6 text-amber-900">{hasAnalytics ? 'این آمار از کلیک‌های واقعی روی تبلیغ‌های فروشگاهت حساب شده است.' : 'هنوز کسی روی تبلیغ‌هایت کلیک نکرده است.'}</p>
      {/* 1. Header Card */}
      <div className="bg-gradient-to-l from-brand-deep via-brand-deep to-brand rounded-2xl p-4 text-white shadow-xs space-y-2">
        <div className="flex items-center gap-2">
          <Megaphone className="w-5 h-5 text-indigo-200"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold">تبلیغات فروشگاهت</h3>
        </div>
        <p className="text-[10px] text-indigo-100 leading-relaxed">
          پیشنهاد ویژه بساز، بنر آماده بگیر و ببین تبلیغ‌هایت چقدر دیده و زده شده‌اند.
        </p>
      </div>

      {/* 2. Section 4.5: Ad Performance Metrics with Average Click Share */}
      <div className="grid grid-cols-3 gap-2">
        <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between text-slate-500 mb-1">
            <span className="text-[10px] font-bold text-slate-700">تعداد کلیک‌ها</span>
            <MousePointerClick className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-xs font-extrabold text-slate-900">
            {toPersianDigits(validClicksCount)}
          </div>
          <span className="text-[9px] text-slate-400 block mt-1">کلیک واقعی روی تبلیغ‌ها</span>
        </div>

        <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between text-slate-500 mb-1">
            <span className="text-[10px] font-bold text-slate-700">هزینه هر کلیک</span>
            <Sparkles className="w-3.5 h-3.5 text-amber-500"  strokeWidth={1.5}/>
          </div>
          <div className="text-xs font-extrabold text-blue-700">
            هنوز تعیین نشده
          </div>
          <span className="text-[9px] text-brand-deep font-bold block mt-1">برای تبلیغ‌های فروش، قیمتی تعیین نشده است</span>
        </div>

        <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between text-slate-500 mb-1">
            <span className="text-[10px] font-bold text-slate-700">درآمد از تبلیغات</span>
            <ShieldCheck className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-xs font-extrabold text-emerald-600">
            +{formatPrice(totalAdRevenue)}
          </div>
            <span className="text-[9px] text-emerald-600 font-bold block mt-1">هنوز درآمدی ثبت نشده است</span>
        </div>
      </div>

      {/* 3. Section 4.3: Three Distinct Widgets */}
      <section className="space-y-3 rounded-2xl border border-blue-200 bg-white p-4">
        <h3 className="text-sm font-bold">شگفت‌انگیز اختصاصی فروشگاه من</h3>
        <p className="rounded-xl bg-emerald-50 border border-emerald-200 px-3 py-2 text-[11px] text-emerald-800 leading-relaxed">این ویجت پس از ذخیره، در ویترین فروشگاه شما نمایش داده می‌شود (قالب‌های مدرن، الکترونیک و دیجی‌استایل).</p>
        <label className="flex items-center gap-2 text-xs font-bold"><input aria-label="نمایش شگفت‌انگیز اختصاصی در پیش‌نمایش پنل" type="checkbox" checked={isSellerDealsActive} onChange={() => toggleWidget('storeDealsWidget')} />نمایش در پیش‌نمایش همین بخش</label>
        <p className="text-xs leading-6 text-slate-500">تصویر، نام و قیمت کالاها را ببینید و انتخاب کنید. فقط کالاهای فعال کاتالوگ فروشگاه در این ویجت قرار می‌گیرند.</p>
        <label className="block text-xs">عنوان ویجت<input aria-label="عنوان ویجت" value={dealTitle} onChange={(event) => setDealTitle(event.target.value)} className="mt-1 w-full rounded-xl border bg-slate-50 p-2" /></label>
        <label className="block text-xs">مدت پیشنهاد (ساعت)<input aria-label="مدت پیشنهاد (ساعت)" type="number" min="1" max="168" value={customDealsHours} onChange={(event) => setCustomDealsHours(event.target.value)} className="mt-1 w-full rounded-xl border bg-slate-50 p-2" /></label>
        <p className="text-[11px] leading-6 text-slate-500">زمان پیشنهاد از لحظه ذخیره شروع می‌شود. ذخیره دوباره زمان را از نو آغاز می‌کند. قیمت کالا همان قیمت فعلی فروشگاه است؛ انتخاب کالا به‌تنهایی تخفیف جدید ایجاد نمی‌کند.</p>
        <input aria-label="جست‌وجوی کالاهای ویجت" placeholder="جستجوی نام کالا…" value={productSearch} onChange={(event) => setProductSearch(event.target.value)} className="w-full rounded-xl border bg-slate-50 p-2 text-xs" />
        <label className="flex gap-2 text-xs"><input type="checkbox" checked={selectedOnly} onChange={(event) => setSelectedOnly(event.target.checked)} />فقط انتخاب‌شده‌ها · {toPersianDigits(selectedDealsProducts.length)} کالا</label>
        <div className="grid max-h-96 grid-cols-2 gap-2 overflow-y-auto">{visibleProducts.map((product) => <label key={product.id} className={`cursor-pointer space-y-2 rounded-xl border p-2 ${selectedDealsProducts.includes(product.id) ? 'border-blue-600 bg-blue-50' : 'border-slate-200'}`}>
          <img loading="lazy" src={product.image} alt={product.title} className="h-24 w-full object-contain" />
          <span className="block text-[11px] font-bold">{product.title}</span><span className="block text-[10px] text-emerald-700">{formatPrice(product.price)} تومان</span>
          <span className="flex items-center gap-2 text-xs"><input aria-label={`انتخاب ${product.title}`} type="checkbox" checked={selectedDealsProducts.includes(product.id)} onChange={(event) => setSelectedDealsProducts((ids) => event.target.checked ? [...ids, product.id] : ids.filter((id) => id !== product.id))} />انتخاب کالا</span>
        </label>)}</div>
        {visibleProducts.length === 0 && <p className="p-4 text-xs text-slate-500">کالایی پیدا نشد. برای افزودن کالا به فروشگاه از تب انتخاب کالا استفاده کنید.</p>}
        {currentStore.sellerDealsWidgetConfig && <div className="rounded-xl bg-emerald-50 p-3 text-xs leading-6 text-emerald-900"><strong>تنظیمات ذخیره‌شده: {currentStore.sellerDealsWidgetConfig.title}</strong><p>{toPersianDigits(currentStore.sellerDealsWidgetConfig.productIds.length)} کالا · پایان: {Number.isNaN(Date.parse(currentStore.sellerDealsWidgetConfig.endsAt)) ? 'ذخیره مجدد زمان لازم است' : new Date(currentStore.sellerDealsWidgetConfig.endsAt).toLocaleString('fa-IR')}</p></div>}
        <button type="button" disabled={saving} onClick={handleSaveSellerDeals} className="w-full rounded-xl bg-brand-deep p-2 text-xs font-bold text-white disabled:opacity-50">{saving ? 'در حال ذخیره...' : 'ذخیره کالاهای ویجت'}</button>
      </section>
      <SellerAdLibrary />
      <section className="space-y-2 rounded-2xl border border-slate-200 bg-white p-3.5 shadow-2xs">
        <div className="flex items-center justify-between"><h3 className="text-xs font-bold text-slate-800">برنامه‌های تبلیغاتی تو</h3><span className="text-[10px] text-slate-400">{campaignLoading ? 'در حال دریافت...' : `${toPersianDigits(campaigns.length)} برنامه`}</span></div>
        {campaignError && <p className="text-xs text-rose-600">{campaignError}</p>}
        {!campaignLoading && campaigns.length === 0 && <p className="rounded-xl bg-slate-50 p-3 text-xs text-slate-500 leading-6">هنوز برنامه تبلیغاتی نساخته‌ای. برنامه (کمپین) یعنی یک تبلیغ زمان‌دار؛ مثلاً بنری که مدتی نشان داده می‌شود. از کادر پایین اسمش را بنویس و «ساختن» را بزن.</p>}
        {campaigns.map((campaign) => <div key={campaign.id} className="flex items-center justify-between gap-2 rounded-xl border border-slate-100 bg-slate-50 p-2.5 text-xs"><div><strong>{campaign.name}</strong><span className="mr-2 text-[10px] text-slate-500">{{banner: 'بنر', link: 'لینک', widget: 'پیشنهاد ویژه'}[campaign.type] || campaign.type} · {campaign.status === 'active' ? 'فعال' : campaign.status === 'paused' ? 'متوقف' : campaign.status}</span></div><div className="flex gap-1">{campaign.status === 'active' && <button type="button" className="rounded-lg bg-amber-100 px-2 py-1 text-[10px] text-amber-800" onClick={() => storeApi.setSellerCampaignStatus(currentStore.slug, campaign.id, 'paused').then(() => setCampaigns((items) => items.map((item) => item.id === campaign.id ? { ...item, status: 'paused' } : item)))}>متوقف کن</button>}{campaign.status === 'paused' && <button type="button" className="rounded-lg bg-emerald-100 px-2 py-1 text-[10px] text-emerald-800" onClick={() => storeApi.setSellerCampaignStatus(currentStore.slug, campaign.id, 'active').then(() => setCampaigns((items) => items.map((item) => item.id === campaign.id ? { ...item, status: 'active' } : item)))}>دوباره فعال کن</button>}</div></div>)}
        <div className="grid gap-2 border-t border-slate-100 pt-2 sm:grid-cols-[1fr_auto_auto]"><input value={newCampaignName} onChange={(event) => setNewCampaignName(event.target.value)} placeholder="اسم برنامه، مثلاً: بنر اینستاگرام" className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs" /><select value={newCampaignType} onChange={(event) => setNewCampaignType(event.target.value as 'banner'|'link')} className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs"><option value="banner">بنر</option><option value="link">لینک</option></select><button type="button" disabled={creatingCampaign} onClick={createCampaign} className="rounded-xl bg-brand-deep px-3 py-2 text-xs font-bold text-white disabled:opacity-50">ساختن</button></div>
      </section>
      <div className="space-y-3">
        {/* Widget A: شگفت‌انگیز سراسری (Central Amazing Deals) */}
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
          <div className="flex items-center justify-between border-b border-slate-100 pb-2">
            <div className="flex items-center gap-2">
              <Flame className="w-4 h-4 text-rose-500"  strokeWidth={1.5}/>
              <div>
                <span className="text-xs font-bold text-slate-800 block">پیشنهادهای ویژه همه فروشگاه‌ها</span>
                <span className="text-[10px] text-slate-400">تخفیف‌هایی که پلتفرم برای همه فروشگاه‌ها گذاشته است</span>
              </div>
            </div>
            <input
              type="checkbox"
              checked={isCentralDealsActive}
              onChange={() => toggleWidget('dealsSlider')}
              className="w-4 h-4 accent-blue-600 cursor-pointer"
            />
          </div>

          {isCentralDealsActive && <SellerCentralDeals key={currentStore.id} />}
        </div>

        {/* Widget C: پرفروش‌ترین‌ها (Best Sellers) */}
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs flex items-center justify-between">
          <div className="flex items-center gap-2">
            <TrendingUp className="w-4 h-4 text-emerald-600"  strokeWidth={1.5}/>
            <div>
              <span className="text-xs font-bold text-slate-800 block">پرفروش‌ترین کالاهای همه فروشگاه‌ها</span>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                خودکار از روی {toPersianDigits(bestSellersCount)} کالایی ساخته می‌شود که در همه فروشگاه‌ها بیشتر از همه سفارش گرفته‌اند
              </span>
            </div>
          </div>
          <span className="text-[9px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full border border-emerald-200">
            خودکار
          </span>
        </div>
      </div>

      <button
        type="button"
        onClick={() => setShowStatsModal(true)}
        className="w-full py-2 bg-purple-50 hover:bg-purple-100 text-purple-700 font-bold text-xs rounded-xl border border-purple-200 flex items-center justify-center gap-1.5 transition-colors"
      >
        <span>دیدن گزارش کامل کلیک‌ها</span>
      </button>

      <AdCampaignStatsModal
        isOpen={showStatsModal}
        campaign={{ name: 'گزارش تبلیغات فروشگاه', views: toPersianDigits(analytics.impressions), clicks: toPersianDigits(validClicksCount), ctr: analytics.ctr }}
        onClose={() => setShowStatsModal(false)}
      />
    </div>
  );
};
