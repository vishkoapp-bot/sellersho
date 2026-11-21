import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalizeDomainRows } from './SellerDomainsPanel';

// Batch 1 (C6, H1): the domains panel fed the /seller/domains response
// straight into setDomains and then called domains.filter(...) on
// render. Any non-array payload threw on first mount and blanked the
// whole app. The guard must normalise every shape to an array.
// (The panel itself cannot be rendered in this harness — no jsdom —
// so the exported normaliser is covered directly; the error boundary
// is the second line of defence for any remaining mount throw.)

const row = { domain: 'shop.example.com', storeId: 's-1', verificationToken: 'tok', status: 'active', approved: true };

test('normalizeDomainRows passes a bare array through', () => {
  assert.deepEqual(normalizeDomainRows([row]), [row]);
});

test('normalizeDomainRows unwraps a {domains: [...]} envelope', () => {
  assert.deepEqual(normalizeDomainRows({ domains: [row] }), [row]);
});

test('normalizeDomainRows returns [] for null, an object, or a non-array domains field — never a throw', () => {
  assert.deepEqual(normalizeDomainRows(null), []);
  assert.deepEqual(normalizeDomainRows({}), []);
  assert.deepEqual(normalizeDomainRows({ domains: null }), []);
  assert.deepEqual(normalizeDomainRows('error-page'), []);
  assert.deepEqual(normalizeDomainRows(undefined), []);
});
