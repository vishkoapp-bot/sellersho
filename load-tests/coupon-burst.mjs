// Coupon-last-use burst profile (FIN-013 §3.2, NEW — no HTTP-level
// coupon-burst profile existed; repo-level proof is P8,
// TestFIN007LastUse100, 100 goroutines, pool 25).
//
// Two variants, same shape as P8 but over HTTP through the Caddy/TLS
// path, reusing the lifecycle queues:
//   A: one coupon maxUses=1, 100 concurrent checkout attempts from
//      distinct sessions — accept exactly 1, reject 99 with a clear
//      pre-commit `coupon_exhausted` (409) response, never a silently
//      undiscounted order.
//   B: one coupon maxUses=100, 1,000 concurrent checkout attempts —
//      accept exactly 100, reject 900 the same way.
// Burst window 60 s; then a 5-min settle window asserting
// reservation/counter/order counts via SQL (runner oracle — the
// counts live in order_coupon_reservations / seller_discount_codes /
// orders, see the oracle_scope below; the runner-side SQL is owed the
// same way hot-account's oracle lives in load_test_native.py).
// Metrics: per-attempt latency, coupon_exhausted count vs
// silent-discount count (must be 0), deadlock/serialization-failure
// retries (bounded, counted, never hidden).
// The coupon is provisioned through the seller API (a staff seller
// session from the sessions file creates it on cap-store-1); the
// fixture does not seed coupons. Certified:false by design.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadSessions, loadStaffSessions, loadCa} from './target-config.mjs';

const capTarget = resolveTarget();
const users = loadSessions(capTarget);
if (users.length !== 1000 || new Set(users.map((u) => u.id)).size !== 1000) {
  throw Error('1000 independent sessions required');
}
const staff = loadStaffSessions(capTarget);
const seller = staff.find((s) => s.role === 'seller' && s.storeId);
if (!seller) throw Error('no seller staff session (coupon provisioning needs one)');
const storeSlug = seller.storeId; // cap stores: slug == id (cap-store-N)
const agent = new https.Agent({keepAlive: true, maxSockets: 100, ca: loadCa(capTarget), rejectUnauthorized: true});
const requests = [], helpers = [];
const summary = {
  certified: false,
  scope: 'LOCAL coupon-burst: maxUses=1 x100 attempts, maxUses=100 x1000 attempts, one coupon on cap-store-1',
  variants: {},
  failed_gates: [], requests_raw: requests, helper_requests_raw: helpers,
  oracle_scope: 'runner SQL: reservation count == used_count == discounted orders per coupon (order_coupon_reservations state IN (reserved,consumed,review_hold) vs seller_discount_codes.used_count); zero silently-undiscounted orders (orders with the coupon code and coupon_discount_amount=0 must not exist)',
};

