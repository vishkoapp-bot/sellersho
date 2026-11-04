import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { Building2, Star, Truck, FileText } from 'lucide-react';

// Team 06 live-parity (D54/D15): this tab used to render mockSuppliers with
// local-only state. It now loads real suppliers from
// GET /api/v1/admin/suppliers/list and drives approve/reject and the
// direct-shipping decision through real admin endpoints. No supplier row
// is fabricated locally: the "add supplier" form that created local-only
// rows was removed, because no admin create-supplier endpoint is
// confirmed in the live build (suppliers are created by registration).
// The exact live approve/reject route is unverified (U06); the routes
// used here are the reconstruction choice under /admin/suppliers/.

interface AdminSupplierRow {
  id: string;
  name: string;
  logoUrl?: string;
  rating: number;
  totalProducts: number;
  deliveryScore: number;
  warehouseCity: string;
  warehouseCityConfirmed?: boolean;
  approvalStatus?: string;
  signedContractVersion?: number;
  defaultMarkupType?: string | null;
  defaultMarkupValue?: number | null;
  rejectionReason?: string | null;
  tapinShopId?: string | null;
  tapinConnected?: boolean;
}

interface ShippingMethodRow {
  supplierId: string;
  method: string;
  status: string;
}

type StatusFilter = 'all' | 'pending_approval' | 'approved' | 'rejected';

const statusLabel = (status?: string): string => {
  switch (status) {
    case 'pending_approval':
      return 'در انتظار تأیید';
    case 'approved':
      return 'تأییدشده';
    case 'rejected':
      return 'ردشده';
    default:
      return status || 'نامشخص';
  }
};

