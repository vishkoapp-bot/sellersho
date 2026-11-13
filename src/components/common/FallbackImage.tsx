import React, { useEffect, useRef, useState } from 'react';

// P1-12/13/14: ONE shared image-fallback pattern, standardised on the
// codebase's own correct implementations (FashionProductCard's
// state-driven fallthrough, DealProductCard's state-driven Package
// fallback, SellerStorefrontHero's imgFailed state).
//
// Behaviour:
// - `srcs` are candidate URLs in priority order; the first that loads wins.
// - Failures are tracked in state BY SRC STRING, never by hiding the
//   element (no display:none) and never by retrying a known-broken URL
//   (no ?retry=1, no setTimeout, no second request while `srcs` is unchanged).
// - When every src has failed (or `srcs` is empty after filtering
//   blanks), the designed `fallback` node renders — never an <img>.
// - The failed set resets whenever the `srcs` list changes (keyed on
//   srcs.join('|'), same technique as the gallery's galleryKey), so a
//   URL that failed for a previous product/variant is retried for the
//   next one. Failure state is component-local, never persisted.
// - `onLoad` fires when a candidate loads; `onExhausted` fires once
//   (on the transition) when no candidate remains, so a parent can
//   hide chrome that only makes sense with a loaded image (zoom
//   button, counter, arrows). Extra img props (data-testid, aria-*, …)
//   are forwarded to the <img>.

interface FallbackImageProps
  extends Omit<React.ImgHTMLAttributes<HTMLImageElement>, 'src' | 'srcSet' | 'onError' | 'onLoad'> {
  srcs: (string | undefined | null)[];
  alt: string;
  fallback: React.ReactNode;
  onLoad?: () => void;
  onExhausted?: () => void;
}

export const FallbackImage: React.FC<FallbackImageProps> = ({
  srcs,
  alt,
  fallback,
  loading = 'lazy',
  decoding = 'async',
  onLoad,
  onExhausted,
  ...imgProps
}) => {
  const candidates = Array.from(
    new Set(srcs.filter((s): s is string => typeof s === 'string' && s.trim().length > 0)),
  );
  const srcsKey = candidates.join('|');
  const [failedSrcs, setFailedSrcs] = useState<string[]>([]);
  useEffect(() => {
    setFailedSrcs([]);
  }, [srcsKey]);
  const current = candidates.find((src) => !failedSrcs.includes(src));
  const exhausted = !current;
  const exhaustedNotifiedRef = useRef(false);
  useEffect(() => {
    if (exhausted) {
      if (!exhaustedNotifiedRef.current) {
        exhaustedNotifiedRef.current = true;
        onExhausted?.();
      }
    } else {
      exhaustedNotifiedRef.current = false;
    }
  });
  if (exhausted) return <>{fallback}</>;
  return (
    <img
      {...imgProps}
      src={current}
      alt={alt}
      loading={loading}
      decoding={decoding}
      onLoad={() => onLoad?.()}
      onError={() =>
        setFailedSrcs((prev) => (prev.includes(current) ? prev : [...prev, current]))
      }
    />
  );
};