function call(principal, kind, path, body, helper = false, tokenOverride = null) {
  return new Promise((resolve) => {
    let done = false;
    const start = performance.now();
    const finish = (status, data) => {
      if (done) return;
      done = true;
      (helper ? helpers : requests).push({principal, route_class: kind, status, latency_ms: performance.now() - start});
      resolve({status, body: data});
    };
    const token = tokenOverride || users[principal]?.token;
    const req = https.request({hostname: capTarget.ip || capTarget.host, port: capTarget.port, servername: capTarget.host,
      path, method: body ? 'POST' : 'GET', agent, timeout: 10000,
      headers: {Authorization: 'Bearer ' + token, 'Content-Type': 'application/json', Host: capTarget.host,
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

async function createCoupon(code, maxUses) {
  const now = Date.now();
  const res = await call(-1, 'prepare_coupon', '/api/v1/seller/stores/' + storeSlug + '/discount-codes',
    {code, mode: 'percent', value: 10, startsAt: new Date(now - 3600e3).toISOString(), endsAt: new Date(now + 7200e3).toISOString(), maxUses}, true, seller.token);
  if (res.status !== 201 || !res.body?.id) throw Error('coupon creation failed: ' + res.status + ' ' + JSON.stringify(res.body)?.slice(0, 200));
  return res.body;
}

async function runVariant(name, code, maxUses, attemptCount) {
  const v = {maxUses, attempts: attemptCount, accepted: 0, coupon_exhausted: 0, silent_discount: 0, other: 0, failed_gates: []};
  summary.variants[name] = v;
  await createCoupon(code, maxUses);
  // Prepare one quote per attempt (helper calls, sequential — the burst
  // itself is the concurrent part). Products of cap-store-1 only:
  // product 100*(k+1) (same rule as hot-account); round-robin for the
  // 1000-attempt variant (100 distinct products on this store).
  const quotes = [];
  for (let principal = 0; principal < attemptCount; principal++) {
    const product = 100 * ((principal % 100) + 1);
    const items = [{productId: 'cap-product-' + product, variantId: uuid('cap-sku-' + ((product - 1) * 5 + 1)), quantity: 1}];
    const destination = {province: 'Tehran', city: 'Tehran', address: 'Synthetic coupon-burst address', postalCode: '1111111111'};
    const quote = await call(principal, 'prepare_quote', '/api/v1/shipping/quote', {items, destination}, true);
    if (quote.status !== 200 || !quote.body?.quoteId) throw Error(name + ': quote preparation failed for principal ' + principal);
    quotes.push({principal, items, destination, quoteId: quote.body.quoteId});
  }
  const started = performance.now();
  const mark = requests.length;
  const results = await Promise.all(quotes.map((q) =>
    call(q.principal, 'burst_order', '/api/v1/orders',
      {storeId: 'cap-store-1', items: q.items, shippingQuoteId: q.quoteId,
       customerInfo: {...q.destination, fullName: 'Synthetic capacity customer', phone: users[q.principal].mobile},
       paymentMethod: 'cash', discountCode: code, idempotencyKey: 'cap-coupon-' + name + '-' + crypto.randomUUID()})));
  v.elapsed_seconds = (performance.now() - started) / 1000;
  for (const r of results) {
    if (r.status === 201) {
      const discounted = r.body?.order?.couponDiscountAmount > 0 || r.body?.couponDiscountAmount > 0;
      if (discounted) v.accepted++;
      else v.silent_discount++; // 201 without a discount: the bug this profile exists to catch
    } else if (r.status === 409 && (r.body?.code === 'coupon_exhausted' || r.body?.error === 'coupon_exhausted')) {
      v.coupon_exhausted++;
    } else {
      v.other++;
    }
  }
  const times = requests.slice(mark).filter((r) => r.route_class === 'burst_order').map((r) => r.latency_ms).sort((a, b) => a - b);
  if (times.length) {
    v.p95_ms = times[Math.ceil(times.length * .95) - 1];
    v.p99_ms = times[Math.ceil(times.length * .99) - 1];
    if (v.p95_ms > 750 || v.p99_ms > 2000) v.failed_gates.push('burst_latency');
  }
  if (v.accepted !== maxUses) v.failed_gates.push('accepted_count');
  if (v.coupon_exhausted !== attemptCount - maxUses - v.other) v.failed_gates.push('exhausted_count');
  if (v.silent_discount !== 0) v.failed_gates.push('silent_discount');
  if (v.other) v.failed_gates.push('other_outcomes');
  summary.failed_gates.push(...v.failed_gates.map((g) => name + ':' + g));
}

try {
  await runVariant('maxUses1', 'FIN013BURST1', 1, 100);
  await runVariant('maxUses100', 'FIN013BURST100', 100, 1000);
} catch (error) {
  summary.failed_gates.push('preparation_or_execution');
  summary.failure = error.message;
} finally {
  summary.requests = requests.length;
  summary.helper_api_calls = helpers.length;
  agent.destroy();
  fs.writeFileSync(process.env.CAPACITY_SUMMARY_FILE || '/tmp/summary.json', JSON.stringify(summary, null, 2));
  process.exitCode = summary.failed_gates.length ? 1 : 0;
}
