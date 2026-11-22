import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
test('warehouse login shows staff names and leaves the mobile field empty after selecting a station', async () => {
  await visit('/', 390, async page => {
    await clickText(page, 'ورود عملیات انبار');
    await page.waitForSelector('select option');
    const labels = await page.$$eval('select option', options => options.map(option => option.textContent?.trim()));
    assert.ok(labels.some(label => label?.includes('علی کارگر')));
    assert.ok(labels.some(label => label?.includes('علیرضا حسینی')));
    assert.doesNotMatch(labels.join('\n'), /09\d{9}|\*\*\*/);
    const mobile = () => page.$eval('input[type="tel"]', node => (node as HTMLInputElement).value);
    assert.equal(await mobile(), '');
    await page.select('select', 'WH-MHD-HUB');
    assert.equal(await mobile(), '');
  });
});
