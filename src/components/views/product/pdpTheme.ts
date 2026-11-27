import type { Store } from '../../../types';
import type { TemplateConfig } from '../../storefront/templates/templateTypes';
import { resolveTemplateConfig } from '../../storefront/templates/templateTypes';
import { isKnownTemplateId, resolveTemplateAccent } from '../../storefront/templates/TemplateRegistry';

export interface PdpTheme {
  templateId: string | null;
  accent: string;
}

const DEFAULT_PDP_THEME: PdpTheme = { templateId: null, accent: '#5d44f8' };

export function resolvePdpTheme(store?: Store | null): PdpTheme {
  if (!store || store.ownerType !== 'dropshipper') return DEFAULT_PDP_THEME;
  const templateId = store.templateId || 'tpl-modern-1';
  if (!isKnownTemplateId(templateId)) return DEFAULT_PDP_THEME;
  const config = resolveTemplateConfig(templateId, (store.templateConfig || {}) as TemplateConfig);
  const accent = resolveTemplateAccent(templateId, config.accentColor);
  return { templateId, accent };
}
