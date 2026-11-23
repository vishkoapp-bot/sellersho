import assert from 'node:assert/strict';
import { test } from 'node:test';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { Product } from '../../../types';
import { ProductDetailTabs, ProductStockLine } from './ProductDetailInfo';

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

test('PDP stock line uses Persian digits and falls back when the store count is unavailable', () => {
  const render = (details: Partial<Product>) => renderToStaticMarkup(
    <ProductStockLine product={{ stock: 110, ...details } as Product} />,
  );
  assert.match(render({ storeCount: 3 }), /۱۱۰ عدد موجود در ۳ فروشگاه/);
  assert.match(render({ storeCount: 0 }), /۱۱۰ عدد موجود در انبار/);
  assert.match(render({}), /۱۱۰ عدد موجود در انبار/);
  assert.match(render({ stock: 0, storeCount: 3 }), /ناموجود در انبار/);
});
