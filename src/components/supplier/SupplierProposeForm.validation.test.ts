import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateProposeStep1, validateProposeStep3, isProposeMatrixStale, pickNicheForCategory, parsePosInt, normalizeRestoredDraftNumerics } from './SupplierProposeForm';
import type { NicheDto } from '../../services/catalogApi';
import { buildProductMatrix } from '../../services/productModel';
import type { ProductSchema } from '../../types/productSchema';
import type { CategorySelection } from './SupplierCategoryPicker';

const leafCategory: CategorySelection = {
  slug: 'digital',
  title: 'کالای دیجیتال',
  path: [{ id: 'cat-digital', title: 'کالای دیجیتال', slug: 'digital' }],
  pathDisplay: 'کالای دیجیتال',
  isLeaf: true,
};
const nonLeafCategory: CategorySelection = { ...leafCategory, isLeaf: false };

const validStep1 = {
  title: 'ساعت هوشمند',
  brand: 'اپل',
  noBrand: false,
  category: leafCategory,
  description: 'توضیحات کامل محصول با بیش از سی نویسه برای تست اعتبارسنجی.',
  selectedNicheIds: ['niche-smartwatch'],
  nichesReady: true,
};

test('step 1: valid input passes; each gate fires with the submit message, in order', () => {
  assert.deepEqual(validateProposeStep1(validStep1), []);
  assert.deepEqual(validateProposeStep1({ ...validStep1, title: '  ' }).map((e) => e.field), ['title']);
  assert.equal(validateProposeStep1({ ...validStep1, title: '' })[0].message, 'لطفاً نام کالا را وارد کنید.');
  assert.equal(validateProposeStep1({ ...validStep1, brand: 'x'.repeat(101) })[0].message, 'نام برند نمی‌تواند بیشتر از ۱۰۰ نویسه باشد.');
  assert.equal(validateProposeStep1({ ...validStep1, category: nonLeafCategory })[0].message, 'دسته‌بندی کالا را انتخاب کنید.');
  assert.equal(validateProposeStep1({ ...validStep1, category: null })[0].message, 'دسته‌بندی کالا را انتخاب کنید.');
  assert.equal(validateProposeStep1({ ...validStep1, brand: '' })[0].message, 'برند کالا را وارد کنید یا گزینه «بدون برند» را انتخاب کنید.');
  assert.deepEqual(validateProposeStep1({ ...validStep1, brand: '' , noBrand: true }), []);
  assert.equal(validateProposeStep1({ ...validStep1, description: 'کوتاه' })[0].message, 'توضیحات محصول باید حداقل ۳۰ نویسه باشد.');
  assert.equal(validateProposeStep1({ ...validStep1, selectedNicheIds: [] })[0].message, 'حداقل یک موضوع (نیچ) برای این کالا انتخاب کن.');
});

test('step 1: niche gate does not block while niches are still loading', () => {
  assert.deepEqual(validateProposeStep1({ ...validStep1, selectedNicheIds: [], nichesReady: false }), []);
});

test('step 3: weight/dims all required; packaging must be all-or-none and positive integers', () => {
  const valid = { weightGrams: '1200', lengthCm: '40', widthCm: '30', heightCm: '20', packLengthCm: '', packWidthCm: '', packHeightCm: '' };
  assert.deepEqual(validateProposeStep3(valid), []);
  assert.equal(validateProposeStep3({ ...valid, weightGrams: '' })[0].field, 'weightDims');
  assert.equal(validateProposeStep3({ ...valid, weightGrams: '' })[0].message, 'وزن (گرم) و هر سه بُعد کالا (سانتی‌متر) الزامی است؛ بدون آن‌ها پیشنهاد تأیید نمی‌شود.');
  assert.equal(validateProposeStep3({ ...valid, heightCm: '0' })[0].field, 'weightDims');
  assert.equal(validateProposeStep3({ ...valid, packLengthCm: '45' })[0].message, 'ابعاد بسته‌بندی را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).');
  assert.equal(validateProposeStep3({ ...valid, packLengthCm: '45', packWidthCm: '35', packHeightCm: '0' })[0].message, 'ابعاد بسته‌بندی باید عددهای صحیح مثبت باشند (سانتی‌متر).');
  assert.deepEqual(validateProposeStep3({ ...valid, packLengthCm: '45', packWidthCm: '35', packHeightCm: '25' }), []);
});

test('step 3: formatted inputs pass — Persian digits, separators, unit suffixes (G5 half B)', () => {
  const persian = { weightGrams: '۱٬۲۰۰', lengthCm: '۴۰', widthCm: '۳۰', heightCm: '۲۰', packLengthCm: '', packWidthCm: '', packHeightCm: '' };
  assert.deepEqual(validateProposeStep3(persian), []);
  const latinSep = { ...persian, weightGrams: '1,200', lengthCm: '40', widthCm: '30', heightCm: '20' };
  assert.deepEqual(validateProposeStep3(latinSep), []);
  const withUnit = { ...persian, weightGrams: '1200 گرم', lengthCm: '40', widthCm: '30', heightCm: '20' };
  assert.deepEqual(validateProposeStep3(withUnit), []);
  const packFormatted = { ...persian, packLengthCm: '۴۵', packWidthCm: '۳۵', packHeightCm: '۲۵' };
  assert.deepEqual(validateProposeStep3(packFormatted), []);
  // The guards still fire: empty, zero and non-numeric stay invalid.
  assert.equal(validateProposeStep3({ ...persian, weightGrams: '' })[0].field, 'weightDims');
  assert.equal(validateProposeStep3({ ...persian, weightGrams: '۰' })[0].field, 'weightDims');
  assert.equal(validateProposeStep3({ ...persian, weightGrams: 'abc' })[0].field, 'weightDims');
  assert.equal(validateProposeStep3({ ...packFormatted, packHeightCm: '۰' })[0].message, 'ابعاد بسته‌بندی باید عددهای صحیح مثبت باشند (سانتی‌متر).');
});

