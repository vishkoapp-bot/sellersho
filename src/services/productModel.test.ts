import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildProductMatrix, selectionKey, serializeProductDraft } from './productModel';
import type { ProductSchema } from '../types/productSchema';
const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'color', name: 'Color', source: 'color', role: 'axis', values: [{ id: 'r', label: 'Red' }, { id: 'b', label: 'Blue' }] }, { id: 'size', name: 'Size', source: 'local', role: 'axis', values: [{ id: 's', label: 'S' }, { id: 'm', label: 'M' }, { id: 'l', label: 'L' }] }] };
test('two axes form six independent combinations and preserve UUID/prices/stock/exclusions', () => { let i = 0; const rows = buildProductMatrix(schema, [], () => `id-${++i}`); assert.equal(rows.length, 6); assert.equal(new Set(rows.map(r => selectionKey(r.selection))).size, 6); rows[0] = { ...rows[0], cost: '100', stock: '7', active: false }; const reordered = { ...schema, attributes: [...schema.attributes].reverse() }; const next = buildProductMatrix(reordered, rows, () => `id-${++i}`); const kept = next.find(r => r.id === rows[0].id)!; assert.equal(kept.cost, '100'); assert.equal(kept.stock, '7'); assert.equal(kept.active, false); });
test('5x5 supported; too large bounded before allocation', () => { const attrs = schema.attributes.map(a => ({ ...a, values: Array.from({ length: 5 }, (_, i) => ({ id: String(i), label: String(i) })) })); assert.equal(buildProductMatrix({ ...schema, attributes: attrs }, []).length, 25); assert.equal(buildProductMatrix({ ...schema, attributes: attrs.map(a => ({ ...a, values: Array.from({ length: 30 }, (_, i) => ({ id: String(i), label: String(i) })) })) }, []).length, 900); assert.throws(() => buildProductMatrix({ ...schema, attributes: attrs.map(a => ({ ...a, values: Array.from({ length: 100 }, (_, i) => ({ id: String(i), label: String(i) })) })) }, [])); });
test('manual subset: unselected rows are never serialised; all unselected is an error', () => { let i = 0; const rows = buildProductMatrix(schema, [], () => `id-${++i}`); rows.forEach(r => { r.cost = '100'; r.stock = '2'; }); rows[0].selected = false; rows[1].selected = false; const payload = serializeProductDraft(schema, rows); assert.equal(payload.variants.length, 4); assert.equal(payload.stock, 8); assert.equal(payload.supplierBaseCost, 100); rows.forEach(r => { r.selected = false; }); assert.throws(() => serializeProductDraft(schema, rows), /حداقل یک ترکیب را انتخاب کنید/); });
test('subset validation: weight, partial dims and sale window errors carry the row name', () => { let i = 0; const rows = buildProductMatrix(schema, [], () => `id-${++i}`); rows.forEach(r => { r.cost = '100'; r.stock = '2'; }); rows[0].weightGrams = '0'; assert.throws(() => serializeProductDraft(schema, rows), /وزن/); rows[0].weightGrams = '1200'; rows[0].lengthCm = '40'; assert.throws(() => serializeProductDraft(schema, rows), /ابعاد/); rows[0].lengthCm = '40'; rows[0].widthCm = '30'; rows[0].heightCm = '20'; rows[0].salePrice = '50000'; rows[0].saleFrom = '2026-12-02T10:00'; rows[0].saleTo = '2026-12-01T10:00'; assert.throws(() => serializeProductDraft(schema, rows), /تاریخ پایان حراج/); rows[0].saleTo = '2026-12-03T10:00'; const payload = serializeProductDraft(schema, rows); const first = payload.variants.find(v => v.id === rows[0].id)!; assert.equal(first.weight_grams, 1200); assert.equal(first.length_mm, 400); assert.equal(first.width_mm, 300); assert.equal(first.height_mm, 200); assert.equal(first.sale_price, 50000); assert.equal(first.sale_from, new Date('2026-12-02T10:00').toISOString()); assert.equal(first.shipping_class, null); });
test('old rows without the new fields still serialise (backward compatibility)', () => { let i = 0; const rows = buildProductMatrix(schema, [], () => `id-${++i}`); const legacy = rows.map(({ selected, imageUrl, images, weightGrams, lengthCm, widthCm, heightCm, salePrice, saleFrom, saleTo, shippingClass, taxClass, ...rest }) => ({ ...rest, cost: '100', stock: '1' })); const payload = serializeProductDraft(schema, legacy as any); assert.equal(payload.variants.length, 6); assert.equal(payload.variants[0].weight_grams, null); });
test('fixed attributes never become SKU axes and invalid or stale matrix cannot submit', () => { const fixed = { ...schema, attributes: [{ ...schema.attributes[0], role: 'fixed' as const, values: [schema.attributes[0].values[0]] }, schema.attributes[1]] }; const rows = buildProductMatrix(fixed, []); assert.equal(rows.length, 3); assert(!('color' in rows[0].selection)); assert.throws(() => serializeProductDraft(fixed, rows)); rows.forEach(r => { r.cost = '100'; r.stock = '0'; }); const payload = serializeProductDraft(fixed, rows); assert.equal(payload.variants.length, 3); assert.equal(payload.stock, 0); assert.equal(payload.schemaVersion, 2); assert.throws(() => serializeProductDraft({ ...fixed, attributes: [...fixed.attributes, { ...schema.attributes[0], name: 'Color duplicate' }] }, rows)); });
test('non-secure context (live plain HTTP): default id factory falls back instead of throwing', () => {
  // Regression: the propose form calls buildProductMatrix with the default
  // factory in a useState initializer; on plain HTTP crypto.randomUUID is
  // undefined, the throw whitescreened the whole app (no error boundary).
  const c = globalThis.crypto as unknown as { randomUUID?: unknown };
  const orig = c.randomUUID;
  (c as { randomUUID?: unknown }).randomUUID = undefined;
  try {
    const simple: ProductSchema = { version: 2, type: 'simple', attributes: [] };
    const rows = buildProductMatrix(simple, []);
    assert.equal(rows.length, 1);
    assert.equal(typeof rows[0].id, 'string');
    assert.ok(rows[0].id.length > 0);
    const multi = buildProductMatrix(schema, []);
    assert.equal(multi.length, 6);
    assert.equal(new Set(multi.map((r) => r.id)).size, 6);
  } finally {
    (c as { randomUUID?: unknown }).randomUUID = orig;
  }
});
