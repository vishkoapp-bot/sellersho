import React, { useState, useEffect } from 'react';
import { ChevronDown, CreditCard, Truck, ShieldCheck, RotateCcw, Headphones } from 'lucide-react';
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

  const services = features.slice(0, 4);
  const describedServices = services.filter(item => item.description?.trim());

  return (
    <section className="central-trust" aria-label="خدمات فروشگاه">
      <ul role="list">
        {services.map(item => {
          const Icon = ICONS[item.icon] || ShieldCheck;
          return (
            <li key={item.title}>
              <Icon className="size-5" strokeWidth={1.6} aria-hidden="true" />
              <span>{item.title}</span>
            </li>
          );
        })}
      </ul>
      {describedServices.length > 0 && (
        <details>
          <summary>جزئیات خدمات <ChevronDown size={16} aria-hidden="true" /></summary>
          <dl>
            {describedServices.map(item => (
              <div key={item.title}>
                <dt>{item.title}</dt>
                <dd>{item.description}</dd>
              </div>
            ))}
          </dl>
        </details>
      )}
    </section>
  );
};
