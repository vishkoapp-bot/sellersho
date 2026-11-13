import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  bonusReadStorageKey,
  formatNotificationTime,
  loadReadBonusIds,
  mapOutboxNotificationToItem,
  storeReadBonusIds,
} from './NotificationDrawer';
import type { OutboxNotification } from '../../services/notificationsApi';

const outbox = (kind: string, status: string): OutboxNotification => ({
  id: '11111111-1111-1111-1111-111111111111',
  kind,
  title: 'سفارش شما بسته‌بندی شد',
  body: 'سفارش ۱۲۳ بسته‌بندی شد.',
  payload: { order_id: 'o1' },
  status,
  createdAt: '2026-11-13T10:00:00Z',
});

it('maps packed/shipped rows to order items with unread state', () => {
  const item = mapOutboxNotificationToItem(outbox('order_status', 'pending'));
  assert.equal(item.id, '11111111-1111-1111-1111-111111111111');
  assert.equal(item.title, 'سفارش شما بسته‌بندی شد');
  assert.equal(item.message, 'سفارش ۱۲۳ بسته‌بندی شد.');
  assert.equal(item.type, 'order');
  assert.equal(item.read, false);
  assert.notEqual(item.time, '');
  assert.notEqual(item.time, 'به‌تازگی');
});

it('maps read rows as read and finance kinds to the finance type', () => {
  assert.equal(mapOutboxNotificationToItem(outbox('order_status', 'read')).read, true);
  assert.equal(mapOutboxNotificationToItem(outbox('finance', 'pending')).type, 'finance');
  assert.equal(mapOutboxNotificationToItem(outbox('dispute_filed', 'pending')).type, 'system');
  assert.equal(mapOutboxNotificationToItem(outbox('return_qc', 'pending')).type, 'warehouse');
});

it('formats an invalid date as "به‌تازگی"', () => {
  assert.equal(formatNotificationTime('not-a-date'), 'به‌تازگی');
  assert.equal(formatNotificationTime(''), 'به‌تازگی');
});

it('persists bonus read state per store in localStorage (P1-14)', () => {
  const store = new Map<string, string>();
  (globalThis as any).localStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => { store.set(k, v); },
    removeItem: (k: string) => { store.delete(k); },
  };
  try {
    assert.equal(bonusReadStorageKey('mone'), 'seler-notifications-read-bonus:mone');
    assert.deepEqual([...loadReadBonusIds('mone')], []);
    storeReadBonusIds('mone', new Set(['notif-bonus-b1', 'notif-bonus-b2']));
    assert.deepEqual([...loadReadBonusIds('mone')].sort(), ['notif-bonus-b1', 'notif-bonus-b2']);
    // A different store has its own read-set.
    assert.deepEqual([...loadReadBonusIds('sahara1')], []);
    // Corrupt stored JSON degrades to an empty set, never throws.
    store.set(bonusReadStorageKey('mone'), '{not json');
    assert.deepEqual([...loadReadBonusIds('mone')], []);
  } finally {
    delete (globalThis as any).localStorage;
  }
});
