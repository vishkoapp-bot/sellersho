import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser, clickText, fillInput, waitText } from '../../../scripts/fixtures/catalog/batch8-browser';
import { OFFICIAL_NICHES } from '../../data/niches';

const visit = batch8Browser();
type Page = Parameters<Parameters<typeof visit>[2]>[0];
type Scenario = 'picker' | 'undo' | 'paging';
type FixtureWindow = typeof window & {
  batch8Fixture: {
    settingsPuts: Array<{ slug: string; config: { selectedNicheIds: string[] } }>;
    productsPuts: Array<{ slug: string; productIds: string[] }>;
    candidateCalls: Array<{ storeSlug: string; q?: string; cursor?: string; ids?: string[] }>;
    settingsInFlight: number;
    settingsMaxInFlight: number;
    failNextSettingsPut: boolean;
    failNextProductsPut: boolean;
    staleCandidateIds: string[];
    staleCandidateIdsOnProductsFailure: string[];
    settingsDelayMs: number;
    simulateExternalAdd: (id: string) => void;
  };
};

const slug = 'batch-eight-store';
const smartwatch = OFFICIAL_NICHES.find(({ id }) => id === 'niche-smartwatch')!.title;
const audio = OFFICIAL_NICHES.find(({ id }) => id === 'niche-audio')!.title;
const powerbank = OFFICIAL_NICHES.find(({ id }) => id === 'niche-powerbank')!.title;
const nicheTitles = OFFICIAL_NICHES.map(({ title }) => title);
const headphone = 'هدفون بی‌سیم تست';
const speaker = 'اسپیکر بلوتوثی تست';
const watch = 'ساعت مچی تست';
const showcaseProduct = 'کالای دیجیتال نمونه 0';
const showcaseIds = Array.from({ length: 24 }, (_, index) => `batch8-product-${index}`);
const inactiveBadge = 'غیرفعال — تا فعال نشود در فروشگاه نمایش داده نمی‌شود';
const pickerEmpty = 'در همه کالاهای پلتفرم کالایی با این جستجو پیدا نشد';
const confirm = 'تأیید و ذخیره';
const undoBanner = '۱ کالا از فروشگاه حذف شد';
const pickerInput = 'input[aria-label="جستجوی همه کالاهای پلتفرم"]';
const extraInput = 'input[placeholder="جستجو در بقیه کالاها…"]';
const nicheInput = 'input[placeholder="جستجوی کالا در نیچ‌هایی که انتخاب کردی…"]';
const addButtons = 'button[aria-label^="افزودن "][aria-label$=" به فروشگاه"]';

const waitForCatalog = async (page: Page) => {
  await waitText(page, smartwatch);
  await waitText(page, 'کالاهای نیچ‌های تو');
};
const withCatalog = (scenario: Scenario, run: (page: Page) => Promise<void>) =>
  visit(`/?panel=seller&tab=catalog&seller&drp3132=${scenario}`, 900, async page => {
    await waitForCatalog(page);
    await run(page);
  });

const clickNiche = (page: Page, title: string) => clickText(page, title);
const chipSelected = (page: Page, title: string) => page.evaluate(value =>
  [...document.querySelectorAll('button')].some(button =>
    button.textContent?.trim() === value && button.className.includes('bg-blue-600')),
  title);
const waitForSettings = (page: Page, count: number) => page.waitForFunction((title, expected) => {
  const fixture = (window as FixtureWindow).batch8Fixture;
  const chip = [...document.querySelectorAll('button')].find(button => button.textContent?.trim() === title);
  return fixture.settingsPuts.length === expected && fixture.settingsInFlight === 0 && chip && !chip.disabled;
}, {}, audio, count);

async function openExtras(page: Page) {
  await page.waitForFunction(() => [...document.querySelectorAll('button')]
    .some(button => button.innerText.includes('بقیه کالاها')));
  await page.evaluate(() => [...document.querySelectorAll('button')]
    .find(button => button.innerText.includes('بقیه کالاها'))?.click());
  await page.waitForSelector(extraInput, { visible: true });
}

