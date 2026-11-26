import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser, type ElementHandle, type Page } from 'puppeteer';
import { activeStoreSlugOf } from '../../context/StoreContext';
import type { Store } from '../../types';
import { batch8Browser, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

let server: ViteDevServer;
let browser: Browser;
let url: string;
before(async () => {
  server = await createServer({ configFile: false, logLevel: 'error', cacheDir: 'releases/.vite-catalog-test',
    esbuild: { jsx: 'automatic' }, define: { 'import.meta.env.VITE_USE_MOCK': '"false"' },
    optimizeDeps: { entries: ['scripts/fixtures/catalog/index.html'] }, server: { host: '127.0.0.1', port: 0 } });
  await server.listen();
  url = `http://127.0.0.1:${(server.httpServer!.address() as any).port}/scripts/fixtures/catalog/index.html`;
  const executablePath = process.env.CHROME_PATH || ['C:/Program Files/Google/Chrome/Application/chrome.exe',
    'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
  browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'], ...(executablePath ? { executablePath } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); });

async function ready(page: Page) {
  await page.goto(url);
  await page.waitForFunction(() => {
    const h = (window as any).catalogHarness;
    return h?.state && !h.state().loading && h.state().products === 2;
  });
}

test('explicit central catalog does not reuse the previous storefront scope', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await ready(page);
    await page.evaluate(() => (window as any).catalogHarness.select('mone'));
    await page.waitForFunction(() => (window as any).catalogHarness.state().slug === 'mone');
    await page.evaluate(() => (window as any).catalogHarness.open());
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
    const result = await page.evaluate(() => {
      const h = (window as any).catalogHarness;
      return { paints: h.paints, state: h.state() };
    });
    assert.ok(result.paints.length > 0);
    assert.equal(result.paints[0].view, 'catalog');
    assert.equal(result.state.slug, 'central');
    assert.equal(result.state.view, 'catalog');
  } finally { await context.close(); }
});

test('central catalog renders both products and stays in the catalog', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await page.evaluate(() => (window as any).catalogHarness.open());
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
    const result = await page.evaluate(() => {
      const h = (window as any).catalogHarness;
      return { paints: h.paints, state: h.state(), text: document.body.textContent };
    });
    assert.equal(result.paints[0].view, 'catalog');
    assert.equal(result.paints[0].rows, 2); assert.equal(result.paints[0].heading, true);
    assert.equal(result.state.view, 'catalog'); assert.equal(result.state.slug, 'central');
    assert.match(result.text, /تیشرت زنانه/); assert.match(result.text, /شلوار مردانه/);
  } finally { await context.close(); }
});

test('category discovery inside the storefront stays scoped to that storefront', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await page.evaluate(() => (window as any).catalogHarness.select('mone'));
    await page.waitForFunction(() => (window as any).catalogHarness.state().slug === 'mone');
    await page.evaluate(() => (window as any).catalogHarness.navigateCatalog());
    await page.waitForFunction(() => (window as any).catalogHarness.state().view === 'seller_storefront');
    assert.equal(await page.evaluate(() => (window as any).catalogHarness.state().slug), 'mone');
  } finally { await context.close(); }
});

test('store scope derives from the selected or routed store', () => {
  const central = { slug: 'central', ownerType: 'central' } as Store;
  const mone = { slug: 'mone', ownerType: 'dropshipper' } as Store;
  assert.equal(activeStoreSlugOf({ routeStoreSlug: null, currentStore: mone }), 'mone');
  assert.equal(activeStoreSlugOf({ routeStoreSlug: null, currentStore: central }), null);
  assert.equal(activeStoreSlugOf({ routeStoreSlug: 'mone', currentStore: central }), 'mone');
});

const RANGE_ERROR = 'حداقل قیمت نمی‌تواند بیشتر از حداکثر قیمت باشد';
const MIN_LABEL = 'حداقل قیمت (تومان)';
const MAX_LABEL = 'حداکثر قیمت (تومان)';
const SLIDER = '[data-testid="standalone-slider"]';

async function typeIntoStandalone(page: Page, label: string, digits: string) {
  const selector = `${SLIDER} input[aria-label="${label}"]`;
  const input = await page.$(selector);
  assert.ok(input, `missing standalone slider input ${selector}`);
  await input.click();
  await input.evaluate((el) => { const inputEl = el as HTMLInputElement; inputEl.setSelectionRange(0, inputEl.value.length); });
  await page.keyboard.type(digits);
}

