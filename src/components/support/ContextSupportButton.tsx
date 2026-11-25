import { useEffect, useId, useState } from 'react';
import { Headphones } from 'lucide-react';
import { supportApi, type SupportReference, type SupportReferenceInput, type SupportRole } from '../../services/supportApi';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { SupportTicketForm, supportCategoryLabels } from './SupportTicketForm';

interface Props extends Omit<SupportReferenceInput, 'kind' | 'referenceId'> {
  role: SupportRole;
  kind: 'order' | 'shipment' | 'return' | 'financial';
  referenceId: string;
  className?: string;
}

const actionLabels: Record<Props['kind'], string> = {
  order: 'پشتیبانی سفارش',
  shipment: 'پشتیبانی ارسال',
  return: 'پشتیبانی مرجوعی',
  financial: 'پشتیبانی مالی',
};

export function ContextSupportButton({ role, kind, referenceId, referenceType, referenceCode, storeId, className }: Props) {
  const [open, setOpen] = useState(false);
  if (!referenceId) return null;
  return (
    <>
      <button type="button" onClick={event => { event.stopPropagation(); setOpen(true); }} className={className ?? 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-lg border border-brand-border px-2.5 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand'}>
        <Headphones aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
        {actionLabels[kind]}
      </button>
      {open && <ContextSupportDialog key={JSON.stringify([role, kind, referenceId, referenceType, referenceCode, storeId])}
        role={role} kind={kind} referenceId={referenceId} referenceType={referenceType} referenceCode={referenceCode} storeId={storeId} onClose={() => setOpen(false)} />}
    </>
  );
}

function ContextSupportDialog({ role, kind, referenceId, referenceType, referenceCode, storeId, onClose }: Omit<Props, 'className'> & { onClose: () => void }) {
  const titleId = useId();
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<SupportReference | null>(null);
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setPreview(null);
    setError('');
    void supportApi.preview(role, { kind, referenceId, referenceType, referenceCode, storeId }, controller.signal)
      .then(result => { if (!controller.signal.aborted) setPreview(result); })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setError(err instanceof Error ? err.message : 'دریافت اطلاعات انجام نشد.');
      });
    return () => controller.abort();
  }, [role, kind, referenceId, referenceType, referenceCode, storeId, attempt]);

  const label = preview?.referenceLabel;
  return (
    <SupplierDialog titleId={titleId} onDismiss={busy ? undefined : onClose}>
      <SupplierDialogHeading id={titleId} title={actionLabels[kind]} onClose={onClose} busy={busy} />
      {label ? (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-brand-border bg-brand-soft p-3 text-sm text-brand-deep">
            <span className="font-bold">{supportCategoryLabels[kind]}</span>
            <bdi dir="auto" className="break-all font-mono text-xs">{label}</bdi>
          </div>
          <SupportTicketForm role={role} context={{ kind, referenceId, referenceType, referenceCode, storeId }}
            initialSubject={`پیگیری ${supportCategoryLabels[kind]} - ${label}`.slice(0, 200)} onBusyChange={setBusy} />
        </div>
      ) : error ? (
        <div className="space-y-2.5">
          <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>
          <button type="button" className="min-h-11 rounded-xl border border-slate-200 px-3 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand" onClick={() => setAttempt(current => current + 1)}>تلاش دوباره</button>
        </div>
      ) : <p role="status" className="py-3 text-sm text-slate-600">در حال دریافت اطلاعات…</p>}
    </SupplierDialog>
  );
}
