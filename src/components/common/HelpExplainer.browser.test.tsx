import assert from 'node:assert/strict';
import { test } from 'node:test';
import { build } from 'esbuild';
import type { Page } from 'puppeteer';
import { batch8Browser } from '../../../scripts/fixtures/catalog/batch8-browser';

// Live-DOM counterpart of HelpExplainer.test.tsx. Run in CI/live acceptance;
// a fixture launch failure is not evidence that any interaction passed.
const visit = batch8Browser();
const product = '#products-section h2 button[aria-haspopup="dialog"]';
const order = '#fixture-order button[aria-haspopup="dialog"]';
const dialog = '[role="dialog"][aria-label="توضیح ساده"]';
const productText = 'کالاها پیش از ارسال کنترل کیفیت می‌شوند؛ ارسال به سراسر کشور';

async function expanded(page: Page, selector: string, value: boolean) {
  await page.waitForFunction((query, expected) =>
    document.querySelector(query)?.getAttribute('aria-expanded') === String(expected), {}, selector, value);
}
async function closed(page: Page) {
  await page.waitForSelector(dialog, { hidden: true });
}
async function focused(page: Page, selector: string) {
  await page.waitForFunction(query => document.activeElement === document.querySelector(query), {}, selector);
}
async function ready(page: Page, selector = product) {
  await page.waitForSelector(selector, { visible: true });
  // Finish scrolling before opening, since page scroll intentionally closes help.
  await page.$eval(selector, node => node.scrollIntoView({ block: 'center', behavior: 'instant' }));
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
}
async function showOrder(page: Page) {
  await page.waitForFunction(() => typeof (window as any).batch8Fixture?.showOrder === 'function');
  await page.evaluate(() => (window as any).batch8Fixture.showOrder(0, false));
  await ready(page, order);
}

test('cases 2–5, 7: real homepage help toggles, closes outside/Esc, and supports keyboard without focus-open', async () => {
  await visit('/', 390, async page => {
    await ready(page);
    await page.focus(product);
    await expanded(page, product, false);
    assert.equal(await page.$(dialog), null);
    await page.click(product);
    await expanded(page, product, true);
    assert.equal(await page.$eval(`${dialog} p`, node => node.textContent), productText);
    await page.click(product);
    await expanded(page, product, false);
    await closed(page);
    await page.click(product);
    await expanded(page, product, true);
    await page.mouse.click(2, 2);
    await closed(page);
    await expanded(page, product, false);
    await page.click(product);
    await expanded(page, product, true);
    await page.keyboard.press('Escape');
    await closed(page);
    await focused(page, product);
    for (const key of ['Enter', 'Space'] as const) {
      await page.keyboard.press(key);
      await expanded(page, product, true);
      await page.keyboard.press(key);
      await expanded(page, product, false);
      await closed(page);
    }
  });
});

test('case 6 and 390px adjacent hit areas: two real anchors remain independent and only one opens', async () => {
  await visit('/', 390, async page => {
    await showOrder(page);
    await ready(page);
    // Controlled adjacency of REAL mounted anchors: no cloned buttons or fake click handlers.
    // This tests the shared hit-area contract, not the natural homepage spacing.
    await page.evaluate((a, b) => {
      for (const [index, query] of [a, b].entries()) {
        Object.assign((document.querySelector(query) as HTMLElement).style,
          { position: 'fixed', left: `${20 + index * 44}px`, top: '100px', zIndex: '9999' });
      }
    }, product, order);
    const hits = await page.evaluate((a, b) => [a, b].map(query => {
      const node = document.querySelector(query)!;
      const rect = node.getBoundingClientRect();
      const after = getComputedStyle(node, '::after');
      return { left: rect.left + parseFloat(after.left), right: rect.right - parseFloat(after.right),
        top: rect.top + parseFloat(after.top), bottom: rect.bottom - parseFloat(after.bottom) };
    }), product, order);
    assert.ok(hits[0].right <= hits[1].left, 'expanded hit areas must not overlap');
    for (const hit of hits) {
      assert.ok(hit.right - hit.left >= 32);
      assert.ok(hit.bottom - hit.top >= 32);
    }
    // Stay inside the rounded pseudo-element edge, outside each 24px visual circle.
    await page.touchscreen.tap(hits[0].left + 3, (hits[0].top + hits[0].bottom) / 2);
    await expanded(page, product, true);
    // DOM click deliberately avoids outside-pointerdown masking a broken single-open event.
    await page.$eval(order, node => (node as HTMLButtonElement).click());
    await expanded(page, product, false);
    await expanded(page, order, true);
    assert.equal(await page.$$eval(dialog, nodes => nodes.length), 1);
    assert.equal(await page.$$eval(`${dialog} li`, nodes => nodes.length), 2);
    await page.keyboard.press('Escape');
    await closed(page);
    await page.touchscreen.tap(hits[1].right - 3, (hits[1].top + hits[1].bottom) / 2);
    await expanded(page, order, true);
    await expanded(page, product, false);
  });
});

