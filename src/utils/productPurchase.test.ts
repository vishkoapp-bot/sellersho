import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import type { Product, ProductVariant } from '../types';
import { axisOptionVariant, initialPurchaseVariant, resolvePurchase, cartQuantityAllowed, sellerVariantPreview, mergeCartPurchase, productForVariant } from './productPurchase';

const sku = (id: string, selection: Record<string, string>, stock = 5): ProductVariant => ({
  id, selection, stock, spnc: id, name: id, variantName: id, attributes: {}, isDefault: false,
  inventoryMode: 'tracked', isActive: true, minCartQty: 1, maxCartQty: null,
});
const fixture = (): Product => ({
  title: 'Product', slug: 'product', category: 'clothing', supplierId: 'supplier', supplierName: 'Supplier',
  platformMargin: 0, discountPercent: 0, cashbackPercent: 0, rating: 0, reviewsCount: 0, salesCount: 0,
  image: '', images: [], specs: {}, description: '', warehouse: '', preparationDays: 1, supplierWarranty: '',
  id: 'p', price: 900, originalPrice: 1000, stock: 50, productType: 'variable', schemaVersion: 2,
  supplierBaseCost: 99999,
  productSchema: { version: 2, type: 'variable', attributes: [
    { id: 'color', name: 'Color', role: 'axis', source: 'color', values: [{ id: 'r', label: 'Red' }, { id: 'b', label: 'Blue' }] },
    { id: 'size', name: 'Size', role: 'axis', source: 'local', values: [{ id: 's', label: 'S' }, { id: 'm', label: 'M' }] },
  ] },
  variants: [sku('red-s', { color: 'r', size: 's' }), sku('blue-m', { color: 'b', size: 'm' }), sku('red-m', { color: 'r', size: 'm' }, 0)],
  variantPrices: { 'red-s': { price: 1100, originalPrice: 1200, discountPercent: 8 }, 'blue-m': { price: 2200, originalPrice: 2400, discountPercent: 8 } },
});

