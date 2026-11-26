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
const sectionClass = 'min-w-0 space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5';

function SupplierProfileForm({ profile }: { profile: SupplierProfileFull }) {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [description, setDescription] = useState(profile.description || '');
  const [city, setCity] = useState(profile.warehouse_city || '');
  const [cityConfirmed, setCityConfirmed] = useState(profile.warehouse_city_confirmed !== false);
  const [logo, setLogo] = useState(profile.logo_url || '');
  const [logoBroken, setLogoBroken] = useState(false);
  const [cityError, setCityError] = useState('');
  const [saveError, setSaveError] = useState('');
  const cityRef = useRef<HTMLInputElement>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);
  const busy = saving || uploading || deleting;

  const onFile = async (files: FileList | null) => {
    const file = files?.[0];
    if (!file || busy) return;
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
    if (busy) return;
    setDeleting(true);
    try {
      await supplierApi.updateSupplierProfile({ logo_url: '' });
      setLogo('');
      success('لوگو حذف شد.');
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'حذف لوگو انجام نشد.');
    } finally {
      setDeleting(false);
    }
  };

  const save = async () => {
    if (busy) return;
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
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <form aria-labelledby="supplier-profile-business" onSubmit={event => { event.preventDefault(); void save(); }} className={sectionClass}>
        <h3 id="supplier-profile-business" className="text-base font-extrabold text-[#261b41]">اطلاعات کسب‌وکار</h3>
        <div className="rounded-xl bg-slate-50 p-3">
          <p className="text-xs font-bold text-slate-600">نام ثبت‌شده</p>
          <p className="mt-1 break-words text-sm text-slate-700">{profile.name || profile.company_name || '—'}</p>
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
        <button type="submit" disabled={busy} className="min-h-11 w-full rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60 sm:w-auto">
          {saving ? 'در حال ذخیره…' : 'ذخیره پروفایل'}
        </button>
      </form>
      <section aria-labelledby="supplier-profile-logo-heading" className={sectionClass}>
        <h3 id="supplier-profile-logo-heading" className="text-base font-extrabold text-[#261b41]">لوگوی تأمین‌کننده</h3>
        <div className="flex flex-wrap items-start gap-3">
          <div className="grid size-16 shrink-0 place-items-center overflow-hidden rounded-2xl border border-slate-200 bg-slate-50">
            {logo && !logoBroken ? (
              <img src={logo} alt="لوگوی تأمین‌کننده" className="size-full object-contain" onError={() => setLogoBroken(true)} />
            ) : (
              <ImageIcon aria-hidden="true" className="size-6 text-slate-500" strokeWidth={1.5} />
            )}
          </div>
          <div className="flex flex-wrap gap-1.5">
            <button type="button" disabled={busy} aria-describedby="supplier-profile-logo-help" onClick={() => fileRef.current?.click()} className="flex min-h-11 items-center gap-1.5 rounded-xl border border-[#dfd8ef] px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">
              <Upload aria-hidden="true" className="size-4" strokeWidth={1.5} />
              {uploading ? 'در حال بارگذاری…' : logo ? 'تعویض لوگو' : 'بارگذاری لوگو'}
            </button>
            {logo && (
              <button type="button" disabled={busy} onClick={() => void deleteLogo()} className="flex min-h-11 items-center gap-1.5 rounded-xl px-3 text-sm font-bold text-rose-700 hover:bg-rose-50 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-60">
                <Trash2 aria-hidden="true" className="size-4" strokeWidth={1.5} />
                {deleting ? 'در حال حذف…' : 'حذف لوگو'}
              </button>
            )}
            <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="hidden" onChange={event => void onFile(event.target.files)} />
          </div>
        </div>
        <p id="supplier-profile-logo-help" className="text-xs leading-6 text-slate-600">لوگو پس از بارگذاری ذخیره می‌شود. قالب‌های <bdi dir="ltr">JPG، PNG، WEBP</bdi> تا ۲ مگابایت پذیرفته می‌شوند.</p>
      </section>
    </div>
  );
}
