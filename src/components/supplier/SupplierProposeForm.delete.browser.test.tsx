import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const thumbnails = '.group > img[alt^="تصویر "]';

test('uploaded image delete button is visible and tappable without hover at 390px', async () => {
  await visit('/', 390, async page => {
    // Reuse the harness bundle and styles, installing list mocks in the same
    // script task as supplier bootstrap so auth restoration cannot race them.
    const assets = await page.evaluate(() => ({
      script: [...document.scripts].map(script => script.textContent).join('\n'),
      styles: [...document.querySelectorAll('style')].map(style => style.textContent).join('\n'),
    }));
    await page.goto(`http://batch8.test/fixture?route=${encodeURIComponent('/supplier?supplier')}`, { waitUntil: 'domcontentloaded' });
    await page.addStyleTag({ content: assets.styles });
    const installListMocks = () => {
      const fixtureFetch = window.fetch;
      window.fetch = async (input, init) => {
        const path = new URL(String(input), location.origin).pathname;
        const listPaths = ['products', 'products/proposals', 'warranties', 'inbounds',
          'discrepancies', 'settlement/history', 'price-increases'];
        if (listPaths.some(endpoint => path === `/api/v1/supplier/${endpoint}`)) {
          return Response.json([]);
        }
        return fixtureFetch(input, init);
      };
    };
    await page.addScriptTag({ content: `${assets.script}\n;(${installListMocks.toString()})();` });
    // Dismiss the contract update using the fixture's mocked API only.
    await page.waitForSelector('input[type="checkbox"]');
    await page.click('input[type="checkbox"]');
    await clickText(page, 'قبول دارم، ادامه بده');
    await clickText(page, 'کالاها و تنوع‌ها');
    await clickText(page, 'پیشنهاد کالای جدید');
    await page.waitForFunction(selector => {
      const images = [...document.querySelectorAll<HTMLImageElement>(selector)];
      return images.length === 2 && images.every(image => image.complete && image.naturalWidth > 0);
    }, {}, thumbnails);
    const originalImages = await page.$$eval(thumbnails, images => images.map(image => image.getAttribute('src')));
    assert.notEqual(originalImages[0], originalImages[1]);

    // Inspect before any pointer movement over the thumbnails.
    const buttons = await page.$$eval('button[title="حذف"]', elements => elements.map(button => {
      const rect = button.getBoundingClientRect();
      return {
        opacity: getComputedStyle(button).opacity,
        width: rect.width,
        height: rect.height,
        label: button.getAttribute('aria-label'),
      };
    }));
    assert.equal(buttons.length, 2);
    for (const button of buttons) {
      assert.equal(button.opacity, '1');
      assert.ok(button.width >= 32 && button.height >= 32, `delete target is ${button.width}×${button.height}px`);
    }
    assert.deepEqual(buttons.map(button => button.label), ['حذف تصویر ۱', 'حذف تصویر ۲']);

    await page.click('button[aria-label="حذف تصویر ۲"]');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 1, {}, thumbnails);
    assert.deepEqual(await page.$$eval(thumbnails, images => images.map(image => image.getAttribute('src'))), [originalImages[0]]);
  });
});
