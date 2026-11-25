import assert from 'node:assert/strict';
import test from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { GitDeployProbe } from './GitDeployProbe';

const popup = (pathname: string, search: string) =>
  renderToStaticMarkup(<GitDeployProbe location={{ pathname, search }} />);

test('the homepage opens a closable git deploy verification popup only on request', () => {
  const html = popup('/', '?git-deploy=1');
  assert.match(html, /<dialog\b/);
  assert.match(html, />git deploy</);
  assert.match(html, /<form[^>]*\bmethod="dialog"/);
  assert.match(html, /<button type="submit"/);
  assert.equal(popup('/', ''), '');
  assert.equal(popup('/', '?git-deploy=0'), '');
  assert.equal(popup('/products', '?git-deploy=1'), '');
});
