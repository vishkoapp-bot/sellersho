import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { ChevronDown, Download, FileUp, Filter, Layers, Pencil, Search } from 'lucide-react';
import { newUuid } from '../services/uuid';
import { formatPrice, toPersianDigits } from '../utils/formatters';
import {
  canRunVariantEdit, parseVariantCSV, parseVariantEdit, parseVariantWorkbookRows, previewVariantEdits, runVariantEdits,
  VARIANT_BATCH_LIMIT, writeVariantCSV,
  type VariantBaseline, type VariantEditPreview, type VariantField, type VariantWorkspaceRole,
} from '../utils/variantWorkspace';

export interface WorkspaceRow extends VariantBaseline {
  productId: string;
  title: string;
  name: string;
  spnc: string;
  active: boolean;
  attention: boolean;
  available?: boolean;
  reserved?: number;
  price?: number | null;
  effectiveMargin?: number | null;
  pendingCost?: number;
  pendingClear?: boolean;
  summary?: boolean;
}

type SaveVariant = Parameters<typeof runVariantEdits>[2];
type FormFields = Parameters<typeof parseVariantEdit>[0];
const buttonClass = 'min-h-11 rounded-xl border border-slate-200 bg-white px-3 text-xs font-bold text-slate-700 hover:bg-slate-50 disabled:opacity-50';
const controlClass = 'min-h-11 w-full rounded-xl border border-slate-200 bg-white px-3 text-sm text-slate-800 focus:border-blue-500 focus:outline-none';
const labels: Record<VariantField, string> = { stock: 'موجودی', cost: 'قیمت عمده', visible: 'نمایش', margin: 'درصد سود' };
const outcomeTone = { saved: 'text-emerald-700', pending: 'text-amber-800', error: 'text-rose-700', conflict: 'text-rose-700' };

const money = (value: number | null | undefined) => value == null || value <= 0 ? '—' : formatPrice(value) + ' تومان';
const percent = (value: number | null | undefined) => value == null ? '—' : toPersianDigits(value) + '٪';
const count = (value: number | undefined) => value == null ? '—' : toPersianDigits(value);
const searchKey = (value: string) => value.toLowerCase().replace(/[يى]/g, 'ی').replace(/ك/g, 'ک').trim();
const statusText = (row: WorkspaceRow, role: VariantWorkspaceRole) => {
  if (!row.active) return 'غیرفعال';
  if (role === 'supplier') return row.stock === 0 ? 'ناموجود' : (row.stock ?? 0) <= 5 ? 'موجودی کم' : 'موجود';
  if (row.visible === false) return 'پنهان در فروشگاه';
  return row.available ? 'قابل فروش' : 'غیرقابل فروش';
};
const editable = (row: WorkspaceRow, role: VariantWorkspaceRole) =>
  !row.summary && (role === 'supplier' ? row.canEditStock || row.canEditCost : row.canEditSeller);

function formatEditValue(field: VariantField, value: WorkspaceRow[VariantField]) {
  if (value == null) return field === 'cost' ? 'ارث‌بری از کالا' : field === 'margin' ? 'پیش‌فرض مجاز' : '—';
  if (field === 'visible') return value ? 'نمایش' : 'پنهان';
  if (field === 'stock') return count(Number(value));
  if (field === 'cost') return money(Number(value));
  return percent(Number(value));
}

