import assert from 'node:assert/strict';
import { test } from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { Product } from '../../../types';
import { ProductDetailTabs } from './ProductDetailInfo';

test('specs tab renders only available description, structured rows, and logistics', () => {
  const render = (details: Partial<Product>) => {
    const html = renderToStaticMarkup(<ProductDetailTabs product={{ specs: {}, ...details } as Product} />);
    assert.doesNotMatch(html, /مشخصاتی برای این کالا ثبت نشده است/);
    return html;
  };
  assert.match(render({ description: 'باتری تا ۴۰ ساعت' }), /باتری تا ۴۰ ساعت/);
  assert.match(render({ specs: { باتری: '۴۰ ساعت' } }), /باتری<.*۴۰ ساعت/);
  const weightOnly = render({ weightGrams: 500 });
  assert.match(weightOnly, /وزن/);
  assert.doesNotMatch(render({}), /وزن|۴۰ ساعت/);
});
