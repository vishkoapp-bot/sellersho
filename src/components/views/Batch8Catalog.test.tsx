import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const rows = '[data-testid="product-item"]';
const searchInput = 'header input[type="text"]';
const digital = '[data-testid="category-item"]';

async function chooseDigital(page: Parameters<typeof clickText>[0]) {
  await page.waitForFunction(selector => {
    const tile = [...document.querySelectorAll(selector)].find(node => node.textContent?.includes('کالای دیجیتال'));
    return tile?.textContent?.includes('۲ کالا');
  }, {}, digital);
  await page.$$eval(digital, tiles =>
    (tiles.find(node => node.textContent?.includes('کالای دیجیتال')) as HTMLElement).click());
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
    assert.doesNotMatch(await page.$eval('body', el => el.innerText), /\(۴۱\)|\(۰\)|کالای عمومی نمونه/);
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, '(۲)');
    assert.equal(await page.$$eval(rows, elements => elements.length), 2);

    const mobileSearch = 'header .lg\\:hidden input[type="text"]';
    await fillInput(page, mobileSearch, 'missing');
    const search = await page.$(mobileSearch);
    assert.ok(search);
    await waitText(page, 'کالای دیجیتال نمونه 0');
    await search.press('Enter');
    await page.waitForFunction(() => new URLSearchParams(location.search).get('q') === 'missing' && (window as any).batch8Fixture?.pending);
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    await page.evaluate(() => (window as any).batch8Fixture.finish('error'));
    await waitText(page, 'Fixture catalog unavailable');
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
    await waitText(page, 'جستجو: «missing»');
    assert.equal(await page.$$eval(rows, elements => elements.length), 0);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.state().catalogTotal), 0);
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, 'کالایی یافت نشد');
  });
});

test('zero-result search hides sidebar controls and preserves the desktop content column', async () => {
  await visit('/?q=missing', 1280, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture?.pending);
    await page.waitForSelector('aside input[aria-label="حداقل قیمت (تومان)"]', { visible: true });
    assert.equal(await page.$eval('aside', element => Boolean(element.firstElementChild?.getClientRects().length)), true);
    await page.evaluate(() => (window as any).batch8Fixture.finish());
    await waitText(page, 'کالایی یافت نشد');
    assert.equal(await page.$eval('aside', element => Boolean(element.firstElementChild?.getClientRects().length)), false);
    assert.ok(await page.$eval('aside + div', element => element.getBoundingClientRect().width > 240));
    assert.equal(await page.$eval('aside input[aria-label="حداقل قیمت (تومان)"]', element => element.getClientRects().length), 0);
    assert.ok(await page.$eval('aside', aside => [...aside.querySelectorAll('button')]
      .filter(button => ['همه', 'کالای دیجیتال'].includes(button.textContent?.trim() ?? ''))
      .every(button => button.getClientRects().length === 0)));
    await waitText(page, '(۰)');
    await clickText(page, 'حذف فیلترها');
    await page.waitForFunction(selector => document.querySelectorAll(selector).length > 2, {}, rows);
    assert.equal(await page.$eval('aside', element => Boolean(element.firstElementChild?.getClientRects().length)), true);
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

test('the terms strip overflows with an RTL affordance and scrolls after a tab change', async () => {
  await visit('/terms', 360, async page => {
    await waitText(page, 'مجوزها');
    const tabs = 'div[class*="mask-image"]';
    const initial = await page.$eval(tabs, row => ({
      overflows: row.scrollWidth > row.clientWidth,
      left: row.scrollLeft,
      mask: getComputedStyle(row).maskImage,
    }));
    assert.equal(initial.overflows, true);
    assert.equal(initial.left, 0);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.staticTabScrolls), 0);
    assert.match(initial.mask, /gradient/);
    await clickText(page, 'مجوزها');
    await page.waitForFunction(selector => {
      const row = document.querySelector(selector);
      const button = row?.lastElementChild;
      if (!row || !button) return false;
      return button.getBoundingClientRect().left >= row.getBoundingClientRect().left;
    }, {}, tabs);
    assert.ok(await page.evaluate(() => (window as any).batch8Fixture.staticTabScrolls > 0));
  });
});

for (const route of ['/terms', '/license']) {
  test(`all five tabs fit on ${route} at 500px`, async () => {
    await visit(route, 500, async page => {
      await waitText(page, 'مجوزها');
      assert.equal(new URL(page.url()).pathname, route);
      const row = await page.$eval('div[class*="mask-image"]', element => {
        const bounds = element.getBoundingClientRect();
        return {
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          labels: [...element.querySelectorAll('button span')].map(label => {
            const rect = label.getBoundingClientRect();
            return {
              text: label.textContent,
              visible: rect.width > 0 && rect.left >= bounds.left && rect.right <= bounds.right,
            };
          }),
        };
      });
      assert.ok(row.scrollWidth <= row.clientWidth, `tab strip is ${row.scrollWidth}px in ${row.clientWidth}px`);
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
      await page.waitForSelector('nav.fixed', { visible: true });
      await page.waitForSelector('footer', { visible: true });
      const bounds = await page.evaluate(async () => {
        const footer = document.querySelector('footer')!;
        const nav = [...document.querySelectorAll('nav.fixed')].find(node => node.getClientRects().length)!;
        const shell = document.querySelector('#root .pb-24')!;
        const clearButtons = [...document.querySelectorAll('button')].filter(node => node.textContent?.trim() === 'حذف فیلترها');
        window.scrollTo(0, document.documentElement.scrollHeight);
        await new Promise(resolve => requestAnimationFrame(resolve));
        const navTop = nav.getBoundingClientRect().top;
        const emptyButton = clearButtons[1]?.getBoundingClientRect();
        return {
          footerBottom: footer.getBoundingClientRect().bottom,
          navTop,
          navHeight: nav.getBoundingClientRect().height,
          shellPadding: parseFloat(getComputedStyle(shell).paddingBottom),
          emptyButtonBottom: emptyButton?.bottom,
          resetCount: clearButtons.length,
        };
      });
      assert.equal(bounds.navHeight, 64);
      assert.ok(bounds.shellPadding >= bounds.navHeight + 16);
      assert.ok(bounds.footerBottom <= bounds.navTop);
      if (route.includes('missing')) {
        assert.equal(bounds.resetCount, 2);
        assert.ok(bounds.emptyButtonBottom != null && bounds.emptyButtonBottom < bounds.navTop);
      }
    });
  });
}
