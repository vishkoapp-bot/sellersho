import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Page } from 'puppeteer';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const token = (page: Page) => page.evaluate(() => localStorage.getItem('seler_sho_jwt_token'));

async function finishWarehouseOTP(page: Page) {
  await page.waitForSelector('#warehouse-mobile', { visible: true });
  assert.equal(await page.$eval('#warehouse-mobile', input => (input as HTMLInputElement).value), '');
  await page.type('#warehouse-mobile', '۰۹۱۲۰۰۰۰۰۰۰');
  await clickText(page, 'دریافت کد تأیید');
  await page.waitForSelector('#warehouse-otp', { visible: true });
  await page.type('#warehouse-otp', '۱۲۳۴');
  await clickText(page, 'تأیید و ورود به پنل انبار');
}

test('warehouse entry never promotes a base role with no active warehouse capability', () =>
  visit('/warehouse?stale-warehouse-role&section=shipping', 390, async page => {
    await waitText(page, 'حساب فعلی مجوز پنل انبار را ندارد');
    assert.equal(await page.$('select'), null);
    assert.equal(await page.$('#warehouse-main'), null);
    await finishWarehouseOTP(page);
    await waitText(page, 'این شماره مجوز ورود به پنل انبار را ندارد');
    assert.equal(await page.$('#warehouse-main'), null);
    assert.equal(await token(page), 'fixture-existing-token');
  }));

test('warehouse OTP opens the requested section when a staff capability is active', () =>
  visit('/warehouse?warehouse-otp&section=receive', 390, async page => {
    await finishWarehouseOTP(page);
    await page.waitForSelector('#warehouse-carton-code', { visible: true });
    assert.equal(await token(page), 'fixture-token');
    assert.equal(new URL(page.url()).searchParams.get('section'), 'receive');
  }));

test('the public warehouse entry opens the guarded warehouse route', () =>
  visit('/', 390, async page => {
    await clickText(page, 'ورود عملیات انبار');
    await page.waitForSelector('#warehouse-mobile', { visible: true });
    assert.equal(new URL(page.url()).pathname, '/warehouse');
    assert.equal(await page.$('#warehouse-main'), null);
  }));
