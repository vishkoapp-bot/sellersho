// CAP-01 D1+D4: one env-overridable load target for every Node driver.
//
// The ONLY permitted target hostname is the disposable domain
// `capacity.example.test`. The guard below rejects any other hostname,
// any non-loopback connect IP, and any non-HTTPS/HTTP scheme choice
// outside the declared values, so no driver can be pointed at
// production (sellershow.ir, 46.249.98.190, or any real host) by
// accident or by a stray environment variable.
//
// Native runs (this VM cannot run Docker) connect to the loopback
// nginx TLS endpoint: CAPACITY_TARGET_IP=127.0.0.1 with
// CAPACITY_TARGET_PORT=8443, while SNI and the Host header stay
// `capacity.example.test`, so TLS hostname verification still applies.
// The Docker runner keeps the defaults (host resolves inside its
// isolated network, port 443, CA at /trust/root.crt).
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

export const CAPACITY_HOST = 'capacity.example.test';
const LOOPBACK = new Set(['127.0.0.1', '::1']);

export function resolveTarget(env = process.env) {
  const host = env.CAPACITY_TARGET_HOST || CAPACITY_HOST;
  const port = Number(env.CAPACITY_TARGET_PORT || 443);
  const protocol = env.CAPACITY_TARGET_PROTOCOL || 'https';
  const ip = env.CAPACITY_TARGET_IP || '';
  const caFile = env.CAPACITY_CA_FILE || '/trust/root.crt';
  const sessionsFile = env.CAPACITY_SESSIONS_FILE || '/sessions/users.json';
  const smsHost = env.CAPACITY_SMS_HOST || 'sms';
  const smsPort = Number(env.CAPACITY_SMS_PORT || 8081);
  if (host !== CAPACITY_HOST) {
    throw Error(`refused non-disposable load target host: ${host}`);
  }
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw Error(`refused load target port: ${env.CAPACITY_TARGET_PORT}`);
  }
  if (protocol !== 'https' && protocol !== 'http') {
    throw Error(`refused load target protocol: ${protocol}`);
  }
  if (ip && !LOOPBACK.has(ip)) {
    throw Error(`refused non-loopback load target IP: ${ip}`);
  }
  if (smsHost !== 'sms' && smsHost !== '127.0.0.1' && smsHost !== '::1') {
    throw Error(`refused non-disposable SMS double host: ${smsHost}`);
  }
  if (!Number.isInteger(smsPort) || smsPort < 1 || smsPort > 65535) {
    throw Error(`refused SMS double port: ${env.CAPACITY_SMS_PORT}`);
  }
  return {host, port, protocol, ip, caFile, sessionsFile, smsHost, smsPort};
}

// https.request options for the guarded target. `agent` is the caller's
// keep-alive agent (CA already loaded from target.caFile for https).
// SNI + Host header always carry the disposable domain, even when the
// TCP connection goes to the loopback IP.
export function targetRequestOptions(target, agent) {
  const opts = {
    hostname: target.ip || target.host,
    port: target.port,
    agent,
    headers: {Host: target.host + (target.port === 443 ? '' : ':' + target.port)},
  };
  if (target.protocol === 'https') opts.servername = target.host;
  return opts;
}

export function loadSessions(target) {
  return JSON.parse(fs.readFileSync(target.sessionsFile)).users;
}

export function loadCa(target) {
  return fs.readFileSync(target.caFile);
}

// Absolute path of load-tests/workload.json next to this module.
export function workloadPath() {
  return path.join(path.dirname(fileURLToPath(import.meta.url)), 'workload.json');
}
