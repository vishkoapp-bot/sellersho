import type { Store } from '../../../types';
import type { TemplateConfig } from '../../storefront/templates/templateTypes';
import { resolveTemplateConfig } from '../../storefront/templates/templateTypes';
import { resolveTemplateAccent } from '../../storefront/templates/TemplateRegistry';

// PDP template bridge: the product page is one shared component, but
// inside a dropshipper storefront it must wear that store's template
// identity instead of the central store's design language. Resolution
// mirrors SellerStorefrontView exactly: the store's templateId
// (absent = tpl-modern-1, the live default), the saved config merged
// under the template blueprint via resolveTemplateConfig, and the
// accent via resolveTemplateAccent (config accent -> template accent
// -> platform violet). The PDP root republishes the accent as the
// --tpl-accent custom property, the same variable template roots set.
//
// The descriptor table below is the routing: it maps a templateId to
// the PDP treatment that template has earned. Only tpl-fashion-1 has
// one today - the Aastin editorial token set (page #faf8f5, ink
// #1c1c1a, muted #737373, line #e5e0d8, band #3a4441, square corners),
// switched on via `fashion` in the PDP components. Every other
// template resolves to the default descriptor, so its PDP keeps
// rendering the platform design exactly until that template gets
// its own PDP pass.
export interface PdpTheme {
  templateId: string | null;
  fashion: boolean;
  accent: string;
}

const DEFAULT_PDP_THEME: PdpTheme = { templateId: null, fashion: false, accent: '#5d44f8' };

const PDP_TEMPLATE_DESCRIPTORS: Record<string, { fashion: boolean }> = {
  'tpl-fashion-1': { fashion: true },
  'tpl-modern-1': { fashion: false },
  'tpl-electronic-1': { fashion: false },
  'tpl-digikala-1': { fashion: false },
  'tpl-tech-1': { fashion: false },
};

export function resolvePdpTheme(store?: Store | null): PdpTheme {
  if (!store || store.ownerType !== 'dropshipper') return DEFAULT_PDP_THEME;
  const templateId = store.templateId || 'tpl-modern-1';
  const config = resolveTemplateConfig(templateId, (store.templateConfig || {}) as TemplateConfig);
  const accent = resolveTemplateAccent(templateId, config.accentColor);
  const descriptor = PDP_TEMPLATE_DESCRIPTORS[templateId];
  return { templateId, fashion: descriptor ? descriptor.fashion : false, accent };
}
