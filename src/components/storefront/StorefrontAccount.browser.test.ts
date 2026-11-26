import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const storefront = '/store/batch-eight-store';
const account = 'header button[title="ورود به حساب کاربری"]';
const profile = 'پروفایل کاربری من';

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
