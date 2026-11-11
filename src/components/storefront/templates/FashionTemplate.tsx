import { ArrowLeft, ChevronLeft, ChevronRight, ImageOff } from 'lucide-react';
import './fashion.css';
import React, { useEffect, useMemo, useState } from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  heroSlidesOf,
  bannersOf,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
  sectionEnabled,
  sectionKey,
} from './templateTypes';
import type { TemplateBanner, TemplateConfig, TemplateHeroSlide } from './templateTypes';
import type { Product, Store } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { formatPrice, toPersianDigits } from '../../../utils/formatters';
import { cardPriceOf } from '../../../utils/cardPrice';
import { apiRequest } from '../../../services/apiClient';
import { groupProductsByCategory } from './StoreSections';
import type { CategoryGroup } from './StoreSections';

const CONTAINER = 'mx-auto w-full max-w-[1440px] px-4 sm:px-6 lg:px-10';

const FOCUS_RING =
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--tpl-accent)]';

const externalLinkProps = (link: string): { target?: string; rel?: string } =>
  /^https?:\/\//i.test((link || '').trim()) ? { target: '_blank', rel: 'noreferrer' } : {};

const Wrap: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className={`${CONTAINER} empty:hidden`}>{children}</div>
);

const FashionSectionHeader: React.FC<{ title: string }> = ({ title }) => (
  <h2 className="text-right text-xl font-bold leading-[1.5] tracking-normal text-[#1c1c1a] lg:text-[28px]">
    {title}
  </h2>
);

const TileLabel: React.FC<{ title: string; sub?: string }> = ({ title, sub }) => (
  <div className="mt-2.5 text-center">
    <p className="text-sm font-bold leading-6 text-[#1c1c1a]">{title}</p>
    {sub ? <p className="mt-0.5 text-[11px] leading-5 text-[#737373]">{sub}</p> : null}
  </div>
);

const brandOf = (p: Product): string => (p.brand || '').trim();

interface MediaLedger {
  used: Set<string>;
  macroTaken: Set<string>;
}

const peekUnusedImage = (ledger: MediaLedger, candidates: (string | undefined)[]): string => {
  for (const c of candidates) {
    const src = (c || '').trim();
    if (src && !ledger.used.has(src)) return src;
  }
  return '';
};

const claimImage = (ledger: MediaLedger, src: string): void => {
  if (src) ledger.used.add(src);
};

const tileLayout = (count: number, fullGrid: string): { grid: string; aspect: string } => {
  if (count <= 1) return { grid: 'grid-cols-1', aspect: 'aspect-[16/10] lg:aspect-[21/9]' };
  if (count === 2) return { grid: 'grid-cols-2', aspect: 'aspect-square' };
  return { grid: fullGrid, aspect: 'aspect-square' };
};

const FashionHero: React.FC<{
  slides: TemplateHeroSlide[];
  autoplay?: boolean;
  title: string;
  subtitle: string;
  ctaText: string;
  ctaLink: string;
  label: string;
}> = ({ slides, autoplay, title, subtitle, ctaText, ctaLink, label }) => {
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  const count = slides.length;
  useEffect(() => { setIndex(0); }, [count]);
  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReducedMotion(mq.matches);
    const change = () => setReducedMotion(mq.matches);
    mq.addEventListener('change', change);
    return () => mq.removeEventListener('change', change);
  }, []);
  useEffect(() => {
    if (!autoplay || count < 2 || paused || reducedMotion) return;
    const timer = window.setInterval(() => setIndex(i => (i + 1) % count), 6000);
    return () => window.clearInterval(timer);
  }, [autoplay, count, paused, reducedMotion]);
  const active = slides[index % count] || {};
  const heading = active.title || title;
  const link = active.link || ctaLink;
  return (
    <section data-testid="store-hero-slider" className={`fashion-hero ${slides[index % count]?.image ? '' : 'fashion-hero-text-only'}`}
      aria-roledescription="نمایش اسلاید" aria-label="کالکشن فروشگاه"
      onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setPaused(true)}
      onBlurCapture={e => { if (!e.currentTarget.contains(e.relatedTarget)) setPaused(false); }}>
      <div className="fashion-hero-media">
        {active.image ? <img key={active.image} src={active.image} alt={heading} fetchPriority="high" className="fashion-hero-photo" /> : null}
        {count > 1 && <div className="fashion-slide-controls">
          <button type="button" aria-label="اسلاید قبلی" onClick={() => setIndex(i => (i - 1 + count) % count)}><ChevronRight size={18} /></button>
          <span aria-live={paused ? 'polite' : 'off'}>{toPersianDigits(index % count + 1)} / {toPersianDigits(count)}</span>
          <button type="button" aria-label="اسلاید بعدی" onClick={() => setIndex(i => (i + 1) % count)}><ChevronLeft size={18} /></button>
        </div>}
      </div>
      <div className="fashion-hero-copy">
        <span className="fashion-eyebrow">{label}</span>
        <h1>{heading}</h1>
        {(active.subtitle || subtitle) && <p>{active.subtitle || subtitle}</p>}
        <a href={link} {...externalLinkProps(link)} className={`fashion-button ${FOCUS_RING}`}>{ctaText}<ArrowLeft size={18} strokeWidth={1.5} /></a>
      </div>
    </section>
  );
};

