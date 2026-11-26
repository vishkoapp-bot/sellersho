import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const selector = 'input[aria-label="درصد سود تنوع تنوع نمونه"]';
const rowTitle = 'کالای تنوع‌دار DRP-33';
const plainTitle = 'کالای ساده DRP-33';

type Page = Parameters<Parameters<typeof visit>[2]>[0];

const rowText = (page: Page, title: string) => page.evaluate(value =>
  [...document.querySelectorAll('h4')].find(heading => heading.textContent === value)
    ?.parentElement?.parentElement?.parentElement?.textContent ?? '', title);

const waitRow = (page: Page, pct: string) => page.waitForFunction((title, value) =>
  [...document.querySelectorAll('h4')].find(heading => heading.textContent === title)
    ?.parentElement?.textContent?.includes(value), {}, rowTitle, pct);

const withCatalog = (scenario: string, run: (page: Page) => Promise<void>) =>
  visit('/?panel=seller&tab=catalog&seller&drp33=' + scenario, 900, async page => {
    await page.waitForFunction(() => (window as any).batch8Fixture.effectiveMarginReads > 0);
    await waitText(page, rowTitle);
    await run(page);
  });

async function openModal(page: Page) {
  await page.evaluate(title => {
    const row = [...document.querySelectorAll('h4')].find(heading => heading.textContent === title)
      ?.parentElement?.parentElement?.parentElement;
    row?.querySelector<HTMLButtonElement>('button[title="تنظیم سود این کالا"]')?.click();
  }, rowTitle);
  await page.waitForSelector(selector, { visible: true });
}

test('Escape closes the price dialog and returns focus to the product action', () =>
  withCatalog('unset', async page => {
    await page.evaluate(title => {
      const row = [...document.querySelectorAll('h4')].find(heading => heading.textContent === title)
        ?.parentElement?.parentElement?.parentElement;
      row?.querySelector<HTMLButtonElement>('button[title="تنظیم سود این کالا"]')?.focus();
    }, rowTitle);
    await page.keyboard.press('Enter');
    await page.waitForSelector(selector, { visible: true });
    await page.focus(selector);
    await page.keyboard.press('Escape');
    await page.waitForSelector(selector, { hidden: true });
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('title')), 'تنظیم سود این کالا');
  }));

test('unset variant uses 35% before discounts while the plain product keeps its 15% override', () =>
  withCatalog('unset', async page => {
    await waitRow(page, '۳۵٪');
    const row = await rowText(page, rowTitle);
    assert.match(row, /پیش از تخفیف/);
    assert.match(row, /۱۳۵٬۰۰۰/);
    assert.match(row, /۳۵٬۰۰۰/);
    assert.match(await rowText(page, plainTitle), /۱۵٪/);
    await openModal(page);
    assert.equal(await page.$eval(selector, field => (field as HTMLInputElement).placeholder), '۳۵');
    const modal = await page.$eval(selector, field => ({
      variant: field.parentElement?.parentElement?.textContent,
      numericInputs: field.closest('form')?.querySelectorAll('input[type="number"]').length,
    }));
    assert.match(modal.variant ?? '', /۱۳۵٬۰۰۰/);
    assert.equal(modal.numericInputs, 1);
  }));

test('explicit variant override displays 15% and saving or clearing it refreshes the row', () =>
  withCatalog('override', async page => {
    await waitRow(page, '۱۵٪');
    await openModal(page);
    await page.waitForFunction(query => document.querySelector<HTMLInputElement>(query)?.value === '15', {}, selector);
    await fillInput(page, selector, '20');
    await page.keyboard.press('Enter');
    await waitText(page, 'تنظیمات تنوع‌ها ذخیره شد.');
    await clickText(page, 'بستن');
    await waitRow(page, '۲۰٪');

    await openModal(page);
    await page.waitForFunction(query => document.querySelector<HTMLInputElement>(query)?.value === '20', {}, selector);
    await fillInput(page, selector, '');
    await clickText(page, 'ذخیره تنظیمات تنوع‌ها');
    await page.waitForFunction(() => (window as any).batch8Fixture.variantOverrideClears === 1);
    await page.waitForFunction(() => [...document.querySelectorAll('button')]
      .some(button => button.textContent?.trim() === 'ذخیره تنظیمات تنوع‌ها' && !button.disabled));
    await clickText(page, 'بستن');
    await waitRow(page, '۳۵٪');
  }));

