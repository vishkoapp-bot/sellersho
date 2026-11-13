import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { FieldValue, SupplierGranularFeedback, fieldSection, ImageGallery, imageFieldUrl, allFieldsApproved } from './ProductFieldReview';
import { ToastProvider } from '../../context/ToastContext';

const proposal = {
  id: 'proposal', title: 'محصول نمونه', categorySlug: 'clothes', schemaVersion: 2, productType: 'variable',
  productSchema: { version: 2, type: 'variable', attributes: [{ id: 'size', name: 'سایز', role: 'axis', source: 'requested', values: [{ id: 's', label: 'کوچک' }, { id: 'm', label: 'متوسط' }] }] },
  variants: [{ id: 'sku-a', selection: { size: 's' }, supplier_base_cost: 1000, stock: 5, is_active: true }],
};
const field = (field_key: string, value: any = undefined, decision = 'rejected') => ({ field_key, value, decision, value_hash: 'hash', reason: 'اصلاح شود' });

it('groups review fields into sections for the admin modal', () => {
  assert.equal(fieldSection('/title').id, 'identity');
  assert.equal(fieldSection('/categorySlug').id, 'identity');
  assert.equal(fieldSection('/imageUrls/0').id, 'images');
  assert.equal(fieldSection('/imageUrl').id, 'images');
  assert.equal(fieldSection('/supplierBaseCost').id, 'pricing');
  assert.equal(fieldSection('/variants').id, 'variants');
  assert.equal(fieldSection('/variants/sku-a/stock').id, 'variants');
  assert.equal(fieldSection('/productSchema').id, 'variants');
  assert.equal(fieldSection('/weightGrams').id, 'logistics');
  assert.equal(fieldSection('/preparationDays').id, 'logistics');
});
it('shows complete readable schema options even when the reviewed root snapshot stores only structure', () => {
  const html = renderToStaticMarkup(<FieldValue field={field('/productSchema', { version: 2, type: 'variable', attributes: [{ id: 'size', name: 'سایز', role: 'axis' }] })} refs={{ proposal }} />);
  assert.match(html, /سایز/);
  assert.match(html, /محور تنوع/);
  assert.match(html, /کوچک/);
  assert.match(html, /متوسط/);
  assert.doesNotMatch(html, /\[object Object\]|&quot;version&quot;/);
});
it('renders SKU structure and selection leaves with human option labels', () => {
  const html = renderToStaticMarkup(<><FieldValue field={field('/variants', [{ id: 'sku-a', selection: { size: 's' }, is_active: true }])} refs={{ proposal }} /><FieldValue field={field('/variants/sku-a/selection/size', 's')} refs={{ proposal }} /></>);
  assert.match(html, /سایز: کوچک/);
  assert.match(html, /قیمت عمده/);
  assert.match(html, /موجودی/);
  assert.doesNotMatch(html, /sku-a|\[object Object\]/);
});
it('combines structural correction locks with ordinary rejected supplier inputs', () => {
  const html = renderToStaticMarkup(<ToastProvider><SupplierGranularFeedback proposal={proposal}
    fields={[field('/variants'), field('/variants/sku-a/stock'), field('/variants/sku-a/supplier_base_cost', 1000, 'approved'), field('/variants/sku-a/selection/size'), field('/title')]}
    onClose={() => {}} /></ToastProvider>);
  assert.match(html, /aria-label="اصلاح ساختار و ترکیب‌های محصول"/);
  assert.match(html, /aria-label="قیمت سایز: کوچک" disabled=""/);
  assert.doesNotMatch(html, /aria-label="موجودی سایز: کوچک" disabled=""/);
  assert.match(html, /<textarea aria-label="نام کالا"/);
  assert.match(html, /<select aria-label="انتخاب سایز برای سایز: کوچک"/);
  assert.doesNotMatch(html, /<textarea[^>]*aria-label="ساختار ترکیب‌های محصول"/);
});
it('keeps the legacy supplier numerical leaf correction input', () => {
  const legacy = { ...proposal, schemaVersion: 0, productSchema: undefined, variants: [{ variant_name: 'قدیمی', stock: 3, supplier_base_cost: 1000 }] };
  const html = renderToStaticMarkup(<ToastProvider><SupplierGranularFeedback proposal={legacy} fields={[field('/variants/0/stock')]} onClose={() => {}} /></ToastProvider>);
  assert.match(html, /<textarea aria-label="تنوع \/ قدیمی \/ موجودی"/);
  assert.doesNotMatch(html, /aria-label="اصلاح ساختار و ترکیب‌های محصول"/);
});
it('renders nullable cart limit leaves as ordinary corrections with readable default values', () => {
  const html = renderToStaticMarkup(<ToastProvider><SupplierGranularFeedback proposal={proposal}
    fields={[field('/variants/sku-a/min_cart_qty', 1), field('/variants/sku-a/max_cart_qty', null)]} onClose={() => {}} /></ToastProvider>);
  assert.match(html, /<textarea aria-label="تنوع \/ سایز: کوچک \/ حداقل تعداد خرید"[^>]*>1<\/textarea>/);
  assert.match(html, /<textarea aria-label="تنوع \/ سایز: کوچک \/ حداکثر تعداد خرید"/);
  assert.match(html, /بدون محدودیت تعداد/);
  assert.match(html, /خالی بگذارید تا محدودیت تعداد برداشته شود/);
  assert.doesNotMatch(html, /aria-label="اصلاح ساختار و ترکیب‌های محصول"/);
});

