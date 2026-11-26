import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { normalizeDomainRows, SellerDomainsPanel } from './SellerDomainsPanel';

// Batch 1 (C6, H1): the domains panel fed the /seller/domains response
// straight into setDomains and then called domains.filter(...) on
// render. Any non-array payload threw on first mount and blanked the
// whole app. The guard must normalise every shape to an array.
// The normalizer covers API shapes independently of the registration view.

const row = { domain: 'shop.example.com', storeId: 's-1', verificationToken: 'tok', status: 'active', approved: true };

test('domain registration starts collapsed and supports keyboard form submission', () => {
  const markup = renderToStaticMarkup(createElement(SellerDomainsPanel, { slug: 'demo', storeId: 's-1' }));
  assert.match(markup, /<details(?![^>]*\bopen\b)[^>]*>\s*<summary\b/);
  assert.match(markup, /<form\b[^>]*>.*<label\b[^>]*>.*<input\b[^>]*>.*<button type="submit"/s);
});

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

test('normalizeDomainRows drops null and non-object elements — never a throw', () => {
  const defaults = { domain: 'second.ir', storeId: '', verificationToken: '', status: '', approved: false };
  const rows = [row, null, 'x', 42, { domain: 'second.ir' }, undefined, { domain: 42 }, { domain: '' }];
  assert.deepEqual(normalizeDomainRows(rows), [row, defaults]);
  assert.deepEqual(normalizeDomainRows({ domains: rows }).filter((d) => d.storeId === 's-1'), [row]);
  for (const invalid of [42, [null], [null, null], [{ domain: '' }], [{ domain: 42 }]]) {
    assert.deepEqual(normalizeDomainRows(invalid), []);
  }
});
