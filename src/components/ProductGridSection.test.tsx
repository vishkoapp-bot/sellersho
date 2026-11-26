import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('mobile homepage offers a short product preview and sends full browsing to the catalog', async () => {
  await visit('/', 390, async page => {
    await page.waitForSelector('#products-section [data-testid="product-item"]');
    assert.equal(await page.$$eval('#products-section [data-testid="product-item"]', cards => cards.length), 8);
    const next = '#products-section a[href="/?category=all"]';
    await page.$eval(next, target => target.scrollIntoView({ block: 'center' }));
    await page.click(next);
    await page.waitForFunction(() => new URL(location.href).searchParams.get('category') === 'all');
    await page.waitForSelector('main#main-content [data-testid="product-item"]');
    assert.ok(await page.$$eval('main#main-content [data-testid="product-item"]', cards => cards.length) > 8);
  });
});

test('mobile home has keyboard-accessible product links and does not overflow a narrow viewport', async () => {
  await visit('/', 320, async page => {
    await page.waitForSelector('#products-section .product-title-link');
    await page.waitForSelector('.central-trust small');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
    const service = await page.$eval('.central-trust > div', item => {
      const text = item.querySelector('small')!;
      return { direction: getComputedStyle(item).flexDirection, font: getComputedStyle(text).fontSize };
    });
    assert.deepEqual(service, { direction: 'column', font: '13px' });
    const selector = '#products-section .product-title-link';
    const href = await page.$eval(selector, link => link.getAttribute('href'));
    assert.match(href || '', /^\/product\//);
    await page.focus(selector);
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => location.pathname.startsWith('/product/'));
  });
});

test('desktop categories menu expands through keyboard and follows actual category links', async () => {
  await visit('/', 1440, async page => {
    await waitText(page, 'دسته‌بندی کالاها');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 1440);
    assert.ok(await page.$eval('.central-hero', section => section.getBoundingClientRect().height < 500));
    const selector = '.central-menu-trigger';
    await page.focus(selector);
    await page.keyboard.press('Enter');
    await page.waitForSelector('.central-menu-panel:not([hidden]) .central-menu-all');
    await page.keyboard.press('Escape');
    assert.equal(await page.$eval(selector, node => node.getAttribute('aria-expanded')), 'false');
    await page.keyboard.press('Enter');
    const category = await page.$eval('.central-menu-all', link => new URL((link as HTMLAnchorElement).href).searchParams.get('category'));
    assert.ok(category);
    await page.click('.central-menu-all');
    await page.waitForFunction(category => new URL(location.href).searchParams.get('category') === category, {}, category);
  });
});
