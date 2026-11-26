import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, DiscountCode } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { Calendar, Tag } from 'lucide-react';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

// Team Discounts (Phase 3): seller discount-CODE management only.
// Campaigns are managed in the live-parity 'discount-campaigns' tab
// (SellerDiscountCampaignsTab) and are deliberately NOT duplicated
// here; this tab only creates, lists and toggles codes, which
// live-parity did not have. Every discount is margin-only by server
// rule: it never touches wholesale cost or platform fee, and the
// discounted margin is what settlement pays.
// G8: the delete rule, exported so the unit test pins it — the row
// renders the «حذف» button only when this is true.
export const canDeleteDiscountCode = (code: DiscountCode): boolean => (code.usedCount ?? 0) === 0;

export const SellerDiscountTab: React.FC = () => {
  const { currentStore, formatPrice } = useStore();
  const { success, error } = useToast();
  const [codes, setCodes] = useState<DiscountCode[]>([]);
  const [codesLoading, setCodesLoading] = useState(false);
  const [codesError, setCodesError] = useState(false);
  const [code, setCode] = useState('');
  const [codeMode, setCodeMode] = useState<'percent' | 'fixed'>('percent');
  const [codeValue, setCodeValue] = useState('10');
  const [codeStartsAt, setCodeStartsAt] = useState('');
  const [codeEndsAt, setCodeEndsAt] = useState('');
  const [codeMaxUses, setCodeMaxUses] = useState('');
  const [creating, setCreating] = useState(false);

  const load = useCallback(() => {
    if (isMockMode() || !currentStore.slug) return;
    setCodesLoading(true);
    setCodesError(false);
    void storeApi.getDiscountCodes(currentStore.slug)
      .then(setCodes)
      .catch(() => setCodesError(true))
      .finally(() => setCodesLoading(false));
  }, [currentStore.slug]);

  useEffect(() => { load(); }, [load]);

  const submitCode = async () => {
    if (creating) return;
    setCreating(true);
    try {
      await storeApi.createDiscountCode(currentStore.slug, {
        code, mode: codeMode, value: Number(codeValue) || 0,
        startsAt: codeStartsAt ? new Date(codeStartsAt).toISOString() : new Date().toISOString(),
        endsAt: codeEndsAt ? new Date(codeEndsAt).toISOString() : new Date(Date.now() + 30 * 86400000).toISOString(),
        maxUses: codeMaxUses ? Number(codeMaxUses) : undefined,
      });
      success('کد تخفیف ساخته شد.');
      load();
    } catch (e: any) {
      error(e?.message || 'ساخت کد تخفیف انجام نشد.');
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="space-y-4">
      <SupplierSectionHeading title="کدهای تخفیف" description="کدی بساز که مشتری در پرداخت وارد کند. مبلغ تخفیف از حاشیه سودت کم می‌شود." icon={<Tag />} />
      <form onSubmit={event => { event.preventDefault(); void submitCode(); }} className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:p-5">
        <h3 className="text-base font-extrabold text-[#261b41]">کد تخفیف</h3>
        <div>
          <label htmlFor="seller-new-discount-code" className="mb-1.5 block text-xs font-bold text-slate-700">کد قابل استفاده در پرداخت</label>
          <input id="seller-new-discount-code" className="min-h-11 w-full rounded-xl border border-slate-200 p-2 text-sm focus-visible:ring-2 focus-visible:ring-brand" dir="ltr" autoComplete="off" placeholder="SAVE10" value={code} required onChange={(e) => setCode(e.target.value)} />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
          <div>
            <label htmlFor="discount-code-mode" className="mb-1.5 block text-[11px] font-bold text-slate-700">نوع تخفیف</label>
            <select id="discount-code-mode" className="w-full min-w-0 border rounded-xl p-2 text-xs" value={codeMode} onChange={(e) => setCodeMode(e.target.value as any)}>
              <option value="percent">درصد</option>
              <option value="fixed">مبلغ ثابت (تومان)</option>
            </select>
          </div>
          <div>
            <label htmlFor="discount-code-value" className="mb-1.5 block text-[11px] font-bold text-slate-700">مقدار</label>
            <input id="discount-code-value" className="w-full min-w-0 border rounded-xl p-2 text-xs" placeholder="مقدار" value={codeValue} onChange={(e) => setCodeValue(e.target.value)} />
          </div>
          <div>
            <label htmlFor="discount-code-max-uses" className="mb-1.5 block text-[11px] font-bold text-slate-700">حداکثر دفعات استفاده</label>
            <input id="discount-code-max-uses" className="w-full min-w-0 border rounded-xl p-2 text-xs" placeholder="حداکثر دفعات (اختیاری)" value={codeMaxUses} onChange={(e) => setCodeMaxUses(e.target.value)} />
          </div>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          <div>
            <label htmlFor="discount-code-starts-at" className="block text-[11px] font-bold text-slate-700 mb-1.5 flex items-center gap-1">
              <Calendar className="w-3.5 h-3.5 text-slate-400" />
              <span>تاریخ و ساعت شروع</span>
            </label>
            <input id="discount-code-starts-at" type="datetime-local" className="w-full min-w-0 border rounded-xl p-2 text-xs" value={codeStartsAt} onChange={(e) => setCodeStartsAt(e.target.value)} />
          </div>
          <div>
            <label htmlFor="discount-code-ends-at" className="block text-[11px] font-bold text-slate-700 mb-1.5 flex items-center gap-1">
              <Calendar className="w-3.5 h-3.5 text-slate-400" />
              <span>تاریخ و ساعت پایان</span>
            </label>
            <input id="discount-code-ends-at" type="datetime-local" className="w-full min-w-0 border rounded-xl p-2 text-xs" value={codeEndsAt} onChange={(e) => setCodeEndsAt(e.target.value)} />
          </div>
        </div>
        <p className="text-xs leading-5 text-slate-600">با تاریخ‌های خالی، کد از همین لحظه تا ۳۰ روز فعال است.</p>
        <button type="submit" disabled={creating} className="min-h-11 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">{creating ? 'در حال ساخت…' : 'ساخت کد تخفیف'}</button>
      </form>
      <section className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5" aria-labelledby="seller-discount-codes">
        <h3 id="seller-discount-codes" className="text-base font-extrabold text-[#261b41]">کدهای ثبت‌شده</h3>
        {codesLoading && <p role="status" className="text-sm text-slate-600">در حال دریافت کدهای تخفیف…</p>}
        {codesError && (
          <div className="flex flex-wrap items-center gap-2">
            <p role="alert" className="text-sm text-rose-700">دریافت کدهای تخفیف انجام نشد.</p>
            <button type="button" onClick={load} className="min-h-11 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">تلاش دوباره</button>
          </div>
        )}
        {!codesLoading && !codesError && codes.length === 0 && <p className="text-sm leading-6 text-slate-600">هنوز کد تخفیفی ثبت نکرده‌ای.</p>}
        <div className="space-y-2">
          {codes.map((c) => (
            <div key={c.id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-slate-50 p-3 text-sm">
              <span className="min-w-0 break-words"><strong><bdi>{c.code}</bdi></strong> · {c.mode === 'percent' ? `${c.value}٪` : `${formatPrice(c.value)} تومان`} · استفاده‌شده: {c.usedCount}{c.maxUses ? `/${c.maxUses}` : ''} · {c.isActive ? 'فعال' : 'غیرفعال'}</span>
              <span className="flex items-center gap-2">
                <button type="button" className="min-h-11 rounded-lg bg-brand-soft px-2 text-xs font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand" onClick={() => storeApi.setDiscountCodeActive(currentStore.slug, c.id, !c.isActive).then(load).catch((e: any) => error(e?.message || 'خطا'))}>{c.isActive ? 'غیرفعال کن' : 'فعال کن'}</button>
                {canDeleteDiscountCode(c) && (
                  <button type="button" className="min-h-11 rounded-lg px-2 text-xs font-bold text-rose-700 hover:bg-rose-50 focus-visible:ring-2 focus-visible:ring-rose-600" onClick={() => {
                    if (!window.confirm(`کد ${c.code} حذف شود؟ این عمل قابل بازگشت نیست.`)) return;
                    storeApi.deleteDiscountCode(currentStore.slug, c.id)
                      .then(() => { success('کد تخفیف حذف شد.'); load(); })
                      .catch((e: any) => error(e?.message || 'حذف کد تخفیف انجام نشد.'));
                  }}>حذف</button>
                )}
              </span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
};
