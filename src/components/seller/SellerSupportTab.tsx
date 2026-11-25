import { useCallback, useEffect, useRef, useState } from 'react';
import { Headphones, MessageSquare } from 'lucide-react';
import { supportApi, type SupportRole, type SupportTicket } from '../../services/supportApi';
import { SupportTicketForm, supportCategoryLabels, supportStatusLabels } from '../support/SupportTicketForm';

export function SellerSupportTab({ role = 'seller', storeId }: { role?: SupportRole; storeId?: string }) {
  const [tickets, setTickets] = useState<SupportTicket[]>([]);
  const [cursor, setCursor] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);

  const load = useCallback(async (next = '') => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    setError('');
    try {
      const page = await supportApi.list(role, next, controller.signal);
      if (controller.signal.aborted) return;
      setTickets(current => next ? [...current, ...page.tickets] : page.tickets);
      setCursor(page.hasMore ? page.nextCursor : '');
    } catch (err: unknown) {
      if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'دریافت پیام‌ها انجام نشد.');
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [role]);

  useEffect(() => {
    setTickets([]);
    setCursor('');
    void load();
    return () => request.current?.abort();
  }, [load]);

  return (
    <div dir="rtl" className="space-y-4 pb-6">
      <section aria-label="پیام تازه به پشتیبانی" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-2xs sm:p-5">
        <div className="flex items-center gap-3">
          <div className="grid size-10 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep">
            <Headphones aria-hidden="true" className="size-5" strokeWidth={1.75} />
          </div>
          <div>
            <h3 className="text-base font-extrabold text-slate-900">پیام تازه به پشتیبانی</h3>
            <p className="mt-0.5 text-xs leading-5 text-slate-600">سؤال یا مشکل را بنویس و پاسخ را در همین بخش دنبال کن.</p>
          </div>
        </div>
        <SupportTicketForm key={`${role}:${storeId ?? ''}`} role={role} context={{ kind: 'general', storeId }} onCreated={() => void load()} />
      </section>

      <section aria-label="پیام‌های پشتیبانی" className="space-y-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2 px-1">
          <h4 className="text-sm font-extrabold text-slate-900">پیام‌های پشتیبانی</h4>
          <button type="button" onClick={() => void load()} disabled={loading} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">تازه‌سازی پیام‌ها</button>
        </div>
        {loading && <p role="status" className="text-xs text-slate-600">در حال دریافت پیام‌ها…</p>}
        {error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-xs text-rose-800">{error}</p>}
        {!loading && !error && tickets.length === 0 && (
          <p className="rounded-xl border border-dashed border-slate-200 bg-white p-4 text-sm leading-6 text-slate-600">هنوز پیامی برای پشتیبانی نفرستاده‌ای.</p>
        )}
        {tickets.map(ticket => (
          <details key={ticket.id} className="rounded-2xl border border-slate-200 bg-white shadow-2xs">
            <summary className="cursor-pointer list-none rounded-xl p-3.5 focus-visible:ring-2 focus-visible:ring-brand">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-start gap-2 text-sm font-bold text-slate-900"><MessageSquare aria-hidden="true" className="mt-1 size-4 shrink-0 text-brand-deep" /> <span className="break-words">{ticket.subject}</span></div>
                  <p className="mt-1 flex flex-wrap items-center gap-x-2 ps-6 text-xs text-slate-600">
                    <span>{supportCategoryLabels[ticket.category] ?? supportCategoryLabels.general}</span>
                    {ticket.referenceLabel && <bdi dir="auto" className="break-all font-mono">{ticket.referenceLabel}</bdi>}
                    <span>{new Date(ticket.createdAt).toLocaleDateString('fa-IR')}</span>
                  </p>
                </div>
                <span className={`rounded-full px-2.5 py-1 text-[11px] font-bold ${ticket.status === 'resolved' ? 'bg-emerald-50 text-emerald-800' : ticket.status === 'open' ? 'bg-amber-50 text-amber-800' : 'bg-brand-soft text-brand-deep'}`}>
                  {supportStatusLabels[ticket.status] ?? 'در انتظار پاسخ'}
                </span>
              </div>
            </summary>
            <div className="space-y-2 border-t border-slate-100 p-3.5">
              {(ticket.messageTotal ?? 0) > ticket.messages.length && <p className="text-xs text-slate-600">آخرین پیام‌های این گفت‌وگو</p>}
              {ticket.messages.map((message, index) => (
                <p key={index} className="whitespace-pre-wrap break-words rounded-xl bg-slate-50 p-3 text-sm leading-7 text-slate-800">{message}</p>
              ))}
              <bdi dir="ltr" className="block break-all font-mono text-[10px] text-slate-500">{ticket.id}</bdi>
            </div>
          </details>
        ))}
        {cursor && <button type="button" onClick={() => void load(cursor)} disabled={loading} className="min-h-11 rounded-xl border border-brand-border px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">پیام‌های قدیمی‌تر</button>}
      </section>
    </div>
  );
}
