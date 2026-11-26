import React from 'react';
import { Product } from '../types';
import { ProductCarousel } from './ProductCarousel';
import { ArrowLeft } from 'lucide-react';
import { useStore } from '../context/StoreContext';

import { pathForView } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';

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
    <section id={id} data-testid="product-carousel-banner" className="central-product-section">
      <div className="central-section-title">
        <h2>{title}</h2>
        <a href={pathForView('catalog', { categorySlug })}
          onClick={event => followStoreLink(event, () => openCatalogCategory(categorySlug || 'all'))}>
          مشاهده همه <ArrowLeft size={17} aria-hidden="true" />
        </a>
      </div>
      <ProductCarousel products={products.slice(0, 16)} />
    </section>
  );
};
