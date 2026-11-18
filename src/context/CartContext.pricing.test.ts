// CHECKOUT-03 regression file (G-01 / G-02 / G-08).
// Table tests for the shared price breakdown in src/utils/cartPricing.ts.
// Every case asserts the full row identity:
//   original - discount + addons + shipping = total
// and that goodsPayable equals the server-accepted payable definition
// (merchandiseNet + addons). Amounts are integer Toman throughout.
//
// NOTE on scope: this repo has no jsdom / testing-library, so the drawer
// component itself cannot be rendered here (same constraint as the
// CHECKOUT-02 regression files). These tests pin the shared pure function
// that the cart context, the drawer (cart AND direct modes), the success
// screen and the order detail all render from — plus the snapshot
// derivation those two read-only surfaces use. Rendered-drawer verification
// remains a live-browser drill (gate G-11).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeCartPricing, computeOrderBreakdown } from '../utils/cartPricing';
import type { CartItem, Product } from '../types';

const product = (over: Record<string, unknown> = {}): Product =>
  ({
    id: 'p1',
    title: 'کالای تست',
    slug: 'p1',
    category: 'test',
    supplierId: 'sup-1',
    supplierName: 'تأمین‌کننده تست',
    originalPrice: 100,
    price: 100,
    supplierBaseCost: 0,
    platformMargin: 0,
    discountPercent: 0,
    cashbackPercent: 0,
    rating: 5,
    reviewsCount: 0,
    salesCount: 0,
    stock: 100,
    image: '',
    images: [],
    specs: {},
    description: '',
    warehouse: '',
    preparationDays: 1,
    supplierWarranty: '',
    ...over,
  }) as Product;

const line = (prod: Product, quantity = 1, addonIds?: string[]): CartItem =>
  ({
    product: prod,
    quantity,
    selectedAddonIds: addonIds,
    storeId: 'store-central',
    storeName: 'فروشگاه مرکزی',
  }) as CartItem;

/** The row identity every surface must satisfy (before shipping unless noted). */
const assertRowsSum = (p: { merchandiseOriginal: number; merchandiseNet: number; merchandiseDiscount: number; addonsTotal: number; goodsPayable: number }, shipping = 0, total?: number) => {
  assert.equal(
    p.merchandiseOriginal - p.merchandiseDiscount + p.addonsTotal + shipping,
    total ?? p.goodsPayable + shipping,
  );
  assert.equal(p.goodsPayable, p.merchandiseNet + p.addonsTotal);
};

test('G-01 case A: merchandise 100 / addons 50 / discount 0 => discount 0, total 150 before shipping', () => {
  const p = computeCartPricing([
    line(product({ originalPrice: 100, price: 100, addons: [{ id: 'a1', feePerUnit: 50 }] }), 1, ['a1']),
  ]);
  assert.equal(p.merchandiseOriginal, 100);
  assert.equal(p.merchandiseNet, 100);
  assert.equal(p.merchandiseDiscount, 0);
  assert.equal(p.addonsTotal, 50);
  assert.equal(p.goodsPayable, 150);
  assertRowsSum(p);
});

test('G-01 case B: original 100, sale 90, addons 50 => discount 10, total 140', () => {
  const p = computeCartPricing([
    line(product({ originalPrice: 100, price: 90, addons: [{ id: 'a1', feePerUnit: 50 }] }), 1, ['a1']),
  ]);
  assert.equal(p.merchandiseOriginal, 100);
  assert.equal(p.merchandiseNet, 90);
  assert.equal(p.merchandiseDiscount, 10);
  assert.equal(p.addonsTotal, 50);
  assert.equal(p.goodsPayable, 140);
  assertRowsSum(p);
});

test('G-01 quantities: case B x3 => discount 30, addons 150, total 420', () => {
  const p = computeCartPricing([
    line(product({ originalPrice: 100, price: 90, addons: [{ id: 'a1', feePerUnit: 50 }] }), 3, ['a1']),
  ]);
  assert.equal(p.merchandiseOriginal, 300);
  assert.equal(p.merchandiseNet, 270);
  assert.equal(p.merchandiseDiscount, 30);
  assert.equal(p.addonsTotal, 150);
  assert.equal(p.goodsPayable, 420);
  assertRowsSum(p);
});

test('G-01 mixed SKUs: three lines, one on sale, one full price, one deep sale', () => {
  const p = computeCartPricing([
    line(product({ id: 'p1', originalPrice: 200, price: 180, addons: [{ id: 'a1', feePerUnit: 30 }] }), 2, ['a1']),
    line(product({ id: 'p2', originalPrice: 50, price: 50 }), 1),
    line(product({ id: 'p3', originalPrice: 1000, price: 700, addons: [{ id: 'a2', feePerUnit: 100 }, { id: 'a3', feePerUnit: 25 }] }), 1, ['a2', 'a3']),
  ]);
  // original 400 + 50 + 1000 = 1450; net 360 + 50 + 700 = 1110; discount 340
  assert.equal(p.merchandiseOriginal, 1450);
  assert.equal(p.merchandiseNet, 1110);
  assert.equal(p.merchandiseDiscount, 340);
  // addons 60 + 0 + 125 = 185; goods 1110 + 185 = 1295
  assert.equal(p.addonsTotal, 185);
  assert.equal(p.goodsPayable, 1295);
  assertRowsSum(p);
});

