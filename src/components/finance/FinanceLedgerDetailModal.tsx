import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { financeApi, recentLedgerWindow, LedgerEntryDto } from '../../services/financeApi';
import { X, Receipt, Printer, AlertCircle, FileText } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  isOpen: boolean;
  onClose: () => void;
}

type LedgerRequest = { offset: number; cursor?: string };

export const FinanceLedgerDetailModal: React.FC<Props> = ({ isOpen, onClose }) =>
  isOpen ? <LedgerWindow onClose={onClose} /> : null;

const LedgerWindow: React.FC<Pick<Props, 'onClose'>> = ({ onClose }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const [openedAt] = useState(() => new Date());
  const [request, setRequest] = useState<LedgerRequest>({ offset: 0 });
  const [entries, setEntries] = useState<LedgerEntryDto[]>([]);
  const [nextCursor, setNextCursor] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const requestedPage = useRef(request);
  const { offset } = request;
  const { dateFrom, dateTo, limit } = recentLedgerWindow(openedAt, offset);

  useEffect(() => {
    let cancelled = false;
    financeApi.getLedger({ dateFrom, dateTo, limit, cursor: request.cursor })
      .then((page) => {
        if (cancelled || requestedPage.current !== request) return;
        setEntries(current => request.cursor ? [...current, ...page.entries] : page.entries);
        setNextCursor(page.hasMore ? page.nextCursor : undefined);
        setLoading(false);
      })
      .catch(() => {
        if (cancelled || requestedPage.current !== request) return;
        setError(true);
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, [dateFrom, dateTo, limit, request]);

  const load = (page: LedgerRequest) => {
    requestedPage.current = page;
    setLoading(true);
    setError(false);
    if (!page.cursor) {
      setEntries([]);
      setNextCursor(undefined);
    }
    setRequest(page);
  };

  const displayDate = (date: string) => new Date(date).toLocaleDateString('fa-IR', { timeZone: 'UTC' });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[440px] w-full p-4 shadow-2xl border border-slate-100 flex flex-col max-h-[85vh] overflow-hidden space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-teal-50 text-teal-600 flex items-center justify-center">
              <Receipt className="w-4 h-4" strokeWidth={1.5} />
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">دفتر روزنامه و اسناد حسابداری دوبل</h3>
              <span className="text-[10px] text-slate-500 block">
                {offset === 0 ? 'اسناد ۳۱ روز اخیر' : 'اسناد بازهٔ ۳۱ روزه'} · {displayDate(dateFrom)} تا {displayDate(dateTo)}
              </span>
              {entries.length > 0 && <span className="text-[10px] text-slate-500 block">{toPersianDigits(entries.length)} سند نمایش داده شده</span>}
            </div>
          </div>
          <button type="button" aria-label="بستن دفتر کل" onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4" strokeWidth={1.5} />
          </button>
        </div>

        <div className="overflow-y-auto no-scrollbar space-y-2 text-xs flex-1">
          {loading && entries.length === 0 && (
            <div className="p-4 text-center text-[11px] text-slate-400">در حال بارگذاری اسناد…</div>
          )}
          {error && (
            <div className="p-3 bg-rose-50 border border-rose-200 rounded-xl flex items-center gap-2 text-rose-700 text-[11px]">
              <AlertCircle className="w-4 h-4 shrink-0" strokeWidth={1.5} />
              <span>اسناد دفتر کل بارگذاری نشد.</span>
              <button type="button" onClick={() => load({ ...request })} className="mr-auto underline">تلاش دوباره</button>
            </div>
          )}
          {!loading && !error && entries.length === 0 && (
            <div className="p-6 text-center space-y-2">
              <FileText className="w-8 h-8 text-slate-300 mx-auto" strokeWidth={1.5} />
              <p className="text-xs font-bold text-slate-600">داده‌ای ثبت نشده</p>
              <p className="text-[10px] text-slate-400 leading-relaxed">در این بازه هیچ سند حسابداری ثبت نشده است.</p>
            </div>
          )}
          {entries.map((entry) => (
            <div key={entry.id} className="p-2.5 bg-slate-50 rounded-xl space-y-1 border border-slate-200/80">
              <div className="flex justify-between font-bold text-slate-800 text-[11px] gap-2">
                <span className="text-right">{entry.reason}</span>
                <span className="font-mono text-slate-400 text-[10px] shrink-0">{entry.id}</span>
              </div>
              <div className="text-[10px] text-slate-500">
                <span>حساب: <strong className="font-mono">{entry.targetAccount}</strong></span>
                <span className="mr-2">تاریخ: {entry.date ? new Date(entry.date).toLocaleDateString('fa-IR') : '—'}</span>
              </div>
              <div className="flex justify-between text-[10px] text-slate-500 pt-1 border-t border-slate-200">
                <span>بدهکار: <strong className="font-mono text-slate-700">{entry.type === 'debit' ? formatPrice(entry.amount) + ' ت' : '-'}</strong></span>
                <span>بستانکار: <strong className="font-mono text-emerald-700">{entry.type === 'credit' ? formatPrice(entry.amount) + ' ت' : '-'}</strong></span>
              </div>
            </div>
          ))}
          {!error && nextCursor && (
            <button type="button" disabled={loading} onClick={() => load({ offset, cursor: nextCursor })} className="w-full py-2 bg-slate-100 rounded-lg disabled:opacity-50">
              {loading ? 'در حال بارگذاری…' : 'بارگذاری اسناد بیشتر'}
            </button>
          )}
        </div>

        <div className="flex justify-between border-t border-slate-100 pt-2">
          <button type="button" onClick={() => load({ offset: offset + 1 })} className="text-xs text-teal-700">بازهٔ قدیمی‌تر</button>
          {offset > 0 && (
            <button type="button" onClick={() => load({ offset: offset - 1 })} className="text-xs text-teal-700">بازهٔ جدیدتر</button>
          )}
        </div>

        <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
          <button
            onClick={() => window.print()}
            className="py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl flex items-center justify-center gap-1"
          >
            <Printer className="w-3.5 h-3.5" strokeWidth={1.5} />
            <span>چاپ اسناد نمایش‌داده‌شده</span>
          </button>
          <button
            onClick={onClose}
            className="py-2 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl"
          >
            بستن
          </button>
        </div>
      </motion.div>
    </div>
  );
};
