import React, { useState, useEffect } from 'react';
import { useToast } from '../../context/ToastContext';
import { wmsApi, WarehouseBinDto } from '../../services/wmsApi';
import { X, Layers, Search, MapPin, Package } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  isOpen: boolean;
  onClose: () => void;
}

export const WarehouseBinLookupModal: React.FC<Props> = ({ isOpen, onClose }) => {
  const toPersianDigits = (value: number) => new Intl.NumberFormat('fa-IR').format(value);
  const { success, error } = useToast();
  const [binQuery, setBinQuery] = useState('');
  const [serverBins, setServerBins] = useState<WarehouseBinDto[]>([]);

  useEffect(() => {
    if (isOpen) {
      wmsApi.getBins()
        .then((data) => {
          if (Array.isArray(data)) setServerBins(data);
        })
        .catch(() => setServerBins([]));
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const binContents: { [key: string]: any } = {};

  serverBins.forEach((b) => {
    binContents[b.binCode] = {
      hall: b.hallName,
      aisle: b.aisle,
      shelf: b.shelf,
      capacity: `${toPersianDigits(b.capacityPercent)}٪ پر`,
      items: b.productTitle ? [{ title: b.productTitle, count: b.quantity, barcode: b.productId || '-' }] : [],
    };
  });

  const currentData = binContents[binQuery.trim().toUpperCase()] || null;

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    if (!binQuery.trim()) return;
    if (binContents[binQuery.trim().toUpperCase()]) {
      success(`موجودی قفسه ${binQuery} با موفقیت استعلام شد.`);
    } else {
      error(`قفسه ${binQuery} خالی است یا در نقشه انبار تعریف نشده است.`);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[420px] w-full p-4 shadow-2xl border border-slate-100 flex flex-col max-h-[85vh] overflow-hidden space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-amber-50 text-amber-600 flex items-center justify-center">
              <Layers className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">استعلام جایگاه فیزیکی قفسه (Bin)</h3>
              <span className="text-[10px] text-slate-400 block">مشاهده اقلام دپو شده در هر قفسه</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* Search */}
        <form onSubmit={handleSearch} className="flex gap-1.5">
          <input
            type="text"
            placeholder="شناسه جایگاه (مثال: B-12-03)..."
            value={binQuery}
            onChange={(e) => setBinQuery(e.target.value)}
            className="flex-1 text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-mono tracking-wider focus:outline-hidden"
            dir="ltr"
          />
          <button
            type="submit"
            className="px-3 py-2 bg-amber-500 hover:bg-amber-600 text-white font-bold text-xs rounded-xl shadow-xs"
          >
            جستجو
          </button>
        </form>

        {currentData ? (
          <div className="space-y-2.5 text-xs overflow-y-auto no-scrollbar flex-1">
            <div className="p-2.5 bg-slate-50 rounded-xl space-y-1 text-[11px] border border-slate-200">
              <div className="flex justify-between font-bold text-slate-800">
                <span>{currentData.hall}</span>
                <span className="text-blue-600">{currentData.capacity}</span>
              </div>
              <div className="flex justify-between text-slate-500 text-[10px]">
                <span>{currentData.aisle}</span>
                <span>قفسه: {currentData.shelf}</span>
              </div>
            </div>

            <div className="space-y-1.5">
              <h4 className="font-bold text-slate-800 text-[11px]">کالاهای موجود در این قفسه:</h4>
              {currentData.items.map((it: any, idx: number) => (
                <div key={idx} className="p-2 bg-white rounded-xl border border-slate-200 text-[11px] space-y-0.5">
                  <div className="flex justify-between font-bold text-slate-800">
                    <span>{it.title}</span>
                    <span className="text-emerald-700">{toPersianDigits(it.count)} عدد</span>
                  </div>
                  <span className="text-[10px] text-slate-400 font-mono block" dir="ltr">بارکد: {it.barcode}</span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="p-6 text-center text-slate-400 text-xs bg-slate-50 rounded-xl">
            جایگاهی با این شناسه یافت نشد یا در حال حاضر خالی است.
          </div>
        )}

        <button
          onClick={onClose}
          className="w-full py-2 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl"
        >
          بستن
        </button>
      </motion.div>
    </div>
  );
};
