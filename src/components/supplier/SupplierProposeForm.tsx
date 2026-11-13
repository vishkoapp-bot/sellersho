import React, { useState, useEffect, useRef } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { catalogApi, NicheDto } from '../../services/catalogApi';
import { getApiBase, ApiError } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { SupplierCategoryPicker, CategorySelection } from './SupplierCategoryPicker';
import { SupplierProductDefinitionEditor } from './SupplierProductDefinitionEditor';
import type { ProductSchema } from '../../types/productSchema';
import { buildProductMatrix, serializeProductDraft, DraftProductSKU, selectionKey } from '../../services/productModel';
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

// ---- Wizard: draft autosave + per-step validation (SPEC launch-readiness/ux-wizard) ----

const DRAFT_KEY = 'sellersho:supplier-propose-draft:v1';
const DRAFT_VERSION = 1;

type WizardStep = 1 | 2 | 3 | 4;
const STEP_LABELS = ['کالا و دسته', 'ویژگی‌ها', 'قیمت و ارسال', 'تصویر و بازبینی'];

export interface ProposeStepError {
  field: string;
  message: string;
}

interface ProposeDraftV1 {
  version: number;
  savedAt: string;
  step: number;
  title: string;
  brand: string;
  noBrand: boolean;
  cost: string;
  category: CategorySelection | null;
  selectedNicheIds: string[];
  description: string;
  stock: string;
  prepDays: string;
  fulfillment: string;
  warehouseId: string;
  productType: 'simple' | 'variable';
  productSchema: ProductSchema;
  variants: DraftProductSKU[];
  weightGrams: string;
  lengthCm: string;
  widthCm: string;
  heightCm: string;
  packLengthCm: string;
  packWidthCm: string;
  packHeightCm: string;
  isOriginal: boolean;
  images: string[];
  imageLink: string;
}

const normalizeDraftRow = (v: Partial<DraftProductSKU>): DraftProductSKU => ({
  id: '',
  selection: {},
  name: '',
  cost: '',
  stock: '',
  active: true,
  imageUrl: '',
  weightGrams: '',
  lengthCm: '',
  widthCm: '',
  heightCm: '',
  salePrice: '',
  saleFrom: '',
  saleTo: '',
  shippingClass: '',
  taxClass: '',
  ...v,
  selected: v.selected ?? true,
  isDefault: v.isDefault ?? false,
  images: v.images ?? [],
});

function readDraftFromStorage(): ProposeDraftV1 | null {
  try {
    const raw = localStorage.getItem(DRAFT_KEY);
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null || (parsed as { version?: unknown }).version !== DRAFT_VERSION) return null;
    return parsed as ProposeDraftV1;
  } catch {
    return null;
  }
}

// Step 1 gate — a re-partition of submit()'s checks, in the same order.
// Pure function (exported for tests); the component only renders its errors.
export function validateProposeStep1(input: {
  title: string;
  brand: string;
  noBrand: boolean;
  category: CategorySelection | null;
  description: string;
  selectedNicheIds: string[];
  nichesReady: boolean;
}): ProposeStepError[] {
  const errors: ProposeStepError[] = [];
  if (!input.title.trim()) errors.push({ field: 'title', message: 'لطفاً نام کالا را وارد کنید.' });
  if (input.brand.trim().length > 100) errors.push({ field: 'brand', message: 'نام برند نمی‌تواند بیشتر از ۱۰۰ نویسه باشد.' });
  if (!input.category || input.category.isLeaf === false) errors.push({ field: 'category', message: 'دسته‌بندی کالا را انتخاب کنید.' });
  if (!input.noBrand && !input.brand.trim()) errors.push({ field: 'brand', message: 'برند کالا را وارد کنید یا گزینه «بدون برند» را انتخاب کنید.' });
  if (Array.from(input.description.trim()).length < 30) errors.push({ field: 'description', message: 'توضیحات محصول باید حداقل ۳۰ نویسه باشد.' });
  if (input.nichesReady && input.selectedNicheIds.length !== 1) errors.push({ field: 'niche', message: 'حداقل یک موضوع (نیچ) برای این کالا انتخاب کن.' });
  return errors;
}

