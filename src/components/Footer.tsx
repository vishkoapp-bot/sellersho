import { HelpExplainer } from './common/HelpExplainer';
import React, { useState, useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { apiRequest, ApiError } from '../services/apiClient';
import { siteContentApi, FALLBACK_TRUST_FEATURES, TrustFeature } from '../services/siteContentApi';
import { Sparkles, Phone, Mail, MapPin, FileText, HelpCircle, Lock, Info, Package, Store, Warehouse, BadgeCheck, CreditCard, Truck, ShieldCheck, RotateCcw, Headphones } from 'lucide-react';

const TRUST_ICONS: Record<string, React.ComponentType<{ className?: string; strokeWidth?: number | string }>> = {
  CreditCard,
  Truck,
  ShieldCheck,
  RotateCcw,
  Headphones,
};

// FIX 12: live footer layout — brand / دسترسی سریع / همکاری / contact in
// one 4-column row, newsletter in a SEPARATE white card row below, and
// the link list exactly live's (site-content footer.link_*): faq without
// "(FAQ)", dropship with "ینگ", plus "مجوزها و نمادها", and the email
// typo fixed (sellershow.ir -> selersho.ir). The brand tagline pill
// stays: live's footer carries it too (footer.tagline).
// FIX 13: live's newsletter is NOT a full-width card below the grid — it
// is a SPLIT row INSIDE the grid (hidden lg:block lg:col-span-4): heading
// on the right, white form card on the left, hidden on mobile. The form
// posts to /api/v1/newsletter/subscribe with live's validation and
// messages (the old form faked success without any API call).
export const Footer: React.FC = () => {
  const { openStaticPage, setActiveView, toPersianDigits } = useStore();
  const [newsMobile, setNewsMobile] = useState('');
  const [newsEmail, setNewsEmail] = useState('');
  const [newsDone, setNewsDone] = useState(false);
  const [newsError, setNewsError] = useState('');
  const [newsSubmitting, setNewsSubmitting] = useState(false);

  useEffect(() => {
    try {
      if (localStorage.getItem('drup_newsletter_store-central')) setNewsDone(true);
    } catch {
      // private-mode storage may throw; the form still works
    }
  }, []);

  const handleNewsletterSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (newsSubmitting) return;
    const mobile = newsMobile.trim();
    if (!/^09[0-9]{9}$/.test(mobile)) {
      setNewsError('شماره موبایل معتبر نیست. شماره را به‌صورت ۰۹xxxxxxxxx وارد کنید.');
      return;
    }
    const email = newsEmail.trim();
    if (email !== '' && !/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(email)) {
      setNewsError('ایمیل واردشده معتبر نیست.');
      return;
    }
    setNewsError('');
    setNewsSubmitting(true);
    try {
      await apiRequest('/newsletter/subscribe', {
        method: 'POST',
        body: JSON.stringify({ storeSlug: 'central', mobile, email: email || undefined, source: 'footer' }),
      });
      try {
        localStorage.setItem('drup_newsletter_store-central', '1');
      } catch {
        // ignore storage failures
      }
      setNewsDone(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) {
        setNewsError('تلاش بیش از حد؛ کمی بعد دوباره امتحان کنید.');
      } else {
        setNewsError('خطا در برقراری ارتباط؛ دوباره تلاش کنید.');
      }
    } finally {
      setNewsSubmitting(false);
    }
  };
  const [trustFeatures, setTrustFeatures] = useState<TrustFeature[]>(FALLBACK_TRUST_FEATURES.slice(0, 4));

  // Live parity (bundle Z5): the terms link is site-content
  // footer.link_terms when present, else the dynamic return-window
  // label (days!=null ? `... ${days} روزه` : `قوانین و ضمانت تست`).
  // The return-window helper returns null on failure/invalid, exactly
  // like live getReturnWindowDays (valid 1..365, else null). The
  // tagline pill and about text also come from site-content
  // (footer.tagline / footer.about_text) with live defaults.
  const [returnWindowDays, setReturnWindowDays] = useState<number | null>(null);
  const [footerContent, setFooterContent] = useState<Record<string, unknown>>({});

  useEffect(() => {
    let cancelled = false;
    apiRequest<{ days?: unknown }>('/public/return-window-days')
      .then((res) => {
        const n = Number(res?.days);
        if (!cancelled && Number.isFinite(n) && n >= 1 && n <= 365) setReturnWindowDays(Math.round(n));
      })
      .catch(() => {});
    apiRequest<{ content?: Record<string, unknown> }>('/site-content')
      .then((res) => { if (!cancelled && res?.content) setFooterContent(res.content); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);

  const siteText = (key: string, fallback: string): string => {
    const v = footerContent[key];
    return typeof v === 'string' ? v : fallback;
  };
  const footerTagline = siteText('footer.tagline', 'فروشگاه آنلاین کالا و خدمات');
  const footerAboutText = siteText('footer.about_text', 'خرید آنلاین انواع کالای دیجیتال، مد و لوازم خانگی با مهلت تست و مرجوعی و ارسال به سراسر کشور.');
  const termsLabel = siteText('footer.link_terms', returnWindowDays != null ? `قوانین و ضمانت ${toPersianDigits(returnWindowDays)} روزه` : 'قوانین و ضمانت تست');

  useEffect(() => {
    let cancelled = false;
    siteContentApi.getTrustFeatures().then((list) => {
      if (!cancelled) setTrustFeatures(list.slice(0, 4));
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const linkCls = 'flex items-center gap-1.5 text-slate-400 hover:text-white transition-colors text-right';
  const iconCls = 'w-3.5 h-3.5 text-brand';

  return (
    <footer className="bg-slate-900 text-slate-300 pt-8 pb-10 border-t border-slate-800 px-4 lg:px-8 xl:px-10 2xl:px-12 select-none">
      <div className="grid grid-cols-2 gap-6 lg:grid-cols-[1.5fr_1fr_1fr_1.2fr] lg:gap-10 xl:gap-16 lg:space-y-0 lg:py-12">
        {trustFeatures.length > 0 && (
          <div className="hidden lg:flex lg:col-span-4 items-center gap-8 border-b border-slate-800 pb-6">
            {trustFeatures.map((f) => {
              const Icon = TRUST_ICONS[f.icon] || ShieldCheck;
              const trustTitle = f.title.includes('ضمانت تست')
                ? (returnWindowDays != null ? `ضمانت تست ${toPersianDigits(returnWindowDays)} روزه` : 'ضمانت تست')
                : f.title;
              return (
                <div key={f.title} className="flex items-center gap-2">
                  <Icon className="w-5 h-5 text-slate-400 shrink-0" strokeWidth={1.5} />
                  <div>
                    <p className="text-xs font-bold text-slate-100">{trustTitle}</p>
                    <p className="text-[11px] text-slate-400">{f.description}</p>
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* Brand */}
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded-lg bg-blue-600 flex items-center justify-center text-white shadow-xs">
              <Sparkles className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </div>
            <span className="font-black text-sm text-white">سلرشو</span>
            <span className="text-[10px] text-blue-400 bg-blue-950/60 border border-blue-800/60 px-1.5 py-0.2 rounded">
              {footerTagline}
            </span>
          </div>
          <p className="text-[11px] text-slate-400 leading-relaxed">
            {footerAboutText}
          </p>
        </div>

        {/* دسترسی سریع */}
        <div className="space-y-2 text-xs">
          <h4 className="hidden lg:block text-sm font-bold text-slate-100">دسترسی سریع</h4>
          <button onClick={() => openStaticPage('about')} className={linkCls}><Info className={iconCls}  strokeWidth={1.5}/><span>درباره فروشگاه سلرشو</span></button>
          <button onClick={() => openStaticPage('terms')} className={linkCls}><FileText className={iconCls}  strokeWidth={1.5}/><span>{termsLabel}</span></button>
          <button onClick={() => openStaticPage('privacy')} className={linkCls}><Lock className={iconCls}  strokeWidth={1.5}/><span>حریم خصوصی و امنیت</span></button>
          <button onClick={() => openStaticPage('faq')} className={linkCls}><HelpCircle className={iconCls}  strokeWidth={1.5}/><span>پرسش‌های متداول</span></button>
          <button onClick={() => openStaticPage('license')} className={linkCls}><BadgeCheck className={iconCls}  strokeWidth={1.5}/><span>مجوزها و نمادها</span></button>
        </div>

        {/* همکاری */}
        <div className="space-y-2 text-xs">
          <h4 className="hidden lg:block text-sm font-bold text-slate-100">همکاری</h4>
          <button onClick={() => setActiveView('supplier_landing')} className={linkCls}><Package className={iconCls}  strokeWidth={1.5}/><span>همکاری تأمین‌کننده</span></button>
          <button onClick={() => setActiveView('dropshipper_landing')} className={linkCls}><Store className={iconCls}  strokeWidth={1.5}/><span>راه‌اندازی دراپ‌شیپینگ</span></button>
          <button onClick={() => setActiveView('warehouse_panel')} className={linkCls}><Warehouse className={iconCls}  strokeWidth={1.5}/><span>ورود عملیات انبار</span></button>
        </div>

        {/* Contact */}
        <div className="space-y-2 text-xs text-slate-400">
          <div className="flex items-center gap-2">
            <Phone className={iconCls}  strokeWidth={1.5}/>
            <span dir="ltr">021 - 9100 8080</span>
          </div>
          <div className="flex items-center gap-2">
            <Mail className={iconCls}  strokeWidth={1.5}/>
            <span>support@selersho.ir</span>
          </div>
          <div className="flex items-start gap-2">
            <MapPin className={`${iconCls} mt-0.5`}  strokeWidth={1.5}/>
            <span>تهران، مجتمع فناوری و لجستیک مرکزی</span>
          </div>
        </div>

        {/* Newsletter — live's split row inside the grid (desktop only) */}
        <div className="hidden lg:block lg:col-span-4 border-t border-slate-800 pt-6">
          <div className="flex items-start justify-between gap-10">
            <div className="max-w-md">
              <h3 className="text-base lg:text-lg font-bold text-slate-100 flex items-center gap-1">از تخفیف‌ها و کالاهای جدید باخبر شوید<HelpExplainer text="شماره موبایل خود را ثبت کنید تا از جدیدترین کالاها و پیشنهادها مطلع شوید." /></h3>
            </div>
            <div className="w-full max-w-md rounded-2xl bg-white p-4 shrink-0 text-slate-800">
              {newsDone ? (
                <p className="text-emerald-600 font-bold text-xs">شماره شما برای اطلاع‌رسانی همین فروشگاه ثبت شد.</p>
              ) : (
                <form onSubmit={handleNewsletterSubmit} className="space-y-2">
                  <label className="block">
                    <span className="text-[10px] font-bold text-slate-700 block">شماره موبایل</span>
                    <input
                      type="tel"
                      dir="ltr"
                      value={newsMobile}
                      onChange={(e) => setNewsMobile(e.target.value)}
                      placeholder="09xxxxxxxxx"
                      className="w-full rounded-xl border border-slate-200 bg-white focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/20 text-[11px] p-2.5 text-left placeholder:text-left"
                    />
                  </label>
                  <label className="block">
                    <span className="text-[10px] font-bold text-slate-700 block">ایمیل (اختیاری)</span>
                    <input
                      type="email"
                      dir="ltr"
                      value={newsEmail}
                      onChange={(e) => setNewsEmail(e.target.value)}
                      placeholder="you@example.com"
                      className="w-full rounded-xl border border-slate-200 bg-white focus:outline-none focus:border-brand focus:ring-2 focus:ring-brand/20 text-[11px] p-2.5 text-left placeholder:text-left"
                    />
                  </label>
                  {newsError !== '' && <p className="text-rose-500 font-bold text-[11px]">{newsError}</p>}
                  <button
                    type="submit"
                    disabled={newsSubmitting}
                    className="w-full font-bold text-white rounded-xl flex items-center justify-center gap-1.5 transition-opacity disabled:opacity-50 py-2.5 text-[11px] bg-[#5d44f8] hover:bg-[#4a33d8]"
                  >
                    {newsSubmitting ? 'در حال ثبت…' : 'عضویت در اطلاع‌رسانی'}
                  </button>
                </form>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Copyright */}

      <div className="border-t border-slate-800 pt-4 mt-6 text-center text-[10px] lg:text-xs text-slate-500 space-y-3">
        <button
          onClick={() => window.scrollTo({ top: 0, behavior: 'smooth' })}
          className="inline-flex items-center gap-1 rounded-full border border-slate-700 px-3 py-1.5 text-[11px] text-slate-300 hover:text-white hover:border-slate-500 transition-colors"
        >
          بازگشت به بالا
        </button>
        <p>© ۱۴۰۵ سلرشو. کلیه حقوق محفوظ است.</p>
      </div>
    </footer>
  );
};
