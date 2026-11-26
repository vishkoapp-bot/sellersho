import React, { createContext, useContext, useState, useCallback } from 'react';
import { CheckCircle2, AlertCircle, AlertTriangle, Info, X } from 'lucide-react';
import { motion, AnimatePresence, useReducedMotion } from 'motion/react';

export type ToastType = 'success' | 'error' | 'warning' | 'info';

export interface ToastMessage {
  id: string;
  type: ToastType;
  message: string;
}

interface ToastContextType {
  showToast: (message: string, type?: ToastType) => void;
  success: (message: string) => void;
  error: (message: string) => void;
  warning: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastContextType | undefined>(undefined);

export const ToastProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);
  const reduceMotion = useReducedMotion();

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const showToast = useCallback((message: string, type: ToastType = 'info') => {
    const id = Math.random().toString(36).substring(2, 9);
    setToasts((prev) => [...prev, { id, type, message }]);

    setTimeout(() => {
      removeToast(id);
    }, 3800);
  }, [removeToast]);

  const success = useCallback((msg: string) => showToast(msg, 'success'), [showToast]);
  const error = useCallback((msg: string) => showToast(msg, 'error'), [showToast]);
  const warning = useCallback((msg: string) => showToast(msg, 'warning'), [showToast]);
  const info = useCallback((msg: string) => showToast(msg, 'info'), [showToast]);

  return (
    <ToastContext.Provider value={{ showToast, success, error, warning, info }}>
      {children}

      {/* Floating Toast Notification Container pinned inside viewport */}
      <div data-toast-stack aria-live="polite" aria-relevant="additions" className="fixed top-4 inset-x-0 max-w-[480px] mx-auto z-[10000] pointer-events-none px-3 flex flex-col gap-2">
        <AnimatePresence>
          {toasts.map((toast) => {
            const styles = {
              success: 'bg-slate-900/95 text-white border-emerald-500/40',
              error: 'bg-rose-950/95 text-white border-rose-500/40',
              warning: 'bg-amber-950/95 text-white border-amber-500/40',
              info: 'bg-slate-900/95 text-white border-blue-500/40',
            }[toast.type];

            const Icon = {
              success: CheckCircle2,
              error: AlertCircle,
              warning: AlertTriangle,
              info: Info,
            }[toast.type];

            const iconColor = {
              success: 'text-emerald-400',
              error: 'text-rose-400',
              warning: 'text-amber-400',
              info: 'text-blue-400',
            }[toast.type];

            return (
              <motion.div
                key={toast.id}
                initial={reduceMotion ? false : { opacity: 0, y: 16, scale: 0.95 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={reduceMotion ? { opacity: 0 } : { opacity: 0, y: -10, scale: 0.95 }}
                transition={{ duration: reduceMotion ? 0 : 0.2 }}
                className={`rounded-2xl border py-1.5 ps-3 pe-1.5 shadow-xl backdrop-blur-md flex items-center justify-between gap-2.5 ${styles}`}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Icon aria-hidden="true" className={`size-4 shrink-0 ${iconColor}`} />
                  <span className="break-words text-xs font-medium leading-relaxed">
                    {toast.message}
                  </span>
                </div>
                <button
                  type="button"
                  aria-label="بستن پیام"
                  onClick={() => removeToast(toast.id)}
                  className="pointer-events-auto grid size-11 shrink-0 place-items-center rounded-lg text-slate-300 hover:bg-white/10 hover:text-white focus-visible:outline-2 focus-visible:outline-white"
                >
                  <X aria-hidden="true" className="size-4" strokeWidth={1.5} />
                </button>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  );
};

export const useToast = (): ToastContextType => {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
};