test('case 8-open: title is the heading and accessible-label suffix', async () => {
  // Batch 1 has no title-bearing app call site. Mount the production component
  // in a test-only root in the full-app fixture, without modifying its provider.
  const bundle = await build({
    stdin: { contents: `import React from 'react';
      import { createRoot } from 'react-dom/client';
      import { HelpExplainer } from './src/components/common/HelpExplainer';
      const root = document.createElement('div'); root.id = 'help-title-probe';
      Object.assign(root.style, { position: 'fixed', left: '100px', top: '100px', zIndex: '9999' });
      document.body.append(root);
      createRoot(root).render(<HelpExplainer title="عنوان راهنما" text="متن راهنما" />);
      const mixed = document.createElement('div'); mixed.id = 'help-mixed-probe';
      Object.assign(mixed.style, { position: 'fixed', left: '150px', top: '100px', zIndex: '9999' });
      document.body.append(mixed);
      createRoot(mixed).render(<HelpExplainer title="راهنمای مختلط" text={'خط معرفی\\n- مورد یک\\n- مورد دو'} />);`,
      resolveDir: process.cwd(), loader: 'tsx' },
    bundle: true, write: false, platform: 'browser', format: 'iife',
    define: { 'process.env.NODE_ENV': '"production"' },
  });
  await visit('/', 390, async page => {
    await ready(page);
    await page.addScriptTag({ content: bundle.outputFiles[0].text });
    const trigger = '#help-title-probe button';
    await ready(page, trigger);
    assert.equal(await page.$eval(trigger, node => node.getAttribute('aria-label')), 'توضیح: عنوان راهنما');
    await page.click(trigger);
    await expanded(page, trigger, true);
    const panel = '[role="dialog"][aria-label="توضیح: عنوان راهنما"]';
    assert.equal(await page.$eval(`${panel} h3`, node => node.textContent), 'عنوان راهنما');
    assert.equal(await page.$eval(`${panel} p`, node => node.textContent), 'متن راهنما');
    await page.keyboard.press('Escape');
    await page.waitForSelector(panel, { hidden: true });
    const mixedTrigger = '#help-mixed-probe button';
    await ready(page, mixedTrigger);
    await page.click(mixedTrigger);
    await expanded(page, mixedTrigger, true);
    const mixedPanel = '[role="dialog"][aria-label="توضیح: راهنمای مختلط"]';
    assert.deepEqual(await page.$$eval(`${mixedPanel} p`, nodes => nodes.map(node => node.textContent)), ['خط معرفی']);
    assert.deepEqual(await page.$$eval(`${mixedPanel} li`, nodes => nodes.map(node => node.textContent)), ['مورد یک', 'مورد دو']);
  });
});