const searchPicker = (page: Page, query: string) => fillInput(page, pickerInput, query);
const pickerButton = (title: string) => `button[aria-label="افزودن ${title} به فروشگاه"]`;
const pickerRowText = async (page: Page, title: string) => {
  const selector = pickerButton(title);
  await page.waitForSelector(selector, { visible: true });
  return page.$eval(selector, button => button.parentElement?.innerText ?? '');
};
const addPickerRow = async (page: Page, title: string) => {
  await page.waitForSelector(pickerButton(title), { visible: true });
  await page.$eval(pickerButton(title), button => (button as HTMLButtonElement).click());
};
const confirmDraft = async (page: Page) => {
  await page.waitForFunction(value => [...document.querySelectorAll('button')]
    .some(button => button.innerText.trim() === value && !button.disabled), {}, confirm);
  await clickText(page, confirm);
};
const waitForNoConfirm = (page: Page) => page.waitForFunction(value =>
  ![...document.querySelectorAll('button')].some(button =>
    [value, 'در حال ذخیره…', 'انصراف'].includes(button.innerText.trim())), {}, confirm);

const calls = (page: Page) => page.evaluate(() => {
  const { settingsPuts, productsPuts, candidateCalls, settingsMaxInFlight } = (window as FixtureWindow).batch8Fixture;
  return { settingsPuts, productsPuts, candidateCalls, settingsMaxInFlight };
});

const waitShowcaseRow = (page: Page, title: string, present = true) =>
  page.waitForFunction((value, expected) =>
    [...document.querySelectorAll('h4')].some(heading => heading.textContent === value) === expected,
  {}, title, present);
const waitShowcaseSwitch = (page: Page, title: string, checked: boolean) =>
  page.waitForFunction((value, state) => {
    const heading = [...document.querySelectorAll('h4')].find(node => node.textContent === value);
    return heading?.parentElement?.parentElement?.parentElement
      ?.querySelector('button[role="switch"]')?.getAttribute('aria-checked') === String(state);
  }, {}, title, checked);
const toggleShowcaseOff = async (page: Page, title: string) => {
  await waitShowcaseSwitch(page, title, true);
  await page.evaluate(value => {
    const heading = [...document.querySelectorAll('h4')].find(node => node.textContent === value);
    heading?.parentElement?.parentElement?.parentElement?.querySelector<HTMLButtonElement>('button[role="switch"]')?.click();
  }, title);
  await waitShowcaseSwitch(page, title, false);
};

const searchExtras = (page: Page, query: string) => fillInput(page, extraInput, query);
// The extra showcase list displays the first ten matches. Search for the
// appended fixture products so assertions observe their actual showcase rows.
async function findHeadphoneInExtras(page: Page) {
  await openExtras(page);
  await searchExtras(page, headphone);
  await waitShowcaseRow(page, headphone);
}
async function removeHeadphone(page: Page) {
  await findHeadphoneInExtras(page);
  await toggleShowcaseOff(page, headphone);
  await confirmDraft(page);
  await waitText(page, undoBanner);
  await waitShowcaseRow(page, headphone, false);
}
async function reloadFixture(page: Page) {
  await (page as Page & { reloadFixture: () => Promise<void> }).reloadFixture();
  await waitForCatalog(page);
}

test('DRP-31 saves the audio chip immediately without a products PUT', () =>
  withCatalog('picker', async page => {
    await clickNiche(page, audio);
    await waitForSettings(page, 1);
    const { settingsPuts, productsPuts } = await calls(page);
    assert.deepEqual(settingsPuts, [{ slug, config: { selectedNicheIds: ['niche-smartwatch', 'niche-audio'] } }]);
    assert.deepEqual(productsPuts, []);
    assert.equal(await chipSelected(page, audio), true);
    await waitForNoConfirm(page);
  }));

