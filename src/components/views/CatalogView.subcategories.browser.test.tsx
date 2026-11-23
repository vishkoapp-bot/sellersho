import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('subcategories expand and are selectable on mobile at 390px', async () => {
  await visit('/', 390, async page => {
    const tiles = '[data-testid="category-item"]';
    const rows = '[data-testid="product-item"]';
    const chevron = 'button[aria-label="زیردسته‌های کالای دیجیتال"]';
    const chipRow = 'aside div.overflow-x-auto';
    const mobileBlock = `${chipRow} + div.lg\\:hidden`;
    const child = `${mobileBlock} button`;

    await page.waitForFunction(selector => {
      const tile = [...document.querySelectorAll(selector)].find(node => node.textContent?.includes('کالای دیجیتال'));
      return tile?.textContent?.includes('۲ کالا');
    }, {}, tiles);
    await page.$$eval(tiles, elements =>
      (elements.find(node => node.textContent?.includes('کالای دیجیتال')) as HTMLElement).click());
    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, rows);
    await page.waitForSelector(chipRow);

    const toggle = await page.$eval(chevron, element => {
      const rect = element.getBoundingClientRect();
      return { display: getComputedStyle(element).display, left: rect.left, right: rect.right,
        top: rect.top, bottom: rect.bottom, width: rect.width, height: rect.height };
    });
    assert.notEqual(toggle.display, 'none');
    assert.ok(toggle.width > 0 && toggle.height >= 40);
    assert.ok(toggle.left >= 0 && toggle.right <= 390 && toggle.top >= 0 && toggle.bottom <= 760);

    // Selecting the homepage tile already expands its category and ancestors.
    if (await page.$eval(chevron, element => element.getAttribute('aria-expanded') === 'true')) {
      await page.click(chevron);
      await page.waitForFunction(selector => document.querySelector(selector)?.getAttribute('aria-expanded') === 'false', {}, chevron);
    }
    assert.equal(await page.$(mobileBlock), null);
    await page.click(chevron);
    await page.waitForSelector(child, { visible: true });
    const expanded = await page.$eval(child, element => {
      const block = element.closest('div.lg\\:hidden')!;
      const row = block.previousElementSibling!;
      const rect = element.getBoundingClientRect();
      return { text: element.textContent?.trim(), left: rect.left, right: rect.right, width: rect.width,
        blockTop: block.getBoundingClientRect().top, rowBottom: row.getBoundingClientRect().bottom,
        overflow: document.documentElement.scrollWidth };
    });
    assert.equal(expanded.text, 'موبایل');
    assert.ok(expanded.width > 0 && expanded.left >= 0 && expanded.right <= 390);
    assert.ok(expanded.blockTop >= expanded.rowBottom);
    assert.ok(expanded.overflow <= 390);
    assert.equal(await page.$eval(chevron, element => element.getAttribute('aria-expanded')), 'true');

    await page.click(child);
    await page.waitForFunction(selector => {
      const products = document.querySelectorAll(selector);
      return products.length === 1 && products[0].textContent?.includes('کالای عمومی نمونه 2');
    }, {}, rows);
    assert.equal(await page.$$eval(rows, elements => elements.length), 1);
    assert.match(await page.$eval(rows, element => element.textContent ?? ''), /کالای عمومی نمونه 2/);
    // In-catalog selection is state-only by design: the URL is written
    // only by navigation into the catalog (openCatalogCategory), never by
    // a chip click — desktop and mobile alike. The entry URL
    // (?category=digital, from the homepage tile) therefore stays put;
    // the selection itself is proven by the product list above.
    assert.equal(new URL(page.url()).searchParams.get('category'), 'digital');
  });
});
