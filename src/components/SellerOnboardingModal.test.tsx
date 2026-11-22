import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
for (const failure of [false, true]) {
  test(`seller wizard resumes after login and ${failure ? 'localizes a 401' : 'registers once'}`, async () => {
    await visit('/?admin', 390, async page => {
      await clickText(page, 'راه‌اندازی دراپ‌شیپینگ');
      await clickText(page, 'شروع');
      await waitText(page, 'برای افتتاح فروشگاه ابتدا وارد حساب کاربری شوید');
      assert.equal(await page.$('input[placeholder="my-store"]'), null);
      assert.equal(await page.evaluate(() => (window as any).batch8Fixture.registrations), 0);
      await fillInput(page, 'input[type="tel"][placeholder="۰۹۱۲۳۴۵۶۷۸۹"]', '09120000000');
      await clickText(page, 'دریافت کد تأیید');
      await page.waitForSelector('input[maxlength="4"]');
      await fillInput(page, 'input[maxlength="4"]', '1234');
      await clickText(page, 'تأیید و ورود');
      try {
        await page.waitForSelector('input[placeholder="my-store"]', { timeout: 5000 });
      } catch {
        const result = await page.evaluate(() => ({
          view: (window as any).batch8Fixture.state().view,
          verified: (window as any).batch8Fixture.verifications,
          notices: document.querySelector('.fixed.top-4')?.textContent,
          headings: [...document.querySelectorAll('h1, h3, [role="status"]')].map(node => node.textContent?.trim()).slice(-8),
          inputs: [...document.querySelectorAll('input')].map(node => node.placeholder),
        }));
        assert.fail(JSON.stringify(result));
      }
      assert.equal(new URL(page.url()).pathname, '/');
      assert.equal(await page.evaluate(() => (window as any).batch8Fixture.state().view), 'dropshipper_landing');
      await fillInput(page, 'input[placeholder^="مثلاً"]', 'فروشگاه نمونه');
      await fillInput(page, 'input[placeholder="my-store"]', 'batch-eight-store');
      await fillInput(page, 'input[placeholder="نام و نام خانوادگی"]', 'مدیر نمونه');
      await clickText(page, 'مرحله بعدی');
      await clickText(page, 'مرحله بعدی');
      await waitText(page, 'SIG-DRP-');
      const signature = () => page.$eval('body', node => node.innerText.match(/SIG-DRP-\d{4}/)?.[0]);
      const code = await signature();
      assert.ok(code);
      await page.click('input[type="checkbox"]');
      assert.equal(await signature(), code);
      await page.evaluate(value => { (window as any).batch8Fixture.rejectRegistration = value; }, failure);
      await clickText(page, 'امضای دیجیتال و افتتاح فروشگاه');
      await waitText(page, failure ? 'برای افتتاح فروشگاه ابتدا وارد شوید' : 'با موفقیت افتتاح گردید');
      const content = await page.$eval('body', element => element.innerText);
      assert.doesNotMatch(content, /Unauthorized|\{"error"/);
      if (!failure) {
        assert.equal((content.match(/با موفقیت افتتاح گردید/g) ?? []).length, 1);
        const ids = await page.evaluate(() => (window as any).batch8Fixture.state().stores as string[]);
        assert.equal(ids.filter(id => id === 'fixture-store').length, 1);
        assert.equal(ids.some(id => /^store-\d+$/.test(id)), false);
      }
      assert.equal(await page.evaluate(() => (window as any).batch8Fixture.registrations), 1);
    });
  });
}
