// INV-12 Phase B workload — 2-minute 100 capture-attempts/s probes.
//
// *** DO NOT RUN — GATED ON OWNER DECISIONS (audit §7, Phase B item 5). ***
// This file is the workload definition + result tagging + artifact saving
// only. The run itself is blocked until ALL THREE owner inputs are answered:
//   (a) Logging — may the probe run before the REL-02 Q4 log fix lands, or
//       must it wait? (100 RPS x 120 s with per-query SQL success lines risks
//       ~24 GB/day-equivalent disk pressure on the target host; running with
//       logging suppressed measures a different system than prod.)
//   (b) Hardware — this VM saturates at ~35 captures/s under contention
//       (audit §3.1) and ~1 s p50 even for disjoint SKUs (§3.2). Where does
//       the probe run — a larger staging host, or deferred to CAP-02's
//       prod-shaped hardware? No number from this VM can be quoted as a
//       capacity result.
//   (c) Cancel admission — FIN-013's per-account admission/queueing for
//       CancelOrder is an open design decision (known 58-64/100 success rate
//       under the current 5 s lock budget). State the budget the probe is
//       judged against before running dispatch/cancel races at rate.
// The file refuses to run unless INV12_PROBE_APPROVED=1 is set in the
// environment (set it only after (a)-(c) are answered), and it refuses any
// non-loopback target through the CAP-01 target guard in target-config.mjs.
//
// Built on the CAP-01 harness (hot-sku.mjs + hot-sku-policy.mjs +
// target-config.mjs): the same 1,000-user session fixture, the same
// insufficient_stock 409 tagging (conflicts are counted separately from
// purchases and from failures), the same Shaparak simulator token -> callback
// flow, the same 750 ms p95 / 2000 ms p99 mutation SLOs, and the same
// duplicate-callback replay check for every paid order.
//
// Two probes, selected by INV12_PROBE (default: hotspot):
//   hotspot     — 2 minutes at 100 capture-attempts/s on ONE SKU with 5 units
//                 (CAP-01's focused profile, extended from a 100-contender
//                 burst to a sustained 120 s rate). Expected: 5 paid, the
//                 rest explicit 409 insufficient_stock conflicts.
//   distributed — 2 minutes at 100 capture-attempts/s across 100 SKUs with
//                 5 units each (round-robin). Expected: at most 500 paid,
//                 the rest explicit 409 conflicts.
// Target fixture required (same shape as CAP-01's): store `cap-store-100`,
// product `cap-hot-product` (hotspot) or `cap-dist-product-<0..99>`
// (distributed), each with one tracked variant and 5 sellable units, and the
// Shaparak simulator enabled. Preparation fails closed if the fixture is
// missing — a failed preparation is a harness failure, never a pass.
//
// Preparation: all orders are prepared (quote + order) BEFORE the measured
// window, so no preparation load pollutes the measured latencies. Payment
// holds expire after 15 minutes, so the run refuses to start if preparation
// took longer than 10 minutes. Each prepared order is consumed by exactly
// one capture attempt; if the ready queue ever empties mid-window the attempt
// is counted as `starved` (a harness failure, not a target failure).
//
// Usage (only after the owner decisions above are answered):
//   INV12_PROBE_APPROVED=1 INV12_PROBE=hotspot \
//     CAPACITY_TARGET_IP=127.0.0.1 CAPACITY_TARGET_PORT=<loopback nginx port> \
//     CAPACITY_SESSIONS_FILE=<sessions/users.json> CAPACITY_CA_FILE=<ca> \
//     node load-tests/inventory-100rps.js
// Summary JSON: $INV12_SUMMARY_FILE (default /tmp/inventory-100rps-summary.json).
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadSessions, loadCa} from './target-config.mjs';

// --- Gate: owner decisions (a), (b), (c) above. No silent runs. ---
if (process.env.INV12_PROBE_APPROVED !== '1') {
  console.error(
    'INV-12 probe REFUSED: owner decisions (a) logging, (b) hardware, (c) cancel admission are unanswered.\n' +
    'See the header of load-tests/inventory-100rps.js and audit §7 Phase B item 5.\n' +
    'Set INV12_PROBE_APPROVED=1 only after all three are answered.',
  );
  process.exit(2);
}

const PROBE = process.env.INV12_PROBE || 'hotspot';
if (PROBE !== 'hotspot' && PROBE !== 'distributed') {
  throw Error(`unknown INV12_PROBE: ${PROBE} (want hotspot|distributed)`);
}
const RATE_PER_S = 100;
const DURATION_S = 120;
const TOTAL_ATTEMPTS = RATE_PER_S * DURATION_S; // 12,000
const UNITS_PER_SKU = 5;
const MAX_PREPARE_MINUTES = 10;

const capTarget = resolveTarget();
const users = loadSessions(capTarget);
if (users.length !== 1000 || new Set(users.map((u) => u.id)).size !== 1000) {
  throw Error('independent full session fixture required');
}
const agent = new https.Agent({
  keepAlive: true,
  maxSockets: 1000,
  ca: loadCa(capTarget),
  rejectUnauthorized: true,
});

