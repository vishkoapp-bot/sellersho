import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { SupplierProductDefinitionEditor, toggleSchemaAttribute, formatSep, parseSep } from './SupplierProductDefinitionEditor';
import { buildProductMatrix, serializeProductDraft } from '../../services/productModel';
import type { CatalogProductAttribute, ProductSchema } from '../../types/productSchema';

const color: CatalogProductAttribute = { id: 'color', name: 'رنگ', source: 'color', values: [{ id: 'r', label: 'قرمز' }, { id: 'b', label: 'آبی' }] };
const size: CatalogProductAttribute = { id: 'size', name: 'سایز', source: 'local', values: [{ id: 's', label: 'کوچک' }, { id: 'm', label: 'متوسط' }] };
const emptyVariable: ProductSchema = { version: 2, type: 'variable', attributes: [] };

it('keeps requested definitions visible and disables approved values in structural correction', () => {
  const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'requested:size', name: 'سایز سفارشی', role: 'axis', source: 'requested', values: [{ id: 'requested:s', label: 'کوچک سفارشی' }] }] };
  const html = renderToStaticMarkup(<SupplierProductDefinitionEditor categorySlug="clothes" productType="variable" schema={schema}
    rows={[{ id: 'old', name: 'کوچک', selection: { 'requested:size': 'requested:s' }, cost: '1000', stock: '5', active: true, selected: true, isDefault: false, imageUrl: '', images: [], weightGrams: '', lengthCm: '', widthCm: '', heightCm: '', salePrice: '', saleFrom: '', saleTo: '', shippingClass: '', taxClass: ''}]}
    onSchema={() => {}} onRows={() => {}} brand="" noBrand={false} onBrand={() => {}} onNoBrand={() => {}}
    hideBrand schemaEditable={false} skuStructureEditable={false} canEditSKUValue={(_id, leaf) => leaf === 'stock'} />);
  assert.match(html, /کوچک سفارشی/);
  assert.match(html, /<fieldset disabled=""[^>]*data-attribute-id="requested:size"/);
  assert.match(html, /aria-label="قیمت کوچک" disabled=""/);
  assert.doesNotMatch(html, /aria-label="موجودی کوچک" disabled=""/);
  assert.doesNotMatch(html, /aria-label="برند محصول"/);
  assert.match(html, /disabled=""[^>]*>ساخت \/ به‌روزرسانی ترکیب‌ها/);
});

it('ticking two attributes at once adds both as variant axes (variable product)', () => {
  // The checkbox list calls the toggle once per tick, before any
  // generate: both land in the schema together, as axes.
  const afterBoth = toggleSchemaAttribute(toggleSchemaAttribute(emptyVariable, color, 'variable', true), size, 'variable', true);
  assert.equal(afterBoth.attributes.length, 2);
  assert.deepEqual(afterBoth.attributes.map(a => [a.id, a.role]), [['color', 'axis'], ['size', 'axis']]);
  assert.deepEqual(afterBoth.attributes.map(a => a.values), [[], []]);
  // Ticking an already-chosen attribute does not duplicate it.
  assert.equal(toggleSchemaAttribute(afterBoth, color, 'variable', true).attributes.length, 2);
});

it('ticking an attribute on a simple product adds it as fixed, never as an axis', () => {
  const simple: ProductSchema = { version: 2, type: 'simple', attributes: [] };
  const after = toggleSchemaAttribute(simple, color, 'simple', true);
  assert.equal(after.attributes[0].role, 'fixed');
});

it('unticking removes only that attribute, like the delete button', () => {
  const afterBoth = toggleSchemaAttribute(toggleSchemaAttribute(emptyVariable, color, 'variable', true), size, 'variable', true);
  const afterUntick = toggleSchemaAttribute(afterBoth, color, 'variable', false);
  assert.deepEqual(afterUntick.attributes.map(a => a.id), ['size']);
  // Unticking an attribute that was never added is a no-op.
  assert.deepEqual(toggleSchemaAttribute(afterUntick, color, 'variable', false).attributes.map(a => a.id), ['size']);
});

it('picking values for both ticked attributes then one generate builds the combined matrix', () => {
  const schema: ProductSchema = {
    version: 2, type: 'variable',
    attributes: [
      { ...color, role: 'axis', values: color.values },
      { ...size, role: 'axis', values: size.values },
    ],
  };
  // One generate() call = one buildProductMatrix call on this schema.
  const rows = buildProductMatrix(schema, []);
  assert.equal(rows.length, 4);
  assert.equal(new Set(rows.map(r => JSON.stringify(r.selection))).size, 4);
  assert.ok(rows.some(r => r.name === 'رنگ: قرمز / سایز: کوچک'));
  assert.ok(rows.some(r => r.name === 'رنگ: آبی / سایز: متوسط'));
});

