import React, { useEffect, useRef, useState } from 'react';
import { Monitor, Smartphone } from 'lucide-react';
import { StorefrontTemplate } from '../storefront/templates/TemplateRegistry';
import type { StorefrontTemplateProps } from '../storefront/templates/templateTypes';

// Live device emulator for the «ظاهر فروشگاه» editor: renders the real
// StorefrontTemplate with the seller's (possibly unsaved) draft config, in
// a phone frame (390px, scrollable) or on a desktop stage (1280px wide,
// scaled down to the available width with a CSS transform).

export interface TemplatePreviewEmulatorProps extends StorefrontTemplateProps {
  templateId: string;
}

const DESKTOP_STAGE_WIDTH = 1280;
const DESKTOP_STAGE_HEIGHT = 800;

export const TemplatePreviewEmulator: React.FC<TemplatePreviewEmulatorProps> = ({
  templateId,
  store,
  products,
  accentColor,
  config,
  categories,
}) => {
  const [device, setDevice] = useState<'mobile' | 'desktop'>('mobile');
  const stageWrapRef = useRef<HTMLDivElement | null>(null);
  const [stageWrapWidth, setStageWrapWidth] = useState(0);

  useEffect(() => {
    const el = stageWrapRef.current;
    if (!el) return;
    const update = () => setStageWrapWidth(el.clientWidth);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, [device]);

  const desktopScale = stageWrapWidth > 0 ? stageWrapWidth / DESKTOP_STAGE_WIDTH : 0;

  const body = (
    <StorefrontTemplate
      templateId={templateId}
      store={store}
      products={products}
      accentColor={accentColor}
      config={config}
      categories={categories}
    />
  );

  return (
    <div className="space-y-3 rounded-2xl border border-slate-200 bg-white p-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-bold text-slate-900">پیش‌نمایش زنده</h3>
        <div className="flex rounded-xl bg-slate-100 p-1" role="group" aria-label="نوع دستگاه پیش‌نمایش">
          <button
            type="button"
            onClick={() => setDevice('mobile')}
            aria-pressed={device === 'mobile'}
            className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-[11px] font-bold ${
              device === 'mobile' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'
            }`}
          >
            <Smartphone className="h-3.5 w-3.5" strokeWidth={1.5} />
            موبایل
          </button>
          <button
            type="button"
            onClick={() => setDevice('desktop')}
            aria-pressed={device === 'desktop'}
            className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-[11px] font-bold ${
              device === 'desktop' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500'
            }`}
          >
            <Monitor className="h-3.5 w-3.5" strokeWidth={1.5} />
            دسکتاپ
          </button>
        </div>
      </div>

      {device === 'mobile' ? (
        <div className="relative mx-auto w-[390px] max-w-full overflow-hidden rounded-[2rem] border-[10px] border-slate-900 bg-white shadow-xl">
          <div className="absolute left-1/2 top-2 z-20 h-4 w-24 -translate-x-1/2 rounded-full bg-slate-900" aria-hidden="true" />
          <div className="h-[720px] overflow-y-auto" dir="rtl">
            {body}
          </div>
        </div>
      ) : (
        <div
          ref={stageWrapRef}
          dir="ltr"
          className="relative w-full overflow-hidden rounded-xl border border-slate-200 bg-slate-50"
          style={{ height: desktopScale > 0 ? DESKTOP_STAGE_HEIGHT * desktopScale : 320 }}
        >
          {desktopScale > 0 && (
            <div
              className="absolute left-0 top-0"
              style={{
                width: DESKTOP_STAGE_WIDTH,
                height: DESKTOP_STAGE_HEIGHT,
                transform: `scale(${desktopScale})`,
                transformOrigin: 'top left',
              }}
            >
              <div dir="rtl" className="h-full w-full overflow-hidden bg-white">
                {body}
              </div>
            </div>
          )}
        </div>
      )}

      <p className="text-center text-[11px] leading-5 text-slate-400">
        پیش‌نمایش زنده — تغییرات ذخیره‌نشده هم این‌جا دیده می‌شود
      </p>
    </div>
  );
};
