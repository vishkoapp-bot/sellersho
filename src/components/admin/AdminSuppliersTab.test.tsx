import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import {
  AdminSupplierCardView,
  AdminSupplierDetailView,
  formatMarkup,
  auditHistoryReason,
} from './AdminSuppliersTab';
import type { AdminSupplierRow, SupplierDetail } from './AdminSuppliersTab';
import { toPersianDigits } from '../../utils/formatters';

const noop = () => {};
const noopSupplier = (_s: AdminSupplierRow) => {};
const noopId = (_id: string) => {};

const row = (overrides: Partial<AdminSupplierRow> = {}): AdminSupplierRow => ({
  id: 'sup-a',
  name: 'تأمین‌کننده الف',
  rating: 4.5,
  totalProducts: 12,
  deliveryScore: 92,
  warehouseCity: 'تهران',
  warehouseCityConfirmed: true,
  approvalStatus: 'approved',
  signedContractVersion: 2,
  defaultMarkupType: 'percent',
  defaultMarkupValue: 20,
  tapinShopId: 'shop-1',
  tapinConnected: true,
  description: 'تأمین‌کنندهٔ لوازم خانگی',
  badge: 'برگزیده',
  approvedBy: 'admin-1',
  createdAt: '2026-10-01T10:00:00Z',
  supportsDirectShipping: true,
  ...overrides,
});

const detail = (overrides: Partial<SupplierDetail> = {}): SupplierDetail => ({
  supplier: { ...row(), platformContractVersion: 3, owner: { userId: 'user-owner-1', fullName: 'مالک الف', mobile: '09120000001', email: 'owner@example.com', isActive: true } },
  counts: {
    products: 12,
    activeProducts: 10,
    variants: 34,
    proposals: { pending: 2, approved: 5, rejected: 1 },
    orders: { paid: 7, directShipmentsByStatus: { shipped: 3, delivered: 2 } },
  },
  warehouses: [{ id: 'swh-1', name: 'انبار اصفهان', provinceName: 'اصفهان', cityName: 'اصفهان', isActive: true }],
  members: [{ id: 'mem-1', fullName: 'عضو تیم الف', username: 'member1', mobile: '09120000003', roleKey: 'manager', isActive: true }],
  shippingMethods: [{ supplierId: 'sup-a', method: 'direct', status: 'enabled' }],
  tapin: { shopId: 'shop-1', connected: true },
  finance: {
    currencyUnit: 'TOMAN',
    balance: { earned: 200000, reserved: 50000, paid: 0, returnCharges: 0, returnClawbacks: 0, adjustments: 0, storageFees: 0, available: 150000 },
    settlements: [{ id: 'pay-1', amount: 50000, status: 'pending', created_at: '2026-11-01T10:00:00Z' }],
  },
  history: [{ id: 'LOG-1', timestamp: '2026-11-02T10:00:00Z', operator: 'admin:admin-1', action: 'supplier_suspend', details: '{"reason":"تخلف از قرارداد","supplier_id":"sup-a"}' }],
  gaps: ['سابقهٔ تأیید/رد در دسترس نیست: تأیید و رد تأمین‌کننده در سوابق ثبت نمی‌شود و قابل بازیابی نیست؛ فقط تعلیق و فعال‌سازی مجدد در تاریخچه دیده می‌شود.'],
  ...overrides,
});

const cardHtml = (r: AdminSupplierRow = row()) =>
  renderToStaticMarkup(
    <AdminSupplierCardView
      supplier={r}
      toPersianDigits={toPersianDigits}
      onOpenDetail={noopSupplier}
    />,
  );

const detailHtml = (d: SupplierDetail = detail()) =>
  renderToStaticMarkup(
    <AdminSupplierDetailView
      supplier={d.supplier}
      detail={d}
      detailLoading={false}
      detailError={null}
      hasDirectShip
      busyId={null}
      toPersianDigits={toPersianDigits}
      onClose={noop}
      onRetryDetail={noop}
      onContract={noopSupplier}
      onApprove={noopId}
      onReject={noopSupplier}
      onSuspend={noopSupplier}
      onReactivate={noopId}
      onToggleDirectShip={noopId}
    />,
  );

it('card: labels the stored count as products and shows the status', () => {
  const html = cardHtml();
  assert.ok(html.includes('تعداد کالا در کاتالوگ'), 'card must label totalProducts as a product count');
  assert.ok(!html.includes('تعداد تنوع'), 'card must not call totalProducts a variant count');
  assert.ok(html.includes('تأییدشده'), 'status renders');
  assert.ok(html.includes('برگزیده'), 'badge renders');
});

it('card: exposes one native detail button', () => {
  const html = cardHtml();
  assert.equal((html.match(/<button\b/g) ?? []).length, 1);
  assert.doesNotMatch(html, /role="button"/);
  assert.match(html, /<button[^>]+aria-haspopup="dialog"[^>]+aria-label="مشاهده جزئیات تأمین‌کننده الف"/);
});

