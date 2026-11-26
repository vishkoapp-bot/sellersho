import React, { useCallback, useEffect, useRef, useState, useId } from 'react';
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
// Fashion PDP treatment (Aastin tokens): square inputs on white with
// hairline borders and an ink focus ring for the fashion storefront.
const inputClassNameFashion =
  'w-full p-2.5 rounded-none bg-white border border-[#e5e0d8] text-sm focus:outline-hidden focus:border-[#1c1c1a] focus:ring-2 focus:ring-[#1c1c1a]/15';
const labelClassNameFashion = 'text-[11px] font-bold text-[#1c1c1a] block mb-1';

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
  fashion?: boolean;
  central?: boolean;
}> = ({ open, item, onClose, onConfirm, fashion = false, central = false }) => {
  const { customerUser, formatPrice, toPersianDigits } = useStore();
  const dialogId = useId();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const inputCls = central
    ? 'min-h-11 w-full rounded-xl border border-[var(--shop-line,#dde2d7)] bg-white px-3 py-2.5 text-base'
    : fashion ? inputClassNameFashion : inputClassName;
  const labelCls = central ? 'mb-1.5 block text-sm font-bold text-[var(--shop-ink,#172d26)]' : fashion ? labelClassNameFashion : labelClassName;
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
  const [addressMessage, setAddressMessage] = useState<{ text: string; type: 'error' | 'success' } | null>(null);
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

  const showAuth = view === 'auth' && !customerUser.isLoggedIn;
  const showAddress = open && !showAuth;
  useEffect(() => {
    if (!showAddress) return;
    const { overflow } = document.body.style;
    const dialog = dialogRef.current;
    const previousFocus = document.activeElement;
    if (central && dialog && !dialog.open) dialog.showModal();
    document.body.style.overflow = 'hidden';
    return () => {
      dialog?.close();
      document.body.style.overflow = overflow;
      if (central && previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, [showAddress, central]);

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
    setAddressMessage(null);
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

  const reportAddressError = (text: string) => {
    if (central) setAddressMessage({ text, type: 'error' });
    else error(text);
  };

  const handleSaveNewAddress = async () => {
    const phoneDigits = toLatinDigits(form.receiverPhone.trim()).replace(/\D/g, '');
    const postalDigits = toLatinDigits(form.postalCode.trim()).replace(/\D/g, '');
    if (!form.receiverFullName.trim()) {
      reportAddressError('نام گیرنده را وارد کنید.');
      return;
    }
    if (!isValidIranianMobile(phoneDigits)) {
      reportAddressError('شماره موبایل معتبر وارد کنید (مثل ۰۹۱۲۳۴۵۶۷۸۹).');
      return;
    }
    if (!form.provinceName.trim()) {
      reportAddressError('استان را انتخاب یا وارد کنید.');
      return;
    }
    if (!form.cityName.trim()) {
      reportAddressError('شهر را انتخاب یا وارد کنید.');
      return;
    }
    if (!isValidIranianPostalCode(postalDigits)) {
      reportAddressError('کد پستی ۱۰ رقمی معتبر وارد کنید.');
      return;
    }
    if (!form.addressDetail.trim()) {
      reportAddressError('نشانی دقیق را وارد کنید.');
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
    setAddressMessage(null);
    setSaving(true);
    try {
      const res = await apiRequest<{ success?: boolean; id?: string }>('/customer/addresses', {
        method: 'POST',
        auth: true,
        body: JSON.stringify(payload),
      });
      if (central) setAddressMessage({ text: 'نشانی ذخیره شد.', type: 'success' });
      else success('آدرس ذخیره شد ✓');
      await loadAddresses();
      if ((res as any)?.id) setSelectedId((res as any).id);
      setView('list');
    } catch (e: any) {
      reportAddressError(e?.message || 'ذخیره آدرس ناموفق بود.');
    } finally {
      setSaving(false);
    }
  };

  const handleConfirm = () => {
    const selected = addresses.find((a) => a.id === selectedId);
    if (selected) onConfirm(selected);
  };

  if (!open) return null;

  if (showAuth) {
    return (
      <AuthModal
        central={central}
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

  const content = (
    <>
      <div className={central ? 'sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-[var(--shop-line,#dde2d7)] bg-white px-4 py-3 sm:px-5' : fashion ? 'sticky top-0 bg-[#faf8f5] z-10 flex items-center justify-between border-b border-[#e5e0d8] px-4 py-3' : 'sticky top-0 bg-white z-10 flex items-center justify-between border-b border-slate-100 px-4 py-3'}>
        <div className={central ? 'flex items-center gap-3' : ''}>
          {central && <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep"><MapPin size={21} aria-hidden="true" /></span>}
          <div>
            <h3 id={dialogId} className={central ? 'text-lg font-black text-[var(--shop-ink,#172d26)]' : fashion ? 'text-sm font-extrabold text-[#1c1c1a]' : 'text-sm font-extrabold text-slate-900'}>خرید فوری</h3>
            <span className={central ? 'block text-xs text-[var(--shop-muted,#4f6359)]' : `text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-400'} block`}>
              {view === 'new' ? 'آدرس تحویل جدید' : 'آدرس تحویل را انتخاب کنید'}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="بستن"
          className={central ? 'grid size-11 shrink-0 place-items-center rounded-xl text-[var(--shop-muted,#4f6359)] hover:bg-brand-soft' : fashion ? 'w-11 h-11 rounded-none hover:bg-[#f2f2f2] text-[#737373] flex items-center justify-center' : 'w-11 h-11 rounded-full hover:bg-slate-100 text-slate-500 flex items-center justify-center'}
        >
          <X className="w-5 h-5" strokeWidth={1.5} />
        </button>
      </div>

      <div className={central ? 'space-y-4 px-4 pt-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:px-5' : 'p-4 space-y-4'}>
        <div className={central ? 'space-y-2 rounded-2xl border border-[var(--shop-line,#dde2d7)] bg-white p-3.5' : fashion ? 'bg-[#f2f2f2] rounded-none p-3 space-y-1' : 'bg-slate-50 rounded-2xl p-3 space-y-1'}>
          <p className={central ? 'text-sm font-extrabold leading-6 text-[var(--shop-ink,#172d26)]' : fashion ? 'text-xs font-bold text-[#1c1c1a] line-clamp-2' : 'text-xs font-bold text-slate-800 line-clamp-2'}>{item.title}</p>
          {item.variantLabel && <p className={`text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-500'}`}>{item.variantLabel}</p>}
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
            <span className={fashion ? 'text-[#737373]' : 'text-slate-500'}>تعداد: {toPersianDigits(item.qty)}</span>
            <span className={central ? 'text-sm font-black tabular-nums text-brand-deep' : fashion ? 'font-extrabold text-[#1c1c1a]' : 'font-extrabold text-brand-deep'}>{formatPrice(item.totalPrice)} تومان</span>
          </div>
        </div>

        {view !== 'new' && (
          <>
            {addressMessage?.type === 'success' && <p role="status" className="rounded-xl bg-brand-soft px-3 py-2 text-sm font-bold text-brand-deep">{addressMessage.text}</p>}
            {loading ? (
              <div className={`flex items-center justify-center gap-2 py-8 ${fashion ? 'text-[#737373]' : 'text-slate-500'} text-xs`}>
                <Loader2 className="w-4 h-4 animate-spin" strokeWidth={1.5} />
                <span>در حال بارگذاری آدرس‌ها…</span>
              </div>
            ) : loadError ? (
              <div className={central ? 'space-y-3 rounded-2xl border border-rose-200 bg-rose-50 p-4 text-center' : 'text-center py-6 space-y-3'}>
                <p role="alert" className="text-sm font-bold text-rose-800">بارگذاری آدرس‌ها ناموفق بود.</p>
                <button
                  onClick={() => void loadAddresses()}
                  className={fashion ? 'px-4 py-2.5 rounded-none bg-[#f2f2f2] text-[#1c1c1a] text-xs font-bold' : 'px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 text-xs font-bold'}
                >
                  تلاش مجدد
                </button>
              </div>
            ) : addresses.length === 0 ? (
              <div className={central ? 'space-y-3 rounded-2xl border border-dashed border-[var(--shop-line,#dde2d7)] bg-white p-5 text-center' : 'text-center py-6 space-y-3'}>
                {central ? <span className="mx-auto grid size-12 place-items-center rounded-2xl bg-brand-soft text-brand-deep"><MapPin size={22} aria-hidden="true" /></span> : <MapPin className="w-8 h-8 text-slate-300 mx-auto" strokeWidth={1.5} />}
                <p className={`text-xs ${fashion ? 'text-[#737373]' : 'text-slate-500'}`}>شما هنوز آدرسی ثبت نکرده‌اید.</p>
                <button
                  onClick={openNewAddress}
                  className={fashion ? 'px-4 py-3 rounded-none bg-[#3a4441] text-[#faf8f5] text-xs font-bold' : 'px-4 py-3 rounded-xl bg-brand text-white text-xs font-bold'}
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
                      aria-pressed={isSelected}
                      className={`w-full text-right p-3 ${fashion ? 'rounded-none' : 'rounded-2xl'} border transition-colors ${
                        isSelected
                          ? fashion
                            ? 'border-[#1c1c1a] bg-[#f2f2f2]'
                            : 'border-brand bg-brand-soft/40'
                          : fashion
                            ? 'border-[#e5e0d8] bg-white hover:border-[#1c1c1a]'
                            : central ? 'border-[var(--shop-line,#dde2d7)] bg-white hover:border-brand' : 'border-slate-200 hover:border-slate-300'
                      }`}
                    >
                      <div className="flex items-start gap-2.5">
                        <span
                          className={`mt-0.5 w-5 h-5 shrink-0 rounded-full border-2 flex items-center justify-center ${
                            isSelected
                              ? fashion
                                ? 'border-[#1c1c1a] bg-[#1c1c1a]'
                                : 'border-brand bg-brand'
                              : fashion
                                ? 'border-[#e5e0d8] bg-white'
                                : 'border-slate-300 bg-white'
                          }`}
                        >
                          {isSelected && <Check className="w-3 h-3 text-white" strokeWidth={3} />}
                        </span>
                        <span className="flex-1 min-w-0">
                          <span className="flex items-center gap-2 flex-wrap">
                            <span className={fashion ? 'text-xs font-bold text-[#1c1c1a]' : 'text-xs font-bold text-slate-800'}>{address.receiverFullName}</span>
                            {address.isDefault && (
                              <span className={fashion ? 'text-[10px] font-bold bg-white text-[#1c1c1a] border border-[#e5e0d8] px-2 py-0.5 rounded-none' : 'text-[10px] font-bold bg-brand-soft text-brand-deep px-2 py-0.5 rounded-full'}>
                                پیش‌فرض
                              </span>
                            )}
                          </span>
                          <span className={`text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-500'} block mt-1`}>
                            {address.provinceName}، {address.cityName}
                          </span>
                          <span className={`text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-500'} block mt-0.5 line-clamp-2`}>
                            {address.addressDetail}
                          </span>
                          <span className={`text-[10px] ${fashion ? 'text-[#737373]' : 'text-slate-400'} block mt-1`}>
                            کد پستی: {toPersianDigits(address.postalCode || '—')}
                          </span>
                        </span>
                      </div>
                    </button>
                  );
                })}
                <div className={central ? 'grid grid-cols-1 gap-2 pt-2 min-[375px]:grid-cols-[auto_1fr]' : 'flex items-center gap-2 pt-1'}>
                  <button
                    onClick={openNewAddress}
                    className={central ? 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl border border-[var(--shop-line,#dde2d7)] bg-white px-3 text-sm font-bold text-brand-deep hover:bg-brand-soft' : fashion ? 'flex items-center gap-1.5 px-3 py-3 rounded-none bg-white border border-[#e5e0d8] text-[#1c1c1a] text-xs font-bold' : 'flex items-center gap-1.5 px-3 py-3 rounded-2xl bg-white border border-slate-200 text-slate-700 text-xs font-bold'}
                  >
                    <Plus className="w-4 h-4" strokeWidth={1.5} />
                    آدرس جدید
                  </button>
                  <button
                    onClick={handleConfirm}
                    disabled={!selected}
                    className={central ? 'min-h-11 rounded-xl bg-brand px-3 text-sm font-extrabold text-white hover:bg-brand-deep disabled:opacity-50' : fashion ? 'flex-1 py-3 rounded-none bg-[#3a4441] hover:bg-[#1c1c1a] disabled:opacity-50 text-[#faf8f5] text-xs font-extrabold' : 'flex-1 py-3 rounded-2xl bg-brand hover:bg-brand-deep disabled:opacity-50 text-white text-xs font-extrabold'}
                  >
                    ادامه و خرید با این آدرس
                  </button>
                </div>
              </div>
            )}
          </>
        )}

        {view === 'new' && (
          <div className={central ? 'space-y-3 rounded-2xl border border-[var(--shop-line,#dde2d7)] bg-white p-3 sm:p-4' : 'space-y-3'}>
            <button type="button" onClick={() => setView('list')} className={central ? 'min-h-11 rounded-xl px-2 text-sm font-bold text-brand-deep hover:bg-brand-soft' : fashion ? 'text-[11px] text-[#1c1c1a] font-bold underline underline-offset-4' : 'text-[11px] text-brand-deep font-bold'}>
              بازگشت به فهرست آدرس‌ها
            </button>
            <div>
              <label htmlFor={`${dialogId}-recipient`} className={labelCls}>نام گیرنده *</label>
              <input
                id={`${dialogId}-recipient`} value={form.receiverFullName}
                onChange={(e) => setForm((prev) => ({ ...prev, receiverFullName: e.target.value }))}
                placeholder="نام و نام خانوادگی تحویل‌گیرنده"
                className={inputCls}
              />
            </div>
            <div>
              <label htmlFor={`${dialogId}-phone`} className={labelCls}>شماره موبایل *</label>
              <input
                id={`${dialogId}-phone`} type="tel"
                dir="ltr"
                value={form.receiverPhone}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    receiverPhone: toLatinDigits(e.target.value).replace(/\D/g, '').slice(0, 11),
                  }))
                }
                placeholder="۰۹۱۲۳۴۵۶۷۸۹"
                className={`${inputCls} text-right`}
              />
            </div>
            <div>
              <label htmlFor={`${dialogId}-province`} className={labelCls}>استان *</label>
              {hasProvinces ? (
                <select id={`${dialogId}-province`} value={provinceValue} onChange={(e) => handleProvinceSelect(e.target.value)} className={inputCls}>
                  <option value="">استان را انتخاب کنید</option>
                  {(provinces || []).map((item) => (
                    <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
                      {item.name}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  id={`${dialogId}-province`} value={form.provinceName}
                  onChange={(e) =>
                    setForm((prev) => ({ ...prev, provinceName: e.target.value, provinceCode: undefined, cityName: '', cityCode: undefined }))
                  }
                  placeholder="استان"
                  className={inputCls}
                />
              )}
            </div>
            <div>
              <label htmlFor={`${dialogId}-city`} className={labelCls}>شهر *</label>
              {showCitySelect ? (
                <select
                  id={`${dialogId}-city`} value={form.cityCode !== undefined ? JSON.stringify(form.cityCode) : ''}
                  onChange={(e) => handleCitySelect(e.target.value)}
                  className={inputCls}
                >
                  <option value="">شهر را انتخاب کنید</option>
                  {(cities || []).map((item) => (
                    <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
                      {item.name}
                    </option>
                  ))}
                </select>
              ) : showCityLoading ? (
                <input id={`${dialogId}-city`} disabled placeholder="در حال بارگذاری شهرها…" className={`${inputCls} opacity-60`} />
              ) : (
                <input
                  id={`${dialogId}-city`} value={form.cityName}
                  onChange={(e) => setForm((prev) => ({ ...prev, cityName: e.target.value, cityCode: undefined }))}
                  placeholder="شهر"
                  className={inputCls}
                />
              )}
            </div>
            <div>
              <label htmlFor={`${dialogId}-postal`} className={labelCls}>کد پستی ۱۰ رقمی *</label>
              <input
                dir="ltr"
                id={`${dialogId}-postal`} value={form.postalCode}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    postalCode: toLatinDigits(e.target.value).replace(/\D/g, '').slice(0, 10),
                  }))
                }
                placeholder="بدون خط تیره"
                className={`${inputCls} text-right`}
              />
            </div>
            <div>
              <label htmlFor={`${dialogId}-address`} className={labelCls}>نشانی دقیق *</label>
              <textarea
                id={`${dialogId}-address`} rows={2}
                value={form.addressDetail}
                onChange={(e) => setForm((prev) => ({ ...prev, addressDetail: e.target.value }))}
                placeholder="خیابان، کوچه، پلاک، واحد…"
                className={inputCls}
              />
            </div>
            <div>
              <label htmlFor={`${dialogId}-address-label`} className={labelCls}>عنوان آدرس (اختیاری)</label>
              <input
                id={`${dialogId}-address-label`} value={form.label}
                onChange={(e) => setForm((prev) => ({ ...prev, label: e.target.value }))}
                placeholder="مثل خانه یا محل کار"
                className={inputCls}
              />
            </div>
            <label className={fashion ? 'flex items-center gap-2 text-xs text-[#1c1c1a] cursor-pointer' : 'flex items-center gap-2 text-xs text-slate-700 cursor-pointer'}>
              <input
                type="checkbox"
                checked={form.isDefault}
                onChange={(e) => setForm((prev) => ({ ...prev, isDefault: e.target.checked }))}
                className={central ? 'size-4 accent-brand' : fashion ? 'w-4 h-4 accent-[#1c1c1a]' : 'w-4 h-4 accent-[#5d44f8]'}
              />
              ثبت به‌عنوان آدرس پیش‌فرض
            </label>
            {addressMessage?.type === 'error' && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 px-3 py-2 text-sm font-bold text-rose-800">{addressMessage.text}</p>}
            <button
              onClick={handleSaveNewAddress}
              disabled={saving}
              className={fashion ? 'w-full py-3.5 rounded-none bg-[#3a4441] hover:bg-[#1c1c1a] disabled:opacity-50 text-[#faf8f5] text-xs font-extrabold' : 'w-full py-3.5 rounded-2xl bg-brand hover:bg-brand-deep disabled:opacity-50 text-white text-xs font-extrabold'}
            >
              {saving ? 'در حال ذخیره…' : 'ذخیره آدرس و انتخاب آن'}
            </button>
          </div>
        )}
      </div>
    </>
  );

  if (central) {
    return createPortal(
      <dialog
        ref={dialogRef}
        dir="rtl"
        aria-labelledby={dialogId}
        onCancel={event => { event.preventDefault(); onClose(); }}
        onClick={event => {
          if (event.target !== event.currentTarget) return;
          const rect = event.currentTarget.getBoundingClientRect();
          if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) onClose();
        }}
        className="central-portal fixed inset-0 mx-auto mb-0 mt-auto max-h-[92dvh] w-full max-w-full overflow-y-auto overscroll-contain rounded-t-[var(--shop-radius-lg,24px)] border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] p-0 text-[var(--shop-ink,#172d26)] shadow-2xl backdrop:bg-[#10271f]/55 sm:m-auto sm:w-[calc(100%-2rem)] sm:max-w-[35rem] sm:rounded-[var(--shop-radius-lg,24px)] [--color-brand:var(--shop-forest,#164638)] [--color-brand-deep:var(--shop-hover,#0e392c)] [--color-brand-soft:var(--shop-soft,#edf2e9)] [&_:is(button,input,select,textarea):focus-visible]:outline-2 [&_:is(button,input,select,textarea):focus-visible]:outline-offset-2 [&_:is(button,input,select,textarea):focus-visible]:outline-[var(--shop-focus,#98601c)]"
      >
        {content}
      </dialog>,
      document.body,
    );
  }

  return createPortal(
    <div
      dir="rtl"
      className="fixed inset-0 z-[70] flex items-end sm:items-center justify-center bg-slate-950/60 p-0 sm:p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className={fashion ? 'w-full max-w-[504px] bg-[#faf8f5] max-h-[92dvh] overflow-y-auto rounded-none shadow-2xl lg:max-w-[560px]' : 'w-full max-w-[504px] bg-white max-h-[92dvh] overflow-y-auto rounded-t-3xl sm:rounded-3xl shadow-2xl lg:max-w-[560px]'}
        onClick={(e) => e.stopPropagation()}
      >
        {content}
      </div>
    </div>,
    document.body,
  );
};
