import React, { useState } from 'react';
import { useStore } from '../context/StoreContext';
import { Store as StoreIcon, ChevronDown, CheckCircle2 } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';

export const StoreDropdown: React.FC = () => {
  const { currentStore, setCurrentStore, stores } = useStore();
  const [isOpen, setIsOpen] = useState(false);


  return (
    <div className="relative">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-1 px-2.5 py-1 rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-800 text-[11px] font-bold transition-colors border border-slate-200/60"
        title="تغییر ویترین / فروشگاه فعال"
      >
        <StoreIcon className="w-3.5 h-3.5 text-blue-600 shrink-0"  strokeWidth={1.5}/>
        <span className="truncate max-w-[110px]">{currentStore.name}</span>
        <ChevronDown className={`w-3 h-3 transition-transform shrink-0 ${isOpen ? 'rotate-180' : ''}`}  strokeWidth={1.5}/>
      </button>

      <AnimatePresence>
        {isOpen && (
          <motion.div
            initial={{ opacity: 0, y: 8, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 8, scale: 0.95 }}
            transition={{ duration: 0.15 }}
            className="absolute right-0 mt-2 w-72 bg-white rounded-2xl shadow-xl border border-slate-100 p-2 z-50 text-xs"
          >
            <div className="px-3 py-2 border-b border-slate-100 mb-1">
              <p className="font-bold text-slate-800">تغییر فروشگاه</p>
              <p className="text-[11px] text-slate-400 mt-0.5">
                انتخاب ویترین فعال
              </p>
            </div>
            {stores.map((store) => (
              <button
                key={store.id}
                onClick={() => {
                  setCurrentStore(store);
                  setIsOpen(false);
                }}
                className={`w-full text-right px-3 py-2.5 rounded-xl flex items-start justify-between transition-colors ${
                  currentStore.id === store.id ? 'bg-blue-50 text-blue-700' : 'hover:bg-slate-50 text-slate-700'
                }`}
              >

                {store.logo ? (
                  <img src={store.logo} alt="" className="w-8 h-8 rounded-lg object-cover shrink-0" />
                ) : null}
                <div>
                  <div className="font-bold flex items-center gap-1.5">
                    {store.name}
                    {store.ownerType === 'central' && (
                      <span className="text-[10px] bg-blue-100 text-blue-700 px-1.5 py-0.2 rounded font-normal">مرکزی</span>
                    )}
                  </div>
                  <p className="text-[11px] text-slate-400 mt-0.5 line-clamp-1">{store.tagline}</p>
                </div>
                {currentStore.id === store.id && (
                  <CheckCircle2 className="w-4 h-4 text-blue-600 shrink-0 mt-0.5"  strokeWidth={1.5}/>
                )}
              </button>
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
};
