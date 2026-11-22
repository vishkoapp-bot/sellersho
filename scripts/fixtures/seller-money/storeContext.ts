import { createContext, useContext } from 'react';
import { formatPrice, toPersianDigits } from '../../../src/utils/formatters';

interface FixtureState {
  currentStore: { id?: string; slug: string; name: string };
  orders: unknown[];
}

export const FixtureStoreContext = createContext<FixtureState>({
  currentStore: { id: 'a', slug: 'a', name: 'Seller A' },
  orders: [],
});

export function useStore() {
  return {
    ...useContext(FixtureStoreContext),
    formatPrice,
    toPersianDigits,
    stores: [],
    payoutRequests: [],
    platformSettings: {},
  };
}
