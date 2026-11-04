import React from 'react';
import { toPersianDigits } from '../utils/formatters';

// Team 08 live parity (D62): ReturnWindowBadge chunk (live
// ReturnWindowBadge-MERGED.js, 5,022 bytes, imported from main/admin/
// warehouse/panels). States from live evidence: "N days left", expired
// (rose), and no-record. Fed by Team 03's returnWindowEndsAt data
// (GET /orders/<id>/shipping). No window rule is invented here: the
// backend supplies the end date; this component only renders it.
export const ReturnWindowBadge: React.FC<{
  returnWindowEndsAt?: string | null;
  className?: string;
}> = ({ returnWindowEndsAt, className }) => {
  if (!returnWindowEndsAt) {
    return (
      <span className={`inline-flex items-center rounded-lg bg-slate-100 px-2 py-1 text-[10px] font-bold text-slate-500 ${className || ''}`}>
        رکوردی برای مهلت مرجوعی وجود ندارد
      </span>
    );
  }
  const ends = new Date(returnWindowEndsAt).getTime();
  if (Number.isNaN(ends)) {
    return (
      <span className={`inline-flex items-center rounded-lg bg-slate-100 px-2 py-1 text-[10px] font-bold text-slate-500 ${className || ''}`}>
        رکوردی برای مهلت مرجوعی وجود ندارد
      </span>
    );
  }
  const daysLeft = Math.ceil((ends - Date.now()) / 86400000);
  if (daysLeft < 0) {
    return (
      <span className={`inline-flex items-center rounded-lg bg-rose-100 px-2 py-1 text-[10px] font-bold text-rose-700 ${className || ''}`}>
        مهلت مرجوعی به پایان رسیده
      </span>
    );
  }
  return (
    <span className={`inline-flex items-center rounded-lg bg-emerald-50 px-2 py-1 text-[10px] font-bold text-emerald-700 ${className || ''}`}>
      {toPersianDigits(daysLeft)} روز مانده تا پایان مهلت مرجوعی
    </span>
  );
};