const FashionMacroCategories: React.FC<{
  groups: CategoryGroup[];
  ledger: MediaLedger;
  banners: TemplateBanner[];
  limit: number;
  onSelect: (slug: string) => void;
}> = ({ groups, ledger, banners, limit, onSelect }) => {
  const entries = groups.length ? groups.slice(0, limit).map((group, i) => ({
    title: group.title, image: banners[i]?.image || group.image || group.products[0]?.image,
    link: banners[i]?.link || '#products', slug: !banners[i]?.link || banners[i]?.link === '#products' ? group.slug : '',
  })) : banners.slice(0, limit).map(b => ({ title: b.title || 'مشاهده مجموعه', image: b.image, link: b.link || '#products', slug: '' }));
  groups.slice(0, limit).forEach(g => ledger.macroTaken.add(g.slug));
  if (!entries.length) return null;
  return <section id="fashion-categories" data-testid="store-category-macro" aria-label="دسته‌بندی‌ها" className="fashion-category-section">
    <div className="fashion-category-grid" style={{ gridTemplateColumns: `repeat(${entries.length}, minmax(0, 1fr))` }}>
      {entries.map((entry, i) => <a key={entry.slug || i} href={entry.link} {...externalLinkProps(entry.link)}
        onClick={() => { if (entry.slug) onSelect(entry.slug); }} className={`fashion-category ${FOCUS_RING}`}>
        <div className="fashion-category-image">
          {entry.image && (entry.image === '/templates/fashion/categories.webp'
            ? <div role="img" aria-label={entry.title} className="fashion-category-contact" style={{ backgroundImage: `url(${entry.image})`, backgroundPosition: `${i * 50}% center` }} />
            : <img src={entry.image} alt={entry.title} loading="lazy" />)}
        </div>
        <span>{entry.title}<ArrowLeft size={16} strokeWidth={1.5} /></span>
      </a>)}
    </div>
    {groups.length > limit && <div className="fashion-category-links">{groups.slice(limit).map(g => <a key={g.slug} href="#products" onClick={() => onSelect(g.slug)}>{g.title}</a>)}</div>}
  </section>;
};

const FashionStory: React.FC<{ store: Store; config: TemplateConfig }> = ({ store, config }) => {
  if (!sectionEnabled(config, 'about')) return null;
  const custom = templateCustom(config);
  const text = (
    (custom.aboutText || '').trim() ||
    (store.description || store.shortDescription || '').trim()
  );
  if (!text) return null;
  const paragraphs = text.split(/\n+/).map((p) => p.trim()).filter(Boolean);
  const image = (custom.aboutImage || '').trim();
  const title = customText(config, 'aboutTitle', 'درباره فروشگاه');
  if (!image && paragraphs.length === 1 && text.length <= 140) {
    return (
      <section data-testid="store-about" className="max-w-[560px]">
        <FashionSectionHeader title={title} />
        <p className="mt-3 text-[15px] leading-8 text-[#737373]">{paragraphs[0]}</p>
      </section>
    );
  }
  const prose = (
    <div className="max-w-[560px]">
      <FashionSectionHeader title={title} />
      <div className="mt-5 space-y-4">
        {paragraphs.map((p, i) => (
          <p key={i} className="text-[15px] leading-8 text-[#737373]">
            {p}
          </p>
        ))}
      </div>
    </div>
  );
  return (
    <section data-testid="store-about">
      {image ? (
        <div className="grid items-center gap-8 lg:grid-cols-2 lg:gap-16">
          {prose}
          <div className="overflow-hidden rounded-[2px] bg-[#f2f2f2]">
            <img src={image} alt={title} loading="lazy" className="aspect-square w-full object-cover" />
          </div>
        </div>
      ) : (
        <div className="max-w-3xl">{prose}</div>
      )}
    </section>
  );
};

