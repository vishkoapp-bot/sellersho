import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const customer = 'access-fixture-role=customer&access-fixture-caps=customer';
const overlay = 'dialog.central-portal[open]';

test('central notifications on narrow screens match the storefront and return focus on Escape', () =>
  visit(`/?${customer}`, 320, async page => {
    const bell = 'header button[title="اعلان‌ها"]';
    await page.waitForSelector(bell, { visible: true });
    await page.click(bell);
    await page.waitForSelector(overlay, { visible: true });
    const layout = await page.$eval(overlay, dialog => ({
      background: getComputedStyle(dialog).backgroundColor,
      title: document.getElementById(dialog.getAttribute('aria-labelledby')!)?.textContent?.trim(),
      insideViewport: dialog.getBoundingClientRect().right <= innerWidth,
    }));
    assert.deepEqual(layout, { background: 'rgb(248, 246, 239)', title: 'اعلان‌ها', insideViewport: true });
    await page.keyboard.press('Escape');
    await page.waitForSelector(overlay, { hidden: true });
    await page.waitForFunction(selector => document.activeElement?.matches(selector), {}, bell);
  }));

test('central buy-now addresses keep validation visible in the mobile dialog', () =>
  visit(`/product/batch8-product-0?${customer}`, 320, async page => {
    const buy = '.central-pdp-actions button:last-child';
    await page.waitForSelector(buy, { visible: true });
    await page.click(buy);
    await page.waitForSelector(overlay, { visible: true });
    assert.equal(await page.$eval(overlay, dialog => getComputedStyle(dialog).backgroundColor), 'rgb(248, 246, 239)');
    await clickText(page, 'افزودن آدرس جدید', `${overlay} button`);
    const namedField = await page.$eval(`${overlay} label`, label => (label as HTMLLabelElement).control?.tagName);
    assert.equal(namedField, 'INPUT');
    await clickText(page, 'ذخیره آدرس و انتخاب آن', `${overlay} button`);
    assert.equal(await page.$eval(`${overlay} [role="alert"]`, alert => alert.textContent), 'نام گیرنده را وارد کنید.');
    await page.keyboard.press('Escape');
    await page.waitForSelector(overlay, { hidden: true });
    await page.waitForFunction(selector => document.activeElement?.matches(selector), {}, buy);
  }));
