import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { isMockMode } from '../../services/apiClient';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { AdminApprovalInbox } from './AdminApprovalInbox';
import { AdminStoresListView } from './AdminNewTabs';
import { Search, Globe, Check, X, Clock, Store, Lock, Unlock, DollarSign, Package, TrendingUp, Layers, Inbox } from 'lucide-react';

export const AdminStoresTab: React.FC = () => {
  return isMockMode() ? <SampleStoreManagement /> : <StoreManagement />;
};

function StoreManagement() {
  const { stores } = useStore();
  const [section, setSection] = useState<'stores' | 'inbox'>('stores');
  const [search, setSearch] = useState('');
  const query = search.trim().toLowerCase();
  const filtered = stores.filter(store => store.name.toLowerCase().includes(query) || store.slug.toLowerCase().includes(query));

  return (
    <div className="space-y-3 pb-6">
      <div role="group" aria-label="مدیریت ویترین‌ها" className="grid grid-cols-2 gap-1 rounded-xl bg-slate-100 p-1">
        {(['stores', 'inbox'] as const).map(tab => (
          <button key={tab} type="button" aria-pressed={section === tab} onClick={() => setSection(tab)}
            className={`min-h-11 rounded-lg px-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-brand ${section === tab ? 'bg-white text-brand-deep' : 'text-slate-700'}`}>
            {tab === 'stores' ? 'ویترین‌ها' : 'صندوق تأییدها'}
          </button>
        ))}
      </div>
      {section === 'inbox' ? <AdminApprovalInbox /> : (
        <section aria-labelledby="store-management-heading" className="space-y-3">
          <div className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
            <h3 id="store-management-heading" className="text-sm font-bold text-slate-900">ویترین‌های ثبت‌شده</h3>
            <p className="text-sm leading-6 text-slate-600">تعلیق ویترین و تغییر تنظیمات حاشیه سود از این صفحه در دسترس نیست.</p>
            <label className="block space-y-1">
              <span className="block text-sm font-bold text-slate-700">جست‌وجوی نام یا نامک فروشگاه</span>
              <input type="search" value={search} onChange={event => setSearch(event.target.value)}
                className="min-h-11 w-full rounded-xl border border-slate-300 px-3 text-base focus-visible:outline-2 focus-visible:outline-brand" />
            </label>
          </div>
          {query && filtered.length === 0
            ? <p className="rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-600">ویترینی با این عبارت پیدا نشد.</p>
            : <AdminStoresListView stores={filtered} />}
        </section>
      )}
    </div>
  );
}

