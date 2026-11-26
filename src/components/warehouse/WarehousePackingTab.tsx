import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, CheckCircle2, ChevronDown, FileText, Package, Printer, RefreshCw } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { unitsApi } from '../../services/unitsApi';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import { normalizePersianText } from '../../utils/routing';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

interface ServerPackItem {
  id?: string;
  orderItemId?: string;
  order_item_id?: string;
  productTitle?: string;
  variantName?: string;
  quantity?: number;
  packagingLengthMm?: number;
  packagingWidthMm?: number;
  packagingHeightMm?: number;
}

interface ServerPackOrder {
  id: string;
  orderNumber: string;
  storeName?: string;
  customerName?: string;
  customerCity?: string;
  totalAmount?: number;
  items?: ServerPackItem[];
}

interface Props {
  selectedOrderNumber?: string;
  selectedOrderId?: string;
  onGoToShipping: (orderNumber: string, orderId: string) => void;
}

interface PackedOrder {
  id: string;
  orderNumber: string;
  trackingNumber?: string;
}

const primaryButton = 'flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60';
const secondaryButton = 'flex min-h-11 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const fieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-normal text-slate-900 focus-visible:ring-2 focus-visible:ring-brand';
const boxOptions = [
  ['سایز ۲ (۲۰×۱۵×۱۰ سانتی‌متر)', 'کارتن استاندارد سایز ۲'],
  ['سایز ۳ (۲۵×۲۰×۱۵ سانتی‌متر)', 'کارتن پستی سایز ۳'],
  ['پاکت حباب‌دار A4', 'پاکت پستی ضدضربه'],
];
const dimensionFields = [
  ['packagingLengthMm', 'طول (سانتی‌متر)'],
  ['packagingWidthMm', 'عرض (سانتی‌متر)'],
  ['packagingHeightMm', 'ارتفاع (سانتی‌متر)'],
] as const;

export const WarehousePackingTab: React.FC<Props> = ({ selectedOrderNumber, selectedOrderId, onGoToShipping }) => {
  const [orders, setOrders] = useState<ServerPackOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [showQueue, setShowQueue] = useState(false);
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [packed, setPacked] = useState<PackedOrder | null>(null);
  const [busy, setBusy] = useState(false);
  const receiptHeading = useRef<HTMLHeadingElement>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const data = await apiRequest<{ orders?: ServerPackOrder[] } | ServerPackOrder[]>('/warehouse/orders/ready-to-pack', { auth: true });
      setOrders(Array.isArray(data) ? data : (data.orders || []));
    } catch (cause) {
      setLoadError(cause instanceof Error ? cause.message : 'صف بسته‌بندی دریافت نشد.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);
  useEffect(() => { if (packed) receiptHeading.current?.focus(); }, [packed]);

  const fromPicking = !showQueue && Boolean(selectedOrderId || selectedOrderNumber);
  const activeOrder = fromPicking
    ? orders.find(order => selectedOrderId ? order.id === selectedOrderId : order.orderNumber === selectedOrderNumber)
    : chosenId !== null ? orders.find(order => order.id === chosenId) : orders.length === 1 ? orders[0] : undefined;

  const returnToQueue = () => {
    setChosenId(null);
    setShowQueue(true);
    setPacked(null);
    void reload();
  };

  return <div className="space-y-4 pb-6">
    <SupplierSectionHeading title="میز بسته‌بندی" icon={<Package />} description="اقلام را بررسی کنید و مشخصات بسته را ثبت کنید."
      action={!packed && <button type="button" aria-label="به‌روزرسانی صف بسته‌بندی" onClick={() => void reload()} disabled={loading || busy}
        className="grid size-11 place-items-center rounded-xl border border-slate-200 bg-white text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
        <RefreshCw aria-hidden="true" className="size-4" />
      </button>} />
    {packed ? <section className="space-y-4 rounded-[22px] border border-emerald-200 bg-white p-4 shadow-sm sm:p-5">
      <div role="status" className="space-y-2">
        <h3 ref={receiptHeading} tabIndex={-1} className="flex items-center gap-2 text-base font-extrabold text-emerald-800">
          <CheckCircle2 aria-hidden="true" className="size-5 shrink-0" /> بسته‌بندی سفارش <bdi className="font-mono">{packed.orderNumber}</bdi> ثبت شد
        </h3>
        {packed.trackingNumber && <p className="break-words text-sm text-slate-700">کد رهگیری سامانه <bdi dir="ltr" className="font-mono font-bold">{packed.trackingNumber}</bdi></p>}
      </div>
      <div className="flex flex-col gap-2 sm:flex-row">
        <button type="button" className={primaryButton} onClick={() => onGoToShipping(packed.orderNumber, packed.id)}>ادامه به تحویل <ArrowLeft aria-hidden="true" className="size-4" /></button>
        <button type="button" className={secondaryButton} onClick={returnToQueue}>بازگشت به صف بسته‌بندی</button>
      </div>
    </section> : <>
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && <>
        {!fromPicking && orders.length > 1 && <label className="block rounded-2xl border border-[#e6e2f0] bg-white p-4 text-sm font-bold text-slate-800">
          سفارش آماده بسته‌بندی
          <select aria-label="سفارش آماده بسته‌بندی" className={fieldClass} value={chosenId ?? ''} disabled={busy} onChange={event => setChosenId(event.target.value || null)}>
            <option value="">سفارش را انتخاب کنید</option>
            {orders.map(order => <option key={order.id} value={order.id}>{[order.orderNumber, order.storeName, order.customerName].filter(Boolean).join(' · ')}</option>)}
          </select>
        </label>}
        {activeOrder ? <PackingOrderForm key={activeOrder.id} order={activeOrder} busy={busy} setBusy={setBusy}
          onPacked={trackingNumber => setPacked({ id: activeOrder.id, orderNumber: activeOrder.orderNumber, trackingNumber })} />
          : fromPicking || chosenId ? <SupplierEmptyState icon={<Package />} title="سفارش انتخاب‌شده در صف بسته‌بندی نیست."
            action={<button type="button" className={`${secondaryButton} mx-auto`} onClick={returnToQueue}>مشاهدهٔ صف بسته‌بندی</button>} />
            : !orders.length && <SupplierEmptyState icon={<Package />} title="سفارشی در صف بسته‌بندی وجود ندارد." description="سفارش پس از پایان جمع‌آوری در این صف قرار می‌گیرد." />}
        {fromPicking && activeOrder && <button type="button" className={secondaryButton} disabled={busy} onClick={returnToQueue}>مشاهدهٔ صف بسته‌بندی</button>}
      </>}
    </>}
  </div>;
};

