import { useId, useRef, useState, type FormEvent } from 'react';
import { CheckCircle2, Send } from 'lucide-react';
import { supportApi, type SupportCategory, type SupportReferenceInput, type SupportRole, type SupportStatus } from '../../services/supportApi';
import { newUuid } from '../../services/uuid';

export const supportCategoryLabels: Record<SupportCategory, string> = {
  general: 'عمومی',
  order: 'سفارش',
  shipment: 'ارسال و محموله',
  return: 'مرجوعی',
  financial: 'مالی',
};

export const supportStatusLabels: Record<SupportStatus, string> = {
  open: 'در انتظار پاسخ',
  assigned: 'در حال بررسی',
  pending: 'در انتظار پیگیری',
  resolved: 'حل‌شده',
  closed: 'بسته‌شده',
};

export const supportInputClass = 'w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand';
export const supportPrimaryButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-brand focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60';

interface Props {
  role: SupportRole;
  context: SupportReferenceInput;
  initialSubject?: string;
  onCreated?: (id: string) => void;
  onBusyChange?: (busy: boolean) => void;
}

export function SupportTicketForm({ role, context, initialSubject = '', onCreated, onBusyChange }: Props) {
  const id = useId();
  const [subject, setSubject] = useState(initialSubject);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [createdId, setCreatedId] = useState('');
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const requestKey = useRef<string | null>(null);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting.current || createdId || !subject.trim() || !message.trim()) return;
    submitting.current = true;
    setBusy(true);
    onBusyChange?.(true);
    setError('');
    try {
      requestKey.current ??= newUuid();
      const result = await supportApi.create(role, { ...context, subject: subject.trim(), message: message.trim() }, requestKey.current);
      setCreatedId(result.id);
      onCreated?.(result.id);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'ثبت پیام انجام نشد.');
    } finally {
      submitting.current = false;
      setBusy(false);
      onBusyChange?.(false);
    }
  };

  if (createdId) {
    return (
      <div className="space-y-3">
        <div role="status" className="flex items-start gap-2 rounded-xl border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-900">
          <CheckCircle2 aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
          <div>
            <p className="font-bold">درخواست ثبت شد</p>
            <p className="mt-1 leading-6">پاسخ را در بخش پشتیبانی پنل ببین.</p>
            <bdi dir="ltr" className="mt-1 block break-all font-mono text-xs">{createdId}</bdi>
          </div>
        </div>
        {context.kind === 'general' && (
          <button type="button" className={supportPrimaryButtonClass} onClick={() => { requestKey.current = null; setCreatedId(''); setSubject(''); setMessage(''); }}>
            پیام تازه به پشتیبانی
          </button>
        )}
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="space-y-3">
      <div>
        <label htmlFor={`${id}-subject`} className="mb-1 block text-xs font-bold text-slate-700">موضوع پیام</label>
        <input id={`${id}-subject`} value={subject} maxLength={200} disabled={busy} onChange={event => setSubject(event.target.value)}
          className={supportInputClass} placeholder="مثلاً پیگیری وضعیت سفارش" required />
      </div>
      <div>
        <label htmlFor={`${id}-message`} className="mb-1 block text-xs font-bold text-slate-700">شرح درخواست</label>
        <textarea id={`${id}-message`} value={message} maxLength={10000} disabled={busy} onChange={event => setMessage(event.target.value)}
          className={supportInputClass} rows={4} placeholder="بگو چه اتفاقی افتاده و چه کمکی لازم داری…" required />
      </div>
      {error && <p role="alert" className="text-sm text-rose-700">{error}</p>}
      <button type="submit" disabled={busy || !subject.trim() || !message.trim()} className={supportPrimaryButtonClass}>
        <Send aria-hidden="true" className="size-4" strokeWidth={1.75} />
        {busy ? 'در حال فرستادن…' : 'فرستادن پیام'}
      </button>
    </form>
  );
}
