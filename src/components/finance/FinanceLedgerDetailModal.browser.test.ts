import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { test } from 'node:test';
import { build } from 'esbuild';
import puppeteer from 'puppeteer';

test('ledger loads the next cursor page and can open an earlier date window', { timeout: 90000 }, async () => {
  const bundle = await build({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{
      name: 'store-fixture',
      setup(builder) {
        builder.onResolve({ filter: /(?:^|\/)StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
        builder.onLoad({ filter: /.*/, namespace: 'fixture' }, () => ({
          loader: 'js', contents: 'export const useStore = () => ({formatPrice: String, toPersianDigits: String});',
        }));
      },
    }],
    stdin: {
      loader: 'tsx', resolveDir: process.cwd(),
      contents: `
        import React from 'react';
        import { createRoot } from 'react-dom/client';
        import { FinanceLedgerDetailModal } from './src/components/finance/FinanceLedgerDetailModal';
        import { tokenStore } from './src/services/apiClient';
        tokenStore.setToken('synthetic');
        createRoot(document.getElementById('root')).render(<FinanceLedgerDetailModal isOpen onClose={() => {}} />);
      `,
    },
  });
  const requests: URLSearchParams[] = [];
  const server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://localhost');
    if (url.pathname !== '/api/v1/finance/ledger') {
      response.setHeader('Content-Type', 'text/html');
      response.end('<div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
      return;
    }
    requests.push(url.searchParams);
    const page = requests.length;
    const entries = Array.from({ length: page === 1 ? 100 : 1 }, (_, index) => ({
      id: `ledger-${page}-${index}`, date: url.searchParams.get('dateFrom'),
      reason: page === 1 ? 'initial ledger entry' : page === 2 ? 'older ledger entry' : 'earlier window entry',
      type: 'credit', targetAccount: 'platform', amount: 100,
    }));
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ entries, hasMore: page === 1, nextCursor: page === 1 ? 'ledger+next/cursor' : '', limit: 100 }));
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  const browser = await puppeteer.launch({
    headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'],
    ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}),
  }).catch(async error => {
    await new Promise<void>(resolve => server.close(() => resolve()));
    throw error;
  });
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${address.port}/ledger-fixture`);
    await page.waitForFunction(() => document.body.textContent?.includes('initial ledger entry'));
    const text = await page.$eval('body', body => body.textContent ?? '');
    assert.match(text, /اسناد ۳۱ روز اخیر/);
    assert.match(text, /100 سند نمایش داده شده/);
    assert.match(requests[0].get('dateFrom') ?? '', /^\d{4}-\d{2}-\d{2}$/);
    assert.equal(requests[0].get('limit'), '100');
    const click = (name: string) => page.evaluate(label => {
      const button = Array.from(document.querySelectorAll('button')).find(element => element.textContent?.trim() === label);
      if (!button) throw new Error('Button not found');
      button.click();
    }, name);
    await click('بارگذاری اسناد بیشتر');
    await page.waitForFunction(() => document.body.textContent?.includes('older ledger entry'));
    assert.equal(requests[1].get('cursor'), 'ledger+next/cursor');
    assert.equal(requests[1].get('dateFrom'), requests[0].get('dateFrom'));
    assert.equal(requests[1].get('dateTo'), requests[0].get('dateTo'));
    assert.match(await page.$eval('body', body => body.textContent ?? ''), /101 سند نمایش داده شده/);
    assert.doesNotMatch(await page.$eval('body', body => body.textContent ?? ''), /بارگذاری اسناد بیشتر/);
    await click('بازهٔ قدیمی‌تر');
    await page.waitForFunction(() => document.body.textContent?.includes('earlier window entry'));
    assert.ok(requests[2].get('dateTo')! < requests[0].get('dateFrom')!);
    assert.equal(requests[2].get('cursor'), null);
    assert.doesNotMatch(await page.$eval('body', body => body.textContent ?? ''), /initial ledger entry/);
  } finally {
    await browser.close();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
