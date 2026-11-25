import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sellerMoneyBrowserFixture } from './sellerMoneyBrowserFixture';

const withPage = sellerMoneyBrowserFixture('wallet');
const withMockPage = sellerMoneyBrowserFixture('wallet', true);
const amountInput = 'form input[type="number"]';
const shabaInput = 'form input[placeholder="IRxxxxxxxxxxxxxxxxxxxxxxxx"]';
const unavailable = 'برداشت زودهنگام فعلاً ممکن نیست.';

test('wallet retains the authoritative withdrawable figure and labels the release rule', () => withPage('', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('تفکیک موجودی کیف پول'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /پولی که همین حالا می‌توانی برداشت کنی۵۲٬۸۰۰/);
  assert.match(text ?? '', /سود آزادشده پس از پایان مهلت ۷ روزهٔ مرجوعی/);
  assert.match(text ?? '', /سود سفارش‌های در راه۱۲٬۰۰۰ تومان/);
  assert.match(text ?? '', /سود آزادشده قابل برداشت۶۳٬۰۰۰ تومان/);
}));

test('wallet explicitly labels the local pending value if the breakdown read fails', () => withPage('&breakdownFailure', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('۹۰۰'));
  const text = await page.evaluate(() => document.body.textContent);
  assert.match(text ?? '', /سود سفارش‌های در راه \(برآورد محلی\):۹۰۰/);
  assert.doesNotMatch(text ?? '', /تفکیک موجودی کیف پول/);
}));

test('wallet hides the withdrawal form at 52,800 without a POST or native validation', () => withPage('', async page => {
  await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
  assert.match(await page.$eval('body', el => el.textContent ?? ''), /موجودی قابل برداشت شما ۵۲٬۸۰۰ تومان است\. حداقل مبلغ برداشت زودهنگام ۱٬۰۰۰٬۰۰۰ تومان است؛ برداشت زودهنگام فعلاً ممکن نیست\./);
  await page.evaluate(() => document.querySelector<HTMLButtonElement>('form button[type="submit"]')?.click());
  await page.keyboard.press('Enter');
  assert.equal(await page.$('form'), null);
  const events = await page.evaluate(() => ({
    requests: (window as any).sellerMoney.withdrawalRequests(),
    invalid: (window as any).sellerMoney.invalidEvents(),
  }));
  assert.deepEqual(events, { requests: [], invalid: 0 });
}));

for (const [balance, digits] of [[0, '۰'], [999999, '۹۹۹٬۹۹۹']] as const) {
  test(`wallet hides the withdrawal form at ${balance}`, () => withPage(`&balance=${balance}`, async page => {
    await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
    assert.ok((await page.$eval('body', el => el.textContent))?.includes(`موجودی قابل برداشت شما ${digits} تومان است.`));
    assert.equal(await page.$(amountInput), null);
  }));
}

for (const [balance, digits] of [[1000000, '۱٬۰۰۰٬۰۰۰'], [1250000, '۱٬۲۵۰٬۰۰۰']] as const) {
  test(`wallet can withdraw all at ${balance} with the original native bounds`, () => withPage(`&balance=${balance}`, async page => {
    await page.waitForSelector(amountInput);
    const bounds = await page.$eval(amountInput, el => {
      const input = el as HTMLInputElement;
      return { min: input.min, max: input.max };
    });
    assert.deepEqual(bounds, { min: '1000000', max: String(balance) });
    await page.evaluate(() => {
      const button = Array.from(document.querySelectorAll('form button')).find(el => el.textContent?.includes('برداشت همه موجودی'));
      (button as HTMLButtonElement).click();
    });
    assert.equal(await page.$eval(amountInput, el => (el as HTMLInputElement).value), String(balance));
    const text = await page.$eval('body', el => el.textContent ?? '');
    assert.ok(text.includes(`برداشت همه موجودی (${digits} تومان)`));
    assert.ok(text.includes('حداقل مبلغ برداشت زودهنگام: ۱٬۰۰۰٬۰۰۰ تومان'));
    await page.waitForFunction(() => document.body.textContent?.includes('کارمزد برداشت زودهنگام (۵٪)'));

    const shaba = 'IR060170000000000000000000';
    await page.type(shabaInput, shaba);
    await page.click('form button[type="submit"]');
    await page.waitForFunction(() => (window as any).sellerMoney.withdrawalRequests().length > 0);
    assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.withdrawalRequests()), [{ amount: balance, shaba }]);
  }));
}

