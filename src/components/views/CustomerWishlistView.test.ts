import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { Product, ProductVariant } from '../../types';
import { cardPriceOf, cardPriceVariantOf } from '../../utils/cardPrice';
import { resolvePurchase } from '../../utils/productPurchase';
import { resolveWishlistQuickAdd } from './CustomerWishlistView';

const cheapVariant: ProductVariant = {
  id: 'v-cheap', variantName: 'ارزان', name: 'ارزان', spnc: 'v-cheap', attributes: {},
  isDefault: true, stock: 0, isActive: true, minCartQty: 1, maxCartQty: null, inventoryMode: 'tracked',
};
const availableVariant: ProductVariant = {
  ...cheapVariant, id: 'v-ok', variantName: 'موجود', name: 'موجود', spnc: 'v-ok',
  isDefault: false, stock: 3,
};
const product: Product = {
  id: 'p', title: 'Product', slug: 'product', category: 'clothing', supplierId: 'supplier',
  supplierName: 'Supplier', supplierBaseCost: 0, platformMargin: 0, discountPercent: 0, cashbackPercent: 0,
  rating: 0, reviewsCount: 0, salesCount: 0, image: '', images: [], specs: {}, description: '',
  warehouse: '', preparationDays: 1, supplierWarranty: '', price: 100000, originalPrice: 100000,
  stock: 3, productType: 'variable', schemaVersion: 2, isActive: true,
  variants: [cheapVariant, availableVariant],
  variantPrices: {
    'v-cheap': { price: 100000, originalPrice: 100000, discountPercent: 0 },
    'v-ok': { price: 150000, originalPrice: 150000, discountPercent: 0 },
  },
};

test('wishlist quick-add uses the purchasable variant at the displayed card price', () => {
  const cardPrice = cardPriceOf(product).price;
  assert.equal(cardPrice, 150000);
  const purchase = resolveWishlistQuickAdd(product);
  assert.ok(purchase);
  assert.equal(purchase.variant?.id, 'v-ok');
  assert.equal(purchase.product.price, cardPrice);
});

test('the sold-out effective default fails purchase resolution', () => {
  assert.equal(resolvePurchase(product, 'v-cheap'), undefined);
  assert.ok(resolvePurchase(product, 'v-ok'));
});

test('wishlist quick-add fails when both variants are sold out', () => {
  const soldOut = { ...product, variants: [cheapVariant, { ...availableVariant, stock: 0 }] };
  assert.equal(resolveWishlistQuickAdd(soldOut), undefined);
});

test('wishlist quick-add prefers a purchasable store default over a cheaper variant', () => {
  const storeProduct = {
    ...product,
    storeDefaultVariantId: 'v-ok',
    variants: [cheapVariant, availableVariant, { ...availableVariant, id: 'v-other', spnc: 'v-other' }],
    variantPrices: {
      ...product.variantPrices,
      'v-other': { price: 120000, originalPrice: 120000, discountPercent: 0 },
    },
  };
  const purchase = resolveWishlistQuickAdd(storeProduct);
  assert.ok(purchase);
  assert.equal(purchase.variant?.id, 'v-ok');
  assert.equal(purchase.product.price, 150000);
});

test('wishlist quick-add resolves a simple product without variants only when in stock', () => {
  const simple: Product = {
    ...product, productType: 'simple', schemaVersion: undefined, stock: 5, variants: [], variantPrices: {},
  };
  const purchase = resolveWishlistQuickAdd(simple);
  assert.ok(purchase);
  assert.equal(purchase.variant, undefined);
  assert.equal(resolveWishlistQuickAdd({ ...simple, stock: 0 }), undefined);
});

test('wishlist quick-add fails for a variable product without priced variants', () => {
  const unpriced = { ...product, variantPrices: {} };
  assert.equal(cardPriceVariantOf(unpriced).variant, null);
  assert.equal(resolveWishlistQuickAdd(unpriced), undefined);
});

test('wishlist quick-add uses the selected variant minimum quantity', () => {
  const withMinimum = { ...product, variants: [cheapVariant, { ...availableVariant, minCartQty: 2 }] };
  assert.equal(resolveWishlistQuickAdd(withMinimum)?.minQty, 2);
});

test('wishlist view never reads the effective default flag', () => {
  const source = readFileSync(new URL('./CustomerWishlistView.tsx', import.meta.url), 'utf8');
  assert.equal((source.match(/isDefault/g) ?? []).length, 0);
});
