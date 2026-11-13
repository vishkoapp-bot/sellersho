import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { RoleNotificationDrawer, countUnreadOutboxNotifications, formatOutboxDate } from './RoleNotificationDrawer';
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

const render = (items: OutboxNotification[], extra?: React.ReactNode) =>
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
