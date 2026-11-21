// Boundary kill matrix driver (CAP-02 W-3.1) — two-phase, runner-orchestrated.
//
// Boundaries (each: setup -> backend SIGKILL at the boundary -> restart
// -> replay -> exactly-one-effect asserted via the W-2.1 gate):
//   b1 authorize->capture: token issued (authorize done); backend killed
//      BEFORE the callback is sent; replay sends the callback, which must
//      succeed exactly once.
//   b2 capture->response: callback sent while the payment double verifies
//      slowly (CAPACITY_PAYMENT_LATENCIES_MS=5000); backend killed ~1s
//      into verification, so the first callback's outcome is UNKNOWN
//      (never returned); replay re-sends the same callback and must
//      produce exactly one paid order / one capture / one ledger set.
//   b3 response->commit: callback sent with fast verify; backend killed
//      immediately after the request is flushed, before the response is
//      read, so the response is lost; replay re-sends the same callback
//      and must produce exactly one effect. (The driver cannot distinguish
//      "killed before commit" from "killed after commit before the
//      response flush" — both are covered by the same exactly-one-effect
//      assertion; the distinction is recorded here, not hidden.)
//   b4 hold-expiry: order created (hold starts, 15-min deadline);
//      backend killed during the expiry worker's batch after the deadline
//      passes (the runner installs a slow-expiry trigger so the batch is
//      interruptible — same technique as the repo's
//      TestJobsCrashRestartAndMidBatchCancellation); after restart the
//      order must be expired exactly once, no duplicate effects.
//      NOTE (queue-jump): the runner sets the b4 order's
//      payment_expires_at to '2000-01-01' so it is first in the expiry
//      queue — the fixture leaves a ~7200-order expired backlog and the
//      job clears ~21 candidates per 5 s batch, so a real 15-min hold
//      would never be reached in the profile window. The hold DURATION
//      is not what b4 tests; see capacity-evidence.txt §5.
//
// Phases (CAPACITY_BOUNDARY_PHASE):
//   setup    — quote + order + token; writes the state file; exits.
//   callback — reads the state file, sends the callback, awaits it,
//      records the outcome ('paid' | 'lost' | other); used for b2/b3
//      while the runner kills mid-flight.
//   replay   — reads the state file, re-sends the callback with the same
//      token (b1/b2/b3) or verifies the order expired (b4); asserts the
//      order's final state via the own-orders list.
// The runner (not this driver) performs the kill/restart and runs the
// W-2.1 gate (scripts/cap02_reconcile.py) per boundary, plus the
// negative control (a second payment receipt planted for the boundary
// order -> gate FAILS -> restore -> gate PASSES). The literal "duplicate
// paid payment_transactions row" plant is impossible by construction —
// the partial unique index payment_transactions_one_active_per_order
// rejects it — so the receipt plant is the faithful double-effect.
// Certified:false by design.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadSessions, loadCa} from './target-config.mjs';

const BOUNDARY = process.env.CAPACITY_BOUNDARY || '';
const PHASE = process.env.CAPACITY_BOUNDARY_PHASE || '';
const STATE_FILE = process.env.CAPACITY_BOUNDARY_STATE_FILE || '';
const PRODUCTS = {b1: 100, b2: 200, b3: 300, b4: 400};
if (!PRODUCTS[BOUNDARY]) throw Error('CAPACITY_BOUNDARY must be b1|b2|b3|b4, got ' + BOUNDARY);
if (!['setup', 'callback', 'replay'].includes(PHASE)) throw Error('CAPACITY_BOUNDARY_PHASE must be setup|callback|replay, got ' + PHASE);
if (!STATE_FILE) throw Error('CAPACITY_BOUNDARY_STATE_FILE is required');

