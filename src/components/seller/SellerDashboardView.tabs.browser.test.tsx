import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const stripSelector = '[aria-label="زبانه‌های پنل فروشنده"]';

test('active seller tab is scrolled fully into view at 390px', async () => {
  await visit('/?panel=seller&tab=discount-codes&seller', 390, async page => {
    await page.waitForSelector(stripSelector, { visible: true });
    const assertNoOverflow = async () => {
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= 390),
        'the page must not overflow horizontally');
    };
    const waitForActiveTab = async (label: string) => {
      await page.waitForFunction((selector, text) => {
        const strip = document.querySelector(selector);
        const button = [...(strip?.querySelectorAll('button') ?? [])]
          .find(node => node.textContent?.trim() === text);
        if (!strip || !button || !button.classList.contains('bg-brand')) return false;
        const outer = strip.getBoundingClientRect();
        const inner = button.getBoundingClientRect();
        return inner.width > 0 && inner.left >= outer.left && inner.right <= outer.right
          && inner.top >= outer.top && inner.bottom <= outer.bottom;
      }, {}, stripSelector, label);
      await assertNoOverflow();
    };

    await assertNoOverflow();
    await waitForActiveTab('کدهای تخفیف');
    assert.notEqual(await page.$eval(stripSelector, node => getComputedStyle(node).maskImage), 'none');

    // DOM clicks avoid Puppeteer's automatic scrolling masking a missing tab effect.
    for (const label of ['پشتیبانی', 'نمای کلی']) {
      await clickText(page, label, `${stripSelector} button`);
      await assertNoOverflow();
      await waitForActiveTab(label);
    }
  });
});
