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
  /** A4: server-side 400 (inverted range) — renders the same inline message. */
  serverRangeError?: boolean;
}

// A4: single source of truth for the price-range error message — text
// validation, thumb/keyboard crossing and the server 400 all render this
// constant verbatim.
export const PRICE_RANGE_ERROR_MESSAGE = 'حداقل قیمت نمی‌تواند بیشتر از حداکثر قیمت باشد';

const FA_DIGITS = '۰۱۲۳۴۵۶۷۸۹';
const AR_DIGITS = '٠١٢٣٤٥٦٧٨٩';

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

// A4: digit characters only (Latin/Persian/Arabic), mapped to Latin — the
// live-formatting handler works on this; parseFa stays the commit side.
const digitsOnly = (raw: string): string => raw
  .replace(/[۰-۹]/g, (d) => String(FA_DIGITS.indexOf(d)))
  .replace(/[٠-٩]/g, (d) => String(AR_DIGITS.indexOf(d)))
  .replace(/[^0-9]/g, '');

// A4: position in `formatted` just after its `count`-th digit character,
// for caret restoration after live formatting inserts separators.
const caretAfterDigits = (formatted: string, count: number): number => {
  let seen = 0;
  for (let i = 0; i < formatted.length; i++) {
    if (/[۰-۹]/.test(formatted[i])) {
      seen++;
      if (seen === count) return i + 1;
    }
  }
  return formatted.length;
};

const round1000 = (v: number) => Math.round(v / 1000) * 1000;

