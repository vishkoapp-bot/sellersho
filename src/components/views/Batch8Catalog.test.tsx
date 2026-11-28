import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser({ inlineVazirmatn: true });
const rows = '[data-testid="product-item"]';
const searchInput = 'header [role="search"] input[name="q"]';
const digital = '#category a[data-testid="category-item"][href="/?category=digital"]';
const catalogStatus = '#main-content header [role="status"]';

async function chooseDigital(page: Parameters<typeof clickText>[0]) {
  await page.locator(digital).click();
}

for (const width of [390, 1280]) {
  test(`homepage and catalog display one search box at ${width}px`, async () => {
    await visit('/', width, async page => {
      const searchCount = () => page.$$eval(searchInput, nodes => nodes.filter(node => node.getClientRects().length).length);
      await page.waitForSelector('[data-testid="category-item"]');
      assert.equal(await searchCount(), 1);
      await chooseDigital(page);
      await page.waitForFunction(() => Boolean((window as any).batch8Fixture?.pending));
      assert.equal(await searchCount(), 1);
      await page.evaluate(() => (window as any).batch8Fixture.finish());
      await page.waitForFunction(selector => document.querySelectorAll(selector).length === 2, {}, rows);
      assert.deepEqual(await page.$$eval('header', headers => headers.slice(0, 2).map(header => getComputedStyle(header).position)), ['sticky', 'static']);
      await page.evaluate(() => window.scrollBy(0, 300));
      const box = await page.$eval('header', element => element.getBoundingClientRect().top);
      assert.ok(box >= 0 && box < 100);
    });
  });
}

test('opening a homepage tile clears stale products and count, then restores on terminal error', async () => {
  await visit('/', 390, async page => {
    await chooseDigital(page);
    await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    const pending = await page.evaluate(() => (window as any).batch8Fixture.state());
    assert.equal(pending.catalogTotal, 0);
    assert.equal(pending.productCount, 0);
    await page.waitForSelector(catalogStatus);
    assert.equal(await page.$eval(catalogStatus, node => node.textContent), 'در حال بارگذاری کاتالوگ…');
    assert.doesNotMatch(await page.$eval('#main-content', el => el.textContent ?? ''), /کالای عمومی نمونه/);
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, '۲ کالا');
    assert.equal(await page.$$eval(rows, elements => elements.length), 2);

    await fillInput(page, searchInput, 'missing');
    const search = await page.$(searchInput);
    assert.ok(search);
    await waitText(page, 'کالای دیجیتال نمونه 0');
    await search.press('Enter');
    await page.waitForFunction(() => new URLSearchParams(location.search).get('q') === 'missing' && (window as any).batch8Fixture?.pending);
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    await page.evaluate(() => (window as any).batch8Fixture.finish('error'));
    await waitText(page, 'بارگذاری نتایج تازه انجام نشد. نتایج قبلی نمایش داده می‌شود.');
    assert.equal(await page.$$eval(rows, elements => elements.length), 2);
    const restored = await page.evaluate(() => (window as any).batch8Fixture.state());
    assert.equal(restored.catalogTotal, 2);
    assert.equal(restored.productCount, 2);
  });
});

test('an aborted category request cannot restore the preceding homepage results', async () => {
  await visit('/', 390, async page => {
    await chooseDigital(page);
    await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
    await page.evaluate(() => {
      history.pushState(null, '', '/?q=missing');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    await waitText(page, 'نتایج جستجوی «missing»');
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.state().catalogTotal), 0);
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, 'کالایی یافت نشد');
  });
});

test('zero-result search leaves desktop filters available and lets shoppers reset the search', async () => {
  await visit('/?q=missing', 1280, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
    const filters = 'aside[aria-label="فیلترهای کاتالوگ"]';
    await page.waitForSelector(filters, { visible: true });
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, 'کالایی یافت نشد');
    await page.waitForSelector(`${filters} input[type="checkbox"]`, { visible: true });
    assert.ok(await page.$eval('section[aria-label="نتایج کاتالوگ"]', element => element.getBoundingClientRect().width > 240));
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    assert.equal(await page.$eval(catalogStatus, node => node.textContent), '۰ کالا');
    await clickText(page, 'حذف فیلترها');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length > 2, {}, rows);
    assert.equal(new URL(page.url()).searchParams.get('q'), null);
  });
});

test('price-range error keeps the catalog controls visible even with no products', async () => {
  await visit('/?q=range-error', 1280, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
    await page.waitForSelector('aside input[aria-label="حداقل قیمت (تومان)"]', { visible: true });
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, 'حداقل قیمت نمی‌تواند بیشتر از حداکثر قیمت باشد');
    await page.waitForSelector('aside input[aria-label="حداقل قیمت (تومان)"]', { visible: true });
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
  });
});

