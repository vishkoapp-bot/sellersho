import React, { useState, useEffect } from 'react';
import { bannersApi, ApiBanner, cleanBannerTitle, isUsableBanner } from '../services/bannersApi';
import { useStore } from '../context/StoreContext';
import { ChevronRight, ChevronLeft } from 'lucide-react';

// Live homepage hero (live bundle D5): real banners from
// GET /banners?position=hero — image + title/subtitle overlay, 4.5s
// auto-advance, null when no usable banner exists. The previous gradient
// mock slides (src/data/banners.ts) are gone; nothing here falls back to
// mock data.
export const HeroSlider: React.FC = () => {
  const { openCatalogCategory } = useStore();
  const [banners, setBanners] = useState<ApiBanner[]>([]);
  const [currentIndex, setCurrentIndex] = useState(0);

  useEffect(() => {
    let cancelled = false;
    bannersApi.getBanners('hero')
      .then((list) => {
        if (!cancelled) setBanners(list.filter(isUsableBanner));
      })
      .catch(() => {
        if (!cancelled) setBanners([]);
      });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    if (banners.length <= 1) return;
    const timer = setInterval(() => {
      setCurrentIndex((prev) => (prev + 1) % banners.length);
    }, 4500);
    return () => clearInterval(timer);
  }, [banners.length]);

  if (banners.length === 0) return null;

  const prevSlide = () => {
    setCurrentIndex((prev) => (prev - 1 + banners.length) % banners.length);
  };

  const nextSlide = () => {
    setCurrentIndex((prev) => (prev + 1) % banners.length);
  };

  const handleBannerClick = (banner: ApiBanner) => {
    if (banner.linkUrl && banner.linkUrl.startsWith('#')) {
      const el = document.querySelector(banner.linkUrl);
      if (el) {
        el.scrollIntoView({ behavior: 'smooth' });
        return;
      }
    }
    openCatalogCategory('all', cleanBannerTitle(banner.title));
  };

  return (
    // FIX 16: full-bleed is structural now: App.tsx renders this hero
    // OUTSIDE the gutter-padded <main> (live banner-reorder model), so
    // no negative margins are needed here. px-3.5 on mobile matches
    // live's base .page-gutter (14px); lg:px-0 is edge-to-edge.
    <div className="px-3.5 lg:px-0 py-1 lg:py-2">
      <div className="relative overflow-hidden shadow-sm aspect-[2.3/1] lg:aspect-auto lg:h-[300px] rounded-2xl lg:rounded-none w-full flex items-center bg-slate-100">
        {banners.map((banner, i) => {
          const title = cleanBannerTitle(banner.title);
          return (
            <div
              key={banner.id}
              className={`absolute inset-0 w-full h-full cursor-pointer transition-opacity duration-500 ${
                i === currentIndex ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'
              }`}
              onClick={() => handleBannerClick(banner)}
            >
              <img
                src={banner.imageUrl}
                alt={title}
                loading={i === 0 ? 'eager' : 'lazy'}
                decoding="async"
                className="w-full h-full object-cover"
              />
              {(title || banner.subtitle || banner.linkUrl) && (
                <div className="absolute inset-y-0 right-0 z-10 flex flex-col justify-center text-right pr-14 pl-2 lg:pr-16 bg-gradient-to-l from-black/60 via-black/25 to-transparent pointer-events-none w-[52%] lg:w-[40%] xl:w-[32%] 2xl:w-[28%]">
                  {title && <h3 className="text-white text-[15px] lg:text-2xl xl:text-3xl font-extrabold tracking-tight drop-shadow-md leading-snug line-clamp-2">{title}</h3>}
                  {banner.subtitle && <p className="text-white/90 text-[11px] lg:text-sm xl:text-base font-medium mt-1 drop-shadow leading-relaxed line-clamp-2">{banner.subtitle}</p>}
                  {banner.linkUrl && (
                    <button
                      type="button"
                      onClick={(e) => { e.stopPropagation(); handleBannerClick(banner); }}
                      className="pointer-events-auto self-start mt-2.5 px-4 py-2 rounded-xl bg-[#5d44f8] hover:bg-[#4a33d8] text-white text-[11px] lg:text-sm lg:px-6 lg:py-3 font-bold shadow-md transition-colors"
                    >
                      مشاهده
                    </button>
                  )}
                </div>
              )}
            </div>
          );
        })}

        {banners.length > 1 && (
          <>
            <button
              onClick={prevSlide}
              className="absolute right-2 z-20 w-11 h-11 rounded-full bg-black/30 hover:bg-black/50 backdrop-blur-sm text-white flex items-center justify-center transition-colors"
              title="قبلی"
              aria-label="اسلاید قبلی"
            >
              <ChevronRight className="w-4 h-4"  strokeWidth={1.5}/>
            </button>

            <button
              onClick={nextSlide}
              className="absolute left-2 z-20 w-11 h-11 rounded-full bg-black/30 hover:bg-black/50 backdrop-blur-sm text-white flex items-center justify-center transition-colors"
              title="بعدی"
              aria-label="اسلاید بعدی"
            >
              <ChevronLeft className="w-4 h-4"  strokeWidth={1.5}/>
            </button>

            <div className="absolute bottom-2 inset-x-0 z-20 flex justify-center gap-1">
              {banners.map((_, i) => (
                <button
                  key={i}
                  onClick={() => setCurrentIndex(i)}
                  className={`h-1.5 rounded-full transition-all ${
                    i === currentIndex ? 'w-5 bg-white shadow-xs' : 'w-1.5 bg-white/50'
                  }`}
                />
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
};
