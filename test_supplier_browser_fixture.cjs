const { request } = require('./test_helpers.cjs');
const fs = require('fs');

async function required(method, path, body, token, expected) {
  const response = await request(method, path, body, token);
  if (response.status !== expected && !(Array.isArray(expected) && expected.includes(response.status))) {
    throw new Error(`${method} ${path}: expected ${expected}, got ${response.status}: ${JSON.stringify(response.body)}`);
  }
  return response;
}

async function main() {
  const suffix = String(Date.now()).slice(-7);
  const supplierMobile = `0919${suffix}`;
  const supplierRegistration = await required('POST', '/api/v1/auth/supplier/register', {
    company_name: `Browser Fixture Supplier ${suffix}`,
    contact_person: 'Supplier Browser E2E',
    username: `browser_fixture_${suffix}`,
    password: 'TestPass123!',
    phone: supplierMobile,
    email: `browser-fixture-${suffix}@example.test`,
    warehouse_city: 'Tehran',
  }, null, [200, 201]);

  await required('PUT', '/api/v1/supplier/profile/shipping-settings', { supportsDirectShipping: true }, supplierRegistration.body.token, 200);
  const proposal = await required('POST', '/api/v1/supplier/products/propose', {
    title: `Browser Fixture Direct Product ${suffix}`,
    slug: `browser-fixture-direct-${suffix}`,
    categorySlug: 'digital',
    supplierBaseCost: 10000000,
    stock: 100,
    fulfillmentMethod: 'supplier_direct',
  }, supplierRegistration.body.token, 201);
  const adminToken = await (async () => {
    const sent = await required('POST', '/api/v1/auth/otp/send', { mobile: '09000000001' }, null, 200);
    return (await required('POST', '/api/v1/auth/otp/verify', { mobile: '09000000001', code: sent.body.demoCode || '12345', role: 'admin' }, null, 200)).body.token;
  })();
  const approval = await required('POST', `/api/v1/admin/products/approve/${proposal.body.proposal_id}`, { approved: true, notes: 'Browser supplier E2E fixture' }, adminToken, 200);
  const productId = approval.body.product_id || proposal.body.product.id;

  const sellerMobile = `0918${suffix}`;
  const sellerOtp = await required('POST', '/api/v1/auth/otp/send', { mobile: sellerMobile }, null, 200);
  const seller = await required('POST', '/api/v1/auth/otp/verify', { mobile: sellerMobile, code: sellerOtp.body.demoCode || '12345', role: 'seller' }, null, 200);
  const storeSlug = `browser-fixture-store-${suffix}`;
  await required('POST', '/api/v1/stores/register', { storeName: `Browser Fixture Store ${suffix}`, slug: storeSlug, ownerName: 'Browser Seller', profitMarginPercent: 15 }, seller.body.token, [200, 201]);
  await required('PUT', `/api/v1/stores/${storeSlug}/products`, { productIds: [productId] }, seller.body.token, 200);

  const customerMobile = `0917${suffix}`;
  const customerOtp = await required('POST', '/api/v1/auth/otp/send', { mobile: customerMobile }, null, 200);
  const customer = await required('POST', '/api/v1/auth/otp/verify', { mobile: customerMobile, code: customerOtp.body.demoCode || '12345', role: 'customer' }, null, 200);
  const order = await required('POST', '/api/v1/orders', {
    storeId: storeSlug,
    customerInfo: { fullName: 'Browser Fixture Customer', phone: customerMobile, province: 'Tehran', city: 'Tehran', address: 'Browser E2E Address', postalCode: '1417755331' },
    paymentMethod: 'cash',
    items: [{ productId, quantity: 1 }],
  }, customer.body.token, [200, 201]);
  const orderId = order.body.orderId || order.body.id;
  const orderNumber = order.body.orderNumber;
  const paymentToken = await required('POST', '/api/v1/payments/shaparak/token', { orderId, orderNumber, amount: order.body.payableAmount, redirectUrl: 'http://localhost:5173/payment-result' }, customer.body.token, 200);
  await required('POST', '/api/v1/payments/shaparak/callback', { token: paymentToken.body.token, orderNumber, status: 'success' }, null, [200, 302]);

  const shipmentList = await required('GET', '/api/v1/supplier/orders', null, supplierRegistration.body.token, 200);
  const shipment = shipmentList.body.orders.find((item) => item.orderId === orderId);
  if (!shipment) throw new Error(`Supplier-owned order ${orderId} was not returned by supplier orders API`);
  const evidence = { supplierMobile, supplierToken: supplierRegistration.body.token, orderId, orderNumber, shipmentId: shipment.id, customerToken: customer.body.token, customerMobile };
  fs.writeFileSync('supplier-browser-fixture.json', JSON.stringify(evidence, null, 2), 'utf8');
  console.log(JSON.stringify({ orderId, orderNumber, shipmentId: shipment.id, supplierMobile }));
}

main().catch((error) => { console.error(error.message); process.exit(1); });