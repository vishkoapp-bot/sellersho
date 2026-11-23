import React, { useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { HelpCircle } from 'lucide-react';

export const HELP_POPOVER_Z = 10001;
export const HELP_SHEET_BACKDROP_Z = 10000;
export const HELP_OPEN_EVENT = 'help-explainer:open';

export type HelpExplainerProps = {
  text: string;
  title?: string;
  placement?: 'auto' | 'below' | 'above';
  size?: 'sm' | 'md';
  variant?: 'auto' | 'popover' | 'sheet';
  sections?: Array<{ heading?: string; body: string }>;
  ariaLabel?: string;
};

export function resolveHelpPresentation({ text, size, variant = 'auto', sections = [] }: HelpExplainerProps) {
  const length = text.length + sections.reduce((total, section) => total + (section.heading?.length ?? 0) + section.body.length, 0);
  return {
    size: size ?? (length <= 200 ? 'sm' : 'md'),
    variant: variant === 'auto'
      ? (length > 300 || /\n\s*\n|^\s*[-•] /m.test(text) ? 'sheet' : 'popover')
      : variant,
  } as const;
}

export function positionHelpPopover(
  trigger: { top: number; bottom: number; right: number; left: number },
  width: number, height: number, viewportWidth: number, viewportHeight: number,
  placement: HelpExplainerProps['placement'] = 'auto', rtl = true,
) {
  const below = viewportHeight - trigger.bottom - 4;
  const above = trigger.top - 4;
  const opensAbove = placement === 'above' || (placement === 'auto' && below < height && above > below);
  return {
    left: Math.max(8, Math.min(rtl ? trigger.right - width : trigger.left, viewportWidth - width - 8)),
    top: opensAbove ? Math.max(8, trigger.top - height - 4) : Math.max(8, Math.min(trigger.bottom + 4, viewportHeight - height - 8)),
  };
}

export function HelpExplainer({ text, title, placement = 'auto', size, variant = 'auto', sections, ariaLabel }: HelpExplainerProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 8, top: 8 });
  const [direction, setDirection] = useState<'rtl' | 'ltr'>('rtl');
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const closedByOther = useRef(false);
  const presentation = resolveHelpPresentation({ text, size, variant, sections });
  const sheet = presentation.variant === 'sheet';
  const label = ariaLabel ?? (title ? `توضیح: ${title}` : 'توضیح ساده');

  useEffect(() => {
    const onOtherOpen = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== id) {
        closedByOther.current = true;
        setOpen(false);
      }
    };
    document.addEventListener(HELP_OPEN_EVENT, onOtherOpen);
    return () => document.removeEventListener(HELP_OPEN_EVENT, onOtherOpen);
  }, [id]);

  useEffect(() => {
    if (!open) return;
    closedByOther.current = false;
    const trigger = triggerRef.current;
    const panel = panelRef.current;
    const rtl = !trigger || getComputedStyle(trigger).direction !== 'ltr';
    setDirection(rtl ? 'rtl' : 'ltr');
    const measure = () => {
      if (!sheet && trigger && panel) {
        const rect = panel.getBoundingClientRect();
        setPosition(positionHelpPopover(trigger.getBoundingClientRect(), rect.width, rect.height, window.innerWidth, window.innerHeight, placement, rtl));
      }
    };
    measure();
    const frame = requestAnimationFrame(measure);
    if (sheet) closeRef.current?.focus();
    const close = () => setOpen(false);
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!panel?.contains(target) && !trigger?.contains(target)) {
        event.preventDefault();
        close();
      }
    };
    const scroll = (event: Event) => {
      if (!(event.target instanceof Node) || !panel?.contains(event.target)) close();
    };
    const keydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        close();
        trigger?.focus();
      } else if (sheet && event.key === 'Tab') {
        // The sheet contains only prose and one close control.
        event.preventDefault();
        closeRef.current?.focus();
      }
    };
    const focusin = (event: FocusEvent) => {
      if (sheet && !panel?.contains(event.target as Node)) closeRef.current?.focus();
    };
    document.addEventListener('pointerdown', outside, true);
    document.addEventListener('keydown', keydown, true);
    document.addEventListener('scroll', scroll, true);
    document.addEventListener('focusin', focusin);
    window.addEventListener('resize', close);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener('pointerdown', outside, true);
      document.removeEventListener('keydown', keydown, true);
      document.removeEventListener('scroll', scroll, true);
      document.removeEventListener('focusin', focusin);
      window.removeEventListener('resize', close);
      const restoreFocus = !closedByOther.current;
      closedByOther.current = false;
      if (sheet && restoreFocus && triggerRef.current === trigger && trigger && document.contains(trigger)) trigger.focus();
    };
  }, [open, sheet, placement]);

  const lines = text.split('\n');
  const bulletLines = lines.filter(line => /^\s*[-•] /.test(line));
  const intro = lines.filter(line => !/^\s*[-•] /.test(line)).join('\n');

  const body = (
    <div className="text-xs leading-6 text-slate-600 font-normal">
      {bulletLines.length ? (
        <>
          {intro.trim() && <p className="whitespace-pre-line">{intro}</p>}
          <ul className="list-disc ps-4 space-y-2">{bulletLines.map((line, index) => <li key={index}>{line.replace(/^\s*[-•] /, '')}</li>)}</ul>
        </>
      ) : text ? <p className="whitespace-pre-line">{text}</p> : null}
      {sections?.map((section, index) => (
        <dl key={index} className="mt-2">
          {section.heading && <dt className="font-bold text-slate-800">{section.heading}</dt>}
          <dd className="whitespace-pre-line">{section.body}</dd>
        </dl>
      ))}
    </div>
  );

  return (
    <>
      <button
        ref={triggerRef} type="button" aria-expanded={open} aria-label={label}
        aria-controls={open ? id : undefined} aria-haspopup="dialog"
        className="relative inline-flex items-center justify-center w-6 h-6 shrink-0 rounded-full bg-brand-soft border border-brand-border text-brand-deep ms-1 focus-visible:outline-2 focus-visible:outline-offset-2 outline-brand-deep after:absolute after:-inset-[5px] after:rounded-full"
        onClick={(event) => {
          event.stopPropagation();
          if (!open) document.dispatchEvent(new CustomEvent(HELP_OPEN_EVENT, { detail: id }));
          setOpen(!open);
        }}
      >
        <HelpCircle className="w-3.5 h-3.5" strokeWidth={1.5} aria-hidden="true" />
      </button>
      {open && createPortal(
        <>
          {sheet && <div className="fixed inset-0 bg-black/40" style={{ zIndex: HELP_SHEET_BACKDROP_Z }} onClick={() => setOpen(false)} aria-hidden="true" />}
          <div
            ref={panelRef} id={id} role="dialog" aria-label={label} aria-modal={sheet || undefined} dir={direction}
            style={{ zIndex: HELP_POPOVER_Z, ...(!sheet ? position : {}) }}
            className={sheet
              ? 'fixed bottom-0 inset-x-0 mx-auto max-h-[80vh] overflow-y-auto rounded-t-2xl bg-white shadow-xl p-3 text-right sm:bottom-auto sm:top-1/2 sm:-translate-y-1/2 sm:max-w-lg sm:rounded-2xl'
              : `fixed ${presentation.size === 'sm' ? 'w-60' : 'w-72'} max-w-[calc(100vw-2rem)] max-h-[50vh] overflow-y-auto bg-white border border-slate-200 rounded-xl shadow-lg p-3 text-right`}
          >
            {sheet ? <>
              <div className="mx-auto mb-3 h-1 w-10 rounded-full bg-slate-300 sm:hidden" aria-hidden="true" />
              <div className="flex items-center justify-between gap-2 mb-1">
                {title && <h3 className="text-xs font-bold text-slate-800">{title}</h3>}
                <button ref={closeRef} type="button" aria-label="بستن" onClick={() => setOpen(false)} className="ms-auto w-8 h-8 text-slate-600 focus-visible:outline-2 outline-brand-deep">×</button>
              </div>
            </> : title && <h3 className="text-xs font-bold text-slate-800 mb-1">{title}</h3>}
            {body}
          </div>
        </>, document.body,
      )}
    </>
  );
}
