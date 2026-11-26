import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });

test('connected commission keeps the recorded report and price reviews and hides demo settings', { timeout: 60000 }, () =>
  visit('/admin?admin=super', 320, async page => {
    await page.waitForSelector('#admin-main');
    await page.evaluate(() => {
      const originalFetch = window.fetch.bind(window);
      let pending = true;
      window.fetch = async (input, options) => {
        const { pathname, searchParams } = new URL(String(input), location.href);
        if (pathname === '/api/v1/finance/treasury/commission') {
          return Response.json({
            success: true, days: Number(searchParams.get('days')), total: 25000,
            releasedTotal: 20000, unreleasedTotal: 5000, orderCount: 1, trend: [],
          });
        }
        if (pathname === '/api/v1/admin/price-increases') {
          return Response.json(pending ? [{
            id: 'review-1', productId: 'product-fixture', supplierId: 'supplier-fixture',
            productTitle: 'کالای نمونه افزایش قیمت', supplierName: 'تأمین‌کننده نمونه', oldReference: 100000,
            newReference: 120000, pct: 20, status: 'pending', isException: false,
            autoApproved: false, agreementReview: false, createdAt: '2026-11-22',
          }] : []);
        }
        if (pathname === '/api/v1/admin/price-increases/review-1/review' && options?.method === 'POST') {
          if (JSON.parse(String(options.body)).approved !== true) return new Response(null, { status: 400 });
          pending = false;
          return Response.json({ success: true, id: 'review-1', status: 'approved' });
        }
        return originalFetch(input, options);
      };
    });
    await page.click('header button[aria-label="باز کردن منوی پنل"]');
    const section = 'dialog[open] a[data-section="commission"]';
    await page.waitForSelector(section);
    await page.$eval(section, link => link.closest('details')?.querySelector('summary')?.click());
    await page.click(section);
    await waitText(page, 'کالای نمونه افزایش قیمت');
    await waitText(page, '۲۵٬۰۰۰');
    assert.doesNotMatch(await page.$eval('#admin-main', main => main.textContent ?? ''), /ذخیره قواعد مالی|تعریف پروموشن جدید|نسخه‌بندی و متن قراردادها/);
    await clickText(page, 'تأیید و اعمال');
    await waitText(page, 'درخواستی در انتظار بررسی نیست.');
  }));
