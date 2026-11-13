// Pure-function tests for the category suggester (spec §9).
// Fixtures are hand-built CategoryDto[] trees mirroring the live tree in
// launch-readiness/category-suggest/categories-excerpt.json; no network.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  normalizePersian,
  tokenize,
  buildCompoundVocabulary,
  suggestCategories,
  decideAutoApply,
} from './categorySuggest';
import type { CategoryDto } from './catalogApi';

const cat = (slug: string, title: string, parentId: string | null): CategoryDto => ({
  id: `cat-${slug}`,
  title,
  slug,
  iconName: '',
  parentId,
});

/** Root -> leaf chain; each entry is [slug, title]. */
function chain(entries: [string, string][]): CategoryDto[] {
  return entries.map(([slug, title], i) => cat(slug, title, i === 0 ? null : `cat-${entries[i - 1][0]}`));
}

/** Merge chains, deduping shared ancestors by slug. */
function combine(...lists: CategoryDto[][]): CategoryDto[] {
  const bySlug = new Map<string, CategoryDto>();
  for (const c of lists.flat()) if (!bySlug.has(c.slug)) bySlug.set(c.slug, c);
  return [...bySlug.values()];
}

// Paths mirror the excerpt (women leaves, example leaves, branch samples).
// Fabricated leaves (not in the excerpt) are marked: their paths mirror the
// analogous live leaves (men's hoodies mirror the women's, kitten bottle
// mirrors the baby bottle, engine cover mirrors the phone-cover branch).
const FIXTURE = combine(
  // Fashion / women
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-tee-shirts', 'تی شرت زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-sportwear', 'لباس ورزشی زنانه'], ['women-sport-t-shirts-and-polos', 'تی شرت ورزشی زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-traditional-tee-shirts', 'تی شرت سنتی زنانه']]), // fabricated
  chain([['fashion', 'مد و پوشاک'], ['boys-apparel', 'پسرانه'], ['boys-clothing', 'لباس پسرانه'], ['boys-tee-polo-sport', 'تیشرت و پولوشرت ورزشی پسرانه']]), // the joined-تیشرت leaf
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-homewear', 'لباس خواب و راحتی زنانه'], ['women-homewear-tops-and-t-shirts', 'تاپ و تی‌شرت راحتی زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-shoes', 'کفش زنانه'], ['women-sport-shoes', 'کفش ورزشی زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-accessories', 'اکسسوری زنانه'], ['women-watches', 'ساعت زنانه'], ['women-digital-watches', 'ساعت دیجیتال زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-accessories', 'اکسسوری زنانه'], ['women-watches', 'ساعت زنانه'], ['women-analouge-watches', 'ساعت عقربه ای زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-accessories', 'اکسسوری زنانه'], ['women-jewelry', 'زیورآلات زنانه'], ['uni-pendants-sets-women', 'ست آویز زنانه و مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-accessories', 'اکسسوری زنانه'], ['women-jewelry', 'زیورآلات زنانه'], ['women-pendants', 'آویز زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-hoodie', 'هودی زنانه'], ['women-hoodies-collection', 'مجموعه هودی زنانه']]),
  chain([['fashion', 'مد و پوشاک'], ['womens-apparel', 'زنانه'], ['women-clothing', 'لباس زنانه'], ['women-hoodie', 'هودی زنانه'], ['women-print-hoodies', 'هودی چاپی زنانه']]),
  // Fashion / men
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-shoes', 'کفش مردانه'], ['men-sport-shoes', 'کفش ورزشی مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-shoes', 'کفش مردانه'], ['casual-shoes-for-men', 'کفش روزمره مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-shoes', 'کفش مردانه'], ['men-driving-shoes', 'کفش کالج مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-shoes', 'کفش مردانه'], ['men-formal-shoes', 'کفش رسمی مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-clothing', 'لباس مردانه'], ['men-hoodie', 'هودی مردانه'], ['men-print-hoodie', 'هودی چاپی مردانه']]), // fabricated
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-clothing', 'لباس مردانه'], ['men-hoodie', 'هودی مردانه'], ['men-hoodies-collection', 'مجموعه هودی مردانه']]), // fabricated
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-clothing', 'لباس مردانه'], ['men-sportwear', 'لباس ورزشی مردانه'], ['men-sport-sweatshirts-hoodies', 'سویشرت و هودی ورزشی مردانه']]), // fabricated
  chain([['fashion', 'مد و پوشاک'], ['uni-apparel', 'زنانه و مردانه'], ['uni-shoes', 'کفش زنانه و مردانه']]),
  chain([['rural-products', 'محصولات بومی و محلی'], ['rural-clothing', 'پوشاک بومی و محلی'], ['men-traditional', 'لباس سنتی و محلی'], ['men-traditional-clothing', 'لباس محلی مردانه'], ['men-traditional-shoes', 'کفش دست‌دوز مردانه']]),
  chain([['fashion', 'مد و پوشاک'], ['mens-apparel', 'مردانه'], ['men-accessories', 'اکسسوری مردانه'], ['men-jewelry', 'زیورآلات مردانه'], ['uni-pendants-sets-men', 'ست آویز زنانه و مردانه']]), // fabricated
  // Digital
  chain([['digital', 'کالای دیجیتال'], ['wearable-gadget', 'ساعت و مچ بند هوشمند'], ['smart-watch', 'ساعت هوشمند']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['cell-phone-pouch-cover', 'کیف و کاور گوشی']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['headphone-cover', 'کیف و کاور هدفون']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['cell-phone-screen-guard', 'محافظ صفحه نمایش گوشی']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['mobile-lens-screen-protector', 'محافظ لنز گوشی موبایل']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['cell-phone-battery', 'باتری گوشی']]),
  chain([['digital', 'کالای دیجیتال'], ['accessories-main', 'لوازم جانبی کالای دیجیتال'], ['mobile-accessories', 'لوازم جانبی گوشی موبایل'], ['car-charger', 'شارژر تبلت و موبایل']]),
  chain([['digital', 'کالای دیجیتال'], ['computer-parts', 'کامپیوتر و تجهیزات جانبی'], ['computer-devices', 'قطعات کامپیوتر'], ['graphics-card', 'کارت گرافیک']]),
  chain([['digital', 'کالای دیجیتال'], ['computer-parts', 'کامپیوتر و تجهیزات جانبی'], ['data-storage', 'تجهیزات ذخیره‌سازی اطلاعات'], ['external-ssd', 'حافظه SSD اکسترنال']]),
  chain([['digital', 'کالای دیجیتال'], ['computer-parts', 'کامپیوتر و تجهیزات جانبی'], ['data-storage', 'تجهیزات ذخیره‌سازی اطلاعات'], ['memory-card', 'کارت حافظه']]), // fabricated decoy
  // Vehicles (engine cover — fabricated, mirrors the phone-cover branch)
  chain([['vehicles-spare-parts', 'خودرو و موتورسیکلت'], ['car-parts', 'قطعات خودرو'], ['engine-cover', 'قاب و کاور موتور']]),
  // Home & kitchen
  chain([['home-and-kitchen', 'خانه و آشپزخانه'], ['home-kitchen-appliances', 'آشپزخانه'], ['kitchen-appliances', 'لوازم پخت و پز'], ['cooking-dishes', 'ظروف پخت و پز'], ['pots-pans', 'قابلمه و تابه']]),
  chain([['home-and-kitchen', 'خانه و آشپزخانه'], ['home-kitchen-appliances', 'آشپزخانه'], ['kitchen-appliances', 'لوازم پخت و پز'], ['cooking-tools', 'ابزار آشپزی'], ['other-cooking-utensils', 'سایر ابزار آشپزی'], ['in-pot', 'در قابلمه']]),
  chain([['rural-products', 'محصولات بومی و محلی'], ['rural-home', 'خانه و کاشانه'], ['rural-dishes', 'ظروف سنتی'], ['rural-pot-dig', 'دیگ و قابلمه سنتی']]),
  chain([['home-and-kitchen', 'خانه و آشپزخانه'], ['home-kitchen-appliances', 'آشپزخانه'], ['kitchen-textiles', 'منسوجات آشپزخانه'], ['pot-holders', 'زیر لیوانی، زیر بشقابی و زیر قابلمه ای'], ['panmate', 'زیر قابلمه‌ای و زیربشقابی']]),
  // Mother & child
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['baby-bottle', 'شیشه شیر، سرلاک و داروخوری']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['baby-bottle-accessory', 'ملزومات شیشه شیر']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['pacifier-and-accessories', 'پستانک و ملزومات']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['milking', 'شیردوش']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['protective-and-chest-pads', 'محافظ و پد سینه']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['feeding-pillow', 'بالش شیردهی']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['bottle-warmer-and-strilizer', 'استریل، گرم و خشک کننده شیشه شیر']]),
  chain([['mother-and-child', 'مادر و کودک'], ['personal-accessories', 'پستانک و لوازم شیردهی'], ['feeding-cloak', 'کاور شیردهی']]),
  // Pet (kitten bottle — fabricated, mirrors the baby bottle)
  chain([['pet', 'پت شاپ'], ['cat-supplies', 'لوازم گربه'], ['kitten-milk-bottle', 'شیشه شیر بچه گربه']]),
  // Coffee maker (fabricated; the joined قهوه‌ساز leaf feeds the vocab)
  chain([['home-appliance', 'لوازم خانگی برقی'], ['kitchen-electric', 'لوازم برقی آشپزخانه'], ['coffee-maker', 'قهوه‌ساز برقی']]),
);

