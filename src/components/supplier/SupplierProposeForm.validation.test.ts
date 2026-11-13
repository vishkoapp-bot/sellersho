import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateProposeStep1, validateProposeStep3, isProposeMatrixStale } from './SupplierProposeForm';
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
