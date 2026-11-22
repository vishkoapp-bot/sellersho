import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  AdminPlaceholderTab,
  AdminStoresListView,
  AdminHomepageSectionsListView,
  AdminBannersListView,
  AdminSiteContentListView,
  AdminCallbackRetriesListView,
  AdminNewsletterSubscribersListView,
  AdminReviewsListView,
  AdminStaffListView,
  AdminHomepageTab,
  AdminBannersTab,
  AdminSiteContentTab,
  AdminCallbackRetriesTab,
  AdminNewsletterTab,
  AdminReviewsTab,
  AdminStaffTab,
  flattenSiteContent,
} from './AdminNewTabs';
import type { Store } from '../../types';
import type {
  HomepageSectionDto,
  AdminBannerDto,
  NewsletterSubscriberDto,
  AdminStaffDto,
  CallbackRetryDto,
} from '../../services/adminApi';

// D1 (Batch 4a) + D6. Harness is tsx --test + renderToStaticMarkup
// (no jsdom), following AdminSuppliersTab.test.tsx: rendering tests
// target the exported presentational list views with fixture data, and
// the containers render their loading state (useEffect never runs in
// SSR, so no fetch happens and no adminApi mock is needed — the
// loading state itself is the assertion). What this harness cannot
// cover — the container fetch wiring (loading -> rows after the GET
// resolves) and the D6 scroll effect (a useEffect on activeTab) —
// needs a DOM/browser; D6 is covered by the puppeteer fixture test
// AdminDashboardView.scroll.test.tsx instead. AdminStoresListTab's
// container uses useStore() and throws outside a provider, so only
// its presentational view is rendered here.

// The stale copy must be gone from every render in this file.
const STALE_COPY = /پس از ورود به حساب/;

const store = (overrides: Partial<Store> = {}): Store => ({
  id: 'store-1',
  name: 'فروشگاه الف',
  slug: 'store-a',
  ownerType: 'dropshipper',
  ownerName: 'مالک الف',
  tagline: '',
  profitMarginPercent: 15,
  description: '',
  activeCategories: [],
  totalSales: 0,
  monthlyRevenueEstimate: 0,
  status: 'active',
  ...overrides,
});

it('placeholder no longer claims content appears after super_admin sign-in', () => {
  const html = renderToStaticMarkup(<AdminPlaceholderTab title="بخش آزمایشی" />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /این بخش هنوز پیاده‌سازی نشده است/);
  assert.match(html, /بخش آزمایشی/);
});

it('stores-list renders rows from useStore data and an honest empty state', () => {
  const html = renderToStaticMarkup(<AdminStoresListView stores={[store(), store({ id: 'store-2', name: 'فروشگاه ب', slug: 'store-b' })]} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /فروشگاه الف/);
  assert.match(html, /store-a/);
  assert.match(html, /مالک الف/);
  assert.match(html, /فروشگاه ب/);
  const empty = renderToStaticMarkup(<AdminStoresListView stores={[]} />);
  assert.match(empty, /فروشگاهی ثبت نشده است/);
  assert.doesNotMatch(empty, STALE_COPY);
});

it('homepage sections render type/title/order/active and an honest empty state', () => {
  const sections: HomepageSectionDto[] = [
    { id: 'sec-1', sectionType: 'hero', title: 'بنر اصلی', sortOrder: 1, isActive: true },
    { id: 'sec-2', sectionType: 'categories', title: null, sortOrder: 2, isActive: false },
  ];
  const html = renderToStaticMarkup(<AdminHomepageSectionsListView sections={sections} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /بنر اصلی/);
  assert.match(html, /hero/);
  assert.match(html, /categories/);
  assert.match(html, /غیرفعال/);
  const empty = renderToStaticMarkup(<AdminHomepageSectionsListView sections={[]} />);
  assert.match(empty, /بخشی برای صفحه اصلی ثبت نشده است/);
});

it('banners render title/position/order/active and an honest empty state', () => {
  const banners: AdminBannerDto[] = [
    { id: 'b1', title: 'بنر فروش', position: 'home-top', sortOrder: 1, isActive: true, imageUrl: 'https://example.com/b.jpg' },
  ];
  const html = renderToStaticMarkup(<AdminBannersListView banners={banners} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /بنر فروش/);
  assert.match(html, /home-top/);
  assert.match(html, /فعال/);
  const empty = renderToStaticMarkup(<AdminBannersListView banners={[]} />);
  assert.match(empty, /بنری ثبت نشده است/);
});

