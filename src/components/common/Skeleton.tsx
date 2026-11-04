import React from 'react';

export const TextSkeleton: React.FC<{ className?: string }> = ({ className = 'h-4 w-full' }) => (
  <div className={`skeleton-shimmer rounded-md ${className}`} />
);

export const ProductCardSkeleton: React.FC = () => (
  <div className="w-full bg-white rounded-[12px] border border-[#e2e5ee] overflow-hidden p-0 flex flex-col justify-between" style={{ height: '284px' }}>
    <div className="relative w-full aspect-square skeleton-shimmer flex items-center justify-center">
      <div className="w-8 h-8 rounded-full bg-slate-200" />
      <div className="absolute bottom-2 inset-x-2 h-7 bg-slate-200/90 rounded-[4px]" />
    </div>
    <div className="p-2 space-y-2 flex-1 flex flex-col justify-between">
      <div className="space-y-1.5">
        <div className="h-3 w-16 bg-slate-200 rounded skeleton-shimmer" />
        <div className="h-3.5 w-full bg-slate-200 rounded skeleton-shimmer" />
      </div>
      <div className="space-y-1 pt-1">
        <div className="h-3 w-14 bg-slate-200 rounded skeleton-shimmer" />
        <div className="flex justify-between items-center">
          <div className="h-4 w-20 bg-slate-200 rounded skeleton-shimmer" />
          <div className="h-4 w-8 bg-slate-200 rounded-full skeleton-shimmer" />
        </div>
      </div>
    </div>
  </div>
);

export const CarouselSkeleton: React.FC<{ count?: number }> = ({ count = 3 }) => (
  <div className="flex gap-2.5 overflow-hidden px-3.5 py-1">
    {Array.from({ length: count }).map((_, i) => (
      <div key={i} className="w-[170px] shrink-0">
        <ProductCardSkeleton />
      </div>
    ))}
  </div>
);
