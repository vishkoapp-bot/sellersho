import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sellerMoneyBrowserFixture } from './sellerMoneyBrowserFixture';

const withPage = sellerMoneyBrowserFixture('wallet');

test('wallet retains the authoritative withdrawable figure and labels the release rule', () => withPage('', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('تفکیک موجودی کیف پول'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /پولی که همین حالا می‌توانی برداشت کنی۵۲٬۸۰۰/);
  assert.match(text ?? '', /سود آزادشده پس از پایان مهلت ۷ روزهٔ مرجوعی/);
  assert.match(text ?? '', /سود سفارش‌های در راه۱۲٬۰۰۰ تومان/);
  assert.match(text ?? '', /سود آزادشده قابل برداشت۶۳٬۰۰۰ تومان/);
}));

test('wallet explicitly labels the local pending value if the breakdown read fails', () => withPage('&breakdownFailure', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('۹۰۰'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /سود سفارش‌های در راه \(برآورد محلی\):۹۰۰/);
  assert.doesNotMatch(text ?? '', /تفکیک موجودی کیف پول/);
}));
