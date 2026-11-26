import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { isMockMode } from '../../services/apiClient';
import { adminSectionHref } from '../admin/AdminNavigation';
import { Receipt, CheckCircle2, Search, RefreshCw, AlertCircle } from 'lucide-react';

const paymentLinks = [
  { section: 'finance-pending-payments', label: 'سفارش‌های پرداخت‌نشده' },
  { section: 'finance-callback-retries', label: 'سوابق پرداخت‌های ناموفق' },
] as const;

export function FinanceReconciliationTab({ onNavigateTab }: { onNavigateTab?: (section: string) => void }) {
  const { adminSession } = useStore();
  const canViewCallbackHistory = adminSession?.role === 'admin' || adminSession?.role === 'super_admin';
  if (isMockMode()) return <SampleReconciliation />;
  return (
    <section aria-labelledby="shaparak-heading" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4">
      <h3 id="shaparak-heading" className="flex items-center gap-2 text-base font-bold text-slate-900">
        <Receipt aria-hidden="true" className="size-5 shrink-0 text-brand-deep" />
        تطبیق تراکنش‌های شاپرک
      </h3>
      <p className="text-sm leading-6 text-slate-700">استعلام مستقیم شناسه مرجع شاپرک در این پنل در دسترس نیست.</p>
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
        {paymentLinks.filter(({ section }) => section !== 'finance-callback-retries' || canViewCallbackHistory).map(({ section, label }) => (
          <a key={section} href={adminSectionHref(section)} onClick={event => {
            if (!onNavigateTab || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
            event.preventDefault();
            onNavigateTab(section);
          }} className="inline-flex min-h-11 items-center rounded-lg border border-brand-border px-3 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:outline-2 focus-visible:outline-brand">
            {label}
          </a>
        ))}
      </div>
    </section>
  );
}

function SampleReconciliation() {
  const { formatPrice, toPersianDigits } = useStore();
  const { success, error, info } = useToast();
  const [searchRrn, setSearchRrn] = useState('');
  const [isSearching, setIsSearching] = useState(false);

  const [transactions, setTransactions] = useState([
    {
      id: 'TXN-88091',
      orderNumber: 'ORD-702774',
      storeName: 'فروشگاه بی‌بی استایل',
      gateway: 'به‌پرداخت ملت (شاپرک)',
      rrn: '109283746501',
      amount: 5365738,
      status: 'matched',
      time: 'امروز - ۱۲:۳۰',
    },
    {
      id: 'TXN-88090',
      orderNumber: 'ORD-702775',
      storeName: 'تک‌هاب پلاس',
      gateway: 'سداد بانک ملی (شاپرک)',
      rrn: '109283746502',
      amount: 4999000,
      status: 'matched',
      time: 'امروز - ۱۲:۱۵',
    },
    {
      id: 'TXN-88089',
      orderNumber: 'ORD-702770',
      storeName: 'سلرشو مرکزی',
      gateway: 'سامان کیش (شاپرک)',
      rrn: '109283746503',
      amount: 2040000,
      status: 'matched',
      time: 'امروز - ۱۱:۴۵',
    },
  ]);

  const handleInquireRrn = (e: React.FormEvent) => {
    e.preventDefault();
    if (!searchRrn.trim()) return;
    setIsSearching(true);

    setTimeout(() => {
      setIsSearching(false);
      const query = searchRrn.trim();
      const existing = transactions.find(t => t.rrn === query);

      if (existing) {
        success(`تراکنش ${existing.id} با شناسه مرجع ${query} تطبیق‌یافته و معتبر است.`);
      } else if (query.length >= 8) {
        const newTx = {
          id: `TXN-${Math.floor(10000 + Math.random() * 90000)}`,
          orderNumber: `ORD-${Math.floor(100000 + Math.random() * 900000)}`,
          storeName: 'فروشگاه بی‌بی استایل',
          gateway: 'شاپرک مرکزی',
          rrn: query,
          amount: 1850000,
          status: 'matched',
          time: 'هم‌اکنون',
        };
        setTransactions(prev => [newTx, ...prev]);
        success(`شناسه مرجع ${query} از سوئیچ شاپرک استعلام شد و با فاکتور تطبیق داده شد.`);
        setSearchRrn('');
      } else {
        error('شناسه مرجع RRN باید حداقل ۸ رقم باشد.');
      }
    }, 400);
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Receipt className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">تطبیق خودکار تراکنش‌های درگاه شاپرک</h3>
          </div>
          <span className="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full border border-emerald-200">
            تطبیق ۱۰۰٪
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          ثبت قطعی سفارش فقط با تطبیق شناسه مرجع شاپرک (RRN)، مبلغ و ارز ریالی سفارش با سرور پذیرنده انجام می‌شود (بخش ۷ و ۸ پروپوزال).
        </p>

        {/* Live RRN Inquiry Form */}
        <form onSubmit={handleInquireRrn} className="pt-1 flex gap-2">
          <input
            type="text"
            placeholder="استعلام دستی شناسه مرجع شاپرک (RRN)..."
            value={searchRrn}
            onChange={(e) => setSearchRrn(e.target.value)}
            className="flex-1 text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-mono tracking-wider focus:outline-hidden"
            dir="ltr"
          />
          <button
            type="submit"
            disabled={isSearching || !searchRrn.trim()}
            className="px-3 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-bold text-[11px] rounded-xl transition-colors shrink-0 flex items-center gap-1"
          >
            {isSearching ? <RefreshCw className="w-3.5 h-3.5 animate-spin"  strokeWidth={1.5}/> : <Search className="w-3.5 h-3.5"  strokeWidth={1.5}/>}
            <span>استعلام RRN</span>
          </button>
        </form>
      </div>

      <div className="space-y-2.5">
        {transactions.map((tx) => (
          <div
            key={tx.id}
            className="p-3 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-2 text-xs"
          >
            <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
              <div>
                <span className="font-mono font-black text-slate-800">{tx.id}</span>
                <span className="text-[10px] text-slate-400 block mt-0.5">سفارش: {tx.orderNumber}</span>
              </div>
              <span className="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full">
                تطبیق موفق
              </span>
            </div>

            <div className="text-[11px] text-slate-600 space-y-1">
              <div className="flex justify-between">
                <span className="text-slate-400">درگاه پذیرنده:</span>
                <span>{tx.gateway}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-400">شناسه مرجع (RRN):</span>
                <span className="font-mono font-bold" dir="ltr">{tx.rrn}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-400">ویترین:</span>
                <span className="font-bold text-slate-800">{tx.storeName}</span>
              </div>
            </div>

            <div className="pt-1.5 border-t border-slate-100 flex items-center justify-between font-bold">
              <span className="text-[10px] text-slate-400 font-normal">مبلغ واریز به حساب تجاری:</span>
              <span className="text-emerald-600 font-black">{formatPrice(tx.amount)} تومان</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
