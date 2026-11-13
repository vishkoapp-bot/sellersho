import type { ProductSchema } from '../types/productSchema';
import type { DraftProductSKU } from '../services/productModel';
import { selectionKey, validateProductSchema } from '../services/productModel';
type Decision = { field_key: string; decision: string };
export const cartLimitCorrectionValue = (key: string, text: string): number | null | '' => {
  const value = text.trim().replace(/[۰-۹]/g, digit => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(digit)));
  if (!value) return key.endsWith('/max_cart_qty') ? null : '';
  return Number(value);
};
export const pointerEscape = (value: string) => value.replace(/~/g, '~0').replace(/\//g, '~1');
const segments = (key: string) => key.split('/').slice(1).map(value => value.replace(/~1/g, '/').replace(/~0/g, '~'));
export const pointerValue = (data: any, key: string): any => segments(key).reduce((value, segment) => {
  if (Array.isArray(value) && !/^\d+$/.test(segment)) return value.find(item => String(item?.id) === segment);
  return value != null && Object.prototype.hasOwnProperty.call(value, segment) ? value[segment] : undefined;
}, data);
export const skuReviewLabel = (proposal: any, variant: any): string => {
  const schema: ProductSchema | undefined = proposal?.productSchema;
  const parts = schema?.attributes.filter(attribute => attribute.role === 'axis').map(attribute => {
    const option = attribute.values.find(option => option.id === variant.selection?.[attribute.id]);
    return option ? `${attribute.name}: ${option.label}` : '';
  }).filter(Boolean);
  return parts?.length ? parts.join(' / ') : variant?.variant_name || variant?.variantName || variant?.name || 'مدل اصلی';
};
export const schemaReviewCaption = (key: string, proposal: any): string | undefined => {
  const path = segments(key);
  if (path[0] === 'productSchema') {
    if (path.length === 1) return 'ساختار ویژگی‌های محصول';
    if (path[1] === 'attributes') {
      const attribute = pointerValue(proposal, `/productSchema/attributes/${pointerEscape(path[2] ?? '')}`);
      if (attribute) return `ویژگی محصول / ${attribute.name}`;
    }
  }
  if (path[0] === 'variants') {
    if (path.length === 1) return 'ساختار ترکیب‌های محصول';
    const variant = pointerValue(proposal, `/variants/${pointerEscape(path[1])}`);
    if (variant) {
      const leaf = path[2] === 'selection'
        ? proposal.productSchema?.attributes.find((attribute: any) => attribute.id === path[3])?.name
        : ({ stock: 'موجودی', supplier_base_cost: 'قیمت عمده (تومان)', variant_name: 'نام تنوع', min_cart_qty: 'حداقل تعداد خرید', max_cart_qty: 'حداکثر تعداد خرید' } as Record<string, string>)[path[2]];
      if (leaf) return `تنوع / ${skuReviewLabel(proposal, variant)} / ${leaf}`;
    }
  }
  return undefined;
};
export const selectionReviewChoices = (key: string, proposal: any): { label: string; value: string }[] => {
  const path = segments(key);
  if (path[0] !== 'variants' || path[2] !== 'selection' || path.length !== 4) return [];
  return proposal?.productSchema?.attributes.find((attribute: any) => attribute.id === path[3])?.values.map((value: any) => ({ label: value.label, value: value.id })) ?? [];
};
const mmToCm = (mm: any): string => mm == null || mm === '' ? '' : String(Number(mm) / 10);
const isoToLocal = (iso: any): string => { if (!iso) return ''; const d = new Date(iso); return Number.isNaN(d.getTime()) ? '' : d.toISOString().slice(0, 16); };
export const proposalDraftRows = (proposal: any): DraftProductSKU[] => (proposal.variants ?? []).map((variant: any) => ({
  id: variant.id, selection: { ...variant.selection }, name: skuReviewLabel(proposal, variant),
  cost: String(variant.supplier_base_cost ?? variant.supplierBaseCost ?? ''), stock: String(variant.stock ?? ''), active: variant.is_active !== false && variant.isActive !== false,
  // Variant systems P0/P1(c)/P2(b): carry the draft details through the
  // correction editor so a structural correction does not silently drop
  // them (they are only *edited* when the whole variants group is
  // rejected — see correctionPermissions below).
  selected: true,
  // B2: carry the product-level default pick through the correction
  // editor (proposal stores it as is_default; serializeProductDraft
  // resolves the effective default the same way — see productModel.ts).
  isDefault: variant.is_default ?? variant.isDefault ?? false,
  imageUrl: variant.image_url ?? variant.imageUrl ?? '',
  images: [...(variant.image_urls ?? variant.images ?? [])],
  weightGrams: variant.weight_grams != null ? String(variant.weight_grams) : (variant.weightGrams != null ? String(variant.weightGrams) : ''),
  lengthCm: mmToCm(variant.length_mm ?? variant.lengthMm),
  widthCm: mmToCm(variant.width_mm ?? variant.widthMm),
  heightCm: mmToCm(variant.height_mm ?? variant.heightMm),
  salePrice: variant.sale_price != null ? String(variant.sale_price) : (variant.salePrice != null ? String(variant.salePrice) : ''),
  saleFrom: isoToLocal(variant.sale_from ?? variant.saleFrom),
  saleTo: isoToLocal(variant.sale_to ?? variant.saleTo),
  shippingClass: variant.shipping_class ?? variant.shippingClass ?? '',
  taxClass: variant.tax_class ?? variant.taxClass ?? '',
}));
export const correctionPermissions = (proposal: any, fields: Decision[]) => {
  const rejected = new Set(fields.filter(field => field.decision === 'rejected').map(field => field.field_key));
  const root = rejected.has('/productSchema');
  const variantsEditable = rejected.has('/variants');
  return {
    schemaEditable: root || [...rejected].some(key => /^\/productSchema\/attributes\/[^/]+$/.test(key)),
    attributeListEditable: root,
    variantsEditable,
    attributeEditable: (id: string) => root || rejected.has(`/productSchema/attributes/${pointerEscape(id)}`),
    canEditSKUSelection: (id: string, attributeID: string) => variantsEditable || rejected.has(`/variants/${pointerEscape(id)}/selection/${pointerEscape(attributeID)}`),
    // New variant-detail leaves (image/physical/sale/classes) have no
    // per-leaf review keys in the dossier, so they are editable only
    // when the whole variants group was rejected (wholesale replace).
    canEditSKUValue: (id: string, leaf: 'stock' | 'supplier_base_cost' | 'weight_grams' | 'dimensions' | 'sale_price' | 'image' | 'shipping_class' | 'tax_class') =>
      leaf === 'stock' || leaf === 'supplier_base_cost'
        ? rejected.has(`/variants/${pointerEscape(id)}/${leaf}`) || (variantsEditable && !proposal.variants?.some((variant: any) => variant.id === id))
        : variantsEditable,
  };
};
const canonical = (value: any): string => JSON.stringify(value, (_key, item) => item && typeof item === 'object' && !Array.isArray(item) ? Object.fromEntries(Object.entries(item).sort(([a], [b]) => a.localeCompare(b))) : item);
const structuralRows = (rows: DraftProductSKU[]) => rows.map(row => ({ id: row.id, selection: row.selection, is_active: row.active })).sort((a, b) => a.id.localeCompare(b.id));

export const buildStructuralCorrection = (proposal: any, fields: Decision[], schema: ProductSchema, rows: DraftProductSKU[]): Record<string, any> => {
  const permissions = correctionPermissions(proposal, fields);
  const originalSchema: ProductSchema = proposal.productSchema;
  const previous = proposalDraftRows(proposal);
  const patch: Record<string, any> = {};
  validateProductSchema(schema);
  if (canonical(schema) !== canonical(originalSchema)) {
    if (permissions.attributeListEditable) patch['/productSchema'] = schema;
    else {
      if (schema.type !== originalSchema.type || schema.version !== originalSchema.version || canonical(schema.attributes.map(attribute => attribute.id)) !== canonical(originalSchema.attributes.map(attribute => attribute.id))) throw Error('تغییر ساختار ویژگی‌ها نیازمند رد گروه ساختار محصول است.');
      for (const attribute of schema.attributes) {
        const original = originalSchema.attributes.find(item => item.id === attribute.id);
        if (canonical(attribute) !== canonical(original)) {
          if (!permissions.attributeEditable(attribute.id)) throw Error('این ویژگی تأیید شده و قابل اصلاح نیست.');
          patch[`/productSchema/attributes/${pointerEscape(attribute.id)}`] = attribute;
        }
      }
    }
  }
  const structureChanged = canonical(structuralRows(rows)) !== canonical(structuralRows(previous));
  const replacingStructure = structureChanged && permissions.variantsEditable;
  if (structureChanged && !permissions.variantsEditable) {
    const shape = (items: DraftProductSKU[]) => items.map(row => ({ id: row.id, active: row.active })).sort((a, b) => a.id.localeCompare(b.id));
    if (canonical(shape(rows)) !== canonical(shape(previous))) throw Error('تغییر ساختار ترکیب‌ها نیازمند رد گروه تنوع‌ها است.');
    for (const row of rows) {
      const old = previous.find(item => item.id === row.id)!;
      for (const id of new Set([...Object.keys(old.selection), ...Object.keys(row.selection)])) {
        if (row.selection[id] === old.selection[id]) continue;
        if (!permissions.canEditSKUSelection(row.id, id) || !row.selection[id]) throw Error('تغییر ساختار انتخاب‌های تأییدشدهٔ تنوع مجاز نیست.');
        patch[`/variants/${pointerEscape(row.id)}/selection/${pointerEscape(id)}`] = row.selection[id];
      }
    }
  }
  if (new Set(rows.map(row => row.id)).size !== rows.length || rows.some(row => !row.id)) throw Error('شناسهٔ ترکیب‌ها باید یکتا باشد.');
  const activeKeys = new Set<string>();
  const axes = schema.attributes.filter(attribute => attribute.role === 'axis');
  const fullRows = rows.map(row => {
    const old = previous.find(item => item.id === row.id);
    const original = proposal.variants?.find((item: any) => item.id === row.id);
    if (row.active) {
      if (Object.keys(row.selection).length !== axes.length || axes.some(attribute => !attribute.values.some(value => value.id === row.selection[attribute.id]))) throw Error('ترکیب‌ها با ویژگی‌های انتخاب‌شده سازگار نیستند؛ ساختار ترکیب‌ها باید اصلاح شود.');
      const key = selectionKey(row.selection);
      if (activeKeys.has(key)) throw Error('ترکیب فعال تکراری است.');
      activeKeys.add(key);
    }
    const cost = row.cost.trim() ? Number(row.cost) : 0;
    const stock = row.stock.trim() ? Number(row.stock) : 0;
    if (!Number.isSafeInteger(cost) || cost < 0 || cost > 1e12 || (row.active && cost <= 0) || !Number.isSafeInteger(stock) || stock < 0 || stock > 1e9) throw Error(`قیمت و موجودی «${row.name}» را درست وارد کنید.`);
    for (const [leaf, value, original] of [['supplier_base_cost', cost, old ? Number(old.cost) : undefined], ['stock', stock, old ? Number(old.stock) : undefined]] as const) {
      if (old && value !== original) {
        if (!permissions.canEditSKUValue(row.id, leaf)) throw Error(`قیمت یا موجودی تأییدشدهٔ «${row.name}» قابل تغییر نیست.`);
        if (!replacingStructure) patch[`/variants/${pointerEscape(row.id)}/${leaf}`] = value;
      }
    }
    return { id: row.id, selection: row.selection, variant_name: skuReviewLabel({ productSchema: schema }, row), attributes: Object.fromEntries(axes.map(attribute => [attribute.name, attribute.values.find(value => value.id === row.selection[attribute.id])?.label ?? ''])), supplier_base_cost: cost, stock, is_active: row.active,
      // Variant details ride along on a wholesale variants replace so
      // they are not dropped by a structural correction (same snake_case
      // mapping as serializeProductDraft).
      // F1: backend reads ONLY v["images"] (hero first) — the old
      // image_url/image_urls keys were silently dropped at publication.
      // B2: is_default rides along; normalised to exactly one default
      // (explicit pick among active rows, else first active) below.
      is_default: row.isDefault ?? false,
      images: row.imageUrl.trim() ? [row.imageUrl.trim(), ...row.images.filter(u => u.trim() !== '')] : row.images.filter(u => u.trim() !== ''),
      weight_grams: row.weightGrams.trim() === '' ? null : Number(row.weightGrams),
      length_mm: row.lengthCm.trim() === '' ? null : Number(row.lengthCm) * 10,
      width_mm: row.widthCm.trim() === '' ? null : Number(row.widthCm) * 10,
      height_mm: row.heightCm.trim() === '' ? null : Number(row.heightCm) * 10,
      sale_price: row.salePrice.trim() === '' ? null : Number(row.salePrice),
      sale_from: row.saleFrom.trim() === '' ? null : new Date(row.saleFrom).toISOString(),
      sale_to: row.saleTo.trim() === '' ? null : new Date(row.saleTo).toISOString(),
      shipping_class: row.shippingClass.trim() || null,
      tax_class: row.taxClass.trim() || null,
      ...(original ? { min_cart_qty: original.min_cart_qty ?? 1, max_cart_qty: original.max_cart_qty ?? null } : {}),
    };
  });
  // B2: exactly one is_default among the active rows in a wholesale
  // replace — explicit pick wins, else the first active row (same rule
  // as serializeProductDraft).
  const defaultId = fullRows.find(r => r.is_default && r.is_active)?.id ?? fullRows.find(r => r.is_active)?.id;
  for (const r of fullRows) r.is_default = r.id === defaultId;
  if (!rows.some(row => row.active)) throw Error('حداقل یک ترکیب فعال لازم است.');
  if (replacingStructure) patch['/variants'] = fullRows;
  return patch;
};
