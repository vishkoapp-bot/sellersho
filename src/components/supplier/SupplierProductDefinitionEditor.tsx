import React, { useEffect, useId, useState } from 'react';
import { catalogApi } from '../../services/catalogApi';
import { supplierApi } from '../../services/supplierApi';
import type { CatalogProductAttribute, ProductAttribute, ProductSchema } from '../../types/productSchema';
import { MAX_PRODUCT_COMBINATIONS, MAX_MATRIX_ROWS } from '../../types/productSchema';
import { buildProductMatrix, DraftProductSKU, newUuid, selectionKey } from '../../services/productModel';

// Thousands separators, copied verbatim from SupplierProposeForm:
// state stays raw Latin digits; only the DISPLAYED value is grouped
// with ٬ (U+066C) and Persian digits. serializeProductDraft expects
// raw digits, so nothing downstream ever sees the separators.
export const formatSep = (raw: string): string => {
  const digits = raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
  if (digits === '') return '';
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬').replace(/\d/g, (ch) => '۰۱۲۳۴۵۶۷۸۹'[parseInt(ch, 10)]);
};

export const parseSep = (raw: string): string =>
  raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');

// P2(b) class vocabularies — the backend enum (P2(b) spec). Until the
// backend serves a list, these fixed options are what is stored; the
// classes are never displayed on the PDP.
export const SHIPPING_CLASS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'پیش‌فرض (ارث از کالا)' },
  { value: 'standard', label: 'استاندارد' },
  { value: 'fragile', label: 'شکننده' },
  { value: 'oversized', label: 'بزرگ‌ابعاد' },
  { value: 'heavy', label: 'سنگین' },
  { value: 'cold_chain', label: 'یخچالی' },
];
export const TAX_CLASS_OPTIONS: { value: string; label: string }[] = [
  { value: '', label: 'پیش‌فرض (ارث از کالا)' },
  { value: 'standard', label: 'استاندارد' },
  { value: 'reduced', label: 'کاهش‌یافته' },
  { value: 'zero', label: 'صفر' },
  { value: 'exempt', label: 'معاف' },
];

type SKUDetailField = 'stock' | 'supplier_base_cost' | 'weight_grams' | 'dimensions' | 'sale_price' | 'image' | 'shipping_class' | 'tax_class';

