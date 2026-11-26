import type { ReactNode } from 'react';

export function SupplierSectionHeading({ title, description, icon, action }: {
  title: string;
  description: string;
  icon: ReactNode;
  action?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 px-0.5 pb-3 pt-1 sm:pb-4">
      <div className="flex min-w-0 items-start gap-2.5">
        <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep [&>svg]:size-[18px]">{icon}</span>
        <div className="min-w-0">
          <h2 className="text-base font-extrabold leading-7 text-slate-900 sm:text-lg">{title}</h2>
          {description && <p className="text-sm leading-6 text-slate-600">{description}</p>}
        </div>
      </div>
      {action}
    </header>
  );
}