const vocab = buildCompoundVocabulary(FIXTURE);
const slugs = (r: { top: { slug: string }[] }): string[] => r.top.map((t) => t.slug);

// --- Spec §9.1: normalization ---------------------------------------------

test('normalization: تیشرت / تی‌شرت / تی شرت tokenize identically', () => {
  assert.deepEqual(tokenize('تی‌شرت', vocab), ['تیشرت']);
  assert.deepEqual(tokenize('تیشرت', vocab), ['تیشرت']);
  assert.deepEqual(tokenize('تی شرت', vocab), ['تیشرت']);
  assert.equal(normalizePersian('تی‌شرت'), 'تیشرت');
});

test('normalization: Arabic ي/ك match Persian-titled leaves', () => {
  assert.deepEqual(tokenize('تيشرت زنانه', vocab), ['تیشرت', 'زنانه']);
  assert.deepEqual(tokenize('كفش ورزشی مردانه', vocab), ['کفش', 'ورزشی', 'مردانه']);
  const r = suggestCategories('كفش ورزشی مردانه', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'men-sport-shoes');
});

test('normalization: plural strip, latin case, persian digits', () => {
  assert.deepEqual(tokenize('یخچال‌های', vocab), ['یخچال']);
  assert.deepEqual(tokenize('یخچالها', vocab), ['یخچال']);
  assert.deepEqual(tokenize('SSD', vocab), ['ssd']);
  assert.deepEqual(tokenize('ssd', vocab), ['ssd']);
  assert.deepEqual(tokenize('۳G', vocab), ['3g']);
  assert.deepEqual(tokenize('کفش و جوراب', vocab), ['کفش', 'جوراب']); // و stopword dropped
});