for (const [route, heading] of [
  ['/dashboard', 'ورود به پنل همکاری دراپ‌شیپر'],
  ['/seller', 'ورود به پنل همکاری دراپ‌شیپر'],
  ['/supplier', 'ورود به پنل شرکای تأمین کالا'],
]) {
  test(`${route} shows the logged-out panel guard`, async () => {
    await visit(route, 390, async page => {
      await waitText(page, heading);
      assert.equal(new URL(page.url()).pathname, route);
      assert.ok(await page.$('input[type="tel"]'));
    });
  });
}

const infoTabs = 'main nav[aria-label="راهنمای خرید"]';

test('the RTL terms strip scrolls its selected tab into view', async () => {
  await visit('/terms', 360, async page => {
    const license = `${infoTabs} a[href="/license"]`;
    await page.waitForSelector(license);
    const initial = await page.$eval(infoTabs, row => ({
      overflows: row.scrollWidth > row.clientWidth,
      direction: getComputedStyle(row).direction,
    }));
    assert.deepEqual(initial, { overflows: true, direction: 'rtl' });
    assert.equal(await page.$eval(`${infoTabs} [aria-current="page"]`, node => node.getAttribute('href')), '/terms');
    // Let the application scroll the link. A Puppeteer click would scroll it first.
    await page.$eval(license, (node: HTMLAnchorElement) => node.click());
    await page.waitForFunction(selector => {
      const tab = document.querySelector(selector);
      if (tab?.getAttribute('aria-current') !== 'page') return false;
      const rect = tab.getBoundingClientRect();
      const row = tab.parentElement!.getBoundingClientRect();
      return rect.left >= row.left && rect.right <= row.right;
    }, {}, license);
    assert.equal(new URL(page.url()).pathname, '/license');
  });
});

for (const route of ['/terms', '/license']) {
  test(`all five tabs fit on ${route} at 500px`, async () => {
    await visit(route, 500, async page => {
      await page.waitForSelector(`${infoTabs} a[href="/license"]`);
      assert.equal(new URL(page.url()).pathname, route);
      const row = await page.$eval(infoTabs, element => {
        const bounds = element.getBoundingClientRect();
        return {
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          targetHeights: [...element.querySelectorAll('a')].map(link => link.getBoundingClientRect().height),
          labels: [...element.querySelectorAll('a span')].map(label => {
            const rect = label.getBoundingClientRect();
            return {
              text: label.textContent,
              visible: rect.width > 0 && rect.left >= bounds.left && rect.right <= bounds.right,
            };
          }),
        };
      });
      assert.ok(row.scrollWidth <= row.clientWidth, `tab strip is ${row.scrollWidth}px in ${row.clientWidth}px`);
      assert.ok(row.targetHeights.every(height => height >= 44));
      assert.deepEqual(row.labels, ['درباره ما', 'قوانین و ضمانت', 'حریم خصوصی', 'پرسش‌های متداول', 'مجوزها']
        .map(text => ({ text, visible: true })));
    });
  });
}

for (const [route, width] of [
  ['/', 390],
  ['/?q=missing', 390],
  ['/terms', 390],
  ['/store/batch-eight-store', 390],
  ['/store/batch-eight-store', 800],
] as const) {
  test(`bottom navigation clears the shell and footer on ${route} at ${width}px`, async () => {
    await visit(route, width, async page => {
      if (route.includes('missing')) {
        await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
        await page.evaluate(() => (window as any).batch8Fixture.finish());
        await waitText(page, 'کالایی یافت نشد');
      }
      const nav = route.startsWith('/store/') ? 'nav[aria-label="منوی پایین ویترین"]' : 'nav[aria-label="منوی پایین فروشگاه"]';
      await page.waitForSelector(nav, { visible: true });
      await page.waitForSelector('footer', { visible: true });
      const bounds = await page.evaluate(async navSelector => {
        const footer = document.querySelector('footer')!;
        const nav = document.querySelector(navSelector)!;
        const reset = [...document.querySelectorAll('main button')].find(node => node.textContent?.trim() === 'مشاهده همه محصولات');
        window.scrollTo(0, document.documentElement.scrollHeight);
        await new Promise(resolve => requestAnimationFrame(resolve));
        const navBounds = nav.getBoundingClientRect();
        const footerControls = [...footer.querySelectorAll('a, button, input')].filter(node => node.getClientRects().length > 0);
        return {
          footerControlCount: footerControls.length,
          footerLastControlBottom: Math.max(...footerControls.map(node => node.getBoundingClientRect().bottom)),
          navTop: navBounds.top,
          navBottomOffset: innerHeight - navBounds.bottom,
          navHeight: navBounds.height,
          emptyButtonBottom: reset?.getBoundingClientRect().bottom,
        };
      }, nav);
      assert.ok(bounds.navHeight >= 64);
      assert.ok(Math.abs(bounds.navBottomOffset) < 1);
      assert.ok(bounds.footerControlCount > 0);
      assert.ok(bounds.footerLastControlBottom <= bounds.navTop);
      if (route.includes('missing')) {
        assert.ok(bounds.emptyButtonBottom != null && bounds.emptyButtonBottom < bounds.navTop);
      }
    });
  });
}
