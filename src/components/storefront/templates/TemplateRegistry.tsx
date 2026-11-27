import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';

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

export const TEMPLATE_ACCENTS: Record<StoreTemplateId, string> = {
  'tpl-electronic-1': '#9e3d15',
  'tpl-fashion-1': '#703738',
  'tpl-digikala-1': '#9f313d',
  'tpl-modern-1': '#365849',
  'tpl-tech-1': '#1b5673',
};

export function resolveTemplateAccent(templateId: string, configAccent?: string): string {
  if (configAccent) return configAccent;
  const accent = (TEMPLATE_ACCENTS as Record<string, string | undefined>)[templateId];
  return accent || '#5d44f8';
}

const ModernTemplate = React.lazy(() => import('./ModernTemplate').then(module => ({ default: module.ModernTemplate })));
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

function StorefrontAnchor() {
  React.useEffect(() => {
    const anchor = window.location.hash;
    if (['#products', '#fashion-search', '#fashion-contact', '#fashion-categories', '#about', '#marketplace-deals'].includes(anchor)) {
      document.getElementById(anchor.slice(1))?.scrollIntoView({ block: 'start' });
    }
  }, []);
  return null;
}

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
  return (
    <React.Suspense fallback={<div className="p-6 text-center text-xs text-slate-500">در حال بارگذاری قالب ویترین…</div>}>
      {body}
      <StorefrontAnchor />
    </React.Suspense>
  );
};
