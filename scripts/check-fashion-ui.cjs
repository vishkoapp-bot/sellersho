// Run from the repository root. Uses the real template with an isolated store fixture.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const puppeteer = require('puppeteer');

async function main() {
  const { createServer } = await import('vite');
  const root = process.cwd();
  const fixture = path.join(root, 'scripts/fixtures/fashion');
  const output = path.join(root, 'releases/fashion-ui-check');
  fs.mkdirSync(output, { recursive: true });
  const server = await createServer({
    configFile: path.join(root, 'vite.config.ts'),
    cacheDir: path.join(root, 'releases/.vite-fashion-test'),
    optimizeDeps: { entries: ['scripts/fixtures/fashion/index.html'] },
    server: { host: '127.0.0.1', port: 3113, fs: { allow: [root, fs.realpathSync(path.join(root, 'node_modules'))] } },
    plugins: [{ name: 'fashion-test-context', enforce: 'pre', resolveId(source) {
      if (source.endsWith('/context/StoreContext')) return path.join(fixture, 'context.tsx');
    } }],
  });
  let browser;
  try {
    await server.listen();
    const executablePath = process.env.CHROME_PATH || [
      'C:/Program Files/Google/Chrome/Application/chrome.exe',
      'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
    ].find(candidate => fs.existsSync(candidate));
    browser = await puppeteer.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.setViewport({ width: 1440, height: 1000 });
    await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/scripts/fixtures/fashion/index.html`, { waitUntil: 'networkidle0' });
    await page.evaluate(() => document.fonts.ready);
    for (const width of [320, 390, 768, 1024, 1440, 1920]) {
      await page.setViewport({ width, height: 900 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Page overflows at ${width}px`);
      const count = await page.$$('.fashion-product');
      assert.equal(count.length, 4);
      console.log(`PASS responsive layout at ${width}px`);
    }
    await page.setViewport({ width: 1440, height: 1000 });
    await page.screenshot({ path: path.join(output, 'desktop.png'), fullPage: true });
    await page.setViewport({ width: 390, height: 844 });
    await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
    await page.click('[aria-label="باز کردن منو"]');
    assert(await page.$('.fashion-header-menu'));
    await page.click('[aria-label="بستن منو"]');
    assert.equal(await page.$('.fashion-header-menu'), null);
    console.log('PASS mobile menu');

    await page.click('[aria-label="فیلتر دسته‌بندی محصولات"] button:nth-child(2)');
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 2);
    assert.equal(await page.$eval('[aria-label="فیلتر دسته‌بندی محصولات"] button:nth-child(2)', e => e.getAttribute('aria-pressed')), 'true');
    await page.click('[aria-label="فیلتر دسته‌بندی محصولات"] button:first-child');
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 4);
    await page.evaluate(() => window.preview.setSearchQuery('چرمی'));
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 1);
    await page.click('[aria-label="فیلتر دسته‌بندی محصولات"] button:first-child');
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 4);
    console.log('PASS category filtering, selected state, reset and search');

    await page.evaluate(() => {
      const product = window.preview.fixtureProducts[0];
      window.preview.setProducts([{ ...product, images: [product.image, '/templates/fashion/knitwear.webp'] }]);
    });
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 1);
    assert.equal(await page.$('.fashion-secondary-image'), null, 'Secondary photo loaded before interaction');
    await page.$eval('.fashion-product', e => e.focus());
    await page.waitForSelector('.fashion-secondary-image');
    await page.keyboard.press('Enter');
    assert.equal(await page.evaluate(() => window.lastAction.name), 'product');
    console.log('PASS deferred secondary image and keyboard product action');

    await page.evaluate(() => {
      const product = window.preview.fixtureProducts[1];
      window.preview.setProducts([{ ...product, image: '/missing-fashion-image.webp', images: [] }]);
    });
    await page.waitForSelector('[aria-label="تصویر محصول موجود نیست"]');
    assert.equal(await page.$('.fashion-product-image img'), null);
    await page.evaluate(() => {
      const product = window.preview.fixtureProducts[2];
      window.preview.setProducts([{ ...product, image: '/missing-fashion-primary.webp', images: ['/templates/fashion/knitwear.webp'] }]);
    });
    await page.waitForFunction(() => document.querySelector('.fashion-product-image img')?.getAttribute('src') === '/templates/fashion/knitwear.webp');
    console.log('PASS missing image and alternate-image recovery');

    // A slow deferred request must not blank an already loaded primary image.
    await page.setViewport({ width: 1440, height: 900 });
    let heldRequest;
    await page.setRequestInterception(true);
    const intercept = request => {
      if (request.url().includes('slow-secondary')) heldRequest = request;
      else request.continue();
    };
    page.on('request', intercept);
    await page.evaluate(() => {
      const product = window.preview.fixtureProducts[0];
      window.preview.setProducts([{ ...product, id: 'slow-photo', images: [product.image, '/templates/fashion/knitwear.webp?slow-secondary=1'] }]);
    });
    await page.waitForFunction(() => document.querySelector('.fashion-product h3').textContent.includes('ژاکت'));
    await page.hover('.fashion-product');
    await page.waitForSelector('.fashion-secondary-image');
    await new Promise(resolve => setTimeout(resolve, 450));
    assert(heldRequest, 'The secondary request was not held');
    assert.equal(await page.$eval('.fashion-product-image > img:first-child', e => Number(getComputedStyle(e).opacity)), 1);
    await heldRequest.continue();
    await page.waitForSelector('.fashion-product-image[data-secondary-ready="true"]');
    await page.setRequestInterception(false);
    page.off('request', intercept);
    console.log('PASS slow secondary image retains the primary until loaded');

    await page.evaluate(() => window.preview.setConfig({ custom: { heroImage: '' } }));
    await page.waitForSelector('.fashion-hero-text-only');
    const textHeroWidths = await page.evaluate(() => ({ hero: document.querySelector('.fashion-hero').clientWidth, copy: document.querySelector('.fashion-hero-copy').clientWidth }));
    assert(Math.abs(textHeroWidths.hero - textHeroWidths.copy) < 2, 'Text-only desktop hero leaves an empty column');
    console.log('PASS text-only desktop hero');

    await page.evaluate(() => {
      const product = window.preview.fixtureProducts[3];
      window.preview.setProducts([{ ...product, title: 'عنوان بلند محصول '.repeat(12), variants: [{ id: 'default', isDefault: true }], variantPrices: { default: { price: 1111000, originalPrice: 1234000, discountPercent: 10 } } }]);
      window.preview.setConfig({ custom: { heroTitle: 'عنوان بلند فروشنده '.repeat(8), heroSubtitle: 'متنبدونفاصله'.repeat(30), lookbookTitle: 'عنوانبدونفاصله'.repeat(20), lookbookSubtitle: 'متنبدونفاصله'.repeat(30), heroImage: '/templates/fashion/knitwear.webp' } });
    });
    await page.waitForFunction(() => document.querySelector('h1').textContent.includes('عنوان بلند فروشنده'));
    for (const width of [320, 768, 1024]) {
      await page.setViewport({ width, height: 900 });
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Long seller text overflows at ${width}px`);
    }
    const price = await page.$eval('.fashion-price > span', e => e.textContent);
    assert.equal(price.replace(/[۰-۹]/g, d => String(d.charCodeAt(0) - 1776)).replace(/\D/g, ''), '1111000');
    assert.equal(await page.$eval('.fashion-hero-photo', e => e.getAttribute('src')), '/templates/fashion/knitwear.webp');
    console.log('PASS long seller copy, replacement image and default-variant price');
    await page.evaluate(() => { window.preview.setSearchQuery('بدون نتیجه'); });
    await page.waitForFunction(() => document.querySelectorAll('.fashion-product').length === 0);
    assert((await page.$eval('#products', e => e.textContent)).includes('یافت نشد'));
    assert.deepEqual(errors, []);
    console.log('PASS empty search and no browser exceptions');
  } finally {
    if (browser) await browser.close();
    await server.close();
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
