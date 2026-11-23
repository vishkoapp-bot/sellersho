import assert from 'node:assert/strict';
import { test } from 'node:test';
import { batch8Browser } from '../../../scripts/fixtures/catalog/batch8-browser';

const visit = batch8Browser();
const cardSelector = '.rounded-2xl.border.bg-white.p-4';
const dateSelector = 'input[type="datetime-local"]';

const visitDiscountForm = (run: Parameters<typeof visit>[2]) =>
  visit('/?panel=seller&tab=discount-codes&seller', 390, async page => {
    await page.waitForFunction(() =>
      [...document.querySelectorAll('h3')].some(heading => heading.textContent?.trim() === 'کد تخفیف'));
    await run(page);
  });

test('discount date inputs do not overlap and stay inside the card at 390px', async () => {
  await visitDiscountForm(async page => {
    const { card, inputs, scrollWidth } = await page.$$eval(dateSelector, (fields, selector) => ({
      card: fields[0]?.closest(selector)?.getBoundingClientRect().toJSON(),
      inputs: fields.map(field => field.getBoundingClientRect().toJSON()),
      scrollWidth: document.documentElement.scrollWidth,
    }), cardSelector);

    assert.equal(inputs.length, 2);
    assert.ok(card, 'discount form card exists');
    const [start, end] = inputs;
    assert.ok(start.right <= end.left || end.right <= start.left ||
      start.bottom <= end.top || end.bottom <= start.top, 'date inputs do not intersect');
    for (const input of inputs) {
      assert.ok(input.left >= card.left && input.right <= card.right &&
        input.top >= card.top && input.bottom <= card.bottom, 'date input stays inside the card');
    }
    assert.ok(scrollWidth <= 390, `document scroll width is ${scrollWidth}px`);
  });
});

test('discount type/value/max-uses fields are labelled and full-width at 390px', async () => {
  await visitDiscountForm(async page => {
    const labels = ['نوع تخفیف', 'مقدار', 'حداکثر دفعات استفاده'];
    const fields = await page.$$eval(`${cardSelector} label`, (elements, expectedLabels) =>
      elements.flatMap(label => {
        const text = label.textContent?.trim() ?? '';
        if (!expectedLabels.includes(text)) return [];
        const controls = [...(label.parentElement?.querySelectorAll('select, input') ?? [])];
        return [{ text, controls: controls.map(control => ({
          tag: control.tagName,
          width: control.getBoundingClientRect().width,
        })) }];
      }), labels);

    assert.deepEqual(fields.map(field => field.text), labels);
    assert.deepEqual(fields.map(field => field.controls.map(control => control.tag)), [
      ['SELECT'], ['INPUT'], ['INPUT'],
    ]);
    const select = await page.$eval(`${cardSelector} select`, element => ({
      selectedText: element.selectedOptions[0]?.textContent?.trim(),
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
    }));
    assert.equal(select.selectedText, 'درصد');
    assert.ok(select.scrollWidth <= select.clientWidth, 'selected option fits inside the select');
    for (const { text, controls: [control] } of fields) {
      assert.ok(control.width >= 250, `${text} is ${control.width}px wide`);
    }
  });
});

test('discount date inputs have start/end labels in order at 390px', async () => {
  await visitDiscountForm(async page => {
    const labels = ['تاریخ و ساعت شروع', 'تاریخ و ساعت پایان'];
    const positions = await page.$$eval(`${cardSelector} label`, (elements, texts, selector) =>
      texts.map(text => {
        const label = elements.find(element => element.textContent?.includes(text));
        if (!label) return null;
        return [...(label.parentElement?.querySelectorAll(selector) ?? [])]
          .map(input => input.getBoundingClientRect().top);
      }), labels, dateSelector);

    const [start, end] = positions;
    assert.ok(start, 'start label exists');
    assert.ok(end, 'end label exists');
    assert.equal(start.length, 1);
    assert.equal(end.length, 1);
    assert.ok(start[0] < end[0], 'start input is above the end input');
  });
});
