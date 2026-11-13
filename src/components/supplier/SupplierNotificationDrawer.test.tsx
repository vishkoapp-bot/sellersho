import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { SupplierNotificationDrawer, countUnreadNotifications, rejectionProposalId, selectProposalFromNotification } from './SupplierNotificationDrawer';
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

const render = (items: SupplierNotification[]) =>
  renderToStaticMarkup(
    <SupplierNotificationDrawer isOpen onClose={() => {}} onSelectProposal={() => {}} initialNotifications={items} />,
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

it('renders nothing when closed', () => {
  const html = renderToStaticMarkup(
    <SupplierNotificationDrawer isOpen={false} onClose={() => {}} onSelectProposal={() => {}} initialNotifications={[rejected('1', 'pending')]} />,
  );
  assert.equal(html, '');
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
