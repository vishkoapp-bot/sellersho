import React, { useEffect, useState } from 'react';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import { pointerValue, schemaReviewCaption, selectionReviewChoices, skuReviewLabel, cartLimitCorrectionValue } from '../../utils/productReview';
import { SupplierProductStructuralCorrection } from '../supplier/SupplierProductStructuralCorrection';
export { pointerValue } from '../../utils/productReview';
const labels: Record<string, string> = { title: 'نام کالا', brand: 'برند', noBrand: 'بدون برند', description: 'توضیحات', specs: 'مشخصات فنی', categorySlug: 'دسته‌بندی', categoryAttributes: 'ویژگی‌های دسته', nicheIds: 'موضوع کالا', barcode: 'بارکد', warranty: 'گارانتی', imageUrl: 'تصویر اصلی', imageUrls: 'تصاویر', stock: 'موجودی', supplierBaseCost: 'قیمت عمده (تومان)', supplier_base_cost: 'قیمت عمده (تومان)', variants: 'تنوع', variant_name: 'نام تنوع', attributes: 'ویژگی', productType: 'نوع محصول', fulfillmentMethod: 'روش ارسال', supplierWarehouseId: 'انبار تأمین‌کننده', weightGrams: 'وزن (گرم)', lengthMm: 'طول (میلی‌متر)', widthMm: 'عرض (میلی‌متر)', heightMm: 'ارتفاع (میلی‌متر)', packagingLengthMm: 'طول بسته', packagingWidthMm: 'عرض بسته', packagingHeightMm: 'ارتفاع بسته', preparationDays: 'آماده‌سازی (روز)', isOriginal: 'اصالت کالا', requestedAttributes: 'ویژگی پیشنهادی', colorId: 'رنگ', brandId: 'برند' };
Object.assign(labels, { category: labels.categorySlug, supplier_base_cost: labels.supplierBaseCost, supplier_warranty: labels.warranty, preparation_days: labels.preparationDays, images: labels.imageUrls, image: labels.imageUrl, image_url: labels.imageUrl, is_original: labels.isOriginal, niche_ids: labels.nicheIds, length_mm: labels.lengthMm, width_mm: labels.widthMm, height_mm: labels.heightMm, weight_grams: labels.weightGrams, warehouse: 'انبار', suggested_platform_margin: 'حاشیه پیشنهادی پلتفرم', color: 'رنگ', size: 'سایز' });
export const reviewLabel = (key: string) => key.split('/').slice(1).map(k => { k = k.replace(/~1/g, '/').replace(/~0/g, '~'); return /^\d+$/.test(k) ? String(Number(k) + 1) : (Object.prototype.hasOwnProperty.call(labels, k) ? labels[k] : k); }).join(' / ');
export type ReviewField = {
    field_key: string;
    value: any;
    decision: string;
    reason?: string;
    value_hash: string;
};
const valueLabels: Record<string, string> = { simple: 'تکی', variable: 'متغیر', warehouse: 'انبار مرکزی', supplier_direct: 'ارسال مستقیم' };
const shown = (value: any): string => value === null || value === undefined ? 'ثبت نشده' : typeof value === 'boolean' ? value ? 'بله' : 'خیر' : Array.isArray(value) ? value.map(shown).join('، ') : typeof value === 'object' ? Object.keys(value).length ? Object.entries(value).map(([k, v]) => `${labels[k] || k}: ${shown(v)}`).join('\n') : 'ثبت نشده' : (typeof value === 'string' && Object.prototype.hasOwnProperty.call(valueLabels, value) ? valueLabels[value] : String(value));
export const isImageReviewField = (fieldKey: string) => /^\/(?:imageUrls|images)\/\d+$|^\/(?:imageUrl|image_url|image)$/.test(fieldKey);
/** Pure grouping helper: maps a review field key to its review section. */
export function fieldSection(fieldKey: string): { id: string; title: string } {
    if (isImageReviewField(fieldKey))
        return { id: 'images', title: 'تصاویر' };
    if (fieldKey.startsWith('/variants') || fieldKey.startsWith('/productSchema'))
        return { id: 'variants', title: 'تنوع و ویژگی‌ها' };
    if (fieldKey === '/supplierBaseCost' || fieldKey === '/supplier_base_cost' || fieldKey === '/suggested_platform_margin')
        return { id: 'pricing', title: 'قیمت‌گذاری' };
    if (['/stock', '/weightGrams', '/weight_grams', '/lengthMm', '/widthMm', '/heightMm', '/length_mm', '/width_mm', '/height_mm', '/packagingLengthMm', '/packagingWidthMm', '/packagingHeightMm', '/preparationDays', '/preparation_days', '/fulfillmentMethod', '/supplierWarehouseId', '/warehouse'].includes(fieldKey))
        return { id: 'logistics', title: 'موجودی و ارسال' };
    return { id: 'identity', title: 'مشخصات کالا' };
}
export const FIELD_SECTION_ORDER: { id: string; title: string }[] = [
    { id: 'identity', title: 'مشخصات کالا' },
    { id: 'images', title: 'تصاویر' },
    { id: 'pricing', title: 'قیمت‌گذاری' },
    { id: 'variants', title: 'تنوع و ویژگی‌ها' },
    { id: 'logistics', title: 'موجودی و ارسال' },
];
export const decisionMeta: Record<string, { label: string; cls: string }> = {
    pending: { label: 'در انتظار بررسی', cls: 'bg-amber-50 text-amber-800 border-amber-200' },
    approved: { label: 'تأیید شده', cls: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
    rejected: { label: 'رد شده', cls: 'bg-rose-50 text-rose-700 border-rose-200' },
};
/** Publish gate: every field — including image fields — must be approved. */
export const allFieldsApproved = (fields: ReviewField[]): boolean => fields.length > 0 && fields.every(f => f.decision === 'approved');
function useReviewReferences(proposal: any) {
    const [refs, setRefs] = useState<any>({});
    const [refsReady, setRefsReady] = useState(false);
    const [refsError, setRefsError] = useState(false);
    useEffect(() => {
        let active = true;
        setRefs({});
        setRefsReady(false);
        setRefsError(false);
        if (!proposal)
            return;
        Promise.all([apiRequest<any[]>('/categories'), apiRequest<any[]>('/niches'), proposal.categorySlug ? apiRequest<any>(`/categories/${encodeURIComponent(proposal.categorySlug)}/${proposal.schemaVersion === 2 ? 'product-attributes' : 'attributes'}`) : Promise.resolve({})]).then(([categories, niches, facets]) => { if (active) {
            setRefs({ categories, niches, facets });
            setRefsReady(true);
        } }).catch(() => { if (active)
            setRefsError(true); });
        return () => { active = false; };
    }, [proposal?.categorySlug]);
    return { ...refs, proposal, refsReady, refsError };
}
function fieldCaption(field: ReviewField, refs: any) { const semantic = schemaReviewCaption(field.field_key, refs.proposal); if (semantic) return semantic; const key = field.field_key; const match = key.match(/^\/categoryAttributes\/attributes\/([^/]+)/); if (match) {
    const attr = refs.facets?.attributes?.find((a: any) => String(a.id) === match[1]);
    if (attr)
        return 'ویژگی دسته / ' + attr.title;
} return reviewLabel(key); }
function fieldChoices(field: ReviewField, refs: any): {
    label: string;
    value: any;
}[] {
    const key = field.field_key;
    if (/^\/variants\/[^/]+\/selection\/[^/]+$/.test(key)) return selectionReviewChoices(key, refs.proposal);
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
    if (/^\/variants\/[^/]+\/selection\/[^/]+$/.test(key)) result = selectionReviewChoices(key, refs.proposal).find(option => option.value === field.value)?.label;
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
function ExpandableText({ text }: { text: string }) {
    const [expanded, setExpanded] = useState(false);
    if (text.length <= 400)
        return <p className="whitespace-pre-wrap break-words text-sm leading-7">{text}</p>;
    return <div>
        <p className={`whitespace-pre-wrap break-words text-sm leading-7 ${expanded ? '' : 'line-clamp-6'}`}>{text}</p>
        <button type="button" onClick={() => setExpanded(v => !v)} className="mt-1 min-h-11 px-2 text-xs font-bold text-blue-700">{expanded ? 'نمایش کمتر' : 'نمایش کامل متن'}</button>
    </div>;
}
function ReviewImage({ src, alt }: { src: string; alt: string }) {
    const [broken, setBroken] = useState(false);
    if (broken)
        return <p className="rounded-xl border border-dashed border-slate-300 p-3 text-xs leading-7 text-slate-500">تصویر بارگذاری نشد. <a href={src} target="_blank" rel="noreferrer" className="font-bold text-blue-700 underline">باز کردن اصل تصویر</a></p>;
    return <a href={src} target="_blank" rel="noreferrer"><img src={src} alt={alt} loading="lazy" onError={() => setBroken(true)} className="max-h-64 max-w-full rounded-xl bg-slate-50 object-contain sm:max-h-80"/></a>;
}
/** Displayable URL for an image review field, or null when its value is not a viewable image. */
export function imageFieldUrl(field: ReviewField, refs: any): string | null {
    const value = displayReference(field, refs) ?? field.value;
    return typeof value === 'string' && /^(https?:\/\/|\/static\/)/.test(value) ? value : null;
}
export function ImageGallery({ fields, refs, title, disabled, onDecision, onReason }: { fields: ReviewField[]; refs: any; title: string; disabled?: boolean; onDecision: (fieldKey: string, decision: string) => void; onReason: (fieldKey: string, reason: string) => void }) {
    const urls = fields.map(f => ({ key: f.field_key, url: imageFieldUrl(f, refs), caption: fieldCaption(f, refs) })).filter((u): u is { key: string; url: string; caption: string } => u.url !== null);
    const [selected, setSelected] = useState(0);
    const [broken, setBroken] = useState<Record<string, boolean>>({});
    if (!urls.length)
        return <p className="text-sm text-slate-500">تصویری برای این کالا ثبت نشده است.</p>;
    const current = urls[Math.min(selected, urls.length - 1)];
    const fileName = (u: string) => { try {
        return decodeURIComponent(u.split('?')[0].split('/').pop() || u);
    }
    catch { return u; } };
    const currentField = fields.find(f => f.field_key === current.key);
    const currentMeta = decisionMeta[currentField?.decision ?? 'pending'] || decisionMeta.pending;
    const missingReason = currentField?.decision === 'rejected' && !currentField.reason?.trim();
    return <div className="space-y-3">
        <div className="rounded-xl border border-slate-200 bg-slate-50 p-2">
            {broken[current.key] ? <p className="p-6 text-center text-sm leading-7 text-slate-500">تصویر بارگذاری نشد. <a href={current.url} target="_blank" rel="noreferrer" className="font-bold text-blue-700 underline">باز کردن اصل تصویر</a></p>
                : <img src={current.url} alt={`تصویر ${Math.min(selected, urls.length - 1) + 1} از ${urls.length} — ${title}`} onError={() => setBroken(p => ({ ...p, [current.key]: true }))} className="mx-auto max-h-80 w-full rounded-lg object-contain"/>}
        </div>
        <div className="flex items-center justify-between gap-2 text-xs text-slate-500">
            <span>{current.caption} — تصویر {toPersianDigits(Math.min(selected, urls.length - 1) + 1)} از {toPersianDigits(urls.length)}</span>
            <a href={current.url} target="_blank" rel="noreferrer" className="shrink-0 font-bold text-blue-700 underline">باز کردن در اندازه کامل</a>
        </div>
        <p className="break-all text-[11px] text-slate-400" dir="ltr">{fileName(current.url)}</p>
        {currentField && <div className="space-y-2 rounded-xl border border-slate-200 p-3">
            <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-bold">نتیجهٔ بازبینی این تصویر</span>
                <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-bold ${currentMeta.cls}`}>{currentMeta.label}</span>
            </div>
            <label className="block text-sm">نتیجهٔ بازبینی
                <select aria-label={`نتیجه ${fieldCaption(currentField, refs)}`} disabled={disabled} value={currentField.decision} onChange={e => onDecision(currentField.field_key, e.target.value)} className="mt-1 block w-full min-h-11 rounded-lg border p-2 text-sm sm:w-auto sm:min-w-56">
                    <option value="pending">در انتظار بررسی</option>
                    <option value="approved">تأیید</option>
                    <option value="rejected">رد و درخواست اصلاح</option>
                </select>
            </label>
            {currentField.decision === 'rejected' && <>
                <textarea aria-label={`دلیل رد ${fieldCaption(currentField, refs)}`} value={currentField.reason || ''} disabled={disabled} onChange={e => onReason(currentField.field_key, e.target.value)} placeholder="دلیل رد و توضیح اصلاح موردنیاز — الزامی" className={`w-full rounded-lg border p-2 text-sm ${missingReason ? 'border-rose-400 bg-white' : ''}`}/>
                {missingReason && <p role="alert" className="text-xs font-bold text-rose-700">دلیل رد الزامی است؛ بدون دلیل نمی‌توان ذخیره کرد.</p>}
            </>}
        </div>}
        {urls.length > 1 && <div className="flex flex-wrap gap-2" role="listbox" aria-label="تصاویر کالا">
            {urls.map((u, i) => { const uf = fields.find(f => f.field_key === u.key); const uMeta = decisionMeta[uf?.decision ?? 'pending'] || decisionMeta.pending; return <button key={u.key} type="button" role="option" aria-selected={i === Math.min(selected, urls.length - 1)} aria-label={`تصویر ${i + 1}: ${u.caption} — ${uMeta.label}`} onClick={() => setSelected(i)} className={`relative h-16 w-16 shrink-0 overflow-hidden rounded-lg border-2 ${i === Math.min(selected, urls.length - 1) ? 'border-blue-600' : 'border-slate-200'} ${broken[u.key] ? 'bg-slate-100 text-[10px] text-slate-400' : ''}`}>
                {broken[u.key] ? 'بارگذاری نشد' : <img src={u.url} alt="" loading="lazy" onError={() => setBroken(p => ({ ...p, [u.key]: true }))} className="h-full w-full object-cover"/>}
                {uf && uf.decision !== 'pending' && <span aria-hidden="true" title={uMeta.label} className={`absolute left-1 top-1 h-3 w-3 rounded-full border-2 border-white ${uf.decision === 'approved' ? 'bg-emerald-500' : 'bg-rose-500'}`} />}
            </button>; })}
        </div>}
    </div>;
}
export function ReviewAttributeValue({ attribute }: { attribute: any }) {
    return <div className="rounded-xl border border-slate-200 bg-white p-3 space-y-2 text-xs leading-7">
      <p className="font-bold">{attribute.name} <span className="font-normal text-slate-500">({attribute.role === 'axis' ? 'محور تنوع' : 'ویژگی ثابت'})</span></p>
      <ul className="flex flex-wrap gap-2">{(attribute.values ?? []).map((value: any) => <li key={value.id} className="rounded-lg bg-slate-100 px-2 py-1">{value.label}</li>)}</ul>
      {attribute.source === 'requested' && <p className="text-amber-800">ویژگی یا مقدار پیشنهادی همراه محصول بازبینی می‌شود.</p>}
    </div>;
}
export function FieldValue({ field, refs = {} }: { field: ReviewField; refs?: any }) {
    const value = displayReference(field, refs) ?? field.value;
    if (/^\/variants\/[^/]+\/max_cart_qty$/.test(field.field_key) && value == null) return <p className="text-sm leading-7">بدون محدودیت تعداد</p>;
    if (field.field_key === '/productSchema') {
      const schema = refs.proposal?.productSchema ?? value;
      return <div className="space-y-2"><p className="text-xs">نوع محصول: {schema?.type === 'variable' ? 'متغیر' : 'تکی'}</p>{schema?.attributes?.map((attribute: any) => <ReviewAttributeValue key={attribute.id} attribute={attribute} />)}</div>;
    }
    if (/^\/productSchema\/attributes\/[^/]+$/.test(field.field_key)) return <ReviewAttributeValue attribute={value ?? pointerValue(refs.proposal, field.field_key)} />;
    if (field.field_key === '/variants' && Array.isArray(value)) {
      const variants = refs.proposal?.variants ?? value;
      return <ul className="space-y-2">{variants.map((variant: any, index: number) => <li key={variant.id ?? index} className="rounded-xl border bg-white p-3 text-xs leading-7">
        <p className="font-bold">{skuReviewLabel(refs.proposal, variant)}</p><p>{variant.is_active === false || variant.isActive === false ? 'کنار گذاشته‌شده از فروش' : 'ترکیب فعال'}</p>
        {variant.stock != null && <p>موجودی: {Number(variant.stock).toLocaleString('fa-IR')}</p>}
        {(variant.supplier_base_cost ?? variant.supplierBaseCost) != null && <p>قیمت عمده: {Number(variant.supplier_base_cost ?? variant.supplierBaseCost).toLocaleString('fa-IR')} تومان</p>}
      </li>)}</ul>;
    }
    if (isImageReviewField(field.field_key) && typeof value === 'string' && /^(https?:\/\/|\/static\/)/.test(value)) {
      const match = field.field_key.match(/(\d+)$/);
      const num = match ? Number(match[1]) + 1 : 1;
      const alt = refs.proposal?.title ? `تصویر ${num} — ${refs.proposal.title}` : `تصویر کالا ${num}`;
      return <ReviewImage src={value} alt={alt} />;
    }
    return <ExpandableText text={typeof value === 'number' ? value.toLocaleString('fa-IR') : shown(value)} />;
}
export function ProductFieldReview({ id, onClose, onUpdated }: {
    id: string;
    onClose: () => void;
    onUpdated: () => void;
}) {
    const { error, success } = useToast();
    const [fields, setFields] = useState<ReviewField[]>([]);
    const [baseline,setBaseline]=useState<ReviewField[]>([]);
    const [showApproved,setShowApproved]=useState(false);
    const dirty=fields.filter(f=>{const old=baseline.find(v=>v.field_key===f.field_key);return !old||old.decision!==f.decision||(old.reason||'')!==(f.reason||'')});
    const [title, setTitle] = useState('');
    const [proposal, setProposal] = useState<any>(null);
    const refs = useReviewReferences(proposal);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState('');
    const load = async () => { setLoading(true); setLoadError(''); try {
        const d = await apiRequest<any>(`/admin/product-field-reviews/${encodeURIComponent(id)}`, { auth: true });
        setFields(d.fields);
        setBaseline(d.fields);
        setProposal(d.proposal);
        setTitle(d.proposal.title);
    }
    catch (e) {
        setLoadError(e instanceof Error ? e.message : 'بارگذاری ناموفق بود');
    }
    finally {
        setLoading(false);
    } };
    useEffect(() => { void load(); }, [id]);
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose]);
    const approvedCount = fields.filter(f => f.decision === 'approved').length;
    const pendingCount = fields.filter(f => f.decision === 'pending').length;
    const rejectedCount = fields.filter(f => f.decision === 'rejected').length;
    const remaining = fields.length - approvedCount;
    const rejectedWithoutReason = fields.some(f => f.decision === 'rejected' && !f.reason?.trim());
    const save = async (publish = false) => { if (saving || loading)
        return; if (rejectedWithoutReason) {
        error('برای هر مورد ردشده دلیل بنویسید.');
        return;
    } setSaving(true); try {
        if(dirty.length){
        const d = await apiRequest<any>(`/admin/product-field-reviews/${encodeURIComponent(id)}`, { method: 'PUT', auth: true, body: JSON.stringify({ fields:dirty }) });
        setFields(d.fields);setBaseline(d.fields);
        }
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
        setSaving(false);
    } };
    const visibleFields = fields.filter(f=>showApproved||f.decision!=='approved');
    const visibleImageFields = visibleFields.filter(f => isImageReviewField(f.field_key));
    const contextRows: [string, string][] = proposal ? [
        ['تأمین‌کننده', proposal.supplierName || proposal.supplierId || ''],
        ['تاریخ ثبت', proposal.createdAt || proposal.submittedAt ? (() => { try { return new Date(proposal.createdAt || proposal.submittedAt).toLocaleDateString('fa-IR'); } catch { return String(proposal.createdAt || proposal.submittedAt); } })() : ''],
        ['بارکد', proposal.barcode || ''],
        ['دسته‌بندی', refs.categories?.find((c: any) => c.slug === proposal.categorySlug)?.title || proposal.categorySlug || proposal.category || ''],
        ['گارانتی', proposal.warranty || ''],
        ['نوع محصول', proposal.productType ? (valueLabels[proposal.productType] ?? proposal.productType) : ''],
    ].filter(([, v]) => v) as [string, string][] : [];
    const setFieldDecision = (fieldKey: string, decision: string) => setFields(p => p.map(v => v.field_key === fieldKey ? { ...v, decision } : v));
    const setFieldReason = (fieldKey: string, reason: string) => setFields(p => p.map(v => v.field_key === fieldKey ? { ...v, reason } : v));
    const renderFieldCard = (f: ReviewField) => {
        const caption = fieldCaption(f, refs);
        const meta = decisionMeta[f.decision] || decisionMeta.pending;
        const isDirty = dirty.some(d => d.field_key === f.field_key);
        const missingReason = f.decision === 'rejected' && !f.reason?.trim();
        return <article data-field-key={f.field_key} key={f.field_key} className={`rounded-xl border p-3 space-y-3 sm:p-4 ${f.decision === 'rejected' ? 'border-rose-300 bg-rose-50' : 'border-slate-200'}`}>
            <div className="flex items-start justify-between gap-2">
                <h4 className="font-bold text-sm leading-7">{caption}</h4>
                <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-bold ${meta.cls}`}>{meta.label}</span>
            </div>
            {isDirty && <p className="text-[11px] font-bold text-blue-700">تغییر کرده — هنوز ذخیره نشده</p>}
            <FieldValue field={f} refs={refs}/>
            <label className="block text-sm">نتیجهٔ بازبینی
                <select aria-label={`نتیجه ${caption}`} disabled={saving || proposal?.status === 'approved'} value={f.decision} onChange={e => setFields(p => p.map((v, j) => v.field_key === f.field_key ? { ...v, decision: e.target.value } : v))} className="mt-1 block w-full min-h-11 rounded-lg border p-2 text-sm sm:w-auto sm:min-w-56">
                    <option value="pending">در انتظار بررسی</option>
                    <option value="approved">تأیید</option>
                    <option value="rejected">رد و درخواست اصلاح</option>
                </select>
            </label>
            {f.decision === 'rejected' && <>
                <textarea aria-label={`دلیل رد ${caption}`} value={f.reason || ''} disabled={saving} onChange={e => setFields(p => p.map((v, j) => v.field_key === f.field_key ? { ...v, reason: e.target.value } : v))} placeholder="دلیل رد و توضیح اصلاح موردنیاز — الزامی" className={`w-full rounded-lg border p-2 text-sm ${missingReason ? 'border-rose-400 bg-white' : ''}`}/>
                {missingReason && <p role="alert" className="text-xs font-bold text-rose-700">دلیل رد الزامی است؛ بدون دلیل نمی‌توان ذخیره کرد.</p>}
            </>}
        </article>;
    };
    return <div className="fixed inset-0 z-50 bg-slate-900/60 flex items-center justify-center p-3" dir="rtl"><section role="dialog" aria-modal="true" aria-label="بازبینی فیلدی محصول" className="bg-white rounded-2xl w-full max-w-4xl max-h-[92vh] flex flex-col overflow-hidden"><header className="border-b p-4 sm:p-5 space-y-2">
        <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
                <h3 className="font-bold text-base sm:text-lg leading-8">بازبینی: {title || '…'}</h3>
                {proposal?.status === 'approved' && <p className="mt-1 text-xs font-bold text-emerald-700">این کالا قبلاً تأیید و منتشر شده است؛ تصمیم‌ها فقط خواندنی‌اند.</p>}
            </div>
            <button type="button" onClick={onClose} aria-label="بستن پنجره بازبینی" className="shrink-0 min-h-11 rounded-lg border px-3 py-2 text-sm">بستن</button>
        </div>
        {contextRows.length > 0 && <dl className="grid grid-cols-1 gap-x-4 gap-y-1 rounded-xl bg-slate-50 p-3 text-xs leading-7 sm:grid-cols-2">
            {contextRows.map(([k, v]) => <div key={k} className="flex gap-1.5"><dt className="shrink-0 font-bold text-slate-500">{k}:</dt><dd className="min-w-0 break-words font-bold text-slate-800">{v}</dd></div>)}
        </dl>}
    </header><div className="overflow-auto p-4 space-y-4 sm:p-5">
 {loading && <p role="status" className="rounded-xl bg-slate-50 p-3 text-sm leading-7">در حال دریافت فیلدهای بازبینی…</p>}
 {saving && <p role="status" className="rounded-xl bg-blue-50 p-3 text-sm leading-7 text-blue-800">در حال ذخیرهٔ تصمیم‌ها…</p>}
 {proposal && !refs.refsReady && !refs.refsError && <p role="status" className="text-xs text-slate-400">در حال دریافت نام دسته‌ها و مرجع‌ها…</p>}
 {proposal && refs.refsError && <p role="alert" className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-7 text-amber-800">مرجع‌ها (دسته، موضوع، ویژگی) بارگذاری نشد؛ برخی نام‌ها ممکن است به‌صورت خام نمایش داده شوند.</p>}
 {proposal?.productType && <div className="rounded-xl bg-slate-50 p-3 text-sm leading-7"><strong>خلاصهٔ محاسبه‌شده از تنوع‌ها</strong><p>مجموع موجودی: {toPersianDigits(proposal.stock ?? 0)} — قیمت مبنا: {proposal.supplierBaseCost != null ? `${formatPrice(Number(proposal.supplierBaseCost))} تومان` : 'ثبت نشده'}</p><p>قیمت و موجودی را در ردیف هر تنوع بررسی کنید؛ جمع‌ها خودکار به‌روز می‌شوند.</p>{proposal.variants?.map((v: any, i: number) => <p key={i}>تنوع {i + 1}: {skuReviewLabel(proposal, v)}</p>)}</div>}
 {!loading && !loadError && fields.length > 0 && <div className="flex flex-wrap items-center justify-between gap-2">
    <p role="status" className="text-sm leading-7"><strong>{toPersianDigits(approvedCount)}</strong> تأییدشده • <strong>{toPersianDigits(pendingCount)}</strong> در انتظار بررسی • <strong>{toPersianDigits(rejectedCount)}</strong> ردشده • مجموع {toPersianDigits(fields.length)}</p>
    <button type="button" className="min-h-11 rounded-lg border px-3 py-2 text-xs font-bold" onClick={()=>setShowApproved(v=>!v)}>{showApproved ? 'نمایش فقط موارد نیازمند بررسی' : `مشاهده موارد تأییدشده (${toPersianDigits(approvedCount)})`}</button>
 </div>}
 {loadError ? <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-4 text-sm leading-7 text-rose-800"><p className="font-bold">بارگذاری بازبینی ناموفق بود.</p><p>{loadError}</p><button type="button" onClick={() => void load()} className="min-h-11 rounded-lg bg-rose-600 px-4 py-2 text-sm font-bold text-white">تلاش مجدد</button></div> : <>
    {visibleImageFields.length > 0 && <section aria-label="تصاویر"><h3 className="sticky top-0 z-10 -mx-1 bg-white/95 py-2 text-sm font-bold text-slate-800">تصاویر <span className="font-normal text-slate-400">({toPersianDigits(visibleImageFields.length)} تصویر)</span></h3><ImageGallery fields={visibleImageFields} refs={refs} title={title} disabled={saving || proposal?.status === 'approved'} onDecision={setFieldDecision} onReason={setFieldReason} />{visibleImageFields.filter(f => !imageFieldUrl(f, refs)).map(f => <div key={f.field_key} className="mt-3"><p className="mb-2 text-xs leading-7 text-slate-500">تصویر «{fieldCaption(f, refs)}» قابل نمایش نیست؛ با این حال برای انتشار باید برای آن تصمیم بگیرید.</p>{renderFieldCard(f)}</div>)}</section>}
    {FIELD_SECTION_ORDER.filter(s => s.id !== 'images').map(section => {
        const sectionFields = visibleFields.filter(f => fieldSection(f.field_key).id === section.id);
        if (!sectionFields.length) return null;
        const secApproved = sectionFields.filter(f => f.decision === 'approved').length;
        const secRejected = sectionFields.filter(f => f.decision === 'rejected').length;
        return <section key={section.id} aria-label={section.title}>
            <h3 className="sticky top-0 z-10 -mx-1 bg-white/95 py-2 text-sm font-bold text-slate-800">{section.title} <span className="font-normal text-slate-400">({toPersianDigits(sectionFields.length)} مورد{sectionFields.length - secApproved - secRejected > 0 ? `، ${toPersianDigits(sectionFields.length - secApproved - secRejected)} در انتظار` : ''}{secRejected > 0 ? `، ${toPersianDigits(secRejected)} ردشده` : ''})</span></h3>
            <div className="space-y-3">{sectionFields.map(renderFieldCard)}</div>
        </section>;
    })}
    {!loading && fields.length > 0 && visibleFields.length === 0 && <p className="rounded-xl bg-emerald-50 p-4 text-sm leading-7 text-emerald-800">همهٔ موارد تأیید شده‌اند. برای دیدن آن‌ها «مشاهده موارد تأییدشده» را بزنید.</p>}
 </>}
 </div><footer className="border-t bg-white p-3 sm:p-4">
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="text-xs leading-7 text-slate-500">
            <p><strong className="text-slate-800">{toPersianDigits(approvedCount)} از {toPersianDigits(fields.length)}</strong> مورد تأیید شده</p>
            {remaining > 0 && !loadError && <p>برای انتشار، {toPersianDigits(remaining)} مورد دیگر باید تأیید شود.</p>}
            {remaining === 0 && fields.length > 0 && !loadError && <p className="font-bold text-emerald-700">همهٔ موارد تأیید شده‌اند — آمادهٔ انتشار است.</p>}
            {!dirty.length && !loadError && fields.length > 0 && <p>تغییری برای ذخیره نیست.</p>}
            {dirty.length > 0 && <p>{toPersianDigits(dirty.length)} تغییر ذخیره‌نشده دارید.</p>}
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
            <button disabled={saving || loading || !dirty.length || proposal?.status === 'approved' || !!loadError || !fields.length} onClick={() => void save()} className="min-h-11 rounded-lg bg-slate-800 px-4 py-3 text-sm font-bold text-white disabled:opacity-40">ذخیره / ارسال موارد ردشده</button>
            <button disabled={saving || loading || proposal?.status === 'approved' || !!loadError || !fields.length || !allFieldsApproved(fields)} onClick={() => void save(true)} className="min-h-11 rounded-lg bg-emerald-700 px-4 py-3 text-sm font-bold text-white disabled:opacity-40">تأیید نهایی و انتشار</button>
        </div>
    </div>
 </footer></section></div>;
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
    const [structuralEdits, setStructuralEdits] = useState<Record<string, any>>({});
    const [structuralError, setStructuralError] = useState('');
    const [busy, setBusy] = useState(false);
    const refs = useReviewReferences(proposal);
    const fields: ReviewField[] = initial.filter(f => f.field_key.startsWith('/')).map(f => { const current = pointerValue(proposal, f.field_key); return { ...f, value: current === undefined ? f.value : current }; });
    const rejected=fields.filter(f=>f.decision==='rejected');
    const other=fields.filter(f=>f.decision!=='rejected');
    const isManaged = (key: string) => key === '/productSchema' || /^\/productSchema\/attributes\/[^/]+$/.test(key) || key === '/variants' || /^\/variants\/[^/]+\/(stock|supplier_base_cost|selection\/[^/]+)$/.test(key);
    const managedStructural = proposal.schemaVersion === 2 && proposal.productSchema && rejected.some(field => isManaged(field.field_key));
    const changed={...Object.fromEntries(Object.entries(edits).filter(([key,value])=>JSON.stringify(value)!==JSON.stringify(pointerValue(proposal,key)))),...structuralEdits};
    const renderField=(f:ReviewField)=>(<article data-field-key={f.field_key} key={f.field_key} className={`rounded-2xl border p-4 space-y-3 ${f.decision === 'rejected' ? 'border-amber-200 bg-white shadow-sm' : 'border-slate-200 bg-slate-50'}`}><h4 className="text-xs font-bold">{fieldCaption(f, refs)}</h4><div className="rounded-xl bg-slate-50 p-3"><p className="text-[11px] font-bold text-slate-500 mb-1">مقدار فعلی</p><FieldValue field={f} refs={refs}/></div><p className="text-xs">{f.decision === 'approved' ? 'تأیید شده' : f.decision === 'rejected' ? 'نیاز به اصلاح' : 'در انتظار بازبینی'}</p>{f.decision === 'rejected' && <><div className="rounded-xl border border-amber-200 bg-amber-50 p-3"><p className="text-xs font-bold text-amber-900 mb-1">نظر بازبین</p><p className="text-sm leading-7 text-amber-950">{f.reason}</p></div><p className="text-xs font-bold">مقدار اصلاح‌شده</p>{managedStructural && isManaged(f.field_key) ? <p className="text-xs text-violet-800 leading-7">این مورد را در فرم یکپارچهٔ ویژگی‌ها و ترکیب‌ها در پایین اصلاح کنید.</p> : fieldChoices(f, refs).length || /^\/variants\/[^/]+\/selection\/[^/]+$/.test(f.field_key) ? <select aria-label={fieldCaption(f, refs)} disabled={busy} className="w-full border rounded-lg p-2 text-sm" value={String(edits[f.field_key] ?? f.value ?? '')} onChange={e => { const selected = fieldChoices(f, refs).find(v => String(v.value) === e.target.value); if (selected)
        setEdits(p => ({ ...p, [f.field_key]: selected.value })); }}><option value={String(f.value ?? '')}>{shown(displayReference(f, refs) ?? f.value)}</option>{fieldChoices(f, refs).filter(v => String(v.value) !== String(f.value ?? '')).map(v => <option key={String(v.value)} value={String(v.value)}>{v.label}</option>)}</select> : typeof f.value === 'boolean' ? <select aria-label={fieldCaption(f, refs)} value={String(edits[f.field_key] ?? f.value)} onChange={e => setEdits(p => ({ ...p, [f.field_key]: e.target.value === 'true' }))}><option value="true">بله</option><option value="false">خیر</option></select> : <textarea aria-label={fieldCaption(f, refs)} disabled={busy} rows={f.field_key === '/description' ? 6 : 2} className="w-full border border-slate-300 rounded-xl p-3 text-sm leading-7 focus:border-violet-500 outline-none" value={texts[f.field_key] ?? (typeof f.value === 'object' ? '' : String(f.value ?? ''))} onChange={e => { const text = e.target.value; setTexts(p => ({ ...p, [f.field_key]: text })); let value: any = text; if (/^\/variants\/[^/]+\/(min_cart_qty|max_cart_qty)$/.test(f.field_key)) value = cartLimitCorrectionValue(f.field_key, text); else if (typeof f.value === 'number' || /^\/(packagingLengthMm|packagingWidthMm|packagingHeightMm)$/.test(f.field_key))
        value = text === '' ? '' : Number(text.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))));
    else if (f.value && typeof f.value === 'object')
        value = Array.isArray(f.value) ? text.split('\n').filter(Boolean) : Object.fromEntries(text.split('\n').map(l => { const i = l.indexOf(':'); return i < 0 ? [l, ''] : [l.slice(0, i).trim(), l.slice(i + 1).trim()]; })); setEdits(p => ({ ...p, [f.field_key]: value })); }} placeholder={f.field_key.endsWith('/max_cart_qty') ? 'خالی بگذارید تا محدودیت تعداد برداشته شود' : 'مقدار اصلاح‌شده'}/>}</>}</article>);
    const submit = async () => { if (structuralError || !Object.keys(changed).length)
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
          {managedStructural && <SupplierProductStructuralCorrection key={proposal.id} proposal={proposal} fields={fields} busy={busy} onPatch={(patch, message) => { setStructuralEdits(patch); setStructuralError(message); }} />}
          {other.length>0&&<details className="rounded-xl border border-slate-200 bg-white p-3"><summary className="cursor-pointer text-xs font-bold leading-7">سایر اطلاعات کالا ({other.filter(f=>f.decision==='approved').length} تأییدشده، {other.filter(f=>f.decision==='pending').length} در انتظار بررسی)</summary><div className="mt-3 space-y-3">{other.map(renderField)}</div></details>}
        </div>
        <footer className="p-4 border-t bg-white flex flex-wrap items-center justify-between gap-3"><p className="text-xs text-slate-500">{Object.keys(changed).length} از {rejected.length} اصلاح آمادهٔ ارسال</p><button type="button" disabled={busy||!!structuralError||!Object.keys(changed).length} onClick={()=>void submit()} className="bg-violet-700 text-white px-4 py-3 rounded-xl text-sm font-bold disabled:opacity-40">{busy?'در حال ارسال…':'ارسال اصلاحات برای بازبینی'}</button></footer>
      </section></div>;
}