test('case 9-open: real F-C-04 sheet has two clean bullets, named close, backdrop, and focus return', async () => {
  await visit('/', 390, async page => {
    await showOrder(page);
    await page.click(order);
    await expanded(page, order, true);
    await page.waitForSelector(`${dialog}[aria-modal="true"]`);
    const bullets = await page.$$eval(`${dialog} li`, nodes => nodes.map(node => node.textContent ?? ''));
    assert.equal(bullets.length, 2);
    assert.ok(bullets.join('').length > 300);
    assert.ok(bullets[0].includes('FIXTURE'), 'order number is interpolated from the fixture');
    assert.ok(bullets.every(text => !/^\s*[-•] /.test(text)));
    assert.ok(!(await page.$eval(dialog, node => node.textContent ?? '')).includes('\\n'));
    assert.equal(await page.$$eval(`${dialog} p`, nodes => nodes.length), 0, 'all-bullets content adds no paragraph');
    assert.equal(await page.$eval(dialog, node => node.classList.contains('w-60') || node.classList.contains('w-72')), false);
    const close = `${dialog} button[aria-label="بستن"]`;
    await focused(page, close);
    await page.keyboard.press('Tab');
    await focused(page, close);
    const backdrop = await page.$eval(dialog, node => {
      const backdrop = node.previousElementSibling as HTMLElement;
      return { hidden: backdrop.getAttribute('aria-hidden'), z: getComputedStyle(backdrop).zIndex };
    });
    assert.deepEqual(backdrop, { hidden: 'true', z: '10000' });
    await page.keyboard.press('Escape');
    await closed(page);
    await focused(page, order);
    await page.click(order);
    await expanded(page, order, true);
    await page.mouse.click(2, 2);
    await closed(page);
    await focused(page, order);
    await page.click(order);
    await expanded(page, order, true);
    await page.click(close);
    await closed(page);
    await focused(page, order);
  });
});

test('P1-5: replacing a sheet with a popover does not steal focus back to the closed sheet trigger', async () => {
  await visit('/', 390, async page => {
    await showOrder(page);
    await page.click(order);
    await expanded(page, order, true);
    // Invoke the real second trigger without backdrop/outside-click closing A first.
    await page.$eval(product, node => {
      (node as HTMLButtonElement).click();
      (node as HTMLButtonElement).focus({ preventScroll: true });
    });
    await expanded(page, order, false);
    await expanded(page, product, true);
    assert.equal(await page.$$eval(dialog, nodes => nodes.length), 1);
    assert.equal(await page.evaluate(query => document.activeElement === document.querySelector(query), order), false);
    // Reopen A and ensure the consumed suppression flag does not suppress Esc return.
    await page.keyboard.press('Escape');
    await closed(page);
    await ready(page, order);
    await page.click(order);
    await expanded(page, order, true);
    await page.keyboard.press('Escape');
    await closed(page);
    await focused(page, order);
  });
});

test('scroll inside help stays open; ancestor scroll closes; 390px edge geometry stays in bounds and flips', async () => {
  await visit('/', 390, async page => {
    await ready(page);
    // Controlled edge placements exercise measured DOM geometry on a real anchor.
    for (const [left, top] of [[0, 80], [366, 80], [100, 720]]) {
      await page.$eval(product, (node, position) => {
        Object.assign((node as HTMLElement).style,
          { position: 'fixed', left: `${position[0]}px`, top: `${position[1]}px`, zIndex: '9999' });
      }, [left, top]);
      await page.click(product);
      await expanded(page, product, true);
      await page.waitForFunction((query, flip) => {
        const panel = document.querySelector('[role="dialog"][aria-label="توضیح ساده"]');
        const trigger = document.querySelector(query);
        if (!panel || !trigger) return false;
        const rect = panel.getBoundingClientRect();
        return rect.width > 0 && rect.left >= 0 && rect.right <= 390 && rect.bottom <= innerHeight
          && (!flip || rect.bottom <= trigger.getBoundingClientRect().top);
      }, {}, product, top === 720);
      await page.keyboard.press('Escape');
      await closed(page);
    }
    await page.click(product);
    await expanded(page, product, true);
    // Constrain this short real text so it actually scrolls, without changing copy.
    await page.$eval(dialog, node => { (node as HTMLElement).style.maxHeight = '32px'; });
    assert.ok(await page.$eval(dialog, node => node.scrollHeight > node.clientHeight));
    await page.$eval(dialog, node => { node.scrollTop = 10; });
    await page.waitForFunction(query => (document.querySelector(query)?.scrollTop ?? 0) > 0, {}, dialog);
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expanded(page, product, true);
    // Scroll events do not bubble: capture on document must catch this ancestor.
    await page.$eval('#products-section', node => node.dispatchEvent(new Event('scroll')));
    await closed(page);
    await expanded(page, product, false);
  });
});
