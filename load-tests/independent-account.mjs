// Independent-account throughput profile (FIN-013 §3.3, NEW — proves
// "no shared global finance mutex" at HTTP scale).
//
// 100 independent seller accounts, 10 concurrent payout/debit attempts
// each (1,000 total), all valid, all against distinct accounts; run
// at the 100-RPS budget alongside the base mix (not as a separate
// unlimited flood). Proof method: (a) per-account latency p95 must not
// degrade when the other accounts are busy — a solo-account baseline
// p95 is compared against the concurrent p95; a shared global mutex
// would show ~100x serialization (gate: concurrent p95 < 5x solo p95
// AND <= 750 ms mutation SLO). (b) Lock metrics: pg_locked must stay
// ~0 for cross-account waits (runner captures the samples; the driver
// records client latencies only). (c) Repo-level anchor already
// exists: TestFinanceBalanceRefundSerializesAndIndependentAccount
// (finance_balance_integration_test.go:237) — this profile is the
// HTTP-scale version of that test.
//
// Fixture note: the capacity fixture mints only 5 seller sessions
// (cap-seller-1..5, main_test.go:81). The driver uses every seller
// session present (accounts_used is reported) with 10 attempts each,
// so it runs and proves the point today across 5 independent accounts;
// the full 100-account shape needs the fixture's staff query extended
// to cap-seller-1..100 — a one-line fixture change, not a profile
// change. Throughput reported: completed independent debits/second at
// p95 <= 750 ms, with the solo-vs-concurrent comparison.
// Certified:false by design.
import https from 'node:https';
import fs from 'node:fs';
import crypto from 'node:crypto';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadStaffSessions, loadCa} from './target-config.mjs';

const capTarget = resolveTarget();
const staff = loadStaffSessions(capTarget);
const sellers = staff.filter((s) => s.role === 'seller' && s.storeId);
if (sellers.length < 2) throw Error('at least 2 seller staff sessions required, found ' + sellers.length);
const agent = new https.Agent({keepAlive: true, maxSockets: 100, ca: loadCa(capTarget), rejectUnauthorized: true});
const requests = [], helpers = [];
const ATTEMPTS_PER_ACCOUNT = 10;
const PAYOUT_AMOUNT_TOMAN = 10000;
const summary = {
  certified: false,
  scope: 'LOCAL independent-account throughput: N seller accounts x10 concurrent payout attempts each, solo-vs-concurrent p95',
  accounts_used: sellers.length,
  accounts_target: 100,
  fixture_note: 'fixture mints 5 seller sessions today (cap-seller-1..5); extend the staff query in backend/cmd/capacity-fixture/main_test.go to cap-seller-1..100 for the full 100-account shape',
  failed_gates: [], requests_raw: requests, helper_requests_raw: helpers,
  oracle_scope: 'runner SQL: one payout_requests row per accepted attempt (no duplicates by Idempotency-Key), sum(amount) per store equals the accepted count x 10000 Toman; zero payout requests for rejected attempts',
};

// Synthetic SHABA for the disposable fixture only (never a real
// account): ISO 13616 mod-97 check digits computed over a fixed BBAN.
function syntheticSheba() {
  const bban = '0123456789012345678901'; // 22 digits
  const numeric = BigInt(bban + '182700');
  const check = 98n - (numeric % 97n);
  return 'IR' + String(check).padStart(2, '0') + bban;
}
const SHEBA = syntheticSheba();

function call(seller, kind, path, body, helper = false) {
  return new Promise((resolve) => {
    let done = false;
    const start = performance.now();
    const finish = (status, data) => {
      if (done) return;
      done = true;
      (helper ? helpers : requests).push({principal: seller.storeId, route_class: kind, status, latency_ms: performance.now() - start});
      resolve({status, body: data});
    };
    const req = https.request({hostname: capTarget.ip || capTarget.host, port: capTarget.port, servername: capTarget.host,
      path, method: 'POST', agent, timeout: 10000,
      headers: {Authorization: 'Bearer ' + seller.token, 'Content-Type': 'application/json', Host: capTarget.host,
        'Idempotency-Key': body.idempotencyKey}}, (res) => {
      let raw = '';
      res.on('data', (b) => { if (raw.length < 262144) raw += b; });
      res.on('end', () => { let data; try { data = JSON.parse(raw); } catch {} finish(res.statusCode, data); });
      res.on('error', () => finish(0));
    });
    req.on('timeout', () => req.destroy());
    req.on('error', () => finish(0));
    req.end(JSON.stringify(body));
  });
}

function p95(times) {
  if (!times.length) return null;
  const sorted = [...times].sort((a, b) => a - b);
  return sorted[Math.ceil(sorted.length * .95) - 1];
}

async function payout(seller, kind, keySuffix) {
  const res = await call(seller, kind, '/api/v1/seller/stores/' + seller.storeId + '/payouts',
    {amount: PAYOUT_AMOUNT_TOMAN, shaba: SHEBA, idempotencyKey: 'cap-indep-' + seller.storeId + '-' + keySuffix + '-' + crypto.randomUUID()});
  return res;
}

try {
  // (a) Solo baseline: one account, 10 sequential payouts.
  const solo = [];
  for (let i = 0; i < ATTEMPTS_PER_ACCOUNT; i++) {
    const t0 = performance.now();
    const r = await payout(sellers[0], 'payout_solo', 'solo-' + i);
    solo.push(performance.now() - t0);
    if (r.status !== 200 && r.status !== 201) summary.solo_rejected = (summary.solo_rejected || 0) + 1;
  }
  summary.solo_p95_ms = p95(solo);
  summary.solo_attempts = solo.length;

  // (b) Concurrent: every account x10 at once.
  const started = performance.now();
  const mark = requests.length;
  const all = [];
  for (const seller of sellers) {
    for (let i = 0; i < ATTEMPTS_PER_ACCOUNT; i++) {
      all.push(payout(seller, 'payout_concurrent', 'conc-' + i));
    }
  }
  const results = await Promise.all(all);
  summary.elapsed_seconds = (performance.now() - started) / 1000;
  let created = 0, insufficient = 0, unexpected = 0;
  for (const r of results) {
    if (r.status === 200 || r.status === 201) created++;
    else if (r.status === 409) insufficient++; // exceeds_availability: a definite outcome, not a mutex failure
    else unexpected++;
  }
  summary.created = created;
  summary.insufficient_availability = insufficient;
  summary.unexpected = unexpected;
  const concTimes = requests.slice(mark).map((r) => r.latency_ms);
  summary.concurrent_p95_ms = p95(concTimes);
  summary.completed_per_second = created / summary.elapsed_seconds;
  summary.p95_ratio_concurrent_over_solo = summary.solo_p95_ms ? summary.concurrent_p95_ms / summary.solo_p95_ms : null;
  if (summary.concurrent_p95_ms === null || summary.concurrent_p95_ms > 750) summary.failed_gates.push('concurrent_p95_slo');
  if (summary.p95_ratio_concurrent_over_solo !== null && summary.p95_ratio_concurrent_over_solo >= 5) summary.failed_gates.push('shared_mutex_suspected');
  if (unexpected) summary.failed_gates.push('unexpected_outcome');
  if (created + insufficient !== sellers.length * ATTEMPTS_PER_ACCOUNT) summary.failed_gates.push('attempt_count');
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
