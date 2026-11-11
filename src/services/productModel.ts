import type { ProductSchema } from '../types/productSchema';
import { MAX_PRODUCT_COMBINATIONS } from '../types/productSchema';
export interface DraftProductSKU {
    id: string;
    selection: Record<string, string>;
    name: string;
    cost: string;
    stock: string;
    active: boolean;
}
export const selectionKey = (selection: Record<string, string>) => JSON.stringify(Object.entries(selection).sort(([a], [b]) => a.localeCompare(b)));
const newID = () => crypto.randomUUID();
export function validateProductSchema(schema: ProductSchema) {
    if (schema.version !== 2 || !['simple', 'variable'].includes(schema.type) || schema.attributes.length > 100)
        throw Error('ساختار محصول معتبر نیست.');
    const ids = new Set<string>(), names = new Set<string>();
    for (const a of schema.attributes) {
        const normalized = a.name.trim().replace(/\s+/g, ' ');
        if (!a.id || !normalized || ids.has(a.id) || names.has(normalized))
            throw Error('هر ویژگی فقط یک‌بار قابل تعریف است.');
        ids.add(a.id);
        names.add(normalized);
        if (!a.values.length || a.values.length > 500 || new Set(a.values.map(v => v.id)).size !== a.values.length || a.values.some(v => !v.id || !v.label.trim()))
            throw Error('مقدارهای ویژگی را انتخاب کنید.');
        if (a.role === 'fixed' && a.values.length !== 1)
            throw Error('ویژگی ثابت فقط یک مقدار دارد.');
        if (schema.type === 'simple' && a.role === 'axis')
            throw Error('محصول تکی محور تنوع ندارد.');
    }
    if (schema.type === 'variable' && !schema.attributes.some(a => a.role === 'axis'))
        throw Error('حداقل یک محور تنوع انتخاب کنید.');
}
export function buildProductMatrix(schema: ProductSchema, previous: DraftProductSKU[], idFactory: () => string = newID): DraftProductSKU[] {
    validateProductSchema(schema);
    const axes = schema.attributes.filter(a => a.role === 'axis').sort((a, b) => a.id.localeCompare(b.id));
    if (axes.reduce((count, a) => count * a.values.length, 1) > MAX_PRODUCT_COMBINATIONS)
        throw Error(`حداکثر ${MAX_PRODUCT_COMBINATIONS} ترکیب قابل ثبت است. تعداد مقدارها را کاهش دهید.`);
    let combinations: Record<string, string>[] = [{}];
    for (const axis of axes)
        combinations = combinations.flatMap(row => axis.values.map(value => ({ ...row, [axis.id]: value.id })));
    const old = new Map(previous.map(v => [selectionKey(v.selection), v]));
    return combinations.map(selection => { const existing = old.get(selectionKey(selection)); const name = axes.length ? axes.map(a => `${a.name}: ${a.values.find(v => v.id === selection[a.id])!.label}`).join(' / ') : 'مدل اصلی'; return { id: existing?.id ?? idFactory(), selection, name, cost: existing?.cost ?? '', stock: existing?.stock ?? '', active: existing?.active ?? true }; });
}
export function serializeProductDraft(schema: ProductSchema, rows: DraftProductSKU[]) {
    validateProductSchema(schema);
    const expected = buildProductMatrix(schema, rows);
    if (expected.length !== rows.length || new Set(rows.map(v => selectionKey(v.selection))).size !== rows.length || rows.some(v => !expected.some(e => selectionKey(e.selection) === selectionKey(v.selection))))
        throw Error('پس از تغییر ویژگی‌ها، ترکیب‌ها را به‌روز کنید.');
    const active = rows.filter(v => v.active);
    if (!active.length)
        throw Error('حداقل یک ترکیب فعال لازم است.');
    for (const row of active)
        if (!row.cost.trim() || !row.stock.trim() || !Number.isSafeInteger(Number(row.cost)) || Number(row.cost) <= 0 || Number(row.cost) > 1e12 || !Number.isSafeInteger(Number(row.stock)) || Number(row.stock) < 0 || Number(row.stock) > 1e9)
            throw Error(`قیمت و موجودی «${row.name}» را درست وارد کنید.`);
    return { schemaVersion: 2, productType: schema.type, productSchema: schema, supplierBaseCost: Math.min(...active.map(v => Number(v.cost))), stock: active.reduce((sum, v) => sum + Number(v.stock), 0), variants: rows.map(v => ({ id: v.id, selection: v.selection, variant_name: v.name, attributes: Object.fromEntries(schema.attributes.filter(a => a.role === 'axis').map(a => [a.name, a.values.find(o => o.id === v.selection[a.id])!.label])), supplier_base_cost: v.active ? Number(v.cost) : 0, stock: v.active ? Number(v.stock) : 0, is_active: v.active })) };
}
