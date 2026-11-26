import React, { useEffect, useState } from 'react';
import { ArrowLeft, ChevronLeft, ChevronRight, LayoutGrid } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { bannersApi, cleanBannerTitle, isUsableBanner, type ApiBanner } from '../services/bannersApi';
import { pathForView } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';

export function campaignAddress(banner: ApiBanner) {
  const link = banner.linkUrl?.trim();
  if (link?.startsWith('#')) return link.length > 1 ? link : null;
  if (link?.startsWith('/') && !/^\/[\\/]/.test(link)) return link;
  if (link && /^https?:\/\//i.test(link)) return link;
  return null;
}

export const HeroSlider: React.FC<{ position?: 'hero' | 'secondary' }> = ({ position = 'hero' }) => {
  const { openCatalogCategory, setActiveView, toPersianDigits, categories } = useStore();
  const [banners, setBanners] = useState<ApiBanner[]>([]);
  const [index, setIndex] = useState(0);

  useEffect(() => {
    let cancelled = false;
    bannersApi.getBanners(position)
      .then(list => {
        if (cancelled) return;
        setBanners(list.filter(position === 'hero' ? banner => !!banner.imageUrl?.trim() : isUsableBanner));
        setIndex(0);
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [position]);

  const banner = banners[index];
  const title = cleanBannerTitle(banner?.title);
  const editorialHero = position === 'hero' && title && banner?.imageUrl.startsWith('/static/uploads/banners/selersho-editorial-');
  const address = banner ? campaignAddress(banner) : null;
  const categorySlug = categories.find(category => title && category.title === title)?.slug ?? 'all';
  const hasCampaignAction = address ? address !== pathForView('catalog') : categorySlug !== 'all';
  const show = (delta: number) => setIndex(current => (current + delta + banners.length) % banners.length);
  const controls = banners.length > 1 && (
    <div className="central-campaign-controls">
      <button type="button" onClick={() => show(-1)} aria-label="پیشنهاد قبلی"><ChevronRight size={20} aria-hidden="true" /></button>
      <span aria-live="polite">{toPersianDigits(index + 1)} از {toPersianDigits(banners.length)}</span>
      <button type="button" onClick={() => show(1)} aria-label="پیشنهاد بعدی"><ChevronLeft size={20} aria-hidden="true" /></button>
    </div>
  );

  if (position === 'hero') return (
    <section className="central-hero" aria-labelledby="central-home-title">
      <h1 id="central-home-title" className="sr-only">فروشگاه آنلاین سلرشو</h1>
      <div className="central-hero-media">
        <img className="central-hero-fallback" src="/storefront/central-hero.webp"
          srcSet="/storefront/central-hero-small.webp 640w, /storefront/central-hero.webp 1200w"
          sizes="(min-width: 1352px) 1280px, 94vw" alt="" width="1200" height="800" fetchPriority="high" decoding="async" />
        {banner && <img key={banner.id} className={editorialHero ? 'central-hero-banner central-hero-banner--editorial' : 'central-hero-banner'} src={banner.imageUrl.trim()}
          alt={editorialHero ? '' : [title, banner.subtitle?.trim()].filter(Boolean).join('، ') || 'بنر فروشگاه سلرشو'}
          fetchPriority="high" decoding="async" onError={() => {
            setBanners(current => current.filter(item => item.id !== banner.id));
            setIndex(current => Math.min(current, Math.max(0, banners.length - 2)));
          }} />}
        {editorialHero && <div className="central-hero-caption" aria-live="polite">
          <h2>{title}</h2>
          {banner.subtitle && <p>{banner.subtitle}</p>}
        </div>}
      </div>
      <div className="central-hero-bar">
        <div className="central-hero-actions">
          {hasCampaignAction && <a className="central-button" href={address || pathForView('catalog', { categorySlug })}
            onClick={address ? undefined : event => followStoreLink(event, () => openCatalogCategory(categorySlug))}>
            مشاهده پیشنهاد <ArrowLeft size={19} aria-hidden="true" />
          </a>}
          <a className={hasCampaignAction ? 'central-button central-button--outline' : 'central-button'} href={pathForView('catalog')}
            onClick={event => followStoreLink(event, () => openCatalogCategory('all'))}>مشاهده همه کالاها<ArrowLeft size={19} aria-hidden="true" /></a>
          <a className="central-button central-button--outline" href={pathForView('categories_tree')}
            onClick={event => followStoreLink(event, () => setActiveView('categories_tree'))}><LayoutGrid size={17} aria-hidden="true" /> دسته‌بندی‌ها</a>
        </div>
        {controls}
      </div>
    </section>
  );

  if (!banner) return null;

  return (
    <section className="central-campaign" aria-label="پیشنهادهای فروشگاه" data-testid="slider-banner">
      <div className="central-campaign-copy">
        {title && <h2>{title}</h2>}
        {banner.subtitle && <p>{banner.subtitle}</p>}
        <a href={address || pathForView('catalog', { categorySlug })}
          onClick={address ? undefined : event => followStoreLink(event, () => openCatalogCategory(categorySlug))}>
          مشاهده پیشنهاد <ArrowLeft size={18} aria-hidden="true" />
        </a>
        {controls}
      </div>
      {banner.imageUrl && <img className="central-campaign-image" src={banner.imageUrl} alt={title ? '' : 'بنر پیشنهاد فروشگاه'} width="760" height="320" loading="lazy" decoding="async" />}
    </section>
  );
};
