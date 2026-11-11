import React, { useState } from 'react';
import type { ProductSchema } from '../../types/productSchema';
import type { DraftProductSKU } from '../../services/productModel';
import { buildStructuralCorrection, correctionPermissions, proposalDraftRows } from '../../utils/productReview';
import { SupplierProductDefinitionEditor } from './SupplierProductDefinitionEditor';

interface Props {
  proposal: any;
  fields: { field_key: string; decision: string }[];
  busy?: boolean;
  onPatch: (patch: Record<string, any>, error: string) => void;
}

export function SupplierProductStructuralCorrection({ proposal, fields, busy = false, onPatch }: Props) {
  const [schema, setSchema] = useState<ProductSchema>(() => structuredClone(proposal.productSchema));
  const [rows, setRows] = useState<DraftProductSKU[]>(() => proposalDraftRows(proposal));
  const [failure, setFailure] = useState('');
  const permissions = correctionPermissions(proposal, fields);
  const commit = (nextSchema: ProductSchema, nextRows: DraftProductSKU[]) => {
    setSchema(nextSchema);
    setRows(nextRows);
    try {
      const patch = buildStructuralCorrection(proposal, fields, nextSchema, nextRows);
      setFailure('');
      onPatch(patch, '');
    } catch (error) {
      const message = error instanceof Error ? error.message : 'اصلاحات ساختاری معتبر نیست.';
      setFailure(message);
      onPatch({}, message);
    }
  };
  return <fieldset disabled={busy} className="space-y-3 min-w-0" aria-label="اصلاح ساختار و ترکیب‌های محصول">
    <p className="text-xs text-slate-600 leading-7">فقط گروه‌ها و مقدارهای ردشده قابل تغییرند. قیمت و موجودی تأییدشدهٔ ترکیب‌های قبلی حفظ می‌شود. برای هر ترکیب جدید، قیمت و موجودی را وارد کنید.</p>
    {permissions.attributeListEditable && <label className="text-xs font-bold block">نوع محصول
      <select aria-label="اصلاح نوع محصول" value={schema.type} className="border rounded-xl p-2 mx-2"
        onChange={event => {
          const type = event.target.value as 'simple' | 'variable';
          commit({ ...schema, type }, rows);
        }}><option value="simple">تکی</option><option value="variable">متغیر</option></select>
    </label>}
    <SupplierProductDefinitionEditor categorySlug={proposal.categorySlug ?? proposal.category ?? null}
      productType={schema.type} schema={schema} rows={rows}
      onSchema={next => commit(next, rows)} onRows={next => commit(schema, next)}
      brand={proposal.brand ?? ''} noBrand={!!proposal.noBrand} onBrand={() => {}} onNoBrand={() => {}}
      hideBrand schemaEditable={permissions.schemaEditable}
      attributeListEditable={permissions.attributeListEditable} canEditAttribute={permissions.attributeEditable}
      skuStructureEditable={permissions.variantsEditable} canEditSKUValue={permissions.canEditSKUValue} canEditSKUSelection={permissions.canEditSKUSelection} />
    {!permissions.variantsEditable && <p className="text-xs text-slate-500 leading-7">ساختار ترکیب‌ها تأیید شده یا در انتظار بازبینی است؛ افزودن، حذف و بازسازی ترکیب‌ها فقط پس از رد همان گروه ممکن است.</p>}
    {failure && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs text-rose-800 leading-7">{failure}</p>}
  </fieldset>;
}
