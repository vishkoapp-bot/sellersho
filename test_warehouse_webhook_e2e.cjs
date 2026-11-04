const { request, BASE_URL } = require('./test_helpers.cjs');
const WEBHOOK_SECRET = process.env.TRACKING_WEBHOOK_SECRET || 'drup_tracking_webhook_secret_dev_2026';
let state = { customerToken: null, customerUser: null, orderID: null, orderNumber: null, trackingCode: null, productID: null };
const steps = [];
function logStep(step, name, status, detail) {
  steps.push({ step, name, status, detail });
  const icon = status === 'PASS' ? '✅' : '❌';
  console.log(`${icon} Step ${step}: ${name} - ${status}`);
  if (detail) console.log(`   ${detail}`);
}
async function authenticateCustomer() {
  const phone = `0912${Date.now().toString().slice(-7)}`;
  const otpRes = await request('POST', '/api/v1/auth/otp/send', { mobile: phone });
  if (otpRes.status !== 200) { logStep(1, 'Customer OTP', 'FAIL', `Status ${otpRes.status}`); throw new Error('OTP failed'); }
  const code = otpRes.body?.demoCode || '123456';
  const verifyRes = await request('POST', '/api/v1/auth/otp/verify', { mobile: phone, code, role: 'customer' });
  if (verifyRes.status !== 200 || !verifyRes.body?.token) { logStep(1, 'Customer Auth', 'FAIL', `Status ${verifyRes.status}`); throw new Error('Auth failed'); }
  state.customerToken = verifyRes.body.token;
  state.customerUser = verifyRes.body.user;
  logStep(1, 'Customer Authentication', 'PASS', `Authenticated as ${state.customerUser.fullName}`);
}
async function createWarehouseOrder() {
  const productsRes = await request('GET', '/api/v1/products?in_stock=true');
  if (productsRes.status !== 200 || !productsRes.body?.length) { logStep(2, 'Fetch Products', 'FAIL', 'No products'); throw new Error('No products'); }
  const product = productsRes.body.find((candidate) => candidate.id === 'prod-1' && candidate.stock > 0);
  if (!product) { logStep(2, 'Select Warehouse Product', 'FAIL', 'No warehouse-fulfilled product available'); throw new Error('No warehouse product'); }
  state.productID = product.id;
  const orderPayload = {
    storeId: 'central',
    customerInfo: {
      fullName: state.customerUser.fullName,
      province: 'تهران',
      city: 'تهران',
      address: 'آدرس تست انبار',
      postalCode: '1234567890',
    },
    paymentMethod: 'cash',
    items: [{ productId: product.id, quantity: 1 }],
  };
  const orderRes = await request('POST', '/api/v1/orders', orderPayload, state.customerToken);
  if (orderRes.status !== 201 || !orderRes.body?.orderId) { logStep(2, 'Create Order', 'FAIL', `Status ${orderRes.status}`); throw new Error('Order creation failed'); }
  state.orderID = orderRes.body.orderId; state.orderNumber = orderRes.body.orderNumber; state.trackingCode = orderRes.body.trackingCode;
  const tokenRes = await request('POST', '/api/v1/payments/shaparak/token', {
    orderId: state.orderID,
    orderNumber: state.orderNumber,
    amount: orderRes.body.payableAmount,
    redirectUrl: 'http://localhost:5173/payment-result',
  }, state.customerToken);
  if (tokenRes.status !== 200 || !tokenRes.body?.token) { logStep(2, 'Payment Token', 'FAIL', `Status ${tokenRes.status}`); throw new Error('Payment token failed'); }
  const paymentRes = await request('POST', '/api/v1/payments/shaparak/callback', { token: tokenRes.body.token, orderNumber: state.orderNumber, resCode: '0', cardMask: '6037-99**-****-5544' });
  if (paymentRes.status !== 200 && paymentRes.status !== 302) { logStep(2, 'Payment', 'FAIL', `Status ${paymentRes.status}`); throw new Error('Payment failed'); }
  logStep(2, 'Create Warehouse Order', 'PASS', `Order ${state.orderNumber} paid`);
}
async function webhookRequest(status, secret = WEBHOOK_SECRET) {
  const response = await request(
    'POST',
    '/api/v1/webhooks/tracking/update',
    { trackingNumber: state.trackingCode, status },
    null,
    { 'X-Webhook-Secret': secret }
  );
  return response;
}
async function progressWarehouseOrder() {
  const mobile = '09000000071';
  const otp = await request('POST', '/api/v1/auth/otp/send', { mobile });
  const login = await request('POST', '/api/v1/auth/otp/verify', { mobile, code: otp.body?.demoCode || '12345', role: 'warehouse_staff' });
  if (login.status !== 200 || !login.body?.token) throw new Error(`Warehouse auth failed: ${login.status}`);
  const token = login.body.token;
  const start = await request('POST', `/api/v1/warehouse/orders/${state.orderID}/start-picking`, {}, token);
  if (start.status !== 200 || start.body?.status !== 'picking') throw new Error(`Start picking failed: ${start.status}`);
  const pick = await request('POST', `/api/v1/warehouse/orders/${state.orderID}/pick-item`, { productId: state.productID, quantity: 1, barcode: '6260123456789', binLocation: 'A-12-05' }, token);
  if (pick.status !== 200) throw new Error(`Pick item failed: ${pick.status}`);
  const complete = await request('POST', `/api/v1/warehouse/orders/${state.orderID}/complete-picking`, {}, token);
  if (complete.status !== 200 || complete.body?.status !== 'picked') throw new Error(`Complete picking failed: ${complete.status}`);
  const pack = await request('POST', `/api/v1/warehouse/orders/${state.orderID}/pack`, {}, token);
  if (pack.status !== 200 || pack.body?.status !== 'packed') throw new Error(`Pack failed: ${pack.status}`);
  state.trackingCode = pack.body?.trackingNumber || state.trackingCode;
  const ship = await request('POST', `/api/v1/warehouse/orders/${state.orderID}/ship`, { carrier: 'پست پیشتاز', trackingNumber: state.trackingCode }, token);
  if (ship.status !== 200 || ship.body?.status !== 'shipped') throw new Error(`Ship failed: ${ship.status}`);
  logStep(3, 'Warehouse Processing → Shipped', 'PASS', 'processing → picking → picked → packed → shipped');
}
async function testWebhookAuth() {
  const noSecret = await webhookRequest('delivered', '');
  if (noSecret.status !== 401) { logStep(3, 'Webhook Auth: Missing Secret', 'FAIL', `Expected 401, got ${noSecret.status}`); throw new Error('Should reject missing secret'); }
  const badSecret = await webhookRequest('delivered', 'wrong-secret');
  if (badSecret.status !== 401) { logStep(3, 'Webhook Auth: Invalid Secret', 'FAIL', `Expected 401, got ${badSecret.status}`); throw new Error('Should reject invalid secret'); }
  logStep(4, 'Webhook Authentication Guards', 'PASS', 'Missing and invalid secrets rejected');
}
async function testWebhookFlow() {
  const shipped = await webhookRequest('shipped');
  if (shipped.status !== 200) { logStep(5, 'Webhook: Mark Shipped', 'FAIL', `Status ${shipped.status}`); throw new Error('Shipped webhook failed'); }
  const delivered = await webhookRequest('delivered');
  if (delivered.status !== 200 || !delivered.body?.success) { logStep(5, 'Webhook: Delivered', 'FAIL', `Status ${delivered.status}`); throw new Error('Delivered webhook failed'); }
  logStep(5, 'Webhook: Shipped → Delivered', 'PASS', 'Carrier webhook processed');
}
async function verifyStatus() {
  const orderRes = await request('GET', `/api/v1/orders/track/${state.trackingCode}`);
  if (orderRes.status !== 200) { logStep(6, 'Verify Status', 'FAIL', `Status ${orderRes.status}`); throw new Error('Fetch failed'); }
  const order = orderRes.body;
  if (order.status !== 'delivered') { logStep(6, 'Verify Status', 'FAIL', `Expected 'delivered', got '${order.status}'`); throw new Error('Wrong status'); }
  if (order.trackingCode !== state.trackingCode && order.tracking_code !== state.trackingCode) { logStep(6, 'Verify Tracking Code', 'FAIL', 'Tracking code did not persist'); throw new Error('Tracking code incorrect'); }
  const ordersRes = await request('GET', '/api/v1/orders', null, state.customerToken);
  const persisted = Array.isArray(ordersRes.body) ? ordersRes.body.find((candidate) => candidate.id === state.orderID || candidate.orderId === state.orderID) : null;
  if (!persisted || (persisted.paymentStatus || persisted.payment_status) !== 'paid' || persisted.profitReleased) { logStep(6, 'Verify Payment and Profit Lock', 'FAIL', 'Paid/unreleased invariants not present in customer order response'); throw new Error('Financial invariant failed'); }
  logStep(6, 'Verify Order Status After Webhook', 'PASS', 'Order delivered, profit locked');
}
async function testIdempotency() {
  const dup = await webhookRequest('delivered');
  if (dup.status !== 200) { logStep(6, 'Idempotency', 'FAIL', `Status ${dup.status}`); throw new Error('Duplicate failed'); }
  logStep(7, 'Webhook Idempotency', 'PASS', 'Duplicate handled');
}
async function testConfirmation() {
  const confirm = await request('POST', `/api/v1/orders/confirm-delivery/${state.orderID}`, {}, state.customerToken);
  if (confirm.status !== 200) { logStep(7, 'Confirmation', 'FAIL', `Status ${confirm.status}`); throw new Error('Confirmation failed'); }
  const orderRes = await request('GET', `/api/v1/orders/track/${state.trackingCode}`);
  const order = orderRes.body;
  if (order.status !== 'delivered_confirmed') { logStep(7, 'Verify Status', 'FAIL', `Expected 'delivered_confirmed', got '${order.status}'`); throw new Error('Status incorrect'); }
  const confirmedOrders = await request('GET', '/api/v1/orders', null, state.customerToken);
  const confirmed = Array.isArray(confirmedOrders.body) ? confirmedOrders.body.find((candidate) => candidate.id === state.orderID || candidate.orderId === state.orderID) : null;
  if (!confirmed || confirmed.status !== 'delivered_confirmed' || !confirmed.profitReleased) { logStep(7, 'Verify Profit Released', 'FAIL', 'Confirmed financial state not present in customer order response'); throw new Error('Profit not released'); }
  logStep(8, 'Customer Confirmation Releases Profit', 'PASS', 'Profit correctly released');
}
async function testConfirmIdempotency() {
  const dup = await request('POST', `/api/v1/orders/confirm-delivery/${state.orderID}`, {}, state.customerToken);
  if (dup.status !== 404) { logStep(8, 'Confirm Idempotency', 'FAIL', `Expected 404, got ${dup.status}`); throw new Error('Should be 404'); }
  logStep(9, 'Confirmation Idempotency', 'PASS', 'Duplicate rejected');
}
async function report() {
  console.log('\n' + '='.repeat(80)); console.log('WAREHOUSE WEBHOOK E2E TEST REPORT'); console.log('='.repeat(80));
  const passed = steps.filter(s => s.status === 'PASS').length; const failed = steps.filter(s => s.status === 'FAIL').length; const total = steps.length;
  console.log(`\nTotal Steps: ${total}\nPassed: ${passed}\nFailed: ${failed}\nSuccess Rate: ${((passed / total) * 100).toFixed(1)}%`);
  console.log('\n' + '-'.repeat(80)); console.log('DETAILED RESULTS:'); console.log('-'.repeat(80));
  steps.forEach(s => { const icon = s.status === 'PASS' ? '✅' : '❌'; console.log(`${icon} Step ${s.step}: ${s.name}`); if (s.detail) console.log(`   ${s.detail}`); });
  console.log('\n' + '='.repeat(80));
  if (failed > 0) { console.error('\n❌ TEST FAILED\n'); process.exitCode = 1; } else { console.log('\n✅ ALL TESTS PASSED\n'); }
}
async function main() {
  try { console.log('Starting Warehouse Webhook E2E Test...\n'); await authenticateCustomer(); await createWarehouseOrder(); await progressWarehouseOrder(); await testWebhookAuth(); await testWebhookFlow(); await verifyStatus(); await testIdempotency(); await testConfirmation(); await testConfirmIdempotency(); await report(); }
  catch (error) { console.error('\n❌ Test failed:', error.message); if (steps.length > 0) { steps.push({ step: 'fatal', name: 'Execution', status: 'FAIL', detail: error.message }); await report(); } process.exitCode = 1; }
}
if (require.main === module) { main(); }
module.exports = { main };