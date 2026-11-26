import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, before } from 'node:test';
import { build } from 'esbuild';
import puppeteer, { type Browser, type Page } from 'puppeteer';
import tailwindcss from '@tailwindcss/vite';
import { createServer, type ViteDevServer } from 'vite';

export function batch8Browser({ inlineVazirmatn = false } = {}) {
  let browser: Browser;
  let server: ViteDevServer;
  let cacheDir: string;
  const origin = 'http://batch8.test';
  let html: string;
  let javascript: string;
  let styles: string;
  before(async () => {
    cacheDir = await mkdtemp(join(tmpdir(), 'batch8-vite-'));
    const bundle = await build({
      entryPoints: ['scripts/fixtures/catalog/composed.provider.tsx'],
      write: false,
      bundle: true,
      platform: 'browser',
      format: 'iife',
      jsx: 'automatic',
      loader: { '.css': 'empty' },
      define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{"VITE_USE_MOCK":"false"}' },
    });
    server = await createServer({
      configFile: false,
      plugins: [tailwindcss()],
      esbuild: { jsx: 'automatic' },
      cacheDir,
      logLevel: 'error',
      optimizeDeps: { noDiscovery: true },
      server: { middlewareMode: true, hmr: false, ws: false, watch: null },
    });
    javascript = bundle.outputFiles[0].text;
    html = await readFile('scripts/fixtures/catalog/composed.html', 'utf8');
    const compiled = await server.transformRequest('/src/index.css?direct');
    if (!compiled) throw new Error('Missing compiled fixture stylesheet');
    styles = compiled.code + '\n' + await readFile('src/components/storefront/templates/fashion.css', 'utf8');
    if (inlineVazirmatn) {
      // Geometry tests need the app font; the hermetic browser aborts asset requests.
      const sources = [...new Set([...styles.matchAll(/(?:[^"'()\s]*\/)?(Vazirmatn-[A-Za-z]+\.woff2)/g)].map(match => match[0]))];
      if (!sources.length) throw new Error('Missing Vazirmatn asset references in compiled fixture styles');
      for (const source of sources) {
        const file = source.split('/').at(-1)!;
        const font = await readFile(join('node_modules/vazirmatn/fonts/webfonts', file));
        styles = styles.replaceAll(source, `data:font/woff2;base64,${font.toString('base64')}`);
      }
    }
    browser = await puppeteer.launch({
      executablePath: process.env.CHROME_PATH,
      pipe: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox', '--no-proxy-server', '--disable-dev-shm-usage'],
    });
  });
  after(async () => {
    await browser?.close();
    await server?.close();
    if (cacheDir) await rm(cacheDir, { recursive: true, force: true });
  });
  return async (route: string, width: number, run: (page: Page) => Promise<void>) => {
    const context = await browser.createBrowserContext();
    try {
      const page = await context.newPage();
      await page.setViewport({ width, height: 760 });
      page.setDefaultTimeout(30000);
      page.on('pageerror', error => process.stderr.write(`Browser fixture error: ${String(error)}\n`));
      await page.setRequestInterception(true);
      page.on('request', request => {
        const url = new URL(request.url());
        if (request.isNavigationRequest() && url.origin === origin && url.pathname === '/fixture') {
          void request.respond({ status: 200, contentType: 'text/html', body: html });
        } else if (url.protocol === 'data:') {
          void request.continue();
        } else {
          void request.abort();
        }
      });
      const fixtureUrl = `${origin}/fixture?route=${encodeURIComponent(route)}`;
      const injectFixture = async () => {
        await page.addStyleTag({ content: styles });
        await page.addScriptTag({ content: javascript });
      };
      await page.goto(fixtureUrl, { waitUntil: 'domcontentloaded', timeout: 60000 });
      await injectFixture();
      Object.assign(page, {
        reloadFixture: async () => {
          // The bundle replaces the document URL with the app route on mount.
          await page.evaluate(url => history.replaceState(null, '', url), fixtureUrl);
          await page.reload({ waitUntil: 'domcontentloaded' });
          await injectFixture();
        },
      });
      await run(page);
    } finally {
      await context.close();
    }
  };
}

export const waitText = (page: Page, text: string) =>
  page.waitForFunction(value => document.body.innerText.includes(value), {}, text);

export async function clickText(page: Page, text: string, selector = 'button') {
  await page.waitForFunction((value, query) =>
    [...document.querySelectorAll<HTMLElement>(query)].some(element => element.innerText.trim() === value),
  {}, text, selector);
  await page.evaluate((value, query) =>
    [...document.querySelectorAll<HTMLElement>(query)].find(element => element.innerText.trim() === value)?.click(),
  text, selector);
}

export async function fillInput(page: Page, selector: string, value: string) {
  await page.waitForSelector(selector, { visible: true });
  await page.$eval(selector, (node, text) => {
    const input = node as HTMLInputElement;
    input.focus();
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    setValue.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }, value);
}
