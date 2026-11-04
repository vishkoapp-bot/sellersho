import React from 'react';
import { Header } from './Header';
import { SearchX, Home, ArrowRight } from 'lucide-react';
import { Footer } from './Footer';

// FIX 16: live has TWO 404 components, and the candidate conflated them.
// Generic 404 (bundle Eb, used for unknown pages and product misses):
// white bg, w-20 circle bg-brand/10 with a SearchX icon (magnifier + x,
// w-10 text-brand), text-5xl black numerals, both buttons (primary
// "بازگشت به خانه" + text link "بازگشت به صفحه قبل").
// Store-path 404 (bundle ek, used for ANY /store/* miss, suspended or
// nonexistent): bg #FAF9F7, stone-200/70 circle with a stone-400
// SearchX, stone-900 numerals, the store body text, and ONLY the back
// link -- no primary button. Fix 15 used the generic view for suspended
// slugs; the browser proved live's suspended page has only the link.
// Trust strip: live shows it ONCE, inside the site footer (Z5 renders
// its own trust row). The candidate rendered TrustFeatures light cards
// PLUS the footer trust row on 404 pages -- the duplication the
// re-check flagged. TrustFeatures is removed here; Footer keeps its
// own trust row, exactly like live.
// Icon identities: Wb (live's icon) is SearchX (magnifier + x, the
// re-check's description); ld (home button icon) is Home; ns (back
// link icon) is ArrowRight (RTL "back" direction). All are the only
// lucide glyphs matching the bundle's usage + the browser description.
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
      <Header />
      <main className={`flex-1 flex flex-col items-center justify-center px-6 py-16 text-center select-none ${isStore ? 'bg-[#FAF9F7]' : 'bg-white'}`}>
        <div className={`w-20 h-20 rounded-full flex items-center justify-center mb-5 ${isStore ? 'bg-stone-200/70' : 'bg-brand/10'}`}>
          <SearchX className={`w-10 h-10 ${isStore ? 'text-stone-400' : 'text-brand'}`} strokeWidth={1.5} />
        </div>
        <h1 className={`text-5xl font-black tracking-tight ${isStore ? 'text-stone-900' : 'text-slate-900'}`}>۴۰۴</h1>
        <h2 className={`mt-2 text-sm font-bold ${isStore ? 'text-stone-800' : 'text-slate-800'}`}>
          {isStore ? 'صفحه مورد نظر یافت نشد' : (title || 'صفحه مورد نظر یافت نشد')}
        </h2>
        <p className={`mt-2 text-xs leading-relaxed max-w-[260px] ${isStore ? 'text-stone-500' : 'text-slate-500'}`}>
          {isStore ? 'به نظر می‌رسد نشانی فروشگاه اشتباه است یا این فروشگاه دیگر فعال نیست.' : (message || 'به نظر می‌رسد آدرسی که وارد کرده‌اید اشتباه است یا این صفحه حذف شده است.')}
        </p>
        {!isStore && (
          <button
            type="button"
            onClick={onBack}
            className="mt-6 w-full max-w-[240px] py-3 bg-brand hover:bg-brand-deep text-white font-bold text-sm rounded-2xl flex items-center justify-center gap-2 transition-colors"
          >
            <Home className="w-4 h-4" strokeWidth={1.5} />
            <span>بازگشت به خانه</span>
          </button>
        )}
        <button
          type="button"
          onClick={goBack}
          className={`flex items-center gap-1 transition-colors ${isStore ? 'mt-6 text-xs font-bold text-stone-500 hover:text-stone-800' : 'mt-3 text-xs font-bold text-slate-500 hover:text-brand'}`}
        >
          <ArrowRight className="w-3.5 h-3.5" strokeWidth={1.5} />
          <span>بازگشت به صفحه قبل</span>
        </button>
      </main>
      <Footer />
    </div>
  );
};
