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
                onClick={() => toggleCategory(cat.id)}
                className={`p-2.5 rounded-xl border flex items-center justify-between transition-colors ${
                  active
                    ? 'bg-blue-50 border-blue-500 text-blue-700 font-bold'
                    : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-50'
                }`}
              >
                <span>{cat.title}</span>
                {active && <Check className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
};
