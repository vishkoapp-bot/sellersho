import React, { useState } from 'react';
import { ArrowLeft, ImageOff } from 'lucide-react';
import type { Product } from '../../types';
import { cardPriceOf, cardPriceRangeOf } from '../../utils/cardPrice';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import './templates/fashion.css';

export const FashionProductCard: React.FC<{ product: Product; onOpen: (p: Product) => void }> = ({ product, onOpen }) => {
  const price = cardPriceOf(product);
  const priceRange = cardPriceRangeOf(product);
  const [previewRequested, setPreviewRequested] = useState(false);
  const [readyPreview, setReadyPreview] = useState('');
  const [failedSources, setFailedSources] = useState<string[]>([]);
  const availableImages = Array.from(new Set([product.image, ...(product.images || [])]))
    .filter((src): src is string => Boolean(src) && !failedSources.includes(src));
  const [primaryImage, secondImage] = availableImages;
  const secondaryReady = Boolean(previewRequested && secondImage && readyPreview === secondImage);
  const markFailed = (src: string) => setFailedSources(previous => previous.includes(src) ? previous : [...previous, src]);

  return <button type="button" onClick={() => onOpen(product)}
    onPointerEnter={event => { if (event.pointerType === 'mouse') setPreviewRequested(true); }}
    onFocus={() => setPreviewRequested(true)}
    className="fashion-product group" aria-label={`مشاهده ${product.title}`}>
    <div className="fashion-product-image" data-secondary-ready={secondaryReady}>
      {primaryImage ? <img src={primaryImage} alt={product.title} loading="lazy" decoding="async"
        onError={() => markFailed(primaryImage)} className={secondaryReady ? 'fashion-primary-image' : ''} />
        : <div className="fashion-no-image" role="img" aria-label="تصویر محصول موجود نیست"><ImageOff size={28} strokeWidth={1} /><span>تصویر محصول موجود نیست</span></div>}
      {previewRequested && secondImage && <img src={secondImage} alt="" loading="lazy" decoding="async"
        onLoad={() => setReadyPreview(secondImage)} onError={() => markFailed(secondImage)} className="fashion-secondary-image" />}
      {price.discountPercent > 0 && <span className="fashion-discount">٪{toPersianDigits(price.discountPercent)}</span>}
      <span className="fashion-product-quick">مشاهده محصول<ArrowLeft size={16} /></span>
    </div>
    <div className="fashion-product-info">
      {product.brand && <span className="fashion-product-brand">{product.brand}</span>}
      <h3>{product.title}</h3>
      <div className="fashion-price"><span>{formatPrice(price.price)} <small>تومان</small></span>
        {price.discountPercent > 0 && price.originalPrice > price.price && <del>{formatPrice(price.originalPrice)}</del>}
      </div>
      {priceRange.hasRange && (
        <span data-testid="price-range" className="fashion-price-range">از {formatPrice(priceRange.min)} تا {formatPrice(priceRange.max)} تومان</span>
      )}
      {(product.variants?.length || 0) > 1 && <span className="fashion-variants">{toPersianDigits(product.variants!.length)} تنوع</span>}
    </div>
  </button>;
};
