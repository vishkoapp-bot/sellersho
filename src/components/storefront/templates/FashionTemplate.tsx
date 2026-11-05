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

// Fashion = light editorial gallery, mirroring the aassttiinn.com
// homepage skeleton filled with the store's own content: a slim
// announcement bar stacked flush on a full-bleed hero image stream, a
// macro category entrance with a chip row, a brand-story split, a
// category tile grid, mosaic collection cards, an editorial lookbook
// band, portrait brand tiles, large banner tiles, a price-free
// featured image cluster, customer-quote cards, a hairline FAQ and a
// dark newsletter band — closing with the catalogue floor, the only
// place prices appear.
//
// Template identity is a WHITELIST: the shared app-style sections
// (deals, timedDeals, discounted, campaign, topRated, bestsellers,
// trustStrip) never render inside tpl-fashion-1, even when a saved
// config enables them; every block below is fashion-native. Palette
// is fixed: page #faf8f5, tile #f2f2f2, announcement #efeae3, card
// #ffffff, ink #1c1c1a, muted #737373, line #e5e0d8, band #3a4441.
// A media ledger threads the page in render order so an editorial
// image is never repeated by a later module; the catalogue floor is
// the index and is exempt. Everything renders from the resolved
// config order; every text, image and price comes from the
// seller-editable custom config, the store's own fields or real
// product data — nothing is fabricated, and blocks without data hide
// cleanly.

const CONTAINER = 'mx-auto w-full max-w-[1440px] px-4 lg:px-10';

// Focus ring for every interactive surface in this template; consumes
// the seller accent via the --tpl-accent custom property set on the
// template root.
const FOCUS_RING =
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--tpl-accent)]';

// Seller-configured links open in a new tab ONLY when they point at
// an external http(s) URL; internal links (#products, paths) stay in
// the same tab.
const externalLinkProps = (link: string): { target?: string; rel?: string } =>
  /^https?:\/\//i.test((link || '').trim()) ? { target: '_blank', rel: 'noreferrer' } : {};

// empty:hidden is load-bearing: a section component that self-hides
// returns null, leaving this div childless — display:none then removes
// it from the page's flex column entirely, so a hidden section
// contributes zero height AND consumes no rhythm gap around it.
const Wrap: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className={`${CONTAINER} empty:hidden`}>{children}</div>
);

// Section headings: one type token for the whole template — large,
// extrabold, dark, right-aligned. No eyebrows, no rules, no kickers.
const AastinSectionHeader: React.FC<{ title: string }> = ({ title }) => (
  <h2 className="text-right text-[28px] font-extrabold leading-[1.4] tracking-normal text-[#1c1c1a] lg:text-[40px]">
    {title}
  </h2>
);

// Small bold centred label under a tile, with an optional muted
// meta line (real count / brand line).
const TileLabel: React.FC<{ title: string; sub?: string }> = ({ title, sub }) => (
  <div className="mt-2.5 text-center">
    <p className="text-sm font-bold leading-6 text-[#1c1c1a]">{title}</p>
    {sub ? <p className="mt-0.5 text-[11px] leading-5 text-[#737373]">{sub}</p> : null}
  </div>
);

const brandOf = (p: Product): string => (p.brand || '').trim();

// --- Media ledger ---------------------------------------------------
// One used-images set threaded through the page in render order:
// hero slides claim first (in blueprint order), then macro tiles,
// the category grid, collections, brands, banners and the featured
// cluster. A product-derived module picks the first candidate image
// the ledger has not seen; when none is left it falls back to the
// next product's image or hides. Catalogue-floor cards are exempt —
// the floor is the index, repetition there is expected.
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