// --- Spec §9.2: compound merge --------------------------------------------

test('compound merge: قهوه ساز matches a leaf titled قهوه‌ساز …', () => {
  assert.deepEqual(tokenize('قهوه ساز', vocab), tokenize('قهوه‌ساز', vocab));
  assert.deepEqual(tokenize('قهوه ساز', vocab), ['قهوهساز']);
  const r = suggestCategories('قهوه ساز برقی', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'coffee-maker');
});

// --- Spec §9.3: worked examples §7 -----------------------------------------

test('§7 #1: تیشرت زنانه auto-selects women-tee-shirts, score 17', () => {
  for (const title of ['تیشرت زنانه', 'تی‌شرت زنانه', 'تی شرت زنانه']) {
    const r = suggestCategories(title, FIXTURE);
    assert.equal(r.verdict, 'auto');
    assert.equal(r.top.length, 1);
    assert.equal(r.top[0].slug, 'women-tee-shirts');
    assert.equal(r.top[0].score, 17);
    assert.equal(r.top[0].pathDisplay, 'مد و پوشاک / زنانه / لباس زنانه / تی شرت زنانه');
  }
});

test('§7 #2: کفش ورزشی مردانه auto-selects men-sport-shoes, score 22', () => {
  const r = suggestCategories('کفش ورزشی مردانه', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'men-sport-shoes');
  assert.equal(r.top[0].score, 22);
});

test('§7 #3: ساعت هوشمند auto-selects smart-watch, score 19', () => {
  const r = suggestCategories('ساعت هوشمند', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'smart-watch');
  assert.equal(r.top[0].score, 19);
});

