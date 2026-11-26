import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('the mobile homepage starts with the managed banner and accessible shopping actions', async () => {
  await visit('/', 320, async page => {
    await page.waitForSelector('.central-hero-banner', { visible: true });
    assert.equal(await page.$eval('main > section', element => element.className), 'central-hero');
    assert.equal(await page.$('.central-intro-copy'), null);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 320);
    assert.match(await page.$eval('.central-hero-banner', image => image.getAttribute('alt') || ''), /کالاهای دیجیتال/);
    assert.deepEqual(await page.$$eval('.central-hero-actions a', links => links.map(link => ({
      href: link.getAttribute('href'),
      usable: link.getBoundingClientRect().height >= 44,
    }))), [
      { href: '#products-section', usable: true },
      { href: '/?category=all', usable: true },
      { href: '/categories', usable: true },
    ]);
    await page.focus('.central-hero-actions a[href="/categories"]');
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => location.pathname === '/categories');
  });
});

test('a secondary campaign retains its headline and CTA on mobile', async () => {
  await visit('/', 390, async page => {
    const campaign = '[data-testid="slider-banner"]';
    await page.waitForSelector(`${campaign} h2`, { visible: true });
    assert.equal(await page.$eval(`${campaign} a`, link => link.getAttribute('href')), '#products-section');
    assert.ok(await page.$eval(campaign, frame => {
      const link = frame.querySelector('a')!.getBoundingClientRect();
      const bounds = frame.getBoundingClientRect();
      return link.height >= 44 && link.bottom <= bounds.bottom && link.left >= bounds.left && link.right <= bounds.right;
    }));
  });
});