const requests = [];
const helpers = [];
const summary = {
  certified: false,
  scope: `INV-12 ${PROBE} 100 capture-attempts/s x 120s`,
  probe: PROBE,
  rate_per_s: RATE_PER_S,
  duration_s: DURATION_S,
  attempts: TOTAL_ATTEMPTS,
  paid: 0,
  conflicts: 0, // explicit 409 insufficient_stock — never counted as purchases
  wrongAmount: 0,
  failures: 0,
  starved: 0, // harness ran out of prepared orders mid-window
  duplicateCallbacks: 0,
  duplicateCallbacksExpected: 0,
  failed_gates: [],
  requests_raw: requests,
  helper_requests_raw: helpers,
};

function call(principal, kind, path, body, helper = false) {
  return new Promise((resolve) => {
    let done = false;
    const start = performance.now();
    const finish = (status, data) => {
      if (done) return;
      done = true;
      (helper ? helpers : requests).push({
        principal,
        route_class: kind,
        status,
        latency_ms: performance.now() - start,
      });
      resolve({status, body: data});
    };
    const req = https.request(
      {
        hostname: capTarget.ip || capTarget.host,
        port: capTarget.port,
        servername: capTarget.host,
        path,
        method: body ? 'POST' : 'GET',
        agent,
        timeout: 10000,
        headers: {
          Authorization: 'Bearer ' + users[principal].token,
          'Content-Type': 'application/json',
          Host: capTarget.host,
          ...(body?.idempotencyKey ? {'Idempotency-Key': body.idempotencyKey} : {}),
        },
      },
      (res) => {
        let raw = '';
        res.on('data', (b) => {
          if (raw.length < 262144) raw += b;
        });
        res.on('end', () => {
          let data;
          try {
            data = JSON.parse(raw);
          } catch {}
          finish(res.statusCode, data);
        });
        res.on('error', () => finish(0));
      },
    );
    req.on('timeout', () => req.destroy());
    req.on('error', () => finish(0));
    req.end(body ? JSON.stringify(body) : undefined);
  });
}

function productFor(attempt) {
  if (PROBE === 'hotspot') return {productId: 'cap-hot-product', index: 0};
  const index = attempt % 100;
  return {productId: `cap-dist-product-${index}`, index};
}

function variantFor(productId) {
  const h = crypto.createHash('md5').update(`inv12-${productId}`).digest('hex');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

const destination = {
  province: 'Tehran',
  city: 'Tehran',
  address: 'Synthetic INV-12 address',
  postalCode: '1111111111',
};

async function prepareOrder(principal, attempt) {
  const {productId} = productFor(attempt);
  const items = [{productId, variantId: variantFor(productId), quantity: 1}];
  const quote = await call(
    principal,
    'prepare_quote',
    '/api/v1/shipping/quote',
    {items, destination},
    true,
  );
  if (quote.status !== 200 || !quote.body?.quoteId) {
    throw Error(`valid quote preparation failed for ${productId}: ${quote.status}`);
  }
  const order = await call(
    principal,
    'prepare_order',
    '/api/v1/orders',
    {
      storeId: 'cap-store-100',
      items,
      shippingQuoteId: quote.body.quoteId,
      customerInfo: {
        ...destination,
        fullName: 'Synthetic INV-12 customer',
        phone: users[principal].mobile,
      },
      paymentMethod: 'cash',
      idempotencyKey: `inv12-${PROBE}-` + crypto.randomUUID(),
    },
    true,
  );
  if (
    order.status !== 201 ||
    !order.body?.id ||
    order.body.storeId !== 'cap-store-100' ||
    !Number.isSafeInteger(order.body.payableAmount)
  ) {
    throw Error(`valid order preparation failed for ${productId}: ${order.status}`);
  }
  return {principal, ...order.body};
}

// One capture attempt: token initiation, then the simulator callback on success.
// A 409 with code insufficient_stock is a conflict (tagged, not a failure).
async function attemptCapture(order) {
  const token = await call(order.principal, 'initiate', '/api/v1/payments/shaparak/token', {
    orderId: order.id,
    orderNumber: order.orderNumber,
    amount: order.payableAmount,
  });
  if (token.status === 409 && token.body?.code === 'insufficient_stock') {
    summary.conflicts++;
    return;
  }
  if (token.status !== 200 || !token.body?.token) {
    summary.failures++;
    return;
  }
  if (token.body.amount !== order.payableAmount) {
    summary.wrongAmount++;
    return;
  }
  const paid = await call(order.principal, 'callback', '/api/v1/payments/shaparak/callback', {
    token: token.body.token,
    orderNumber: order.orderNumber,
    amount: order.payableAmount,
  });
  if (paid.status !== 200 || paid.body?.status !== 'paid' || paid.body.orderNumber !== order.orderNumber) {
    summary.failures++;
    return;
  }
  summary.paid++;
  summary.duplicateCallbacksExpected += 2;
  const duplicates = await Promise.all([
    call(order.principal, 'duplicate_callback', '/api/v1/payments/shaparak/callback', {
      token: token.body.token,
      orderNumber: order.orderNumber,
      amount: order.payableAmount,
    }),
    call(order.principal, 'duplicate_callback', '/api/v1/payments/shaparak/callback', {
      token: token.body.token,
      orderNumber: order.orderNumber,
      amount: order.payableAmount,
    }),
  ]);
  for (const replay of duplicates) {
    if (
      replay.status === 200 &&
      replay.body?.status === 'paid' &&
      replay.body.orderNumber === order.orderNumber
    ) {
      summary.duplicateCallbacks++;
    } else {
      summary.failures++;
    }
  }
}

function percentile(sorted, p) {
  if (!sorted.length) return null;
  return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * p) - 1)];
}

