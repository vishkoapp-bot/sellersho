import React from 'react';
import { Header } from './Header';
import { SearchX, Home, ArrowRight } from 'lucide-react';
import { Footer } from './Footer';

// Missing seller storefronts keep their own neutral view without platform chrome.
export const NotFoundView: React.FC<{
  title?: string;
  message?: string;
  variant?: 'generic' | 'store';
  onBack: () => void;
}> = ({ title, message, variant = 'generic', onBack }) => {
  const goBack = () => {
    if (typeof window !== 'undefined' && window.history.length > 1) {
      window.history.back();
    } else {
      onBack();
    }
  };
  const isStore = variant === 'store';
  return (
    <div className="flex-1 flex flex-col bg-white min-h-screen">
      {!isStore && <Header mainId="main-content" />}
      <main id={isStore ? undefined : 'main-content'} tabIndex={-1} className={`flex-1 flex flex-col items-center justify-center px-6 py-16 text-center ${isStore ? 'bg-[#FAF9F7]' : 'bg-[var(--shop-bg,#f8f6ef)] text-[var(--shop-ink,#172d26)]'}`}>
        <div className={`w-20 h-20 rounded-full flex items-center justify-center mb-5 ${isStore ? 'bg-stone-200/70' : 'bg-[var(--shop-soft,#edf2e9)]'}`}>
          <SearchX className={`w-10 h-10 ${isStore ? 'text-stone-400' : 'text-[var(--shop-forest,#164638)]'}`} strokeWidth={1.5} />
        </div>
        <h1 className={`text-5xl font-black tracking-tight ${isStore ? 'text-stone-900' : 'text-[var(--shop-ink,#172d26)]'}`}>۴۰۴</h1>
        <h2 className={`mt-2 text-lg font-bold ${isStore ? 'text-stone-800' : 'text-[var(--shop-ink,#172d26)]'}`}>
          {isStore ? 'صفحه مورد نظر یافت نشد' : (title || 'صفحه مورد نظر یافت نشد')}
        </h2>
        <p className={`mt-2 max-w-sm text-sm leading-7 ${isStore ? 'text-stone-500' : 'text-[var(--shop-muted,#4f6359)]'}`}>
          {isStore ? 'به نظر می‌رسد نشانی فروشگاه اشتباه است یا این فروشگاه دیگر فعال نیست.' : (message || 'به نظر می‌رسد آدرسی که وارد کرده‌اید اشتباه است یا این صفحه حذف شده است.')}
        </p>
        {!isStore && (
          <button
            type="button"
            onClick={onBack}
            className="central-button mt-6 w-full max-w-60"
          >
            <Home className="w-4 h-4" strokeWidth={1.5} />
            <span>بازگشت به خانه</span>
          </button>
        )}
        <button
          type="button"
          onClick={goBack}
          className={`flex items-center gap-1 transition-colors ${isStore ? 'mt-6 text-xs font-bold text-stone-500 hover:text-stone-800' : 'mt-3 min-h-11 rounded-xl px-3 text-sm font-bold text-[var(--shop-forest,#164638)] hover:bg-[var(--shop-soft,#edf2e9)]'}`}
        >
          <ArrowRight className="w-3.5 h-3.5" strokeWidth={1.5} />
          <span>بازگشت به صفحه قبل</span>
        </button>
      </main>
      {!isStore && <Footer />}
    </div>
  );
};