async function sliderSnapshot(page: Page) {
  return page.evaluate(() => {
    const h = (window as any).catalogHarness;
    const root = document.querySelector('[data-testid="standalone-slider"]')!;
    const min = root.querySelector('input[aria-label="حداقل قیمت (تومان)"]') as HTMLInputElement;
    const max = root.querySelector('input[aria-label="حداکثر قیمت (تومان)"]') as HTMLInputElement;
    const err = root.querySelector('[data-testid="price-range-error"]');
    return {
      value: h.slider.value() as [number, number],
      changes: h.slider.changes() as Array<[number, number]>,
      error: err ? err.textContent : null,
      minValue: min.value,
      maxValue: max.value,
      activeIsMin: document.activeElement === min,
      activeIsMax: document.activeElement === max,
    };
  });
}

async function catalogInputHandle(page: Page, label: string) {
  const handle = await page.evaluateHandle((lbl) => {
    return [...document.querySelectorAll('input')].find((el) => el.getAttribute('aria-label') === lbl && !el.closest('[data-testid="standalone-slider"]')) ?? null;
  }, label);
  const el = handle.asElement() as ElementHandle<HTMLInputElement> | null;
  assert.ok(el, `missing catalog input ${label}`);
  return el;
}

async function typeIntoCatalog(page: Page, label: string, digits: string) {
  const input = await catalogInputHandle(page, label);
  await input.click();
  await input.evaluate((el) => { const inputEl = el as HTMLInputElement; inputEl.setSelectionRange(0, inputEl.value.length); });
  await page.keyboard.type(digits);
}

async function catalogPriceInputs(page: Page) {
  return page.evaluate(() => {
    const els = [...document.querySelectorAll('input')].filter((e) => !e.closest('[data-testid="standalone-slider"]'));
    const min = els.find((e) => e.getAttribute('aria-label') === 'حداقل قیمت (تومان)') as HTMLInputElement | undefined;
    const max = els.find((e) => e.getAttribute('aria-label') === 'حداکثر قیمت (تومان)') as HTMLInputElement | undefined;
    return [min ? min.value : null, max ? max.value : null];
  });
}

async function openCatalog(page: Page) {
  await page.evaluate(() => (window as any).catalogHarness.open());
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
}

async function clickButtonWithText(page: Page, text: string) {
  await page.evaluate((t) => {
    [...document.querySelectorAll('button')].find((b) => b.textContent?.includes(t))?.click();
  }, text);
}

test('A1 — min-then-max inversion is rejected inline, keeps the draft and fires no inverted commit', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await typeIntoStandalone(page, MIN_LABEL, '50000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[0] === 50000000);
    await typeIntoStandalone(page, MAX_LABEL, '1000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.querySelector('[data-testid="standalone-slider"] [data-testid="price-range-error"]'));
    const snap = await sliderSnapshot(page);
    assert.equal(snap.error, RANGE_ERROR);
    assert.equal(snap.maxValue, '۱٬۰۰۰٬۰۰۰');
    assert.equal(snap.activeIsMax, true);
    assert.deepEqual(snap.value, [50000000, 63800000]);
    assert.ok(!snap.changes.some(([a, b]) => a > b), 'no inverted pair was ever committed');
  } finally { await context.close(); }
});

test('A2 — reverse order inversion is rejected the same way', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await typeIntoStandalone(page, MAX_LABEL, '1000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[1] === 1000000);
    await typeIntoStandalone(page, MIN_LABEL, '50000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.querySelector('[data-testid="standalone-slider"] [data-testid="price-range-error"]'));
    const snap = await sliderSnapshot(page);
    assert.equal(snap.error, RANGE_ERROR);
    assert.equal(snap.minValue, '۵۰٬۰۰۰٬۰۰۰');
    assert.equal(snap.activeIsMin, true);
    assert.deepEqual(snap.value, [198000, 1000000]);
    assert.ok(!snap.changes.some(([a, b]) => a > b), 'no inverted pair was ever committed');
  } finally { await context.close(); }
});

test('A3 — equal bounds are valid, not an inversion', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await typeIntoStandalone(page, MIN_LABEL, '1000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[0] === 1000000);
    await typeIntoStandalone(page, MAX_LABEL, '1000000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[1] === 1000000);
    const snap = await sliderSnapshot(page);
    assert.equal(snap.error, null);
    assert.deepEqual(snap.value, [1000000, 1000000]);
  } finally { await context.close(); }
});

