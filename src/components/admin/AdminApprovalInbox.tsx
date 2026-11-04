import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { PendingApprovalItem, ApprovalEntityType } from '../../types';
import { adminApi, PendingApprovalDto } from '../../services/adminApi';
import { isMockMode } from '../../services/apiClient';
import { 
  Inbox, 
  Check, 
  X, 
  Clock, 
  CheckCircle2, 
  Eye, 
  Store, 
  Globe, 
  Package, 
  DollarSign, 
  Link2 
} from 'lucide-react';

export const AdminApprovalInbox: React.FC = () => {
  const { unifiedPendingApprovals, approveUnifiedItem, rejectUnifiedItem, toPersianDigits } = useStore();
  const { success, warning } = useToast();
  const [selectedType, setSelectedType] = useState<'all' | ApprovalEntityType>('all');
  const [inspectingItem, setInspectingItem] = useState<PendingApprovalItem | null>(null);
  const [serverApprovals, setServerApprovals] = useState<PendingApprovalDto[]>([]);

  const fetchApprovals = () => {
    if (!isMockMode()) {
      adminApi.getApprovals()
        .then((data) => {
          if (Array.isArray(data)) setServerApprovals(data);
        })
        .catch((err) => console.warn('Failed to fetch approvals:', err));
    }
  };

  useEffect(() => {
    fetchApprovals();
  }, []);

  const combinedApprovals: PendingApprovalItem[] = [
    ...serverApprovals.map((a): PendingApprovalItem => ({
      id: a.id,
      entityType: (a.entityType as ApprovalEntityType) || 'store_name',
      targetId: a.targetId,
      title: a.title,
      requestedData: a.requestedData || {},
      requestedBy: a.requestedBy,
      status: a.status,
      createdAt: a.createdAt ? new Date(a.createdAt).toLocaleDateString('fa-IR') : 'امروز',
    })),
    ...unifiedPendingApprovals,
  ];

  const filtered = combinedApprovals.filter((item) => {
    if (selectedType !== 'all' && item.entityType !== selectedType) return false;
    return item.status === 'pending';
  });

  const getEntityIcon = (type: ApprovalEntityType) => {
    switch (type) {
      case 'store_name':
        return <Store className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>;
      case 'store_slug':
        return <Link2 className="w-3.5 h-3.5 text-indigo-600"  strokeWidth={1.5}/>;
      case 'custom_domain':
        return <Globe className="w-3.5 h-3.5 text-teal-600"  strokeWidth={1.5}/>;
      case 'proposed_product':
        return <Package className="w-3.5 h-3.5 text-purple-600"  strokeWidth={1.5}/>;
      case 'payout':
        return <DollarSign className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>;
    }
  };

  const getEntityBadge = (type: ApprovalEntityType) => {
    const labels: Record<ApprovalEntityType, { text: string; cls: string }> = {
      store_name: { text: 'تغییر نام ویترین', cls: 'bg-blue-50 text-blue-700 border-blue-200' },
      store_slug: { text: 'تغییر نامک (Slug)', cls: 'bg-indigo-50 text-indigo-700 border-indigo-200' },
      custom_domain: { text: 'اتصال دامنه اختصاصی', cls: 'bg-teal-50 text-teal-700 border-teal-200' },
      proposed_product: { text: 'کالای پیشنهادی تأمین', cls: 'bg-purple-50 text-purple-700 border-purple-200' },
      payout: { text: 'تسویه حساب پایا', cls: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
    };
    const b = labels[type];
    return (
      <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full border ${b.cls}`}>
        {b.text}
      </span>
    );
  };

  const handleApprove = async (item: PendingApprovalItem) => {
    if (!isMockMode() && serverApprovals.some((s) => s.id === item.id)) {
      try {
        await adminApi.reviewApproval(item.id, { status: 'approved' });
        fetchApprovals();
      } catch (err) {
        console.warn('Server approval review failed:', err);
      }
    }
    approveUnifiedItem(item);
    success(`درخواست «${item.title}» تأیید و در سیستم اعمال شد.`);
    if (inspectingItem?.id === item.id) setInspectingItem(null);
  };

  const handleReject = async (item: PendingApprovalItem) => {
    if (!isMockMode() && serverApprovals.some((s) => s.id === item.id)) {
      try {
        await adminApi.reviewApproval(item.id, {
          status: 'rejected',
          rejectionReason: 'عدم انطباق با ضوابط پلتفرم',
        });
        fetchApprovals();
      } catch (err) {
        console.warn('Server rejection review failed:', err);
      }
    }
    rejectUnifiedItem(item, 'عدم انطباق با ضوابط پلتفرم');
    warning(`درخواست «${item.title}» رد شد.`);
    if (inspectingItem?.id === item.id) setInspectingItem(null);
  };

  return (
    <div className="space-y-3 pb-6 select-none">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-indigo-700">
            <Inbox className="w-5 h-5"  strokeWidth={1.5}/>
            <div>
              <h3 className="text-xs font-bold text-slate-800">صندوق متمرکز تاییدات (Approval Inbox)</h3>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                تأیید یکپارچه تغییر نام، نامک ویترین، دامنه‌ها، کالاهای جدید و تسویه‌ها
              </span>
            </div>
          </div>
          <span className="text-[10px] font-bold bg-amber-50 text-amber-800 border border-amber-200 px-2.5 py-0.5 rounded-full">
            {toPersianDigits(filtered.length)} درخواست در صف
          </span>
        </div>

        <div className="flex gap-1 overflow-x-auto no-scrollbar pt-1 text-xs">
          {[
            { id: 'all', label: 'همه' },
            { id: 'store_name', label: 'نام ویترین' },
            { id: 'store_slug', label: 'نامک (Slug)' },
            { id: 'custom_domain', label: 'دامنه' },
            { id: 'proposed_product', label: 'کالای جدید' },
            { id: 'payout', label: 'تسویه' },
          ].map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setSelectedType(tab.id as any)}
              className={`py-1 px-3 rounded-xl font-bold whitespace-nowrap transition-all ${
                selectedType === tab.id
                  ? 'bg-slate-900 text-white shadow-2xs'
                  : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      <div className="space-y-2.5">
        {filtered.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs border border-slate-100">
            درخواستی در این صف وجود ندارد.
          </div>
        ) : (
          filtered.map((item) => (
            <div
              key={item.id}
              className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2 text-xs"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div className="flex items-center gap-1.5">
                  {getEntityIcon(item.entityType)}
                  <span className="font-bold text-slate-900">{item.title}</span>
                </div>
                {getEntityBadge(item.entityType)}
              </div>

              <div className="bg-slate-50 p-2 rounded-xl text-[10px] text-slate-500 flex justify-between">
                <span>شناسه: <strong className="font-mono text-slate-700">{item.id}</strong> • متقاضی: {item.requestedBy}</span>
                <span className="font-mono">{item.createdAt}</span>
              </div>

              <div className="flex items-center gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => handleApprove(item)}
                  className="flex-1 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[11px] rounded-xl flex items-center justify-center gap-1 shadow-2xs"
                >
                  <Check className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  <span>تأیید و اعمال</span>
                </button>
                <button
                  type="button"
                  onClick={() => handleReject(item)}
                  className="flex-1 py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[11px] rounded-xl border border-rose-200 flex items-center justify-center gap-1"
                >
                  <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  <span>رد درخواست</span>
                </button>
                <button
                  type="button"
                  onClick={() => setInspectingItem(item)}
                  className="p-1.5 bg-slate-100 hover:bg-slate-200 text-slate-600 rounded-xl"
                  title="مشاهده جزئیات داده"
                >
                  <Eye className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                </button>
              </div>
            </div>
          ))
        )}
      </div>

      {inspectingItem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold text-slate-900">{inspectingItem.title}</h4>
              <button onClick={() => setInspectingItem(null)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div className="space-y-1.5">
              {Object.entries(inspectingItem.requestedData).map(([k, v]) => (
                <div key={k} className="p-2 bg-slate-50 rounded-xl flex justify-between">
                  <span className="text-slate-400">{k}:</span>
                  <span className="font-bold text-slate-800 text-[11px]">
                    {Array.isArray(v) ? v.join(', ') : String(v)}
                  </span>
                </div>
              ))}
            </div>
            <div className="flex gap-2 pt-2 border-t">
              <button
                type="button"
                onClick={() => handleApprove(inspectingItem)}
                className="flex-1 py-2 bg-emerald-600 text-white font-bold rounded-xl"
              >
                تأیید درخواست
              </button>
              <button
                type="button"
                onClick={() => handleReject(inspectingItem)}
                className="px-3 py-2 bg-rose-50 text-rose-700 font-bold rounded-xl"
              >
                رد
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};