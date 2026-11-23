import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, DiscountCode } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { Calendar, Tag } from 'lucide-react';

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
  const [code, setCode] = useState('');
  const [codeMode, setCodeMode] = useState<'percent' | 'fixed'>('percent');
  const [codeValue, setCodeValue] = useState('10');
  const [codeStartsAt, setCodeStartsAt] = useState('');
  const [codeEndsAt, setCodeEndsAt] = useState('');
  const [codeMaxUses, setCodeMaxUses] = useState('');

  const load = useCallback(() => {
    if (isMockMode() || !currentStore.slug) return;
    storeApi.getDiscountCodes(currentStore.slug).then(setCodes).catch(() => setCodes([]));
  }, [currentStore.slug]);

  useEffect(() => { load(); }, [load]);

  const submitCode = async () => {
    try {
      await storeApi.createDiscountCode(currentStore.slug, {
        code, mode: codeMode, value: Number(codeValue) || 0,
        startsAt: codeStartsAt ? new Date(codeStartsAt).toISOString() : new Date().toISOString(),
        endsAt: codeEndsAt ? new Date(codeEndsAt).toISOString() : new Date(Date.now() + 30 * 86400000).toISOString(),
        maxUses: codeMaxUses ? Number(codeMaxUses) : undefined,
      });
      success('کد تخفیف ساخته شد.');
      load();
    } catch (e: any) { error(e?.message || 'ساخت کد تخفیف انجام نشد.'); }
  };

  return (
    <div className="space-y-6 p-4">
      <div className="rounded-2xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900">
        تخفیف فقط از حاشیه سود خود شما کم می‌شود؛ قیمت خرید از تأمین‌کننده و کارمزد پلتفرم تغییر نمی‌کند و قیمت نهایی هرگز از مجموع آن دو پایین‌تر نمی‌رود. مدیریت کمپین‌های تخفیف در زبانه «کمپین‌های تخفیف» انجام می‌شود.
      </div>

      <div className="rounded-2xl border border-slate-200 bg-white p-4 space-y-3">
        <h3 className="font-bold text-sm flex items-center gap-2"><Tag className="w-4 h-4"  strokeWidth={1.5}/> کد تخفیف</h3>
        <input className="w-full border rounded-xl p-2 text-xs" placeholder="کد تخفیف، مثلاً SAVE10" value={code} onChange={(e) => setCode(e.target.value)} />
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
        <button className="bg-blue-600 text-white rounded-xl px-4 py-2 text-xs font-bold" onClick={submitCode}>ساخت کد تخفیف</button>
        <div className="space-y-2">
          {codes.map((c) => (
            <div key={c.id} className="flex items-center justify-between rounded-xl bg-slate-50 p-2 text-xs">
              <span><strong>{c.code}</strong> · {c.mode === 'percent' ? `${c.value}٪` : `${formatPrice(c.value)} تومان`} · استفاده‌شده: {c.usedCount}{c.maxUses ? `/${c.maxUses}` : ''} · {c.isActive ? 'فعال' : 'غیرفعال'}</span>
              <span className="flex items-center gap-1">
                <button className="rounded-lg bg-slate-200 px-2 py-1 text-[10px]" onClick={() => storeApi.setDiscountCodeActive(currentStore.slug, c.id, !c.isActive).then(load).catch((e: any) => error(e?.message || 'خطا'))}>{c.isActive ? 'غیرفعال' : 'فعال'}</button>
                {canDeleteDiscountCode(c) && (
                  <button className="rounded-lg bg-rose-100 text-rose-700 px-2 py-1 text-[10px] font-bold" onClick={() => {
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
      </div>
    </div>
  );
};
