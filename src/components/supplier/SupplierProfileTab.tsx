import { SupplierSectionHeading } from './SupplierSectionHeading';
import React, { useState, useRef } from 'react';
import { supplierApi, type SupplierProfileFull } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { User, Upload, Trash2, ImageIcon } from 'lucide-react';
import { SupplierResourceState } from './SupplierResourceState';
import { useSupplierResource } from './useSupplierResource';

// Live supplier profile tab (Tn): registered name (read-only), logo upload
// (reuses the product-image upload endpoint), description, warehouse city
// with confirmation flag, and save.
export const SupplierProfileTab: React.FC = () => {
  const profile = useSupplierResource(supplierApi.getSupplierProfileFull);
  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="پروفایل تأمین‌کننده" description="اطلاعات و لوگوی کسب‌وکارت را مدیریت کن." icon={<User />} />
      <SupplierResourceState loading={profile.loading} error={profile.error} retry={profile.reload} />
      {!profile.loading && !profile.error && profile.data && <SupplierProfileForm profile={profile.data} />}
    </div>
  );
};

const fieldClass = 'mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-brand';

function SupplierProfileForm({ profile }: { profile: SupplierProfileFull }) {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [description, setDescription] = useState(profile.description || '');
  const [city, setCity] = useState(profile.warehouse_city || '');
  const [cityConfirmed, setCityConfirmed] = useState(profile.warehouse_city_confirmed !== false);
  const [logo, setLogo] = useState(profile.logo_url || '');
  const [logoBroken, setLogoBroken] = useState(false);
  const [cityError, setCityError] = useState('');
  const [saveError, setSaveError] = useState('');
  const cityRef = useRef<HTMLInputElement>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);

  const onFile = async (files: FileList | null) => {
    const file = files?.[0];
    if (!file) return;
    if (fileRef.current) fileRef.current.value = '';
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
      error('فرمت لوگو نامعتبر است. فقط JPG، PNG و WEBP مجاز هستند.');
      return;
    }
    if (file.size > 2 * 1024 * 1024) {
      error('حجم لوگو نباید بیشتر از ۲ مگابایت باشد.');
      return;
    }
    setUploading(true);
    try {
      const uploaded = await supplierApi.uploadLogoImage(file);
      if (!uploaded.imageUrl) throw new Error('خطا در آپلود لوگو');
      const updated = await supplierApi.updateSupplierProfile({ logo_url: uploaded.imageUrl });
      setLogo(updated.logo_url || uploaded.imageUrl);
      setLogoBroken(false);
      success('لوگو ذخیره شد.');
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'آپلود لوگو انجام نشد.');
    } finally {
      setUploading(false);
    }
  };

  const deleteLogo = async () => {
    setSaving(true);
    try {
      await supplierApi.updateSupplierProfile({ logo_url: '' });
      setLogo('');
      success('لوگو حذف شد.');
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'حذف لوگو انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const save = async () => {
    if (saving || uploading) return;
    setSaveError('');
    if (description.trim().length > 2000) {
      setSaveError('توضیحات تأمین‌کننده باید حداکثر ۲۰۰۰ نویسه باشد.');
      return;
    }
    if (!city.trim()) {
      setCityError('نام شهر انبار را وارد کن.');
      cityRef.current?.focus();
      return;
    }
    setSaving(true);
    try {
      const updated = await supplierApi.updateSupplierProfile({ description: description.trim(), warehouse_city: city.trim() });
      setDescription(updated.description || '');
      setCity(updated.warehouse_city || '');
      setCityConfirmed(updated.warehouse_city_confirmed !== false);
      success('پروفایل ذخیره شد.');
    } catch (cause: unknown) {
      setSaveError(cause instanceof Error ? cause.message : 'ذخیره پروفایل انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  return (
      <form onSubmit={event => { event.preventDefault(); void save(); }} className="space-y-5 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <div>
          <p className="text-sm font-bold">نام ثبت‌شده</p>
          <p className="mt-1 break-words text-sm text-slate-700">{profile.name || profile.company_name || '—'}</p>
        </div>
        <div>
          <h3 className="mb-2 text-sm font-bold">لوگوی تأمین‌کننده</h3>
          <div className="flex items-center gap-3">
            <div className="w-16 h-16 rounded-2xl border border-slate-200 bg-slate-50 flex items-center justify-center overflow-hidden shrink-0">
              {logo && !logoBroken ? (
                <img src={logo} alt="لوگوی تأمین‌کننده" className="w-full h-full object-contain" onError={() => setLogoBroken(true)} />
              ) : (
                <ImageIcon className="w-6 h-6 text-slate-300" strokeWidth={1.5} />
              )}
            </div>
            <div className="flex flex-col gap-1.5">
              <button type="button" disabled={uploading || saving} onClick={() => fileRef.current?.click()} className="flex min-h-11 items-center gap-1.5 rounded-xl border border-[#dfd8ef] px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">
                <Upload className="w-3.5 h-3.5" strokeWidth={1.5} />
                {uploading ? 'در حال آپلود…' : logo ? 'تعویض لوگو' : 'بارگذاری لوگو'}
              </button>
              {logo && (
                <button type="button" disabled={uploading || saving} onClick={() => void deleteLogo()} className="flex min-h-11 items-center gap-1.5 rounded-xl bg-rose-50 px-3 text-sm font-bold text-rose-700 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-60">
                  <Trash2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                  حذف لوگو
                </button>
              )}
              <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="hidden" onChange={(event) => void onFile(event.target.files)} />
            </div>
          </div>
          <p className="mt-2 text-xs leading-6 text-slate-600">لوگو در پنل‌های داخلی نمایش داده می‌شود و پس از بارگذاری ذخیره می‌شود. فرمت‌های JPG، PNG و WEBP تا ۲ مگابایت پذیرفته می‌شوند.</p>
        </div>
        <div>
          <label htmlFor="supplier-profile-description" className="block text-sm font-bold">توضیحات تأمین‌کننده</label>
          <textarea id="supplier-profile-description" aria-describedby="supplier-profile-description-count" disabled={saving} value={description} onChange={(event) => setDescription(event.target.value)} rows={4} maxLength={2000} placeholder="توضیح کوتاه درباره کسب‌وکارت" className={fieldClass} />
          <p id="supplier-profile-description-count" className="mt-1 text-xs text-slate-600">{toPersianDigits(description.length)} از ۲۰۰۰ نویسه</p>
        </div>
        <div>
          <label htmlFor="supplier-profile-city" className="block text-sm font-bold">شهر انبار</label>
          <input ref={cityRef} id="supplier-profile-city" aria-required="true" aria-invalid={!!cityError} aria-describedby={cityError ? 'supplier-profile-city-error supplier-profile-city-help' : 'supplier-profile-city-help'} disabled={saving} type="text" value={city} onChange={(event) => { setCity(event.target.value); setCityError(''); }} maxLength={50} placeholder="مثلاً تهران" className={fieldClass} />
          {cityError && <p id="supplier-profile-city-error" role="alert" className="mt-1 text-sm text-rose-700">{cityError}</p>}
          <p id="supplier-profile-city-help" className="mt-1 text-xs leading-6 text-slate-600">این شهر، مبدأ پیش‌فرض کالاهایی است که انبار مشخص ندارند.</p>
          {!cityConfirmed && city.trim() && <p className="mt-1 text-xs leading-6 text-amber-800">این شهر در انتظار تأیید است. با ذخیره، شهر اعلام‌شدهٔ تو ثبت می‌شود.</p>}
        </div>
        {saveError && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{saveError}</p>}
        <button type="submit" disabled={saving || uploading} className="min-h-11 w-full rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60 sm:w-auto">
          {saving ? 'در حال ذخیره…' : 'ذخیره پروفایل'}
        </button>
      </form>
  );
}
