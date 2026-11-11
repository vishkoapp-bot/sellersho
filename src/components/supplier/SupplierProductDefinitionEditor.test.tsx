import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { SupplierProductDefinitionEditor } from './SupplierProductDefinitionEditor';
import type { ProductSchema } from '../../types/productSchema';

it('keeps requested definitions visible and disables approved values in structural correction', () => {
  const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'requested:size', name: 'سایز سفارشی', role: 'axis', source: 'requested', values: [{ id: 'requested:s', label: 'کوچک سفارشی' }] }] };
  const html = renderToStaticMarkup(<SupplierProductDefinitionEditor categorySlug="clothes" productType="variable" schema={schema}
    rows={[{ id: 'old', name: 'کوچک', selection: { 'requested:size': 'requested:s' }, cost: '1000', stock: '5', active: true }]}
    onSchema={() => {}} onRows={() => {}} brand="" noBrand={false} onBrand={() => {}} onNoBrand={() => {}}
    hideBrand schemaEditable={false} skuStructureEditable={false} canEditSKUValue={(_id, leaf) => leaf === 'stock'} />);
  assert.match(html, /کوچک سفارشی/);
  assert.match(html, /<fieldset disabled=""[^>]*data-attribute-id="requested:size"/);
  assert.match(html, /aria-label="قیمت کوچک" disabled=""/);
  assert.doesNotMatch(html, /aria-label="موجودی کوچک" disabled=""/);
  assert.doesNotMatch(html, /aria-label="برند محصول"/);
  assert.match(html, /disabled=""[^>]*>ساخت \/ به‌روزرسانی ترکیب‌ها/);
});
