import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { panelsForCapabilities } from './context/AuthContext';

describe('capability-aware protected workspace routing', () => {
  it('authorizes seller selection for a supplier-role identity with seller capability', () => {
    const authorized = panelsForCapabilities(['customer', 'seller', 'supplier']);

    assert.deepEqual(authorized, ['customer', 'seller_dashboard', 'supplier_dashboard']);
    assert.equal(authorized.includes('seller_dashboard'), true);
  });
});