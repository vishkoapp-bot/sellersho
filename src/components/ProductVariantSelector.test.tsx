import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { ProductVariantSelector } from './ProductVariantSelector';
import type { Product, ProductVariant } from '../types';

const variant = (id: string, selection: Record<string, string>, stock = 2): ProductVariant => ({
  id, selection, stock, spnc: id, name: id, variantName: id, attributes: {}, isDefault: false,
  minCartQty: 1, maxCartQty: null, inventoryMode: 'tracked', isActive: true,
});
const product = {
  productType: 'variable', schemaVersion: 2, stock: 4,
  productSchema: { version: 2, type: 'variable', attributes: [
    { id: 'color', name: 'Color', role: 'axis', source: 'local', values: [{ id: 'r', label: 'Red' }, { id: 'b', label: 'Blue' }] },
    { id: 'size', name: 'Size', role: 'axis', source: 'local', values: [{ id: 's', label: 'Small' }, { id: 'm', label: 'Medium' }] },
  ] },
  variants: [variant('red-s', { color: 'r', size: 's' }), variant('blue-m', { color: 'b', size: 'm' }), variant('red-m', { color: 'r', size: 'm' }, 0)],
  variantPrices: { 'red-s': { price: 100, originalPrice: 100, discountPercent: 0 }, 'blue-m': { price: 200, originalPrice: 200, discountPercent: 0 } },
} as unknown as Product;

it('renders named independent axes, selected values, and disabled unavailable combinations', () => {
  const html = renderToStaticMarkup(<ProductVariantSelector product={product} variants={product.variants!} selectedId="red-s" onSelect={() => {}} />);
  assert.equal((html.match(/<fieldset/g) ?? []).length, 2);
  assert.match(html, /<legend[^>]*>Color<\/legend>/);
  assert.match(html, /<legend[^>]*>Size<\/legend>/);
  assert.match(html, /aria-pressed="true"[^>]*>Red<\/button>/);
  assert.match(html, /disabled=""[^>]*>Medium<\/button>/);
  assert.doesNotMatch(html, /disabled=""[^>]*>Blue<\/button>/);
});

it('keeps a normal selectable legacy default SKU and omits inactive SKU chips', () => {
  const legacy = [variant('default', {}, 2), variant('choice', {}, 2), { ...variant('retired', {}, 2), isActive: false }];
  legacy[0].isDefault = true;
  const html = renderToStaticMarkup(<ProductVariantSelector variants={legacy} selectedId="default" onSelect={() => {}} />);
  assert.match(html, /default/);
  assert.match(html, /choice/);
  assert.doesNotMatch(html, /retired/);
});