// P0/P1(c)/P2(b) per-variant details, collapsed by default so matrix
// rows stay as compact as today. Shared by the propose form (draft
// rows) and the published-variants modal (via the same field names).
export function VariantDetailsEditor({ row, onChange, sessionToken, canEdit, onError }: {
  row: DraftProductSKU;
  onChange: (patch: Partial<DraftProductSKU>) => void;
  sessionToken?: string;
  canEdit: (field: SKUDetailField) => boolean;
  onError: (message: string) => void;
}) {
  const [uploading, setUploading] = useState(false);
  const toLatin = (raw: string) => raw.replace(/[۰-۹]/g, (c) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c)));
  const numInput = (value: string, patch: (v: string) => void) => ({
    value, inputMode: 'numeric' as const,
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => patch(toLatin(e.target.value).replace(/[^0-9]/g, '')),
  });
  const uploadFiles = async (files: File[]) => {
    if (!canEdit('image')) return;
    if (row.imageUrl && row.images.length >= 3) { onError('هر تنوع حداکثر ۱ تصویر اصلی و ۳ تصویر گالری دارد.'); return; }
    for (const file of files) {
      if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) { onError('فرمت تصویر نامعتبر است. فقط فرمت‌های JPG، PNG و WEBP مجاز هستند.'); return; }
      if (file.size > 2 * 1024 * 1024) { onError('حجم هر فایل تصویر نباید بیشتر از ۲ مگابایت باشد.'); return; }
    }
    setUploading(true);
    try {
      let hero = row.imageUrl;
      const gallery = [...row.images];
      for (const file of files) {
        const { imageUrl } = await supplierApi.uploadVariantImage(file, sessionToken);
        if (!hero) hero = imageUrl;
        else if (gallery.length < 3) gallery.push(imageUrl);
      }
      onChange({ imageUrl: hero, images: gallery });
    } catch (cause: unknown) {
      onError(cause instanceof Error ? cause.message : 'خطا در آپلود تصویر');
    } finally {
      setUploading(false);
    }
  };
  const inputCls = 'w-full rounded-lg border p-2 mt-1 text-xs bg-white';
  const canImage = canEdit('image');
  return (
    <details className="rounded-xl border border-slate-200 bg-slate-50 p-2.5">
      <summary className="text-xs font-bold cursor-pointer">تصویر، وزن و حراج این تنوع</summary>
      <div className="space-y-3 pt-2">
        <div>
          <span className="text-xs font-bold block">تصویر تنوع</span>
          <div className="flex flex-wrap items-center gap-2 pt-1">
            {row.imageUrl && (
              <span className="relative inline-block w-12 h-12 rounded-lg overflow-hidden border border-slate-200 bg-white">
                <img src={row.imageUrl} alt="تصویر اصلی تنوع" className="w-full h-full object-cover" />
                <span className="absolute bottom-0 inset-x-0 bg-amber-400 text-amber-950 text-[8px] font-bold text-center">شاخص</span>
                <button type="button" disabled={!canImage} onClick={() => onChange({ imageUrl: row.images[0] ?? '', images: row.images.slice(1) })} className="absolute top-0 left-0 bg-rose-500 text-white text-[9px] px-1 rounded-bl" aria-label="حذف تصویر اصلی تنوع">✕</button>
              </span>
            )}
            {row.images.map((src, i) => (
              <span key={i} className="relative inline-block w-12 h-12 rounded-lg overflow-hidden border border-slate-200 bg-white">
                <img src={src} alt={`تصویر گالری ${i + 1} تنوع`} className="w-full h-full object-cover" />
                <button type="button" disabled={!canImage} onClick={() => onChange({ imageUrl: src, images: [row.imageUrl, ...row.images.filter((_, j) => j !== i)].filter(Boolean) })} className="absolute bottom-0 inset-x-0 bg-white/90 text-[8px] font-bold text-violet-700" title="شاخص شود">شاخص شود</button>
                <button type="button" disabled={!canImage} onClick={() => onChange({ images: row.images.filter((_, j) => j !== i) })} className="absolute top-0 left-0 bg-rose-500 text-white text-[9px] px-1 rounded-bl" aria-label="حذف تصویر گالری تنوع">✕</button>
              </span>
            ))}
            <label className={`rounded-lg border-2 border-dashed border-slate-300 bg-white px-2 py-2 text-[11px] font-bold text-violet-700 ${canImage && !uploading ? 'cursor-pointer' : 'opacity-50'}`}>
              {uploading ? 'در حال آپلود…' : '+ افزودن تصویر تنوع'}
              <input type="file" accept="image/jpeg,image/png,image/webp" multiple disabled={!canImage || uploading} onChange={(e) => { const files = e.target.files ? Array.from(e.target.files) : []; if (files.length) void uploadFiles(files); e.target.value = ''; }} className="hidden" />
            </label>
          </div>
          <p className="text-[10px] text-slate-500 pt-1">۱ تصویر اصلی + تا ۳ تصویر گالری؛ خالی یعنی همان تصویر کالا.</p>
        </div>
        <div>
          <span className="text-xs font-bold block">وزن و ابعاد این تنوع</span>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 pt-1">
            <label className="text-xs">وزن (گرم)<input aria-label={'وزن تنوع ' + row.name} disabled={!canEdit('weight_grams')} placeholder="ارث از محصول" className={inputCls} {...numInput(row.weightGrams, (v) => onChange({ weightGrams: v }))} /></label>
            <label className="text-xs">طول (سانتی‌متر)<input aria-label={'طول تنوع ' + row.name} disabled={!canEdit('dimensions')} placeholder="ارث از محصول" className={inputCls} {...numInput(row.lengthCm, (v) => onChange({ lengthCm: v }))} /></label>
            <label className="text-xs">عرض (سانتی‌متر)<input aria-label={'عرض تنوع ' + row.name} disabled={!canEdit('dimensions')} placeholder="ارث از محصول" className={inputCls} {...numInput(row.widthCm, (v) => onChange({ widthCm: v }))} /></label>
            <label className="text-xs">ارتفاع (سانتی‌متر)<input aria-label={'ارتفاع تنوع ' + row.name} disabled={!canEdit('dimensions')} placeholder="ارث از محصول" className={inputCls} {...numInput(row.heightCm, (v) => onChange({ heightCm: v }))} /></label>
          </div>
          <p className="text-[10px] text-slate-500 pt-1">خالی یعنی همان وزن و ابعاد کالا.</p>
        </div>
        <div>
          <span className="text-xs font-bold block">حراج این تنوع</span>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 pt-1">
            <label className="text-xs">قیمت حراج (تومان)<input aria-label={'قیمت حراج تنوع ' + row.name} disabled={!canEdit('sale_price')} inputMode="numeric" placeholder="بدون حراج" className={inputCls} value={formatSep(row.salePrice)} onChange={(e) => onChange({ salePrice: parseSep(e.target.value) })} /></label>
            <label className="text-xs">شروع حراج<input aria-label={'شروع حراج تنوع ' + row.name} disabled={!canEdit('sale_price')} type="datetime-local" className={inputCls} value={row.saleFrom} onChange={(e) => onChange({ saleFrom: e.target.value })} /></label>
            <label className="text-xs">پایان حراج<input aria-label={'پایان حراج تنوع ' + row.name} disabled={!canEdit('sale_price')} type="datetime-local" className={inputCls} value={row.saleTo} onChange={(e) => onChange({ saleTo: e.target.value })} /></label>
          </div>
          <p className="text-[10px] text-slate-500 pt-1">قیمت حراج باید کمتر از قیمت عادی تنوع باشد؛ خالی یعنی بدون حراج.</p>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <label className="text-xs">کلاس حمل‌ونقل<select aria-label={'کلاس حمل‌ونقل تنوع ' + row.name} disabled={!canEdit('shipping_class')} value={row.shippingClass} onChange={(e) => onChange({ shippingClass: e.target.value })} className={inputCls}>{SHIPPING_CLASS_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></label>
          <label className="text-xs">کلاس مالیاتی<select aria-label={'کلاس مالیاتی تنوع ' + row.name} disabled={!canEdit('tax_class')} value={row.taxClass} onChange={(e) => onChange({ taxClass: e.target.value })} className={inputCls}>{TAX_CLASS_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></label>
        </div>
      </div>
    </details>
  );
}
interface Props {
    categorySlug: string | null;
    productType: 'simple' | 'variable';
    schema: ProductSchema;
    onSchema: (schema: ProductSchema) => void;
    rows: DraftProductSKU[];
    onRows: (rows: DraftProductSKU[]) => void;
    brand: string;
    noBrand: boolean;
    onBrand: (brand: string) => void;
    onNoBrand: (value: boolean) => void;
    schemaEditable?: boolean;
    hideBrand?: boolean;
    skuStructureEditable?: boolean;
    attributeListEditable?: boolean;
    canEditAttribute?: (id: string) => boolean;
    canEditSKUValue?: (id: string, field: SKUDetailField) => boolean;
    canEditSKUSelection?: (id: string, attributeID: string) => boolean;
    // Supplier session token for variant-image uploads (the propose form
    // already holds it; the editor must not fetch() the upload itself).
    sessionToken?: string;
}
// Attribute picker (checkbox list, the primary path): ticking a box
// adds the attribute at once — as a variant axis for variable
// products (so it builds combinations), as a fixed attribute for
// simple products (today's behaviour there). Unticking removes it,
// exactly like the per-attribute «حذف ویژگی» button. Pure so the
// tick-two-at-once flow is unit-testable without a DOM.
export function toggleSchemaAttribute(schema: ProductSchema, definition: CatalogProductAttribute, productType: 'simple' | 'variable', checked: boolean): ProductSchema {
    if (checked) {
        if (schema.attributes.some(a => a.id === definition.id)) return schema;
        return { version: 2, type: productType, attributes: [...schema.attributes, { ...definition, role: productType === 'variable' ? 'axis' : 'fixed', values: [] }] };
    }
    return { version: 2, type: productType, attributes: schema.attributes.filter(a => a.id !== definition.id) };
}
export function SupplierProductDefinitionEditor({ categorySlug, productType, schema, onSchema, rows, onRows, brand, noBrand, onBrand, onNoBrand, schemaEditable = true, hideBrand = false, skuStructureEditable = true, attributeListEditable = true, canEditAttribute = () => true, canEditSKUValue = () => true, canEditSKUSelection, sessionToken }: Props) {
    const [catalog, setCatalog] = useState<CatalogProductAttribute[]>([]);
    const [brands, setBrands] = useState<{
        id: number;
        titleFa: string;
        titleEn: string;
    }[]>([]);
    const [loading, setLoading] = useState(false);
    const [failure, setFailure] = useState('');
    const [retry, setRetry] = useState(0);
    const [requesting, setRequesting] = useState(false);
    const [requestName, setRequestName] = useState('');
    const [requestValues, setRequestValues] = useState('');
    const [requestRole, setRequestRole] = useState<'fixed' | 'axis'>('axis');
    const [newOptions, setNewOptions] = useState<Record<string, string>>({});
    const [page, setPage] = useState(0);
    const [bulkCost, setBulkCost] = useState('');
    const [bulkStock, setBulkStock] = useState('');
    // P1(b) manual subset: false = build every combination (today's
    // behaviour); true = only the checked rows are created.
    const [manualMode, setManualMode] = useState(false);
    // B2: radio group for the product-level default variant (one per
    // product); useId so two mounted editors never share a group.
    const defaultGroupId = useId();
    useEffect(() => { let active = true; setCatalog([]); setBrands([]); setFailure(''); if (!categorySlug) {
        setLoading(false);
        return;
    } setLoading(true); catalogApi.getProductAttributes(categorySlug).then(data => { if (active) {
        setCatalog(data.attributes);
        setBrands(data.brands);
    } }).catch(e => { if (active)
        setFailure(e.message || 'بارگذاری ویژگی‌ها ناموفق بود.'); }).finally(() => { if (active)
        setLoading(false); }); return () => { active = false; }; }, [categorySlug, retry]);
    const definitions = catalog.map(a => ({ ...a, values: [...a.values] }));
    for (const draft of schema.attributes) {
        const existing = definitions.find(a => a.id === draft.id);
        if (existing) {
            const values = new Map(existing.values.map(value => [value.id, value]));
            for (const value of draft.values)
                values.set(value.id, value);
            existing.values = [...values.values()];
        }
        else
            definitions.push({ ...draft, values: [...draft.values] });
    }
    const update = (attributes: ProductAttribute[]) => onSchema({ version: 2, type: productType, attributes });
    const axes = schema.attributes.filter(a => a.role === 'axis');
    const count = axes.reduce((n, a) => n * a.values.length, 1);
    const pages = Math.max(1, Math.ceil(rows.length / 20));
    const toggleAttribute = (id: string, checked: boolean) => { const a = definitions.find(a => a.id === id); if (a)
        update(toggleSchemaAttribute(schema, a, productType, checked).attributes); };
    const setValues = (id: string, valueID: string) => update(schema.attributes.map(a => { if (a.id !== id)
        return a; const option = definitions.find(c => c.id === id)?.values.find(v => v.id === valueID) ?? a.values.find(v => v.id === valueID); if (!option)
        return a; return { ...a, values: a.role === 'fixed' ? [option] : a.values.some(v => v.id === valueID) ? a.values.filter(v => v.id !== valueID) : [...a.values, option] }; }));
    const generate = () => { try {
        onRows(buildProductMatrix({ ...schema, type: productType }, rows));
        setFailure('');
        setPage(0);
    }
    catch (e) {
        setFailure(e instanceof Error ? e.message : 'ساخت ترکیب‌ها ناموفق بود.');
    } };
    const addRequest = () => { const name = requestName.trim(); const values = [...new Set(requestValues.split(/[\n،,]/).map(v => v.trim()).filter(Boolean))]; if (!name || !values.length || schema.attributes.some(a => a.name.trim() === name)) {
        setFailure('نام یکتا و مقدارهای ویژگی را وارد کنید.');
        return;
    } if ((productType === 'simple' || requestRole === 'fixed') && values.length !== 1) {
        setFailure('ویژگی ثابت فقط یک مقدار دارد.');
        return;
    } const a: ProductAttribute = { id: 'requested:' + newUuid(), name, source: 'requested', role: productType === 'simple' ? 'fixed' : requestRole, values: values.map(label => ({ id: 'requestedv:' + newUuid(), label })) }; setCatalog(p => [...p, a]); update([...schema.attributes, a]); setRequestName(''); setRequestValues(''); setRequesting(false); setFailure(''); };
    const matrixCurrent = (() => { try {
        const generated = buildProductMatrix({ ...schema, type: productType }, rows);
        return generated.length === rows.length && generated.every(g => rows.some(v => selectionKey(v.selection) === selectionKey(g.selection)));
    }
    catch {
        return false;
    } })();
    return <section className="rounded-2xl border border-slate-200 bg-white p-3 sm:p-4 space-y-4" aria-label="تعریف یکپارچه محصول">
 <header><h4 className="font-bold text-sm">ویژگی‌های محصول و تنوع‌ها</h4><p className="text-xs text-slate-500 leading-6 mt-1">ویژگی‌ها را همین‌جا یکجا تیک بزنید، بعد مقدار هر کدام را انتخاب کنید و با «ساخت / به‌روزرسانی ترکیب‌ها» همهٔ ترکیب‌ها را یکجا بسازید. مشخص کنید مقدار هر ویژگی برای همهٔ کالا ثابت است یا تنوع‌های جدا می‌سازد.</p></header>
 {!categorySlug ? <p className="text-xs text-amber-800 bg-amber-50 rounded-xl p-3">ابتدا دسته و زیردستهٔ نهایی را انتخاب کنید.</p> : <>
 {loading && <p role="status" className="text-xs">در حال دریافت ویژگی‌های همین دسته…</p>}
 {failure && <div role="alert" className="rounded-xl bg-rose-50 p-3 text-xs text-rose-700">{failure}<button type="button" onClick={() => setRetry(n => n + 1)} className="underline mx-2">دریافت دوباره فهرست</button></div>}
 {!hideBrand && <><label className="block text-xs font-bold">برند<select aria-label="برند محصول" disabled={loading || noBrand} value={brand} onChange={e => onBrand(e.target.value)} className="w-full rounded-xl border p-3 mt-1 bg-white disabled:opacity-40"><option value="">انتخاب برند همین دسته</option>{brands.map(b => <option key={b.id} value={b.titleFa}>{b.titleFa}{b.titleEn && b.titleEn !== b.titleFa ? ` (${b.titleEn})` : ''}</option>)}</select></label>
 <label className="flex gap-2 text-xs"><input type="checkbox" checked={noBrand} onChange={e => onNoBrand(e.target.checked)}/>بدون برند</label></>}
 <fieldset disabled={loading || !schemaEditable || !attributeListEditable} className="rounded-xl border p-3 space-y-2" aria-label="انتخاب ویژگی‌های محصول"><legend className="font-bold text-xs px-2">{productType === 'variable' ? 'ویژگی‌های متغیر را یکجا تیک بزنید' : 'ویژگی‌های محصول را تیک بزنید'}</legend><p className="text-[11px] text-slate-500 leading-6">{productType === 'variable' ? 'هر ویژگی تیک‌خورده یک محور تنوع می‌شود؛ مقدارهایش را پایین انتخاب کنید.' : 'هر ویژگی تیک‌خورده یک ویژگی ثابت می‌شود؛ مقدارش را پایین انتخاب کنید.'}</p>{definitions.length === 0 ? <p className="text-xs text-slate-500">برای این دسته ویژگی‌ای ثبت نشده است؛ می‌توانید پایین ویژگی درخواست کنید.</p> : definitions.map(a => { const chosen = schema.attributes.find(v => v.id === a.id); return <label key={a.id} className={`flex items-center gap-2 rounded-lg border p-2 text-xs cursor-pointer ${chosen ? 'bg-violet-50 border-violet-400' : 'bg-white'}`}><input type="checkbox" aria-label={'انتخاب ویژگی ' + a.name} checked={!!chosen} disabled={loading || !schemaEditable || !attributeListEditable || !canEditAttribute(a.id)} onChange={e => toggleAttribute(a.id, e.target.checked)} /><span className="font-bold">{a.name}</span><span className="text-slate-500">{a.values.length} مقدار</span>{chosen && <span className="text-violet-700">{chosen.role === 'axis' ? 'محور تنوع' : 'ثابت'}</span>}</label>; })}</fieldset>
 {schema.attributes.map(a => {
                const all = definitions.find(c => c.id === a.id)?.values ?? a.values;
                return <fieldset disabled={!schemaEditable || !canEditAttribute(a.id)} key={a.id} data-attribute-id={a.id} className="rounded-xl border p-3 space-y-3"><legend className="font-bold text-xs px-2">{a.name}{a.source === 'requested' ? ' — درخواست جدید همراه محصول' : ''}</legend>{(a.source === 'requested' || a.source === 'local') && <label className="text-xs block">نام ویژگی<input aria-label={'نام ویژگی ' + a.name} value={a.name} onChange={e => update(schema.attributes.map(attribute => attribute.id === a.id ? { ...attribute, name: e.target.value } : attribute))} className="w-full border rounded-lg p-2 mt-1"/></label>}<div className="flex flex-wrap gap-3 items-center text-xs"><label className="flex gap-1"><input type="radio" name={'role-' + a.id} checked={a.role === 'fixed'} onChange={() => update(schema.attributes.map(v => v.id === a.id ? { ...v, role: 'fixed', values: v.values.slice(0, 1) } : v))}/>ویژگی ثابت</label>{productType === 'variable' && <label className="flex gap-1"><input type="radio" name={'role-' + a.id} checked={a.role === 'axis'} onChange={() => update(schema.attributes.map(v => v.id === a.id ? { ...v, role: 'axis' } : v))}/>محور تنوع — چند مقدار</label>}<button type="button" disabled={!attributeListEditable} onClick={() => update(schema.attributes.filter(v => v.id !== a.id))} className="text-rose-700 mr-auto">حذف ویژگی</button></div>
 {a.role === 'fixed' ? <select aria-label={'مقدار ' + a.name} value={a.values[0]?.id ?? ''} onChange={e => setValues(a.id, e.target.value)} className="w-full rounded-xl border p-2.5 text-xs"><option value="">انتخاب یک مقدار</option>{all.map(v => <option key={v.id} value={v.id}>{v.label}</option>)}</select> : <div className="max-h-48 overflow-auto flex flex-wrap gap-2">{all.map(v => <label key={v.id} className={`flex items-center gap-2 rounded-lg border p-2 text-xs cursor-pointer ${a.values.some(x => x.id === v.id) ? 'bg-violet-50 border-violet-400' : 'bg-white'}`}><input type="checkbox" checked={a.values.some(x => x.id === v.id)} onChange={() => setValues(a.id, v.id)}/>{v.label}</label>)}</div>}
 {a.values.filter(value => a.source === 'requested' || a.source === 'local' || value.id.startsWith('requestedv:')).map(value => <label key={value.id} className="text-xs block">نام مقدار پیشنهادی<input aria-label={'نام مقدار ' + value.label + ' در ' + a.name} value={value.label} onChange={e => update(schema.attributes.map(attribute => attribute.id === a.id ? { ...attribute, values: attribute.values.map(option => option.id === value.id ? { ...option, label: e.target.value } : option) } : attribute))} className="w-full border rounded-lg p-2 mt-1"/></label>)}
 <div className="flex flex-wrap gap-2 text-xs"><input aria-label={'درخواست مقدار جدید ' + a.name} value={newOptions[a.id] ?? ''} onChange={e => setNewOptions(p => ({ ...p, [a.id]: e.target.value }))} placeholder="مقدار موجود نیست؟" className="min-w-0 flex-1 border rounded-lg p-2"/><button type="button" onClick={() => { const label = newOptions[a.id]?.trim(); if (!label || all.some(v => v.label.trim() === label)) {
                    setFailure('مقدار جدید و غیرتکراری بنویسید.');
                    return;
                } const option = { id: 'requestedv:' + newUuid(), label }; setCatalog(p => p.map(c => c.id === a.id ? { ...c, values: [...c.values, option] } : c)); update(schema.attributes.map(c => c.id === a.id ? { ...c, values: c.role === 'fixed' ? [option] : [...c.values, option] } : c)); setNewOptions(p => ({ ...p, [a.id]: '' })); }} className="border rounded-lg p-2 text-violet-700">درخواست و استفاده در محصول</button></div>
 <p className="text-[11px] text-slate-500">{a.role === 'fixed' ? 'این مقدار برای کل محصول یکسان است و دوباره در تنوع‌ها پرسیده نمی‌شود.' : `${a.values.length} مقدار انتخاب شده؛ هر ترکیب، قیمت و موجودی مستقل خواهد داشت.`}</p>
 </fieldset>;
            })}
 <button type="button" disabled={!schemaEditable || !attributeListEditable} onClick={() => setRequesting(v => !v)} className="text-xs font-bold text-violet-700">+ درخواست ویژگی یا محور موجودنیست</button>
 {requesting && <div className="rounded-xl bg-amber-50 border border-amber-200 p-3 space-y-2"><p className="text-xs leading-6">مثلاً «سایز» را با مقدارهای S، M و L درخواست کنید. همین حالا در محصول قابل استفاده است و همراه آن بازبینی می‌شود.</p><input aria-label="نام ویژگی پیشنهادی" value={requestName} onChange={e => setRequestName(e.target.value)} placeholder="نام ویژگی، مثلاً سایز" className="w-full rounded-lg border p-2 text-xs"/><textarea aria-label="مقدارهای ویژگی پیشنهادی" value={requestValues} onChange={e => setRequestValues(e.target.value)} placeholder="هر مقدار در یک خط" className="w-full rounded-lg border p-2 text-xs"/>{productType === 'variable' && <select aria-label="نقش ویژگی پیشنهادی" value={requestRole} onChange={e => setRequestRole(e.target.value as 'fixed' | 'axis')} className="rounded-lg border p-2 text-xs"><option value="axis">محور تنوع</option><option value="fixed">ویژگی ثابت</option></select>}<button type="button" onClick={addRequest} className="rounded-lg bg-amber-900 text-white p-2 text-xs">افزودن درخواست به محصول</button></div>}
 <div className="rounded-xl bg-slate-50 border p-3 space-y-2"><p className="text-xs leading-6">{productType === 'simple' ? 'محصول تکی: یک قیمت و موجودی مستقل.' : `${axes.length} محور × مقدارهای انتخاب‌شده = ${count} ترکیب (نمایش تا ${MAX_MATRIX_ROWS}؛ ثبت تا ${MAX_PRODUCT_COMBINATIONS} ترکیب انتخاب‌شده)`}</p>{productType === 'variable' && <div className="flex flex-wrap gap-3 text-xs"><label className="flex gap-1"><input type="radio" name="subset-mode" checked={!manualMode} onChange={() => setManualMode(false)} />همه‌ی ترکیب‌ها ساخته شوند</label><label className="flex gap-1"><input type="radio" name="subset-mode" checked={manualMode} onChange={() => setManualMode(true)} />فقط ترکیب‌هایی که انتخاب می‌کنم ساخته شوند</label></div>}<button type="button" disabled={!skuStructureEditable || loading || count > MAX_MATRIX_ROWS} onClick={generate} className="rounded-xl bg-violet-700 text-white p-3 text-xs font-bold disabled:opacity-40">ساخت / به‌روزرسانی ترکیب‌ها</button>{rows.length > 0 && !matrixCurrent && <p role="status" className="text-xs text-amber-800">ویژگی‌ها تغییر کرده‌اند؛ ترکیب‌ها را به‌روز کنید. قیمت و موجودی ترکیب‌های مشترک حفظ می‌شود.</p>}</div>
 {rows.length > 0 && <>{manualMode && <div className="flex flex-wrap gap-2 items-center text-xs"><button type="button" onClick={() => onRows(rows.map(v => ({ ...v, selected: true })))} className="border rounded-lg p-2">انتخاب همه</button><button type="button" onClick={() => onRows(rows.map(v => ({ ...v, selected: false })))} className="border rounded-lg p-2">پاک کردن انتخاب</button><span className="text-slate-500">{rows.filter(r => r.selected !== false).length} از {rows.length} ترکیب انتخاب شده</span></div>}<div className="flex flex-wrap gap-2 items-center text-xs"><input aria-label="قیمت گروهی" disabled={!rows.some(row => row.active && canEditSKUValue(row.id, 'supplier_base_cost'))} inputMode="numeric" value={formatSep(bulkCost)} onChange={e => setBulkCost(parseSep(e.target.value))} placeholder="قیمت برای همهٔ فعال‌ها" className="min-w-0 flex-1 border rounded-lg p-2"/><input aria-label="موجودی گروهی" disabled={!rows.some(row => row.active && canEditSKUValue(row.id, 'stock'))} inputMode="numeric" value={formatSep(bulkStock)} onChange={e => setBulkStock(parseSep(e.target.value))} placeholder="موجودی برای همهٔ فعال‌ها" className="min-w-0 flex-1 border rounded-lg p-2"/><button type="button" onClick={() => onRows(rows.map(v => v.active ? { ...v, ...(bulkCost.trim() && canEditSKUValue(v.id, 'supplier_base_cost') ? { cost: bulkCost.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : {}), ...(bulkStock.trim() && canEditSKUValue(v.id, 'stock') ? { stock: bulkStock.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : {}) } : v))} className="border rounded-lg p-2">اعمال گروهی</button></div>
 <div className="space-y-2">{rows.slice(page * 20, page * 20 + 20).map(row => <article key={row.id} data-sku-id={row.id} className={`rounded-xl border p-3 space-y-2 ${row.active && row.selected !== false ? 'bg-white' : 'bg-slate-100 opacity-70'}`}><div className="flex flex-wrap justify-between gap-2 text-xs"><strong>{row.name}</strong><span className="flex flex-wrap gap-3"><label className="flex gap-2"><input type="checkbox" disabled={!skuStructureEditable} checked={row.selected !== false} aria-label={'ایجاد ترکیب ' + row.name} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, selected: e.target.checked } : v))}/>ایجاد شود</label>{productType === 'variable' && <label className="flex gap-2"><input type="radio" name={defaultGroupId} disabled={!skuStructureEditable || row.selected === false || !row.active} checked={row.isDefault === true && row.selected !== false && row.active} aria-label={'تنوع پیش‌فرض ' + row.name} onChange={() => onRows(rows.map(v => ({ ...v, isDefault: v.id === row.id })))} />پیش‌فرض</label>}<label className="flex gap-2"><input type="checkbox" disabled={!skuStructureEditable} checked={row.active} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, active: e.target.checked } : v))}/>این ترکیب موجود است</label></span></div>{canEditSKUSelection && axes.length > 0 && <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">{axes.map(axis => <label key={axis.id} className="text-xs">{axis.name}<select aria-label={'انتخاب ' + axis.name + ' برای ' + row.name} disabled={!canEditSKUSelection(row.id, axis.id)} value={row.selection[axis.id] ?? ''} onChange={e => onRows(rows.map(item => { if (item.id !== row.id)
                return item; const selection = { ...item.selection, [axis.id]: e.target.value }; const name = axes.map(attribute => `${attribute.name}: ${attribute.values.find(value => value.id === selection[attribute.id])?.label ?? 'انتخاب نشده'}`).join(' / '); return { ...item, selection, name }; }))} className="w-full rounded-lg border p-2 mt-1"><option value="">انتخاب مقدار</option>{axis.values.map(value => <option key={value.id} value={value.id}>{value.label}</option>)}</select></label>)}</div>}{row.active ? <div className="grid grid-cols-2 gap-2"><label className="text-xs">قیمت عمده (تومان)<input aria-label={'قیمت ' + row.name} disabled={row.selected === false || !canEditSKUValue(row.id, 'supplier_base_cost')} inputMode="numeric" value={formatSep(row.cost)} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, cost: parseSep(e.target.value) } : v))} className="w-full rounded-lg border p-2 mt-1"/></label><label className="text-xs">موجودی این ترکیب<input aria-label={'موجودی ' + row.name} disabled={row.selected === false || !canEditSKUValue(row.id, 'stock')} inputMode="numeric" value={formatSep(row.stock)} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, stock: parseSep(e.target.value) } : v))} className="w-full rounded-lg border p-2 mt-1"/></label></div> : <p className="text-xs">ترکیب ناموجود از فروش کنار گذاشته می‌شود؛ با فعال‌کردن دوباره، اطلاعات واردشده برمی‌گردد.</p>}{row.selected === false && <p className="text-xs">این ترکیب اصلاً ساخته نمی‌شود و در پیشنهاد فرستاده نمی‌شود.</p>}{row.active && row.selected !== false && <VariantDetailsEditor row={row} sessionToken={sessionToken} canEdit={(field) => canEditSKUValue(row.id, field)} onError={setFailure} onChange={(patch) => onRows(rows.map(v => v.id === row.id ? { ...v, ...patch } : v))} />}</article>)}</div>
 <nav aria-label="صفحه‌بندی ترکیب‌ها" className="flex justify-between items-center text-xs"><button type="button" disabled={page === 0} onClick={() => setPage(p => p - 1)} className="border rounded-lg p-2 disabled:opacity-40">قبلی</button><span>صفحهٔ {page + 1} از {pages} — {rows.filter(r => r.active).length} ترکیب فعال · {rows.filter(r => r.selected !== false).length} ترکیب برای ایجاد</span><button type="button" disabled={page + 1 >= pages} onClick={() => setPage(p => p + 1)} className="border rounded-lg p-2 disabled:opacity-40">بعدی</button></nav></>}
 </>}
 </section>;
}
