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
export const proposalDraftRows = (proposal: any): DraftProductSKU[] => (proposal.variants ?? []).map((variant: any) => ({
  id: variant.id, selection: { ...variant.selection }, name: skuReviewLabel(proposal, variant),
  cost: String(variant.supplier_base_cost ?? variant.supplierBaseCost ?? ''), stock: String(variant.stock ?? ''), active: variant.is_active !== false && variant.isActive !== false,
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
    canEditSKUValue: (id: string, leaf: 'stock' | 'supplier_base_cost') =>
      rejected.has(`/variants/${pointerEscape(id)}/${leaf}`) || (variantsEditable && !proposal.variants?.some((variant: any) => variant.id === id)),
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
      ...(original ? { min_cart_qty: original.min_cart_qty ?? 1, max_cart_qty: original.max_cart_qty ?? null } : {}),
    };
  });
  if (!rows.some(row => row.active)) throw Error('حداقل یک ترکیب فعال لازم است.');
  if (replacingStructure) patch['/variants'] = fullRows;
  return patch;
};
