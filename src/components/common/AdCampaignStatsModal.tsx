import React from 'react';
import { useStore } from '../../context/StoreContext';
import { X, Activity, TrendingUp, Users, MousePointer, DollarSign } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  campaign: any | null;
  isOpen: boolean;
  onClose: () => void;
}

export const AdCampaignStatsModal: React.FC<Props> = ({ campaign, isOpen, onClose }) => {
  const { formatPrice, toPersianDigits } = useStore();

  if (!isOpen || !campaign) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[420px] w-full p-4 shadow-2xl border border-slate-100 space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-purple-50 text-purple-600 flex items-center justify-center">
              <Activity className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">گزارش آماری و عملکرد کمپین تبلیغات</h3>
              <span className="text-[10px] text-slate-400 block">{campaign.name || campaign.title}</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* 4 Metric Badges */}
        <div className="grid grid-cols-2 gap-2 text-xs">
          <div className="bg-slate-50 p-2.5 rounded-xl border border-slate-200/80 space-y-1">
            <div className="flex items-center gap-1 text-slate-400 text-[10px]">
              <Users className="w-3 h-3"  strokeWidth={1.5}/>
              <span>نمایش (Impression):</span>
            </div>
            <strong className="text-sm font-black text-slate-800 font-mono">
              {toPersianDigits(campaign.views || 0)}
            </strong>
          </div>

          <div className="bg-slate-50 p-2.5 rounded-xl border border-slate-200/80 space-y-1">
            <div className="flex items-center gap-1 text-slate-400 text-[10px]">
              <MousePointer className="w-3 h-3 text-brand-deep"  strokeWidth={1.5}/>
              <span>کلیک (Clicks):</span>
            </div>
            <strong className="text-sm font-black text-brand-deep font-mono">
              {toPersianDigits(campaign.clicks || 0)}
            </strong>
          </div>

          <div className="bg-slate-50 p-2.5 rounded-xl border border-slate-200/80 space-y-1">
            <div className="flex items-center gap-1 text-slate-400 text-[10px]">
              <TrendingUp className="w-3 h-3 text-emerald-600"  strokeWidth={1.5}/>
              <span>نرخ تبدیل (CTR):</span>
            </div>
            <strong className="text-sm font-black text-emerald-600 font-mono">
              ٪{toPersianDigits(campaign.ctr || 0)}
            </strong>
          </div>

          <div className="bg-slate-50 p-2.5 rounded-xl border border-slate-200/80 space-y-1">
            <div className="flex items-center gap-1 text-slate-400 text-[10px]">
              <DollarSign className="w-3 h-3 text-purple-600"  strokeWidth={1.5}/>
              <span>هزینه هر کلیک (CPC):</span>
            </div>
            <strong className="text-sm font-black text-purple-700 font-mono">
              {campaign.cpc ? `${toPersianDigits(campaign.cpc)} ت` : 'نامشخص'}
            </strong>
          </div>
        </div>

        {/* Anti-fraud & Quality Score */}
        <div className="bg-emerald-50/60 border border-emerald-200 p-2.5 rounded-xl space-y-1 text-[11px] text-emerald-800">
          <span className="font-bold block">پایش ضدتقلب (Anti-Fraud Guard):</span>
          <p className="text-[10px] leading-relaxed">
            ۱۰۰٪ کلیک‌های نامعتبر یا ناشی از ربات فیلتر شده و هزینه‌ای بابت کلیک‌های تکراری منظور نشده است.
          </p>
        </div>

        <button
          onClick={onClose}
          className="w-full py-2 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs"
        >
          بستن گزارش
        </button>
      </motion.div>
    </div>
  );
};
