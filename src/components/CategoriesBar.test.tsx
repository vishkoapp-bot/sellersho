import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('category tiles keep a space and a layout gap between count and unit', async () => {
  await visit('/', 390, async page => {
    const selector = '[data-testid="category-item"] p .inline-flex';
    await page.waitForFunction(query => document.querySelector(query)?.textContent?.includes('۲ کالا'), {}, selector);
    const count = await page.$eval(selector, node => ({
      text: node.textContent,
      items: node.children.length,
      gap: parseFloat(getComputedStyle(node).columnGap),
    }));
    assert.equal(count.text, '۲ کالا');
    assert.equal(count.items, 2);
    assert.ok(count.gap > 0);
  });
});
