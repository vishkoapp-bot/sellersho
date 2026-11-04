import React, { useCallback, useEffect, useRef, useState } from 'react';

// FIX 13: faithful clone of live's dual-thumb price slider (bundle component
// Qy). Log-scale over the price bounds (linear when the lower bound is
// <= 0, like live), RTL-aware pointer math (fraction measured from the
// right), two thumbs with role="slider" and data-thumb="min"/"max",
// Persian-digit textboxes with حداقل/حداکثر labels, edit-on-blur.
interface PriceRangeSliderProps {
  boundMin: number;
  boundMax: number;
  value: [number, number];
  onChange: (value: [number, number]) => void;
  showHeader?: boolean;
}

const FA_DIGITS = '۰۱۲۳۴۵۶۷۸۹';

const toFa = (n: number): string => {
  const grouped = Math.round(n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, '٬');
  return grouped.replace(/\d/g, (d) => FA_DIGITS[Number(d)]);
};

const parseFa = (raw: string): number => {
  const latin = raw
    .replace(/[۰-۹]/g, (d) => String(FA_DIGITS.indexOf(d)))
    .replace(/[٠-٩]/g, (d) => String('٠١٢٣٤٥٦٧٨٩'.indexOf(d)));
  const digits = latin.replace(/[^0-9]/g, '');
  return digits === '' ? NaN : Number(digits);
};

const round1000 = (v: number) => Math.round(v / 1000) * 1000;

