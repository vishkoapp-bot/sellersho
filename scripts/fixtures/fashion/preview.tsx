import React from 'react';
import { createRoot } from 'react-dom/client';
import '../../../src/index.css';
import { ProductDetailRelated } from '../../../src/components/views/product/ProductDetailRelated';
import { ToastProvider } from '../../../src/context/ToastContext';
import { PreviewProvider, useStore } from './context';
import { FashionTemplate } from '../../../src/components/storefront/templates/FashionTemplate';
import { SellerStorefrontHeader } from '../../../src/components/storefront/SellerStorefrontHeader';
import { SellerStorefrontFooter, StorefrontBottomNav } from '../../../src/components/storefront/SellerStorefrontFooter';

function Page() {
  const state = useStore();
  const params = new URLSearchParams(window.location.search);
  if (params.get('surface') === 'related') return <ProductDetailRelated products={state.products} fashion={params.get('theme') !== 'central'} onOpen={state.openProductDetail} />;
  return <>
    <SellerStorefrontHeader variant="fashion" onOpenSearch={() => { (window as any).lastAction = { name: 'search' }; }} />
    <FashionTemplate store={state.currentStore} config={state.currentStore.templateConfig} products={state.products} categories={state.categories} accentColor="#793f3c" />
    <SellerStorefrontFooter variant="fashion" />
    <StorefrontBottomNav />
  </>;
}
createRoot(document.getElementById('root')!).render(<ToastProvider><PreviewProvider><Page /></PreviewProvider></ToastProvider>);
