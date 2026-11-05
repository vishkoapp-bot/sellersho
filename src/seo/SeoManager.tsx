import React, { useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { setDocumentHead, headForView, setStoreIdentity } from './seo';
import { resolveTemplateAccent } from '../components/storefront/templates/TemplateRegistry';

// Team 08 live parity (D65): runtime SEO manager. Runs on every route
// change and updates document head via seo.ts. OG tags stay brand-neutral
// for /store/* (see seo.ts note): scrapers do not run JS, so per-store OG
// cannot be served by this SPA without SSR.
export const SeoManager: React.FC = () => {
  const { activeView, currentStore, currentProductDetail, storefrontSlug, storefrontSuspended, storefrontInvalid, routeStoreSlug } = useStore();
  // Store scope for identity purposes: the PDP's route scope, else a
  // dropshipper current store. The scoped store object is only used
  // once the context alignment has installed it as currentStore -
  // before that, identity stays platform-neutral rather than naming
  // the wrong store.
  const scopedSlug =
    routeStoreSlug ??
    (currentStore?.ownerType === 'dropshipper' && currentStore.slug ? currentStore.slug : null);
  const scopedStore =
    scopedSlug && currentStore?.slug === scopedSlug ? currentStore : undefined;
  useEffect(() => {
    // FIX 15/16: a suspended or nonexistent storefront renders live's
    // 404 page (ek for /store/*), so its tab title must be live's
    // 404 title, not the store name.
    setDocumentHead(
      headForView(activeView === 'seller_storefront' && (storefrontSuspended || storefrontInvalid) ? 'not_found' : activeView, {
        storeName:
          activeView === 'seller_storefront'
            ? currentStore?.name || storefrontSlug || undefined
            : activeView === 'product_detail'
              ? scopedStore?.name
              : undefined,
        storeTagline:
          activeView === 'seller_storefront'
            ? currentStore?.tagline || undefined
            : activeView === 'product_detail'
              ? scopedStore?.tagline || undefined
              : undefined,
        storeDescription:
          activeView === 'product_detail'
            ? scopedStore?.description || scopedStore?.shortDescription || undefined
            : undefined,
        productTitle: activeView === 'product_detail' ? currentProductDetail?.title : undefined,
      }),
    );
  }, [activeView, currentStore, currentProductDetail, storefrontSlug, storefrontSuspended, storefrontInvalid, scopedStore]);
  useEffect(() => {
    // Runtime theme-color + favicon follow the store scope (the
    // storefront home already names the store in its title; a scoped
    // PDP or store page must not keep the platform's violet tab and
    // platform icon). Scope cleared -> platform values restored.
    if (scopedStore) {
      setStoreIdentity({
        themeColor: resolveTemplateAccent(
          scopedStore.templateId || 'tpl-modern-1',
          (scopedStore.templateConfig as { accentColor?: string } | undefined)?.accentColor,
        ),
        iconHref: scopedStore.logo || undefined,
      });
    } else {
      setStoreIdentity(null);
    }
  }, [scopedStore]);
  return null;
};
