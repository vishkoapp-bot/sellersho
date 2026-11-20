// Hot-account profile (CAP-01): 100 customers with paid orders on ONE
// shared store concurrently cancel, then each retries the cancel.
// The shared seller account + refund-obligation writer is the
// contention point. Duplicate cancels must NOT create a second
// refund obligation (FIN-003: one durable obligation per paid
// cancellation). The runner's SQL oracle (see load_test_isolated.py /
// load_test_native.py) checks obligations per order; this driver only
// records the client-observed outcomes. Certified:false by design.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadSessions, loadCa} from './target-config.mjs';

const capTarget = resolveTarget();
const users = loadSessions(capTarget);
if (users.length !== 1000 || new Set(users.map((u) => u.id)).size !== 1000) {
  throw Error('1000 independent sessions required');
}
const agent = new https.Agent({keepAlive: true, maxSockets: 100, ca: loadCa(capTarget), rejectUnauthorized: true});
const requests = [], helpers = [];
const summary = {
  certified: false,
  scope: 'LOCAL hot-account: 100 paid orders on one store, concurrent paid-cancel + duplicate cancel retry',
  contenders: 100,
  paid: 0, cancelled: 0, duplicate_rejected: 0, unexpected: 0,
  failed_gates: [], requests_raw: requests, helper_requests_raw: helpers,
  oracle_scope: 'runner SQL: one order_refund_obligations row (purpose=cancellation) per cancelled paid order, zero duplicates, zero paid-cancelled orders without an obligation',
};

function call(principal, kind, path, body, helper = false) {
  return new Promise((resolve) => {
    let done = false;
    const start = performance.now();
    const finish = (status, data) => {
      if (done) return;
      done = true;
      (helper ? helpers : requests).push({principal, route_class: kind, status, latency_ms: performance.now() - start});
      resolve({status, body: data});
    };
    const req = https.request({hostname: capTarget.ip || capTarget.host, port: capTarget.port, servername: capTarget.host,
      path, method: body ? 'POST' : 'GET', agent, timeout: 10000,
      headers: {Authorization: 'Bearer ' + users[principal].token, 'Content-Type': 'application/json', Host: capTarget.host,
        ...(body?.idempotencyKey ? {'Idempotency-Key': body.idempotencyKey} : {})}}, (res) => {
      let raw = '';
      res.on('data', (b) => { if (raw.length < 262144) raw += b; });
      res.on('end', () => { let data; try { data = JSON.parse(raw); } catch {} finish(res.statusCode, data); });
      res.on('error', () => finish(0));
    });
    req.on('timeout', () => req.destroy());
    req.on('error', () => finish(0));
    req.end(body ? JSON.stringify(body) : undefined);
  });
}

function uuid(text) {
  const h = crypto.createHash('md5').update(text).digest('hex');
  return h.slice(0, 8) + '-' + h.slice(8, 12) + '-' + h.slice(12, 16) + '-' + h.slice(16, 20) + '-' + h.slice(20);
}

const orders = [];
try {
  // Prepare 100 paid orders on the SAME store (cap-store-1) so every
  // cancel contends on one seller account / refund-obligation writer.
  for (let principal = 0; principal < 100; principal++) {
    // Products of cap-store-1 only (same product->store rule as the
    // commerce drivers: store = 1 + (product % 100)); every cancel
    // then contends on the one shared seller account.
    const product = 100 * (principal + 1);
    const items = [{productId: 'cap-product-' + product, variantId: uuid('cap-sku-' + ((product - 1) * 5 + 1)), quantity: 1}];
    const destination = {province: 'Tehran', city: 'Tehran', address: 'Synthetic hot-account address', postalCode: '1111111111'};
    const quote = await call(principal, 'prepare_quote', '/api/v1/shipping/quote', {items, destination}, true);
    if (quote.status !== 200 || !quote.body?.quoteId) throw Error('hot-account quote preparation failed');
    const order = await call(principal, 'prepare_order', '/api/v1/orders',
      {storeId: 'cap-store-1', items, shippingQuoteId: quote.body.quoteId,
       customerInfo: {...destination, fullName: 'Synthetic capacity customer', phone: users[principal].mobile},
       paymentMethod: 'cash', idempotencyKey: 'cap-hotacct-' + crypto.randomUUID()}, true);
    if (order.status !== 201 || !order.body?.id) throw Error('hot-account order preparation failed');
    const token = await call(principal, 'prepare_token', '/api/v1/payments/shaparak/token',
      {orderId: order.body.id, orderNumber: order.body.orderNumber, amount: order.body.payableAmount}, true);
    if (token.status !== 200 || !token.body?.token || token.body.amount !== order.body.payableAmount) {
      throw Error('hot-account token preparation failed');
    }
    const paid = await call(principal, 'prepare_callback', '/api/v1/payments/shaparak/callback',
      {token: token.body.token, orderNumber: order.body.orderNumber, amount: order.body.payableAmount}, true);
    if (paid.status !== 200 || paid.body?.status !== 'paid') throw Error('hot-account payment preparation failed');
    orders.push({principal, id: order.body.id, amount: order.body.payableAmount});
    summary.paid++;
  }
  const started = performance.now();
  // Measured burst: 100 concurrent paid cancels on the shared account.
  const cancels = await Promise.all(orders.map((o) =>
    call(o.principal, 'cancel', '/api/v1/orders/' + o.id + '/cancel', {})));
  for (const r of cancels) {
    if (r.status === 200) summary.cancelled++;
    else summary.unexpected++;
  }
  // Duplicate-effect probe: retry every cancel once. A second cancel of
  // an already-cancelled order must be rejected (409/400), never a
  // second obligation.
  const retries = await Promise.all(orders.map((o) =>
    call(o.principal, 'duplicate_cancel', '/api/v1/orders/' + o.id + '/cancel', {})));
  for (const r of retries) {
    if (r.status === 409 || r.status === 400) summary.duplicate_rejected++;
    else summary.unexpected++;
  }
  summary.elapsed_seconds = (performance.now() - started) / 1000;
  summary.route_metrics = {};
  for (const kind of ['cancel', 'duplicate_cancel']) {
    const times = requests.filter((r) => r.route_class === kind).map((r) => r.latency_ms).sort((a, b) => a - b);
    const p95 = times[Math.ceil(times.length * .95) - 1], p99 = times[Math.ceil(times.length * .99) - 1];
    summary.route_metrics[kind] = {requests: times.length, p95_ms: p95, p99_ms: p99};
    if (!times.length || p95 > 750 || p99 > 2000) summary.failed_gates.push(kind + '_latency');
  }
  if (summary.cancelled !== 100) summary.failed_gates.push('cancel_count');
  if (summary.duplicate_rejected !== 100) summary.failed_gates.push('duplicate_cancel_not_rejected');
  if (summary.unexpected) summary.failed_gates.push('unexpected_cancel_outcome');
} catch (error) {
  summary.failed_gates.push('preparation_or_execution');
  summary.failure = error.message;
} finally {
  summary.requests = requests.length;
  summary.helper_api_calls = helpers.length;
  agent.destroy();
  fs.writeFileSync('/tmp/summary.json', JSON.stringify(summary, null, 2));
  process.exitCode = summary.failed_gates.length ? 1 : 0;
}
