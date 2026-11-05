// Team 08 live parity (D65): runtime meta manager.
// Live dist index.html references src/seo/SeoManager.tsx and src/seo/seo.ts
// (setDocumentHead). The source had no src/seo/ at all and a bare Vite
// index.html template.
// SEO NOTE (copied from the live dist comment, and binding here): the
// tags below are DELIBERATELY brand-neutral. This SPA is served for every
// URL — including /store/<slug> storefronts, which are independent
// businesses — and link scrapers (WhatsApp/Telegram) do not run
// JavaScript. Static brand-specific tags (og:site_name, brand-specific
// og:title, central-logo og:image) would mislabel every shared storefront
// link, so they are intentionally absent/neutral here. True per-URL OG
// for scrapers requires SSR, which is out of scope.

export interface SeoHead {
  title: string;
  description: string;
}

const DEFAULT_HEAD: SeoHead = {
  title: 'سلرشو | پلتفرم تأمین و انبارداری متمرکز کالا',
  description:
    'خرید آنلاین انواع کالای دیجیتال، مد و لوازم خانگی با مهلت تست و مرجوعی و ارسال به سراسر کشور.',
};

function upsertMeta(selector: string, content: string): void {
  if (typeof document === 'undefined') return;
  let el = document.querySelector<HTMLMetaElement>(selector);
  if (!el) {
    el = document.createElement('meta');
    const property = selector.match(/property="([^"]+)"/)?.[1];
    const name = selector.match(/name="([^"]+)"/)?.[1];
    if (property) el.setAttribute('property', property);
    if (name) el.setAttribute('name', name);
    document.head.appendChild(el);
  }
  el.setAttribute('content', content);
}

// setDocumentHead: runtime (post-JS) meta update only. It never writes
// og:site_name or a central-logo og:image, so a shared /store/<slug>
// link keeps the neutral tags from index.html for scrapers.
export function setDocumentHead(head: Partial<SeoHead> = {}): void {
  if (typeof document === 'undefined') return;
  const title = head.title || DEFAULT_HEAD.title;
  const description = head.description || DEFAULT_HEAD.description;
  document.title = title;
  upsertMeta('meta[name="description"]', description);
  upsertMeta('meta[property="og:title"]', 'فروشگاه اینترنتی');
  upsertMeta('meta[property="og:description"]', description);
  upsertMeta('meta[name="twitter:title"]', 'فروشگاه اینترنتی');
  upsertMeta('meta[name="twitter:description"]', description);
}

export const PLATFORM_THEME_COLOR = '#5d44f8';
export const PLATFORM_ICON_HREF = '/icon-192.png';

// Runtime store identity: while a store scope is active, the live
// tab's theme-color and favicon are the STORE's (template accent and
// logo); when the scope clears, the platform values from index.html
// are restored. This affects only the live tab - the scraper-facing
// OG tags stay neutral by design (see the note at the top of this
// file); it never writes og:site_name or a brand og:image.
export function setStoreIdentity(identity: { themeColor?: string; iconHref?: string } | null): void {
  if (typeof document === 'undefined') return;
  upsertMeta('meta[name="theme-color"]', identity?.themeColor || PLATFORM_THEME_COLOR);
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (!link) {
    link = document.createElement('link');
    link.setAttribute('rel', 'icon');
    document.head.appendChild(link);
  }
  link.setAttribute('href', identity?.iconHref || PLATFORM_ICON_HREF);
}

export function headForView(
  view: string,
  options: { storeName?: string; storeTagline?: string; storeDescription?: string; productTitle?: string } = {},
): SeoHead {
  if (view === 'seller_storefront' && options.storeName) {
    // Live uses the store tagline as the title suffix, not a generic
    // "online store" suffix.
    const suffix = options.storeTagline ? ` | ${options.storeTagline}` : '';
    return { title: `${options.storeName}${suffix}`, description: DEFAULT_HEAD.description };
  }
  if (view === 'product_detail' && options.productTitle) {
    if (options.storeName) {
      // Store-scoped PDP: the tab names the STORE - the business the
      // customer is buying from - and describes it with the store's
      // own text, not the platform's name and description.
      return {
        title: `${options.productTitle} | ${options.storeName}`,
        description: options.storeDescription || options.storeTagline || DEFAULT_HEAD.description,
      };
    }
    // FIX 12: live product title suffix is "| سلرشو".
    return { title: `${options.productTitle} | سلرشو`, description: DEFAULT_HEAD.description };
  }
  if (view === 'not_found') {
    // FIX 12: live 404 page title.
    return { title: 'صفحه یافت نشد | سلرشو', description: DEFAULT_HEAD.description };
  }
  return DEFAULT_HEAD;
}
