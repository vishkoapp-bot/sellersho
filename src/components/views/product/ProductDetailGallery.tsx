import React, { useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { ZoomIn, X, Sparkles, Package, ChevronLeft, ChevronRight } from 'lucide-react';
import { toPersianDigits } from '../../../utils/formatters';

interface Props {
  mainImage: string;
  images?: string[];
  title: string;
  discountPercent?: number;
  fashion?: boolean;
}

export const ProductDetailGallery: React.FC<Props> = ({
  mainImage,
  images = [],
  title,
  discountPercent = 0,
  fashion = false,
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

  // Imageless products get a designed empty state, not the giant
  // aspect-square frame: a short surface panel with a small centred
  // «بدون تصویر» treatment (tile surface in fashion, slate in the
  // central design). With an image the frame is the usual square.
  const hasImage = gallery.length > 0;
  const frameClass = !hasImage
    ? fashion
      ? 'relative w-full h-44 lg:h-64 bg-[#f2f2f2] rounded-none border border-[#e5e0d8] flex flex-col items-center justify-center gap-2 overflow-hidden'
      : 'relative w-full h-44 lg:h-64 bg-slate-50 rounded-3xl border border-slate-200/90 flex flex-col items-center justify-center gap-2 overflow-hidden shadow-xs'
    : fashion
      ? 'relative w-full aspect-square xl:max-h-[560px] 2xl:max-h-[620px] bg-white rounded-none border border-[#e5e0d8] p-4 flex items-center justify-center overflow-hidden group'
      : 'relative w-full aspect-square xl:max-h-[560px] 2xl:max-h-[620px] bg-white rounded-3xl border border-slate-200/90 p-4 flex items-center justify-center overflow-hidden shadow-xs group';

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
    <div className={fashion ? 'p-3.5 bg-[#faf8f5] border-b border-[#e5e0d8] lg:sticky lg:top-24' : 'p-3.5 bg-slate-50/70 border-b border-slate-100 lg:sticky lg:top-24'}>
      {/* Main Image Frame */}
      <div className={frameClass} {...swipeHandlers}>

        {/* Zoom Button */}
        {selectedImg && (
        <button
          type="button"
          onClick={() => setIsZoomOpen(true)}
          className={fashion ? 'absolute bottom-3 left-3 p-3 bg-white/90 hover:bg-white text-[#1c1c1a] rounded-none border border-[#e5e0d8] transition-all opacity-80 group-hover:opacity-100 active:scale-95' : 'absolute bottom-3 left-3 p-3 bg-white/90 hover:bg-white text-slate-700 rounded-xl shadow-xs border border-slate-200 transition-all opacity-80 group-hover:opacity-100 active:scale-95'}
          title="بزرگ‌نمایی تصویر"
          aria-label="بزرگ‌نمایی تصویر"
        >
          <ZoomIn className="w-4 h-4"  strokeWidth={1.5}/>
        </button>
        )}

        {/* Image Counter */}
        {gallery.length > 1 && (
          <div className={fashion ? 'absolute bottom-3 right-3 px-2 py-0.5 bg-[#1c1c1a]/70 backdrop-blur-xs text-white text-[10px] font-bold rounded-none' : 'absolute bottom-3 right-3 px-2 py-0.5 bg-slate-900/70 backdrop-blur-xs text-white text-[10px] font-bold rounded-lg'}>
            {toPersianDigits(currentIndex + 1)} / {toPersianDigits(gallery.length)}
          </div>
        )}

        {gallery.length > 1 && (
          <>
            <button
              type="button"
              onClick={goNext}
              aria-label="تصویر بعدی"
              className={fashion ? 'absolute left-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-none bg-white/90 hover:bg-white border border-[#e5e0d8] text-[#1c1c1a] flex items-center justify-center transition-all active:scale-95' : 'absolute left-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/90 hover:bg-white shadow-xs border border-slate-200 text-slate-700 flex items-center justify-center transition-all active:scale-95'}
            >
              <ChevronLeft className="w-5 h-5" strokeWidth={1.5} />
            </button>
            <button
              type="button"
              onClick={goPrev}
              aria-label="تصویر قبلی"
              className={fashion ? 'absolute right-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-none bg-white/90 hover:bg-white border border-[#e5e0d8] text-[#1c1c1a] flex items-center justify-center transition-all active:scale-95' : 'absolute right-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white/90 hover:bg-white shadow-xs border border-slate-200 text-slate-700 flex items-center justify-center transition-all active:scale-95'}
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
          <>
            <Package aria-hidden="true" className={fashion ? 'w-8 h-8 text-[#737373]' : 'w-8 h-8 text-slate-300'} strokeWidth={1.5} />
            <span className={fashion ? 'text-[11px] font-bold text-[#737373]' : 'text-[11px] font-bold text-slate-400'}>بدون تصویر</span>
          </>
        )}
      </div>

      {/* Badges Row (live: flow row below the image) */}
      <div className="flex items-center gap-1.5 flex-wrap mt-2.5 px-1">
        {discountPercent > 0 && (
          <span className={fashion ? 'text-[10px] font-black bg-rose-500 text-white px-2.5 py-1 rounded-none' : 'text-[10px] font-black bg-rose-500 text-white px-2.5 py-1 rounded-full'}>
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
              className={`relative w-12 h-12 lg:w-16 lg:h-16 ${fashion ? 'rounded-none' : 'rounded-2xl'} bg-white border p-1.5 shrink-0 transition-all ${
                selectedImg === im
                  ? fashion
                    ? 'border-[#1c1c1a] ring-1 ring-[#1c1c1a] scale-105'
                    : 'border-brand-deep ring-2 ring-brand-border shadow-xs scale-105'
                  : fashion
                    ? 'border-[#e5e0d8] opacity-70 hover:opacity-100'
                    : 'border-slate-200 opacity-70 hover:opacity-100'
              }`}
            >
            <Package aria-hidden="true" className="absolute inset-0 m-auto w-5 h-5 text-slate-300" strokeWidth={1.5} />
            <img src={im} alt="" loading="lazy" decoding="async" className={fashion ? 'relative w-full h-full object-contain rounded-none' : 'relative w-full h-full object-contain rounded-xl'} onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }} />
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
                className={fashion ? 'max-h-[75vh] max-w-full object-contain rounded-none' : 'max-h-[75vh] max-w-full object-contain rounded-2xl'}
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
                  className={`w-12 h-12 ${fashion ? 'rounded-none' : 'rounded-xl'} border p-1 shrink-0 ${
                    selectedImg === im
                      ? fashion
                        ? 'border-white ring-2 ring-white'
                        : 'border-brand ring-2 ring-brand'
                      : 'border-white/30 opacity-60'
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