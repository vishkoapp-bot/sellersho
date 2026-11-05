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
} from './templateTypes';
import type { TemplateBanner, TemplateConfig, TemplateHeroSlide } from './templateTypes';
import type { Product, Store } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { formatPrice, toPersianDigits } from '../../../utils/formatters';
import { apiRequest } from '../../../services/apiClient';
import {
  DealsSection,
  TimedDealsSection,
  BestsellersSection,
  TrustStripSection,
  DiscountedSection,
  TopRatedSection,
  CampaignSection,
  FaqSection,
  groupProductsByCategory,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';

// Fashion = light editorial gallery, mirroring the aassttiinn.com
// homepage skeleton filled with the store's own content: a full-bleed
// hero image stream, a macro category entrance with a chip row, a
// brand-story split, a «دسته‌بندی‌ها» tile grid, mosaic collection
// cards, portrait designer/brand tiles, large banner tiles, a
// price-free featured image cluster, magazine-style quote cards and a
// dark newsletter band — closing with the catalogue floor, the only
// place prices appear. Cream/off-white page, square tiles on pale
// grey (#f2f2f2), sharp corners, charcoal type, large bold
// right-aligned headings and small bold centred labels under tiles.
// Everything renders from the resolved config order; every text,
// image and price comes from the seller-editable custom config, the
// store's own fields or real product data — nothing is fabricated,
// and blocks without data hide cleanly.

const CONTAINER = 'mx-auto w-full max-w-[1440px] px-4 lg:px-10';

const Wrap: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className={CONTAINER}>{children}</div>
);

// Section headings: large, bold, dark, right-aligned — no eyebrows,
// no rules, no Latin kickers.
const AastinSectionHeader: React.FC<{ title: string }> = ({ title }) => (
  <h2 className="text-right text-3xl font-black leading-snug tracking-tight text-neutral-900 lg:text-[2.6rem] lg:leading-tight">
    {title}
  </h2>
);

// Small bold centred label under a tile, with an optional muted
// sub-line (real count / brand line).
const TileLabel: React.FC<{ title: string; sub?: string }> = ({ title, sub }) => (
  <div className="mt-2.5 text-center">
    <p className="text-sm font-bold leading-6 text-neutral-900">{title}</p>
    {sub ? <p className="mt-0.5 text-[11px] leading-5 text-neutral-500">{sub}</p> : null}
  </div>
);

const groupImage = (group: CategoryGroup): string =>
  group.image || (group.products[0] && group.products[0].image) || '';

// Quiet static announcement line: the announcement text (seller
// config, else the store tagline) centred on a hairline — no marquee,
// no accent pill.
const AastinAnnouncement: React.FC<{ store: Store; config: TemplateConfig }> = ({ store, config }) => {
  if (!sectionEnabled(config, 'announcement')) return null;
  const custom = templateCustom(config);
  const text = (custom.announcementText || '').trim() || (store.tagline || '').trim();
  if (!text) return null;
  const link = (custom.announcementLink || '').trim();
  const inner = (
    <p className="border-b border-neutral-200/90 py-2.5 text-center text-xs font-bold leading-6 text-neutral-600">
      {text}
    </p>
  );
  return (
    <section data-testid="store-announcement">
      {link ? (
        <a href={link} target="_blank" rel="noreferrer" className="block transition hover:text-neutral-900">
          {inner}
        </a>
      ) : (
        inner
      )}
    </section>
  );
};

