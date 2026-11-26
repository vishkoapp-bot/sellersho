import { useState } from 'react';
import { apiRequest } from '../../services/apiClient';
import { useStore } from '../../context/StoreContext';

interface Point { x: number; y: number; z: number; }
interface Route { stops: { binCode: string; items: string[] }[]; missingBins: string[]; distanceMeters: number; algorithm: string; }

const nf = new Intl.NumberFormat('fa-IR', { maximumFractionDigits: 2 });
const detailClass = 'rounded-2xl border border-brand-border bg-white px-4';
const summaryClass = 'min-h-11 cursor-pointer py-3 text-sm font-extrabold text-brand-deep';
const inputClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base focus-visible:outline-2 focus-visible:outline-brand sm:text-sm';
const actionClass = 'min-h-11 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-brand-deep/90 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

function CoordinateInputs({ value, onChange }: { value: Point; onChange: (point: Point) => void }) {
  return (
    <div className="grid grid-cols-3 gap-2" dir="ltr">
      {(['x', 'y', 'z'] as const).map(axis => (
        <label key={axis} className="min-w-0 space-y-1 text-sm font-medium text-slate-700">
          <span className="block">{axis.toUpperCase()} (متر)</span>
          <input aria-label={`مختصات ${axis}`} type="number" step="0.01" value={value[axis]}
            onFocus={event => event.currentTarget.select()}
            onChange={event => onChange({ ...value, [axis]: Number(event.target.value) })} className={inputClass} />
        </label>
      ))}
    </div>
  );
}

