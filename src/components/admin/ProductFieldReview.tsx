import React, { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import type { ProposedProduct } from '../../types';
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
const reviewButton = 'inline-flex min-h-11 items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-bold leading-5 text-slate-700 hover:bg-slate-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';
const reviewPrimary = 'inline-flex min-h-11 items-center justify-center rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold leading-5 text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';

function FieldDecisionControl({ field, caption, disabled, readOnly, onDecision, onReason }: {
    field: ReviewField;
    caption: string;
    disabled?: boolean;
    readOnly?: boolean;
    onDecision: (fieldKey: string, decision: string) => void;
    onReason: (fieldKey: string, reason: string) => void;
}) {
    const reasonHelp = useId();
    const missingReason = field.decision === 'rejected' && !field.reason?.trim();
    if (readOnly) return field.reason ? <p className="whitespace-pre-wrap text-sm leading-6 text-slate-700 [overflow-wrap:anywhere]">{field.reason}</p> : null;
    return <div className="space-y-2">
        <div role="group" aria-label={'نتیجه ' + caption} className="grid grid-cols-3 gap-1.5">
            {[
                { value: 'pending', text: 'در انتظار', action: 'در انتظار بررسی', active: 'border-amber-300 bg-amber-50 text-amber-900' },
                { value: 'approved', text: 'تأیید', action: 'تأیید', active: 'border-emerald-300 bg-emerald-50 text-emerald-800' },
                { value: 'rejected', text: 'درخواست اصلاح', action: 'رد و درخواست اصلاح', active: 'border-rose-300 bg-rose-50 text-rose-800' },
            ].map(option => <button key={option.value} type="button" disabled={disabled}
                aria-label={option.action + ' ' + caption} aria-pressed={field.decision === option.value}
                onClick={() => onDecision(field.field_key, option.value)}
                className={'min-h-11 rounded-lg border px-1.5 py-1.5 text-xs font-bold leading-5 focus-visible:outline-2 focus-visible:outline-brand disabled:opacity-50 ' + (field.decision === option.value ? option.active : 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50')}>
                {option.text}
            </button>)}
        </div>
        {field.decision === 'rejected' && <>
            <label className="block text-sm font-bold text-slate-800">دلیل رد
                <textarea aria-label={'دلیل رد ' + caption} value={field.reason || ''} disabled={disabled} required maxLength={2000}
                    aria-invalid={missingReason} aria-describedby={missingReason ? reasonHelp : undefined}
                    onChange={event => onReason(field.field_key, event.target.value)} rows={2}
                    placeholder="اصلاح موردنیاز را برای تأمین‌کننده بنویسید"
                    className={'mt-1 block w-full rounded-lg border bg-white p-2.5 text-base font-normal leading-6 focus-visible:outline-2 focus-visible:outline-brand sm:text-sm ' + (missingReason ? 'border-rose-400' : 'border-slate-300')} />
            </label>
            {missingReason && <p id={reasonHelp} role="alert" className="text-xs font-bold text-rose-700">دلیل رد الزامی است.</p>}
        </>}
    </div>;
}
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
export function ImageGallery({ fields, refs, title, disabled, readOnly, onDecision, onReason }: {
    fields: ReviewField[];
    refs: any;
    title: string;
    disabled?: boolean;
    readOnly?: boolean;
    onDecision: (fieldKey: string, decision: string) => void;
    onReason: (fieldKey: string, reason: string) => void;
}) {
    const images = fields.flatMap(field => {
        const url = imageFieldUrl(field, refs);
        return url ? [{ field, url, caption: fieldCaption(field, refs) }] : [];
    });
    const [selectedKey, setSelectedKey] = useState<string>();
    const [broken, setBroken] = useState<Record<string, boolean>>({});
    if (!images.length) return null;
    const selected = Math.max(0, images.findIndex(image => image.field.field_key === selectedKey));
    const current = images[selected];
    const currentMeta = decisionMeta[current.field.decision] || decisionMeta.pending;
    return <div className="min-w-0 space-y-3 rounded-xl border border-slate-200 bg-white p-3 sm:p-4">
        {images.length > 1 && <div className="flex gap-2 overflow-x-auto p-1" role="group" aria-label="تصاویر کالا">
            {images.map(({ field, url, caption }, index) => {
                const meta = decisionMeta[field.decision] || decisionMeta.pending;
                return <button key={field.field_key} type="button" aria-pressed={index === selected}
                    aria-label={'تصویر ' + (index + 1) + ': ' + caption + ' — ' + meta.label}
                    onClick={() => setSelectedKey(field.field_key)}
                    className={'relative size-14 shrink-0 overflow-hidden rounded-lg border-2 focus-visible:outline-2 focus-visible:outline-brand ' + (index === selected ? 'border-brand' : 'border-slate-200')}>
                    {broken[field.field_key] ? <span className="text-[10px] text-slate-600">بارگذاری نشد</span>
                        : <img src={url} alt="" loading="lazy" onError={() => setBroken(items => ({ ...items, [field.field_key]: true }))} className="size-full object-cover" />}
                    {field.decision !== 'pending' && <span aria-hidden="true" title={meta.label} className={'absolute end-1 top-1 size-3 rounded-full border-2 border-white ' + (field.decision === 'approved' ? 'bg-emerald-500' : 'bg-rose-500')} />}
                </button>;
            })}
        </div>}
        <div className="rounded-xl bg-slate-50 p-2">
            {broken[current.field.field_key] ? <p className="p-4 text-center text-sm leading-6 text-slate-600">تصویر بارگذاری نشد.</p>
                : <img src={current.url} alt={'تصویر ' + (selected + 1) + ' از ' + images.length + ' — ' + title}
                    onError={() => setBroken(items => ({ ...items, [current.field.field_key]: true }))}
                    className="mx-auto max-h-64 w-full rounded-lg object-contain sm:max-h-80" />}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-1 text-sm">
            <p className="font-bold">{current.caption} <span className="font-normal text-slate-600">({toPersianDigits(selected + 1)} از {toPersianDigits(images.length)})</span></p>
            <span className={'rounded-full border px-2 py-0.5 text-xs font-bold ' + currentMeta.cls}>{currentMeta.label}</span>
        </div>
        <a href={current.url} target="_blank" rel="noreferrer" className="inline-flex min-h-11 items-center rounded-xl px-2 text-sm font-bold text-brand-deep underline hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand">باز کردن تصویر کامل</a>
        <FieldDecisionControl field={current.field} caption={current.caption} disabled={disabled} readOnly={readOnly} onDecision={onDecision} onReason={onReason} />
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
type FieldReviewResponse = {
    proposal: any;
    fields: ReviewField[];
};

export function ProductFieldReview({ id, initialTitle, onClose, onUpdated, onRejectProduct }: {
    id: string;
    initialTitle?: string;
    onClose: (status?: ProposedProduct['status']) => void;
    onUpdated: (status?: ProposedProduct['status']) => void;
    onRejectProduct?: (reason: string) => Promise<void>;
}) {
    const { success } = useToast();
    const dialogRef = useRef<HTMLDialogElement>(null);
    const titleId = useId();
    const [fields, setFields] = useState<ReviewField[]>([]);
    const [baseline, setBaseline] = useState<ReviewField[]>([]);
    const [showApproved, setShowApproved] = useState(false);
    const [proposal, setProposal] = useState<FieldReviewResponse['proposal']>(null);
    const refs = useReviewReferences(proposal);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');
    const [actionError, setActionError] = useState('');
    const [notice, setNotice] = useState('');
    const [action, setAction] = useState<'save' | 'publish' | 'reject'>();
    const [rejecting, setRejecting] = useState(false);
    const [rejectReason, setRejectReason] = useState('');
    const endpoint = '/admin/product-field-reviews/' + encodeURIComponent(id);
    const title = proposal?.title || initialTitle;
    const readOnly = proposal?.status === 'approved';
    const canRejectProduct = onRejectProduct && !readOnly && proposal?.status !== 'rejected';

    useLayoutEffect(() => {
        const dialog = dialogRef.current;
        const opener = document.activeElement;
        dialog?.showModal();
        return () => {
            dialog?.close();
            if (opener instanceof HTMLElement && opener.isConnected) opener.focus({ preventScroll: true });
        };
    }, []);
    const load = async (signal?: AbortSignal) => {
        setLoading(true);
        setLoadError('');
        try {
            const data = await apiRequest<FieldReviewResponse>(endpoint, { auth: true, signal });
            setFields(data.fields);
            setBaseline(data.fields);
            setProposal(data.proposal);
        } catch (e) {
            if (!signal?.aborted) setLoadError(e instanceof Error ? e.message : 'بارگذاری ناموفق بود');
        } finally {
            if (!signal?.aborted) setLoading(false);
        }
    };
    useEffect(() => {
        const controller = new AbortController();
        void load(controller.signal);
        return () => controller.abort();
    }, [id]);

    const savedFields = new Map(baseline.map(field => [field.field_key, field]));
    const dirty = fields.filter(field => {
        const old = savedFields.get(field.field_key);
        return !old || field.decision !== old.decision || (field.reason || '') !== (old.reason || '');
    });
    const approvedCount = fields.filter(field => field.decision === 'approved').length;
    const rejectedCount = fields.filter(field => field.decision === 'rejected').length;
    const pendingCount = fields.length - approvedCount - rejectedCount;
    const publishReady = allFieldsApproved(fields);
    const rejectedWithoutReason = fields.some(field => field.decision === 'rejected' && !field.reason?.trim());
    const unavailable = !!action || loading || readOnly || !!loadError;
    const save = async (publish = false) => {
        if (unavailable || rejectedWithoutReason || (publish ? !publishReady : !dirty.length)) return;
        setAction(publish ? 'publish' : 'save');
        setActionError('');
        setNotice('');
        try {
            let status = proposal?.status;
            if (dirty.length) {
                const data = await apiRequest<FieldReviewResponse>(endpoint, { method: 'PUT', auth: true, body: JSON.stringify({ fields: dirty }) });
                setFields(data.fields);
                setBaseline(data.fields);
                setProposal(data.proposal);
                status = data.proposal.status;
            }
            if (publish) {
                await apiRequest('/admin/approvals/' + encodeURIComponent(id) + '/review', { method: 'POST', auth: true, body: JSON.stringify({ status: 'approved' }) });
                onUpdated('approved');
                onClose('approved');
                success('کالا تأیید و منتشر شد.');
            } else {
                setNotice(rejectedCount ? 'موارد نیازمند اصلاح برای تأمین‌کننده ارسال شد.' : 'تصمیم‌ها ذخیره شد.');
                onUpdated(status);
            }
        } catch (e) {
            setActionError(e instanceof Error ? e.message : 'ثبت تصمیم ناموفق بود.');
        } finally {
            setAction(undefined);
        }
    };
    const reject = async () => {
        if (action || !onRejectProduct || !rejectReason.trim()) return;
        setAction('reject');
        setActionError('');
        try {
            await onRejectProduct(rejectReason.trim());
            onUpdated('rejected');
            onClose('rejected');
            success('کالا رد شد و دلیل آن به تأمین‌کننده ارسال شد.');
        } catch (e) {
            setActionError(e instanceof Error ? e.message : 'رد کالا ناموفق بود.');
        } finally {
            setAction(undefined);
        }
    };
    const dismiss = () => {
        if (action) return;
        if (rejecting) {
            setRejecting(false);
            setActionError('');
        } else onClose();
    };

    const visibleFields = fields.filter(field => showApproved || readOnly || field.decision !== 'approved'
        || savedFields.get(field.field_key)?.decision !== 'approved');
    const groups = FIELD_SECTION_ORDER.map(section => ({
        ...section,
        fields: visibleFields.filter(field => fieldSection(field.field_key).id === section.id),
    })).filter(section => section.fields.length);
    const submittedAt = proposal?.submittedAt || proposal?.createdAt || proposal?.created_at;
    const contextRows = [
        ['تأمین‌کننده', proposal?.supplierName || proposal?.supplierId],
        ['تاریخ ثبت', submittedAt && new Date(submittedAt).toLocaleDateString('fa-IR')],
        ['بارکد', proposal?.barcode],
        ['دسته‌بندی', refs.categories?.find((category: any) => category.slug === proposal?.categorySlug)?.title || proposal?.categorySlug || proposal?.category],
        ['گارانتی', proposal?.warranty],
        ['نوع محصول', proposal?.productType && (valueLabels[proposal.productType] ?? proposal.productType)],
    ].filter(([, value]) => value);
    const setFieldDecision = (fieldKey: string, decision: string) => {
        setNotice('');
        setActionError('');
        setFields(items => items.map(field => field.field_key === fieldKey ? { ...field, decision, reason: decision === 'rejected' ? field.reason : '' } : field));
    };
    const setFieldReason = (fieldKey: string, reason: string) => {
        setNotice('');
        setFields(items => items.map(field => field.field_key === fieldKey ? { ...field, reason } : field));
    };
    const renderFieldCard = (field: ReviewField) => {
        const caption = fieldCaption(field, refs);
        const meta = decisionMeta[field.decision] || decisionMeta.pending;
        return <article key={field.field_key} data-field-key={field.field_key} className="min-w-0 space-y-3 rounded-xl border border-slate-200 bg-white p-3 sm:p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h4 className="text-sm font-bold leading-6 [overflow-wrap:anywhere]">{caption}</h4>
                <span className={'rounded-full border px-2 py-0.5 text-xs font-bold ' + meta.cls}>{meta.label}</span>
            </div>
            <FieldValue field={field} refs={refs} />
            <FieldDecisionControl field={field} caption={caption} disabled={!!action} readOnly={readOnly}
                onDecision={setFieldDecision} onReason={setFieldReason} />
        </article>;
    };
    const errorMessage = actionError && <p role="alert" className="rounded-lg bg-rose-50 p-2 text-sm leading-6 text-rose-900">{actionError}</p>;
    const progress = { save: 'در حال ذخیرهٔ تصمیم‌ها…', publish: 'در حال انتشار کالا…', reject: 'در حال رد کالا…' };

    return <dialog ref={dialogRef} aria-labelledby={titleId} dir="rtl"
        onCancel={event => { event.preventDefault(); event.stopPropagation(); dismiss(); }}
        className="m-auto h-dvh max-h-dvh w-full max-w-none overflow-hidden bg-white p-0 text-slate-900 backdrop:bg-slate-950/55 open:flex open:flex-col sm:h-[min(54rem,calc(100dvh-2rem))] sm:w-[calc(100%_-_2rem)] sm:max-w-4xl sm:rounded-2xl sm:border sm:border-slate-200 sm:shadow-2xl">
        <header className="shrink-0 border-b border-slate-200 p-3 sm:p-4">
            <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                    <h2 id={titleId} className="text-base font-extrabold leading-7 sm:text-lg">{rejecting ? 'رد کالا و بازگشت به تأمین‌کننده' : 'بازبینی کالا'}</h2>
                    {title && <p title={title} className="line-clamp-2 text-sm font-medium leading-6 text-slate-700 [overflow-wrap:anywhere]">{title}</p>}
                </div>
                <button type="button" disabled={!!action} onClick={dismiss} className={reviewButton}
                    aria-label={rejecting ? 'بازگشت به بازبینی' : 'بستن پنجره بازبینی'}>{rejecting ? 'بازگشت' : 'بستن'}</button>
            </div>
        </header>

        {rejecting ? <>
            <div className="min-h-0 flex-1 space-y-3 overflow-y-auto overscroll-contain p-3 sm:p-4">
                <p className="text-sm leading-7 text-slate-700">دلیل رد در اعلان و فهرست پیشنهادهای تأمین‌کننده نمایش داده می‌شود.</p>
                <label className="block text-sm font-bold">دلیل رد کالا
                    <textarea autoFocus aria-label="دلیل رد کالا" disabled={!!action} required value={rejectReason} rows={4}
                        onChange={event => setRejectReason(event.target.value)} placeholder="مواردی که باید اصلاح شوند را بنویسید"
                        className="mt-1 block w-full rounded-xl border border-slate-300 p-3 text-base font-normal leading-7 focus-visible:outline-2 focus-visible:outline-brand sm:text-sm" />
                </label>
            </div>
            <footer className="shrink-0 space-y-2 border-t border-slate-200 p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
                {errorMessage}
                {action && <p role="status" className="text-xs font-bold text-slate-700">{progress[action]}</p>}
                <div className="flex flex-wrap gap-2">
                    <button type="button" disabled={!!action || !rejectReason.trim()} onClick={() => void reject()}
                        className="min-h-11 flex-1 rounded-xl bg-rose-700 px-4 py-2 text-sm font-bold text-white hover:bg-rose-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-rose-700 disabled:opacity-50 sm:flex-none">تأیید رد کالا</button>
                    <button type="button" disabled={!!action} onClick={dismiss} className={reviewButton}>انصراف</button>
                </div>
            </footer>
        </> : <>
            <div className="min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain bg-slate-50 p-3 sm:p-4">
                {loading && <p role="status" className="rounded-xl bg-white p-3 text-sm leading-7">در حال دریافت فیلدهای بازبینی…</p>}
                {loadError && <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm leading-7 text-rose-900">
                    <p className="font-bold">بارگذاری بازبینی ناموفق بود.</p>
                    <p>{loadError}</p>
                    <button type="button" onClick={() => void load()} className={reviewButton}>تلاش مجدد</button>
                </div>}
                {readOnly && <p role="status" className="rounded-xl bg-emerald-50 p-3 text-sm font-bold leading-6 text-emerald-800">این کالا تأیید و منتشر شده است.</p>}
                {proposal?.rejection_reason && proposal.status === 'rejected' && <p className="rounded-xl bg-rose-50 p-3 text-sm leading-6 text-rose-900">دلیل رد کالا {proposal.rejection_reason}</p>}
                {contextRows.length > 0 && <dl className="grid gap-x-4 gap-y-1 rounded-xl border border-slate-200 bg-white p-3 text-sm leading-6 sm:grid-cols-2">
                    {contextRows.map(([label, value]) => <div key={label} className="flex min-w-0 items-start gap-2">
                        <dt className="shrink-0 text-slate-600">{label}</dt>
                        <dd className="min-w-0 font-semibold [overflow-wrap:anywhere]" dir="auto">{value}</dd>
                    </div>)}
                </dl>}
                {proposal && !refs.refsReady && !refs.refsError && <p role="status" className="text-xs text-slate-600">در حال دریافت نام دسته‌ها و مرجع‌ها…</p>}
                {proposal && refs.refsError && <p role="alert" className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-6 text-amber-900">نام بعضی دسته‌ها و ویژگی‌ها بارگذاری نشد.</p>}
                {proposal?.productType && <div className="space-y-1 rounded-xl border border-slate-200 bg-white p-3 text-sm leading-6">
                    <p className="font-bold">قیمت و موجودی</p>
                    <p>مجموع موجودی {toPersianDigits(proposal.stock ?? 0)} · قیمت مبنا {proposal.supplierBaseCost != null ? formatPrice(Number(proposal.supplierBaseCost)) + ' تومان' : 'ثبت نشده'}</p>
                    {proposal.variants?.map((variant: any, index: number) => <p key={variant.id ?? index} className="[overflow-wrap:anywhere]">تنوع {toPersianDigits(index + 1)} · {skuReviewLabel(proposal, variant)}</p>)}
                </div>}
                {(!loading && !loadError && fields.length > 0 || canRejectProduct) && (
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        {fields.length > 0 && <p className="text-sm leading-6 text-slate-700">{toPersianDigits(pendingCount)} در انتظار · {toPersianDigits(rejectedCount)} نیازمند اصلاح</p>}
                        {!readOnly && baseline.some(field => field.decision === 'approved') && <button type="button" aria-pressed={showApproved}
                            onClick={() => setShowApproved(previous => !previous)} className={reviewButton}>
                            {showApproved ? 'پنهان کردن تأییدشده‌ها' : 'نمایش تأییدشده‌ها'}
                        </button>}
                        {canRejectProduct && <button type="button" disabled={!!action} onClick={() => { setActionError(''); setRejecting(true); }}
                            className="min-h-11 rounded-xl px-3 text-sm font-bold text-rose-800 hover:bg-rose-50 focus-visible:outline-2 focus-visible:outline-rose-700 disabled:opacity-50">رد کل کالا</button>}
                    </div>
                )}
                {!loading && !loadError && groups.map(group => (
                    <section key={group.id} aria-label={group.title} className="space-y-2">
                        <h3 className="sticky top-0 z-10 bg-slate-50 py-2 text-sm font-extrabold">{group.title} <span className="font-normal text-slate-600">({toPersianDigits(group.fields.length)})</span></h3>
                        {group.id === 'images' ? <>
                            <ImageGallery fields={group.fields} refs={refs} title={title || ''} disabled={!!action} readOnly={readOnly}
                                onDecision={setFieldDecision} onReason={setFieldReason} />
                            {group.fields.filter(field => !imageFieldUrl(field, refs)).map(renderFieldCard)}
                        </> : group.fields.map(renderFieldCard)}
                    </section>
                ))}
                {!loading && !loadError && !fields.length && <p className="rounded-xl bg-white p-3 text-sm text-slate-700">فیلدی برای بازبینی ثبت نشده است.</p>}
                {!loading && publishReady && !visibleFields.length && <p className="rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-800">همهٔ موارد تأیید شده‌اند.</p>}
            </div>
            {!readOnly && !loading && !loadError && fields.length > 0 && <footer className="shrink-0 space-y-2 border-t border-slate-200 bg-white p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:p-4">
                {errorMessage}
                {action && <p role="status" className="text-xs font-bold text-slate-700">{progress[action]}</p>}
                {notice && <p role="status" className="text-sm font-bold text-emerald-800">{notice}</p>}
                <div className="flex flex-wrap items-center justify-between gap-x-2 text-xs leading-5 text-slate-700">
                    <p><strong>{toPersianDigits(approvedCount)} از {toPersianDigits(fields.length)}</strong> مورد تأیید شده</p>
                    {dirty.length > 0 && <p>{toPersianDigits(dirty.length)} تغییر ذخیره‌نشده</p>}
                    {rejectedWithoutReason && <p className="font-bold text-rose-800">برای موارد ردشده دلیل بنویسید.</p>}
                </div>
                <div className="grid grid-cols-2 gap-2 sm:flex sm:justify-end">
                    <button type="button" disabled={unavailable || !dirty.length || rejectedWithoutReason}
                        onClick={() => void save()} className={reviewButton}>{rejectedCount ? 'ارسال برای اصلاح' : 'ذخیره تصمیم‌ها'}</button>
                    <button type="button" disabled={unavailable || !publishReady}
                        onClick={() => void save(true)} className={reviewPrimary}>تأیید نهایی و انتشار</button>
                </div>
            </footer>}
        </>}
    </dialog>;
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
