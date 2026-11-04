const assert = require('node:assert/strict');
const { execFileSync } = require('node:child_process');
const { request, getAuthToken } = require('./test_helpers.cjs');
function sql(query) {
  return execFileSync('docker', ['exec', 'drup_postgres', 'psql', '-U', 'drup_admin', '-d', 'drup_db', '-tAc', query], { encoding: 'utf8' }).trim();
}
async function checked(method, path, body, token, status = 200) {
  const result = await request(method, path, body, token);
  assert.equal(result.status, status, `${method} ${path}: ${JSON.stringify(result.body)}`);
  return result.body;
}
async function run() {
  const fixtures = JSON.parse(sql("SELECT json_agg(x) FROM (SELECT p.id product,s.id supplier,u.mobile FROM products p JOIN suppliers s ON s.id=p.supplier_id JOIN users u ON u.id=s.owner_user_id WHERE s.id LIKE 'sup-direct_e2e_%' AND p.fulfillment_method='supplier_direct' AND p.stock>5 ORDER BY p.created_at DESC LIMIT 2) x"));
  assert.equal(fixtures.length, 2);
  assert.notEqual(fixtures[0].supplier, fixtures[1].supplier);
  const tokens = await Promise.all(fixtures.map(f => getAuthToken(f.mobile)));
  const mobile = `0917${String(Date.now()).slice(-7)}`;
  const sent = await checked('POST', '/api/v1/auth/otp/send', { mobile });
  const seller = await checked('POST', '/api/v1/auth/otp/verify', { mobile, code: sent.demoCode, role: 'seller' });
  const slug = `split-audit-${Date.now()}`;
  await checked('POST', '/api/v1/stores/register', { storeName: slug, slug, ownerName: 'Split Audit', profitMarginPercent: 15 }, seller.token, 201);
  await checked('PUT', `/api/v1/stores/${slug}/products`, { productIds: fixtures.map(f => f.product) }, seller.token);
  const customer = await getAuthToken('09129876543');
  const order = await checked('POST', '/api/v1/orders', { storeId: slug, customerInfo: { fullName: 'Split Customer', province: 'Tehran', city: 'Tehran', address: 'Audit address', postalCode: '1234567890' }, paymentMethod: 'cash', items: fixtures.map(f => ({ productId: f.product, quantity: 1 })) }, customer, 201);
  const id = order.id || order.orderId;
  assert.match(id, /^ord-[a-f0-9-]+$/);
  assert.equal(sql(`SELECT COUNT(*) FROM order_items WHERE order_id='${id}'`), '2');
  assert.equal(sql(`SELECT COUNT(DISTINCT supplier_id) FROM supplier_shipments WHERE order_id='${id}'`), '2');
  const payment = await checked('POST', '/api/v1/payments/shaparak/token', { orderId: id, orderNumber: order.orderNumber, amount: order.payableAmount, redirectUrl: 'http://localhost:3000/payment-result' }, customer);
  await checked('POST', '/api/v1/payments/shaparak/callback', { token: payment.token, orderNumber: order.orderNumber, status: 'success' });
  const allocations = [];
  for (let i = 0; i < 2; i++) {
    const list = await checked('GET', '/api/v1/supplier/shipments', null, tokens[i]);
    const mine = list.shipments.filter(s => s.orderId === id);
    assert.equal(mine.length, 1);
    assert.equal(mine[0].supplierId, fixtures[i].supplier);
    allocations.push(mine[0]);
  }
  for (let i = 0; i < 2; i++) {
    await checked('POST', `/api/v1/supplier/shipments/${allocations[1-i].id}/ship`, { carrier: 'Audit', trackingNumber: `foreign-${id}` }, tokens[i], 403);
    await checked('PUT', `/api/v1/supplier/products/${fixtures[1-i].product}/stock`, { new_stock: 5 }, tokens[i], 403);
    await checked('POST', `/api/v1/supplier/shipments/${allocations[i].id}/ship`, { carrier: 'Audit', trackingNumber: `split-${i}-${id}` }, tokens[i]);
    if (i === 0) assert.equal(sql(`SELECT status FROM orders WHERE id='${id}'`), 'processing');
  }
  assert.equal(sql(`SELECT status FROM orders WHERE id='${id}'`), 'shipped');
  for (let i = 0; i < 2; i++) {
    const response = await fetch('http://localhost:8080/api/v1/webhooks/tracking/update', {
      method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Webhook-Secret': process.env.TRACKING_WEBHOOK_SECRET || 'drup_tracking_webhook_secret_dev_2026' },
      body: JSON.stringify({ trackingNumber: `split-${i}-${id}`, status: 'delivered' }),
    });
    assert.equal(response.status, 200);
    if (i === 0) assert.equal(sql(`SELECT status FROM orders WHERE id='${id}'`), 'shipped', 'First supplier delivery must not mark aggregate order delivered');
  }
  assert.equal(sql(`SELECT status FROM orders WHERE id='${id}'`), 'delivered');
  await checked('POST', `/api/v1/orders/confirm-delivery/${id}`, {}, customer);
  assert.equal(sql(`SELECT status FROM orders WHERE id='${id}'`), 'delivered_confirmed');
  console.log(JSON.stringify({ order: id, allocations: allocations.map(s => ({ id: s.id, supplier: s.supplierId })), split: 'PASS', independentShipping: 'PASS', supplierIsolation: 'PASS' }));
}
run().catch(error => { console.error(error); process.exitCode = 1; });