import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cardPriceOf, cardPriceRangeOf, cardPriceVariantOf } from './cardPrice';

const base = { price: 900000, originalPrice: 1000000, discountPercent: 10 };
const variants = [{ id: 'product-default', isDefault: true, stock: 2 }, { id: 'store-default', stock: 2 }, { id: 'first-other', stock: 2 }];
const variantPrices = {
  'product-default': { price: 200000, originalPrice: 250000, discountPercent: 20 },
  'store-default': { price: 300000, originalPrice: 300000, discountPercent: 0 },
  'first-other': { price: 100000, originalPrice: 200000, discountPercent: 50 },
};

test('card uses the priced active store default before cheaper siblings', () => {
  assert.deepEqual(cardPriceOf({ ...base, variants, variantPrices, storeDefaultVariantId: 'store-default' }), variantPrices['store-default']);
  assert.deepEqual(cardPriceOf({ ...base, variants, variantPrices }), variantPrices['first-other']);
});

test('inactive or unpriced defaults fall through in server variant order, with a product fallback', () => {
  const active = [{ id: 'product-default', isDefault: true, isActive: false }, { id: 'unpriced', isDefault: true }, { id: 'first-other' }, { id: 'store-default' }];
  assert.deepEqual(cardPriceOf({ ...base, variants: active, variantPrices, storeDefaultVariantId: 'unpriced' }), variantPrices['first-other']);
  assert.deepEqual(cardPriceOf({ ...base, variants: [{ id: 'unpriced' }], variantPrices: {} }), base);
});

test('gold bracelet: card price and label come from the cheapest purchasable named variant', () => {
  const bracelet = {
    price: 83520000, originalPrice: 83520000, discountPercent: 0,
    productType: 'variable' as const,
    variants: [
      { id: 'v-default', variantName: 'پیش‌فرض', isDefault: true, stock: 2 },
      { id: 'v-plain', variantName: 'ساده', stock: 2 },
      { id: 'v-jeweled', variantName: 'نگین‌دار', stock: 2 },
    ],
    variantPrices: {
      'v-default': { price: 80040000, originalPrice: 80040000, discountPercent: 0 },
      'v-plain': { price: 76560000, originalPrice: 76560000, discountPercent: 0 },
      'v-jeweled': { price: 88320000, originalPrice: 88320000, discountPercent: 0 },
    },
  };
  assert.equal(cardPriceOf(bracelet).price, 76560000);
  assert.notEqual(cardPriceOf(bracelet).price, 83520000, 'product-level price must not be the card price');
  assert.deepEqual(cardPriceRangeOf(bracelet), { min: 76560000, max: 88320000, hasRange: true });
  const labelled = cardPriceVariantOf(bracelet);
  assert.equal(labelled.triple.price, 76560000);
  assert.deepEqual(labelled.variant, { id: 'v-plain', label: 'ساده' });
  // No priced variant → product-level triple with no variant label.
  const unpriced = cardPriceVariantOf({ price: 1000, originalPrice: 1000, variants: [{ id: 'x' }], variantPrices: {} });
  assert.equal(unpriced.triple.price, 1000);
  assert.equal(unpriced.variant, null);
});

test('explicit backend suspended-sale flag prevents reapplying a below-floor configured sale', () => {
  const authoritative = { price: 600000, originalPrice: 1000000, discountPercent: 40, salePrice: 1, onSale: false };
  assert.deepEqual(cardPriceOf({ ...base, variants: [{ id: 'selected', isDefault: true }], variantPrices: { selected: authoritative } }),
    { price: 600000, originalPrice: 1000000, discountPercent: 40 });
});

test('already applied backend sale is not stacked and main price stays distinct from secondary variant range', () => {
  const price = { price: 500000, originalPrice: 600000, discountPercent: 17, salePrice: 500000, onSale: true };
  assert.deepEqual(cardPriceOf({ ...base, variants: [{ id: 'selected', isDefault: true }], variantPrices: { selected: price } }),
    { price: 500000, originalPrice: 600000, discountPercent: 17 });
  const product = { ...base, productType: 'variable' as const, variants, variantPrices, storeDefaultVariantId: 'store-default', priceMin: 100000, priceMax: 300000 };
  assert.equal(cardPriceOf(product).price, 300000);
  assert.deepEqual(cardPriceRangeOf(product), { min: 100000, max: 300000, hasRange: true });
});

test('a priced sold-out store default falls through to the cheapest purchasable SKU', () => {
  const product = {
    ...base, productType: 'variable' as const, variantPrices, storeDefaultVariantId: 'store-default',
    variants: variants.map(variant => variant.id === 'store-default' ? { ...variant, stock: 0 } : variant),
  };
  assert.deepEqual(cardPriceOf(product), variantPrices['first-other']);
});

test('only simple cards can price from a sole synthetic-named SKU', () => {
  const price = { price: 100000, originalPrice: 100000, discountPercent: 0 };
  const product = {
    ...base, schemaVersion: 2, storeDefaultVariantId: 'synthetic',
    variants: [{ id: 'synthetic', variantName: 'پیش‌فرض', stock: 2 }],
    variantPrices: { synthetic: price },
  };
  assert.deepEqual(cardPriceOf({ ...product, productType: 'variable' }), base);
  assert.deepEqual(cardPriceOf({ ...product, productType: 'simple' }), price);
});

test('computed variable ranges exclude stale synthetic prices and preserve backend ranges', () => {
  const product = {
    ...base, productType: 'variable' as const,
    variants: [...variants, { id: 'synthetic', variantName: 'پیش‌فرض', stock: 2 }],
    variantPrices: { ...variantPrices, synthetic: { price: 1, originalPrice: 1, discountPercent: 0 } },
  };
  assert.deepEqual(cardPriceRangeOf(product), { min: 100000, max: 300000, hasRange: true });
  assert.deepEqual(cardPriceRangeOf({ ...product, priceMin: 1, priceMax: 300000 }),
    { min: 1, max: 300000, hasRange: true });
});
