import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { ProductCard } from './ProductCard';

type CarouselProduct = React.ComponentProps<typeof ProductCard>['product'];

// FIX 13: live's carousel (bundle component Zc) — horizontal snap scroll
// with .carousel-card widths from live CSS, desktop arrow buttons, and a
// "مشاهده همه" end tile handled by the caller. Used by the homepage
// "منتخب محصولات" section in its unfiltered state.
export const ProductCarousel: React.FC<{ products: CarouselProduct[]; endTile?: React.ReactNode }> = ({ products, endTile }) => {
  const trackRef = useRef<HTMLDivElement>(null);
  const [canPrev, setCanPrev] = useState(false);
  const [canNext, setCanNext] = useState(false);

  const updateArrows = useCallback(() => {
    const track = trackRef.current;
    if (!track) return;
    const trackRect = track.getBoundingClientRect();
    const children = Array.from(track.children) as HTMLElement[];
    if (children.length === 0) {
      setCanPrev(false);
      setCanNext(false);
      return;
    }
    const first = children[0].getBoundingClientRect();
    const last = children[children.length - 1].getBoundingClientRect();
    // RTL: content extends to the right (prev) and to the left (next).
    setCanPrev(first.right > trackRect.right + 1);
    setCanNext(last.left < trackRect.left - 1);
  }, []);

  useEffect(() => {
    updateArrows();
    const track = trackRef.current;
    if (!track) return;
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(updateArrows) : null;
    ro?.observe(track);
    window.addEventListener('resize', updateArrows);
    return () => {
      ro?.disconnect();
      window.removeEventListener('resize', updateArrows);
    };
  }, [updateArrows, products.length, endTile != null]);

  // FIX 29: live Zc scroll paging — step by 3 hidden cards (lb=3), with
  // end handling when the target is the last child. The previous version
  // jumped straight to the end on next (last hidden card) and to the
  // first hidden card on prev, unlike live gradual paging (~3 cards per
  // click, about 15 clicks for 40 cards plus the end tile).
  const scroll = (dir: "prev" | "next") => {
    const track = trackRef.current;
    if (!track) return;
    const trackRect = track.getBoundingClientRect();
    const children = Array.from(track.children) as HTMLElement[];
    if (children.length === 0) return;
    const behavior = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth';
    if (dir === "next") {
      const hidden = children.filter((c) => c.getBoundingClientRect().left < trackRect.left - 1);
      if (hidden.length === 0) return;
      const target = hidden[Math.min(3 - 1, hidden.length - 1)];
      if (target === children[children.length - 1]) {
        const step = children.length > 1
          ? children[0].getBoundingClientRect().right - children[1].getBoundingClientRect().right
          : 0;
        const maxScroll = track.scrollWidth - track.clientWidth;
        const index = step > 0
          ? Math.min(children.length - 1, Math.max(0, Math.round(maxScroll / step)))
          : children.length - 1;
        children[index].scrollIntoView({ behavior, block: "nearest", inline: "start" });
      } else {
        target.scrollIntoView({ behavior, block: "nearest", inline: "nearest" });
      }
    } else {
      const hidden = children.filter((c) => c.getBoundingClientRect().right > trackRect.right + 1);
      if (hidden.length === 0) return;
      const target = hidden[Math.max(0, hidden.length - 3)];
      target.scrollIntoView({
        behavior,
        block: "nearest",
        inline: target === children[0] ? "start" : "nearest",
      });
    }
  };
  if (products.length === 0) return null;

  return (
    <div className="central-carousel relative">
      <div
        ref={trackRef}
        onScroll={updateArrows}
        className="central-carousel-track flex items-stretch gap-2.5 lg:gap-4 xl:gap-7 overflow-x-auto no-scrollbar pb-1 pt-0.5 snap-x snap-mandatory"
      >
        {products.map((product) => (
          <div key={product.id} className="carousel-card shrink-0 snap-start">
            <ProductCard product={product} />
          </div>
        ))}
        {endTile && (
          <div className="carousel-card shrink-0 snap-start">{endTile}</div>
        )}
      </div>
      <button
        type="button"
        aria-label="کالاهای قبلی"
        onClick={() => scroll('prev')}
        disabled={!canPrev}
        className="hidden lg:flex absolute right-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white border border-slate-200 shadow-md items-center justify-center text-slate-700 hover:shadow-lg transition-shadow disabled:opacity-40 disabled:cursor-default disabled:hover:shadow-md"
      >
        <ChevronRight className="size-5" strokeWidth={1.5} aria-hidden="true" />
      </button>
      <button
        type="button"
        aria-label="کالاهای بعدی"
        onClick={() => scroll('next')}
        disabled={!canNext}
        className="hidden lg:flex absolute left-2 top-1/2 -translate-y-1/2 z-10 w-11 h-11 rounded-full bg-white border border-slate-200 shadow-md items-center justify-center text-slate-700 hover:shadow-lg transition-shadow disabled:opacity-40 disabled:hover:shadow-md"
      >
        <ChevronLeft className="size-5" strokeWidth={1.5} aria-hidden="true" />
      </button>
    </div>
  );
};
