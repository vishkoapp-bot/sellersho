import { SupplierGranularFeedback } from '../admin/ProductFieldReview';
import React, { useState, useEffect, useCallback } from 'react';
import { useToast } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { Loader2, CheckCircle2, XCircle, Clock, AlertTriangle } from 'lucide-react';

// Live manager-feedback modal (zl): per-field review dossier for one
// proposal + resubmission of rejected fields. Field rows follow live's
// yl list (CatalogReviewDossier). Newline is the separator for specs,
// images and variants lines (live joins/splits on newline).
const FIELDS = [
  { key: 'title', label: 'نام محصول' },
  { key: 'images', label: 'تصاویر' },
  { key: 'description', label: 'توضیحات' },
  { key: 'specs', label: 'مشخصات فنی' },
  { key: 'brand', label: 'برند' },
  { key: 'price_c', label: 'قیمت خرید (C)' },
  { key: 'stock', label: 'موجودی اولیه' },
  { key: 'variants', label: 'تنوع‌ها' },
  { key: 'category', label: 'دسته‌بندی' },
  { key: 'warranty', label: 'گارانتی' },
];

const formatNumber = (raw: string): string => {
  const digits = raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
  if (digits === '') return '';
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬').replace(/\d/g, (ch) => '۰۱۲۳۴۵۶۷۸۹'[parseInt(ch, 10)]);
};

const parseDigits = (raw: string): string =>
  raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');

const strOf = (v: unknown): string => (v === undefined || v === null ? '' : String(v));

const proposalValue = (proposal: any, key: string): string => {
  const get = (k: string) => (proposal && proposal[k] !== undefined && proposal[k] !== null ? proposal[k] : '');
  switch (key) {
    case 'title':
      return strOf(get('title'));
    case 'description':
      return strOf(get('description') || get('desc'));
    case 'price_c':
      return strOf(get('supplierBaseCost') ?? get('price_c') ?? get('cost'));
    case 'stock':
      return strOf(get('stock') ?? get('initial_stock'));
    case 'barcode':
      return strOf(get('barcode'));
    case 'category':
      return strOf(get('categorySlug') || get('category'));
    case 'warranty':
      return strOf(get('warranty'));
    case 'specs': {
      const w = get('specs') || get('attributes') || {};
      if (typeof w === 'object') return Object.entries(w).map(([k, v]) => `${k}: ${v}`).join('\n');
      return strOf(w);
    }
    case 'images': {
      const arr = Array.isArray(get('imageUrls')) ? get('imageUrls') : Array.isArray(get('images')) ? get('images') : [];
      const single = get('imageUrl') || get('image_url') || get('image');
      const all = arr.length ? arr : single ? [single] : [];
      return all.join('\n');
    }
    case 'variants': {
      const w = get('variants');
      if (!Array.isArray(w)) return '';
      return w.map((v: any) => {
        const attrs = v.attributes && typeof v.attributes === 'object' ? Object.entries(v.attributes).map(([k, val]) => `${k}: ${val}`).join('، ') : '';
        const cost = v.supplier_base_cost ?? v.supplierBaseCost ?? null;
        return `${v.variant_name || v.name || ''} | ${v.stock ?? 0}${cost !== null && cost !== '' ? ` | ${cost}` : ''}${attrs ? ` | ${attrs}` : ''}`;
      }).join('\n');
    }
    default:
      return '';
  }
};

const parseVariantsText = (text: string): any[] => {
  const out: any[] = [];
  text.split('\n').forEach((line) => {
    const parts = line.split('|').map((p) => p.trim());
    const name = parts[0] || '';
    if (!name) return;
    const stockDigits = parseDigits(parts[1] || '');
    const stock = stockDigits ? parseInt(stockDigits, 10) : 0;
    const attrs: Record<string, string> = {};
    let cost: number | undefined;
    for (const part of parts.slice(2)) {
      if (!part) continue;
      if (part.includes(':')) {
        part.split('،').forEach((pair) => {
          const i = pair.indexOf(':');
          if (i > 0) {
            const k = pair.slice(0, i).trim();
            const v = pair.slice(i + 1).trim();
            if (k && v) attrs[k] = v;
          }
        });
      } else {
        const d = parseDigits(part);
        const n = d ? parseInt(d, 10) : NaN;
        if (Number.isSafeInteger(n) && n > 0) cost = n;
      }
    }
    out.push({ variant_name: name, attributes: attrs, stock: Number.isSafeInteger(stock) && stock >= 0 ? stock : 0, ...(cost !== undefined ? { supplier_base_cost: cost } : {}) });
  });
  return out;
};

