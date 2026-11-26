import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { apiRequest, isMockMode } from '../../services/apiClient';
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

function ApprovedManualTransfers() {
  const { adminSession, formatPrice } = useStore();
  const { success } = useToast();
  const canConfirm = adminSession?.role === 'admin' || adminSession?.role === 'super_admin';
  const [settlements, setSettlements] = useState<ApprovedSettlement[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [trackingRef, setTrackingRef] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [confirmError, setConfirmError] = useState('');
  const heading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    void apiRequest<{ settlements: ApprovedSettlement[] }>('/finance/settlements/approved', { auth: true, signal: controller.signal })
      .then(data => {
        if (controller.signal.aborted) return;
        if (!Array.isArray(data?.settlements)) throw new Error('پاسخ فهرست تسویه‌ها کامل دریافت نشد.');
        setSettlements(data.settlements);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setLoadError(error instanceof Error ? error.message : 'دریافت تسویه‌ها انجام نشد.');
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [attempt]);

  const reload = () => {
    setLoading(true);
    setLoadError('');
    setAttempt(value => value + 1);
  };

  const handleConfirm = async (event: React.FormEvent<HTMLFormElement>, id: string) => {
    event.preventDefault();
    const reference = trackingRef.trim();
    if (!canConfirm || confirming || expandedId !== id || !reference) return;
    setConfirmError('');
    setConfirming(true);
    try {
      await apiRequest(`/admin/payouts/${encodeURIComponent(id)}/confirm-manual-transfer`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({
          transfer_reference: reference,
          transfer_date: new Date().toISOString(),
        }),
      });
      success('انتقال بانکی تأیید شد و سند حسابداری در دفتر کل ثبت گردید.');
      setTrackingRef('');
      setExpandedId(null);
      setSettlements(items => items.filter(item => item.id !== id));
      heading.current?.focus();
      reload();
    } catch (error: unknown) {
      setConfirmError(error instanceof Error ? error.message : 'تأیید انتقال انجام نشد.');
    } finally {
      setConfirming(false);
    }
  };

  return (
    <section aria-labelledby="approved-transfers-heading" className="space-y-3">
      <div className="space-y-1 rounded-2xl border border-amber-200 bg-amber-50 p-4">
        <h3 id="approved-transfers-heading" ref={heading} tabIndex={-1} className="text-base font-bold text-amber-950">تسویه‌های تأییدشده در انتظار واریز</h3>
        <p className="text-sm leading-6 text-amber-900">
          {canConfirm ? 'پس از انجام انتقال بانکی، ثبت شماره پیگیری، سند حسابداری را در دفتر کل ایجاد می‌کند.' : 'مدیر پس از انتقال بانکی، شماره پیگیری واریز را ثبت می‌کند.'}
        </p>
      </div>
      {loading && <p role="status" className="rounded-xl bg-white p-3 text-sm text-slate-600">در حال دریافت تسویه‌ها…</p>}
      {loadError && (
        <div className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">
          <p role="alert" className="[overflow-wrap:anywhere]">{loadError}</p>
          <button type="button" onClick={reload} className="mt-1 min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>
        </div>
      )}
      {!loading && !loadError && settlements.length === 0 && <p className="rounded-xl bg-white p-3 text-sm text-slate-600">تسویه‌ای در انتظار واریز نیست.</p>}
      {!loading && !loadError && settlements.map(item => (
        <article key={item.id} className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 text-sm [overflow-wrap:anywhere]">
          <div>
            <h4 className="font-bold text-slate-900">{item.recipientName}</h4>
            <bdi className="font-mono text-xs text-slate-600" dir="ltr">{item.id}</bdi>
          </div>
          <div>
            <span className="block text-slate-600">شماره شبای مقصد</span>
            <bdi className="font-mono text-slate-900" dir="ltr">{item.shaba}</bdi>
            {item.bankName && <span className="block text-slate-600">{item.bankName}</span>}
          </div>
          <div className="flex flex-wrap justify-between gap-2 font-bold text-emerald-800">
            <span>مبلغ</span>
            <span>{formatPrice(item.amount)} تومان</span>
          </div>
          {canConfirm && (
            expandedId === item.id ? (
              <form onSubmit={event => void handleConfirm(event, item.id)} className="space-y-2 border-t border-slate-200 pt-3">
                <label className="block space-y-1">
                  <span className="block font-bold">شماره پیگیری انتقال بانکی</span>
                  <input autoFocus value={trackingRef} onChange={event => setTrackingRef(event.target.value)} placeholder="شماره پیگیری انتقال بانکی" dir="ltr" required disabled={confirming}
                    className="min-h-11 w-full min-w-0 rounded-xl border border-slate-300 px-3 font-mono text-base focus-visible:outline-2 focus-visible:outline-brand" />
                </label>
                {confirmError && <p role="alert" className="rounded-lg bg-rose-50 p-2 text-rose-800">{confirmError}</p>}
                {confirming && <p role="status" className="text-slate-600">در حال ثبت واریز…</p>}
                <div className="grid grid-cols-2 gap-2">
                  <button type="submit" disabled={confirming || !trackingRef.trim()} className="min-h-11 rounded-xl bg-brand-deep px-2 font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50">
                    تأیید واریز و ثبت سند
                  </button>
                  <button type="button" disabled={confirming} onClick={event => {
                    const card = event.currentTarget.closest('article');
                    setExpandedId(null);
                    setTrackingRef('');
                    requestAnimationFrame(() => card?.querySelector('button')?.focus());
                  }} className="min-h-11 rounded-xl bg-slate-100 px-2 font-bold text-slate-700 focus-visible:outline-2 focus-visible:outline-brand disabled:opacity-50">
                    انصراف
                  </button>
                </div>
              </form>
            ) : (
              <button type="button" disabled={confirming} onClick={() => {
                setTrackingRef('');
                setConfirmError('');
                setExpandedId(item.id);
              }} className="min-h-11 w-full rounded-xl bg-brand-deep px-3 font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50 sm:w-auto">
                ثبت انتقال بانکی
              </button>
            )
          )}
        </article>
      ))}
    </section>
  );
}

export function FinancePayoutsTab() {
  return isMockMode() ? <SamplePayouts /> : <ApprovedManualTransfers />;
}

const SamplePayouts: React.FC = () => {
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
