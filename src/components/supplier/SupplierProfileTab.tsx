import React, { useState, useEffect, useRef } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { Loader2, User, Upload, Trash2, ImageIcon } from 'lucide-react';

// Live supplier profile tab (Tn): registered name (read-only), logo upload
// (reuses the product-image upload endpoint), description, warehouse city
// with confirmation flag, and save.
export const SupplierProfileTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [city, setCity] = useState('');
  const [cityConfirmed, setCityConfirmed] = useState(true);
  const [logo, setLogo] = useState('');
  const [logoBroken, setLogoBroken] = useState(false);
  const fileRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    let on = true;
    supplierApi.getSupplierProfileFull()
      .then((profile) => {
        if (!on) return;
        setName(profile.name || profile.company_name || '');
        setDescription(profile.description || '');
        setCity(profile.warehouse_city || '');
        setCityConfirmed(profile.warehouse_city_confirmed !== false);
        setLogo(profile.logo_url || '');
      })
      .catch((err: unknown) => { if (on) error(err instanceof Error ? err.message : 'دریافت پروفایل انجام نشد.'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
  }, [error]);

  const onFile = async (files: FileList | null) => {
    const file = files?.[0];
    if (!file) return;
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
      if (fileRef.current) fileRef.current.value = '';
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
    if (description.trim().length > 2000) {
      error('توضیحات تأمین‌کننده بیش از حد مجاز است.');
      return;
    }
    if (!city.trim()) {
      error('نام شهر انبار را وارد کنید.');
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
      error(cause instanceof Error ? cause.message : 'ذخیره پروفایل انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-10 text-slate-400">
        <Loader2 className="w-6 h-6 animate-spin"  strokeWidth={1.5}/>
      </div>
    );
  }

  return (
    <div className="space-y-3 pb-6">
      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-3">
        <h3 className="text-sm font-bold text-slate-800 flex items-center gap-1.5">
          <User className="w-4 h-4 text-brand-deep" strokeWidth={1.5} />
          پروفایل تأمین‌کننده
        </h3>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">نام ثبت‌شده (غیرقابل ویرایش)</label>
          <input type="text" value={name} disabled className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 text-slate-500" />
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">لوگوی تأمین‌کننده</label>
          <div className="flex items-center gap-3">
            <div className="w-16 h-16 rounded-2xl border border-slate-200 bg-slate-50 flex items-center justify-center overflow-hidden shrink-0">
              {logo && !logoBroken ? (
                <img src={logo} alt="لوگوی تأمین‌کننده" className="w-full h-full object-contain" onError={() => setLogoBroken(true)} />
              ) : (
                <ImageIcon className="w-6 h-6 text-slate-300" strokeWidth={1.5} />
              )}
            </div>
            <div className="flex flex-col gap-1.5">
              <button type="button" disabled={uploading || saving} onClick={() => fileRef.current?.click()} className="min-h-[40px] px-3 rounded-xl bg-brand-deep text-white text-xs font-bold disabled:opacity-50 flex items-center gap-1.5">
                <Upload className="w-3.5 h-3.5" strokeWidth={1.5} />
                {uploading ? 'در حال آپلود…' : logo ? 'تعویض لوگو' : 'بارگذاری لوگو'}
              </button>
              {logo && (
                <button type="button" disabled={uploading || saving} onClick={() => void deleteLogo()} className="min-h-[36px] px-3 rounded-xl bg-rose-50 text-rose-600 text-xs font-bold disabled:opacity-50 flex items-center gap-1.5">
                  <Trash2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                  حذف لوگو
                </button>
              )}
              <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" className="hidden" onChange={(event) => void onFile(event.target.files)} />
            </div>
          </div>
          <span className="text-[9px] text-slate-400 block mt-1">لوگو فقط در پنل‌های داخلی (تأمین‌کننده، انبار، ادمین) دیده می‌شود و هرگز به مشتری نمایش داده نمی‌شود. فایل روی زیرساخت سلرشو ذخیره می‌شود؛ نشانی بیرونی پذیرفته نیست.</span>
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">توضیحات تأمین‌کننده ({toPersianDigits(description.trim().length)} از ۲۰۰۰ نویسه)</label>
          <textarea value={description} onChange={(event) => setDescription(event.target.value)} rows={3} placeholder="توضیح کوتاه درباره کسب‌وکار شما…" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-none resize-none" />
        </div>
        <div>
          <label className="text-[10px] text-slate-600 block mb-1">شهر انبار</label>
          <input type="text" value={city} onChange={(event) => setCity(event.target.value)} maxLength={50} placeholder="مثال: تهران" className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden" />
          <span className="text-[9px] text-slate-500 block mt-1">این شهر، مبدأ پیش‌فرض کالاهایی است که انبار مشخص ندارند. اگر برای کالایی مستقیم انبار خودت را انتخاب کنی، شهر آن انبار مبدأ است، نه این شهر.</span>
          {!cityConfirmed && city.trim() && <span className="text-[9px] text-amber-600 block mt-1">این شهر هنوز تأیید نشده است — با ذخیره، شهری که وارد کرده‌اید به‌عنوان شهر اعلام‌شدهٔ شما ثبت می‌شود.</span>}
        </div>
        <button type="button" disabled={saving || uploading} onClick={() => void save()} className="w-full min-h-[44px] rounded-xl bg-brand-deep text-white text-xs font-bold disabled:opacity-50">
          {saving ? 'در حال ذخیره…' : 'ذخیره پروفایل'}
        </button>
      </div>
    </div>
  );
};
