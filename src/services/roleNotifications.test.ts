import { it, mock } from 'node:test';
import assert from 'node:assert/strict';
import {
  adminNotificationsApi,
  customerNotificationsApi,
  normalizeNotificationsResponse,
  sellerNotificationsApi,
  warehouseNotificationsApi,
} from './notificationsApi';

const jsonResponse = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

const outboxBody = {
  success: true,
  notifications: [
    { id: 'n1', kind: 'order_status', title: 'سفارش جدید ثبت شد', body: 'سفارش ۱۲۳', payload: { order_id: 'o1' }, status: 'pending', createdAt: '2026-11-13T10:00:00Z' },
    { id: 'n2', kind: 'finance', title: 'تسویه انجام شد', body: 'مبلغ واریز شد', payload: {}, status: 'read', createdAt: '2026-11-12T10:00:00Z' },
  ],
  unreadCount: 1,
};

it('each role client hits its own scoped endpoint and keeps the envelope', async () => {
  const cases = [
    { api: customerNotificationsApi, url: '/api/v1/notifications' },
    { api: sellerNotificationsApi, url: '/api/v1/seller/notifications' },
    { api: adminNotificationsApi, url: '/api/v1/admin/notifications' },
    { api: warehouseNotificationsApi, url: '/api/v1/warehouse/notifications' },
  ] as const;
  for (const { api, url } of cases) {
    const calls: { url: string; init: RequestInit }[] = [];
    const mocked = mock.method(globalThis, 'fetch', async (u: string | URL | Request, init: RequestInit) => {
      calls.push({ url: String(u), init });
      return jsonResponse(outboxBody);
    });
    try {
      const res = await api.getNotifications();
      assert.equal(calls[0].url, url);
      assert.equal(calls[0].init.method, 'GET');
      assert.equal(res.notifications.length, 2);
      assert.equal(res.unreadCount, 1);
      assert.equal(res.notifications[0].payload?.order_id, 'o1');
    } finally {
      mocked.mock.restore();
    }
  }
});

it('marks all read with an empty body and a single notification with {ids}', async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const mocked = mock.method(globalThis, 'fetch', async (u: string | URL | Request, init: RequestInit) => {
    calls.push({ url: String(u), init });
    return jsonResponse({ success: true, updated: 1 });
  });
  try {
    await sellerNotificationsApi.markNotificationsRead();
    assert.equal(calls[0].url, '/api/v1/seller/notifications/read');
    assert.equal(calls[0].init.method, 'POST');
    assert.equal(String(calls[0].init.body), '{}');
    await adminNotificationsApi.markNotificationsRead(['n1']);
    assert.equal(calls[1].url, '/api/v1/admin/notifications/read');
    assert.equal(String(calls[1].init.body), JSON.stringify({ ids: ['n1'] }));
    await warehouseNotificationsApi.markNotificationsRead(['n1', 'n2']);
    assert.equal(String(calls[2].init.body), JSON.stringify({ ids: ['n1', 'n2'] }));
  } finally {
    mocked.mock.restore();
  }
});

it('keeps the P1-15 missing-table envelope (success + empty list + unreadCount 0)', () => {
  const res = normalizeNotificationsResponse({ success: true, notifications: [], unreadCount: 0 });
  assert.equal(res.success, true);
  assert.deepEqual(res.notifications, []);
  assert.equal(res.unreadCount, 0);
});

it('derives unreadCount when the envelope omits it and tolerates a bare array', () => {
  const res = normalizeNotificationsResponse({ notifications: [{ id: 'n1', status: 'pending' }] });
  assert.equal(res.unreadCount, 1);
  const bare = normalizeNotificationsResponse([{ id: 'n1', status: 'read' }]);
  assert.deepEqual(bare.notifications.length, 1);
  assert.equal(bare.unreadCount, 0);
});
