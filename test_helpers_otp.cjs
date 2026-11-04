// Selersho E2E OTP helper — TEST/STAGING ONLY, never production.
// Replaces the old test_helpers.cjs getAuthToken(), which assumed the OTP
// send response returned a demoCode. It does not: handleSendOTP() in
// backend/cmd/server/auth_handlers.go returns only {success, mobile,
// expiresIn: 120} and discards the code. Guessing "12345" always fails.
//
// Two supported ways to obtain a code, in order:
//  1) TEST_OTP_CODE env var — a code the tester read from a real SMS on a
//     phone they own, used within the 120s TTL. Use this for a single
//     manual run.
//  2) STAGING_REDIS_CONTAINER env var — the test runner runs on the same
//     host as an isolated staging backend (APP_ENV=test, IPPANEL disabled,
//     its own Postgres/Redis, never production). After sending OTP, the
//     code is read from staging Redis (key otp:<mobile>) via docker exec.
//     STAGING_REDIS_PASSWORD must be supplied via env, never hardcoded.
// If neither is available, getOTPToken() fails loudly instead of guessing.
const http = require('http');
const { execSync } = require('child_process');

const BASE_URL = process.env.E2E_BASE_URL || 'http://localhost:8080';
const HOST = process.env.E2E_HOST || 'localhost';
const PORT = parseInt(process.env.E2E_PORT || '8080', 10);

function request(method, path, body = null, token = null, extraHeaders = {}) {
  return new Promise((resolve) => {
    const startTime = Date.now();
    const headers = { ...extraHeaders };
    let data = null;
    if (body !== null) {
      if (typeof body === 'string') data = body;
      else { data = JSON.stringify(body); headers['Content-Type'] = 'application/json'; }
      headers['Content-Length'] = Buffer.byteLength(data);
    }
    if (token) headers['Authorization'] = `Bearer ${token}`;
    const req = http.request({ hostname: HOST, port: PORT, path: encodeURI(path), method, headers, timeout: 6000 }, (res) => {
      let respData = '';
      res.on('data', (c) => (respData += c));
      res.on('end', () => {
        let parsed = null;
        try { parsed = JSON.parse(respData); } catch { parsed = respData; }
        resolve({ status: res.statusCode, body: parsed, rawBody: respData, latency: Date.now() - startTime });
      });
    });
    req.on('error', (err) => resolve({ status: 0, body: null, rawBody: err.message, latency: Date.now() - startTime, error: err.message }));
    if (data) req.write(data);
    req.end();
  });
}

function maskMobile(mobile) {
  if (!mobile || mobile.length < 7) return '****';
  return mobile.slice(0, 4) + '****' + mobile.slice(-3);
}

function readCodeFromStagingRedis(mobile) {
  const container = process.env.STAGING_REDIS_CONTAINER;
  const password = process.env.STAGING_REDIS_PASSWORD;
  if (!container || !password) return null;
  // Retry briefly: OTP is stored in Redis as soon as send succeeds, but
  // allow a few seconds for the staging backend to write it.
  for (let i = 0; i < 10; i++) {
    try {
      const out = execSync(
        `docker exec ${container} redis-cli -a "${password}" --no-auth-warning GET "otp:${mobile}" 2>/dev/null`,
        { encoding: 'utf8', timeout: 5000 }
      ).trim();
      if (out && out !== '(nil)' && /^\d{4}$/.test(out)) return out;
    } catch { /* retry */ }
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 500);
  }
  return null;
}

async function getOTPToken(mobile, role) {
  // SKIP_OTP_SEND=1: the caller already sent OTP (e.g. a staging wrapper
  // that fetched the code from staging Redis) and set TEST_OTP_CODE.
  // Skipping the second send avoids the 30s cooldown and, more
  // importantly, avoids overwriting the code with a new one.
  if (process.env.SKIP_OTP_SEND !== '1') {
    const sendRes = await request('POST', '/api/v1/auth/otp/send', { mobile });
    if (sendRes.status !== 200) {
      throw new Error(`OTP send failed for ${maskMobile(mobile)}: HTTP ${sendRes.status} ${JSON.stringify(sendRes.body)} (note: 30s resend cooldown per mobile)`);
    }
  }
  let code = process.env.TEST_OTP_CODE || null;
  if (!code) code = readCodeFromStagingRedis(mobile);
  if (!code) {
    throw new Error(
      `No OTP code available for ${maskMobile(mobile)}. ` +
      `Set TEST_OTP_CODE=<code from SMS> within 120s, or run against an isolated staging backend with STAGING_REDIS_CONTAINER + STAGING_REDIS_PASSWORD set. ` +
      `Never guess a code and never run this against production.`
    );
  }
  const verifyRes = await request('POST', '/api/v1/auth/otp/verify', { mobile, code, role: role || undefined });
  if (verifyRes.status !== 200 || !verifyRes.body?.token) {
    throw new Error(`OTP verify failed for ${maskMobile(mobile)}: HTTP ${verifyRes.status} ${JSON.stringify(verifyRes.body)} — if the user is new, the requested role must already be provisioned, otherwise verify returns 403.`);
  }
  return verifyRes.body;
}

module.exports = { request, getOTPToken, maskMobile, BASE_URL };
