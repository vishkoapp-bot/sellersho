import React from 'react';
import type { Product } from '../../../types';
import { ProductCard } from '../../ProductCard';

export const TemplateProductGrid: React.FC<{
  products: Product[];
  columnsClass: string;
}> = ({ products, columnsClass }) => {
  if (products.length === 0) {
    return <p className="text-xs text-slate-500 py-6 text-center">به‌زودی محصولات جدید می‌رسند</p>;
  }
  return (
    <div className={`grid gap-2 ${columnsClass}`}>
      {products.map((p) => (
        <ProductCard key={p.id} product={p} />
      ))}
    </div>
  );
};
