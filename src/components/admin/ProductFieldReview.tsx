import React, { useEffect, useState } from 'react';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
const labels: Record<string, string> = { title: 'نام کالا', brand: 'برند', noBrand: 'بدون برند', description: 'توضیحات', specs: 'مشخصات فنی', categorySlug: 'دسته‌بندی', categoryAttributes: 'ویژگی‌های دسته', nicheIds: 'موضوع کالا', barcode: 'بارکد', warranty: 'گارانتی', imageUrl: 'تصویر اصلی', imageUrls: 'تصاویر', stock: 'موجودی', supplierBaseCost: 'قیمت عمده (تومان)', supplier_base_cost: 'قیمت عمده (تومان)', variants: 'تنوع', variant_name: 'نام تنوع', attributes: 'ویژگی', productType: 'نوع محصول', fulfillmentMethod: 'روش ارسال', supplierWarehouseId: 'انبار تأمین‌کننده', weightGrams: 'وزن (گرم)', lengthMm: 'طول (میلی‌متر)', widthMm: 'عرض (میلی‌متر)', heightMm: 'ارتفاع (میلی‌متر)', packagingLengthMm: 'طول بسته', packagingWidthMm: 'عرض بسته', packagingHeightMm: 'ارتفاع بسته', preparationDays: 'آماده‌سازی (روز)', isOriginal: 'اصالت کالا', requestedAttributes: 'ویژگی پیشنهادی', colorId: 'رنگ', brandId: 'برند' };
Object.assign(labels, { category: labels.categorySlug, supplier_base_cost: labels.supplierBaseCost, supplier_warranty: labels.warranty, preparation_days: labels.preparationDays, images: labels.imageUrls, image: labels.imageUrl, image_url: labels.imageUrl, is_original: labels.isOriginal, niche_ids: labels.nicheIds, length_mm: labels.lengthMm, width_mm: labels.widthMm, height_mm: labels.heightMm, weight_grams: labels.weightGrams, warehouse: 'انبار', suggested_platform_margin: 'حاشیه پیشنهادی پلتفرم', color: 'رنگ', size: 'سایز' });
export const reviewLabel = (key: string) => key.split('/').slice(1).map(k => { k = k.replace(/~1/g, '/').replace(/~0/g, '~'); return /^\d+$/.test(k) ? String(Number(k) + 1) : (Object.prototype.hasOwnProperty.call(labels, k) ? labels[k] : k); }).join(' / ');
export const pointerValue = (data: any, key: string) => key.split('/').slice(1).reduce((v, k) => v?.[k.replace(/~1/g, '/').replace(/~0/g, '~')], data);
export type ReviewField = {
    field_key: string;
    value: any;
    decision: string;
    reason?: string;
    value_hash: string;
};
const valueLabels: Record<string, string> = { simple: 'تکی', variable: 'متغیر', warehouse: 'انبار مرکزی', supplier_direct: 'ارسال مستقیم' };
const shown = (value: any): string => value === null || value === undefined ? 'ثبت نشده' : typeof value === 'boolean' ? value ? 'بله' : 'خیر' : typeof value === 'object' ? Object.keys(value).length ? Object.entries(value).map(([k, v]) => `${labels[k] || k}: ${String(v)}`).join('\n') : 'ثبت نشده' : (typeof value === 'string' && Object.prototype.hasOwnProperty.call(valueLabels, value) ? valueLabels[value] : String(value));
function useReviewReferences(proposal: any) {
    const [refs, setRefs] = useState<any>({});
    useEffect(() => {
        let active = true;
        setRefs({});
        if (!proposal)
            return;
        Promise.all([apiRequest<any[]>('/categories'), apiRequest<any[]>('/niches'), proposal.categorySlug ? apiRequest<any>(`/categories/${encodeURIComponent(proposal.categorySlug)}/attributes`) : Promise.resolve({})]).then(([categories, niches, facets]) => { if (active)
            setRefs({ categories, niches, facets }); }).catch(() => { });
        return () => { active = false; };
    }, [proposal?.categorySlug]);
    return refs;
}
function fieldCaption(field: ReviewField, refs: any) { const key = field.field_key; const match = key.match(/^\/categoryAttributes\/attributes\/([^/]+)/); if (match) {
    const attr = refs.facets?.attributes?.find((a: any) => String(a.id) === match[1]);
    if (attr)
        return 'ویژگی دسته / ' + attr.title;
} return reviewLabel(key); }
function fieldChoices(field: ReviewField, refs: any): {
    label: string;
    value: any;
}[] {
    const key = field.field_key;
    const match = key.match(/^\/categoryAttributes\/attributes\/([^/]+)/);
    if (match)
        return (refs.facets?.attributes?.find((a: any) => String(a.id) === match[1])?.values || []).map((v: any) => ({ label: v.titleFa, value: v.id }));
    if (key === '/categoryAttributes/colorId')
        return (refs.facets?.colors || []).map((v: any) => ({ label: v.title, value: v.id }));
    if (key === '/categoryAttributes/brandId')
        return (refs.facets?.brands || []).map((v: any) => ({ label: v.titleFa, value: v.id }));
    if (key === '/brand')
        return (refs.facets?.brands || []).map((v: any) => ({ label: v.titleFa, value: v.titleFa }));
    if (key === '/categorySlug')
        return (refs.categories || []).map((v: any) => ({ label: v.title, value: v.slug }));
    if (key.startsWith('/nicheIds/'))
        return (refs.niches || []).map((v: any) => ({ label: v.title, value: v.id }));
    if (/^\/variants\/\d+\/attributes\//.test(key)) {
        const name = key.split('/').pop()?.replace(/~1/g, '/').replace(/~0/g, '~');
        if (name === 'رنگ' || name === 'color')
            return (refs.facets?.colors || []).map((v: any) => ({ label: v.title, value: v.title }));
        return (refs.facets?.attributes?.find((a: any) => a.title === name)?.values || []).map((v: any) => ({ label: v.titleFa, value: v.titleFa }));
    }
    return [];
}
function displayReference(field: ReviewField, refs: any) {
    const key = field.field_key;
    let result: any;
    if (key === '/categorySlug')
        result = refs.categories?.find((c: any) => c.slug === field.value)?.title;
    if (key.startsWith('/nicheIds/'))
        result = refs.niches?.find((n: any) => n.id === field.value)?.title;
    if (key === '/categoryAttributes/colorId')
        result = refs.facets?.colors?.find((n: any) => n.id === field.value)?.title;
    if (key === '/categoryAttributes/brandId')
        result = refs.facets?.brands?.find((n: any) => n.id === field.value)?.titleFa;
    const match = key.match(/^\/categoryAttributes\/attributes\/([^/]+)/);
    if (match)
        result = refs.facets?.attributes?.find((a: any) => String(a.id) === match[1])?.values?.find((v: any) => v.id === field.value)?.titleFa;
    return result;
}
function FieldValue({ field, refs = {} }: {
    field: ReviewField;
    refs?: any;
}) { const value = displayReference(field, refs) ?? field.value; return /^\/(?:imageUrls|images)\/\d+$|^\/(?:imageUrl|image_url|image)$/.test(field.field_key) && typeof value === 'string' && /^(https?:\/\/|\/static\/)/.test(value) ? <a href={value} target="_blank" rel="noreferrer"><img src={value} alt="تصویر کالا" className="max-h-48 max-w-full rounded-xl object-contain"/></a> : <p className="whitespace-pre-wrap break-words text-sm leading-7">{typeof value === 'number' ? value.toLocaleString('fa-IR') : shown(value)}</p>; }
export function ProductFieldReview({ id, onClose, onUpdated }: {
    id: string;
    onClose: () => void;
    onUpdated: () => void;
}) {
    const { error, success } = useToast();
    const [fields, setFields] = useState<ReviewField[]>([]);
    const [title, setTitle] = useState('');
    const [proposal, setProposal] = useState<any>(null);
    const refs = useReviewReferences(proposal);
    const [busy, setBusy] = useState(true);
    const [loadError, setLoadError] = useState('');
    const load = async () => { setBusy(true); setLoadError(''); try {
        const d = await apiRequest<any>(`/admin/product-field-reviews/${encodeURIComponent(id)}`, { auth: true });
        setFields(d.fields);
        setProposal(d.proposal);
        setTitle(d.proposal.title);
    }
    catch (e) {
        setLoadError(e instanceof Error ? e.message : 'بارگذاری ناموفق بود');
    }
    finally {
        setBusy(false);
    } };
    useEffect(() => { void load(); }, [id]);
    const save = async (publish = false) => { if (busy)
        return; if (fields.some(f => f.decision === 'rejected' && !f.reason?.trim())) {
        error('برای هر مورد ردشده دلیل بنویسید.');
        return;
    } setBusy(true); try {
        const d = await apiRequest<any>(`/admin/product-field-reviews/${encodeURIComponent(id)}`, { method: 'PUT', auth: true, body: JSON.stringify({ fields }) });
        setFields(d.fields);
        if (publish) {
            await apiRequest(`/admin/approvals/${encodeURIComponent(id)}/review`, { method: 'POST', auth: true, body: JSON.stringify({ status: 'approved' }) });
            success('محصول پس از بازبینی همهٔ فیلدها منتشر شد.');
            onUpdated();
            onClose();
        }
        else {
            success(fields.some(f => f.decision === 'rejected') ? 'دلایل برای اصلاح به تأمین‌کننده ارسال شد.' : 'تصمیم‌ها ذخیره شد.');
            onUpdated();
        }
    }
    catch (e) {
        error(e instanceof Error ? e.message : 'ذخیره ناموفق بود');
    }
    finally {
        setBusy(false);
    } };
    return <div className="fixed inset-0 z-50 bg-slate-900/60 flex items-center justify-center p-3" dir="rtl"><section role="dialog" aria-label="بازبینی فیلدی محصول" className="bg-white rounded-2xl w-full max-w-4xl max-h-[92vh] flex flex-col"><header className="p-4 border-b flex justify-between"><h3 className="font-bold">بازبینی: {title}</h3><button onClick={onClose} disabled={busy}>بستن</button></header><div className="overflow-auto p-4 space-y-3">
 {busy && <p role="status">در حال دریافت یا ذخیره…</p>}
 {proposal?.productType && <div className="rounded-xl bg-slate-50 p-3 text-xs leading-7"><strong>خلاصهٔ محاسبه‌شده از تنوع‌ها</strong><p>مجموع موجودی: {proposal.stock} — قیمت مبنا: {proposal.supplierBaseCost}</p><p>قیمت و موجودی را در ردیف هر تنوع بررسی کنید؛ جمع‌ها خودکار به‌روز می‌شوند.</p>{proposal.variants?.map((v: any, i: number) => <p key={i}>تنوع {i + 1}: {v.variant_name}</p>)}</div>}
 {loadError ? <div role="alert">{loadError}<button onClick={() => void load()}>تلاش مجدد</button></div> : fields.map((f, i) => <article data-field-key={f.field_key} key={f.field_key} className={`rounded-xl border p-3 space-y-2 ${f.decision === 'rejected' ? 'border-rose-300 bg-rose-50' : 'border-slate-200'}`}><h4 className="font-bold text-xs">{fieldCaption(f, refs)}</h4><FieldValue field={f} refs={refs}/><label className="text-xs block">نتیجهٔ بازبینی<select aria-label={`نتیجه ${reviewLabel(f.field_key)}`} disabled={busy || proposal?.status === 'approved'} value={f.decision} onChange={e => setFields(p => p.map((v, j) => j === i ? { ...v, decision: e.target.value } : v))} className="border rounded-lg p-2 mx-2"><option value="pending">در انتظار بررسی</option><option value="approved">تأیید</option><option value="rejected">رد و درخواست اصلاح</option></select></label>{f.decision === 'rejected' && <textarea aria-label={`دلیل رد ${reviewLabel(f.field_key)}`} value={f.reason || ''} disabled={busy} onChange={e => setFields(p => p.map((v, j) => j === i ? { ...v, reason: e.target.value } : v))} placeholder="دلیل رد و توضیح اصلاح موردنیاز — الزامی" className="w-full border rounded-lg p-2 text-sm"/>}</article>)}
 </div><footer className="border-t p-3 flex flex-wrap gap-2 text-xs"><span>{fields.filter(f => f.decision === 'approved').length} از {fields.length} مورد تأیید شده</span><button disabled={busy || proposal?.status === 'approved' || !!loadError || !fields.length} onClick={() => void save()} className="bg-slate-800 text-white rounded-lg p-3">ذخیره / ارسال موارد ردشده</button><button disabled={busy || proposal?.status === 'approved' || !!loadError || !fields.length || fields.some(f => f.decision !== 'approved')} onClick={() => void save(true)} className="bg-emerald-700 text-white rounded-lg p-3 disabled:opacity-40">تأیید نهایی و انتشار</button></footer></section></div>;
}
export function SupplierGranularFeedback({ proposal, fields: initial, onClose, onResubmitted }: {
    proposal: any;
    fields: any[];
    onClose: () => void;
    onResubmitted?: () => void;
}) {
    const { error, success } = useToast();
    const [edits, setEdits] = useState<Record<string, any>>({});
    const [texts, setTexts] = useState<Record<string, string>>({});
    const [busy, setBusy] = useState(false);
    const refs = useReviewReferences(proposal);
    const fields: ReviewField[] = initial.filter(f => f.field_key.startsWith('/')).map(f => ({ ...f, value: pointerValue(proposal, f.field_key) }));
    const rejected=fields.filter(f=>f.decision==='rejected');
    const other=fields.filter(f=>f.decision!=='rejected');
    const changed=Object.fromEntries(Object.entries(edits).filter(([key,value])=>JSON.stringify(value)!==JSON.stringify(pointerValue(proposal,key))));
    const renderField=(f:ReviewField)=>(<article data-field-key={f.field_key} key={f.field_key} className={`rounded-2xl border p-4 space-y-3 ${f.decision === 'rejected' ? 'border-amber-200 bg-white shadow-sm' : 'border-slate-200 bg-slate-50'}`}><h4 className="text-xs font-bold">{fieldCaption(f, refs)}</h4><div className="rounded-xl bg-slate-50 p-3"><p className="text-[11px] font-bold text-slate-500 mb-1">مقدار فعلی</p><FieldValue field={f} refs={refs}/></div><p className="text-xs">{f.decision === 'approved' ? 'تأیید شده' : f.decision === 'rejected' ? 'نیاز به اصلاح' : 'در انتظار بازبینی'}</p>{f.decision === 'rejected' && <><div className="rounded-xl border border-amber-200 bg-amber-50 p-3"><p className="text-xs font-bold text-amber-900 mb-1">نظر بازبین</p><p className="text-sm leading-7 text-amber-950">{f.reason}</p></div><p className="text-xs font-bold">مقدار اصلاح‌شده</p>{fieldChoices(f, refs).length ? <select aria-label={reviewLabel(f.field_key)} disabled={busy} className="w-full border rounded-lg p-2 text-sm" value={String(edits[f.field_key] ?? f.value ?? '')} onChange={e => { const selected = fieldChoices(f, refs).find(v => String(v.value) === e.target.value); if (selected)
        setEdits(p => ({ ...p, [f.field_key]: selected.value })); }}><option value={String(f.value ?? '')}>{shown(displayReference(f, refs) ?? f.value)}</option>{fieldChoices(f, refs).filter(v => String(v.value) !== String(f.value ?? '')).map(v => <option key={String(v.value)} value={String(v.value)}>{v.label}</option>)}</select> : typeof f.value === 'boolean' ? <select aria-label={reviewLabel(f.field_key)} value={String(edits[f.field_key] ?? f.value)} onChange={e => setEdits(p => ({ ...p, [f.field_key]: e.target.value === 'true' }))}><option value="true">بله</option><option value="false">خیر</option></select> : <textarea aria-label={reviewLabel(f.field_key)} disabled={busy} rows={f.field_key === '/description' ? 6 : 2} className="w-full border border-slate-300 rounded-xl p-3 text-sm leading-7 focus:border-violet-500 outline-none" value={texts[f.field_key] ?? (typeof f.value === 'object' ? '' : String(f.value ?? ''))} onChange={e => { const text = e.target.value; setTexts(p => ({ ...p, [f.field_key]: text })); let value: any = text; if (typeof f.value === 'number' || /^\/(packagingLengthMm|packagingWidthMm|packagingHeightMm)$/.test(f.field_key))
        value = text === '' ? '' : Number(text.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))));
    else if (f.value && typeof f.value === 'object')
        value = Array.isArray(f.value) ? text.split('\n').filter(Boolean) : Object.fromEntries(text.split('\n').map(l => { const i = l.indexOf(':'); return i < 0 ? [l, ''] : [l.slice(0, i).trim(), l.slice(i + 1).trim()]; })); setEdits(p => ({ ...p, [f.field_key]: value })); }} placeholder="مقدار اصلاح‌شده"/>}</>}</article>);
    const submit = async () => { if (!Object.keys(changed).length)
        return; setBusy(true); try {
        await apiRequest(`/supplier/catalog/review/${encodeURIComponent(proposal.id)}/resubmit`, { method: 'POST', auth: true, body: JSON.stringify({ data: changed }) });
        success('اصلاحات برای بازبینی مجدد ارسال شد.');
        onResubmitted?.();
        onClose();
    }
    catch (e) {
        error(e instanceof Error ? e.message : 'ارسال اصلاحات ناموفق بود');
    }
    finally {
        setBusy(false);
    } };
    return <div className="fixed inset-0 z-50 bg-slate-900/60 flex items-center justify-center p-3" dir="rtl">
      <section role="dialog" aria-label="اصلاح موارد ردشده" className="bg-white rounded-2xl w-full max-w-3xl max-h-[92vh] flex flex-col overflow-hidden">
        <header className="border-b border-slate-200 p-4 sm:p-5 space-y-2"><div className="flex items-start justify-between gap-3"><div><h3 className="font-bold text-lg text-slate-900">اصلاح کالا برای بازبینی مجدد</h3><p className="text-sm text-slate-500 mt-1">{proposal.title}</p></div><button type="button" onClick={onClose} disabled={busy} className="text-xs border rounded-lg px-3 py-2">بستن</button></div></header>
        <div className="overflow-auto p-4 sm:p-5 space-y-4 bg-slate-50/60">
          <div role="status" className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-7"><strong>{rejected.length} مورد نیاز به اصلاح دارد.</strong><p>نظر بازبین را بخوانید، مقدار همان مورد را اصلاح کنید و دوباره بفرستید. نیازی به ثبت مجدد کل محصول نیست.</p></div>
          {rejected.map(renderField)}
          {other.length>0&&<details className="rounded-xl border border-slate-200 bg-white p-3"><summary className="cursor-pointer text-xs font-bold leading-7">سایر اطلاعات کالا ({other.filter(f=>f.decision==='approved').length} تأییدشده، {other.filter(f=>f.decision==='pending').length} در انتظار بررسی)</summary><div className="mt-3 space-y-3">{other.map(renderField)}</div></details>}
        </div>
        <footer className="p-4 border-t bg-white flex flex-wrap items-center justify-between gap-3"><p className="text-xs text-slate-500">{Object.keys(changed).length} از {rejected.length} اصلاح آمادهٔ ارسال</p><button type="button" disabled={busy||!Object.keys(changed).length} onClick={()=>void submit()} className="bg-violet-700 text-white px-4 py-3 rounded-xl text-sm font-bold disabled:opacity-40">{busy?'در حال ارسال…':'ارسال اصلاحات برای بازبینی'}</button></footer>
      </section></div>;
}