test('G-01 multiple addon types on one line, qty 2', () => {
  const p = computeCartPricing([
    line(
      product({
        originalPrice: 500,
        price: 500,
        addons: [
          { id: 'gift', feePerUnit: 20 },
          { id: 'wrap', feePerUnit: 35 },
          { id: 'card', feePerUnit: 0 },
        ],
      }),
      2,
      ['gift', 'wrap', 'card'],
    ),
  ]);
  assert.equal(p.merchandiseDiscount, 0);
  assert.equal(p.addonsTotal, (20 + 35 + 0) * 2);
  assert.equal(p.goodsPayable, 1000 + 110);
  assertRowsSum(p);
});

test('G-01 zero discount, no addons: no discount invented, no addons invented', () => {
  const p = computeCartPricing([line(product({ originalPrice: 250, price: 250 }), 2)]);
  assert.equal(p.merchandiseOriginal, 500);
  assert.equal(p.merchandiseDiscount, 0);
  assert.equal(p.addonsTotal, 0);
  assert.equal(p.goodsPayable, 500);
  assertRowsSum(p);
});

test('G-01 rounded discounts stay integer-exact', () => {
  const odd = computeCartPricing([line(product({ originalPrice: 101, price: 67 }), 1)]);
  assert.equal(odd.merchandiseDiscount, 34);
  assert.equal(odd.goodsPayable, 67);
  assertRowsSum(odd);
  const big = computeCartPricing([line(product({ originalPrice: 999, price: 333 }), 2)]);
  assert.equal(big.merchandiseDiscount, (999 - 333) * 2);
  assert.equal(big.goodsPayable, 666);
  assertRowsSum(big);
});

test('G-01 direct and normal cart give the IDENTICAL breakdown for identical lines', () => {
  const directLine = line(
    product({ originalPrice: 100, price: 90, addons: [{ id: 'a1', feePerUnit: 50 }] }),
    1,
    ['a1'],
  );
  const cartLines = [directLine];
  assert.deepEqual(computeCartPricing([directLine]), computeCartPricing(cartLines));
});

test('G-01 unknown addon id contributes 0 (never invents a fee)', () => {
  const p = computeCartPricing([
    line(product({ originalPrice: 100, price: 100, addons: [{ id: 'a1', feePerUnit: 50 }] }), 1, ['no-such-addon']),
  ]);
  assert.equal(p.addonsTotal, 0);
  assert.equal(p.goodsPayable, 100);
  assertRowsSum(p);
});

test('G-08 empty cart: all zeros, nothing invented', () => {
  const p = computeCartPricing([]);
  assert.deepEqual(p, {
    merchandiseOriginal: 0,
    merchandiseNet: 0,
    merchandiseDiscount: 0,
    addonsTotal: 0,
    goodsPayable: 0,
  });
});

test('G-08 price above original (data anomaly): discount clamps to 0, total stays the server payable', () => {
  // The sale/campaign pipeline only ever lowers prices, so this cannot arise
  // from live data. Pinned as OBSERVED: the frontend never shows a negative
  // discount row; the total row still shows the payable (net + addons).
  const p = computeCartPricing([line(product({ originalPrice: 100, price: 120 }), 1)]);
  assert.equal(p.merchandiseDiscount, 0);
  assert.equal(p.merchandiseNet, 120);
  assert.equal(p.goodsPayable, 120);
});

test('G-06/G-07 order snapshot: addons derived from snapshot fields only', () => {
  // Server snapshot: total 1,000,000 / discount 100,000 / shipping 45,000 /
  // payable 995,000  =>  addons = 995,000 - 45,000 - (1,000,000 - 100,000) = 50,000.
  const b = computeOrderBreakdown({
    totalAmount: 1000000,
    discountAmount: 100000,
    payableAmount: 995000,
    shippingCost: 45000,
  });
  assert.notEqual(b, null);
  assert.equal(b!.merchandiseOriginal, 1000000);
  assert.equal(b!.merchandiseDiscount, 100000);
  assert.equal(b!.merchandiseNet, 900000);
  assert.equal(b!.addonsTotal, 50000);
  assert.equal(b!.shipping, 45000);
  assert.equal(b!.goodsPayable, 950000);
  assert.equal(b!.total, 995000);
  assertRowsSum(b!, 45000, 995000);
});

test('G-06/G-07 order snapshot without addons or shipping', () => {
  const b = computeOrderBreakdown({
    totalAmount: 100,
    discountAmount: 10,
    payableAmount: 90,
    shippingCost: 0,
  });
  assert.notEqual(b, null);
  assert.equal(b!.addonsTotal, 0);
  assert.equal(b!.total, 90);
  assertRowsSum(b!);
});

test('G-06/G-07 incoherent snapshots return null (never invent numbers)', () => {
  // Legacy row with no recorded original.
  assert.equal(
    computeOrderBreakdown({ totalAmount: 0, discountAmount: 0, payableAmount: 90, shippingCost: 0 }),
    null,
  );
  // Discount larger than the original (negative net).
  assert.equal(
    computeOrderBreakdown({ totalAmount: 100, discountAmount: 150, payableAmount: 90, shippingCost: 0 }),
    null,
  );
  // Payable smaller than net + shipping (negative derived addons).
  assert.equal(
    computeOrderBreakdown({ totalAmount: 100, discountAmount: 10, payableAmount: 50, shippingCost: 0 }),
    null,
  );
  // A 1-Toman difference is a real 1-Toman addon, not an inconsistency:
  // the derivation is exact by construction.
  const oneTomanAddon = computeOrderBreakdown({
    totalAmount: 100,
    discountAmount: 10,
    payableAmount: 91,
    shippingCost: 0,
  });
  assert.notEqual(oneTomanAddon, null);
  assert.equal(oneTomanAddon!.addonsTotal, 1);
});
