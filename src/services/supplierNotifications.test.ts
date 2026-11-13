import { it, mock } from 'node:test';
import assert from 'node:assert/strict';
import { supplierApi } from './supplierApi';

const jsonResponse = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

it('fetches supplier notifications from the supplier-scoped endpoint and keeps the envelope', async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const mocked = mock.method(globalThis, 'fetch', async (url: string | URL | Request, init: RequestInit) => {
    calls.push({ url: String(url), init });
    return jsonResponse({
      success: true,
      notifications: [
        { id: 'n1', kind: 'proposal_rejected', title: 'پیشنهاد «تیشرت» رد شد', body: 'علت: عکس نامناسب', payload: { proposal_id: 'prop-1', event: 'proposal_rejected', entity_type: 'proposed_product', product_title: 'تیشرت' }, status: 'pending', createdAt: '2026-11-13T10:00:00Z' },
        { id: 'n2', kind: 'proposal_rejected', title: 'پیشنهاد «شلوار» رد شد', body: 'دلیلی ثبت نشده است', payload: { proposal_id: 'prop-2', event: 'proposal_rejected', entity_type: 'proposed_product', product_title: 'شلوار' }, status: 'read', createdAt: '2026-11-12T10:00:00Z' },
      ],
      unreadCount: 1,
    });
  });
  try {
    const res = await supplierApi.getNotifications();
    assert.equal(calls[0].url, '/api/v1/supplier/notifications');
    assert.equal(calls[0].init.method, 'GET');
    assert.equal(res.notifications.length, 2);
    assert.equal(res.unreadCount, 1);
    assert.equal(res.notifications[0].payload?.proposal_id, 'prop-1');
  } finally { mocked.mock.restore(); }
});

it('derives unreadCount when the outbox table is missing (envelope without unreadCount)', async () => {
  const mocked = mock.method(globalThis, 'fetch', async () => jsonResponse({ notifications: [] }));
  try {
    const res = await supplierApi.getNotifications();
    assert.deepEqual(res.notifications, []);
    assert.equal(res.unreadCount, 0);
  } finally { mocked.mock.restore(); }
});

it('marks all read with an empty body and a single notification with {ids}', async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const mocked = mock.method(globalThis, 'fetch', async (url: string | URL | Request, init: RequestInit) => {
    calls.push({ url: String(url), init });
    return jsonResponse({ success: true, updated: 1 });
  });
  try {
    await supplierApi.markNotificationsRead();
    assert.equal(calls[0].url, '/api/v1/supplier/notifications/read');
    assert.equal(calls[0].init.method, 'POST');
    assert.equal(String(calls[0].init.body), '{}');
    await supplierApi.markNotificationsRead(['n1']);
    assert.equal(String(calls[1].init.body), JSON.stringify({ ids: ['n1'] }));
  } finally { mocked.mock.restore(); }
});
