import React, { useState, useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { ZoomIn, X, Package, ChevronLeft, ChevronRight } from 'lucide-react';
import { toPersianDigits } from '../../../utils/formatters';
import { SaleCountdown } from '../../common/SaleCountdown';
import { FallbackImage } from '../../common/FallbackImage';
import { CampaignDiscountBadge } from '../../common/CampaignDiscountBadge';

interface Props {
  mainImage: string;
  images?: string[];
  title: string;
  discountPercent?: number;
  campaignName?: string;
  fashion?: boolean;
  central?: boolean;
  // P0: when the variant image itself 404s, fall back to the product
  // image (passed by the PDP as displayBase.image).
  fallbackImage?: string;
  // P1(c): active sale window of the selected variant (from
  // productForVariant); the countdown renders under the badges row.
  saleEndsAt?: string | null;
  saleStartsAt?: string | null;
}

export const ProductDetailGallery: React.FC<Props> = ({
  mainImage,
  images = [],
  title,
  discountPercent = 0,
  campaignName,
  fashion = false,
  central = false,
  fallbackImage,
  saleEndsAt,
  saleStartsAt,
}) => {
  const validMainImage = typeof mainImage === 'string' && mainImage.trim() ? mainImage : null;
  const [selectedImg, setSelectedImg] = useState(validMainImage);
  const [isZoomOpen, setIsZoomOpen] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const zoomButtonRef = useRef<HTMLButtonElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  // P1-14: true once every main-image candidate has failed — the zoom
  // button, counter and arrows only make sense with a loaded image.
  const [mainImageFailed, setMainImageFailed] = useState(false);

  const gallery = Array.from(new Set([validMainImage, ...(images || [])])).filter(
    (image): image is string => typeof image === 'string' && image.trim().length > 0
  );
  const currentIndex = selectedImg && gallery.indexOf(selectedImg) !== -1 ? gallery.indexOf(selectedImg) : 0;
  const swipeStart = useRef<{ x: number; y: number } | null>(null);
  const goNext = () => { if (gallery.length > 1) setSelectedImg(gallery[(currentIndex + 1) % gallery.length]); };
  const goPrev = () => { if (gallery.length > 1) setSelectedImg(gallery[(currentIndex - 1 + gallery.length) % gallery.length]); };

  // P0: two variants can share a hero image but carry different
  // galleries — reset on the whole gallery, not just the main image.
  const galleryKey = [validMainImage, ...(images || [])].join('|');
  useEffect(() => {
    setSelectedImg(validMainImage);
    setIsZoomOpen(false);
    setMainImageFailed(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [galleryKey]);
  // P1-14: main-image candidates in priority order — the selected
  // (variant) image, then the product image, then the PDP's
  // fallbackImage. The variant -> product chain is preserved; the
  // shared empty state is the terminal fallback after it.
  const mainSrcs = Array.from(
    new Set([selectedImg, validMainImage, fallbackImage]),
  ).filter((src): src is string => typeof src === 'string' && src.trim().length > 0);
  const emptyState = (
    <>
      <Package aria-hidden="true" className={fashion ? 'w-8 h-8 text-[#737373]' : 'w-8 h-8 text-slate-300'} strokeWidth={1.5} />
      <span className={fashion ? 'text-[11px] font-bold text-[#737373]' : 'text-[11px] font-bold text-slate-400'}>بدون تصویر</span>
    </>
  );

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!isZoomOpen || !dialog) return;
    dialog.showModal();
    closeButtonRef.current?.focus({ preventScroll: true });
    return () => {
      if (dialog.open) dialog.close();
      zoomButtonRef.current?.focus();
    };
  }, [isZoomOpen]);

  // Imageless products get a designed empty state, not the giant
  // aspect-square frame: a short surface panel with a small centred
  // «بدون تصویر» treatment (tile surface in fashion, slate in the
  // central design). With an image the frame is the usual square.
  const hasImage = mainSrcs.length > 0;
  const frameClass = !hasImage
    ? fashion
      ? 'relative w-full h-44 lg:h-64 bg-[#f2f2f2] rounded-none border border-[#e5e0d8] flex flex-col items-center justify-center gap-2 overflow-hidden'
      : 'relative w-full h-44 lg:h-64 bg-slate-50 rounded-3xl border border-slate-200/90 flex flex-col items-center justify-center gap-2 overflow-hidden shadow-xs'
    : fashion
      ? 'relative w-full aspect-square xl:max-h-[560px] 2xl:max-h-[620px] bg-[#eee6dc] rounded-xl border border-[#ded4c8] p-2 flex items-center justify-center overflow-hidden group'
      : 'relative w-full aspect-square xl:max-h-[560px] 2xl:max-h-[620px] bg-white rounded-3xl border border-slate-200/90 p-4 flex items-center justify-center overflow-hidden shadow-xs group';

  const swipeHandlers = {
    onTouchStart: (event: React.TouchEvent) => {
      const touch = event.touches[0];
      swipeStart.current = touch ? { x: touch.clientX, y: touch.clientY } : null;
    },
    onTouchEnd: (event: React.TouchEvent) => {
      const start = swipeStart.current;
      const touch = event.changedTouches[0];
      swipeStart.current = null;
      if (!start || !touch || gallery.length <= 1) return;
      const dx = touch.clientX - start.x;
      const dy = touch.clientY - start.y;
      if (Math.abs(dx) > 48 && Math.abs(dx) > Math.abs(dy)) {
        if (dx < 0) goNext();
        else goPrev();
      }
    },
    onTouchCancel: () => { swipeStart.current = null; },
  };

  return (
    <div className={central ? 'central-pdp-gallery-card' : fashion ? 'p-3.5 bg-[#faf8f5] border-b border-[#e5e0d8] lg:sticky lg:top-24' : 'p-3.5 bg-slate-50/70 border-b border-slate-100 lg:sticky lg:top-24'}>
      {/* Main Image Frame */}
      <div className={`${frameClass}${central ? ' central-pdp-gallery-frame' : ''}`} {...swipeHandlers}>

        {/* Zoom Button */}
        {hasImage && !mainImageFailed && (
        <button
          type="button"
          ref={zoomButtonRef}
          aria-haspopup="dialog"
          onClick={() => setIsZoomOpen(true)}
          className={fashion ? 'absolute bottom-3 left-3 p-3 bg-white/90 hover:bg-white text-[#1c1c1a] rounded-none border border-[#e5e0d8] transition-all opacity-80 group-hover:opacity-100 active:scale-95' : 'absolute bottom-3 left-3 p-3 bg-white/90 hover:bg-white text-slate-700 rounded-xl shadow-xs border border-slate-200 transition-all opacity-80 group-hover:opacity-100 active:scale-95'}
          title="بزرگ‌نمایی تصویر"
          aria-label="بزرگ‌نمایی تصویر"
        >
          <ZoomIn className="w-4 h-4"  strokeWidth={1.5}/>
        </button>
        )}

        {/* Image Counter */}
        {gallery.length > 1 && !mainImageFailed && (
          <div className={fashion ? 'absolute bottom-3 right-3 px-2 py-0.5 bg-[#1c1c1a]/70 backdrop-blur-xs text-white text-[10px] font-bold rounded-none' : 'absolute bottom-3 right-3 px-2 py-0.5 bg-slate-900/70 backdrop-blur-xs text-white text-[10px] font-bold rounded-lg'}>
            {toPersianDigits(currentIndex + 1)} / {toPersianDigits(gallery.length)}
          </div>
        )}

        {gallery.length > 1 && !mainImageFailed && (
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

        <FallbackImage
          srcs={mainSrcs}
          alt={title}
          className="w-full h-full object-contain transition-transform duration-300 group-hover:scale-105 cursor-zoom-in"
          loading="eager"
          fetchPriority="high"
          onClick={() => setIsZoomOpen(true)}
          onLoad={() => setMainImageFailed(false)}
          onExhausted={() => setMainImageFailed(true)}
          fallback={emptyState}
        />
      </div>

      {/* Badges Row (live: flow row below the image) */}
      {discountPercent > 0 && (
      <div className="flex items-center gap-1.5 flex-wrap mt-2.5 px-1">
        {central ? <CampaignDiscountBadge central campaignName={campaignName} discountPercent={discountPercent} /> : (
          <span className={fashion ? 'text-[10px] font-black bg-rose-500 text-white px-2.5 py-1 rounded-none' : 'text-[10px] font-black bg-rose-500 text-white px-2.5 py-1 rounded-full'}>
            {toPersianDigits(discountPercent)}٪ تخفیف ویژه
          </span>
        )}
      </div>
      )}
      {/* P1(c): sale countdown on its own line under the badges (390px
          is too narrow to sit next to the badge). */}
      {saleEndsAt && (
        <div className="mt-1.5 px-1">
          <SaleCountdown endsAt={saleEndsAt} startsAt={saleStartsAt} fashion={fashion} central={central} />
        </div>
      )}

      {/* Thumbnails Row */}
      {gallery.length > 1 && (
        <div className="flex gap-2 justify-center mt-2.5 overflow-x-auto no-scrollbar px-1 py-1">
          {gallery.map((im, idx) => (
            <button
              key={im}
              type="button"
              aria-label={`تصویر ${toPersianDigits(idx + 1)}`}
              aria-pressed={selectedImg === im}
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
        <dialog
          ref={dialogRef}
          aria-label={`تصاویر ${title}`}
          className="fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none border-0 bg-slate-950 p-4 select-none open:flex open:flex-col open:justify-between"
          onCancel={(event) => { event.preventDefault(); setIsZoomOpen(false); }}
          onClick={(event) => { if (event.target === event.currentTarget) setIsZoomOpen(false); }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowLeft') { event.preventDefault(); goNext(); }
            if (event.key === 'ArrowRight') { event.preventDefault(); goPrev(); }
          }}
          {...swipeHandlers}
        >
          <div className="flex items-center justify-between text-white">
            <span className="text-xs font-bold truncate min-w-0 max-w-[280px] sm:max-w-[420px] lg:max-w-[560px]">{title}</span>
            <button
              type="button"
              ref={closeButtonRef}
              onClick={() => setIsZoomOpen(false)}
              aria-label="بستن بزرگ‌نمایی"
              className="shrink-0 w-11 h-11 rounded-full bg-white text-slate-900 hover:bg-slate-100 flex items-center justify-center active:scale-95"
            >
              <X className="w-5 h-5"  strokeWidth={1.5}/>
            </button>
          </div>

          <div className="flex-1 relative flex items-center justify-center p-4">
            {hasImage && (
              <FallbackImage
                srcs={mainSrcs}
                alt={title}
                loading="eager"
                className={fashion ? 'max-h-[75vh] max-w-full object-contain rounded-none' : 'max-h-[75vh] max-w-full object-contain rounded-2xl'}
                fallback={
                  <div className="flex flex-col items-center justify-center gap-2 text-white/70">
                    <Package aria-hidden="true" className="w-8 h-8" strokeWidth={1.5} />
                    <span className="text-[11px] font-bold">بدون تصویر</span>
                  </div>
                }
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
                  key={im}
                  type="button"
                  aria-label={`تصویر ${toPersianDigits(idx + 1)}`}
                  aria-pressed={selectedImg === im}
                  onClick={() => setSelectedImg(im)}
                  className={`relative w-12 h-12 ${fashion ? 'rounded-none' : 'rounded-xl'} border p-1 shrink-0 ${
                    selectedImg === im
                      ? fashion
                        ? 'border-white ring-2 ring-white'
                        : 'border-brand ring-2 ring-brand'
                      : 'border-white/30 opacity-60'
                  }`}
                >
                  <Package aria-hidden="true" className="absolute inset-0 m-auto w-5 h-5 text-white/50" strokeWidth={1.5} />
                  <img src={im} alt="" loading="lazy" decoding="async" className="relative w-full h-full object-contain" onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }} />
                </button>
              ))}
            </div>
          )}
        </dialog>,
        document.body,
      )}
    </div>
  );
};
