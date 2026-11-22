import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sellerMoneyBrowserFixture } from './sellerMoneyBrowserFixture';

const withPage = sellerMoneyBrowserFixture('analytics');

test('analytics waits for a store, ignores order refreshes, and drops a superseded response', () => withPage('&noStore', async page => {
  assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.analytics()), []);
  await page.evaluate(() => (window as any).sellerMoney.select('a'));
  await page.waitForFunction(() => (window as any).sellerMoney.analytics().length === 1);
  await page.evaluate(async () => {
    (window as any).sellerMoney.refreshOrders();
    await new Promise(requestAnimationFrame);
  });
  assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.analytics()), [{ storeId: 'a', aborted: false }]);
  await page.evaluate(() => (window as any).sellerMoney.select('b'));
  await page.waitForFunction(() => (window as any).sellerMoney.analytics().length === 2);
  assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.analytics()), [
    { storeId: 'a', aborted: true }, { storeId: 'b', aborted: false },
  ]);
  await page.evaluate(() => (window as any).sellerMoney.resolveAnalytics(1, 402800));
  await page.waitForFunction(() => document.body.textContent?.includes('۴۰۲٬۸۰۰'));
  await page.evaluate(async () => {
    (window as any).sellerMoney.resolveAnalytics(0, 403800);
    await new Promise(requestAnimationFrame);
  });
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /مجموع فروش \(همهٔ زمان\).*۴۰۲٬۸۰۰/);
  assert.match(text ?? '', /تعداد سفارش‌ها \(همهٔ زمان\)۳/);
  assert.match(text ?? '', /وضعیت سفارش‌های پرداخت‌شده \(همهٔ زمان\).*۳/);
  assert.match(text ?? '', /بازدید و تبدیل — ۳۰ روز اخیر/);
  assert.doesNotMatch(text ?? '', /۴۰۳٬۸۰۰/);
}));
