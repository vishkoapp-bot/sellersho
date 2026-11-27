import { useEffect, useState, type ReactNode } from 'react';
import { ArrowLeft, ChevronLeft, ChevronRight, Pause, Play, Search } from 'lucide-react';
import type { Product, Store } from '../../../types';
import { toPersianDigits } from '../../../utils/formatters';
import { FallbackImage } from '../../common/FallbackImage';
import { followStoreSection } from '../../../utils/followStoreLink';
import { customText, heroSlidesOf, orderedEnabledSections, templateCustom, type TemplateConfig } from './templateTypes';

export function StorefrontHero({ store, config, products, aside, showSearchShortcut = false }: {
  store: Store;
  config: TemplateConfig;
  products: Product[];
  aside?: ReactNode;
  showSearchShortcut?: boolean;
}) {
  const custom = templateCustom(config);
  const sections = orderedEnabledSections(config);
  const slides = heroSlidesOf(config);
  const [index, setIndex] = useState(0);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [stopped, setStopped] = useState(false);
  const [reduced, setReduced] = useState(false);
  const count = slides.length;
  const active = count > 0 ? index % count : 0;
  const slide = slides[active];
  useEffect(() => {
    const preference = matchMedia('(prefers-reduced-motion: reduce)');
    const sync = () => setReduced(preference.matches);
    sync();
    preference.addEventListener('change', sync);
    return () => preference.removeEventListener('change', sync);
  }, []);
  useEffect(() => {
    if (!custom.heroAutoplay || count < 2 || reduced || stopped || hovered || focused) return;
    const timer = window.setInterval(() => setIndex(i => (i + 1) % count), 6000);
    return () => window.clearInterval(timer);
  }, [count, custom.heroAutoplay, focused, hovered, reduced, stopped]);

  const title = slide?.title || customText(config, 'heroTitle', store.name);
  const subtitle = slide?.subtitle || custom.heroSubtitle || custom.heroFallbackSubtitle || store.shortDescription || store.tagline || store.description;
  const catalogEnabled = sections.includes('products') || sections.includes('allProducts');
  const href = slide?.link || custom.heroCtaLink || (catalogEnabled ? '#products' : '');
  const image = slide?.image || custom.heroImage;
  const samples = image ? [] : [...new Set(products.map(product => product.image || product.images?.[0]).filter(Boolean))].slice(0, 3);
  const media = image ? [image] : samples;
  return (
    <section className="showcase-hero" data-testid="store-hero-slider" data-media={media.length > 0} data-custom-image={Boolean(image)}
      onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      onFocusCapture={() => setFocused(true)} onBlurCapture={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}
      aria-label={count > 1 ? 'بنرهای فروشگاه' : undefined}>
      <div className="showcase-hero-copy">
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
        <div className="showcase-hero-actions">
          {href && <a href={href} className="showcase-hero-cta" onClick={showSearchShortcut && href.startsWith('#') ? followStoreSection : undefined}>{custom.heroCtaText || 'مشاهده کالاها'}<ArrowLeft size={19} aria-hidden="true" /></a>}
          {showSearchShortcut && catalogEnabled && <a href="#fashion-search" className="modern-text-link" onClick={followStoreSection}>
            <Search size={18} aria-hidden="true" /> جست‌وجوی کالا
          </a>}
        </div>
        {count > 1 && <div className="showcase-hero-controls">
          <button type="button" aria-label="اسلاید قبلی" onClick={() => setIndex((active - 1 + count) % count)}><ChevronRight size={18} aria-hidden="true" /></button>
          <span aria-live={stopped || focused || hovered ? 'polite' : 'off'}>{toPersianDigits(active + 1)} از {toPersianDigits(count)}</span>
          <button type="button" aria-label="اسلاید بعدی" onClick={() => setIndex((active + 1) % count)}><ChevronLeft size={18} aria-hidden="true" /></button>
          {custom.heroAutoplay && !reduced && <button type="button" aria-label={stopped ? 'پخش خودکار اسلایدها' : 'توقف پخش خودکار'} onClick={() => setStopped(value => !value)}>
            {stopped ? <Play size={16} aria-hidden="true" /> : <Pause size={16} aria-hidden="true" />}
          </button>}
        </div>}
      </div>
      {media.length > 0 && <div className="showcase-hero-media" data-count={media.length} aria-hidden="true">
        {media.map((src, position) => <FallbackImage key={src} srcs={[src]} alt="" width={720} height={540}
          fetchPriority={position === 0 ? 'high' : 'auto'} loading={position === 0 ? 'eager' : 'lazy'} fallback={null} />)}
      </div>}
      {aside}
    </section>
  );
}
