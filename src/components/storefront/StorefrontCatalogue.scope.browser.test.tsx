import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const products = '#products [data-testid="product-item"]';
const search = '#products input[name="q"]';

test('a storefront search finds matching products and categories beyond the first page', () =>
  visit('/store/batch-eight-store?template=modern&storefront-catalog', 360, async page => {
    await page.waitForSelector(products, { visible: true });
    const hero = await page.$eval('.showcase-hero h1', node => node.textContent);
    await fillInput(page, search, 'دوربین کمیاب');
    await page.keyboard.press('Enter');
    await waitText(page, 'دوربین کمیاب ویترین');
    assert.equal(await page.$$eval(products, nodes => nodes.length), 1);
    assert.match(await page.$eval(`${products} .product-title-link`, node => node.getAttribute('href') ?? ''),
      /^\/store\/batch-eight-store\/product\/batch8-product-10$/);
    assert.equal(await page.$eval('.showcase-hero h1', node => node.textContent), hero);

    await page.click('#products button[aria-label="پاک کردن جست‌وجو"]');
    await page.locator('#products button[data-category="camera"]').click();
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, products);
    assert.match(await page.$eval('#products [role="status"]', node => node.textContent ?? ''), /^۲ کالا$/);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 360);
  }));
