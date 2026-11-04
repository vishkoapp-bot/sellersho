import React, { useState } from 'react';
import { useStore } from '../context/StoreContext';
import { TrendingUp } from 'lucide-react';

export const SellerProfitCalculator: React.FC = () => {
  const { formatPrice, toPersianDigits } = useStore();
  const [salesVolume, setSalesVolume] = useState<number>(50000000); // 50 Million Tomans
  const [profitMargin, setProfitMargin] = useState<number>(15); // 15%

  const estimatedProfit = Math.round(salesVolume * (profitMargin / 100));

  return (
    <div className="bg-white/10 backdrop-blur-xl border border-white/15 rounded-3xl p-5 md:p-6 shadow-xl space-y-4">
      <div className="flex items-center justify-between border-b border-white/10 pb-3">
        <div className="flex items-center gap-2">
          <TrendingUp className="w-5 h-5 text-emerald-400"  strokeWidth={1.5}/>
          <span className="font-bold text-sm text-white">ماشین‌حساب تخمین درآمد شما</span>
        </div>
        <span className="text-[11px] text-blue-300 font-medium">ماهانه</span>
      </div>

      {/* Slider 1: Sales Volume */}
      <div className="space-y-1.5">
        <div className="flex justify-between text-xs">
          <span className="text-slate-300">میزان فروش ماهانه:</span>
          <span className="font-black text-white">{formatPrice(salesVolume)} تومان</span>
        </div>
        <input
          type="range"
          min={10000000}
          max={250000000}
          step={5000000}
          value={salesVolume}
          onChange={(e) => setSalesVolume(Number(e.target.value))}
          className="w-full h-2 bg-white/20 rounded-lg appearance-none cursor-pointer accent-blue-400"
        />
      </div>

      {/* Slider 2: Profit Margin */}
      <div className="space-y-1.5">
        <div className="flex justify-between text-xs">
          <span className="text-slate-300">حاشیه سود شما:</span>
          <span className="font-black text-emerald-400">{toPersianDigits(profitMargin)}٪</span>
        </div>
        <input
          type="range"
          min={5}
          max={35}
          step={1}
          value={profitMargin}
          onChange={(e) => setProfitMargin(Number(e.target.value))}
          className="w-full h-2 bg-white/20 rounded-lg appearance-none cursor-pointer accent-emerald-400"
        />
      </div>

      {/* Result Display Box */}
      <div className="bg-gradient-to-r from-emerald-950/70 to-teal-950/70 border border-emerald-500/30 rounded-2xl p-4 text-center">
        <p className="text-xs text-emerald-300 mb-1">سود خالص ماهانه شما:</p>
        <div className="text-2xl md:text-3xl font-black text-emerald-400 tracking-tight">
          {formatPrice(estimatedProfit)}
          <span className="text-xs font-normal text-emerald-200 mr-1.5">تومان</span>
        </div>
        <p className="text-[10px] text-emerald-200/80 mt-1">
          بدون هزینه انبارداری، ریسک دپو و خواب سرمایه
        </p>
      </div>
    </div>
  );
};