test('wallet shows loading until the balance arrives, without flashing unavailable', () => withPage('&deferBalance', async page => {
  await page.waitForFunction(() => (window as any).sellerMoney.balancePending());
  const text = await page.$eval('body', el => el.textContent ?? '');
  assert.ok(text.includes('در حال دریافت موجودی…'));
  assert.ok(!text.includes(unavailable));
  assert.equal(await page.$(amountInput), null);
  await page.evaluate(() => (window as any).sellerMoney.resolveBalance());
  await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
}));

test('wallet shows its balance load error without the below-floor message', () => withPage('&balanceFailure', async page => {
  await page.waitForFunction(() => document.body.textContent?.includes('Fixture balance failure'));
  const text = await page.$eval('body', el => el.textContent ?? '');
  assert.ok(!text.includes(unavailable));
  assert.ok(!text.includes('در حال دریافت موجودی…'));
  assert.equal(await page.$(amountInput), null);
}));

test('wallet hides the form below the configured 2,000,000 floor', () => withPage('&floor=2000000&balance=1500000', async page => {
  await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
  assert.match(await page.$eval('body', el => el.textContent ?? ''), /حداقل مبلغ برداشت زودهنگام ۲٬۰۰۰٬۰۰۰ تومان است/);
  assert.equal(await page.$('form'), null);
  await page.keyboard.press('Enter');
  assert.deepEqual(await page.evaluate(() => (window as any).sellerMoney.withdrawalRequests()), []);
}));

test('wallet shows the form above the configured 50,000 floor', () => withPage('&floor=50000&balance=52800', async page => {
  await page.waitForSelector(amountInput);
  assert.equal(await page.$eval(amountInput, el => (el as HTMLInputElement).min), '50000');
}));

test('wallet waits for the breakdown before showing withdrawal availability', () => withPage('&deferBreakdown&balance=52800', async page => {
  await page.waitForFunction(() =>
    (window as any).sellerMoney.breakdownPending() && document.body.textContent?.includes('پولی که همین حالا می‌توانی برداشت کنی۵۲٬۸۰۰'));
  const text = await page.$eval('body', el => el.textContent ?? '');
  assert.ok(text.includes('در حال دریافت موجودی…'));
  assert.ok(!text.includes(unavailable));
  assert.equal(await page.$('form'), null);
  await page.evaluate(() => (window as any).sellerMoney.resolveBreakdown());
  await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
}));

test('wallet shows a settings error when the breakdown fails', () => withPage('&breakdownFailure&balance=52800', async page => {
  await page.waitForFunction(() => document.querySelector('[role="status"]')?.textContent?.includes('دریافت تنظیمات برداشت انجام نشد.'));
  assert.ok(!(await page.$eval('body', el => el.textContent ?? '')).includes(unavailable));
  assert.equal(await page.$('form'), null);
}));

test('wallet mock mode uses the inclusive 500,000 floor', async () => {
  await withMockPage('&balance=499999', async page => {
    await page.waitForFunction(copy => document.body.textContent?.includes(copy), {}, unavailable);
    assert.match(await page.$eval('body', el => el.textContent ?? ''), /۴۹۹٬۹۹۹ تومان است\. حداقل مبلغ برداشت زودهنگام ۵۰۰٬۰۰۰ تومان/);
    assert.equal(await page.$(amountInput), null);
  });
  await withMockPage('&balance=500000', async page => {
    await page.waitForSelector(amountInput);
    assert.deepEqual(await page.$eval(amountInput, el => {
      const input = el as HTMLInputElement;
      return [input.min, input.max];
    }), ['500000', '500000']);
    assert.match(await page.$eval('body', el => el.textContent ?? ''), /کمترین برداشت: ۵۰۰٬۰۰۰ تومان/);
  });
});
