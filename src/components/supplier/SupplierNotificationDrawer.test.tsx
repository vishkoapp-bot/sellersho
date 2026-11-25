import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { SupplierNotificationDrawer, countSupplierNotificationsByTab, countUnreadNotifications, filterSupplierNotificationsByTab, markOneSupplierNotificationRead, rejectionProposalId, selectProposalFromNotification, type NotificationTab } from './SupplierNotificationDrawer';
import type { SupplierNotification } from '../../services/supplierApi';

const rejected = (id: string, status: string): SupplierNotification => ({
  id,
  kind: 'proposal_rejected',
  title: `پیشنهاد «کالا ${id}» رد شد`,
  body: 'علت عدم تأیید: عکس نامناسب است.',
  payload: { proposal_id: `prop-${id}`, event: 'proposal_rejected', entity_type: 'proposed_product', product_title: `کالا ${id}` },
  status,
  createdAt: '2026-11-13T10:00:00Z',
});

const otherRow = (id: string, status: string): SupplierNotification => ({
  ...rejected(id, status),
  kind: 'order_status',
  title: `اعلان ${id}`,
  payload: undefined,
});

const render = (items: SupplierNotification[], initialTab?: NotificationTab) =>
  renderToStaticMarkup(
    <SupplierNotificationDrawer isOpen onClose={() => {}} onSelectProposal={() => {}} initialNotifications={items} initialTab={initialTab} />,
  );

it('counts unread and extracts the deep-link proposal id only for rejections', () => {
  assert.equal(countUnreadNotifications([rejected('1', 'pending'), rejected('2', 'read')]), 1);
  assert.equal(rejectionProposalId(rejected('1', 'pending')), 'prop-1');
  assert.equal(rejectionProposalId({ ...rejected('1', 'pending'), kind: 'proposal_approved' }), null);
  assert.equal(rejectionProposalId({ ...rejected('1', 'pending'), payload: undefined }), null);
});

it('renders a rejection item with title, reason body, unread styling and the resubmit CTA', () => {
  const html = render([rejected('1', 'pending'), rejected('2', 'read')]);
  assert.match(html, /۱ اعلان جدید خوانده‌نشده/);
  assert.match(html, /پیشنهاد «کالا 1» رد شد/);
  assert.match(html, /علت عدم تأیید: عکس نامناسب است\./);
  assert.match(html, /ویرایش و ارسال مجدد/);
  assert.match(html, /ring-rose-200/);
});

it('renders a non-rejection kind without the resubmit CTA and an all-read subtitle when nothing is unread', () => {
  const other: SupplierNotification = { ...rejected('1', 'read'), kind: 'order_status', payload: undefined };
  const html = render([other]);
  assert.match(html, /تمام اعلان‌ها خوانده شده است/);
  assert.doesNotMatch(html, /ویرایش و ارسال مجدد/);
});

it('renders the empty state when there are no notifications', () => {
  const html = render([]);
  assert.match(html, /اعلان جدیدی نیست\./);
});

it('renders the supplier-inbox empty state when there are no notifications', () => {
  const html = render([]);
  assert.match(html, /<dialog[^>]*aria-labelledby="supplier-notifications-title"/);
  assert.match(html, /پیشنهاد کالا، سفارش و ارسال اینجا نمایش داده می‌شوند\./);
});

it('renders nothing when closed', () => {
  const html = renderToStaticMarkup(
    <SupplierNotificationDrawer isOpen={false} onClose={() => {}} onSelectProposal={() => {}} initialNotifications={[rejected('1', 'pending')]} />,
  );
  assert.equal(html, '');
});

// ITEM 3 — read/unread tabs: Persian-digit counts cover the full list,
// and each tab filters the rendered rows.
it('shows tab counts for all / unread / read with Persian digits', () => {
  const items = [rejected('1', 'pending'), otherRow('2', 'pending'), rejected('3', 'read')];
  assert.deepEqual(countSupplierNotificationsByTab(items), { all: 3, unread: 2, read: 1 });
  assert.deepEqual(filterSupplierNotificationsByTab(items, 'unread').map((n) => n.id), ['1', '2']);
  assert.deepEqual(filterSupplierNotificationsByTab(items, 'read').map((n) => n.id), ['3']);
  assert.deepEqual(filterSupplierNotificationsByTab(items, 'all').map((n) => n.id), ['1', '2', '3']);
  const html = render(items);
  assert.match(html, /همه ۳/);
  assert.match(html, /خوانده نشده ۲/);
  assert.match(html, /خوانده شده ۱/);
});

it('filters rows by tab and shows a tab-specific empty state', () => {
  const items = [rejected('1', 'pending'), otherRow('2', 'pending'), rejected('3', 'read')];
  const unreadHtml = render(items, 'unread');
  assert.match(unreadHtml, /پیشنهاد «کالا 1» رد شد/);
  assert.match(unreadHtml, /اعلان 2/);
  assert.doesNotMatch(unreadHtml, /پیشنهاد «کالا 3» رد شد/);
  const readHtml = render(items, 'read');
  assert.match(readHtml, /پیشنهاد «کالا 3» رد شد/);
  assert.doesNotMatch(readHtml, /پیشنهاد «کالا 1» رد شد/);
  assert.doesNotMatch(readHtml, /اعلان 2/);
  const emptyReadHtml = render([rejected('1', 'pending')], 'read');
  assert.match(emptyReadHtml, /اعلان خوانده‌شده‌ای نیست\./);
  assert.doesNotMatch(emptyReadHtml, /پیشنهاد «کالا 1» رد شد/);
});

