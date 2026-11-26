import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';
import { adminNavigationFor } from './AdminNavigation';

const visit = batch8Browser({ entryPoint: 'scripts/fixtures/catalog/admin-mobile.provider.tsx', inlineVazirmatn: true });
const sections = adminNavigationFor('super_admin').flatMap(group => group.items);
const menuButton = 'header button[aria-label="باز کردن منوی پنل"]';
const fixtureReference = 'FIXTURE-REFERENCE-' + '7'.repeat(90);

const hasOverflow = (page: Page) => page.evaluate(() => {
  const main = document.querySelector('#admin-main')!;
  return main.scrollWidth - main.clientWidth > 1 || document.documentElement.scrollWidth > innerWidth;
});

test('every admin section opens from the mobile menu and fits at 320px and 390px', { timeout: 180000 }, () =>
  visit('/admin?admin=super', 320, async page => {
    await page.waitForSelector('#admin-main');
    const issues: string[] = [];
    for (const width of [320, 390]) {
      await page.setViewport({ width, height: 760 });
      for (const { id, label } of sections) {
        await page.evaluate(() => { delete window.__sellershoLastError; });
        await page.tap(menuButton);
        const link = `dialog[open] a[data-section="${id}"]`;
        await page.waitForSelector(link);
        await page.$eval(link, item => {
          const group = item.closest('details');
          if (group && !group.open) group.querySelector('summary')?.click();
          item.scrollIntoView({ block: 'nearest' });
        });
        await page.tap(link);
        await page.waitForSelector(`aside a[data-section="${id}"][aria-current="page"]`);
        assert.equal(await page.$eval('#admin-page-title', heading => heading.textContent), label);
        await page.evaluate(async () => { await document.fonts.ready; });
        const renderError = await page.evaluate(() => window.__sellershoLastError?.message);
        if (renderError) issues.push(`${width}px/${id} render error: ${renderError}`);
        if (await hasOverflow(page)) issues.push(`${width}px/${id}`);
      }
    }
    assert.deepEqual(issues, [], 'admin sections that extend beyond the viewport');
  }));

test('failed callback history can be reloaded and long gateway references wrap on a phone', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=finance-callback-retries&fail-first', 320, async page => {
    await waitText(page, 'خطای نمونه هنگام دریافت پرداخت‌های ناموفق');
    await page.waitForSelector('#admin-main [role="alert"]', { timeout: 2500 });
    await clickText(page, 'تلاش دوباره');
    await waitText(page, fixtureReference);
    assert.equal(await hasOverflow(page), false);
    assert.equal(await page.$('#admin-main [role="alert"]'), null);
  }));

test('warehouse capacity load can be retried and a failed update is reported as a failure', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=warehouses&fail-first', 320, async page => {
    await waitText(page, 'ظرفیت واقعی جایگاه‌ها');
    await page.waitForSelector('#admin-main [role="alert"]', { timeout: 2500 });
    await clickText(page, 'تلاش دوباره');
    await waitText(page, 'MOBILE-BIN-01');
    assert.equal(await hasOverflow(page), false);
    await clickText(page, 'ذخیره ظرفیت');
    await waitText(page, 'خطای نمونه هنگام ذخیره ظرفیت');
    assert.match(await page.$eval('#admin-main [role="alert"]', element => element.textContent ?? ''), /خطای نمونه هنگام ذخیره ظرفیت/);
  }));

test('storage fee calculation requires loaded settings and explicit confirmation', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=storage-fee-settings&fail-first', 320, async page => {
    await waitText(page, 'خطای نمونه هنگام دریافت تعرفه‌ها');
    assert.equal(await page.evaluate(() => [...document.querySelectorAll<HTMLButtonElement>('#admin-main button')]
      .find(node => node.textContent?.trim() === 'اجرای محاسبه هزینه انبارداری')?.disabled), true);
    await clickText(page, 'تلاش دوباره');
    await page.waitForFunction(() => [...document.querySelectorAll<HTMLButtonElement>('#admin-main button')]
      .some(node => node.textContent?.trim() === 'اجرای محاسبه هزینه انبارداری' && !node.disabled));
    await clickText(page, 'اجرای محاسبه هزینه انبارداری');
    await page.waitForSelector('dialog[open]', { visible: true });
    assert.equal(await page.evaluate(() => (window as any).adminMobileFixture.storageRuns.length), 0);
    await page.keyboard.press('Escape');
    await page.waitForSelector('dialog[open]', { hidden: true });
    assert.equal(await page.evaluate(() => (window as any).adminMobileFixture.storageRuns.length), 0);
    await clickText(page, 'اجرای محاسبه هزینه انبارداری');
    await clickText(page, 'تأیید و اجرای محاسبه', 'dialog[open] button');
    await waitText(page, 'محاسبه هزینه انبارداری اجرا شد.');
    assert.equal(await page.evaluate(() => (window as any).adminMobileFixture.storageRuns.length), 1);
  }));

