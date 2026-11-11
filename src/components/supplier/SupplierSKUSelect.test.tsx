import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { SupplierSKUSelect } from './SupplierSKUSelect';
import { SupplierOrderLines } from './SupplierOrderItemIdentity';
import type { SupplierProductDto } from '../../services/supplierApi';

it('requires an exact SKU and disables return choices with no owned eligible stock', () => {
  const product = { id: 'p', schemaVersion: 2 } as SupplierProductDto;
  const html = renderToStaticMarkup(<SupplierSKUSelect product={product} rows={[
    { id: 'a', variant_name: 'آبی / بزرگ', stock: 20, spnc: 'SPNC-A' },
    { id: 'b', variant_name: 'قرمز / کوچک', stock: 20, spnc: 'SPNC-B' },
  ]} selectedId="" onSelect={() => {}} label="تنوع برگشتی" available={id => id === 'a' ? 2 : 0} />);
  assert.match(html, /required=""/);
  assert.match(html, /آبی \/ بزرگ · SPNC-A/);
  assert.match(html, /<option value="b" disabled=""/);
  assert.doesNotMatch(html, /<option value="a" disabled=""/);
});
it('renders purchased SKU snapshots separately for identical products', () => {
  const html = renderToStaticMarkup(<SupplierOrderLines items={[
    { productId: 'p', productTitle: 'پیراهن', variantId: 'a', variantName: 'آبی / بزرگ', variantSpnc: 'SPNC-A', quantity: 2 },
    { productId: 'p', productTitle: 'پیراهن', variantId: 'b', variantName: 'قرمز / کوچک', variantSpnc: 'SPNC-B', quantity: 1 },
  ]} />);
  assert.equal((html.match(/<li /g) ?? []).length, 2);
  assert.match(html, /آبی \/ بزرگ/);
  assert.match(html, /قرمز \/ کوچک/);
  assert.match(html, /SPNC-A/);
  assert.match(html, /SPNC-B/);
});
