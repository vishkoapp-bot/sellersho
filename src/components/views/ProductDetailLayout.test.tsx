import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const buy = 'button[aria-label="افزایش تعداد"]';
for (const scope of ['', '/store/batch-eight-store']) {
  test(`${scope || 'central'} PDP shows the floating buy bar on downward scroll and hides it when returning to the tabs`, async () => {
    await visit(`${scope}/product/batch8-product-0`, 390, async page => {
      await page.waitForSelector(buy, { visible: true });
      await page.waitForSelector('[data-testid="product-related"]');
      const floating = `div.fixed.${scope ? 'bottom-0' : 'bottom-16'} ${buy}`;
      const tab = 'button';
      await page.$eval(buy, button => {
        const bar = button.parentElement!.parentElement!;
        window.scrollTo(0, window.scrollY + bar.getBoundingClientRect().bottom + 8);
      });
      await page.waitForSelector(floating, { visible: true });
      const loaded = await page.evaluate(async () => {
        await document.fonts.ready;
        return [...document.fonts].some(font => font.family === 'Vazirmatn' && font.status === 'loaded');
      });
      assert.ok(loaded, 'PDP geometry must use the bundled Vazirmatn font');
      const geometry = await page.evaluate((selector, tabSelector) => {
        const bar = document.querySelector(selector)!.parentElement!.parentElement!.getBoundingClientRect();
        const specs = [...document.querySelectorAll(tabSelector)].find(node => node.textContent?.trim() === 'مشخصات فنی')!;
        const description = [...document.querySelectorAll('p')].find(node => node.textContent?.trim() === 'توضیحات کالای نمونه')!;
        const nav = [...document.querySelectorAll('nav.fixed')].find(node => node.getClientRects().length);
        return {
          tabBottom: specs.getBoundingClientRect().bottom,
          descriptionBottom: description.getBoundingClientRect().bottom,
          barTop: bar.top,
          bottom: innerHeight - bar.bottom,
          buyBarHeight: bar.height,
          fixedBuyBars: document.querySelectorAll(selector).length,
          navHeight: nav?.getBoundingClientRect().height ?? 0,
          padding: parseFloat(getComputedStyle(document.querySelector('.pb-36')!).paddingBottom),
        };
      }, floating, tab);
      assert.ok(geometry.tabBottom < geometry.barTop && geometry.descriptionBottom < geometry.barTop);
      assert.equal(geometry.bottom, scope ? 0 : 64);
      assert.equal(geometry.navHeight, scope ? 0 : 64);
      assert.equal(geometry.fixedBuyBars, 1);
      assert.ok(geometry.padding >= geometry.buyBarHeight + geometry.navHeight, JSON.stringify(geometry));
      await page.evaluate(() => {
        [...document.querySelectorAll('button')].find(node => node.textContent?.trim() === 'مشخصات فنی')?.scrollIntoView({ block: 'center' });
      });
      await page.waitForSelector(floating, { hidden: true });
      assert.ok(await page.evaluate(() => {
        const button = [...document.querySelectorAll('button')].find(node => node.textContent?.trim() === 'مشخصات فنی')!;
        const bounds = button.getBoundingClientRect();
        return bounds.top >= 0 && bounds.bottom < innerHeight - 64;
      }));
    });
  });
}

test('add-to-cart toast stays above the open drawer totals', async () => {
  await visit('/product/batch8-product-0', 390, async page => {
    await clickText(page, 'افزودن به سبد');
    await waitText(page, 'به سبد خرید افزوده شد');
    await page.evaluate(() => {
      const buttons = [...document.querySelectorAll<HTMLElement>('button')];
      buttons.find(node => node.title.includes('سبد خرید') && !node.title.includes('افزودن'))?.click();
    });
    await waitText(page, 'مبلغ قابل پرداخت');
    const toast = await page.waitForSelector('.fixed.top-4.z-\\[10000\\]', { visible: true });
    assert.ok(toast);
    assert.ok(await toast.evaluate(node => !node.className.includes('bottom-')));
  });
});
