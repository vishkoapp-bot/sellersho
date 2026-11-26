import React, { useState, useEffect } from 'react';
import { CreditCard, Truck, ShieldCheck, RotateCcw, Headphones } from 'lucide-react';
import { siteContentApi, FALLBACK_TRUST_FEATURES, TrustFeature } from '../services/siteContentApi';

const ICONS: Record<string, React.ComponentType<{ className?: string; strokeWidth?: number | string }>> = {
  CreditCard,
  Truck,
  ShieldCheck,
  RotateCcw,
  Headphones,
};

export const TrustFeatures: React.FC = () => {
  const [features, setFeatures] = useState<TrustFeature[]>(FALLBACK_TRUST_FEATURES);

  useEffect(() => {
    let cancelled = false;
    siteContentApi.getTrustFeatures().then((list) => {
      if (!cancelled) setFeatures(list);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, []);

  return (
    <section className="central-trust" aria-label="خدمات فروشگاه">
      {features.slice(0, 4).map(item => {
        const Icon = ICONS[item.icon] || ShieldCheck;
        return (
          <div key={item.title}>
            <span aria-hidden="true"><Icon className="size-5" strokeWidth={1.6} /></span>
            <p><strong>{item.title}</strong><small>{item.description}</small></p>
          </div>
        );
      })}
    </section>
  );
};
