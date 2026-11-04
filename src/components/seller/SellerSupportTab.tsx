import React, { useEffect, useState } from 'react';
import { TicketConversationModal } from '../common/TicketConversationModal';
import { Headphones, Send, CheckCircle2, Info } from 'lucide-react';
import { storeApi, SellerTicket } from '../../services/storeApi';

// Live parity (SellerDashboardView-MERGED.js, support tab Dn, offsets
// ~229900-234000): live always fetches via getSellerTickets (no demo
// tickets), maps every status to a label (answered / closed / open), and
// uses the informal second-person copy below verbatim. The previous
// candidate version hardcoded "پاسخ داده شد" for every ticket, showed
// invented demo tickets in mock mode, and promised a "۲ ساعته" reply
// that live never promises — all three are removed here.
const statusLabel = (status: string): { label: string; cls: string } => {
  switch (status) {
    case 'answered':
      return { label: 'پاسخ داده شد', cls: 'text-emerald-700 bg-emerald-50' };
    case 'closed':
      return { label: 'بسته شد', cls: 'text-slate-500 bg-slate-100' };
    case 'open':
    default:
      return { label: 'در انتظار پاسخ', cls: 'text-amber-700 bg-amber-50' };
  }
};

export const SellerSupportTab: React.FC = () => {
  const [subject, setSubject] = useState('');
  const [message, setMessage] = useState('');
  const [sent, setSent] = useState(false);
  const [tickets, setTickets] = useState<SellerTicket[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeTicket, setActiveTicket] = useState<any | null>(null);

  useEffect(() => {
    setLoading(true);
    storeApi.getSellerTickets().then(setTickets).catch((err) => setError(err.message || 'خطا در دریافت تیکت‌ها')).finally(() => setLoading(false));
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!subject.trim() || !message.trim()) return;
    try {
      await storeApi.createSellerTicket(subject.trim(), message.trim());
      setTickets(await storeApi.getSellerTickets());
    } catch (err: any) {
      setError(err.message || 'ثبت تیکت انجام نشد');
      return;
    }
    setSent(true);
    setTimeout(() => {
      setSent(false);
      setSubject('');
      setMessage('');
    }, 2500);
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="rounded-2xl border border-brand-border bg-brand-soft/50 px-3.5 py-2.5">
        <div className="flex items-start gap-2">
          <Info className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} />
          <p className="flex-1 text-xs leading-6 text-slate-700">سؤال یا مشکلی داری؟ این‌جا برای پشتیبانی پلتفرم بنویس؛ جواب را هم همین‌جا می‌بینی.</p>
        </div>
      </div>

      {/* 1. New Ticket Form */}
      <form onSubmit={handleSubmit} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-brand-deep">
          <Headphones className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-800">پیام تازه به پشتیبانی</h3>
        </div>

        <div>
          <label className="text-xs font-bold text-slate-700 block mb-1">موضوع پیام</label>
          <input
            type="text"
            placeholder="مثلاً: پولم کی واریز می‌شود؟ یا: سفارشم چه شد؟"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            required
          />
        </div>

        <div>
          <label className="text-xs font-bold text-slate-700 block mb-1">پیامت را بنویس</label>
          <textarea
            rows={3}
            placeholder="مشکلت را ساده و کامل بنویس…"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            required
          />
        </div>

        <button
          type="submit"
          className="w-full py-3 min-h-[48px] bg-brand hover:bg-brand-deep text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5"
        >
          {sent ? (
            <>
              <CheckCircle2 className="w-4 h-4" strokeWidth={1.5} />
              <span>پیامت فرستاده شد؛ جواب را همین‌جا می‌بینی</span>
            </>
          ) : (
            <>
              <Send className="w-3.5 h-3.5" strokeWidth={1.5} />
              <span>فرستادن پیام</span>
            </>
          )}
        </button>
      </form>

      {/* 2. Previous Tickets */}
      <div className="space-y-2">
        <h4 className="text-xs font-bold text-slate-800">پیام‌های قبلی تو</h4>

        {loading && <p className="text-xs text-slate-500">در حال دریافت…</p>}
        {error && <p className="text-xs text-rose-600">{error}</p>}
        {!loading && tickets.length === 0 && <p className="text-xs text-slate-500 leading-6">هنوز پیامی نفرستاده‌ای. سؤالت را در کادر بالای همین صفحه بنویس و بفرست؛ جواب پشتیبانی این‌جا می‌آید.</p>}
        {tickets.map((t) => {
          const status = statusLabel(t.status);
          return (
            <div
              key={t.id}
              onClick={() => setActiveTicket(t)}
              className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2 cursor-pointer hover:border-brand transition-colors"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <span className="text-xs font-bold text-slate-800 hover:text-brand-deep">{t.subject}</span>
                <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${status.cls}`}>
                  {status.label}
                </span>
              </div>

              <div className="bg-slate-50 rounded-xl p-2.5 text-[11px] text-slate-600">
                <span className="text-slate-400 block text-[10px] mb-1">جواب پشتیبانی:</span>
                <p className="leading-relaxed">{t.messages?.join('\n') || 'در انتظار پاسخ پشتیبانی'}</p>
              </div>

              <div className="flex justify-between text-[10px] text-slate-400">
                <span className="font-mono">{t.id}</span>
                <span>{new Date(t.createdAt).toLocaleDateString('fa-IR')}</span>
              </div>
            </div>
          );
        })}
      </div>

      <TicketConversationModal
        isOpen={!!activeTicket}
        ticket={activeTicket}
        onClose={() => setActiveTicket(null)}
      />
    </div>
  );
};
