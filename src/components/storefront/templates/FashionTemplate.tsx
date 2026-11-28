import { ArrowLeft, ChevronLeft, ChevronRight, LayoutGrid, Pause, Play } from 'lucide-react';
import './fashion.css';
import React, { useEffect, useMemo, useState } from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  resolveFashionMacroEntries,
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
import { toPersianDigits } from '../../../utils/formatters';
import { FashionProductCard } from '../FashionProductCard';
import { apiRequest } from '../../../services/apiClient';
import { groupProductsByCategory } from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { StorefrontCatalogue } from './StorefrontCatalogue';
import { FallbackImage } from '../../common/FallbackImage';

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
  const [autoplayStopped, setAutoplayStopped] = useState(false);
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
    if (!autoplay || count < 2 || paused || autoplayStopped || reducedMotion) return;
    const timer = window.setInterval(() => setIndex(i => (i + 1) % count), 6000);
    return () => window.clearInterval(timer);
  }, [autoplay, count, paused, autoplayStopped, reducedMotion]);
  const active = slides[index % count] || {};
  const heading = active.title || title;
  const link = active.link || ctaLink;
  return (
    <section data-testid="store-hero-slider" className={`fashion-hero ${slides[index % count]?.image ? '' : 'fashion-hero-text-only'}`}
      aria-roledescription="نمایش اسلاید" aria-label="کالکشن فروشگاه"
      onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setPaused(true)}
      onBlurCapture={e => { if (!e.currentTarget.contains(e.relatedTarget)) setPaused(false); }}>
      <div className="fashion-hero-copy">
        {label !== heading && <span className="fashion-eyebrow">{label}</span>}
        <h1>{heading}</h1>
        {(active.subtitle || subtitle) && <p>{active.subtitle || subtitle}</p>}
        {link && <a href={link} {...externalLinkProps(link)} className="fashion-button">{ctaText}<ArrowLeft size={18} strokeWidth={1.5} aria-hidden="true" /></a>}
      </div>
      <div className="fashion-hero-media">
        {active.image ? <FallbackImage key={active.image} srcs={[active.image, '/templates/fashion/hero.webp']} alt=""
          width={1200} height={900} loading="eager" fetchPriority={index === 0 ? 'high' : 'auto'} fallback={null} className="fashion-hero-photo" /> : null}
        {count > 1 && <div className="fashion-slide-controls">
          <button type="button" aria-label="اسلاید قبلی" onClick={() => setIndex(i => (i - 1 + count) % count)}><ChevronRight size={18} aria-hidden="true" /></button>
          <span aria-live={paused || autoplayStopped ? 'polite' : 'off'}>{toPersianDigits(index % count + 1)} از {toPersianDigits(count)}</span>
          <button type="button" aria-label="اسلاید بعدی" onClick={() => setIndex(i => (i + 1) % count)}><ChevronLeft size={18} aria-hidden="true" /></button>
          {autoplay && !reducedMotion && <button type="button" aria-label={autoplayStopped ? 'پخش خودکار اسلایدها' : 'توقف پخش خودکار'} onClick={() => setAutoplayStopped(value => !value)}>
            {autoplayStopped ? <Play size={16} aria-hidden="true" /> : <Pause size={16} aria-hidden="true" />}
          </button>}
        </div>}
      </div>
    </section>
  );
};

const FashionMacroCategories: React.FC<{
  groups: CategoryGroup[];
  ledger: MediaLedger;
  banners: TemplateBanner[];
  limit: number;
  title: string;
  onSelect: (slug: string) => void;
}> = ({ groups, ledger, banners, limit, title, onSelect }) => {
  const entries = resolveFashionMacroEntries(groups, banners, limit);
  groups.slice(0, limit).forEach(g => ledger.macroTaken.add(g.slug));
  entries.forEach((entry) => { if (entry.image) claimImage(ledger, entry.image); });
  if (!entries.length) return null;
  return <section id="fashion-categories" data-testid="store-category-macro" aria-label="دسته‌بندی‌ها" className="fashion-category-section">
    <FashionSectionHeader title={title} />
    <div className="fashion-category-grid" data-count={entries.length}>
      {entries.map((entry, i) => <a key={entry.slug || i} href={entry.link} {...externalLinkProps(entry.link)}
        onClick={() => { if (entry.slug) onSelect(entry.slug); }} className={`fashion-category ${FOCUS_RING}`}>
        <div className="fashion-category-image" data-panel={entry.image === '/templates/fashion/categories.webp' && !entry.slug ? i : undefined}>
          {entry.image ? <FallbackImage srcs={[entry.image]} alt="" width={480} height={360} loading="lazy" decoding="async"
            fallback={<LayoutGrid size={26} strokeWidth={1.3} aria-hidden="true" />} /> : <LayoutGrid size={26} strokeWidth={1.3} aria-hidden="true" />}
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
      <section id="about" data-testid="store-about" className="max-w-[560px]">
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
    <section id="about" data-testid="store-about">
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
  onSelect: (slug: string) => void;
}> = ({ groups, title, ledger, onSelect }) => {
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
    <a key={card.group.slug} href="#products" onClick={() => onSelect(card.group.slug)} className={widthCls}>
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
    </a>
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
  return (
    <section data-testid="store-featured" className="space-y-6 lg:space-y-8">
      <FashionSectionHeader title={title} />
      <div className="fashion-product-grid">
        {products.map(product => <FashionProductCard key={product.id} product={product} onOpen={onOpen} />)}
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

export const FashionTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories, productFeed }) => {
  const cfg = resolveTemplateConfig('tpl-fashion-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const configuredSlides = heroSlidesOf(cfg);
  const slides = configuredSlides.length ? configuredSlides : [{ image: templateCustom(cfg).heroImage, title: templateCustom(cfg).heroTitle, subtitle: templateCustom(cfg).heroSubtitle }];
  const banners = bannersOf(cfg);
  const custom = templateCustom(cfg);
  const {
    openProductDetail,
    setSelectedCategory,
    openCatalogBrand,
    clearCatalogBrand,
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
  const heroCtaLink = custom.heroCtaLink || (order.some(key => key === 'allProducts' || key === 'products') ? '#products' : '');
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

  const selectCategory = (slug: string): void => {
    clearCatalogBrand();
    setSearchQuery('');
    setSelectedCategory(slug);
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
            <FashionMacroCategories groups={categoryGroups} ledger={ledger} onSelect={selectCategory} banners={banners}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')} limit={Math.min(customCount(cfg, 'categoriesCount', 3), 3)} />
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
              onSelect={selectCategory}
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
          .slice(0, 4);
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
        return (
          <Wrap>
            <StorefrontCatalogue editorial items={items} categoryGroups={categoryGroups} productsTitle={productsTitle} productFeed={productFeed} />
          </Wrap>
        );
      }
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

  return (
    <main id="fashion-main"
      className="fashion-storefront flex-1 bg-[#faf8f5] pb-10 text-[#1c1c1a] lg:pb-14"
      style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}
    >
      {!order.some(key => key === 'hero' || key === 'heroSlides') && <h1 className="sr-only">{store.name}</h1>}
      <div className="fashion-section-stack">{nodes}</div>
    </main>
  );
};
export default FashionTemplate;
