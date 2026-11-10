import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import { ModernTemplate } from './ModernTemplate';

// Team 08 live parity (D04): template registry. Live mapping (H4):
// tpl-digikala-1 / tpl-fashion-1 / tpl-electronic-1 / tpl-tech-1 -> lazy
// chunks; tpl-modern-1 -> component inside the main bundle (Cv). This
// registry reproduces exactly that split: ModernTemplate is statically
// imported (main bundle); the other four are React.lazy chunks.
// Accent defaults are the live Ue map. tpl-tech-1 has NO accent in the
// recovered Ue evidence, so it has no entry here and falls back to the
// templateConfig accent or the platform theme colour; inventing a tech
// accent would be a guess (TODO below).
// Template thumbs: live dist has /templates/*-thumb.svg files, but their
// names never appear in live JS (U44), so thumbnails are referenced only
// from the server-provided thumbnailUrl, never constructed here.
export const STORE_TEMPLATE_IDS = [
  'tpl-modern-1',
  'tpl-tech-1',
  'tpl-electronic-1',
  'tpl-fashion-1',
  'tpl-digikala-1',
] as const;

export type StoreTemplateId = (typeof STORE_TEMPLATE_IDS)[number];

export const TEMPLATE_ACCENTS: Partial<Record<StoreTemplateId, string>> = {
  'tpl-electronic-1': '#f97847',
  'tpl-fashion-1': '#793f3c',
  'tpl-digikala-1': '#ef4056',
  'tpl-modern-1': '#0D7377',
  // TODO: tpl-tech-1 accent is not in the recovered live Ue map; do not
  // invent one. Fallback is templateConfig.accentColor, then #5d44f8.
};

export function resolveTemplateAccent(templateId: string, configAccent?: string): string {
  if (configAccent) return configAccent;
  const accent = (TEMPLATE_ACCENTS as Record<string, string | undefined>)[templateId];
  return accent || '#5d44f8';
}

const DigikalaTemplate = React.lazy(() => import('./DigikalaTemplate'));
const FashionTemplate = React.lazy(() => import('./FashionTemplate'));
const ElectronicTemplate = React.lazy(() => import('./ElectronicTemplate'));
const TechTemplate = React.lazy(() => import('./TechTemplate'));

export function isKnownTemplateId(value: string): value is StoreTemplateId {
  return (STORE_TEMPLATE_IDS as readonly string[]).includes(value);
}

// Unknown/invalid template: live renders a status page with the same
// frame (component Q4 in live JS), not a crash and not a silent fallback
// to another template.
export const TemplateFallback: React.FC<{ templateId: string }> = ({ templateId }) => (
  <div className="flex-1 flex flex-col items-center justify-center p-6 text-center bg-slate-50 min-h-[480px] space-y-3">
    <h2 className="text-sm font-bold text-slate-900">قالب ویترین نامعتبر است</h2>
    <p className="text-xs text-slate-500 leading-6 max-w-[280px]">
      قالب «{templateId}» شناخته نشد. لطفاً از پنل فروشنده یک قالب معتبر انتخاب کنید.
    </p>
  </div>
);

export const StorefrontTemplate: React.FC<
  StorefrontTemplateProps & { templateId: string }
> = ({ templateId, ...props }) => {
  if (!isKnownTemplateId(templateId)) {
    return <TemplateFallback templateId={templateId} />;
  }
  const body =
    templateId === 'tpl-modern-1' ? (
      <ModernTemplate {...props} />
    ) : templateId === 'tpl-digikala-1' ? (
      <DigikalaTemplate {...props} />
    ) : templateId === 'tpl-fashion-1' ? (
      <FashionTemplate {...props} />
    ) : templateId === 'tpl-electronic-1' ? (
      <ElectronicTemplate {...props} />
    ) : (
      <TechTemplate {...props} />
    );
  if (templateId === 'tpl-modern-1') return body;
  return (
    <React.Suspense fallback={<div className="p-6 text-center text-xs text-slate-500">در حال بارگذاری قالب ویترین…</div>}>
      {body}
    </React.Suspense>
  );
};
