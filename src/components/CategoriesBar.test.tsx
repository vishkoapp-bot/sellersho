import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('homepage category tiles link to their catalog with a readable label', async () => {
  await visit('/', 390, async page => {
    const selector = '#category a[data-testid="category-item"][href="/?category=digital"]';
    await page.waitForSelector(selector, { visible: true });
    const tile = await page.$eval(selector, node => ({
      text: node.textContent?.trim(),
      height: node.getBoundingClientRect().height,
    }));
    assert.equal(tile.text, 'کالای دیجیتال');
    assert.ok(tile.height >= 44);
  });
});
