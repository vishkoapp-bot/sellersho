import assert from 'node:assert/strict';
import { test } from 'node:test';
import { normalizeOrder } from './SellerOrdersTab';

test('normalizeOrder builds a nested title from a live order item', () => {
  const liveItem = {
    discountAmount: 0,
    finalUnitPrice: 135000,
    id: 'item-1',
    orderId: 'order-1',
    platformFee: 8000,
    pricingPolicyVersion: 'toman-bps-nearest100-floor-v1',
    productId: 'prod-bracelet',
    productTitle: 'دستبند طلا ۱۸ عیار',
    quantity: 1,
    sellerMargin: 27000,
    variantId: 'variant-1',
    variantName: 'ساده',
    variantSpnc: 'TEST-1',
    wholesaleCost: 100000,
  };
  const [item] = normalizeOrder({ items: [liveItem] }).items;
  assert.deepEqual(item, { ...liveItem, product: { title: 'دستبند طلا ۱۸ عیار' } });
});

test('normalizeOrder uses the first non-empty title in nested, camelCase, snake_case order', () => {
  const [nested, camel, snake] = normalizeOrder({ items: [
    { product: { title: 'nested' }, productTitle: 'camel', product_title: 'snake' },
    { product: { title: '' }, productTitle: 'camel', product_title: 'snake' },
    { productTitle: '', product_title: 'snake' },
  ] }).items;
  assert.equal(nested.product.title, 'nested');
  assert.equal(camel.product.title, 'camel');
  assert.equal(snake.product.title, 'snake');
});

test('normalizeOrder treats omitted and empty titles alike', () => {
  const [omitted, empty] = normalizeOrder({ items: [
    { productId: 'deleted' },
    { productId: 'deleted', product: { id: 'deleted', title: '' }, productTitle: '', product_title: '' },
  ] }).items;
  assert.equal(omitted.product, undefined);
  assert.equal(empty.product, undefined);
});

test('normalizeOrder preserves the nested mock-mode item', () => {
  const mockItem = {
    product: { id: 'prod-mock', title: 'کالای آزمایشی', price: 100000, stock: 10 },
    quantity: 2,
    variantName: 'رنگ آبی',
  };
  assert.deepEqual(normalizeOrder({ items: [mockItem] }).items, [mockItem]);
});