const FashionCategoryGrid: React.FC<{
  groups: CategoryGroup[];
  title: string;
  ledger: MediaLedger;
  onSelect: (slug: string) => void;
}> = ({ groups, title, ledger, onSelect }) => {
  const resolved: { group: CategoryGroup; image: string }[] = [];
  for (const group of groups) {
    const image = peekUnusedImage(ledger, [group.image, ...group.products.map((p) => p.image)]);
    if (image) {
      claimImage(ledger, image);
      resolved.push({ group, image });
    }
  }
  if (resolved.length === 0) return null;
  const layout = tileLayout(resolved.length, 'grid-cols-2 lg:grid-cols-3');
  return (
    <section data-testid="store-category-cards" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      <div className={`grid gap-3 lg:gap-4 ${layout.grid}`}>
        {resolved.map(({ group, image }) => (
          <a
            key={group.slug}
            href="#products"
            onClick={() => onSelect(group.slug)}
            className={`group block ${FOCUS_RING}`}
          >
            <div className={`relative overflow-hidden rounded-[2px] bg-[#f2f2f2] ${layout.aspect}`}>
              <img
                src={image}
                alt={group.title}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
              />
            </div>
            <TileLabel title={group.title} sub={`${toPersianDigits(group.count)} کالا`} />
          </a>
        ))}
      </div>
    </section>
  );
};

const FashionCollections: React.FC<{
  groups: CategoryGroup[];
  title: string;
  ledger: MediaLedger;
}> = ({ groups, title, ledger }) => {
  const cards: { group: CategoryGroup; images: { src: string; title: string }[] }[] = [];
  for (const group of groups) {
    if (cards.length >= 6) break;
    const images: { src: string; title: string }[] = [];
    const seenInCard = new Set<string>();
    for (const p of group.products) {
      if (images.length >= 4) break;
      const src = (p.image || '').trim();
      if (src && !seenInCard.has(src) && !ledger.used.has(src)) {
        seenInCard.add(src);
        images.push({ src, title: p.title });
      }
    }
    if (images.length >= 2) {
      images.forEach((im) => claimImage(ledger, im.src));
      cards.push({ group, images });
    }
  }
  if (cards.length === 0) return null;
  const renderCard = (card: (typeof cards)[number], widthCls: string): React.ReactNode => (
    <div key={card.group.slug} className={widthCls}>
      <div className="grid grid-cols-2 gap-1 overflow-hidden rounded-[2px] bg-white">
        {card.images.map((im, i) => (
          <div
            key={`${im.src}-${i}`}
            className={`overflow-hidden bg-[#f2f2f2] ${card.images.length === 3 && i === 0 ? 'col-span-2 aspect-[16/10]' : 'aspect-square'}`}
          >
            <img src={im.src} alt={im.title} loading="lazy" className="h-full w-full object-cover" />
          </div>
        ))}
      </div>
      <TileLabel title={card.group.title} sub={`${toPersianDigits(card.group.count)} کالا`} />
    </div>
  );
  return (
    <section data-testid="store-collections" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      {cards.length === 1 ? (
        <div className="mx-auto w-full max-w-[680px]">{renderCard(cards[0], 'w-full')}</div>
      ) : (
        <div className="flex snap-x gap-4 overflow-x-auto pb-2 no-scrollbar">
          {cards.map((card) => renderCard(card, 'w-[270px] shrink-0 snap-start lg:w-[350px]'))}
        </div>
      )}
    </section>
  );
};

const FashionLookbookBand: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  if (!sectionEnabled(config, 'lookbook')) return null;
  const custom = templateCustom(config);
  const title = custom.lookbookTitle?.trim();
  const image = custom.lookbookImage?.trim();
  const link = custom.lookbookLink?.trim() || '#products';
  if (!title && !image) return null;
  return <section data-testid="store-lookbook" className="fashion-lookbook">
    {image && <img src={image} alt="" loading="lazy" />}
    <div><h2>{title}</h2>{custom.lookbookSubtitle && <p>{custom.lookbookSubtitle}</p>}
      <a href={link} {...externalLinkProps(link)} className={`fashion-button ${FOCUS_RING}`}>مشاهده مجموعه<ArrowLeft size={16} /></a>
    </div>
  </section>;
};

