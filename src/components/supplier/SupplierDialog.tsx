import { useLayoutEffect, useRef, type ReactNode } from 'react';

export function SupplierDialog({ titleId, descriptionId, onDismiss, children }: {
  titleId: string;
  descriptionId?: string;
  onDismiss?: () => void;
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
      onCancel={event => { event.preventDefault(); onDismiss?.(); }}
      className="m-auto max-h-[calc(100dvh-2rem)] w-[calc(100%_-_2rem)] max-w-md overflow-y-auto overscroll-contain rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-slate-900 shadow-2xl backdrop:bg-slate-950/55 sm:p-5">
      {children}
    </dialog>
  );
}
