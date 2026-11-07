import React from 'react';
import { Sparkles, Flame, Tag } from 'lucide-react';
import { useStore } from '../../context/StoreContext';

interface CampaignDiscountBadgeProps {
  campaignName?: string;
  discountPercent?: number;
  size?: 'sm' | 'md' | 'lg';
  className?: string;
}

export const CampaignDiscountBadge: React.FC<CampaignDiscountBadgeProps> = ({
  campaignName,
  discountPercent,
  size = 'md',
  className = '',
}) => {
  const { toPersianDigits } = useStore();

  if (!campaignName && (!discountPercent || discountPercent <= 0)) {
    return null;
  }

  const cleanName = campaignName
    ? campaignName.replace(/\([^)]*\)/g, '').trim()
    : 'جشنواره تخفیف';

  if (size === 'sm') {
    return (
      <div
        className={`inline-flex items-center gap-1 rounded-md bg-gradient-to-l from-rose-600 to-amber-600 px-1.5 py-0.5 text-[9px] font-black text-white shadow-xs ${className}`}
      >
        <Sparkles className="w-2.5 h-2.5 shrink-0" />
        <span className="truncate max-w-[110px]">{cleanName}</span>
        {discountPercent && discountPercent > 0 && (
          <span className="bg-white/20 px-1 rounded text-[8px] font-extrabold mr-0.5">
            {toPersianDigits(discountPercent)}٪
          </span>
        )}
      </div>
    );
  }

  if (size === 'lg') {
    return (
      <div
        className={`flex items-center justify-between gap-2 rounded-xl bg-gradient-to-r from-rose-50 via-amber-50 to-orange-50 border border-rose-200/80 p-2.5 shadow-2xs ${className}`}
      >
        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-rose-500 to-amber-500 text-white shadow-xs">
            <Flame className="w-4 h-4 animate-bounce" />
          </div>
          <div>
            <span className="text-[10px] font-bold text-amber-900/80 block">تخفیف ویژه جشنواره</span>
            <h4 className="text-xs font-black text-rose-950">{cleanName}</h4>
          </div>
        </div>
        {discountPercent && discountPercent > 0 && (
          <div className="flex items-center gap-1 rounded-lg bg-rose-600 px-2.5 py-1 text-white shadow-xs">
            <Tag className="w-3.5 h-3.5" />
            <span className="text-xs font-black">{toPersianDigits(discountPercent)}٪ تخفیف</span>
          </div>
        )}
      </div>
    );
  }

  // Default 'md'
  return (
    <div
      className={`inline-flex items-center gap-1.5 rounded-lg bg-gradient-to-l from-rose-500 to-orange-500 px-2 py-0.5 text-[10px] font-black text-white shadow-xs ${className}`}
    >
      <Sparkles className="w-3 h-3 shrink-0" />
      <span className="truncate max-w-[130px]">{cleanName}</span>
      {discountPercent && discountPercent > 0 && (
        <span className="bg-black/15 px-1 rounded text-[9px] font-extrabold">
          {toPersianDigits(discountPercent)}٪
        </span>
      )}
    </div>
  );
};
