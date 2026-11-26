import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const buy = 'button[aria-label="افزایش تعداد"]';
test('a legacy product asks for a real model and ignores links to the parent placeholder', async () => {
  await visit('/product/batch8-product-0?legacy-parent&spnc=LEGACY-PARENT', 390, async page => {
    const options = '.central-pdp-options button';
    await page.waitForSelector(options);
    assert.deepEqual(await page.$$eval(options, buttons => buttons.map(button => button.textContent?.trim())), ['اسپرت', 'استاندارد']);
    assert.equal(await page.$(`${options}[aria-pressed="true"]`), null);
    assert.equal(await page.$('.central-pdp-actions'), null);
    await clickText(page, 'اسپرت', options);
    assert.equal(await page.$eval('.central-pdp-subtotal strong', node => node.textContent?.trim()), '۱۵۰٬۰۰۰ تومان');
    await page.waitForSelector('.central-pdp-actions button:not(:disabled)');
  });
});
test('central PDP shows an accessible mobile purchase shortcut with a live subtotal and no duplicate controls', async () => {
  await visit('/product/batch8-product-0', 390, async page => {
    await page.waitForSelector('.central-pdp-mobile-buy button', { visible: true });
    const shortcut = await page.waitForSelector('.central-pdp-mobile-buy', { visible: true });
    assert.equal(await shortcut?.$eval('strong', node => node.textContent?.trim()), '۱۰۰٬۰۰۰ تومان');
    const padding = await page.evaluate(() => {
      const bar = document.querySelector('.central-pdp-mobile-buy')!.getBoundingClientRect();
      const nav = [...document.querySelectorAll('nav.fixed')].find(node => node.getClientRects().length);
      return { bottom: bar.bottom, navTop: nav?.getBoundingClientRect().top ?? innerHeight };
    });
    assert.ok(padding.bottom <= padding.navTop);
    await page.click('.central-pdp-mobile-buy button');
    await page.waitForSelector('.central-pdp-mobile-buy', { hidden: true });
    assert.equal(await page.evaluate(() => document.activeElement?.className), 'central-pdp-purchase');
    await page.click(buy);
    assert.equal(await page.$eval('.central-pdp-subtotal strong', node => node.textContent?.trim()), '۲۰۰٬۰۰۰ تومان');
    assert.equal(await page.$$eval(buy, buttons => buttons.length), 1);
    await clickText(page, 'مشخصات فنی');
    await waitText(page, 'توضیحات کالای نمونه');
  });
});

test('central PDP follows the linked model and reflects new selection, price, and quantity limits', async () => {
  await visit('/product/batch8-product-0?pdp-flow&spnc=PDP-FOREST', 390, async page => {
    await page.waitForSelector('.central-pdp-options button[aria-pressed="true"]');
    assert.equal(await page.$eval('.central-pdp-options button[aria-pressed="true"]', node => node.textContent?.trim()), 'سبز');
    assert.equal(await page.$eval('.central-pdp-subtotal strong', node => node.textContent?.trim()), '۱۵۰٬۰۰۰ تومان');
    await clickText(page, 'سنگی', '.central-pdp-options button');
    assert.equal(await page.$eval('.central-pdp-subtotal strong', node => node.textContent?.trim()), '۲۰۰٬۰۰۰ تومان');
    assert.equal(await page.$eval('button[aria-label="کاهش تعداد"]', button => (button as HTMLButtonElement).disabled), true);

    const zoom = 'button[aria-label="بزرگ‌نمایی تصویر"]';
    await page.click(zoom);
    await page.waitForSelector('dialog[open]', { visible: true });
    await page.waitForFunction(() => Boolean(document.activeElement?.closest('dialog[open]')));
    await page.keyboard.press('Escape');
    await page.waitForSelector('dialog[open]', { hidden: true });
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), 'بزرگ‌نمایی تصویر');
  });
});

test('central PDP shows unavailable stock without offering checkout actions', async () => {
  await visit('/product/batch8-product-0?pdp-flow&sold-out', 390, async page => {
    await page.waitForSelector('.central-pdp-stock[data-available="false"]');
    assert.equal(await page.$('.central-pdp-actions, .central-pdp-mobile-buy'), null);
  });
});

test('central PDP shows the purchase section beside the gallery on desktop', async () => {
  await visit('/product/batch8-product-0', 1280, async page => {
    await page.waitForSelector('.central-pdp-purchase', { visible: true });
    const geometry = await page.evaluate(() => {
      const gallery = document.querySelector('.central-pdp-gallery')!.getBoundingClientRect();
      const buy = document.querySelector('.central-pdp-purchase')!.getBoundingClientRect();
      const detail = document.querySelector('.central-pdp-details')!.getBoundingClientRect();
      return { galleryLeft: gallery.left, buyRight: buy.right, buyBottom: buy.bottom, detailsTop: detail.top };
    });
    assert.ok(geometry.galleryLeft > geometry.buyRight && geometry.detailsTop > geometry.buyBottom);
    assert.equal(await page.evaluate(() => Boolean(document.querySelector('.central-pdp-mobile-buy')?.getClientRects().length)), false);
  });
});

for (const scope of ['/store/batch-eight-store']) {
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
    await clickText(page, 'مشاهدهٔ سبد خرید');
    await page.waitForSelector('[data-toast-stack]', { visible: true });
    await page.waitForFunction(() => ['جمع کالاها', 'مبلغ قابل پرداخت'].some(copy => document.body.innerText.includes(copy)));
    const toast = await page.waitForSelector('.fixed.top-4.z-\\[10000\\]', { visible: true });
    assert.ok(toast);
    assert.ok(await toast.evaluate(node => !node.className.includes('bottom-')));
  });
});
