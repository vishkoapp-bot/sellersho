import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';

export function SupplierDialog({ titleId, descriptionId, onDismiss, wide, children }: {
  titleId: string;
  descriptionId?: string;
  onDismiss?: () => void;
  wide?: boolean;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useLayoutEffect(() => {
    const dialog = ref.current;
    const opener = document.activeElement;
    dialog?.showModal();
    return () => {
      dialog?.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus({ preventScroll: true });
    };
  }, []);

  return (
    <dialog ref={ref} aria-labelledby={titleId} aria-describedby={descriptionId} dir="rtl"
      onCancel={event => { event.preventDefault(); event.stopPropagation(); onDismiss?.(); }}
      className={`m-auto max-h-[calc(100dvh-2rem)] w-[calc(100%_-_2rem)] overflow-y-auto overscroll-contain rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-slate-900 shadow-2xl backdrop:bg-slate-950/55 sm:p-5 ${wide ? 'max-w-2xl' : 'max-w-md'}`}>
      {children}
    </dialog>
  );
}

export function SupplierDialogHeading({ id, title, detail, onClose, busy }: {
  id: string;
  title: string;
  detail?: string;
  onClose: () => void;
  busy?: boolean;
}) {
  return (
    <div className="mb-4 flex items-start justify-between gap-3 border-b border-[#e6e2f0] pb-3">
      <div className="min-w-0">
        <h3 id={id} className="text-base font-extrabold leading-7 text-[#261b41]">{title}</h3>
        {detail && <p className="break-words text-sm leading-6 text-slate-600">{detail}</p>}
      </div>
      <button type="button" aria-label={`بستن ${title}`} disabled={busy} onClick={onClose} className="grid size-11 shrink-0 place-items-center rounded-xl border border-slate-200 text-slate-700 hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
        <X aria-hidden="true" className="size-5" strokeWidth={1.75} />
      </button>
    </div>
  );
}
