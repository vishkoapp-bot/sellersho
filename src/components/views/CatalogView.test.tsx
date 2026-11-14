import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser, type Page } from 'puppeteer';
import { activeStoreSlugOf } from '../../context/StoreContext';
import type { Store } from '../../types';

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
  browser = await puppeteer.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); });

async function ready(page: Page) {
  await page.goto(url);
  await page.waitForFunction(() => {
    const h = (window as any).catalogHarness;
    return h?.state && !h.state().loading && h.state().products === 2;
  });
}

test('dropshipper catalog suppresses the central assortment on its first commit and then bounces', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await page.evaluate(() => (window as any).catalogHarness.select('mone'));
    await page.waitForFunction(() => (window as any).catalogHarness.state().slug === 'mone');
    await page.evaluate(() => (window as any).catalogHarness.open());
    await page.waitForFunction(() => (window as any).catalogHarness.state().view === 'seller_storefront');
    const result = await page.evaluate(() => {
      const h = (window as any).catalogHarness;
      return { paints: h.paints, state: h.state(), text: document.body.textContent };
    });
    assert.ok(result.paints.length > 0);
    assert.equal(result.paints[0].view, 'catalog', 'layout recording precedes the navigation effect');
    for (const paint of result.paints) {
      assert.equal(paint.rows, 0); assert.equal(paint.heading, false);
    }
    assert.equal(result.state.slug, 'mone');
    assert.doesNotMatch(result.text, /تیشرت زنانه|شلوار مردانه|کاتالوگ جامع کالاها/);
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

test('catalog navigation preserves the selected dropshipper store', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage(); await ready(page);
    await page.evaluate(() => (window as any).catalogHarness.select('mone'));
    await page.waitForFunction(() => (window as any).catalogHarness.state().slug === 'mone');
    await page.evaluate(() => (window as any).catalogHarness.navigateCatalog());
    await page.waitForFunction(() => (window as any).catalogHarness.state().view === 'catalog');
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