// Step 3 gate — weight/dims + packaging, same messages as submit().
export function validateProposeStep3(input: {
  weightGrams: string;
  lengthCm: string;
  widthCm: string;
  heightCm: string;
  packLengthCm: string;
  packWidthCm: string;
  packHeightCm: string;
}): ProposeStepError[] {
  const errors: ProposeStepError[] = [];
  const posInt = (raw: string): number | undefined => {
    const n = Number(raw);
    return raw.trim() !== '' && Number.isSafeInteger(n) && n > 0 ? n : undefined;
  };
  if (posInt(input.weightGrams) === undefined || posInt(input.lengthCm) === undefined || posInt(input.widthCm) === undefined || posInt(input.heightCm) === undefined) {
    errors.push({ field: 'weightDims', message: 'وزن (گرم) و هر سه بُعد کالا (سانتی‌متر) الزامی است؛ بدون آن‌ها پیشنهاد تأیید نمی‌شود.' });
  }
  const packParts = [input.packLengthCm, input.packWidthCm, input.packHeightCm];
  const packFilled = packParts.filter((p) => p.trim() !== '').length;
  if (packFilled > 0 && packFilled < 3) {
    errors.push({ field: 'packaging', message: 'ابعاد بسته‌بندی را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).' });
  } else if (packFilled === 3 && (posInt(input.packLengthCm) === undefined || posInt(input.packWidthCm) === undefined || posInt(input.packHeightCm) === undefined)) {
    errors.push({ field: 'packaging', message: 'ابعاد بسته‌بندی باید عددهای صحیح مثبت باشند (سانتی‌متر).' });
  }
  return errors;
}

