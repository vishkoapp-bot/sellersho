import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { createServer, type ViteDevServer } from 'vite';
import puppeteer, { type Browser, type Page } from 'puppeteer';

// D6 (Batch 4a): changing admin menu items resets the main scroll
// position. Real-browser test on the production AdminDashboardView
// (same puppeteer + vite-fixture harness as CatalogView.test.tsx):
// scroll a long tab to the bottom, switch tabs, assert scrollY === 0
// and the new tab's heading is visible. Also walks the eight D1 tabs
// and asserts the stale «پس از ورود به حساب» copy is gone from each.
// The super_admin session is seeded in localStorage before mount
// (saved-session path only — never triggers an OTP); every /api/
// fetch returns '{}', so each D1 list renders its honest empty state.

let server: ViteDevServer;
let browser: Browser;
let url: string;

before(async () => {
  // The project vite config (react + tailwind plugins) is used on
  // purpose: the scroll assertion is geometric, so the real Tailwind
  // layout (lg:flex-row sidebar + sticky header) must apply.
  server = await createServer({ logLevel: 'error', cacheDir: 'releases/.vite-admin-scroll-test',
    esbuild: { jsx: 'automatic' }, define: { 'import.meta.env.VITE_USE_MOCK': '"false"' },
    optimizeDeps: { entries: ['scripts/fixtures/admin-scroll/index.html'] }, server: { host: '127.0.0.1', port: 0 } });
  await server.listen();
  url = `http://127.0.0.1:${(server.httpServer!.address() as any).port}/scripts/fixtures/admin-scroll/index.html`;
  const executablePath = process.env.CHROME_PATH || undefined;
  browser = await puppeteer.launch({ headless: true, args: ['--no-sandbox', '--disable-setuid-sandbox'], ...(executablePath ? { executablePath } : {}) });
});
after(async () => { await browser?.close(); await server?.close(); });

async function openDashboard(page: Page) {
  await page.evaluateOnNewDocument(() => {
    localStorage.setItem('drup_auth_token', 'fixture-admin-token');
    localStorage.setItem('drup_current_user', JSON.stringify({
      id: 'fixture-admin', mobile: '09933070870', fullName: 'مدیر آزمایشی',
      role: 'super_admin', capabilities: ['super_admin'],
    }));
  });
  await page.setViewport({ width: 1280, height: 500 });
  // domcontentloaded (not 'load'): first run pre-bundles deps, which can
  // take longer than the default 30 s navigation timeout.
  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 120000 });
  await page.waitForFunction(() => {
    const h = (window as any).adminScrollHarness;
    return h?.activeTab() === 'overview' && document.body.textContent?.includes('سامانه مدیریت یکپارچه');
  }, { timeout: 120000 });
}

const h = (page: Page, expr: string) => page.evaluate((e) => (window as any).adminScrollHarness[e](), expr);

test('switching admin tabs resets window scroll to the top', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await openDashboard(page);
    // The overview tab is long; scroll to its bottom first.
    await page.evaluate(() => (window as any).adminScrollHarness.scrollToBottom());
    await new Promise((r) => setTimeout(r, 200));
    const scrolled = await h(page, 'scrollY');
    assert.ok(scrolled > 0, `overview tab did not scroll (scrollY=${scrolled}); the fixture page is not tall enough to prove the reset`);
    // Switch to a short tab: the new tab must start at the top.
    await page.evaluate(() => (window as any).adminScrollHarness.clickTab('بنرها'));
    await page.waitForFunction(() => (window as any).adminScrollHarness.activeTab() === 'banners');
    assert.equal(await h(page, 'scrollY'), 0, 'scrollY is not 0 after switching to banners');
    assert.ok(await page.evaluate(() => (window as any).adminScrollHarness.tabHeadingVisible('بنرها')), 'banners heading is not visible without manual scrolling');
    // Reverse direction (short -> long) and via a different tab.
    await page.evaluate(() => (window as any).adminScrollHarness.clickTab('نمای کلی'));
    await page.waitForFunction(() => (window as any).adminScrollHarness.activeTab() === 'overview');
    assert.equal(await h(page, 'scrollY'), 0, 'scrollY is not 0 after switching back to overview');
  } finally { await context.close(); }
});

test('the eight D1 tabs render lists, not the stale super_admin stub copy', async () => {
  const context = await browser.createBrowserContext();
  try {
    const page = await context.newPage();
    await openDashboard(page);
    const tabs: Array<[string, string, string, boolean]> = [
      // [nav label, tab id, text that must appear, isEmptyState]
      ['لیست فروشگاه‌ها', 'stores-list', 'mone', false],
      ['صفحه اصلی', 'homepage', 'بخشی برای صفحه اصلی ثبت نشده است', true],
      ['بنرها', 'banners', 'بنری ثبت نشده است', true],
      ['محتوای سایت و صفحات', 'site-content', 'محتوایی ثبت نشده است', true],
      ['پرداخت‌های ناموفق', 'finance-callback-retries', 'پرداخت ناموفقی ثبت نشده است', true],
      ['مشترکان خبرنامه', 'newsletter', 'مشترکی ثبت نشده است', true],
      ['نظرات مشتریان', 'reviews', 'نظری ثبت نشده است', true],
      ['کارکنان انبار', 'staff', 'کارکنی ثبت نشده است', true],
    ];
    for (const [label, id, mustAppear] of tabs) {
      await page.evaluate((l) => (window as any).adminScrollHarness.clickTab(l), label);
      await page.waitForFunction((want) => (window as any).adminScrollHarness.activeTab() === want, {}, id);
      const text = await h(page, 'bodyText');
      assert.doesNotMatch(text, /پس از ورود به حساب/, `${label} still renders the stale copy`);
      assert.ok(text.includes(mustAppear), `${label} does not render its list (${mustAppear})`);
    }
  } finally { await context.close(); }
});