test('§7 #4: قاب گوشی سامسونگ auto-selects cell-phone-pouch-cover, score 13', () => {
  const r = suggestCategories('قاب گوشی سامسونگ', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'cell-phone-pouch-cover');
  assert.equal(r.top[0].score, 13);
});

test('§7 #5: کارت گرافیک RTX 4060 auto-selects graphics-card, score 13', () => {
  const r = suggestCategories('کارت گرافیک RTX 4060', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'graphics-card');
  assert.equal(r.top[0].score, 13);
});

test('§7 #6: شیشه شیر نوزاد is chips — top tie 11/11, third 9', () => {
  const r = suggestCategories('شیشه شیر نوزاد', FIXTURE);
  assert.equal(r.verdict, 'chips');
  assert.deepEqual(slugs(r), ['baby-bottle-accessory', 'baby-bottle', 'kitten-milk-bottle']);
  assert.deepEqual(r.top.map((t) => t.score), [11, 11, 9]);
});

test('§7 #7: قابلمه استیل is chips — 6 / 5 / 4', () => {
  const r = suggestCategories('قابلمه استیل', FIXTURE);
  assert.equal(r.verdict, 'chips');
  assert.deepEqual(slugs(r), ['pots-pans', 'in-pot', 'rural-pot-dig']);
  assert.deepEqual(r.top.map((t) => t.score), [6, 5, 4]);
});

test('§7 #8: کتانی مردانه is chips (never auto), three-way 13 tie', () => {
  const r = suggestCategories('کتانی مردانه', FIXTURE);
  assert.equal(r.verdict, 'chips');
  assert.deepEqual(slugs(r), ['casual-shoes-for-men', 'men-driving-shoes', 'men-formal-shoes']);
  assert.deepEqual(r.top.map((t) => t.score), [13, 13, 13]);
});

test('§7 #9: کفش مردانه is chips — four-way 14 tie, no plain leaf exists', () => {
  const r = suggestCategories('کفش مردانه', FIXTURE);
  assert.equal(r.verdict, 'chips');
  assert.deepEqual(slugs(r), ['casual-shoes-for-men', 'men-driving-shoes', 'men-formal-shoes']);
  assert.deepEqual(r.top.map((t) => t.score), [14, 14, 14]);
});

test('§7 #10: اشتراک اسپاتیفای is nothing', () => {
  const r = suggestCategories('اشتراک اسپاتیفای', FIXTURE);
  assert.equal(r.verdict, 'none');
  assert.deepEqual(r.top, []);
});

test('§7 #11: SSD اکسترنال auto-selects external-ssd, score 9', () => {
  const r = suggestCategories('SSD اکسترنال', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'external-ssd');
  assert.equal(r.top[0].score, 9);
});

test('§7 #12: هودی مردانه is chips — 14 / 13 / 10', () => {
  const r = suggestCategories('هودی مردانه', FIXTURE);
  assert.equal(r.verdict, 'chips');
  assert.deepEqual(slugs(r), ['men-print-hoodie', 'men-hoodies-collection', 'men-sport-sweatshirts-hoodies']);
  assert.deepEqual(r.top.map((t) => t.score), [14, 13, 10]);
});

// --- Spec §8: brand words cost nothing -------------------------------------

test('brand word in the title still auto-selects (تیشرت زنانه نایک, 15)', () => {
  const r = suggestCategories('تیشرت زنانه نایک', FIXTURE);
  assert.equal(r.verdict, 'auto');
  assert.equal(r.top[0].slug, 'women-tee-shirts');
  assert.equal(r.top[0].score, 15);
});

// --- Spec §9.4: thresholds --------------------------------------------------

test('thresholds: a 1-token title suggests nothing', () => {
  for (const title of ['کفش', 'یخچال', 'موبایل', 'تیشرت', '', '   ', '!!!']) {
    assert.equal(suggestCategories(title, FIXTURE).verdict, 'none', title);
  }
});

test('thresholds: a fabricated exact tie is chips, never auto', () => {
  const r = suggestCategories('ست آویز زنانه و مردانه', FIXTURE);
  assert.equal(r.verdict, 'chips');
  // Same tokens, different paths: tie broken by slug ascending.
  assert.deepEqual(slugs(r).slice(0, 2), ['uni-pendants-sets-men', 'uni-pendants-sets-women']);
  assert.deepEqual(r.top.slice(0, 2).map((t) => t.score), [23, 23]);
});

