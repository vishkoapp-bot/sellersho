import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { ShieldCheck, History, Filter, Search, User, Clock, AlertCircle } from 'lucide-react';

export const AdminAuditLogTab: React.FC = () => {
  const { auditLogs } = useStore();
  const [filterCat, setFilterCat] = useState<string>('all');
  const [searchTerm, setSearchTerm] = useState('');

  const filtered = auditLogs.filter((log) => {
    const matchCat = filterCat === 'all' || log.category === filterCat;
    const matchSearch =
      !searchTerm ||
      log.action.includes(searchTerm) ||
      log.operator.includes(searchTerm) ||
      log.details.includes(searchTerm);
    return matchCat && matchSearch;
  });

  const getCategoryBadge = (cat: string) => {
    switch (cat) {
      case 'commission':
        return <span className="text-[9px] bg-blue-50 text-blue-700 font-bold px-2 py-0.5 rounded-full">تعرفه‌ها و فرمول</span>;
      case 'store_status':
        return <span className="text-[9px] bg-amber-50 text-amber-700 font-bold px-2 py-0.5 rounded-full">تعلیق / فعال‌سازی</span>;
      case 'catalog':
        return <span className="text-[9px] bg-emerald-50 text-emerald-700 font-bold px-2 py-0.5 rounded-full">کاتالوگ و بارکد</span>;
      case 'finance':
        return <span className="text-[9px] bg-teal-50 text-teal-700 font-bold px-2 py-0.5 rounded-full">اسناد مالی</span>;
      case 'dispute':
        return <span className="text-[9px] bg-rose-50 text-rose-700 font-bold px-2 py-0.5 rounded-full">حکم داوری</span>;
      default:
        return <span className="text-[9px] bg-slate-100 text-slate-700 font-bold px-2 py-0.5 rounded-full">سیستمی</span>;
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* Header Info */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-blue-600">
            <History className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">دفتر ثبت سوابق و ممیزی اقدامات حساس (Audit Trail)</h3>
          </div>
          <span className="text-[10px] bg-blue-50 text-blue-700 font-bold px-2 py-0.5 rounded-full">
            فصل ۳ و ۲۳ پروپوزال
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          طبق الزامات امنیتی، هرگونه تعلیق ویترین، تغییر سهم ثابت پلتفرم ($F$)، تأیید کاتالوگ، صدور اسناد تعدیل و احکام داوری با ذکر زمان، شناسه و نام کارشناس ثبت دائمی می‌شود.
        </p>

        {/* Filters */}
        <div className="flex flex-wrap items-center gap-2 pt-1 border-t border-slate-100">
          <div className="relative flex-1 min-w-[180px]">
            <input
              type="text"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              placeholder="جستجو در سوابق، اقدام یا کاربر..."
              className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200"
            />
          </div>
          <select
            value={filterCat}
            onChange={(e) => setFilterCat(e.target.value)}
            className="text-xs p-2 rounded-xl bg-slate-50 border border-slate-200"
          >
            <option value="all">همه دسته‌ها</option>
            <option value="commission">تعرفه‌ها و فرمول</option>
            <option value="store_status">تعلیق / فعال‌سازی</option>
            <option value="catalog">کاتالوگ و بارکد</option>
            <option value="finance">اسناد مالی</option>
            <option value="dispute">حکم داوری</option>
          </select>
        </div>
      </div>

      {/* Log Entries List */}
      <div className="space-y-2">
        {filtered.map((log) => (
          <div key={log.id} className="p-3 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-1.5 text-xs">
            <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
              <div className="flex items-center gap-2">
                <span className="font-mono font-bold text-slate-800 text-[11px]">{log.id}</span>
                <strong className="text-slate-900 text-xs">{log.action}</strong>
              </div>
              {getCategoryBadge(log.category)}
            </div>

            <p className="text-[11px] text-slate-700 leading-relaxed">{log.details}</p>

            <div className="flex items-center justify-between text-[10px] text-slate-400 pt-1 border-t border-slate-100">
              <span className="flex items-center gap-1 text-slate-600">
                <User className="w-3 h-3 text-slate-400"  strokeWidth={1.5}/>
                <strong>{log.operator}</strong>
              </span>
              <span className="flex items-center gap-1 font-mono">
                <Clock className="w-3 h-3"  strokeWidth={1.5}/>
                <span>{log.timestamp}</span>
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};