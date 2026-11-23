import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const heroFrame = 'div.relative.overflow-hidden:has(> div > img + div > h3)';

for (const { name, selector, minimumFontSize } of [
  { name: 'long hero title keeps the CTA inside the frame at 390px', selector: heroFrame, minimumFontSize: 11 },
  { name: 'long secondary-banner title keeps the CTA inside the frame at 390px', selector: '[data-testid="slider-banner"] > div', minimumFontSize: 12 },
]) {
  test(name, async () => {
    await visit('/', 390, async page => {
      await page.waitForSelector(heroFrame, { visible: true });
      await page.waitForSelector(`${selector} h3`, { visible: true });
      await page.evaluate(() => document.fonts.ready);
      const layout = await page.$eval(selector, frame => {
        const slide = frame.querySelector(':scope > .opacity-100')!;
        const title = slide.querySelector('h3')!;
        const subtitle = slide.querySelector('p')!;
        const button = [...slide.querySelectorAll('button')].find(node => node.textContent?.trim() === 'مشاهده');
        if (!button) throw new Error('Missing banner CTA');
        return {
          frame: frame.getBoundingClientRect().toJSON(),
          button: button.getBoundingClientRect().toJSON(),
          titleClamp: getComputedStyle(title).webkitLineClamp,
          subtitleClamp: getComputedStyle(subtitle).webkitLineClamp,
          subtitleFontSize: parseFloat(getComputedStyle(subtitle).fontSize),
        };
      });
      assert.ok(layout.frame.width > 0 && layout.frame.height > 0);
      assert.ok(layout.button.width > 0 && layout.button.height > 0);
      assert.ok(layout.button.top >= layout.frame.top, 'CTA top stays inside frame');
      assert.ok(layout.button.bottom <= layout.frame.bottom, 'CTA bottom stays inside frame');
      assert.ok(layout.button.left >= layout.frame.left, 'CTA left stays inside frame');
      assert.ok(layout.button.right <= layout.frame.right, 'CTA right stays inside frame');
      assert.equal(layout.titleClamp, '2');
      assert.equal(layout.subtitleClamp, '2');
      assert.ok(layout.subtitleFontSize >= minimumFontSize);
    });
  });
}
