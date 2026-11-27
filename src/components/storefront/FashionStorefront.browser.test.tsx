import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const storefront = '/store/batch-eight-store';
const products = '#products .fashion-product';

test('fashion shoppers can search, sort, add a page and open a scoped product at 320 pixels', () =>
  visit(storefront + '?fashion', 320, async page => {
    await page.waitForSelector('.fashion-hero-photo', { visible: true });
    await page.waitForSelector(products, { visible: true });
    assert.equal(await page.$$eval(products, cards => cards.length), 8);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);

    await page.click('header a[aria-label="جست‌وجو در ویترین"]');
    assert.equal(await page.evaluate(() => document.activeElement?.id), 'fashion-search');
    await fillInput(page, '#fashion-search', 'ديجيتال');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, products);
    await page.select('.fashion-sort-field select', 'expensive');
    assert.match(await page.$eval(products, card => card.getAttribute('href') || ''), /batch8-product-1$/);
    await page.click('button[aria-label="پاک کردن جست‌وجو"]');
    await clickText(page, 'نمایش کالاهای بیشتر');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 16, {}, products);

    await page.select('.fashion-sort-field select', 'cheapest');
    const link = await page.$eval(products, card => card.getAttribute('href'));
    assert.equal(link, storefront + '/product/batch8-product-0');
    await page.click(products);
    await page.waitForFunction(path => location.pathname === path, {}, link);
    await page.waitForSelector('.fashion-pdp h1', { visible: true });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
    await clickText(page, 'افزودن به سبد', '.fashion-pdp button');
    const cart = '[data-checkout-scope="storefront"]';
    await page.waitForSelector(cart, { visible: true });
    assert.equal(await page.$eval(cart, drawer => getComputedStyle(drawer).backgroundColor), 'rgb(251, 248, 243)');
    await page.focus('#cart-drawer-title');
    await page.keyboard.press('Escape');
    await page.waitForSelector(cart, { hidden: true });
    await page.click('.fashion-header a[aria-label="جست‌وجو در ویترین"]');
    await page.waitForSelector('#fashion-search', { visible: true });
    assert.equal(new URL(page.url()).pathname, storefront);
    assert.equal(new URL(page.url()).hash, '#fashion-search');
  }));

test('fashion hero uses a balanced desktop grid and renders the original photo', () =>
  visit(storefront + '?fashion', 1440, async page => {
    await page.waitForFunction(() => (document.querySelector('.fashion-hero-photo') as HTMLImageElement)?.naturalWidth > 0);
    const geometry = await page.evaluate(() => {
      const media = document.querySelector('.fashion-hero-media')!.getBoundingClientRect();
      const copy = document.querySelector('.fashion-hero-copy')!.getBoundingClientRect();
      return {
        sideBySide: media.right <= copy.left + 1 && Math.abs(media.top - copy.top) <= 1,
        mobileNavHidden: !document.querySelector('.fashion-bottom-nav')?.getClientRects().length,
        width: document.documentElement.scrollWidth,
      };
    });
    assert.deepEqual(geometry, { sideBySide: true, mobileNavHidden: true, width: 1440 });
  }));
