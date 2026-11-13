import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { financeApi, DisputeDto } from '../../services/financeApi';
import { isMockMode } from '../../services/apiClient';
import { ShieldAlert, CheckCircle2, RotateCcw, Truck, Warehouse, Check, UserX, AlertCircle, FileText } from 'lucide-react';

export const SupportDisputesTab: React.FC = () => {
  const { disputes, resolveDisputeAndAdjust, adjustmentRecords, formatPrice, toPersianDigits } = useStore();
  const { success, warning, error } = useToast();
  const [filterType, setFilterType] = useState<'all' | 'delivery_undelivered' | 'return_7day'>('all');
  const [serverDisputes, setServerDisputes] = useState<DisputeDto[]>([]);

  const fetchDisputes = () => {
    if (!isMockMode()) {
      financeApi.getDisputes()
        .then((data) => {
          if (Array.isArray(data)) setServerDisputes(data);
        })
        .catch((err) => console.warn('Failed to load live disputes:', err));
    }
  };

  useEffect(() => {
    fetchDisputes();
  }, []);

  const activeDisputesList = (!isMockMode() && serverDisputes.length > 0)
    ? serverDisputes.map((d) => ({
        id: d.id,
        orderNumber: d.orderNumber,
        type: d.type,
        customerName: d.customerName,
        productTitle: d.productTitle,
        claimAmount: d.claimAmount,
        reason: d.reason,
        storeName: d.storeId,
        supplierName: d.supplierName || 'تأمین‌کننده رسمی',
        status: d.status,
        responsibleParty: d.responsibleParty,
        resolutionAction: d.resolutionAction as any,
      }))
    : disputes;

  const filteredDisputes = activeDisputesList.filter((d) => {
    if (filterType === 'all') return true;
    return d.type === filterType;
  });

  const handleResolveAction = async (
    disputeId: string,
    responsibleParty: string,
    actionType: 'release_to_seller' | 'refund_carrier' | 'refund_warehouse' | 'refund_supplier' | 'refund_seller'
  ) => {
    if (!isMockMode()) {
      try {
        await financeApi.resolveDispute(disputeId, {
          action: actionType,
          responsibleParty,
        });
        fetchDisputes();
      } catch (err: any) {
        // B1 (round 2): a failed backend resolve must NOT toast success
        // and must NOT flip local state — the dispute stays pending and
        // the admin is told the resolution did not happen.
        console.warn('Backend dispute resolve notice:', err);
        error(err?.message || 'ثبت حکم داوری در سرور انجام نشد؛ پرونده همچنان در انتظار رسیدگی است.');
        return;
      }
    }

    resolveDisputeAndAdjust(disputeId, responsibleParty, actionType);
    if (actionType === 'release_to_seller') {
      success('تحویل مرسوله تأیید شد؛ ادعای عدم تحویل رد و درآمد به موجودی قابل برداشت سلر اضافه گردید.');
    } else {
      warning(`حکم استرداد به مشتری صادر شد و سند تعدیل بدهکار برای «${responsibleParty}» به ثبت رسید (کیف پول سلر کسر نگردید).`);
    }
  };

  return (
    <div className="space-y-3.5 pb-6 select-none">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-rose-600">
            <ShieldAlert className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">مرکز یکپارچه داوری اختلافات و دعاوی (عدم تحویل / مرجوعی)</h3>
          </div>
          <span className="text-[10px] bg-rose-50 text-rose-700 font-bold px-2 py-0.5 rounded-full border border-rose-200">
            {toPersianDigits(disputes.filter((d) => d.status === 'pending').length)} پرونده در جریان
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          دعاوی عدم تحویل مرسوله و مرجوعی ۷ روزه در این بخش داوری می‌شوند. در صورت مفقودی کالا در حمل، خسارت از شرکت باربری یا انبار کسر شده و کیف پول سلر مصون می‌ماند.
        </p>

        {/* Filter sub-tabs */}
        <div className="flex bg-slate-100 p-1 rounded-xl text-[11px] font-bold gap-1 pt-1">
          <button
            type="button"
            onClick={() => setFilterType('all')}
            className={`flex-1 py-1 rounded-lg transition-all ${filterType === 'all' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            همه پرونده‌ها ({toPersianDigits(disputes.length)})
          </button>
          <button
            type="button"
            onClick={() => setFilterType('delivery_undelivered')}
            className={`flex-1 py-1 rounded-lg transition-all ${filterType === 'delivery_undelivered' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            عدم تحویل ({toPersianDigits(disputes.filter((d) => d.type === 'delivery_undelivered').length)})
          </button>
          <button
            type="button"
            onClick={() => setFilterType('return_7day')}
            className={`flex-1 py-1 rounded-lg transition-all ${filterType === 'return_7day' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'}`}
          >
            مرجوعی ۷ روزه ({toPersianDigits(disputes.filter((d) => d.type === 'return_7day').length)})
          </button>
        </div>
      </div>

      <div className="space-y-3">
        {filteredDisputes.length === 0 ? (
          <div className="p-8 bg-white rounded-2xl border border-slate-100 text-center text-slate-400 text-xs">
            پرونده اختلافی در این دسته‌بندی وجود ندارد.
          </div>
        ) : (
          filteredDisputes.map((d) => {
            const isResolved = d.status === 'resolved';
            const isUndelivered = d.type === 'delivery_undelivered';

            return (
              <div
                key={d.id}
                className="p-3.5 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-2.5 text-xs"
              >
                <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-black text-slate-800">{d.id}</span>
                    <span className="text-[10px] text-slate-400">سفارش: {d.orderNumber}</span>
                    <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                      isUndelivered ? 'bg-amber-100 text-amber-900 border border-amber-300' : 'bg-blue-50 text-blue-700 border border-blue-200'
                    }`}>
                      {isUndelivered ? 'عدم تحویل مرسوله' : 'مرجوعی ۷ روزه'}
                    </span>
                  </div>
                  <span className="font-bold text-rose-600 text-xs">
                    مبلغ ادعا: {formatPrice(d.claimAmount)} تومان
                  </span>
                </div>

                <div>
                  <h4 className="font-bold text-slate-800">{d.productTitle}</h4>
                  <div className="bg-slate-50 p-2 rounded-xl mt-1.5 text-[10px] text-slate-600 space-y-0.5">
                    <div>خریدار: <strong className="text-slate-800">{d.customerName}</strong></div>
                    <div>ویترین فروشگاه: <strong className="text-blue-700">{d.storeName}</strong></div>
                    <div className="text-rose-700 pt-1 border-t border-slate-200/60 font-medium">
                      شرح گزارش خریدار: {d.reason}
                    </div>
                  </div>
                </div>

                {isResolved ? (
                  <div className="p-2.5 bg-emerald-50 border border-emerald-200 text-emerald-800 text-[10px] font-bold rounded-xl flex items-center gap-2">
                    <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0"  strokeWidth={1.5}/>
                    <div>
                      <div>حکم داوری صادر شد: مسئولیت مالی به حساب «{d.responsibleParty}» ثبت گردید.</div>
                      <span className="text-[9px] text-emerald-600 font-normal">
                        {d.responsibleParty?.includes('رد ادعا') || d.resolutionAction === 'release_to_seller'
                          ? 'درآمد به کیف پول سلر آزاد شد.'
                          : 'مبلغ به خریدار مسترد گردید؛ کیف پول سلر مصون ماند و سند تعدیل بدهکار ثبت شد.'}
                      </span>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-2 pt-1 border-t border-slate-100">
                    <span className="text-[10px] text-slate-500 font-bold block">
                      {isUndelivered
                        ? 'اقدام داوری پرونده عدم تحویل (بررسی نتیجه استعلام سامانه رهگیری پستی):'
                        : 'تعیین طرف مسئول پرداخت خسارت مرجوعی:'}
                    </span>

                    {isUndelivered ? (
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                        <button
                          type="button"
                          onClick={() => handleResolveAction(d.id, 'شرکت حمل‌ونقل و باربری', 'refund_carrier')}
                          className="py-2 px-2.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[10px] rounded-xl border border-rose-200 flex items-center justify-center gap-1 transition-colors text-right"
                        >
                          <Truck className="w-3.5 h-3.5 shrink-0"  strokeWidth={1.5}/>
                          <span>تأیید مفقودی در حمل (عودت به خریدار + کسر از باربری)</span>
                        </button>
                        <button
                          type="button"
                          onClick={() => handleResolveAction(d.id, 'تأیید تحویل به خریدار (رد ادعا)', 'release_to_seller')}
                          className="py-2 px-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[10px] rounded-xl flex items-center justify-center gap-1 shadow-2xs transition-colors"
                        >
                          <Check className="w-3.5 h-3.5 shrink-0"  strokeWidth={1.5}/>
                          <span>تأیید تحویل پستی (رد ادعا + آزادسازی فوری به سلر)</span>
                        </button>
                      </div>
                    ) : (
                      <div className="grid grid-cols-3 gap-1.5">
                        <button
                          type="button"
                          onClick={() => handleResolveAction(d.id, 'تأمین‌کننده کالا', 'refund_supplier')}
                          className="py-1.5 bg-indigo-50 hover:bg-indigo-100 text-indigo-700 font-bold text-[9px] rounded-lg"
                        >
                          کسر از تأمین‌کننده (ایراد کالا)
                        </button>
                        <button
                          type="button"
                          onClick={() => handleResolveAction(d.id, 'شرکت حمل‌ونقل و باربری', 'refund_carrier')}
                          className="py-1.5 bg-amber-50 hover:bg-amber-100 text-amber-700 font-bold text-[9px] rounded-lg"
                        >
                          کسر از شرکت حمل (خسارت)
                        </button>
                        <button
                          type="button"
                          onClick={() => handleResolveAction(d.id, 'دراپ‌شیپر', 'refund_seller')}
                          className="py-1.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[9px] rounded-lg"
                        >
                          کسر از دراپ‌شیپر (مغایرت)
                        </button>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      {/* Accounting Adjustments Log Preview */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center gap-1.5 text-slate-800">
          <FileText className="w-4 h-4 text-teal-600"  strokeWidth={1.5}/>
          <h4 className="text-xs font-bold">اسناد حسابداری تعدیل صادرشده اخیر (Double-Entry Adjustments)</h4>
        </div>
        <p className="text-[10px] text-slate-500">
          رعایت استاندارد سند معکوس و عطف: اسناد مالی داوری مستقیماً در دفتر کل ثبت شده و قابل حذف نیستند.
        </p>

        <div className="space-y-1.5 pt-1">
          {adjustmentRecords.slice(0, 3).map((adj) => (
            <div key={adj.id} className="p-2 bg-slate-50 border border-slate-100 rounded-xl flex items-center justify-between text-[10px]">
              <div>
                <span className="font-mono font-bold text-slate-800">{adj.id}</span> • <span>طرف حساب: {adj.targetAccount}</span>
                <span className="text-slate-400 block mt-0.5">{adj.reason}</span>
              </div>
              <div className="text-left font-mono shrink-0 mr-2">
                <span className="font-bold text-rose-700 block">-{formatPrice(adj.amount)} ت</span>
                <span className="text-[9px] text-slate-400">{adj.type === 'debit' ? 'بدهکار' : 'بستانکار'}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
