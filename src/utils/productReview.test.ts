import { it } from 'node:test';
import assert from 'node:assert/strict';
import { pointerValue, skuReviewLabel, schemaReviewCaption, selectionReviewChoices, correctionPermissions, buildStructuralCorrection, proposalDraftRows, cartLimitCorrectionValue } from './productReview';
import type { ProductSchema } from '../types/productSchema';

const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'size/id', name: 'سایز', role: 'axis', source: 'requested', values: [{ id: 's', label: 'کوچک' }, { id: 'm', label: 'متوسط' }] }] };
const proposal = { schemaVersion: 2, productType: 'variable', productSchema: schema, variants: [{ id: 'sku-a', variant_name: 'مدل قدیمی', selection: { 'size/id': 's' }, supplier_base_cost: 1000, stock: 5, is_active: true }] };
const rejected = (...keys: string[]) => keys.map(field_key => ({ field_key, decision: 'rejected' }));

it('resolves stable attribute and SKU ids through arrays and preserves legacy numeric pointers', () => {
  assert.equal(pointerValue(proposal, '/variants/sku-a/stock'), 5);
  assert.equal(pointerValue(proposal, '/variants/0/stock'), 5);
  assert.equal(pointerValue(proposal, '/productSchema/attributes/size~1id/values/m/label'), 'متوسط');
  assert.equal(pointerValue(proposal, '/variants/unknown/stock'), undefined);
});
it('labels UUID review paths with schema and option captions', () => {
  assert.equal(skuReviewLabel(proposal, proposal.variants[0]), 'سایز: کوچک');
  assert.equal(schemaReviewCaption('/variants/sku-a/selection/size~1id', proposal), 'تنوع / سایز: کوچک / سایز');
  assert.equal(schemaReviewCaption('/productSchema/attributes/size~1id', proposal), 'ویژگی محصول / سایز');
  assert.deepEqual(selectionReviewChoices('/variants/sku-a/selection/size~1id', proposal), [{ label: 'کوچک', value: 's' }, { label: 'متوسط', value: 'm' }]);
});
it('grants only rejected structural groups and protects existing approved financial leaves', () => {
  const permissions = correctionPermissions(proposal, rejected('/variants', '/productSchema/attributes/size~1id', '/variants/sku-a/stock'));
  assert.equal(permissions.schemaEditable, true);
  assert.equal(permissions.attributeEditable('size/id'), true);
  assert.equal(permissions.attributeEditable('other'), false);
  assert.equal(permissions.attributeListEditable, false);
  assert.equal(permissions.variantsEditable, true);
  assert.equal(permissions.canEditSKUValue('sku-a', 'stock'), true);
  assert.equal(permissions.canEditSKUValue('sku-a', 'supplier_base_cost'), false);
  assert.equal(permissions.canEditSKUValue('new-id', 'supplier_base_cost'), true);
  assert.equal(correctionPermissions(proposal, []).canEditSKUValue('new-id', 'stock'), false);
});
it('emits a single rejected attribute correction without touching the root schema or SKU prices', () => {
  const edited = structuredClone(schema);
  edited.attributes[0].values[0].label = 'کوچک اصلاح‌شده';
  const patch = buildStructuralCorrection(proposal, rejected('/productSchema/attributes/size~1id'), edited, proposalDraftRows(proposal));
  assert.deepEqual(Object.keys(patch), ['/productSchema/attributes/size~1id']);
  assert.deepEqual(patch['/productSchema/attributes/size~1id'], edited.attributes[0]);
});
it('rejects changing an approved existing SKU price through a rejected structural group', () => {
  const rows = proposalDraftRows(proposal);
  rows[0].cost = '1200';
  rows[0].active = false;
  assert.throws(() => buildStructuralCorrection(proposal, rejected('/variants'), schema, rows), /قیمت|تأیید/);
});
it('sends full structural rows with original approved values, even when deactivating a SKU', () => {
  const rows = proposalDraftRows(proposal);
  rows[0].active = false;
  rows.push({ id: 'sku-b', name: 'متوسط', selection: { 'size/id': 'm' }, cost: '1500', stock: '2', active: true });
  const patch = buildStructuralCorrection(proposal, rejected('/variants'), schema, rows);
  assert.equal(patch['/variants'][0].supplier_base_cost, 1000);
  assert.equal(patch['/variants'][0].stock, 5);
  assert.equal(patch['/variants'][0].is_active, false);
  assert.equal(patch['/variants'][1].id, 'sku-b');
  assert.equal(patch['/variants'][1].supplier_base_cost, 1500);
});
it('emits ordinary rejected SKU leaves while leaving approved structure untouched', () => {
  const rows = proposalDraftRows(proposal);
  rows[0].stock = '7';
  assert.deepEqual(buildStructuralCorrection(proposal, rejected('/variants/sku-a/stock'), schema, rows), { '/variants/sku-a/stock': 7 });
  rows[0].active = false;
  assert.throws(() => buildStructuralCorrection(proposal, rejected('/variants/sku-a/stock'), schema, rows), /ساختار/);
});
it('corrects a rejected attribute and selected option together without replacing approved SKU structure', () => {
  const editedSchema = structuredClone(schema);
  editedSchema.attributes[0].values = [{ id: 'm', label: 'متوسط' }];
  const rows = proposalDraftRows(proposal);
  rows[0].selection['size/id'] = 'm';
  const fields = rejected('/productSchema/attributes/size~1id', '/variants/sku-a/selection/size~1id');
  const patch = buildStructuralCorrection(proposal, fields, editedSchema, rows);
  assert.deepEqual(Object.keys(patch).sort(), ['/productSchema/attributes/size~1id', '/variants/sku-a/selection/size~1id']);
  assert.equal(patch['/variants/sku-a/selection/size~1id'], 'm');
  assert.equal(correctionPermissions(proposal, fields).canEditSKUSelection('sku-a', 'size/id'), true);
  assert.equal(correctionPermissions(proposal, fields).canEditSKUSelection('other', 'size/id'), false);
});
it('preserves approved cart limits by UUID in a full structural correction, including nullable maximum', () => {
  const original = { ...proposal, variants: [
    { ...proposal.variants[0], min_cart_qty: 2, max_cart_qty: 8 },
    { ...proposal.variants[0], id: 'sku-b', selection: { 'size/id': 'm' }, min_cart_qty: 1, max_cart_qty: null },
  ] };
  const rows = proposalDraftRows(original);
  rows.reverse();
  rows.find(row => row.id === 'sku-a')!.active = false;
  const patch = buildStructuralCorrection(original, rejected('/variants'), schema, rows);
  const a = patch['/variants'].find((row: any) => row.id === 'sku-a');
  const b = patch['/variants'].find((row: any) => row.id === 'sku-b');
  assert.equal(a.min_cart_qty, 2);
  assert.equal(a.max_cart_qty, 8);
  assert.equal(b.min_cart_qty, 1);
  assert.equal(b.max_cart_qty, null);
});
it('gives limit leaves semantic captions and parses nullable maximum corrections as numeric values', () => {
  assert.equal(schemaReviewCaption('/variants/sku-a/min_cart_qty', proposal), 'تنوع / سایز: کوچک / حداقل تعداد خرید');
  assert.equal(schemaReviewCaption('/variants/sku-a/max_cart_qty', proposal), 'تنوع / سایز: کوچک / حداکثر تعداد خرید');
  assert.equal(cartLimitCorrectionValue('/variants/sku-a/max_cart_qty', ''), null);
  assert.equal(cartLimitCorrectionValue('/variants/sku-a/max_cart_qty', '۸'), 8);
  assert.equal(cartLimitCorrectionValue('/variants/sku-a/min_cart_qty', '۲'), 2);
});
