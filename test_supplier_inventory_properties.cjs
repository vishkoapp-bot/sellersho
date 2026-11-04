const { request, getAuthToken } = require('./test_helpers.cjs');
const assert = require('assert');

async function main() {
  const suffix = String(Date.now()).slice(-7);
  const supplierMobile = `0916${suffix}`;
  const registration = await request('POST', '/api/v1/auth/supplier/register', {
    company_name: `Inventory Properties Supplier ${suffix}`, contact_person: 'Inventory Test', username: `inventory_${suffix}`, password: 'TestPass123!', phone: supplierMobile, email: `inventory-${suffix}@test.com`, warehouse_city: 'Tehran'
  });
  assert([200,201].includes(registration.status));
  const supplierToken = registration.body.token;
  const proposal = await request('POST', '/api/v1/supplier/products/propose', { title:`Inventory Product ${suffix}`, slug:`inventory-product-${suffix}`, categorySlug:'digital', supplierBaseCost:1000000, stock:10, fulfillmentMethod:'supplier_direct', description:'محصول آزمایشی با مشخصات کامل برای تست موجودی و مرجوعی جزئی.', imageUrl:'https://example.com/inventory-product.jpg', warranty:'۱۸ ماه گارانتی شرکتی' }, supplierToken);
  if (proposal.status !== 201) throw new Error(`proposal failed: ${proposal.status} ${JSON.stringify(proposal.body)}`);
  const productId = proposal.body.product.id;
  const adminToken = await getAuthToken('09000000001');
  const approval = await request('POST', `/api/v1/admin/products/approve/${proposal.body.proposal_id}`, { approved:true, notes:'Inventory properties focused test' }, adminToken);
  assert.equal(approval.status, 200);
  const partial = await request('POST', '/api/v1/supplier/returns', { productId, quantity:3, collectionMethod:'supplier_collect', reason:'تست سلامت و بررسی بازگشت بخشی از موجودی' }, supplierToken);
  assert.equal(partial.status, 201);
  assert.equal(partial.body.requestedQuantity, 3);
  const excessive = await request('POST', '/api/v1/supplier/returns', { productId, quantity:8, collectionMethod:'supplier_collect', reason:'درخواست بیش از موجودی باقی‌مانده' }, supplierToken);
  assert.equal(excessive.status, 400);
  const warranties = await request('GET', '/api/v1/supplier/warranties', null, supplierToken);
  assert.equal(warranties.status, 200);
  const invalidWarranty = await request('POST', '/api/v1/supplier/warranties', { productId, name:'گارانتی ناقص', description:'کوتاه', images:[] }, supplierToken);
  assert.equal(invalidWarranty.status, 400);
  const warranty = await request('POST', '/api/v1/supplier/warranties', { productId, name:`گارانتی کامل ${suffix}`, description:'این توضیحات کامل برای بررسی کیفیت، مدت تعهد و شرایط خدمات پس از فروش کالا ثبت شده است.', images:['https://example.com/inventory-proof.jpg'] }, supplierToken);
  assert.equal(warranty.status, 201);
  console.log(JSON.stringify({supplierMobile,productId,partialStatus:partial.body.status,remainingEligible:partial.body.eligibleQuantity,warrantyRequest:warranty.body.id}));
}

main().catch((error) => { console.error(error); process.exit(1); });