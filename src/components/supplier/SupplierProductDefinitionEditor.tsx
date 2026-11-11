import React, { useEffect, useState } from 'react';
import { catalogApi } from '../../services/catalogApi';
import type { CatalogProductAttribute, ProductAttribute, ProductSchema } from '../../types/productSchema';
import { MAX_PRODUCT_COMBINATIONS } from '../../types/productSchema';
import { buildProductMatrix, DraftProductSKU, selectionKey } from '../../services/productModel';
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
    canEditSKUValue?: (id: string, field: 'stock' | 'supplier_base_cost') => boolean;
    canEditSKUSelection?: (id: string, attributeID: string) => boolean;
}
export function SupplierProductDefinitionEditor({ categorySlug, productType, schema, onSchema, rows, onRows, brand, noBrand, onBrand, onNoBrand, schemaEditable = true, hideBrand = false, skuStructureEditable = true, attributeListEditable = true, canEditAttribute = () => true, canEditSKUValue = () => true, canEditSKUSelection }: Props) {
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
    const add = (id: string) => { const a = definitions.find(a => a.id === id); if (a)
        update([...schema.attributes, { ...a, role: 'fixed', values: [] }]); };
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
    } const a: ProductAttribute = { id: 'requested:' + crypto.randomUUID(), name, source: 'requested', role: productType === 'simple' ? 'fixed' : requestRole, values: values.map(label => ({ id: 'requestedv:' + crypto.randomUUID(), label })) }; setCatalog(p => [...p, a]); update([...schema.attributes, a]); setRequestName(''); setRequestValues(''); setRequesting(false); setFailure(''); };
    const matrixCurrent = (() => { try {
        const generated = buildProductMatrix({ ...schema, type: productType }, rows);
        return generated.length === rows.length && generated.every(g => rows.some(v => selectionKey(v.selection) === selectionKey(g.selection)));
    }
    catch {
        return false;
    } })();
    return <section className="rounded-2xl border border-slate-200 bg-white p-3 sm:p-4 space-y-4" aria-label="تعریف یکپارچه محصول">
 <header><h4 className="font-bold text-sm">ویژگی‌های محصول و تنوع‌ها</h4><p className="text-xs text-slate-500 leading-6 mt-1">هر ویژگی را فقط یک‌بار انتخاب کنید. مشخص کنید مقدار آن برای همهٔ کالا ثابت است یا تنوع‌های جدا می‌سازد.</p></header>
 {!categorySlug ? <p className="text-xs text-amber-800 bg-amber-50 rounded-xl p-3">ابتدا دسته و زیردستهٔ نهایی را انتخاب کنید.</p> : <>
 {loading && <p role="status" className="text-xs">در حال دریافت ویژگی‌های همین دسته…</p>}
 {failure && <div role="alert" className="rounded-xl bg-rose-50 p-3 text-xs text-rose-700">{failure}<button type="button" onClick={() => setRetry(n => n + 1)} className="underline mx-2">دریافت دوباره فهرست</button></div>}
 {!hideBrand && <><label className="block text-xs font-bold">برند<select aria-label="برند محصول" disabled={loading || noBrand} value={brand} onChange={e => onBrand(e.target.value)} className="w-full rounded-xl border p-3 mt-1 bg-white disabled:opacity-40"><option value="">انتخاب برند همین دسته</option>{brands.map(b => <option key={b.id} value={b.titleFa}>{b.titleFa}{b.titleEn && b.titleEn !== b.titleFa ? ` (${b.titleEn})` : ''}</option>)}</select></label>
 <label className="flex gap-2 text-xs"><input type="checkbox" checked={noBrand} onChange={e => onNoBrand(e.target.checked)}/>بدون برند</label></>}
 <select aria-label="افزودن ویژگی محصول" value="" disabled={loading || !schemaEditable || !attributeListEditable} onChange={e => add(e.target.value)} className="w-full rounded-xl border p-3 text-xs bg-slate-50"><option value="">+ انتخاب ویژگی مثل رنگ، جنس یا سایز</option>{definitions.filter(a => !schema.attributes.some(v => v.id === a.id)).map(a => <option key={a.id} value={a.id}>{a.name}</option>)}</select>
 {schema.attributes.map(a => {
                const all = definitions.find(c => c.id === a.id)?.values ?? a.values;
                return <fieldset disabled={!schemaEditable || !canEditAttribute(a.id)} key={a.id} data-attribute-id={a.id} className="rounded-xl border p-3 space-y-3"><legend className="font-bold text-xs px-2">{a.name}{a.source === 'requested' ? ' — درخواست جدید همراه محصول' : ''}</legend>{(a.source === 'requested' || a.source === 'local') && <label className="text-xs block">نام ویژگی<input aria-label={'نام ویژگی ' + a.name} value={a.name} onChange={e => update(schema.attributes.map(attribute => attribute.id === a.id ? { ...attribute, name: e.target.value } : attribute))} className="w-full border rounded-lg p-2 mt-1"/></label>}<div className="flex flex-wrap gap-3 items-center text-xs"><label className="flex gap-1"><input type="radio" name={'role-' + a.id} checked={a.role === 'fixed'} onChange={() => update(schema.attributes.map(v => v.id === a.id ? { ...v, role: 'fixed', values: v.values.slice(0, 1) } : v))}/>ویژگی ثابت</label>{productType === 'variable' && <label className="flex gap-1"><input type="radio" name={'role-' + a.id} checked={a.role === 'axis'} onChange={() => update(schema.attributes.map(v => v.id === a.id ? { ...v, role: 'axis' } : v))}/>محور تنوع — چند مقدار</label>}<button type="button" disabled={!attributeListEditable} onClick={() => update(schema.attributes.filter(v => v.id !== a.id))} className="text-rose-700 mr-auto">حذف ویژگی</button></div>
 {a.role === 'fixed' ? <select aria-label={'مقدار ' + a.name} value={a.values[0]?.id ?? ''} onChange={e => setValues(a.id, e.target.value)} className="w-full rounded-xl border p-2.5 text-xs"><option value="">انتخاب یک مقدار</option>{all.map(v => <option key={v.id} value={v.id}>{v.label}</option>)}</select> : <div className="max-h-48 overflow-auto flex flex-wrap gap-2">{all.map(v => <label key={v.id} className={`flex items-center gap-2 rounded-lg border p-2 text-xs cursor-pointer ${a.values.some(x => x.id === v.id) ? 'bg-violet-50 border-violet-400' : 'bg-white'}`}><input type="checkbox" checked={a.values.some(x => x.id === v.id)} onChange={() => setValues(a.id, v.id)}/>{v.label}</label>)}</div>}
 {a.values.filter(value => a.source === 'requested' || a.source === 'local' || value.id.startsWith('requestedv:')).map(value => <label key={value.id} className="text-xs block">نام مقدار پیشنهادی<input aria-label={'نام مقدار ' + value.label + ' در ' + a.name} value={value.label} onChange={e => update(schema.attributes.map(attribute => attribute.id === a.id ? { ...attribute, values: attribute.values.map(option => option.id === value.id ? { ...option, label: e.target.value } : option) } : attribute))} className="w-full border rounded-lg p-2 mt-1"/></label>)}
 <div className="flex flex-wrap gap-2 text-xs"><input aria-label={'درخواست مقدار جدید ' + a.name} value={newOptions[a.id] ?? ''} onChange={e => setNewOptions(p => ({ ...p, [a.id]: e.target.value }))} placeholder="مقدار موجود نیست؟" className="min-w-0 flex-1 border rounded-lg p-2"/><button type="button" onClick={() => { const label = newOptions[a.id]?.trim(); if (!label || all.some(v => v.label.trim() === label)) {
                    setFailure('مقدار جدید و غیرتکراری بنویسید.');
                    return;
                } const option = { id: 'requestedv:' + crypto.randomUUID(), label }; setCatalog(p => p.map(c => c.id === a.id ? { ...c, values: [...c.values, option] } : c)); update(schema.attributes.map(c => c.id === a.id ? { ...c, values: c.role === 'fixed' ? [option] : [...c.values, option] } : c)); setNewOptions(p => ({ ...p, [a.id]: '' })); }} className="border rounded-lg p-2 text-violet-700">درخواست و استفاده در محصول</button></div>
 <p className="text-[11px] text-slate-500">{a.role === 'fixed' ? 'این مقدار برای کل محصول یکسان است و دوباره در تنوع‌ها پرسیده نمی‌شود.' : `${a.values.length} مقدار انتخاب شده؛ هر ترکیب، قیمت و موجودی مستقل خواهد داشت.`}</p>
 </fieldset>;
            })}
 <button type="button" disabled={!schemaEditable || !attributeListEditable} onClick={() => setRequesting(v => !v)} className="text-xs font-bold text-violet-700">+ درخواست ویژگی یا محور موجودنیست</button>
 {requesting && <div className="rounded-xl bg-amber-50 border border-amber-200 p-3 space-y-2"><p className="text-xs leading-6">مثلاً «سایز» را با مقدارهای S، M و L درخواست کنید. همین حالا در محصول قابل استفاده است و همراه آن بازبینی می‌شود.</p><input aria-label="نام ویژگی پیشنهادی" value={requestName} onChange={e => setRequestName(e.target.value)} placeholder="نام ویژگی، مثلاً سایز" className="w-full rounded-lg border p-2 text-xs"/><textarea aria-label="مقدارهای ویژگی پیشنهادی" value={requestValues} onChange={e => setRequestValues(e.target.value)} placeholder="هر مقدار در یک خط" className="w-full rounded-lg border p-2 text-xs"/>{productType === 'variable' && <select aria-label="نقش ویژگی پیشنهادی" value={requestRole} onChange={e => setRequestRole(e.target.value as 'fixed' | 'axis')} className="rounded-lg border p-2 text-xs"><option value="axis">محور تنوع</option><option value="fixed">ویژگی ثابت</option></select>}<button type="button" onClick={addRequest} className="rounded-lg bg-amber-900 text-white p-2 text-xs">افزودن درخواست به محصول</button></div>}
 <div className="rounded-xl bg-slate-50 border p-3 space-y-2"><p className="text-xs leading-6">{productType === 'simple' ? 'محصول تکی: یک قیمت و موجودی مستقل.' : `${axes.length} محور × مقدارهای انتخاب‌شده = ${count} ترکیب (حداکثر ${MAX_PRODUCT_COMBINATIONS})`}</p><button type="button" disabled={!skuStructureEditable || loading || count > MAX_PRODUCT_COMBINATIONS} onClick={generate} className="rounded-xl bg-violet-700 text-white p-3 text-xs font-bold disabled:opacity-40">ساخت / به‌روزرسانی ترکیب‌ها</button>{rows.length > 0 && !matrixCurrent && <p role="status" className="text-xs text-amber-800">ویژگی‌ها تغییر کرده‌اند؛ ترکیب‌ها را به‌روز کنید. قیمت و موجودی ترکیب‌های مشترک حفظ می‌شود.</p>}</div>
 {rows.length > 0 && <><div className="flex flex-wrap gap-2 items-center text-xs"><input aria-label="قیمت گروهی" disabled={!rows.some(row => row.active && canEditSKUValue(row.id, 'supplier_base_cost'))} inputMode="numeric" value={bulkCost} onChange={e => setBulkCost(e.target.value)} placeholder="قیمت برای همهٔ فعال‌ها" className="min-w-0 flex-1 border rounded-lg p-2"/><input aria-label="موجودی گروهی" disabled={!rows.some(row => row.active && canEditSKUValue(row.id, 'stock'))} inputMode="numeric" value={bulkStock} onChange={e => setBulkStock(e.target.value)} placeholder="موجودی برای همهٔ فعال‌ها" className="min-w-0 flex-1 border rounded-lg p-2"/><button type="button" onClick={() => onRows(rows.map(v => v.active ? { ...v, ...(bulkCost.trim() && canEditSKUValue(v.id, 'supplier_base_cost') ? { cost: bulkCost.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : {}), ...(bulkStock.trim() && canEditSKUValue(v.id, 'stock') ? { stock: bulkStock.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : {}) } : v))} className="border rounded-lg p-2">اعمال گروهی</button></div>
 <div className="space-y-2">{rows.slice(page * 20, page * 20 + 20).map(row => <article key={row.id} data-sku-id={row.id} className={`rounded-xl border p-3 space-y-2 ${row.active ? 'bg-white' : 'bg-slate-100 opacity-70'}`}><div className="flex flex-wrap justify-between gap-2 text-xs"><strong>{row.name}</strong><label className="flex gap-2"><input type="checkbox" disabled={!skuStructureEditable} checked={row.active} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, active: e.target.checked } : v))}/>این ترکیب موجود است</label></div>{canEditSKUSelection && axes.length > 0 && <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">{axes.map(axis => <label key={axis.id} className="text-xs">{axis.name}<select aria-label={'انتخاب ' + axis.name + ' برای ' + row.name} disabled={!canEditSKUSelection(row.id, axis.id)} value={row.selection[axis.id] ?? ''} onChange={e => onRows(rows.map(item => { if (item.id !== row.id)
                return item; const selection = { ...item.selection, [axis.id]: e.target.value }; const name = axes.map(attribute => `${attribute.name}: ${attribute.values.find(value => value.id === selection[attribute.id])?.label ?? 'انتخاب نشده'}`).join(' / '); return { ...item, selection, name }; }))} className="w-full rounded-lg border p-2 mt-1"><option value="">انتخاب مقدار</option>{axis.values.map(value => <option key={value.id} value={value.id}>{value.label}</option>)}</select></label>)}</div>}{row.active ? <div className="grid grid-cols-2 gap-2"><label className="text-xs">قیمت عمده (تومان)<input aria-label={'قیمت ' + row.name} disabled={!canEditSKUValue(row.id, 'supplier_base_cost')} inputMode="numeric" value={row.cost} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, cost: e.target.value.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : v))} className="w-full rounded-lg border p-2 mt-1"/></label><label className="text-xs">موجودی این ترکیب<input aria-label={'موجودی ' + row.name} disabled={!canEditSKUValue(row.id, 'stock')} inputMode="numeric" value={row.stock} onChange={e => onRows(rows.map(v => v.id === row.id ? { ...v, stock: e.target.value.replace(/[۰-۹]/g, c => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(c))) } : v))} className="w-full rounded-lg border p-2 mt-1"/></label></div> : <p className="text-xs">ترکیب ناموجود از فروش کنار گذاشته می‌شود؛ با فعال‌کردن دوباره، اطلاعات واردشده برمی‌گردد.</p>}</article>)}</div>
 <nav aria-label="صفحه‌بندی ترکیب‌ها" className="flex justify-between items-center text-xs"><button type="button" disabled={page === 0} onClick={() => setPage(p => p - 1)} className="border rounded-lg p-2 disabled:opacity-40">قبلی</button><span>صفحهٔ {page + 1} از {pages} — {rows.filter(r => r.active).length} ترکیب فعال</span><button type="button" disabled={page + 1 >= pages} onClick={() => setPage(p => p + 1)} className="border rounded-lg p-2 disabled:opacity-40">بعدی</button></nav></>}
 </>}
 </section>;
}
