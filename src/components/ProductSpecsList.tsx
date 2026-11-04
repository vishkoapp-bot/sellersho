import React from 'react';

interface ProductSpecsListProps {
  description: string;
  specs?: { [key: string]: string };
}

export const ProductSpecsList: React.FC<ProductSpecsListProps> = ({ description, specs }) => {
  return (
    <div className="border-t border-slate-100 pt-4 space-y-3">
      <h4 className="text-xs font-bold text-slate-800">توضیحات و بررسی کالا:</h4>
      <p className="text-xs md:text-sm text-slate-600 leading-relaxed">{description}</p>

      {specs && (
        <div className="pt-2">
          <h4 className="text-xs font-bold text-slate-800 mb-2">مشخصات فنی:</h4>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
            {Object.entries(specs).map(([key, value]) => (
              <div key={key} className="flex justify-between p-2 rounded-xl bg-slate-50 border border-slate-100">
                <span className="text-slate-400">{key}:</span>
                <span className="font-bold text-slate-700">{value}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
