import React, { useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { ZoomIn, X, Sparkles, Package, ChevronLeft, ChevronRight } from 'lucide-react';
import { toPersianDigits } from '../../../utils/formatters';

interface Props {
  mainImage: string;
  images?: string[];
  title: string;
  discountPercent?: number;
}

export const ProductDetailGallery: React.FC<Props> = ({
  mainImage,
  images = [],
  title,
  discountPercent = 0,
}) => {
  const validMainImage = typeof mainImage === 'string' && mainImage.trim() ? mainImage : null;
  const [selectedImg, setSelectedImg] = useState(validMainImage);
  const [isZoomOpen, setIsZoomOpen] = useState(false);

  const gallery = Array.from(new Set([validMainImage, ...(images || [])])).filter(
    (image): image is string => typeof image === 'string' && image.trim().length > 0
  );
  const currentIndex = selectedImg && gallery.indexOf(selectedImg) !== -1 ? gallery.indexOf(selectedImg) : 0;
  const touchX = useRef<number | null>(null);
  const goNext = () => { if (gallery.length > 1) setSelectedImg(gallery[(currentIndex + 1) % gallery.length]); };
  const goPrev = () => { if (gallery.length > 1) setSelectedImg(gallery[(currentIndex - 1 + gallery.length) % gallery.length]); };

  useEffect(() => {
    setSelectedImg(validMainImage);
    setIsZoomOpen(false);
  }, [validMainImage]);

  useEffect(() => {
    if (!isZoomOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setIsZoomOpen(false);
      else if (e.key === 'ArrowLeft') goNext();
      else if (e.key === 'ArrowRight') goPrev();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isZoomOpen, currentIndex, gallery.length]);

  const swipeHandlers = {
    onTouchStart: (e: React.TouchEvent) => { touchX.current = e.touches[0]?.clientX ?? null; },
    onTouchEnd: (e: React.TouchEvent) => {
      if (touchX.current == null || gallery.length <= 1) return;
      const dx = (e.changedTouches[0]?.clientX ?? touchX.current) - touchX.current;
      if (Math.abs(dx) > 48) { if (dx < 0) goNext(); else goPrev(); }
      touchX.current = null;
    },
  };

  return (
    <div className="p-3.5 bg-slate-50/70 border-b border-slate-100 lg:sticky lg:top-24">
      {/* Main Image Frame */}
      <div className="relative w-full aspect-square xl:max-h-[560px] 2xl:max-h-[620px] bg-white rounded-3xl border border-slate-200/90 p-4 flex items-center justify-center overflow-hidden shadow-xs group" {...swipeHandlers}>

        {/* Zoom Button */}
        {selectedImg && (
        <button
          type="button"
          onClick={() => setIsZoomOpen(true)}
          className="absolute bottom-3 left-3 p-3 bg-white/90 hover:bg-white text-slate-700 rounded-xl shadow-xs border border-slate-200 transition-all opacity-80 group-hover:opacity-100 active:scale-95"
          title="بزرگ‌نمایی تصویر"
          aria-label="بزرگ‌نمایی تصویر"
        >
          <ZoomIn className="w-4 h-4"  strokeWidth={1.5}/>
        </button>
        )}

        {/* Image Counter */}
        {gallery.length > 1 && (
          <div className="absolute bottom-3 right-3 px-2 py-0.5 bg-slate-900/70 backdrop-blur-xs text-white text-[10px] font-bold rounded-lg">
            {toPersianDigits(currentIndex + 1)} / {toPersianDigits(gallery.length)}
          </div>
        )}

        {gallery.length > 1 && (
          <>
            <button
              type="button"
              onClick={goNext}
              aria-label="تصویر بعدی"
              className="absolute left-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/90 hover:bg-white shadow-xs border border-slate-200 text-slate-700 flex items-center justify-center transition-all active:scale-95"
            >
              <ChevronLeft className="w-5 h-5" strokeWidth={1.5} />
            </button>
            <button
              type="button"
              onClick={goPrev}
              aria-label="تصویر قبلی"
              className="absolute right-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/90 hover:bg-white shadow-xs border border-slate-200 text-slate-700 flex items-center justify-center transition-all active:scale-95"
            >
              <ChevronRight className="w-5 h-5" strokeWidth={1.5} />
            </button>
          </>
        )}

        {selectedImg ? (
          <img
            src={selectedImg}
            alt={title}
            className="w-full h-full object-contain transition-transform duration-300 group-hover:scale-105 cursor-zoom-in"
            onClick={() => setIsZoomOpen(true)}
            onError={(e) => {
              if (validMainImage && e.currentTarget.src !== validMainImage) e.currentTarget.src = validMainImage;
            }}
          />
        ) : (
          <Package aria-label="تصویر کالا موجود نیست" className="w-16 h-16 text-slate-300"  strokeWidth={1.5}/>
        )}
      </div>

      {/* Badges Row (live: flow row below the image) */}
      <div className="flex items-center gap-1.5 flex-wrap mt-2.5 px-1">
        {discountPercent > 0 && (
          <span className="text-[10px] font-black bg-rose-500 text-white px-2.5 py-1 rounded-full">
            {toPersianDigits(discountPercent)}٪ تخفیف ویژه
          </span>
        )}
      </div>

      {/* Thumbnails Row */}
      {gallery.length > 1 && (
        <div className="flex gap-2 justify-center mt-2.5 overflow-x-auto no-scrollbar px-1 py-1">
          {gallery.map((im, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => setSelectedImg(im)}
              className={`relative w-12 h-12 lg:w-16 lg:h-16 rounded-2xl bg-white border p-1.5 shrink-0 transition-all ${
                selectedImg === im
                  ? 'border-brand-deep ring-2 ring-brand-border shadow-xs scale-105'
                  : 'border-slate-200 opacity-70 hover:opacity-100'
              }`}
            >
            <Package aria-hidden="true" className="absolute inset-0 m-auto w-5 h-5 text-slate-300" strokeWidth={1.5} />
            <img src={im} alt="" loading="lazy" decoding="async" className="relative w-full h-full object-contain rounded-xl" onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }} />
            </button>
          ))}
        </div>
      )}

      {/* Lightbox Modal */}
      {isZoomOpen && createPortal(
        <div
          className="fixed inset-0 z-[70] bg-slate-950 flex flex-col justify-between p-4 select-none"
          onClick={(e) => { if (!(e.target as HTMLElement).closest('img, button')) setIsZoomOpen(false); }}
          {...swipeHandlers}
        >
          <div className="flex items-center justify-between text-white">
            <span className="text-xs font-bold truncate min-w-0 max-w-[280px] sm:max-w-[420px] lg:max-w-[560px]">{title}</span>
            <button
              type="button"
              onClick={() => setIsZoomOpen(false)}
              aria-label="بستن بزرگ‌نمایی"
              className="shrink-0 w-11 h-11 rounded-full bg-white text-slate-900 hover:bg-slate-100 flex items-center justify-center active:scale-95"
            >
              <X className="w-5 h-5"  strokeWidth={1.5}/>
            </button>
          </div>

          <div className="flex-1 relative flex items-center justify-center p-4">
            {selectedImg && (
              <img
                src={selectedImg}
                alt={title}
                className="max-h-[75vh] max-w-full object-contain rounded-2xl"
              />
            )}
            {gallery.length > 1 && (
              <>
                <button
                  type="button"
                  onClick={goNext}
                  aria-label="تصویر بعدی"
                  className="absolute left-1 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/10 hover:bg-white/20 text-white flex items-center justify-center active:scale-95"
                >
                  <ChevronLeft className="w-5 h-5" strokeWidth={1.5} />
                </button>
                <button
                  type="button"
                  onClick={goPrev}
                  aria-label="تصویر قبلی"
                  className="absolute right-1 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/10 hover:bg-white/20 text-white flex items-center justify-center active:scale-95"
                >
                  <ChevronRight className="w-5 h-5" strokeWidth={1.5} />
                </button>
              </>
            )}
          </div>

          {gallery.length > 1 && (
            <div className="text-center text-white text-[11px] font-bold pb-1">
              {toPersianDigits(currentIndex + 1)} / {toPersianDigits(gallery.length)}
            </div>
          )}
          {gallery.length > 1 && (
            <div className="flex gap-2 justify-center overflow-x-auto no-scrollbar py-2">
              {gallery.map((im, idx) => (
                <button
                  key={idx}
                  onClick={() => setSelectedImg(im)}
                  className={`w-12 h-12 rounded-xl border p-1 shrink-0 ${
                    selectedImg === im ? 'border-brand ring-2 ring-brand' : 'border-white/30 opacity-60'
                  }`}
                >
                  <img src={im} alt="" loading="lazy" decoding="async" className="w-full h-full object-contain" />
                </button>
              ))}
            </div>
          )}
        </div>,
        document.body,
      )}
    </div>
  );
};