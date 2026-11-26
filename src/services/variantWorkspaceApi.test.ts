import assert from 'node:assert/strict';
import test from 'node:test';
import { saveSupplierVariantWork } from './variantWorkspaceApi';
import { VariantEditConflict, type VariantEditPreview } from '../utils/variantWorkspace';

const source = { id: 'sku-1', productId: 'product-1' };
const stock: VariantEditPreview = { id: source.id, line: 2, current: { id: source.id, stock: 5 }, edit: { stock: 4, reason: 'counted' } };
const cost: VariantEditPreview = { id: source.id, line: 3, current: { id: source.id, cost: null, productCost: 1000 }, edit: { cost: 2000 } };

test('a successful HTTP status without a matching stock receipt can be retried using the same identity', async t => {
  const keys: string[] = [];
  t.mock.method(globalThis, 'fetch', async (_url: string, options: RequestInit) => {
    keys.push(new Headers(options.headers).get('Idempotency-Key')!);
    if (keys.length === 1) return Response.json({ stock: 4 });
    return Response.json({ product_id: source.productId, variant_id: source.id, stock: 4, retired_printed_codes: ['OLD-SPNCX'] });
  });
  await assert.rejects(saveSupplierVariantWork(source, stock, 'stock', 'attempt-stock'));
  assert.deepEqual(await saveSupplierVariantWork(source, stock, 'stock', 'attempt-stock'),
    { message: 'موجودی ثبت شد.', retired: ['OLD-SPNCX'] });
  assert.deepEqual(keys, ['attempt-stock', 'attempt-stock']);
});

test('supplier prices use a durable attempt and expose rejection as terminal', async t => {
  const keys: string[] = [];
  t.mock.method(globalThis, 'fetch', async (_url: string, options: RequestInit) => {
    keys.push(new Headers(options.headers).get('Idempotency-Key')!);
    if (keys.length === 1) return Response.json({});
    if (keys.length === 2) return Response.json({ status: 'pending_review', requestId: 'price-row' }, { status: 202 });
    return Response.json({ error: 'variant_workspace_price_rejected' }, { status: 409 });
  });
  await assert.rejects(saveSupplierVariantWork(source, cost, 'cost', 'attempt-price'));
  assert.equal((await saveSupplierVariantWork(source, cost, 'cost', 'attempt-price')).state, 'pending');
  await assert.rejects(saveSupplierVariantWork(source, cost, 'cost', 'attempt-price'), VariantEditConflict);
  assert.deepEqual(keys, ['attempt-price', 'attempt-price', 'attempt-price']);
});