test('DRP-31 niche and product edits persist in separate PUTs', () =>
  withCatalog('picker', async page => {
    await clickNiche(page, audio);
    await toggleShowcaseOff(page, showcaseProduct);
    await confirmDraft(page);
    await waitText(page, undoBanner);
    const { settingsPuts, productsPuts } = await calls(page);
    assert.equal(settingsPuts.length, 1);
    assert.deepEqual(productsPuts, [{ slug, productIds: showcaseIds.slice(1) }]);
  }));

test('DRP-31 rolls back the chip on a settings PUT failure', () =>
  withCatalog('picker', async page => {
    await page.evaluate(() => { (window as FixtureWindow).batch8Fixture.failNextSettingsPut = true; });
    await clickNiche(page, audio);
    await waitText(page, 'ذخیره نیچ انجام نشد؛ دوباره تلاش کن');
    await waitForSettings(page, 1);
    assert.equal(await chipSelected(page, audio), false);
    const { settingsPuts, productsPuts } = await calls(page);
    assert.equal(settingsPuts.length, 1);
    assert.deepEqual(productsPuts, []);
  }));

test('DRP-31 disables all chips during a PUT and lets the next tap save after it settles', () =>
  withCatalog('picker', async page => {
    await page.evaluate(() => { (window as FixtureWindow).batch8Fixture.settingsDelayMs = 200; });
    const disabled = await page.evaluate(async (first, second, titles) => {
      const chips = [...document.querySelectorAll('button')].filter(button => titles.includes(button.innerText.trim()));
      chips.find(button => button.innerText.trim() === first)?.click();
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
      const allDisabled = chips.length === titles.length && chips.every(button => button.disabled);
      chips.find(button => button.innerText.trim() === second)?.click();
      return allDisabled;
    }, audio, powerbank, nicheTitles);
    assert.equal(disabled, true);
    await waitForSettings(page, 1);
    assert.equal((await calls(page)).settingsMaxInFlight, 1);
    assert.equal(await chipSelected(page, powerbank), false);
    await clickNiche(page, powerbank);
    await waitForSettings(page, 2);
    assert.equal((await calls(page)).settingsMaxInFlight, 1);
    assert.equal(await chipSelected(page, powerbank), true);
  }));

test('DRP-31 reload restores both saved niches', () =>
  withCatalog('picker', async page => {
    await clickNiche(page, audio);
    await waitForSettings(page, 1);
    await reloadFixture(page);
    assert.equal(await chipSelected(page, smartwatch), true);
    assert.equal(await chipSelected(page, audio), true);
    assert.deepEqual((await calls(page)).settingsPuts, []);
  }));

test('DRP-31 last selected niche cannot be deselected', () =>
  withCatalog('picker', async page => {
    assert.equal(await chipSelected(page, smartwatch), true);
    await clickNiche(page, smartwatch);
    await waitForSettings(page, 0);
    assert.equal(await chipSelected(page, smartwatch), true);
    assert.deepEqual((await calls(page)).settingsPuts, []);
  }));

test('DRP-32 absent product is absent from both showcase searches', () =>
  withCatalog('picker', async page => {
    await waitShowcaseRow(page, headphone, false);
    await fillInput(page, nicheInput, 'هدفون');
    await waitText(page, 'در نیچ‌هایی که انتخاب کردی، کالایی با این جستجو نیست.');
    await openExtras(page);
    await waitShowcaseRow(page, headphone, false);
    await searchExtras(page, 'هدفون');
    await page.waitForFunction(() => [...document.querySelectorAll('p')]
      .filter(node => node.textContent === 'کالایی پیدا نشد').length === 2);
    await waitShowcaseRow(page, headphone, false);
  }));

