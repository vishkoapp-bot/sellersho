import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  bonusReadStorageKey,
  countNotificationsByTab,
  filterNotificationsByTab,
  formatNotificationTime,
  isBonusNotification,
  loadReadBonusIds,
  mapOutboxNotificationToItem,
  markOneCustomerNotificationRead,
  storeReadBonusIds,
  type NotificationItem,
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

// ITEM 3 — read/unread tabs: counts and filtering span BOTH slices
// the customer drawer merges (server outbox rows + localStorage
// referral-bonus rows), never just one of them.
const customerItem = (id: string, read: boolean): NotificationItem => ({
  id,
  role: 'customer',
  title: `اعلان ${id}`,
  message: 'متن اعلان',
  time: 'به‌تازگی',
  read,
  type: 'order',
});

it('tab counts and filters cover bonus and outbox rows together', () => {
  const items = [
    customerItem('notif-bonus-b1', false),
    customerItem('notif-bonus-b2', true),
    customerItem('11111111-1111-1111-1111-111111111111', false),
    customerItem('22222222-2222-2222-2222-222222222222', true),
  ];
  assert.deepEqual(countNotificationsByTab(items), { all: 4, unread: 2, read: 2 });
  assert.deepEqual(filterNotificationsByTab(items, 'unread').map((n) => n.id), ['notif-bonus-b1', '11111111-1111-1111-1111-111111111111']);
  assert.deepEqual(filterNotificationsByTab(items, 'read').map((n) => n.id), ['notif-bonus-b2', '22222222-2222-2222-2222-222222222222']);
  assert.deepEqual(filterNotificationsByTab(items, 'all').map((n) => n.id), items.map((n) => n.id));
  assert.equal(isBonusNotification(items[0]), true);
  assert.equal(isBonusNotification(items[2]), false);
});

// ITEM 3 — per-item mark-read: a bonus row stays local-only (its id
// is not a server id, so no POST may ever fire for it); an outbox row
// POSTs {ids: [id]}. The drawer itself cannot be mounted in this
// harness (no jsdom; useStore throws without StoreContext providers),
// so the decision — the part that differs per row — is pinned here,
// and the drawer's row-tap and "خوانده شد" button both call this
// helper verbatim.
it('bonus row mark-read stays local-only and persists the read id', () => {
  const store = new Map<string, string>();
  (globalThis as any).localStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => { store.set(k, v); },
    removeItem: (k: string) => { store.delete(k); },
  };
  try {
    const marked: string[][] = [];
    const result = markOneCustomerNotificationRead(
      customerItem('notif-bonus-b1', false),
      'mone',
      async (ids) => { marked.push(ids); },
    );
    assert.equal(result, 'local');
    assert.deepEqual(marked, []);
    assert.deepEqual([...loadReadBonusIds('mone')], ['notif-bonus-b1']);
  } finally {
    delete (globalThis as any).localStorage;
  }
});

it('outbox row mark-read posts {ids: [id]} and skips already-read rows', async () => {
  const marked: string[][] = [];
  const outboxId = '11111111-1111-1111-1111-111111111111';
  assert.equal(markOneCustomerNotificationRead(customerItem(outboxId, false), 'mone', async (ids) => { marked.push(ids); }), 'server');
  assert.deepEqual(marked, [[outboxId]]);
  assert.equal(markOneCustomerNotificationRead(customerItem(outboxId, true), 'mone', async (ids) => { marked.push(ids); }), 'already-read');
  assert.deepEqual(marked, [[outboxId]]);
  assert.equal(markOneCustomerNotificationRead(customerItem('notif-bonus-b1', true), 'mone', async (ids) => { marked.push(ids); }), 'already-read');
  // Best-effort: a failing POST never throws.
  assert.equal(markOneCustomerNotificationRead(customerItem(outboxId, false), 'mone', async () => { throw new Error('network down'); }), 'server');
  await new Promise((resolve) => setImmediate(resolve));
});