describe('canonical customer SKU resolution', () => {
  it('requires a real active SKU and never substitutes the product price for a V2 SKU', () => {
    const product = fixture();
    assert.equal(resolvePurchase(product), undefined);
    assert.equal(resolvePurchase(product, 'foreign'), undefined);
    assert.equal(resolvePurchase(product, 'red-m'), undefined);
    assert.equal(resolvePurchase(product, 'red-s')?.product.price, 1100);
    assert.equal(resolvePurchase(product, 'red-s')?.variant?.id, 'red-s');
    product.variants![0].isActive = false;
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
    assert.equal(resolvePurchase(product, 'blue-m')?.product.price, 2200);
    delete product.variantPrices!['blue-m'];
    assert.equal(resolvePurchase(product, 'blue-m'), undefined);
  });
  it('preserves a valid sold-out SPNC deep link while ordinary initial selection picks stock', () => {
    const product = fixture();
    assert.equal(initialPurchaseVariant(product, 'red-m')?.id, 'red-m');
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
  });
  it('resolves sparse axis choices without fabricating a combination or allowing tracked zero stock', () => {
    const product = fixture();
    assert.equal(axisOptionVariant(product, 'red-s', 'color', 'b')?.id, 'blue-m');
    assert.equal(axisOptionVariant(product, 'red-s', 'size', 'm'), undefined);
    assert.equal(axisOptionVariant(product, 'blue-m', 'size', 's'), undefined);
  });
  it('keeps legacy unknown SKU inventory purchasable using product stock and preserves its price', () => {
    const product = fixture();
    product.productType = 'legacy'; product.schemaVersion = 0;
    product.variants = [{ ...sku('old', {}, 0), inventoryMode: 'legacy', isDefault: true }];
    assert.equal(resolvePurchase(product)?.product.price, 900);
    assert.equal(resolvePurchase(product)?.maxQty, 50);
  });
  it('applies the strictest stock/max bound and aggregates addon-separated cart lines', () => {
    const product = fixture();
    product.variants![0].minCartQty = 2;
    product.variants![0].maxCartQty = 3;
    const purchase = resolvePurchase(product, 'red-s')!;
    const lines = [{ product, quantity: 2, selectedVariantId: 'red-s', selectedAddonIds: ['gift'] }];
    assert.equal(cartQuantityAllowed(lines, purchase, 1), true);
    assert.equal(cartQuantityAllowed(lines, purchase, 2), false);
    assert.equal(cartQuantityAllowed([], purchase, 1), false);
    assert.equal(cartQuantityAllowed([], purchase, NaN), false);
    assert.equal(cartQuantityAllowed([], purchase, 2.5), false);
    product.variants![0].maxCartQty = 100;
    assert.equal(resolvePurchase(product, 'red-s')?.maxQty, 5);
  });
  it('uses only per-SKU authorized cost or authoritative public SKU price for seller previews', () => {
    const product = fixture();
    const variant = product.variants![0];
    assert.deepEqual(sellerVariantPreview(product, variant, 20, 10), { finalPrice: 1100 });
    variant.supplierBaseCost = 1000;
    assert.deepEqual(sellerVariantPreview(product, variant, 20, 10), { baseCost: 1200, finalPrice: 1300, profit: 100 });
  });
  it('counts old cart snapshots with an implicit default against the same SKU limit', () => {
    const product = fixture();
    product.productType = 'legacy'; product.schemaVersion = 0;
    product.variants![0].isDefault = true;
    product.variants![0].maxCartQty = 3;
    const purchase = resolvePurchase(product, 'red-s')!;
    const historical = { ...product, price: 77 };
    assert.equal(cartQuantityAllowed([{ product: historical, quantity: 3 }], purchase, 1), false);
    assert.equal(historical.price, 77);
  });
  it('does not treat an explicit zero maximum or malformed stock as unlimited', () => {
    const product = fixture();
    product.variants![0].maxCartQty = 0;
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
    product.variants![0].maxCartQty = null;
    product.variants![0].stock = NaN;
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
  });
  it('fails closed for canonical variable SKUs with missing inventory or price metadata', () => {
    const product = fixture();
    product.schemaVersion = undefined;
    product.variants![2].inventoryMode = undefined;
    product.variantPrices!['red-m'] = { price: 100, originalPrice: 100, discountPercent: 0 };
    assert.equal(resolvePurchase(product, 'red-m'), undefined);
    delete product.variantPrices!['red-s'];
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
    product.variantPrices!['red-s'] = { price: NaN, originalPrice: 100, discountPercent: 0 };
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
  });
  it('preserves later choices when changing one of three axes and disables excluded completions', () => {
    const product = fixture();
    product.productSchema!.attributes.push({ id: 'fabric', name: 'Fabric', role: 'axis', source: 'local', values: [{ id: 'cotton', label: 'Cotton' }, { id: 'wool', label: 'Wool' }] });
    product.variants = [sku('red-s-cotton', { color: 'r', size: 's', fabric: 'cotton' }), sku('blue-s-cotton', { color: 'b', size: 's', fabric: 'cotton' }), sku('blue-m-wool', { color: 'b', size: 'm', fabric: 'wool' })];
    product.variantPrices = Object.fromEntries(product.variants.map((variant) => [variant.id, { price: 10, originalPrice: 10, discountPercent: 0 }]));
    assert.equal(axisOptionVariant(product, 'red-s-cotton', 'color', 'b')?.id, 'blue-s-cotton');
    assert.equal(axisOptionVariant(product, 'blue-s-cotton', 'size', 'm')?.id, 'blue-m-wool');
    assert.equal(axisOptionVariant(product, 'blue-m-wool', 'fabric', 'cotton'), undefined);
  });
  it('merges the exact cart key, preserves historical price snapshots, and rejects cumulative overselling', () => {
    const product = fixture();
    product.productType = 'legacy'; product.schemaVersion = 0;
    const purchase = resolvePurchase(product, 'red-s')!;
    const original = [{ product: { ...product, price: 77 }, quantity: 2, selectedVariantId: 'red-s', selectedAddonIds: ['gift'], storeId: 'store-a' }];
    const merged = mergeCartPurchase(original, purchase, 1, { selectedAddonIds: ['gift'], storeId: 'store-a' });
    assert.equal(merged.length, 1);
    assert.equal(merged[0].quantity, 3);
    assert.equal(merged[0].product.price, 77);
    assert.equal(original[0].quantity, 2);
    const separate = mergeCartPurchase(merged, purchase, 2, { selectedAddonIds: ['wrap'], storeId: 'store-a' });
    assert.equal(separate.length, 2);
    assert.equal(separate[1].selectedVariantId, 'red-s');
    assert.equal(separate[1].product.price, 1100);
    assert.equal(mergeCartPurchase(separate, purchase, 1, { storeId: 'store-a' }), separate);
  });
  it('increases only one persisted duplicate key so the checked aggregate remains exact', () => {
    const product = fixture();
    const purchase = resolvePurchase(product, 'red-s')!;
    const original = [
      { product, quantity: 2, selectedVariantId: 'red-s' },
      { product, quantity: 2, selectedVariantId: 'red-s' },
    ];
    const result = mergeCartPurchase(original, purchase, 1, {});
    assert.equal(result.reduce((sum, line) => sum + line.quantity, 0), 5);
  });
  it('shows canonical fixed attributes as specifications while selectors own the variation axes', () => {
    const product = fixture();
    product.specs = { Warranty: 'One year', Color: 'legacy axis text' };
    product.productSchema!.attributes.push({ id: 'fabric', name: 'Fabric', role: 'fixed', source: 'local', values: [{ id: 'cotton', label: 'Cotton' }] });
    const display = productForVariant(product, product.variants![0]);
    assert.deepEqual(display.specs, { Warranty: 'One year', Fabric: 'Cotton' });
    assert.deepEqual(product.specs, { Warranty: 'One year', Color: 'legacy axis text' });
    assert.equal(productForVariant({ ...product, productSchema: undefined }).specs, product.specs);
  });
});
