import React from 'react';
import { Check } from 'lucide-react';

interface SellerStep2Props {
  formData: {
    categories: string[];
  };
  setFormData: React.Dispatch<React.SetStateAction<any>>;
}

export const SellerStep2: React.FC<SellerStep2Props> = ({ formData, setFormData }) => {
  const toggleCategory = (catId: string) => {
    setFormData((prev: any) => ({
      ...prev,
      categories: prev.categories.includes(catId)
        ? prev.categories.filter((c: string) => c !== catId)
        : [...prev.categories, catId],
    }));
  };

  return (
    <div className="space-y-4">
      <div>
        <label className="block text-xs font-bold text-slate-700 mb-2">
          دسته‌بندی‌های فعال در فروشگاه شما:
        </label>
        <div className="grid grid-cols-2 gap-2 text-xs">
          {[
            { id: 'digital', title: 'کالای دیجیتال' },
            { id: 'fashion', title: 'مد و پوشاک' },
            { id: 'beauty', title: 'زیبایی و سلامت' },
            { id: 'gold', title: 'طلا و زیورآلات' },
            { id: 'appliances', title: 'لوازم خانگی' },
            { id: 'car', title: 'ابزار و خودرو' },
          ].map((cat) => {
            const active = formData.categories.includes(cat.id);
            return (
              <button
                type="button"
                key={cat.id}
                aria-pressed={active}
                onClick={() => toggleCategory(cat.id)}
                className={`flex min-h-11 items-center justify-between gap-2 rounded-xl border p-2.5 text-start transition-colors ${
                  active
                    ? 'border-[var(--shop-forest,#164638)] bg-[var(--shop-soft,#edf2e9)] font-bold text-[var(--shop-forest,#164638)]'
                    : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'
                }`}
              >
                <span>{cat.title}</span>
                {active && <Check className="w-3.5 h-3.5 shrink-0 text-[var(--shop-forest,#164638)]"  strokeWidth={1.5}/>}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};