test('the box editor waits for a successful catalogue read before enabling writes', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=warehouses&fail-tapin', 320, async page => {
    await waitText(page, 'خطای نمونه هنگام دریافت کاتالوگ جعبه‌ها');
    const saveText = 'ذخیره کاتالوگ جعبه‌ها';
    assert.doesNotMatch(await page.$eval('#admin-main', main => main.textContent ?? ''), new RegExp(saveText));
    await clickText(page, 'تلاش دوباره');
    const title = await page.waitForFunction(() => [...document.querySelectorAll<HTMLInputElement>('#admin-main input')]
      .some(input => input.value === 'جعبه نمونه'));
    assert.ok(title);
    assert.equal(await hasOverflow(page), false);
    assert.match(await page.$eval('#admin-main', main => main.textContent ?? ''), new RegExp(saveText));
  }));

test('connected payouts show real queue failures and hide illustrative transfers', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=finance-payouts&fail-first', 390, async page => {
    await page.waitForSelector('#admin-main');
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /PAY-701|PAY-702/);
    await page.waitForSelector('#admin-main [role="alert"]', { timeout: 2500 });
    await clickText(page, 'تلاش دوباره');
    await waitText(page, 'گیرنده یک');
    assert.equal(await hasOverflow(page), false);
  }));

test('a pending transfer keeps its bank reference attached to the chosen recipient', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=finance-payouts', 320, async page => {
    await waitText(page, 'گیرنده یک');
    await clickText(page, 'ثبت انتقال بانکی');
    const input = '#admin-main input[placeholder="شماره پیگیری انتقال بانکی"]';
    await fillInput(page, input, 'REF-FIRST');
    await clickText(page, 'تأیید واریز و ثبت سند');
    await page.waitForFunction(() => (window as any).adminMobileFixture.pendingTransfers.length === 1);
    await clickText(page, 'ثبت انتقال بانکی');
    assert.equal(await page.$eval(input, node => (node.closest('article') ?? node.parentElement?.parentElement)?.textContent?.includes('گیرنده یک')), true);
    await page.evaluate(() => (window as any).adminMobileFixture.finishTransfer());
    await waitText(page, 'خطای نمونه هنگام ثبت واریز');
    assert.equal(await page.$eval(input, node => node.value), 'REF-FIRST');
    await clickText(page, 'ثبت انتقال بانکی');
    assert.equal(await page.$eval(input, node => node.value), '');
  }));

test('finance specialists can read approved payouts without a transfer confirmation control', { timeout: 60000 }, () =>
  visit('/admin?admin=finance&section=finance-payouts', 390, async page => {
    await waitText(page, 'گیرنده یک');
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /ثبت انتقال بانکی/);
  }));

test('the live anti-fraud report does not show the sample widget publishing controls', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=ad-network', 320, async page => {
    await waitText(page, 'کمپین نمونه');
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /تعریف بنر آماده جدید/);
    const reportWindow = await page.waitForSelector('select[aria-label="بازهٔ گزارش"]', { visible: true });
    assert.ok(reportWindow);
    await reportWindow.select('30');
    await waitText(page, 'کمپین نمونه');
    assert.equal(await hasOverflow(page), false);
  }));

test('the connected reconciliation view never offers a simulated Shaparak lookup', { timeout: 60000 }, () =>
  visit('/admin?admin=finance&section=finance-reconciliation', 390, async page => {
    await page.waitForSelector('#admin-main');
    assert.equal(await page.$('#admin-main form'), null);
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /TXN-88091/);
    assert.ok(await page.$('#admin-main a[href="/admin?section=finance-pending-payments"]'));
    assert.equal(await page.$('#admin-main a[href="/admin?section=finance-callback-retries"]'), null);
    await page.tap('#admin-main a[href="/admin?section=finance-pending-payments"]');
    await waitText(page, fixtureReference);
  }));

