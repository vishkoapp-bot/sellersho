import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const customer = 'access-fixture-role=customer&access-fixture-caps=customer';
const overlay = 'dialog.central-portal[open]';

test('help pages have a route-specific title and a shareable active link', () =>
  visit('/terms', 390, async page => {
    const current = '.central-info-tabs a[aria-current="page"]';
    await page.waitForSelector(current, { visible: true });
    assert.equal(await page.$eval(current, link => link.getAttribute('href')), '/terms');
    assert.equal(await page.$eval('#main-content h1', title => title.textContent?.trim()), 'terms');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 390);
  }));

test('checkout names its next step and keeps the delivery form within a narrow viewport', () =>
  visit(`/product/batch8-product-0?${customer}`, 320, async page => {
    await clickText(page, 'افزودن به سبد');
    await clickText(page, 'مشاهدهٔ سبد خرید');
    const cart = '[data-checkout-scope="central"]';
    await page.waitForSelector(cart, { visible: true });
    await clickText(page, 'ادامه به نشانی و ارسال', `${cart} button`);
    await page.waitForSelector('#cart-checkout-full-name', { visible: true });
    const layout = await page.$eval(cart, element => {
      const box = element.getBoundingClientRect();
      return { width: box.width, contentWidth: element.scrollWidth, step: element.querySelector('[aria-current="step"]')?.textContent?.trim() };
    });
    assert.equal(layout.width, 320);
    assert.equal(layout.contentWidth, 320);
    assert.match(layout.step ?? '', /نشانی و ثبت/);

    // The lazily loaded cart must keep the delivery draft between openings.
    const nameField = '#cart-checkout-full-name';
    await page.$eval(nameField, input => (input as HTMLInputElement).select());
    await page.type(nameField, 'Test Recipient');
    await page.keyboard.press('Escape');
    await page.waitForSelector(cart, { hidden: true });
    await page.click('.central-bottom-nav button[aria-label*="سبد خرید"]');
    await clickText(page, 'ادامه به نشانی و ارسال', `${cart} button`);
    await page.waitForSelector(nameField, { visible: true });
    assert.equal(await page.$eval(nameField, input => (input as HTMLInputElement).value), 'Test Recipient');
  }));

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
