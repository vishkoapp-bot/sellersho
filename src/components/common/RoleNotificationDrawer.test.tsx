import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { RoleNotificationDrawer, countOutboxNotificationsByTab, countUnreadOutboxNotifications, filterOutboxNotificationsByTab, formatOutboxDate, markOneOutboxNotificationRead, type NotificationTab } from './RoleNotificationDrawer';
import { sellerNotificationsApi, type OutboxNotification } from '../../services/notificationsApi';

const row = (id: string, kind: string, status: string): OutboxNotification => ({
  id,
  kind,
  title: `اعلان ${id}`,
  body: 'متن اعلان',
  payload: {},
  status,
  createdAt: '2026-11-13T10:00:00Z',
});

const render = (items: OutboxNotification[], extra?: React.ReactNode, initialTab?: NotificationTab) =>
  renderToStaticMarkup(
    <RoleNotificationDrawer
      isOpen
      onClose={() => {}}
      title="اعلان‌های فروشگاه"
      emptyTitle="اعلان جدیدی نیست."
      emptyHint="راهنما"
      accent="brand"
      api={sellerNotificationsApi}
      initialNotifications={items}
      extraContent={extra}
      initialTab={initialTab}
    />,
  );

it('counts unread rows and formats the outbox date', () => {
  assert.equal(countUnreadOutboxNotifications([row('1', 'order_status', 'pending'), row('2', 'order_status', 'read')]), 1);
  assert.notEqual(formatOutboxDate('2026-11-13T10:00:00Z'), '');
  assert.equal(formatOutboxDate('not-a-date'), '');
});

it('renders rows with title, body, unread styling and the Persian unread count', () => {
  const html = render([row('1', 'order_status', 'pending'), row('2', 'finance', 'read')]);
  assert.match(html, /اعلان‌های فروشگاه/);
  assert.match(html, /۱ اعلان جدید خوانده‌نشده/);
  assert.match(html, /اعلان 1/);
  assert.match(html, /متن اعلان/);
  assert.match(html, /خواندن همه/);
  // Unread row carries the accent ring; the read row does not.
  assert.match(html, /ring-brand-border/);
});

it('renders the empty state when there are no notifications and no extra content', () => {
  const html = render([]);
  assert.match(html, /اعلان جدیدی نیست\./);
  assert.match(html, /تمام اعلان‌ها خوانده شده است/);
});

it('renders extra content above the list (admin alerts section)', () => {
  const html = render([], <div key="alerts">هشدارهای نیازمند رسیدگی</div>);
  assert.match(html, /هشدارهای نیازمند رسیدگی/);
  assert.doesNotMatch(html, /اعلان جدیدی نیست\./);
});

it('renders nothing when closed', () => {
  const html = renderToStaticMarkup(
    <RoleNotificationDrawer
      isOpen={false}
      onClose={() => {}}
      title="اعلان‌های فروشگاه"
      emptyTitle="اعلان جدیدی نیست."
      emptyHint="راهنما"
      api={sellerNotificationsApi}
      initialNotifications={[row('1', 'order_status', 'pending')]}
    />,
  );
  assert.equal(html, '');
});

// ITEM 3 — read/unread tabs: Persian-digit counts cover the full list,
// and each tab filters the rendered rows.
it('shows tab counts for all / unread / read with Persian digits', () => {
  const items = [row('1', 'order_status', 'pending'), row('2', 'order_status', 'pending'), row('3', 'finance', 'read')];
  assert.deepEqual(countOutboxNotificationsByTab(items), { all: 3, unread: 2, read: 1 });
  assert.deepEqual(filterOutboxNotificationsByTab(items, 'unread').map((n) => n.id), ['1', '2']);
  assert.deepEqual(filterOutboxNotificationsByTab(items, 'read').map((n) => n.id), ['3']);
  assert.deepEqual(filterOutboxNotificationsByTab(items, 'all').map((n) => n.id), ['1', '2', '3']);
  const html = render(items);
  assert.match(html, /همه ۳/);
  assert.match(html, /خوانده‌نشده ۲/);
  assert.match(html, /خوانده‌شده ۱/);
});

it('filters rows by tab and shows a tab-specific empty state', () => {
  const items = [row('1', 'order_status', 'pending'), row('2', 'order_status', 'pending'), row('3', 'finance', 'read')];
  const unreadHtml = render(items, undefined, 'unread');
  assert.match(unreadHtml, /اعلان 1/);
  assert.match(unreadHtml, /اعلان 2/);
  assert.doesNotMatch(unreadHtml, /اعلان 3/);
  const readHtml = render(items, undefined, 'read');
  assert.match(readHtml, /اعلان 3/);
  assert.doesNotMatch(readHtml, /اعلان 1/);
  assert.doesNotMatch(readHtml, /اعلان 2/);
  const emptyReadHtml = render([row('1', 'order_status', 'pending')], undefined, 'read');
  assert.match(emptyReadHtml, /اعلان خوانده‌شده‌ای نیست\./);
  assert.doesNotMatch(emptyReadHtml, /اعلان 1/);
  const emptyUnreadHtml = render([row('3', 'finance', 'read')], undefined, 'unread');
  assert.match(emptyUnreadHtml, /اعلان خوانده‌نشده‌ای نیست\./);
});

// ITEM 3 — explicit per-item "خوانده شد" button: present on unread
// rows only (the row-tap behaviour is unchanged alongside it).
it('renders a per-item read button on unread rows only', () => {
  const html = render([row('1', 'order_status', 'pending'), row('2', 'finance', 'read')]);
  assert.match(html, /خوانده شد: اعلان 1/);
  assert.doesNotMatch(html, /خوانده شد: اعلان 2/);
  assert.equal((html.match(/>خوانده شد</g) ?? []).length, 1);
});

// The button's wire call, pinned without a DOM: this harness has no
// jsdom, so the click itself cannot be dispatched — the JSX binds the
// button's onClick to handleMarkOneRead, which calls this helper
// verbatim (same pattern as the supplier CTA-handler tests).
it('per-item mark-read posts {ids: [id]} and skips already-read rows', async () => {
  const marked: string[][] = [];
  assert.equal(markOneOutboxNotificationRead(row('1', 'order_status', 'pending'), async (ids) => { marked.push(ids); }), true);
  assert.deepEqual(marked, [['1']]);
  assert.equal(markOneOutboxNotificationRead(row('2', 'finance', 'read'), async (ids) => { marked.push(ids); }), false);
  assert.deepEqual(marked, [['1']]);
  // Best-effort: a failing POST never throws.
  assert.equal(markOneOutboxNotificationRead(row('3', 'order_status', 'pending'), async () => { throw new Error('network down'); }), true);
  await new Promise((resolve) => setImmediate(resolve));
});
