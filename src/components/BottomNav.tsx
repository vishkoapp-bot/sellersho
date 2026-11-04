import React, { useState } from 'react';
import { useStore } from '../context/StoreContext';
import { useAuth } from '../context/AuthContext';
import { AuthModal } from './auth/AuthModal';
import { Home, LayoutGrid, ShoppingBag, Package, User } from 'lucide-react';

export const BottomNav: React.FC = () => {
  const { 
    cartItemsCount, 
    setIsCartOpen, 
    activeView,
    setActiveView,
    toPersianDigits 
  } = useStore();
  const { isAuthenticated } = useAuth();
  const [isAuthModalOpen, setIsAuthModalOpen] = useState(false);

  // Enforce Chapter 5 Storefront Isolation:
  // Do not render Central Bottom Nav inside isolated seller storefronts, portal dashboards, or product detail view
  if (
    activeView === 'seller_storefront' ||
    activeView === 'seller_dashboard' ||
    activeView === 'supplier_dashboard' ||
    activeView === 'warehouse_panel'
  ) {
    return null;
  }

  const scrollToTop = () => {
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  return (
    <nav className="fixed bottom-0 inset-x-0 max-w-[504px] mx-auto z-40 bg-white border-t border-slate-200/80 h-16 px-1 shadow-[0_-2px_12px_rgba(0,0,0,0.06)] border-x border-slate-200/80 flex items-center justify-around select-none lg:hidden">
      {/* 1. Home */}
      <button
        onClick={() => {
          setActiveView('customer');
          scrollToTop();
        }}
        className={`flex flex-col items-center justify-center flex-1 py-1 active:scale-95 transition-all ${
          activeView === 'customer' ? 'text-[#007dfa]' : 'text-[#707583]'
        }`}
      >
        <Home className="w-5 h-5"  strokeWidth={1.5}/>
        <span className="text-[11px] font-medium mt-1">خانه</span>
      </button>

      {/* 2. Categories Tree */}
      <button
        onClick={() => setActiveView('categories_tree')}
        className={`flex flex-col items-center justify-center flex-1 py-1 active:scale-95 transition-all ${
          activeView === 'categories_tree' ? 'text-[#007dfa]' : 'text-[#707583] hover:text-[#007dfa]'
        }`}
      >
        <LayoutGrid className="w-5 h-5"  strokeWidth={1.5}/>
        <span className="text-[11px] font-normal mt-1">دسته‌ها</span>
      </button>

      {/* 3. Cart with Badge */}
      <button
        onClick={() => setIsCartOpen(true)}
        className="flex flex-col items-center justify-center flex-1 py-1 text-[#707583] hover:text-[#007dfa] relative active:scale-95 transition-all"
      >
        <div className="relative">
          <ShoppingBag className="w-5 h-5"  strokeWidth={1.5}/>
          {cartItemsCount > 0 && (
            <span className="absolute -top-1.5 -right-2.5 bg-[#ea004b] text-white font-bold text-[9px] w-4 h-4 rounded-full flex items-center justify-center ring-2 ring-white">
              {toPersianDigits(cartItemsCount)}
            </span>
          )}
        </div>
        <span className="text-[11px] font-normal mt-1">سبد خرید</span>
      </button>

      {/* 4. Orders */}
      <button
        onClick={() => setActiveView('customer_orders')}
        className={`flex flex-col items-center justify-center flex-1 py-1 active:scale-95 transition-all ${
          activeView === 'customer_orders' ? 'text-[#007dfa]' : 'text-[#707583] hover:text-[#007dfa]'
        }`}
      >
        <Package className="w-5 h-5"  strokeWidth={1.5}/>
        <span className="text-[11px] font-normal mt-1">سفارش‌ها</span>
      </button>

      {/* 5. Account / Profile */}
      <button
        onClick={() => {
          if (isAuthenticated) {
            setActiveView('customer_profile');
          } else {
            setIsAuthModalOpen(true);
          }
        }}
        className={`flex flex-col items-center justify-center flex-1 py-1 active:scale-95 transition-all ${
          activeView === 'customer_profile' ? 'text-[#007dfa]' : 'text-[#707583] hover:text-[#007dfa]'
        }`}
      >
        <User className="w-5 h-5"  strokeWidth={1.5}/>
        <span className="text-[11px] font-normal mt-1">{isAuthenticated ? 'حساب من' : 'ورود / حساب'}</span>
      </button>
      <AuthModal
        isOpen={isAuthModalOpen}
        onClose={() => setIsAuthModalOpen(false)}
        onSuccess={() => {
          setIsAuthModalOpen(false);
          setActiveView('customer_profile');
        }}
      />
    </nav>
  );
};