test('parsePosInt is the one shared helper: normalises digits, rejects empty/zero/non-numeric', () => {
  assert.equal(parsePosInt('۱٬۲۰۰'), 1200);
  assert.equal(parsePosInt('1,200'), 1200);
  assert.equal(parsePosInt('1200 گرم'), 1200);
  assert.equal(parsePosInt('1200'), 1200);
  assert.equal(parsePosInt('۴۰'), 40);
  assert.equal(parsePosInt(''), undefined);
  assert.equal(parsePosInt('  '), undefined);
  assert.equal(parsePosInt('۰'), undefined);
  assert.equal(parsePosInt('0'), undefined);
  assert.equal(parsePosInt('abc'), undefined);
  assert.equal(parsePosInt('12.5'), undefined);
  assert.equal(parsePosInt('-5'), undefined);
});

test('draft restore normalises formatted numerics and never mangles a non-numeric value to 0 (G5 half B)', () => {
  const restored = normalizeRestoredDraftNumerics({
    weightGrams: '۱٬۲۰۰',
    lengthCm: '۴۰',
    widthCm: '30',
    heightCm: '20',
    packLengthCm: '۴۵',
    packWidthCm: '',
    packHeightCm: '۲۵',
    cost: '۱٬۵۰۰',
    stock: 'abc',
    prepDays: '',
    title: 'ساعت هوشمند',
  } as never);
  assert.equal(restored.weightGrams, '1200');
  assert.equal(restored.lengthCm, '40');
  assert.equal(restored.widthCm, '30');
  assert.equal(restored.heightCm, '20');
  assert.equal(restored.packLengthCm, '45');
  assert.equal(restored.packWidthCm, '');
  assert.equal(restored.packHeightCm, '25');
  assert.equal(restored.cost, '1500');
  // A genuinely non-numeric legacy value falls back to '' (empty state),
  // never to 0; unrelated fields pass through untouched.
  assert.equal(restored.stock, '');
  assert.equal(restored.prepDays, '');
  assert.equal(restored.title, 'ساعت هوشمند');
  assert.equal(normalizeRestoredDraftNumerics({ lengthCm: '1.5' } as never).lengthCm, '');
  assert.equal(normalizeRestoredDraftNumerics({ lengthCm: '-5' } as never).lengthCm, '');
  // Plain digits survive the round-trip unchanged (regression).
  assert.equal(normalizeRestoredDraftNumerics({ weightGrams: '1200', lengthCm: '40' } as never).weightGrams, '1200');
});

test('matrix staleness: unchanged rows are current; a schema change without rebuild is stale', () => {
  const schema: ProductSchema = { version: 2, type: 'variable', attributes: [{ id: 'color', name: 'رنگ', source: 'color', role: 'axis', values: [{ id: 'r', label: 'قرمز' }, { id: 'b', label: 'آبی' }] }] };
  let i = 0;
  const rows = buildProductMatrix(schema, [], () => `id-${++i}`);
  assert.equal(isProposeMatrixStale(schema, rows), false);
  const changed: ProductSchema = { ...schema, attributes: [{ ...schema.attributes[0], values: [...schema.attributes[0].values, { id: 'g', label: 'سبز' }] }] };
  assert.equal(isProposeMatrixStale(changed, rows), true);
  const simple: ProductSchema = { version: 2, type: 'simple', attributes: [] };
  assert.equal(isProposeMatrixStale(simple, buildProductMatrix(simple, [], () => `id-${++i}`)), false);
});

const fashionNiches: NicheDto[] = [
  { id: 'niche-women-style', title: 'استایل زنانه', slug: 'women-style', categorySlug: 'fashion' },
  { id: 'niche-men-essentials', title: 'ضروری‌های مردانه', slug: 'men-essentials', categorySlug: 'fashion' },
  { id: 'niche-leather', title: 'کیف و اکسسوری چرم طبیعی', slug: 'leather', categorySlug: 'fashion' },
  { id: 'niche-smartwatch', title: 'ساعت هوشمند و گجت‌های پوشیدنی', slug: 'smartwatch', categorySlug: 'digital' },
];

test('niche pick: women tee-shirt picks women-style, not the old hardcoded leather', () => {
  assert.equal(pickNicheForCategory(['مد و پوشاک', 'لباس زنانه', 'تی شرت زنانه'], fashionNiches)?.id, 'niche-women-style');
});

test('niche pick: leather bag picks leather, men shirt picks men-essentials, no match returns null', () => {
  assert.equal(pickNicheForCategory(['مد و پوشاک', 'کیف و کفش', 'کیف چرم زنانه'], fashionNiches)?.id, 'niche-leather');
  assert.equal(pickNicheForCategory(['مد و پوشاک', 'لباس مردانه', 'پیراهن مردانه'], fashionNiches)?.id, 'niche-men-essentials');
  assert.equal(pickNicheForCategory(['کالای دیجیتال', 'هدفون'], fashionNiches), null);
  assert.equal(pickNicheForCategory([], fashionNiches), null);
  assert.equal(pickNicheForCategory(['مد و پوشاک'], []), null);
});