export const AdminSuppliersTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [suppliersList, setSuppliersList] = useState<AdminSupplierRow[]>([]);
  const [directShip, setDirectShip] = useState<{ [key: string]: boolean }>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [selectedContractSupplier, setSelectedContractSupplier] = useState<AdminSupplierRow | null>(null);
  const [rejectSupplier, setRejectSupplier] = useState<AdminSupplierRow | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);

  const loadSuppliers = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      // /records (not /list) so the contract modal also has rejection
      // reason and Tapin connection state, which /list omits.
      const suppliers = await apiRequest<AdminSupplierRow[]>('/admin/suppliers/records', { method: 'GET', auth: true });
      setSuppliersList(Array.isArray(suppliers) ? suppliers : []);
      try {
        const methods = await apiRequest<ShippingMethodRow[]>('/admin/suppliers/shipping-methods', { method: 'GET', auth: true });
        const toggles: { [key: string]: boolean } = {};
        (Array.isArray(methods) ? methods : []).forEach((m) => {
          if (m.method === 'direct') toggles[m.supplierId] = m.status === 'enabled';
        });
        setDirectShip(toggles);
      } catch {
        // Shipping-method rows are supplementary; the supplier list is the
        // primary data and must still render if this call fails.
        setDirectShip({});
      }
    } catch (err: any) {
      setLoadError(err?.message || 'خطا در دریافت فهرست تأمین‌کنندگان');
      setSuppliersList([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSuppliers();
  }, [loadSuppliers]);

  const approveSupplier = async (id: string) => {
    setBusyId(id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(id)}/approve`, { method: 'POST', auth: true, body: JSON.stringify({}) });
      success('تأمین‌کننده تأیید شد.');
      await loadSuppliers();
    } catch (err: any) {
      error(err?.message || 'تأیید تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const rejectSupplierConfirm = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!rejectSupplier) return;
    setBusyId(rejectSupplier.id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(rejectSupplier.id)}/reject`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ reason: rejectReason }),
      });
      success('تأمین‌کننده رد شد.');
      setRejectSupplier(null);
      setRejectReason('');
      await loadSuppliers();
    } catch (err: any) {
      error(err?.message || 'رد تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const toggleDirectShip = async (id: string) => {
    const nextEnabled = !directShip[id];
    setBusyId(id);
    try {
      await apiRequest('/admin/suppliers/shipping-methods', {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ supplierId: id, method: 'direct', status: nextEnabled ? 'enabled' : 'disabled' }),
      });
      setDirectShip((prev) => ({ ...prev, [id]: nextEnabled }));
      success('وضعیت مجوز ارسال مستقیم تأمین‌کننده به‌روزرسانی شد.');
    } catch (err: any) {
      error(err?.message || 'به‌روزرسانی مجوز ارسال مستقیم ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const visibleSuppliers = statusFilter === 'all'
    ? suppliersList
    : suppliersList.filter((s) => s.approvalStatus === statusFilter);

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">
            تأمین‌کنندگان رسمی ({toPersianDigits(visibleSuppliers.length)} برند همکار)
          </h3>
          <button
            onClick={loadSuppliers}
            className="px-2.5 py-1 text-[10px] font-bold rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-700"
          >
            به‌روزرسانی فهرست
          </button>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          پیش‌فرض پلتفرم ارسال از انبار مرکزی است. مجوز ارسال مستقیم فقط با تأیید مدیر برای تأمین‌کنندگان واجد شرایط فعال می‌شود (بخش ۱ و ۹ پروپوزال).
        </p>
        <div className="flex gap-1.5 pt-1">
          {([
            ['all', 'همه'],
            ['pending_approval', 'در انتظار تأیید'],
            ['approved', 'تأییدشده'],
            ['rejected', 'ردشده'],
          ] as [StatusFilter, string][]).map(([value, label]) => (
            <button
              key={value}
              onClick={() => setStatusFilter(value)}
              className={`px-2.5 py-1 text-[10px] font-bold rounded-lg transition-colors ${
                statusFilter === value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {loading && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 text-[11px] text-slate-500">
          در حال دریافت فهرست تأمین‌کنندگان…
        </div>
      )}

      {!loading && loadError && (
        <div className="bg-white rounded-2xl p-3.5 border border-rose-200 text-[11px] text-rose-700">
          {loadError}
        </div>
      )}

      {!loading && !loadError && visibleSuppliers.length === 0 && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 text-[11px] text-slate-500">
          تأمین‌کننده‌ای با این وضعیت یافت نشد.
        </div>
      )}

      <div className="space-y-2.5">
        {visibleSuppliers.map((supplier) => {
          const hasDirectShip = !!directShip[supplier.id];
          const isPending = supplier.approvalStatus === 'pending_approval';
          return (
            <div
              key={supplier.id}
              className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div className="flex items-center gap-2">
                  {supplier.logoUrl ? (
                    <img
                      src={supplier.logoUrl}
                      alt={supplier.name}
                      className="w-8 h-8 rounded-full object-contain border border-slate-100 p-0.5"
                    />
                  ) : (
                    <Building2 className="w-8 h-8 text-slate-300"  strokeWidth={1.5}/>
                  )}
                  <div>
                    <h4 className="text-xs font-bold text-slate-900">{supplier.name}</h4>
                    <span className="text-[10px] text-slate-400">{supplier.warehouseCity}</span>
                  </div>
                </div>

                <div className="flex items-center gap-1.5">
                  <span className="text-[10px] font-bold text-slate-600">{statusLabel(supplier.approvalStatus)}</span>
                  <div className="flex items-center gap-1 text-xs text-amber-500 font-bold">
                    <Star className="w-3.5 h-3.5 fill-current"  strokeWidth={1.5}/>
                    <span>{toPersianDigits(supplier.rating)}</span>
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600">
                <div>تعداد تنوع در کاتالوگ: <strong className="text-slate-800">{toPersianDigits(supplier.totalProducts)} قلم</strong></div>
                <div>امتیاز تحویل بموقع: <strong className="text-emerald-600">٪{toPersianDigits(supplier.deliveryScore)}</strong></div>
                <div>نسخه قرارداد امضاشده: <strong className="text-slate-800">{supplier.signedContractVersion != null ? toPersianDigits(supplier.signedContractVersion) : '—'}</strong></div>
                <div>شهر انبار تأییدشده: <strong className="text-slate-800">{supplier.warehouseCityConfirmed ? 'بله' : 'خیر'}</strong></div>
              </div>

              {supplier.rejectionReason && (
                <div className="text-[10px] text-rose-700 bg-rose-50 border border-rose-100 rounded-xl p-2">
                  دلیل رد: {supplier.rejectionReason}
                </div>
              )}

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
                <div className="flex items-center gap-1.5">
                  <Truck className="w-3.5 h-3.5 text-slate-400"  strokeWidth={1.5}/>
                  <span className="text-[10px] text-slate-600">مجوز استثنای ارسال مستقیم:</span>
                </div>

                <div className="flex items-center gap-1.5">
                  <button
                    onClick={() => setSelectedContractSupplier(supplier)}
                    className="px-2 py-1 text-[10px] font-bold rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center gap-1"
                  >
                    <FileText className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>قرارداد</span>
                  </button>
                  {isPending && (
                    <>
                      <button
                        onClick={() => approveSupplier(supplier.id)}
                        disabled={busyId === supplier.id}
                        className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-emerald-600 text-white disabled:opacity-50"
                      >
                        تأیید
                      </button>
                      <button
                        onClick={() => setRejectSupplier(supplier)}
                        disabled={busyId === supplier.id}
                        className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-rose-100 text-rose-700 disabled:opacity-50"
                      >
                        رد
                      </button>
                    </>
                  )}
                  <button
                    onClick={() => toggleDirectShip(supplier.id)}
                    disabled={busyId === supplier.id}
                    className={`px-2.5 py-1 text-[10px] font-bold rounded-lg transition-colors disabled:opacity-50 ${
                      hasDirectShip
                        ? 'bg-blue-600 text-white'
                        : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
                    }`}
                  >
                    {hasDirectShip ? 'فعال (با مجوز)' : 'غیرفعال (ارسال انبار)'}
                  </button>
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {rejectSupplier && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <form onSubmit={rejectSupplierConfirm} className="bg-white rounded-2xl max-w-sm w-full p-4 border border-slate-200 shadow-2xl space-y-3 text-xs">
            <h4 className="font-extrabold text-slate-900 text-sm">رد تأمین‌کننده: {rejectSupplier.name}</h4>
            <div>
              <label className="block text-[11px] font-bold text-slate-700 mb-1">دلیل رد (اختیاری)</label>
              <input
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder="دلیل رد درخواست"
                className="w-full p-2.5 rounded-xl bg-slate-50 border border-slate-200"
              />
            </div>
            <div className="flex gap-2 pt-1">
              <button type="submit" disabled={busyId === rejectSupplier.id} className="flex-1 py-2 bg-rose-600 text-white font-bold rounded-xl disabled:opacity-50">
                تأیید رد
              </button>
              <button type="button" onClick={() => { setRejectSupplier(null); setRejectReason(''); }} className="px-3 py-2 bg-slate-100 text-slate-600 font-bold rounded-xl">
                انصراف
              </button>
            </div>
          </form>
        </div>
      )}

      {selectedContractSupplier && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl max-w-md w-full p-4 border border-slate-200 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2">
              <h4 className="font-extrabold text-slate-900">خلاصه قرارداد تجاری: {selectedContractSupplier.name}</h4>
              <button onClick={() => setSelectedContractSupplier(null)} className="px-2 py-1 bg-slate-100 rounded-lg font-bold">
                بستن
              </button>
            </div>
            <div className="space-y-1.5 text-slate-600">
              <div className="flex justify-between"><span>مدل همکاری:</span><strong className="text-slate-900">امانی در انبار مرکزی (Consignment)</strong></div>
              <div className="flex justify-between"><span>دوره تسویه حساب:</span><strong className="text-emerald-700">پس از انقضای ۷ روز مهلت تست خریدار</strong></div>
              <div className="flex justify-between"><span>وضعیت تأیید:</span><strong className="text-slate-900">{statusLabel(selectedContractSupplier.approvalStatus)}</strong></div>
              <div className="flex justify-between"><span>نسخه قرارداد امضاشده:</span><strong className="text-slate-900">{selectedContractSupplier.signedContractVersion != null ? toPersianDigits(selectedContractSupplier.signedContractVersion) : '—'}</strong></div>
              <div className="flex justify-between"><span>اتصال تاپین:</span><strong className="text-slate-900">{selectedContractSupplier.tapinConnected ? 'متصل' : 'متصل نیست'}</strong></div>
              <div className="flex justify-between"><span>امتیاز SLA تحویل:</span><strong className="text-blue-600">٪{toPersianDigits(selectedContractSupplier.deliveryScore)}</strong></div>
              {selectedContractSupplier.rejectionReason && (
                <p className="p-2.5 bg-rose-50 rounded-xl border border-rose-100 text-[11px] leading-relaxed">
                  دلیل رد: {selectedContractSupplier.rejectionReason}
                </p>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
