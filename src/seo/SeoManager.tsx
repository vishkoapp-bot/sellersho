import React, { useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { setDocumentHead, headForView } from './seo';

// Team 08 live parity (D65): runtime SEO manager. Runs on every route
// change and updates document head via seo.ts. OG tags stay brand-neutral
// for /store/* (see seo.ts note): scrapers do not run JS, so per-store OG
// cannot be served by this SPA without SSR.
export const SeoManager: React.FC = () => {
  const { activeView, currentStore, currentProductDetail, storefrontSlug, storefrontSuspended, storefrontInvalid } = useStore();
  useEffect(() => {
    // FIX 15/16: a suspended or nonexistent storefront renders live's
    // 404 page (ek for /store/*), so its tab title must be live's
    // 404 title, not the store name.
    setDocumentHead(
      headForView(activeView === 'seller_storefront' && (storefrontSuspended || storefrontInvalid) ? 'not_found' : activeView, {
        storeName: activeView === 'seller_storefront' ? currentStore?.name || storefrontSlug || undefined : undefined,
        storeTagline: activeView === 'seller_storefront' ? currentStore?.tagline || undefined : undefined,
        productTitle: activeView === 'product_detail' ? currentProductDetail?.title : undefined,
      }),
    );
  }, [activeView, currentStore, currentProductDetail, storefrontSlug, storefrontSuspended, storefrontInvalid]);
  return null;
};
