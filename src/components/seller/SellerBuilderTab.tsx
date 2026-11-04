import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { StoreSocialLinks } from '../../types';
import { Layout, Check, Palette, Eye, Share2, Instagram, Send, MessageCircle, ChevronDown } from 'lucide-react';

interface Props {
  onOpenStorefront: () => void;
  // Wired by the dashboard like SellerOverviewTab's onNavigateTab; until it
  // is passed, the "go to appearance" button below has nowhere to navigate.
  onNavigateTab?: (tab: string) => void;
}

// Live parity: in live this tab ("صفحه‌ساز") is the OLD builder page. Live
// shows a banner on top explaining that template/colour/banner/layout are
// now managed from "ظاهر فروشگاه", that this page's settings have no effect
// on the live storefront, and keeps the old settings collapsed by default.
// Everything below the banner (themes, colours, sections, social links) is
// live's old page, kept verbatim where the bundle proves it.
export const SellerBuilderTab: React.FC<Props> = ({ onOpenStorefront, onNavigateTab }) => {
  const { currentStore, updateStoreSocialLinks, updateStoreBuilderConfig } = useStore();
  const { success } = useToast();
  const [showOldSettings, setShowOldSettings] = useState(false);
  const [selectedTheme, setSelectedTheme] = useState(currentStore.builderConfig?.selectedTheme || 'digital');
  const [accentColor, setAccentColor] = useState(currentStore.builderConfig?.accentColor || '#5d44f8');
  const [widgets, setWidgets] = useState(
    currentStore.builderConfig?.widgets || {
      heroBanner: true,
      dealsSlider: true,
      categoriesGrid: true,
      storeDealsWidget: true,
      trustBar: true,
      contactInfo: true,
    }
  );
  const [socialLinks, setSocialLinks] = useState<StoreSocialLinks>(() => ({
    instagram: currentStore.builderConfig?.socialLinks?.instagram || '',
    telegram: currentStore.builderConfig?.socialLinks?.telegram || '',
    whatsapp: currentStore.builderConfig?.socialLinks?.whatsapp || '',
    eitaa: currentStore.builderConfig?.socialLinks?.eitaa || '',
    bale: currentStore.builderConfig?.socialLinks?.bale || '',
  }));
  const [saved, setSaved] = useState(false);

  const themes = [
    { id: 'digital', name: 'دیجیتال و گجت', desc: 'مناسب هندزفری، پاوربانک و اکسسوری' },
    { id: 'fashion', name: 'مد و پوشاک', desc: 'مناسب کیف چرم، کفش و اکسسوری استایل' },
    { id: 'home', name: 'خانه و آشپزخانه', desc: 'چیدمان مدرن لوازم خانگی و کاربردی' },
    { id: 'general', name: 'فروشگاه جامع', desc: 'ویترین کامل تمام دسته‌بندی‌های فعال' },
  ];

  // Live's builder swatch set (live default accent is #5d44f8).
  const colors = ['#5d44f8', '#059669', '#7c3aed', '#ea004b', '#0f172a'];

  const toggleWidget = (key: keyof typeof widgets) => {
    setWidgets(prev => ({ ...prev, [key]: !prev[key] }));
  };

  const handleSave = () => {
    updateStoreBuilderConfig({
      selectedTheme,
      accentColor,
      widgets,
      socialLinks,
    }, true);
    updateStoreSocialLinks(socialLinks);
    setSaved(true);
    success('تنظیمات ذخیره شد.');
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* Live banner: this is the old page; appearance moved to "ظاهر فروشگاه". */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
        <h3 className="text-xs font-bold text-slate-800">می‌خواهی ظاهر فروشگاهت را عوض کنی؟</h3>
        <p className="text-xs leading-6 text-slate-600">
          قالب، رنگ، بنر و چیدمان فروشگاهت از بخش «ظاهر فروشگاه» عوض می‌شود. این صفحه قدیمی است و تنظیماتش روی فروشگاهت اثری ندارد.
        </p>
        <button
          type="button"
          onClick={() => onNavigateTab?.('template')}
          className="w-full min-h-[48px] rounded-xl bg-blue-700 text-white text-xs font-extrabold"
        >
          رفتن به «ظاهر فروشگاه»
        </button>
        <button
          type="button"
          onClick={() => setShowOldSettings((v) => !v)}
          aria-expanded={showOldSettings}
          className="w-full flex items-center justify-center gap-1 min-h-[44px] text-[11px] font-bold text-slate-400"
        >
          <span>{showOldSettings ? 'بستن تنظیمات این صفحه' : 'نمایش تنظیمات این صفحه (قدیمی)'}</span>
          <ChevronDown className={`w-3.5 h-3.5 transition-transform ${showOldSettings ? 'rotate-180' : ''}`} strokeWidth={1.5} />
        </button>
      </div>

      {showOldSettings && (
        <>
      {/* Live "توجه" note for the old page. */}
      <div className="rounded-2xl bg-amber-50 border border-amber-200 px-3.5 py-2.5 text-[11px] text-amber-800 leading-relaxed">
        <strong>توجه:</strong> تنظیمات این بخش فعلاً روی ویترین زنده اعمال نمی‌شود؛ ظاهر فروشگاه شما از تب «ظاهر فروشگاه» مدیریت می‌شود. مقادیر این صفحه فقط ذخیره می‌شوند.
      </div>

      {/* 1. Theme Template Selector */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
        <div className="flex items-center gap-1.5 text-blue-600">
          <Layout className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">قالب موضوعی ویترین</h3>
        </div>

        <div className="grid grid-cols-2 gap-2">
          {themes.map((t) => (
            <div
              key={t.id}
              onClick={() => setSelectedTheme(t.id)}
              className={`p-2.5 rounded-xl border text-right cursor-pointer transition-all ${
                selectedTheme === t.id
                  ? 'bg-blue-50/60 border-blue-600 text-blue-900 ring-1 ring-blue-200'
                  : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
              }`}
            >
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-bold">{t.name}</span>
                {selectedTheme === t.id && <Check className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>}
              </div>
              <span className="text-[9px] text-slate-400 block leading-tight">{t.desc}</span>
            </div>
          ))}
        </div>
      </div>

      {/* 2. Brand Color Palette */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center gap-1.5 text-purple-600">
          <Palette className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">رنگ سازمانی ویترین</h3>
        </div>

        <div className="flex items-center gap-3">
          {colors.map((c) => (
            <button
              key={c}
              onClick={() => setAccentColor(c)}
              className="w-8 h-8 rounded-full flex items-center justify-center transition-transform active:scale-90"
              style={{ backgroundColor: c }}
            >
              {accentColor === c && <Check className="w-4 h-4 text-white"  strokeWidth={1.5}/>}
            </button>
          ))}
        </div>
      </div>

      {/* 3. Section Toggles (live: currently inactive on the live storefront) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <h3 className="text-xs font-bold text-slate-800 mb-2">بخش‌های صفحه اصلی ویترین (فعلاً غیرفعال روی ویترین زنده)</h3>

        <div className="space-y-1.5 text-xs text-slate-700">
          {[
            { key: 'heroBanner', label: 'اسلایدر بنر بزرگ بالای صفحه' },
            { key: 'dealsSlider', label: 'کاروسل پیشنهادهای تخفیف‌دار ویترین' },
            { key: 'categoriesGrid', label: 'نوار افقی دسته‌بندی‌های کالا' },
            { key: 'trustBar', label: 'نشان‌های تضمین اصالت و ارسال انبار' },
            { key: 'contactInfo', label: 'بخش اطلاعات تماس و پشتیبانی فروشگاه' },
          ].map((item) => (
            <label
              key={item.key}
              className="flex items-center justify-between p-2 rounded-xl bg-slate-50 border border-slate-100 cursor-pointer"
            >
              <span className="text-[11px] font-medium">{item.label}</span>
              <input
                type="checkbox"
                checked={widgets[item.key as keyof typeof widgets]}
                onChange={() => toggleWidget(item.key as keyof typeof widgets)}
                className="w-4 h-4 accent-blue-600 rounded cursor-pointer"
              />
            </label>
          ))}
        </div>
      </div>

      {/* 4. Social Media Accounts */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
        <div className="flex items-center gap-1.5 text-pink-600">
          <Share2 className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">لینک شبکه‌های اجتماعی ویترین شما</h3>
        </div>
        <p className="text-[10px] text-slate-500">
          این لینک‌ها ذخیره می‌شوند ولی فعلاً در ویترین زنده نمایش داده نمی‌شوند.
        </p>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
          <div>
            <label className="text-[10px] font-bold text-slate-600 block mb-1">اینستاگرام:</label>
            <input
              type="text"
              placeholder="@your_store"
              value={socialLinks.instagram || ''}
              onChange={(e) => setSocialLinks((prev) => ({ ...prev, instagram: e.target.value }))}
              className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-mono"
              dir="ltr"
            />
          </div>

          <div>
            <label className="text-[10px] font-bold text-slate-600 block mb-1">تلگرام:</label>
            <input
              type="text"
              placeholder="t.me/your_channel"
              value={socialLinks.telegram || ''}
              onChange={(e) => setSocialLinks((prev) => ({ ...prev, telegram: e.target.value }))}
              className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-mono"
              dir="ltr"
            />
          </div>

          <div>
            <label className="text-[10px] font-bold text-slate-600 block mb-1">واتساپ:</label>
            <input
              type="text"
              placeholder="۰۹۱۲…"
              value={socialLinks.whatsapp || ''}
              onChange={(e) => setSocialLinks((prev) => ({ ...prev, whatsapp: e.target.value }))}
              className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-mono"
              dir="ltr"
            />
          </div>

          <div>
            <label className="text-[10px] font-bold text-slate-600 block mb-1">ایتا:</label>
            <input
              type="text"
              placeholder="eitaa.com/your_store"
              value={socialLinks.eitaa || ''}
              onChange={(e) => setSocialLinks((prev) => ({ ...prev, eitaa: e.target.value }))}
              className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl text-xs font-mono"
              dir="ltr"
            />
          </div>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <button
          onClick={handleSave}
          className="flex-1 py-3 min-h-[48px] bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs transition-all"
        >
          {saved ? 'ذخیره شد ✓' : 'ذخیره تنظیمات'}
        </button>
        <button
          onClick={onOpenStorefront}
          className="flex items-center gap-1.5 px-3.5 py-3 min-h-[48px] bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl transition-all"
        >
          <Eye className="w-4 h-4"  strokeWidth={1.5}/>
          <span>مشاهده فروشگاه</span>
        </button>
      </div>
        </>
      )}
    </div>
  );
};
