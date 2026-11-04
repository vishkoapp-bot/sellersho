import React, { useState, useEffect, useRef } from 'react';
import { catalogApi, FacetAttribute } from '../../services/catalogApi';
import { Loader2 } from 'lucide-react';

// Live category-attributes editor (ql): Digikala-style per-category
// attributes (single-select chips), colors, and brand suggestions. The
// brand select only suggests; the final brand stays the "brand" field.
export interface CategoryAttributesValue {
  attributes: Record<string, number[]>;
  colorId: number | null;
  brandId: number | null;
}

export const EMPTY_CATEGORY_ATTRIBUTES: CategoryAttributesValue = { attributes: {}, colorId: null, brandId: null };

export const isEmptyCategoryAttributes = (value: CategoryAttributesValue): boolean =>
  value.colorId === null && value.brandId === null && Object.values(value.attributes).every((list) => list.length === 0);

interface FacetsResponse {
  attributes: FacetAttribute[];
  colors: { id: number; title: string; hexCode: string }[];
  brands: { id: number; titleFa: string; titleEn: string }[];
  inherited: boolean;
}

export const SupplierCategoryAttributes: React.FC<{
  categorySlug: string | null;
  value: CategoryAttributesValue;
  onChange: (value: CategoryAttributesValue) => void;
  onBrandSuggest: (titleFa: string) => void;
}> = ({ categorySlug, value, onChange, onBrandSuggest }) => {
  const [facets, setFacets] = useState<FacetsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  const seqRef = useRef(0);

  useEffect(() => {
    if (!categorySlug) {
      setFacets(null);
      setError(null);
      setLoading(false);
      return;
    }
    const seq = ++seqRef.current;
    setLoading(true);
    setError(null);
    catalogApi.getCategoryAttributes(categorySlug)
      .then((res: FacetsResponse) => { if (seqRef.current === seq) { setFacets(res); setLoading(false); } })
      .catch((cause: unknown) => { if (seqRef.current === seq) { setError(cause instanceof Error ? cause.message : 'خطا در بارگذاری ویژگی‌های دسته.'); setLoading(false); } });
  }, [categorySlug, retry]);

  if (!categorySlug) return null;

  const toggleAttribute = (attrId: number, valueId: number) => {
    const key = String(attrId);
    const current = value.attributes[key] ?? [];
    const next = current.length === 1 && current[0] === valueId ? [] : [valueId];
    onChange({ ...value, attributes: { ...value.attributes, [key]: next } });
  };

  return (
    <div className="space-y-3 pt-1">
      <label className="text-[10px] font-bold text-slate-700 block">ویژگی‌های دسته (دیجی‌کالا):</label>
      {loading && (
        <p className="text-[10px] text-slate-500 flex items-center gap-1.5">
          <Loader2 className="w-3.5 h-3.5 animate-spin"  strokeWidth={1.5}/>
          در حال بارگذاری ویژگی‌های دسته…
        </p>
      )}
      {!loading && error && (
        <div className="bg-red-50 border border-red-200 rounded-xl px-2.5 py-2">
          <p className="text-[10px] leading-5 text-red-700">{error}</p>
          <button type="button" onClick={() => setRetry((n) => n + 1)} className="mt-1 text-[10px] font-bold text-red-700 underline">تلاش دوباره</button>
        </div>
      )}
      {!loading && !error && facets && (
        <>
          {facets.inherited && (
            <p className="text-[10px] leading-5 bg-violet-50 border border-violet-200 text-violet-800 rounded-xl px-2.5 py-2">ویژگی‌های زیردسته‌ها — این دسته خودش ویژگی ندارد؛ ویژگی‌های زیر از زیردسته‌های آن گرد آمده‌اند.</p>
          )}
          {facets.attributes.length === 0 ? (
            <p className="text-[10px] leading-5 bg-slate-50 border border-slate-200 text-slate-500 rounded-xl px-2.5 py-2">برای این دسته و زیردسته‌های آن ویژگی قابل انتخابی ثبت نشده است؛ مشخصات را در بخش «مشخصات فنی» بنویسید.</p>
          ) : (
            facets.attributes.map((attr) => {
              const selected = (value.attributes[String(attr.id)] ?? [])[0] ?? null;
              return (
                <div key={attr.id} className="space-y-1">
                  <p className="text-[10px] font-bold text-slate-700">{attr.title}</p>
                  <div className="flex flex-wrap gap-1.5">
                    {attr.values.map((v) => {
                      const isSelected = selected === v.id;
                      return (
                        <label key={v.id} className={`flex items-center gap-1 px-2 py-1 rounded-lg text-[10px] font-bold cursor-pointer border transition-all ${isSelected ? 'bg-brand-deep text-white border-brand-deep' : 'bg-slate-100 text-slate-600 border-slate-200 hover:bg-slate-200'}`}>
                          <input type="checkbox" checked={isSelected} onChange={() => toggleAttribute(attr.id, v.id)} className="accent-[#5d44f8]" />
                          {v.titleFa}
                        </label>
                      );
                    })}
                  </div>
                </div>
              );
            })
          )}
          {facets.colors.length > 0 && (
            <div className="space-y-1">
              <p className="text-[10px] font-bold text-slate-700">رنگ:</p>
              <div className="flex flex-wrap gap-1.5">
                {facets.colors.map((color) => {
                  const isSelected = value.colorId === color.id;
                  return (
                    <button
                      key={color.id}
                      type="button"
                      title={color.title}
                      onClick={() => onChange({ ...value, colorId: isSelected ? null : color.id })}
                      className={`flex items-center gap-1.5 px-2 py-1 rounded-lg text-[10px] font-bold border transition-all ${isSelected ? 'border-[#5d44f8] bg-violet-50 text-slate-800' : 'bg-slate-100 text-slate-600 border-slate-200 hover:bg-slate-200'}`}
                    >
                      <span className="inline-block w-4 h-4 rounded-full border border-slate-300" style={{ backgroundColor: color.hexCode || '#e2e8f0' }} />
                      {color.title}
                    </button>
                  );
                })}
              </div>
            </div>
          )}
          {facets.brands.length > 0 && (
            <div className="space-y-1">
              <label className="text-[10px] font-bold text-slate-700 block">پیشنهاد برند از فهرست دسته:</label>
              <select
                value={value.brandId ?? ''}
                onChange={(event) => {
                  const id = event.target.value ? Number(event.target.value) : null;
                  const brand = facets.brands.find((b) => b.id === id);
                  onChange({ ...value, brandId: id });
                  if (brand) onBrandSuggest(brand.titleFa);
                }}
                className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200"
              >
                <option value="">— انتخاب برند از فهرست دسته —</option>
                {facets.brands.map((brand) => (
                  <option key={brand.id} value={brand.id}>{brand.titleFa}{brand.titleEn && brand.titleEn !== brand.titleFa ? ` (${brand.titleEn})` : ''}</option>
                ))}
              </select>
              <p className="text-[10px] leading-5 text-slate-500">فقط پیشنهاد است؛ برند نهایی همان فیلد «برند محصول» است.</p>
            </div>
          )}
        </>
      )}
    </div>
  );
};
