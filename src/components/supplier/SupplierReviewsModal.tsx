import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi } from '../../services/supplierApi';
import { Star, CheckCircle2 } from 'lucide-react';
import { SupplierDialog, SupplierDialogHeading } from './SupplierDialog';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';

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
    <SupplierDialog titleId="supplier-reviews-title" onDismiss={onClose}>
      <SupplierDialogHeading id="supplier-reviews-title" title="نظرات خریداران" detail={productTitle} onClose={onClose} />
      <SupplierResourceState loading={loading} error={failed ? 'دریافت نظرات انجام نشد.' : ''} retry={() => { setLoading(true); load(0); }} />
      {!loading && !failed && (count === 0 || total === 0 ? (
            <SupplierEmptyState icon={<Star />} title="هنوز نظری برای این کالا ثبت نشده است." />
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
                    <p className="whitespace-pre-line break-words text-sm leading-6 text-slate-700">{rv.body}</p>
                  </article>
                ))}
              </div>
              {items.length < total && (
                <button type="button" disabled={loadingMore} onClick={() => { setLoadingMore(true); load(items.length); }} className="min-h-11 w-full rounded-xl border border-slate-200 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
                  {loadingMore ? 'در حال بارگذاری…' : 'نمایش نظرات بیشتر'}
                </button>
              )}
            </div>
          ))}
    </SupplierDialog>
  );
};
