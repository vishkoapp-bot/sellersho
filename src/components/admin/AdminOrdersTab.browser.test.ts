import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('mobile admin order tracking searches filtered results, pages, and opens recorded parcel details', () =>
  visit('/admin/orders?admin&fixture-admin-orders', 360, async page => {
    try {
      await page.waitForFunction(() => document.body.innerText.includes('ORD-123'), { timeout: 10000 });
    } catch (cause) {
      const content = await page.$eval('body', body => body.innerText.slice(0, 500));
      throw new Error(`Admin order fixture did not load at ${page.url()}. Page content: ${content}`, { cause });
    }
    await clickText(page, 'سفارش‌های بیشتر');
    await waitText(page, 'ORD-456');
    assert.match(await page.$eval('main', main => main.innerText), /ORD-123/);

    await page.select('#admin-orders-status', 'preparing');
    await fillInput(page, '#admin-orders-search', '۷۱۲۳۴۵');
    await clickText(page, 'جست‌وجو');
    const button = await page.waitForSelector('button[aria-label="جزئیات رهگیری سفارش ORD-712"]');
    assert.ok(button);
    await button.click();
    await page.waitForSelector('dialog[open]');
    const details = await page.$eval('dialog', dialog => dialog.innerText);
    assert.match(details, /بسته‌بندی‌شده/);
    assert.match(details, /59999999/);
    assert.match(details, /712345/);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 360);

    await page.keyboard.press('Escape');
    await page.waitForSelector('dialog', { hidden: true });
    assert.equal(await button.evaluate(element => document.activeElement === element), true);
    assert.doesNotMatch(await page.$eval('main', main => main.innerText), /ORD-456/);
  }));
