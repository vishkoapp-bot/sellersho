import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';
import { warehouseSections, type WarehouseSection } from './WarehouseNavigation';

const visit = batch8Browser();
const menuButton = 'header button[aria-label="باز کردن منوی پنل"]';
const menu = 'dialog[open][aria-labelledby="warehouse-menu-title"]';

async function selectSection(page: Page, section: WarehouseSection, mobile: boolean) {
  if (mobile) {
    await page.click(menuButton);
    await page.waitForSelector(menu, { visible: true });
  }
  const link = await page.waitForSelector(`${mobile ? menu : 'aside'} a[data-section="${section}"]`);
  assert.ok(link);
  const disclosure = await link.evaluateHandle(anchor => {
    const group = anchor.closest('details');
    return group && !group.open ? group.querySelector('summary') : null;
  });
  const summary = disclosure.asElement();
  if (summary) await (await summary.toElement('summary')).click();
  await link.click();
  await page.waitForSelector(`aside a[aria-current="page"][data-section="${section}"]`);
  if (mobile) await page.waitForSelector(menu, { hidden: true });
}

test('warehouse navigation opens all grouped sections at mobile and desktop widths', { timeout: 180000 }, () =>
  visit('/warehouse?warehouse', 320, async page => {
    await page.waitForSelector('#warehouse-main', { visible: true });
    await page.waitForSelector('main section[aria-label="کارهای امروز"]');
    const summary = await page.waitForSelector('main details > summary');
    assert.ok(summary);
    await summary.click();
    await waitText(page, 'انبار شهر نمونه');
    assert.equal(await page.$eval('main [role="progressbar"]', meter => meter.getAttribute('aria-valuenow')), '25');

    await page.focus(menuButton);
    await page.keyboard.press('Enter');
    await page.waitForSelector(menu, { visible: true });
    assert.ok(await page.$(`${menu} a[data-section="overview"][aria-current="page"]`));
    await page.keyboard.press('Escape');
    await page.waitForSelector(menu, { hidden: true });
    assert.equal(await page.evaluate(selector => document.activeElement?.matches(selector), menuButton), true);

    assert.deepEqual(await page.$$eval('aside a[data-section]', links => links.map(link => (link as HTMLElement).dataset.section)), warehouseSections.map(section => section.id));
    for (const width of [320, 1440]) {
      await page.setViewport({ width, height: 820 });
      for (const { id, label } of warehouseSections) {
        await selectSection(page, id, width < 1024);
        assert.equal(await page.$eval('#warehouse-page-title', title => title.textContent), label);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${id} overflows at ${width}px`);
      }
    }
    assert.ok(await page.$eval('#warehouse-main', main => main.getBoundingClientRect().width > 850), 'desktop warehouse should not be limited to the mobile shell');
  }));

test('mobile receipt follows the exact line through quality control, put-away and stock verification', { timeout: 180000 }, () =>
  visit('/warehouse?warehouse&section=receive', 320, async page => {
    const scanInput = '#warehouse-carton-code';
    await page.waitForSelector(scanInput, { visible: true });
    await page.type(scanInput, 'CARTON-57');
    await page.keyboard.press('Enter');
    await page.waitForSelector('button[data-carton-confirm]', { visible: true });
    await page.click('button[data-carton-confirm]');
    await page.click('button[data-carton-submit]');
    await page.waitForSelector('button[data-carton-next]', { visible: true });
    await page.focus('button[data-carton-next]');
    await page.keyboard.press('Enter');

    const exactLine = 'section[data-qc-active][data-receipt-line-id="fixture-line-from-carton"]';
    await page.waitForSelector(exactLine, { visible: true });
    assert.ok((await page.$eval(exactLine, row => row.textContent))?.includes('کالای همان کارتن'));
    assert.equal(new URL(page.url()).searchParams.get('section'), 'qc');
    assert.equal(page.url().includes('fixture-line-from-carton'), false);
    const requests = await page.evaluate(() => (window as any).batch8Fixture.warehouseReceipts);
    assert.deepEqual(requests, [{ items: [{ variant_id: 'carton-sku', received_qty: 1, qc_status: 'pass' }] }]);

    await page.goBack();
    await page.waitForSelector(scanInput, { visible: true });
    await page.goForward();
    await page.waitForSelector(exactLine, { visible: true });
    assert.equal(await page.$eval('main nav [aria-current="step"]', step => step.textContent?.trim().endsWith('کنترل کیفیت')), true);

    const passedCount = `${exactLine} input[type="number"]`;
    await page.$eval(passedCount, input => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '1');
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await page.type(`${exactLine} textarea`, 'سالم');
    await page.click(`${exactLine} button[data-qc-submit]`);
    await clickText(page, 'رفتن به صف جانمایی', 'main button');
    await page.waitForSelector('main section select');
    assert.equal(new URL(page.url()).searchParams.get('section'), 'putaway');
    assert.equal(await page.$eval('main section h3', heading => heading.textContent?.trim()), 'کالای همان کارتن');
    await page.select('main section select', 'BIN-57');
    await clickText(page, 'رزرو ظرفیت قفسه', 'main section button');
    await clickText(page, 'تکمیل جانمایی', 'main section button');
    await clickText(page, 'بررسی موجودی قفسه', 'main button');
    await page.waitForSelector('main input[type="search"]');
    assert.equal(await page.$eval('main input[type="search"]', input => (input as HTMLInputElement).value), 'BIN-57');
    await waitText(page, 'کالای همان کارتن');
    assert.equal(new URL(page.url()).searchParams.get('section'), 'inventory');
  }));

test('direct warehouse link remains guarded for a visitor without a warehouse session', { timeout: 180000 }, () =>
  visit('/warehouse?section=shipping', 390, async page => {
    await waitText(page, 'ورود به سامانه مدیریت انبار');
    assert.equal(await page.$('#warehouse-main'), null);
  }));