// Step 2 staleness — mirrors the editor's own «ویژگی‌ها تغییر کرده‌اند»
// warning: the saved rows no longer match what the schema would build.
// serializeProductDraft throws the same message for selected rows, but a
// stale matrix can still serialise when the saved rows are a subset, so
// this is checked separately after serialize passes.
export function isProposeMatrixStale(schema: ProductSchema, rows: DraftProductSKU[]): boolean {
  try {
    const generated = buildProductMatrix({ ...schema, type: schema.type }, rows);
    return generated.length !== rows.length || !generated.every((g) => rows.some((v) => selectionKey(v.selection) === selectionKey(g.selection)));
  } catch {
    return false;
  }
}

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

  const [restoredDraft] = useState<ProposeDraftV1 | null>(readDraftFromStorage);
  const [step, setStep] = useState<WizardStep>(() => {
    const s = restoredDraft?.step;
    return s === 1 || s === 2 || s === 3 || s === 4 ? s : 1;
  });
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [step2Error, setStep2Error] = useState<string | null>(null);
  const fieldRefs = useRef<Record<string, HTMLElement | null>>({});
  const step2AlertRef = useRef<HTMLParagraphElement | null>(null);
  const suppressAutosaveRef = useRef(false);

  const [title, setTitle] = useState(restoredDraft?.title ?? '');
  const [brand, setBrand] = useState(restoredDraft?.brand ?? '');
  const [noBrand, setNoBrand] = useState(restoredDraft?.noBrand ?? false);
  const [cost, setCost] = useState(restoredDraft?.cost ?? '');
  const [warranty, setWarranty] = useState('۱۸ ماه گارانتی شرکتی');
  const [requestingWarranty, setRequestingWarranty] = useState(false);
  const [warrantyRequestName, setWarrantyRequestName] = useState('');
  const [category, setCategory] = useState<CategorySelection | null>(() => {
    const c = restoredDraft?.category;
    return c && typeof c.slug === 'string' && Array.isArray(c.path) ? c : null;
  });
  const [allNiches, setAllNiches] = useState<NicheDto[]>([]);
  const [selectedNicheIds, setSelectedNicheIds] = useState<string[]>(() => {
    const ids = restoredDraft?.selectedNicheIds;
    return Array.isArray(ids) && ids.length > 0 && ids.every((id) => typeof id === 'string') ? ids : ['niche-smartwatch'];
  });
  const [description, setDescription] = useState(restoredDraft?.description ?? '');
  const [stock, setStock] = useState(restoredDraft?.stock ?? '100');
  const [prepDays, setPrepDays] = useState(restoredDraft?.prepDays ?? '1');
  const [fulfillment, setFulfillment] = useState(restoredDraft?.fulfillment ?? 'warehouse');
  const [warehouseId, setWarehouseId] = useState(restoredDraft?.warehouseId ?? '');
  const [productType, setProductType] = useState<'simple' | 'variable'>(restoredDraft?.productType === 'variable' ? 'variable' : 'simple');
  const [productSchema,setProductSchema]=useState<ProductSchema>(() => {
    const s = restoredDraft?.productSchema;
    return s && Array.isArray(s.attributes) ? s : {version:2,type:'simple',attributes:[]};
  });
  const [variants,setVariants]=useState<DraftProductSKU[]>(() => {
    const rows = restoredDraft?.variants;
    return Array.isArray(rows) && rows.length > 0 ? rows.map(normalizeDraftRow) : buildProductMatrix({version:2,type:'simple',attributes:[]},[]);
  });
  // Restored schema/variants must survive the mount run of the reset
  // effect below (it would otherwise rebuild an empty matrix). Skip the
  // reset only while category+type still match the restored draft; any
  // later change resets exactly as before. Idempotent, so StrictMode's
  // double effect invocation on mount cannot wipe the draft either.
  const restoredSchemaKeyRef = useRef(restoredDraft ? `${restoredDraft.category?.slug ?? ''}|${restoredDraft.productType ?? 'simple'}` : null);
  useEffect(()=>{const key=`${category?.slug ?? ''}|${productType}`;if(restoredSchemaKeyRef.current!==null&&restoredSchemaKeyRef.current===key)return;const schema:ProductSchema={version:2,type:productType,attributes:[]};setProductSchema(schema);setVariants(productType==='simple'?buildProductMatrix(schema,[]):[]);},[category?.slug,productType]);
  const [weightGrams, setWeightGrams] = useState(restoredDraft?.weightGrams ?? '');
  const [lengthCm, setLengthCm] = useState(restoredDraft?.lengthCm ?? '');
  const [widthCm, setWidthCm] = useState(restoredDraft?.widthCm ?? '');
  const [heightCm, setHeightCm] = useState(restoredDraft?.heightCm ?? '');
  const [packLengthCm, setPackLengthCm] = useState(restoredDraft?.packLengthCm ?? '');
  const [packWidthCm, setPackWidthCm] = useState(restoredDraft?.packWidthCm ?? '');
  const [packHeightCm, setPackHeightCm] = useState(restoredDraft?.packHeightCm ?? '');
  const [isOriginal, setIsOriginal] = useState(restoredDraft?.isOriginal ?? true);
  const [images, setImages] = useState<string[]>(() => {
    const list = restoredDraft?.images;
    return Array.isArray(list) ? list.filter((u) => typeof u === 'string') : [];
  });
  const [firstImage, setFirstImage] = useState<string | null>(() => {
    const list = restoredDraft?.images;
    return Array.isArray(list) && typeof list[0] === 'string' ? list[0] : null;
  });
  const [imageLink, setImageLink] = useState(restoredDraft?.imageLink ?? '');
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
  // Step-1 brand options: same endpoint the editor uses for its brand list.
  const [step1Brands, setStep1Brands] = useState<{ id: number; titleFa: string; titleEn: string }[]>([]);
  const [brandsLoading, setBrandsLoading] = useState(false);

  useEffect(() => {
    catalogApi.getNiches().then(setAllNiches).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!category?.isLeaf || !category.slug) { setStep1Brands([]); setBrandsLoading(false); return; }
    let active = true;
    setBrandsLoading(true);
    catalogApi.getProductAttributes(category.slug).then((data) => { if (active) setStep1Brands(data.brands); }).catch(() => { if (active) setStep1Brands([]); }).finally(() => { if (active) setBrandsLoading(false); });
    return () => { active = false; };
  }, [category?.slug, category?.isLeaf]);

  // Draft autosave: debounced ~500ms, try/catch — storage failures are
  // swallowed, never toasted. A pristine (untouched) form removes the key
  // instead of writing, so a successful submit / manual clear is never
  // resurrected by a trailing write.
  useEffect(() => {
    const pristine = step === 1 && title === '' && brand === '' && !noBrand && category === null && description === '' && stock === '100' && prepDays === '1' && fulfillment === 'warehouse' && warehouseId === '' && productType === 'simple' && productSchema.attributes.length === 0 && variants.length === 1 && variants[0]?.cost === '' && variants[0]?.stock === '' && weightGrams === '' && lengthCm === '' && widthCm === '' && heightCm === '' && packLengthCm === '' && packWidthCm === '' && packHeightCm === '' && isOriginal && images.length === 0 && imageLink === '';
    if (pristine) {
      try { localStorage.removeItem(DRAFT_KEY); } catch { /* storage unavailable */ }
      return;
    }
    const draft: ProposeDraftV1 = { version: DRAFT_VERSION, savedAt: new Date().toISOString(), step, title, brand, noBrand, cost, category, selectedNicheIds, description, stock, prepDays, fulfillment, warehouseId, productType, productSchema, variants, weightGrams, lengthCm, widthCm, heightCm, packLengthCm, packWidthCm, packHeightCm, isOriginal, images, imageLink };
    const timer = setTimeout(() => {
      if (suppressAutosaveRef.current) { suppressAutosaveRef.current = false; return; }
      try { localStorage.setItem(DRAFT_KEY, JSON.stringify(draft)); } catch { /* quota / storage unavailable — never toast */ }
    }, 500);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, title, brand, noBrand, cost, category, selectedNicheIds, description, stock, prepDays, fulfillment, warehouseId, productType, productSchema, variants, weightGrams, lengthCm, widthCm, heightCm, packLengthCm, packWidthCm, packHeightCm, isOriginal, images, imageLink]);

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
      return kept.length > 0 ? [kept[0]] : [visibleNiches[0].id];
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [topSlug]);

  const toggleNiche = (id: string) =>
    setSelectedNicheIds([id]);

  // ---- Per-step validation helpers ----
  const refFor = <T extends HTMLElement>(field: string) => (el: T | null) => { fieldRefs.current[field] = el; };
  const clearFieldError = (field: string) => {
    setFieldErrors((prev) => {
      if (!(field in prev)) return prev;
      const next = { ...prev };
      delete next[field];
      return next;
    });
  };
  const fieldError = (field: string) => fieldErrors[field] ? <p id={`propose-err-${field}`} role="alert" className="text-[11px] text-rose-700">{fieldErrors[field]}</p> : null;
  const invalidClass = (field: string) => fieldErrors[field] ? ' border-rose-400' : '';
  const invalidProps = (field: string) => fieldErrors[field]
    ? { 'aria-invalid': true as const, 'aria-describedby': `propose-err-${field}` }
    : { 'aria-invalid': false as const };
  const focusField = (field: string) => {
    setTimeout(() => {
      const el = fieldRefs.current[field];
      if (!el) return;
      el.scrollIntoView({ block: 'center' });
      el.focus({ preventScroll: true });
    }, 0);
  };
  const applyFieldErrors = (errs: ProposeStepError[]) => {
    const map: Record<string, string> = {};
    errs.forEach((e) => { map[e.field] = e.message; });
    setFieldErrors(map);
    error(errs[0].message);
    focusField(errs[0].field);
  };
  const blockStep2 = (msg: string) => {
    setStep2Error(msg);
    error(msg);
    setTimeout(() => {
      const row = variants.find((v) => msg.includes(`«${v.name}»`));
      const target = row ? document.querySelector(`[data-sku-id="${row.id}"]`) : step2AlertRef.current;
      target?.scrollIntoView({ block: 'center' });
    }, 0);
  };
  const goBack = () => {
    setFieldErrors({});
    setStep2Error(null);
    setStep((s) => (s > 1 ? ((s - 1) as WizardStep) : s));
  };
  const goNext = () => {
    if (step === 1) {
      const errs = validateProposeStep1({ title, brand, noBrand, category, description, selectedNicheIds, nichesReady: visibleNiches.length > 0 });
      if (errs.length > 0) { applyFieldErrors(errs); return; }
      setFieldErrors({});
      setStep(2);
      return;
    }
    if (step === 2) {
      try {
        serializeProductDraft(productSchema, variants);
      } catch (cause) {
        blockStep2(cause instanceof Error ? cause.message : 'ساختار محصول را کامل کنید.');
        return;
      }
      if (isProposeMatrixStale(productSchema, variants)) {
        blockStep2('پس از تغییر ویژگی‌ها، ترکیب‌ها را به‌روز کنید.');
        return;
      }
      setStep2Error(null);
      setStep(3);
      return;
    }
    if (step === 3) {
      const errs = validateProposeStep3({ weightGrams, lengthCm, widthCm, heightCm, packLengthCm, packWidthCm, packHeightCm });
      if (errs.length > 0) { applyFieldErrors(errs); return; }
      setFieldErrors({});
      setStep(4);
    }
  };
  const clearDraft = () => {
    try { localStorage.removeItem(DRAFT_KEY); } catch { /* storage unavailable */ }
    setStep(1);
    setFieldErrors({});
    setStep2Error(null);
    setTitle('');
    setBrand('');
    setNoBrand(false);
    setCost('');
    setWarranty('۱۸ ماه گارانتی شرکتی');
    setCategory(null);
    setDescription('');
    setStock('100');
    setPrepDays('1');
    setFulfillment('warehouse');
    setWarehouseId('');
    setProductType('simple');
    setProductSchema({version:2,type:'simple',attributes:[]});
    setVariants(buildProductMatrix({version:2,type:'simple',attributes:[]},[]));
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
  };

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
          signal: AbortSignal.timeout(60000),
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
    if (!title.trim()) { error('لطفاً نام کالا را وارد کنید.'); return; }
    if (brand.trim().length > 100) { error('نام برند نمی‌تواند بیشتر از ۱۰۰ نویسه باشد.'); return; }
    if (!category || category.isLeaf === false) { error('دسته‌بندی کالا را انتخاب کنید.'); return; }
    if (Array.from(description.trim()).length < 30) { error('توضیحات محصول باید حداقل ۳۰ نویسه باشد.'); return; }
    if (!noBrand && !brand.trim()) { error('برند کالا را وارد کنید یا گزینه «بدون برند» را انتخاب کنید.'); return; }
    let productDraft:ReturnType<typeof serializeProductDraft>;
    try{productDraft=serializeProductDraft(productSchema,variants)}catch(cause){error(cause instanceof Error?cause.message:'ساختار محصول را کامل کنید.');return;}
    const costNum=productDraft.supplierBaseCost;
    if (selectedNicheIds.length !== 1) { error('حداقل یک موضوع (نیچ) برای این کالا انتخاب کن.'); return; }
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
    const specsObject=Object.fromEntries(productSchema.attributes.filter(a=>a.role==='fixed').map(a=>[a.name,a.values[0]?.label??'']));
    const variantPayload=productDraft.variants;
    const variantStockSum=productDraft.stock;
    const payload: Record<string, unknown> = {
      ...productDraft,
      title: title.trim(),
      brand: noBrand ? '' : brand.trim(),
      noBrand,
      supplierBaseCost: costNum,
      categorySlug: category.slug,
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
      setCategory(null);
      setDescription('');
      setStock('100');
      setPrepDays('1');
      setFulfillment('warehouse');
      setWarehouseId('');
      setProductSchema({version:2,type:'simple',attributes:[]});setVariants(buildProductMatrix({version:2,type:'simple',attributes:[]},[]));
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
      try { localStorage.removeItem(DRAFT_KEY); } catch { /* storage unavailable */ }
      setStep(1);
      setFieldErrors({});
      setStep2Error(null);
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
      try { localStorage.removeItem(DRAFT_KEY); } catch { /* storage unavailable */ }
      suppressAutosaveRef.current = true;
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

  // ---- Step 4 review summary (read-only) ----
  const nicheTitle = allNiches.find((n) => n.id === selectedNicheIds[0])?.title ?? selectedNicheIds[0] ?? '—';
  const reviewVariantCount = variants.filter((v) => v.selected !== false).length;
  const reviewActiveCosts = variants.filter((v) => v.active && v.selected !== false).map((v) => Number(v.cost)).filter((n) => Number.isSafeInteger(n) && n > 0);
  const reviewMinCost = reviewActiveCosts.length > 0 ? Math.min(...reviewActiveCosts) : null;
  const reviewWarehouse = fulfillment === 'supplier_direct'
    ? (myWarehouses.find((w) => w.id === warehouseId)?.name ?? (profileCityConfirmed ? `شهر انبار پروفایل${profileCity ? ` (${profileCity})` : ''}` : '—'))
    : 'ارسال از انبار مرکزی';
  const descChars = Array.from(description.trim());
  const descPreview = descChars.slice(0, 120).join('') + (descChars.length > 120 ? '…' : '');
  const reviewRow = (label: string, value: React.ReactNode, editStep: WizardStep) => (
    <div className="flex items-start justify-between gap-2 text-xs">
      <span className="text-slate-500 shrink-0">{label}</span>
      <span className="text-slate-800 flex-1 text-left break-words">{value}</span>
      <button type="button" onClick={() => { setFieldErrors({}); setStep2Error(null); setStep(editStep); }} className="text-[11px] font-bold text-violet-700 shrink-0">ویرایش</button>
    </div>
  );

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
        <div className="flex items-center" aria-label="مراحل پیشنهاد کالا">
          {STEP_LABELS.map((label, i) => {
            const n = (i + 1) as WizardStep;
            const done = n < step;
            const current = n === step;
            const circle = (
              <span className={`w-7 h-7 rounded-full flex items-center justify-center text-[11px] font-bold shrink-0 ${current ? 'text-white' : done ? 'bg-emerald-500 text-white' : 'bg-slate-200 text-slate-500'}`} style={current ? { backgroundColor: '#5d44f8' } : undefined}>
                {done ? '✓' : toPersianDigits(n)}
              </span>
            );
            return (
              <React.Fragment key={label}>
                {i > 0 && <div className={`h-px flex-1 ${i < step ? 'bg-emerald-500' : 'bg-slate-200'}`} />}
                {done ? (
                  <button type="button" onClick={() => { setFieldErrors({}); setStep2Error(null); setStep(n); }} aria-label={`بازگشت به مرحلهٔ ${label}`} className="shrink-0 rounded-full">{circle}</button>
                ) : (
                  <span className="shrink-0 rounded-full" aria-current={current ? 'step' : undefined}>{circle}</span>
                )}
              </React.Fragment>
            );
          })}
        </div>
        <div className="grid grid-cols-4 mt-1">
          {STEP_LABELS.map((label, i) => (
            <span key={label} className={`text-[10px] text-center leading-4 ${i + 1 === step ? 'font-bold text-slate-800' : 'text-slate-500'}`}>{label}</span>
          ))}
        </div>
        <p className="text-[11px] text-slate-500 mt-2">مرحلهٔ {toPersianDigits(step)} از ۴ — {STEP_LABELS[step - 1]}</p>
      </div>

      {step === 1 && (
        <div className="space-y-3">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">نام کالا</label>
            <input ref={refFor('title')} type="text" placeholder="مثال: ساعت هوشمند اولترا ۴…" value={title} onChange={(e) => { setTitle(e.target.value); clearFieldError('title'); }} className={inputClass + invalidClass('title')} {...invalidProps('title')} />
            {fieldError('title')}
          </div>
          <div className="space-y-2 pt-1 border-t border-slate-100">
            <div>
              <label className="text-[10px] font-bold text-slate-700 block mb-1">دسته‌بندی اصلی کالا:</label>
              <div ref={refFor('category')} tabIndex={-1} className="outline-none">
                <SupplierCategoryPicker value={category} onSelect={(cat) => { setCategory(cat); setBrand(''); clearFieldError('category'); clearFieldError('brand'); }} />
              </div>
              {fieldError('category')}
            </div>
            <div>
              <label className="text-[10px] font-bold text-slate-700 block mb-1">برند</label>
              <select ref={refFor('brand')} aria-label="برند محصول" disabled={brandsLoading || noBrand || !category?.isLeaf} value={brand} onChange={(e) => { setBrand(e.target.value); setNoBrand(false); clearFieldError('brand'); }} className={inputClass + invalidClass('brand')} {...invalidProps('brand')}>
                <option value="">انتخاب برند همین دسته</option>
                {step1Brands.map((b) => <option key={b.id} value={b.titleFa}>{b.titleFa}{b.titleEn && b.titleEn !== b.titleFa ? ` (${b.titleEn})` : ''}</option>)}
              </select>
              {fieldError('brand')}
              <label className="flex gap-2 text-xs mt-1.5"><input type="checkbox" checked={noBrand} onChange={(e) => { setNoBrand(e.target.checked); clearFieldError('brand'); }} />بدون برند</label>
            </div>
            <div>
              <label className="text-[10px] font-bold text-slate-700 block mb-1">موضوع کالا (نیچ) — فقط یک موضوع انتخاب کن:</label>
              <span className="text-[9px] text-slate-400 block mb-1 leading-4">نیچ یعنی موضوع و دسته تخصصی کالا. فروشگاه‌ها کالاها را بر اساس همین موضوع‌ها پیدا و انتخاب می‌کنند.</span>
              {showNoMappingNote && (
                <p className="text-[10px] leading-5 bg-slate-50 border border-slate-200 text-slate-500 rounded-xl px-2.5 py-2 mb-1.5">برای این دسته نیچ تخصصی ثبت نشده است؛ همهٔ موضوع‌ها نمایش داده می‌شوند — یکی را انتخاب کن.</p>
              )}
              <div ref={refFor('niche')} tabIndex={-1} role="radiogroup" aria-label="موضوع کالا" aria-invalid={fieldErrors.niche ? true : false} className="flex flex-wrap gap-1.5 pt-0.5 outline-none">
                {visibleNiches.map((niche) => {
                  const isSelected = selectedNicheIds.includes(niche.id);
                  return (
                    <button
                      key={niche.id}
                      type="button"
                      role="radio"
                      aria-checked={isSelected}
                      onClick={() => { toggleNiche(niche.id); clearFieldError('niche'); }}
                      className={`px-2 py-1 rounded-lg text-[10px] font-bold transition-all flex items-center gap-1 ${isSelected ? 'bg-brand-deep text-white shadow-2xs' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'}`}
                    >
                      <span>{niche.title}</span>
                    </button>
                  );
                })}
              </div>
              {fieldError('niche')}
            </div>
          </div>
          <div className="pt-2 border-t border-slate-100">
            <label className="text-[10px] font-bold text-slate-700 block mb-1">توضیحات کامل محصول: ({toPersianDigits(Array.from(description.trim()).length)} از حداقل ۳۰ نویسه)</label>
            <textarea ref={refFor('description')} value={description} onChange={(e) => { setDescription(e.target.value); clearFieldError('description'); }} placeholder="توضیحات کامل درباره محصول، ویژگی‌ها، کاربردها و مزایای آن…" rows={3} className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none resize-none' + invalidClass('description')} {...invalidProps('description')} />
            {fieldError('description')}
            <span className="text-[9px] text-slate-400 block mt-1">توضیح حداقل ۳۰ نویسه الزامی است — توضیح نازک در صفحه‌ی محصول اعتماد خریدار را کم می‌کند.</span>
          </div>
          <button type="button" onClick={clearDraft} className="text-[11px] text-rose-700 underline">پاک کردن پیش‌نویس و شروع دوباره</button>
        </div>
      )}

      {step === 2 && (
        <div className="space-y-3">
          <fieldset className="rounded-xl border p-3 space-y-2">
            <legend className="text-xs font-bold">نوع محصول</legend>
            <label className="inline-flex items-center gap-2 ml-4 text-xs"><input type="radio" name="productType" checked={productType === 'simple'} onChange={() => setProductType('simple')} />محصول تکی — یک قیمت و موجودی</label>
            <label className="inline-flex items-center gap-2 text-xs"><input type="radio" name="productType" checked={productType === 'variable'} onChange={() => setProductType('variable')} />محصول متغیر — قیمت و موجودی برای هر تنوع</label>
          </fieldset>
          {step2Error && <p ref={step2AlertRef} role="alert" className="text-[11px] leading-5 text-rose-700 bg-rose-50 border border-rose-200 rounded-xl px-2.5 py-2">{step2Error}</p>}
          <SupplierProductDefinitionEditor categorySlug={category?.isLeaf===false?null:category?.slug??null} productType={productType} schema={productSchema} onSchema={(s) => { setProductSchema(s); setStep2Error(null); }} rows={variants} onRows={(r) => { setVariants(r); setStep2Error(null); }} brand={brand} noBrand={noBrand} onBrand={value=>{setBrand(value);setNoBrand(false)}} onNoBrand={setNoBrand} sessionToken={sessionToken} hideBrand />
        </div>
      )}

      {step === 3 && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">شرایط گارانتی</label>
              <select value={warranty} onChange={(e) => { if (e.target.value !== '__request__') setWarranty(e.target.value); else setRequestingWarranty(true); }} className={inputClass}>
                <option value={warranty}>{warranty}</option>
                {warranties.map((w) => <option key={w.id} value={w.name}>{w.name}</option>)}
                <option value="__request__">گارانتی من در لیست نیست</option>
              </select>
            </div>
          </div>
          <div className="grid grid-cols-3 gap-2">
            <div>
              <label className="text-[10px] text-slate-600 block mb-1">موجودی انبار:</label>
              <p className="text-xs p-2.5">{toPersianDigits(filledVariantStock)} (مجموع موجودی ردیف‌ها)</p>
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
          <div className="space-y-1.5 pt-1 border-t border-slate-100">
            <label className="text-[10px] font-bold text-slate-700 block">ابعاد و وزن کالا (الزامی):</label>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">وزن (گرم) — الزامی</label>
                <input ref={refFor('weightDims')} type="text" inputMode="numeric" value={formatSep(weightGrams)} onChange={(e) => { setWeightGrams(parseSep(e.target.value)); clearFieldError('weightDims'); }} placeholder="۱٬۲۰۰" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('weightDims')} {...invalidProps('weightDims')} />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">طول (سانتی‌متر) — الزامی</label>
                <input type="text" inputMode="numeric" value={formatSep(lengthCm)} onChange={(e) => { setLengthCm(parseSep(e.target.value)); clearFieldError('weightDims'); }} placeholder="۴۰" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('weightDims')} {...invalidProps('weightDims')} />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">عرض (سانتی‌متر) — الزامی</label>
                <input type="text" inputMode="numeric" value={formatSep(widthCm)} onChange={(e) => { setWidthCm(parseSep(e.target.value)); clearFieldError('weightDims'); }} placeholder="۳۰" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('weightDims')} {...invalidProps('weightDims')} />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">ارتفاع (سانتی‌متر) — الزامی</label>
                <input type="text" inputMode="numeric" value={formatSep(heightCm)} onChange={(e) => { setHeightCm(parseSep(e.target.value)); clearFieldError('weightDims'); }} placeholder="۲۰" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('weightDims')} {...invalidProps('weightDims')} />
              </div>
            </div>
            {fieldError('weightDims')}
            <p className="text-[10px] text-slate-500 leading-5">وزن و ابعاد کالا برای تأیید پیشنهاد الزامی است؛ انبار هنگام تحویل اندازه واقعی را می‌گیرد و جایگزین می‌کند. هزینه ارسال بر اساس وزن واقعی حساب می‌شود. اگر تنوعی وزن یا ابعاد متفاوتی دارد، همان را در جزئیات همان تنوع بنویس؛ خالی بماند، همین وزن و ابعاد برای آن تنوع حساب می‌شود.</p>
            <label className="text-[10px] font-bold text-slate-700 block pt-1">ابعاد بسته‌بندی (اختیاری — پیش‌فرض مرسوله):</label>
            <div className="grid grid-cols-3 gap-2">
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">طول بسته (سانتی‌متر)</label>
                <input ref={refFor('packaging')} type="text" inputMode="numeric" value={formatSep(packLengthCm)} onChange={(e) => { setPackLengthCm(parseSep(e.target.value)); clearFieldError('packaging'); }} placeholder="۴۵" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('packaging')} {...invalidProps('packaging')} />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">عرض بسته (سانتی‌متر)</label>
                <input type="text" inputMode="numeric" value={formatSep(packWidthCm)} onChange={(e) => { setPackWidthCm(parseSep(e.target.value)); clearFieldError('packaging'); }} placeholder="۳۵" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('packaging')} {...invalidProps('packaging')} />
              </div>
              <div>
                <label className="text-[10px] text-slate-600 block mb-1">ارتفاع بسته (سانتی‌متر)</label>
                <input type="text" inputMode="numeric" value={formatSep(packHeightCm)} onChange={(e) => { setPackHeightCm(parseSep(e.target.value)); clearFieldError('packaging'); }} placeholder="۲۵" min="1" className={'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none' + invalidClass('packaging')} {...invalidProps('packaging')} />
              </div>
            </div>
            {fieldError('packaging')}
            <p className="text-[10px] text-slate-500 leading-5">هزینه پست بر اساس ابعاد بسته حساب می‌شود، نه ابعاد کالا. اگر بسته‌بندی ثابتی داری، همین‌جا پیش‌فرضش را بنویس؛ انبار هنگام بسته‌بندی آن را تأیید یا اصلاح می‌کند. خالی گذاشتنش مانع تأیید نیست.</p>
          </div>
        </div>
      )}

      {step === 4 && (
        <div className="space-y-3">
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
          <div className="space-y-2 pt-2 border-t border-slate-100">
            <h5 className="text-xs font-bold text-slate-800">بازبینی پیشنهاد</h5>
            {reviewRow('نام کالا', title.trim() || '—', 1)}
            {reviewRow('دسته‌بندی', category?.pathDisplay ?? '—', 1)}
            {reviewRow('برند', noBrand ? 'بدون برند' : brand.trim() || '—', 1)}
            {reviewRow('توضیحات', descPreview || '—', 1)}
            {reviewRow('موضوع (نیچ)', nicheTitle, 1)}
            {reviewRow('تنوع‌ها', `${toPersianDigits(reviewVariantCount)} تنوع • ${toPersianDigits(filledVariantStock)} موجودی • ${reviewMinCost !== null ? `${toPersianDigits(reviewMinCost)} تومان` : '—'}`, 2)}
            {reviewRow('گارانتی', warranty, 3)}
            {reviewRow('آماده‌سازی', `${formatSep(prepDays) || '۱'} روز`, 3)}
            {reviewRow('ارسال', reviewWarehouse, 3)}
            {reviewRow('وزن و ابعاد', weightGrams ? `${formatSep(weightGrams)} گرم • ${formatSep(lengthCm)}×${formatSep(widthCm)}×${formatSep(heightCm)} سانتی‌متر` : '—', 3)}
            {reviewRow('بسته‌بندی', packLengthCm && packWidthCm && packHeightCm ? `${formatSep(packLengthCm)}×${formatSep(packWidthCm)}×${formatSep(packHeightCm)} سانتی‌متر` : '—', 3)}
            {reviewRow('اصل بودن', isOriginal ? 'اصل و اورجینال' : 'غیر اصل', 4)}
            {reviewRow('تصاویر', images.length > 0 ? (
              <span className="inline-flex items-center gap-2">
                <img src={images[0]} alt="تصویر شاخص" className="w-8 h-8 rounded-lg object-cover border border-slate-200" />
                <span>{toPersianDigits(images.length)} تصویر</span>
              </span>
            ) : (imageLink.trim() ? 'لینک تصویر اینترنتی' : '—'), 4)}
          </div>
          <button type="button" onClick={clearDraft} className="text-[11px] text-rose-700 underline">پاک کردن پیش‌نویس و شروع دوباره</button>
        </div>
      )}

      <div className="sticky bottom-0 z-10 -mx-3.5 -mb-3.5 mt-3 bg-white border-t border-slate-200 px-3.5 pt-2.5" style={{ paddingBottom: 'max(10px, env(safe-area-inset-bottom))' }}>
        <div className="flex gap-2">
          {step > 1 && (
            <button type="button" onClick={goBack} className="min-h-[44px] px-4 rounded-xl bg-slate-100 text-slate-600 text-xs font-bold">بازگشت</button>
          )}
          {step < 4 ? (
            <button type="button" onClick={goNext} disabled={saving || uploading} className="flex-1 min-h-[44px] rounded-xl text-white text-xs font-bold disabled:opacity-50" style={{ backgroundColor: '#5d44f8' }}>ادامه</button>
          ) : (
            <button type="button" onClick={submit} disabled={saving || uploading} className="flex-1 py-2 min-h-[44px] bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl transition-colors shadow-2xs disabled:opacity-50">فرستادن برای بررسی مدیر</button>
          )}
        </div>
      </div>
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
