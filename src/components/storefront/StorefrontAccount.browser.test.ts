import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const storefront = '/store/batch-eight-store';
const account = 'header button[title="ورود به حساب کاربری"]';
const profile = 'پروفایل کاربری من';
const pendingSession = 'holdInitialProfile&access-fixture-role=customer&access-fixture-caps=customer,seller';

test('the public shop has no workspace switcher before or after verifying a multi-role session', () =>
  visit('/?' + pendingSession, 390, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture.initialProfilePending());
    await page.waitForSelector('.central-hero', { visible: true });
    assert.equal(await page.$('.workspace-switcher, .route-loading--screen'), null);
    await page.evaluate(() => (window as any).batch8Fixture.finishInitialProfile());
    await page.waitForFunction(() => (window as any).batch8Fixture.state().isSessionReady);
    assert.equal(await page.$('.workspace-switcher, #account-panels-title'), null);
    await page.click('nav[aria-label="منوی پایین فروشگاه"] a[href="/account"]');
    await page.waitForSelector('#account-panels-title', { visible: true });
  }));

test('protected routes show a loader and retry a failed session check with the same credential', () =>
  visit('/account?rejectInitialProfile&' + pendingSession, 390, async page => {
    const loader = '.route-loading--screen[role="status"]';
    await page.waitForSelector(loader + ' .route-loading-icon', { visible: true });
    assert.equal(await page.$('#account-panels-title'), null);
    await page.waitForFunction(() => (window as any).batch8Fixture.initialProfilePending());
    await page.evaluate(() => (window as any).batch8Fixture.finishInitialProfile());
    await waitText(page, 'بررسی حساب ممکن نشد.');
    await clickText(page, 'تلاش مجدد');
    await waitText(page, profile);
    assert.ok(await page.evaluate(() => localStorage.getItem('seler_sho_jwt_token')));
  }));

test('an existing account can save its missing name and open only its allowed panels without OTP', () =>
  visit('/account?legacy-account-name&access-fixture-role=customer&access-fixture-caps=customer,seller,supplier,super_admin', 390, async page => {
    const details = 'section[aria-label="مشخصات حساب"]';
    const links = 'nav[aria-labelledby="account-panels-title"]';
    await page.waitForSelector('#account-full-name', { visible: true });
    assert.doesNotMatch(await page.$eval(details, element => element.textContent ?? ''), /کاربر جدید/);
    await fillInput(page, '#account-full-name', 'مریم احمدی');
    await clickText(page, 'ذخیرهٔ نام');
    await page.waitForFunction(selector => document.querySelector(selector)?.textContent === 'مریم احمدی', {}, details + ' h2');
    assert.deepEqual(await page.$$eval(links + ' a', nodes => nodes.map(node => node.getAttribute('href'))), ['/seller', '/supplier', '/admin']);
    assert.match(await page.$eval(links, element => element.textContent ?? ''), /پنل سوپر ادمین/);
    const token = await page.evaluate(() => localStorage.getItem('seler_sho_jwt_token'));
    assert.ok(token);
    await page.click(links + ' a[href="/seller"]');
    await page.waitForSelector('#seller-main', { visible: true });
    assert.equal(await page.evaluate(() => localStorage.getItem('seler_sho_jwt_token')), token);
  }));

test('a customer with no other capabilities sees their saved name and no workspace links', () =>
  visit('/account?access-fixture-role=customer&access-fixture-caps=customer', 390, async page => {
    await waitText(page, 'کاربر نمونه');
    assert.equal(await page.$('#account-panels-title'), null);
  }));

