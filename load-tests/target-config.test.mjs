import test from 'node:test';
import assert from 'node:assert/strict';
import {CAPACITY_HOST, resolveTarget} from './target-config.mjs';

test('default target is the disposable domain only', () => {
  const t = resolveTarget({});
  assert.equal(t.host, CAPACITY_HOST);
  assert.equal(t.port, 443);
  assert.equal(t.protocol, 'https');
  assert.equal(t.ip, '');
});

test('native loopback override keeps the disposable hostname', () => {
  const t = resolveTarget({
    CAPACITY_TARGET_IP: '127.0.0.1',
    CAPACITY_TARGET_PORT: '8443',
    CAPACITY_CA_FILE: '/tmp/ca.crt',
    CAPACITY_SESSIONS_FILE: '/tmp/users.json',
    CAPACITY_SMS_HOST: '127.0.0.1',
  });
  assert.equal(t.host, CAPACITY_HOST);
  assert.equal(t.ip, '127.0.0.1');
  assert.equal(t.port, 8443);
  assert.equal(t.smsHost, '127.0.0.1');
});

test('prod and other real hosts are refused', () => {
  for (const host of ['sellershow.ir', '46.249.98.190', 'example.com', 'localhost', 'capacity.example.test.evil.com']) {
    assert.throws(() => resolveTarget({CAPACITY_TARGET_HOST: host}), /refused non-disposable/);
  }
});

test('non-loopback connect IP, bad port, bad protocol and bad SMS host are refused', () => {
  assert.throws(() => resolveTarget({CAPACITY_TARGET_IP: '46.249.98.190'}), /non-loopback/);
  assert.throws(() => resolveTarget({CAPACITY_TARGET_IP: '10.0.0.1'}), /non-loopback/);
  assert.throws(() => resolveTarget({CAPACITY_TARGET_PORT: '0'}), /port/);
  assert.throws(() => resolveTarget({CAPACITY_TARGET_PORT: '99999'}), /port/);
  assert.throws(() => resolveTarget({CAPACITY_TARGET_PROTOCOL: 'ftp'}), /protocol/);
  assert.throws(() => resolveTarget({CAPACITY_SMS_HOST: 'sms.example.com'}), /SMS double host/);
});
