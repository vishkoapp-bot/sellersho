import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { X, Check, Loader2, MapPin, Plus } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import type { DirectBuyAddress } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { AuthModal } from './auth/AuthModal';
import { apiRequest } from '../services/apiClient';
import { isValidIranianMobile, isValidIranianPostalCode } from '../utils/validators';
import { geoApi, toLatinDigits, type GeoItem } from './ProvinceCityPicker';

// Worker A — live parity (index-MERGED.js buy-now address modal s3).
// Flow: خرید فوری stages a direct-buy payload, then this modal opens.
// Unauthenticated -> AuthModal titled «ورود برای خرید فوری»; authenticated
// -> saved-address list (default: previous selection, else isDefault, else
// first) or a new-address form. Confirm returns the chosen saved address;
// closing without confirming cancels the whole buy-now (the caller clears
// directBuy). The new-address form's province/city selects come ONLY from
// the /geo/* dataset (see ProvinceCityPicker); when it is unavailable the
// form degrades to free-text inputs, like live.
//
// NOTE: this modal talks to /customer/addresses directly instead of
// extending services/addressApi.ts (another team's file): list mapping
// mirrors that file, create returns {success, id} on this backend.

interface BackendAddress {
  id: string;
  label?: string | null;
  receiverFullName: string;
  receiverPhone?: string | null;
  provinceName: string;
  cityName: string;
  postalCode?: string | null;
  addressDetail: string;
  isDefault: boolean;
}

const mapBackendAddress = (a: BackendAddress): DirectBuyAddress => ({
  id: a.id,
  label: a.label || undefined,
  receiverFullName: a.receiverFullName,
  receiverPhone: a.receiverPhone || '',
  provinceName: a.provinceName,
  cityName: a.cityName,
  postalCode: a.postalCode || '',
  addressDetail: a.addressDetail,
  isDefault: a.isDefault,
});

const inputClassName =
  'w-full p-2.5 rounded-xl bg-slate-50 border border-slate-200 text-sm focus:outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20';
const labelClassName = 'text-[11px] font-bold text-slate-700 block mb-1';

interface NewAddressForm {
  label: string;
  receiverFullName: string;
  receiverPhone: string;
  provinceName: string;
  provinceCode: unknown;
  cityName: string;
  cityCode: unknown;
  postalCode: string;
  addressDetail: string;
  isDefault: boolean;
}

const blankForm: NewAddressForm = {
  label: '',
  receiverFullName: '',
  receiverPhone: '',
  provinceName: '',
  provinceCode: undefined,
  cityName: '',
  cityCode: undefined,
  postalCode: '',
  addressDetail: '',
  isDefault: false,
};

export interface BuyNowItemSummary {
  title: string;
  qty: number;
  totalPrice: number;
  variantLabel?: string;
}

