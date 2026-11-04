import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { ArrowDownToLine, CheckCircle2, XCircle, ShieldCheck, Download } from 'lucide-react';

interface ApprovedSettlement {
  id: string;
  recipientName: string;
  shaba: string;
  bankName: string;
  amount: number;
}

// Live parity: the approved-settlements manual-transfer section inside the
// live finance-payouts tab (AdminDashboardView-MERGED.js, Jd component,
// rendered by the payouts tab after its pending list). It lists GET
// /finance/settlements/approved and, per item, the button below opens the
// tracking-number input; the confirm button posts
// {transfer_reference, transfer_date: now ISO} to POST
// /admin/payouts/<id>/confirm-manual-transfer, the same body live sends
// (the live API helper defaults transfer_date to new Date().toISOString()).
// On success: the live toast, input cleared, form closed, list reloaded.
// Like live, the section renders nothing until the list is loaded and
// non-empty. The pre-existing mock cards above are left untouched.
const ApprovedManualTransfers: React.FC<{ formatPrice: (n: number) => string }> = ({ formatPrice }) => {
  const { success, error } = useToast();
  const [settlements, setSettlements] = useState<ApprovedSettlement[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [trackingRef, setTrackingRef] = useState("");
  const [confirming, setConfirming] = useState(false);

  const load = useCallback(() => {
    apiRequest<any>("/finance/settlements/approved", { method: "GET", auth: true })
      .then((data) => {
        setSettlements(Array.isArray(data?.settlements) ? data.settlements : []);
        setLoaded(true);
      })
      .catch(() => setLoaded(true));
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleConfirm = async (id: string) => {
    if (!trackingRef.trim()) {
      error("شماره پیگیری انتقال بانکی الزامی است.");
      return;
    }
    setConfirming(true);
    try {
      await apiRequest(`/admin/payouts/${encodeURIComponent(id)}/confirm-manual-transfer`, {
        method: "POST",
        auth: true,
        body: JSON.stringify({
          transfer_reference: trackingRef.trim(),
          transfer_date: new Date().toISOString(),
        }),
      });
      success("انتقال بانکی تأیید شد و سند حسابداری در دفتر کل ثبت گردید.");
      setTrackingRef("");
      setExpandedId(null);
      load();
    } catch (err: any) {
      error(err?.message || "تأیید انتقال انجام نشد.");
    } finally {
      setConfirming(false);
    }
  };

  if (!loaded || settlements.length === 0) return null;

  return (
    <div className="space-y-2">
      <div className="rounded-2xl border border-amber-200 bg-amber-50 p-3">
        <p className="text-[11px] font-bold text-amber-800">این تسویه‌ها به‌صورت دستی انجام می‌شود — پس از انتقال بانکی، تأیید کنید</p>
        <p className="mt-1 text-[10px] text-amber-700 leading-relaxed">این درخواست‌ها تأیید شده‌اند ولی هنوز پولی جابه‌جا نشده است. پس از انجام انتقال بانکی (کارت‌به‌کارت/پایا/ساتنا)، شماره پیگیری را وارد و «تأیید واریز» را بزنید. فقط پس از این تأیید، سند حسابداری در دفتر کل ثبت می‌شود.</p>
      </div>
      {settlements.map((item) => (
        <div key={item.id} className="p-4 bg-white rounded-2xl border border-amber-200/70 shadow-2xs space-y-2.5 text-xs">
          <div className="flex items-center justify-between">
            <div>
              <span className="font-black text-slate-800">{item.recipientName}</span>
              <span className="text-[10px] text-slate-400 font-mono block mt-0.5" dir="ltr">{item.id}</span>
            </div>
            <span className="text-[9px] bg-amber-100 text-amber-800 font-bold px-2 py-0.5 rounded-full">تأییدشده — در انتظار واریز دستی</span>
          </div>
          <div>
            <span className="text-[11px] text-slate-400 block">شماره شبای مقصد:</span>
            <span className="text-[11px] font-mono text-slate-500 mt-0.5 block tracking-wider" dir="ltr">{item.shaba} ({item.bankName})</span>
          </div>
          <div className="flex justify-between text-emerald-700 font-black">
            <span>مبلغ:</span>
            <span>{formatPrice(item.amount)} تومان</span>
          </div>
          {expandedId === item.id ? (
            <div className="space-y-2 pt-1 border-t border-slate-100">
              <input value={trackingRef} onChange={(e) => setTrackingRef(e.target.value)} placeholder="شماره پیگیری انتقال بانکی" dir="ltr" className="w-full min-h-[44px] rounded-xl border px-3 text-xs font-mono" />
              <div className="grid grid-cols-2 gap-2">
                <button onClick={() => handleConfirm(item.id)} disabled={confirming} className="min-h-[44px] px-4 bg-brand-deep hover:opacity-90 disabled:opacity-50 text-white font-bold text-xs rounded-xl">
                  {confirming ? "در حال ثبت…" : "تأیید واریز و ثبت سند"}
                </button>
                <button onClick={() => { setExpandedId(null); setTrackingRef(""); }} disabled={confirming} className="min-h-[44px] px-4 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl">
                  انصراف
                </button>
              </div>
            </div>
          ) : (
            <button onClick={() => setExpandedId(item.id)} className="w-full min-h-[44px] px-4 bg-brand-deep hover:opacity-90 text-white font-bold text-xs rounded-xl transition-colors">
              ثبت انتقال بانکی
            </button>
          )}
        </div>
      ))}
    </div>
  );
};


export const FinancePayoutsTab: React.FC = () => {
  const { executeSupplierMonthlyPayouts, formatPrice, toPersianDigits } = useStore();
  const { success, warning } = useToast();
  const [approvedMap, setApprovedMap] = useState<{ [key: string]: boolean }>({});
  const [rejectingPayout, setRejectingPayout] = useState<any | null>(null);
  const [cycleType, setCycleType] = useState<'all' | 'seller_daily' | 'supplier_monthly'>('all');

  const payoutRequests = [
    {
      id: 'PAY-701',
      recipientType: 'دراپ‌شیپر',
      recipientName: 'بهاره رضوانی (فروشگاه بی‌بی استایل)',
      shaba: 'IR620170000001234567890123',
      bankName: 'بانک سامان',
      amount: 2000000,
      clearedProfit: 2450000,
      requestDate: 'امروز - ۱۰:۱۵',
    },
    {
      id: 'PAY-702',
      recipientType: 'تأمین‌کننده',
      recipientName: 'شرکت بازرگانی فناوری نوین',
      shaba: 'IR880120000000987654321001',
      bankName: 'بانک ملت',
      amount: 20000000,
      clearedProfit: 36950000,
      requestDate: 'امروز - ۱۱:۰۰',
    },
  ];

  const filteredRequests = payoutRequests.filter((r) => {
    if (cycleType === 'seller_daily') return r.recipientType === 'دراپ‌شیپر';
    if (cycleType === 'supplier_monthly') return r.recipientType === 'تأمین‌کننده';
    return true;
  });

  const handleApprove = (id: string, name: string, amount: number) => {
    setApprovedMap(prev => ({ ...prev, [id]: true }));
    success(`حواله پایا به مبلغ ${formatPrice(amount)} تومان برای ${name} صادر شد.`);
  };

  const handleConfirmReject = () => {
    if (!rejectingPayout) return;
    warning(`درخواست تسویه ${rejectingPayout.id} به دلیل نقص تطبیق بانکی متوقف گردید.`);
    setRejectingPayout(null);
  };

  const handleExecuteBatchSuppliers = () => {
    executeSupplierMonthlyPayouts();
    success('تسویه ماهانه کلیه تأمین‌کنندگان محاسبه و حواله‌های تجمیعی پایا صادر گردید.');
  };

  const handleExportPayaBatch = () => {
    const lines = [
      '# DRUP CENTRAL PAYA BATCH EXPORT - FORMAT SHAPARAK/PAYA',
      `# Date: ${new Date().toISOString()}`,
      `# Total Records: ${payoutRequests.length}`,
      ...payoutRequests.map(r => `${r.shaba},${r.amount},${r.recipientName.replace(/,/g, ' ')},${r.id}`)
    ].join('\n');

    const blob = new Blob([lines], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `paya-batch-${Date.now()}.txt`;
    a.click();
    URL.revokeObjectURL(url);
    success('فایل متنی استاندارد حواله گروهی پایا دانلود شد.');
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">چرخه تسویه‌حساب (سلر روزانه پایا / تأمین‌کننده ماهانه)</h3>
          <span className="text-[10px] bg-amber-50 text-amber-700 font-bold px-2 py-0.5 rounded-full border border-amber-200">
            {toPersianDigits(filteredRequests.length)} درخواست
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          تسویه دراپ‌شیپرها به‌صورت روزانه پس از آزادسازی قطعی انجام می‌شود؛ تسویه تأمین‌کنندگان در چرخه منظم ماهانه اجرا می‌گردد.
        </p>

        {/* Cycle Sub-tabs (Requirement 18) */}
        <div className="flex bg-slate-100 p-1 rounded-xl text-[11px] font-bold gap-1">
          <button
            type="button"
            onClick={() => setCycleType('all')}
            className={`flex-1 py-1 rounded-lg transition-all ${cycleType === 'all' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            همه تسویه‌ها
          </button>
          <button
            type="button"
            onClick={() => setCycleType('seller_daily')}
            className={`flex-1 py-1 rounded-lg transition-all ${cycleType === 'seller_daily' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            صف روزانه سلرها (آزادشده)
          </button>
          <button
            type="button"
            onClick={() => setCycleType('supplier_monthly')}
            className={`flex-1 py-1 rounded-lg transition-all ${cycleType === 'supplier_monthly' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            دوره ماهانه تأمین‌کنندگان
          </button>
        </div>

        <div className="grid grid-cols-2 gap-2 pt-1">
          <button
            onClick={handleExportPayaBatch}
            className="py-2 bg-slate-900 hover:bg-slate-800 text-white font-bold text-[10px] rounded-xl flex items-center justify-center gap-1 shadow-xs"
          >
            <Download className="w-3.5 h-3.5 text-blue-400"  strokeWidth={1.5}/>
            <span>خروجی فایل پایا بانک مرکزی</span>
          </button>
          <button
            onClick={handleExecuteBatchSuppliers}
            className="py-2 bg-indigo-600 hover:bg-indigo-700 text-white font-bold text-[10px] rounded-xl flex items-center justify-center gap-1 shadow-xs"
          >
            <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>اجرای تسویه ماهانه تأمین‌کنندگان</span>
          </button>
        </div>
      </div>

      <div className="space-y-3">
        {filteredRequests.map((req) => {
          const isApproved = approvedMap[req.id];
          return (
            <div
              key={req.id}
              className="p-3.5 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-2.5 text-xs"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <span className="font-black text-slate-800 font-mono">{req.id}</span>
                  <span className="text-[10px] text-slate-400 block mt-0.5">{req.requestDate}</span>
                </div>
                <span className="text-[9px] bg-blue-50 text-blue-700 font-bold px-2 py-0.5 rounded-full border border-blue-200">
                  {req.recipientType}
                </span>
              </div>

              <div>
                <span className="font-bold text-slate-900 block">{req.recipientName}</span>
                <span className="text-[11px] font-mono text-slate-500 mt-0.5 block tracking-wider" dir="ltr">
                  {req.shaba} ({req.bankName})
                </span>
              </div>

              <div className="p-2 bg-slate-50 rounded-xl space-y-1 text-[11px]">
                <div className="flex justify-between">
                  <span className="text-slate-400">موجودی قطعی آزاد:</span>
                  <span className="font-bold text-slate-800">{formatPrice(req.clearedProfit)} ت</span>
                </div>
                <div className="flex justify-between text-emerald-700 font-black">
                  <span>مبلغ درخواستی تسویه:</span>
                  <span>{formatPrice(req.amount)} تومان</span>
                </div>
              </div>

              {isApproved ? (
                <div className="p-2 bg-emerald-50 text-emerald-700 text-[11px] font-bold rounded-xl text-center flex items-center justify-center gap-1.5">
                  <CheckCircle2 className="w-4 h-4 text-emerald-600"  strokeWidth={1.5}/>
                  <span>حواله پایا صادر شد • سند مالی در دفتر کل ثبت گردید</span>
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
                  <button
                    onClick={() => handleApprove(req.id, req.recipientName, req.amount)}
                    className="py-2 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[11px] rounded-xl transition-colors"
                  >
                    تأیید و صدور حواله پایا
                  </button>
                  <button
                    onClick={() => setRejectingPayout(req)}
                    className="py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl transition-colors"
                  >
                    رد / مسدودی موقت
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>

      <ApprovedManualTransfers formatPrice={formatPrice} />

      <ConfirmDialog
        isOpen={!!rejectingPayout}
        title="توقف درخواست تسویه‌حساب"
        message={`آیا از مسدودسازی موقت درخواست ${rejectingPayout?.id} متعلق به ${rejectingPayout?.recipientName} اطمینان دارید؟`}
        confirmText="تأیید توقف تسویه"
        isDanger={true}
        onConfirm={handleConfirmReject}
        onCancel={() => setRejectingPayout(null)}
      />
    </div>
  );
};
