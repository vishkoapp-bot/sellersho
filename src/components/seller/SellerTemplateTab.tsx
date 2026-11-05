import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi, StoreTemplate, SellerStoreTemplate } from '../../services/storeApi';
import { TEMPLATE_ACCENTS, resolveTemplateAccent } from '../storefront/templates/TemplateRegistry';
import type { TemplateConfig } from '../storefront/templates/templateTypes';

// Live parity (seller "ظاهر فروشگاه" tab, template card): live's template
// switch applies immediately after a confirmation explaining which settings
// are kept, the colour card falls back to the template's own accent, and
// publish uses live's toasts. Template data comes from GET /store-templates
// (live bundle calls getStoreTemplates) and the saved template from
// GET /seller/store/template; save/publish use the candidate's existing
// update/publish endpoints for that saved template.
export const SellerTemplateTab: React.FC = () => {
  const { currentStore } = useStore();
  const [templates, setTemplates] = useState<StoreTemplate[]>([]);
  const [current, setCurrent] = useState<SellerStoreTemplate | null>(null);
  const [selectedId, setSelectedId] = useState<string>('tpl-modern-1');
  const [accent, setAccent] = useState<string>('');
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [applying, setApplying] = useState<boolean>(false);

  const load = () => {
    setLoading(true);
    setError(null);
    storeApi.getStoreTemplates()
      .then((res) => {
        const list = res.templates || [];
        setTemplates(list);
        storeApi.getSellerStoreTemplate(currentStore.slug)
          .then((saved) => {
            setCurrent(saved);
            if (saved.templateId) {
              setSelectedId(saved.templateId);
              const cfg = (saved.templateConfig || {}) as TemplateConfig;
              if (cfg.accentColor) setAccent(cfg.accentColor);
            } else if (list.length > 0) {
              // Live default: the first template of the list when none saved.
              setSelectedId(list[0].id);
            }
          })
          .catch(() => {
            // No saved template yet: keep the live default below.
            if (list.length > 0) setSelectedId(list[0].id);
          })
          .finally(() => setLoading(false));
      })
      .catch((err: any) => {
        setLoading(false);
        setError(err?.message || 'بارگذاری تنظیمات ظاهر فروشگاه انجام نشد.');
      });
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentStore.slug]);

  const effectiveAccent = accent || resolveTemplateAccent(selectedId);
  const shownTemplateId = current?.templateId || selectedId;
  const currentName = templates.find((t) => t.id === shownTemplateId)?.name || shownTemplateId;

  const handleSave = async (publish: boolean) => {
    setNotice(null);
    setError(null);
    try {
      const config: TemplateConfig = { accentColor: effectiveAccent };
      await storeApi.updateSellerStoreTemplate({ templateId: selectedId, templateConfig: config as unknown as Record<string, unknown> });
      if (publish) {
        await storeApi.publishSellerStoreTemplate({ slug: currentStore.slug, templateId: selectedId, templateConfig: config as unknown as Record<string, unknown> });
        setNotice('ظاهر فروشگاه منتشر شد.');
      } else {
        setNotice('تغییرات ذخیره‌نشده دارید — برای نمایش در ویترین، آن‌ها را منتشر کنید.');
      }
    } catch (err: any) {
      if (publish) {
        setError('انتشار انجام نشد. لطفاً دوباره تلاش کنید.');
      } else {
        setError(err?.message || 'ذخیره قالب انجام نشد.');
      }
    }
  };

  const handleSelectTemplate = (id: string) => {
    if (id === selectedId) return;
    // Live: changing the template applies immediately, after this confirm.
    setConfirmId(id);
  };

  const handleConfirmTemplateChange = async () => {
    if (!confirmId) return;
    setApplying(true);
    setNotice(null);
    setError(null);
    try {
      const config: TemplateConfig = { accentColor: effectiveAccent };
      await storeApi.updateSellerStoreTemplate({ templateId: confirmId, templateConfig: config as unknown as Record<string, unknown> });
      setSelectedId(confirmId);
      setCurrent((prev) => (prev ? { ...prev, templateId: confirmId } : prev));
      setNotice('قالب فروشگاه تغییر کرد.');
    } catch {
      setError('تغییر قالب انجام نشد.');
    } finally {
      setApplying(false);
      setConfirmId(null);
    }
  };

  const handleResetAccent = () => {
    setAccent('');
    setNotice('به رنگ اصلی قالب برگشت.');
  };

  return (
    <div className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4">
      <h2 className="text-sm font-bold text-slate-900">قالب فروشگاه</h2>
      <p className="text-xs leading-6 text-slate-500">
        هر تغییری که این‌جا بدهی، پایین صفحه در پیش‌نمایش می‌بینی. هر بخش دکمه «ذخیره و انتشار» خودش را دارد؛ با زدن آن، تغییراتت در فروشگاه دیده می‌شود. تغییرات ذخیره‌نشده هم خودکار نگه داشته می‌شوند و با رفرش صفحه از دست نمی‌روند.
      </p>
      <p className="text-xs leading-6 text-slate-500">
        تعویض قالب بلافاصله اعمال می‌شود و تنظیمات ظاهری شما حفظ می‌ماند.
      </p>
      <p className="text-xs leading-6 text-slate-500">
        قالب فعلی: <span className="font-bold text-slate-800">{currentName}</span>
      </p>
      {loading && <p className="rounded-xl bg-slate-50 p-3 text-xs text-slate-500">در حال بارگذاری تنظیمات ظاهر فروشگاه…</p>}
      {error && <p role="alert" className="rounded-xl bg-rose-50 border border-rose-200 p-3 text-xs text-rose-700">{error}</p>}
      {error && !loading && (
        <button type="button" onClick={load} className="rounded-xl bg-slate-900 px-3 py-2 text-xs font-bold text-white">تلاش مجدد</button>
      )}
      {notice && <p role="status" className="rounded-xl bg-emerald-50 border border-emerald-200 p-3 text-xs text-emerald-700">{notice}</p>}
      {!loading && !error && templates.length === 0 && (
        <p className="rounded-xl border border-dashed p-5 text-xs leading-6 text-slate-500">قالبی برای نمایش وجود ندارد.</p>
      )}
      {!loading && templates.length > 0 && (
        <div className="grid grid-cols-2 gap-2">
          {templates.map((meta) => {
            const id = meta.id;
            const preset = TEMPLATE_ACCENTS[id as keyof typeof TEMPLATE_ACCENTS];
            return (
              <button
                key={id}
                type="button"
                onClick={() => handleSelectTemplate(id)}
                aria-pressed={selectedId === id}
                className={`rounded-2xl border p-3 text-right ${selectedId === id ? 'border-blue-600 bg-blue-50' : 'border-slate-200'}`}
              >
                <span className="block text-xs font-bold text-slate-900">{meta.name || id}</span>
                <span className="mt-1 block text-[11px] text-slate-500" dir="ltr">{id}</span>
                {meta.thumbnailUrl ? <img src={meta.thumbnailUrl} alt="" loading="lazy" onError={(e) => { e.currentTarget.style.display = 'none'; }} className="mt-2 h-16 w-full rounded-xl object-cover bg-slate-100" /> : null}
                {preset ? (
                  <span className="mt-2 inline-block h-4 w-4 rounded-full border border-slate-200" style={{ backgroundColor: preset }} />
                ) : null}
              </button>
            );
          })}
        </div>
      )}
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <label className="text-xs font-bold text-slate-700">رنگ فروشگاه:</label>
          <input
            type="color"
            value={/^#[0-9a-fA-F]{6}$/.test(effectiveAccent) ? effectiveAccent : '#5d44f8'}
            onChange={(e) => setAccent(e.target.value)}
            className="h-8 w-12 rounded-lg border border-slate-200 bg-white p-1"
          />
          <span className="text-[11px] text-slate-500" dir="ltr">{effectiveAccent}</span>
          <button type="button" onClick={handleResetAccent} className="mr-auto text-[11px] font-bold text-blue-700">بازگشت به رنگ پیش‌فرض</button>
        </div>
        <p className="text-[11px] leading-5 text-slate-500">
          {accent ? 'رنگ اصلی فروشگاه شما؛ پیش‌فرض، رنگ اصلی قالب است.' : 'رنگ فعلی، رنگ اصلی قالب است.'}
        </p>
      </div>
      <div className="flex gap-2">
        <button type="button" onClick={() => handleSave(false)} className="flex-1 rounded-xl bg-slate-900 py-2.5 text-xs font-bold text-white">ذخیره قالب</button>
        <button type="button" onClick={() => handleSave(true)} className="flex-1 rounded-xl bg-blue-600 py-2.5 text-xs font-bold text-white">انتشار قالب</button>
      </div>
      {confirmId && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
            <h4 className="font-bold text-slate-900">تغییر قالب</h4>
            <p className="leading-6 text-slate-600">
              تنظیمات فعلی‌ات که برای قالب جدید هم کاربرد دارد (مثل رنگ، بنر و متن‌ها) با قالب جدید ذخیره می‌شود؛ فقط تنظیماتی که قالب جدید ندارد کنار گذاشته می‌شوند. ادامه می‌دهید؟
            </p>
            <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
              <button
                type="button"
                onClick={() => setConfirmId(null)}
                className="py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
              >
                انصراف
              </button>
              <button
                type="button"
                onClick={handleConfirmTemplateChange}
                disabled={applying}
                className="py-2.5 bg-blue-600 hover:bg-blue-700 text-white font-bold text-xs rounded-xl disabled:opacity-50"
              >
                {applying ? 'در حال اعمال…' : 'تغییر قالب'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
