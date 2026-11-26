import { useEffect, useState } from 'react';
import { History } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { adminApi, type AuditLogDto } from '../../services/adminApi';
import { isMockMode } from '../../services/apiClient';
import { SupplierResourceState } from '../supplier/SupplierResourceState';

const categories: Record<string, string> = {
  commission: 'تعرفه و کارمزد',
  store_status: 'وضعیت فروشگاه',
  catalog: 'کالا و بارکد',
  warehouse: 'انبار',
  finance: 'مالی',
  dispute: 'داوری',
  subscription: 'اشتراک',
  supplier: 'تأمین‌کننده',
};

export function AdminAuditLogTab() {
  const { auditLogs } = useStore();
  const mock = isMockMode();
  const [category, setCategory] = useState('');
  const [search, setSearch] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<{ rows: AuditLogDto[]; error: string } | null>(null);

  useEffect(() => {
    if (mock) return;
    const controller = new AbortController();
    setResult(null);
    void adminApi.getAuditLogs(category, controller.signal).then(rows => {
      if (!Array.isArray(rows)) throw new Error('پاسخ سوابق حسابرسی کامل نیست.');
      if (!controller.signal.aborted) setResult({ rows, error: '' });
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) {
        setResult({ rows: [], error: cause instanceof Error ? cause.message : 'دریافت سوابق حسابرسی انجام نشد.' });
      }
    });
    return () => controller.abort();
  }, [category, attempt, mock]);

  const query = search.trim().toLocaleLowerCase('fa');
  const rows = mock ? auditLogs : result?.rows ?? [];
  const filtered = rows.filter(row => (!category || row.category === category)
    && (!query || [row.id, row.action, row.operator, row.details].some(value => value?.toLocaleLowerCase('fa').includes(query))));
  const loading = !mock && result === null;
  const error = result?.error ?? '';

  return (
    <section className="space-y-3" aria-label="سوابق حسابرسی">
      <div className="grid min-w-0 gap-3 rounded-2xl border border-slate-200 bg-white p-4 sm:grid-cols-[1fr_13rem]">
        <p className="flex items-center gap-2 text-sm font-bold text-slate-700 sm:col-span-2">
          <History className="size-5 text-brand-deep" aria-hidden="true" />
          {mock ? 'سوابق نمونهٔ حسابرسی' : '۱۰۰ رویداد آخر در دستهٔ انتخاب‌شده'}
        </p>
        <label className="block min-w-0 space-y-1 text-sm font-bold text-slate-700">
          <span>جست‌وجو در سوابق</span>
          <input type="search" value={search} onChange={event => setSearch(event.target.value)}
            className="min-h-11 w-full rounded-xl border border-slate-300 px-3 font-normal focus-visible:outline-2 focus-visible:outline-brand" />
        </label>
        <label className="block min-w-0 space-y-1 text-sm font-bold text-slate-700">
          <span>دستهٔ رویداد</span>
          <select aria-label="دستهٔ رویداد" value={category} onChange={event => setCategory(event.target.value)}
            className="min-h-11 w-full rounded-xl border border-slate-300 px-2 font-normal focus-visible:outline-2 focus-visible:outline-brand">
            <option value="">همهٔ دسته‌ها</option>
            {Object.entries(categories).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
          </select>
        </label>
      </div>
      <SupplierResourceState loading={loading} error={error} retry={() => setAttempt(value => value + 1)} />
      {!loading && !error && (filtered.length === 0 ? (
        <p role="status" className="rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-700">
          {category || query ? 'سابقه‌ای مطابق فیلتر ثبت نشده است.' : 'هنوز سابقه‌ای ثبت نشده است.'}
        </p>
      ) : (
        <ul className="space-y-2">
          {filtered.map(row => (
            <li key={row.id} className="min-w-0 space-y-2 rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-700">
              <div className="flex flex-wrap items-start justify-between gap-2">
                <h2 className="min-w-0 font-extrabold text-slate-900 [overflow-wrap:anywhere]">{row.action}</h2>
                <span className="rounded-lg bg-brand-soft px-2 py-1 text-xs font-bold text-brand-deep">{categories[row.category] || 'سیستمی'}</span>
              </div>
              <p className="whitespace-pre-wrap leading-6 [overflow-wrap:anywhere]">{row.details}</p>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-slate-100 pt-2 text-xs">
                <bdi className="[overflow-wrap:anywhere]">{row.operator}</bdi>
                <time dateTime={row.timestamp}>{new Date(row.timestamp).toLocaleString('fa-IR')}</time>
                <bdi className="font-mono text-slate-500 [overflow-wrap:anywhere]">{row.id}</bdi>
              </div>
            </li>
          ))}
        </ul>
      ))}
    </section>
  );
}
