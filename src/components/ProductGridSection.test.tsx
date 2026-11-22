import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('homepage separates catalog total, loaded count, and remaining server count', async () => {
  await visit('/', 390, async page => {
    await waitText(page, '(۴۱ کالا)');
    await waitText(page, 'مشاهده همه کالاهای بارگذاری‌شده (۲۴ کالا)');
    await waitText(page, 'نمایش بیشتر (۱۷ کالای دیگر)');
    await clickText(page, 'فقط کالای اصل');
    await waitText(page, '(۳ از ۴۱ کالا)');
    await waitText(page, 'نمایش بیشتر (۱۷ کالای دیگر)');
    assert.doesNotMatch(await page.$eval('body', page => page.innerText), /نمایش بیشتر \(۳۸|نمایش بیشتر \(۴۱/);
  });
});

test('homepage labels a single remaining product and omits load-more at the end', async () => {
  await visit('/?total=25', 390, async page => {
    await waitText(page, 'نمایش بیشتر (۱ کالای دیگر)');
  });
  await visit('/?total=24', 390, async page => {
    await waitText(page, '(۲۴ کالا)');
    assert.doesNotMatch(await page.$eval('body', page => page.innerText), /نمایش بیشتر/);
  });
});