// --- Spec §9.5: synonyms ----------------------------------------------------

test('synonyms: قاب گوشی ranks cell-phone-pouch-cover above the motor cover', () => {
  const r = suggestCategories('قاب گوشی', FIXTURE);
  assert.equal(r.top[0].slug, 'cell-phone-pouch-cover');
  assert.equal(r.top[0].score, 13);
});

// --- Spec §9.6: no-match / chips-floor regression ---------------------------

test('no-match: بلیط هواپیما تهران مشهد is nothing (chips floor is 6, not 5)', () => {
  const r = suggestCategories('بلیط هواپیما تهران مشهد', FIXTURE);
  assert.equal(r.verdict, 'none');
  assert.deepEqual(r.top, []);
});

// --- Spec §9.7: determinism -------------------------------------------------

test('determinism: same input twice gives identical output', () => {
  const first = suggestCategories('شیشه شیر نوزاد', FIXTURE);
  const second = suggestCategories('شیشه شیر نوزاد', FIXTURE);
  assert.deepEqual(first, second);
  assert.deepEqual(suggestCategories('تیشرت زنانه', FIXTURE), suggestCategories('تیشرت زنانه', FIXTURE));
});

// --- Form-side rule: manual pick is never overridden (spec §6) --------------

test('decideAutoApply: empty selection auto-applies the winner', () => {
  const result = suggestCategories('تیشرت زنانه', FIXTURE);
  const d = decideAutoApply(result, { slug: null, autoApplied: false }, null, 'تیشرت زنانه');
  assert.equal(d.apply?.slug, 'women-tee-shirts');
  assert.equal(d.clearStaleAuto, false);
});

test('decideAutoApply: a manual pick is never overridden', () => {
  const result = suggestCategories('تیشرت زنانه', FIXTURE);
  const d = decideAutoApply(result, { slug: 'men-sport-shoes', autoApplied: false }, null, 'تیشرت زنانه');
  assert.equal(d.apply, null);
  assert.equal(d.clearStaleAuto, false);
});

test('decideAutoApply: an auto-applied pick is replaced when the title changes', () => {
  const result = suggestCategories('کفش ورزشی مردانه', FIXTURE);
  const d = decideAutoApply(result, { slug: 'women-tee-shirts', autoApplied: true }, null, 'کفش ورزشی مردانه');
  assert.equal(d.apply?.slug, 'men-sport-shoes');
  assert.equal(d.clearStaleAuto, false);
});

test('decideAutoApply: an auto-applied pick that still wins is kept', () => {
  const result = suggestCategories('تیشرت زنانه نایک', FIXTURE);
  const d = decideAutoApply(result, { slug: 'women-tee-shirts', autoApplied: true }, null, 'تیشرت زنانه نایک');
  assert.equal(d.apply, null);
  assert.equal(d.clearStaleAuto, false);
});

test('decideAutoApply: a dismissed auto suggestion is never re-applied', () => {
  const result = suggestCategories('تیشرت زنانه', FIXTURE);
  const d = decideAutoApply(result, { slug: null, autoApplied: false }, { title: 'تیشرت زنانه', slug: 'women-tee-shirts' }, 'تیشرت زنانه');
  assert.equal(d.apply, null);
  assert.equal(d.clearStaleAuto, false);
});

test('decideAutoApply: chips/none clears a stale auto pick, never a manual one', () => {
  const chips = suggestCategories('کفش مردانه', FIXTURE);
  assert.equal(chips.verdict, 'chips');
  assert.deepEqual(decideAutoApply(chips, { slug: 'women-tee-shirts', autoApplied: true }, null, 'کفش مردانه'), {
    apply: null,
    clearStaleAuto: true,
  });
  assert.deepEqual(decideAutoApply(chips, { slug: 'men-sport-shoes', autoApplied: false }, null, 'کفش مردانه'), {
    apply: null,
    clearStaleAuto: false,
  });
  const none = suggestCategories('اشتراک اسپاتیفای', FIXTURE);
  assert.deepEqual(decideAutoApply(none, { slug: 'women-tee-shirts', autoApplied: true }, null, 'اشتراک اسپاتیفای'), {
    apply: null,
    clearStaleAuto: true,
  });
});
