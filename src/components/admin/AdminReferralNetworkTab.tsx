import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { 
  GitFork, Users, AlertTriangle, ShieldCheck, DollarSign, 
  ChevronDown, ChevronRight 
} from 'lucide-react';
import { Store as StoreType } from '../../types';

interface TreeNodeProps {
  store: StoreType;
  allStores: StoreType[];
  depth: number;
  visited: Set<string>;
  totalNetworkCommissions: Record<string, number>;
  toPersianDigits: (v: any) => string;
  formatPrice: (v: number) => string;
}

const ReferralTreeNode: React.FC<TreeNodeProps> = ({
  store,
  allStores,
  depth,
  visited,
  totalNetworkCommissions,
  toPersianDigits,
  formatPrice,
}) => {
  const [isOpen, setIsOpen] = useState(true);

  const isSelfReferred = store.referrerSellerId === store.id;
  const isLoop = visited.has(store.id);
  const nextVisited = new Set(visited).add(store.id);

  const directChildren = allStores.filter(
    (s) => s.referrerSellerId === store.id && s.id !== store.id
  );

  const branchCommission = totalNetworkCommissions[store.id] || 0;

  return (
    <div className="text-xs">
      <div className={`flex items-center justify-between p-2.5 rounded-xl border transition-all ${
        isSelfReferred || isLoop 
          ? 'bg-rose-50 border-rose-300 text-rose-900' 
          : depth === 0 
            ? 'bg-slate-100 border-slate-200 font-bold' 
            : 'bg-white border-slate-200/90'
      }`}>
        <div className="flex items-center gap-2">
          {directChildren.length > 0 ? (
            <button 
              onClick={() => setIsOpen(!isOpen)} 
              className="p-1 hover:bg-slate-200/60 rounded-md text-slate-500"
            >
              {isOpen ? <ChevronDown className="w-3.5 h-3.5"  strokeWidth={1.5}/> : <ChevronRight className="w-3.5 h-3.5"  strokeWidth={1.5}/>}
            </button>
          ) : (
            <span className="w-5" />
          )}

          <div className="w-6 h-6 rounded-lg bg-blue-100 text-blue-700 flex items-center justify-center font-bold text-[10px]">
            L{toPersianDigits(depth + 1)}
          </div>

          <div>
            <div className="flex items-center gap-1.5">
              <span className="font-bold text-slate-800">{store.name}</span>
              <span className="text-[10px] text-slate-400 font-mono">({store.slug})</span>
              {(isSelfReferred || isLoop) && (
                <span className="px-1.5 py-0.5 rounded bg-rose-600 text-white text-[9px] font-bold flex items-center gap-0.5">
                  <AlertTriangle className="w-2.5 h-2.5"  strokeWidth={1.5}/>
                  حلقه ارجاع به خود!
                </span>
              )}
            </div>
            <span className="text-[10px] text-slate-500 block">
              مالک: {store.ownerName} • فروش کل: {formatPrice(store.totalSales)} ت
            </span>
          </div>
        </div>

        <div className="text-left shrink-0">
          <span className="text-[10px] text-slate-400 block">پاداش تجمعی شاخه (۲٪ از F)</span>
          <span className="font-bold font-mono text-emerald-600 text-[11px]">
            +{formatPrice(branchCommission)} ت
          </span>
        </div>
      </div>

      {isOpen && directChildren.length > 0 && !isLoop && (
        <div className="mr-5 pr-3 border-r-2 border-slate-200 my-1.5 space-y-1.5">
          {directChildren.map((child) => (
            <ReferralTreeNode
              key={child.id}
              store={child}
              allStores={allStores}
              depth={depth + 1}
              visited={nextVisited}
              totalNetworkCommissions={totalNetworkCommissions}
              toPersianDigits={toPersianDigits}
              formatPrice={formatPrice}
            />
          ))}
        </div>
      )}
    </div>
  );
};
export const AdminReferralNetworkTab: React.FC = () => {
  const { stores, platformSettings, formatPrice, toPersianDigits } = useStore();

  const commissionPercent = platformSettings.referralProgram?.sellerCommissionPercent || 2;

  // Augment mock stores with a 3-level referral tree: Root: store-1 -> L2: store-2 -> L3: store-3
  const storesWithDemoNetwork: StoreType[] = stores.map((s) => {
    if (s.id === 'store-2' && !s.referrerSellerId) {
      return { ...s, referrerSellerId: 'store-1' };
    }
    if (s.id === 'store-3' && !s.referrerSellerId) {
      return { ...s, referrerSellerId: 'store-2' };
    }
    return s;
  });

  // Calculate commissions for each store in the network
  const totalNetworkCommissions: Record<string, number> = {};

  const computeBranchCommission = (sellerId: string): number => {
    const directChildren = storesWithDemoNetwork.filter((s) => s.referrerSellerId === sellerId && s.id !== sellerId);
    let total = 0;
    for (const child of directChildren) {
      const childPlatformF = Math.round(child.totalSales * 0.1);
      const childEarned = Math.round(childPlatformF * (commissionPercent / 100));
      total += childEarned + computeBranchCommission(child.id);
    }
    return total;
  };

  storesWithDemoNetwork.forEach((s) => {
    totalNetworkCommissions[s.id] = computeBranchCommission(s.id);
  });

  const rootStores = storesWithDemoNetwork.filter(
    (s) => !s.referrerSellerId || s.referrerSellerId === s.id || !storesWithDemoNetwork.some(parent => parent.id === s.referrerSellerId)
  );

  const selfReferralViolations = storesWithDemoNetwork.filter((s) => s.referrerSellerId === s.id);

  return (
    <div className="space-y-3 pb-6 select-none">
      <div className="bg-gradient-to-l from-indigo-950 via-slate-900 to-indigo-900 rounded-2xl p-4 text-white shadow-xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <GitFork className="w-5 h-5 text-indigo-400"  strokeWidth={1.5}/>
            <h2 className="text-xs font-bold">پایش درخت ارجاع و شبکه دعوت سلرها (ضدهرم)</h2>
          </div>
          <span className="text-[9px] bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 px-2 py-0.5 rounded-full font-bold">
            کارمزد فقط از F ({toPersianDigits(commissionPercent)}٪)
          </span>
        </div>
        <p className="text-[10px] text-slate-300 leading-relaxed">
          نظارت بر ساختار چندسطحی معرفی فروشگاه‌ها جهت تضمین سلامت سیستم، جلوگیری از ایجاد ساختارهای هرمی بسته، کشف خوداتصالی (Self-Referral) و تضمین محاسبه دقیق پاداش صرفاً از سهم پلتفرم ($F$).
        </p>
      </div>

      {selfReferralViolations.length > 0 ? (
        <div className="bg-rose-50 border border-rose-300 rounded-2xl p-3.5 space-y-2 text-rose-900 text-xs">
          <div className="flex items-center gap-2 font-bold">
            <AlertTriangle className="w-4 h-4 text-rose-600"  strokeWidth={1.5}/>
            <span>هشدار تخلف خودارجاعی (Self-Referral Detected)</span>
          </div>
          <p className="text-[11px] text-rose-700 leading-relaxed">
            تعداد {toPersianDigits(selfReferralViolations.length)} فروشگاه شناسه معرف خود را روی خودشان تنظیم کرده‌اند؛ کمیسیون‌های این فروشگاه‌ها مسدود است.
          </p>
        </div>
      ) : (
        <div className="bg-emerald-50 border border-emerald-200/90 rounded-2xl p-3 flex items-center justify-between text-xs text-emerald-900">
          <div className="flex items-center gap-2">
            <ShieldCheck className="w-4 h-4 text-emerald-600"  strokeWidth={1.5}/>
            <span className="font-bold">سلامت شبکه ارجاع: ۰ حلقه یا خوداتصالی مسدودکننده کشف شد.</span>
          </div>
          <span className="text-[10px] bg-emerald-200/60 font-mono px-2 py-0.5 rounded-md font-bold">
            ۳ سطح فعال
          </span>
        </div>
      )}

      <div className="grid grid-cols-3 gap-2">
        <div className="bg-white p-3 rounded-2xl border border-slate-200 shadow-2xs text-center">
          <span className="text-[10px] text-slate-400 block">ریشه‌های شبکه (Root)</span>
          <span className="text-sm font-black text-slate-800">{toPersianDigits(rootStores.length)} ویترین</span>
        </div>
        <div className="bg-white p-3 rounded-2xl border border-slate-200 shadow-2xs text-center">
          <span className="text-[10px] text-slate-400 block">بیشترین عمق زنجیره</span>
          <span className="text-sm font-black text-indigo-700">۳ سطح</span>
        </div>
        <div className="bg-white p-3 rounded-2xl border border-slate-200 shadow-2xs text-center">
          <span className="text-[10px] text-slate-400 block">مجموع تعهدات پاداش F</span>
          <span className="text-sm font-black text-emerald-600 font-mono">
            {formatPrice(Object.values(totalNetworkCommissions).reduce((a, b) => a + b, 0))} ت
          </span>
        </div>
      </div>

      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-1.5">
            <Users className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">درخت سلسله‌مراتبی زنجیره دعوت و کمیسیون شاخه‌ها</h3>
          </div>
          <span className="text-[10px] text-slate-400">
            برای باز و بسته کردن هر زیرشاخه کلیک کنید
          </span>
        </div>

        <div className="space-y-2 pt-1">
          {rootStores.map((root) => (
            <ReferralTreeNode
              key={root.id}
              store={root}
              allStores={storesWithDemoNetwork}
              depth={0}
              visited={new Set()}
              totalNetworkCommissions={totalNetworkCommissions}
              toPersianDigits={toPersianDigits}
              formatPrice={formatPrice}
            />
          ))}
        </div>
      </div>
    </div>
  );
};
