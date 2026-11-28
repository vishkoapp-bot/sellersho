import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const storefront = '/store/batch-eight-store';
const items = '#products [data-testid="product-item"]';
const buyPanel = '.storefront-buy-panel:not(.storefront-buy-panel--floating)';
const themes = {
  modern: 'rgb(247, 247, 241)',
  electronic: 'rgb(250, 246, 239)',
  tech: 'rgb(242, 246, 248)',
  digikala: 'rgb(248, 246, 244)',
};

test('Modern makes categories and the catalogue reachable early on narrow screens with an older layout', () =>
  visit(`${storefront}?template=modern&modern-support-order`, 320, async page => {
    await page.waitForSelector(items);
    await page.waitForSelector('[data-testid="store-faq"]');
    const order = await page.$$eval('.modern-sections > section[data-testid]', sections =>
      sections.map(section => section.getAttribute('data-testid')));
    assert.deepEqual(order.slice(0, 5), ['store-hero-slider', 'store-category-mosaic', 'store-products', 'store-featured', 'store-trust-strip']);
    assert.equal(order.at(-1), 'store-faq');
    assert.equal(await page.$('[data-testid="store-newsletter"]'), null);
    const category = 'a.modern-category';
    assert.equal(await page.$$eval(category, cards => cards.filter(card => card.getClientRects().length).length), 4);
    const start = await page.$eval('#products', section => section.getBoundingClientRect().top);
    assert.ok(start < await page.evaluate(() => innerHeight - 60), `catalogue starts at ${start}px`);
    await page.click('.modern-category-expand');
    assert.equal(await page.$$eval(category, cards => cards.filter(card => card.getClientRects().length).length), 7);
    await page.click(`${category}[data-category="کتاب"]`);
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 1, {}, items);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
  }));

test('central storefront footer keeps its navigation without a newsletter form', () =>
  visit('/', 390, async page => {
    await page.waitForSelector('.central-footer nav');
    assert.equal(await page.$('.central-footer form'), null);
  }));

test('modern keeps its category links, spotlight and availability filter usable on a narrow display', () =>
  visit(`${storefront}?template=modern&modern-stock-filter`, 320, async page => {
    await page.waitForSelector(items);
    const spotlight = '[data-testid="store-featured"] a[href*="/product/"]';
    assert.match(await page.$eval(spotlight, link => link.getAttribute('href') || ''), /\/store\/batch-eight-store\/product\//);
    await clickText(page, 'جست‌وجوی کالا', '.showcase-hero a');
    assert.equal(new URL(page.url()).hash, '#fashion-search');
    await page.$eval('[data-testid="store-category-mosaic"] a[data-category="digital"]', link => (link as HTMLAnchorElement).click());
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, items);
    await page.click('.modern-availability input');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 1, {}, items);
    await clickText(page, 'نمایش همه کالاها', '#products button');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 8, {}, items);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
  }));

for (const [template, paper] of Object.entries(themes)) {
  test(`${template} keeps discovery, model selection and checkout scoped on mobile`, () =>
    visit(`${storefront}?template=${template}&pdp-flow`, 320, async page => {
      await page.waitForSelector(items, { visible: true });
      assert.ok(await page.$(`header[data-storefront-theme="tpl-${template}-1"]`));
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
      await page.click('header a[aria-label="جست‌وجو در ویترین"]');
      assert.equal(await page.evaluate(() => document.activeElement?.id), 'fashion-search');
      await fillInput(page, '#fashion-search', 'ديجيتال');
      await page.keyboard.press('Enter');
      await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, items);
      await page.click('#products button[aria-label="پاک کردن جست‌وجو"]');
      await clickText(page, 'نمایش کالاهای بیشتر');
      await page.waitForFunction(selector => document.querySelectorAll(selector).length === 16, {}, items);

      await page.select('#products select', 'cheapest');
      await page.waitForSelector(`${items} .product-title-link`, { visible: true });
      assert.match(await page.$eval(`${items} .product-title-link`, a => a.getAttribute('href') || ''), /\/store\/batch-eight-store\/product\/batch8-product-0$/);
      await page.click(`${items} button[aria-label^="انتخاب مدل"]`);
      await page.waitForSelector(`.fashion-pdp[data-storefront-theme="tpl-${template}-1"]`);
      const scope = new URL(page.url()).pathname;
      assert.equal(scope, `${storefront}/product/batch8-product-0`);
      const choose = '.fashion-pdp button';
      await clickText(page, 'سبز', choose);
      await page.waitForFunction(() => Array.from(document.querySelectorAll('.fashion-pdp button[aria-pressed="true"]'))
        .some(button => button.textContent?.includes('سبز')));
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
      const buyBeforeSpecs = await page.$eval(buyPanel, panel => (panel.compareDocumentPosition(
        document.querySelector('button[aria-controls][aria-pressed]')!,
      ) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0);
      assert.equal(buyBeforeSpecs, true);
      await page.$eval(`${buyPanel} button[title="افزودن کالا به سبد خرید"]`, button => (button as HTMLButtonElement).click());
      const drawer = `[data-checkout-scope="storefront"][data-storefront-theme="tpl-${template}-1"]`;
      await page.waitForSelector(drawer, { visible: true });
      assert.equal(await page.$eval(drawer, node => getComputedStyle(node).backgroundColor), paper);
      assert.match(await page.$eval(drawer, node => node.textContent || ''), /سبز/);
      await page.focus('#cart-drawer-title');
      await page.keyboard.press('Escape');
      await page.waitForSelector(drawer, { hidden: true });
      await page.click('header a[aria-label="جست‌وجو در ویترین"]');
      await page.waitForSelector('#fashion-search');
      assert.equal(new URL(page.url()).pathname, storefront);
      assert.ok(await page.$('nav[aria-label="منوی پایین ویترین"] button[aria-label^="سبد خرید"]'));
    }));
}
