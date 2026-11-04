import React, { useState, useEffect } from 'react';
import { CreditCard, Truck, ShieldCheck, RotateCcw, Headphones } from 'lucide-react';
import { siteContentApi, FALLBACK_TRUST_FEATURES, TrustFeature } from '../services/siteContentApi';

const ICONS: Record<string, React.ComponentType<{ className?: string; strokeWidth?: number | string }>> = {
  CreditCard,
  Truck,
  ShieldCheck,
  RotateCcw,
  Headphones,
};

const COLORS: Record<string, string> = {
  violet: 'text-violet-600 bg-violet-50',
  emerald: 'text-emerald-600 bg-emerald-50',
  amber: 'text-amber-600 bg-amber-50',
  rose: 'text-rose-600 bg-rose-50',
  blue: 'text-blue-600 bg-blue-50',
  indigo: 'text-indigo-600 bg-indigo-50',
};

// FIX 12: the trust strip reads the live texts from /api/v1/site-content
// (trust_features) instead of hardcoding 4 short items — staging
// site-content already holds the same 5 live values, so this is CODE,
// not data. The fallback is live's exact text, not the old short copy.
// FIX 13: live's API holds 5 trust_features but live's frontend renders
// a.slice(0, 4) in a lg:grid-cols-4 strip (its bundle fallback has only
// 4 items). Match the RENDERED 4, not the API 5.
export const TrustFeatures: React.FC = () => {
  const [features, setFeatures] = useState<TrustFeature[]>(FALLBACK_TRUST_FEATURES);

  useEffect(() => {
    let cancelled = false;
    siteContentApi.getTrustFeatures().then((list) => {
      if (!cancelled) setFeatures(list);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="px-3.5 py-1 lg:px-6 lg:py-3 xl:px-10 2xl:px-12">
      <div className="bg-white rounded-2xl p-3 border border-slate-100 shadow-xs">
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-2.5 xl:gap-4 2xl:gap-6">
          {features.slice(0, 4).map((item) => {
            const Icon = ICONS[item.icon] || ShieldCheck;
            return (
              <div key={item.title} className="flex items-center gap-2 p-1.5 bg-slate-50/70 rounded-xl lg:gap-3 lg:p-2.5">
                <div className={`w-8 h-8 lg:w-10 lg:h-10 rounded-lg flex items-center justify-center shrink-0 ${COLORS[item.color] || COLORS.blue}`}>
                  <Icon className="w-4 h-4 lg:w-5 lg:h-5" strokeWidth={1.5} />
                </div>
                <div className="min-w-0">
                  <h3 className="text-[11px] lg:text-sm font-bold text-slate-800 break-words leading-relaxed">
                    {item.title}
                  </h3>
                  <p className="text-[9px] lg:text-xs text-slate-400 break-words leading-relaxed">
                    {item.description}
                  </p>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
};