test('A4 — dragging the min thumb past the max stops at max with the message', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    // Start below the full range: from [198000, 63800000] the pointer
    // fraction caps at hi, so the min thumb could only ever reach hi
    // exactly and never cross it. With hi at 50000000 a drag past the
    // max thumb position is a real crossing.
    await page.evaluate(() => (window as any).catalogHarness.slider.set([198000, 50000000]));
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[1] === 50000000);
    // The fixture page loads no CSS, so the track is 0px tall (h-6 never
    // applies) and no mouse point lands on it. Give it its real height
    // first; the pointer handlers only need the track rect.
    const track = await page.evaluate(() => {
      const root = document.querySelector('[data-testid="standalone-slider"]')!;
      const el = root.querySelector('[data-thumb="min"]')!.parentElement!;
      el.style.height = '24px';
      const r = el.getBoundingClientRect();
      return { left: r.left, right: r.right, y: r.top + r.height / 2 };
    });
    await page.mouse.move(track.right - 5, track.y);
    await page.mouse.down();
    await page.mouse.move(track.left + 5, track.y, { steps: 20 });
    await page.mouse.up();
    await page.waitForFunction(() => document.querySelector('[data-testid="standalone-slider"] [data-testid="price-range-error"]'));
    let snap = await sliderSnapshot(page);
    assert.deepEqual(snap.value, [50000000, 50000000]);
    assert.equal(snap.error, RANGE_ERROR);
    // Drag back (right, toward lower values) from the track clears it:
    // both thumbs sit at the max, so a track press ties and picks min.
    await page.mouse.move(track.left + 100, track.y);
    await page.mouse.down();
    await page.mouse.move(track.left + 180, track.y, { steps: 10 });
    await page.mouse.up();
    await page.waitForFunction(() => !document.querySelector('[data-testid="standalone-slider"] [data-testid="price-range-error"]'));
    snap = await sliderSnapshot(page);
    assert.ok(snap.value[0] < snap.value[1], `expected a valid pair after dragging back, got ${snap.value}`);
  } finally { await context.close(); }
});

test('A5 — keyboard crossing stops at the other bound with the message', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    // End on the min thumb: [198000, 5000000] -> End wants boundMax.
    await page.evaluate(() => (window as any).catalogHarness.slider.set([198000, 5000000]));
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[1] === 5000000);
    await page.evaluate(() => (document.querySelector('[data-testid="standalone-slider"] [data-thumb="min"]') as HTMLElement).focus());
    await page.keyboard.press('End');
    await page.waitForFunction(() => document.querySelector('[data-testid="standalone-slider"] [data-testid="price-range-error"]'));
    let snap = await sliderSnapshot(page);
    assert.deepEqual(snap.value, [5000000, 5000000]);
    assert.equal(snap.error, RANGE_ERROR);
    // Home on the max thumb: [1000000, 63800000] -> Home wants boundMin.
    await page.evaluate(() => (window as any).catalogHarness.slider.set([1000000, 63800000]));
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[0] === 1000000);
    await page.evaluate(() => (document.querySelector('[data-testid="standalone-slider"] [data-thumb="max"]') as HTMLElement).focus());
    await page.keyboard.press('Home');
    await page.waitForFunction(() => (window as any).catalogHarness.slider.value()[1] === 1000000);
    snap = await sliderSnapshot(page);
    assert.deepEqual(snap.value, [1000000, 1000000]);
    assert.equal(snap.error, RANGE_ERROR);
  } finally { await context.close(); }
});

test('A6 — live digit formatting with caret preservation, and Latin paste', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    const input = await page.$(`${SLIDER} input[aria-label="${MAX_LABEL}"]`);
    assert.ok(input);
    await input.click();
    await input.evaluate((el) => { const inputEl = el as HTMLInputElement; inputEl.setSelectionRange(0, inputEl.value.length); });
    const expected = ['۱', '۱۰', '۱۰۰', '۱٬۰۰۰', '۱۰٬۰۰۰', '۱۰۰٬۰۰۰', '۱٬۰۰۰٬۰۰۰'];
    for (let i = 0; i < 7; i++) {
      await page.keyboard.press((['1', '0', '0', '0', '0', '0', '0'] as const)[i]);
      await page.waitForFunction((exp) => {
        const el = document.querySelector('[data-testid="standalone-slider"] input[aria-label="حداکثر قیمت (تومان)"]') as HTMLInputElement;
        return el.value === exp;
      }, {}, expected[i]);
      const state = await page.evaluate(() => {
        const el = document.querySelector('[data-testid="standalone-slider"] input[aria-label="حداکثر قیمت (تومان)"]') as HTMLInputElement;
        return { value: el.value, caret: el.selectionStart };
      });
      assert.equal(state.value, expected[i]);
      assert.equal(state.caret, state.value.length, `caret not at end after keystroke ${i + 1}`);
    }
    // Paste Latin ungrouped digits into the min field (one input event
    // carrying the whole string, like a real paste). Do not focus() the
    // target first: focusing blurs the max input, whose commit re-renders
    // and restores the pasted value before the event is dispatched.
    await page.evaluate(() => {
      const el = document.querySelector('[data-testid="standalone-slider"] input[aria-label="حداقل قیمت (تومان)"]') as HTMLInputElement;
      const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
      setter.call(el, '50000000');
      el.dispatchEvent(new InputEvent('input', { bubbles: true }));
    });
    await page.waitForFunction(() => {
      const el = document.querySelector('[data-testid="standalone-slider"] input[aria-label="حداقل قیمت (تومان)"]') as HTMLInputElement;
      return el.value === '۵۰٬۰۰۰٬۰۰۰';
    });
    const minValue = await page.evaluate(() => (document.querySelector('[data-testid="standalone-slider"] input[aria-label="حداقل قیمت (تومان)"]') as HTMLInputElement).value);
    assert.equal(minValue, '۵۰٬۰۰۰٬۰۰۰');
  } finally { await context.close(); }
});