// Adaptive tile layout for tile grids: a lone tile goes wide and
// cinematic instead of shrinking into a right-aligned stub, two
// tiles split the row as squares, and three or more use the caller's
// full grid.
const tileLayout = (count: number, fullGrid: string): { grid: string; aspect: string } => {
  if (count <= 1) return { grid: 'grid-cols-1', aspect: 'aspect-[16/10] lg:aspect-[21/9]' };
  if (count === 2) return { grid: 'grid-cols-2', aspect: 'aspect-square' };
  return { grid: fullGrid, aspect: 'aspect-square' };
};

// Slim announcement bar: the announcement text (seller config, else
// the store tagline) as one centred line in a full-bleed band on a
// subtly deeper cream. The bar is auto-height (min 36px / 40px
// desktop) and the text may wrap to two lines instead of truncating
// away; when the bar is a link its hit area is a full 44px. The bar
// renders as one structural stack with the hero (see the root), so
// no rhythm gap ever separates them.
const AastinAnnouncement: React.FC<{ store: Store; config: TemplateConfig }> = ({ store, config }) => {
  if (!sectionEnabled(config, 'announcement')) return null;
  const custom = templateCustom(config);
  const text = (custom.announcementText || '').trim() || (store.tagline || '').trim();
  if (!text) return null;
  const link = (custom.announcementLink || '').trim();
  const inner = (
    <div className={`${CONTAINER} flex ${link ? 'min-h-[44px]' : 'min-h-9 lg:min-h-10'} items-center justify-center py-1.5`}>
      <p className="line-clamp-2 text-center text-xs font-bold leading-5 lg:text-[13px]">{text}</p>
    </div>
  );
  return (
    <section data-testid="store-announcement" className="w-full bg-[#efeae3] text-[#1c1c1a]">
      {link ? (
        <a href={link} {...externalLinkProps(link)} className={`block transition hover:opacity-75 ${FOCUS_RING}`}>
          {inner}
        </a>
      ) : (
        inner
      )}
    </section>
  );
};

