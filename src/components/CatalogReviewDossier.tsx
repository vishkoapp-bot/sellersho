import React, { useEffect, useState } from 'react';
import { apiRequest } from '../services/apiClient';

// Team 08 live parity (D61): CatalogReviewDossier chunk (live
// CatalogReviewDossier-MERGED.js, 30,189 bytes, imported from
// main/admin/supplier/panels). Fields are exactly Team 04's dossier
// endpoint (domain.CatalogReviewDossier): package dimensions, product
// height, warranty, central-warehouse vs direct-supplier shipping,
// reviewer free-text rejection reasons, and the approval note
// "در صورت تأیید، کالا بدون مبدأ ساخته می‌شود".
// No rejection-reason enum is invented: rejectionReason is free text
// from the backend (or absent). The supplier fetch path is NOT guessed:
// only the admin endpoint GET /admin/catalog/review/<id> is wired here;
// for supplier panels the caller must pass a loaded dossier prop.
export interface CatalogReviewDossierData {
  productId: string;
  proposalId?: string;
  title: string;
  slug: string;
  brand: string;
  category: string;
  status: string;
  packageDimensions: { lengthMm?: number | null; widthMm?: number | null; heightMm?: number | null; weightGrams?: number | null };
  productHeightMm?: number | null;
  weightGrams?: number | null;
  warranty: string;
  fulfillmentMethod: string;
  shippingMode: string;
  specs: Record<string, unknown>;
  images: string[];
  variantCount: number;
  rejectionReason?: string | null;
  approvalNote: string;
}

function shippingModeLabel(mode: string, fulfillment: string): string {
  if (mode === 'central-warehouse' || fulfillment === 'warehouse') return 'ارسال از انبار مرکزی';
  if (mode === 'direct-supplier' || fulfillment === 'supplier_direct') return 'ارسال مستقیم از تأمین‌کننده';
  return mode || fulfillment || '—';
}

export const CatalogReviewDossier: React.FC<{
  dossier?: CatalogReviewDossierData;
  dossierId?: string;
}> = ({ dossier, dossierId }) => {
  const [loaded, setLoaded] = useState<CatalogReviewDossierData | null>(dossier || null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (dossier || !dossierId) return;
    let cancelled = false;
    apiRequest<CatalogReviewDossierData>(`/admin/catalog/review/${encodeURIComponent(dossierId)}`, { auth: true })
      .then((data) => { if (!cancelled) setLoaded(data); })
      .catch((err: any) => { if (!cancelled) setError(err?.message || 'خطا در دریافت پرونده بررسی'); });
    return () => { cancelled = true; };
  }, [dossier, dossierId]);
  if (error) {
    return <div className="rounded-2xl border border-rose-200 bg-rose-50 p-4 text-xs text-rose-700">{error}</div>;
  }
  if (!loaded) {
    return <div className="rounded-2xl border border-slate-200 bg-white p-4 text-xs text-slate-500">در حال بارگذاری پرونده بررسی کاتالوگ…</div>;
  }
  const dims = loaded.packageDimensions || {};
  return (
    <div className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 text-xs">
      <h3 className="text-sm font-bold text-slate-900">پرونده بررسی کاتالوگ</h3>
      <div className="grid grid-cols-2 gap-2 text-slate-600">
        <span>کالا:</span><span className="font-bold text-slate-900">{loaded.title}</span>
        <span>برند:</span><span className="font-bold text-slate-900">{loaded.brand || '—'}</span>
        <span>دسته:</span><span className="font-bold text-slate-900">{loaded.category || '—'}</span>
        <span>وضعیت:</span><span className="font-bold text-slate-900">{loaded.status}</span>
        <span>ابعاد بسته:</span>
        <span className="font-bold text-slate-900">
          {[dims.lengthMm, dims.widthMm, dims.heightMm].map((v) => (v === null || v === undefined ? '—' : v)).join(' × ')} میلی‌متر
        </span>
        <span>ارتفاع کالا:</span><span className="font-bold text-slate-900">{loaded.productHeightMm ?? '—'} میلی‌متر</span>
        <span>وزن:</span><span className="font-bold text-slate-900">{loaded.weightGrams ?? dims.weightGrams ?? '—'} گرم</span>
        <span>گارانتی:</span><span className="font-bold text-slate-900">{loaded.warranty || '—'}</span>
        <span>نحوه ارسال:</span><span className="font-bold text-slate-900">{shippingModeLabel(loaded.shippingMode, loaded.fulfillmentMethod)}</span>
        <span>تعداد واریانت:</span><span className="font-bold text-slate-900">{loaded.variantCount}</span>
      </div>
      {loaded.images.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {loaded.images.map((src, i) => (
            <img key={i} src={src} alt="" className="h-16 w-16 rounded-xl border border-slate-200 object-contain" />
          ))}
        </div>
      )}
      {loaded.specs && Object.keys(loaded.specs).length > 0 && (
        <div className="space-y-1">
          <h4 className="font-bold text-slate-800">مشخصات فنی</h4>
          {Object.entries(loaded.specs).map(([k, v]) => (
            <div key={k} className="flex justify-between text-slate-600"><span>{k}</span><span className="font-bold text-slate-900">{String(v)}</span></div>
          ))}
        </div>
      )}
      {loaded.rejectionReason ? (
        <div className="rounded-xl bg-rose-50 border border-rose-200 p-3 text-rose-800">
          <span className="font-bold">دلیل رد بررسی‌کننده: </span>{loaded.rejectionReason}
        </div>
      ) : null}
      <p className="rounded-xl bg-blue-50 border border-blue-200 p-3 text-blue-800 leading-6">{loaded.approvalNote}</p>
      {/* TODO(U31/U42-adjacent): reviewer free-text rejection reasons are
          rendered verbatim above; no enum is assumed. Live evidence
          includes reasons such as "املای برند اشتباه است" as examples
          only, not as a closed list. */}
    </div>
  );
};
