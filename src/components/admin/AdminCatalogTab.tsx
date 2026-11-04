import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { Package, Check, X, ShieldCheck, Tag, Search, Filter, Eye, AlertTriangle } from 'lucide-react';

export const AdminCatalogTab: React.FC = () => {
  const {
    products,
    proposedProducts,
    approveAndMergeProduct,
    rejectProposedProduct,
    toggleProductOriginality,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, warning } = useToast();
  const [approvedIds, setApprovedIds] = useState<{ [key: string]: boolean }>({});
  const [rejectingItem, setRejectingItem] = useState<any | null>(null);
  const [inspectingItem, setInspectingItem] = useState<any | null>(null);

  // Requirement 16: Multi-Filter Search Bar
  const [searchQuery, setSearchQuery] = useState('');
  const [supplierFilter, setSupplierFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState<'all' | 'pending' | 'approved'>('all');

  const pendingApprovals = proposedProducts.filter((p) => {
    if (statusFilter === 'pending' && p.status !== 'pending') return false;
    if (statusFilter === 'approved' && p.status !== 'approved') return false;
    if (supplierFilter !== 'all' && p.supplierId !== supplierFilter) return false;
    if (searchQuery) {
      const q = searchQuery.toLowerCase();
      return p.title.toLowerCase().includes(q) || (p.barcode || '').includes(q) || p.supplierName.toLowerCase().includes(q);
    }
    return true;
  });

  const handleApprove = async (item: any) => {
    setApprovedIds(prev => ({ ...prev, [item.id]: true }));
    await approveAndMergeProduct(item);
    success(`کالای "${item.title}" تأیید و به کاتالوگ مرکزی اضافه شد.`);
  };

  const handleConfirmReject = () => {
    if (!rejectingItem) return;
    rejectProposedProduct(rejectingItem, 'عدم انطباق کیفی مشخصات فنی');
    warning(`کالای "${rejectingItem.title}" به دلیل عدم انطباق کیفی به تأمین‌کننده عودت شد.`);
    setRejectingItem(null);
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* Requirement 16: Multi-Filter Search Bar */}
      <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800 flex items-center gap-1.5">
            <Filter className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
            <span>جستجو و فیلتر پیشرفته صف تأیید کالا</span>
          </h3>
          <span className="text-[10px] text-slate-400">
            {toPersianDigits(pendingApprovals.length)} مورد
          </span>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 text-xs">
          <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5">
            <Search className="w-3.5 h-3.5 text-slate-400 ml-1.5"  strokeWidth={1.5}/>
            <input
              type="text"
              placeholder="جستجوی نام، بارکد یا تأمین‌کننده..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
            />
          </div>

          <select
            value={supplierFilter}
            onChange={(e) => setSupplierFilter(e.target.value)}
            className="p-1.5 bg-slate-50 border border-slate-200 rounded-xl text-xs"
          >
            <option value="all">همه تأمین‌کنندگان</option>
            <option value="sup-technolife">تکنولایف</option>
            <option value="sup-mashhad-leather">صنایع چرم مشهد</option>
            <option value="sup-khanoumi">خانومی</option>
          </select>

          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as any)}
            className="p-1.5 bg-slate-50 border border-slate-200 rounded-xl text-xs"
          >
            <option value="all">همه وضعیت‌ها</option>
            <option value="pending">در انتظار بررسی</option>
            <option value="approved">تأییدشده</option>
          </select>
        </div>
      </div>

      {/* Pending Approval Queue */}
      <div className="bg-white rounded-2xl p-3.5 border border-blue-200 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Package className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">صف کالاهای پیشنهادی تأمین‌کنندگان</h3>
          </div>
          <span className="text-[10px] bg-blue-100 text-blue-700 font-bold px-2 py-0.5 rounded-full">
            تطبیق بارکد و کیفیت
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          بارکد کارخانه به تنهایی اثبات یکسان‌بودن مشخصات نیست؛ مدیر پلتفرم تطبیق مشخصات، گارانتی و کیفیت محتوا را بررسی و تأیید می‌کند (بخش ۴ پروپوزال).
        </p>

        <div className="space-y-2 pt-1">
          {pendingApprovals.map((item) => {
            const isApproved = approvedIds[item.id];
            return (
              <div
                key={item.id}
                onClick={() => setInspectingItem(item)}
                className="p-3 bg-slate-50 hover:bg-blue-50/50 cursor-pointer rounded-xl border border-slate-200/80 space-y-2 transition-colors"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-1.5">
                    <Eye className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>
                    <h4 className="text-xs font-bold text-slate-800">{item.title}</h4>
                  </div>
                  <span className="text-[9px] font-mono bg-white px-2 py-0.5 rounded border border-slate-200" dir="ltr">
                    {item.barcode}
                  </span>
                </div>

                <div className="flex items-center justify-between text-[10px] text-slate-500">
                  <span>تأمین‌کننده: {item.supplierName}</span>
                  <span>قیمت پیشنهادی (C): <strong className="text-slate-800">{formatPrice(item.proposedCost)} ت</strong></span>
                </div>

                {isApproved ? (
                  <div className="p-1.5 bg-emerald-50 text-emerald-700 text-[10px] font-bold rounded-lg text-center">
                    کالا به کاتالوگ مرکزی اضافه و برای تمام ویترین‌ها فعال شد.
                  </div>
                ) : (
                  <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-200" onClick={(e) => e.stopPropagation()}>
                    <button
                      onClick={() => handleApprove(item)}
                      className="py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <Check className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>تأیید و ادغام در کاتالوگ</span>
                    </button>
                    <button
                      onClick={() => setRejectingItem(item)}
                      className="py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>عدم انطباق مشخصات</span>
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Active Catalog Overview with Originality Badges (Requirement 23) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-bold text-slate-800">کاتالوگ فعال و اصالت کالاها (الزام حقوقی بج غیر اصل)</h4>
          <span className="text-[10px] text-slate-400">{toPersianDigits(products.length)} محصول مرجع</span>
        </div>
        <p className="text-[10px] text-slate-500">
          برای کالاهای غیر اصل و های‌کپی، وضعیت را تعیین کنید تا بج «غیر اصل» در تمام ویترین‌ها درج شود.
        </p>

        <div className="space-y-1.5 pt-1">
          {products.slice(0, 6).map((p) => {
            const isOrig = p.isOriginal !== false;
            return (
              <div key={p.id} className="p-2 bg-slate-50 border border-slate-100 rounded-xl flex items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <img src={p.image} alt={p.title} className="w-7 h-7 rounded-lg object-cover border" />
                  <span className="font-bold text-slate-800 truncate max-w-[180px]">{p.title}</span>
                </div>

                <div className="flex items-center gap-2">
                  <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                    isOrig ? 'bg-emerald-50 text-emerald-700' : 'bg-amber-100 text-amber-800 font-black'
                  }`}>
                    {isOrig ? 'اصل' : 'غیر اصل (های‌کپی)'}
                  </span>
                  <button
                    onClick={() => {
                      toggleProductOriginality(p.id, !isOrig);
                      success(`وضعیت کالای ${p.title} تغییر کرد.`);
                    }}
                    className="px-2 py-0.5 bg-white border border-slate-200 text-slate-700 text-[10px] rounded-lg"
                  >
                    تغییر
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Requirement 16: Proposed Product Details Modal */}
      {inspectingItem && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold text-slate-900">مشخصات فنی کالای پیشنهادی</h4>
              <button onClick={() => setInspectingItem(null)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div className="flex gap-2.5 items-center">
              <img
                src={inspectingItem.image || 'https://images.unsplash.com/photo-1523275335684-37898b6baf30?w=200'}
                alt={inspectingItem.title}
                className="w-16 h-16 rounded-xl object-cover border shrink-0"
              />
              <div>
                <h5 className="font-bold text-slate-900 text-xs">{inspectingItem.title}</h5>
                <span className="text-[10px] text-slate-400 block mt-0.5">تأمین‌کننده: {inspectingItem.supplierName}</span>
                <span className="text-[10px] font-mono text-blue-600 block mt-0.5" dir="ltr">بارکد: {inspectingItem.barcode}</span>
              </div>
            </div>
            <div className="bg-slate-50 p-2.5 rounded-xl space-y-1 text-[11px]">
              <div className="flex justify-between">
                <span>قیمت عمده پیشنهادی (C):</span>
                <strong className="text-slate-900 font-mono">{formatPrice(inspectingItem.proposedCost)} ت</strong>
              </div>
              <div className="flex justify-between">
                <span>گارانتی:</span>
                <span>{inspectingItem.warranty || 'ضمانت سلامت فیزیکی'}</span>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2 pt-2 border-t">
              <button
                onClick={() => handleApprove(inspectingItem)}
                className="py-2 bg-emerald-600 hover:bg-emerald-700 text-white font-bold rounded-xl"
              >
                تأیید و افزودن
              </button>
              <button
                onClick={() => {
                  setRejectingItem(inspectingItem);
                  setInspectingItem(null);
                }}
                className="py-2 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold rounded-xl"
              >
                رد کالا
              </button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        isOpen={!!rejectingItem}
        title="رد صلاحیت و عودت کالا"
        message={`آیا از رد درخواست کالای "${rejectingItem?.title}" به دلیل عدم انطباق کیفی اطمینان دارید؟ این اقدام برای تأمین‌کننده پیامک خواهد شد.`}
        confirmText="تأیید عودت کالا"
        isDanger={true}
        onConfirm={handleConfirmReject}
        onCancel={() => setRejectingItem(null)}
      />
    </div>
  );
};