// Hero. With configured slides: a full-bleed tall image stream —
// slides crossfade, the active slide's title is the page's single H1
// overlaid large and white at the bottom-right, small pill dots ride
// bottom-centre on 44px hit areas, autoplay pauses on hover/focus
// and respects prefers-reduced-motion, inactive slides are
// aria-hidden and out of the tab order. Without slides: a purely
// TYPOGRAPHIC hero — the store name as the single H1, the tagline
// and one underlined text CTA on the plain page background. A
// product photo or the store logo is never placed under the H1.
const AastinHero: React.FC<{
  slides: TemplateHeroSlide[];
  autoplay?: boolean;
  title: string;
  subtitle: string;
  ctaText: string;
  ctaLink: string;
}> = ({ slides, autoplay, title, subtitle, ctaText, ctaLink }) => {
  const count = slides.length;
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    setIndex(0);
  }, [count]);
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReducedMotion(mq.matches);
    const onChange = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  useEffect(() => {
    if (!autoplay || count <= 1 || paused || reducedMotion) return undefined;
    const timer = setInterval(() => setIndex((i) => (i + 1) % count), 5000);
    return () => clearInterval(timer);
  }, [autoplay, count, paused, reducedMotion]);

  if (count === 0) {
    return (
      <section data-testid="store-hero-slider" className="w-full bg-[#faf8f5]">
        <div className={`${CONTAINER} py-14 lg:py-20`}>
          <h1 className="max-w-2xl text-4xl font-extrabold leading-[1.35] tracking-normal text-[#1c1c1a] lg:text-6xl">
            {title}
          </h1>
          {subtitle ? (
            <p className="mt-3 max-w-xl text-sm leading-8 text-[#737373] lg:text-base">{subtitle}</p>
          ) : null}
          <a
            href={ctaLink}
            className={`mt-3 inline-flex min-h-[44px] items-center text-sm font-bold text-[#1c1c1a] underline decoration-2 underline-offset-8 transition hover:opacity-75 ${FOCUS_RING}`}
          >
            {ctaText}
          </a>
        </div>
      </section>
    );
  }

  const activeIndex = ((index % count) + count) % count;
  const active = slides[activeIndex];
  return (
    <section
      data-testid="store-hero-slider"
      className="relative w-full overflow-hidden bg-[#f2f2f2]"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      <div className="relative h-[80svh] lg:h-[92svh]">
        {slides.map((slide, i) => {
          const isActive = i === activeIndex;
          const frame = slide.image ? (
            <img
              src={slide.image}
              alt={slide.title || ''}
              className="h-full w-full object-cover object-[center_30%]"
              loading={i === 0 ? 'eager' : 'lazy'}
              fetchPriority={i === 0 ? 'high' : undefined}
            />
          ) : (
            <div className="flex h-full w-full items-center bg-[#f2f2f2] px-6">
              {isActive ? (
                <h1 className="text-3xl font-extrabold leading-[1.35] tracking-normal text-[#1c1c1a] lg:text-5xl">
                  {slide.title || ''}
                </h1>
              ) : (
                <span className="text-3xl font-extrabold leading-[1.35] tracking-normal text-[#1c1c1a] lg:text-5xl">
                  {slide.title || ''}
                </span>
              )}
            </div>
          );
          const cls = `absolute inset-0 transition-opacity duration-700 ${
            isActive ? 'opacity-100' : 'pointer-events-none opacity-0'
          }`;
          return slide.link ? (
            <a
              key={`${slide.image || slide.title || ''}-${i}`}
              href={slide.link}
              {...externalLinkProps(slide.link)}
              aria-hidden={!isActive}
              tabIndex={isActive ? 0 : -1}
              className={`block ${cls} ${FOCUS_RING}`}
            >
              {frame}
            </a>
          ) : (
            <div key={`${slide.image || slide.title || ''}-${i}`} aria-hidden={!isActive} className={cls}>
              {frame}
            </div>
          );
        })}
        {(active.title || active.subtitle) && active.image ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 via-black/15 to-transparent">
            <div className={`${CONTAINER} pb-16 lg:pb-20`}>
              {active.title ? (
                <h1 className="max-w-2xl text-4xl font-extrabold leading-[1.35] tracking-normal text-white lg:text-6xl">
                  {active.title}
                </h1>
              ) : null}
              {active.subtitle ? (
                <p className="mt-3 max-w-xl text-sm leading-8 text-white/85 lg:text-base">{active.subtitle}</p>
              ) : null}
            </div>
          </div>
        ) : null}
        {count > 1 ? (
          <div className="absolute bottom-5 left-1/2 z-10 flex -translate-x-1/2 items-center">
            {slides.map((s, i) => (
              <button
                key={`${s.image || s.title || ''}-dot-${i}`}
                type="button"
                aria-label={`اسلاید ${toPersianDigits(i + 1)}`}
                onClick={() => setIndex(i)}
                className={`flex h-11 w-11 items-center justify-center ${FOCUS_RING}`}
              >
                <span
                  className={`h-1.5 rounded-full transition-all duration-300 ${
                    i === activeIndex ? 'w-7 bg-white' : 'w-2.5 bg-white/45 hover:bg-white/75'
                  }`}
                />
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </section>
  );
};

// Macro category entrance: the first three category groups (with an
// image the ledger still has) as large tiles, then every remaining
// group as a chip. Tiles and chips are real navigation — each one
// selects its category in the store's own filter state and jumps to
// the catalogue floor. Groups without imagery never become tiles;
// they stay reachable as chips. On mobile three tiles lay out as
// two squares + one wide tile; on desktop they sit three-across.
const AastinMacroCategories: React.FC<{
  groups: CategoryGroup[];
  ledger: MediaLedger;
  onSelect: (slug: string) => void;
}> = ({ groups, ledger, onSelect }) => {
  const tiles: { group: CategoryGroup; image: string }[] = [];
  for (const group of groups) {
    if (tiles.length >= 3) break;
    const image = peekUnusedImage(ledger, [group.image, ...group.products.map((p) => p.image)]);
    if (image) {
      claimImage(ledger, image);
      tiles.push({ group, image });
    }
  }
  tiles.forEach((t) => ledger.macroTaken.add(t.group.slug));
  const chips = groups.filter((g) => !ledger.macroTaken.has(g.slug));
  if (tiles.length === 0 && chips.length === 0) return null;
  const gridCls = tiles.length <= 1 ? 'grid-cols-1' : tiles.length === 2 ? 'grid-cols-2' : 'grid-cols-2 lg:grid-cols-3';
  const tileAspect = (i: number): string => {
    if (tiles.length <= 1) return 'aspect-[16/10] lg:aspect-[21/9]';
    if (tiles.length === 3 && i === 2) return 'aspect-[16/10] lg:aspect-square';
    return 'aspect-square';
  };
  const tileSpan = (i: number): string => (tiles.length === 3 && i === 2 ? 'col-span-2 lg:col-span-1' : '');
  return (
    <section data-testid="store-category-macro" className="space-y-6">
      {tiles.length > 0 ? (
        <div className={`grid gap-3 lg:gap-4 ${gridCls}`}>
          {tiles.map(({ group, image }, i) => (
            <a
              key={group.slug}
              href="#products"
              onClick={() => onSelect(group.slug)}
              className={`group block ${tileSpan(i)} ${FOCUS_RING}`}
            >
              <div className={`relative overflow-hidden rounded-[2px] bg-[#f2f2f2] ${tileAspect(i)}`}>
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
      ) : null}
      {chips.length > 0 ? (
        <div className="flex flex-wrap justify-center gap-2">
          {chips.map((group) => (
            <a
              key={group.slug}
              href="#products"
              onClick={() => onSelect(group.slug)}
              className={`inline-flex min-h-[44px] items-center rounded-full border border-[#e5e0d8] bg-white px-4 text-xs font-bold text-[#1c1c1a] transition hover:border-[#1c1c1a] ${FOCUS_RING}`}
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
// large square image (config aboutImage). Text source: config
// aboutText, else the store's own description / shortDescription —
// never the tagline, which already appears in the announcement and
// the hero. No text → the block hides. A single short line without
// an image renders as a compact one-line intro, not the full split.
const AastinStory: React.FC<{ store: Store; config: TemplateConfig }> = ({ store, config }) => {
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
        <AastinSectionHeader title={title} />
        <p className="mt-3 text-[15px] leading-8 text-[#737373]">{paragraphs[0]}</p>
      </section>
    );
  }
  const prose = (
    <div className="max-w-[560px]">
      <AastinSectionHeader title={title} />
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

// «دسته‌بندی‌ها» tile grid: square tiles on pale grey, label centred
// underneath, each tile a real link into the filtered catalogue
// floor. The caller passes only the groups the macro entrance did
// not take (capped at 3 or 6), and groups whose imagery the ledger
// has exhausted drop out here — a category never renders twice on
// the page and never as an image-less placeholder tile.
const AastinCategoryGrid: React.FC<{
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
      <AastinSectionHeader title={title} />
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

// Collections rail: each card is a mosaic of DISTINCT product images
// from one category group — never padded with the category image.
// Four distinct images form the 2×2 mosaic; three form one wide
// tile over two squares; two sit as equal tiles; below two distinct
// unused images the group is skipped — a group renders only a
// complete mosaic or not at all. Caption is the real category
// name and its real product count. When exactly one group survives
// the ledger, its mosaic renders as a centred spotlight at a
// capped width instead of a lone rail card in an empty row.
const AastinCollections: React.FC<{
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
      <AastinSectionHeader title={title} />
      {cards.length === 1 ? (
        // A lone surviving mosaic is a spotlight, not a rail item:
        // centred at a capped width, so the section never reads as
        // a broken grid whose remaining cells sit empty.
        <div className="mx-auto w-full max-w-[680px]">{renderCard(cards[0], 'w-full')}</div>
      ) : (
        <div className="flex snap-x gap-4 overflow-x-auto pb-2 no-scrollbar">
          {cards.map((card) => renderCard(card, 'w-[270px] shrink-0 snap-start lg:w-[350px]'))}
        </div>
      )}
    </section>
  );
};

// Editorial lookbook band: the configured lookbook image as a tall
// full-width still with the editorial title/subtitle overlaid,
// wrapped in its link when set. Without an image, title + subtitle
// render as a quiet pale panel; with neither, the band hides. The
// lookbookTitle lives ONLY here — the collections mosaic carries
// its own collectionsTitle.
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
          {title ? (
            <p className="text-[30px] font-extrabold leading-[1.35] tracking-normal text-white lg:text-[48px]">{title}</p>
          ) : null}
          {subtitle ? <p className="mt-2 max-w-xl text-sm leading-8 text-white/85">{subtitle}</p> : null}
        </div>
      ) : null}
    </div>
  ) : (
    <div className="rounded-[2px] bg-[#f2f2f2] px-6 py-10 text-center lg:py-14">
      {title ? (
        <p className="text-[30px] font-extrabold leading-[1.35] tracking-normal text-[#1c1c1a] lg:text-[48px]">{title}</p>
      ) : null}
      {subtitle ? <p className="mx-auto mt-3 max-w-xl text-sm leading-8 text-[#737373]">{subtitle}</p> : null}
    </div>
  );
  return (
    <section data-testid="store-lookbook">
      {link ? (
        <a href={link} {...externalLinkProps(link)} className={`block ${FOCUS_RING}`}>
          {body}
        </a>
      ) : (
        body
      )}
    </section>
  );
};

// Brand tiles: distinct real product brands, each represented by
// a real product image the ledger still
// has, label centred underneath; tiles are real links into the
// brand-filtered catalogue floor. A brand without an available
// image appears as a text chip instead of a placeholder tile. The
// section needs at least two brands to exist at all — a single
// brand no longer gets a whole section to itself.
const AastinBrandTiles: React.FC<{
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
      <AastinSectionHeader title={title} />
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

// Large sharp banner tiles: one banner spans wide, two or more sit
// in a pair grid, and an odd last banner spans the full row on a
// cinematic ratio. A banner's title, when the seller set one, sits
// centred underneath — never overlaid.
const AastinBanners: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => {
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

// Featured image cluster: featured products as pure imagery — the
// first spans large, the rest sit in squares; exactly four picks
// form a clean 2×2 instead. Editorial sections carry NO prices and
// NO badges; clicking a tile opens the product, like every product
// surface. The caller passes only picks whose image the ledger
// still has, so the cluster never repeats an earlier module.
const AastinSpotlight: React.FC<{
  products: Product[];
  title: string;
  onOpen: (p: Product) => void;
}> = ({ products, title, onOpen }) => {
  if (products.length === 0) return null;
  const four = products.length === 4;
  return (
    <section data-testid="store-featured" className="space-y-6 lg:space-y-8">
      <AastinSectionHeader title={title} />
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

// Testimonials: the seller-configured customer quotes are the only
// editorial text source the platform has, so they render as quiet
// quote cards under their own «نظرات مشتریان» heading — never dressed
// up as a magazine. Nothing configured → the section does not exist.
const AastinTestimonials: React.FC<{ config: TemplateConfig; title: string }> = ({ config, title }) => {
  if (!sectionEnabled(config, 'testimonials')) return null;
  const items = (templateCustom(config).testimonials || []).filter(
    (t) => t && typeof t.text === 'string' && t.text.trim() !== '',
  );
  if (items.length === 0) return null;
  return (
    <section data-testid="store-testimonials" className="space-y-6 lg:space-y-8">
      <AastinSectionHeader title={title} />
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

// Fashion-native FAQ: the template's H2 token over hairline-
// separated rows — no white cards, no shadow, no accent bar. The
// items come from exactly the data source the shared FAQSection
// uses: the seller's configured faqItems win; otherwise the
// platform-wide FAQ from /static-pages.
const AastinFaq: React.FC<{ config: TemplateConfig }> = ({ config }) => {
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
      <AastinSectionHeader title="پرسش‌های متداول" />
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

// Catalogue-floor card, styled like aassttiinn's listing cards:
// square tile on pale grey, bold name, muted brand line, price below.
// This is the ONLY card on the page that shows a price. A product
// without an image keeps a plain tile square with a small
// «بدون تصویر» note — never a giant placeholder letter.
const AastinProductCard: React.FC<{ product: Product; onOpen: (p: Product) => void }> = ({ product, onOpen }) => (
  <button type="button" onClick={() => onOpen(product)} className={`group block w-full cursor-pointer text-right ${FOCUS_RING}`}>
    <div className="relative aspect-square overflow-hidden rounded-[2px] bg-[#f2f2f2]">
      {product.image ? (
        <img
          src={product.image}
          alt={product.title}
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
        />
      ) : (
        <span className="absolute inset-0 flex items-center justify-center text-[11px] font-bold text-[#737373]">
          بدون تصویر
        </span>
      )}
    </div>
    <p className="mt-3 line-clamp-2 text-sm font-bold leading-6 text-[#1c1c1a]">{product.title}</p>
    {product.brand ? (
      <p className="mt-0.5 truncate text-xs leading-5 text-[#737373]">{product.brand}</p>
    ) : null}
    <p className="mt-1.5 text-sm font-black text-[#1c1c1a]">
      {formatPrice(cardPriceOf(product).price)} <span className="text-xs font-bold text-[#737373]">تومان</span>
    </p>
  </button>
);

// Dark charcoal grey-green newsletter band (#3a4441), full-bleed:
// title + subtitle on one side, an underline-style mobile input and
// a sharp ink-on-cream submit on the other. The subscribe contract
// is the platform's own (mobile number + store slug, same endpoint
// and validation as the shared NewsletterSection).
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
  const slides = heroSlidesOf(cfg);
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
  // Media ledger: rebuilt per render, claimed in render order.
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

  // Catalogue-floor filter state: the storefront's own mechanisms —
  // the category filter the store category tiles set, and the brand
  // request a brand click raises. A filter only applies when it
  // matches this store's own catalogue; the floor heading names the
  // active filter and offers the way back to the full floor.
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
  // The store's own search box writes the shared searchQuery; it
  // composes with the brand/category filters above over this
  // store's items, using the legacy grid's title/description
  // predicate, and the heading names the search while it is on.
  const searching = !!searchQuery;
  const floorItems = searching
    ? categoryItems.filter((p) => p.title.includes(searchQuery) || p.description.includes(searchQuery))
    : categoryItems;
  const floorTitle = searching ? `جستجو: «${searchQuery}»` : brandFilter || (selectedGroup ? selectedGroup.title : productsTitle);
  const clearFloorFilter = (): void => {
    if (brandFilter) clearCatalogBrand();
    if (selectedGroup) setSelectedCategory('all');
    if (searching) setSearchQuery('');
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return <AastinAnnouncement store={store} config={cfg} />;
      case 'hero':
      case 'heroSlides':
        if (!once('hero')) return null;
        slides.forEach((s) => claimImage(ledger, (s.image || '').trim()));
        return (
          <AastinHero
            slides={slides}
            autoplay={custom.heroAutoplay}
            title={heroTitle}
            subtitle={heroSubtitle}
            ctaText={heroCtaText}
            ctaLink={heroCtaLink}
          />
        );
      case 'categoryRail':
        return (
          <Wrap>
            <AastinMacroCategories groups={categoryGroups} ledger={ledger} onSelect={setSelectedCategory} />
          </Wrap>
        );
      case 'about': {
        claimImage(ledger, (custom.aboutImage || '').trim());
        return (
          <Wrap>
            <AastinStory store={store} config={cfg} />
          </Wrap>
        );
      }
      case 'categoryGrid': {
        // Only the groups the macro entrance did not take, capped to
        // complete rows of three (3 or 6). When ≤3 groups exist and
        // the macro entrance is on, it takes them all and this grid
        // simply has nothing left to show.
        const remainder = categoryGroups.filter((g) => !ledger.macroTaken.has(g.slug));
        let limit = Math.min(remainder.length, customCount(cfg, 'categoriesCount', 6));
        if (limit > 6) limit = 6;
        if (limit === 4 || limit === 5) limit = 3;
        return (
          <Wrap>
            <AastinCategoryGrid
              groups={remainder.slice(0, limit)}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')}
              ledger={ledger}
              onSelect={setSelectedCategory}
            />
          </Wrap>
        );
      }
      case 'collections':
        return (
          <Wrap>
            <AastinCollections
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
            <AastinLookbookBand config={cfg} />
          </Wrap>
        );
      }
      case 'brands':
        return (
          <Wrap>
            <AastinBrandTiles
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
        banners.forEach((b) => claimImage(ledger, (b.image || '').trim()));
        return (
          <Wrap>
            <AastinBanners banners={banners} />
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
            <AastinSpotlight products={picks} title={customText(cfg, 'featuredTitle', 'کالاهای منتخب')} onOpen={openProductDetail} />
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
        // Template-identity whitelist: the shared app-style sections
        // never render inside tpl-fashion-1, even when a saved config
        // enables them. The seller panel still offers them for the
        // other templates.
        return null;
      case 'faq':
        return (
          <Wrap>
            <AastinFaq config={cfg} />
          </Wrap>
        );
      case 'testimonials':
        return (
          <Wrap>
            <AastinTestimonials config={cfg} title={testimonialsTitle} />
          </Wrap>
        );
      case 'products':
      case 'allProducts': {
        if (!once('products')) return null;
        if (floorItems.length === 0 && !searching) return null;
        const count = floorItems.length;
        const gridCls =
          count === 1 ? 'grid-cols-1' : count === 2 ? 'grid-cols-2' : count === 3 ? 'grid-cols-2 lg:grid-cols-3' : 'grid-cols-2 lg:grid-cols-4';
        const wrapCls = count === 1 ? 'mx-auto w-full max-w-[380px]' : count === 2 ? 'mx-auto w-full max-w-[880px]' : '';
        return (
          <Wrap>
            <section id="products" data-testid="store-products" className="space-y-6 lg:space-y-8">
              <div className="flex items-center justify-between gap-4">
                <AastinSectionHeader title={floorTitle} />
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
              {floorItems.length === 0 ? (
                <p className="py-6 text-center text-xs font-bold text-[#737373]">کالایی با این مشخصات یافت نشد.</p>
              ) : (
                <div className={wrapCls}>
                  <div className={`grid gap-x-3 gap-y-8 lg:gap-x-4 ${gridCls}`}>
                    {floorItems.map((p) => (
                      <AastinProductCard key={p.id} product={p} onOpen={openProductDetail} />
                    ))}
                  </div>
                </div>
              )}
            </section>
          </Wrap>
        );
      }
      case 'newsletter':
        return <AastinNewsletter config={cfg} storeSlug={store.slug} />;
      default:
        return null;
    }
  };

  // Announcement + hero form ONE structural stack: when the resolved
  // order places them adjacently they render inside a single flex
  // child, so the page rhythm gap never separates the bar from the
  // hero it belongs to (this replaces the old negative-margin hack).
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
      className={`flex-1 bg-[#faf8f5] text-[#1c1c1a] ${lastKey === 'newsletter' ? '' : 'pb-14 lg:pb-24'}`}
      style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}
    >
      <div className="flex flex-col gap-14 lg:gap-24">{nodes}</div>
    </div>
  );
};
export default FashionTemplate;
