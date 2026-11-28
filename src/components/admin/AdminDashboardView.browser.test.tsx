import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const menuButton = 'header button[aria-label="باز کردن منوی پنل"]';
const menu = 'dialog[open][aria-labelledby="admin-menu-title"]';
const title = (page: Page) => page.$eval('#admin-page-title', item => item.textContent);
const mobileOverflow = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth > innerWidth);

test('mobile admin shortcuts, grouped menu and browser history open the same three workflows', { timeout: 180000 }, () =>
  visit('/admin?admin', 320, async page => {
    await page.waitForSelector('#admin-main', { visible: true });
    assert.equal(await title(page), 'پیشخوان مدیریت');
    assert.equal(await page.$('.super-admin-theme'), null);
    assert.equal(await page.evaluate(() => document.activeElement?.id), '');
    assert.deepEqual(await page.$$eval('main nav[aria-label="مسیرهای اصلی مدیریت"] a', links => links.map(link => link.getAttribute('href'))),
      ['/admin?section=catalog', '/admin?section=suppliers', '/admin?section=orders']);
    await page.click('main nav a[href="/admin?section=catalog"]');
    await page.waitForSelector('aside a[data-section="catalog"][aria-current="page"]');
    assert.equal(await title(page), 'بررسی کالاها');
    assert.equal(await mobileOverflow(page), false);
    await page.focus(menuButton);
    await page.keyboard.press('Enter');
    await page.waitForSelector(menu, { visible: true });
    assert.ok(await page.$(`${menu} a[data-section="catalog"][aria-current="page"]`));
    await page.keyboard.press('Escape');
    await page.waitForSelector(menu, { hidden: true });
    assert.equal(await page.evaluate(query => document.activeElement?.matches(query), menuButton), true);
    await page.click(menuButton);
    await page.click(`${menu} a[data-section="suppliers"]`);
    await page.waitForSelector(menu, { hidden: true });
    assert.equal(await title(page), 'تأمین‌کنندگان');
    assert.equal(await mobileOverflow(page), false);
    await page.click(menuButton);
    await page.click(`${menu} a[data-section="orders"]`);
    await page.waitForSelector('aside a[data-section="orders"][aria-current="page"]');
    assert.equal(await title(page), 'رهگیری سفارش‌ها');
    assert.equal(await mobileOverflow(page), false);
    await page.goBack();
    await page.waitForSelector('aside a[data-section="suppliers"][aria-current="page"]');
    await page.goForward();
    await page.waitForSelector('aside a[data-section="orders"][aria-current="page"]');
    await page.setViewport({ width: 1440, height: 850 });
    assert.ok(await page.$eval('#admin-main', main => main.getBoundingClientRect().width > 850));
  }));

test('super admin uses the storefront palette on mobile, including its menu and notifications', () =>
  visit('/admin?admin=super', 390, async page => {
    await page.waitForSelector('#admin-main');
    assert.equal(await page.$eval('[data-panel-workspace="admin"]', panel => getComputedStyle(panel).backgroundColor), 'rgb(248, 246, 239)');
    assert.equal(await page.$eval(menuButton, button => getComputedStyle(button).backgroundColor), 'rgb(237, 242, 233)');
    await page.click(menuButton);
    await page.waitForSelector(menu, { visible: true });
    assert.equal(await page.$eval(`${menu} [aria-current="page"]`, link => getComputedStyle(link).color), 'rgb(14, 57, 44)');
    await page.keyboard.press('Escape');
    await page.click('header button[aria-label^="اعلان‌ها"]');
    const currentFilter = 'dialog[open][aria-labelledby="admin-notifications-title"] button[aria-pressed="true"]';
    await page.waitForSelector(currentFilter, { visible: true });
    assert.equal(await page.$eval(currentFilter, button => getComputedStyle(button).backgroundColor), 'rgb(237, 242, 233)');
    assert.equal(await mobileOverflow(page), false);
  }));

test('staff with a finance role see their permitted default instead of a restricted direct link', { timeout: 120000 }, () =>
  visit('/admin/orders?admin=finance', 390, async page => {
    await page.waitForSelector('#admin-main');
    assert.equal(await title(page), 'تراز و دفتر کل');
    await waitText(page, 'این بخش برای حساب شما در دسترس نیست');
    assert.equal(await page.$('aside a[data-section="orders"]'), null);
    assert.equal(await page.$('aside a[data-section="staff"]'), null);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.adminReads.includes('/api/v1/admin/orders')), false);
  }));

test('anonymous direct admin order links keep the sign-in guard', { timeout: 120000 }, () =>
  visit('/admin?section=orders', 390, async page => {
    await page.waitForSelector('input[type="tel"]', { visible: true });
    assert.equal(await page.$('#admin-main'), null);
  }));

test('the retired subscribers link opens the default dashboard without a menu item or request', { timeout: 120000 }, () =>
  visit('/admin?admin=super&section=newsletter', 390, async page => {
    await page.waitForSelector('#admin-main', { visible: true });
    assert.equal(await title(page), 'پیشخوان مدیریت');
    await page.click(menuButton);
    await page.waitForSelector(menu);
    assert.equal(await page.$('a[data-section="newsletter"]'), null);
    assert.equal(await page.evaluate(() => (window as any).batch8Fixture.adminReads.includes('/api/v1/admin/newsletter/subscribers')), false);
  }));

test('admin navigation resets scroll and opens the existing management lists', { timeout: 180000 }, () =>
  visit('/admin?admin=super', 1280, async page => {
    await page.setViewport({ width: 1280, height: 500 });
    await page.waitForSelector('#admin-main');

    async function openSection(section: string) {
      const selector = `aside a[data-section="${section}"]`;
      await page.$eval(selector, element => (element as HTMLAnchorElement).click());
      await page.waitForSelector(`${selector}[aria-current="page"]`);
    }

    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    assert.ok(await page.evaluate(() => window.scrollY > 0), 'overview must scroll before switching sections');
    await openSection('banners');
    assert.equal(await page.evaluate(() => window.scrollY), 0);
    assert.equal(await title(page), 'بنرها');
    await openSection('overview');
    assert.equal(await page.evaluate(() => window.scrollY), 0);

    const lists = [
      ['stores-list', 'فروشگاه مرکزی'],
      ['homepage', 'بخشی برای صفحه اصلی ثبت نشده است'],
      ['banners', 'بنری ثبت نشده است'],
      ['site-content', 'محتوایی ثبت نشده است'],
      ['finance-callback-retries', 'پرداخت ناموفقی ثبت نشده است'],
      ['reviews', 'نظری ثبت نشده است'],
      ['staff', 'کارکنی ثبت نشده است'],
    ];
    for (const [section, expected] of lists) {
      await openSection(section);
      await waitText(page, expected);
      assert.doesNotMatch(await page.$eval('main', main => main.innerText), /پس از ورود به حساب/);
    }
  }));
