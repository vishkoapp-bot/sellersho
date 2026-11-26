import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../scripts/fixtures/catalog/batch8-browser';
import { formatPrice } from '../utils/formatters';

const visit = batch8Browser();
for (const fare of [0, 15000]) {
  test(`checkout and order views display an explicit shipping fare of ${fare} toman`, async () => {
    const expected = `${formatPrice(fare)} تومان`;
    await visit('/', 390, async page => {
      await page.waitForFunction(() => (window as any).batch8Fixture?.state?.().productCount === 24);
      await page.evaluate(cost => (window as any).batch8Fixture.openCheckout(cost), fare);
      await waitText(page, 'انبار نمونه');
      const amounts = await page.evaluate(() => [...document.querySelectorAll('span')]
        .filter(node => node.textContent === 'هزینه ارسال سفارش:' || node.textContent?.includes('محموله ۱ · از انبار نمونه'))
        .map(node => node.nextElementSibling?.textContent?.trim()));
      assert.ok(amounts.length >= 2);
      assert.ok(amounts.every(value => value === expected));
      assert.doesNotMatch(await page.$eval('body', body => body.innerText), /رایگان/);

      await page.evaluate(() => (window as any).batch8Fixture.showCheckoutError());
      await waitText(page, 'خطای آزمایشی ثبت سفارش');
      assert.ok(await page.$eval('.fixed.top-4.z-\\[10000\\]', node => node.textContent?.includes('خطای آزمایشی ثبت سفارش')));
    });
    for (const detail of [false, true]) {
      await visit('/', 390, async page => {
        await page.waitForFunction(() => typeof (window as any).batch8Fixture?.showOrder === 'function');
        await page.evaluate((cost, inDetail) => (window as any).batch8Fixture.showOrder(cost, inDetail), fare, detail);
        await page.waitForSelector('#fixture-order');
        const amounts = await page.$$eval('#fixture-order span', nodes => nodes
          .filter(node => node.textContent?.trim() === 'هزینه ارسال:')
          .map(node => node.nextElementSibling?.textContent?.trim()));
        assert.deepEqual(amounts, [expected]);
        assert.doesNotMatch(await page.$eval('#fixture-order', order => (order as HTMLElement).innerText), /رایگان/);
      });
    }
  });
}

test('shopping from an empty central cart dismisses the dialog and opens the catalog', async () => {
  await visit('/', 390, async page => {
    await clickText(page, 'سبد خرید');
    await waitText(page, 'سبد خرید شما در حال حاضر خالی است.');
    await clickText(page, 'مشاهده محصولات');
    await page.waitForSelector('button[aria-label="بستن سبد خرید"]', { hidden: true, timeout: 2000 });
    await page.waitForFunction(() => (window as any).batch8Fixture?.state().view === 'catalog');
  });
});

test('mobile checkout keeps the address in front and can close by keyboard', async () => {
  await visit('/', 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture?.state?.().productCount === 24);
    await page.evaluate(() => (window as any).batch8Fixture.openCheckout(15000));
    await page.waitForSelector('[role="dialog"][aria-labelledby="cart-drawer-title"]', { timeout: 2000 });
    assert.equal(await page.$eval('[aria-current="step"] > span:last-child', el => el.textContent?.trim()), 'نشانی و ثبت');
    await page.waitForFunction(() => document.activeElement?.id === 'cart-drawer-title');
    await page.keyboard.press('Escape');
    await page.waitForSelector('#cart-drawer-title', { hidden: true });
  });
});

test('an address edit disables checkout before the debounced shipping quote refresh', async () => {
  await visit('/', 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture?.state?.().productCount === 24);
    await page.evaluate(() => (window as any).batch8Fixture.openCheckout(15000));
    const submit = 'button[form="checkout-details"]';
    await page.waitForSelector(`${submit}:not(:disabled)`);
    const blocked = await page.$eval('#cart-checkout-address', async (node, selector) => {
      const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
      setValue.call(node, 'نشانی ویرایش‌شده');
      node.dispatchEvent(new Event('input', { bubbles: true }));
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
      return document.querySelector<HTMLButtonElement>(selector)?.disabled;
    }, submit);
    assert.equal(blocked, true);
    await page.waitForSelector(`${submit}:not(:disabled)`);
  });
});

test('the central checkout keeps its own palette and fits the actions after a storefront visit', async () => {
  await visit('/store/batch-eight-store', 320, async page => {
    const drawer = '[role="dialog"][aria-labelledby="cart-drawer-title"]';
    await page.waitForFunction(() => (window as any).batch8Fixture?.state?.().storeSlug === 'batch-eight-store');
    await page.evaluate(() => (window as any).batch8Fixture.openWorkspace('customer'));
    await page.waitForFunction(() => {
      const state = (window as any).batch8Fixture?.state?.();
      return state?.view === 'customer' && state.storeSlug === 'central' && state.productCount === 24;
    });
    await page.evaluate(() => (window as any).batch8Fixture.openCheckout(15000));
    await page.waitForSelector(drawer + ' [form="checkout-details"]');
    assert.equal(await page.$eval(drawer, node => node.getAttribute('data-checkout-scope')), 'central');
    const footer = await page.$eval(drawer + ' > footer', node => ({
      width: node.clientWidth,
      contentWidth: node.scrollWidth,
      buttons: [...node.querySelectorAll('button')].map(button => button.getBoundingClientRect().height),
    }));
    assert.equal(footer.width, footer.contentWidth);
    assert.ok(footer.buttons.every(height => height >= 44));
  });
});