export function BinSuggestions({ taskId, onSelect }: { taskId: string; onSelect: (bin: string) => void }) {
  const [origin, setOrigin] = useState<Point>({ x: 0, y: 0, z: 0 });
  const [rows, setRows] = useState<{ binCode: string; distanceMeters: number }[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const suggest = async () => {
    setBusy(true);
    setError('');
    setRows(null);
    try {
      setRows(await apiRequest(`/warehouse/putaway/tasks/${encodeURIComponent(taskId)}/suggestions`, {
        method: 'POST', auth: true, body: JSON.stringify(origin),
      }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'دریافت پیشنهاد ناموفق بود.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <details className={detailClass}>
      <summary className={summaryClass}>پیشنهاد قفسه نزدیک</summary>
      <form onSubmit={event => { event.preventDefault(); if (!busy) void suggest(); }} className="space-y-3 pb-4">
        <p className="text-sm leading-6 text-slate-600">مختصات فعلی کالا را بر حسب متر وارد کنید.</p>
        <CoordinateInputs value={origin} onChange={value => { setOrigin(value); setRows(null); }} />
        <button type="submit" disabled={busy} className={actionClass}>{busy ? 'در حال محاسبه…' : 'پیدا کردن قفسه مناسب'}</button>
        {error && <p role="alert" className="text-sm text-rose-800">{error}</p>}
        {rows?.length === 0 && <p role="status" className="text-sm text-slate-700">قفسهٔ مناسب پیدا نشد. مختصات و ظرفیت‌ها را بررسی کنید.</p>}
        {rows?.map(bin => (
          <button type="button" key={bin.binCode} onClick={() => onSelect(bin.binCode)}
            className="flex min-h-11 w-full flex-wrap items-center justify-between gap-2 rounded-xl border border-brand-border bg-brand-bg px-3 py-2 text-sm font-medium text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">
            <bdi className="break-all font-mono">{bin.binCode}</bdi>
            <span>{nf.format(bin.distanceMeters)} متر · انتخاب</span>
          </button>
        ))}
      </form>
    </details>
  );
}

export function PickingRoutePlanner({ orderIds }: { orderIds: string[] }) {
  const [start, setStart] = useState<Point>({ x: 0, y: 0, z: 0 });
  const [warehouse, setWarehouse] = useState('');
  const [route, setRoute] = useState<Route | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const canOptimize = !busy && warehouse.trim() && orderIds.length > 0;

  const optimize = async () => {
    setError('');
    setRoute(null);
    setBusy(true);
    try {
      setRoute(await apiRequest('/warehouse/picking-route', { method: 'POST', auth: true, body: JSON.stringify({ start, warehouse, orderIds }) }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'محاسبهٔ مسیر ناموفق بود.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <details className={detailClass}>
      <summary className={summaryClass}>مسیر جمع‌آوری گروهی سفارش‌ها</summary>
      <form onSubmit={event => { event.preventDefault(); if (canOptimize) void optimize(); }} className="space-y-3 pb-4">
        <p className="text-sm leading-6 text-slate-600">نام انبار و مختصات میز شروع را وارد کنید.</p>
        <label className="block space-y-1 text-sm font-medium text-slate-700">
          <span className="block">نام ثبت‌شدهٔ انبار</span>
          <input aria-label="نام انبار مسیر" value={warehouse} onChange={event => setWarehouse(event.target.value)} className={inputClass} />
        </label>
        <CoordinateInputs value={start} onChange={setStart} />
        <button type="submit" disabled={!canOptimize} className={actionClass}>
          {busy ? 'در حال محاسبه…' : `محاسبه مسیر برای ${nf.format(orderIds.length)} سفارش`}
        </button>
        {error && <p role="alert" className="text-sm text-rose-800">{error}</p>}
        {route && <div className="space-y-2 text-sm text-slate-700">
          {route.missingBins.length > 0 && <p role="alert" className="rounded-xl bg-amber-50 p-3 text-amber-900">مسیر ناقص است. مختصات این قفسه‌ها در انبار ثبت نشده است. {route.missingBins.map(bin => <bdi className="me-2 break-all font-mono" key={bin}>{bin}</bdi>)}</p>}
          <p role="status" className="font-bold">{nf.format(route.distanceMeters)} متر · {route.algorithm === 'held-karp' ? 'مسیر دقیق' : 'مسیر تقریبی'}</p>
          <ol className="space-y-2">{route.stops.map((stop, i) => <li key={stop.binCode} className="rounded-xl bg-brand-bg p-3">
            <strong>{nf.format(i + 1)}. <bdi className="break-all font-mono">{stop.binCode}</bdi></strong>
            {stop.items.map(item => <p key={item} className="mt-1 break-words text-slate-700">{item}</p>)}
          </li>)}</ol>
          <p>پس از آخرین قفسه به میز شروع برگردید.</p>
        </div>}
      </form>
    </details>
  );
}

export function WarehousePositionEditor() {
  const { warehouseSession, adminSession } = useStore();
  const canEdit = !!adminSession || warehouseSession?.role === 'logistics_specialist';
  const [bin, setBin] = useState('');
  const [position, setPosition] = useState<Point>({ x: 0, y: 0, z: 0 });
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  if (!canEdit) return null;

  const save = async () => {
    setBusy(true);
    setMessage('');
    try {
      await apiRequest(`/warehouse/bins/${encodeURIComponent(bin)}/position`, {
        method: 'PUT', auth: true, body: JSON.stringify(position),
      });
      setMessage('مختصات قفسه ثبت شد.');
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'ثبت انجام نشد.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <details className={detailClass}>
      <summary className={summaryClass}>ثبت مختصات قفسه‌ها</summary>
      <form onSubmit={event => { event.preventDefault(); if (!busy && bin.trim()) void save(); }} className="space-y-3 pb-4">
        <p className="text-sm leading-6 text-slate-600">مختصات را از نقشه انبار و بر حسب متر وارد کنید.</p>
        <label className="block space-y-1 text-sm font-medium text-slate-700">
          <span className="block">کد قفسه</span>
          <input aria-label="کد قفسه" dir="ltr" value={bin} autoCapitalize="off" autoComplete="off" spellCheck={false}
            onChange={event => setBin(event.target.value)} className={inputClass} />
        </label>
        <CoordinateInputs value={position} onChange={setPosition} />
        <button type="submit" disabled={busy || !bin.trim()} className={actionClass}>{busy ? 'در حال ثبت…' : 'ثبت مختصات'}</button>
        {message && <p role="status" className="text-sm text-slate-700">{message}</p>}
      </form>
    </details>
  );
}