const FashionBrandTiles: React.FC<{
  products: Product[];
  title: string;
  ledger: MediaLedger;
  onSelect: (brand: string) => void;
}> = ({ products, title, ledger, onSelect }) => {
  const brands = useMemo(() => {
    const byBrand = new Map<string, { products: Product[]; count: number }>();
    products.forEach((p) => {
      const name = brandOf(p);
      if (!name) return;
      const entry = byBrand.get(name);
      if (entry) {
        entry.count += 1;
        entry.products.push(p);
      } else {
        byBrand.set(name, { products: [p], count: 1 });
      }
    });
    return Array.from(byBrand.entries()).map(([name, v]) => ({ name, products: v.products, count: v.count }));
  }, [products]);
  if (brands.length < 2) return null;
  const peeked = brands.map((b) => ({ ...b, image: peekUnusedImage(ledger, b.products.map((p) => p.image)) }));
  const tiled = peeked.filter((b) => b.image !== '').slice(0, 4);
  tiled.forEach((b) => claimImage(ledger, b.image));
  const tiledNames = new Set(tiled.map((b) => b.name));
  const chipped = peeked.filter((b) => !tiledNames.has(b.name));
  const gridCls =
    tiled.length <= 1 ? 'grid-cols-1 max-w-[300px]' : tiled.length === 2 ? 'grid-cols-2' : tiled.length === 3 ? 'grid-cols-2 lg:grid-cols-3' : 'grid-cols-2 lg:grid-cols-4';
  return (
    <section data-testid="store-brands" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      {tiled.length > 0 ? (
        <div className={`grid gap-3 lg:gap-4 ${gridCls}`}>
          {tiled.map((brand) => (
            <a
              key={brand.name}
              href="#products"
              onClick={() => onSelect(brand.name)}
              className={`group block ${FOCUS_RING}`}
            >
              <div className="relative aspect-[3/4] overflow-hidden rounded-[2px] bg-[#f2f2f2]">
                <img
                  src={brand.image}
                  alt={brand.name}
                  loading="lazy"
                  className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
                />
              </div>
              <TileLabel title={brand.name} />
            </a>
          ))}
        </div>
      ) : null}
      {chipped.length > 0 ? (
        <div className="flex flex-wrap justify-center gap-2">
          {chipped.map((brand) => (
            <a
              key={brand.name}
              href="#products"
              onClick={() => onSelect(brand.name)}
              className={`inline-flex min-h-[44px] items-center rounded-full border border-[#e5e0d8] bg-white px-4 text-xs font-bold text-[#1c1c1a] transition hover:border-[#1c1c1a] ${FOCUS_RING}`}
            >
              {brand.name}
              <span className="ms-2 text-[11px] font-bold text-[#737373]">{toPersianDigits(brand.count)} کالا</span>
            </a>
          ))}
        </div>
      ) : null}
    </section>
  );
};

const FashionBanners: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => {
  if (banners.length === 0) return null;
  const single = banners.length === 1;
  const oddLast = !single && banners.length % 2 === 1;
  return (
    <section data-testid="store-banners" className={`grid gap-3 lg:gap-4 ${single ? 'grid-cols-1' : 'grid-cols-1 sm:grid-cols-2'}`}>
      {banners.map((b, i) => {
        const isOddLast = oddLast && i === banners.length - 1;
        const aspect = single
          ? 'aspect-[16/10] lg:aspect-[21/9]'
          : isOddLast
            ? 'aspect-[4/3] sm:aspect-[21/9]'
            : 'aspect-[4/3]';
        const tile = (
          <>
            <div className={`group relative overflow-hidden rounded-[2px] bg-[#f2f2f2] ${aspect}`}>
              <img
                src={b.image}
                alt={b.title || ''}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
              />
            </div>
            {b.title ? <TileLabel title={b.title} /> : null}
          </>
        );
        const spanCls = isOddLast ? 'sm:col-span-2' : '';
        return b.link ? (
          <a key={`${b.image}-${i}`} href={b.link} {...externalLinkProps(b.link)} className={`block ${spanCls} ${FOCUS_RING}`}>
            {tile}
          </a>
        ) : (
          <div key={`${b.image}-${i}`} className={spanCls}>{tile}</div>
        );
      })}
    </section>
  );
};

const FashionSpotlight: React.FC<{
  products: Product[];
  title: string;
  onOpen: (p: Product) => void;
}> = ({ products, title, onOpen }) => {
  if (products.length === 0) return null;
  const four = products.length === 4;
  return (
    <section data-testid="store-featured" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      <div className={`grid gap-3 lg:gap-4 ${four ? 'grid-cols-2' : 'grid-cols-2 lg:grid-cols-4'}`}>
        {products.map((p, i) => (
          <button
            key={p.id}
            type="button"
            onClick={() => onOpen(p)}
            className={`group relative block w-full cursor-pointer overflow-hidden rounded-[2px] bg-[#f2f2f2] text-right ${FOCUS_RING} ${
              four
                ? 'aspect-square'
                : i === 0
                  ? 'col-span-2 aspect-[4/3] lg:row-span-2 lg:aspect-auto lg:h-full'
                  : 'aspect-square'
            }`}
          >
            <img
              src={p.image}
              alt={p.title}
              loading="lazy"
              className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
            />
          </button>
        ))}
      </div>
    </section>
  );
};

