import React from 'react';
import { ArrowLeft, ShoppingBag } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { pathForView } from '../utils/routing';
import { FallbackImage } from './common/FallbackImage';
import { followStoreLink } from '../utils/followStoreLink';

export const CategoriesBar: React.FC = () => {
  const { openCatalogCategory, setActiveView, categories, catalogLoading } = useStore();
  const roots = categories.filter(category => !category.parentId);

  return (
    <section id="category" data-testid="category" className="central-categories" aria-labelledby="central-categories-title">
      <div className="central-section-title">
        <h2 id="central-categories-title">دسته‌بندی کالاها</h2>
        <a href={pathForView('categories_tree')} onClick={event => followStoreLink(event, () => setActiveView('categories_tree'))}>
          همه دسته‌ها <ArrowLeft size={17} aria-hidden="true" />
        </a>
      </div>
      <div className="central-category-rail" aria-busy={catalogLoading && !roots.length}>
        {catalogLoading && !roots.length ? Array.from({ length: 6 }, (_, index) => (
          <div className="central-category central-category--loading motion-safe:animate-pulse" key={index} aria-hidden="true" />
        )) : roots.map(category => (
          <a className="central-category" key={category.id} data-testid="category-item"
            href={pathForView('catalog', { categorySlug: category.slug })}
            onClick={event => followStoreLink(event, () => openCatalogCategory(category.slug))}>
            <span className="central-category-image">
              <FallbackImage srcs={[category.image]} alt="" loading="lazy" width={56} height={56}
                fallback={<ShoppingBag size={24} aria-hidden="true" />} />
            </span>
            <span>{category.title}</span>
          </a>
        ))}
      </div>
    </section>
  );
};
