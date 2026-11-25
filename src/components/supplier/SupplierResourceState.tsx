import React from 'react';

export const SupplierResourceState: React.FC<{ loading: boolean; error: string; retry: () => void }> = ({ loading, error, retry }) => loading ? (
  <p role="status" className="rounded-xl border border-[#e6e2f0] bg-white p-3 text-sm text-slate-700">در حال دریافت اطلاعات…</p>
) : error ? (
  <div role="alert" className="space-y-3 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"><p>{error}</p><button type="button" onClick={retry} className="min-h-11 rounded-lg bg-white px-3 py-2 font-bold hover:bg-rose-100 focus-visible:ring-2 focus-visible:ring-rose-700">تلاش دوباره</button></div>
) : null;

export function SupplierEmptyState({ icon, title, description, action }: {
  icon: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white px-4 py-6 text-center shadow-sm sm:p-8">
      <div aria-hidden="true" className="mx-auto grid size-11 place-items-center rounded-2xl bg-brand-soft text-brand-deep [&>svg]:size-5">{icon}</div>
      <div role="status">
        <p className="text-sm font-extrabold leading-6 text-[#261b41]">{title}</p>
        {description && <p className="mx-auto mt-1 max-w-md text-sm leading-6 text-slate-600">{description}</p>}
      </div>
      {action}
    </div>
  );
}
