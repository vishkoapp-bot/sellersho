import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi, StoreTemplate, SellerStoreTemplate } from '../../services/storeApi';
import { catalogApi } from '../../services/catalogApi';
import { mapBackendProduct } from '../../context/ProductContext';
import { TEMPLATE_ACCENTS, resolveTemplateAccent } from '../storefront/templates/TemplateRegistry';
import {
  blueprintFor,
  canonicalSectionKey,
  resolveTemplateConfig,
  sectionKey,
} from '../storefront/templates/templateTypes';
import type {
  TemplateBanner,
  TemplateConfig,
  TemplateCustom,
  TemplateFaqItem,
  TemplateHeroSlide,
  TemplateSection,
  TemplateTestimonial,
  TemplateTrustItem,
} from '../storefront/templates/templateTypes';
import { TemplatePreviewEmulator } from './TemplatePreviewEmulator';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { ArrowDown, ArrowUp, ChevronDown, ChevronUp, LayoutTemplate, Plus, Trash2 } from 'lucide-react';

// «ظاهر فروشگاه» — professional template editor. The seller edits a DRAFT
// config (sections order/visibility + every section's content) and sees it
// live in the device emulator; nothing reaches the storefront until
// «ذخیره و انتشار». The draft autosaves to localStorage per store+template
// and is restored on load; publishing clears it. Template switching still
// applies immediately (persisted via updateSellerStoreTemplate): the
// seller's saved content (custom) and the current effective accent carry
// over, and the new template's blueprint supplies the section lineup.

type CustomBag = TemplateCustom & Record<string, unknown>;

const draftStorageKey = (slug: string, templateId: string) => `tpl-draft:${slug}:${templateId}`;

function readStoredDraft(slug: string, templateId: string): TemplateConfig | null {
  try {
    const raw = window.localStorage.getItem(draftStorageKey(slug, templateId));
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as TemplateConfig) : null;
  } catch {
    return null;
  }
}

function clearStoredDraft(slug: string, templateId: string) {
  try {
    window.localStorage.removeItem(draftStorageKey(slug, templateId));
  } catch {
    // storage unavailable: the in-memory draft still works.
  }
}

const str = (value: unknown): string => (typeof value === 'string' ? value : '');

// ---------------------------------------------------------------------------
// Small form primitives (module level so inputs never lose focus on render).
// ---------------------------------------------------------------------------

const labelCls = 'block text-[11px] font-bold text-slate-600 mb-1';
const inputCls =
  'w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs text-slate-800 placeholder-slate-400 focus:border-blue-500 focus:outline-none';

const TextField: React.FC<{
  label: string;
  value: string;
  onChange: (value: string) => void;
  ltr?: boolean;
}> = ({ label, value, onChange, ltr }) => (
  <div>
    <label className={labelCls}>{label}</label>
    <input
      type="text"
      value={value}
      dir={ltr ? 'ltr' : undefined}
      onChange={(e) => onChange(e.target.value)}
      className={inputCls}
    />
  </div>
);

const TextAreaField: React.FC<{
  label: string;
  value: string;
  onChange: (value: string) => void;
}> = ({ label, value, onChange }) => (
  <div>
    <label className={labelCls}>{label}</label>
    <textarea
      value={value}
      rows={3}
      onChange={(e) => onChange(e.target.value)}
      className={`${inputCls} leading-6 resize-y`}
    />
  </div>
);

const NumberField: React.FC<{
  label: string;
  value: number | undefined;
  min?: number;
  max?: number;
  onChange: (value: number | undefined) => void;
}> = ({ label, value, min, max, onChange }) => (
  <div>
    <label className={labelCls}>{label}</label>
    <input
      type="number"
      value={typeof value === 'number' ? value : ''}
      min={min}
      max={max}
      dir="ltr"
      onChange={(e) => {
        const raw = e.target.value;
        if (raw === '') {
          onChange(undefined);
          return;
        }
        const n = Number(raw);
        if (!Number.isFinite(n)) return;
        let next = Math.floor(n);
        if (typeof min === 'number') next = Math.max(min, next);
        if (typeof max === 'number') next = Math.min(max, next);
        onChange(next);
      }}
      className={inputCls}
    />
  </div>
);

const ToggleField: React.FC<{
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}> = ({ label, checked, onChange }) => (
  <div className="flex items-center justify-between gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2">
    <span className="text-[11px] font-bold text-slate-600">{label}</span>
    <Switch checked={checked} onChange={onChange} ariaLabel={label} />
  </div>
);