export const PriceRangeSlider: React.FC<PriceRangeSliderProps> = ({
  boundMin,
  boundMax,
  value,
  onChange,
  showHeader = true,
  serverRangeError = false,
}) => {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragThumb = useRef<'min' | 'max' | null>(null);
  const minInputRef = useRef<HTMLInputElement>(null);
  const maxInputRef = useRef<HTMLInputElement>(null);
  const [activeThumb, setActiveThumb] = useState<'min' | 'max' | null>(null);
  const [draftMin, setDraftMin] = useState<string | null>(null);
  const [draftMax, setDraftMax] = useState<string | null>(null);
  // A4: which field the inline message is about ('min' | 'max'), or null.
  const [rangeError, setRangeError] = useState<'min' | 'max' | null>(null);
  // A4: last rejected draft, so a repeat rejection of the exact same
  // draft does not refocus the field a second time (focus-trap guard).
  const lastRejectedRef = useRef<{ which: 'min' | 'max'; draft: string } | null>(null);

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

  // A4: the single funnel for thumb/pointer/keyboard commits. The value
  // is clamped to the server bounds only; crossing the other bound stops
  // the thumb exactly at it ([hi, hi] / [lo, lo]) and sets the inline
  // message instead of silently rewriting the pair.
  const commitThumbValue = useCallback(
    (thumb: 'min' | 'max', v: number) => {
      const clamped = Math.min(boundMax, Math.max(boundMin, round1000(v)));
      if (thumb === 'min') {
        if (clamped > hi) { onChange([hi, hi]); setRangeError('min'); }
        else { onChange([clamped, hi]); setRangeError(null); }
      } else {
        if (clamped < lo) { onChange([lo, lo]); setRangeError('max'); }
        else { onChange([lo, clamped]); setRangeError(null); }
      }
    },
    [boundMin, boundMax, hi, lo, onChange],
  );

  const applyFrac = useCallback(
    (thumb: 'min' | 'max', f: number) => {
      commitThumbValue(thumb, valueFromFrac(f));
    },
    [commitThumbValue, valueFromFrac],
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
    commitThumbValue(thumb, next);
  };

  // A4: live formatting while typing (replaces format-on-blur). Only digit
  // characters survive; the draft is always the grouped Persian-digit
  // rendering, so raw Latin digits never flash. The caret is restored to
  // just after the same number of digits it preceded, once React commits.
  const handleDraftInput = (which: 'min' | 'max') => (e: React.ChangeEvent<HTMLInputElement>) => {
    const input = e.target;
    const raw = input.value;
    const sel = input.selectionStart ?? raw.length;
    const digitsBefore = digitsOnly(raw.slice(0, sel)).length;
    const digits = digitsOnly(raw);
    const setDraft = which === 'min' ? setDraftMin : setDraftMax;
    if (digits === '') { setDraft(''); return; }
    setDraft(toFa(Number(digits)));
    const ref = which === 'min' ? minInputRef : maxInputRef;
    requestAnimationFrame(() => {
      const el = ref.current;
      if (!el) return;
      const pos = caretAfterDigits(el.value, digitsBefore);
      el.setSelectionRange(pos, pos);
    });
  };

  // A4: invalid text commit — reject, don't rewrite. The draft stays
  // visible, the committed pair is unchanged (so no fetch fires), the
  // offending field is refocused once; a repeat rejection of the exact
  // same draft does not refocus again (the user Tabbed away deliberately).
  const rejectDraft = (which: 'min' | 'max', draft: string) => {
    setRangeError(which);
    const last = lastRejectedRef.current;
    if (last && last.which === which && last.draft === draft) return;
    lastRejectedRef.current = { which, draft };
    (which === 'min' ? minInputRef.current : maxInputRef.current)?.focus();
  };

  const commitDraft = (which: 'min' | 'max') => {
    const draft = which === 'min' ? draftMin : draftMax;
    if (draft === null) return;
    const clearDraft = () => { if (which === 'min') setDraftMin(null); else setDraftMax(null); };
    // Empty means "abandon edit": revert to the committed value, no
    // onChange, no error (unchanged behaviour).
    if (draft.trim() === '') { clearDraft(); return; }
    const parsed = parseFa(draft);
    if (Number.isNaN(parsed)) { clearDraft(); return; }
    const v = Math.min(boundMax, Math.max(boundMin, round1000(parsed)));
    if (which === 'min' && v > hi) { rejectDraft('min', draft); return; }
    if (which === 'max' && v < lo) { rejectDraft('max', draft); return; }
    clearDraft();
    setRangeError(null);
    lastRejectedRef.current = null;
    if (which === 'min') onChange([v, hi]);
    else onChange([lo, v]);
  };

  // Keep drafts in sync when the value changes from outside (e.g. slider drag).
  // A4: the local error clears only when the incoming value is the full
  // bounds (an external reset). A crossing drag commits [hi, hi] / [lo, lo]
  // and must NOT clear the message it just set, so never clear here
  // unconditionally.
  useEffect(() => {
    setDraftMin(null);
    setDraftMax(null);
    if (lo === boundMin && hi === boundMax) {
      setRangeError(null);
      lastRejectedRef.current = null;
    }
  }, [lo, hi, boundMin, boundMax]);

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
            ref={minInputRef}
            aria-invalid={rangeError === 'min' || undefined}
            aria-describedby={rangeError === 'min' || serverRangeError ? 'price-range-error' : undefined}
            className="w-full min-w-0 px-2 py-1.5 min-h-[36px] rounded-lg bg-white border border-slate-200 text-slate-700 text-[11px] lg:text-xs font-bold text-center outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
            value={draftMin ?? toFa(lo)}
            onChange={handleDraftInput('min')}
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
            ref={maxInputRef}
            aria-invalid={rangeError === 'max' || undefined}
            aria-describedby={rangeError === 'max' || serverRangeError ? 'price-range-error' : undefined}
            className="w-full min-w-0 px-2 py-1.5 min-h-[36px] rounded-lg bg-white border border-slate-200 text-slate-700 text-[11px] lg:text-xs font-bold text-center outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
            value={draftMax ?? toFa(hi)}
            onChange={handleDraftInput('max')}
            onBlur={() => commitDraft('max')}
            onKeyDown={(e) => {
              if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
            }}
          />
        </label>
      </div>
      {(rangeError !== null || serverRangeError) && (
        <p data-testid="price-range-error" role="alert" id="price-range-error" className="text-[11px] text-rose-600 font-bold">
          {PRICE_RANGE_ERROR_MESSAGE}
        </p>
      )}
    </div>
  );
};