test('account sections open inside the profile and both back buttons restore the account menu', () =>
  visit('/account?access-fixture-role=customer&access-fixture-caps=customer', 390, async page => {
    const menu = 'nav[aria-label="امکانات حساب"]';
    const back = 'button[aria-label="بازگشت به منوی حساب"]';
    for (const [label, section] of [
      ['سفارش‌های من', 'orders'],
      ['مرجوعی‌های من', 'returns'],
      ['علاقه‌مندی‌ها', 'wishlist'],
      ['شرایط خرید و مرجوعی', 'terms'],
    ]) {
      await clickText(page, label, menu + ' button > span > span:first-child');
      await page.waitForSelector(back, { visible: true });
      assert.equal(new URL(page.url()).pathname, '/account');
      assert.equal(new URL(page.url()).searchParams.get('section'), section);
      assert.equal(await page.$eval('main h1', node => node.textContent?.trim()), profile);
      if (section === 'orders' || section === 'returns') {
        const tab = 'nav[aria-label="سفارش و مرجوعی"] button[aria-pressed="true"]';
        await page.waitForSelector(tab, { visible: true });
        assert.equal(await page.$eval(tab, node => node.textContent?.trim()), section === 'returns' ? 'مرجوعی‌های من' : 'سفارش‌ها');
      } else {
        await waitText(page, section === 'terms' ? label : 'هنوز کالایی ذخیره نکرده‌اید');
      }
      if (section === 'wishlist') await page.goBack();
      else await page.click(back);
      await page.waitForSelector(menu, { visible: true });
      assert.equal(new URL(page.url()).pathname, '/account');
      assert.equal(new URL(page.url()).searchParams.has('section'), false);
    }
  }));

test('opening the storefront account after a dashboard logout asks for OTP', () =>
  visit('/seller?seller', 1440, async page => {
    await page.waitForSelector('#seller-main');
    await page.click('[data-panel-workspace="seller"] header button[aria-label="خروج از حساب"]');
    await page.waitForSelector(account, { visible: true });
    assert.equal(new URL(page.url()).pathname, storefront);
    await page.click(account);
    await page.waitForSelector('input[type="tel"]', { visible: true });
    assert.doesNotMatch(await page.evaluate(() => document.body.innerText), new RegExp(profile));
    assert.equal(await page.evaluate(() => localStorage.getItem('seler_sho_jwt_token')), null);
    await page.click('button[aria-label="بستن"]');
    await page.waitForSelector(account, { visible: true });
    assert.equal(new URL(page.url()).pathname, storefront);
    await page.goBack();
    await page.waitForSelector('input[type="tel"]', { visible: true });
    assert.doesNotMatch(await page.evaluate(() => document.body.innerText), new RegExp(profile));
  }));

test('a verified customer reaches the storefront profile after account sign-in', () =>
  visit(storefront, 390, async page => {
    await page.waitForSelector(account, { visible: true });
    await page.click(account);
    await fillInput(page, 'input[type="tel"]', '09120000000');
    await clickText(page, 'دریافت کد تأیید');
    await page.waitForSelector('input[maxlength="4"]', { visible: true });
    await page.type('input[maxlength="4"]', '1234');
    await clickText(page, 'تأیید و ورود');
    await waitText(page, profile);
    assert.equal(new URL(page.url()).pathname, `${storefront}/account`);
    assert.equal(await page.$('input[type="tel"]'), null);
  }));

test('a seller session and referral do not replace the central store, while the seller workspace and storefront retain their own shop', () =>
  visit('/?seller&ref=batch-eight-store', 390, async page => {
    await page.waitForFunction(() => {
      const state = (window as any).batch8Fixture?.state?.();
      return state?.isSessionReady && state.userId && state.view === 'customer' && state.storeSlug === 'central';
    });
    assert.equal(new URL(page.url()).pathname, '/');

    async function open(view: string) {
      await page.evaluate(target => (window as any).batch8Fixture.openWorkspace(target), view);
    }
    async function waitForScope(view: string, storeSlug: string, pathname: string) {
      await page.waitForFunction((expectedView, slug, path) => {
        const state = (window as any).batch8Fixture.state();
        return state.view === expectedView && state.storeSlug === slug && location.pathname === path;
      }, {}, view, storeSlug, pathname);
    }

    await open('seller_dashboard');
    await waitForScope('seller_dashboard', 'batch-eight-store', '/seller');
    await open('seller_storefront');
    await waitForScope('seller_storefront', 'batch-eight-store', storefront);
    await page.waitForSelector('header button[title="حساب کاربری"]', { visible: true });
    await open('customer_wishlist');
    await waitForScope('customer_wishlist', 'batch-eight-store', storefront + '/account/wishlist');
    await open('static_page');
    await waitForScope('static_page', 'batch-eight-store', storefront + '/about');
    await open('customer');
    await waitForScope('customer', 'central', '/');
    await open('customer_wishlist');
    await waitForScope('customer_wishlist', 'central', '/account/wishlist');
    await page.goBack();
    await waitForScope('customer', 'central', '/');
    await open('seller_dashboard');
    await waitForScope('seller_dashboard', 'batch-eight-store', '/seller');
  }));
