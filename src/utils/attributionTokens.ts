/**
 * Attribution Tokens & Privacy Separation Utility
 * Priority 1: Separates Ad Banner Click Tracking (CPC) from Persistent Store Sales Attribution (CPS)
 * 
 * - `ad_token`: Used exclusively for CPC banner/widget click accounting, short-lived, never assigns store order commissions
 * - `ref`: Used exclusively for CPS store referral attribution, persistent in localStorage, tracks store sales commissions
 */

export interface ParsedAttributionParams {
  adToken: string | null;
  storeRef: string | null;
  campaignTag: string | null;
}

export const generateAdToken = (sellerSlug: string, campaignTag: string = 'banner'): string => {
  const nonce = Math.floor(1000 + Math.random() * 9000);
  return `ad_${sellerSlug}_${campaignTag}_${nonce}`;
};

export const generateStoreRefToken = (sellerSlug: string): string => {
  return sellerSlug.trim().toLowerCase();
};

export const parseAttributionParams = (search: string): ParsedAttributionParams => {
  if (!search) {
    return { adToken: null, storeRef: null, campaignTag: null };
  }
  const cleanSearch = search.startsWith('?') ? search.slice(1) : search;
  const params = new URLSearchParams(cleanSearch);

  return {
    adToken: params.get('ad_token'),
    storeRef: params.get('ref'),
    campaignTag: params.get('utm_source') || params.get('campaign'),
  };
};

export const buildAdBannerLink = (targetUrl: string, sellerSlug: string, campaignTag: string = 'html_banner'): string => {
  const token = generateAdToken(sellerSlug, campaignTag);
  const separator = targetUrl.includes('?') ? '&' : '?';
  return `${targetUrl}${separator}ad_token=${token}&utm_source=${campaignTag}`;
};

export const buildStoreReferralLink = (storeSlug: string, campaignTag: string = 'bio'): string => {
  return `https://sellershow.ir/store/${storeSlug}?ref=${storeSlug}&utm_source=${campaignTag}`;
};
