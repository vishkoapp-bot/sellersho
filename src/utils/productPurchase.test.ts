import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import type { Product, ProductVariant } from '../types';
import { axisOptionVariant, cheapestPurchaseVariant, initialDetailPurchaseVariant, initialPurchaseVariant, resolvePurchase, resolveStockBadge, cartQuantityAllowed, sellerVariantPreview, mergeCartPurchase, productForVariant } from './productPurchase';

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
  // B1(b): an inactive product with stock 500 and active in-stock
  // variants must not show «۵۰۰ عدد موجود» — resolvePurchase is
  // undefined for it, so the badge resolves to 'inactive', and the
  // same product active shows its real (maxQty) count.
  it('stock badge never shows a count for a product the buy bar cannot sell', () => {
    const product = fixture();
    product.stock = 500;
    product.isActive = false;
    assert.equal(resolvePurchase(product, 'red-s'), undefined);
    assert.deepEqual(resolveStockBadge(product, 'red-s'), { kind: 'inactive' });
    assert.deepEqual(resolveStockBadge(product), { kind: 'inactive' });
    product.isActive = true;
    assert.deepEqual(resolveStockBadge(product, 'red-s'), { kind: 'in-stock', stock: 5 });
    assert.deepEqual(resolveStockBadge(product, 'red-m'), { kind: 'out-of-stock' });
    assert.deepEqual(resolveStockBadge(product), { kind: 'out-of-stock' });
  });
  it('preserves a valid sold-out SPNC deep link while ordinary initial selection picks stock', () => {
    const product = fixture();
    assert.equal(initialPurchaseVariant(product, 'red-m')?.id, 'red-m');
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
  });
  it('defaults to the lowest payable price, then the greatest stock, then the first variant', () => {
    const product = fixture();
    const [first, second, soldOut] = product.variants!;
    second.stock = 15;
    product.variantPrices![soldOut.id] = { price: 1, originalPrice: 1, discountPercent: 0 };
    assert.equal(initialDetailPurchaseVariant(product)?.id, first.id);
    product.variantPrices![second.id].salePrice = 1100;
    second.maxCartQty = 1;
    assert.equal(initialDetailPurchaseVariant(product)?.id, second.id);
    second.stock = first.stock;
    assert.equal(initialDetailPurchaseVariant(product)?.id, first.id);
    assert.equal(initialDetailPurchaseVariant(product, soldOut.spnc)?.id, soldOut.id);
    first.stock = second.stock = 0;
    assert.equal(initialDetailPurchaseVariant(product), undefined);
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

describe('cheapest purchase variant', () => {
  it('compares effective sale prices and keeps ties in variant order', () => {
    const product = fixture();
    product.variantPrices!['blue-m'].salePrice = 1000;
    assert.equal(initialPurchaseVariant(product)?.id, 'blue-m');
    product.variantPrices!['blue-m'].salePrice = 1100;
    product.variants![1].isDefault = true;
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
  });

  it('skips synthetic spellings and excludes unpriced, inactive, and sold-out options', () => {
    const product = fixture();
    product.variants!.unshift(
      { ...sku('synthetic-zwnj', {}), variantName: 'پیش‌فرض', isDefault: true },
      { ...sku('synthetic-plain', {}), variantName: '', name: 'پیشفرض' },
      { ...sku('inactive', {}), isActive: false },
      sku('unpriced', {}),
    );
    for (const id of ['synthetic-zwnj', 'synthetic-plain', 'inactive', 'red-m']) {
      product.variantPrices![id] = { price: 1, originalPrice: 1, discountPercent: 0 };
    }
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
    assert.equal(cheapestPurchaseVariant(product)?.id, 'red-s');
    assert.equal(resolvePurchase(product, initialPurchaseVariant(product)?.id)?.variant?.id, 'red-s');

    product.productType = 'legacy';
    product.schemaVersion = 0;
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
    assert.equal(resolvePurchase(product), undefined);
    assert.equal(resolvePurchase(product, 'synthetic-zwnj'), undefined);
  });

  it('honors a deep link before a purchasable per-store default and the cheapest option', () => {
    const product = fixture();
    assert.equal(initialPurchaseVariant(product, 'red-m', 'blue-m')?.id, 'red-m');
    assert.equal(initialPurchaseVariant(product, null, 'blue-m')?.id, 'blue-m');
    assert.equal(initialPurchaseVariant(product, null, 'red-m')?.id, 'red-s');
  });

  it('falls through stale variable links and store defaults to the cheapest visible SKU', () => {
    const product = fixture();
    product.variants!.unshift({ ...sku('synthetic', {}), variantName: 'پیش‌فرض' });
    product.variantPrices!.synthetic = { price: 1, originalPrice: 1, discountPercent: 0 };
    assert.equal(initialPurchaseVariant(product, 'synthetic')?.id, 'red-s');
    assert.equal(initialPurchaseVariant(product, null, 'synthetic')?.id, 'red-s');
  });

  it('falls back to a real active option if no priced variants exist', () => {
    const product = fixture();
    product.variants!.unshift({ ...sku('synthetic', {}), variantName: 'پیشفرض', isDefault: true });
    product.variantPrices = {};
    assert.equal(initialPurchaseVariant(product)?.id, 'red-s');
    const synthetic = product.variants![0];
    const blue = product.variants![2];
    product.variants = [synthetic, blue];
    assert.equal(initialPurchaseVariant(product)?.id, 'blue-m');
    assert.equal(resolvePurchase(product), undefined);
    assert.equal(resolvePurchase(product, 'blue-m'), undefined);
    product.variants = [synthetic];
    assert.equal(initialPurchaseVariant(product), undefined);
    assert.equal(resolvePurchase(product), undefined);

    product.productType = 'legacy';
    product.schemaVersion = 0;
    product.variants = [synthetic, blue];
    assert.equal(initialPurchaseVariant(product)?.id, 'blue-m');
    assert.equal(resolvePurchase(product)?.variant, undefined);
    product.variants = [blue];
    assert.equal(resolvePurchase(product)?.variant?.id, 'blue-m');
    product.variants = [synthetic];
    assert.equal(resolvePurchase(product)?.variant?.id, 'synthetic');
  });
});

for (const [productType, schemaVersion] of [['simple', 2], ['legacy', 0]] as const) {
  const variant = { ...sku('default', {}, 10), variantName: 'پیش‌فرض', isDefault: true };
  const product: Product = {
    ...fixture(), productType, schemaVersion, productSchema: undefined, stock: 10,
    variants: [variant],
    variantPrices: { default: { price: 1200, originalPrice: 1200, discountPercent: 0 } },
  };

  it(`keeps the sole پیش‌فرض SKU purchasable for ${productType} products`, () => {
    const purchase = resolvePurchase(product);
    assert.deepEqual({
      initial: initialPurchaseVariant(product)?.id,
      resolved: purchase?.variant?.id,
      price: purchase?.product.price,
      stock: purchase?.maxQty,
      badge: resolveStockBadge(product),
    }, {
      initial: variant.id,
      resolved: variant.id,
      price: 1200,
      stock: 10,
      badge: { kind: 'in-stock', stock: 10 },
    });
    assert.equal(initialPurchaseVariant(product, variant.spnc), variant);
    assert.equal(initialPurchaseVariant(product, null, variant.id), variant);
  });
}
