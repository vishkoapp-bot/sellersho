import React from 'react';
import { Heart, Home, LayoutGrid, ShoppingBag, User } from 'lucide-react';
import { useStore } from '../context/StoreContext';
import { pathForView } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';

export const BottomNav: React.FC = () => {
  const { cartItemsCount, setIsCartOpen, activeView, setActiveView, toPersianDigits } = useStore();
  const items = [
    { view: 'customer', label: 'خانه', Icon: Home },
    { view: 'categories_tree', label: 'دسته‌ها', Icon: LayoutGrid },
    { view: 'customer_wishlist', label: 'علاقه‌مندی‌ها', Icon: Heart },
    { view: 'customer_profile', label: 'حساب من', Icon: User },
  ] as const;

  const link = ({ view, label, Icon }: typeof items[number]) => (
    <a key={view} href={pathForView(view)} aria-current={activeView === view ? 'page' : undefined}
      onClick={event => followStoreLink(event, () => {
        setActiveView(view);
        if (view === 'customer') window.scrollTo({ top: 0 });
      })}>
      <Icon size={21} aria-hidden="true" /><span>{label}</span>
    </a>
  );

  return (
    <nav className="central-bottom-nav" aria-label="منوی پایین فروشگاه">
      {items.slice(0, 2).map(link)}
      <button type="button" onClick={() => setIsCartOpen(true)}
        aria-label={cartItemsCount ? `سبد خرید، ${toPersianDigits(cartItemsCount)} کالا` : 'سبد خرید'}>
        <span className="relative"><ShoppingBag size={21} aria-hidden="true" />
          {cartItemsCount > 0 && <span className="central-count">{toPersianDigits(cartItemsCount)}</span>}
        </span>
        <span>سبد خرید</span>
      </button>
      {items.slice(2).map(link)}
    </nav>
  );
};
