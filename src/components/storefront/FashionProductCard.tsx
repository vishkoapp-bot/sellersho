import React, { useState } from 'react';
import { ArrowLeft, ImageOff } from 'lucide-react';
import type { Product } from '../../types';
import { cardPriceOf, cardPriceRangeOf } from '../../utils/cardPrice';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import { buildProductUrl } from '../../utils/routing';
import { followStoreLink } from '../../utils/followStoreLink';
import { activeStoreSlugOf, useStore } from '../../context/StoreContext';
import { activeProductVariants, resolvePurchase } from '../../utils/productPurchase';
import './templates/fashion.css';

export const FashionProductCard: React.FC<{ product: Product; onOpen: (p: Product) => void }> = ({ product, onOpen }) => {
  const price = cardPriceOf(product);
  const priceRange = cardPriceRangeOf(product);
  const { currentStore, routeStoreSlug } = useStore();
  const availableVariants = activeProductVariants(product);
  const available = product.variants?.length
    ? availableVariants.some(variant => !!resolvePurchase(product, variant.id))
    : !!resolvePurchase(product);
  const [previewRequested, setPreviewRequested] = useState(false);
  const [readyPreview, setReadyPreview] = useState('');
  const [failedSources, setFailedSources] = useState<string[]>([]);
  const availableImages = Array.from(new Set([product.image, ...(product.images || [])]))
    .filter((src): src is string => Boolean(src) && !failedSources.includes(src));
  const [primaryImage, secondImage] = availableImages;
  const secondaryReady = Boolean(previewRequested && secondImage && readyPreview === secondImage);
  const markFailed = (src: string) => setFailedSources(previous => previous.includes(src) ? previous : [...previous, src]);

  return <a href={buildProductUrl(product, activeStoreSlugOf({ currentStore, routeStoreSlug }) ?? undefined)}
    onClick={event => followStoreLink(event, () => onOpen(product))}
    onPointerEnter={event => { if (event.pointerType === 'mouse') setPreviewRequested(true); }}
    onFocus={() => setPreviewRequested(true)}
    className="fashion-product group" aria-label={`مشاهده ${product.title}`}>
    <div className="fashion-product-image" data-secondary-ready={secondaryReady}>
      {primaryImage ? <img src={primaryImage} alt="" width={480} height={600} loading="lazy" decoding="async"
        onError={() => markFailed(primaryImage)} className={secondaryReady ? 'fashion-primary-image' : ''} />
        : <div className="fashion-no-image" role="img" aria-label="تصویر محصول موجود نیست"><ImageOff size={28} strokeWidth={1} /><span>تصویر محصول موجود نیست</span></div>}
      {previewRequested && secondImage && <img src={secondImage} alt="" width={480} height={600} loading="lazy" decoding="async"
        onLoad={() => setReadyPreview(secondImage)} onError={() => markFailed(secondImage)} className="fashion-secondary-image" />}
      {!available ? <span className="fashion-discount fashion-stock-badge">ناموجود</span>
        : price.discountPercent > 0 && <span className="fashion-discount">{toPersianDigits(price.discountPercent)}٪ تخفیف</span>}
    </div>
    <div className="fashion-product-info">
      {product.brand && <span className="fashion-product-brand">{product.brand}</span>}
      <div className="fashion-product-title"><h3>{product.title}</h3><ArrowLeft size={15} aria-hidden="true" /></div>
      <div className="fashion-price"><span>{formatPrice(price.price)} <small>تومان</small></span>
        {price.discountPercent > 0 && price.originalPrice > price.price && <del>{formatPrice(price.originalPrice)}</del>}
      </div>
      {priceRange.hasRange && (
        <span data-testid="price-range" className="fashion-price-range">از {formatPrice(priceRange.min)} تا {formatPrice(priceRange.max)} تومان</span>
      )}
      {availableVariants.length > 1 && <span className="fashion-variants">{toPersianDigits(availableVariants.length)} تنوع</span>}
    </div>
  </a>;
};
