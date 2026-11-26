import { ProductFieldReview, reviewLabel } from './ProductFieldReview';
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

const formatDetailValue = (v: any): string => {
  if (v === null || v === undefined) return 'ثبت نشده';
  if (typeof v === 'boolean') return v ? 'بله' : 'خیر';
  if (typeof v === 'number') return v.toLocaleString('fa-IR');
  if (Array.isArray(v)) return v.map(formatDetailValue).join('، ');
  if (typeof v === 'object') return JSON.stringify(v, null, 2);
  return String(v);
};

export const AdminApprovalInbox: React.FC = () => {
  const { unifiedPendingApprovals, approveUnifiedItem, rejectUnifiedItem, toPersianDigits } = useStore();
  const { success, warning, error } = useToast();
  const [selectedType, setSelectedType] = useState<'all' | ApprovalEntityType>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [inspectingItem, setInspectingItem] = useState<PendingApprovalItem | null>(null);
  const [serverApprovals, setServerApprovals] = useState<PendingApprovalDto[]>([]);
  const [loadError, setLoadError] = useState('');

  const fetchApprovals = () => {
    if (!isMockMode()) {
      setLoadError('');
      adminApi.getApprovals()
        .then((data) => {
          if (Array.isArray(data)) setServerApprovals(data);
        })
        .catch((err) => setLoadError(err instanceof Error ? err.message : 'بارگذاری درخواست‌ها ناموفق بود'));
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
    ...(isMockMode() ? unifiedPendingApprovals : []),
  ];

  const filtered = combinedApprovals.filter((item) => {
    if (selectedType !== 'all' && item.entityType !== selectedType) return false;
    if (item.status !== 'pending') return false;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      return item.title.toLowerCase().includes(q) || (item.requestedBy || '').toLowerCase().includes(q);
    }
    return true;
  });
  const countByType = (type: ApprovalEntityType) => combinedApprovals.filter(i => i.status === 'pending' && i.entityType === type).length;

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
    const b = labels[type] || {text: String(type), cls: 'bg-slate-50 text-slate-700'};
    return (
      <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full border ${b.cls}`}>
        {b.text}
      </span>
    );
  };

  const handleApprove = async (item: PendingApprovalItem) => {
    if(item.entityType === "proposed_product") {setInspectingItem(item);return;}
    if (!isMockMode() && serverApprovals.some((s) => s.id === item.id)) {
      try {
        await adminApi.reviewApproval(item.id, { status: 'approved' });
        fetchApprovals();
      } catch (err) {
        error(err instanceof Error ? err.message : 'تأیید درخواست در سرور ناموفق بود؛ تغییری اعمال نشد.');
        return;
      }
    }
    approveUnifiedItem(item);
    success(`درخواست «${item.title}» تأیید و در سیستم اعمال شد.`);
    if (inspectingItem?.id === item.id) setInspectingItem(null);
  };

  const handleReject = async (item: PendingApprovalItem) => {
    if(item.entityType === "proposed_product") {setInspectingItem(item);return;}
    if (!isMockMode() && serverApprovals.some((s) => s.id === item.id)) {
      try {
        await adminApi.reviewApproval(item.id, {
          status: 'rejected',
          rejectionReason: 'عدم انطباق با ضوابط پلتفرم',
        });
        fetchApprovals();
      } catch (err) {
        error(err instanceof Error ? err.message : 'رد درخواست در سرور ناموفق بود؛ تغییری اعمال نشد.');
        return;
      }
    }
    rejectUnifiedItem(item, 'عدم انطباق با ضوابط پلتفرم');
    warning(`درخواست «${item.title}» رد شد.`);
    if (inspectingItem?.id === item.id) setInspectingItem(null);
  };

  return (
    <div className="space-y-3 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-indigo-700">
            <Inbox className="w-5 h-5"  strokeWidth={1.5}/>
            <div>
              <h3 className="text-sm font-bold text-slate-800">صندوق متمرکز تاییدات (Approval Inbox)</h3>
              <span className="text-xs text-slate-400 block mt-0.5">
                تأیید یکپارچه تغییر نام، نامک ویترین، دامنه‌ها، کالاهای جدید و تسویه‌ها
              </span>
            </div>
          </div>
          <span className="text-xs font-bold bg-amber-50 text-amber-800 border border-amber-200 px-2.5 py-0.5 rounded-full">
            {toPersianDigits(filtered.length)} درخواست در صف
          </span>
        </div>

        <input
          type="text"
          placeholder="جستجوی عنوان یا متقاضی..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          aria-label="جستجوی درخواست‌ها"
          className="w-full min-h-11 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-800 placeholder-slate-400 focus:outline-hidden"
        />

        <div className="flex gap-1 overflow-x-auto no-scrollbar pt-1 text-xs">
          {[
            { id: 'all', label: 'همه', count: combinedApprovals.filter(i => i.status === 'pending').length },
            { id: 'store_name', label: 'نام ویترین', count: countByType('store_name') },
            { id: 'store_slug', label: 'نامک (Slug)', count: countByType('store_slug') },
            { id: 'custom_domain', label: 'دامنه', count: countByType('custom_domain') },
            { id: 'proposed_product', label: 'کالای جدید', count: countByType('proposed_product') },
            { id: 'payout', label: 'تسویه', count: countByType('payout') },
          ].map((tab) => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setSelectedType(tab.id as any)}
              className={`min-h-9 py-1.5 px-3 rounded-xl font-bold whitespace-nowrap transition-all ${
                selectedType === tab.id
                  ? 'bg-slate-900 text-white shadow-2xs'
                  : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {tab.label} ({toPersianDigits(tab.count)})
            </button>
          ))}
        </div>
      </div>

      {loadError && <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs leading-7 text-rose-800"><p className="font-bold">بارگذاری صندوق تاییدات ناموفق بود.</p><p>{loadError}</p><button type="button" onClick={fetchApprovals} className="min-h-11 rounded-lg bg-rose-600 px-4 py-2 text-xs font-bold text-white">تلاش مجدد</button></div>}

      <div className="space-y-2.5">
        {filtered.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-sm border border-slate-100">
            درخواستی در این صف وجود ندارد.
          </div>
        ) : (
          filtered.map((item) => {
            const previewEntries = Object.entries(item.requestedData || {}).slice(0, 3);
            return (
            <div
              key={item.id}
              className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2 text-xs"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div className="flex items-center gap-1.5 min-w-0">
                  {getEntityIcon(item.entityType)}
                  <span className="font-bold text-sm text-slate-900 truncate">{item.title}</span>
                </div>
                {getEntityBadge(item.entityType)}
              </div>

              <div className="bg-slate-50 p-2 rounded-xl text-xs text-slate-500 flex flex-wrap gap-x-2 gap-y-1 justify-between">
                <span>شناسه: <strong className="font-mono text-slate-700">{item.id}</strong> • متقاضی: {item.requestedBy}</span>
                <span className="font-mono">{item.createdAt}</span>
              </div>

              {item.entityType === 'proposed_product' && <p className="rounded-lg bg-purple-50 px-2 py-1.5 text-xs leading-7 text-purple-800">بازبینی فیلدبه‌فیلد این کالا در پنجرهٔ بازبینی انجام می‌شود؛ تأیید نهایی فقط پس از تأیید همهٔ فیلدها ممکن است.</p>}
              {previewEntries.length > 0 && item.entityType !== 'proposed_product' && <dl className="space-y-1 rounded-xl bg-slate-50 p-2 text-xs leading-7">
                {previewEntries.map(([k, v]) => <div key={k} className="flex justify-between gap-2"><dt className="shrink-0 text-slate-400">{reviewLabel(`/${k}`)}:</dt><dd className="min-w-0 truncate font-bold text-slate-800">{typeof v === 'object' && v !== null ? 'چند مقدار — جزئیات را باز کنید' : formatDetailValue(v)}</dd></div>)}
                {Object.keys(item.requestedData || {}).length > 3 && <p className="text-[11px] text-slate-400">و {toPersianDigits(Object.keys(item.requestedData).length - 3)} مورد دیگر…</p>}
              </dl>}

              <div className="flex items-center gap-2 pt-1">
                {item.entityType === 'proposed_product' ? (
                  <button
                    type="button"
                    onClick={() => setInspectingItem(item)}
                    className="flex-1 min-h-11 py-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl flex items-center justify-center gap-1 shadow-2xs"
                  >
                    <Check className="w-4 h-4"  strokeWidth={1.5}/>
                    <span>بازبینی فیلدها</span>
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={() => handleApprove(item)}
                      className="flex-1 min-h-11 py-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl flex items-center justify-center gap-1 shadow-2xs"
                    >
                      <Check className="w-4 h-4"  strokeWidth={1.5}/>
                      <span>تأیید و اعمال</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => handleReject(item)}
                      className="flex-1 min-h-11 py-2.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-xs rounded-xl border border-rose-200 flex items-center justify-center gap-1"
                    >
                      <X className="w-4 h-4"  strokeWidth={1.5}/>
                      <span>رد درخواست</span>
                    </button>
                  </>
                )}
                <button
                  type="button"
                  onClick={() => setInspectingItem(item)}
                  aria-label={`مشاهده جزئیات ${item.title}`}
                  className="min-h-11 min-w-11 p-2.5 bg-slate-100 hover:bg-slate-200 text-slate-600 rounded-xl"
                  title="مشاهده جزئیات داده"
                >
                  <Eye className="w-4 h-4"  strokeWidth={1.5}/>
                </button>
              </div>
            </div>
            );
          })
        )}
      </div>

      {inspectingItem?.entityType === "proposed_product" && <ProductFieldReview id={inspectingItem.id} onClose={()=>setInspectingItem(null)} onUpdated={fetchApprovals}/> }
      {inspectingItem && inspectingItem.entityType !== "proposed_product" && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs">
          <div role="dialog" aria-modal="true" aria-label={`جزئیات ${inspectingItem.title}`} className="bg-white rounded-2xl max-w-lg w-full max-h-[92vh] flex flex-col p-4 shadow-2xl space-y-3 text-sm">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold text-slate-900">{inspectingItem.title}</h4>
              <button type="button" onClick={() => setInspectingItem(null)} aria-label="بستن جزئیات" className="min-h-9 min-w-9 p-1.5 rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors"><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div className="space-y-1.5 overflow-auto">
              {Object.entries(inspectingItem.requestedData).map(([k, v]) => (
                <div key={k} className="p-2 bg-slate-50 rounded-xl flex justify-between gap-2">
                  <span className="shrink-0 text-slate-400">{reviewLabel(`/${k}`)}:</span>
                  <span className="font-bold text-slate-800 text-xs leading-7 text-left">
                    {typeof v === 'object' && v !== null ? <pre className="whitespace-pre-wrap break-words max-h-64 overflow-auto" dir="auto">{formatDetailValue(v)}</pre> : formatDetailValue(v)}
                  </span>
                </div>
              ))}
            </div>
            <div className="flex gap-2 pt-2 border-t">
              <button
                type="button"
                onClick={() => handleApprove(inspectingItem)}
                className="flex-1 min-h-11 py-2.5 bg-emerald-600 text-white font-bold rounded-xl text-sm"
              >
                تأیید درخواست
              </button>
              <button
                type="button"
                onClick={() => handleReject(inspectingItem)}
                className="min-h-11 px-4 py-2.5 bg-rose-50 text-rose-700 font-bold rounded-xl text-sm"
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
