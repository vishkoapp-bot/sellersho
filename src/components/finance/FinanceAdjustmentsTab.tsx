import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { financeApi, LedgerEntryDto } from '../../services/financeApi';
import { isMockMode } from '../../services/apiClient';
import { FileEdit, CheckCircle2, History } from 'lucide-react';

export const FinanceAdjustmentsTab: React.FC = () => {
  const { formatPrice } = useStore();
  const { success } = useToast();
  const [accountType, setAccountType] = useState('supplier');
  const [targetAccount, setTargetAccount] = useState('شرکت بازرگانی فناوری نوین (SUP-8820)');
  const [adjustmentType, setAdjustmentType] = useState('debit');
  const [amount, setAmount] = useState('150000');
  const [reason, setReason] = useState('تعدیل هزینه انبارداری و بسته‌بندی محموله INB-9941');
  const [applied, setApplied] = useState(false);
  const [serverLedger, setServerLedger] = useState<LedgerEntryDto[]>([]);

  useEffect(() => {
    if (!isMockMode()) {
      financeApi.getLedger()
        .then((data) => {
          if (Array.isArray(data)) setServerLedger(data);
        })
        .catch((err) => console.warn('Failed to load ledger:', err));
    }
  }, []);

  const defaultMockLogs = [
    {
      id: 'ADJ-301',
      date: '۱۴۰۵/۰۷/۰۱',
      account: 'شرکت بازرگانی فناوری نوین',
      type: 'کسورات (بدهکار)',
      amount: 1450000,
      reason: 'کسر هزینه خدمات انبارداری ماهانه طبق مصوبه تعرفه',
      operator: 'کارشناس ارشد مالی',
    },
    {
      id: 'ADJ-302',
      date: '۱۴۰۵/۰۶/۲۹',
      account: 'فروشگاه بی‌بی استایل',
      type: 'بستانکار (افزایش مانده)',
      amount: 400000,
      reason: 'تخصیص پاداش تحقق پله ۳ فروش تجمیعی دوره قبل',
      operator: 'سیستم محاسباتی Action Scheduler',
    },
  ];

  const adjustmentLogs = (!isMockMode() && serverLedger.length > 0)
    ? serverLedger.map((l) => ({
        id: l.id,
        date: l.createdAt ? new Date(l.createdAt).toLocaleDateString('fa-IR') : 'امروز',
        account: l.targetAccount,
        type: l.type === 'debit' ? 'کسورات (بدهکار)' : 'بستانکار (افزایش مانده)',
        amount: l.amount,
        reason: l.reason,
        operator: l.operator,
      }))
    : defaultMockLogs;

  const handleApply = (e: React.FormEvent) => {
    e.preventDefault();
    setApplied(true);
    success('سند حسابداری معکوس با موفقیت صادر گردید.');
    setTimeout(() => setApplied(false), 2500);
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* 1. New Adjustment Form */}
      <form onSubmit={handleApply} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-blue-600">
          <FileEdit className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">صدور سند تعدیل و اصلاح حساب (سند معکوس)</h3>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          طبق بخش ۸ پروپوزال، تغییر مستقیم مانده‌ها ممنوع است و هرگونه اصلاح مالی با صدور سند حسابداری معکوس، دلیل مکتوب و شناسه کارشناس مسئول ثبت دائمی می‌گردد.
        </p>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">طرف حساب هدف</label>
          <input
            type="text"
            value={targetAccount}
            onChange={(e) => setTargetAccount(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            required
          />
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">نوع عملیات سند</label>
            <select
              value={adjustmentType}
              onChange={(e) => setAdjustmentType(e.target.value)}
              className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            >
              <option value="debit">بدهکار کردن (کاهش مانده / کسورات)</option>
              <option value="credit">بستانکار کردن (افزایش مانده / پاداش)</option>
            </select>
          </div>

          <div>
            <label className="text-[10px] text-slate-600 block mb-1">مبلغ تعدیل (تومان)</label>
            <input
              type="number"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
              required
            />
          </div>
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">علت و سند عطف حسابداری</label>
          <input
            type="text"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            required
          />
        </div>

        <button
          type="submit"
          className="w-full py-2 bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
        >
          {applied ? 'سند معکوس در دفتر مالی ثبت گردید' : 'ثبت سند حسابداری معکوس'}
        </button>
      </form>

      {/* 2. Adjustment Ledger History */}
      <div className="space-y-2">
        <h4 className="text-xs font-bold text-slate-800">آخرین اسناد تعدیل و اصلاحی ثبت‌شده</h4>

        {adjustmentLogs.map((log) => (
          <div key={log.id} className="p-3 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-1 text-xs">
            <div className="flex items-center justify-between">
              <span className="font-mono font-black text-slate-800">{log.id}</span>
              <span className="text-[10px] text-slate-400">{log.date}</span>
            </div>
            <div className="flex items-center justify-between font-bold">
              <span>{log.account}</span>
              <span className={log.type.includes('بدهکار') ? 'text-rose-600' : 'text-emerald-600'}>
                {log.type.includes('بدهکار') ? '-' : '+'}{formatPrice(log.amount)} تومان
              </span>
            </div>
            <p className="text-[10px] text-slate-500 pt-1 border-t border-slate-100">{log.reason}</p>
          </div>
        ))}
      </div>
    </div>
  );
};
