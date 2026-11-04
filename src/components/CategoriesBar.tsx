import React, { useEffect, useRef } from 'react';
import { useStore } from '../context/StoreContext';
import { ChevronLeft, Coins, Gem, Home, Plug, Shirt, ShoppingBag, Smartphone, Sparkles } from 'lucide-react';

// Live category-icon map (live bundle A5: Smartphone, Shirt, Sparkles,
// Home, Coins, Plug, Gem). Default for an empty/unknown iconName is
// UNPROVEN (live uses an unknown default component Pn): ShoppingBag is
// used as the fallback.
const CATEGORY_ICONS: Record<string, React.ComponentType<{ className?: string; strokeWidth?: number | string }>> = {
  Smartphone,
  Shirt,
  Sparkles,
  Home,
  Coins,
  Plug,
  Gem,
};

export const CategoriesBar: React.FC = () => {
  const { openCatalogCategory, setActiveView, categories, catalogLoading, products, toPersianDigits } = useStore();
  const scrollRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef({ active: false, startX: 0, startScrollLeft: 0, moved: false });
  const suppressClickRef = useRef(false);

  // Live drag-to-scroll (live bundle _5): mouse-drag scrolls the row and
  // a drag suppresses the click that follows it.
  const handlePointerMove = (e: PointerEvent) => {
    const el = scrollRef.current;
    const d = dragRef.current;
    if (!el || !d.active) return;
    const delta = e.clientX - d.startX;
    if (!d.moved && Math.abs(delta) < 6) return;
    d.moved = true;
    el.scrollLeft = d.startScrollLeft + delta;
  };
  const handlePointerUp = () => {
    const d = dragRef.current;
    if (d.active) {
      d.active = false;
      suppressClickRef.current = d.moved;
      d.moved = false;
      window.removeEventListener('pointermove', handlePointerMove);
      window.removeEventListener('pointerup', handlePointerUp);
      window.removeEventListener('pointercancel', handlePointerUp);
    }
  };
  useEffect(() => () => {
    window.removeEventListener('pointermove', handlePointerMove);
    window.removeEventListener('pointerup', handlePointerUp);
    window.removeEventListener('pointercancel', handlePointerUp);
  }, []);

  // Live wheel-to-horizontal-scroll (live bundle _5): non-passive so it
  // can preventDefault when it scrolls; edge checks stop at both ends.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth + 1 || Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return;
      const delta = e.deltaY;
      if (delta === 0) return;
      const rect = el.getBoundingClientRect();
      const children = el.children;
      if (children.length === 0) return;
      const firstRect = children[0].getBoundingClientRect();
      const lastRect = children[children.length - 1].getBoundingClientRect();
      const atRight = firstRect.right <= rect.right + 1;
      const atLeft = lastRect.left >= rect.left - 1;
      const down = delta > 0;
      if ((atRight && !down) || (atLeft && down)) return;
      e.preventDefault();
      el.scrollLeft -= delta;
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  const handlePointerDown = (e: React.PointerEvent) => {
    const el = scrollRef.current;
    if (!el || e.pointerType !== 'mouse' || e.button !== 0) return;
    suppressClickRef.current = false;
    dragRef.current = { active: true, startX: e.clientX, startScrollLeft: el.scrollLeft, moved: false };
    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', handlePointerUp);
    window.addEventListener('pointercancel', handlePointerUp);
  };
  const handleClickCapture = (e: React.MouseEvent) => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      e.preventDefault();
      e.stopPropagation();
    }
  };

  return (
    <section id="category" data-testid="category" className="bg-white mb-5">
      {/* Categories Carousel — live bundle _5 + Jm (framed, size lg) */}
      <div className="flex items-center">
        <div
          ref={scrollRef}
          onPointerDown={handlePointerDown}
          onClickCapture={handleClickCapture}
          onDragStart={(e) => e.preventDefault()}
          className="flex items-start gap-5 overflow-x-auto no-scrollbar px-4 pt-3 pb-3 flex-1 min-w-0 lg:gap-6 xl:gap-8 lg:px-6 cursor-grab active:cursor-grabbing"
        >
          {catalogLoading && categories.length === 0
            ? Array.from({ length: 6 }).map((_, index) => (
                <div key={index} className="flex flex-col items-center shrink-0 w-[74px] xl:w-[110px] animate-pulse">
                  <div className="w-[70px] h-[70px] xl:w-[100px] xl:h-[100px] rounded-full bg-slate-100" />
                  <div className="w-14 h-3 mt-2.5 rounded-full bg-slate-100" />
                </div>
              ))
            : categories.filter((cat) => !cat.parentId).map((cat) => {
                // Live gradient fallback when the DB gradient is empty;
                // the candidate mapping turns an empty gradient into
                // 'from-slate-50 to-blue-50', which must map to live's.
                const gradient = (cat as any).gradient && (cat as any).gradient !== 'from-slate-50 to-blue-50'
                  ? (cat as any).gradient
                  : 'from-slate-400 to-slate-600';
                const FallbackIcon = CATEGORY_ICONS[(cat as any).iconName] || ShoppingBag;
                // Live count: the current store's products in this category.
                const count = products.filter((p) => p.category === cat.slug).length;
                return (
                  <div
                    key={cat.id}
                    onClick={() => openCatalogCategory(cat.slug)}
                    data-testid="category-item"
                    className="flex flex-col items-center shrink-0 w-[74px] xl:w-[110px] cursor-pointer select-none group"
                  >
                    <div className="transition-transform duration-200 group-hover:scale-105 group-active:scale-90">
                      <div className="w-[70px] h-[70px] xl:w-[100px] xl:h-[100px] rounded-full bg-[#faf9ff] p-[3px] ring-[1.5px] ring-brand/30 shadow-[0_2px_12px_rgba(93,68,248,0.10)]">
                        {cat.image ? (
                          <img
                            src={cat.image}
                            alt=""
                            className="w-full h-full rounded-full object-cover"
                            loading="lazy"
                            onError={(e) => {
                              (e.target as HTMLImageElement).style.display = 'none';
                            }}
                          />
                        ) : (
                          <div className={`w-full h-full rounded-full bg-gradient-to-br ${gradient} flex items-center justify-center`}>
                            <FallbackIcon className="w-8 h-8 xl:w-11 xl:h-11 text-white" strokeWidth={1.8} />
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Title */}
                    <p className="text-xs lg:text-sm font-semibold text-center mt-2 text-slate-700 leading-snug whitespace-nowrap">
                      {cat.title}
                    </p>
                    {/* Live count: current-store products, all viewports. */}
                    {count > 0 && (
                      <p className="text-[10px] lg:text-xs font-bold text-slate-400 text-center mt-0.5 leading-tight">
                        {toPersianDigits(count)} کالا
                      </p>
                    )}
                  </div>
                );
              })}
        </div>
        {/* Live trailing block (separator + view-all button) at the row end. */}
        <div className="flex items-center shrink-0 pe-4">
          <div className="w-px h-10 bg-slate-200" />
          <button onClick={() => setActiveView('categories_tree')} className="flex items-center gap-0.5 ms-3 text-[12px] lg:text-sm font-semibold text-brand hover:opacity-85 transition-opacity py-2">
            <span>مشاهده همه</span>
            <ChevronLeft className="w-3.5 h-3.5 lg:w-4 lg:h-4" />
          </button>
        </div>
      </div>
    </section>
  );
};
