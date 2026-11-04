import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { financeApi, CommissionDto } from '../../services/financeApi';
import { Sparkline } from '../finance/Sparkline';
import { mockSuppliers } from '../../data/suppliers';
import { DollarSign, Save, CheckCircle2, ShieldCheck, Sliders, Plus, Trash2, Building2, Package, Search, Sparkles, Gift, Tag, X } from 'lucide-react';

export const AdminCommissionTab: React.FC = () => {
  const {
    products,
    platformSettings,
    updatePlatformSettings,
    createSupplierPromotion,
    deleteSupplierPromotion,
    toPersianDigits,
    formatPrice,
  } = useStore();
  const { success, error } = useToast();

  const [minProfitMargin, setMinProfitMargin] = useState((platformSettings.minProfitMargin || 5).toString());
  const [maxProfitMargin, setMaxProfitMargin] = useState((platformSettings.maxProfitMargin || 35).toString());
  const [attributionModel, setAttributionModel] = useState<'last_touch' | 'first_touch'>('last_touch');
  const [cookieDays, setCookieDays] = useState('30');
  const [showConfirm, setShowConfirm] = useState(false);
  const [saved, setSaved] = useState(false);

  // Live "سهم محقق‌شده پلتفرم" stats (GET /finance/treasury/commission?days=).
  const [commissionDays, setCommissionDays] = useState(30);
  const [commission, setCommission] = useState<CommissionDto | null>(null);
  const [commissionError, setCommissionError] = useState(false);

  useEffect(() => {
    setCommissionError(false);
    financeApi.getCommission(commissionDays)
      .then((res) => setCommission(res && res.success ? res : null))
      .catch(() => setCommissionError(true));
  }, [commissionDays]);

  // Requirement 12: Supplier Promotion Form State
  const [showPromoModal, setShowPromoModal] = useState(false);
  const [promoTitle, setPromoTitle] = useState('');
  const [promoSupplierId, setPromoSupplierId] = useState('all');
  const [promoType, setPromoType] = useState<'platform_fee_discount' | 'warehouse_free_storage'>('platform_fee_discount');
  const [promoValue, setPromoValue] = useState('20');
  const [promoValidUntil, setPromoValidUntil] = useState('۱۴۰۵/۰۲/۳۱');


  const handleCreatePromo = (e: React.FormEvent) => {
    e.preventDefault();
    if (!promoTitle.trim()) {
      error('عنوان پروموشن الزامی است.');
      return;
    }
    createSupplierPromotion({
      title: promoTitle.trim(),
      supplierId: promoSupplierId,
      discountType: promoType,
      discountValue: Number(promoValue) || 10,
      validUntil: promoValidUntil,
      status: 'active',
    });
    success('کمپین تشویقی تأمین‌کننده با موفقیت فعال گردید.');
    setShowPromoModal(false);
    setPromoTitle('');
  };

  const handleFormSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setShowConfirm(true);
  };

  const handleConfirmSave = () => {
    setShowConfirm(false);
    setSaved(true);
    success(`بازه سود مجاز سلرها به‌روزرسانی شد.`);
    setTimeout(() => setSaved(false), 2500);
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* Live commission stats: realised platform share on paid orders */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs overflow-hidden">
        <div className="p-3.5 pb-2 flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-teal-700">
            <DollarSign className="w-4 h-4" strokeWidth={1.5} />
            <h3 className="text-xs font-bold text-slate-800">سهم محقق‌شده پلتفرم</h3>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="text-[9px] bg-slate-900 text-white font-bold px-1.5 py-0.5 rounded-full">محرمانه · فقط مدیر</span>
            <div className="flex bg-slate-100 p-0.5 rounded-lg text-[10px] font-bold gap-0.5">
              {[7, 30].map((d) => (
                <button
                  key={d}
                  type="button"
                  onClick={() => setCommissionDays(d)}
                  className={`min-h-[40px] px-3 py-0.5 rounded-md transition-all ${commissionDays === d ? 'bg-white text-teal-700 shadow-2xs' : 'text-slate-500'}`}
                >
                  {toPersianDigits(d)} روزه
                </button>
              ))}
            </div>
          </div>
        </div>
        {commissionError ? (
          <p className="p-3.5 text-[11px] text-rose-600">خطا در بارگذاری سهم محقق‌شده.</p>
        ) : commission ? commission.total === 0 ? (
          <div className="p-3.5 text-[11px] text-slate-500 leading-relaxed">
            <span className="font-bold text-slate-600 block mb-1">—</span>
            هنوز هیچ سفارش پرداخت‌شده‌ای ثبت نشده است؛ سهم پلتفرم پس از اولین پرداخت قطعی اینجا نمایش داده می‌شود.
          </div>
        ) : (
          <div className="px-3.5 pb-3.5 space-y-2.5">
            <div className="bg-gradient-to-l from-slate-900 via-teal-950 to-slate-900 rounded-xl p-3 text-white">
              <span className="text-[11px] font-bold opacity-90">جمع سهم ثبت‌شده روی سفارش‌های پرداخت‌شده</span>
              <div className="text-xl font-extrabold mt-1">{formatPrice(commission.total)} <span className="text-[10px] font-normal opacity-80">تومان</span></div>
              <div className="flex gap-3 mt-1.5 text-[10px] opacity-90">
                <span>آزادشده: <strong>{formatPrice(commission.releasedTotal)}</strong></span>
                <span>در انتظار آزادسازی: <strong>{formatPrice(commission.unreleasedTotal)}</strong></span>
                <span>{toPersianDigits(commission.orderCount)} سفارش</span>
              </div>
            </div>
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[10px] font-bold text-slate-600">روند {toPersianDigits(commissionDays)} روز اخیر</span>
                <span className="text-[9px] text-slate-400">بنفش: ثبت‌شده · سبز: آزادشده</span>
              </div>
              <Sparkline data={(commission.trend || []).map((t) => t.commission ?? 0)} color="#5d44f8" height={52} ariaLabel="روند سهم پلتفرم" />
            </div>
            {commission.note && (
              <p className="text-[9px] text-slate-400 leading-relaxed">{commission.note}</p>
            )}
          </div>
        ) : (
          <p className="p-3.5 text-[11px] text-slate-400">در حال محاسبه از داده‌های واقعی…</p>
        )}
      </div>
      <form onSubmit={handleFormSubmit} className="space-y-3.5">
        {/* 1. Global Platform Fee Setting (Default F) */}
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
          <div className="flex items-center gap-1.5 text-blue-600">
            <DollarSign className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">قواعد سهم پلتفرم (درصدی)</h3>
          </div>
          <p className="text-[10px] text-slate-500 leading-relaxed">
            سهم پلتفرم درصدی از قیمت مرجع است: قیمت پایه = قیمت مرجع × (۱ + درصد سهم). درصد سراسری از تنظیمات پلتفرم می‌آید و برای محصول یا دسته خاص می‌تواند متفاوت باشد.
          </p>


        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">کف سود مجاز سلر (٪)</label>
            <input
              type="number"
              value={minProfitMargin}
              onChange={(e) => setMinProfitMargin(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
              required
            />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">سقف سود مجاز سلر (٪)</label>
            <input
              type="number"
              value={maxProfitMargin}
              onChange={(e) => setMaxProfitMargin(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
              required
            />
          </div>
        </div>
      </div>

      {/* 2. Attribution Rules (Section 7) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-purple-600">
          <Sliders className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">قواعد انتساب فروش و ماندگاری کوکی</h3>
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">مدل انتساب معرفی خریدار به سایت مرکزی:</label>
          <select
            value={attributionModel}
            onChange={(e) => setAttributionModel(e.target.value as any)}
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
          >
            <option value="last_touch">آخرین معرفی (Last-touch) - اولویت با جدیدترین لینک معرف</option>
            <option value="first_touch">اولین معرفی (First-touch) - انتساب به اولین معرف خریدار</option>
          </select>
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">مدت اعتبار کوکی انتساب (روز)</label>
          <input
            type="number"
            value={cookieDays}
            onChange={(e) => setCookieDays(e.target.value)}
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
            required
          />
          <span className="text-[9px] text-slate-400 mt-1 block">
            خریدهای مکرر کاربر طی ۳۰ روز آینده به دراپ‌شیپر اولیه منتسب خواهد شد.
          </span>
        </div>
      </div>

      <button
        type="submit"
        className="w-full py-2.5 bg-blue-600 hover:bg-blue-700 text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5"
      >
        {saved ? (
          <>
            <CheckCircle2 className="w-4 h-4"  strokeWidth={1.5}/>
            <span>تنظیمات سراسری پلتفرم با موفقیت ذخیره شد</span>
          </>
        ) : (
          <>
            <Save className="w-4 h-4"  strokeWidth={1.5}/>
            <span>ذخیره قواعد مالی و انتساب شبکه</span>
          </>
        )}
      </button>

      <ConfirmDialog
        isOpen={showConfirm}
        title="تغییر بازه سود مجاز سلرها"
        message={`آیا از تغییر بازه سود مجاز سلرها به ${toPersianDigits(minProfitMargin)}٪ تا ${toPersianDigits(maxProfitMargin)}٪ اطمینان دارید؟ این تغییر بر روی قیمت مصرف‌کننده تمام ویترین‌ها تأثیرگذار خواهد بود.`}
        confirmText="تأیید و ذخیره تغییرات"
        isDanger={false}
        onConfirm={handleConfirmSave}
        onCancel={() => setShowConfirm(false)}
      />
    </form>
      {/* Requirement 12: Supplier Promotion Campaigns */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-indigo-600">
            <Gift className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">کمپین‌ها و پروموشن‌های ویژه تأمین‌کنندگان</h3>
          </div>
          <button
            type="button"
            onClick={() => setShowPromoModal(true)}
            className="flex items-center gap-1 bg-indigo-50 hover:bg-indigo-100 text-indigo-700 font-bold text-[10px] px-2.5 py-1.5 rounded-xl transition-colors"
          >
            <Plus className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>تعریف پروموشن جدید</span>
          </button>
        </div>

        <p className="text-[10px] text-slate-500 leading-relaxed">
          تعریف بسته‌های تشویقی برای تأمین‌کنندگان (تخفیف موقت روی کارمزد F پلتفرم یا انبارش رایگان).
        </p>

        <div className="space-y-2">
          {(platformSettings.supplierPromotions || []).map((promo) => (
            <div key={promo.id} className="p-3 bg-slate-50 border border-slate-200/80 rounded-xl flex items-center justify-between text-xs">
              <div>
                <div className="flex items-center gap-2">
                  <span className="font-bold text-slate-900">{promo.title}</span>
                  <span className="text-[9px] bg-emerald-100 text-emerald-800 font-bold px-1.5 py-0.2 rounded-md">
                    {promo.discountType === 'platform_fee_discount' ? `${toPersianDigits(promo.discountValue)}٪ تخفیف F` : `${toPersianDigits(promo.discountValue)} روز انبارش رایگان`}
                  </span>
                </div>
                <span className="text-[10px] text-slate-400 block mt-0.5">
                  مخاطب: {promo.supplierId === 'all' ? 'تمام تأمین‌کنندگان' : promo.supplierId} • مهلت: {promo.validUntil}
                </span>
              </div>
              <button
                type="button"
                onClick={() => {
                  deleteSupplierPromotion(promo.id);
                  success('پروموشن حذف شد.');
                }}
                className="p-1 text-slate-400 hover:text-rose-600"
              >
                <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
              </button>
            </div>
          ))}
        </div>
      </div>

      {/* Modal: New Supplier Promotion */}
      {showPromoModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <form onSubmit={handleCreatePromo} className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold text-slate-900">تعریف پروموشن ویژه تأمین‌کننده</h4>
              <button type="button" onClick={() => setShowPromoModal(false)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div>
              <label className="text-[10px] font-bold text-slate-600 block mb-1">عنوان کمپین:</label>
              <input
                type="text"
                value={promoTitle}
                onChange={(e) => setPromoTitle(e.target.value)}
                placeholder="مثال: آفر ویژه انبارش رایگان فصل بهار"
                className="w-full p-2 bg-slate-50 border rounded-xl"
                required
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="text-[10px] font-bold text-slate-600 block mb-1">تأمین‌کننده هدف:</label>
                <select
                  value={promoSupplierId}
                  onChange={(e) => setPromoSupplierId(e.target.value)}
                  className="w-full p-2 bg-slate-50 border rounded-xl"
                >
                  <option value="all">همه تأمین‌کنندگان</option>
                  {mockSuppliers.map((s) => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="text-[10px] font-bold text-slate-600 block mb-1">نوع پروموشن:</label>
                <select
                  value={promoType}
                  onChange={(e) => setPromoType(e.target.value as any)}
                  className="w-full p-2 bg-slate-50 border rounded-xl"
                >
                  <option value="platform_fee_discount">تخفیف روی سهم F</option>
                  <option value="warehouse_free_storage">انبارش روزانه رایگان</option>
                </select>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="text-[10px] font-bold text-slate-600 block mb-1">
                  {promoType === 'platform_fee_discount' ? 'درصد تخفیف (٪):' : 'تعداد روز رایگان:'}
                </label>
                <input
                  type="number"
                  value={promoValue}
                  onChange={(e) => setPromoValue(e.target.value)}
                  className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                  required
                />
              </div>
              <div>
                <label className="text-[10px] font-bold text-slate-600 block mb-1">مهلت اعتبار:</label>
                <input
                  type="text"
                  value={promoValidUntil}
                  onChange={(e) => setPromoValidUntil(e.target.value)}
                  className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                  placeholder="۱۴۰۵/۰۲/۳۱"
                  required
                />
              </div>
            </div>
            <div className="flex gap-2 pt-2 border-t">
              <button type="submit" className="flex-1 py-2 bg-indigo-600 text-white font-bold rounded-xl">ایجاد پروموشن</button>
              <button type="button" onClick={() => setShowPromoModal(false)} className="px-3 py-2 bg-slate-100 rounded-xl">انصراف</button>
            </div>
          </form>
        </div>
      )}
      {/* Item 7: Platform Contracts Management & Auto-Versioning */}
      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-1.5 text-blue-600">
            <ShieldCheck className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">نسخه‌بندی و متن قراردادهای دیجیتال پلتفرم</h3>
          </div>
          <span className="text-[10px] text-slate-400 font-mono">
            آخرین ویرایش: {platformSettings.contracts?.lastUpdated || '۱۴۰۵/۰۱/۱۵'}
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          ویرایش هر یک از متون قراردادها به صورت خودکار شماره نسخه (Version) را ۱ واحد افزایش می‌دهد و از طرفین در ورود بعدی امضای مجدد درخواست می‌گردد.
        </p>

        <div className="space-y-2">
          <div>
            <div className="flex items-center justify-between text-[11px] mb-1">
              <span className="font-bold text-slate-700">متن قرارداد همکاری با دراپ‌شیپرها (سلرها)</span>
              <span className="font-mono text-[10px] bg-blue-50 text-blue-700 px-2 py-0.5 rounded-md font-bold">
                نسخه فعال: {toPersianDigits(platformSettings.contracts?.sellerContractVersion || 2)}.۰
              </span>
            </div>
            <textarea
              rows={3}
              value={platformSettings.contracts?.sellerContractText}
              onChange={(e) => {
                updatePlatformSettings({
                  contracts: {
                    ...platformSettings.contracts,
                    sellerContractText: e.target.value,
                    sellerContractVersion: (platformSettings.contracts?.sellerContractVersion || 2) + 1,
                    lastUpdated: new Date().toLocaleDateString('fa-IR'),
                  }
                });
              }}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 text-slate-700 leading-relaxed focus:outline-hidden"
            />
          </div>

          <div>
            <div className="flex items-center justify-between text-[11px] mb-1">
              <span className="font-bold text-slate-700">متن قرارداد همکاری با تأمین‌کنندگان کالا</span>
              <span className="font-mono text-[10px] bg-purple-50 text-purple-700 px-2 py-0.5 rounded-md font-bold">
                نسخه فعال: {toPersianDigits(platformSettings.contracts?.supplierContractVersion || 2)}.۰
              </span>
            </div>
            <textarea
              rows={3}
              value={platformSettings.contracts?.supplierContractText}
              onChange={(e) => {
                updatePlatformSettings({
                  contracts: {
                    ...platformSettings.contracts,
                    supplierContractText: e.target.value,
                    supplierContractVersion: (platformSettings.contracts?.supplierContractVersion || 2) + 1,
                    lastUpdated: new Date().toLocaleDateString('fa-IR'),
                  }
                });
              }}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 text-slate-700 leading-relaxed focus:outline-hidden"
            />
          </div>
        </div>
      </div>


      <ConfirmDialog
        isOpen={showConfirm}
        title="تغییر بازه سود مجاز سلرها"
        message={`آیا از تغییر بازه سود مجاز سلرها به ${toPersianDigits(minProfitMargin)}٪ تا ${toPersianDigits(maxProfitMargin)}٪ اطمینان دارید؟ این تغییر بر روی قیمت مصرف‌کننده تمام ویترین‌ها تأثیرگذار خواهد بود.`}
        confirmText="تأیید و ذخیره تغییرات"
        isDanger={false}
        onConfirm={handleConfirmSave}
        onCancel={() => setShowConfirm(false)}
      />
    </div>
  );
};