const FashionTestimonials: React.FC<{ config: TemplateConfig; title: string }> = ({ config, title }) => {
  if (!sectionEnabled(config, 'testimonials')) return null;
  const items = (templateCustom(config).testimonials || []).filter(
    (t) => t && typeof t.text === 'string' && t.text.trim() !== '',
  );
  if (items.length === 0) return null;
  return (
    <section data-testid="store-testimonials" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3 lg:gap-4">
        {items.map((t, i) => (
          <figure key={`${t.name || ''}-${i}`} className="rounded-[2px] border border-[#e5e0d8] bg-white p-6">
            <span className="text-4xl font-black leading-none text-[#737373]">«</span>
            <blockquote className="mt-3 text-sm leading-8 text-[#737373]">{t.text}</blockquote>
            {t.name ? <figcaption className="mt-4 text-sm font-bold text-[#1c1c1a]">{t.name}</figcaption> : null}
          </figure>
        ))}
      </div>
    </section>
  );
};

const FashionFaq: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  const configItems = (templateCustom(config).faqItems || []).filter(
    (f) => f && typeof f.q === 'string' && f.q.trim() !== '' && typeof f.a === 'string' && f.a.trim() !== '',
  );
  const [faqs, setFaqs] = useState<{ q: string; a: string }[]>([]);
  useEffect(() => {
    if (configItems.length > 0) return undefined;
    let on = true;
    apiRequest<{ pages: { key: string; content: string }[] }>('/static-pages')
      .then((res) => {
        if (!on) return;
        const page = (res.pages || []).find((p) => p.key === 'faq');
        if (!page) return;
        try {
          const parsed = JSON.parse(page.content);
          if (Array.isArray(parsed)) setFaqs(parsed.filter((x) => x && x.q && x.a));
        } catch { /* non-JSON faq content renders nothing */ }
      })
      .catch(() => {});
    return () => { on = false; };
  }, [config]);
  const items = configItems.length > 0 ? configItems : faqs;
  if (!sectionEnabled(config, 'faq') || items.length === 0) return null;
  return (
    <section data-testid="store-faq" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title="پرسش‌های متداول" />
      <div className="divide-y divide-[#e5e0d8] border-y border-[#e5e0d8]">
        {items.map((f) => (
          <div key={f.q} className="py-5">
            <p className="text-[15px] font-bold leading-7 text-[#1c1c1a]">{f.q}</p>
            <p className="mt-2 text-sm leading-8 text-[#737373]">{f.a}</p>
          </div>
        ))}
      </div>
    </section>
  );
};

const FashionProductCard: React.FC<{ product: Product; onOpen: (p: Product) => void }> = ({ product, onOpen }) => {
  const price = cardPriceOf(product);
  const [previewRequested, setPreviewRequested] = useState(false);
  const [readyPreview, setReadyPreview] = useState('');
  const [failedSources, setFailedSources] = useState<string[]>([]);
  const availableImages = Array.from(new Set([product.image, ...(product.images || [])]))
    .filter((src): src is string => Boolean(src) && !failedSources.includes(src));
  const [primaryImage, secondImage] = availableImages;
  const secondaryReady = Boolean(previewRequested && secondImage && readyPreview === secondImage);
  const markFailed = (src: string) => setFailedSources(previous => previous.includes(src) ? previous : [...previous, src]);

  return <button type="button" onClick={() => onOpen(product)}
    onPointerEnter={event => { if (event.pointerType === 'mouse') setPreviewRequested(true); }}
    onFocus={() => setPreviewRequested(true)}
    className={`fashion-product group ${FOCUS_RING}`} aria-label={`مشاهده ${product.title}`}>
    <div className="fashion-product-image" data-secondary-ready={secondaryReady}>
      {primaryImage ? <img src={primaryImage} alt={product.title} loading="lazy" decoding="async"
        onError={() => markFailed(primaryImage)} className={secondaryReady ? 'fashion-primary-image' : ''} />
        : <div className="fashion-no-image" role="img" aria-label="تصویر محصول موجود نیست"><ImageOff size={28} strokeWidth={1} /><span>تصویر محصول موجود نیست</span></div>}
      {previewRequested && secondImage && <img src={secondImage} alt="" loading="lazy" decoding="async"
        onLoad={() => setReadyPreview(secondImage)} onError={() => markFailed(secondImage)} className="fashion-secondary-image" />}
      {price.discountPercent > 0 && <span className="fashion-discount">٪{toPersianDigits(price.discountPercent)}</span>}
      <span className="fashion-product-quick">مشاهده محصول<ArrowLeft size={16} /></span>
    </div>
    <div className="fashion-product-info">
      {product.brand && <span className="fashion-product-brand">{product.brand}</span>}
      <h3>{product.title}</h3>
      <div className="fashion-price"><span>{formatPrice(price.price)} <small>تومان</small></span>
        {price.discountPercent > 0 && price.originalPrice > price.price && <del>{formatPrice(price.originalPrice)}</del>}
      </div>
      {(product.variants?.length || 0) > 1 && <span className="fashion-variants">{toPersianDigits(product.variants!.length)} تنوع</span>}
    </div>
  </button>;
};

