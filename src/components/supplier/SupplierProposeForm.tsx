import React, { useState, useEffect } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { catalogApi, NicheDto } from '../../services/catalogApi';
import { getApiBase, ApiError } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { SupplierCategoryPicker, CategorySelection } from './SupplierCategoryPicker';
import { SupplierCategoryAttributes, CategoryAttributesValue, EMPTY_CATEGORY_ATTRIBUTES, isEmptyCategoryAttributes } from './SupplierCategoryAttributes';
import { Plus, Trash2, Loader2, ImagePlus, ExternalLink, AlertTriangle } from 'lucide-react';

// Live propose form (Jl form block): supplier's new-product proposal.
// Field order, labels, hints, validations and payload mirror live's Jl.
const formatSep = (raw: string): string => {
  const digits = raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
  if (digits === '') return '';
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬').replace(/\d/g, (ch) => '۰۱۲۳۴۵۶۷۸۹'[parseInt(ch, 10)]);
};

const parseSep = (raw: string): string =>
  raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');

// Live's Bs niche map (category top slug -> niche ids), using the DB niche ids.
const NICHE_BY_CATEGORY: Record<string, string[]> = {
  digital: ['niche-smartwatch', 'niche-powerbank', 'niche-headphone'],
  electronics: ['niche-powerbank', 'niche-headphone'],
  gadgets: ['niche-smartwatch'],
  fashion: ['niche-leather'],
  apparel: ['niche-leather'],
  gold: ['niche-gold18'],
  jewelry: ['niche-gold18'],
  appliances: ['niche-home-appliances'],
  home: ['niche-home-appliances'],
  beauty: ['niche-skincare'],
  cosmetics: ['niche-skincare'],
  culture: ['niche-books'],
  books: ['niche-books'],
  car: ['niche-auto'],
  tools: ['niche-auto'],
  supermarket: ['niche-books'],
  travel: ['niche-auto'],
};

const DEFAULT_CATEGORY: CategorySelection = {
  slug: 'digital',
  title: 'کالای دیجیتال',
  path: [{ id: 'cat-digital', title: 'کالای دیجیتال', slug: 'digital' }],
  pathDisplay: 'کالای دیجیتال',
};

interface VariantDraft {
  name: string;
  color: string;
  size: string;
  custom: { key: string; value: string }[];
  stock: string;
  cost: string;
}

const EMPTY_VARIANT = (): VariantDraft => ({ name: '', color: '', size: '', custom: [], stock: '', cost: '' });

export interface ProposeFormProps {
  warranties: { id: string; name: string }[];
  myWarehouses: { id: string; name: string; cityName?: string; isActive?: boolean }[];
  profileCity: string;
  profileCityConfirmed: boolean;
  sessionToken?: string;
  onSubmitted: () => void;
  onChallengeSubmitted: () => void;
}

