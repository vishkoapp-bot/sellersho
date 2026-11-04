import React from 'react';
import { useStore } from '../../context/StoreContext';

// ISO-A (user-ordered storefront separation): this page renders ONLY
// this store's own description. The previous version filled an empty
// description with a fabricated generic text, added a second paragraph
// claiming every order ships via Pishtaz/registered freight, and showed
// four trust badges (authenticity, fast post, 7-day test, after-sales)
// for every store regardless of that store's actual policies. None of
// those strings appear in the live bundle (grep over /opt/drup/dist
// assets: zero hits for the fallback sentence), so they are candidate
// inventions, not live parity. The template path already self-hides
// its about section when there is no text (StoreSections AboutSection);
// this legacy tab keeps a neutral empty state instead of inventing one.
export const SellerStorefrontAbout: React.FC = () => {
  const { currentStore } = useStore();
  const description = (currentStore.description || '').trim();

  return (
    <div className="px-3.5 py-4 space-y-3.5">
      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-3">
        <h2 className="text-sm font-bold text-slate-800">
          درباره فروشگاه {currentStore.name}
        </h2>
        {description !== '' ? (
          <p className="text-xs text-slate-600 leading-relaxed">
            {description}
          </p>
        ) : (
          <p className="text-xs text-slate-500 leading-relaxed">
            برای این فروشگاه هنوز توضیحی ثبت نشده است.
          </p>
        )}
      </div>
    </div>
  );
};