test('connected campaigns and warehouses do not offer simulated settings writes', { timeout: 60000 }, async () => {
  await visit('/admin?admin=super&section=campaigns', 320, async page => {
    await page.waitForSelector('#admin-main');
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /ذخیره بودجه|ذخیره سهمیه|حذف طرح/);
    assert.equal(await page.$('#admin-main form'), null);
  });
  await visit('/admin?admin=super&section=warehouses', 390, async page => {
    await waitText(page, 'MOBILE-BIN-01');
    assert.doesNotMatch(await page.$eval('#admin-main', element => element.textContent ?? ''), /WH-TEH-01|به‌روزرسانی تعرفه خدمات انبار/);
    await waitText(page, 'کاتالوگ پلتفرم');
  });
});

test('connected storefronts show real records and the approval inbox without local-only actions', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=stores', 320, async page => {
    await waitText(page, 'فروشگاه مرکزی');
    assert.doesNotMatch(await page.$eval('#admin-main', main => main.textContent ?? ''), /تعلیق فروشگاه|پلن: طلایی|شناسنامه ویترین|NaN/);
    await clickText(page, 'صندوق تأییدها');
    await waitText(page, 'درخواست تغییر نام ویترین نمونه');
    assert.equal(await hasOverflow(page), false);
    await clickText(page, 'تأیید و اعمال');
    await waitText(page, 'درخواستی در این صف وجود ندارد.');
  }));

test('unpaid order details and manual-payment action fit on the same phone-width card', { timeout: 60000 }, () =>
  visit('/admin?admin=super&section=finance-pending-payments', 320, async page => {
    await waitText(page, fixtureReference);
    const summary = await page.waitForSelector('#admin-main summary', { visible: true });
    assert.ok(summary);
    const geometry = await summary.evaluate(element => {
      const rect = element.getBoundingClientRect();
      return { visible: rect.left >= 0 && rect.right <= innerWidth, height: rect.height };
    });
    assert.ok(geometry.visible && geometry.height >= 44, JSON.stringify(geometry));
    await summary.tap();
    await waitText(page, 'نوع مدرک موردنیاز هنوز از طرف مالک مشخص نشده است');
    assert.equal(await hasOverflow(page), false);
    assert.equal(await page.$eval('details button[type="submit"]', button => button.disabled), true);
  }));


test('audit history displays server rows, retries failures and filters the server records on mobile', () =>
  visit('/admin?admin=super&section=audit-logs&fail-audit', 320, async page => {
    await waitText(page, 'خطای نمونه هنگام دریافت سوابق');
    await clickText(page, 'تلاش دوباره');
    await waitText(page, 'ثبت نمونه از سرور');
    assert.equal(await hasOverflow(page), false);
    await page.select('select[aria-label="دستهٔ رویداد"]', 'catalog');
    await waitText(page, 'سابقه‌ای مطابق فیلتر ثبت نشده است.');
    assert.doesNotMatch(await page.$eval('#admin-main', main => main.textContent ?? ''), /ثبت نمونه از سرور/);
  }));

test('SMS settings cannot overwrite server configuration until an acknowledged read', () =>
  visit('/admin?admin=super&section=sms&fail-sms', 320, async page => {
    await waitText(page, 'خطای نمونه هنگام دریافت تنظیمات پیامک');
    assert.equal(await page.$('#sms-settings-save'), null);
    assert.deepEqual(await page.evaluate(() => (window as any).adminMobileFixture.smsWrites), []);
    await clickText(page, 'تلاش دوباره');
    await page.waitForSelector('#sms-settings-save');
    await waitText(page, 'اعتبار در دسترس نیست');
    await page.click('#sms-settings-save');
    await waitText(page, 'تنظیمات ذخیره شد.');
    assert.deepEqual(await page.evaluate(() => (window as any).adminMobileFixture.smsWrites), [{
      enabled: true, baseUrl: 'https://provider.example', sender: '+983000505', patternCode: 'fixture-pattern',
    }]);
    assert.equal(await hasOverflow(page), false);
  }));