export function VariantWorkspace({
  role, load, save, onChanged, initiallyOpen = false, attentionOnly = false, onBack, onRetired, title,
}: {
  role: VariantWorkspaceRole;
  load: (signal: AbortSignal) => Promise<WorkspaceRow[]>;
  save: (source: WorkspaceRow, row: VariantEditPreview, field: VariantField, attempt: string, signal: AbortSignal) => ReturnType<SaveVariant>;
  onChanged?: () => void;
  initiallyOpen?: boolean;
  attentionOnly?: boolean;
  onBack?: () => void;
  onRetired?: (codes: string[]) => void;
  title: string;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  const [filter, setFilter] = useState(attentionOnly ? 'attention' : 'all');
  const [query, setQuery] = useState('');
  const [rows, setRows] = useState<WorkspaceRow[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [target, setTarget] = useState<string[]>([]);
  const [form, setForm] = useState<FormFields>({});
  const [preview, setPreview] = useState<VariantEditPreview[]>([]);
  const [batch, setBatch] = useState('');
  const [showImport, setShowImport] = useState(false);
  const [revision, setRevision] = useState(0);
  const [rowLimit, setRowLimit] = useState(50);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const saveController = useRef<AbortController | null>(null);
  const fileRequest = useRef(0);
  const editor = useRef<HTMLFormElement>(null);
  const panelId = useId();
  const formId = useId();

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    setLoading(true);
    setLoadError('');
    void load(controller.signal).then(
      result => {
        if (controller.signal.aborted) return;
        setRows(result);
        setLoading(false);
      },
      cause => {
        if (controller.signal.aborted) return;
        setLoadError(cause instanceof Error ? cause.message : 'تنوع‌ها بارگیری نشدند.');
        setLoading(false);
      },
    );
    return () => controller.abort();
  }, [open, revision, load]);
  useEffect(() => () => saveController.current?.abort(), []);
  useEffect(() => {
    if (!target.length || !editor.current) return;
    editor.current.scrollIntoView({ block: 'nearest' });
    editor.current.focus({ preventScroll: true });
  }, [target]);

  const rowsById = useMemo(() => new Map(rows.map(row => [row.id, row])), [rows]);
  const filtered = rows.filter(row => {
    switch (filter) {
      case 'attention': if (!row.attention) return false; break;
      case 'hidden': if (row.visible !== false) return false; break;
      case 'unavailable': if (!row.active || row.available !== false) return false; break;
      case 'stock': if (!row.active || (row.stock ?? 0) > 5) return false; break;
      case 'pending': if (row.pendingCost == null) return false; break;
      case 'inactive': if (row.active) return false; break;
    }
    return !query.trim() || searchKey([row.title, row.name, row.spnc, row.id].join(' ')).includes(searchKey(query));
  });
  const displayed = filtered.slice(0, rowLimit);
  const selectedRows = selected.filter(id => {
    const row = rowsById.get(id);
    return row && editable(row, role);
  });
  const targets = target.flatMap(id => rowsById.has(id) ? [rowsById.get(id)!] : []);
  const canStock = role === 'supplier' && targets.some(row => row.canEditStock);
  const canCost = role === 'supplier' && targets.some(row => row.canEditCost);
  const canMargin = role === 'seller' && targets.some(row => !row.marginLocked);
  const validRows = preview.filter(canRunVariantEdit).length;

  const showEdit = (ids: string[]) => {
    fileRequest.current++;
    setTarget(ids);
    setForm({});
    setShowImport(false);
    setPreview([]);
    setError('');
  };
  const readyPreview = (entries: Parameters<typeof previewVariantEdits>[0]) => {
    setPreview(previewVariantEdits(entries, rows, role));
    setBatch(newUuid());
    setError('');
  };
  const inspectFile = async (file: File) => {
    const request = ++fileRequest.current;
    setPreview([]);
    setError('');
    try {
      if (file.size > 512000) throw new Error('حجم فایل بیشتر از حد مجاز است.');
      let edits: Parameters<typeof readyPreview>[0];
      if (/\.xlsx$/i.test(file.name)) {
        const { readSheet } = await import('read-excel-file/browser');
        let sheet: readonly (readonly unknown[])[];
        try {
          sheet = await readSheet(file, 1);
        } catch {
          throw new Error('برگه اول فایل Excel خوانده نشد.');
        }
        edits = parseVariantWorkbookRows(sheet, role);
      } else {
        edits = parseVariantCSV(await file.text(), role);
      }
      if (request === fileRequest.current) readyPreview(edits);
    } catch (cause) {
      if (request === fileRequest.current) setError((cause as Error).message);
    }
  };
  const downloadTemplate = () => {
    try {
      const raw = writeVariantCSV(role, selectedRows);
      const url = URL.createObjectURL(new Blob([raw], { type: 'text/csv;charset=utf-8' }));
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = role + '-variants.csv';
      anchor.click();
      setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (cause) {
      setError((cause as Error).message);
    }
  };
  const apply = async () => {
    setSaving(true);
    const controller = new AbortController();
    saveController.current = controller;
    const result = await runVariantEdits(preview, batch, (row, field, attempt) => {
      const source = rowsById.get(row.id);
      if (!source || controller.signal.aborted) return Promise.reject(new Error('تنوع در دسترس نیست.'));
      return save(source, row, field, attempt, controller.signal);
    });
    if (controller.signal.aborted) return;
    const retired = new Set<string>();
    for (let index = 0; index < result.length; index++) {
      if (preview[index].outcomes?.stock?.state === 'saved') continue;
      for (const code of result[index].outcomes?.stock?.retired ?? []) retired.add(code);
    }
    if (retired.size) onRetired?.([...retired]);
    setPreview(result);
    setSaving(false);
    setRevision(value => value + 1);
    onChanged?.();
  };

  return (
    <section dir="rtl" className="rounded-2xl border border-slate-200 bg-white shadow-2xs">
      <div className="flex flex-wrap items-center gap-2 p-3.5 sm:p-4">
        <button type="button" aria-controls={panelId} aria-expanded={open} disabled={saving}
          onClick={() => setOpen(value => !value)}
          className="flex min-h-11 min-w-0 flex-1 items-center gap-3 text-right font-extrabold text-slate-900">
          <span className="rounded-xl bg-blue-50 p-2 text-blue-700"><Layers size={19} aria-hidden /></span>
          <span className="text-sm sm:text-base">{title}</span>
          <ChevronDown size={17} aria-hidden className={'mr-auto shrink-0 text-slate-500 transition-transform ' + (open ? 'rotate-180' : '')} />
        </button>
        {onBack && <button type="button" className={buttonClass} onClick={onBack} disabled={saving}>بازگشت به کاتالوگ</button>}
      </div>
      {open && (
        <div id={panelId} className="space-y-3 border-t border-slate-100 p-3.5 sm:p-4">
          <div className="grid gap-2 sm:grid-cols-[1fr_11rem_auto]">
            <label className="relative block">
              <span className="sr-only">جست‌وجوی تنوع</span>
              <Search className="pointer-events-none absolute right-3 top-3 text-slate-500" size={17} aria-hidden />
              <input type="search" value={query} onChange={event => { setQuery(event.target.value); setRowLimit(50); }}
                placeholder="جست‌وجو با نام کالا، تنوع یا کد SPNC" className={controlClass + ' pr-9'} />
            </label>
            <label className="relative block">
              <span className="sr-only">وضعیت تنوع</span>
              <Filter className="pointer-events-none absolute right-3 top-3.5 text-slate-500" size={15} aria-hidden />
              <select value={filter} onChange={event => { setFilter(event.target.value); setRowLimit(50); }} className={controlClass + ' pr-9'}>
                <option value="all">همه وضعیت‌ها</option>
                <option value="attention">نیازمند توجه</option>
                {role === 'supplier' ? <>
                  <option value="stock">کم یا ناموجود</option><option value="pending">قیمت در حال بررسی</option>
                </> : <>
                  <option value="hidden">پنهان در فروشگاه</option><option value="unavailable">غیرقابل فروش</option>
                </>}
                <option value="inactive">غیرفعال</option>
              </select>
            </label>
            <button type="button" className={buttonClass} onClick={() => setRevision(value => value + 1)} disabled={loading || saving}>تازه‌سازی</button>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" className={buttonClass} disabled={loading || !!loadError || saving || !selectedRows.length}
              onClick={() => showEdit(selectedRows)}>ویرایش {count(selectedRows.length)} تنوع</button>
            <button type="button" className={buttonClass + ' inline-flex items-center gap-1.5'} disabled={loading || !!loadError || saving}
              onClick={() => { showEdit([]); setShowImport(true); }}><FileUp size={15} aria-hidden /> ورود از Excel یا CSV</button>
            <button type="button" className={buttonClass + ' inline-flex items-center gap-1.5'} onClick={downloadTemplate} disabled={saving}>
              <Download size={15} aria-hidden /> فایل الگو
            </button>
          </div>
          {loadError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{loadError}</p>}
          {loading && <p role="status" className="text-sm text-slate-600">در حال بارگیری تنوع‌ها…</p>}
          {!loading && !loadError && !filtered.length && <p className="rounded-xl bg-slate-50 p-4 text-sm text-slate-600">تنوعی با این وضعیت یا عبارت پیدا نشد.</p>}
          {showImport && (
            <div className="space-y-2 rounded-xl border border-blue-100 bg-blue-50/60 p-3">
              <p className="text-xs leading-6 text-slate-700">
                فایل الگو را با Excel باز کنید و با فرمت XLSX یا CSV UTF-8 ذخیره کنید. در فایل Excel فقط برگه اول خوانده می‌شود. سلول خالی بدون تغییر می‌ماند.
                {' '}{role === 'supplier' ? 'تغییر موجودی به علت نیاز دارد. برای ارث‌بری قیمت کالا، inherit وارد کنید.'
                  : 'برای نمایش visible، برای پنهان‌کردن hidden و برای سود پیش‌فرض inherit وارد کنید.'}
              </p>
              <label className="block text-sm font-bold text-slate-800">
                انتخاب فایل تغییرات
                <input type="file" accept=".xlsx,.csv,.tsv,text/csv,text/tab-separated-values,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" className="mt-2 block min-h-11 w-full max-w-sm text-xs"
                  disabled={saving || loading || !!loadError} onChange={event => {
                    const file = event.currentTarget.files?.[0];
                    event.currentTarget.value = '';
                    if (file) void inspectFile(file);
                  }} />
              </label>
            </div>
          )}
          {target.length > 0 && (
            <form id={formId} ref={editor} tabIndex={-1} className="space-y-2 rounded-xl border border-blue-100 bg-blue-50/60 p-3" onSubmit={event => {
              event.preventDefault();
              try {
                const edit = parseVariantEdit(form);
                readyPreview(target.map((id, index) => ({ id, line: index + 1, edit })));
              } catch (cause) {
                setPreview([]);
                setError((cause as Error).message);
              }
            }}>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-bold text-slate-900">{targets.length === 1 ? targets[0].title + ' · ' + targets[0].name : 'ویرایش گروهی تنوع‌ها'}</h3>
                <button type="button" className={buttonClass} disabled={saving} onClick={() => showEdit([])}>بستن</button>
              </div>
              <fieldset disabled={saving} className="grid gap-2 sm:grid-cols-2">
                {canStock && <>
                  <label className="text-xs font-bold text-slate-800">موجودی قابل فروش جدید
                    <input inputMode="numeric" className={controlClass + ' mt-1'} value={form.stock ?? ''} onChange={event => { setForm({ ...form, stock: event.target.value }); setPreview([]); }} />
                  </label>
                  <label className="text-xs font-bold text-slate-800">علت اصلاح موجودی
                    <input className={controlClass + ' mt-1'} maxLength={500} value={form.reason ?? ''} onChange={event => { setForm({ ...form, reason: event.target.value }); setPreview([]); }} />
                  </label>
                </>}
                {canCost && <label className="text-xs font-bold text-slate-800">قیمت عمده (تومان)
                  <input className={controlClass + ' mt-1'} placeholder="مبلغ یا inherit" value={form.base_cost ?? ''} onChange={event => { setForm({ ...form, base_cost: event.target.value }); setPreview([]); }} />
                </label>}
                {role === 'seller' && <label className="text-xs font-bold text-slate-800">نمایش در فروشگاه
                  <select className={controlClass + ' mt-1'} value={form.visibility ?? ''} onChange={event => { setForm({ ...form, visibility: event.target.value }); setPreview([]); }}>
                    <option value="">بدون تغییر</option><option value="visible">نمایش</option><option value="hidden">پنهان</option>
                  </select>
                </label>}
                {canMargin && <label className="text-xs font-bold text-slate-800">درصد سود
                  <input className={controlClass + ' mt-1'} placeholder="درصد یا inherit" value={form.margin_pct ?? ''} onChange={event => { setForm({ ...form, margin_pct: event.target.value }); setPreview([]); }} />
                </label>}
              </fieldset>
              <button type="submit" className={buttonClass} disabled={saving || loading || !!loadError}>پیش‌نمایش تغییرات</button>
            </form>
          )}
          {error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
          {preview.length > 0 && (
            <div className="space-y-2 rounded-xl border border-blue-100 bg-slate-50 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="text-sm font-bold text-slate-900">پیش‌نمایش تغییرات · {count(preview.length)} سطر</h3>
                <button type="button" className="min-h-11 rounded-xl bg-blue-700 px-4 text-xs font-bold text-white hover:bg-blue-800 disabled:opacity-50"
                  disabled={!validRows || saving || loading || !!loadError} onClick={() => void apply()}>
                  {saving ? 'در حال ثبت…' : (preview.some(row => row.outcomes) ? 'تلاش دوباره برای ' : 'ثبت ') + count(validRows) + ' سطر'}
                </button>
              </div>
              <ul aria-live="polite" className="max-h-80 divide-y divide-slate-200 overflow-auto rounded-lg bg-white px-3">
                {preview.map(row => {
                  const source = rowsById.get(row.id);
                  return <li key={row.line} className="space-y-1 py-2 text-xs">
                    <p className="font-bold text-slate-800">سطر {count(row.line)} · {source ? source.title + ' · ' + source.name : row.id}</p>
                    {row.error && <p className="text-rose-700">{row.error}</p>}
                    {(Object.keys(labels) as VariantField[]).filter(field => row.edit[field] !== undefined).map(field => {
                      const outcome = row.outcomes?.[field];
                      return <p key={field} className={outcome ? outcomeTone[outcome.state] : 'text-slate-600'}>
                        {labels[field]} · {formatEditValue(field, row.current?.[field])} ← {formatEditValue(field, row.edit[field])}
                        {outcome && ' · ' + outcome.message}
                      </p>;
                    })}
                  </li>;
                })}
              </ul>
            </div>
          )}
          {!loadError && displayed.length > 0 && (
            <ul className="divide-y divide-slate-100 overflow-hidden rounded-xl border border-slate-200">
              {displayed.map(row => (
                <li key={row.id} className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-3 gap-y-2 px-2 py-3 sm:grid-cols-[auto_minmax(0,1.25fr)_minmax(0,2fr)_auto] sm:items-center sm:px-3">
                  <label className="grid min-h-11 min-w-11 place-items-center">
                    <input type="checkbox" aria-label={'انتخاب ' + row.title + ' ' + row.name}
                      className="size-4 accent-blue-700 disabled:opacity-30" disabled={!editable(row, role) || saving || (!selected.includes(row.id) && selected.length >= VARIANT_BATCH_LIMIT)}
                      checked={selected.includes(row.id)} onChange={event => setSelected(ids => event.target.checked ? [...ids, row.id] : ids.filter(id => id !== row.id))} />
                  </label>
                  <div className="min-w-0 self-center">
                    <p className="break-words text-sm font-extrabold text-slate-900">{row.title}</p>
                    <p className="break-words text-xs text-slate-600">{row.name} {row.spnc && <bdi className="text-slate-500">· {row.spnc}</bdi>}</p>
                  </div>
                  <dl className="col-span-3 row-start-2 grid grid-cols-2 gap-2 rounded-lg bg-slate-50 px-2 py-2 text-xs sm:col-span-1 sm:row-auto sm:grid-cols-4 sm:bg-transparent sm:p-0">
                    {role === 'supplier' ? <>
                      <Metric label="قابل فروش" value={count(row.stock)} />
                      <Metric label="رزروشده" value={count(row.reserved)} />
                      <Metric label="قیمت عمده" value={money(row.cost ?? row.productCost)} />
                    </> : <>
                      <Metric label="نمایش" value={row.summary ? '—' : row.visible ? 'روشن' : 'پنهان'} />
                      <Metric label="سود مؤثر" value={percent(row.effectiveMargin)} />
                      <Metric label="قیمت مصرف‌کننده" value={money(row.price)} />
                    </>}
                    <Metric label="وضعیت" value={statusText(row, role)} attention={row.attention} />
                    {role === 'supplier' && row.pendingCost != null && (
                      <p className="col-span-full text-xs font-bold text-amber-800">
                        در انتظار بررسی · {row.pendingClear ? 'ارث‌بری از قیمت کالا' : money(row.pendingCost)}
                      </p>
                    )}
                  </dl>
                  <button type="button" className={buttonClass + ' col-start-3 row-start-1 inline-flex items-center gap-1 self-center sm:col-start-auto sm:row-start-auto'}
                    aria-label={'ویرایش ' + row.title + ' ' + row.name} aria-controls={target.includes(row.id) ? formId : undefined}
                    onClick={() => showEdit([row.id])}
                    disabled={saving || loading || !editable(row, role)}><Pencil size={14} aria-hidden /> ویرایش</button>
                </li>
              ))}
            </ul>
          )}
          {displayed.length < filtered.length && <button type="button" className={buttonClass} onClick={() => setRowLimit(limit => limit + 50)}>تنوع‌های بیشتر</button>}
        </div>
      )}
    </section>
  );
}

function Metric({ label, value, attention }: { label: string; value: string; attention?: boolean }) {
  return <div><dt className="text-slate-500">{label}</dt><dd className={'mt-1 font-bold ' + (attention ? 'text-amber-800' : 'text-slate-800')}>{value}</dd></div>;
}
