import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parsePathname, pathForStoreDomain, pathForView } from './routing';

test('panel routes resolve to their guarded views and preserve seller tab deep links', () => {
  const overview = { view: 'seller_dashboard', sellerTab: 'overview' };
  assert.deepEqual(parsePathname('/seller'), overview);
  assert.deepEqual(parsePathname('/seller/'), overview);
  assert.deepEqual(parsePathname('/supplier'), { view: 'supplier_dashboard' });
  assert.deepEqual(parsePathname('/warehouse/', '?section=qc'), { view: 'warehouse_panel' });
  assert.equal(pathForView('seller_dashboard'), '/seller');
  assert.equal(pathForView('seller_dashboard', { sellerTab: 'overview' }), '/seller');
  assert.equal(pathForView('supplier_dashboard'), '/supplier');
  assert.equal(pathForView('warehouse_panel'), '/warehouse');
  const orders = pathForView('seller_dashboard', { sellerTab: 'orders' });
  assert.equal(orders, '/?panel=seller&tab=orders');
  assert.deepEqual(parsePathname('/', orders.slice(1)), { view: 'seller_dashboard', sellerTab: 'orders' });
  assert.deepEqual(parsePathname('/admin'), { view: 'admin_panel' });
  assert.deepEqual(parsePathname('/unknown'), { view: 'not_found' });
  assert.deepEqual(parsePathname('/dashboard'), { view: 'dashboard' });
  assert.deepEqual(parsePathname('/dashboard/'), { view: 'dashboard' });
  assert.equal(pathForView('dashboard'), '/dashboard');
});

test('catalog links preserve both the category and search and distinguish browse-all from home', () => {
  const url = new URL(pathForView('catalog', { searchQuery: '  تلفن همراه  ', categorySlug: 'digital' }), 'http://fixture.test');
  assert.deepEqual(parsePathname(url.pathname, url.search), { view: 'catalog', query: 'تلفن همراه', category: 'digital' });
  assert.equal(pathForView('catalog'), '/?category=all');
  assert.deepEqual(parsePathname('/', '?category=all'), { view: 'catalog', category: 'all' });
  assert.deepEqual(parsePathname('/'), { view: 'customer' });
  assert.deepEqual(parsePathname(pathForView('categories_tree')), { view: 'categories_tree' });
});

test('public account and information routes stay within their URL scope', () => {
  const wishlist = pathForView('customer_wishlist', { storeSlug: 'mone' });
  assert.equal(wishlist, '/store/mone/account/wishlist');
  assert.deepEqual(parsePathname(wishlist), { view: 'customer_wishlist', storeSlug: 'mone' });
  assert.equal(pathForView('customer_wishlist'), '/account/wishlist');
  assert.equal(pathForView('static_page', { storeSlug: 'mone', staticPageKey: 'terms' }), '/store/mone/terms');
  assert.deepEqual(parsePathname('/store/mone/terms'), { view: 'static_page', staticPageKey: 'terms', storeSlug: 'mone' });
  assert.deepEqual(parsePathname('/terms'), { view: 'static_page', staticPageKey: 'terms' });
  assert.equal(pathForStoreDomain('/', 'mone'), '/store/mone');
  assert.equal(pathForStoreDomain('/account/orders', 'mone'), '/store/mone/account/orders');
  assert.equal(pathForStoreDomain('/terms', 'mone'), '/store/mone/terms');
  assert.equal(pathForStoreDomain('/store/other/account', 'mone'), '/store/mone');
  assert.equal(pathForStoreDomain('/product/item/', 'mone'), '/store/mone/product/item/');
  assert.equal(pathForStoreDomain('/store/mone/product/item/', 'mone'), '/store/mone/product/item/');
  assert.equal(pathForStoreDomain('/seller', 'mone'), '/seller');
});

test('collaboration landing pages have addresses that survive reload', () => {
  assert.equal(pathForView('supplier_landing'), '/become-supplier');
  assert.equal(pathForView('dropshipper_landing'), '/become-seller');
  assert.deepEqual(parsePathname('/become-supplier'), { view: 'supplier_landing' });
  assert.deepEqual(parsePathname('/become-seller'), { view: 'dropshipper_landing' });
});

test('the central shop alias resolves to the central route and never a reseller storefront', () => {
  assert.deepEqual(parsePathname('/store/central'), { view: 'customer' });
  assert.deepEqual(parsePathname('/store/central', '?category=gold'), { view: 'catalog', category: 'gold' });
  assert.equal(pathForView('seller_storefront', { storeSlug: 'central' }), '/');
});