test('DRP-32 inactive candidate is labelled and can rejoin the showcase', () =>
  withCatalog('picker', async page => {
    await openExtras(page);
    await searchPicker(page, 'هدفون');
    assert.ok((await pickerRowText(page, headphone)).includes(inactiveBadge));
    await addPickerRow(page, headphone);
    await page.waitForSelector(pickerButton(headphone), { hidden: true });
    assert.deepEqual((await calls(page)).productsPuts, []);
    await confirmDraft(page);
    await searchExtras(page, headphone);
    await waitShowcaseRow(page, headphone);
    const { productsPuts, candidateCalls } = await calls(page);
    assert.deepEqual(productsPuts, [{ slug, productIds: [...showcaseIds, 'drp32-headphone'] }]);
    assert.deepEqual(candidateCalls.filter(call => call.ids), [{ storeSlug: slug, ids: ['drp32-headphone'] }]);
  }));

test('DRP-32 active candidate can rejoin the showcase without an inactive badge', () =>
  withCatalog('picker', async page => {
    await openExtras(page);
    await searchPicker(page, 'اسپیکر');
    assert.ok(!(await pickerRowText(page, speaker)).includes(inactiveBadge));
    await addPickerRow(page, speaker);
    await confirmDraft(page);
    await searchExtras(page, speaker);
    await waitShowcaseRow(page, speaker);
  }));

test('DRP-32 picker excludes zero-cost products', () =>
  withCatalog('picker', async page => {
    await openExtras(page);
    await searchPicker(page, 'بدون قیمت');
    await waitText(page, pickerEmpty);
    assert.equal((await calls(page)).candidateCalls.at(-1)?.q, 'بدون قیمت');
  }));

test('DRP-32 picker excludes products already in the showcase', () =>
  withCatalog('picker', async page => {
    await waitShowcaseRow(page, showcaseProduct);
    await openExtras(page);
    await searchPicker(page, showcaseProduct);
    await waitText(page, pickerEmpty);
    assert.equal((await calls(page)).candidateCalls.at(-1)?.q, showcaseProduct);
  }));

test('DRP-32 picker appends the next candidate page', () =>
  withCatalog('paging', async page => {
    await openExtras(page);
    await waitText(page, 'نمایش بیشتر');
    assert.equal(await page.$$eval(addButtons, buttons => buttons.length), 2);
    await clickText(page, 'نمایش بیشتر');
    await page.waitForSelector(pickerButton(watch), { visible: true });
    assert.equal(await page.$$eval(addButtons, buttons => buttons.length), 3);
    assert.deepEqual((await calls(page)).candidateCalls.map(({ q, cursor }) => [q, cursor ?? null]),
      [['', null], ['', 'drp32-cursor-1']]);
  }));

test('DRP-32 undo restores a removed product and clears the offer', () =>
  withCatalog('undo', async page => {
    await removeHeadphone(page);
    await clickText(page, 'بازگردانی');
    await waitShowcaseRow(page, headphone);
    await waitText(page, 'کالاهای حذف‌شده بازگردانده شدند');
    assert.ok(!(await page.evaluate(() => document.body.innerText)).includes(undoBanner));
    assert.deepEqual((await calls(page)).productsPuts, [
      { slug, productIds: showcaseIds },
      { slug, productIds: [...showcaseIds, 'drp32-headphone'] },
    ]);
  }));

test('DRP-32 undo offer disappears on reload', () =>
  withCatalog('undo', async page => {
    await removeHeadphone(page);
    await reloadFixture(page);
    assert.ok(!(await page.evaluate(() => document.body.innerText)).includes(undoBanner));
  }));

test('DRP-32 failed undo leaves the removal and offers a retry', () =>
  withCatalog('undo', async page => {
    await removeHeadphone(page);
    await page.evaluate(() => { (window as FixtureWindow).batch8Fixture.failNextProductsPut = true; });
    await clickText(page, 'بازگردانی');
    await waitText(page, 'بازگردانی انجام نشد؛ دوباره تلاش کن');
    await waitShowcaseRow(page, headphone, false);
    await waitForNoConfirm(page);
    await page.waitForFunction(() => [...document.querySelectorAll('button')]
      .some(button => button.innerText.trim() === 'بازگردانی' && !button.disabled));
    assert.equal((await calls(page)).productsPuts.length, 2);
  }));

