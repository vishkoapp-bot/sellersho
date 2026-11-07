import React, { useState, useEffect } from 'react';
import { Sparkles, CheckCircle2, X } from 'lucide-react';

export const AntigravityModal: React.FC = () => {
  const [isOpen, setIsOpen] = useState(true);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-[99999] flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-fade-in">
      <div className="relative w-full max-w-md overflow-hidden rounded-2xl bg-white shadow-2xl border border-indigo-100 p-6 text-center transition-all">
        {/* Glow decorative effect */}
        <div className="absolute -top-12 -right-12 h-32 w-32 rounded-full bg-gradient-to-br from-indigo-500/20 to-purple-500/20 blur-2xl pointer-events-none" />
        <div className="absolute -bottom-12 -left-12 h-32 w-32 rounded-full bg-gradient-to-tr from-blue-500/20 to-teal-500/20 blur-2xl pointer-events-none" />

        {/* Close Button */}
        <button
          onClick={() => setIsOpen(false)}
          className="absolute top-4 left-4 rounded-full p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition"
          aria-label="بستن"
        >
          <X size={20} />
        </button>

        {/* Icon */}
        <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-indigo-600 to-purple-600 text-white shadow-lg shadow-indigo-500/30">
          <Sparkles size={32} className="animate-pulse" />
        </div>

        {/* Title & Message */}
        <div className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full bg-emerald-50 text-emerald-700 text-xs font-semibold mb-3 border border-emerald-200">
          <CheckCircle2 size={14} />
          <span>تأیید اتصال و دسترسی توسعه‌دهنده</span>
        </div>

        <h3 className="text-xl font-bold text-slate-900 mb-2 font-sans">
          من از آنتی گرویتی وصل شدم
        </h3>

        <p className="text-sm text-slate-600 leading-relaxed mb-6 font-sans">
          اتصال هوشمند Antigravity به مخزن و سرور پارس‌پک برقرار شد. اکنون قابلیت توسعه، اعمال تغییرات، تست و استقرار خودکار بر روی پروژه سلرشو کاملاً آماده است.
        </p>

        {/* Action Button */}
        <button
          type="button"
          onClick={() => setIsOpen(false)}
          className="w-full rounded-xl bg-gradient-to-r from-indigo-600 to-purple-600 py-3 px-4 font-bold text-white shadow-md hover:from-indigo-700 hover:to-purple-700 transition active:scale-[0.98]"
        >
          متوجه شدم و ورود به سایت
        </button>
      </div>
    </div>
  );
};