// Full-bleed tall hero image stream. Slides crossfade; a slide's
// title/subtitle sit large and white at the bottom-right, a linked
// slide is one whole anchor, and small pill dots ride bottom-centre.
// Without slides, the single hero image (config → first product →
// store logo) carries the store title plus ONE underlined text link
// — never a button box.
const AastinHero: React.FC<{
  slides: TemplateHeroSlide[];
  autoplay?: boolean;
  fallbackImage?: string;
  fallbackTitle: string;
  fallbackSubtitle: string;
  ctaText: string;
  ctaLink: string;
}> = ({ slides, autoplay, fallbackImage, fallbackTitle, fallbackSubtitle, ctaText, ctaLink }) => {
  const count = slides.length;
  const [index, setIndex] = useState(0);
  useEffect(() => {
    setIndex(0);
  }, [count]);
  useEffect(() => {
    if (!autoplay || count <= 1) return undefined;
    const timer = setInterval(() => setIndex((i) => (i + 1) % count), 5000);
    return () => clearInterval(timer);
  }, [autoplay, count]);

  if (count === 0) {
    return (
      <section data-testid="store-hero-slider" className="relative w-full overflow-hidden bg-[#efefef]">
        {fallbackImage ? (
          <img src={fallbackImage} alt={fallbackTitle} className="h-[80svh] w-full object-cover lg:h-[92svh]" />
        ) : (
          <div className="h-[80svh] w-full lg:h-[92svh]" />
        )}
        <div
          className={`absolute inset-0 flex flex-col justify-end ${
            fallbackImage ? 'bg-gradient-to-t from-black/60 via-black/15 to-transparent' : ''
          }`}
        >
          <div className={`${CONTAINER} pb-16 lg:pb-20`}>
            <h1
              className={`max-w-2xl text-4xl font-black leading-tight tracking-tight lg:text-6xl ${
                fallbackImage ? 'text-white' : 'text-neutral-900'
              }`}
            >
              {fallbackTitle}
            </h1>
            {fallbackSubtitle ? (
              <p
                className={`mt-3 max-w-xl text-sm leading-8 lg:text-base ${
                  fallbackImage ? 'text-white/85' : 'text-neutral-500'
                }`}
              >
                {fallbackSubtitle}
              </p>
            ) : null}
            <a
              href={ctaLink}
              className={`mt-5 inline-block text-sm font-bold underline decoration-2 underline-offset-8 transition hover:opacity-75 ${
                fallbackImage ? 'text-white' : 'text-neutral-900'
              }`}
            >
              {ctaText}
            </a>
          </div>
        </div>
      </section>
    );
  }

  const active = slides[index % count];
  return (
    <section data-testid="store-hero-slider" className="relative w-full overflow-hidden bg-[#efefef]">
      <div className="relative h-[80svh] lg:h-[92svh]">
        {slides.map((slide, i) => {
          const frame = slide.image ? (
            <img src={slide.image} alt={slide.title || ''} className="h-full w-full object-cover" loading="lazy" />
          ) : (
            <div className="flex h-full w-full items-center bg-[#ece9e2] px-6">
              <span className="text-3xl font-black leading-snug text-neutral-900 lg:text-5xl">{slide.title || ''}</span>
            </div>
          );
          const cls = `absolute inset-0 transition-opacity duration-700 ${
            i === index % count ? 'opacity-100' : 'pointer-events-none opacity-0'
          }`;
          return slide.link ? (
            <a key={`${slide.image || slide.title || ''}-${i}`} href={slide.link} target="_blank" rel="noreferrer" className={`block ${cls}`}>
              {frame}
            </a>
          ) : (
            <div key={`${slide.image || slide.title || ''}-${i}`} className={cls}>
              {frame}
            </div>
          );
        })}
        {(active.title || active.subtitle) && active.image ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 via-black/15 to-transparent">
            <div className={`${CONTAINER} pb-16 lg:pb-20`}>
              {active.title ? (
                <p className="max-w-2xl text-4xl font-black leading-tight tracking-tight text-white lg:text-6xl">
                  {active.title}
                </p>
              ) : null}
              {active.subtitle ? (
                <p className="mt-3 max-w-xl text-sm leading-8 text-white/85 lg:text-base">{active.subtitle}</p>
              ) : null}
            </div>
          </div>
        ) : null}
        {count > 1 ? (
          <div className="absolute bottom-5 left-1/2 z-10 flex -translate-x-1/2 items-center gap-1.5">
            {slides.map((s, i) => (
              <button
                key={`${s.image || s.title || ''}-dot-${i}`}
                type="button"
                aria-label={`اسلاید ${toPersianDigits(i + 1)}`}
                onClick={() => setIndex(i)}
                className={`h-1.5 rounded-full transition-all duration-300 ${
                  i === index % count ? 'w-7 bg-white' : 'w-2.5 bg-white/45 hover:bg-white/75'
                }`}
              />
            ))}
          </div>
        ) : null}
      </div>
    </section>
  );
};