function evaluate(summary) {
  const failed = [];
  const expectedPaidMax = PROBE === 'hotspot' ? UNITS_PER_SKU : 100 * UNITS_PER_SKU;
  if (summary.paid < 0 || summary.paid > expectedPaidMax) {
    failed.push('paid_out_of_range');
  }
  if (summary.paid + summary.conflicts !== summary.attempts) {
    failed.push('attempts_not_fully_accounted');
  }
  if (summary.wrongAmount !== 0 || summary.failures !== 0 || summary.starved !== 0) {
    failed.push('amount_or_response_or_starvation_assertion');
  }
  if (summary.duplicateCallbacks !== summary.duplicateCallbacksExpected) {
    failed.push('duplicate_callback_replay');
  }
  if (summary.achieved_rate_per_s < RATE_PER_S * 0.95) {
    failed.push('sustained_rate_below_target');
  }
  return failed;
}

try {
  // Phase 1 — prepare every order before the measured window.
  const prepareStarted = performance.now();
  const ready = [];
  const PREPARE_CONCURRENCY = 100;
  let nextAttempt = 0;
  async function feeder() {
    while (nextAttempt < TOTAL_ATTEMPTS) {
      const batch = [];
      while (batch.length < PREPARE_CONCURRENCY && nextAttempt < TOTAL_ATTEMPTS) {
        const attempt = nextAttempt++;
        batch.push(
          prepareOrder(attempt % 1000, attempt).then((order) => ready.push(order)),
        );
      }
      await Promise.all(batch);
    }
  }
  await feeder();
  const prepareMinutes = (performance.now() - prepareStarted) / 60000;
  summary.prepare_minutes = prepareMinutes;
  summary.prepared_orders = ready.length;
  if (ready.length !== TOTAL_ATTEMPTS) {
    throw Error(`prepared ${ready.length} orders, want ${TOTAL_ATTEMPTS}`);
  }
  if (prepareMinutes > MAX_PREPARE_MINUTES) {
    throw Error(
      `preparation took ${prepareMinutes.toFixed(1)} min; payment holds expire after 15 min — refusing a stale window`,
    );
  }

  // Phase 2 — drift-corrected paced window: attempt i at start + i * 10 ms.
  const started = performance.now();
  let launched = 0;
  const inFlight = new Set();
  await new Promise((resolve) => {
    const tick = () => {
      const now = performance.now();
      while (launched < TOTAL_ATTEMPTS && now - started >= launched * (1000 / RATE_PER_S)) {
        const order = ready.shift();
        if (!order) {
          summary.starved++;
          launched++;
          continue;
        }
        launched++;
        const p = attemptCapture(order).finally(() => {
          inFlight.delete(p);
          if (launched >= TOTAL_ATTEMPTS && inFlight.size === 0) resolve();
        });
        inFlight.add(p);
      }
      if (launched < TOTAL_ATTEMPTS) {
        setTimeout(tick, 1);
      } else if (inFlight.size === 0) {
        resolve();
      }
    };
    tick();
  });
  const elapsedS = (performance.now() - started) / 1000;
  summary.elapsed_seconds = elapsedS;
  summary.achieved_rate_per_s = TOTAL_ATTEMPTS / elapsedS;
  summary.distinct_principals = new Set(requests.map((r) => r.principal)).size;

  summary.route_metrics = {};
  for (const kind of ['initiate', 'callback', 'duplicate_callback']) {
    const times = requests
      .filter((r) => r.route_class === kind)
      .map((r) => r.latency_ms)
      .sort((a, b) => a - b);
    summary.route_metrics[kind] = {
      requests: times.length,
      p50_ms: percentile(times, 0.5),
      p95_ms: percentile(times, 0.95),
      p99_ms: percentile(times, 0.99),
    };
    if (!times.length || summary.route_metrics[kind].p95_ms > 750 || summary.route_metrics[kind].p99_ms > 2000) {
      summary.failed_gates.push(`${kind}_latency`);
    }
  }
  summary.failed_gates.push(...evaluate(summary));
} catch (error) {
  summary.failed_gates.push('preparation_or_execution');
  summary.failure = error.message;
} finally {
  summary.requests = requests.length;
  summary.helper_api_calls = helpers.length;
  agent.destroy();
  fs.writeFileSync(
    process.env.INV12_SUMMARY_FILE || '/tmp/inventory-100rps-summary.json',
    JSON.stringify(summary, null, 2),
  );
  process.exitCode = summary.failed_gates.length ? 1 : 0;
}
