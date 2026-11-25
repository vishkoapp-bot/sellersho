import type { ReactNode } from 'react';

export function SupplierSectionHeading({ title, description, icon }: {
  title: string;
  description: string;
  icon: ReactNode;
}) {
  return (
    <section className="flex flex-wrap items-center justify-between gap-4 rounded-[24px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
      <div className="flex min-w-0 items-start gap-3">
        <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-2xl bg-[#eee7fa] text-[#4c347e] [&>svg]:size-5">{icon}</span>
        <div className="min-w-0">
          <h2 className="text-lg font-extrabold leading-7 text-[#261b41] sm:text-xl">{title}</h2>
          <p className="mt-1 text-sm leading-6 text-slate-600">{description}</p>
        </div>
      </div>
    </section>
  );
}
