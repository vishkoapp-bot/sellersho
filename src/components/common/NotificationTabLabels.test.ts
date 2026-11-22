import assert from 'node:assert/strict';
import { test } from 'node:test';
import { NOTIFICATION_TAB_LABELS as customer } from './NotificationDrawer';
import { NOTIFICATION_TAB_LABELS as role } from './RoleNotificationDrawer';
import { NOTIFICATION_TAB_LABELS as supplier } from '../supplier/SupplierNotificationDrawer';

test('notification filter labels use a visible word space across all drawers', () => {
  for (const labels of [customer, role, supplier]) {
    assert.equal(labels.read, 'خوانده شده');
    assert.equal(labels.unread, 'خوانده نشده');
  }
});