it('site-content flattens the nested map to key/value rows and an honest empty state', () => {
  assert.deepEqual(flattenSiteContent({ general: { siteName: 'سلرشو' }, contact: { phone: '021' } }), [
    { key: 'general.siteName', value: 'سلرشو' },
    { key: 'contact.phone', value: '021' },
  ]);
  const html = renderToStaticMarkup(<AdminSiteContentListView content={{ general: { siteName: 'سلرشو' } }} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /general\.siteName/);
  assert.match(html, /سلرشو/);
  const empty = renderToStaticMarkup(<AdminSiteContentListView content={{}} />);
  assert.match(empty, /محتوایی ثبت نشده است/);
});

it('callback-retries render order/status/attempts/last error, with no retry button', () => {
  const retries: CallbackRetryDto[] = [
    {
      id: 'r1', pspReference: 'PSP-1', orderNumber: 'ORD-1', attemptCount: 2, maxAttempts: 5,
      status: 'needs_manual_review', lastError: 'verification failed',
      createdAt: '2026-11-20T10:00:00Z', updatedAt: '2026-11-20T10:00:00Z', eventStatus: 'failed',
    },
  ];
  const html = renderToStaticMarkup(<AdminCallbackRetriesListView retries={retries} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /ORD-1/);
  assert.match(html, /needs_manual_review/);
  assert.match(html, /verification failed/);
  assert.match(html, /failed/);
  // D1 rule: no manual "retry now" action — the view must not render
  // any button at all.
  assert.doesNotMatch(html, /<button/);
  const empty = renderToStaticMarkup(<AdminCallbackRetriesListView retries={[]} />);
  assert.match(empty, /پرداخت ناموفقی ثبت نشده است/);
});

it('newsletter subscribers render mobile/status/store and an honest empty state', () => {
  const subscribers: NewsletterSubscriberDto[] = [
    { id: 'n1', storeId: 'store-1', mobile: '09120000001', email: 'a@example.com', status: 'active', createdAt: '2026-11-01T00:00:00Z' },
  ];
  const html = renderToStaticMarkup(<AdminNewsletterSubscribersListView subscribers={subscribers} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /09120000001/);
  assert.match(html, /active/);
  assert.match(html, /store-1/);
  const empty = renderToStaticMarkup(<AdminNewsletterSubscribersListView subscribers={[]} />);
  assert.match(empty, /مشترکی ثبت نشده است/);
});

it('reviews render customer/rating/status from the raw snake_case columns and an honest empty state', () => {
  const reviews = [
    {
      id: 'REV-1', product_id: 'p1', customer_display_name: 'مشتری الف', rating: 5,
      title: 'عالی', body: 'خیلی خوب بود', status: 'published', created_at: '2026-11-02T10:00:00Z',
    },
  ];
  const html = renderToStaticMarkup(<AdminReviewsListView reviews={reviews} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /مشتری الف/);
  assert.match(html, /عالی/);
  assert.match(html, /خیلی خوب بود/);
  assert.match(html, /published/);
  const empty = renderToStaticMarkup(<AdminReviewsListView reviews={[]} />);
  assert.match(empty, /نظری ثبت نشده است/);
});

it('staff render name/mobile/role/active and an honest empty state', () => {
  const staff: AdminStaffDto[] = [
    { id: 'u1', mobile: '09120000002', fullName: 'کارمند الف', role: 'warehouse_staff', username: '', isActive: true, createdAt: '2026-10-01T00:00:00Z' },
  ];
  const html = renderToStaticMarkup(<AdminStaffListView staff={staff} />);
  assert.doesNotMatch(html, STALE_COPY);
  assert.match(html, /کارمند الف/);
  assert.match(html, /09120000002/);
  assert.match(html, /warehouse_staff/);
  assert.match(html, /فعال/);
  const empty = renderToStaticMarkup(<AdminStaffListView staff={[]} />);
  assert.match(empty, /کارکنی ثبت نشده است/);
});

it('each API-backed tab container renders a loading state with no stale copy', () => {
  for (const [name, el] of [
    ['homepage', <AdminHomepageTab />],
    ['banners', <AdminBannersTab />],
    ['site-content', <AdminSiteContentTab />],
    ['callback-retries', <AdminCallbackRetriesTab />],
    ['newsletter', <AdminNewsletterTab />],
    ['reviews', <AdminReviewsTab />],
    ['staff', <AdminStaffTab />],
  ] as const) {
    const html = renderToStaticMarkup(el);
    assert.doesNotMatch(html, STALE_COPY, `${name} still renders the stale copy`);
    assert.match(html, /در حال بارگذاری…/, `${name} does not render its loading state`);
  }
});