test('A7 — header reset clears an active price filter while on «همه», and empty-state resets match', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await openCatalog(page);
    // A valid price range: min 900000 leaves only the 1,200,000 product.
    await typeIntoCatalog(page, MIN_LABEL, '900000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 1);
    await clickButtonWithText(page, 'حذف فیلترها');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
    assert.deepEqual(await catalogPriceInputs(page), ['۸۵۰٬۰۰۰', '۱٬۲۰۰٬۰۰۰']);
    assert.equal(await page.evaluate(() => [...document.querySelectorAll('button')].some((b) => b.textContent?.includes('حذف فیلترها'))), false);
    // A valid range with zero results shows the empty state; its
    // «حذف فیلترها» must behave identically (same handler).
    await typeIntoCatalog(page, MIN_LABEL, '900000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 1);
    await typeIntoCatalog(page, MAX_LABEL, '900000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.body.textContent?.includes('کالایی یافت نشد'));
    await clickButtonWithText(page, 'حذف فیلترها');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
    assert.deepEqual(await catalogPriceInputs(page), ['۸۵۰٬۰۰۰', '۱٬۲۰۰٬۰۰۰']);
    // Same again, this time via «مشاهده همه محصولات».
    await typeIntoCatalog(page, MIN_LABEL, '900000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 1);
    await typeIntoCatalog(page, MAX_LABEL, '900000');
    await page.keyboard.press('Tab');
    await page.waitForFunction(() => document.body.textContent?.includes('کالایی یافت نشد'));
    await clickButtonWithText(page, 'مشاهده همه محصولات');
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 2);
    assert.deepEqual(await catalogPriceInputs(page), ['۸۵۰٬۰۰۰', '۱٬۲۰۰٬۰۰۰']);
  } finally { await context.close(); }
});

test('A8 — a forced inverted filter 400s into the inline message, not the generic banner', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await openCatalog(page);
    await page.evaluate(() => (window as any).catalogHarness.forceFilter({ minPrice: 50000000, maxPrice: 1000000 }));
    await page.waitForFunction(() => document.querySelector('[data-testid="price-range-error"]'));
    const result = await page.evaluate(() => {
      const h = (window as any).catalogHarness;
      return {
        error: document.querySelector('[data-testid="price-range-error"]')?.textContent ?? null,
        genericBanner: document.querySelectorAll('[data-testid="catalog-load-error"]').length,
        items: document.querySelectorAll('[data-testid="product-item"]').length,
        emptyState: document.body.textContent?.includes('کالایی یافت نشد') ?? false,
        rawBody: document.body.textContent?.includes('{"error"') ?? false,
        flag: h.state().priceRangeError,
      };
    });
    assert.equal(result.error, RANGE_ERROR);
    assert.equal(result.genericBanner, 0);
    assert.equal(result.items, 2);
    assert.equal(result.emptyState, false);
    assert.equal(result.rawBody, false);
    assert.equal(result.flag, true);
    // A valid range clears the message and refilters.
    await page.evaluate(() => (window as any).catalogHarness.forceFilter({ minPrice: 1000000, maxPrice: 5000000 }));
    await page.waitForFunction(() => !document.querySelector('[data-testid="price-range-error"]'));
    await page.waitForFunction(() => document.querySelectorAll('[data-testid="product-item"]').length === 1);
    assert.equal(await page.evaluate(() => (window as any).catalogHarness.state().priceRangeError), false);
  } finally { await context.close(); }
});

const visitCatalog = batch8Browser();

test('catalog load-more shows the remaining count and keeps the header total', async () => {
  for (const [total, header, remaining] of [[41, '۴۱', '۱۷'], [25, '۲۵', '۱']] as const) {
    await visitCatalog(`/?category=all&total=${total}`, 390, async page => {
      await waitText(page, `${header} کالا`);
      await waitText(page, `نمایش بیشتر (${remaining} کالای دیگر)`);
      assert.doesNotMatch(await page.$eval('body', body => body.innerText), /نمایش بیشتر \((۴۱|۲۵)/);
    });
  }
});

test('catalog has no load-more button when all products are loaded', async () => {
  await visitCatalog('/?category=all&total=24', 390, async page => {
    await waitText(page, '۲۴ کالا');
    assert.doesNotMatch(await page.$eval('body', body => body.innerText), /نمایش بیشتر/);
  });
});
