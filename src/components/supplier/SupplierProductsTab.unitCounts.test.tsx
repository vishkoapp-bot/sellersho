import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { ProductUnitChip, sumProductUnitCount } from './SupplierProductsTab';

test('only loaded nonempty variant counts are summed, including zeroes', () => {
  assert.equal(sumProductUnitCount(undefined), null);
  assert.equal(sumProductUnitCount({}), null);
  assert.equal(sumProductUnitCount({ a: 0 }), 0);
  assert.equal(sumProductUnitCount({ a: 4, b: 3 }), 7);
});

test('an unloaded chip displays a dash', () => {
  const html = renderToStaticMarkup(<ProductUnitChip unitCount={null} stock={8} />);
  assert.match(html, />—</);
  assert.doesNotMatch(html, /۸/);
});

test('equal counts display the stock and serial count without a warning', () => {
  const html = renderToStaticMarkup(<ProductUnitChip unitCount={8} stock={8} />);
  assert.match(html, /۸ واحد سریال‌دار از ۸ عدد موجودی/);
  assert.doesNotMatch(html, /amber|مغایرت/);
});

test('different counts display an amber discrepancy', () => {
  const html = renderToStaticMarkup(<ProductUnitChip unitCount={2} stock={8} />);
  assert.match(html, /amber/);
  assert.match(html, /مغایرت: ۲ واحد در برابر ۸ عدد/);
});
