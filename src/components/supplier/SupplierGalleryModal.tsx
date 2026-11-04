import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { Image as ImageIcon, Plus, Star, Trash2, ChevronRight, ChevronLeft, Loader2 } from 'lucide-react';

// Live gallery modal (Ll): supplier product gallery, max 8 images,
// first image is the cover. Uploads apply immediately in this build;
// a server that returns pendingReview still takes live's review branch.
const MAX_IMAGES = 8;
const ALLOWED_TYPES = ['image/jpeg', 'image/png', 'image/webp'];

interface GalleryImage {
  id: string;
  imageUrl: string;
  sortOrder?: number;
}

export const SupplierGalleryModal: React.FC<{
  productId: string;
  productTitle: string;
  onClose: () => void;
  onChanged?: () => void;
}> = ({ productId, productTitle, onClose, onChanged }) => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [images, setImages] = useState<GalleryImage[]>([]);
  const [loading, setLoading] = useState(true);
  const [uploading, setUploading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await supplierApi.getProductImages(productId);
      setImages(list || []);
    } catch {
      error('خطا در دریافت تصاویر گالری.');
    } finally {
      setLoading(false);
    }
  }, [productId, error]);

  useEffect(() => { load(); }, [load]);

  const handleFiles = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    const list = Array.from(files);
    if (images.length + list.length > MAX_IMAGES) {
      error(`حداکثر ${toPersianDigits(MAX_IMAGES)} تصویر برای هر محصول مجاز است.`);
      return;
    }
    for (const f of list) {
      if (!ALLOWED_TYPES.includes(f.type)) {
        error('فرمت نامعتبر. فقط JPG، PNG و WEBP مجاز است.');
        return;
      }
      if (f.size > 5 * 1024 * 1024) {
        error('حجم هر فایل نباید بیشتر از ۵ مگابایت باشد.');
        return;
      }
    }
    setUploading(true);
    try {
      const res: any = await supplierApi.uploadProductImages(productId, list);
      if (res?.pendingReview) {
        success('تصاویر برای بازبینی ارسال شد؛ پس از تأیید در گالری نمایش داده می‌شوند.');
      } else {
        success(`${toPersianDigits(list.length)} تصویر به گالری اضافه شد.`);
      }
      await load();
      onChanged?.();
    } catch (e: any) {
      error(e?.message || 'خطا در آپلود تصاویر');
    } finally {
      setUploading(false);
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  const handleDelete = async (id: string) => {
    setBusyId(id);
    try {
      const res: any = await supplierApi.deleteProductImage(productId, id);
      if (res?.pendingReview) {
        success('درخواست حذف تصویر در صف بازبینی قرار گرفت؛ پس از تأیید حذف می‌شود.');
      } else {
        success('تصویر حذف شد.');
      }
      await load();
      onChanged?.();
    } catch {
      error('خطا در حذف تصویر.');
    } finally {
      setBusyId(null);
    }
  };

  const handleCover = async (id: string) => {
    setBusyId(id);
    try {
      const res: any = await supplierApi.setProductCoverImage(productId, id);
      if (res?.pendingReview) {
        success('درخواست تغییر تصویر شاخص در صف بازبینی قرار گرفت.');
      } else {
        success('تصویر شاخص تنظیم شد.');
      }
      await load();
      onChanged?.();
    } catch {
      error('خطا در تنظیم تصویر شاخص.');
    } finally {
      setBusyId(null);
    }
  };

  const handleMove = async (index: number, delta: number) => {
    const target = index + delta;
    if (target < 0 || target >= images.length) return;
    const next = [...images];
    const [moved] = next.splice(index, 1);
    next.splice(target, 0, moved);
    setImages(next);
    try {
      const res: any = await supplierApi.reorderProductImages(productId, next.map((im) => im.id));
      if (res?.pendingReview) {
        success('درخواست تغییر ترتیب گالری در صف بازبینی قرار گرفت.');
      }
      onChanged?.();
    } catch {
      error('خطا در ذخیره ترتیب.');
      await load();
    }
  };

  return (
    <div className="fixed inset-0 z-[9999] flex items-center justify-center p-3 bg-slate-950/70 backdrop-blur-md" onClick={onClose}>
      <div className="bg-white rounded-2xl w-full max-w-[480px] max-h-[90vh] overflow-hidden flex flex-col shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="p-4 border-b border-slate-100 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-extrabold text-slate-800">گالری تصاویر محصول</h3>
            <p className="text-[11px] text-slate-500 mt-0.5 truncate max-w-[280px]">{productTitle}</p>
          </div>
          <button type="button" onClick={onClose} className="p-2 rounded-xl hover:bg-slate-100 text-slate-500 text-lg leading-none">✕</button>
        </div>
        <div className="p-4 overflow-y-auto no-scrollbar flex-1">
          {loading ? (
            <div className="grid grid-cols-3 gap-2">
              {[1, 2, 3].map((k) => (
                <div key={k} className="aspect-square rounded-xl bg-slate-100 animate-pulse" />
              ))}
            </div>
          ) : (
            <>
              <div className="grid grid-cols-3 gap-2 mb-4">
                {images.map((im, idx) => (
                  <div key={im.id} className="relative group aspect-square rounded-xl overflow-hidden border border-slate-200 bg-slate-50">
                    {im.imageUrl ? (
                      <img src={im.imageUrl} alt="" className="w-full h-full object-cover" loading="lazy" />
                    ) : (
                      <div className="w-full h-full flex items-center justify-center">
                        <ImageIcon className="w-6 h-6 text-slate-300"  strokeWidth={1.5}/>
                      </div>
                    )}
                    {idx === 0 && (
                      <span className="absolute top-1 right-1 flex items-center gap-0.5 bg-amber-400 text-amber-950 text-[9px] font-bold px-1.5 py-0.5 rounded-full">
                        <Star className="w-2.5 h-2.5 fill-current"  strokeWidth={1.5}/> شاخص
                      </span>
                    )}
                    <div className="absolute inset-x-0 bottom-0 p-1 bg-gradient-to-t from-black/60 to-transparent flex items-center justify-between opacity-0 group-hover:opacity-100 transition-opacity">
                      <div className="flex gap-0.5">
                        <button type="button" onClick={() => handleMove(idx, -1)} disabled={idx === 0 || busyId !== null} className="p-1 rounded-lg bg-white/90 text-slate-700 disabled:opacity-30" title="انتقال به قبل">
                          <ChevronRight className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                        </button>
                        <button type="button" onClick={() => handleMove(idx, 1)} disabled={idx === images.length - 1 || busyId !== null} className="p-1 rounded-lg bg-white/90 text-slate-700 disabled:opacity-30" title="انتقال به بعد">
                          <ChevronLeft className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                        </button>
                      </div>
                      <div className="flex gap-0.5">
                        {idx !== 0 && (
                          <button type="button" onClick={() => handleCover(im.id)} disabled={busyId !== null} className="p-1 rounded-lg bg-white/90 text-amber-600" title="تنظیم به‌عنوان شاخص">
                            <Star className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                          </button>
                        )}
                        <button type="button" onClick={() => handleDelete(im.id)} disabled={busyId !== null} className="p-1 rounded-lg bg-white/90 text-rose-600" title="حذف">
                          {busyId === im.id ? <Loader2 className="w-3.5 h-3.5 animate-spin"  strokeWidth={1.5}/> : <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>}
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
                {images.length < MAX_IMAGES && (
                  <button type="button" onClick={() => fileRef.current?.click()} disabled={uploading} className="aspect-square rounded-xl border-2 border-dashed border-brand-border hover:border-brand-deep bg-brand-soft/40 hover:bg-brand-soft flex flex-col items-center justify-center gap-1.5 text-brand-deep transition-colors disabled:opacity-50">
                    {uploading ? <Loader2 className="w-6 h-6 animate-spin"  strokeWidth={1.5}/> : (<><Plus className="w-6 h-6"  strokeWidth={1.5}/><span className="text-[10px] font-bold">افزودن تصویر</span></>)}
                  </button>
                )}
              </div>
              <p className="text-[10px] text-slate-400 text-center">{toPersianDigits(images.length)} از {toPersianDigits(MAX_IMAGES)} تصویر • اولین تصویر، تصویر شاخص محصول است</p>
            </>
          )}
          <input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" multiple className="hidden" onChange={(e) => handleFiles(e.target.files)} />
        </div>
        <div className="p-3 border-t border-slate-100 bg-slate-50 flex items-center gap-2 text-[10px] text-slate-500">
          <ImageIcon className="w-3.5 h-3.5 shrink-0"  strokeWidth={1.5}/>
          <span>با دکمه‌های جهت‌دار ترتیب نمایش را تغییر دهید. تصویر اول به‌عنوان شاخص در لیست‌ها نمایش داده می‌شود.</span>
        </div>
      </div>
    </div>
  );
};
