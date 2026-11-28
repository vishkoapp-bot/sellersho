import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const containerSelector = '[data-toast-stack]';
const toastSelector = `${containerSelector} > .toast-message`;

test('error toast paints above the auth modal at 390px', async () => {
  await visit('/', 390, async page => {
    await page.waitForSelector(containerSelector);
    assert.equal(await page.$eval(containerSelector, element => getComputedStyle(element).zIndex), '10000');

    const loginSelector = 'nav[aria-label="منوی پایین فروشگاه"] a[href="/account"]';
    await page.locator(loginSelector).click();
    await waitText(page, 'ورود به حساب کاربری');
    await page.waitForSelector('button[aria-label="بستن"]', { visible: true });

    const stacking = await page.evaluate(selector => {
      const container = document.querySelector(selector)!;
      return {
        toastZIndex: Number(getComputedStyle(container).zIndex),
        otherFixedZIndices: [...document.querySelectorAll('*')]
          .filter(element => element !== container)
          .map(element => getComputedStyle(element))
          .filter(style => style.position === 'fixed' && style.zIndex !== 'auto')
          .map(style => Number(style.zIndex)),
      };
    }, containerSelector);
    assert.ok(stacking.otherFixedZIndices.includes(9999), 'the auth modal overlay must be open');
    for (const zIndex of stacking.otherFixedZIndices) {
      assert.ok(stacking.toastZIndex > zIndex, `toast must paint above fixed z-index ${zIndex}`);
    }

    await page.waitForFunction(() => typeof (window as any).batch8Fixture?.toast === 'function');
    await page.evaluate(() => (window as any).batch8Fixture.toast('خطای آزمایشی', 'error'));
    await page.waitForSelector(toastSelector, { visible: true });
    await page.waitForFunction(selector => {
      const toast = document.querySelector(selector);
      return toast?.textContent?.includes('خطای آزمایشی') && getComputedStyle(toast).opacity === '1';
    }, {}, toastSelector);

    const visible = await page.$eval(toastSelector, toast => {
      const rect = toast.getBoundingClientRect();
      const dismiss = toast.querySelector('button[aria-label="بستن پیام"]');
      const buttonRect = dismiss?.getBoundingClientRect();
      const hit = buttonRect ? document.elementFromPoint(buttonRect.left + buttonRect.width / 2, buttonRect.top + buttonRect.height / 2) : null;
      return {
        dismissReachable: hit !== null && Boolean(dismiss?.contains(hit)),
        withinViewport: rect.width > 0 && rect.height > 0 && rect.left >= 0 && rect.top >= 0
          && rect.right <= window.innerWidth && rect.bottom <= window.innerHeight,
      };
    });
    assert.equal(visible.dismissReachable, true, 'the modal overlay must not cover the toast dismiss button');
    assert.equal(visible.withinViewport, true, 'the toast must stay inside the viewport');
  });
});
