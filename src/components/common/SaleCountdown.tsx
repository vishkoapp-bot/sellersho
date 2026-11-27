import React, { useState, useEffect } from 'react';
import { toPersianDigits } from '../../utils/formatters';

// P1(c) sale countdown — new component (Nov 13, 2026). No countdown
// existed in the frontend except the OTP resend timers, which carry
// expiry/resend logic and must not be reused here. Renders under the
// discount badge on the PDP only, never on cards.
// Window semantics ([from, to), backend P1(c)): a null start means
// "already started", a null end means "no end" — but without an end
// there is nothing to count down to, so only a future endsAt renders.
interface SaleCountdownProps {
  endsAt: string;
  startsAt?: string | null;
  fashion?: boolean;
  central?: boolean;
}

const pad = (n: number) => String(n).padStart(2, '0');

export const SaleCountdown: React.FC<SaleCountdownProps> = ({ endsAt, startsAt, fashion = false, central = false }) => {
  const [, setTick] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const now = Date.now();
  const end = new Date(endsAt).getTime();
  if (!Number.isFinite(end)) return null;
  if (startsAt) {
    const start = new Date(startsAt).getTime();
    if (Number.isFinite(start) && start > now) {
      const date = new Date(startsAt).toLocaleDateString('fa-IR', { day: 'numeric', month: 'long' });
      return (
        <span className={central ? 'shop-badge shop-badge--time' : fashion ? 'text-[10px] font-bold text-[#737373]' : 'text-[10px] font-bold text-slate-500'}>
          حراج از {toPersianDigits(date)} شروع می‌شود
        </span>
      );
    }
  }
  const remainingMs = end - now;
  if (remainingMs <= 0) return null; // expired: unmounts itself
  const totalSeconds = Math.floor(remainingMs / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const clock = `${toPersianDigits(pad(hours))}:${toPersianDigits(pad(minutes))}:${toPersianDigits(pad(seconds))}`;
  return (
    <span
      data-testid="sale-countdown"
      className={central ? 'shop-badge shop-badge--time' : fashion ? 'text-[10px] font-bold text-[#1c1c1a]' : 'text-[10px] font-bold text-rose-600'}
    >
      {days > 0
        ? `پایان حراج تا ${toPersianDigits(days)} روز و ${clock}`
        : `پایان حراج تا ${clock}`}
    </span>
  );
};
