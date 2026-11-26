import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, fillInput, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('mobile tracking uses the shop palette, fits at 320px, and closes back to its trigger', async () => {
  await visit('/', 320, async page => {
    const trigger = '.central-track-mobile';
    await page.waitForSelector(trigger, { visible: true });
    await page.click(trigger);
    await page.waitForSelector('dialog.tracking-dialog[open]');
    await waitText(page, 'سفارشی برای نمایش در دسترس نیست');
    const dialog = await page.$eval('dialog.tracking-dialog', node => ({
      width: node.getBoundingClientRect().width,
      accent: getComputedStyle(node.querySelector('button.tracking-submit')!).backgroundColor,
      input: node.querySelector('input')?.getBoundingClientRect().height ?? 0,
    }));
    assert.ok(dialog.width < 320 && dialog.input >= 44);
    assert.equal(dialog.accent, 'rgb(22, 70, 56)');
    await fillInput(page, 'input[name="tracking-code"]', 'UNKNOWN-21');
    await page.keyboard.press('Enter');
    await page.waitForSelector('dialog [role="alert"]');
    await page.keyboard.press('Escape');
    await page.waitForSelector('dialog.tracking-dialog', { hidden: true });
    assert.equal(await page.evaluate(selector => document.activeElement?.matches(selector), trigger), true);
  });
});

test('opening tracking loads account orders, keeps the current store, and shows pending payment honestly', async () => {
  await visit('/?access-fixture-role=customer&access-fixture-caps=customer&tracking-orders', 1280, async page => {
    await page.waitForSelector('.central-track', { visible: true });
    await page.click('.central-track');
    await waitText(page, 'DEMO-125');
    await waitText(page, 'در انتظار پرداخت');
    assert.equal(await page.$eval('dialog.tracking-dialog', node => node.textContent?.includes('OTHER-900')), false);
    await fillInput(page, 'input[name="tracking-code"]', 'other-900');
    await page.keyboard.press('Enter');
    await page.waitForSelector('dialog [role="alert"]');
    await fillInput(page, 'input[name="tracking-code"]', 'demo-125');
    await page.keyboard.press('Enter');
    await waitText(page, 'شرکت حمل نمونه');
    assert.equal(await page.$('dialog [role="alert"]'), null);
  });
});