export const SupplierProposeForm: React.FC<ProposeFormProps> = ({ warranties, myWarehouses, profileCity, profileCityConfirmed, sessionToken, onSubmitted, onChallengeSubmitted }) => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();

  const [title, setTitle] = useState('');
  const [brand, setBrand] = useState('');
  const [noBrand, setNoBrand] = useState(false);
  const [cost, setCost] = useState('');
  const [warranty, setWarranty] = useState('۱۸ ماه گارانتی شرکتی');
  const [requestingWarranty, setRequestingWarranty] = useState(false);
  const [warrantyRequestName, setWarrantyRequestName] = useState('');
  const [category, setCategory] = useState<CategorySelection | null>(DEFAULT_CATEGORY);
  const [categoryAttributes, setCategoryAttributes] = useState<CategoryAttributesValue>(EMPTY_CATEGORY_ATTRIBUTES);
  const [allNiches, setAllNiches] = useState<NicheDto[]>([]);
  const [selectedNicheIds, setSelectedNicheIds] = useState<string[]>(['niche-smartwatch']);
  const [description, setDescription] = useState('');
  const [specs, setSpecs] = useState<{ key: string; value: string }[]>([{ key: 'مدل', value: '' }]);
  const [stock, setStock] = useState('100');
  const [prepDays, setPrepDays] = useState('1');
  const [fulfillment, setFulfillment] = useState('warehouse');
  const [warehouseId, setWarehouseId] = useState('');
  const [variants, setVariants] = useState<VariantDraft[]>([]);
  const [weightGrams, setWeightGrams] = useState('');
  const [lengthCm, setLengthCm] = useState('');
  const [widthCm, setWidthCm] = useState('');
  const [heightCm, setHeightCm] = useState('');
  const [packLengthCm, setPackLengthCm] = useState('');
  const [packWidthCm, setPackWidthCm] = useState('');
  const [packHeightCm, setPackHeightCm] = useState('');
  const [isOriginal, setIsOriginal] = useState(true);
  const [images, setImages] = useState<string[]>([]);
  const [firstImage, setFirstImage] = useState<string | null>(null);
  const [imageLink, setImageLink] = useState('');
  const [uploading, setUploading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [challenge, setChallenge] = useState<{ publicProductId: string; message: string } | null>(null);
  const [challengeProposal, setChallengeProposal] = useState<Record<string, unknown> | null>(null);
  const [challengeBranch, setChallengeBranch] = useState<'same_variant' | 'new_variants'>('same_variant');
  const [challengeVariantName, setChallengeVariantName] = useState('');
  const [challengeVariantColor, setChallengeVariantColor] = useState('');
  const [challengeNewNames, setChallengeNewNames] = useState('');
  const [challengePrice, setChallengePrice] = useState('');
  const [challengeSaving, setChallengeSaving] = useState(false);

  useEffect(() => {
    catalogApi.getNiches().then(setAllNiches).catch(() => undefined);
  }, []);

  // Live's niche filter: top path segment (or the slug) selects the mapped
  // subset; without a mapping every niche shows, with the note below.
  const topSlug = category?.path[0]?.slug ?? category?.slug ?? '';
  const mapped = NICHE_BY_CATEGORY[topSlug];
  const visibleNiches = mapped ? allNiches.filter((n) => mapped.includes(n.id)) : allNiches;
  const showNoMappingNote = visibleNiches.length === allNiches.length && (!topSlug || !mapped);

  useEffect(() => {
    if (visibleNiches.length === 0) return;
    setSelectedNicheIds((prev) => {
      const kept = prev.filter((id) => visibleNiches.some((n) => n.id === id));
      return kept.length > 0 ? kept : [visibleNiches[0].id];
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topSlug]);

  const toggleNiche = (id: string) =>
    setSelectedNicheIds((prev) => prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]);

  const uploadImages = async (files: File[]) => {
    if (images.length + files.length > 8) {
      error('حداکثر ۸ تصویر برای هر محصول مجاز است.');
      return;
    }
    for (const file of files) {
      if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
        error('فرمت تصویر نامعتبر است. فقط فرمت‌های JPG، PNG و WEBP مجاز هستند.');
        return;
      }
      if (file.size > 2 * 1024 * 1024) {
        error('حجم هر فایل تصویر نباید بیشتر از ۲ مگابایت باشد.');
        return;
      }
    }
    setUploading(true);
    try {
      const uploaded: string[] = [];
      for (const file of files) {
        const form = new FormData();
        form.append('image', file);
        const res = await fetch(`${getApiBase()}/upload/product-image`, {
          method: 'POST',
          headers: sessionToken ? { Authorization: `Bearer ${sessionToken}` } : {},
          body: form,
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error((data as { error?: string }).error || 'خطا در آپلود تصویر');
        }
        const data = await res.json();
        if (data.success && data.imageUrl) uploaded.push(data.imageUrl);
        else throw new Error((data as { error?: string }).error || 'خطا در آپلود تصویر');
      }
      const next = [...images, ...uploaded];
      setImages(next);
      setFirstImage(next[0] || null);
      success(`${toPersianDigits(uploaded.length)} تصویر با موفقیت بارگذاری شد.`);
    } catch (cause: unknown) {
      const name = (cause as { name?: string })?.name;
      error(name === 'AbortError' ? 'آپلود بیش از حد طول کشید. لطفاً دوباره تلاش کنید.' : cause instanceof Error ? cause.message : 'خطا در برقراری ارتباط با سرور آپلود.');
    } finally {
      setUploading(false);
    }
  };

  const onFiles = (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files ? Array.from(event.target.files) : [];
    if (files.length === 0) return;
    void uploadImages(files);
    event.target.value = '';
  };

  const removeImage = (index: number) => {
    const next = images.filter((_, i) => i !== index);
    setImages(next);
    setFirstImage(next[0] || null);
  };

  const submit = async () => {
    if (saving || uploading) return;
    if (!title.trim()) { error('لطفاً عنوان یا بارکد کالا را وارد کنید.'); return; }
    if (brand.trim().length > 100) { error('نام برند نمی‌تواند بیشتر از ۱۰۰ نویسه باشد.'); return; }
    if (!category) { error('دسته‌بندی کالا را انتخاب کنید.'); return; }
    if (Array.from(description.trim()).length < 30) { error('توضیحات محصول باید حداقل ۳۰ نویسه باشد.'); return; }
    if (!noBrand && !brand.trim()) { error('برند کالا را وارد کنید یا گزینه «بدون برند» را انتخاب کنید.'); return; }
    const costNum = Number(cost);
    if (!Number.isSafeInteger(costNum) || costNum <= 0 || !Number.isSafeInteger(Number(stock)) || Number(stock) < 0) { error('قیمت عمده و موجودی را درست وارد کن.'); return; }
    if (selectedNicheIds.length === 0) { error('حداقل یک موضوع (نیچ) برای این کالا انتخاب کن.'); return; }
    const posInt = (raw: string): number | undefined => {
      const n = Number(raw);
      return raw.trim() !== '' && Number.isSafeInteger(n) && n > 0 ? n : undefined;
    };
    const weight = posInt(weightGrams);
    const lenMm = posInt(lengthCm);
    const widMm = posInt(widthCm);
    const heiMm = posInt(heightCm);
    if (weight === undefined || lenMm === undefined || widMm === undefined || heiMm === undefined) { error('وزن (گرم) و هر سه بُعد کالا (سانتی‌متر) الزامی است؛ بدون آن‌ها پیشنهاد تأیید نمی‌شود.'); return; }
    const packParts = [packLengthCm, packWidthCm, packHeightCm];
    const packFilled = packParts.filter((p) => p.trim() !== '').length;
    if (packFilled > 0 && packFilled < 3) { error('ابعاد بسته‌بندی را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).'); return; }
    const packLen = packFilled === 3 ? posInt(packLengthCm) : undefined;
    const packWid = packFilled === 3 ? posInt(packWidthCm) : undefined;
    const packHei = packFilled === 3 ? posInt(packHeightCm) : undefined;
    if (packFilled === 3 && (packLen === undefined || packWid === undefined || packHei === undefined)) { error('ابعاد بسته‌بندی باید عددهای صحیح مثبت باشند (سانتی‌متر).'); return; }
    const filledVariants = variants.filter((v) => v.name.trim() !== '');
    const seenNames = new Set<string>();
    for (const v of filledVariants) {
      const name = v.name.trim();
      if (seenNames.has(name)) { error(`نام تنوع «${name}» در همین فهرست تکرار شده است.`); return; }
      seenNames.add(name);
      const vStock = v.stock.trim() === '' ? 0 : Number(v.stock);
      if (!Number.isSafeInteger(vStock) || vStock < 0) { error(`موجودی تنوع «${name}» را درست وارد کن.`); return; }
      if (v.cost.trim() !== '') {
        const vCost = Number(v.cost);
        if (!Number.isSafeInteger(vCost) || vCost <= 0) { error(`قیمت عمده تنوع «${name}» را درست وارد کن (عدد مثبت به تومان).`); return; }
      }
    }
    const specsObject: Record<string, string> = {};
    specs.forEach((spec) => { if (spec.key.trim() && spec.value.trim()) specsObject[spec.key.trim()] = spec.value.trim(); });
    const variantPayload = filledVariants.map((v) => {
      const attrs: Record<string, string> = {};
      if (v.color.trim()) attrs['رنگ'] = v.color.trim();
      if (v.size.trim()) attrs['سایز'] = v.size.trim();
      v.custom.forEach((c) => { if (c.key.trim() && c.value.trim()) attrs[c.key.trim()] = c.value.trim(); });
      const parsedCost = v.cost.trim() === '' ? 0 : parseInt(parseSep(v.cost), 10);
      return {
        variant_name: v.name.trim(),
        attributes: attrs,
        stock: Number(v.stock) || 0,
        ...(parsedCost > 0 ? { supplier_base_cost: parsedCost } : {}),
      };
    });
    const variantStockSum = variantPayload.reduce((sum, v) => sum + v.stock, 0);
    const payload: Record<string, unknown> = {
      title: title.trim(),
      brand: noBrand ? '' : brand.trim(),
      noBrand,
      supplierBaseCost: costNum,
      categorySlug: category.slug,
      ...(!isEmptyCategoryAttributes(categoryAttributes) ? { categoryAttributes } : {}),
      nicheIds: selectedNicheIds,
      barcode: '',
      warranty,
      imageUrl: images[0] || imageLink.trim() || '',
      imageUrls: images.length > 0 ? images : imageLink.trim() ? [imageLink.trim()] : [],
      description: description.trim(),
      specs: specsObject,
      stock: variantPayload.length > 0 ? variantStockSum : Number(stock) || 0,
      ...(variantPayload.length > 0 ? { variants: variantPayload } : {}),
      fulfillmentMethod: fulfillment,
      supplierWarehouseId: fulfillment === 'supplier_direct' ? warehouseId : '',
      weightGrams: weight,
      lengthMm: lenMm * 10,
      widthMm: widMm * 10,
      heightMm: heiMm * 10,
      packagingLengthMm: packLen !== undefined ? packLen * 10 : undefined,
      packagingWidthMm: packWid !== undefined ? packWid * 10 : undefined,
      packagingHeightMm: packHei !== undefined ? packHei * 10 : undefined,
      preparationDays: Number(prepDays) || 1,
      isOriginal,
    };
    setSaving(true);
    try {
      await supplierApi.proposeProduct(payload);
      setChallenge(null);
      setChallengeProposal(null);
      success(`کالای «${title}» برای بررسی مدیر فرستاده شد.`);
      setTitle('');
      setBrand('');
      setNoBrand(false);
      setCost('');
      setWarranty('۱۸ ماه گارانتی شرکتی');
      setCategory(DEFAULT_CATEGORY);
      setCategoryAttributes(EMPTY_CATEGORY_ATTRIBUTES);
      setDescription('');
      setSpecs([{ key: 'مدل', value: '' }]);
      setStock('100');
      setPrepDays('1');
      setFulfillment('warehouse');
      setWarehouseId('');
      setVariants([]);
      setWeightGrams('');
      setLengthCm('');
      setWidthCm('');
      setHeightCm('');
      setPackLengthCm('');
      setPackWidthCm('');
      setPackHeightCm('');
      setIsOriginal(true);
      setImages([]);
      setFirstImage(null);
      setImageLink('');
      setSelectedNicheIds(visibleNiches.length > 0 ? [visibleNiches[0].id] : []);
      onSubmitted();
    } catch (cause: unknown) {
      // FIX 52 — live 409 challenge path: the server says another supplier
      // already sells this product; show the challenge box instead of a
      // generic error, keeping the proposal for the challenge submit.
      if (cause instanceof ApiError && cause.status === 409 && (cause.body as { challengeEligible?: boolean } | null)?.challengeEligible) {
        const body = cause.body as { publicProductId?: string; message?: string };
        setChallenge({ publicProductId: body.publicProductId || '', message: body.message || '' });
        setChallengeProposal(payload);
      } else {
        error(cause instanceof Error ? cause.message : 'ثبت پیشنهاد انجام نشد.');
      }
    } finally {
      setSaving(false);
    }
  };

  const submitChallenge = async () => {
    if (challengeSaving || !challengeProposal) return;
    const price = Number(challengePrice);
    if (!Number.isSafeInteger(price) || price <= 0) { error('قیمت پیشنهادی را درست وارد کن.'); return; }
    if (challengeBranch === 'same_variant' && !challengeVariantName.trim()) { error('نام تنوع را دقیقاً مثل تنوع موجود وارد کن.'); return; }
    const newNames = challengeNewNames.split(/[\n,،]/).map((n) => n.trim()).filter(Boolean);
    if (challengeBranch === 'new_variants' && newNames.length === 0) { error('حداقل نام یک تنوع جدید را وارد کن.'); return; }
    setChallengeSaving(true);
    try {
      await supplierApi.createExclusivityChallenge({
        branch: challengeBranch,
        proposedPrice: price,
        ...(challengeBranch === 'same_variant'
          ? { variantName: challengeVariantName.trim(), variantAttributes: challengeVariantColor.trim() ? { رنگ: challengeVariantColor.trim() } : {} }
          : { variantNames: newNames }),
        proposal: challengeProposal,
      });
      success('درخواست چالش ثبت شد و برای بررسی کارشناس ارسال شد.');
      setChallenge(null);
      setChallengeProposal(null);
      setChallengeVariantName('');
      setChallengeVariantColor('');
      setChallengeNewNames('');
      setChallengePrice('');
      onChallengeSubmitted();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت درخواست چالش انجام نشد.');
    } finally {
      setChallengeSaving(false);
    }
  };

  const submitWarrantyRequest = async () => {
    if (!warrantyRequestName.trim() || description.trim().length < 30 || !firstImage) {
      error('نام گارانتی، توضیح کامل (حداقل ۳۰ کاراکتر) و تصویر محصول الزامی است.');
      return;
    }
    try {
      await supplierApi.requestWarranty({ name: warrantyRequestName.trim(), description: description.trim(), images: [images[0] || firstImage] });
      success('درخواست گارانتی برای بررسی مدیر ثبت شد.');
      setRequestingWarranty(false);
      setWarranty(warrantyRequestName.trim());
      setWarrantyRequestName('');
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت درخواست گارانتی انجام نشد.');
    }
  };

  const inputClass = 'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden';
  const filledVariantCount = variants.filter((v) => v.name.trim() !== '').length;
  const filledVariantStock = variants.filter((v) => v.name.trim() !== '').reduce((sum, v) => sum + (Number(v.stock) || 0), 0);

  return (
    <>
      {challenge && (
        <div className="bg-white rounded-2xl p-3.5 border shadow-sm space-y-3" style={{ borderColor: '#5d44f8' }}>
          <div className="flex items-start gap-2">
            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" style={{ color: '#5d44f8' }} strokeWidth={1.5} />
            <div className="space-y-1">
              <h4 className="text-xs font-bold text-slate-800">این کالا قبلاً توسط تأمین‌کننده دیگری ثبت شده است</h4>
              <p className="text-[11px] leading-5 text-slate-500">{challenge.message}</p>
              <p className="text-[11px] leading-5 text-slate-500">اگر فکر می‌کنی می‌توانی همین کالا را عرضه کنی، درخواست چالش ثبت کن تا کارشناس بررسی کند. ثبت درخواست به‌معنای تأیید نیست؛ نتیجهٔ بررسی در بخش «درخواست‌های من» همین صفحه نمایش داده می‌شود.</p>
              {challenge.publicProductId && (
                <a href={`/product/${encodeURIComponent(challenge.publicProductId)}`} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-[11px] font-bold hover:underline" style={{ color: '#5d44f8' }}>
                  <ExternalLink className="w-3.5 h-3.5" strokeWidth={1.5} />
                  <span>مشاهده صفحه عمومی این کالا</span>
                </a>
              )}
            </div>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <button type="button" onClick={() => setChallengeBranch('same_variant')} className={`min-h-[44px] px-2 rounded-xl border text-[11px] font-bold ${challengeBranch === 'same_variant' ? 'text-white' : 'bg-slate-50 text-slate-600 border-slate-200'}`} style={challengeBranch === 'same_variant' ? { backgroundColor: '#5d44f8', borderColor: '#5d44f8' } : undefined}>همین تنوع — دقیقاً همین محصول با همین مشخصات، با قیمت پایین‌تر</button>
            <button type="button" onClick={() => setChallengeBranch('new_variants')} className={`min-h-[44px] px-2 rounded-xl border text-[11px] font-bold ${challengeBranch === 'new_variants' ? 'text-white' : 'bg-slate-50 text-slate-600 border-slate-200'}`} style={challengeBranch === 'new_variants' ? { backgroundColor: '#5d44f8', borderColor: '#5d44f8' } : undefined}>تنوع(های) جدید برای همین کالا</button>
          </div>
          {challengeBranch === 'same_variant' ? (
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">نام تنوع (دقیقاً مثل تنوع موجود)</label>
                <input type="text" value={challengeVariantName} onChange={(e) => setChallengeVariantName(e.target.value)} placeholder="مثال: نارنجی" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden" />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">رنگ تنوع (اگر تنوع رنگی است)</label>
                <input type="text" value={challengeVariantColor} onChange={(e) => setChallengeVariantColor(e.target.value)} placeholder="مثال: نارنجی" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden" />
              </div>
            </div>
          ) : (
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">نام تنوع‌های جدید (هر نام در یک خط)</label>
              <textarea value={challengeNewNames} onChange={(e) => setChallengeNewNames(e.target.value)} placeholder="مثال: طلایی&#10;نقره‌ای" rows={3} className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none resize-none" />
            </div>
          )}
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">قیمت پیشنهادی تو (تومان)</label>
            <input type="text" inputMode="numeric" value={formatSep(challengePrice)} onChange={(e) => setChallengePrice(parseSep(e.target.value))} placeholder="قیمت عمده پیشنهادی" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden" />
          </div>
          <div className="flex gap-2">
            <button type="button" disabled={challengeSaving} onClick={submitChallenge} className="flex-1 min-h-[44px] rounded-xl text-white text-xs font-bold disabled:opacity-50" style={{ backgroundColor: '#5d44f8' }}>{challengeSaving ? 'در حال ثبت…' : 'ثبت درخواست چالش برای بررسی کارشناس'}</button>
            <button type="button" onClick={() => setChallenge(null)} className="min-h-[44px] px-3 rounded-xl bg-slate-100 text-slate-600 text-xs font-bold">انصراف</button>
          </div>
        </div>
      )}
    <div className="bg-white rounded-2xl p-3.5 border border-brand-border shadow-sm space-y-3">
      <h4 className="text-xs font-bold text-slate-800">پیشنهاد کالای جدید</h4>
      <div>
        <label className="text-[10px] text-slate-600 block mb-1">نام یا بارکد کالا</label>
        <input type="text" placeholder="مثال: ساعت هوشمند اولترا ۴…" value={title} onChange={(e) => setTitle(e.target.value)} className={inputClass} />
      </div>
      <div>
        <label className="text-[10px] text-slate-600 block mb-1">برند محصول</label>
        <input type="text" placeholder="مثال: سامسونگ، نایکی، چرم مشهد…" value={brand} onChange={(e) => setBrand(e.target.value)} maxLength={100} disabled={noBrand} className={`${inputClass} disabled:opacity-50`} />
        <label className="flex items-center gap-1.5 mt-1.5 cursor-pointer">
          <input type="checkbox" checked={noBrand} onChange={(e) => setNoBrand(e.target.checked)} className="w-4 h-4 rounded accent-brand-deep" />
          <span className="text-[10px] text-slate-600 font-bold">بدون برند</span>
        </label>
        <span className="text-[9px] text-slate-400 block mt-1">برند در صفحه‌ی محصول و فیلترهای فروشگاه نمایش داده می‌شود؛ اگر کالا برند ندارد، «بدون برند» را بزنید.</span>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">قیمت عمده تو (تومان)</label>
          <span className="text-[9px] text-slate-400 block mb-1 leading-4">پولی که پلتفرم برای هر دانه از این کالا به تو می‌دهد. قیمت فروش به مشتری را پلتفرم جدا حساب می‌کند. مثال: می‌نویسی ۱۰۰ هزار تومان؛ هر فروش، ۱۰۰ هزار تومان برای توست.</span>
          <input type="text" inputMode="numeric" placeholder="۲٬۵۰۰٬۰۰۰" value={formatSep(cost)} onChange={(e) => setCost(parseSep(e.target.value))} className={`${inputClass} font-bold`} />
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">شرایط گارانتی</label>
          <select value={warranty} onChange={(e) => { if (e.target.value !== '__request__') setWarranty(e.target.value); else setRequestingWarranty(true); }} className={inputClass}>
            <option value={warranty}>{warranty}</option>
            {warranties.map((w) => <option key={w.id} value={w.name}>{w.name}</option>)}
            <option value="__request__">گارانتی من در لیست نیست</option>
          </select>
        </div>
      </div>
      <div className="space-y-2 pt-1 border-t border-slate-100">
        <div>
          <label className="text-[10px] font-bold text-slate-700 block mb-1">دسته‌بندی اصلی کالا:</label>
          <SupplierCategoryPicker value={category} onSelect={(cat) => { setCategory(cat); setCategoryAttributes(EMPTY_CATEGORY_ATTRIBUTES); }} />
          <SupplierCategoryAttributes
            categorySlug={category?.slug ?? null}
            value={categoryAttributes}
            onChange={setCategoryAttributes}
            onBrandSuggest={(titleFa) => { setBrand(titleFa); setNoBrand(false); }}
          />
        </div>
        <div>
          <label className="text-[10px] font-bold text-slate-700 block mb-1">موضوع کالا (نیچ) — حداقل یکی را انتخاب کن:</label>
          <span className="text-[9px] text-slate-400 block mb-1 leading-4">نیچ یعنی موضوع و دسته تخصصی کالا. فروشگاه‌ها کالاها را بر اساس همین موضوع‌ها پیدا و انتخاب می‌کنند.</span>
          {showNoMappingNote && (
            <p className="text-[10px] leading-5 bg-slate-50 border border-slate-200 text-slate-500 rounded-xl px-2.5 py-2 mb-1.5">برای این دسته نیچ تخصصی ثبت نشده است؛ همهٔ موضوع‌ها نمایش داده می‌شوند — یکی را انتخاب کن.</p>
          )}
          <div className="flex flex-wrap gap-1.5 pt-0.5">
            {visibleNiches.map((niche) => {
              const isSelected = selectedNicheIds.includes(niche.id);
              return (
                <button
                  key={niche.id}
                  type="button"
                  onClick={() => toggleNiche(niche.id)}
                  className={`px-2 py-1 rounded-lg text-[10px] font-bold transition-all flex items-center gap-1 ${isSelected ? 'bg-brand-deep text-white shadow-2xs' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'}`}
                >
                  <span>{niche.title}</span>
                </button>
              );
            })}
          </div>
        </div>
      </div>
      <div className="pt-2 border-t border-slate-100">
        <label className="text-[10px] font-bold text-slate-700 block mb-1">توضیحات کامل محصول: ({toPersianDigits(Array.from(description.trim()).length)} از حداقل ۳۰ نویسه)</label>
        <textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="توضیحات کامل درباره محصول، ویژگی‌ها، کاربردها و مزایای آن…" rows={3} className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none resize-none" />
        <span className="text-[9px] text-slate-400 block mt-1">توضیح حداقل ۳۰ نویسه الزامی است — توضیح نازک در صفحه‌ی محصول اعتماد خریدار را کم می‌کند.</span>
      </div>
      <div className="space-y-2">
        <label className="text-[10px] font-bold text-slate-700 block">مشخصات فنی محصول:</label>
        {specs.map((spec, index) => (
          <div key={index} className="grid grid-cols-2 gap-2">
            <input type="text" value={spec.key} onChange={(e) => { const next = [...specs]; next[index].key = e.target.value; setSpecs(next); }} placeholder="نام ویژگی (مثلاً: رنگ)" className="text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
            <div className="flex gap-1">
              <input type="text" value={spec.value} onChange={(e) => { const next = [...specs]; next[index].value = e.target.value; setSpecs(next); }} placeholder="مقدار (مثلاً: مشکی)" className="text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 outline-none flex-1" />
              {specs.length > 1 && (
                <button type="button" onClick={() => setSpecs(specs.filter((_, i) => i !== index))} className="px-2 py-1 bg-rose-50 text-rose-600 rounded-lg hover:bg-rose-100 transition-colors" title="حذف این ویژگی">
                  <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                </button>
              )}
            </div>
          </div>
        ))}
        <button type="button" onClick={() => setSpecs([...specs, { key: '', value: '' }])} className="text-[10px] text-brand-deep font-bold hover:underline flex items-center gap-1">
          <Plus className="w-3 h-3"  strokeWidth={1.5}/>
          افزودن ویژگی جدید
        </button>
        {topSlug === 'digital' && !specs.some((s) => s.key.trim() && s.value.trim()) && (
          <p className="text-[10px] leading-5 bg-amber-50 border border-amber-200 text-amber-800 rounded-xl px-2.5 py-2">کالای دیجیتال بدون مشخصات فنی، کمتر فروخته می‌شود؛ دست‌کم چند ویژگی کلیدی (مدل، حافظه، گارانتی…) را بنویسید.</p>
        )}
      </div>
      <div className="grid grid-cols-3 gap-2">
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">موجودی انبار:</label>
          <input type="text" inputMode="numeric" value={formatSep(stock)} onChange={(e) => setStock(parseSep(e.target.value))} placeholder="۱۰۰" min="0" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">آماده‌سازی (روز):</label>
          <input type="text" inputMode="numeric" value={formatSep(prepDays)} onChange={(e) => setPrepDays(parseSep(e.target.value))} placeholder="۱" min="1" max="30" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">روش ارسال:</label>
          <select value={fulfillment} onChange={(e) => { setFulfillment(e.target.value); setWarehouseId(''); }} className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none">
            <option value="warehouse">ارسال از انبار مرکزی</option>
            <option value="supplier_direct">ارسال مستقیم از من</option>
          </select>
        </div>
      </div>
      {fulfillment === 'supplier_direct' && (
        <div className="space-y-1.5">
          <label className="text-[10px] text-slate-600 block">مبدأ ارسال (انبار خودت):</label>
          <select value={warehouseId} onChange={(e) => setWarehouseId(e.target.value)} className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none">
            {profileCityConfirmed ? (
              <option value="">شهر انبار پروفایل{profileCity ? ` (${profileCity})` : ''}</option>
            ) : (
              <option value="">— انباری انتخاب نشده (مبدأ نامشخص) —</option>
            )}
            {myWarehouses.filter((w) => w.isActive).map((w) => (
              <option key={w.id} value={w.id}>{w.name} — {w.cityName}</option>
            ))}
          </select>
          {!profileCityConfirmed && (
            <p className="text-[10px] leading-5 bg-amber-50 border border-amber-200 text-amber-800 rounded-xl px-2.5 py-2">شهر انبار پروفایلت هنوز تأیید نشده است؛ اگر انباری انتخاب نکنی، مبدأ این کالا «انبار تأمین‌کننده» بدون شهر نمایش داده می‌شود. انبارهایت را از تب «انبارهای من» ثبت کن.</p>
          )}
          {myWarehouses.length === 0 && (
            <p className="text-[10px] text-slate-500">هنوز انباری ثبت نکرده‌ای — از تب «انبارهای من» می‌توانی کارخانه یا انبار خودت را ثبت کنی.</p>
          )}
        </div>
      )}
      <div className="space-y-2 pt-1 border-t border-slate-100">
        <label className="text-[10px] font-bold text-slate-700 block">تنوع‌های کالا (اختیاری — همراه پیشنهاد برای بررسی مدیر):</label>
        <p className="text-[10px] text-slate-500 leading-5">اگر این کالا رنگ، سایز یا مدل‌های مختلف دارد، همین‌جا تنوع‌هایش را بنویس؛ پس از تأیید مدیر، هر تنوع با کد SPNC ساخته می‌شود. موجودی هر تنوع را جدا بنویس؛ مجموع موجودی تنوع‌ها، موجودی کل کالا می‌شود. اگر قیمت عمده یک تنوع فرق می‌کند (مثلاً مشکی گران‌تر است)، قیمت همان تنوع را بنویس؛ خالی بماند یعنی همان «قیمت عمده تو» برایش حساب می‌شود.</p>
        {variants.map((variant, index) => (
          <div key={index} className="rounded-xl border border-slate-200 bg-white p-2.5 space-y-2">
            <div className="flex items-center gap-2">
              <span className="text-[10px] font-black text-slate-400 shrink-0">{toPersianDigits(index + 1)}</span>
              <input value={variant.name} onChange={(e) => { const next = [...variants]; next[index].name = e.target.value; setVariants(next); }} placeholder="نام تنوع (مثلاً: آبی / سایز ۴۲)" className="flex-1 rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
              <button type="button" onClick={() => setVariants(variants.filter((_, i) => i !== index))} className="p-2 rounded-lg bg-rose-50 text-rose-600 shrink-0" aria-label="حذف تنوع">
                <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
              </button>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <input value={variant.color} onChange={(e) => { const next = [...variants]; next[index].color = e.target.value; setVariants(next); }} placeholder="رنگ" className="rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
              <input value={variant.size} onChange={(e) => { const next = [...variants]; next[index].size = e.target.value; setVariants(next); }} placeholder="سایز" className="rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
              <input type="text" inputMode="numeric" value={formatSep(variant.stock)} onChange={(e) => { const next = [...variants]; next[index].stock = parseSep(e.target.value); setVariants(next); }} placeholder="موجودی" min="0" className="rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
              <input type="text" inputMode="numeric" value={formatSep(variant.cost)} onChange={(e) => { const next = [...variants]; next[index].cost = parseSep(e.target.value); setVariants(next); }} placeholder="قیمت عمده این تنوع (تومان، اختیاری)" className="rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
            </div>
            {variant.custom.map((attr, attrIndex) => (
              <div key={attrIndex} className="flex items-center gap-2">
                <input value={attr.key} onChange={(e) => { const next = [...variants]; next[index].custom[attrIndex].key = e.target.value; setVariants(next); }} placeholder="ویژگی (مثلاً جنس)" className="flex-1 rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
                <input value={attr.value} onChange={(e) => { const next = [...variants]; next[index].custom[attrIndex].value = e.target.value; setVariants(next); }} placeholder="مقدار" className="flex-1 rounded-xl border border-slate-200 p-2.5 text-xs bg-white focus:border-brand-deep outline-none" />
                <button type="button" onClick={() => { const next = [...variants]; next[index].custom = next[index].custom.filter((_, i) => i !== attrIndex); setVariants(next); }} className="p-2 rounded-lg bg-rose-50 text-rose-600" aria-label="حذف ویژگی">
                  <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                </button>
              </div>
            ))}
            <button type="button" onClick={() => { const next = [...variants]; next[index].custom = [...next[index].custom, { key: '', value: '' }]; setVariants(next); }} className="text-[11px] font-bold text-brand-deep hover:underline">+ افزودن ویژگی دلخواه</button>
          </div>
        ))}
        <button type="button" disabled={variants.length >= 24} onClick={() => setVariants([...variants, EMPTY_VARIANT()])} className="w-full py-2 rounded-xl border border-dashed border-brand-border text-brand-deep text-[11px] font-bold disabled:opacity-40">
          + افزودن تنوع ({toPersianDigits(variants.length)} از {toPersianDigits(24)})
        </button>
        {filledVariantCount > 0 && (
          <p className="text-[10px] leading-5 bg-brand-soft/50 border border-brand-border text-brand-deep rounded-xl px-2.5 py-2">{toPersianDigits(filledVariantCount)} تنوع با مجموع موجودی {toPersianDigits(filledVariantStock)} عدد — همین مجموع به‌عنوان موجودی کل کالا ثبت می‌شود.</p>
        )}
      </div>
      <div className="space-y-1.5 pt-1 border-t border-slate-100">
        <label className="text-[10px] font-bold text-slate-700 block">ابعاد و وزن کالا (الزامی):</label>
        <div className="grid grid-cols-4 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">وزن (گرم) — الزامی</label>
            <input type="text" inputMode="numeric" value={formatSep(weightGrams)} onChange={(e) => setWeightGrams(parseSep(e.target.value))} placeholder="۱٬۲۰۰" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">طول (سانتی‌متر) — الزامی</label>
            <input type="text" inputMode="numeric" value={formatSep(lengthCm)} onChange={(e) => setLengthCm(parseSep(e.target.value))} placeholder="۴۰" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">عرض (سانتی‌متر) — الزامی</label>
            <input type="text" inputMode="numeric" value={formatSep(widthCm)} onChange={(e) => setWidthCm(parseSep(e.target.value))} placeholder="۳۰" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">ارتفاع (سانتی‌متر) — الزامی</label>
            <input type="text" inputMode="numeric" value={formatSep(heightCm)} onChange={(e) => setHeightCm(parseSep(e.target.value))} placeholder="۲۰" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
        </div>
        <p className="text-[10px] text-slate-500 leading-5">وزن و ابعاد کالا برای تأیید پیشنهاد الزامی است؛ انبار هنگام تحویل اندازه واقعی را می‌گیرد و جایگزین می‌کند. هزینه ارسال بر اساس وزن واقعی حساب می‌شود.</p>
        <label className="text-[10px] font-bold text-slate-700 block pt-1">ابعاد بسته‌بندی (اختیاری — پیش‌فرض مرسوله):</label>
        <div className="grid grid-cols-3 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">طول بسته (سانتی‌متر)</label>
            <input type="text" inputMode="numeric" value={formatSep(packLengthCm)} onChange={(e) => setPackLengthCm(parseSep(e.target.value))} placeholder="۴۵" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">عرض بسته (سانتی‌متر)</label>
            <input type="text" inputMode="numeric" value={formatSep(packWidthCm)} onChange={(e) => setPackWidthCm(parseSep(e.target.value))} placeholder="۳۵" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">ارتفاع بسته (سانتی‌متر)</label>
            <input type="text" inputMode="numeric" value={formatSep(packHeightCm)} onChange={(e) => setPackHeightCm(parseSep(e.target.value))} placeholder="۲۵" min="1" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none" />
          </div>
        </div>
        <p className="text-[10px] text-slate-500 leading-5">هزینه پست بر اساس ابعاد بسته حساب می‌شود، نه ابعاد کالا. اگر بسته‌بندی ثابتی داری، همین‌جا پیش‌فرضش را بنویس؛ انبار هنگام بسته‌بندی آن را تأیید یا اصلاح می‌کند. خالی گذاشتنش مانع تأیید نیست.</p>
      </div>
      <div className="flex items-center gap-2 p-2.5 bg-amber-50 border border-amber-200 rounded-xl">
        <input type="checkbox" id="is-original" checked={isOriginal} onChange={(e) => setIsOriginal(e.target.checked)} className="w-4 h-4 rounded accent-amber-600" />
        <label htmlFor="is-original" className="text-[10px] text-slate-700 font-bold flex-1 cursor-pointer">
          این محصول اصل و اورجینال است
          <span className="block text-slate-500 font-normal mt-0.5">(در غیر این صورت برچسب &quot;غیر اصل&quot; روی کارت محصول نمایش داده می‌شود)</span>
        </label>
      </div>
      <div className="space-y-1.5 pt-1 border-t border-slate-100">
        <label className="text-[10px] font-bold text-slate-700 block mb-1">تصاویر محصول (حداکثر ۸ تصویر):</label>
        {uploading && (
          <div className="flex items-center justify-center gap-2 p-3 bg-brand-soft rounded-xl border border-brand-border">
            <div className="animate-spin rounded-full h-4 w-4 border-2 border-brand-deep border-t-transparent" />
            <span className="text-xs font-bold text-brand-deep">در حال آپلود تصویر در سرور…</span>
          </div>
        )}
        {images.length > 0 ? (
          <div className="space-y-2">
            <div className="grid grid-cols-4 gap-2">
              {images.map((src, i) => (
                <div key={i} className="relative aspect-square rounded-xl overflow-hidden border border-slate-200 bg-white group">
                  {src ? (
                    <img src={src} alt={`تصویر ${i + 1}`} className="w-full h-full object-cover" />
                  ) : (
                    <div className="w-full h-full flex items-center justify-center bg-slate-50">
                      <ImagePlus className="w-6 h-6 text-slate-300"  strokeWidth={1.5}/>
                    </div>
                  )}
                  {i === 0 && (
                    <span className="absolute top-1 right-1 bg-amber-400 text-amber-950 text-[8px] font-bold px-1.5 py-0.5 rounded-full">شاخص</span>
                  )}
                  <button type="button" onClick={() => removeImage(i)} className="absolute top-1 left-1 p-1 rounded-lg bg-rose-500 text-white opacity-0 group-hover:opacity-100 transition-opacity" title="حذف">
                    <Trash2 className="w-3 h-3"  strokeWidth={1.5}/>
                  </button>
                </div>
              ))}
              {images.length < 8 && (
                <label htmlFor="product-image-input" className={`aspect-square rounded-xl border-2 border-dashed border-brand-border hover:border-brand-deep bg-brand-soft/40 flex flex-col items-center justify-center gap-1 text-brand-deep transition-colors ${uploading ? 'opacity-50 pointer-events-none' : 'cursor-pointer'}`}>
                  <ImagePlus className="w-5 h-5"  strokeWidth={1.5}/>
                  <span className="text-[9px] font-bold">افزودن</span>
                  <input id="product-image-input" type="file" accept="image/jpeg,image/png,image/webp" multiple disabled={uploading} onChange={onFiles} className="hidden" />
                </label>
              )}
            </div>
            <p className="text-[10px] text-slate-400">{toPersianDigits(images.length)} تصویر بارگذاری شد • تصویر اول به‌عنوان شاخص استفاده می‌شود</p>
            {images.length === 1 && (
              <p className="text-[10px] leading-5 bg-amber-50 border border-amber-200 text-amber-800 rounded-xl px-2.5 py-2">گالری این کالا فعلاً تک‌تصویری است؛ چند زاویه و نمای نزدیک اضافه کنید تا خریدار بهتر ببیند.</p>
            )}
          </div>
        ) : (
          <div className="space-y-2">
            <label htmlFor="product-image-input" className={`flex flex-col items-center justify-center p-3.5 border-2 border-dashed border-slate-200 hover:border-indigo-400 rounded-xl bg-slate-50 hover:bg-brand-soft/30 transition-all text-center ${uploading ? 'opacity-50 pointer-events-none cursor-not-allowed' : 'cursor-pointer'}`}>
              <div className="flex items-center gap-2 text-brand-deep font-bold text-xs mb-1">
                <ImagePlus className="w-4 h-4"  strokeWidth={1.5}/>
                <span>انتخاب تصاویر محصول از دستگاه</span>
              </div>
              <span className="text-[10px] text-slate-400">فرمت‌های مجاز: JPG, PNG, WEBP (حداکثر ۲ مگابایت) • تا ۸ تصویر</span>
              <input id="product-image-input" type="file" accept="image/jpeg,image/png,image/webp" multiple disabled={uploading} onChange={onFiles} className="hidden" />
            </label>
            <div className="flex items-center gap-1.5">
              <input type="text" value={imageLink} onChange={(e) => setImageLink(e.target.value)} placeholder="یا درج لینک تصویر اینترنتی (https://…)" className="w-full text-[11px] p-2 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-mono" dir="ltr" />
            </div>
          </div>
        )}
      </div>
      <button onClick={submit} disabled={saving || uploading} className="w-full py-2 min-h-[44px] bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl transition-colors shadow-2xs">
        فرستادن برای بررسی مدیر
      </button>
      {requestingWarranty && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4">
          <div className="w-full max-w-sm space-y-3 rounded-2xl bg-white p-4">
            <h3 className="text-sm font-bold">گارانتی من در لیست نیست</h3>
            <input aria-label="نام گارانتی جدید" value={warrantyRequestName} onChange={(e) => setWarrantyRequestName(e.target.value)} placeholder="نام گارانتی پیشنهادی" className="w-full rounded-xl border p-2 text-xs" />
            <p className="text-[11px] text-slate-500">توضیحات کامل محصول از فرم پیشنهاد استفاده می‌شود و حداقل ۳۰ کاراکتر لازم است. تصویر محصول نیز باید بارگذاری شده باشد.</p>
            <div className="flex gap-2">
              <button type="button" onClick={submitWarrantyRequest} className="flex-1 rounded-xl bg-brand-deep p-2 text-xs font-bold text-white">ارسال برای بررسی مدیر</button>
              <button type="button" onClick={() => setRequestingWarranty(false)} className="rounded-xl bg-slate-100 p-2 text-xs">انصراف</button>
            </div>
          </div>
        </div>
      )}
    </div>
    </>
  );
};