it('detail: exposes only the review actions available for the supplier status', () => {
  const actions: Record<string, string[]> = {
    pending_approval: ['تأیید تأمین‌کننده', 'رد درخواست'],
    approved: ['تعلیق تأمین‌کننده'],
    suspended: ['فعال‌سازی مجدد'],
    rejected: [],
  };
  const allActions = Object.values(actions).flat();
  for (const [approvalStatus, available] of Object.entries(actions)) {
    const html = detailHtml(detail({ supplier: row({ approvalStatus }) }));
    for (const label of allActions) {
      assert.equal(html.includes(`>${label}</button>`), available.includes(label), `${approvalStatus} ${label}`);
    }
  }
});

it('detail modal: owner, computed counts, warehouses, finance in Toman', () => {
  const html = detailHtml();
  assert.ok(html.includes('تأمین‌کنندهٔ لوازم خانگی'), 'description renders');
  assert.ok(html.includes('سود پیش‌فرض توافق'), 'markup renders');
  assert.ok(html.includes('٪۲۰'), 'percent markup renders with Persian digits');
  assert.ok(html.includes('تأییدکننده'), 'approvedBy renders');
  assert.ok(html.includes('مالک الف'), 'owner name renders');
  assert.ok(html.includes('09120000001'), 'owner mobile renders');
  assert.ok(html.includes('تنوع کالا'), 'variants tile renders');
  assert.ok(html.includes('۳۴'), 'computed variant count (34, not the stored total) renders');
  assert.ok(html.includes('انبار اصفهان'), 'warehouse renders');
  assert.ok(html.includes('عضو تیم الف'), 'member renders');
  assert.ok(html.includes('مالی (تومان)'), 'finance section labels its unit');
  assert.ok(html.includes('۱۵۰٬۰۰۰ تومان'), 'available balance renders in Toman');
  assert.ok(html.includes('متصل'), 'tapin connected renders');
  assert.ok(html.includes('نسخهٔ جاری پلتفرم'), 'platform contract version renders');
  assert.ok(html.includes('۳'), 'platform contract version 3 renders');
  assert.ok(html.includes('supplier_suspend'), 'history action renders');
  assert.ok(html.includes('تخلف از قرارداد'), 'history reason renders');
  assert.ok(html.includes('سابقهٔ تأیید/رد در دسترس نیست'), 'approve/reject audit gap is stated honestly');
  assert.ok(html.includes('مقادیر ثبت‌شده در پرونده‌اند'), 'stored-vs-computed note renders');
});

it('detail modal: honest empty states when sections are empty', () => {
  const empty = detail({
    warehouses: [],
    members: [],
    shippingMethods: [],
    history: [],
    finance: { currencyUnit: 'TOMAN', balance: null, settlements: [] },
    supplier: { ...row(), owner: null },
  });
  const html = detailHtml(empty);
  assert.ok(html.includes('انباری ثبت نشده است'), 'empty warehouses state');
  assert.ok(html.includes('عضو دیگری ثبت نشده است'), 'empty members state');
  assert.ok(html.includes('روش ارسالی ثبت نشده است'), 'empty shipping-methods state');
  assert.ok(html.includes('رویداد مدیریتی ثبت‌شده‌ای'), 'empty history state');
  assert.ok(html.includes('موجودی در دسترس نیست'), 'null balance state');
  assert.ok(html.includes('تسویه‌ای ثبت نشده است'), 'empty settlements state');
  assert.ok(html.includes('مالکی برای این رکورد ثبت نشده است'), 'ownerless central record state');
});

it('detail modal: loading and error states', () => {
  const loading = renderToStaticMarkup(
    <AdminSupplierDetailView
      supplier={row()}
      detail={null}
      detailLoading
      detailError={null}
      hasDirectShip={false}
      busyId={null}
      toPersianDigits={toPersianDigits}
      onClose={noop}
      onRetryDetail={noop}
      onContract={noopSupplier}
      onApprove={noopId}
      onReject={noopSupplier}
      onSuspend={noopSupplier}
      onReactivate={noopId}
      onToggleDirectShip={noopId}
    />,
  );
  assert.ok(loading.includes('در حال دریافت جزئیات'), 'loading state renders');
  const failed = renderToStaticMarkup(
    <AdminSupplierDetailView
      supplier={row()}
      detail={null}
      detailLoading={false}
      detailError="خطا در دریافت جزئیات تأمین‌کننده"
      hasDirectShip={false}
      busyId={null}
      toPersianDigits={toPersianDigits}
      onClose={noop}
      onRetryDetail={noop}
      onContract={noopSupplier}
      onApprove={noopId}
      onReject={noopSupplier}
      onSuspend={noopSupplier}
      onReactivate={noopId}
      onToggleDirectShip={noopId}
    />,
  );
  assert.ok(failed.includes('تلاش دوباره'), 'error state offers a retry');
});

it('formatMarkup: percent, fixed, missing value', () => {
  assert.equal(formatMarkup('percent', 20, toPersianDigits), '٪۲۰');
  assert.equal(formatMarkup('fixed', 50000, toPersianDigits), '۵۰٬۰۰۰ تومان');
  assert.equal(formatMarkup('percent', null, toPersianDigits), null);
  assert.equal(formatMarkup(null, null, toPersianDigits), null);
});

it('auditHistoryReason: extracts reason, tolerates bad JSON', () => {
  assert.equal(auditHistoryReason('{"reason":"تخلف","supplier_id":"sup-a"}'), 'تخلف');
  assert.equal(auditHistoryReason('{"supplier_id":"sup-a"}'), null);
  assert.equal(auditHistoryReason('not json'), null);
});
