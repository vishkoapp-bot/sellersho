import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('returns are directly accessible from a central customer account even when empty', () =>
  visit('/account?access-fixture-role=customer&access-fixture-caps=customer', 390, async page => {
    await page.waitForSelector('#main-content h1');
    await waitText(page, 'پروفایل کاربری من');
    await clickText(page, 'مرجوعی‌های من', '#main-content nav button span.text-sm');
    await page.waitForSelector('#customer-returns-heading', { visible: true });
    await waitText(page, 'درخواستی برای مرجوعی ثبت نکرده‌اید.');
    assert.equal(new URL(page.url()).pathname, '/account/orders');
    assert.equal(new URL(page.url()).hash, '#returns');
    await clickText(page, 'مشاهدهٔ سفارش‌های تحویل‌شده');
    await page.waitForSelector('#customer-returns-heading', { hidden: true });
    assert.equal(new URL(page.url()).hash, '');
  }));
