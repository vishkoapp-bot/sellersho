import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalizeTemplateSections, orderedEnabledSections, resolveTemplateConfig, resolveFashionMacroEntries } from './templateTypes';
import type { CategoryGroup } from './StoreSections';
import type { Product } from '../../../types';

const editorialOrder = ['announcement', 'hero', 'categoryRail', 'products', 'lookbook', 'about'];

test('saving the editorial fashion preset does not restore legacy bundled sections', () => {
  const saved = { sections: editorialOrder.map(key => ({ key, enabled: true, label: key, toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true })) };
  const resolved = resolveTemplateConfig('tpl-fashion-1', saved);
  assert.deepEqual(orderedEnabledSections(resolved), editorialOrder);
  assert.deepEqual(orderedEnabledSections(resolveTemplateConfig('tpl-fashion-1', resolved)), editorialOrder);
});

// B3: dict-form sections carry no order — they must resolve to
// blueprint order (hero before products before faq), never
// alphabetical, with aliases canonicalised into their blueprint slot.
test('dict-form sections resolve to blueprint order, never alphabetical', () => {
  const resolved = resolveTemplateConfig('tpl-modern-1', {
    sections: { faq: true, about: true, hero: true, products: true, categories: true, newsletter: false },
  });
  const enabled = orderedEnabledSections(resolved);
  assert.ok(enabled.indexOf('hero') < enabled.indexOf('faq'), 'hero before faq');
  assert.ok(enabled.indexOf('products') < enabled.indexOf('faq'), 'products before faq');
  assert.ok(enabled.indexOf('categoryRail') < enabled.indexOf('faq'), 'categories alias lands in the categoryRail blueprint slot');
  assert.ok(!enabled.includes('newsletter'), 'explicit false stays disabled');
  // Additional sections render before Modern's fixed end-of-page sections.
  const withUnknown = resolveTemplateConfig('tpl-modern-1', {
    sections: { faq: true, futureSection: true, hero: true },
  });
  const enabledUnknown = orderedEnabledSections(withUnknown);
  assert.ok(enabledUnknown.indexOf('futureSection') < enabledUnknown.indexOf('faq'));
});

