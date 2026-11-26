import React from 'react';
import { ChevronLeft, ListChecks, type LucideIcon } from 'lucide-react';

export interface PanelWorkItem {
  label: string;
  action?: string;
  count: number | undefined;
  icon: LucideIcon;
  urgent?: boolean;
  href?: string;
  open: () => void;
}

const digits = new Intl.NumberFormat('fa-IR');

export function PanelWorkQueue({ items, loading, error, retry }: {
  items: PanelWorkItem[];
  loading: boolean;
  error?: string;
  retry: () => void;
}) {
  return (
    <section aria-label="کارهای امروز" className="rounded-[22px] border border-slate-200 bg-white p-4 shadow-sm sm:p-5">
      <h2 className="flex items-center gap-2 text-base font-extrabold text-slate-900">
        <ListChecks aria-hidden="true" className="size-5 text-brand-deep" /> کارهای امروز
      </h2>
      {loading && <p role="status" className="mt-2 text-xs text-slate-600">در حال دریافت تعداد کارها…</p>}
      {error && <div className="mt-2 flex flex-wrap items-center gap-2 text-sm text-rose-800"><p role="alert">{error}</p><button type="button" className="min-h-11 rounded-lg px-2 font-bold underline" onClick={retry}>تلاش دوباره</button></div>}
      <ul className="mt-3 grid gap-2 sm:grid-cols-2">
        {items.map(({ label, action, count, icon: Icon, urgent, href, open }) => {
          const className = `group flex h-full w-full items-center gap-2 rounded-xl border border-slate-200 p-2.5 text-start hover:border-brand-border hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand ${action ? 'min-h-16' : 'min-h-12'}`;
          const contents = <>
              <span className={`grid size-8 shrink-0 place-items-center rounded-lg ${urgent && count ? 'bg-rose-100 text-rose-800' : 'bg-brand-soft text-brand-deep'}`}><Icon aria-hidden="true" className="size-4" /></span>
              <span className="min-w-0 flex-1"><span className="block text-sm font-bold text-slate-900">{label}</span>{action && <span className="block text-xs leading-5 text-slate-600">{action}</span>}</span>
              <span className="shrink-0 font-extrabold tabular-nums text-slate-900" aria-label={count === undefined ? 'تعداد در دسترس نیست' : undefined}>{count === undefined ? '—' : digits.format(count)}</span>
              <ChevronLeft aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-hover:text-brand-deep" />
          </>;
          return (
            <li key={label} className="min-w-0">
              {href ? <a className={className} href={href} onClick={event => {
                if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                event.preventDefault();
                open();
              }}>{contents}</a> : <button type="button" onClick={open} className={className}>{contents}</button>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