// ITEM 3 — explicit per-item "خوانده شد" button: present on every
// unread row, including non-rejections and rejections without a
// proposal id (rows the resubmit CTA never touches).
it('renders a per-item read button on unread rows of every kind', () => {
  const noProposal = { ...rejected('4', 'pending'), payload: undefined };
  const html = render([rejected('1', 'pending'), otherRow('2', 'pending'), noProposal, rejected('3', 'read')]);
  assert.match(html, /خوانده شد: پیشنهاد «کالا 1» رد شد/);
  assert.match(html, /خوانده شد: اعلان 2/);
  assert.match(html, /خوانده شد: پیشنهاد «کالا 4» رد شد/);
  assert.doesNotMatch(html, /خوانده شد: پیشنهاد «کالا 3» رد شد/);
  assert.equal((html.match(/>خوانده شد</g) ?? []).length, 3);
  // The non-rejection row has the read button but no resubmit CTA.
  const otherHtml = render([otherRow('2', 'pending')]);
  assert.match(otherHtml, /خوانده شد: اعلان 2/);
  assert.doesNotMatch(otherHtml, /ویرایش و ارسال مجدد/);
});

// The button's wire call, pinned without a DOM: this harness has no
// jsdom, so the click itself cannot be dispatched — the JSX binds the
// button's onClick to handleMarkOneRead, which calls this helper
// verbatim (same pattern as the CTA-handler tests above).
it('per-item mark-read posts {ids: [id]} on a non-rejection row', () => {
  const marked: string[][] = [];
  assert.equal(markOneSupplierNotificationRead(otherRow('2', 'pending'), async (ids) => { marked.push(ids); }), true);
  assert.deepEqual(marked, [['2']]);
});

it('per-item mark-read works on a rejection without a proposal id', () => {
  const marked: string[][] = [];
  const noProposal = { ...rejected('4', 'pending'), payload: undefined };
  assert.equal(markOneSupplierNotificationRead(noProposal, async (ids) => { marked.push(ids); }), true);
  assert.deepEqual(marked, [['4']]);
});

it('per-item mark-read skips already-read rows and never throws on a failed POST', async () => {
  const marked: string[][] = [];
  assert.equal(markOneSupplierNotificationRead(rejected('3', 'read'), async (ids) => { marked.push(ids); }), false);
  assert.deepEqual(marked, []);
  assert.equal(markOneSupplierNotificationRead(rejected('1', 'pending'), async () => { throw new Error('network down'); }), true);
  await new Promise((resolve) => setImmediate(resolve));
});

// CTA handler tests: the drawer button's onClick calls
// selectProposalFromNotification verbatim, so these pin the click's
// contract — which proposal id the dashboard receives. What they cannot
// cover is the JSX binding itself (the rendered button actually invoking
// this handler on a real click): that needs DOM event dispatch, which
// this harness (tsx --test + renderToStaticMarkup, no jsdom) cannot run.
it('CTA handler navigates to the rejection proposal id and marks it read first', async () => {
  const order: string[] = [];
  const selected: string[] = [];
  const marked: string[][] = [];
  const reported: Array<number | undefined> = [];
  const returned = selectProposalFromNotification(
    rejected('1', 'pending'),
    (id) => { order.push('select'); selected.push(id); },
    async (ids) => { order.push('mark'); marked.push(ids); return { unreadCount: 0 }; },
    (count) => { reported.push(count); },
  );
  assert.equal(returned, 'prop-1');
  assert.deepEqual(selected, ['prop-1']);
  assert.deepEqual(marked, [['1']]);
  assert.deepEqual(order, ['mark', 'select']);
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(reported, [0]);
});

it('CTA handler does not navigate for a non-rejection kind and never marks read', () => {
  const selected: string[] = [];
  const marked: string[][] = [];
  const other: SupplierNotification = { ...rejected('1', 'read'), kind: 'order_status', payload: undefined };
  const returned = selectProposalFromNotification(
    other,
    (id) => { selected.push(id); },
    async (ids) => { marked.push(ids); },
  );
  assert.equal(returned, null);
  assert.deepEqual(selected, []);
  assert.deepEqual(marked, []);
});

it('CTA handler does not navigate when a rejection carries no proposal id', () => {
  const selected: string[] = [];
  const marked: string[][] = [];
  const returned = selectProposalFromNotification(
    { ...rejected('1', 'pending'), payload: undefined },
    (id) => { selected.push(id); },
    async (ids) => { marked.push(ids); },
  );
  assert.equal(returned, null);
  assert.deepEqual(selected, []);
  assert.deepEqual(marked, []);
});

it('CTA handler still navigates when the best-effort mark-read fails', async () => {
  const selected: string[] = [];
  const reported: Array<number | undefined> = [];
  const returned = selectProposalFromNotification(
    rejected('1', 'pending'),
    (id) => { selected.push(id); },
    async () => { throw new Error('network down'); },
    (count) => { reported.push(count); },
  );
  assert.equal(returned, 'prop-1');
  assert.deepEqual(selected, ['prop-1']);
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(reported, []);
});

it('CTA handler reports undefined unreadCount when the server omits it', async () => {
  const reported: Array<number | undefined> = [];
  selectProposalFromNotification(
    rejected('1', 'pending'),
    () => {},
    async () => ({}),
    (count) => { reported.push(count); },
  );
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(reported, [undefined]);
});
