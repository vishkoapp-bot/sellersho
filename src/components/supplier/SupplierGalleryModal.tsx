import React, { useCallback, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, Image as ImageIcon, Plus, Star, Trash2 } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { supplierApi } from '../../services/supplierApi';
import { SupplierDialog, SupplierDialogHeading } from './SupplierDialog';
import { SupplierResourceState } from './SupplierResourceState';
import { useSupplierResource } from './useSupplierResource';

const MAX_IMAGES = 8;
const ALLOWED_TYPES = ['image/jpeg', 'image/png', 'image/webp'];
const imageAction = 'grid min-h-11 place-items-center rounded-lg bg-white text-slate-700 hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-40';

export const SupplierGalleryModal: React.FC<{
  productId: string;
  productTitle: string;
  onClose: () => void;
  onChanged?: () => void;
}> = ({ productId, productTitle, onClose, onChanged }) => {
  const { toPersianDigits } = useStore();
  const load = useCallback(() => supplierApi.getProductImages(productId), [productId]);
  const gallery = useSupplierResource(load);
  const images = gallery.data ?? [];
  const [busy, setBusy] = useState(false);
  const [confirmImageId, setConfirmImageId] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ text: string; error: boolean } | null>(null);
  const inProgress = useRef(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const changeGallery = async (
    change: () => Promise<unknown>,
    updatedText: string,
    reviewText: string,
    failedText: string,
  ) => {
    if (inProgress.current) return;
    inProgress.current = true;
    setBusy(true);
    setNotice(null);
    try {
      const result = await change();
      await gallery.reload();
      const pending = typeof result === 'object' && result !== null && 'pendingReview' in result && result.pendingReview === true;
      setNotice({ text: pending ? reviewText : updatedText, error: false });
      onChanged?.();
    } catch (cause: unknown) {
      setNotice({ text: cause instanceof Error ? cause.message : failedText, error: true });
    } finally {
      inProgress.current = false;
      setBusy(false);
    }
  };

  const handleFiles = (files: FileList | null) => {
    const list = Array.from(files ?? []);
    if (fileRef.current) fileRef.current.value = '';
    if (list.length === 0) return;
    if (images.length + list.length > MAX_IMAGES) {
      setNotice({ text: `حداکثر ${toPersianDigits(MAX_IMAGES)} تصویر برای هر کالا مجاز است.`, error: true });
    } else if (list.some(file => !ALLOWED_TYPES.includes(file.type))) {
      setNotice({ text: 'فرمت تصویر باید JPG، PNG یا WEBP باشد.', error: true });
    } else if (list.some(file => file.size > 5 * 1024 * 1024)) {
      setNotice({ text: 'حجم هر تصویر باید حداکثر ۵ مگابایت باشد.', error: true });
    } else {
      void changeGallery(() => supplierApi.uploadProductImages(productId, list),
        `${toPersianDigits(list.length)} تصویر به گالری اضافه شد.`, 'تصاویر برای بازبینی ارسال شدند.', 'بارگذاری تصاویر انجام نشد.');
    }
  };

  const moveImage = (index: number, step: number) => {
    const target = index + step;
    if (target < 0 || target >= images.length) return;
    const order = images.map(image => image.id);
    [order[index], order[target]] = [order[target], order[index]];
    void changeGallery(() => supplierApi.reorderProductImages(productId, order),
      'ترتیب تصاویر ذخیره شد.', 'درخواست تغییر ترتیب تصاویر برای بازبینی ارسال شد.', 'ترتیب تصاویر ذخیره نشد.');
  };

  const selectCover = (id: string) => void changeGallery(() => supplierApi.setProductCoverImage(productId, id),
    'تصویر شاخص تغییر کرد.', 'درخواست تغییر تصویر شاخص برای بازبینی ارسال شد.', 'تصویر شاخص تغییر نکرد.');

  const removeImage = (id: string) => {
    setConfirmImageId(null);
    void changeGallery(() => supplierApi.deleteProductImage(productId, id),
      'تصویر حذف شد.', 'درخواست حذف تصویر برای بازبینی ارسال شد.', 'تصویر حذف نشد.');
  };

  return (
    <SupplierDialog titleId="supplier-gallery-title" wide onDismiss={busy ? undefined : onClose}>
      <SupplierDialogHeading id="supplier-gallery-title" title="گالری تصاویر" detail={productTitle} onClose={onClose} busy={busy} />
      <div className="space-y-4">
        <SupplierResourceState loading={gallery.loading && !gallery.data} error={gallery.error} retry={gallery.reload} />
        {busy && <p role="status" className="rounded-xl bg-brand-bg p-3 text-sm text-brand-deep">در حال ذخیرهٔ تغییر گالری…</p>}
        {notice && <p role={notice.error ? 'alert' : 'status'} className={`rounded-xl p-3 text-sm ${notice.error ? 'bg-rose-50 text-rose-800' : 'bg-emerald-50 text-emerald-800'}`}>{notice.text}</p>}
        {!gallery.error && gallery.data && (
          <>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-sm text-slate-700">{toPersianDigits(images.length)} از {toPersianDigits(MAX_IMAGES)} تصویر</p>
              {images.length < MAX_IMAGES && (
                <button type="button" disabled={busy} onClick={() => fileRef.current?.click()} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
                  <Plus aria-hidden="true" className="size-4" /> افزودن تصویر
                </button>
              )}
            </div>
            {images.length === 0 ? (
              <p role="status" className="rounded-xl bg-brand-bg p-4 text-sm text-slate-700">هنوز تصویری ثبت نشده است.</p>
            ) : (
              <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3" aria-label="تصاویر گالری">
                {images.map((image, index) => {
                  const number = toPersianDigits(index + 1);
                  return (
                    <li key={image.id} className="min-w-0 overflow-hidden rounded-2xl border border-[#e6e2f0] bg-[#f8f6fc]">
                      <div className="relative aspect-square bg-slate-50">
                        {image.imageUrl
                          ? <img src={image.imageUrl} alt={`تصویر ${number} از ${productTitle}`} className="size-full object-cover" loading="lazy" />
                          : <ImageIcon aria-hidden="true" className="absolute inset-0 m-auto size-6 text-slate-400" />}
                        {index === 0 && <span className="absolute start-2 top-2 rounded-lg bg-white px-2 py-1 text-xs font-bold text-brand-deep shadow-sm">تصویر شاخص</span>}
                      </div>
                      <div role="group" aria-label={`عملیات تصویر ${number}`} className={`grid gap-1.5 p-2 ${images.length > 1 ? 'grid-cols-2' : 'grid-cols-1'}`}>
                        {images.length > 1 && <>
                          <button type="button" disabled={busy || index === 0} onClick={() => moveImage(index, -1)} aria-label={`انتقال تصویر ${number} به قبل`} title="انتقال به قبل" className={imageAction}><ChevronRight aria-hidden="true" className="size-4" /></button>
                          <button type="button" disabled={busy || index === images.length - 1} onClick={() => moveImage(index, 1)} aria-label={`انتقال تصویر ${number} به بعد`} title="انتقال به بعد" className={imageAction}><ChevronLeft aria-hidden="true" className="size-4" /></button>
                        </>}
                        {index > 0 && <button type="button" disabled={busy} onClick={() => selectCover(image.id)} aria-label={`انتخاب تصویر ${number} به‌عنوان شاخص`} title="انتخاب به‌عنوان شاخص" className={imageAction}><Star aria-hidden="true" className="size-4" /></button>}
                        <button type="button" disabled={busy} onClick={() => setConfirmImageId(image.id)} aria-label={`حذف تصویر ${number}`} title="حذف تصویر" className={`flex min-h-11 items-center justify-center gap-1.5 rounded-lg bg-rose-50 text-xs font-bold text-rose-700 hover:bg-rose-100 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-40 ${index === 0 && images.length > 1 ? 'col-span-2' : ''}`}><Trash2 aria-hidden="true" className="size-4" />{index === 0 && 'حذف تصویر'}</button>
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
            <p className="text-xs leading-6 text-slate-600">تصویر اول در فهرست کالاها نمایش داده می‌شود. {images.length > 1 && 'برای تغییر ترتیب از فلش‌ها استفاده کن. '}فرمت‌های JPG، PNG و WEBP تا ۵ مگابایت پذیرفته می‌شوند.</p>
          </>
        )}
        <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={event => handleFiles(event.target.files)} />
      </div>
      {confirmImageId && (
        <SupplierDialog titleId="supplier-delete-image-title" onDismiss={() => setConfirmImageId(null)}>
          <h3 id="supplier-delete-image-title" className="text-base font-extrabold">تصویر حذف شود؟</h3>
          <p className="mt-2 text-sm leading-7 text-slate-700">این تصویر از گالری «{productTitle}» برداشته می‌شود.</p>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" onClick={() => setConfirmImageId(null)} className="min-h-11 rounded-xl border border-slate-300 px-4 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand">انصراف</button>
            <button type="button" onClick={() => removeImage(confirmImageId)} className="min-h-11 rounded-xl bg-rose-700 px-4 text-sm font-bold text-white hover:bg-rose-800 focus-visible:ring-2 focus-visible:ring-rose-700">تأیید حذف تصویر</button>
          </div>
        </SupplierDialog>
      )}
    </SupplierDialog>
  );
};
