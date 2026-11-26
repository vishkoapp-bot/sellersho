import React, { useEffect, useState } from 'react';
import { ArrowLeft, Store } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { homepageApi, type HomepageSection } from '../services/homepageApi';
import { bannersApi, cleanBannerTitle, isUsableBanner, type ApiBanner } from '../services/bannersApi';
import { followStoreLink } from '../utils/followStoreLink';
import { pathForView } from '../utils/routing';
import { ProductCarouselSection } from './ProductCarouselSection';
import { ProductGridSection } from './ProductGridSection';
import { TrustFeatures } from './TrustFeatures';
import { campaignAddress, HeroSlider } from './HeroSlider';

function BannerTiles({ id, position }: { id: string; position: 'promo' | 'tile' }) {
  const { openCatalogCategory, categories } = useStore();
  const [banners, setBanners] = useState<ApiBanner[]>([]);

  useEffect(() => {
    let active = true;
    bannersApi.getBanners(position).then(items => {
      if (active) setBanners(items.filter(isUsableBanner));
    }).catch(() => {});
    return () => { active = false; };
  }, [position]);

  if (!banners.length) return null;
  return (
    <section id={id} className={`central-promo-grid central-promo-grid--${position}`} aria-label="پیشنهادهای خرید">
      {banners.map(banner => {
        const title = cleanBannerTitle(banner.title);
        const address = campaignAddress(banner);
        const categorySlug = categories.find(category => category.title === title)?.slug ?? 'all';
        return (
          <a key={banner.id} className="central-promo-tile" href={address || pathForView('catalog', { categorySlug })}
            onClick={address ? undefined : event => followStoreLink(event, () => openCatalogCategory(categorySlug))}>
            {banner.imageUrl && <img src={banner.imageUrl} alt={title ? '' : 'پیشنهاد فروشگاه'} width="640" height="320"
              loading="lazy" decoding="async" onError={event => { event.currentTarget.hidden = true; }} />}
            {title && <span className="central-promo-caption">
              <span><strong>{title}</strong>{banner.subtitle && <small>{banner.subtitle}</small>}</span>
              <ArrowLeft size={17} aria-hidden="true" />
            </span>}
          </a>
        );
      })}
    </section>
  );
}

export const HomeFeedSections: React.FC = () => {
  const { products, stores, openStorefront } = useStore();
  const [sections, setSections] = useState<HomepageSection[]>([]);

  useEffect(() => {
    let active = true;
    homepageApi.getSections().then(items => {
      if (active) setSections(items.filter(section => section.isActive).sort((a, b) => a.sortOrder - b.sortOrder));
    }).catch(() => {});
    return () => { active = false; };
  }, []);

  return sections.map(section => {
    const config = section.config || {};
    switch (section.sectionType) {
      case 'trust_features': return <TrustFeatures key={section.id} />;
      case 'product_carousel': {
        const items = products.filter(product => !config.categorySlug || product.category === config.categorySlug);
        return <ProductCarouselSection key={section.id} id={section.id} title={section.title || 'پیشنهادها'} categorySlug={config.categorySlug} products={items} />;
      }
      case 'product_grid': return <ProductGridSection key={section.id} />;
      case 'store_carousel': {
        const active = stores.filter(store => store.ownerType === 'dropshipper' && store.status !== 'suspended' && store.status !== 'inactive');
        if (!active.length) return null;
        return (
          <section key={section.id} className="central-store-section">
            <div className="central-section-title"><h2>{section.title || 'ویترین فروشندگان'}</h2></div>
            <div className="central-store-rail">
              {active.map(store => (
                <a key={store.id} href={pathForView('seller_storefront', { storeSlug: store.slug })}
                  onClick={event => followStoreLink(event, () => openStorefront(store.slug))}>
                  {store.logo ? <img src={store.logo} alt="" width="40" height="40" loading="lazy" /> : <Store size={22} aria-hidden="true" />}
                  <strong>{store.name}</strong>
                  {store.tagline && <span>{store.tagline}</span>}
                </a>
              ))}
            </div>
          </section>
        );
      }
      case 'promo_banner': return (config.index || 0) === 0 ? <BannerTiles key={section.id} id={`promo-${section.id}`} position="promo" /> : null;
      case 'tile_grid': return (config.index || 0) === 0 ? <BannerTiles key={section.id} id={`tile-${section.id}`} position="tile" /> : null;
      case 'secondary_slider': return <HeroSlider key={section.id} position="secondary" />;
      default: return null;
    }
  });
};
