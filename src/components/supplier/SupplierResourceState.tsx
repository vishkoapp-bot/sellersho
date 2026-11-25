import React from 'react';

export const SupplierResourceState: React.FC<{ loading: boolean; error: string; retry: () => void }> = ({ loading, error, retry }) => loading ? (
  <p role="status" className="rounded-xl border border-[#e6e2f0] bg-white p-3 text-sm text-slate-700">در حال دریافت اطلاعات…</p>
) : error ? (
  <div role="alert" className="space-y-3 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800"><p>{error}</p><button type="button" onClick={retry} className="min-h-11 rounded-lg bg-white px-3 py-2 font-bold hover:bg-rose-100 focus-visible:ring-2 focus-visible:ring-rose-700">تلاش دوباره</button></div>
) : null;