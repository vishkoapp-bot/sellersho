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
// CAPACITY_TARGET_PORT=<the run's loopback nginx port>, while SNI and
// the Host header stay `capacity.example.test`, so TLS hostname
// verification still applies. The Docker runner keeps the defaults
// (host resolves inside its isolated network, port 443, CA at
// /trust/root.crt).
import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

export const CAPACITY_HOST = 'capacity.example.test';
const LOOPBACK = new Set(['127.0.0.1', '::1']);

// Critic condition 5 (CAP-01): the DNS hole. When CAPACITY_TARGET_IP
// was unset, drivers connected by hostname and let the system
// resolver pick the address — a stale/hostile /etc/hosts or DNS entry
// for capacity.example.test could have pointed a driver at a real
// host. The runner pins the IP, but a manual invocation did not.
// Now an unset IP resolves the hostname FIRST and the resolved
// address must be loopback (pinned to 127.0.0.1); anything else is
// refused before any driver request. The one exception is the Docker
// generator: inside a container the disposable domain resolves via
// Docker DNS to the Caddy container on the runner-created isolated
// network, so a private address is accepted there only — detected by
// /.dockerenv, never by an env flag a manual invocation could set.
// Unresolvable hostnames fail closed. `hooks` exists so unit tests
// can inject a hostile/stale resolution without touching DNS.
function isPrivateIpv4(ip) {
  if (ip.startsWith('10.') || ip.startsWith('192.168.')) return true;
  const m = /^172\.(\d+)\./.exec(ip);
  return !!m && Number(m[1]) >= 16 && Number(m[1]) <= 31;
}

function defaultLookupHosts(host) {
  const script = `const dns=require('node:dns');`
    + `dns.lookup(process.env.CAPACITY_LOOKUP_HOST,{all:true},(err,addrs)=>{`
    + `if(err){process.exit(3)}console.log(JSON.stringify(addrs.map(a=>a.address)))});`;
  try {
    const out = execFileSync(process.execPath, ['-e', script], {
      encoding: 'utf8', timeout: 10000, stdio: ['ignore', 'pipe', 'pipe'],
      env: {...process.env, CAPACITY_LOOKUP_HOST: host},
    });
    const addrs = JSON.parse(out);
    return Array.isArray(addrs) ? addrs : null;
  } catch {
    return null;
  }
}

function defaultInContainer() {
  return fs.existsSync('/.dockerenv');
}

// Resolve `host` to loopback, or (Docker generator only) keep
// hostname resolution against a private container address. Returns
// the connect IP to pin ('127.0.0.1', or '' to keep resolving by
// hostname inside the container). Throws on anything else.
function guardResolvedHost(host, lookupHosts, inContainer) {
  const addrs = lookupHosts(host);
  if (!addrs || !addrs.length) {
    throw Error(`refused load target ${host}: hostname does not resolve (fail closed; set CAPACITY_TARGET_IP=127.0.0.1 explicitly)`);
  }
  if (addrs.every((a) => LOOPBACK.has(a))) return '127.0.0.1';
  if (inContainer() && addrs.every((a) => isPrivateIpv4(a))) return '';
  throw Error(`refused non-loopback load target resolution for ${host}: ${addrs.join(',')}`);
}

export function resolveTarget(env = process.env, hooks = {}) {
  const lookupHosts = hooks.lookupHosts || defaultLookupHosts;
  const inContainer = hooks.inContainer || defaultInContainer;
  const host = env.CAPACITY_TARGET_HOST || CAPACITY_HOST;
  const port = Number(env.CAPACITY_TARGET_PORT || 443);
  const protocol = env.CAPACITY_TARGET_PROTOCOL || 'https';
  let ip = env.CAPACITY_TARGET_IP || '';
  const caFile = env.CAPACITY_CA_FILE || '/trust/root.crt';
  const sessionsFile = env.CAPACITY_SESSIONS_FILE || '/sessions/users.json';
  let smsHost = env.CAPACITY_SMS_HOST || 'sms';
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
  if (!ip) {
    ip = guardResolvedHost(host, lookupHosts, inContainer);
  }
  if (smsHost === 'sms') {
    // Same hole class as the target host: 'sms' by name could resolve
    // off-loopback on a manual run. Pin it the same way.
    smsHost = guardResolvedHost('sms', lookupHosts, inContainer) || 'sms';
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
