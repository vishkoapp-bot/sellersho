import React, { useState } from 'react';
import { Package } from 'lucide-react';

interface ProductGalleryProps {
  images?: string[];
  mainImage: string;
  title: string;
}

export const ProductGallery: React.FC<ProductGalleryProps> = ({ images, mainImage, title }) => {
  const [activeIdx, setActiveIdx] = useState(0);
  const validImages = (images || []).filter((image) => typeof image === 'string' && image.trim().length > 0);
  const current = validImages[activeIdx] || (typeof mainImage === 'string' && mainImage.trim() ? mainImage : null);

  return (
    <div className="space-y-2">
      <div className="h-52 w-full rounded-2xl overflow-hidden bg-slate-50 border border-slate-100 flex items-center justify-center p-3">
        {current ? (
          <img src={current} alt={title} className="max-h-full max-w-full object-contain" />
        ) : (
          <Package aria-label="تصویر کالا موجود نیست" className="w-12 h-12 text-slate-300"  strokeWidth={1.5}/>
        )}
      </div>
      {validImages.length > 1 && (
        <div className="flex gap-2 justify-center">
          {validImages.map((img, i) => (
            <button
              key={i}
              onClick={() => setActiveIdx(i)}
              className={`w-12 h-12 rounded-xl overflow-hidden border-2 transition-all p-1 bg-white ${
                activeIdx === i ? 'border-blue-600 scale-105' : 'border-slate-200 opacity-70'
              }`}
            >
              <img src={img} alt="" className="w-full h-full object-contain" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

