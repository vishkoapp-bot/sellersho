import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Page } from 'puppeteer';
import { batch8Browser, fillInput } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ entryPoint: 'scripts/fixtures/header/provider.tsx' });

async function ready(page: Page) {
  await page.waitForFunction(() => {
    const h = (window as any).headerHarness;
    return h?.state && !h.state().loading && h.state().products === 2;
  });
  await page.evaluate(() => (window as any).headerHarness.open());
  await page.waitForSelector('header input[type="text"]');
}

// A2: Enter must cancel the pending suggestion fetch — both halves.
// The suggestion read is a deferred promise in the fixture, so a
// resolving fetch can never sneak the dropdown open: the timer-cancel
// half starts no fetch at all, the in-flight half aborts the one that
// started, and the control half proves a resolving fetch DOES open
// the dropdown (the test is not vacuously passing).
test('Enter before the 320ms debounce fires starts no suggestion fetch and never opens the dropdown', async () => {
  await visit('/', 1280, async page => {
    await ready(page);
    // The term is set AND Enter is pressed in ONE evaluate: the input
    // event and the keydown run in the same JS task, so Enter is
    // processed before the 320ms timer can fire. (Separate CDP
    // roundtrips cannot guarantee that on this VM — the Enter dispatch
    // alone can take 100-900ms, past the debounce, which legitimately
    // fires the timer mid-gap; the abort half covers that path, and
    // those fetches always show aborted: true.) One input event still
    // drives the real onChange -> effect -> timer path.
    await page.focus('header input[type="text"]');
    await page.evaluate(() => {
      const input = document.querySelector('header input[type="text"]') as HTMLInputElement;
      const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
      setter.call(input, 'تیشرت');
      input.dispatchEvent(new Event('input', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    });
    await new Promise((r) => setTimeout(r, 700));
    const result = await page.evaluate(() => {
      const h = (window as any).headerHarness;
      return { suggestCalls: h.suggestCalls(), dropdownOpen: h.dropdownOpen(), view: h.state().view };
    });
    assert.equal(result.suggestCalls.length, 0, 'the debounce timer must be cleared before it fires');
    assert.equal(result.dropdownOpen, false);
    assert.equal(result.view, 'catalog', 'Enter still navigates to the catalog');
  });
});

test('Enter after the suggestion fetch starts aborts it in flight and never opens the dropdown', async () => {
  await visit('/', 1280, async page => {
    await ready(page);
    await fillInput(page, 'header input[type="text"]', 'تیشرت');
    await page.waitForFunction(() => (window as any).headerHarness.suggestCalls().length === 1, { timeout: 5000 });
    const before = await page.evaluate(() => (window as any).headerHarness.suggestCalls()[0]);
    assert.equal(before.aborted, false, 'the fetch is in flight, not yet aborted');
    assert.equal(before.params.suggest, true, 'E2: the suggestion fetch carries suggest=1');
    await page.keyboard.press('Enter');
    await new Promise((r) => setTimeout(r, 300));
    const result = await page.evaluate(() => {
      const h = (window as any).headerHarness;
      return { call: h.suggestCalls()[0], dropdownOpen: h.dropdownOpen(), view: h.state().view };
    });
    assert.equal(result.call.aborted, true, 'Enter must abort the in-flight suggestion fetch');
    assert.equal(result.call.settled, false, 'the aborted fetch must never resolve into the dropdown');
    assert.equal(result.dropdownOpen, false);
    assert.equal(result.view, 'catalog');
  });
});

test('control: a resolving suggestion fetch opens the dropdown while the input still has focus', async () => {
  await visit('/', 1280, async page => {
    await ready(page);
    await fillInput(page, 'header input[type="text"]', 'تیشرت');
    await page.waitForFunction(() => (window as any).headerHarness.suggestCalls().length === 1, { timeout: 5000 });
    await page.evaluate(() => (window as any).headerHarness.resolveSuggestion([
      { id: 'prod-1cc63eae', title: 'تیشرت زنانه', price: 850000, originalPrice: 1000000, discountPercent: 15, image: '/templates/fashion/knitwear.webp' },
    ]));
    await page.waitForFunction(() => (window as any).headerHarness.dropdownOpen(), { timeout: 5000 });
    const text = await page.evaluate(() => (window as any).headerHarness.dropdownText());
    assert.match(text, /تیشرت زنانه/);
    await page.keyboard.press('ArrowDown');
    assert.equal(await page.evaluate(() => document.activeElement?.matches('[data-suggestion]')), true);
    await page.keyboard.press('ArrowDown');
    assert.equal(await page.evaluate(() => document.activeElement?.matches('.central-suggestion-all')), true);
    await page.keyboard.press('ArrowUp');
    await page.keyboard.press('Escape');
    assert.equal(await page.evaluate(() => document.activeElement?.matches('#central-search')), true);
    assert.equal(await page.evaluate(() => (window as any).headerHarness.dropdownOpen()), false);
  });
});
