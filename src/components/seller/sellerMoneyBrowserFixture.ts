import { after, before } from 'node:test';
import { resolve } from 'node:path';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser, type Page } from 'puppeteer';

export function sellerMoneyBrowserFixture(view: 'overview' | 'wallet' | 'analytics') {
  let server: ViteDevServer;
  let browser: Browser;
  let url: string;
  before(async () => {
    server = await createServer({
      configFile: false,
      logLevel: 'error',
      cacheDir: `releases/.vite-seller-money-${view}`,
      esbuild: { jsx: 'automatic' },
      define: { 'import.meta.env.VITE_USE_MOCK': '"false"' },
      optimizeDeps: { entries: ['scripts/fixtures/seller-money/index.html'] },
      resolve: {
        alias: { '../../context/StoreContext': resolve('scripts/fixtures/seller-money/storeContext.ts') },
      },
      server: { host: '127.0.0.1', port: 0 },
    });
    await server.listen();
    const address = server.httpServer!.address();
    if (!address || typeof address === 'string') throw new Error('Fixture server has no TCP address');
    url = `http://127.0.0.1:${address.port}/scripts/fixtures/seller-money/index.html?view=${view}`;
    browser = await puppeteer.launch({
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox'],
      executablePath: process.env.CHROME_PATH || undefined,
    });
  });
  after(async () => {
    await browser?.close();
    await server?.close();
  });

  return async (options: string, run: (page: Page) => Promise<void>) => {
    const context = await browser.createBrowserContext();
    try {
      const page = await context.newPage();
      await page.goto(url + options, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => !!(window as any).sellerMoney);
      await run(page);
    } finally {
      await context.close();
    }
  };
}
