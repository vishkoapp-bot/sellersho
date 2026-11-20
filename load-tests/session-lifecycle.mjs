// Session-lifecycle profile (CAP-01): expiry / re-login /
// concurrent-device sessions on a 10-user cohort.
//
// Settled facts this driver records (not gates it invents):
// - No /auth/refresh endpoint exists in the app (verified by route
//   search); tokens are 3h JWTs, so true expiry cannot be exercised
//   inside a short run. Expiry therefore stays OPEN with the reason
//   recorded here, and re-login via the real OTP flow is covered.
// - POST /auth/logout returns success but does not revoke the token
//   server-side (customer_handlers.go handleT01Logout is a no-op);
//   the driver measures that instead of assuming revocation.
// - Concurrent-device: the same token used from two agents at once
//   must return the same identity, never another user's data.
// Certified:false by design; failures are findings, not crashes.
import https from 'node:https';
import http from 'node:http';
import fs from 'node:fs';
import {performance} from 'node:perf_hooks';
import {resolveTarget, loadSessions, loadCa} from './target-config.mjs';

const capTarget = resolveTarget();
const users = loadSessions(capTarget);
if (users.length !== 1000) throw Error('1000 independent sessions required');
const COHORT = 10;
const agentA = new https.Agent({keepAlive: true, maxSockets: 20, ca: loadCa(capTarget), rejectUnauthorized: true});
const agentB = new https.Agent({keepAlive: true, maxSockets: 20, ca: loadCa(capTarget), rejectUnauthorized: true});
const requests = [];
const summary = {
  certified: false,
  scope: 'LOCAL session lifecycle: 10-user cohort, concurrent-device reads, OTP re-login, logout behavior',
  cohort: COHORT,
  refresh_endpoint: 'absent (no /auth/refresh route in backend/cmd/server/main.go)',
  expiry_test: 'OPEN: 3h JWT lifetime exceeds any short run; no refresh endpoint to exercise; re-login path covered instead',
  concurrent_device_ok: 0, re_login_ok: 0, old_token_after_logout_still_valid: 0,
  failed_gates: [], requests_raw: requests,
};

function call(user, path, body, agent, kind) {
  return new Promise((resolve) => {
    let done = false;
    const start = performance.now();
    const finish = (status, data) => {
      if (done) return;
      done = true;
      requests.push({principal: users.indexOf(user), route_class: kind, status, latency_ms: performance.now() - start});
      resolve({status, body: data});
    };
    const req = https.request({hostname: capTarget.ip || capTarget.host, port: capTarget.port, servername: capTarget.host,
      path, method: body ? 'POST' : 'GET', agent, timeout: 5000,
      headers: body ? {'Content-Type': 'application/json', Host: capTarget.host, Authorization: 'Bearer ' + user.token}
                    : {Authorization: 'Bearer ' + user.token, Host: capTarget.host}}, (res) => {
      let text = '';
      res.on('data', (b) => { if (text.length < 65536) text += b; });
      res.on('end', () => { let data; try { data = JSON.parse(text); } catch {} finish(res.statusCode, data); });
    });
    req.on('timeout', () => req.destroy());
    req.on('error', () => finish(0));
    req.end(body ? JSON.stringify(body) : undefined);
  });
}

function readOTP(user) {
  return new Promise((resolve) => {
    let done = false;
    const finish = (value) => { if (done) return; done = true; resolve(value); };
    const req = http.request({hostname: capTarget.smsHost, port: capTarget.smsPort, path: '/fixture/otp', method: 'POST',
      headers: {Authorization: process.env.CAPACITY_SMS_KEY, 'Content-Type': 'application/json'}, timeout: 5000}, (res) => {
      let text = '';
      res.on('data', (b) => { if (text.length < 8192) text += b; });
      res.on('end', () => { try { finish(res.statusCode === 200 ? JSON.parse(text).code : null); } catch { finish(null); } });
    });
    req.on('timeout', () => req.destroy());
    req.on('error', () => finish(null));
    req.end(JSON.stringify({recipient: '98' + user.mobile.slice(1)}));
  });
}

function identityOk(body, user) {
  return body?.userId === user.id || body?.id === user.id || body?.user?.id === user.id || body?.data?.id === user.id;
}

try {
  for (let i = 0; i < COHORT; i++) {
    const user = users[i];
    // Concurrent-device: same token, two agents, same instant.
    const [a, b] = await Promise.all([
      call(user, '/api/v1/auth/me', null, agentA, 'me_device_a'),
      call(user, '/api/v1/auth/me', null, agentB, 'me_device_b'),
    ]);
    if (a.status === 200 && b.status === 200 && identityOk(a.body, user) && identityOk(b.body, user)) {
      summary.concurrent_device_ok++;
    } else {
      summary.failed_gates.push('concurrent_device_identity_' + i);
    }
    // Re-login via the real OTP flow (SMS double, no real SMS).
    const sent = await call(user, '/api/v1/auth/otp/send', {mobile: user.mobile}, agentA, 'otp_send');
    const code = sent.status === 200 ? await readOTP(user) : null;
    const verified = code ? await call(user, '/api/v1/auth/otp/verify', {mobile: user.mobile, code}, agentA, 'otp_verify') : {status: 0};
    if (verified.status === 200 && verified.body?.token && identityOk(verified.body?.user || verified.body, user)) {
      summary.re_login_ok++;
      const newToken = verified.body.token;
      const fresh = {...user, token: newToken};
      const meNew = await call(fresh, '/api/v1/auth/me', null, agentB, 'me_new_token');
      if (meNew.status !== 200 || !identityOk(meNew.body, user)) summary.failed_gates.push('relogin_token_unusable_' + i);
    } else {
      summary.failed_gates.push('relogin_failed_' + i);
    }
    // Logout with the ORIGINAL token, then probe whether it still works.
    const logout = await call(user, '/api/v1/auth/logout', {}, agentA, 'logout');
    const after = await call(user, '/api/v1/auth/me', null, agentA, 'me_after_logout');
    if (logout.status === 200 && after.status === 200) summary.old_token_after_logout_still_valid++;
  }
  if (summary.concurrent_device_ok !== COHORT) summary.failed_gates.push('concurrent_device');
  if (summary.re_login_ok !== COHORT) summary.failed_gates.push('re_login');
  // Logout not revoking the token is a recorded finding (AUTH-01
  // territory), not a driver error: the gate fails honestly so the
  // missing revocation is visible in the verdict, never hidden.
  if (summary.old_token_after_logout_still_valid) summary.failed_gates.push('logout_does_not_revoke_token');
} catch (error) {
  summary.failed_gates.push('execution');
  summary.failure = error.message;
} finally {
  summary.requests = requests.length;
  agentA.destroy();
  agentB.destroy();
  fs.writeFileSync('/tmp/summary.json', JSON.stringify(summary, null, 2));
  process.exitCode = summary.failed_gates.length ? 1 : 0;
}
