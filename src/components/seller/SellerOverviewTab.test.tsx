import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sellerMoneyBrowserFixture } from './sellerMoneyBrowserFixture';

const withPage = sellerMoneyBrowserFixture('overview');

test('overview uses wallet balance and pending profit and labels the report window', () => withPage('', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('۵۲٬۸۰۰'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /سود آماده برداشت۵۲٬۸۰۰/);
  assert.match(text ?? '', /سود در راه۱۲٬۰۰۰.*مهلت مرجوعی/);
  assert.match(text ?? '', /۳۱ روز اخیر۳ سفارش · ۴ کالا/);
}));

test('overview displays an error when the balance read fails', () => withPage('&balanceFailure', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('بارگذاری مبلغ انجام نشد.'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /سود آماده برداشتبارگذاری مبلغ انجام نشد/);
  assert.doesNotMatch(text ?? '', /سود آماده برداشت۰/);
}));

test('switching stores discards the first report even if it resolves last', () => withPage('&deferReport', async page => {
  await page.waitForFunction(() => (window as any).sellerMoney.report().length === 1);
  await page.evaluate(() => (window as any).sellerMoney.select('b'));
  await page.waitForFunction(() => (window as any).sellerMoney.report().length === 2);
  assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.report()), [
    { slug: 'a', aborted: true }, { slug: 'b', aborted: false },
  ]);
  await page.evaluate(() => (window as any).sellerMoney.resolveReport(1, 20000));
  await page.waitForFunction(() => document.body.textContent?.includes('۲۰٬۰۰۰'));
  await page.evaluate(async () => {
    (window as any).sellerMoney.resolveReport(0, 10000);
    await new Promise(requestAnimationFrame);
  });
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /۲۰٬۰۰۰/);
  assert.doesNotMatch(text ?? '', /۱۰٬۰۰۰/);
}));
