import React, { createContext, useContext, useState } from 'react';
import { formatPrice, toPersianDigits } from '../../../src/utils/formatters';

const Context = createContext<any>(null);
export const fixtureProducts = [
  { id: 'p1', title: 'ژاکت بافت دکمه‌دار', category: 'women', image: '/templates/fashion/categories.webp', price: 2980000, originalPrice: 3200000, discountPercent: 7 },
  { id: 'p2', title: 'پالتو پشمی کمربنددار', category: 'women', image: '/templates/fashion/hero.webp', price: 6980000, originalPrice: 6980000, discountPercent: 0 },
  { id: 'p3', title: 'پلیور بافت مردانه', category: 'men', image: '/templates/fashion/knitwear.webp', price: 3280000, originalPrice: 3280000, discountPercent: 0 },
  { id: 'p4', title: 'کیف چرمی ساختاری', category: 'bags', image: '/templates/fashion/categories.webp', price: 5980000, originalPrice: 5980000, discountPercent: 0 },
].map(product => ({ ...product, images: [product.image], description: 'پوشاک پاییزی با کیفیت', brand: 'روژ', stock: 10 }));
export const fixtureCategories = [
  { slug: 'women', title: 'پوشاک زنانه' },
  { slug: 'men', title: 'پوشاک مردانه' },
  { slug: 'bags', title: 'کیف و اکسسوری' },
];
const store = { id: 'preview', slug: 'preview', name: 'روژ', templateId: 'tpl-fashion-1', footerText: 'فروشگاه آنلاین لباس و اکسسوری', ownerType: 'central' };
export const useStore = () => useContext(Context);
export const useStoreManager = () => useContext(Context);

export function PreviewProvider({ children }: { children: React.ReactNode }) {
  const [selectedCategory, setSelectedCategory] = useState('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [catalogBrandRequest, setBrand] = useState<any>(null);
  const [config, setConfig] = useState({});
  const [products, setProducts] = useState(fixtureProducts);
  const action = (name: string) => (...args: unknown[]) => { (window as any).lastAction = { name, args }; };
  const context = {
    selectedCategory, setSelectedCategory, searchQuery, setSearchQuery, catalogBrandRequest,
    clearCatalogBrand: () => setBrand(null), openCatalogBrand: (brand: string) => setBrand({ brand }),
    currentStore: { ...store, templateConfig: config }, products, categories: fixtureCategories,
    cartItemsCount: 2, customerUser: {}, activeView: 'seller_storefront', formatPrice, toPersianDigits,
    openProductDetail: action('product'), addToCart: action('cart-add'), setIsCartOpen: action('cart'), setActiveView: action('navigate'), openStaticPage: action('page'),
  };
  (window as any).preview = { setConfig, setProducts, setSearchQuery, setSelectedCategory, setBrand, fixtureProducts };
  return <Context.Provider value={context}>{children}</Context.Provider>;
}
