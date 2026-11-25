import { useEffect, useId, useState, type FormEvent } from 'react';
import { Headphones, Send } from 'lucide-react';
import { supportApi, type StaffSupportTicket, type SupportStatus } from '../../services/supportApi';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { supportCategoryLabels, supportInputClass, supportPrimaryButtonClass, supportStatusLabels } from '../support/SupportTicketForm';

const staffRoleLabels: Record<string, string> = { customer: 'مشتری', seller: 'فروشنده', supplier: 'تأمین‌کننده' };

export function SupportTicketsTab() {
  const [tickets, setTickets] = useState<StaffSupportTicket[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [activeId, setActiveId] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError('');
    void supportApi.staffList(controller.signal).then(page => {
      if (!controller.signal.aborted) setTickets(page.tickets);
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'دریافت تیکت‌ها انجام نشد.');
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return () => controller.abort();
  }, [refresh]);

  const active = tickets.find(ticket => ticket.id === activeId);
  return (
    <section dir="rtl" className="space-y-3 pb-6">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-slate-200 bg-white p-4">
        <h3 className="flex items-center gap-2 text-sm font-extrabold text-slate-900"><Headphones aria-hidden="true" className="size-5 text-brand-deep" /> تیکت‌های پشتیبانی</h3>
        <button type="button" onClick={() => setRefresh(value => value + 1)} disabled={loading} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">تازه‌سازی فهرست</button>
      </div>
      {loading && <p role="status" className="text-sm text-slate-600">در حال دریافت تیکت‌ها…</p>}
      {error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
      {!loading && !error && tickets.length === 0 && <p className="rounded-xl border border-dashed border-slate-200 bg-white p-4 text-sm text-slate-600">تیکتی برای نمایش وجود ندارد.</p>}
      <div className="space-y-2.5">
        {tickets.map(ticket => (
          <article key={ticket.id} className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4 shadow-2xs">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <h4 className="min-w-0 break-words text-sm font-bold text-slate-900">{ticket.subject}</h4>
              <span className="rounded-full bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">{supportStatusLabels[ticket.status]}</span>
            </div>
            <p className="flex flex-wrap items-center gap-2 text-xs text-slate-600">
              <span>{staffRoleLabels[ticket.requesterRole] ?? ticket.requesterRole}</span>
              <span>{supportCategoryLabels[ticket.category] ?? supportCategoryLabels.general}</span>
              {ticket.referenceLabel && <bdi dir="auto" className="break-all font-mono">{ticket.referenceLabel}</bdi>}
              <span>{new Date(ticket.createdAt).toLocaleDateString('fa-IR')}</span>
            </p>
            <button type="button" onClick={() => setActiveId(ticket.id)} className="min-h-11 rounded-lg border border-brand-border px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">مشاهده گفت‌وگو</button>
          </article>
        ))}
      </div>
      {active && <StaffTicketDialog key={active.id} ticket={active} onClose={() => setActiveId('')}
        onStatusChange={status => setTickets(current => current.map(ticket => ticket.id === active.id ? { ...ticket, status } : ticket))} />}
    </section>
  );
}

function StaffTicketDialog({ ticket, onClose, onStatusChange }: { ticket: StaffSupportTicket; onClose: () => void; onStatusChange: (status: SupportStatus) => void }) {
  const titleId = useId();
  const [thread, setThread] = useState<Awaited<ReturnType<typeof supportApi.staffThread>> | null>(null);
  const [status, setStatus] = useState(ticket.status);
  const [body, setBody] = useState('');
  const [internal, setInternal] = useState(false);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState('');
  const [error, setError] = useState('');
  const [refresh, setRefresh] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setError('');
    void supportApi.staffThread(ticket.id, controller.signal).then(result => {
      if (controller.signal.aborted) return;
      setThread(result);
      setStatus(result.status);
    }).catch((err: unknown) => {
      if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'دریافت گفت‌وگو انجام نشد.');
    });
    return () => controller.abort();
  }, [ticket.id, refresh]);

  const reply = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || !body.trim()) return;
    setBusy(true);
    setError('');
    setFeedback('');
    try {
      const result = await supportApi.staffReply(ticket.id, body.trim(), internal);
      onStatusChange(result.status);
      setBody('');
      setFeedback(internal ? 'یادداشت ثبت شد.' : 'پاسخ فرستاده شد.');
      setRefresh(value => value + 1);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'ثبت پاسخ انجام نشد.');
    } finally {
      setBusy(false);
    }
  };

  const changeStatus = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || status === thread?.status) return;
    setBusy(true);
    setError('');
    setFeedback('');
    try {
      const result = await supportApi.staffSetStatus(ticket.id, status);
      onStatusChange(result.status);
      setThread(current => current && { ...current, status: result.status });
      setFeedback('وضعیت به‌روز شد.');
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'تغییر وضعیت انجام نشد.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <SupplierDialog titleId={titleId} onDismiss={busy ? undefined : onClose} wide>
      <SupplierDialogHeading id={titleId} title={ticket.subject} onClose={onClose} busy={busy} />
      <dl className="mb-3 grid gap-2 rounded-xl bg-brand-soft p-3 text-xs leading-6 text-brand-deep sm:grid-cols-2">
        <div><dt className="font-bold">شناسه تیکت</dt><dd><bdi dir="ltr" className="break-all font-mono">{ticket.id}</bdi></dd></div>
        {ticket.requesterAccountId && <div><dt className="font-bold">حساب درخواست‌دهنده</dt><dd><bdi dir="ltr" className="break-all font-mono">{ticket.requesterAccountId}</bdi></dd></div>}
        {ticket.referenceId && <div><dt className="font-bold">شناسه {supportCategoryLabels[ticket.category]}</dt><dd><bdi dir="ltr" className="break-all font-mono">{ticket.referenceId}</bdi></dd></div>}
        {ticket.relatedOrderNumber && <div><dt className="font-bold">سفارش مرتبط</dt><dd><bdi dir="auto" className="break-all font-mono">{ticket.relatedOrderNumber}</bdi></dd></div>}
      </dl>
      {feedback && <p role="status" className="mb-3 rounded-xl bg-emerald-50 p-3 text-sm text-emerald-800">{feedback}</p>}
      {error && <div className="mb-3 rounded-xl bg-rose-50 p-3 text-sm text-rose-800"><p role="alert">{error}</p><button type="button" className="mt-2 min-h-11 font-bold" onClick={() => setRefresh(value => value + 1)}>تازه‌سازی گفت‌وگو</button></div>}
      {!thread && !error && <p role="status" className="text-sm text-slate-600">در حال دریافت گفت‌وگو…</p>}
      {thread && (
        <div className="space-y-4">
          {thread.messages.length === 0 && <p className="text-sm text-slate-600">پیامی برای این تیکت ثبت نشده است.</p>}
          {thread.messages.map(message => (
            <article key={message.id} className={`rounded-xl border p-3 text-xs ${message.isInternal ? 'border-amber-100 bg-amber-50' : 'border-slate-100 bg-slate-50'}`}>
              <div className="flex flex-wrap items-center justify-between gap-2 text-slate-600">
                <b>{message.isInternal ? 'یادداشت داخلی' : message.isRequester ? 'درخواست‌دهنده' : 'پشتیبانی'}</b>
                <time dateTime={message.createdAt}>{new Date(message.createdAt).toLocaleString('fa-IR')}</time>
              </div>
              <p className="mt-2 whitespace-pre-wrap break-words text-sm leading-7 text-slate-900">{message.body}</p>
            </article>
          ))}
          <form onSubmit={reply} className="space-y-2 border-t border-slate-200 pt-3">
            <label htmlFor={`${titleId}-reply`} className="text-xs font-bold text-slate-700">پاسخ به تیکت</label>
            <textarea id={`${titleId}-reply`} rows={3} maxLength={10000} value={body} onChange={event => setBody(event.target.value)} className={supportInputClass} required />
            <div className="flex flex-wrap items-center justify-between gap-2">
              <label className="flex min-h-11 items-center gap-2 text-xs text-slate-700"><input type="checkbox" checked={internal} onChange={event => setInternal(event.target.checked)} /> یادداشت داخلی</label>
              <button type="submit" disabled={busy || !body.trim()} className={supportPrimaryButtonClass}><Send aria-hidden="true" className="size-4" /> {internal ? 'ثبت یادداشت' : 'فرستادن پاسخ'}</button>
            </div>
          </form>
          <form onSubmit={changeStatus} className="flex flex-wrap items-end gap-2 border-t border-slate-200 pt-3">
            <div className="min-w-36 flex-1">
              <label htmlFor={`${titleId}-status`} className="mb-1 block text-xs font-bold text-slate-700">وضعیت تیکت</label>
              <select id={`${titleId}-status`} className={supportInputClass} value={status} onChange={event => setStatus(event.target.value as SupportStatus)}>
                {Object.entries(supportStatusLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
              </select>
            </div>
            <button type="submit" className={supportPrimaryButtonClass} disabled={busy || status === thread.status}>ثبت وضعیت</button>
          </form>
        </div>
      )}
    </SupplierDialog>
  );
}
