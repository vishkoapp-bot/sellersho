import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { Star, X, CheckCircle2 } from 'lucide-react';

// Live reviews modal (jl shell + rv list): buyer reviews for one product.
// Page size is live's ZC=10; load-more appends by offset.
const PAGE_SIZE = 10;

interface ReviewItem {
  id: string;
  displayName: string;
  rating: number;
  title?: string;
  body: string;
  createdAt: string;
}

const Stars: React.FC<{ rating: number; className?: string }> = ({ rating, className }) => {
  const full = Math.round(rating);
  return (
    <span className="flex items-center gap-px" aria-label={`امتیاز ${rating}`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <Star key={i} className={`${className || 'w-3.5 h-3.5'} ${i <= full ? 'text-amber-400 fill-amber-400' : 'text-slate-300'}`} strokeWidth={1.5} />
      ))}
    </span>
  );
};

export const SupplierReviewsModal: React.FC<{
  productId: string;
  productTitle: string;
  rating: number;
  reviewsCount: number;
  onClose: () => void;
}> = ({ productId, productTitle, rating, reviewsCount, onClose }) => {
  const { toPersianDigits } = useStore();
  const { error } = useToast();
  const [items, setItems] = useState<ReviewItem[]>([]);
  const [total, setTotal] = useState(0);
  const [avgRating, setAvgRating] = useState(rating);
  const [count, setCount] = useState(reviewsCount);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async (offset: number) => {
    try {
      const data: any = await supplierApi.getProductReviews(productId, PAGE_SIZE, offset);
      setItems((prev) => (offset === 0 ? data.items : [...prev, ...data.items]));
      setTotal(data.total);
      setAvgRating(data.rating);
      setCount(data.reviewsCount);
      setFailed(false);
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [productId]);

  useEffect(() => {
    setLoading(true);
    setItems([]);
    setTotal(0);
    load(0);
  }, [load]);

  return (
    <div className="fixed inset-0 z-[70] bg-slate-950/60 backdrop-blur-sm flex items-end sm:items-center justify-center p-0 sm:p-4" dir="rtl">
      <div className="bg-white w-full sm:max-w-lg max-h-[92vh] overflow-y-auto rounded-t-3xl sm:rounded-3xl shadow-2xl">
        <div className="sticky top-0 bg-white/95 backdrop-blur-md border-b border-slate-200 px-4 py-3 flex items-center justify-between z-10">
          <div className="min-w-0">
            <h2 className="text-sm font-extrabold text-slate-900 flex items-center gap-1.5">
              <Star className="w-4 h-4 text-amber-400 fill-amber-400" strokeWidth={1.5} />
              نظرات خریداران
            </h2>
            <p className="text-[11px] text-slate-500 truncate max-w-[260px]">{productTitle} • میانگین {toPersianDigits(avgRating)} از ۵ ({toPersianDigits(count)} نظر)</p>
          </div>
          <button type="button" onClick={onClose} className="p-2 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-600 shrink-0" aria-label="بستن">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>
        <div className="p-4">
          {loading ? (
            <p className="text-[11px] text-slate-400 py-2">در حال بارگذاری نظرات…</p>
          ) : failed ? (
            <div className="py-2 space-y-2">
              <p className="text-[11px] text-slate-500">بارگذاری نظرات ناموفق بود.</p>
              <button type="button" onClick={() => { setLoading(true); load(0); }} className="text-[11px] font-bold px-3 py-1.5 rounded-lg border border-slate-200 text-slate-700 hover:bg-slate-50">تلاش مجدد</button>
            </div>
          ) : count === 0 || total === 0 ? (
            <p className="text-[11px] text-slate-500 py-1">هنوز نظری برای این کالا ثبت نشده است.</p>
          ) : (
            <div className="space-y-3">
              <div className="flex items-center gap-2 flex-wrap">
                <Stars rating={avgRating} />
                <span className="text-[11px] font-bold text-slate-700">میانگین {toPersianDigits(avgRating)} از {toPersianDigits(5)}</span>
                <span className="text-[11px] text-slate-400">•</span>
                <span className="text-[11px] text-slate-500">{toPersianDigits(count)} نظر</span>
              </div>
              <div className="space-y-2.5">
                {items.map((rv) => (
                  <article key={rv.id} className="bg-slate-50 rounded-xl p-2.5 space-y-1.5 border border-slate-100">
                    <div className="flex items-center justify-between gap-2 flex-wrap">
                      <div className="flex items-center gap-1.5">
                        <span className="text-[11px] font-bold text-slate-800">{rv.displayName}</span>
                        <span className="flex items-center gap-0.5 text-[9px] font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-1.5 py-px rounded-full">
                          <CheckCircle2 className="w-3 h-3" strokeWidth={1.5} />
                          خریدار تأییدشده
                        </span>
                      </div>
                      <span className="text-[10px] text-slate-400">{new Date(rv.createdAt).toLocaleDateString('fa-IR')}</span>
                    </div>
                    <Stars rating={rv.rating} className="w-3 h-3" />
                    {rv.title ? <h4 className="text-[11px] font-extrabold text-slate-800">{rv.title}</h4> : null}
                    <p className="text-[11px] text-slate-600 leading-relaxed whitespace-pre-line">{rv.body}</p>
                  </article>
                ))}
              </div>
              {items.length < total && (
                <button type="button" disabled={loadingMore} onClick={() => { setLoadingMore(true); load(items.length); }} className="w-full py-2 text-[11px] font-bold rounded-xl border border-slate-200 text-slate-700 hover:bg-slate-50 disabled:opacity-50 transition-colors" style={{ color: '#5d44f8' }}>
                  {loadingMore ? 'در حال بارگذاری…' : 'نمایش نظرات بیشتر'}
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