test('Modern restores the shopping order and keeps saved promotional sections together', () => {
  const resolved = resolveTemplateConfig('tpl-modern-1', {
    sections: [
      { key: 'faq', enabled: true, label: 'faq', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
      { key: 'deals', enabled: true, label: 'deals', toggleable: true, orderable: true, defaultEnabled: false, selfHidesWhenEmpty: true },
      { key: 'hero', enabled: true, label: 'hero', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
      { key: 'products', enabled: false, label: 'products', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
      { key: 'allProducts', enabled: true, label: 'allProducts', toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true },
    ],
  });
  const enabled = orderedEnabledSections(resolved);
  assert.deepEqual(enabled.slice(0, 6), ['announcement', 'hero', 'categoryRail', 'allProducts', 'deals', 'featured']);
  assert.deepEqual(enabled.slice(-4), ['trustStrip', 'about', 'testimonials', 'faq']);
  assert.ok(Array.isArray(resolved.sections) && resolved.sections.slice(-2).every(section => !section.orderable));
});

// M-4 (Batch 3 fix): pin ALL FIVE blueprint orders on the frontend
// side too — previously only tpl-modern-1 was pinned, so a typo in
// any other blueprint (or a frontend-only edit the backend silently
// overrides for dict-form stores) shipped unnoticed. A dict
// carrying every blueprint key must resolve to exactly blueprint
// order. These lists must match the backend mirror table in
// team05_store_handlers.go (team05TemplateBlueprintSections).
const allBlueprints: Record<string, string[]> = {
  'tpl-modern-1': ['announcement', 'hero', 'categoryRail', 'products', 'featured', 'banners', 'bestsellers', 'trustStrip', 'about', 'testimonials', 'faq'],
  'tpl-electronic-1': ['announcement', 'hero', 'categoryRail', 'deals', 'timedDeals', 'banners', 'bestsellers', 'brands', 'discounted', 'testimonials', 'faq', 'products'],
  'tpl-fashion-1': ['announcement', 'hero', 'categoryRail', 'products', 'lookbook', 'about'],
  'tpl-digikala-1': ['announcement', 'hero', 'categoryRail', 'deals', 'timedDeals', 'banners', 'brands', 'bestsellers', 'discounted', 'topRated', 'campaign', 'faq', 'products'],
  'tpl-tech-1': ['announcement', 'hero', 'trustStrip', 'categoryRail', 'featured', 'brands', 'topRated', 'about', 'faq', 'products'],
};

test('dict-form sections resolve to blueprint order for all five templates', () => {
  for (const [templateId, blueprint] of Object.entries(allBlueprints)) {
    const dict: Record<string, boolean> = {};
    // Scrambled input: reverse blueprint order.
    for (let i = blueprint.length - 1; i >= 0; i--) dict[blueprint[i]] = true;
    dict.newsletter = true;
    const enabled = orderedEnabledSections(resolveTemplateConfig(templateId, { sections: dict }));
    assert.deepEqual(enabled, blueprint, `${templateId} order`);
    const legacy = { key: 'newsletter', label: 'Newsletter', enabled: true, toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true };
    const resolved = resolveTemplateConfig(templateId, { sections: [legacy] });
    assert.ok(Array.isArray(resolved.sections) && resolved.sections.every(section => section.key !== 'newsletter'));
  }
});

// B3: normalizeTemplateSections orders a dict by the given blueprint
// and canonicalises aliases before ordering.
test('normalizeTemplateSections orders dict keys by blueprint with alias canonicalisation', () => {
  const out = normalizeTemplateSections(
    { products: true, categories: true, faq: true, hero: true },
    ['announcement', 'hero', 'categoryRail', 'faq', 'products'],
  )!;
  assert.deepEqual(out.map((s) => s.key), ['hero', 'categoryRail', 'faq', 'products']);
});

test('explicit seller sections and disabled flags survive fashion preset resolution', () => {
  const resolved = resolveTemplateConfig('tpl-fashion-1', {
    sections: { hero: true, collections: true, lookbook: false, products: true },
    custom: { heroImage: '/seller-banner.webp' },
  });
  const enabled = orderedEnabledSections(resolved);
  assert(enabled.includes('collections'));
  assert(!enabled.includes('lookbook'));
  assert(!enabled.includes('categoryGrid'));
  assert.equal(resolved.custom?.heroImage, '/seller-banner.webp');
});

// B4: the macro row used to take its tile images from the BANNERS by
// position (tile 0 = banner A, tile 1 = banner B), so the category
// tiles showed fashion photos instead of their own categories. A tile
// must show its CATEGORY's image (or its first product's photo) —
// never a banner image by index. (The macro component itself cannot
// load under tsx --test — FashionTemplate.tsx imports fashion.css —
// so the extracted pure function is pinned here; the component maps
// entries to <img src> one-to-one.)
const macroGroup = (slug: string, title: string, image: string, productImage?: string): CategoryGroup => ({
  slug, title, image, gradient: '', count: 1,
  products: productImage ? [{ image: productImage } as unknown as Product] : [],
});
const macroBanners = [
  { image: 'fashion-photo-A.jpg', title: 'Banner A' },
  { image: 'fashion-photo-B.jpg', title: 'Banner B' },
];

test('fashion macro tiles show their own category image, never a banner image by position', () => {
  const entries = resolveFashionMacroEntries(
    [macroGroup('digital', 'دیجیتال', 'digital-cat.jpg'), macroGroup('appliances', 'لوازم خانگی', 'appliances-cat.jpg')],
    macroBanners, 2,
  );
  assert.deepEqual(entries.map((e) => e.image), ['digital-cat.jpg', 'appliances-cat.jpg']);
  assert.ok(!entries.some((e) => (e.image || '').includes('fashion-photo')));
});

test('fashion macro falls back to the first product photo when the category has no image', () => {
  const entries = resolveFashionMacroEntries(
    [macroGroup('digital', 'دیجیتال', '', 'digital-prod.jpg'), macroGroup('appliances', 'لوازم خانگی', 'appliances-cat.jpg')],
    macroBanners, 2,
  );
  assert.deepEqual(entries.map((e) => e.image), ['digital-prod.jpg', 'appliances-cat.jpg']);
});