// Macro category entrance: the first three category groups as large
// portrait tiles, then the remaining groups as a chip row (each chip
// names a real category of this store and leads to the catalogue
// floor). No heading — the entrance speaks for itself.
const AastinMacroCategories: React.FC<{ groups: CategoryGroup[] }> = ({ groups }) => {
  const withImagery = groups.filter((g) => groupImage(g));
  const macro = (withImagery.length > 0 ? withImagery : groups).slice(0, 3);
  if (macro.length === 0) return null;
  const macroSlugs = new Set(macro.map((g) => g.slug));
  const chips = groups.filter((g) => !macroSlugs.has(g.slug));
  return (
    <section data-testid="store-category-macro" className="space-y-5">
      <div className="grid grid-cols-3 gap-2 lg:gap-3">
        {macro.map((group) => {
          const image = groupImage(group);
          return (
            <div key={group.slug}>
              <div className="group relative aspect-square overflow-hidden rounded-[2px] bg-[#f2f2f2]">
                {image ? (
                  <img
                    src={image}
                    alt={group.title}
                    loading="lazy"
                    className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
                  />
                ) : (
                  <span className="absolute inset-0 flex items-center justify-center text-5xl font-black text-neutral-300">
                    {group.title.slice(0, 1)}
                  </span>
                )}
              </div>
              <TileLabel title={group.title} sub={`${toPersianDigits(group.count)} کالا`} />
            </div>
          );
        })}
      </div>
      {chips.length > 0 ? (
        <div className="flex flex-wrap justify-center gap-2">
          {chips.map((group) => (
            <a
              key={group.slug}
              href="#products"
              className="rounded-full border border-neutral-300 bg-white px-4 py-1.5 text-xs font-bold text-neutral-700 transition hover:border-neutral-900 hover:text-neutral-900"
            >
              {group.title}
            </a>
          ))}
        </div>
      ) : null}
    </section>
  );
};

