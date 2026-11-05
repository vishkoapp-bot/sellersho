import React from 'react';
import { ArrowRight, Clock, XCircle, Building2 } from 'lucide-react';

export interface SupplierAccountStateInfo {
  kind: 'pending' | 'rejected';
  companyName?: string;
  warehouseCity?: string;
  rejectionReason?: string;
}

interface Props extends SupplierAccountStateInfo {
  onBack: () => void;
}

// GAP 19b: dedicated account-state screen for a supplier owner whose
// account cannot log in yet. Rendered instead of the generic
// "Account is disabled" error when the password-login response
// carries the supplier_pending_approval / supplier_rejected code, and
// right after registration (a fresh account is pending by
// construction).
export const SupplierAccountStateScreen: React.FC<Props> = ({
  kind,
  companyName,
  warehouseCity,
  rejectionReason,
  onBack,
}) => {
  const pending = kind === 'pending';
  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]">
      <header className="bg-white border-b border-slate-100 px-4 py-3.5 flex items-center justify-between sticky top-0 z-10">
        <button
          onClick={onBack}
          className="w-9 h-9 rounded-xl bg-slate-50 hover:bg-slate-100 flex items-center justify-center text-slate-600 active:scale-95 transition-all cursor-pointer"
        >
          <ArrowRight className="w-5 h-5" />
        </button>
        <span className="text-[13px] font-black text-slate-800">وضعیت حساب تأمین‌کننده</span>
        <div className="w-9"></div>
      </header>

      <div className="p-4 flex-1 flex flex-col justify-center max-w-md mx-auto w-full">
        <div className="bg-white rounded-3xl p-5 border border-slate-100 shadow-[0_10px_36px_rgba(15,23,42,0.03)] space-y-4 text-center">
          <div
            className={`w-14 h-14 rounded-2xl flex items-center justify-center mx-auto ${
              pending ? 'bg-amber-50 text-amber-500' : 'bg-red-50 text-red-500'
            }`}
          >
            {pending ? <Clock className="w-7 h-7" /> : <XCircle className="w-7 h-7" />}
          </div>

          <div className="space-y-1.5">
            <h2 className="text-[15px] font-black text-slate-800">
              {pending ? 'در انتظار تأیید مدیریت' : 'درخواست تأمین‌کننده رد شد'}
            </h2>
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
              {pending
                ? 'حساب تأمین‌کننده شما در انتظار تأیید مدیریت است'
                : 'درخواست تأمین‌کننده شما رد شده است'}
            </p>
          </div>

          {(companyName || warehouseCity) && (
            <div className="bg-slate-50 rounded-2xl p-3 text-right space-y-1.5">
              {companyName && (
                <div className="flex items-center gap-1.5 text-[11px] text-slate-600">
                  <Building2 className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                  <span className="font-bold text-slate-500">شرکت:</span>
                  <span className="font-black text-slate-700">{companyName}</span>
                </div>
              )}
              {warehouseCity && (
                <div className="flex items-center gap-1.5 text-[11px] text-slate-600">
                  <span className="font-bold text-slate-500 pr-5">شهر انبار:</span>
                  <span className="font-black text-slate-700">{warehouseCity}</span>
                </div>
              )}
            </div>
          )}

          {pending ? (
            <p className="text-[11px] text-slate-400 leading-relaxed">
              پس از بررسی و تأیید مدیریت، حساب شما فعال می‌شود و می‌توانید با همین اطلاعات وارد پنل تأمین‌کننده شوید.
            </p>
          ) : (
            <>
              {rejectionReason && (
                <div className="text-right bg-red-50 border border-red-100 rounded-2xl p-3 space-y-1">
                  <span className="text-[11px] font-black text-red-700 block">دلیل رد درخواست</span>
                  <p className="text-[11px] text-red-600 leading-relaxed">{rejectionReason}</p>
                </div>
              )}
              <p className="text-[11px] text-slate-400 leading-relaxed">
                اگر فکر می‌کنید اشتباهی رخ داده است، برای پیگیری می‌توانید با پشتیبانی پلتفرم در تماس باشید.
              </p>
            </>
          )}

          <button
            onClick={onBack}
            className="w-full py-3 bg-indigo-600 hover:bg-indigo-700 text-white rounded-2xl text-[12.5px] font-black shadow-md shadow-indigo-100 active:scale-[0.99] transition-all cursor-pointer"
          >
            بازگشت
          </button>
        </div>
      </div>
    </div>
  );
};

// Reads the GAP 19b login contract: when an API error carries one of
// the supplier account-state codes, returns the info the state screen
// needs; otherwise null (caller falls back to the generic toast).
export const supplierAccountStateFromError = (err: any): SupplierAccountStateInfo | null => {
  const body = err?.body ?? err;
  if (!body || typeof body !== 'object') return null;
  if (body.code === 'supplier_pending_approval') {
    return {
      kind: 'pending',
      companyName: body.supplierName,
      warehouseCity: body.warehouseCity,
    };
  }
  if (body.code === 'supplier_rejected') {
    return {
      kind: 'rejected',
      companyName: body.supplierName,
      warehouseCity: body.warehouseCity,
      rejectionReason: body.rejectionReason,
    };
  }
  return null;
};