it('renders the checkbox list as the primary picker: ticked attributes checked, with value counts', () => {
  const schema: ProductSchema = {
    version: 2, type: 'variable',
    attributes: [
      { ...color, role: 'axis', values: color.values },
      { ...size, role: 'axis', values: size.values },
    ],
  };
  const html = renderToStaticMarkup(<SupplierProductDefinitionEditor categorySlug="clothes" productType="variable" schema={schema}
    rows={[]} onSchema={() => {}} onRows={() => {}} brand="" noBrand={false} onBrand={() => {}} onNoBrand={() => {}} hideBrand />);
  assert.match(html, /ویژگی‌های متغیر را یکجا تیک بزنید/);
  assert.match(html, /aria-label="انتخاب ویژگی رنگ" checked=""/);
  assert.match(html, /aria-label="انتخاب ویژگی سایز" checked=""/);
  assert.match(html, /2 مقدار/);
  assert.match(html, /محور تنوع/);
  // The one-by-one dropdown is gone as the primary path.
  assert.doesNotMatch(html, /افزودن ویژگی محصول/);
  assert.doesNotMatch(html, /انتخاب ویژگی مثل رنگ/);
});

it('in the read-only correction view the ticked attributes still show checked but disabled', () => {
  const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'requested:size', name: 'سایز سفارشی', role: 'axis', source: 'requested', values: [{ id: 'requested:s', label: 'کوچک سفارشی' }] }] };
  const html = renderToStaticMarkup(<SupplierProductDefinitionEditor categorySlug="clothes" productType="variable" schema={schema}
    rows={[]} onSchema={() => {}} onRows={() => {}} brand="" noBrand={false} onBrand={() => {}} onNoBrand={() => {}}
    hideBrand schemaEditable={false} skuStructureEditable={false} canEditSKUValue={() => true} />);
  assert.match(html, /aria-label="انتخاب ویژگی سایز سفارشی" disabled="" checked=""/);
});

// Thousands separators: the displayed value is grouped with ٬ and
// Persian digits, while the stored value stays raw Latin digits.
it('formatSep groups with ٬ and Persian digits; parseSep strips a pasted grouped value back to raw digits', () => {
  assert.equal(formatSep('100000'), '۱۰۰٬۰۰۰');
  assert.equal(formatSep('10000'), '۱۰٬۰۰۰');
  assert.equal(formatSep('90000'), '۹۰٬۰۰۰');
  assert.equal(formatSep(''), '');
  assert.equal(formatSep('999'), '۹۹۹');
  // Typing/pasting path shared by all five inputs (per-variant cost and
  // stock, bulk cost and stock, sale price): Persian grouped, Latin
  // grouped, and plain digits all land as raw digits in state.
  assert.equal(parseSep('۱۰۰٬۰۰۰'), '100000');
  assert.equal(parseSep('100,000'), '100000');
  assert.equal(parseSep('100000'), '100000');
  assert.equal(parseSep(''), '');
});

it('price and stock inputs display grouped Persian digits while the stored/submitted values stay raw digits', () => {
  const schema: ProductSchema = { version: 2, type: 'simple', attributes: [] };
  const row = { id: 'main', name: 'مدل اصلی', selection: {}, cost: '100000', stock: '10000', active: true, selected: true, isDefault: false, imageUrl: '', images: [], weightGrams: '', lengthCm: '', widthCm: '', heightCm: '', salePrice: '90000', saleFrom: '', saleTo: '', shippingClass: '', taxClass: '' };
  const html = renderToStaticMarkup(<SupplierProductDefinitionEditor categorySlug="clothes" productType="simple" schema={schema}
    rows={[row]} onSchema={() => {}} onRows={() => {}} brand="" noBrand={false} onBrand={() => {}} onNoBrand={() => {}}
    hideBrand />);
  // Per-variant cost, per-variant stock, and per-variant sale price.
  assert.match(html, /aria-label="قیمت مدل اصلی"[^>]*value="۱۰۰٬۰۰۰"/);
  assert.match(html, /aria-label="موجودی مدل اصلی"[^>]*value="۱۰٬۰۰۰"/);
  assert.match(html, /aria-label="قیمت حراج تنوع مدل اصلی"[^>]*value="۹۰٬۰۰۰"/);
  // No input carries the raw digits as its displayed value.
  assert.doesNotMatch(html, /value="100000"/);
  assert.doesNotMatch(html, /value="10000"/);
  assert.doesNotMatch(html, /value="90000"/);
  // The same raw-digit row serialises to raw numbers — the grouping
  // never leaks into the submit payload.
  const drafted = serializeProductDraft(schema, [row]);
  assert.equal(drafted.supplierBaseCost, 100000);
  assert.equal(drafted.stock, 10000);
  assert.equal(drafted.variants[0].supplier_base_cost, 100000);
  assert.equal(drafted.variants[0].stock, 10000);
  assert.equal(drafted.variants[0].sale_price, 90000);
});
