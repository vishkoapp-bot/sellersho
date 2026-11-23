import assert from 'node:assert/strict';
import { test } from 'node:test';
import { remainingCatalogCount } from './catalogRemaining';

test('remaining catalog count floors at zero and preserves positive differences', () => {
  assert.equal(remainingCatalogCount(23, 24), 0);
  assert.equal(remainingCatalogCount(24, 24), 0);
  assert.equal(remainingCatalogCount(41, 24), 17);
});