const FashionNewsletter: React.FC<{ config: TemplateConfig; storeSlug?: string }> = ({ config, storeSlug }) => {
  const [mobile, setMobile] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    setMobile('');
    setDone(false);
    setError('');
  }, [storeSlug]);
  if (!sectionEnabled(config, 'newsletter')) return null;
  const title = customText(config, 'newsletterTitle', 'عضویت در اطلاع‌رسانی');
  const subtitle = (templateCustom(config).newsletterSubtitle || '').trim();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!/^09\d{9}$/.test(mobile.trim())) {
      setError('شماره موبایل معتبر وارد کنید.');
      return;
    }
    setError('');
    try {
      await apiRequest('/newsletter/subscribe', {
        method: 'POST',
        body: JSON.stringify(storeSlug ? { mobile: mobile.trim(), storeSlug } : { mobile: mobile.trim() }),
      });
      setDone(true);
    } catch {
      setError('ثبت انجام نشد؛ دوباره تلاش کنید.');
    }
  };
  return (
    <section data-testid="store-newsletter" className="w-full bg-[#3a4441] text-white">
      <div className="mx-auto w-full max-w-[1080px] px-4 py-14 lg:px-10 lg:py-[72px]">
        <div className="flex flex-col gap-8 lg:flex-row lg:items-center lg:justify-between">
          <div className="max-w-xl">
            <h2 className="text-[30px] font-extrabold leading-[1.4] tracking-normal lg:text-[40px]">{title}</h2>
            {subtitle ? <p className="mt-2 text-sm leading-7 text-white/70">{subtitle}</p> : null}
          </div>
          <div className="w-full max-w-md">
            {done ? (
              <p className="text-sm font-bold text-emerald-300">شماره شما ثبت شد.</p>
            ) : (
              <form onSubmit={submit} className="flex items-end gap-3">
                <input
                  type="tel"
                  dir="ltr"
                  value={mobile}
                  onChange={(e) => setMobile(e.target.value)}
                  placeholder="09xxxxxxxxx"
                  className="h-12 min-w-0 flex-1 border-0 border-b border-white/40 bg-transparent text-left text-sm text-white outline-none transition placeholder:text-white/40 focus:border-white"
                />
                <button
                  type="submit"
                  className={`h-12 shrink-0 rounded-none bg-[#faf8f5] px-6 text-sm font-bold text-[#3a4441] transition hover:bg-white ${FOCUS_RING}`}
                >
                  عضویت
                </button>
              </form>
            )}
            {error !== '' && <p className="mt-2 text-xs font-bold text-rose-300">{error}</p>}
          </div>
        </div>
      </div>
    </section>
  );
};