const imageField = (field_key: string, decision = 'pending', reason = '', value = '/static/uploads/products/a.webp') => ({ field_key, value, decision, value_hash: 'hash', reason });

it('gives each gallery image an approve/reject decision instead of view-only rendering', () => {
  const html = renderToStaticMarkup(<ImageGallery
    fields={[imageField('/imageUrl'), { ...imageField('/imageUrls/0', 'approved'), value: '/static/uploads/products/b.webp' }]}
    refs={{}} title="کالا" disabled={false} onDecision={() => {}} onReason={() => {}} />);
  // Current image is the first (pending): decision select + pending badge, no reason box yet.
  assert.match(html, /aria-label="نتیجه تصویر اصلی"/);
  assert.match(html, /در انتظار بررسی/);
  assert.doesNotMatch(html, /دلیل رد تصویر اصلی/);
  // Thumbnails carry each image's decision at a glance.
  assert.match(html, /تصویر 2: تصاویر \/ 1 — تأیید شده/);
  assert.match(html, /bg-emerald-500/);
});

it('shows a required reason box when a gallery image is rejected', () => {
  const html = renderToStaticMarkup(<ImageGallery
    fields={[imageField('/imageUrl', 'rejected')]}
    refs={{}} title="کالا" disabled={false} onDecision={() => {}} onReason={() => {}} />);
  assert.match(html, /aria-label="دلیل رد تصویر اصلی"/);
  assert.match(html, /دلیل رد الزامی است/);
  assert.match(html, /رد شده/);
});

it('publish gate is satisfiable once every field including images is approved', () => {
  const f = (field_key: string, decision: string) => ({ field_key, value: 'x', decision, value_hash: 'h' });
  assert.equal(allFieldsApproved([f('/title', 'approved'), f('/imageUrls/0', 'approved')]), true);
  assert.equal(allFieldsApproved([f('/title', 'approved'), f('/imageUrls/0', 'pending')]), false);
  assert.equal(allFieldsApproved([f('/title', 'approved'), f('/imageUrls/0', 'rejected')]), false);
  assert.equal(allFieldsApproved([f('/imageUrl', 'approved')]), true);
  assert.equal(allFieldsApproved([]), false);
});

it('imageFieldUrl returns the URL only for viewable image values', () => {
  assert.equal(imageFieldUrl(imageField('/imageUrl'), {}), '/static/uploads/products/a.webp');
  assert.equal(imageFieldUrl(imageField('/imageUrl', 'pending', '', ''), {}), null);
  assert.equal(imageFieldUrl(imageField('/imageUrl', 'pending', '', 'not-a-url'), {}), null);
  assert.equal(imageFieldUrl(imageField('/imageUrl', 'pending', '', 'https://cdn.example.com/x.jpg'), {}), 'https://cdn.example.com/x.jpg');
});
