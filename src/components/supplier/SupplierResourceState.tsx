import React from 'react';

export const SupplierResourceState: React.FC<{ loading: boolean; error: string; retry: () => void }> = ({ loading, error, retry }) => loading ? (
  <p role="status" className="rounded-xl bg-white p-4 text-xs text-slate-600">در حال دریافت اطلاعات...</p>
) : error ? (
  <div role="alert" className="space-y-3 rounded-xl bg-rose-50 p-4 text-xs text-rose-800"><p>{error}</p><button type="button" onClick={retry} className="rounded-lg bg-white px-3 py-2 font-bold">تلاش دوباره</button></div>
) : null;