function SampleStoreManagement() {
  const {
    stores,
    storeStatuses,
    toggleStoreStatus,
    storeNameChangeRequests,
    approveStoreNameChange,
    rejectStoreNameChange,
    storeSlugChangeRequests,
    approveStoreSlugChange,
    rejectStoreSlugChange,
    customDomainRequests,
    approveCustomDomain,
    rejectCustomDomain,
    unifiedPendingApprovals,
    updateStoreAdminMarginOverride,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, warning } = useToast();
  const [subTab, setSubTab] = useState<'stores' | 'name_requests' | 'domain_requests' | 'inbox'>('stores');
  const [search, setSearch] = useState('');
  const [inspectingStore, setInspectingStore] = useState<any | null>(null);
  const [marginInput, setMarginInput] = useState<string>('20');
  const [isLockedInput, setIsLockedInput] = useState<boolean>(false);
  const [suspendingStore, setSuspendingStore] = useState<{ id: string; name: string; current: 'active' | 'suspended' } | null>(null);

  const pendingNames = storeNameChangeRequests.filter(r => r.status === 'pending');
  const pendingDomains = customDomainRequests.filter(r => r.status === 'pending');
  const pendingTotal = unifiedPendingApprovals?.filter(r => r.status === 'pending').length || 0;

  const handleOpenStore = (store: any) => {
    setInspectingStore(store);
    setMarginInput(
      (store.customStoreMarginOverride !== undefined
        ? store.customStoreMarginOverride
        : store.profitMarginPercent || 20
      ).toString()
    );
    setIsLockedInput(Boolean(store.isMarginLockedByAdmin));
  };

  const handleSaveStoreMarginOverride = () => {
    if (!inspectingStore) return;
    const num = Number(marginInput);
    if (isNaN(num) || num < 0) {
      warning('درصد سود نامعتبر است.');
      return;
    }
    updateStoreAdminMarginOverride(inspectingStore.id, num, isLockedInput);
    success(`سود پایه فروشگاه «${inspectingStore.name}» به ${toPersianDigits(num)}٪ تنظیم شد.`);
    setInspectingStore({
      ...inspectingStore,
      customStoreMarginOverride: num,
      isMarginLockedByAdmin: isLockedInput,
    });
  };

  const handleToggleStatus = () => {
    if (!suspendingStore) return;
    const nextStatus = toggleStoreStatus(suspendingStore.id);
    if (nextStatus === 'suspended') {
      warning(`فروشگاه "${suspendingStore.name}" به حالت تعلیق درآمد و Session Guard سلر مسدود شد.`);
    } else {
      success(`فروشگاه "${suspendingStore.name}" با موفقیت مجدداً فعال گردید.`);
    }
    setSuspendingStore(null);
  };

  const filtered = stores.filter(s => s.name.includes(search) || s.slug.includes(search));

  return (
    <div className="space-y-3 pb-6">
      {/* Navigation Sub-Tabs */}
      <div className="flex bg-slate-100 p-1 rounded-xl text-xs font-bold gap-1">
        <button
          type="button"
          onClick={() => setSubTab('stores')}
          className={`flex-1 py-1.5 rounded-lg transition-all ${
            subTab === 'stores' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'
          }`}
        >
          ویترین‌ها ({toPersianDigits(stores.length)})
        </button>
        <button
          type="button"
          onClick={() => setSubTab('name_requests')}
          className={`flex-1 py-1.5 rounded-lg transition-all flex items-center justify-center gap-1 ${
            subTab === 'name_requests' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'
          }`}
        >
          <span>تغییر نام</span>
          {pendingNames.length > 0 && (
            <span className="bg-amber-500 text-white text-[9px] px-1.5 py-0.2 rounded-full">
              {toPersianDigits(pendingNames.length)}
            </span>
          )}
        </button>
        <button
          type="button"
          onClick={() => setSubTab('domain_requests')}
          className={`flex-1 py-1.5 rounded-lg transition-all flex items-center justify-center gap-1 ${
            subTab === 'domain_requests' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'
          }`}
        >
          <span>دامنه‌ها</span>
          {pendingDomains.length > 0 && (
            <span className="bg-blue-600 text-white text-[9px] px-1.5 py-0.2 rounded-full">
              {toPersianDigits(pendingDomains.length)}
            </span>
          )}
        </button>
        <button
          type="button"
          onClick={() => setSubTab('inbox')}
          className={`flex-1 py-1.5 rounded-lg transition-all flex items-center justify-center gap-1 ${
            subTab === 'inbox' ? 'bg-white text-indigo-700 shadow-2xs font-extrabold' : 'text-slate-600'
          }`}
        >
          <Inbox className="w-3.5 h-3.5 text-indigo-600"  strokeWidth={1.5}/>
          <span>صندوق تاییدات</span>
          {pendingTotal > 0 && (
            <span className="bg-indigo-600 text-white text-[9px] px-1.5 py-0.2 rounded-full font-mono">
              {toPersianDigits(pendingTotal)}
            </span>
          )}
        </button>
      </div>

      {subTab === 'stores' && (
        <>
          {/* Search Header */}
      <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">
            فروشگاه‌های دراپ‌شیپر عضو پلتفرم ({toPersianDigits(stores.length)} ویترین)
          </h3>
          <span className="text-[10px] text-blue-600 bg-blue-50 px-2 py-0.5 rounded-full font-bold">
            نظارت و تأیید نامک
          </span>
        </div>

        <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-2.5 py-1.5">
          <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5"  strokeWidth={1.5}/>
          <input
            type="text"
            placeholder="جستجوی نام فروشگاه یا اسلاگ..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
        </div>
      </div>

      {/* Stores List */}
      <div className="space-y-2.5">
        {filtered.map((store) => {
          const status = storeStatuses[store.id] || 'active';
          return (
            <div
              key={store.id}
              className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5"
            >
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <div className="flex items-center gap-1.5">
                    <span className="text-xs font-bold text-slate-900">{store.name}</span>
                    <span className={`text-[9px] font-bold px-1.5 py-0.2 rounded-full ${
                      status === 'active' 
                        ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' 
                        : 'bg-rose-50 text-rose-700 border border-rose-200'
                    }`}>
                      {status === 'active' ? 'فعال' : 'معلق'}
                    </span>
                  </div>
                  <span className="text-[10px] text-slate-400 font-mono mt-0.5 block" dir="ltr">
                    sellershow.ir/store/{store.slug}
                  </span>
                </div>

                <div className="text-left">
                  <span className="text-xs font-extrabold text-slate-900">
                    {formatPrice(store.totalSales * 450000)} ت
                  </span>
                  <span className="text-[9px] text-slate-400 block mt-0.5">
                    {toPersianDigits(store.totalSales)} سفارش
                  </span>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600">
                <div>مالک: <strong className="text-slate-800">{store.ownerName}</strong></div>
                <div>حاشیه سود تعریف‌شده: <strong className="text-blue-600">٪{toPersianDigits(store.profitMarginPercent)}</strong></div>
              </div>

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
                <span className="text-[10px] text-slate-400">پلن: طلایی یک‌ساله (سقف ۹,۹۹۹ کالا)</span>
                <div className="flex items-center gap-1.5">
                  <button
                    onClick={() => setInspectingStore(store)}
                    className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 transition-colors"
                  >
                    شناسنامه ویترین
                  </button>
                  <button
                    onClick={() => setSuspendingStore({ id: store.id, name: store.name, current: status })}
                    className={`px-2.5 py-1 text-[10px] font-bold rounded-lg transition-colors ${
                      status === 'active'
                        ? 'bg-rose-50 text-rose-700 hover:bg-rose-100'
                        : 'bg-emerald-50 text-emerald-700 hover:bg-emerald-100'
                    }`}
                  >
                    {status === 'active' ? 'تعلیق فروشگاه' : 'فعال‌سازی مجدد'}
                  </button>
                </div>
              </div>
            </div>
          );
        })}
      </div>
      </>
      )}

      {/* Sub-Tab 2: Store Name Change Requests */}
      {subTab === 'name_requests' && (
        <div className="space-y-2.5">
          <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-1">
            <h4 className="text-xs font-bold text-slate-800">صف درخواست‌های تغییر نام فروشگاه</h4>
            <p className="text-[10px] text-slate-500">
              بررسی تغییر نام ویترین‌ها جهت عدم تکراری بودن نامک و حفظ تعهدات طرفین قرارداد.
            </p>
          </div>

          {storeNameChangeRequests.length === 0 ? (
            <div className="bg-white rounded-2xl p-8 text-center text-slate-400 text-xs">
              هیچ درخواستی در صف تغییر نام فروشگاه‌ها وجود ندارد.
            </div>
          ) : (
            storeNameChangeRequests.map((req) => (
              <div key={req.id} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2 text-xs">
                <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                  <div>
                    <span className="font-bold text-slate-800">{req.currentName}</span>
                    <span className="text-[10px] text-slate-400 block font-mono">{req.id} • {req.sellerMobile}</span>
                  </div>
                  <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                    req.status === 'approved' ? 'bg-emerald-50 text-emerald-700' :
                    req.status === 'rejected' ? 'bg-rose-50 text-rose-700' :
                    'bg-amber-50 text-amber-700'
                  }`}>
                    {req.status === 'approved' ? 'تأییدشده' : req.status === 'rejected' ? 'رد شده' : 'در انتظار بررسی'}
                  </span>
                </div>

                <div className="p-2.5 bg-blue-50/60 border border-blue-100 rounded-xl space-y-1 text-[11px]">
                  <span className="text-slate-500 block text-[10px]">نام درخواستی جدید:</span>
                  <span className="font-extrabold text-blue-900 text-xs block">{req.requestedName}</span>
                  <span className="text-slate-400 text-[9px] block">تاریخ درخواست: {req.requestDate}</span>
                </div>

                {req.status === 'pending' ? (
                  <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
                    <button
                      type="button"
                      onClick={() => {
                        approveStoreNameChange(req.id);
                        success(`نام فروشگاه با موفقیت به «${req.requestedName}» تغییر یافت.`);
                      }}
                      className="py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <Check className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>تأیید تغییر نام</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        rejectStoreNameChange(req.id);
                        warning(`درخواست تغییر نام رد شد.`);
                      }}
                      className="py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>رد درخواست</span>
                    </button>
                  </div>
                ) : (
                  <div className="text-[10px] text-slate-400 text-center pt-1 border-t border-slate-100">
                    وضعیت این درخواست نهایی شده است.
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}

      {/* Sub-Tab 3: Custom Domain Requests */}
      {subTab === 'domain_requests' && (
        <div className="space-y-2.5">
          <div className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-1">
            <h4 className="text-xs font-bold text-slate-800">صف درخواست‌های اتصال دامنه‌های اختصاصی</h4>
            <p className="text-[10px] text-slate-500">
              بررسی صحت رکوردهای DNS دامنه‌های درخواستی فروشندگان و تخصیص گواهی امنیتی SSL توسط ادمین.
            </p>
          </div>

          {customDomainRequests.length === 0 ? (
            <div className="bg-white rounded-2xl p-8 text-center text-slate-400 text-xs">
              هیچ درخواستی در صف اتصال دامنه وجود ندارد.
            </div>
          ) : (
            customDomainRequests.map((dom) => (
              <div key={dom.id} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2 text-xs">
                <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                  <div>
                    <span className="font-bold text-slate-800">{dom.storeName}</span>
                    <span className="text-[10px] text-slate-400 block font-mono">{dom.id}</span>
                  </div>
                  <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full ${
                    dom.status === 'approved' ? 'bg-emerald-50 text-emerald-700' :
                    dom.status === 'rejected' ? 'bg-rose-50 text-rose-700' :
                    'bg-blue-50 text-blue-700'
                  }`}>
                    {dom.status === 'approved' ? 'فعال' : dom.status === 'rejected' ? 'رد شده' : 'بررسی DNS'}
                  </span>
                </div>

                <div className="p-2.5 bg-slate-50 rounded-xl space-y-1 text-[11px]">
                  <span className="text-slate-400 block text-[10px]">دامنه درخواستی:</span>
                  <span className="font-mono font-extrabold text-blue-800 text-xs block" dir="ltr">{dom.requestedDomain}</span>
                  <span className="text-slate-400 text-[9px] block">تاریخ ثبت: {dom.requestDate}</span>
                </div>

                {dom.status === 'pending' ? (
                  <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
                    <button
                      type="button"
                      onClick={() => {
                        approveCustomDomain(dom.id);
                        success(`دامنه ${dom.requestedDomain} با موفقیت تایید و متصل شد.`);
                      }}
                      className="py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <Check className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>تأیید و اتصال دامنه</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        rejectCustomDomain(dom.id);
                        warning(`درخواست اتصال دامنه رد شد.`);
                      }}
                      className="py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[10px] rounded-lg flex items-center justify-center gap-1"
                    >
                      <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>عدم انطباق DNS</span>
                    </button>
                  </div>
                ) : (
                  <div className="text-[10px] text-slate-400 text-center pt-1 border-t border-slate-100">
                    این دامنه تعیین تکلیف شده است.
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}

      {/* Sub-Tab 4: Unified Approval Inbox */}
      {subTab === 'inbox' && <AdminApprovalInbox />}

      <ConfirmDialog
        isOpen={!!suspendingStore}
        title={suspendingStore?.current === 'active' ? 'تعلیق دسترسی فروشگاه' : 'فعال‌سازی مجدد فروشگاه'}
        message={
          suspendingStore?.current === 'active'
            ? `آیا از تعلیق موقت فروشگاه "${suspendingStore?.name}" اطمینان دارید؟ در این وضعیت، دسترسی خریداران به ویترین و تسویه‌حساب‌ها متوقف خواهد شد.`
            : `آیا مایل به فعال‌سازی مجدد فروشگاه "${suspendingStore?.name}" هستید؟`
        }
        confirmText={suspendingStore?.current === 'active' ? 'تأیید تعلیق' : 'تأیید فعال‌سازی'}
        isDanger={suspendingStore?.current === 'active'}
        onConfirm={handleToggleStatus}
        onCancel={() => setSuspendingStore(null)}
      />

      {inspectingStore && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl max-w-lg w-full p-4 border border-slate-200 shadow-2xl space-y-3.5 text-xs max-h-[90vh] overflow-y-auto no-scrollbar">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
              <div>
                <h4 className="font-extrabold text-slate-900 text-sm">{inspectingStore.name}</h4>
                <span className="text-[10px] text-slate-400 font-mono" dir="ltr">/{inspectingStore.slug}</span>
              </div>
              <button
                onClick={() => setInspectingStore(null)}
                className="px-2.5 py-1 rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-600 text-xs font-bold"
              >
                بستن
              </button>
            </div>

            {/* Financial & Sales Summary */}
            <div className="grid grid-cols-3 gap-2 text-center">
              <div className="p-2.5 bg-slate-50 rounded-xl border border-slate-100">
                <span className="text-[10px] text-slate-400 block">تعداد فروش</span>
                <span className="font-black text-slate-800 text-sm">{toPersianDigits(inspectingStore.totalSales)}</span>
              </div>
              <div className="p-2.5 bg-slate-50 rounded-xl border border-slate-100">
                <span className="text-[10px] text-slate-400 block">گردش کل فروش</span>
                <span className="font-black text-slate-800 text-xs font-mono">{formatPrice(inspectingStore.totalSales * 450000)} ت</span>
              </div>
              <div className="p-2.5 bg-emerald-50 rounded-xl border border-emerald-100">
                <span className="text-[10px] text-emerald-700 block font-bold">سود سلر</span>
                <span className="font-black text-emerald-800 text-xs font-mono">{formatPrice(Math.round(inspectingStore.totalSales * 450000 * 0.12))} ت</span>
              </div>
            </div>

            {/* General Info */}
            <div className="space-y-1.5 p-2.5 bg-slate-50 rounded-xl text-[11px] text-slate-600">
              <div className="flex justify-between">
                <span>مالک فروشگاه:</span>
                <strong className="text-slate-800">{inspectingStore.ownerName}</strong>
              </div>
              <div className="flex justify-between">
                <span>دسته‌بندی‌های فعال:</span>
                <span className="text-slate-700">{inspectingStore.activeCategories?.join('، ') || 'همه رسته‌ها'}</span>
              </div>
              <div className="flex justify-between">
                <span>پلن فعال:</span>
                <span className="font-bold text-blue-700">{inspectingStore.activePlanId || 'رایگان تریال'}</span>
              </div>
            </div>

            {/* Requirement 13: Management Margin Override & Lock */}
            <div className="p-3 bg-amber-50/70 border border-amber-200/80 rounded-xl space-y-2.5">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-1.5 text-amber-900 font-bold text-xs">
                  {isLockedInput ? <Lock className="w-4 h-4 text-amber-600"  strokeWidth={1.5}/> : <Unlock className="w-4 h-4 text-amber-600"  strokeWidth={1.5}/>}
                  <span>تنظیم دستی حاشیه سود پایه این ویترین (توسط ادمین)</span>
                </div>
              </div>
              <p className="text-[10px] text-amber-700 leading-relaxed">
                این درصد، اولویتی بالاتر از سود دیفالت ۲۰٪ سراسری دارد. اگر قفل مدیریت را فعال کنید، سلر امکان بازنویسی آن را نخواهد داشت.
              </p>

              <div className="grid grid-cols-2 gap-2 items-center pt-1">
                <div>
                  <label className="text-[10px] font-bold text-slate-700 block mb-1">درصد سود پیش‌فرض (٪):</label>
                  <input
                    type="number"
                    min="0"
                    max="100"
                    value={marginInput}
                    onChange={(e) => setMarginInput(e.target.value)}
                    className="w-full p-2 bg-white border border-slate-200 rounded-xl font-bold font-mono text-xs"
                  />
                </div>
                <label className="flex items-center gap-2 mt-4 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={isLockedInput}
                    onChange={(e) => setIsLockedInput(e.target.checked)}
                    className="w-4 h-4 accent-amber-600 rounded"
                  />
                  <span className="text-[11px] font-bold text-slate-800">قفل توسط مدیریت</span>
                </label>
              </div>

              <button
                type="button"
                onClick={handleSaveStoreMarginOverride}
                className="w-full py-2 bg-amber-600 hover:bg-amber-700 text-white font-bold rounded-xl text-xs shadow-2xs transition-colors"
              >
                ذخیره تنظیمات حاشیه سود این فروشگاه
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
