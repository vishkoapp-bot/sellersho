import React from 'react';
import { Sparkles } from 'lucide-react';
import type { Product } from '../../../types';
import { toPersianDigits } from '../../../utils/formatters';
import { ProductCard } from '../../ProductCard';
import { FashionProductCard } from '../../storefront/FashionProductCard';

interface Props {
  products: Product[];
  fashion: boolean;
  central?: boolean;
  onOpen: (product: Product) => void;
}

export const ProductDetailRelated: React.FC<Props> = ({ products, fashion, central = false, onOpen }) => {
  if (products.length === 0) return null;

  if (fashion) {
    const columns = products.length === 1
      ? 'grid-cols-1 max-w-sm mx-auto'
      : products.length === 2
        ? 'grid-cols-2 max-w-2xl mx-auto'
        : products.length === 3
          ? 'grid-cols-2 md:grid-cols-3'
          : 'grid-cols-2 md:grid-cols-3 lg:grid-cols-4';
    return (
      <div className="px-4 pb-6 lg:px-0 lg:pb-0 lg:col-span-2">
        <section data-testid="product-related" data-theme="fashion" aria-label="کالاهای مرتبط و مشابه"
          className="fashion-storefront border-t border-[#dedbd5] pt-6 space-y-6 lg:pt-8 lg:space-y-8">
          <div className="fashion-catalogue-title">
            <h2 className="text-xl lg:text-2xl font-bold leading-relaxed">کالاهای مرتبط و مشابه</h2>
            <span className="fashion-result-count">{toPersianDigits(products.length)} کالا</span>
          </div>
          <div className={`grid gap-x-3 gap-y-8 lg:gap-x-7 ${columns}`}>
            {products.map(product => <FashionProductCard key={product.id} product={product} onOpen={onOpen} />)}
          </div>
        </section>
      </div>
    );
  }

  if (central) {
    return (
      <section data-testid="product-related" className="central-pdp-related" aria-label="کالاهای مشابه">
        <h2 className="text-xl font-extrabold">کالاهای مشابه</h2>
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4 md:gap-4">
          {products.map(product => <ProductCard key={product.id} product={product} />)}
        </div>
      </section>
    );
  }

  return (
    <div className="px-3.5 -mt-3.5 pb-3.5 lg:px-0 lg:pb-0 lg:mt-0 lg:col-span-2">
      <section data-testid="product-related" data-theme="default" className="pt-3 border-t border-slate-100 space-y-2">
        <h3 className="text-base lg:text-lg xl:text-xl font-bold text-slate-800 flex items-center gap-1">
          <Sparkles className="w-3.5 h-3.5 text-brand-deep" strokeWidth={1.5} />
          <span>کالاهای مرتبط و مشابه</span>
        </h3>
        <div className="grid grid-cols-2 gap-2 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
          {products.map(product => <ProductCard key={product.id} product={product} />)}
        </div>
      </section>
    </div>
  );
};