function PackingOrderForm({ order, busy, setBusy, onPacked }: {
  order: ServerPackOrder;
  busy: boolean;
  setBusy: (value: boolean) => void;
  onPacked: (trackingNumber?: string) => void;
}) {
  const { stores } = useStore();
  const { success } = useToast();
  const [weightGrams, setWeightGrams] = useState('');
  const [boxSize, setBoxSize] = useState(boxOptions[0][0]);
  const [dimensions, setDimensions] = useState(() => dimensionFields.map(([field]) => {
    const max = Math.max(0, ...(order.items || []).map(item => item[field] || 0));
    return max > 0 ? String(Math.max(1, Math.round(max / 10))) : '';
  }));
  const [formError, setFormError] = useState('');
  const submitting = useRef(false);
  const matchedStore = stores.find(store => store.name === order.storeName);

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting.current) return;
    const weight = parsePositiveInteger(weightGrams);
    if (!weight) { setFormError('وزن بسته را به گرم و به‌صورت عدد صحیح مثبت وارد کنید.'); return; }
    const filledDimensions = dimensions.filter(value => value.trim()).length;
    const [length, width, height] = dimensions.map(parsePositiveInteger);
    if (filledDimensions && !(length && width && height)) {
      setFormError('طول، عرض و ارتفاع را کامل و به‌صورت عدد صحیح مثبت وارد کنید.');
      return;
    }
    setFormError('');
    submitting.current = true;
    setBusy(true);
    try {
      const result = await apiRequest<{ success?: boolean; status?: string; trackingNumber?: string }>(`/warehouse/orders/${encodeURIComponent(order.id)}/pack`, {
        method: 'POST', auth: true,
        body: JSON.stringify({ weightGrams: weight, boxSize, ...(filledDimensions ? {
          packagingLengthCm: length, packagingWidthCm: width, packagingHeightCm: height,
        } : {}) }),
      });
      if (!result?.success || result.status !== 'packed') throw new Error('بسته‌بندی تأیید نشد؛ صف را دوباره بررسی کنید.');
      onPacked(result.trackingNumber);
      success(`بسته‌بندی سفارش ${order.orderNumber} ثبت شد.`);
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : 'ثبت بسته‌بندی انجام نشد.');
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };

  return <form onSubmit={submit} className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5" aria-busy={busy}>
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[#e6e2f0] pb-3">
      <div className="min-w-0">
        <h3 className="break-words text-base font-extrabold text-slate-900">سفارش <bdi className="font-mono">{order.orderNumber}</bdi></h3>
        {(order.customerName || order.customerCity) && <p className="mt-1 break-words text-sm text-slate-600">{[order.customerName, order.customerCity].filter(Boolean).join(' · ')}</p>}
      </div>
      {order.storeName && <div className="flex min-w-0 items-center gap-2 rounded-lg bg-brand-soft px-2.5 py-1.5 text-sm font-bold text-brand-deep">
        {matchedStore?.logo && <img src={matchedStore.logo} alt="" className="size-6 shrink-0 rounded-md object-cover" />}
        <span className="break-words">{order.storeName}</span>
      </div>}
    </div>

    {order.items?.length ? <ul className="space-y-2">
      {order.items.map((item, index) => {
        const lineId = item.orderItemId ?? item.order_item_id ?? item.id;
        return <li key={lineId ?? index} className="space-y-1 rounded-xl bg-slate-50 p-3 text-sm">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <strong className="min-w-0 break-words text-slate-900">{item.productTitle || 'کالا'}{item.variantName && ` — ${item.variantName}`}</strong>
            <span className="font-bold text-brand-deep">{toPersianDigits(item.quantity || 0)} عدد</span>
          </div>
          {lineId && <PackingUnitCodes orderItemId={lineId} />}
        </li>;
      })}
    </ul> : <p className="text-sm text-slate-600">قلمی برای این سفارش نمایش داده نشده است.</p>}

    <fieldset disabled={busy} className="space-y-3 print:hidden">
      <legend className="mb-2 text-sm font-extrabold text-slate-900">مشخصات بسته</legend>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="text-sm font-semibold text-slate-800">وزن روی ترازو (گرم)
          <input type="text" inputMode="numeric" dir="ltr" name="weightGrams" value={weightGrams} onChange={event => setWeightGrams(event.target.value)} required className={fieldClass} />
        </label>
        <label className="text-sm font-semibold text-slate-800">نوع بسته
          <select value={boxSize} onChange={event => setBoxSize(event.target.value)} className={fieldClass}>
            {boxOptions.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
      </div>
      <details className="group rounded-xl border border-slate-200 px-3">
        <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
          بررسی ابعاد بسته (اختیاری) <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
        </summary>
        <div className="grid grid-cols-1 gap-3 pb-3 sm:grid-cols-3">
          {dimensionFields.map(([field, label], index) => <label key={field} className="text-sm font-semibold text-slate-800">{label}
            <input type="text" inputMode="numeric" dir="ltr" value={dimensions[index]} className={fieldClass}
              onChange={event => setDimensions(current => current.map((value, position) => position === index ? event.target.value : value))} />
          </label>)}
        </div>
      </details>
    </fieldset>

    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[#e6e2f0] pt-3">
      {typeof order.totalAmount === 'number' && <span className="text-sm text-slate-700">مبلغ سفارش <strong>{formatPrice(order.totalAmount)} تومان</strong></span>}
      <div className="flex flex-wrap gap-2">
        <button type="button" onClick={() => window.print()} className={secondaryButton}><FileText aria-hidden="true" className="size-4" />چاپ فاکتور برنددار</button>
        <button type="button" onClick={() => window.print()} className={secondaryButton}><Printer aria-hidden="true" className="size-4" />چاپ فهرست بسته‌بندی</button>
      </div>
    </div>
    {formError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{formError}</p>}
    <button type="submit" disabled={busy} className={`${primaryButton} w-full sm:w-auto`}>
      <Package aria-hidden="true" className="size-4" />{busy ? 'در حال ثبت…' : 'ثبت بسته‌بندی'}
    </button>
  </form>;
}

function parsePositiveInteger(value: string) {
  const normalized = normalizePersianText(value.trim());
  const parsed = Number(normalized);
  return /^\d+$/.test(normalized) && Number.isSafeInteger(parsed) && parsed > 0 ? parsed : 0;
}

function PackingUnitCodes({ orderItemId }: { orderItemId: string }) {
  const [result, setResult] = useState<{ codes: string[]; failed?: boolean } | null>(null);
  useEffect(() => {
    let current = true;
    async function load() {
      const codes: string[] = [];
      let cursor: string | undefined;
      do {
        const page = await unitsApi.listUnits({ order_item_id: orderItemId, cursor, limit: 200 });
        codes.push(...page.units.map(unit => unit.code));
        cursor = page.next_cursor ?? undefined;
      } while (cursor && current);
      if (current) setResult({ codes });
    }
    void load().catch(() => { if (current) setResult({ codes: [], failed: true }); });
    return () => { current = false; };
  }, [orderItemId]);

  if (!result) return <p role="status" className="spncx-pack-unit-code text-xs text-slate-600">در حال دریافت کدهای واحد…</p>;
  if (result.failed) return <p role="alert" className="spncx-pack-unit-code text-xs text-rose-700">کدهای واحد دریافت نشدند.</p>;
  if (!result.codes.length) return null;
  return <div className="spncx-pack-unit-code flex flex-wrap gap-2 text-xs">
    {result.codes.map(code => <code key={code} dir="ltr" className="spncx-pack-unit-code rounded-md bg-white px-2 py-1 font-mono text-brand-deep">{code}</code>)}
  </div>;
}
