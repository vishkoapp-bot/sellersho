import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, clickText } from '../../../scripts/fixtures/catalog/batch8-browser';
import { SELLER_DASHBOARD_TABS } from '../../utils/routing';

const visit = batch8Browser();
const menuButton = 'button[aria-label="باز کردن منوی پنل"]';
const mobileMenu = 'dialog[open][aria-labelledby="seller-menu-title"]';

async function selectSection(page: Page, id: string, onMobile: boolean) {
  if (onMobile) {
    await page.click(menuButton);
    await page.waitForSelector(mobileMenu, { visible: true });
  }
  const link = await page.waitForSelector(`${onMobile ? mobileMenu : 'aside'} a[data-section="${id}"]`);
  assert.ok(link);
  const group = await link.evaluateHandle(link => {
    const details = link.closest('details');
    return details && !details.open ? details.querySelector('summary') : null;
  });
  const summary = group.asElement();
  if (summary) await (await summary.toElement('summary')).click();
  await link.click();
  await page.waitForSelector(`aside a[aria-current="page"][data-section="${id}"]`);
  if (onMobile) await page.waitForSelector(mobileMenu, { hidden: true });
}

test('mobile seller menu supports keyboard navigation, direct links and store switching', { timeout: 180000 }, () =>
  visit('/?panel=seller&tab=discount-codes&seller&multi-store', 320, async page => {
    await page.waitForSelector('#seller-page-title');
    assert.equal(await page.$eval('#seller-page-title', heading => heading.textContent), 'کدهای تخفیف');
    await page.focus(menuButton);
    await page.keyboard.press('Enter');
    await page.waitForSelector(mobileMenu, { visible: true });
    assert.ok(await page.$(`${mobileMenu} a[aria-current="page"][data-section="discount-codes"]`));
    await page.keyboard.press('Escape');
    await page.waitForSelector(mobileMenu, { hidden: true });
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), 'باز کردن منوی پنل');
    await page.click(menuButton);
    const picker = await page.waitForSelector(`${mobileMenu} select`, { visible: true });
    assert.ok(picker);
    await picker.select('fixture-second-store');
    await page.waitForFunction(() => document.querySelector('header p')?.textContent === 'فروشگاه دوم');
    await page.focus(`${mobileMenu} a[data-section="support"]`);
    await page.keyboard.press('Enter');
    await page.waitForFunction(() => document.querySelector('#seller-page-title')?.textContent === 'پشتیبانی' && document.activeElement?.id === 'seller-main');
    await page.goBack();
    await page.waitForSelector('aside a[aria-current="page"][data-section="discount-codes"]');
    await page.goForward();
    await page.waitForSelector('aside a[aria-current="page"][data-section="support"]');
    await selectSection(page, 'overview', true);
    await page.waitForSelector('section[aria-label="کارهای امروز"]');
    const bell = 'header button[aria-label^="اعلان‌ها"]';
    await page.focus(bell);
    await page.keyboard.press('Enter');
    await page.waitForSelector('dialog[open][aria-labelledby="seller-notifications-title"]', { visible: true });
    await page.keyboard.press('Escape');
    await page.waitForSelector('dialog[open][aria-labelledby="seller-notifications-title"]', { hidden: true });
    assert.equal(await page.evaluate(selector => document.activeElement?.matches(selector), bell), true);
    await page.click('main a[href*="work=follow-up"]');
    await page.waitForSelector('aside a[aria-current="page"][data-section="orders"]');
    assert.equal(new URL(page.url()).searchParams.get('work'), 'follow-up');
    await page.waitForFunction(() => document.querySelector('main [aria-label="وضعیت سفارش"] button[aria-pressed="true"]')?.textContent?.includes('نیازمند پیگیری'));
    await page.goBack();
    await page.waitForSelector('aside a[aria-current="page"][data-section="overview"]');
    await selectSection(page, 'subscription', true);
    await clickText(page, 'مدیریت و ساخت فروشگاه', 'main button');
    await page.waitForFunction(() => {
      const summary = document.activeElement;
      return summary?.tagName === 'SUMMARY' && summary.textContent?.includes('فروشگاه‌های من') &&
        summary.parentElement?.hasAttribute('open');
    });
    const create = await page.waitForFunction(() => [...document.querySelectorAll<HTMLButtonElement>('main details[open] button')]
      .find(button => button.innerText.trim() === 'ساخت فروشگاه جدید'));
    const createButton = create.asElement();
    assert.ok(createButton);
    await (await createButton.toElement('button')).focus();
    await page.keyboard.press('Enter');
    const createDialog = 'dialog[open][aria-labelledby="seller-create-store-title"]';
    await page.waitForSelector(createDialog, { visible: true });
    await page.click(`${createDialog} input[type="checkbox"]`);
    await page.click(`${createDialog} button[type="submit"]`);
    await page.waitForFunction(() => document.activeElement?.getAttribute('name') === 'name');
    await page.keyboard.press('Escape');
    await page.waitForSelector(createDialog, { hidden: true });
    assert.equal(await page.evaluate(() => document.activeElement?.textContent?.trim()), 'ساخت فروشگاه جدید');
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  }));

test('all seller sections open from the same grouped navigation on mobile and desktop', { timeout: 180000 }, () =>
  visit('/seller?seller', 320, async page => {
    await page.waitForSelector('#seller-page-title');
    const sections = await page.$$eval('aside a[data-section]', links => links.map(link => (link as HTMLElement).dataset.section!));
    assert.deepEqual([...sections].sort(), [...SELLER_DASHBOARD_TABS].sort());
    for (const width of [320, 1440]) {
      await page.setViewport({ width, height: 820 });
      for (const section of sections) {
        await selectSection(page, section, width < 1024);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${section} overflows at ${width}px`);
      }
    }
  }));

test('renewed seller contract is accepted in the shared keyboard-accessible dialog', { timeout: 180000 }, () =>
  visit('/seller?seller&renew-contract', 390, async page => {
    const modal = 'dialog[open][aria-labelledby="seller-renew-contract-title"]';
    await page.waitForSelector(modal, { visible: true });
    await page.keyboard.press('Escape');
    assert.ok(await page.$(modal));
    await page.focus(`${modal} input[type="checkbox"]`);
    await page.keyboard.press('Space');
    await page.focus(`${modal} button[type="submit"]`);
    await page.keyboard.press('Enter');
    await page.waitForSelector(modal, { hidden: true });
    await page.waitForSelector('#seller-main', { visible: true });
  }));
