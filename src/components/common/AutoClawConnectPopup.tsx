import React, { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'motion/react';
import { Bot, X } from 'lucide-react';

// AutoClaw connectivity marker (temporary test): shows once per browser
// session on the central homepage to verify the AutoClaw -> repo -> deploy
// development loop end to end. Safe to remove once the check is done.
const SESSION_KEY = 'autoclaw-popup-dismissed';

export const AutoClawConnectPopup: React.FC = () => {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    try {
      if (sessionStorage.getItem(SESSION_KEY) !== '1') setOpen(true);
    } catch {
      setOpen(true);
    }
  }, []);

  const dismiss = () => {
    setOpen(false);
    try {
      sessionStorage.setItem(SESSION_KEY, '1');
    } catch {
      /* storage unavailable; popup simply re-shows next mount */
    }
  };

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') dismiss();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  return (
    <AnimatePresence>
      {open && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-sm"
          onClick={dismiss}
        >
          <motion.div
            initial={{ opacity: 0, scale: 0.95, y: 10 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 10 }}
            transition={{ duration: 0.2 }}
            role="dialog"
            aria-modal="true"
            aria-label="پیام اتصال AutoClaw"
            className="relative w-full max-w-sm rounded-2xl bg-white p-6 text-center shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <button
              onClick={dismiss}
              aria-label="بستن"
              className="absolute left-4 top-4 rounded-full p-1.5 text-slate-400 transition hover:bg-slate-100 hover:text-slate-600"
            >
              <X className="h-5 w-5" />
            </button>
            <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500 to-fuchsia-500 shadow-lg shadow-violet-500/30">
              <Bot className="h-9 w-9 text-white" />
            </div>
            <h2 className="text-lg font-bold text-slate-900">من از AutoClaw وصل شدم</h2>
            <p className="mt-2 text-sm leading-6 text-slate-500">
              این پیام یک تست اتصال است: AutoClaw به ریپو وصل شد و این تغییر را روی صفحه اصلی سایت گذاشت.
            </p>
            <button
              onClick={dismiss}
              className="mt-5 w-full rounded-xl bg-slate-900 px-4 py-2.5 text-sm font-bold text-white transition hover:bg-slate-700"
            >
              متوجه شدم
            </button>
          </motion.div>
        </div>
      )}
    </AnimatePresence>
  );
};
