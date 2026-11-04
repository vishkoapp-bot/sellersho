import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storefrontProducts } from './templates/templateTypes';

// FIX 13: live's storefront hero (bundle component v4). Live's public
// store API strips templateConfig entirely, so the custom heroSlides
// staging holds for mone never reach live's storefront — live falls
// back to its p4 Modern defaults: accent #0D7377 and the shared default
// photo /static/uploads/template-banners/tb-tpl-modern-hero-1.webp.
// That file IS the spa/candle still life the browser described on all
// three live storefronts (verified by viewing the file: teal candle,
// serum dropper bottle, soap bar on a teal plate, eucalyptus in a vase,
// cup, linen pouch), and it serves 200 on live and on preview. The
// preview build prefixes this src with /preview (see the preview build
// step); candidate source keeps the plain /static path. Never
// substitute another photo: with no photo, live renders its decorative
// beige hero (the onError branch below), not a look-alike image.
const DEFAULT_HERO_IMAGE = '/static/uploads/template-banners/tb-tpl-modern-hero-1.webp';
const ACCENT = '#0D7377';

export const SellerStorefrontHero: React.FC = () => {
  const { currentStore, products } = useStore();
  const [imgFailed, setImgFailed] = useState(false);
  const items = storefrontProducts(currentStore, products);
  const hasProducts = items.length > 0;
  const tagline = currentStore.tagline || currentStore.shortDescription || '';

  if (imgFailed) {
    // Live's decorative no-photo hero (beige gradients, dot pattern,
    // giant initial letter, badge pill, 36px title).
    return (
      <section
        className="relative w-full overflow-hidden"
        aria-label="معرفی فروشگاه"
        style={{
          background: `radial-gradient(110% 75% at 82% 0%, ${ACCENT}2E, transparent 60%), radial-gradient(90% 65% at 12% 100%, ${ACCENT}1F, transparent 55%), linear-gradient(165deg, #FBF7EF 0%, #F6EFE2 60%, #F0E5D2 100%)`,
        }}
      >
        <div
          className="absolute inset-0 pointer-events-none"
          style={{
            backgroundImage: `radial-gradient(${ACCENT}30 1.1px, transparent 1.1px)`,
            backgroundSize: '22px 22px',
            opacity: 0.5,
          }}
        />
        <span
          className="absolute -left-2 -bottom-10 font-black leading-none select-none pointer-events-none"
          style={{ fontSize: '190px', color: `${ACCENT}14` }}
        >
          {(currentStore.name || '?').charAt(0)}
        </span>
        <div className="relative px-6 py-8">
          <span
            className="inline-flex items-center rounded-full px-3 py-1 text-[11px] font-bold"
            style={{ backgroundColor: `${ACCENT}1A`, color: ACCENT }}
          >
            ویترین اختصاصی
          </span>
          <h2 className="font-black text-[36px] leading-[1.3] tracking-tight mt-3" style={{ color: '#1C1917' }}>
            به فروشگاه ما خوش آمدید
          </h2>
          {tagline !== '' && <p className="text-[13px] text-stone-500 mt-2 leading-6 max-w-[320px]">{tagline}</p>}
        </div>
      </section>
    );
  }

  return (
    <section className="relative w-full overflow-hidden" aria-label="معرفی فروشگاه">
      <img
        src={DEFAULT_HERO_IMAGE}
        alt={currentStore.name}
        className="w-full h-[300px] object-cover"
        onError={() => setImgFailed(true)}
      />
      <div
        className="absolute inset-0"
        style={{
          background:
            'linear-gradient(to top, rgba(28,25,23,0.88) 0%, rgba(28,25,23,0.35) 55%, rgba(28,25,23,0.06) 100%)',
        }}
      />
      <div className="absolute inset-0 flex flex-col justify-end px-6 pb-7">
        <h2 className="text-white font-black text-[30px] leading-[1.3] tracking-tight drop-shadow-sm">
          به فروشگاه ما خوش آمدید
        </h2>
        {tagline !== '' && (
          <p className="text-white/85 text-[13px] mt-2 leading-6 line-clamp-2 max-w-[320px]">{tagline}</p>
        )}
        {hasProducts && (
          <div className="flex items-center gap-2 mt-4">
            <span
              className="inline-flex items-center gap-2 rounded-full px-7 py-3 text-sm font-bold text-white shadow-lg"
              style={{ backgroundColor: ACCENT }}
            >
              مشاهده محصولات
            </span>
          </div>
        )}
      </div>
    </section>
  );
};
