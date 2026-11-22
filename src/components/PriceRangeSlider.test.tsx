import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { PriceRangeSlider, PRICE_RANGE_ERROR_MESSAGE } from './PriceRangeSlider';

const noop = () => {};

it('exports the single price-range error message verbatim', () => {
  assert.equal(
    PRICE_RANGE_ERROR_MESSAGE,
    'حداقل قیمت نمی‌تواند بیشتر از حداکثر قیمت باشد',
  );
});

it('renders no error element and formats committed values in Persian digits', () => {
  const html = renderToStaticMarkup(
    <PriceRangeSlider boundMin={198000} boundMax={63800000} value={[1000000, 5000000]} onChange={noop} />,
  );
  assert.doesNotMatch(html, /price-range-error/);
  assert.match(html, /value="۱٬۰۰۰٬۰۰۰"/);
  assert.match(html, /value="۵٬۰۰۰٬۰۰۰"/);
  assert.doesNotMatch(html, /aria-describedby="price-range-error"/);
});

it('renders the message verbatim when the server reports an inverted range', () => {
  const html = renderToStaticMarkup(
    <PriceRangeSlider
      boundMin={198000}
      boundMax={63800000}
      value={[1000000, 5000000]}
      onChange={noop}
      serverRangeError
    />,
  );
  assert.match(html, /data-testid="price-range-error"/);
  assert.match(html, /role="alert"/);
  assert.match(html, /id="price-range-error"/);
  assert.match(html, /حداقل قیمت نمی‌تواند بیشتر از حداکثر قیمت باشد/);
  // The server error is about the pair, so both inputs describe the message.
  assert.equal((html.match(/aria-describedby="price-range-error"/g) ?? []).length, 2);
  assert.doesNotMatch(html, /aria-invalid="true"/);
});

it('renders no message when serverRangeError is false', () => {
  const html = renderToStaticMarkup(
    <PriceRangeSlider
      boundMin={198000}
      boundMax={63800000}
      value={[198000, 63800000]}
      onChange={noop}
      serverRangeError={false}
    />,
  );
  assert.doesNotMatch(html, /price-range-error/);
});
