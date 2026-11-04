import { apiRequest } from './apiClient';

// Live banner shape: GET /api/v1/banners (public, active only) and
// GET /api/v1/banners?position=<hero|promo|tile|secondary>, ordered by
// sort_order. The live bundle fetches per position (getBanners(position)),
// and this module mirrors that exactly.
export interface ApiBanner {
  id: string;
  title: string;
  subtitle?: string | null;
  imageUrl: string;
  linkUrl?: string | null;
  position: string;
  sortOrder: number;
  isActive: boolean;
}

// Live title cleaning (live bundle `ms`): placeholder titles such as
// "بنر تبلیغاتی ۱" or "بنر کاشی‌ای ۲" are not display titles — live
// renders them as empty and shows only the banner image.
const GENERIC_TITLE = /^\s*(بنر\s+)?(کاشی‌?ای|تبلیغات(ی)?)(\s*[۰-۹0-9]+)?\s*$/;

export const cleanBannerTitle = (title?: string | null): string => {
  const t = (title || '').trim();
  if (!t || GENERIC_TITLE.test(t)) return '';
  return t;
};

// Live banner validity (`md`): a banner is usable when it carries an
// image, a subtitle, or a link — not merely when its title is non-empty.
export const isUsableBanner = (b: ApiBanner): boolean =>
  !!(b.imageUrl || '').trim() || !!(b.subtitle || '').trim() || !!(b.linkUrl || '').trim();

export const bannersApi = {
  getBanners: async (position: string): Promise<ApiBanner[]> => {
    const res = await apiRequest<{ banners: ApiBanner[] }>(
      `/banners?position=${encodeURIComponent(position)}`,
    );
    return (res.banners || []).filter((b) => b.isActive !== false);
  },
};
