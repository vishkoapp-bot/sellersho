const fs = require('fs');
const { request, getAuthToken } = require('./test_helpers.cjs');

async function main() {
  const fixture = JSON.parse(fs.readFileSync('supplier-browser-fixture.json', 'utf8'));
  const customerTracking = await request('GET', '/api/v1/orders/track/BROWSER-TRACK-42A8329C');
  const otherSupplierToken = await getAuthToken('09000000009');
  const otherOrders = await request('GET', '/api/v1/supplier/orders', null, otherSupplierToken);
  const otherMutation = await request('POST', `/api/v1/supplier/orders/${fixture.orderId}/shipping`, {
    carrier: 'پست پیشتاز',
    trackingNumber: 'SPOOF-UNAUTHORIZED',
  }, otherSupplierToken);
  console.log(JSON.stringify({
    orderId: fixture.orderId,
    customerTrackingStatus: customerTracking.status,
    customerOrderStatus: customerTracking.body?.status,
    customerTrackingNumber: customerTracking.body?.trackingCode,
    otherSupplierOrdersStatus: otherOrders.status,
    otherSupplierOrderCount: otherOrders.body?.orders?.length,
    otherSupplierMutationStatus: otherMutation.status,
  }, null, 2));
  if (customerTracking.status !== 200 || customerTracking.body?.status !== 'shipped') process.exitCode = 1;
  if (![200, 403].includes(otherOrders.status) || (otherOrders.body?.orders || []).some((order) => order.orderId === fixture.orderId)) process.exitCode = 1;
  if (![403, 404].includes(otherMutation.status)) process.exitCode = 1;
}

main().catch((error) => { console.error(error); process.exitCode = 1; });