export const SupplierFeedbackModal: React.FC<{
  proposalId: string;
  onClose: () => void;
  onResubmitted?: () => void;
}> = ({ proposalId, onClose, onResubmitted }) => {
  const { success, error } = useToast();
  const [loading, setLoading] = useState(true);
  const [proposal, setProposal] = useState<any>(null);
  const [fields, setFields] = useState<any[]>([]);
  const [history, setHistory] = useState<any[]>([]);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data: any = await supplierApi.getCatalogReview(proposalId);
      setProposal(data.proposal);
      setFields(data.fields || []);
      setHistory(data.history || []);
    } catch {
      error('خطا در بارگذاری.');
    } finally {
      setLoading(false);
    }
  }, [proposalId, error]);

  useEffect(() => { load(); }, [load]);

  const fieldByKey = Object.fromEntries(fields.map((f) => [f.field_key, f]));
  const rejected = fields.filter((f) => f.decision === 'rejected');
  const status = proposal?.status;

  const editorFor = (key: string) => {
    const value = edits[key] !== undefined ? edits[key] : proposalValue(proposal, key);
    const set = (v: string) => setEdits((prev) => ({ ...prev, [key]: v }));
    const base = 'w-full rounded-xl border border-slate-300 px-3 py-2 text-sm mt-2 bg-white';
    switch (key) {
      case 'description':
        return <textarea className={`${base} min-h-[90px] leading-6`} value={value} onChange={(e) => set(e.target.value)} placeholder="متن اصلاح‌شده توضیحات…" />;
      case 'price_c':
      case 'stock':
        return <input type="text" inputMode="numeric" min="0" className={base} value={formatNumber(value)} onChange={(e) => set(e.target.value)} dir="ltr" placeholder="عدد اصلاح‌شده" />;
      case 'specs':
        return <textarea className={`${base} min-h-[90px] font-mono`} value={value} onChange={(e) => set(e.target.value)} dir="ltr" placeholder="هر مشخصات در یک خط — قالب: نام: مقدار" />;
      case 'images':
        return <textarea className={`${base} min-h-[70px] font-mono`} value={value} onChange={(e) => set(e.target.value)} dir="ltr" placeholder="آدرس اینترنتی هر تصویر در یک خط" />;
      case 'variants':
        return <textarea className={`${base} min-h-[90px] font-mono`} value={value} onChange={(e) => set(e.target.value)} dir="rtl" placeholder="هر تنوع در یک خط — قالب: نام تنوع | موجودی | قیمت عمده (تومان، اختیاری) | رنگ: مقدار، سایز: مقدار" />;
      case 'category':
        return <input className={base} value={value} onChange={(e) => set(e.target.value)} placeholder="شناسه دسته‌بندی" />;
      default:
        return <input className={base} value={value} onChange={(e) => set(e.target.value)} placeholder="مقدار اصلاح‌شده…" />;
    }
  };

  const resubmit = async () => {
    if (rejected.length === 0) {
      error('فیلد ردشده‌ای برای اصلاح وجود ندارد.');
      return;
    }
    const data: Record<string, unknown> = {};
    for (const f of rejected) {
      const key = f.field_key;
      if (edits[key] === undefined) continue;
      const raw = edits[key].trim();
      switch (key) {
        case 'title':
          if (raw) data.title = raw;
          break;
        case 'description':
          if (raw) data.description = raw;
          break;
        case 'price_c': {
          const n = Number(raw);
          if (raw && Number.isFinite(n) && n >= 0) data.supplierBaseCost = n;
          break;
        }
        case 'stock': {
          const n = parseInt(parseDigits(raw), 10);
          if (raw && Number.isSafeInteger(n) && n >= 0) data.stock = n;
          break;
        }
        case 'barcode':
          if (raw) data.barcode = raw;
          break;
        case 'category':
          if (raw) data.categorySlug = raw;
          break;
        case 'warranty':
          if (raw) data.warranty = raw;
          break;
        case 'specs': {
          const obj: Record<string, string> = {};
          raw.split('\n').forEach((line) => {
            const i = line.indexOf(':');
            if (i > 0) {
              const k = line.slice(0, i).trim();
              const v = line.slice(i + 1).trim();
              if (k) obj[k] = v;
            }
          });
          data.specs = obj;
          break;
        }
        case 'images': {
          const lines = raw.split('\n').map((l) => l.trim()).filter(Boolean);
          if (lines.length > 0) {
            data.imageUrls = lines;
            data.imageUrl = lines[0];
          }
          break;
        }
        case 'variants':
          data.variants = parseVariantsText(raw);
          break;
        default:
          break;
      }
    }
    if (Object.keys(data).length === 0) {
      error('حداقل یک فیلد ردشده را اصلاح کنید تا ارسال مجدد انجام شود.');
      return;
    }
    setSubmitting(true);
    try {
      await supplierApi.resubmitCatalogReview(proposalId, data);
      success('اصلاحات شما برای بررسی مجدد ارسال شد.');
      onResubmitted?.();
      onClose();
    } catch {
      error('خطا در ارسال مجدد.');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return (
      <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center" dir="rtl">
        <div className="flex items-center justify-center p-12 bg-white rounded-2xl">
          <Loader2 className="w-8 h-8 animate-spin text-violet-600"  strokeWidth={1.5}/>
        </div>
      </div>
    );
  }

  if (fields.some(f => String(f.field_key).startsWith('/'))) return <SupplierGranularFeedback proposal={proposal} fields={fields} onClose={onClose} onResubmitted={onResubmitted}/>;

  return (
    <div className="fixed inset-0 z-50 bg-black/50 flex items-stretch justify-center" dir="rtl">
      <div className="bg-white w-full max-w-2xl my-0 md:my-6 rounded-none md:rounded-2xl overflow-hidden flex flex-col">
        <div className="px-5 py-4 border-b bg-gradient-to-l from-amber-50 to-white">
          <h2 className="font-bold text-lg flex items-center gap-2">
            <AlertTriangle className="w-5 h-5 text-amber-600"  strokeWidth={1.5}/>
            نتیجه بررسی مدیر
          </h2>
          <p className="text-sm text-stone-500 mt-0.5">{proposal?.title}</p>
          {status === 'rejected' && (
            <div className="mt-2 flex items-center gap-2 text-sm bg-red-50 border border-red-200 rounded-xl px-3 py-2">
              <XCircle className="w-4 h-4 text-red-600 shrink-0"  strokeWidth={1.5}/>
              <span>این محصول رد شد. {rejected.length} فیلد نیاز به اصلاح دارد.</span>
            </div>
          )}
          {status === 'approved' && (
            <div className="mt-2 flex items-center gap-2 text-sm bg-emerald-50 border border-emerald-200 rounded-xl px-3 py-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0"  strokeWidth={1.5}/>
              <span>تبریک! محصول تأیید و به کاتالوگ اضافه شد.</span>
            </div>
          )}
        </div>
        <div className="flex-1 overflow-y-auto divide-y">
          {FIELDS.map((field) => {
            const f = fieldByKey[field.key];
            const decision = f?.decision || 'pending';
            return (
              <div key={field.key} className={`px-5 py-3.5 ${decision === 'rejected' ? 'bg-red-50/60' : ''}`}>
                <div className="flex items-center justify-between mb-1.5">
                  <p className="font-bold text-sm">{field.label}</p>
                  {decision === 'approved' && (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-emerald-100 text-emerald-700 flex items-center gap-1">
                      <CheckCircle2 className="w-3 h-3"  strokeWidth={1.5}/>
                      تأیید شد
                    </span>
                  )}
                  {decision === 'rejected' && (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-red-100 text-red-700 flex items-center gap-1">
                      <XCircle className="w-3 h-3"  strokeWidth={1.5}/>
                      نیاز به اصلاح
                    </span>
                  )}
                  {decision === 'pending' && (
                    <span className="text-xs px-2 py-0.5 rounded-full bg-stone-100 text-stone-500 flex items-center gap-1">
                      <Clock className="w-3 h-3"  strokeWidth={1.5}/>
                      در انتظار بررسی
                    </span>
                  )}
                </div>
                {decision === 'rejected' && f?.reason && (
                  <div className="mt-2 text-sm bg-white border border-red-200 rounded-xl px-3 py-2">
                    <p className="text-xs font-bold text-red-600 mb-1">دلیل مدیر:</p>
                    <p className="leading-6">{f.reason}</p>
                  </div>
                )}
                {decision === 'rejected' && status === 'rejected' && (
                  <div className="mt-2 bg-white border border-amber-200 rounded-xl px-3 py-2.5">
                    <p className="text-xs font-bold text-amber-700 mb-0.5">اصلاح این فیلد:</p>
                    {editorFor(field.key)}
                  </div>
                )}
              </div>
            );
          })}
          {history.length > 0 && (
            <div className="px-5 py-4 bg-stone-50">
              <p className="font-bold text-sm mb-2">سوابق</p>
              {history.map((h: any, idx: number) => (
                <p key={idx} className="text-xs text-stone-500 py-0.5">
                  {new Date(h.created_at).toLocaleString('fa-IR')} — {h.action === 'resubmitted' ? 'ارسال مجدد شما' : h.action}{h.field_key ? ` (${h.field_key})` : ''}{h.reason ? `: ${h.reason}` : ''}
                </p>
              ))}
            </div>
          )}
        </div>
        <div className="border-t px-5 py-3 flex justify-between bg-white">
          <button type="button" onClick={onClose} className="px-4 py-2 text-sm rounded-xl border">بستن</button>
          {status === 'rejected' && (
            <button type="button" onClick={resubmit} disabled={submitting} className="px-5 py-2 text-sm font-bold rounded-xl bg-violet-600 text-white flex items-center gap-2 disabled:opacity-50">
              {submitting ? <Loader2 className="w-4 h-4 animate-spin"  strokeWidth={1.5}/> : null}
              اصلاح و ارسال مجدد
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