test('DRP-32 undo preserves a concurrent addition', () =>
  withCatalog('undo', async page => {
    await removeHeadphone(page);
    await page.evaluate(() => (window as FixtureWindow).batch8Fixture.simulateExternalAdd('drp32-speaker'));
    await clickText(page, 'بازگردانی');
    await waitShowcaseRow(page, headphone);
    await searchExtras(page, speaker);
    await waitShowcaseRow(page, speaker);
    assert.deepEqual((await calls(page)).productsPuts.at(-1),
      { slug, productIds: [...showcaseIds, 'drp32-speaker', 'drp32-headphone'] });
  }));

test('DRP-32 drops a stale candidate and confirms the eligible addition in the same tap', () =>
  withCatalog('picker', async page => {
    await openExtras(page);
    await searchPicker(page, 'هدفون');
    await addPickerRow(page, headphone);
    await page.evaluate(() => { (window as FixtureWindow).batch8Fixture.staleCandidateIds = ['drp32-headphone']; });
    await searchPicker(page, 'اسپیکر');
    await addPickerRow(page, speaker);
    await confirmDraft(page);
    await waitText(page, `«${headphone}» دیگر قابل افزودن نیست و از تغییرات حذف شد`);
    await searchExtras(page, speaker);
    await waitShowcaseRow(page, speaker);
    await waitForNoConfirm(page);
    const { productsPuts, candidateCalls } = await calls(page);
    assert.deepEqual(productsPuts, [{ slug, productIds: [...showcaseIds, 'drp32-speaker'] }]);
    assert.deepEqual(candidateCalls.filter(call => call.ids), [{ storeSlug: slug, ids: ['drp32-headphone', 'drp32-speaker'] }]);
  }));

test('DRP-32 failed PUT drops a newly stale addition and preserves the removal draft for retry', () =>
  withCatalog('picker', async page => {
    await openExtras(page);
    await searchPicker(page, 'اسپیکر');
    await addPickerRow(page, speaker);
    await toggleShowcaseOff(page, showcaseProduct);
    await page.evaluate(() => {
      const fixture = (window as FixtureWindow).batch8Fixture;
      fixture.failNextProductsPut = true;
      fixture.staleCandidateIdsOnProductsFailure = ['drp32-speaker'];
    });
    await confirmDraft(page);
    await waitText(page, `«${speaker}» دیگر قابل افزودن نیست و از تغییرات حذف شد`);
    await waitShowcaseSwitch(page, showcaseProduct, false);
    const { productsPuts, candidateCalls } = await calls(page);
    assert.deepEqual(productsPuts, [{ slug, productIds: [...showcaseIds.slice(1), 'drp32-speaker'] }]);
    assert.deepEqual(candidateCalls.filter(call => call.ids).map(call => call.ids), [['drp32-speaker'], ['drp32-speaker']]);
    await confirmDraft(page);
    await waitForNoConfirm(page);
    assert.deepEqual((await calls(page)).productsPuts, [
      { slug, productIds: [...showcaseIds.slice(1), 'drp32-speaker'] },
      { slug, productIds: showcaseIds.slice(1) },
    ]);
  }));

test('DRP-32 cancel resets an unsaved removal without a PUT', () =>
  withCatalog('undo', async page => {
    await findHeadphoneInExtras(page);
    await toggleShowcaseOff(page, headphone);
    await clickText(page, 'انصراف');
    await waitShowcaseSwitch(page, headphone, true);
    await waitForNoConfirm(page);
    assert.deepEqual((await calls(page)).productsPuts, []);
  }));
