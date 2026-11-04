import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { StaticPageKey } from '../../context/storeContextTypes';
import { ArrowRight, ShieldCheck, HelpCircle, FileText, Lock, Info, ChevronDown } from 'lucide-react';
import { apiRequest } from '../../services/apiClient';

// FIX 14: static pages read the SAME public CMS source live uses
// (GET /api/v1/static-pages) instead of hardcoded texts. The old
// hardcoded licence tab claimed issued اینماد and union codes;
// live's CMS text says both are
// "در دست اقدام" with no codes -- that false claim is deleted here
// by rendering only CMS content. About/terms/privacy/license render
// as paragraphs (split on blank lines); FAQ parses its JSON array.
// Loading/error states appear only on fetch failure; on error a
// retry button is shown and no invented fallback text is rendered.
interface CmsPage {
  key: string;
  title: string;
  content: string;
  sortOrder: number;
  isActive: boolean;
}

interface FaqItem {
  q: string;
  a: string;
}

const TAB_ICONS: Record<string, React.ComponentType<{ className?: string; strokeWidth?: number | string }>> = {
  about: Info,
  terms: FileText,
  privacy: Lock,
  faq: HelpCircle,
  license: ShieldCheck,
};
// FIX 15: live's tab labels are hardcoded (bundle Tb), NOT the CMS
// page titles the candidate used: CMS titles are "درباره فروشگاه
// سلرشو / شرایط و قوانین خدمات / حریم خصوصی و امنیت / سوالات
// متداول / مجوزها و نمادهای اعتماد" while live's tabs read as below.
const TAB_LABELS: Record<string, string> = {
  about: 'درباره ما',
  terms: 'قوانین و ضمانت',
  privacy: 'حریم خصوصی',
  faq: 'پرسش‌های متداول',
  license: 'مجوزها',
};

export const StaticPageView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { staticPageKey } = useStore();
  const [pages, setPages] = useState<CmsPage[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<string>(staticPageKey || 'about');
  const [openFaq, setOpenFaq] = useState<number | null>(0);
  const tabRefs = useRef<Record<string, HTMLButtonElement | null>>({});
  // FIX 15: live syncs the active tab when the route's staticPageKey
  // changes (bundle z3) and scrolls the active tab into view; the
  // candidate kept the first tab forever across /about -> /license
  // navigation inside the same view instance.
  useEffect(() => { setActiveTab(staticPageKey || 'about'); setOpenFaq(0); }, [staticPageKey]);
  useEffect(() => {
    const el = tabRefs.current[activeTab];
    if (el) el.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' });
  }, [activeTab]);

  const load = () => {
    setLoadError(null);
    apiRequest<{ pages: CmsPage[] }>('/static-pages', { method: 'GET' })
      .then((data) => {
        const list = (data?.pages || []).filter((p) => p.isActive);
        setPages(list);
        if (list.length > 0 && !list.some((p) => p.key === activeTab)) {
          setActiveTab(list[0].key);
        }
      })
      .catch((err: any) => {
        setLoadError(err?.message || 'خطا در دریافت اطلاعات');
      });
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const activePage = pages?.find((p) => p.key === activeTab) || null;

  const renderParagraphs = (content: string) => (
    <div className="bg-white rounded-2xl border border-slate-200/90 p-4 space-y-2 text-xs text-slate-700 shadow-2xs leading-relaxed">
      {activePage && <h2 className="text-sm font-bold text-slate-900">{activePage.title}</h2>}
      {content.split(/\n\s*\n/).map((para, idx) => (
        <p key={idx}>{para}</p>
      ))}
    </div>
  );

  const renderFaq = (content: string) => {
    let items: FaqItem[] = [];
    try {
      const parsed = JSON.parse(content);
      if (Array.isArray(parsed)) items = parsed;
    } catch {
      items = [];
    }
    return (
      <div className="space-y-2">
        {items.map((f, idx) => (
          <div key={idx} className="bg-white rounded-2xl border border-slate-200/90 p-3 shadow-2xs">
            <button onClick={() => setOpenFaq(openFaq === idx ? null : idx)} className="w-full flex items-center justify-between text-right font-bold text-xs text-slate-800">
              <span>{f.q}</span>
              <ChevronDown className={`w-4 h-4 text-slate-400 transition-transform ${openFaq === idx ? 'rotate-180' : ''}`}  strokeWidth={1.5}/>
            </button>
            {openFaq === idx && <p className="text-[11px] text-slate-600 mt-2 pt-2 border-t border-slate-100">{f.a}</p>}
          </div>
        ))}
      </div>
    );
  };

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none pb-20 lg:pb-8">
      <header className="sticky top-0 z-30 lg:static bg-white border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <h1 className="text-xs font-bold text-slate-900">اطلاعات و قوانین</h1>
        </div>
      </header>

      {/* Tabs (labels from CMS titles, CMS order) */}
      {pages && (
        <div className="flex bg-white border-b border-slate-200 p-2 gap-1 overflow-x-auto no-scrollbar text-xs">
          {pages.map((p) => {
            const Icon = TAB_ICONS[p.key] || Info;
            return (
              <button
                key={p.key}
                ref={(el) => { tabRefs.current[p.key] = el; }}
                onClick={() => setActiveTab(p.key)}
                className={`py-1 px-2.5 rounded-xl font-bold flex items-center gap-1 shrink-0 ${
                  activeTab === p.key ? 'bg-brand text-white shadow-2xs' : 'text-slate-600 hover:bg-slate-100'
                }`}
              >
                <Icon className="w-3.5 h-3.5" strokeWidth={1.5} />
                <span>{TAB_LABELS[p.key] || p.title}</span>
              </button>
            );
          })}
        </div>
      )}

      <div className="p-3.5 w-full lg:max-w-4xl xl:max-w-5xl lg:px-8 lg:mx-auto">
        {loadError && (
          <div className="bg-white rounded-2xl border border-slate-200/90 p-4 space-y-3 text-xs text-slate-700 shadow-2xs text-center">
            <p>{loadError}</p>
            <button onClick={load} className="py-2 px-4 bg-slate-900 text-white font-bold rounded-xl">
              تلاش مجدد
            </button>
          </div>
        )}
        {!loadError && !pages && (
          <div className="bg-white rounded-2xl border border-slate-200/90 p-4 text-xs text-slate-500 shadow-2xs text-center">
            در حال بارگذاری…
          </div>
        )}
        {!loadError && pages && activePage && activePage.key === 'faq' && renderFaq(activePage.content)}
        {!loadError && pages && activePage && activePage.key !== 'faq' && renderParagraphs(activePage.content)}
      </div>
    </div>
  );
};