test('product ceiling drives variant row, blank modal placeholder, preview and copy', () =>
  withCatalog('ceiling', async page => {
    await waitRow(page, '۲۵٪');
    assert.match(await rowText(page, rowTitle), /۱۲۵٬۰۰۰/);
    await openModal(page);
    const field = await page.$eval(selector, input => ({
      placeholder: (input as HTMLInputElement).placeholder,
      value: (input as HTMLInputElement).value,
      preview: input.parentElement?.parentElement?.textContent,
      copy: input.parentElement?.parentElement?.parentElement?.textContent,
    }));
    assert.equal(field.placeholder, '۲۵');
    assert.equal(field.value, '');
    assert.match(field.preview ?? '', /۱۲۵٬۰۰۰/);
    assert.match(field.copy ?? '', /\(۲۵٪\)/);
  }));

test('variant margin above the global max saves within the product ceiling', () =>
  withCatalog('ceiling-high', async page => {
    await waitRow(page, '۴۰٪');
    await openModal(page);
    await fillInput(page, selector, '38');
    await clickText(page, 'ذخیره تنظیمات تنوع‌ها');
    await waitText(page, 'تنظیمات تنوع‌ها ذخیره شد.');
    await clickText(page, 'بستن');
    await waitRow(page, '۳۸٪');
  }));

test('admin-locked variant displays the endpoint margin even with a conflicting saved override', () =>
  withCatalog('locked', async page => {
    await waitRow(page, '۱۸٪');
    assert.match(await rowText(page, rowTitle), /۱۱۸٬۰۰۰/);
    await openModal(page);
    assert.match(await page.$eval(selector, input => input.parentElement?.parentElement?.textContent ?? ''), /۱۱۸٬۰۰۰/);
  }));

for (const scenario of ['failure', 'missing']) {
  test(`variant margin ${scenario} uses the global max with an unverified label`, () =>
    withCatalog(scenario, async page => {
      await waitRow(page, '۳۵٪');
      const row = await rowText(page, rowTitle);
      assert.match(row, /تأیید نشده/);
      assert.doesNotMatch(row, /۱۵٪/);
    }));
}

test('a variable product with all variants hidden never shows its parent margin or editor', () =>
  withCatalog('hidden', async page => {
    assert.doesNotMatch(await rowText(page, rowTitle), /۱۵٪/);
    await openModal(page);
    const form = await page.$eval(selector, field => {
      const modal = field.closest('form');
      return { text: modal?.textContent, numericInputs: modal?.querySelectorAll('input[type="number"]').length };
    });
    assert.doesNotMatch(form.text ?? '', /۱۵٪/);
    assert.equal(form.numericInputs, 1);
  }));

test('seller variant workspace previews native Excel edits and updates the price on mobile', () =>
  visit('/?panel=seller&tab=catalog&seller&drp33=workspace', 390, async page => {
    await clickText(page, 'تنوع‌های فروشگاه نمونه');
    await waitText(page, 'پنهان در فروشگاه');
    await clickText(page, 'ورود از Excel یا CSV');
    const workbook = await readFile(new URL('../../../scripts/fixtures/catalog/variant-bulk-upload.xlsx', import.meta.url));
    await page.$eval('input[type="file"]', (input, bytes) => {
      const data = new DataTransfer();
      data.items.add(new File([new Uint8Array(bytes)], 'changes.xlsx'));
      (input as HTMLInputElement).files = data.files;
      input.dispatchEvent(new Event('change', { bubbles: true }));
    }, Array.from(workbook));
    await waitText(page, 'تنوع در این پنل پیدا نشد.');
    await clickText(page, 'ثبت ۱ سطر');
    await waitText(page, 'درصد سود ثبت شد.');
    await waitText(page, '۱۱۵٬۰۰۰ تومان');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  }));
