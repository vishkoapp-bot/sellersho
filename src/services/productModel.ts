import type { ProductSchema } from '../types/productSchema';
import { MAX_PRODUCT_COMBINATIONS, MAX_MATRIX_ROWS } from '../types/productSchema';
export interface DraftProductSKU {
    id: string;
    selection: Record<string, string>;
    name: string;
    cost: string;
    stock: string;
    active: boolean;
    /** P1(b): false = this combination is never created/sent (draft-only
        concept, distinct from active=false = a created row that is not
        sellable). Default true — the old all-combinations behaviour. */
    selected: boolean;
    /** B2: supplier-picked product-level default variant (exactly one
        per product). serializeProductDraft resolves the effective
        default (explicit pick among selected+active rows, else the
        first selected active row) and emits it as `is_default`. */
    isDefault: boolean;
    /** P0: variant hero image URL (uploaded via POST /upload/product-image); '' = inherit the product image. */
    imageUrl: string;
    /** P0: variant gallery URLs (max 3, hero excluded). */
    images: string[];
    /** P0: variant weight in grams; '' = inherit the product weight. */
    weightGrams: string;
    /** P0: variant dims in cm; all three '' = inherit product dims, otherwise all three required. */
    lengthCm: string;
    widthCm: string;
    heightCm: string;
    /** P1(c): sale price (Toman, final customer price); '' = no sale. */
    salePrice: string;
    /** P1(c): sale window, datetime-local strings; '' = open-ended. */
    saleFrom: string;
    saleTo: string;
    /** P2(b): shipping/tax class; '' = inherit (stored only, never displayed on the PDP). */
    shippingClass: string;
    taxClass: string;
}
export const selectionKey = (selection: Record<string, string>) => JSON.stringify(Object.entries(selection).sort(([a], [b]) => a.localeCompare(b)));
const newID = () => crypto.randomUUID();
// Backward compatibility: old draft rows (tests, stored drafts) lack the
// new fields — every reader normalises through these defaults.
const emptyVariantDetails = () => ({ isDefault: false, imageUrl: '', images: [] as string[], weightGrams: '', lengthCm: '', widthCm: '', heightCm: '', salePrice: '', saleFrom: '', saleTo: '', shippingClass: '', taxClass: '' });
const normalizeRow = (v: DraftProductSKU): DraftProductSKU => ({ ...emptyVariantDetails(), ...v, selected: v.selected ?? true, isDefault: v.isDefault ?? false, images: v.images ?? [] });
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
    // P1(b): the 500 cap moved to SELECTED rows (serializeProductDraft);
    // the matrix itself may materialise up to MAX_MATRIX_ROWS rows.
    if (axes.reduce((count, a) => count * a.values.length, 1) > MAX_MATRIX_ROWS)
        throw Error(`حداکثر ${MAX_MATRIX_ROWS} ترکیب قابل نمایش است. تعداد مقدارها را کاهش دهید.`);
    let combinations: Record<string, string>[] = [{}];
    for (const axis of axes)
        combinations = combinations.flatMap(row => axis.values.map(value => ({ ...row, [axis.id]: value.id })));
    const old = new Map(previous.map(v => [selectionKey(v.selection), normalizeRow(v)]));
    return combinations.map(selection => { const existing = old.get(selectionKey(selection)); const name = axes.length ? axes.map(a => `${a.name}: ${a.values.find(v => v.id === selection[a.id])!.label}`).join(' / ') : 'مدل اصلی'; return { id: existing?.id ?? idFactory(), selection, name, cost: existing?.cost ?? '', stock: existing?.stock ?? '', active: existing?.active ?? true, selected: existing?.selected ?? true, isDefault: existing?.isDefault ?? false, imageUrl: existing?.imageUrl ?? '', images: existing?.images ?? [], weightGrams: existing?.weightGrams ?? '', lengthCm: existing?.lengthCm ?? '', widthCm: existing?.widthCm ?? '', heightCm: existing?.heightCm ?? '', salePrice: existing?.salePrice ?? '', saleFrom: existing?.saleFrom ?? '', saleTo: existing?.saleTo ?? '', shippingClass: existing?.shippingClass ?? '', taxClass: existing?.taxClass ?? '' }; });
}
export function serializeProductDraft(schema: ProductSchema, rows: DraftProductSKU[]) {
    validateProductSchema(schema);
    const normRows = rows.map(normalizeRow);
    const expected = buildProductMatrix(schema, normRows);
    // P1(b): subset at creation — only SELECTED rows are submitted.
    // Unselected rows are never serialised (not even as is_active=false).
    const selected = normRows.filter(v => v.selected);
    if (selected.length === 0)
        throw Error('حداقل یک ترکیب را انتخاب کنید.');
    if (selected.length > MAX_PRODUCT_COMBINATIONS)
        throw Error(`حداکثر ${MAX_PRODUCT_COMBINATIONS} ترکیب قابل ثبت است. تعداد ترکیب‌های انتخاب‌شده را کاهش دهید.`);
    if (new Set(selected.map(v => selectionKey(v.selection))).size !== selected.length)
        throw Error('ترکیب تکراری انتخاب شده است.');
    if (selected.some(v => !expected.some(e => selectionKey(e.selection) === selectionKey(v.selection))))
        throw Error('پس از تغییر ویژگی‌ها، ترکیب‌ها را به‌روز کنید.');
    const active = selected.filter(v => v.active);
    if (!active.length)
        throw Error('حداقل یک ترکیب فعال لازم است.');
    const posInt = (raw: string): number | undefined => { const n = Number(raw); return raw.trim() !== '' && Number.isSafeInteger(n) ? n : undefined; };
    for (const row of active) {
        if (!row.cost.trim() || !row.stock.trim() || !Number.isSafeInteger(Number(row.cost)) || Number(row.cost) <= 0 || Number(row.cost) > 1e12 || !Number.isSafeInteger(Number(row.stock)) || Number(row.stock) < 0 || Number(row.stock) > 1e9)
            throw Error(`قیمت و موجودی «${row.name}» را درست وارد کنید.`);
        // P0 weight: optional (inherit); when set, integer 1..100,000 g.
        if (row.weightGrams.trim() !== '') {
            const w = posInt(row.weightGrams);
            if (w === undefined || w < 1 || w > 100000)
                throw Error(`وزن «${row.name}» باید عدد صحیح بین ۱ تا ۱۰۰٬۰۰۰ گرم باشد.`);
        }
        // P0 dims: all three together or none; each integer 1..300 cm.
        const dims = [row.lengthCm, row.widthCm, row.heightCm];
        const filledDims = dims.filter(d => d.trim() !== '').length;
        if (filledDims > 0 && filledDims < 3)
            throw Error(`ابعاد «${row.name}» را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).`);
        if (filledDims === 3) {
            for (const d of dims) {
                const n = posInt(d);
                if (n === undefined || n < 1 || n > 300)
                    throw Error(`ابعاد «${row.name}» را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).`);
            }
        }
        // P1(c) sale: price integer > 0; window must be well-formed and
        // end after start. Whether the sale is below the regular price
        // is enforced server-side (the 3-tier price is not computable
        // in the propose form); the server error surfaces as-is.
        if (row.salePrice.trim() !== '') {
            const s = posInt(row.salePrice);
            if (s === undefined || s <= 0)
                throw Error(`قیمت حراج «${row.name}» را درست وارد کنید.`);
            const from = row.saleFrom.trim() ? new Date(row.saleFrom).getTime() : NaN;
            const to = row.saleTo.trim() ? new Date(row.saleTo).getTime() : NaN;
            if (row.saleFrom.trim() && !Number.isFinite(from))
                throw Error(`تاریخ شروع حراج «${row.name}» معتبر نیست.`);
            if (row.saleTo.trim() && !Number.isFinite(to))
                throw Error(`تاریخ پایان حراج «${row.name}» معتبر نیست.`);
            if (Number.isFinite(from) && Number.isFinite(to) && !(from < to))
                throw Error(`تاریخ پایان حراج «${row.name}» باید بعد از تاریخ شروع باشد.`);
        }
        // P2(b) classes: stored only, no content validation client-side.
        if (row.shippingClass.length > 50 || row.taxClass.length > 50)
            throw Error(`کلاس حمل‌ونقل یا مالیاتی «${row.name}» بیش از حد طولانی است.`);
    }
    const toISO = (raw: string): string | null => raw.trim() === '' ? null : new Date(raw).toISOString();
    // B2: effective product-level default — the supplier's explicit pick
    // when it is among the selected+active rows, otherwise the first
    // selected active row (matches the backend publication fallback:
    // first selected active variant). Exactly one variant is emitted
    // with is_default=true; a simple product's single row is its default.
    const defaultRow = active.find(v => v.isDefault) ?? active[0];
    return { schemaVersion: 2, productType: schema.type, productSchema: schema, supplierBaseCost: Math.min(...active.map(v => Number(v.cost))), stock: active.reduce((sum, v) => sum + Number(v.stock), 0), variants: selected.map(v => ({ id: v.id, selection: v.selection, variant_name: v.name, attributes: Object.fromEntries(schema.attributes.filter(a => a.role === 'axis').map(a => [a.name, a.values.find(o => o.id === v.selection[a.id])!.label])), supplier_base_cost: v.active ? Number(v.cost) : 0, stock: v.active ? Number(v.stock) : 0, is_active: v.active,
        // F1: the backend validator (product_model_v2.go validateVariantPhysical)
        // and publication (product_publication_v2.go insertProductV2VariantsTx)
        // read ONLY v["images"] — a single array, hero first. The old
        // image_url/image_urls keys were never read and variant images
        // were silently dropped at publication.
        // B2: is_default per variant (snake_case, like the other keys).
        // ASSUMPTION (unconfirmed — backend worker implements it in
        // parallel): the backend honours v["is_default"] at publication
        // with at most one default per product.
        is_default: v.id === defaultRow.id,
        images: v.imageUrl.trim() ? [v.imageUrl.trim(), ...v.images.filter(u => u.trim() !== '')] : v.images.filter(u => u.trim() !== ''),
        weight_grams: v.weightGrams.trim() === '' ? null : Number(v.weightGrams),
        length_mm: v.lengthCm.trim() === '' ? null : Number(v.lengthCm) * 10,
        width_mm: v.widthCm.trim() === '' ? null : Number(v.widthCm) * 10,
        height_mm: v.heightCm.trim() === '' ? null : Number(v.heightCm) * 10,
        sale_price: v.salePrice.trim() === '' ? null : Number(v.salePrice),
        sale_from: toISO(v.saleFrom),
        sale_to: toISO(v.saleTo),
        shipping_class: v.shippingClass.trim() || null,
        tax_class: v.taxClass.trim() || null })) };
}
