import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

for (const { item, title, name } of [
  { item: 'flat', title: 'دستبند طلا ۱۸ عیار', name: 'order detail renders a raw live order title' },
  { item: 'missing', title: 'کالای سفارشی', name: 'order detail renders a fallback for a title-less item' },
]) {
  test(name, () => visit('/', 390, async page => {
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(String(error)));
    await page.waitForFunction(() => typeof (window as any).batch8Fixture?.showOrder === 'function');
    await page.evaluate(kind => (window as any).batch8Fixture.showOrder(0, true, kind), item);
    await page.waitForFunction(expected =>
      Array.from(document.querySelectorAll('#fixture-order span.truncate'))
        .some(span => span.textContent === expected), {}, title);
    assert.deepEqual(errors, []);
  }));
}

test('seller order preview shows live titles and uses the fallback for a deleted product', () =>
  visit('/?panel=seller&tab=orders&seller&fixture-orders', 390, async page => {
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(String(error)));
    await page.waitForFunction(() =>
      document.body.innerText.includes('دستبند طلا ۱۸ عیار — ساده') &&
      document.body.innerText.includes('کالای سفارشی'));
    await clickText(page, 'دیدن جزئیات');
    await page.waitForFunction(() => document.body.innerText.includes('جزئیات کامل سفارش'));
    assert.match(await page.$eval('body', body => body.innerText), /حفظ حریم خصوصی/);
    assert.deepEqual(errors, []);
  }));
