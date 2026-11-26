import React, { useEffect, useState } from 'react';
import { ArrowUp, Mail, Phone, ShoppingBag } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { apiRequest, ApiError } from '../services/apiClient';
import { normalizePersianText, pathForView } from '../utils/routing';
import type { StaticPageKey } from '../context/storeContextTypes';
import { followStoreLink } from '../utils/followStoreLink';

export const Footer: React.FC = () => {
  const { openStaticPage, setActiveView, toPersianDigits } = useStore();
  const [newsMobile, setNewsMobile] = useState('');
  const [newsEmail, setNewsEmail] = useState('');
  const [newsDone, setNewsDone] = useState(false);
  const [newsError, setNewsError] = useState('');
  const [newsSubmitting, setNewsSubmitting] = useState(false);
  const [returnWindowDays, setReturnWindowDays] = useState<number | null>(null);
  const [content, setContent] = useState<Record<string, unknown>>({});

  useEffect(() => {
    let cancelled = false;
    try { if (localStorage.getItem('drup_newsletter_store-central')) setNewsDone(true); } catch { /* storage can be disabled */ }
    apiRequest<{ days?: unknown }>('/public/return-window-days').then(response => {
      const days = Number(response?.days);
      if (!cancelled && Number.isFinite(days) && days >= 1 && days <= 365) setReturnWindowDays(Math.round(days));
    }).catch(() => {});
    apiRequest<{ content?: Record<string, unknown> }>('/site-content')
      .then(response => { if (!cancelled && response?.content) setContent(response.content); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);

  const text = (key: string, fallback: string) => {
    const value = content[key];
    return typeof value === 'string' && value.trim() ? value : fallback;
  };

  const subscribe = async (event: React.FormEvent) => {
    event.preventDefault();
    if (newsSubmitting) return;
    const mobile = normalizePersianText(newsMobile.trim());
    if (!/^09[0-9]{9}$/.test(mobile)) {
      setNewsError('شمارهٔ موبایل را با ۰۹ و در ۱۱ رقم وارد کنید.');
      return;
    }
    setNewsError('');
    setNewsSubmitting(true);
    try {
      await apiRequest('/newsletter/subscribe', {
        method: 'POST', body: JSON.stringify({ storeSlug: 'central', mobile, email: newsEmail.trim() || undefined, source: 'footer' }),
      });
      try { localStorage.setItem('drup_newsletter_store-central', '1'); } catch { /* storage can be disabled */ }
      setNewsDone(true);
    } catch (error) {
      setNewsError(error instanceof ApiError && error.status === 429
        ? 'تعداد تلاش‌ها زیاد بود. کمی بعد دوباره امتحان کنید.'
        : 'عضویت ثبت نشد. دوباره امتحان کنید.');
    } finally {
      setNewsSubmitting(false);
    }
  };

  const infoLinks: [StaticPageKey, string][] = [
    ['about', 'درباره سلرشو'],
    ['faq', 'پرسش‌های متداول'],
    ['terms', text('footer.link_terms', returnWindowDays ? `قوانین و مهلت تست ${toPersianDigits(returnWindowDays)} روزه` : 'قوانین و مهلت تست')],
    ['privacy', 'حریم خصوصی'],
    ['license', 'مجوزها و نمادها'],
  ];

  return (
    <footer className="central-footer">
      <div className="central-wrap">
        <div className="central-footer-grid">
          <div className="central-footer-about">
            <a href="/" className="central-wordmark" onClick={event => followStoreLink(event, () => setActiveView('customer'))}>
              <span className="central-mark"><ShoppingBag size={22} aria-hidden="true" /></span><span>سلرشو</span>
            </a>
            <p>{text('footer.about_text', 'خرید آنلاین انواع کالای دیجیتال، مد و لوازم خانگی با مهلت تست و مرجوعی و ارسال به سراسر کشور.')}</p>
          </div>
          <nav aria-label="راهنمای خرید">
            <h2>راهنمای خرید</h2>
            {infoLinks.map(([key, label]) => (
              <a key={key} href={pathForView('static_page', { staticPageKey: key })}
                onClick={event => followStoreLink(event, () => openStaticPage(key))}>{label}</a>
            ))}
          </nav>
          <nav aria-label="همکاری با سلرشو">
            <h2>همکاری با سلرشو</h2>
            <a href={pathForView('supplier_landing')} onClick={event => followStoreLink(event, () => setActiveView('supplier_landing'))}>همکاری تأمین‌کننده</a>
            <a href={pathForView('dropshipper_landing')} onClick={event => followStoreLink(event, () => setActiveView('dropshipper_landing'))}>راه‌اندازی ویترین دراپ‌شیپینگ</a>
            <a href={pathForView('warehouse_panel')} onClick={event => followStoreLink(event, () => setActiveView('warehouse_panel'))}>ورود عملیات انبار</a>
          </nav>
          <div className="central-footer-contact">
            <h2>ارتباط با ما</h2>
            <a href="tel:+982191008080"><Phone size={16} aria-hidden="true" /><bdi>021-9100-8080</bdi></a>
            <a href="mailto:support@selersho.ir"><Mail size={16} aria-hidden="true" /><bdi>support@selersho.ir</bdi></a>
          </div>
        </div>

        <div className="central-newsletter">
          <h2>از پیشنهادهای سلرشو باخبر شوید</h2>
          {newsDone ? <p role="status">عضویت شما ثبت شد.</p> : (
            <form onSubmit={subscribe}>
              <label><span>شماره موبایل</span>
                <input name="mobile" autoComplete="tel-national" type="tel" inputMode="tel" dir="ltr" value={newsMobile}
                  onChange={event => { setNewsMobile(event.target.value); setNewsError(''); }} required placeholder="09123456789" />
              </label>
              <label><span>ایمیل (اختیاری)</span>
                <input name="email" autoComplete="email" type="email" dir="ltr" value={newsEmail}
                  onChange={event => setNewsEmail(event.target.value)} placeholder="name@example.com" />
              </label>
              <button type="submit" disabled={newsSubmitting}>{newsSubmitting ? 'در حال ثبت…' : 'عضویت در خبرنامه'}</button>
              {newsError && <p role="alert">{newsError}</p>}
            </form>
          )}
        </div>
        <div className="central-footer-bottom">
          <p>© سلرشو. کلیه حقوق محفوظ است.</p>
          <button type="button" onClick={() => window.scrollTo({ top: 0 })}>بازگشت به بالا <ArrowUp size={17} aria-hidden="true" /></button>
        </div>
      </div>
    </footer>
  );
};