const capTarget = resolveTarget();
const users = loadSessions(capTarget);
if (users.length !== 1000 || new Set(users.map((u) => u.id)).size !== 1000) {
  throw Error('1000 independent sessions required');
}
const agent = new https.Agent({keepAlive: true, maxSockets: 20, ca: loadCa(capTarget), rejectUnauthorized: true});
const requests = [], helpers = [];
const summary = {
  certified: false,
  scope: 'LOCAL fault-boundary ' + BOUNDARY + ' phase ' + PHASE + ': kill at payment boundary, replay, exactly-one-effect',
  boundary: BOUNDARY, phase: PHASE,
  failed_gates: [], requests_raw: requests, helper_requests_raw: helpers,
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
      path, method: body ? 'POST' : 'GET', agent, timeout: 15000,
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

const product = PRODUCTS[BOUNDARY];
const variant = uuid('cap-sku-' + ((product - 1) * 5 + 1));
const destination = {province: 'Tehran', city: 'Tehran', address: 'Synthetic boundary address', postalCode: '1111111111'};
// Distinct principals per boundary so the four profiles never share an order.
const principal = {b1: 10, b2: 11, b3: 12, b4: 13}[BOUNDARY];

function readState() {
  return JSON.parse(fs.readFileSync(STATE_FILE, 'utf8'));
}
function writeState(state) {
  fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
}

try {
  if (PHASE === 'setup') {
    const items = [{productId: 'cap-product-' + product, variantId: variant, quantity: 1}];
    const quote = await call(principal, 'prepare_quote', '/api/v1/shipping/quote', {items, destination}, true);
    if (quote.status !== 200 || !quote.body?.quoteId) throw Error('quote preparation failed: ' + quote.status);
    const order = await call(principal, 'prepare_order', '/api/v1/orders',
      {storeId: 'cap-store-1', items, shippingQuoteId: quote.body.quoteId,
       customerInfo: {...destination, fullName: 'Synthetic capacity customer', phone: users[principal].mobile},
       paymentMethod: 'cash', idempotencyKey: 'cap-boundary-' + BOUNDARY + '-' + crypto.randomUUID()}, true);
    if (order.status !== 201 || !order.body?.id || !Number.isSafeInteger(order.body.payableAmount)) {
      throw Error('order preparation failed: ' + order.status);
    }
    const token = await call(principal, 'initiate', '/api/v1/payments/shaparak/token',
      {orderId: order.body.id, orderNumber: order.body.orderNumber, amount: order.body.payableAmount});
    if (token.status === 409 && token.body?.code === 'insufficient_stock') {
      throw Error('boundary product out of stock (fixture shape changed)');
    }
    if (token.status !== 200 || !token.body?.token) throw Error('token issuance failed: ' + token.status);
    const state = {boundary: BOUNDARY, principal, product, storeId: 'cap-store-1',
      orderId: order.body.id, orderNumber: order.body.orderNumber,
      amount: order.body.payableAmount, token: token.body.token,
      // created_at is the hold start: the create-order response carries
      // no paymentExpiresAt (the domain Order struct has no such field),
      // so the runner computes the 15-min hold deadline as
      // created_at + 15 min (the hold maximum is hard-coded in
      // payment_handlers.go:382 / redis_recovery.go:77).
      created_at: new Date().toISOString(),
      paymentExpiresAt: order.body.paymentExpiresAt || null};
    writeState(state);
    summary.order_id = state.orderId;
    summary.token_issued = true;
  } else if (PHASE === 'callback') {
    const state = readState();
    // Write the marker BEFORE sending so the runner can time the kill.
    state.callback_started_at = new Date().toISOString();
    writeState(state);
    // The Docker runner polls the container logs for this marker (it
    // cannot read the state file mid-flight); the native runner uses
    // the state file and ignores stdout.
    console.log('CAPACITY_CALLBACK_STARTED');
    const paid = await call(state.principal, 'callback', '/api/v1/payments/shaparak/callback',
      {token: state.token, orderNumber: state.orderNumber, amount: state.amount});
    // A 5xx during the kill window is indistinguishable from a dropped
    // connection for exactly-once purposes: nginx returns 502 when the
    // upstream dies, and a backend killed mid-response can surface as a
    // 500. Either way the callback's effect is UNKNOWN — the replay +
    // the W-2.1 gate decide. The raw status is preserved in
    // callback_status for the record; only a definite 4xx/200 is not lost.
    state.callback_status = paid.status;
    state.callback_outcome = paid.status === 200 && paid.body?.status === 'paid' ? 'paid'
      : paid.status === 0 || paid.status >= 500 ? 'lost' : 'other:' + paid.status;
    state.callback_body = paid.body || null;
    writeState(state);
    summary.callback_outcome = state.callback_outcome;
    // A lost callback is the EXPECTED outcome for b2/b3 (the runner kills
    // mid-flight); a paid outcome here means the kill landed too late and
    // the boundary was not actually exercised — the runner records that.
    if (state.callback_outcome !== 'lost') summary.failed_gates.push('callback_not_lost');
  } else if (PHASE === 'replay') {
    const state = readState();
    if (BOUNDARY === 'b4') {
      const own = await call(state.principal, 'post_owner_read', '/api/v1/orders', undefined, true);
      // GET /api/v1/orders returns {orders:[...], nextCursor, hasMore,
      // limit} — NOT a bare array (order_handlers.go:1136).
      const orders = Array.isArray(own.body) ? own.body : own.body?.orders;
      if (own.status !== 200 || !Array.isArray(orders)) throw Error('own-orders read failed: ' + own.status);
      const mine = orders.find((o) => o.id === state.orderId);
      summary.order_status = mine?.status || null;
      summary.order_payment_status = mine?.paymentStatus || null;
      if (!mine || mine.status !== 'cancelled') summary.failed_gates.push('order_not_expired');
    } else {
      const paid = await call(state.principal, 'callback_replay', '/api/v1/payments/shaparak/callback',
        {token: state.token, orderNumber: state.orderNumber, amount: state.amount});
      summary.replay_status = paid.status;
      if (paid.status !== 200 || paid.body?.status !== 'paid' || paid.body.orderNumber !== state.orderNumber) {
        summary.failed_gates.push('replay_not_paid_once');
      } else {
        summary.replay_paid = true;
      }
      const own = await call(state.principal, 'post_owner_read', '/api/v1/orders', undefined, true);
      const orders = Array.isArray(own.body) ? own.body : own.body?.orders;
      if (own.status !== 200 || !Array.isArray(orders)) throw Error('own-orders read failed: ' + own.status);
      const mine = orders.filter((o) => o.id === state.orderId);
      summary.order_rows_visible = mine.length;
      if (mine.length !== 1 || mine[0].paymentStatus !== 'paid' || mine[0].payableAmount !== state.amount) {
        summary.failed_gates.push('order_not_exactly_once_paid');
      }
    }
  }
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
