import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { FixtureStoreContext } from './storeContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { SellerAnalyticsTab } from '../../../src/components/seller/SellerAnalyticsTab';
import { SellerOverviewTab } from '../../../src/components/seller/SellerOverviewTab';
import { SellerWalletTab } from '../../../src/components/seller/SellerWalletTab';
import { storeApi } from '../../../src/services/storeApi';
import { tokenStore } from '../../../src/services/apiClient';

const params = new URLSearchParams(window.location.search);
const view = params.get('view');
const analyticsRequests: Array<{ storeId: string | null; signal?: AbortSignal | null; resolve: (response: Response) => void }> = [];
const reportRequests: Array<{ slug: string; signal?: AbortSignal; resolve: (report: { revenue: number; profit: number; orders: number; units: number }) => void }> = [];
storeApi.getSellerReport = (slug, _start, _end, options) => params.has('deferReport')
  ? new Promise(resolve => reportRequests.push({ slug, signal: options?.signal, resolve }))
  : Promise.resolve({ revenue: 1000, profit: 0, orders: 3, units: 4, pendingProfit: 0 });
storeApi.getOwnedBalance = async () => {
  if (params.has('balanceFailure')) throw new Error('Fixture balance failure');
  return { availableAmount: 52800, bonusAvailable: 0, bonusLocked: 0, referralCommissionEarned: 0, unsettledAmount: 0 } as Awaited<ReturnType<typeof storeApi.getOwnedBalance>>;
};
storeApi.getWalletBreakdown = async () => {
  if (params.has('breakdownFailure')) throw new Error('Fixture breakdown failure');
  return { pendingProfit: 12000, releasedProfit: 63000, pendingEarlyGross: 0 } as Awaited<ReturnType<typeof storeApi.getWalletBreakdown>>;
};
storeApi.getOwnedStores = async () => [];
storeApi.getStoreBySlug = async () => undefined;
storeApi.getStoreOrders = async () => [{ storeId: 'a', status: 'processing', dropshipperProfit: 900 }] as Awaited<ReturnType<typeof storeApi.getStoreOrders>>;
storeApi.getPayouts = async () => [];
storeApi.getEarlyWithdrawals = async () => [];

const nativeFetch = window.fetch.bind(window);
window.fetch = (input, options) => {
  const url = new URL(String(input), window.location.href);
  if (url.pathname !== '/api/v1/seller/analytics') return nativeFetch(input, options);
  return new Promise(resolve => analyticsRequests.push({ storeId: url.searchParams.get('storeId'), signal: options?.signal, resolve }));
};
tokenStore.setToken('fixture');

function Fixture() {
  const [currentStore, setStore] = useState({
    id: params.has('noStore') ? undefined : 'a', slug: 'a', name: 'Seller A',
  });
  const [orders, setOrders] = useState<unknown[]>([]);
  (window as any).sellerMoney = {
    select: (id: string) => setStore({ id, slug: id, name: 'Seller ' + id }),
    refreshOrders: () => setOrders([{}]),
    analytics: () => analyticsRequests.map(request => ({ storeId: request.storeId, aborted: !!request.signal?.aborted })),
    report: () => reportRequests.map(request => ({ slug: request.slug, aborted: !!request.signal?.aborted })),
    resolveReport: (index: number, revenue: number) => reportRequests[index].resolve({ revenue, profit: 0, orders: 3, units: 4 }),
    resolveAnalytics: (index: number, totalRevenue: number) => analyticsRequests[index].resolve(new Response(JSON.stringify({
      totalRevenue, orderCount: 3, avgOrderValue: 0, sellerProfit: 20, views: 10, conversionRate: 0,
      statusBreakdown: [{ status: 'delivered', count: 3 }],
      periodComparison: { current: { revenue: 0, orders: 0, avgOrderValue: 0, profit: 0 }, previous: { revenue: 0, orders: 0, avgOrderValue: 0, profit: 0 } },
    }), { headers: { 'Content-Type': 'application/json' } })),
  };

  return (
    <FixtureStoreContext.Provider value={{ currentStore, orders }}>
      <ToastProvider>
        {view === 'analytics' ? <SellerAnalyticsTab /> : view === 'wallet' ? <SellerWalletTab /> : (
          <SellerOverviewTab onNavigateTab={() => {}} onOpenStorefront={() => {}} />
        )}
      </ToastProvider>
    </FixtureStoreContext.Provider>
  );
}

createRoot(document.getElementById('root')!).render(<Fixture />);
