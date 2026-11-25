import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';
import tailwindcss from '@tailwindcss/vite';
import { build as bundleScript } from 'esbuild';
import puppeteer from 'puppeteer';
import { build as buildStyles } from 'vite';

const draftKey = 'sellersho:supplier-propose-draft:v1';
const thumbnails = '.group > img[alt^="تصویر "]';
const fixtures = [
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPQkE8GAAEdAKsz/POGAAAAAElFTkSuQmCC',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGOIdfkBAAKbAZrQLohoAAAAAElFTkSuQmCC',
];
const uploadedImage = fixtures[0] + '#uploaded';

test('mobile proposal removes photos by touch, including while another photo uploads', { timeout: 45000 }, async () => {
  const bundle = await bundleScript({
    write: false, bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic',
    define: { 'process.env.NODE_ENV': '"production"', 'import.meta.env': '{}' },
    plugins: [{ name: 'supplier-store', setup(build) {
      build.onResolve({ filter: /context\/StoreContext$/ }, () => ({ path: 'store', namespace: 'fixture' }));
      build.onLoad({ filter: /^store$/, namespace: 'fixture' }, () => ({
        contents: "export const useStore=()=>({toPersianDigits:(n)=>String(n).replace(/\\d/g,d=>'۰۱۲۳۴۵۶۷۸۹'[Number(d)])});",
      }));
    } }],
    stdin: { loader: 'tsx', resolveDir: process.cwd(), contents: `
      import { createRoot } from 'react-dom/client';
      import { ToastProvider } from './src/context/ToastContext';
      import { SupplierProposeForm } from './src/components/supplier/SupplierProposeForm';
      createRoot(document.getElementById('root')).render(<ToastProvider>
        <SupplierProposeForm warranties={[]} myWarehouses={[]} profileCity="" profileCityConfirmed={false}
          onSubmitted={()=>{}} onChallengeSubmitted={()=>{}} />
      </ToastProvider>);
    ` },
  });
  const cssBuild = await buildStyles({
    configFile: false, plugins: [tailwindcss()], logLevel: 'silent',
    build: { write: false, rollupOptions: { input: 'src/index.css' } },
  });
  const outputs = Array.isArray(cssBuild) ? cssBuild : [cssBuild];
  const css = outputs.flatMap(result => 'output' in result ? result.output : [])
    .find(asset => asset.type === 'asset' && asset.fileName.endsWith('.css'));
  assert.ok(css?.type === 'asset', 'fixture uses the production Tailwind styles');

  let finishUpload!: () => void;
  const uploadGate = new Promise<void>(resolve => { finishUpload = resolve; });
  const server = createServer(async (request, response) => {
    if (request.method === 'POST' && request.url === '/api/v1/upload/product-image') {
      for await (const _chunk of request) { /* consume the multipart body */ }
      await uploadGate;
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify({ success: true, imageUrl: uploadedImage }));
    } else if (request.url === '/fixture.css') {
      response.setHeader('Content-Type', 'text/css');
      response.end(css.source);
    } else if (request.url?.startsWith('/api/')) {
      response.setHeader('Content-Type', 'application/json');
      response.end('[]');
    } else {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      response.end('<!doctype html><html lang="fa" dir="rtl"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/fixture.css"><div id="root"></div><script>' + bundle.outputFiles[0].text + '</script>');
    }
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  assert.ok(address && typeof address === 'object');
  try {
    const browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox'], ...(process.platform === 'win32' ? { channel: 'chrome' as const } : {}) });
    try {
      const page = await browser.newPage();
      page.on('pageerror', error => console.error('Form fixture error:', error));
      await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });
      await page.evaluateOnNewDocument((key, images) => {
        if (!localStorage.getItem(key)) localStorage.setItem(key, JSON.stringify({ version: 1, step: 4, images }));
      }, draftKey, fixtures);
      await page.goto(`http://127.0.0.1:${address.port}/`);
      await page.waitForFunction(selector => {
        const images = [...document.querySelectorAll<HTMLImageElement>(selector)];
        return images.length === 2 && images.every(image => image.complete && image.naturalWidth > 0);
      }, { timeout: 5000 }, thumbnails);
      const buttons = await page.$$eval('button[title="حذف"]', elements => elements.map(button => {
        const rect = button.getBoundingClientRect();
        return { opacity: getComputedStyle(button).opacity, width: rect.width, height: rect.height, label: button.getAttribute('aria-label') };
      }));
      assert.deepEqual(buttons.map(button => button.label), ['حذف تصویر ۱', 'حذف تصویر ۲']);
      for (const button of buttons) {
        assert.equal(button.opacity, '1');
        assert.ok(button.width >= 32 && button.height >= 32, `delete target is ${button.width}×${button.height}px`);
      }
      const photos = () => page.$$eval(thumbnails, images => images.map(image => image.getAttribute('src')));
      const remove = async (number: '۱' | '۲') => {
        const button = await page.waitForSelector(`button[aria-label="حذف تصویر ${number}"]`, { visible: true, timeout: 5000 });
        assert.ok(button);
        await button.tap();
      };
      await remove('۲');
      await page.waitForFunction(selector => document.querySelectorAll(selector).length === 1, { timeout: 5000 }, thumbnails);
      assert.deepEqual(await photos(), [fixtures[0]]);

      const request = page.waitForRequest(req => req.method() === 'POST' && req.url().endsWith('/upload/product-image'), { timeout: 5000 });
      await page.$eval('#product-image-input', element => {
        const transfer = new DataTransfer();
        transfer.items.add(new File(['fixture'], 'third.png', { type: 'image/png' }));
        const input = element as HTMLInputElement;
        input.files = transfer.files;
        input.dispatchEvent(new Event('change', { bubbles: true }));
      });
      await request;
      await remove('۱');
      await page.waitForFunction(selector => document.querySelectorAll(selector).length === 0, { timeout: 5000 }, thumbnails);
      finishUpload();
      await page.waitForFunction((selector, src) =>
        [...document.querySelectorAll(selector)].some(image => image.getAttribute('src') === src),
      { timeout: 5000 }, thumbnails, uploadedImage);
      assert.deepEqual(await photos(), [uploadedImage], 'the completed upload cannot restore a removed photo');
      await page.waitForFunction((key, src) => {
        const saved = JSON.parse(localStorage.getItem(key) ?? '{}');
        return saved.images?.length === 1 && saved.images[0] === src;
      }, { timeout: 5000 }, draftKey, uploadedImage);
      await page.reload();
      await page.waitForSelector(thumbnails, { timeout: 5000 });
      assert.deepEqual(await photos(), [uploadedImage]);
      assert.equal(await page.$eval('img[alt="تصویر شاخص"]', image => image.getAttribute('src')), uploadedImage);
    } finally { await browser.close(); }
  } finally {
    finishUpload();
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
});
