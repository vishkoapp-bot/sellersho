import React, { useState } from 'react';
import { SupplierInboundTab } from './SupplierInboundTab';
import { SupplierLabeledInboundTab } from './SupplierLabeledInboundTab';
import { Package, Tag, Check } from 'lucide-react';

// FIX 46: built from the live bundle SupplierDashboardView-MERGED.js (nn component).
// The inbounds tab opens on this switch, NOT on a form: h2 «فرستادن
// بار به انبار» + help, two mode cards (simple / labeled), a hint while
// no mode is chosen, tn embedded for «simple», ln for «labeled». Live's
// nn takes flow/onFlowChange as props (parent-owned state); this
// container owns the state, initially null so the hint shows.
// NOTES: lucide icons are approximations (live's wa/il/at identities
// are bundle-internal). The previous candidate tab rendered tn directly
// with its own h2/help — that matched live's simple-mode content but
// not live's default tab view, which is this switch.
const MODES = [
  { id: "simple", icon: Package, title: "بار معمولی — بدون برچسب", body: "کالا را انتخاب می‌کنی و می‌نویسی چند عدد است، داخل چند کارتن، و با چه راننده یا باربری‌ای می‌آید. لازم نیست روی کارتن‌ها کدی بچسبانی؛ انبار با همین ثبت، بارت را تحویل می‌گیرد." },
  { id: "labeled", icon: Tag, title: "با برچسب کد روی کارتن", body: "مدل‌های کالا را یکی‌یکی با تعدادشان انتخاب می‌کنی؛ سامانه یک کد به تو می‌دهد. کد را روی کارتن بنویس یا برچسبش را چاپ کن و بچسبان. انبار با آن کد، تعداد هر مدل را جدا چک می‌کند." },
];

export const SupplierInboundFlowTab: React.FC = () => {
  const [flow, setFlow] = useState<string | null>(null);
  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">فرستادن بار به انبار</h2>
      <div className="rounded-2xl border bg-white p-3 text-[11px] leading-5 text-slate-600">هر وقت می‌خواهی بار به انبار مرکزی بفرستی، اول همین‌جا ثبتش کن تا انبار آماده تحویل باشد. دو راه داری؛ هر کدام به کارت می‌آید، همان را انتخاب کن تا فرمش باز شود.</div>
      <div className="space-y-2">
        {MODES.map((m) => {
          const selected = flow === m.id;
          const Icon = m.icon;
          return (
            <button key={m.id} type="button" aria-pressed={selected} onClick={() => setFlow(m.id)} className={`w-full text-right rounded-2xl border p-3.5 flex items-start gap-3 transition-all ${selected ? "border-brand-deep bg-brand-soft/60 shadow-2xs" : "border-slate-200 bg-white hover:border-brand-border"}`}>
              <span className={`w-10 h-10 rounded-xl flex items-center justify-center shrink-0 ${selected ? "bg-brand-deep text-white" : "bg-brand-soft text-brand-deep"}`}><Icon className="w-5 h-5" strokeWidth={1.5} /></span>
              <span className="flex-1 min-w-0">
                <span className="flex items-center gap-2">
                  <span className="text-xs font-extrabold text-slate-800">{m.title}</span>
                  {selected && (<span className="inline-flex items-center gap-1 rounded-full bg-brand-deep text-white px-2 py-0.5 text-[10px] font-bold"><Check className="w-3 h-3" strokeWidth={2} />انتخاب شده</span>)}
                </span>
                <span className="block text-[11px] leading-5 text-slate-500 mt-1">{m.body}</span>
              </span>
            </button>
          );
        })}
      </div>
      {flow === null && (<p className="text-center text-[11px] leading-5 text-slate-400 py-2">یکی از دو راه بالا را انتخاب کن؛ فرم و بارهای قبلی همان راه، این پایین باز می‌شود.</p>)}
      {flow === "simple" && (<SupplierInboundTab embedded />)}
      {flow === "labeled" && (<SupplierLabeledInboundTab />)}
    </div>
  );
};
