import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { panelForRole, panelsForCapabilities } from './AuthContext';

describe('panelForRole', () => {
  for (const [role, panel] of [
    ['customer', 'customer'],
    ['seller', 'seller_dashboard'],
    ['supplier', 'supplier_dashboard'],
    ['warehouse_staff', 'warehouse_panel'],
    ['logistics_specialist', 'warehouse_panel'],
    ['finance_specialist', 'admin_panel'],
    ['support_specialist', 'admin_panel'],
    ['admin', 'admin_panel'],
  ]) {
    it(`${role} routes to ${panel}`, () => {
      assert.equal(panelForRole(role), panel);
    });
  }

  it('does not trust unknown client roles with a privileged panel', () => {
    assert.equal(panelForRole('unknown'), 'customer');
  });

  it('keeps logistics warehouse access separate from finance/admin access', () => {
    assert.deepEqual(panelsForCapabilities(['logistics_specialist']), ['warehouse_panel']);
    assert.deepEqual(panelsForCapabilities(['finance_specialist']), ['admin_panel']);
  });
});
