import React, { useEffect, useId, useRef, useState } from 'react';

export function RetiredPrintedCodesPanel({ codes, onAcknowledge }: { codes: string[]; onAcknowledge: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const [copyStatus, setCopyStatus] = useState('');
  useEffect(() => {
    const panel = dialog.current;
    panel?.showModal();
    return () => panel?.close();
  }, []);

  const copyAll = async () => {
    if (typeof document === 'undefined' || typeof navigator === 'undefined') return;
    const text = codes.join('\n');
    try {
      await navigator.clipboard.writeText(text);
      setCopyStatus('کدها کپی شدند.');
      return;
    } catch {
      // Plain HTTP and denied clipboard permissions can still use selection-based copying.
    }
    const previousFocus = document.activeElement;
    const input = document.createElement('textarea');
    input.value = text;
    input.readOnly = true;
    input.className = 'fixed opacity-0 pointer-events-none';
    dialog.current?.appendChild(input);
    try {
      input.select();
      setCopyStatus(document.execCommand('copy') ? 'کدها کپی شدند.' : 'کپی کدها انجام نشد.');
    } catch {
      setCopyStatus('کپی کدها انجام نشد.');
    } finally {
      input.remove();
      if (previousFocus instanceof HTMLElement) previousFocus.focus();
    }
  };

  return (
    <dialog ref={dialog} onCancel={event => event.preventDefault()} aria-labelledby={titleId} dir="rtl"
      className="m-auto w-[calc(100%-2rem)] max-w-lg space-y-3 rounded-2xl bg-white p-4 text-slate-900 backdrop:bg-slate-950/60">
      <h3 id={titleId} className="text-sm font-bold text-amber-800">این برچسب‌های چاپ‌شده دیگر معتبر نیستند — آن‌ها را از قفسه/کارتن جدا و معدوم کنید:</h3>
      <ul className="max-h-64 space-y-1 overflow-auto rounded-lg bg-amber-50 p-3">
        {codes.map((code, index) => <li key={index} dir="ltr" className="font-mono break-all text-xs">{code}</li>)}
      </ul>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <button type="button" onClick={copyAll} className="rounded-lg border px-3 py-2 font-bold">کپی همه کدها</button>
        <button type="button" onClick={onAcknowledge} className="rounded-lg bg-brand-deep px-3 py-2 font-bold text-white">متوجه شدم</button>
        <span role="status">{copyStatus}</span>
      </div>
    </dialog>
  );
}
