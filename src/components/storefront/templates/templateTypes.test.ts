import { test } from 'node:test';
import assert from 'node:assert/strict';
import { orderedEnabledSections, resolveTemplateConfig } from './templateTypes';

const editorialOrder = ['announcement', 'hero', 'categoryRail', 'products', 'lookbook', 'about'];

test('saving the editorial fashion preset does not restore legacy bundled sections', () => {
  const saved = { sections: editorialOrder.map(key => ({ key, enabled: true, label: key, toggleable: true, orderable: true, defaultEnabled: true, selfHidesWhenEmpty: true })) };
  const resolved = resolveTemplateConfig('tpl-fashion-1', saved);
  assert.deepEqual(orderedEnabledSections(resolved), editorialOrder);
  assert.deepEqual(orderedEnabledSections(resolveTemplateConfig('tpl-fashion-1', resolved)), editorialOrder);
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
