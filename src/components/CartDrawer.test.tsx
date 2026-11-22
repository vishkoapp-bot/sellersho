import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, waitText } from '../../scripts/fixtures/catalog/batch8-browser';
import { formatPrice } from '../utils/formatters';

const visit = batch8Browser();
for (const fare of [0, 15000]) {
  test(`checkout and order views display an explicit shipping fare of ${fare} toman`, async () => {
    const expected = `${formatPrice(fare)} تومان`;
    await visit('/', 390, async page => {
      await page.waitForFunction(() => (window as any).batch8Fixture?.state?.().productCount === 24);
      await page.evaluate(cost => (window as any).batch8Fixture.openCheckout(cost), fare);
      await waitText(page, 'انبار نمونه');
      const amounts = await page.evaluate(() => [...document.querySelectorAll('span')]
        .filter(node => node.textContent === 'هزینه ارسال سفارش:' || node.textContent?.includes('محموله ۱ · از انبار نمونه'))
        .map(node => node.nextElementSibling?.textContent?.trim()));
      assert.ok(amounts.length >= 2);
      assert.ok(amounts.every(value => value === expected));
      assert.doesNotMatch(await page.$eval('body', body => body.innerText), /رایگان/);

      await page.evaluate(() => (window as any).batch8Fixture.showCheckoutError());
      await waitText(page, 'خطای آزمایشی ثبت سفارش');
      assert.ok(await page.$eval('.fixed.top-4.z-50', node => node.textContent?.includes('خطای آزمایشی ثبت سفارش')));
    });
    for (const detail of [false, true]) {
      await visit('/', 390, async page => {
        await page.waitForFunction(() => typeof (window as any).batch8Fixture?.showOrder === 'function');
        await page.evaluate((cost, inDetail) => (window as any).batch8Fixture.showOrder(cost, inDetail), fare, detail);
        await page.waitForSelector('#fixture-order');
        const amounts = await page.$$eval('#fixture-order span', nodes => nodes
          .filter(node => node.textContent?.trim() === 'هزینه ارسال:')
          .map(node => node.nextElementSibling?.textContent?.trim()));
        assert.deepEqual(amounts, [expected]);
        assert.doesNotMatch(await page.$eval('#fixture-order', order => (order as HTMLElement).innerText), /رایگان/);
      });
    }
  });
}
