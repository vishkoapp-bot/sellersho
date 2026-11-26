import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('mobile catalog keeps products visible, expands categories in filters and makes selection sharable', async () => {
  await visit('/?category=digital', 390, async page => {
    const rows = '[data-testid="product-item"]';
    const filters = '#catalog-filters';
    const filterButton = 'button[aria-controls="catalog-filters"]';
    const chevron = `${filters} button[aria-label="زیردسته‌های کالای دیجیتال"]`;

    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, rows);
    await page.waitForSelector(filters, { hidden: true });
    await page.click(filterButton);
    await page.waitForSelector(chevron, { visible: true });
    const toggleHeight = await page.$eval(chevron, element => element.getBoundingClientRect().height);
    assert.ok(toggleHeight >= 44);

    const expansion = await page.$eval(chevron, element => element.getAttribute('aria-expanded'));
    if (expansion === 'true') {
      await page.click(chevron);
      await page.waitForFunction(selector => document.querySelector(selector)?.getAttribute('aria-expanded') === 'false', {}, chevron);
    }
    await page.click(chevron);
    await page.waitForFunction(selector => document.querySelector(selector)?.getAttribute('aria-expanded') === 'true', {}, chevron);
    await page.$$eval(`${filters} button`, buttons => {
      (buttons.find(button => button.textContent?.trim() === 'موبایل') as HTMLButtonElement).click();
    });
    await page.waitForFunction(selector => {
      const products = document.querySelectorAll(selector);
      return products.length === 1 && products[0].textContent?.includes('کالای عمومی نمونه 2');
    }, {}, rows);
    await page.waitForSelector(filters, { hidden: true });
    assert.equal(new URL(page.url()).searchParams.get('category'), 'mobile');
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.goBack();
    await page.waitForFunction(() => new URL(location.href).searchParams.get('category') === 'digital');
    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, rows);
  });
});

test('category directory finds nested categories and opens their catalog URL', () =>
  visit('/categories', 390, async page => {
    await page.waitForSelector('#category-search');
    await fillInput(page, '#category-search', 'موبايل');
    await waitText(page, 'موبایل');
    await page.waitForSelector('main a[href="/?category=mobile"]', { visible: true });
    await page.$eval('main a[href="/?category=mobile"]', link => link.scrollIntoView({ block: 'center' }));
    await page.click('main a[href="/?category=mobile"]');
    await page.waitForSelector('[data-testid="product-item"]');
    assert.equal(new URL(page.url()).searchParams.get('category'), 'mobile');
  }));

test('a failed search can be retried without losing the query or showing a false empty state', () =>
  visit('/?q=retry', 390, async page => {
    const error = '[data-testid="catalog-load-error"]';
    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish('error'));
    await page.waitForSelector(error);
    assert.doesNotMatch(await page.$eval('main', main => main.textContent ?? ''), /کالایی یافت نشد/);
    await page.$eval(error + ' button', button => (button as HTMLButtonElement).click());
    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await page.waitForSelector('[data-testid="product-item"]');
    assert.equal(await page.$(error), null);
    assert.equal(new URL(page.url()).searchParams.get('q'), 'retry');
  }));

test('narrow catalog keeps two aligned cards and restores keyboard focus when filters close', () =>
  visit('/?category=digital', 320, async page => {
    const cards = '.central-catalog-grid [data-testid="product-item"]';
    const trigger = 'button[aria-controls="catalog-filters"]';
    const mobileClose = 'button[aria-label="بستن فیلترها"]';
    await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, cards);
    const geometry = await page.$$eval(cards, items => items.map(item => {
      const card = item.getBoundingClientRect();
      const action = item.querySelector('.product-card-action')?.getBoundingClientRect();
      return { top: card.top, width: card.width, actionWidth: action?.width ?? 0 };
    }));
    assert.equal(geometry[0].top, geometry[1].top);
    assert.equal(geometry[0].width, geometry[1].width);
    assert.ok(geometry.every(item => item.actionWidth >= 44));
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));

    await page.focus(trigger);
    await page.keyboard.press('Enter');
    await page.waitForSelector(mobileClose, { visible: true });
    await page.focus(mobileClose);
    await page.keyboard.press('Escape');
    await page.waitForSelector('#catalog-filters', { hidden: true });
    assert.equal(await page.evaluate(selector => document.activeElement === document.querySelector(selector), trigger), true);
  }));
