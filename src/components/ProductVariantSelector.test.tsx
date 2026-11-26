import { initialPurchaseVariant } from '../utils/productPurchase';
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
  assert.match(html, /aria-pressed="true"[^>]*><span[^>]*><span>Red<\/span>/);
  assert.match(html, /disabled=""[^>]*><span[^>]*><span>Medium<\/span>/);
  assert.doesNotMatch(html, /disabled=""[^>]*><span[^>]*><span>Blue<\/span>/);
});

it('keeps a normal selectable legacy default SKU and omits inactive SKU chips', () => {
  const legacy = [variant('default', {}, 2), variant('choice', {}, 2), { ...variant('retired', {}, 2), isActive: false }];
  legacy[0].isDefault = true;
  const html = renderToStaticMarkup(<ProductVariantSelector variants={legacy} selectedId="default" onSelect={() => {}} />);
  assert.match(html, /default/);
  assert.match(html, /choice/);
  assert.doesNotMatch(html, /retired/);
});

it('localizes legacy attribute labels and keeps unknown keys selectable', () => {
  for (const [key, value, label] of [
    ['style', 'نگیندار', 'سبک'],
    ['color', 'قرمز', 'رنگ'],
    ['size', 'بزرگ', 'سایز'],
    ['material', 'طلا', 'material'],
  ]) {
    const entry = { ...variant('default', {}), name: 'مدل اول', variantName: 'مدل اول', isDefault: true, attributes: { [key]: value } };
    const html = renderToStaticMarkup(<ProductVariantSelector variants={[entry, variant("alternate", {})]} selectedId="default" onSelect={() => {}} />);
    assert.match(html, new RegExp(`>${label}</span>.*: ${value}`));
    if (key === 'style') assert.doesNotMatch(html.replace(/<[^>]+>/g, ""), /style/);
    assert.match(html, /<button[^>]*>(?:(?!<\/button>).)*مدل اول/);
    assert.doesNotMatch(html, /<button[^>]*disabled/);
  }
});

it('omits the synthetic chip and presses the cheapest real variant', () => {
  const variants = [
    { ...variant('default', {}), variantName: 'پیش‌فرض', isDefault: true },
    variant('regular', {}),
    variant('cheapest', {}),
  ];
  const variable = {
    ...product, productSchema: undefined, variants,
    variantPrices: {
      default: { price: 10, originalPrice: 10, discountPercent: 0 },
      regular: { price: 100, originalPrice: 100, discountPercent: 0 },
      cheapest: { price: 50, originalPrice: 50, discountPercent: 0 },
    },
  } satisfies Product;
  const selectedId = initialPurchaseVariant(variable)?.id ?? null;
  const render = (options: ProductVariant[]) => renderToStaticMarkup(
    <ProductVariantSelector product={variable} variants={options} selectedId={selectedId} onSelect={() => {}} />,
  );
  const html = render(variants);
  assert.doesNotMatch(html, /پیش‌?فرض/);
  assert.match(html, /aria-pressed="true"[^>]*><span[^>]*><span[^>]*>cheapest<\/span>/);
  const single = render([variants[0], variants[2]]);
  assert.doesNotMatch(single, /پیش‌?فرض/);
  assert.equal(single.match(/<button\b/g)?.length, 1);
});

it('renders the sole پیش‌فرض variant for a simple product', () => {
  const entry = { ...variant('default', {}), variantName: 'پیش‌فرض' };
  const simple: Product = { ...product, productType: 'simple', productSchema: undefined, variants: [entry] };
  const html = renderToStaticMarkup(
    <ProductVariantSelector product={simple} variants={[entry]} selectedId={entry.id} onSelect={() => {}} />,
  );
  assert.match(html, /<button[^>]*aria-pressed="true".*پیش‌فرض/);
});

it('keeps پیش‌فرض selectable when product metadata is absent', () => {
  const entry = { ...variant('default', {}), variantName: 'پیش‌فرض' };
  const html = renderToStaticMarkup(
    <ProductVariantSelector variants={[entry, variant('other', {})]} selectedId={entry.id} onSelect={() => {}} />,
  );
  assert.match(html, /<button[^>]*aria-pressed="true".*پیش‌فرض/);
});