export const BuyNowAddressModal: React.FC<{
  open: boolean;
  item: BuyNowItemSummary;
  onClose: () => void;
  onConfirm: (address: DirectBuyAddress) => void;
}> = ({ open, item, onClose, onConfirm }) => {
  const { customerUser, formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [view, setView] = useState<'auth' | 'list' | 'new'>('auth');
  const [addresses, setAddresses] = useState<DirectBuyAddress[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [provinces, setProvinces] = useState<GeoItem[] | null>(null);
  const [cities, setCities] = useState<GeoItem[] | null>(null);
  const [citiesSnapshot, setCitiesSnapshot] = useState('');
  const [form, setForm] = useState<NewAddressForm>(blankForm);
  const [saving, setSaving] = useState(false);
  const authJustSucceeded = useRef(false);

  const loadAddresses = useCallback(async (): Promise<DirectBuyAddress[]> => {
    setLoading(true);
    setLoadError(false);
    try {
      const data = await apiRequest<{ addresses: BackendAddress[] }>('/customer/addresses', {
        method: 'GET',
        auth: true,
      });
      const list: DirectBuyAddress[] = Array.isArray((data as any)?.addresses)
        ? (data as any).addresses.map(mapBackendAddress)
        : [];
      setAddresses(list);
      setSelectedId((prev) => {
        if (prev && list.some((a) => a.id === prev)) return prev;
        return list.find((a) => a.isDefault)?.id ?? list[0]?.id ?? null;
      });
      return list;
    } catch {
      setLoadError(true);
      return [];
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    authJustSucceeded.current = false;
    if (customerUser.isLoggedIn) {
      setView('list');
      void loadAddresses();
    } else {
      setView('auth');
    }
  }, [open, customerUser.isLoggedIn, loadAddresses]);

  useEffect(() => {
    if (!open || view === 'auth') return;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }, [open, view]);

  const loadProvinces = useCallback(async () => {
    if (provinces !== null) return;
    try {
      const res = await geoApi.provinces();
      setProvinces(res.available ? res.items : []);
    } catch {
      setProvinces([]);
    }
  }, [provinces]);

  const loadCities = useCallback(async (code: unknown) => {
    const snapshot = JSON.stringify(code);
    setCitiesSnapshot(snapshot);
    setCities(null);
    try {
      const res = await geoApi.cities(code);
      setCities(res.available ? res.items : []);
    } catch {
      setCities([]);
    }
  }, []);

  const openNewAddress = useCallback(() => {
    setForm(blankForm);
    setCities(null);
    setCitiesSnapshot('');
    void loadProvinces();
    setView('new');
  }, [loadProvinces]);

  const handleAuthSuccess = useCallback(() => {
    // Live s3 swallows the AuthModal's own onClose that follows onSuccess.
    authJustSucceeded.current = true;
    setView('list');
    void loadAddresses();
  }, [loadAddresses]);

  const handleAuthClose = useCallback(() => {
    if (authJustSucceeded.current) {
      authJustSucceeded.current = false;
      return;
    }
    onClose();
  }, [onClose]);

  const handleProvinceSelect = (value: string) => {
    if (value === '') {
      setForm((prev) => ({ ...prev, provinceName: '', provinceCode: undefined, cityName: '', cityCode: undefined }));
      setCities(null);
      setCitiesSnapshot('');
      return;
    }
    const node = (provinces || []).find((item) => JSON.stringify(item.code) === value);
    if (node) {
      setForm((prev) => ({ ...prev, provinceName: node.name, provinceCode: node.code, cityName: '', cityCode: undefined }));
      void loadCities(node.code);
    }
  };

  const handleCitySelect = (value: string) => {
    if (value === '') {
      setForm((prev) => ({ ...prev, cityName: '', cityCode: undefined }));
      return;
    }
    const node = (cities || []).find((item) => JSON.stringify(item.code) === value);
    if (node) {
      setForm((prev) => ({ ...prev, cityName: node.name, cityCode: node.code }));
    }
  };

  const handleSaveNewAddress = async () => {
    const phoneDigits = toLatinDigits(form.receiverPhone.trim()).replace(/\D/g, '');
    const postalDigits = toLatinDigits(form.postalCode.trim()).replace(/\D/g, '');
    if (!form.receiverFullName.trim()) {
      error('نام گیرنده را وارد کنید.');
      return;
    }
    if (!isValidIranianMobile(phoneDigits)) {
      error('شماره موبایل معتبر وارد کنید (مثل ۰۹۱۲۳۴۵۶۷۸۹).');
      return;
    }
    if (!form.provinceName.trim()) {
      error('استان را انتخاب یا وارد کنید.');
      return;
    }
    if (!form.cityName.trim()) {
      error('شهر را انتخاب یا وارد کنید.');
      return;
    }
    if (!isValidIranianPostalCode(postalDigits)) {
      error('کد پستی ۱۰ رقمی معتبر وارد کنید.');
      return;
    }
    if (!form.addressDetail.trim()) {
      error('نشانی دقیق را وارد کنید.');
      return;
    }
    const payload = {
      label: form.label.trim(),
      receiverFullName: form.receiverFullName.trim(),
      receiverPhone: phoneDigits,
      provinceName: form.provinceName.trim(),
      provinceCode: form.provinceCode,
      cityName: form.cityName.trim(),
      cityCode: form.cityCode,
      postalCode: postalDigits,
      addressDetail: form.addressDetail.trim(),
      isDefault: form.isDefault,
    };
    setSaving(true);
    try {
      const res = await apiRequest<{ success?: boolean; id?: string }>('/customer/addresses', {
        method: 'POST',
        auth: true,
        body: JSON.stringify(payload),
      });
      success('آدرس ذخیره شد ✓');
      await loadAddresses();
      if ((res as any)?.id) setSelectedId((res as any).id);
      setView('list');
    } catch (e: any) {
      error(e?.message || 'ذخیره آدرس ناموفق بود.');
    } finally {
      setSaving(false);
    }
  };

  const handleConfirm = () => {
    const selected = addresses.find((a) => a.id === selectedId);
    if (selected) onConfirm(selected);
  };

  if (!open) return null;

  if (view === 'auth') {
    return (
      <AuthModal
        isOpen={open}
        onClose={handleAuthClose}
        onSuccess={handleAuthSuccess}
        roleTitle="ورود برای خرید فوری"
      />
    );
  }

  const hasProvinces = provinces !== null && provinces.length > 0;
  const provinceValue = form.provinceCode !== undefined ? JSON.stringify(form.provinceCode) : '';
  const showCitySelect =
    hasProvinces && form.provinceCode !== undefined && cities !== null && cities.length > 0 && citiesSnapshot === provinceValue;
  const showCityLoading =
    hasProvinces && form.provinceCode !== undefined && cities === null && citiesSnapshot === provinceValue;
  const selected = addresses.find((a) => a.id === selectedId) ?? null;

  return createPortal(
    <div
      dir="rtl"
      className="fixed inset-0 z-[70] flex items-end sm:items-center justify-center bg-slate-950/60 p-0 sm:p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className="w-full max-w-[504px] bg-white max-h-[92dvh] overflow-y-auto rounded-t-3xl sm:rounded-3xl shadow-2xl lg:max-w-[560px]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sticky top-0 bg-white z-10 flex items-center justify-between border-b border-slate-100 px-4 py-3">
          <div>
            <h3 className="text-sm font-extrabold text-slate-900">خرید فوری</h3>
            <span className="text-[11px] text-slate-400 block">
              {view === 'list' ? 'آدرس تحویل را انتخاب کنید' : 'آدرس جدید'}
            </span>
          </div>
          <button
            onClick={onClose}
            aria-label="بستن"
            className="w-11 h-11 rounded-full hover:bg-slate-100 text-slate-500 flex items-center justify-center"
          >
            <X className="w-5 h-5" strokeWidth={1.5} />
          </button>
        </div>

        <div className="p-4 space-y-4">
          <div className="bg-slate-50 rounded-2xl p-3 space-y-1">
            <p className="text-xs font-bold text-slate-800 line-clamp-2">{item.title}</p>
            {item.variantLabel && <p className="text-[11px] text-slate-500">{item.variantLabel}</p>}
            <div className="flex items-center justify-between text-xs">
              <span className="text-slate-500">تعداد: {toPersianDigits(item.qty)}</span>
              <span className="font-extrabold text-brand-deep">{formatPrice(item.totalPrice)} تومان</span>
            </div>
          </div>

          {view === 'list' && (
            <>
              {loading ? (
                <div className="flex items-center justify-center gap-2 py-8 text-slate-500 text-xs">
                  <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                  <span>در حال بارگذاری آدرس‌ها…</span>
                </div>
              ) : loadError ? (
                <div className="text-center py-6 space-y-3">
                  <p className="text-xs text-rose-500 font-bold">بارگذاری آدرس‌ها ناموفق بود.</p>
                  <button
                    onClick={() => void loadAddresses()}
                    className="px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 text-xs font-bold"
                  >
                    تلاش مجدد
                  </button>
                </div>
              ) : addresses.length === 0 ? (
                <div className="text-center py-6 space-y-3">
                  <MapPin className="w-8 h-8 text-slate-300 mx-auto" strokeWidth={1.5} />
                  <p className="text-xs text-slate-500">شما هنوز آدرسی ثبت نکرده‌اید.</p>
                  <button
                    onClick={openNewAddress}
                    className="px-4 py-3 rounded-xl bg-brand text-white text-xs font-bold"
                  >
                    افزودن آدرس جدید
                  </button>
                </div>
              ) : (
                <div className="space-y-2">
                  {addresses.map((address) => {
                    const isSelected = address.id === selectedId;
                    return (
                      <button
                        key={address.id}
                        type="button"
                        onClick={() => setSelectedId(address.id)}
                        className={`w-full text-right p-3 rounded-2xl border transition-colors ${
                          isSelected ? 'border-brand bg-brand-soft/40' : 'border-slate-200 hover:border-slate-300'
                        }`}
                      >
                        <div className="flex items-start gap-2.5">
                          <span
                            className={`mt-0.5 w-5 h-5 shrink-0 rounded-full border-2 flex items-center justify-center ${
                              isSelected ? 'border-brand bg-brand' : 'border-slate-300 bg-white'
                            }`}
                          >
                            {isSelected && <Check className="w-3 h-3 text-white" strokeWidth={3} />}
                          </span>
                          <span className="flex-1 min-w-0">
                            <span className="flex items-center gap-2 flex-wrap">
                              <span className="text-xs font-bold text-slate-800">{address.receiverFullName}</span>
                              {address.isDefault && (
                                <span className="text-[10px] font-bold bg-brand-soft text-brand-deep px-2 py-0.5 rounded-full">
                                  پیش‌فرض
                                </span>
                              )}
                            </span>
                            <span className="text-[11px] text-slate-500 block mt-1">
                              {address.provinceName}، {address.cityName}
                            </span>
                            <span className="text-[11px] text-slate-500 block mt-0.5 line-clamp-2">
                              {address.addressDetail}
                            </span>
                            <span className="text-[10px] text-slate-400 block mt-1">
                              کد پستی: {toPersianDigits(address.postalCode || '—')}
                            </span>
                          </span>
                        </div>
                      </button>
                    );
                  })}
                  <div className="flex items-center gap-2 pt-1">
                    <button
                      onClick={openNewAddress}
                      className="flex items-center gap-1.5 px-3 py-3 rounded-2xl bg-white border border-slate-200 text-slate-700 text-xs font-bold"
                    >
                      <Plus className="w-4 h-4" strokeWidth={1.5} />
                      آدرس جدید
                    </button>
                    <button
                      onClick={handleConfirm}
                      disabled={!selected}
                      className="flex-1 py-3 rounded-2xl bg-brand hover:bg-brand-deep disabled:opacity-50 text-white text-xs font-extrabold"
                    >
                      ادامه و خرید با این آدرس
                    </button>
                  </div>
                </div>
              )}
            </>
          )}

          {view === 'new' && (
            <div className="space-y-3">
              <button onClick={() => setView('list')} className="text-[11px] text-brand-deep font-bold">
                بازگشت به فهرست آدرس‌ها
              </button>
              <div>
                <label className={labelClassName}>نام گیرنده *</label>
                <input
                  value={form.receiverFullName}
                  onChange={(e) => setForm((prev) => ({ ...prev, receiverFullName: e.target.value }))}
                  placeholder="نام و نام خانوادگی تحویل‌گیرنده"
                  className={inputClassName}
                />
              </div>
              <div>
                <label className={labelClassName}>شماره موبایل *</label>
                <input
                  type="tel"
                  dir="ltr"
                  value={form.receiverPhone}
                  onChange={(e) =>
                    setForm((prev) => ({
                      ...prev,
                      receiverPhone: toLatinDigits(e.target.value).replace(/\D/g, '').slice(0, 11),
                    }))
                  }
                  placeholder="۰۹۱۲۳۴۵۶۷۸۹"
                  className={`${inputClassName} text-right`}
                />
              </div>
              <div>
                <label className={labelClassName}>استان *</label>
                {hasProvinces ? (
                  <select value={provinceValue} onChange={(e) => handleProvinceSelect(e.target.value)} className={inputClassName}>
                    <option value="">استان را انتخاب کنید</option>
                    {(provinces || []).map((item) => (
                      <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={form.provinceName}
                    onChange={(e) =>
                      setForm((prev) => ({ ...prev, provinceName: e.target.value, provinceCode: undefined, cityName: '', cityCode: undefined }))
                    }
                    placeholder="استان"
                    className={inputClassName}
                  />
                )}
              </div>
              <div>
                <label className={labelClassName}>شهر *</label>
                {showCitySelect ? (
                  <select
                    value={form.cityCode !== undefined ? JSON.stringify(form.cityCode) : ''}
                    onChange={(e) => handleCitySelect(e.target.value)}
                    className={inputClassName}
                  >
                    <option value="">شهر را انتخاب کنید</option>
                    {(cities || []).map((item) => (
                      <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                ) : showCityLoading ? (
                  <input disabled placeholder="در حال بارگذاری شهرها…" className={`${inputClassName} opacity-60`} />
                ) : (
                  <input
                    value={form.cityName}
                    onChange={(e) => setForm((prev) => ({ ...prev, cityName: e.target.value, cityCode: undefined }))}
                    placeholder="شهر"
                    className={inputClassName}
                  />
                )}
              </div>
              <div>
                <label className={labelClassName}>کد پستی ۱۰ رقمی *</label>
                <input
                  dir="ltr"
                  value={form.postalCode}
                  onChange={(e) =>
                    setForm((prev) => ({
                      ...prev,
                      postalCode: toLatinDigits(e.target.value).replace(/\D/g, '').slice(0, 10),
                    }))
                  }
                  placeholder="بدون خط تیره"
                  className={`${inputClassName} text-right`}
                />
              </div>
              <div>
                <label className={labelClassName}>نشانی دقیق *</label>
                <textarea
                  rows={2}
                  value={form.addressDetail}
                  onChange={(e) => setForm((prev) => ({ ...prev, addressDetail: e.target.value }))}
                  placeholder="خیابان، کوچه، پلاک، واحد…"
                  className={inputClassName}
                />
              </div>
              <div>
                <label className={labelClassName}>عنوان آدرس (اختیاری)</label>
                <input
                  value={form.label}
                  onChange={(e) => setForm((prev) => ({ ...prev, label: e.target.value }))}
                  placeholder="مثل خانه یا محل کار"
                  className={inputClassName}
                />
              </div>
              <label className="flex items-center gap-2 text-xs text-slate-700 cursor-pointer">
                <input
                  type="checkbox"
                  checked={form.isDefault}
                  onChange={(e) => setForm((prev) => ({ ...prev, isDefault: e.target.checked }))}
                  className="w-4 h-4 accent-[#5d44f8]"
                />
                ثبت به‌عنوان آدرس پیش‌فرض
              </label>
              <button
                onClick={handleSaveNewAddress}
                disabled={saving}
                className="w-full py-3.5 rounded-2xl bg-brand hover:bg-brand-deep disabled:opacity-50 text-white text-xs font-extrabold"
              >
                {saving ? 'در حال ذخیره…' : 'ذخیره آدرس و انتخاب آن'}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
};
