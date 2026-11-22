import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { financeApi, LedgerEntryDto } from '../../services/financeApi';
import { storeApi } from '../../services/storeApi';
import { apiRequest } from '../../services/apiClient';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { FileEdit, CheckCircle2, History } from 'lucide-react';

interface TargetOption {
  id: string;
  name: string;
}

interface AdjustmentLogRow {
  id: string;
  date: string;
  account: string;
  type: string;
  amount: number;
  reason: string;
  operator: string;
}

export const FinanceAdjustmentsTab: React.FC = () => {
  const { formatPrice } = useStore();
  const { success, error } = useToast();
  const [accountType, setAccountType] = useState<'supplier' | 'seller'>('supplier');
  const [targetId, setTargetId] = useState('');
  const [adjustmentType, setAdjustmentType] = useState<'debit' | 'credit'>('debit');
  const [amount, setAmount] = useState('');
  const [reason, setReason] = useState('');
  const [applied, setApplied] = useState(false);
  const [serverLedger, setServerLedger] = useState<LedgerEntryDto[]>([]);
  const [suppliers, setSuppliers] = useState<TargetOption[]>([]);
  const [stores, setStores] = useState<TargetOption[]>([]);
  const [listsLoading, setListsLoading] = useState(true);
  const [listsError, setListsError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submittedDocId, setSubmittedDocId] = useState<string | null>(null);

  const loadLedger = useCallback(async () => {
    try {
      const { entries } = await financeApi.getLedger();
      setServerLedger(entries);
    } catch (err) {
      console.warn('Failed to load ledger:', err);
    }
  }, []);

  useEffect(() => {
    loadLedger();
    // Target lists load in parallel with the ledger. A mistyped id is
    // exactly how a 404 or a wrong-account document happens, so the form
    // never falls back to a free-text input: on a load failure the
    // submit stays disabled until the lists arrive.
    (async () => {
      setListsLoading(true);
      setListsError(null);
      try {
        const [supList, storeList] = await Promise.all([
          apiRequest<any[]>('/admin/suppliers/list', { method: 'GET', auth: true }),
          storeApi.getAllStores(),
        ]);
        setSuppliers((Array.isArray(supList) ? supList : []).map((s: any) => ({ id: s.id, name: s.name || s.id })));
        setStores((Array.isArray(storeList) ? storeList : []).map((s: any) => ({ id: s.id, name: s.name || s.id })));
      } catch (err: any) {
        setListsError(err?.message || 'خطا در دریافت فهرست طرف‌های حساب');
      } finally {
        setListsLoading(false);
      }
    })();
  }, [loadLedger]);

  const targetOptions = accountType === 'supplier' ? suppliers : stores;
  const selectedTarget = targetOptions.find((t) => t.id === targetId);
  const targetLabel = selectedTarget ? `${selectedTarget.name} (${selectedTarget.id})` : targetId;

  // History is built ONLY from real ledger legs: filter to adjustment
  // documents (reason starts with "سند تعدیل"), group the two legs by
  // their shared docId (relatedOrderOrDispute), one row per document
  // using the payable leg only. The platform counterpart leg is never
  // a row. No mock constants anywhere in this file.
  const adjustmentLogs: AdjustmentLogRow[] = (() => {
    const byDoc = new Map<string, LedgerEntryDto>();
    const sorted = [...serverLedger].sort((a, b) => {
      const ta = new Date(a.createdAt || a.date || 0).getTime();
      const tb = new Date(b.createdAt || b.date || 0).getTime();
      return tb - ta;
    });
    for (const leg of sorted) {
      if (!leg.reason || !leg.reason.startsWith('سند تعدیل')) continue;
      const docId = leg.relatedOrderOrDispute;
      if (!docId || byDoc.has(docId)) continue;
      const isPayable = leg.targetAccount?.startsWith('supplier_payable:') || leg.targetAccount?.startsWith('store_payable:');
      if (isPayable) byDoc.set(docId, leg);
    }
    return [...byDoc.entries()].map(([docId, leg]) => {
      const rawId = (leg.targetAccount || '').replace('supplier_payable:', '').replace('store_payable:', '');
      const list = leg.targetType === 'supplier' ? suppliers : leg.targetType === 'seller' ? stores : [];
      const found = list.find((t) => t.id === rawId);
      return {
        id: docId,
        date: leg.createdAt ? new Date(leg.createdAt).toLocaleDateString('fa-IR') : (leg.date || 'امروز'),
        account: found ? `${found.name} (${rawId})` : leg.targetAccount,
        type: leg.type === 'debit' ? 'کسورات (بدهکار)' : 'بستانکار (افزایش مانده)',
        amount: leg.amount,
        reason: leg.reason.replace(/^سند تعدیل\s*/, ''),
        operator: leg.operator,
      };
    });
  })();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!targetId.trim()) {
      error('طرف حساب را انتخاب کنید.');
      return;
    }
    const parsed = amount.trim() ? Number(amount.trim()) : NaN;
    if (!amount.trim() || !Number.isFinite(parsed) || Math.round(parsed) <= 0) {
      error('مبلغ تعدیل باید عددی بزرگ‌تر از صفر باشد.');
      return;
    }
    if (!reason.trim()) {
      error('علت و سند عطف حسابداری الزامی است.');
      return;
    }
    if (!/^[A-Za-z0-9][A-Za-z0-9_-]{1,63}$/.test(targetId.trim())) {
      error('شناسه طرف حساب معتبر نیست.');
      return;
    }
    setConfirmOpen(true);
  };

  const confirmMessage = `طرف حساب: ${targetLabel}؛ نوع عملیات: ${adjustmentType === 'debit' ? 'بدهکار (کاهش مانده / کسورات)' : 'بستانکار (افزایش مانده)'}؛ مبلغ: ${amount.trim() ? `${formatPrice(Math.round(Number(amount.trim())))} تومان` : '—'}؛ علت: ${reason.trim()}. این سند به‌صورت دوبل و غیرقابل ویرایش ثبت می‌شود؛ اصلاح آن فقط با صدور سند معکوس جدید ممکن است.`;

  const handleConfirmAdjustment = async () => {
    setConfirmOpen(false);
    setSubmitting(true);
    try {
      const res = await financeApi.createAdjustment({
        targetKind: accountType,
        targetId: targetId.trim(),
        direction: adjustmentType,
        amount: Math.round(Number(amount.trim())),
        reason: reason.trim(),
      });
      success(`سند تعدیل ${res.docId} با موفقیت صادر گردید.`);
      setSubmittedDocId(res.docId);
      setTargetId('');
      setAmount('');
      setReason('');
      await loadLedger();
      setApplied(true);
      setTimeout(() => setApplied(false), 2500);
    } catch (err: any) {
      error(err?.message || 'صدور سند تعدیل انجام نشد.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* 1. New Adjustment Form */}
      <form onSubmit={handleSubmit} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-blue-600">
          <FileEdit className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">صدور سند تعدیل و اصلاح حساب (سند معکوس)</h3>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          طبق بخش ۸ پروپوزال، تغییر مستقیم مانده‌ها ممنوع است و هرگونه اصلاح مالی با صدور سند حسابداری معکوس، دلیل مکتوب و شناسه کارشناس مسئول ثبت دائمی می‌گردد.
        </p>

        {listsError && (
          <div className="rounded-xl bg-rose-50 border border-rose-200 p-2.5 text-[11px] text-rose-700">
            {listsError}
          </div>
        )}

        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">نوع طرف حساب</label>
            <select
              value={accountType}
              onChange={(e) => { setAccountType(e.target.value as 'supplier' | 'seller'); setTargetId(''); }}
              className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            >
              <option value="supplier">تأمین‌کننده</option>
              <option value="seller">فروشنده (فروشگاه)</option>
            </select>
          </div>

          <div>
            <label className="text-[10px] text-slate-600 block mb-1">طرف حساب هدف</label>
            <select
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
              disabled={listsLoading || !!listsError}
              className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden disabled:opacity-50"
            >
              <option value="">{listsLoading ? 'در حال دریافت…' : 'انتخاب کنید'}</option>
              {targetOptions.map((t) => (
                <option key={t.id} value={t.id}>{t.name} ({t.id})</option>
              ))}
            </select>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">نوع عملیات سند</label>
            <select
              value={adjustmentType}
              onChange={(e) => setAdjustmentType(e.target.value as 'debit' | 'credit')}
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
              placeholder="مبلغ (تومان)"
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
            placeholder="علت تعدیل"
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            required
          />
        </div>

        <button
          type="submit"
          disabled={submitting || listsLoading || !!listsError}
          className="w-full py-2 bg-[#007dfa] hover:bg-blue-600 text-white font-bold text-xs rounded-xl shadow-xs transition-colors disabled:opacity-50"
        >
          {submitting ? 'در حال ثبت…' : applied ? 'سند معکوس در دفتر مالی ثبت گردید' : 'ثبت سند حسابداری معکوس'}
        </button>
        {submittedDocId && (
          <p className="text-[10px] text-emerald-700 text-center">آخرین سند صادرشده: <span className="font-mono font-bold" dir="ltr">{submittedDocId}</span></p>
        )}
      </form>

      <ConfirmDialog
        isOpen={confirmOpen}
        title="ثبت قطعی سند تعدیل؟"
        message={confirmMessage}
        confirmText="ثبت قطعی سند"
        isDanger={true}
        onConfirm={handleConfirmAdjustment}
        onCancel={() => setConfirmOpen(false)}
      />

      {/* 2. Adjustment Ledger History */}
      <div className="space-y-2">
        <h4 className="text-xs font-bold text-slate-800">آخرین اسناد تعدیل و اصلاحی ثبت‌شده</h4>

        {adjustmentLogs.length === 0 && (
          <div className="p-3 bg-white rounded-2xl border border-slate-200/90 shadow-2xs text-[11px] text-slate-500">
            هنوز سند تعدیلی ثبت نشده است.
          </div>
        )}

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
