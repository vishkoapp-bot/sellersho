import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();

test('typing Persian digits into the admin OTP field keeps them as Latin digits at 390px', async () => {
  await visit('/admin', 390, async page => {
    await waitText(page, 'قدم ۱ از ۲');
    await fillInput(page, 'input[type="tel"]', '09120000000');
    await page.click('button[type="submit"]');
    await waitText(page, 'قدم ۲ از ۲');

    const otpInput = 'input[type="text"][maxlength="4"]';
    await page.waitForSelector(otpInput, { visible: true });
    await page.type(otpInput, '۱۲۳۴');
    assert.equal(await page.$eval(otpInput, input => (input as HTMLInputElement).value), '1234');
  });
});
