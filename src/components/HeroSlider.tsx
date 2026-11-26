import React, { useEffect, useState } from 'react';
import { ArrowLeft, ChevronLeft, ChevronRight, LayoutGrid } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { bannersApi, cleanBannerTitle, isUsableBanner, type ApiBanner } from '../services/bannersApi';
import { pathForView } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';

export function CentralHomeIntro() {
  const { openCatalogCategory, setActiveView } = useStore();
  return (
    <section className="central-intro" aria-labelledby="central-home-title">
      <div className="central-intro-copy">
        <p className="central-intro-label">فروشگاه آنلاین سلرشو</p>
        <h1 id="central-home-title">برای هر روز،<br /> انتخابی بهتر</h1>
        <p>از کالاهای دیجیتال تا پوشاک و لوازم خانه. کالاها را پیدا کنید، جزئیات را ببینید و آنلاین سفارش دهید.</p>
        <div className="central-intro-actions">
          <a className="central-button" href={pathForView('catalog')}
            onClick={event => followStoreLink(event, () => openCatalogCategory('all'))}>مشاهده همه کالاها<ArrowLeft size={19} aria-hidden="true" /></a>
          <a className="central-button central-button--outline" href={pathForView('categories_tree')}
            onClick={event => followStoreLink(event, () => setActiveView('categories_tree'))}><LayoutGrid size={17} aria-hidden="true" /> دسته‌بندی‌ها</a>
        </div>
      </div>
      <div className="central-intro-media">
        <img src="/storefront/central-hero.webp" alt="" width="1200" height="800" fetchPriority="high" decoding="async" />
      </div>
    </section>
  );
}

export function campaignAddress(banner: ApiBanner) {
  const link = banner.linkUrl?.trim();
  if (link?.startsWith('#')) return link.length > 1 ? link : null;
  if (link?.startsWith('/') && !/^\/[\\/]/.test(link)) return link;
  if (link && /^https?:\/\//i.test(link)) return link;
  return null;
}

export const HeroSlider: React.FC<{ position?: 'hero' | 'secondary' }> = ({ position = 'hero' }) => {
  const { openCatalogCategory, toPersianDigits, categories } = useStore();
  const [banners, setBanners] = useState<ApiBanner[]>([]);
  const [index, setIndex] = useState(0);

  useEffect(() => {
    let cancelled = false;
    bannersApi.getBanners(position)
      .then(list => { if (!cancelled) setBanners(list.filter(isUsableBanner)); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [position]);

  const banner = banners[index];
  if (!banner) return null;
  const title = cleanBannerTitle(banner.title);
  const address = campaignAddress(banner);
  const categorySlug = categories.find(category => category.title === title)?.slug ?? 'all';
  const show = (delta: number) => setIndex(current => (current + delta + banners.length) % banners.length);

  return (
    <section className="central-campaign" aria-label="پیشنهادهای فروشگاه" data-testid={position === 'secondary' ? 'slider-banner' : undefined}>
      <div className="central-campaign-copy">
        {title && <h2>{title}</h2>}
        {banner.subtitle && <p>{banner.subtitle}</p>}
        <a href={address || pathForView('catalog', { categorySlug })}
          onClick={address ? undefined : event => followStoreLink(event, () => openCatalogCategory(categorySlug))}>
          مشاهده پیشنهاد <ArrowLeft size={18} aria-hidden="true" />
        </a>
        {banners.length > 1 && <div className="central-campaign-controls">
          <button type="button" onClick={() => show(-1)} aria-label="پیشنهاد قبلی"><ChevronRight size={20} aria-hidden="true" /></button>
          <span aria-live="polite">{toPersianDigits(index + 1)} از {toPersianDigits(banners.length)}</span>
          <button type="button" onClick={() => show(1)} aria-label="پیشنهاد بعدی"><ChevronLeft size={20} aria-hidden="true" /></button>
        </div>}
      </div>
      {banner.imageUrl && <img className="central-campaign-image" src={banner.imageUrl} alt={title ? '' : 'بنر پیشنهاد فروشگاه'} width="760" height="320" loading="lazy" decoding="async" />}
    </section>
  );
};