export const FashionTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-fashion-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const configuredSlides = heroSlidesOf(cfg);
  const slides = configuredSlides.length ? configuredSlides : [{ image: templateCustom(cfg).heroImage, title: templateCustom(cfg).heroTitle, subtitle: templateCustom(cfg).heroSubtitle }];
  const banners = bannersOf(cfg);
  const custom = templateCustom(cfg);
  const {
    openProductDetail,
    selectedCategory,
    setSelectedCategory,
    openCatalogBrand,
    catalogBrandRequest,
    clearCatalogBrand,
    searchQuery,
    setSearchQuery,
  } = useStore();
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };
  const ledger: MediaLedger = { used: new Set<string>(), macroTaken: new Set<string>() };

  const heroTitle = customText(cfg, 'heroTitle', store.name);
  const heroSubtitle = customText(cfg, 'heroSubtitle', store.tagline || '');
  const heroCtaText = customText(cfg, 'heroCtaText', 'مشاهده کالکشن');
  const heroCtaLink = custom.heroCtaLink || '#products';
  const productsTitle = customText(cfg, 'productsTitle', 'همه کالاها');
  const testimonialsTitle =
    (Array.isArray(cfg.sections)
      ? cfg.sections.find((s) => sectionKey(s) === 'testimonials')?.label
      : undefined) || 'نظرات مشتریان';

  const categoryGroups = useMemo(
    () => groupProductsByCategory(items, categories, custom.categoryOverrides),
    [items, categories, cfg],
  );
  const featuredList = useMemo(
    () => items.filter((p) => p.featured).slice(0, customCount(cfg, 'featuredCount', 10)),
    [items, cfg],
  );

  const selectedGroup = categoryGroups.find((g) => g.slug === selectedCategory);
  const brandFilter =
    catalogBrandRequest && items.some((p) => brandOf(p) === catalogBrandRequest.brand)
      ? catalogBrandRequest.brand
      : null;
  const categoryItems = brandFilter
    ? items.filter((p) => brandOf(p) === brandFilter)
    : selectedGroup
      ? items.filter((p) => p.category === selectedGroup.slug)
      : items;
  const query = searchQuery.trim().toLocaleLowerCase('fa-IR');
  const searching = query.length > 0;
  const floorItems = searching
    ? categoryItems.filter((p) => p.title.toLocaleLowerCase('fa-IR').includes(query) || (p.description || '').toLocaleLowerCase('fa-IR').includes(query))
    : categoryItems;
  const floorTitle = searching ? `جستجو: «${searchQuery}»` : brandFilter || (selectedGroup ? selectedGroup.title : productsTitle);
  const selectCategory = (slug: string): void => {
    clearCatalogBrand();
    setSearchQuery('');
    setSelectedCategory(slug);
  };
  const clearFloorFilter = (): void => {
    if (brandFilter) clearCatalogBrand();
    if (selectedGroup) setSelectedCategory('all');
    if (searching) setSearchQuery('');
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return null; // The fashion header owns the announcement above navigation.
      case 'hero':
      case 'heroSlides':
        if (!once('hero')) return null;
        slides.forEach((s) => claimImage(ledger, (s.image || '').trim()));
        return (
          <FashionHero
            slides={slides}
            autoplay={custom.heroAutoplay}
            label={store.name}
            title={heroTitle}
            subtitle={heroSubtitle}
            ctaText={heroCtaText}
            ctaLink={heroCtaLink}
          />
        );
      case 'categoryRail':
        return (
          <Wrap>
            <FashionMacroCategories groups={categoryGroups} ledger={ledger} onSelect={selectCategory} banners={banners} limit={Math.min(customCount(cfg, 'categoriesCount', 3), 3)} />
          </Wrap>
        );
      case 'about': {
        claimImage(ledger, (custom.aboutImage || '').trim());
        return (
          <Wrap>
            <FashionStory store={store} config={cfg} />
          </Wrap>
        );
      }
      case 'categoryGrid': {
        const remainder = categoryGroups.filter((g) => !ledger.macroTaken.has(g.slug));
        let limit = Math.min(remainder.length, customCount(cfg, 'categoriesCount', 6));
        if (limit > 6) limit = 6;
        if (limit === 4 || limit === 5) limit = 3;
        return (
          <Wrap>
            <FashionCategoryGrid
              groups={remainder.slice(0, limit)}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')}
              ledger={ledger}
              onSelect={selectCategory}
            />
          </Wrap>
        );
      }
      case 'collections':
        return (
          <Wrap>
            <FashionCollections
              groups={categoryGroups}
              title={customText(cfg, 'collectionsTitle', 'مجموعه‌ها')}
              ledger={ledger}
            />
          </Wrap>
        );
      case 'lookbook': {
        claimImage(ledger, (custom.lookbookImage || '').trim());
        return (
          <Wrap>
            <FashionLookbookBand config={cfg} />
          </Wrap>
        );
      }
      case 'brands':
        return (
          <Wrap>
            <FashionBrandTiles
              products={items}
              title={customText(cfg, 'brandsTitle', 'برندها')}
              ledger={ledger}
              onSelect={openCatalogBrand}
            />
          </Wrap>
        );
      case 'banners':
      case 'promo':
        if (!once('banners')) return null;
        banners.slice(sectionEnabled(cfg, 'categoryRail') ? 3 : 0).forEach((b) => claimImage(ledger, (b.image || '').trim()));
        return (
          <Wrap>
            <FashionBanners banners={sectionEnabled(cfg, 'categoryRail') ? banners.slice(3) : banners} />
          </Wrap>
        );
      case 'featured': {
        const picks = featuredList
          .filter((p) => {
            const src = (p.image || '').trim();
            return src !== '' && !ledger.used.has(src);
          })
          .slice(0, 5);
        picks.forEach((p) => claimImage(ledger, (p.image || '').trim()));
        return (
          <Wrap>
            <FashionSpotlight products={picks} title={customText(cfg, 'featuredTitle', 'کالاهای منتخب')} onOpen={openProductDetail} />
          </Wrap>
        );
      }
      case 'deals':
      case 'timedDeals':
      case 'discounted':
      case 'campaign':
      case 'topRated':
      case 'bestsellers':
      case 'trustStrip':
        return null;
      case 'faq':
        return (
          <Wrap>
            <FashionFaq config={cfg} />
          </Wrap>
        );
      case 'testimonials':
        return (
          <Wrap>
            <FashionTestimonials config={cfg} title={testimonialsTitle} />
          </Wrap>
        );
      case 'products':
      case 'allProducts': {
        if (!once('products')) return null;
        if (floorItems.length === 0 && !searching && !selectedGroup && !brandFilter) return null;
        const count = floorItems.length;
        const gridCls =
          count === 1 ? 'grid-cols-1 max-w-sm mx-auto' : count === 2 ? 'grid-cols-2 max-w-2xl mx-auto' : 'grid-cols-2 md:grid-cols-3 lg:grid-cols-4';
        const wrapCls = count === 1 ? 'mx-auto w-full max-w-[380px]' : count === 2 ? 'mx-auto w-full max-w-[880px]' : '';
        return (
          <Wrap>
            <section id="products" data-testid="store-products" className="fashion-catalogue space-y-6 lg:space-y-8">
              <div className="fashion-catalogue-heading">
                <div className="fashion-catalogue-title"><FashionSectionHeader title={floorTitle} /><span className="fashion-result-count">{toPersianDigits(floorItems.length)} کالا</span></div>
                {brandFilter || selectedGroup || searching ? (
                  <button
                    type="button"
                    onClick={clearFloorFilter}
                    className={`inline-flex min-h-[44px] shrink-0 items-center text-xs font-bold text-[#737373] underline decoration-2 underline-offset-8 transition hover:text-[#1c1c1a] ${FOCUS_RING}`}
                  >
                    نمایش همه کالاها
                  </button>
                ) : null}
              </div>
              {categoryGroups.length > 1 && <nav className="fashion-catalogue-filters" aria-label="فیلتر دسته‌بندی محصولات">
                <button type="button" aria-pressed={!selectedGroup && !brandFilter} onClick={() => selectCategory('all')}>همه</button>
                {categoryGroups.map(group => <button key={group.slug} type="button" aria-pressed={selectedGroup?.slug === group.slug && !brandFilter}
                  onClick={() => selectCategory(group.slug)}>{group.title}</button>)}
              </nav>}
              {floorItems.length === 0 ? (
                <p className="py-6 text-center text-xs font-bold text-[#737373]">کالایی با این مشخصات یافت نشد.</p>
              ) : (
                <div className={wrapCls}>
                  <div className={`grid gap-x-3 gap-y-8 lg:gap-x-7 lg:gap-y-12 ${gridCls}`}>
                    {floorItems.map((p) => (
                      <FashionProductCard key={p.id} product={p} onOpen={openProductDetail} />
                    ))}
                  </div>
                </div>
              )}
            </section>
          </Wrap>
        );
      }
      case 'newsletter':
        return <FashionNewsletter config={cfg} storeSlug={store.slug} />;
      default:
        return null;
    }
  };

  const nodes: React.ReactNode[] = [];
  for (let i = 0; i < order.length; i += 1) {
    const key = order[i];
    const next = order[i + 1];
    if (key === 'announcement' && (next === 'hero' || next === 'heroSlides')) {
      nodes.push(
        <div key="announcement-hero" className="flex flex-col empty:hidden">
          {renderSection('announcement')}
          {renderSection(next)}
        </div>,
      );
      i += 1;
    } else {
      nodes.push(<React.Fragment key={key}>{renderSection(key)}</React.Fragment>);
    }
  }
  const lastKey = order[order.length - 1];

  return (
    <div
      className={`fashion-storefront flex-1 bg-[#faf8f5] text-[#1c1c1a] ${lastKey === 'newsletter' ? '' : 'pb-10 lg:pb-14'}`}
      style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}
    >
      <div className="fashion-section-stack">{nodes}</div>
    </div>
  );
};
export default FashionTemplate;