export const PriceRangeSlider: React.FC<PriceRangeSliderProps> = ({
  boundMin,
  boundMax,
  value,
  onChange,
  showHeader = true,
}) => {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragThumb = useRef<'min' | 'max' | null>(null);
  const [activeThumb, setActiveThumb] = useState<'min' | 'max' | null>(null);
  const [draftMin, setDraftMin] = useState<string | null>(null);
  const [draftMax, setDraftMax] = useState<string | null>(null);

  const [lo, hi] = value;
  const span = boundMax - boundMin;

  const frac = useCallback(
    (v: number): number => {
      if (span <= 0) return 0;
      if (boundMin <= 0) return Math.min(1, Math.max(0, (v - boundMin) / span));
      const f = (Math.log(Math.max(v, 1)) - Math.log(boundMin)) / (Math.log(boundMax) - Math.log(boundMin));
      return Math.min(1, Math.max(0, f));
    },
    [boundMin, boundMax, span],
  );

  const valueFromFrac = useCallback(
    (f: number): number => {
      const c = Math.min(1, Math.max(0, f));
      if (boundMin <= 0) return round1000(boundMin + c * span);
      return round1000(Math.exp(Math.log(boundMin) + c * (Math.log(boundMax) - Math.log(boundMin))));
    },
    [boundMin, boundMax, span],
  );

  const fracMin = frac(lo);
  const fracMax = frac(hi);

  const fracFromClientX = useCallback((clientX: number): number => {
    const el = trackRef.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return 0;
    // RTL: fraction grows from the right edge, like live.
    return 1 - Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
  }, []);

  const applyFrac = useCallback(
    (thumb: 'min' | 'max', f: number) => {
      const v = valueFromFrac(f);
      if (thumb === 'min') onChange([Math.min(v, hi), hi]);
      else onChange([lo, Math.max(v, lo)]);
    },
    [hi, lo, onChange, valueFromFrac],
  );

  const handleTrackPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement;
    const thumb = (target.dataset?.thumb as 'min' | 'max' | undefined) ?? null;
    const f = fracFromClientX(e.clientX);
    const chosen: 'min' | 'max' =
      thumb ?? (Math.abs(f - fracMin) <= Math.abs(f - fracMax) ? 'min' : 'max');
    dragThumb.current = chosen;
    setActiveThumb(chosen);
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    applyFrac(chosen, f);
  };

  const handleTrackPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!dragThumb.current) return;
    applyFrac(dragThumb.current, fracFromClientX(e.clientX));
  };

  const endDrag = () => {
    dragThumb.current = null;
    setActiveThumb(null);
  };

  const handleThumbKeyDown = (thumb: 'min' | 'max') => (e: React.KeyboardEvent) => {
    const step = Math.max(1000, round1000(span / 200));
    let next: number | null = null;
    // RTL: ArrowLeft/Up increase, ArrowRight/Down decrease (live's mapping).
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (thumb === 'min' ? lo : hi) + step;
    else if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (thumb === 'min' ? lo : hi) - step;
    else if (e.key === 'Home') next = boundMin;
    else if (e.key === 'End') next = boundMax;
    if (next === null) return;
    e.preventDefault();
    const v = Math.min(boundMax, Math.max(boundMin, round1000(next)));
    if (thumb === 'min') onChange([Math.min(v, hi), hi]);
    else onChange([lo, Math.max(v, lo)]);
  };

  const commitDraft = (which: 'min' | 'max') => {
    const draft = which === 'min' ? draftMin : draftMax;
    if (draft === null) return;
    if (which === 'min') setDraftMin(null);
    else setDraftMax(null);
    const parsed = parseFa(draft);
    if (Number.isNaN(parsed)) return;
    const v = Math.min(boundMax, Math.max(boundMin, round1000(parsed)));
    if (which === 'min') onChange([Math.min(v, hi), hi]);
    else onChange([lo, Math.max(v, lo)]);
  };

  // Keep drafts in sync when the value changes from outside (e.g. slider drag).
  useEffect(() => {
    setDraftMin(null);
    setDraftMax(null);
  }, [lo, hi]);

  const thumbClass = (thumb: 'min' | 'max') =>
    `absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-5 h-5 rounded-full bg-white border-2 shadow cursor-grab touch-none focus:outline-hidden focus:ring-2 focus:ring-brand/40 z-10 ${
      activeThumb === thumb ? 'border-brand-deep cursor-grabbing scale-110' : 'border-brand'
    }`;

  return (
    <div className="w-full space-y-2.5" aria-label="محدوده قیمت">
      {showHeader && (
        <div className="flex items-center justify-between">
          <span className="text-[11px] lg:text-xs font-bold text-slate-500">محدوده قیمت</span>
          <span className="text-[10px] lg:text-[11px] text-slate-400">تومان</span>
        </div>
      )}
      <div
        ref={trackRef}
        className="relative h-6 flex items-center select-none"
        onPointerDown={handleTrackPointerDown}
        onPointerMove={handleTrackPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <div className="absolute inset-x-0 h-1.5 rounded-full bg-slate-200" />
        <div
          className="absolute h-1.5 rounded-full bg-brand"
          style={{ left: `${(1 - fracMax) * 100}%`, right: `${fracMin * 100}%` }}
        />
        <div
          role="slider"
          tabIndex={0}
          data-thumb="min"
          aria-label="حداقل قیمت"
          aria-valuemin={boundMin}
          aria-valuemax={boundMax}
          aria-valuenow={lo}
          aria-valuetext={`${toFa(lo)} تومان`}
          className={thumbClass('min')}
          style={{ left: `${(1 - fracMin) * 100}%` }}
          onKeyDown={handleThumbKeyDown('min')}
        />
        <div
          role="slider"
          tabIndex={0}
          data-thumb="max"
          aria-label="حداکثر قیمت"
          aria-valuemin={boundMin}
          aria-valuemax={boundMax}
          aria-valuenow={hi}
          aria-valuetext={`${toFa(hi)} تومان`}
          className={thumbClass('max')}
          style={{ left: `${(1 - fracMax) * 100}%` }}
          onKeyDown={handleThumbKeyDown('max')}
        />
      </div>
      <div className="flex items-start gap-2">
        <label className="flex-1 min-w-0 block">
          <span className="block text-[10px] text-slate-400 mb-1">حداقل قیمت (تومان)</span>
          <input
            type="text"
            inputMode="numeric"
            dir="ltr"
            aria-label="حداقل قیمت (تومان)"
            className="w-full min-w-0 px-2 py-1.5 min-h-[36px] rounded-lg bg-white border border-slate-200 text-slate-700 text-[11px] lg:text-xs font-bold text-center outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
            value={draftMin ?? toFa(lo)}
            onChange={(e) => setDraftMin(e.target.value)}
            onBlur={() => commitDraft('min')}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            }}
          />
        </label>
        <label className="flex-1 min-w-0 block">
          <span className="block text-[10px] text-slate-400 mb-1">حداکثر قیمت (تومان)</span>
          <input
            type="text"
            inputMode="numeric"
            dir="ltr"
            aria-label="حداکثر قیمت (تومان)"
            className="w-full min-w-0 px-2 py-1.5 min-h-[36px] rounded-lg bg-white border border-slate-200 text-slate-700 text-[11px] lg:text-xs font-bold text-center outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
            value={draftMax ?? toFa(hi)}
            onChange={(e) => setDraftMax(e.target.value)}
            onBlur={() => commitDraft('max')}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            }}
          />
        </label>
      </div>
    </div>
  );
};