// Brand-story split: the about text as prose paragraphs beside one
// large square image (config aboutImage). Text source mirrors the
// shared AboutSection: config aboutText, else the store's own
// description fields. No text → the block hides.
const AastinStory: React.FC<{ store: Store; config: TemplateConfig }> = ({ store, config }) => {
  if (!sectionEnabled(config, 'about')) return null;
  const custom = templateCustom(config);
  const text = (
    (custom.aboutText || '').trim() ||
    (store.description || store.shortDescription || store.tagline || '').trim()
  );
  if (!text) return null;
  const paragraphs = text.split(/\n+/).map((p) => p.trim()).filter(Boolean);
  const image = (custom.aboutImage || '').trim();
  const title = customText(config, 'aboutTitle', 'درباره فروشگاه');
  const prose = (
    <div>
      <AastinSectionHeader title={title} />
      <div className="mt-5 space-y-4">
        {paragraphs.map((p, i) => (
          <p key={i} className="text-[15px] leading-8 text-neutral-600">
            {p}
          </p>
        ))}
      </div>
    </div>
  );
  return (
    <section data-testid="store-about">
      {image ? (
        <div className="grid items-center gap-8 lg:grid-cols-2">
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

// «دسته‌بندی‌ها» tile grid: square tiles on pale grey, label centred
// underneath. Shows the groups the macro entrance did not take (or
// all of them when the store has only a few categories).
const AastinCategoryGrid: React.FC<{ groups: CategoryGroup[]; title: string }> = ({ groups, title }) => {
  if (groups.length === 0) return null;
  return (
    <section data-testid="store-category-cards" className="space-y-6">
      <AastinSectionHeader title={title} />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-3 lg:gap-4">
        {groups.map((group) => {
          const image = groupImage(group);
          return (
            <div key={group.slug}>
              <div className="group relative aspect-square overflow-hidden rounded-[2px] bg-[#f2f2f2]">
                {image ? (
                  <img
                    src={image}
                    alt={group.title}
                    loading="lazy"
                    className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
                  />
                ) : (
                  <span className="absolute inset-0 flex items-center justify-center text-5xl font-black text-neutral-300">
                    {group.title.slice(0, 1)}
                  </span>
                )}
              </div>
              <TileLabel title={group.title} sub={`${toPersianDigits(group.count)} کالا`} />
            </div>
          );
        })}
      </div>
    </section>
  );
};

// «مجموعه جدید» collections rail: each card is a 2×2 mosaic of real
// product images from one category group, captioned with the group
// title and its real product count. Groups with fewer than two
// distinct product images cannot form a mosaic and are skipped.
const AastinCollections: React.FC<{ groups: CategoryGroup[]; title: string }> = ({ groups, title }) => {
  const cards = groups
    .map((group) => {
      const images: string[] = [];
      group.products.forEach((p) => {
        if (p.image && !images.includes(p.image)) images.push(p.image);
      });
      if (images.length < 2 && group.image && !images.includes(group.image)) images.push(group.image);
      return { group, images: images.slice(0, 4) };
    })
    .filter((c) => c.images.length >= 2)
    .slice(0, 6);
  if (cards.length === 0) return null;
  return (
    <section data-testid="store-collections" className="space-y-6">
      <AastinSectionHeader title={title} />
      <div className="flex snap-x gap-4 overflow-x-auto pb-2 no-scrollbar">
        {cards.map(({ group, images }) => (
          <div key={group.slug} className="w-[270px] shrink-0 snap-start lg:w-[350px]">
            <div className="grid grid-cols-2 gap-1 overflow-hidden rounded-[2px] bg-white">
              {images.map((src, i) => (
                <div key={`${src}-${i}`} className="aspect-square overflow-hidden bg-[#f2f2f2]">
                  <img src={src} alt={group.title} loading="lazy" className="h-full w-full object-cover" />
                </div>
              ))}
            </div>
            <TileLabel title={group.title} sub={`${toPersianDigits(group.count)} کالا`} />
          </div>
        ))}
      </div>
    </section>
  );
};

// Editorial lookbook band: the configured lookbook image as a tall
// full-width still with the title/subtitle overlaid, wrapped in its
// link when set. Without an image, title + subtitle render as a quiet
// pale panel; with neither, the band hides.
const AastinLookbookBand: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  if (!sectionEnabled(config, 'lookbook')) return null;
  const custom = templateCustom(config);
  const image = (custom.lookbookImage || '').trim();
  const title = (custom.lookbookTitle || '').trim();
  const subtitle = (custom.lookbookSubtitle || '').trim();
  if (!image && !title) return null;
  const link = (custom.lookbookLink || '').trim();
  const body = image ? (
    <div className="relative overflow-hidden rounded-[2px] bg-[#f2f2f2]">
      <img src={image} alt={title} loading="lazy" className="h-[52svh] w-full object-cover lg:h-[72svh]" />
      {title || subtitle ? (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 via-black/15 to-transparent p-6 pt-20 lg:p-10">
          {title ? <p className="text-3xl font-black leading-snug text-white lg:text-5xl">{title}</p> : null}
          {subtitle ? <p className="mt-2 max-w-xl text-sm leading-8 text-white/85">{subtitle}</p> : null}
        </div>
      ) : null}
    </div>
  ) : (
    <div className="rounded-[2px] bg-[#efefef] px-6 py-14 text-center lg:py-20">
      {title ? <p className="text-3xl font-black leading-snug text-neutral-900 lg:text-5xl">{title}</p> : null}
      {subtitle ? <p className="mx-auto mt-3 max-w-xl text-sm leading-8 text-neutral-500">{subtitle}</p> : null}
    </div>
  );
  return (
    <section data-testid="store-lookbook">
      {link ? (
        <a href={link} target="_blank" rel="noreferrer" className="block">
          {body}
        </a>
      ) : (
        body
      )}
    </section>
  );
};

// Designer/brand portrait tiles: distinct product brands (falling
// back to supplier names), each represented by a real product image,
// label centred underneath. Fewer than two brands → hides, so a
// single-brand store never shows a degenerate strip.
const AastinBrandTiles: React.FC<{ products: Product[]; title: string }> = ({ products, title }) => {
  const brands = useMemo(() => {
    const byBrand = new Map<string, string>();
    products.forEach((p) => {
      const name = (p.brand || p.supplierName || '').trim();
      if (name && !byBrand.has(name)) byBrand.set(name, p.image || '');
    });
    return Array.from(byBrand.entries())
      .map(([name, image]) => ({ name, image }))
      .slice(0, 4);
  }, [products]);
  if (brands.length < 2) return null;
  return (
    <section data-testid="store-brands" className="space-y-6">
      <AastinSectionHeader title={title} />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4 lg:gap-4">
        {brands.map((brand) => (
          <div key={brand.name}>
            <div className="group relative aspect-[3/4] overflow-hidden rounded-[2px] bg-[#f2f2f2]">
              {brand.image ? (
                <img
                  src={brand.image}
                  alt={brand.name}
                  loading="lazy"
                  className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
                />
              ) : (
                <span className="absolute inset-0 flex items-center justify-center text-5xl font-black text-neutral-300">
                  {brand.name.slice(0, 1)}
                </span>
              )}
            </div>
            <TileLabel title={brand.name} />
          </div>
        ))}
      </div>
    </section>
  );
};

// Large sharp banner tiles (the designer-banner slot): one banner
// spans wide, two or more sit in a pair grid. A banner's title, when
// the seller set one, sits centred underneath — never overlaid.
const AastinBanners: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => {
  if (banners.length === 0) return null;
  const single = banners.length === 1;
  return (
    <section data-testid="store-banners" className={`grid gap-4 ${single ? 'grid-cols-1' : 'grid-cols-1 sm:grid-cols-2'}`}>
      {banners.map((b, i) => {
        const tile = (
          <>
            <div
              className={`group relative overflow-hidden rounded-[2px] bg-[#f2f2f2] ${
                single ? 'aspect-[16/10] lg:aspect-[21/9]' : 'aspect-[4/3]'
              }`}
            >
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
        return b.link ? (
          <a key={`${b.image}-${i}`} href={b.link} target="_blank" rel="noreferrer" className="block">
            {tile}
          </a>
        ) : (
          <div key={`${b.image}-${i}`}>{tile}</div>
        );
      })}
    </section>
  );
};

// Featured image cluster (the category-spotlight slot): featured
// products as pure imagery — the first spans large, the rest sit in
// squares. Editorial sections carry NO prices and NO badges; clicking
// a tile opens the product, like every product surface.
const AastinSpotlight: React.FC<{
  products: Product[];
  title: string;
  onOpen: (p: Product) => void;
}> = ({ products, title, onOpen }) => {
  const picks = products.slice(0, 5);
  if (picks.length === 0) return null;
  return (
    <section data-testid="store-featured" className="space-y-6">
      <AastinSectionHeader title={title} />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {picks.map((p, i) => (
          <div
            key={p.id}
            onClick={() => onOpen(p)}
            className={`group relative cursor-pointer overflow-hidden rounded-[2px] bg-[#f2f2f2] ${
              i === 0 ? 'col-span-2 aspect-[4/3] lg:row-span-2 lg:aspect-auto lg:h-full' : 'aspect-square'
            }`}
          >
            {p.image ? (
              <img
                src={p.image}
                alt={p.title}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
              />
            ) : (
              <span className="absolute inset-0 flex items-center justify-center text-5xl font-black text-neutral-300">
                {p.title.slice(0, 1)}
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  );
};

// Magazine cards: the seller-configured testimonials are the only
// editorial text source the platform has, so they render as quiet
// article-style quote cards (text + author). Nothing configured →
// the magazine slot simply does not exist.
const AastinMagazine: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  if (!sectionEnabled(config, 'testimonials')) return null;
  const items = (templateCustom(config).testimonials || []).filter(
    (t) => t && typeof t.text === 'string' && t.text.trim() !== '',
  );
  if (items.length === 0) return null;
  return (
    <section data-testid="store-testimonials" className="space-y-6">
      <AastinSectionHeader title="نظرات مشتریان" />
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        {items.map((t, i) => (
          <figure key={`${t.name || ''}-${i}`} className="rounded-[2px] border border-neutral-200/80 bg-white p-6">
            <span className="text-4xl font-black leading-none text-neutral-300">“</span>
            <blockquote className="mt-3 text-sm leading-8 text-neutral-700">{t.text}</blockquote>
            {t.name ? <figcaption className="mt-4 text-sm font-bold text-neutral-900">{t.name}</figcaption> : null}
          </figure>
        ))}
      </div>
    </section>
  );
};

// Catalogue-floor card, styled like aassttiinn's listing cards:
// square tile on pale grey, bold name, muted brand line, price below.
// This is the ONLY card on the page that shows a price.
const AastinProductCard: React.FC<{ product: Product; onOpen: (p: Product) => void }> = ({ product, onOpen }) => (
  <div onClick={() => onOpen(product)} className="group cursor-pointer">
    <div className="relative aspect-square overflow-hidden rounded-[2px] bg-[#f2f2f2]">
      {product.image ? (
        <img
          src={product.image}
          alt={product.title}
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
        />
      ) : (
        <span className="absolute inset-0 flex items-center justify-center text-5xl font-black text-neutral-300">
          {product.title.slice(0, 1)}
        </span>
      )}
    </div>
    <p className="mt-3 line-clamp-2 text-sm font-bold leading-6 text-neutral-900">{product.title}</p>
    {product.brand || product.supplierName ? (
      <p className="mt-0.5 truncate text-xs leading-5 text-neutral-500">{product.brand || product.supplierName}</p>
    ) : null}
    <p className="mt-1.5 text-sm font-black text-neutral-900">
      {formatPrice(product.price)} <span className="text-xs font-bold text-neutral-500">تومان</span>
    </p>
  </div>
);

// Dark charcoal grey-green newsletter band (#3a4441), full-bleed:
// title + subtitle on one side, an underline-style input and an
// outline submit button on the other. The subscribe contract is the
// platform's own (mobile number + store slug, same endpoint and
// validation as the shared NewsletterSection).
const AastinNewsletter: React.FC<{ config: TemplateConfig; storeSlug?: string }> = ({ config, storeSlug }) => {
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
      <div className={`${CONTAINER} py-12 lg:py-16`}>
        <div className="flex flex-col gap-8 lg:flex-row lg:items-center lg:justify-between">
          <div className="max-w-xl">
            <h2 className="text-2xl font-black leading-snug lg:text-3xl">{title}</h2>
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
                  className="min-w-0 flex-1 border-0 border-b border-white/40 bg-transparent py-2.5 text-left text-sm text-white outline-none transition placeholder:text-white/40 focus:border-white"
                />
                <button
                  type="submit"
                  className="shrink-0 rounded-full border border-white/70 px-6 py-2.5 text-sm font-bold text-white transition hover:bg-white hover:text-[#3a4441]"
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
  const slides = heroSlidesOf(cfg);
  const banners = bannersOf(cfg);
  const custom = templateCustom(cfg);
  const { openProductDetail } = useStore();
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  const heroTitle = customText(cfg, 'heroTitle', store.name);
  const heroSubtitle = customText(cfg, 'heroSubtitle', store.tagline || store.description || store.shortDescription || '');
  const heroCtaText = customText(cfg, 'heroCtaText', 'مشاهده کالکشن');
  const heroCtaLink = custom.heroCtaLink || '#products';
  const heroImage = custom.heroImage || (slides[0] && slides[0].image) || (items[0] && items[0].image) || store.logo;
  const productsTitle = customText(cfg, 'productsTitle', 'همه کالاها');

  const categoryGroups = useMemo(
    () => groupProductsByCategory(items, categories, custom.categoryOverrides),
    [items, categories, cfg],
  );
  const featuredList = useMemo(
    () => items.filter((p) => p.featured).slice(0, customCount(cfg, 'featuredCount', 10)),
    [items, cfg],
  );
  // The «دسته‌بندی‌ها» grid shows the groups the macro entrance did
  // not take; tiny catalogues (≤3 groups) show all of them instead.
  const gridGroups = useMemo(() => {
    const limit = customCount(cfg, 'categoriesCount', 6);
    if (categoryGroups.length <= 3) return categoryGroups.slice(0, limit);
    const withImagery = categoryGroups.filter((g) => groupImage(g));
    const macroPool = withImagery.length > 0 ? withImagery : categoryGroups;
    const macroSlugs = new Set(macroPool.slice(0, 3).map((g) => g.slug));
    return categoryGroups.filter((g) => !macroSlugs.has(g.slug)).slice(0, limit);
  }, [categoryGroups, cfg]);

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return (
          <Wrap>
            <AastinAnnouncement store={store} config={cfg} />
          </Wrap>
        );
      case 'hero':
      case 'heroSlides':
        return once('hero') ? (
          <AastinHero
            slides={slides}
            autoplay={custom.heroAutoplay}
            fallbackImage={heroImage}
            fallbackTitle={heroTitle}
            fallbackSubtitle={heroSubtitle}
            ctaText={heroCtaText}
            ctaLink={heroCtaLink}
          />
        ) : null;
      case 'categoryRail':
        return (
          <Wrap>
            <AastinMacroCategories groups={categoryGroups} />
          </Wrap>
        );
      case 'about':
        return (
          <Wrap>
            <AastinStory store={store} config={cfg} />
          </Wrap>
        );
      case 'lookbook':
        // One slot carries their adjacent category grid + collections
        // + editorial band, in their order.
        return (
          <Wrap>
            <div className="space-y-14 lg:space-y-24">
              <AastinCategoryGrid groups={gridGroups} title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')} />
              <AastinCollections groups={categoryGroups} title={customText(cfg, 'lookbookTitle', 'کالکشن جدید')} />
              <AastinLookbookBand config={cfg} />
            </div>
          </Wrap>
        );
      case 'brands':
        return (
          <Wrap>
            <AastinBrandTiles products={items} title={customText(cfg, 'brandsTitle', 'برندها')} />
          </Wrap>
        );
      case 'banners':
      case 'promo':
        return once('banners') ? (
          <Wrap>
            <AastinBanners banners={banners} />
          </Wrap>
        ) : null;
      case 'featured':
        return (
          <Wrap>
            <AastinSpotlight
              products={featuredList}
              title={customText(cfg, 'featuredTitle', 'کالاهای منتخب')}
              onOpen={openProductDetail}
            />
          </Wrap>
        );
      case 'bestsellers':
        return (
          <Wrap>
            <BestsellersSection products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'deals':
        return (
          <Wrap>
            <DealsSection products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'timedDeals':
        return (
          <Wrap>
            <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'discounted':
        return (
          <Wrap>
            <DiscountedSection products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'topRated':
        return (
          <Wrap>
            <TopRatedSection products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'campaign':
        return (
          <Wrap>
            <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'trustStrip':
        return (
          <Wrap>
            <TrustStripSection config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'faq':
        return (
          <Wrap>
            <FaqSection config={cfg} accentColor={accentColor} />
          </Wrap>
        );
      case 'testimonials':
        return (
          <Wrap>
            <AastinMagazine config={cfg} />
          </Wrap>
        );
      case 'products':
      case 'allProducts':
        return once('products') ? (
          items.length > 0 ? (
            <Wrap>
              <section id="products" data-testid="store-products" className="space-y-6">
                <AastinSectionHeader title={productsTitle} />
                <div className="grid grid-cols-2 gap-x-3 gap-y-8 lg:grid-cols-4 lg:gap-x-4">
                  {items.map((p) => (
                    <AastinProductCard key={p.id} product={p} onOpen={openProductDetail} />
                  ))}
                </div>
              </section>
            </Wrap>
          ) : null
        ) : null;
      case 'newsletter':
        return <AastinNewsletter config={cfg} storeSlug={store.slug} />;
      default:
        return null;
    }
  };

  return (
    <div
      className="flex-1 bg-[#faf8f5] text-neutral-900"
      data-template-desktop="lg:grid"
      style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}
    >
      <div className="flex flex-col gap-14 lg:gap-24">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default FashionTemplate;