export const Switch: React.FC<{
  checked: boolean;
  onChange: (value: boolean) => void;
  ariaLabel?: string;
}> = ({ checked, onChange, ariaLabel }) => (
  <button
    type="button"
    role="switch"
    aria-checked={checked}
    aria-label={ariaLabel}
    onClick={() => onChange(!checked)}
    dir="ltr"
    className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${checked ? 'bg-[#5d44f8]' : 'bg-slate-300'}`}
  >
    <span
      className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${checked ? 'left-[22px]' : 'left-0.5'}`}
    />
  </button>
);

function ListEditor<T>(props: {
  items: T[];
  onChange: (items: T[]) => void;
  newItem: () => T;
  addLabel: string;
  emptyText: string;
  renderFields: (item: T, update: (patch: Partial<T>) => void) => React.ReactNode;
}) {
  const { items, onChange, newItem, addLabel, emptyText, renderFields } = props;
  return (
    <div className="space-y-2">
      {items.length === 0 && (
        <p className="rounded-xl border border-dashed border-slate-200 bg-white p-3 text-[11px] leading-5 text-slate-500">
          {emptyText}
        </p>
      )}
      {items.map((item, i) => (
        <div key={i} className="space-y-2 rounded-xl border border-slate-200 bg-white p-3">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-bold text-slate-500">مورد {i + 1}</span>
            <button
              type="button"
              onClick={() => onChange(items.filter((_, j) => j !== i))}
              className="flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] font-bold text-rose-600 hover:bg-rose-50"
            >
              <Trash2 className="h-3.5 w-3.5" strokeWidth={1.5} />
              حذف
            </button>
          </div>
          {renderFields(item, (patch) =>
            onChange(items.map((it, j) => (j === i ? { ...it, ...patch } : it))),
          )}
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...items, newItem()])}
        className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-slate-300 bg-white py-2 text-[11px] font-bold text-blue-700 hover:bg-blue-50"
      >
        <Plus className="h-3.5 w-3.5" strokeWidth={1.5} />
        {addLabel}
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------

export const SellerTemplateTab: React.FC = () => {
  const { currentStore, products, categories, toPersianDigits } = useStore();
  const slug = currentStore.slug;
  const [templates, setTemplates] = useState<StoreTemplate[]>([]);
  const [savedRecord, setSavedRecord] = useState<SellerStoreTemplate | null>(null);
  const [savedConfig, setSavedConfig] = useState<TemplateConfig>({});
  const [selectedId, setSelectedId] = useState<string>('tpl-modern-1');
  const [accent, setAccent] = useState<string>('');
  const [draft, setDraft] = useState<TemplateConfig | null>(null);
  const [dirty, setDirty] = useState<boolean>(false);
  const [expandedKeys, setExpandedKeys] = useState<Record<string, boolean>>({});
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [applying, setApplying] = useState<boolean>(false);
  const [publishing, setPublishing] = useState<boolean>(false);

  // Preview data mirrors SellerStorefrontView: a dropshipper's
  // emulator is fed the store-scoped list (this store's prices),
  // never the unscoped central catalogue; while the scoped fetch
  // is in flight the emulator receives an empty list instead of
  // central-priced stand-ins.
  const [scopedProducts, setScopedProducts] = useState<ReturnType<typeof mapBackendProduct>[] | null>(null);
  useEffect(() => {
    if (currentStore?.ownerType !== 'dropshipper' || !slug) {
      setScopedProducts(null);
      return;
    }
    let cancelled = false;
    // PERF-01: the template preview renders the whole assortment,
    // so this admin surface walks all pages (bounded requests).
    // Public surfaces never do this.
    catalogApi
      .fetchAllProductPages({ storeSlug: slug })
      .then(({ items, complete }) => {
        if (cancelled) return;
        // M-1: a partial walk still previews the pages it loaded;
        // a failed walk leaves the emulator empty (null), never
        // central-priced stand-ins.
        if (items.length > 0) setScopedProducts(items.map(mapBackendProduct));
        else if (complete) setScopedProducts([]);
        else setScopedProducts(null);
        if (!complete) console.warn('Failed to walk all store-scoped product pages.');
      })
      .catch((err) => { if (!cancelled) { console.warn('Failed to fetch store-scoped products:', err); setScopedProducts(null); } });
    return () => { cancelled = true; };
  }, [currentStore?.ownerType, slug]);

  const applyLoaded = (saved: SellerStoreTemplate | null, list: StoreTemplate[]) => {
    const cfg = ((saved?.templateConfig || {}) as TemplateConfig) || {};
    const id = saved?.templateId || (list.length > 0 ? list[0].id : 'tpl-modern-1');
    setSavedRecord(saved);
    setSavedConfig(cfg);
    setSelectedId(id);
    setAccent(typeof cfg.accentColor === 'string' ? cfg.accentColor : '');
    const stored = readStoredDraft(slug, id);
    setDraft(resolveTemplateConfig(id, stored || cfg));
    setDirty(!!stored);
    setExpandedKeys({});
    if (stored) {
      setNotice('پیش‌نویس ذخیره‌نشده‌ات برگردانده شد؛ برای دیده‌شدن در فروشگاه، آن را منتشر کن.');
    }
  };

  const load = () => {
    setLoading(true);
    setError(null);
    storeApi
      .getStoreTemplates()
      .then((res) => {
        const list = res.templates || [];
        setTemplates(list);
        storeApi
          .getSellerStoreTemplate(slug)
          .then((saved) => applyLoaded(saved, list))
          .catch(() => {
            // No saved template yet: defaults below (blueprint of the first).
            applyLoaded(null, list);
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
  }, [slug]);

  // Draft autosave: only unsaved work is persisted, so a publish/revert
  // (dirty=false) never resurrects a stale draft on the next load.
  useEffect(() => {
    if (!draft || !dirty) return;
    try {
      window.localStorage.setItem(draftStorageKey(slug, selectedId), JSON.stringify(draft));
    } catch {
      // storage full/blocked: keep editing in memory.
    }
  }, [draft, dirty, slug, selectedId]);

  const effectiveAccent = accent || resolveTemplateAccent(selectedId);
  const shownTemplateId = savedRecord?.templateId || selectedId;
  const currentName = templates.find((t) => t.id === shownTemplateId)?.name || shownTemplateId;
  const sections: TemplateSection[] =
    draft && Array.isArray(draft.sections) ? (draft.sections as TemplateSection[]) : [];
  const custom: CustomBag = (draft?.custom || {}) as CustomBag;

  const touch = (next: TemplateConfig) => {
    setDraft(next);
    setDirty(true);
  };

  const setCustom = (patch: Partial<TemplateCustom> & Record<string, unknown>) => {
    if (!draft) return;
    touch({ ...draft, custom: { ...((draft.custom || {}) as TemplateCustom), ...patch } });
  };

  const toggleSection = (idx: number) => {
    if (!draft) return;
    const next = sections.map((sec, i) =>
      i === idx ? { ...sec, enabled: sec.enabled === false } : sec,
    );
    touch({ ...draft, sections: next });
  };

  const moveSection = (idx: number, dir: -1 | 1) => {
    if (!draft) return;
    const j = idx + dir;
    if (j < 0 || j >= sections.length) return;
    const next = [...sections];
    const [item] = next.splice(idx, 1);
    next.splice(j, 0, item);
    touch({ ...draft, sections: next });
  };

  const handlePublish = async () => {
    if (!draft || publishing) return;
    setPublishing(true);
    setNotice(null);
    setError(null);
    try {
      const config: TemplateConfig = { ...draft, accentColor: effectiveAccent };
      const asRecord = config as unknown as Record<string, unknown>;
      await storeApi.updateSellerStoreTemplate({ templateId: selectedId, templateConfig: asRecord });
      await storeApi.publishSellerStoreTemplate({
        slug,
        templateId: selectedId,
        templateConfig: asRecord,
      });
      setSavedConfig(config);
      setSavedRecord((prev) =>
        prev
          ? { ...prev, templateId: selectedId, templateConfig: asRecord }
          : { storeId: '', templateId: selectedId, templateConfig: asRecord },
      );
      setDirty(false);
      clearStoredDraft(slug, selectedId);
      setNotice('ظاهر فروشگاه منتشر شد.');
    } catch {
      setError('انتشار انجام نشد. لطفاً دوباره تلاش کنید.');
    } finally {
      setPublishing(false);
    }
  };

  const handleRevert = () => {
    setDraft(resolveTemplateConfig(selectedId, savedConfig));
    setAccent(typeof savedConfig.accentColor === 'string' ? savedConfig.accentColor : '');
    setDirty(false);
    clearStoredDraft(slug, selectedId);
    setNotice('تغییرات ذخیره‌نشده کنار گذاشته شد و به نسخه منتشرشده برگشتی.');
    setError(null);
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
      // Seller content carries over: the saved custom + current effective
      // accent seed the new template; its blueprint supplies the lineup.
      const carry: TemplateConfig = {
        accentColor: effectiveAccent,
        custom: (savedConfig.custom || {}) as TemplateCustom,
      };
      const asRecord = carry as unknown as Record<string, unknown>;
      await storeApi.updateSellerStoreTemplate({ templateId: confirmId, templateConfig: asRecord });
      setSelectedId(confirmId);
      setSavedConfig(carry);
      setSavedRecord((prev) =>
        prev
          ? { ...prev, templateId: confirmId, templateConfig: asRecord }
          : { storeId: '', templateId: confirmId, templateConfig: asRecord },
      );
      setDraft(resolveTemplateConfig(confirmId, carry));
      setDirty(false);
      setExpandedKeys({});
      setNotice('قالب فروشگاه تغییر کرد. بخش‌های قالب تازه را ویرایش کن و بعد «ذخیره و انتشار» را بزن.');
    } catch {
      setError('تغییر قالب انجام نشد.');
    } finally {
      setApplying(false);
      setConfirmId(null);
    }
  };

  const handleAccentChange = (value: string) => {
    setAccent(value);
    setDirty(true);
  };

  const handleResetAccent = () => {
    setAccent('');
    setDirty(true);
    setNotice('به رنگ اصلی قالب برگشت.');
  };

  // ------------------------------------------------------------- field schema
  const slides: TemplateHeroSlide[] = Array.isArray(custom.heroSlides) ? custom.heroSlides : [];
  const banners: TemplateBanner[] = Array.isArray(custom.banners) ? custom.banners : [];
  const trustItems: TemplateTrustItem[] = Array.isArray(custom.trustItems) ? custom.trustItems : [];
  const testimonials: TemplateTestimonial[] = Array.isArray(custom.testimonials)
    ? custom.testimonials
    : [];
  const faqItems: TemplateFaqItem[] = Array.isArray(custom.faqItems) ? custom.faqItems : [];

  const slidesEditor = (
    <ListEditor<TemplateHeroSlide>
      items={slides}
      onChange={(items) => setCustom({ heroSlides: items })}
      newItem={() => ({})}
      addLabel="افزودن اسلاید"
      emptyText="اسلایدی ثبت نشده است؛ بدون اسلاید، بنر اصلی از تیتر و تصویر بنر ساخته می‌شود."
      renderFields={(item, update) => (
        <>
          <TextField label="نشانی تصویر" ltr value={str(item.image)} onChange={(v) => update({ image: v })} />
          <TextField label="تیتر اسلاید" value={str(item.title)} onChange={(v) => update({ title: v })} />
          <TextField label="زیرتیتر اسلاید" value={str(item.subtitle)} onChange={(v) => update({ subtitle: v })} />
          <TextField label="پیوند اسلاید" ltr value={str(item.link)} onChange={(v) => update({ link: v })} />
        </>
      )}
    />
  );

  const bannersEditor = (
    <ListEditor<TemplateBanner>
      items={banners}
      onChange={(items) => setCustom({ banners: items })}
      newItem={() => ({})}
      addLabel="افزودن بنر"
      emptyText="بنری ثبت نشده است؛ این بخش تا وقتی بنری با تصویر ثبت نشود در ویترین نمایش داده نمی‌شود."
      renderFields={(item, update) => (
        <>
          <TextField label="نشانی تصویر" ltr value={str(item.image)} onChange={(v) => update({ image: v })} />
          <TextField label="تیتر بنر" value={str(item.title)} onChange={(v) => update({ title: v })} />
          <TextField label="پیوند بنر" ltr value={str(item.link)} onChange={(v) => update({ link: v })} />
        </>
      )}
    />
  );

  const heroFields = (
    <>
      <TextField label="تیتر بنر" value={str(custom.heroTitle)} onChange={(v) => setCustom({ heroTitle: v })} />
      <TextAreaField label="زیرتیتر بنر" value={str(custom.heroSubtitle)} onChange={(v) => setCustom({ heroSubtitle: v })} />
      <TextField label="متن دکمه" value={str(custom.heroCtaText)} onChange={(v) => setCustom({ heroCtaText: v })} />
      <TextField label="پیوند دکمه" ltr value={str(custom.heroCtaLink)} onChange={(v) => setCustom({ heroCtaLink: v })} />
      <TextField label="تصویر بنر (نشانی)" ltr value={str(custom.heroImage)} onChange={(v) => setCustom({ heroImage: v })} />
      <ToggleField
        label="پخش خودکار اسلایدها"
        checked={custom.heroAutoplay === true}
        onChange={(v) => setCustom({ heroAutoplay: v })}
      />
      <div>
        <p className={labelCls}>اسلایدهای بنر</p>
        {slidesEditor}
      </div>
    </>
  );

  const renderSectionFields = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return (
          <>
            <TextField label="متن اطلاع‌رسانی" value={str(custom.announcementText)} onChange={(v) => setCustom({ announcementText: v })} />
            <TextField label="پیوند اطلاع‌رسانی" ltr value={str(custom.announcementLink)} onChange={(v) => setCustom({ announcementLink: v })} />
          </>
        );
      case 'hero':
      case 'heroSlides':
        return heroFields;
      case 'categoryRail':
        return (
          <>
            <TextField label="تیتر دسته‌بندی‌ها" value={str(custom.categoriesTitle)} onChange={(v) => setCustom({ categoriesTitle: v })} />
            <NumberField label="تعداد دسته‌ها" value={custom.categoriesCount} min={1} max={12} onChange={(v) => setCustom({ categoriesCount: v })} />
            {selectedId === 'tpl-fashion-1' && <div><p className={labelCls}>تصاویر ورودی مجموعه‌ها</p><p className="text-xs text-slate-500 leading-6 mb-3">سه بنر اول برای ورودی‌های زیر بنر اصلی استفاده می‌شوند. تصویر و پیوند هر بنر قابل تغییر است. در فروشگاه دارای کالا، عنوان‌ها از دسته‌بندی کالاها نمایش داده می‌شوند.</p>{bannersEditor}</div>}
          </>
        );
      case 'deals':
        return (
          <>
            <TextField label="تیتر پیشنهادها" value={str(custom.dealsTitle)} onChange={(v) => setCustom({ dealsTitle: v })} />
            <NumberField label="تعداد پیشنهادها" value={custom.dealsCount} min={1} onChange={(v) => setCustom({ dealsCount: v })} />
          </>
        );
      case 'timedDeals':
        return (
          <p className="rounded-xl bg-white border border-slate-200 p-3 text-[11px] leading-6 text-slate-500">
            این بخش از پیشنهادهای زمان‌دار فعال فروشگاه تغذیه می‌شود و محتوای جدا ندارد.
          </p>
        );
      case 'banners':
      case 'promo':
        return bannersEditor;
      case 'brands':
        return (
          <TextField label="تیتر برندها" value={str(custom.brandsTitle)} onChange={(v) => setCustom({ brandsTitle: v })} />
        );
      case 'featured':
        return (
          <>
            <TextField label="تیتر کالاهای منتخب" value={str(custom.featuredTitle)} onChange={(v) => setCustom({ featuredTitle: v })} />
            <NumberField label="تعداد کالاهای منتخب" value={custom.featuredCount} min={1} onChange={(v) => setCustom({ featuredCount: v })} />
          </>
        );
      case 'bestsellers':
        return (
          <>
            <TextField label="تیتر پرفروش‌ترین‌ها" value={str(custom.bestSellersTitle)} onChange={(v) => setCustom({ bestSellersTitle: v })} />
            <NumberField label="تعداد پرفروش‌ترین‌ها" value={custom.bestsellersCount} min={1} onChange={(v) => setCustom({ bestsellersCount: v })} />
          </>
        );
      case 'discounted':
        return (
          <>
            <TextField label="تیتر کالاهای تخفیف‌دار" value={str(custom.discountedTitle)} onChange={(v) => setCustom({ discountedTitle: v })} />
            <NumberField label="تعداد کالاهای تخفیف‌دار" value={custom.discountedCount} min={1} onChange={(v) => setCustom({ discountedCount: v })} />
          </>
        );
      case 'topRated':
        return (
          <>
            <TextField label="تیتر پرامتیازترین‌ها" value={str(custom.topRatedTitle)} onChange={(v) => setCustom({ topRatedTitle: v })} />
            <NumberField label="تعداد پرامتیازترین‌ها" value={custom.topRatedCount} min={1} onChange={(v) => setCustom({ topRatedCount: v })} />
          </>
        );
      case 'trustStrip':
        return (
          <>
            <p className="rounded-xl bg-white border border-slate-200 p-3 text-[11px] leading-6 text-slate-500">
              خالی بماند، موارد پیش‌فرض پلتفرم نمایش داده می‌شود.
            </p>
            <ListEditor<TemplateTrustItem>
              items={trustItems}
              onChange={(items) => setCustom({ trustItems: items })}
              newItem={() => ({ title: '' })}
              addLabel="افزودن مورد اعتماد"
              emptyText="موردی ثبت نشده است."
              renderFields={(item, update) => (
                <>
                  <TextField label="عنوان" value={str(item.title)} onChange={(v) => update({ title: v })} />
                  <TextField label="توضیح" value={str(item.subtitle)} onChange={(v) => update({ subtitle: v })} />
                </>
              )}
            />
          </>
        );
      case 'about':
        return (
          <>
            <TextField label="تیتر درباره فروشگاه" value={str(custom.aboutTitle)} onChange={(v) => setCustom({ aboutTitle: v })} />
            <TextAreaField label="متن درباره فروشگاه" value={str(custom.aboutText)} onChange={(v) => setCustom({ aboutText: v })} />
            <TextField label="تصویر درباره فروشگاه (نشانی)" ltr value={str(custom.aboutImage)} onChange={(v) => setCustom({ aboutImage: v })} />
          </>
        );
      case 'testimonials':
        return (
          <ListEditor<TemplateTestimonial>
            items={testimonials}
            onChange={(items) => setCustom({ testimonials: items })}
            newItem={() => ({ name: '', text: '' })}
            addLabel="افزودن نظر مشتری"
            emptyText="نظری ثبت نشده است؛ این بخش تا وقتی نظری ثبت نشود در ویترین نمایش داده نمی‌شود."
            renderFields={(item, update) => (
              <>
                <TextField label="نام مشتری" value={str(item.name)} onChange={(v) => update({ name: v })} />
                <TextAreaField label="متن نظر" value={str(item.text)} onChange={(v) => update({ text: v })} />
              </>
            )}
          />
        );
      case 'faq':
        return (
          <>
            <p className="rounded-xl bg-white border border-slate-200 p-3 text-[11px] leading-6 text-slate-500">
              خالی بماند، پرسش‌های متداول پلتفرم نمایش داده می‌شود.
            </p>
            <ListEditor<TemplateFaqItem>
              items={faqItems}
              onChange={(items) => setCustom({ faqItems: items })}
              newItem={() => ({ q: '', a: '' })}
              addLabel="افزودن پرسش"
              emptyText="پرسشی ثبت نشده است."
              renderFields={(item, update) => (
                <>
                  <TextField label="پرسش" value={str(item.q)} onChange={(v) => update({ q: v })} />
                  <TextAreaField label="پاسخ" value={str(item.a)} onChange={(v) => update({ a: v })} />
                </>
              )}
            />
          </>
        );
      case 'newsletter':
        return (
          <>
            <TextField label="تیتر خبرنامه" value={str(custom.newsletterTitle)} onChange={(v) => setCustom({ newsletterTitle: v })} />
            <TextField label="زیرتیتر خبرنامه" value={str(custom.newsletterSubtitle)} onChange={(v) => setCustom({ newsletterSubtitle: v })} />
          </>
        );
      case 'lookbook':
        return (
          <>
            <TextField label="تیتر کالکشن" value={str(custom.lookbookTitle)} onChange={(v) => setCustom({ lookbookTitle: v })} />
            <TextField label="زیرتیتر کالکشن" value={str(custom.lookbookSubtitle)} onChange={(v) => setCustom({ lookbookSubtitle: v })} />
            <TextField label="تصویر کالکشن (نشانی)" ltr value={str(custom.lookbookImage)} onChange={(v) => setCustom({ lookbookImage: v })} />
            <TextField label="پیوند کالکشن" ltr value={str(custom.lookbookLink)} onChange={(v) => setCustom({ lookbookLink: v })} />
          </>
        );
      case 'campaign':
        return (
          <p className="rounded-xl bg-white border border-slate-200 p-3 text-[11px] leading-6 text-slate-500">
            از کمپین فعال فروشگاه (بخش تبلیغات) تغذیه می‌شود.
          </p>
        );
      case 'products':
      case 'allProducts':
        return (
          <TextField label="تیتر همه کالاها" value={str(custom.productsTitle)} onChange={(v) => setCustom({ productsTitle: v })} />
        );
      default:
        return null;
    }
  };

  const blueprint = blueprintFor(selectedId);

  return (
    <div className="space-y-4">
      <SupplierSectionHeading title="ظاهر فروشگاه" description="رنگ و بخش‌های ویترین را ویرایش کن، پیش‌نمایش را ببین و تغییرات را منتشر کن." icon={<LayoutTemplate />} />
      <div className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <div>
          <h3 className="text-base font-extrabold text-[#261b41]">قالب فعلی: {currentName}</h3>
          {blueprint && <p className="mt-1 text-sm leading-6 text-slate-600">{blueprint.tagline}</p>}
        </div>
        {loading && <p role="status" className="rounded-xl bg-slate-50 p-3 text-xs text-slate-600">در حال بارگذاری تنظیمات ظاهر فروشگاه…</p>}
        {error && <p role="alert" className="rounded-xl bg-rose-50 border border-rose-200 p-3 text-xs text-rose-700">{error}</p>}
        {error && !loading && (
          <button type="button" onClick={load} className="rounded-xl bg-slate-900 px-3 py-2 text-xs font-bold text-white">تلاش مجدد</button>
        )}
        {notice && <p role="status" className="rounded-xl bg-emerald-50 border border-emerald-200 p-3 text-xs text-emerald-700">{notice}</p>}
        {!loading && !error && templates.length === 0 && (
          <p className="rounded-xl border border-dashed p-5 text-xs leading-6 text-slate-500">قالبی برای نمایش وجود ندارد.</p>
        )}
        {!loading && templates.length > 0 && (
          <details className="group rounded-xl border border-slate-200 px-3">
            <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">
              دیدن قالب‌ها و تغییر قالب
              <ChevronDown aria-hidden="true" className="size-4 shrink-0 group-open:rotate-180" />
            </summary>
            <p className="mb-3 text-sm leading-6 text-slate-600">تغییر قالب پس از تأیید بلافاصله اعمال می‌شود. متن‌ها، بنرها و رنگ به قالب تازه منتقل می‌شوند و چیدمان از قالب تازه می‌آید.</p>
            <div className="grid grid-cols-2 gap-2 pb-3 md:grid-cols-3 xl:grid-cols-5">
              {templates.map((meta) => {
                const id = meta.id;
                const preset = TEMPLATE_ACCENTS[id as keyof typeof TEMPLATE_ACCENTS];
                const tagline = blueprintFor(id)?.tagline || meta.description;
                return (
                  <button
                    key={id}
                    type="button"
                    onClick={() => handleSelectTemplate(id)}
                    aria-pressed={selectedId === id}
                    className={`rounded-2xl border p-3 text-start focus-visible:ring-2 focus-visible:ring-brand ${selectedId === id ? 'border-brand-deep bg-brand-soft' : 'border-slate-200'}`}
                  >
                    <span className="flex flex-wrap items-center justify-between gap-1">
                      <span className="block text-xs font-bold text-slate-900">{meta.name || id}</span>
                      {shownTemplateId === id ? (
                        <span className="rounded-full bg-brand-deep px-2 py-0.5 text-[10px] font-bold text-white">قالب فعلی</span>
                      ) : null}
                    </span>
                    {tagline ? <span className="mt-1 block text-[11px] leading-5 text-slate-500">{tagline}</span> : null}
                    {meta.thumbnailUrl ? <img src={meta.thumbnailUrl} alt="" loading="lazy" onError={(e) => { e.currentTarget.style.display = 'none'; }} className="mt-2 h-16 w-full rounded-xl object-cover bg-slate-100" /> : null}
                    {preset ? (
                      <span className="mt-2 inline-block h-4 w-4 rounded-full border border-slate-200" style={{ backgroundColor: preset }} />
                    ) : null}
                  </button>
                );
              })}
            </div>
          </details>
        )}
      </div>

      {!loading && draft && (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
          <div className="order-2 space-y-3 xl:order-1">
            <div className="space-y-1 rounded-2xl border border-slate-200 bg-white p-4">
              <div className="flex flex-wrap items-center gap-2">
                <label htmlFor="seller-template-accent" className="text-sm font-bold text-slate-700">رنگ فروشگاه</label>
                <input
                  id="seller-template-accent"
                  type="color"
                  value={/^#[0-9a-fA-F]{6}$/.test(effectiveAccent) ? effectiveAccent : '#5d44f8'}
                  onChange={(e) => handleAccentChange(e.target.value)}
                  className="h-8 w-12 rounded-lg border border-slate-200 bg-white p-1"
                />
                <span className="text-[11px] text-slate-500" dir="ltr">{effectiveAccent}</span>
                <button type="button" onClick={handleResetAccent} className="ms-auto min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">بازگشت به رنگ پیش‌فرض</button>
              </div>
              <p className="text-[11px] leading-5 text-slate-500">
                {accent ? 'رنگ اصلی فروشگاه شما؛ پیش‌فرض، رنگ اصلی قالب است.' : 'رنگ فعلی، رنگ اصلی قالب است.'}
              </p>
            </div>

            <div className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
              <div>
                <h3 className="text-base font-extrabold text-[#261b41]">بخش‌های ویترین</h3>
                <p className="mt-1 text-sm leading-6 text-slate-600">
                  هر بخش را باز کن تا محتوایش را ویرایش کنی. از پیکان‌ها برای جابه‌جایی و از کلید کنار نام برای نمایش بخش استفاده کن.
                </p>
              </div>
              {sections.length === 0 && (
                <p className="rounded-xl border border-dashed p-5 text-xs leading-6 text-slate-500">بخشی برای این قالب تعریف نشده است.</p>
              )}
              {sections.map((sec, idx) => {
                const key = canonicalSectionKey(sectionKey(sec));
                const expanded = !!expandedKeys[key];
                const enabled = sec.enabled !== false;
                return (
                  <div key={`${key}-${idx}`} className={`overflow-hidden rounded-2xl border ${enabled ? 'border-slate-200' : 'border-slate-200 bg-slate-50/60'}`}>
                    <div className="flex items-center gap-2 p-3">
                      <div className="flex flex-col gap-0.5">
                        <button
                          type="button"
                          onClick={() => moveSection(idx, -1)}
                          disabled={idx === 0}
                          aria-label="انتقال به بالا"
                          className="rounded-md p-1 text-slate-500 hover:bg-slate-100 disabled:opacity-30"
                        >
                          <ArrowUp className="h-3.5 w-3.5" strokeWidth={1.5} />
                        </button>
                        <button
                          type="button"
                          onClick={() => moveSection(idx, 1)}
                          disabled={idx === sections.length - 1}
                          aria-label="انتقال به پایین"
                          className="rounded-md p-1 text-slate-500 hover:bg-slate-100 disabled:opacity-30"
                        >
                          <ArrowDown className="h-3.5 w-3.5" strokeWidth={1.5} />
                        </button>
                      </div>
                      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-[11px] font-black text-slate-500">
                        {toPersianDigits(idx + 1)}
                      </span>
                      <div className="min-w-0 flex-1">
                        <p className={`truncate text-xs font-bold ${enabled ? 'text-slate-800' : 'text-slate-400'}`}>{sec.label}</p>
                      </div>
                      <Switch checked={enabled} onChange={() => toggleSection(idx)} ariaLabel={`نمایش بخش ${sec.label}`} />
                      <button
                        type="button"
                        onClick={() => setExpandedKeys((prev) => ({ ...prev, [key]: !prev[key] }))}
                        aria-expanded={expanded}
                        aria-label={expanded ? `بستن ویرایش ${sec.label}` : `ویرایش ${sec.label}`}
                        className="grid size-11 shrink-0 place-items-center rounded-lg text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand"
                      >
                        {expanded ? <ChevronUp className="h-4 w-4" strokeWidth={1.5} /> : <ChevronDown className="h-4 w-4" strokeWidth={1.5} />}
                      </button>
                    </div>
                    {expanded && (
                      <div className="space-y-3 border-t border-slate-100 bg-slate-50/70 p-3">
                        {renderSectionFields(key)}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          <div className="order-1 xl:order-2 xl:sticky xl:top-4">
            <TemplatePreviewEmulator
              templateId={selectedId}
              store={currentStore}
              products={currentStore.ownerType === 'dropshipper' ? scopedProducts ?? [] : products}
              categories={categories}
              accentColor={effectiveAccent}
              config={{ ...draft, accentColor: effectiveAccent }}
            />
          </div>
        </div>
      )}

      {!loading && draft && (
        <div className="sticky bottom-3 z-30 flex flex-wrap items-center gap-2 rounded-2xl border border-brand-border bg-white p-3 shadow-sm">
          {dirty ? (
            <span className="flex items-center gap-1.5 text-[11px] font-bold text-amber-600">
              <span className="h-2 w-2 rounded-full bg-amber-500" />
              پیش‌نویس منتشر نشده است
            </span>
          ) : (
            <span className="text-[11px] font-bold text-emerald-600">همه تغییرات منتشر شده است</span>
          )}
          <div className="ms-auto flex flex-wrap gap-2">
            <button
              type="button"
              onClick={handleRevert}
              disabled={!dirty || publishing}
              className="min-h-11 rounded-xl bg-slate-100 px-4 text-xs font-bold text-slate-700 hover:bg-slate-200 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-40"
            >
              بازگردانی
            </button>
            <button
              type="button"
              onClick={handlePublish}
              disabled={publishing}
              className="min-h-11 rounded-xl bg-brand-deep px-4 text-xs font-bold text-white hover:bg-brand-deep/90 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
            >
              {publishing ? 'در حال انتشار…' : 'ذخیره و انتشار'}
            </button>
          </div>
        </div>
      )}

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
