import { unitsApi } from '../../services/unitsApi';
import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { Package, Printer, CheckCircle2, FileText, RefreshCw } from 'lucide-react';

// Live contract (live bundles, read-only):
// - GET /warehouse/orders/ready-to-pack -> { orders } (ReturnWindowBadge-MERGED.js, getReadyToPack)
// - POST /warehouse/orders/{id}/pack { weightGrams, boxSize, packagingLengthCm?,
//   packagingWidthCm?, packagingHeightCm? } (index-MERGED.js, completeOrderPacking)
// No customer name, city, product title or tracking code is ever defaulted here:
// with no order in the server queue the tab shows live's honest empty state.
interface ServerPackItem {
  id?: string;
  orderItemId?: string;
  order_item_id?: string;
  productId?: string;
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
  totalAmount?: number;
  paymentStatus?: string;
  trackingCode?: string;
  status: string;
  items?: ServerPackItem[];
}

interface Props {
  selectedOrderNumber?: string;
  onGoToShipping: () => void;
}

export const WarehousePackingTab: React.FC<Props> = ({ selectedOrderNumber, onGoToShipping }) => {
  const { stores, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [orders, setOrders] = useState<ServerPackOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [weightGrams, setWeightGrams] = useState('380');
  const [boxSize, setBoxSize] = useState('سایز ۲ (۲۰×۱۵×۱۰ سانتی‌متر)');
  const [lengthCm, setLengthCm] = useState('');
  const [widthCm, setWidthCm] = useState('');
  const [heightCm, setHeightCm] = useState('');
  const [packed, setPacked] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const data = await apiRequest<{ orders?: ServerPackOrder[] } | ServerPackOrder[]>('/warehouse/orders/ready-to-pack', { method: 'GET', auth: true });
      setOrders(Array.isArray(data) ? data : (data.orders || []));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'صف بسته‌بندی دریافت نشد.';
      setLoadError(message);
      error(message);
    } finally {
      setLoading(false);
    }
  }, [error]);

  useEffect(() => { void reload(); }, [reload]);

  // A selected order that is no longer in this queue must NOT fall back
  // to orders[0]: that silently presented (and could pack) a different
  // store's order. With no match the tab shows its empty state instead.
  const activeOrder = selectedOrderNumber
    ? orders.find((o) => o.orderNumber === selectedOrderNumber)
    : orders[0];

  // Package dimensions default to the largest item packaging dimension
  // (mm -> cm), exactly as live prefills them; the queue items carry no
  // packaging dimensions, so the fields stay empty until staff fill them.
  useEffect(() => {
    const items = activeOrder?.items || [];
    const maxDim = (pick: (item: ServerPackItem) => number | undefined) => {
      let max = 0;
      items.forEach((item) => {
        const value = pick(item);
        if (typeof value === 'number' && value > max) max = value;
      });
      return max > 0 ? String(Math.round(max / 10)) : '';
    };
    setLengthCm(maxDim((item) => item.packagingLengthMm));
    setWidthCm(maxDim((item) => item.packagingWidthMm));
    setHeightCm(maxDim((item) => item.packagingHeightMm));
  }, [activeOrder]);

  const matchedStore = stores.find((s) => s.name === activeOrder?.storeName);
  const storeDisplayName = activeOrder?.storeName || '';
  const handlePrintStoreInvoice = () => {
    if (!activeOrder) { error('سفارشی برای چاپ فاکتور انتخاب نشده است.'); return; }
    success(`پیش‌نمایش فاکتور مشتری با سربرگ «${storeDisplayName}» آماده شد.`);
    window.print();
  };

  const handlePrintPostLabel = () => {
    if (!activeOrder) { error('سفارشی برای چاپ فهرست بسته‌بندی انتخاب نشده است.'); return; }
    success(`فهرست بسته‌بندی سفارش ${activeOrder.orderNumber} برای چاپ آماده شد.`);
    window.print();
  };

  const handleFinishPacking = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeOrder) { error('سفارشی برای بسته‌بندی انتخاب نشده است.'); return; }
    if (submitting) return;
    const dims = [lengthCm, widthCm, heightCm];
    const filled = dims.filter((value) => value.trim() !== '').length;
    if (filled > 0 && filled < 3) {
      error('ابعاد بسته را کامل وارد کنید: طول، عرض و ارتفاع (سانتی‌متر).');
      return;
    }
    const parsed = dims.map((value) => Number(value));
    if (filled === 3 && !parsed.every((value) => Number.isSafeInteger(value) && value > 0)) {
      error('ابعاد بسته باید عددهای صحیح مثبت باشند (سانتی‌متر).');
      return;
    }
    setSubmitting(true);
    try {
      await apiRequest(`/warehouse/orders/${encodeURIComponent(activeOrder.id)}/pack`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({
          weightGrams: Number(weightGrams) || 380,
          boxSize,
          ...(filled === 3
            ? { packagingLengthCm: parsed[0], packagingWidthCm: parsed[1], packagingHeightCm: parsed[2] }
            : {}),
        }),
      });
      setPacked(true);
      success(`بسته سفارش ${activeOrder.orderNumber} با موفقیت پلمب شد.`);
      await reload();
      setTimeout(() => { onGoToShipping(); }, 1500);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت بسته‌بندی ناموفق بود. دوباره تلاش کنید.');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">در حال دریافت صف بسته‌بندی…</div>;
  }

  if (loadError) {
    return (
      <div className="bg-rose-50 rounded-2xl p-6 text-center text-rose-700 text-xs space-y-2">
        <p>{loadError}</p>
        <button onClick={() => void reload()} className="font-bold underline">تلاش مجدد</button>
      </div>
    );
  }

  if (!activeOrder) {
    return (
      <div className="space-y-3.5 pb-6">
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Package className="w-4 h-4" strokeWidth={1.5} />
            <h3 className="text-xs font-bold text-slate-800">میز بسته‌بندی</h3>
          </div>
        </div>
        <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs leading-relaxed">
          سفارشی در صف بسته‌بندی وجود ندارد.
          <span className="block mt-1">سفارش‌هایی که جمع‌آوری آن‌ها در میز جمع‌آوری کامل شود، اینجا نمایش داده می‌شوند.</span>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={handleFinishPacking} className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-1.5 text-blue-600">
            <Package className="w-4 h-4" strokeWidth={1.5} />
            <h3 className="text-xs font-bold text-slate-800">میز بسته‌بندی</h3>
          </div>
          <span className="text-xs font-mono font-black text-slate-800">{activeOrder.orderNumber}</span>
        </div>

        <div className="bg-blue-50/70 border border-blue-200 p-2.5 rounded-xl space-y-1 text-xs">
          <span className="text-[10px] text-blue-600 font-bold block">
            نام ویترین روی فاکتور و برچسب بسته:
          </span>
          <div className="flex items-center gap-2">
            {matchedStore?.logo && (
              <img src={matchedStore.logo} alt={storeDisplayName} className="w-5 h-5 rounded-md object-cover" />
            )}
            <span className="font-extrabold text-blue-900 block">{storeDisplayName}</span>
          </div>
          <p className="text-[10px] text-blue-700/80 leading-relaxed">
            بسته با لوگو و برند فروشگاه دراپ‌شیپر ارسال می‌شود و نامی از پلتفرم مرکزی یا انبار درج نمی‌گردد.
          </p>
        </div>

        <div>
          <div className="space-y-1 text-xs font-bold text-slate-800">
            {activeOrder.items?.length ? activeOrder.items.map((item, index) => {
              const lineId = item.orderItemId ?? item.order_item_id ?? item.id;
              return <div key={lineId ?? index}>
                <span>{item.productTitle || 'کالا'}{item.variantName && ` — ${item.variantName}`} ({toPersianDigits(item.quantity || 0)} عدد)</span>
                {lineId && <PackingUnitCodes key={activeOrder.id + lineId} orderItemId={lineId} />}
              </div>;
            }) : '—'}
          </div>
          <span className="text-[11px] text-slate-500 mt-0.5 block">
            گیرنده: {activeOrder.customerName} • مبلغ: {toPersianDigits(Math.round(activeOrder.totalAmount || 0))} تومان
          </span>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">کارتن بسته‌بندی</label>
            <select
              value={boxSize}
              onChange={(e) => setBoxSize(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            >
              <option value="سایز ۲ (۲۰×۱۵×۱۰ سانتی‌متر)">کارتن استاندارد سایز ۲</option>
              <option value="سایز ۳ (۲۵×۲۰×۱۵ سانتی‌متر)">کارتن پستی سایز ۳</option>
              <option value="پاکت حباب‌دار A4">پاکت پستی ضدضربه</option>
            </select>
          </div>

          <div>
            <label className="text-[10px] text-slate-600 block mb-1">وزن ترازوی دیجیتال (گرم)</label>
            <input
              type="number"
              value={weightGrams}
              onChange={(e) => setWeightGrams(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
              required
            />
          </div>
        </div>

        <div className="grid grid-cols-3 gap-2">
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">طول بسته (سانتی‌متر)</label>
            <input
              type="text"
              inputMode="numeric"
              value={lengthCm}
              onChange={(e) => setLengthCm(e.target.value)}
              placeholder="پیش‌فرض کالا"
              min="1"
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">عرض بسته (سانتی‌متر)</label>
            <input
              type="text"
              inputMode="numeric"
              value={widthCm}
              onChange={(e) => setWidthCm(e.target.value)}
              placeholder="پیش‌فرض کالا"
              min="1"
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            />
          </div>
          <div>
            <label className="text-[10px] text-slate-600 block mb-1">ارتفاع بسته (سانتی‌متر)</label>
            <input
              type="text"
              inputMode="numeric"
              value={heightCm}
              onChange={(e) => setHeightCm(e.target.value)}
              placeholder="پیش‌فرض کالا"
              min="1"
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            />
          </div>
        </div>
        <p className="text-[10px] text-slate-500 leading-5">
          ابعاد بسته از پیش‌فرض بسته‌بندی کالاها پر می‌شود (بزرگ‌ترین بُعد)؛ پیش از پلمب آن را با بسته واقعی چک و در صورت نیاز اصلاح کنید. خالی گذاشتنش یعنی بسته تأییدشده‌ای ثبت نمی‌شود.
        </p>

        <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
          <button
            type="button"
            onClick={handlePrintStoreInvoice}
            className="py-2 px-3 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1.5 transition-colors"
          >
            <FileText className="w-3.5 h-3.5 text-blue-600" strokeWidth={1.5} />
            <span>چاپ فاکتور برنددار</span>
          </button>

          <button
            type="button"
            onClick={handlePrintPostLabel}
            className="py-2 px-3 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1.5 transition-colors"
          >
            <Printer className="w-3.5 h-3.5 text-blue-600" strokeWidth={1.5} />
            <span>چاپ فهرست بسته‌بندی</span>
          </button>
        </div>

        <button
          type="submit"
          disabled={submitting}
          className="w-full py-2.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-60 disabled:cursor-wait text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5"
        >
          {submitting ? (
            <span>در حال ثبت بسته‌بندی…</span>
          ) : packed ? (
            <>
              <CheckCircle2 className="w-4 h-4" strokeWidth={1.5} />
              <span>بسته پلمب شد • انتقال به سالن تحویل به حمل‌ونقل</span>
            </>
          ) : (
            <>
              <Package className="w-4 h-4" strokeWidth={1.5} />
              <span>تأیید پلمب بسته و تحویل به حمل‌ونقل</span>
            </>
          )}
        </button>
      </div>

      <div className="flex justify-end">
        <button
          type="button"
          onClick={() => void reload()}
          disabled={loading}
          className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50"
          title="به‌روزرسانی"
        >
          <RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} strokeWidth={1.5} />
        </button>
      </div>
    </form>
  );
};

function PackingUnitCodes({ orderItemId }: { orderItemId: string }) {
  const [codes, setCodes] = useState<string[]>([]);
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
      if (current) setCodes(codes);
    }
    void load().catch(() => { if (current) setCodes([]); });
    return () => { current = false; };
  }, [orderItemId]);

  if (!codes.length) return null;
  return <div className="spncx-pack-unit-code flex flex-wrap gap-x-2 text-[11px]">
    {codes.map(code => <code key={code} dir="ltr" className="spncx-pack-unit-code font-mono">{code}</code>)}
  </div>;
}
