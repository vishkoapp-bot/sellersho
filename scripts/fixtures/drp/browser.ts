import { build } from 'esbuild';
import puppeteer, { type HTTPRequest, type Page } from 'puppeteer';

const origin = 'http://seller-fixture.test';

// Exercise real React and API clients. The seller session/provider and HTTP
// responses are fixture inputs rather than live customer data.
export async function withSellerBrowser(
  entry: string,
  provider: string,
  run: (page: Page) => Promise<void>,
  route: (req: HTTPRequest) => { status?: number; body: unknown } | undefined = () => undefined,
  styles = '',
) {
  const { outputFiles } = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    loader: { '.css': 'empty' },
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{"VITE_USE_MOCK":"false"}' },
    stdin: {
      loader: 'tsx', resolveDir: process.cwd(),
      contents: `import React from 'react'; import {createRoot} from 'react-dom/client';
        import {tokenStore} from './src/services/apiClient'; tokenStore.setToken('test-token'); ${entry}`,
    },
    plugins: [{ name: 'seller-fixture-store', setup(b) {
      b.onResolve({ filter: /(?:^|\/)StoreContext$/ }, () => ({ path: 'store', namespace: 'seller-fixture' }));
      b.onResolve({ filter: /(?:^|\/)ToastContext$/ }, () => ({ path: 'toast', namespace: 'seller-fixture' }));
      b.onLoad({ filter: /.*/, namespace: 'seller-fixture' }, args => ({
        loader: 'jsx', resolveDir: process.cwd(),
        contents: args.path === 'store' ? provider : 'export const useToast=()=>({success:()=>{},error:()=>{}});',
      }));
    } }],
  });
  const browser = await puppeteer.launch({
    executablePath: process.env.CHROME_PATH,
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--no-proxy-server', '--disable-dev-shm-usage'],
  });
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: 1440, height: 900 });
    page.setDefaultTimeout(5000);
    await page.setRequestInterception(true);
    page.on('request', req => {
      const url = new URL(req.url());
      if (req.isNavigationRequest() && url.origin === origin && url.pathname === '/fixture') {
        void req.respond({ status: 200, contentType: 'text/html', body: '<!doctype html><meta charset="utf-8"><div id="root"></div>' });
      } else if (url.origin === origin && url.pathname.startsWith('/api/v1/')) {
        const response = route(req);
        void req.respond({ status: response?.status ?? (response ? 200 : 404), contentType: 'application/json', body: JSON.stringify(response?.body ?? null) });
      } else if (url.protocol === 'data:') {
        void req.continue();
      } else {
        void req.abort();
      }
    });
    await page.goto(`${origin}/fixture`);
    if (styles) await page.addStyleTag({ content: styles });
    await page.addScriptTag({ content: outputFiles[0].text });
    await run(page);
  } finally {
    await browser.close();
  }
}
