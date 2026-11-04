import React from 'react';
import { Product } from '../types';
import { ProductCarousel } from './ProductCarousel';
import { ChevronLeft } from 'lucide-react';
import { useStore } from '../context/StoreContext';

interface ProductCarouselSectionProps {
  id?: string;
  title: string;
  categorySlug?: string;
  products: Product[];
  icon?: string;
}

export const ProductCarouselSection: React.FC<ProductCarouselSectionProps> = ({
  id,
  title,
  categorySlug,
  products,
}) => {
  const { openCatalogCategory } = useStore();

  if (!products || products.length === 0) return null;

  return (
    <section id={id} data-testid="product-carousel-banner" className="py-2.5 px-3 lg:px-6 lg:py-4 xl:px-10 2xl:px-12 bg-white border-b border-slate-100/80">
      {/* Snapp Pay Header */}
      <div className="flex items-center justify-between mb-3 px-1">
        <h2 className="text-[14px] lg:text-base font-bold text-slate-900 tracking-tight">
          {title}
        </h2>
        <button
          onClick={() => openCatalogCategory(categorySlug || 'all')}
          className="flex items-center gap-0.5 text-[12px] font-medium text-[#5d44f8] hover:opacity-85 transition-opacity"
        >
          <span>مشاهده همه</span>
          <ChevronLeft className="w-3.5 h-3.5"  strokeWidth={1.5}/>
        </button>
      </div>

      {/* Horizontal Carousel — live geometry + desktop arrows via ProductCarousel. */}
      <ProductCarousel products={products} />
    </section>
  );
};
