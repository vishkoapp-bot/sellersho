import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const headerSelector = 'header[data-storefront-theme="tpl-electronic-1"]';
const railSelector = 'div:has(> div.snap-x > a[href="#products"])';

for (const width of [390, 1280]) {
  test(`category chip rail sticks below the storefront header at ${width}px`, async () => {
    await visit('/store/batch-eight-store', width, async page => {
      await page.waitForSelector(headerSelector, { visible: true });
      await page.waitForSelector(railSelector, { visible: true });
      await page.evaluate(() => window.scrollBy(0, 600));
      await page.waitForFunction(() => window.scrollY >= 600);
      const layout = await page.evaluate((headerQuery, railQuery) => {
        const header = document.querySelector(headerQuery)!;
        const rail = document.querySelector(railQuery)!;
        const button = [...header.querySelectorAll('button')].find(node => node.getClientRects().length)!;
        const headerRect = header.getBoundingClientRect();
        const railRect = rail.getBoundingClientRect();
        const buttonRect = button.getBoundingClientRect();
        const hit = document.elementFromPoint(
          buttonRect.left + buttonRect.width / 2,
          buttonRect.top + buttonRect.height / 2,
        );
        return {
          headerBottom: headerRect.bottom,
          railTop: railRect.top,
          railBottom: railRect.bottom,
          viewportHeight: window.innerHeight,
          position: getComputedStyle(rail).position,
          buttonClickable: hit !== null && button.contains(hit),
        };
      }, headerSelector, railSelector);
      assert.ok(layout.railTop >= layout.headerBottom - 1,
        `rail top ${layout.railTop} overlaps header bottom ${layout.headerBottom}`);
      assert.equal(layout.position, 'sticky');
      assert.ok(layout.railBottom <= layout.viewportHeight, 'rail remains visible after scrolling');
      assert.ok(layout.buttonClickable, 'header first button is reachable at its centre');
    });
  });
}
