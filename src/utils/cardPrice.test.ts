import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cardPriceOf, cardPriceRangeOf } from './cardPrice';

const base = { price: 900000, originalPrice: 1000000, discountPercent: 10 };
const variants = [{ id: 'product-default', isDefault: true }, { id: 'store-default' }, { id: 'first-other' }];
const variantPrices = {
  'product-default': { price: 200000, originalPrice: 250000, discountPercent: 20 },
  'store-default': { price: 300000, originalPrice: 300000, discountPercent: 0 },
  'first-other': { price: 100000, originalPrice: 200000, discountPercent: 50 },
};

test('card uses the priced active store default before product default and cheaper siblings', () => {
  assert.deepEqual(cardPriceOf({ ...base, variants, variantPrices, storeDefaultVariantId: 'store-default' }), variantPrices['store-default']);
  assert.deepEqual(cardPriceOf({ ...base, variants, variantPrices }), variantPrices['product-default']);
});

test('inactive or unpriced defaults fall through in server variant order, with a product fallback', () => {
  const active = [{ id: 'product-default', isDefault: true, isActive: false }, { id: 'unpriced', isDefault: true }, { id: 'first-other' }, { id: 'store-default' }];
  assert.deepEqual(cardPriceOf({ ...base, variants: active, variantPrices, storeDefaultVariantId: 'unpriced' }), variantPrices['first-other']);
  assert.deepEqual(cardPriceOf({ ...base, variants: [{ id: 'unpriced' }], variantPrices: {} }), base);
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
