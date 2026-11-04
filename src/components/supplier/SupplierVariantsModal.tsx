import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { X, Plus, Trash2, Printer, Loader2, Tag } from 'lucide-react';

// Live variants modal (Fl) + cart-limits editor (Ul) + label print
// preview (Ol). QR images are NOT rendered: no QR library exists in the
// pinned node_modules (same known gap as Fix 46's carton print); the
// sheet still shows SPN/SPNC/qr_payload text and stock.
const MAX_BULK = 24;
const ATTR_KEYS = ['رنگ', 'سایز'];

interface VariantRow {
  id: string;
  variant_name: string;
  spnc: string;
  stock: number;
  attributes?: Record<string, string>;
  is_default?: boolean;
  min_cart_qty?: number | null;
  max_cart_qty?: number | null;
}

interface CustomAttr {
  key: string;
  value: string;
}

const emptyBulkRow = () => ({ name: '', color: '', size: '', custom: [] as CustomAttr[] });

const VariantCartLimits: React.FC<{
  productId: string;
  variant: VariantRow;
  onSaved: (v: VariantRow) => void;
}> = ({ productId, variant, onSaved }) => {
  const { error, success } = useToast();
  const [minQty, setMinQty] = useState(String(variant.min_cart_qty ?? 1));
  const [maxQty, setMaxQty] = useState(variant.max_cart_qty != null ? String(variant.max_cart_qty) : '');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setMinQty(String(variant.min_cart_qty ?? 1));
    setMaxQty(variant.max_cart_qty != null ? String(variant.max_cart_qty) : '');
  }, [variant.min_cart_qty, variant.max_cart_qty]);

  const parseNum = (s: string): number | null => {
    const d = s.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
    if (!/^\d+$/.test(d)) return null;
    return parseInt(d, 10);
  };

  const save = async () => {
    const min = parseNum(minQty.trim());
    if (min === null || min < 1) {
      error('حداقل تعداد خرید باید عددی حداقل ۱ باشد.');
      return;
    }
    let max: number | null = null;
    if (maxQty.trim() !== '') {
      max = parseNum(maxQty);
      if (max === null || max < 1) {
        error('حداکثر تعداد خرید باید عددی حداقل ۱ باشد.');
        return;
      }
      if (max < min) {
        error('حداکثر تعداد خرید نمی‌تواند کمتر از حداقل تعداد خرید باشد.');
        return;
      }
    }
    setSaving(true);
    try {
      const updated: any = await supplierApi.updateVariantCartLimits(productId, variant.id, min, max);
      success('محدودیت خرید این تنوع ذخیره شد.');
      onSaved({ ...variant, min_cart_qty: updated.min_cart_qty, max_cart_qty: updated.max_cart_qty });
    } catch (e: any) {
      error(e?.message || 'ذخیره محدودیت خرید انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const stock = variant.stock ?? 0;
  const minParsed = parseNum(minQty.trim());
  const inputCls = 'w-full rounded-xl border border-slate-200 p-2 text-xs bg-white focus:border-brand-deep outline-none text-center';
  return (
    <div className="border-t border-slate-100 pt-2 space-y-1.5">
      <div className="flex items-end gap-2">
        <label className="flex-1">
          <span className="block text-[10px] font-bold text-slate-500 mb-1">حداقل خرید</span>
          <input value={minQty} onChange={(e) => setMinQty(e.target.value)} inputMode="numeric" dir="ltr" className={inputCls} aria-label="حداقل تعداد خرید" />
        </label>
        <label className="flex-1">
          <span className="block text-[10px] font-bold text-slate-500 mb-1">حداکثر خرید</span>
          <input value={maxQty} onChange={(e) => setMaxQty(e.target.value)} inputMode="numeric" dir="ltr" placeholder="—" className={inputCls} aria-label="حداکثر تعداد خرید" />
        </label>
        <button type="button" onClick={save} disabled={saving} className="px-3.5 py-2 rounded-xl bg-brand-deep text-white text-[11px] font-bold hover:bg-brand transition-colors disabled:opacity-50 flex items-center gap-1 shrink-0">
          {saving && <Loader2 className="w-3.5 h-3.5 animate-spin"  strokeWidth={1.5}/>}
          <span>{saving ? 'در حال ذخیره…' : 'ذخیره'}</span>
        </button>
      </div>
      {minParsed !== null && minParsed > stock && (
        <p className="text-[10px] text-amber-600 leading-relaxed" role="alert">حداقل خرید از موجودی فعلی بیشتر است؛ تا وقتی موجودی کمتر از حداقل باشد، این واریانت قابل خرید نیست.</p>
      )}
      <p className="text-[10px] text-slate-400 leading-relaxed">تعداد مجاز از این تنوع در هر سبد خرید یک مشتری؛ حداکثر خالی = بدون محدودیت (فقط تا سقف موجودی).</p>
    </div>
  );
};

const LabelPrintPreview: React.FC<{
  spn: string;
  productTitle: string;
  labels: any[];
  stockBySpnc: Record<string, number>;
  onClose: () => void;
}> = ({ spn, productTitle, labels, stockBySpnc, onClose }) => {
  const { toPersianDigits } = useStore();
  return (
    <div className="fixed inset-0 z-[80] bg-slate-100 overflow-y-auto" dir="rtl">
      <div className="sticky top-0 z-10 bg-white border-b border-slate-200 px-4 py-3 flex items-center justify-between">
        <div>
          <h2 className="text-sm font-extrabold text-slate-900">پیش‌نمایش چاپ برچسب</h2>
          <p className="text-[11px] text-slate-500 mt-0.5">{toPersianDigits(labels.length)} برچسب • هر برچسب روی یک برچسب جداگانه چاپ می‌شود</p>
        </div>
        <div className="flex items-center gap-2">
          <button type="button" onClick={() => window.print()} className="flex items-center gap-1.5 px-4 py-2 rounded-xl bg-brand-deep text-white text-xs font-bold hover:bg-brand transition-colors">
            <Printer className="w-4 h-4"  strokeWidth={1.5}/><span>چاپ</span>
          </button>
          <button type="button" onClick={onClose} className="p-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-600" aria-label="بستن">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>
      </div>
      <div className="p-4 flex flex-col items-center gap-4">
        {labels.length === 0 && (
          <div className="bg-white rounded-2xl p-8 text-center text-xs text-slate-500">برچسبی برای چاپ وجود ندارد.</div>
        )}
        {labels.map((label: any) => (
          <div key={label.spnc} className="bg-white rounded-2xl border-2 border-slate-200 shadow-md p-4 w-full max-w-[340px] space-y-2.5">
            <div className="text-center pb-2 border-b-2 border-brand/20">
              <span className="text-[10px] font-bold text-brand-deep block">کد محصول (SPN)</span>
              <span className="font-mono text-lg font-black text-brand-deep tracking-wider" dir="ltr">{spn}</span>
            </div>
            <div className="text-center">
              <h3 className="text-sm font-extrabold text-slate-900 leading-relaxed">{productTitle}</h3>
              {label.variant_name && <p className="text-xs font-bold text-slate-500 mt-0.5">{label.variant_name}</p>}
            </div>
            <div className="text-center bg-slate-50 rounded-xl py-2">
              <span className="text-[10px] font-bold text-slate-400 block mb-0.5">کد تنوع (SPNC)</span>
              <span className="font-mono text-xl font-black text-slate-900 tracking-widest" dir="ltr">{label.spnc}</span>
            </div>
            <p className="text-center text-[9px] text-slate-400 font-mono break-all" dir="ltr">{label.qr_payload}</p>
            {typeof stockBySpnc[label.spnc] === 'number' && (
              <p className="text-center text-[11px] font-bold text-slate-600">موجودی ثبت‌شده: {toPersianDigits(stockBySpnc[label.spnc])} عدد</p>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};

export const SupplierVariantsModal: React.FC<{
  productId: string;
  productTitle: string;
  onClose: () => void;
  onChanged?: () => void;
}> = ({ productId, productTitle, onClose, onChanged }) => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [variants, setVariants] = useState<VariantRow[]>([]);
  const [spn, setSpn] = useState('');
  const [labels, setLabels] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [formOpen, setFormOpen] = useState(false);
  const [mode, setMode] = useState<'single' | 'bulk'>('single');
  const [singleName, setSingleName] = useState('');
  const [singleAttrs, setSingleAttrs] = useState<Record<string, string>>({ رنگ: '', سایز: '' });
  const [singleCustom, setSingleCustom] = useState<CustomAttr[]>([]);
  const [singleSaving, setSingleSaving] = useState(false);
  const [bulkRows, setBulkRows] = useState([emptyBulkRow(), emptyBulkRow()]);
  const [bulkSaving, setBulkSaving] = useState(false);
  const [printOpen, setPrintOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [vs, lb] = await Promise.all([
        supplierApi.getVariants(productId),
        supplierApi.getLabels(productId).catch(() => null),
      ]);
      setVariants(vs || []);
      if (lb) {
        setSpn(lb.spn || '');
        setLabels(lb.labels || []);
      }
    } catch {
      error('خطا در دریافت تنوع‌های محصول.');
    } finally {
      setLoading(false);
    }
  }, [productId, error]);

  useEffect(() => { load(); }, [load]);

  const collectSingleAttrs = (): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const k of ATTR_KEYS) {
      const v = (singleAttrs[k] || '').trim();
      if (v) out[k] = v;
    }
    for (const c of singleCustom) {
      if (c.key.trim() && c.value.trim()) out[c.key.trim()] = c.value.trim();
    }
    return out;
  };

  const submitSingle = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!singleName.trim()) {
      error('نام تنوع را وارد کنید.');
      return;
    }
    setSingleSaving(true);
    try {
      const res: any = await supplierApi.createVariant(productId, { variant_name: singleName.trim(), attributes: collectSingleAttrs() });
      if (res?.pendingReview) {
        success('درخواست ثبت تنوع در صف بازبینی قرار گرفت؛ پس از تأیید، کد SPNC صادر می‌شود.');
      } else {
        success(`تنوع «${res.variant_name}» با SPNC ‏${res.spnc}‏ ثبت شد.`);
      }
      setSingleName('');
      setSingleAttrs({ رنگ: '', سایز: '' });
      setSingleCustom([]);
      setFormOpen(false);
      await load();
      onChanged?.();
    } catch (err: any) {
      error(err?.message || 'ثبت تنوع انجام نشد.');
    } finally {
      setSingleSaving(false);
    }
  };

  const submitBulk = async (e: React.FormEvent) => {
    e.preventDefault();
    const rows = bulkRows.filter((r) => r.name.trim() !== '');
    if (rows.length === 0) {
      error('حداقل نام یک تنوع را وارد کنید.');
      return;
    }
    if (rows.length > MAX_BULK) {
      error(`در هر درخواست حداکثر ${toPersianDigits(MAX_BULK)} تنوع می‌توانید ثبت کنید.`);
      return;
    }
    const seen = new Set<string>();
    for (const r of rows) {
      const name = r.name.trim();
      if (seen.has(name)) {
        error(`نام تنوع «${name}» در همین فهرست تکرار شده است.`);
        return;
      }
      seen.add(name);
      if (variants.some((v) => v.variant_name === name)) {
        error(`تنوع «${name}» از قبل برای این محصول ثبت شده است.`);
        return;
      }
    }
    setBulkSaving(true);
    try {
      const res: any = await supplierApi.createVariantsBulk(productId, rows.map((r) => {
        const attrs: Record<string, string> = {};
        if (r.color.trim()) attrs['رنگ'] = r.color.trim();
        if (r.size.trim()) attrs['سایز'] = r.size.trim();
        for (const c of r.custom) {
          if (c.key.trim() && c.value.trim()) attrs[c.key.trim()] = c.value.trim();
        }
        return { variant_name: r.name.trim(), attributes: attrs };
      }));
      success(`${toPersianDigits(res.count || rows.length)} تنوع ثبت شد؛ کد SPNC همه با هم صادر شد.`);
      setBulkRows([emptyBulkRow(), emptyBulkRow()]);
      setFormOpen(false);
      await load();
      onChanged?.();
    } catch (err: any) {
      error(err?.message || 'ثبت گروهی تنوع‌ها انجام نشد.');
    } finally {
      setBulkSaving(false);
    }
  };

  const inputCls = 'rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none';
  const stockBySpnc = variants.reduce<Record<string, number>>((acc, v) => {
    if (v.stock !== undefined && v.stock !== null) acc[v.spnc] = v.stock;
    return acc;
  }, {});

  return (
    <>
      <div className="fixed inset-0 z-[70] bg-slate-950/60 backdrop-blur-sm flex items-end sm:items-center justify-center p-0 sm:p-4" dir="rtl">
        <div className="bg-white w-full sm:max-w-lg max-h-[92vh] overflow-y-auto rounded-t-3xl sm:rounded-3xl shadow-2xl">
          <div className="sticky top-0 bg-white border-b border-slate-200 px-4 py-3 flex items-center justify-between z-10">
            <div className="flex items-center gap-2">
              <Tag className="w-4.5 h-4.5 text-brand-deep"  strokeWidth={1.5}/>
              <div>
                <h2 className="text-sm font-extrabold text-slate-900">مدل‌های این کالا</h2>
                <p className="text-[11px] text-slate-500 truncate max-w-[240px]">{productTitle}</p>
              </div>
            </div>
            <button type="button" onClick={onClose} className="p-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-600" aria-label="بستن">
              <X className="w-4 h-4"  strokeWidth={1.5}/>
            </button>
          </div>
          <div className="mx-4 mt-3 rounded-2xl bg-gradient-to-l from-brand-deep to-brand p-3.5 text-white">
            <span className="text-[10px] font-bold opacity-80 block">کد کالا (SPN)</span>
            {loading ? (
              <span className="text-xs opacity-70">در حال دریافت…</span>
            ) : (
              <span className="font-mono text-2xl font-black tracking-widest" dir="ltr">{spn || '—'}</span>
            )}
            <span className="text-[10px] opacity-70 block mt-1">{toPersianDigits(variants.length)} تنوع ثبت‌شده</span>
          </div>
          <div className="p-4 space-y-3">
            <div className="grid grid-cols-2 gap-2">
              <button type="button" onClick={() => setFormOpen((v) => !v)} className="flex items-center justify-center gap-1.5 py-2.5 rounded-xl bg-brand-deep text-white text-xs font-bold hover:bg-brand transition-colors">
                <Plus className="w-4 h-4"  strokeWidth={1.5}/><span>مدل تازه</span>
              </button>
              <button type="button" onClick={() => setPrintOpen(true)} disabled={labels.length === 0} className="flex items-center justify-center gap-1.5 py-2.5 rounded-xl bg-brand-soft text-brand-deep text-xs font-bold hover:bg-brand-border/40 transition-colors disabled:opacity-50">
                <Printer className="w-4 h-4"  strokeWidth={1.5}/><span>چاپ برچسب</span>
              </button>
            </div>
            {formOpen && mode === 'single' && (
              <form onSubmit={submitSingle} className="rounded-2xl border border-brand-border bg-brand-bg p-3 space-y-2.5">
                <div className="flex bg-white p-1 rounded-xl border border-slate-200 text-[11px] font-bold gap-1">
                  <button type="button" onClick={() => setMode('single')} className="flex-1 py-1.5 rounded-lg transition-all bg-brand-deep text-white shadow-2xs">ثبت تکی</button>
                  <button type="button" onClick={() => setMode('bulk')} className="flex-1 py-1.5 rounded-lg transition-all text-slate-600">ثبت گروهی چند تنوع</button>
                </div>
                <input value={singleName} onChange={(e) => setSingleName(e.target.value)} placeholder="نام تنوع (مثلاً: آبی / سایز ۴۲)" className={`w-full ${inputCls}`} />
                <div className="grid grid-cols-2 gap-2">
                  {ATTR_KEYS.map((k) => (
                    <input key={k} value={singleAttrs[k] || ''} onChange={(e) => setSingleAttrs((prev) => ({ ...prev, [k]: e.target.value }))} placeholder={k} className={inputCls} />
                  ))}
                </div>
                {singleCustom.map((c, idx) => (
                  <div key={idx} className="flex items-center gap-2">
                    <input value={c.key} onChange={(e) => setSingleCustom((prev) => prev.map((row, j) => (j === idx ? { ...row, key: e.target.value } : row)))} placeholder="ویژگی (مثلاً جنس)" className={`flex-1 ${inputCls}`} />
                    <input value={c.value} onChange={(e) => setSingleCustom((prev) => prev.map((row, j) => (j === idx ? { ...row, value: e.target.value } : row)))} placeholder="مقدار" className={`flex-1 ${inputCls}`} />
                    <button type="button" onClick={() => setSingleCustom((prev) => prev.filter((_, j) => j !== idx))} className="p-2 rounded-lg bg-rose-50 text-rose-600" aria-label="حذف ویژگی">
                      <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                    </button>
                  </div>
                ))}
                <button type="button" onClick={() => setSingleCustom((prev) => [...prev, { key: '', value: '' }])} className="text-[11px] font-bold text-brand-deep hover:underline">+ افزودن ویژگی دلخواه</button>
                <button type="submit" disabled={singleSaving} className="w-full py-2.5 rounded-xl bg-brand-deep text-white text-xs font-bold hover:bg-brand transition-colors disabled:opacity-50 flex items-center justify-center gap-1.5">
                  {singleSaving && <Loader2 className="w-4 h-4 animate-spin"  strokeWidth={1.5}/>}
                  <span>{singleSaving ? 'در حال ثبت…' : 'ثبت تنوع'}</span>
                </button>
              </form>
            )}
            {formOpen && mode === 'bulk' && (
              <form onSubmit={submitBulk} className="rounded-2xl border border-brand-border bg-brand-bg p-3 space-y-2.5">
                <div className="flex bg-white p-1 rounded-xl border border-slate-200 text-[11px] font-bold gap-1">
                  <button type="button" onClick={() => setMode('single')} className="flex-1 py-1.5 rounded-lg transition-all text-slate-600">ثبت تکی</button>
                  <button type="button" onClick={() => setMode('bulk')} className="flex-1 py-1.5 rounded-lg transition-all bg-brand-deep text-white shadow-2xs">ثبت گروهی چند تنوع</button>
                </div>
                <p className="text-[10px] text-slate-500 leading-relaxed">همه‌ی تنوع‌های این فهرست با یک درخواست برای بازبینی مدیر ارسال می‌شوند و با هم تأیید یا رد می‌شوند. موجودی هر تنوع پس از تأیید، از مسیر «ارسال محموله به انبار» ثبت می‌شود.</p>
                {bulkRows.map((row, idx) => (
                  <div key={idx} className="rounded-xl border border-slate-200 bg-white p-2.5 space-y-2">
                    <div className="flex items-center gap-2">
                      <span className="text-[10px] font-black text-slate-400 shrink-0">{toPersianDigits(idx + 1)}</span>
                      <input value={row.name} onChange={(e) => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, name: e.target.value } : r)))} placeholder="نام تنوع (مثلاً: آبی / سایز ۴۲)" className={`flex-1 ${inputCls}`} />
                      {bulkRows.length > 1 && (
                        <button type="button" onClick={() => setBulkRows((prev) => prev.filter((_, j) => j !== idx))} className="p-2 rounded-lg bg-rose-50 text-rose-600 shrink-0" aria-label="حذف ردیف">
                          <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                        </button>
                      )}
                    </div>
                    <div className="grid grid-cols-2 gap-2">
                      <input value={row.color} onChange={(e) => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, color: e.target.value } : r)))} placeholder="رنگ" className={inputCls} />
                      <input value={row.size} onChange={(e) => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, size: e.target.value } : r)))} placeholder="سایز" className={inputCls} />
                    </div>
                    {row.custom.map((c, cIdx) => (
                      <div key={cIdx} className="flex items-center gap-2">
                        <input value={c.key} onChange={(e) => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, custom: r.custom.map((cc, k) => (k === cIdx ? { ...cc, key: e.target.value } : cc)) } : r)))} placeholder="ویژگی (مثلاً جنس)" className={`flex-1 ${inputCls}`} />
                        <input value={c.value} onChange={(e) => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, custom: r.custom.map((cc, k) => (k === cIdx ? { ...cc, value: e.target.value } : cc)) } : r)))} placeholder="مقدار" className={`flex-1 ${inputCls}`} />
                        <button type="button" onClick={() => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, custom: r.custom.filter((_, k) => k !== cIdx) } : r)))} className="p-2 rounded-lg bg-rose-50 text-rose-600" aria-label="حذف ویژگی">
                          <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                        </button>
                      </div>
                    ))}
                    <button type="button" onClick={() => setBulkRows((prev) => prev.map((r, j) => (j === idx ? { ...r, custom: [...r.custom, { key: '', value: '' }] } : r)))} className="text-[11px] font-bold text-brand-deep hover:underline">+ افزودن ویژگی دلخواه</button>
                  </div>
                ))}
                <button type="button" disabled={bulkRows.length >= MAX_BULK} onClick={() => setBulkRows((prev) => [...prev, emptyBulkRow()])} className="w-full py-2 rounded-xl border border-dashed border-brand-border text-brand-deep text-[11px] font-bold disabled:opacity-40">
                  + افزودن ردیف تنوع ({toPersianDigits(bulkRows.length)} از {toPersianDigits(MAX_BULK)})
                </button>
                <button type="submit" disabled={bulkSaving} className="w-full py-2.5 rounded-xl bg-brand-deep text-white text-xs font-bold hover:bg-brand transition-colors disabled:opacity-50 flex items-center justify-center gap-1.5">
                  {bulkSaving && <Loader2 className="w-4 h-4 animate-spin"  strokeWidth={1.5}/>}
                  <span>{bulkSaving ? 'در حال ارسال…' : 'ارسال گروهی برای بازبینی'}</span>
                </button>
              </form>
            )}
            {loading ? (
              <div className="text-center text-xs text-slate-500 py-8">در حال دریافت تنوع‌ها…</div>
            ) : variants.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-slate-300 p-6 text-center text-xs text-slate-500">هنوز مدلی برای این کالا نساخته‌ای. با «مدل تازه» اولین مدل را بساز.</div>
            ) : (
              <div className="space-y-2">
                {variants.map((v) => (
                  <div key={v.id} className="rounded-2xl border border-slate-200 bg-white p-3 space-y-1.5">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-extrabold text-slate-900">{v.variant_name}</span>
                      {v.is_default && <span className="text-[10px] font-bold bg-brand-soft text-brand-deep px-2 py-0.5 rounded-full">پیش‌فرض</span>}
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-sm font-black text-brand-deep tracking-wider" dir="ltr">{v.spnc}</span>
                      <span className="text-[11px] text-slate-500">موجودی: <strong className="text-slate-800">{toPersianDigits(v.stock ?? 0)}</strong></span>
                    </div>
                    {v.attributes && Object.keys(v.attributes).length > 0 && (
                      <div className="flex flex-wrap gap-1.5">
                        {Object.entries(v.attributes).map(([k, val]) => (
                          <span key={k} className="text-[10px] bg-slate-100 text-slate-600 px-2 py-0.5 rounded-full">{k}: {val}</span>
                        ))}
                      </div>
                    )}
                    <VariantCartLimits productId={productId} variant={v} onSaved={(updated) => setVariants((prev) => prev.map((row) => (row.id === updated.id ? updated : row)))} />
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
      {printOpen && (
        <LabelPrintPreview spn={spn} productTitle={productTitle} labels={labels} stockBySpnc={stockBySpnc} onClose={() => setPrintOpen(false)} />
      )}
    </>
  );
};
