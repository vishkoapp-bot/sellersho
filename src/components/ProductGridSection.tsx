import React from 'react';
import { ArrowLeft, PackageOpen } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { pathForView } from '../utils/routing';
import { ProductCard } from './ProductCard';
import { HelpExplainer } from './common/HelpExplainer';
import { followStoreLink } from '../utils/followStoreLink';

export const ProductGridSection: React.FC = () => {
  const { products, catalogLoading, catalogError, fetchCatalogProducts, openCatalogCategory } = useStore();
  const preview = products.slice(0, 8);
  const catalogLink = (event: React.MouseEvent<HTMLAnchorElement>) => followStoreLink(event, () => openCatalogCategory('all'));

  return (
    <section id="products-section" className="central-product-section" aria-labelledby="central-products-title">
      <div className="central-section-title">
        <h2 id="central-products-title">منتخب کالاها <HelpExplainer text="کالاها پیش از ارسال کنترل کیفیت می‌شوند؛ ارسال به سراسر کشور" /></h2>
        <a href={pathForView('catalog')} onClick={catalogLink}>همه کالاها <ArrowLeft size={17} aria-hidden="true" /></a>
      </div>
      {catalogLoading && !preview.length ? (
        <div className="central-product-grid" role="status" aria-label="در حال بارگذاری کالاها">
          {Array.from({ length: 4 }, (_, index) => <div key={index} aria-hidden="true" className="central-product-skeleton motion-safe:animate-pulse" />)}
        </div>
      ) : preview.length ? (
        <div className="central-product-grid">
          {preview.map(product => <ProductCard key={product.id} product={product} />)}
        </div>
      ) : (
        <div className="central-empty">
          <PackageOpen size={28} aria-hidden="true" />
          <p role={catalogError ? 'alert' : 'status'}>{catalogError ? 'کالاها بارگذاری نشدند.' : 'در حال حاضر کالایی برای نمایش نیست.'}</p>
          {catalogError && <button type="button" className="central-button central-button--outline" onClick={() => { void fetchCatalogProducts(); }}>تلاش مجدد</button>}
        </div>
      )}
    </section>
  );
};
