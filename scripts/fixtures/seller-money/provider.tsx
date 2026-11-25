import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { FixtureStoreContext } from './storeContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { SellerAnalyticsTab } from '../../../src/components/seller/SellerAnalyticsTab';
import { SellerOverviewTab } from '../../../src/components/seller/SellerOverviewTab';
import { SellerWalletTab } from '../../../src/components/seller/SellerWalletTab';
import { storeApi } from '../../../src/services/storeApi';
import { isMockMode, tokenStore } from '../../../src/services/apiClient';

const params = new URLSearchParams(window.location.search);
const view = params.get('view');
const analyticsRequests: Array<{ storeId: string | null; signal?: AbortSignal | null; resolve: (response: Response) => void }> = [];
const reportRequests: Array<{ slug: string; signal?: AbortSignal; resolve: (report: { revenue: number; profit: number; orders: number; units: number }) => void }> = [];
const withdrawalRequests: Array<{ amount: number; shaba: string }> = [];
const availableAmount = Number(params.get('balance') ?? 52800);
const floor = params.get('floor');
let releaseBalance: (() => void) | undefined;
let releaseBreakdown: (() => void) | undefined;
let invalidEvents = 0;
document.addEventListener('invalid', () => { invalidEvents += 1; }, true);
storeApi.getSellerReport = (slug, _start, _end, options) => params.has('deferReport')
  ? new Promise(resolve => reportRequests.push({ slug, signal: options?.signal, resolve }))
  : Promise.resolve({ revenue: 1000, profit: 0, orders: 3, units: 4, pendingProfit: 0 });
storeApi.getOwnedBalance = async slug => {
  if (params.has('deferBalance')) {
    await new Promise<void>(resolve => { releaseBalance = resolve; });
  }
  if (params.has('balanceFailure')) throw new Error('Fixture balance failure');
  return { storeId: slug, availableAmount, bonusAvailable: 0, bonusLocked: 0, referralCommissionEarned: 0, unsettledAmount: 0 } as Awaited<ReturnType<typeof storeApi.getOwnedBalance>>;
};
storeApi.getWalletBreakdown = async slug => {
  if (params.has('deferBreakdown')) {
    await new Promise<void>(resolve => {
      releaseBreakdown = () => {
        releaseBreakdown = undefined;
        resolve();
      };
    });
  }
  if (params.has('breakdownFailure')) throw new Error('Fixture breakdown failure');
  return {
    storeId: slug, availableAmount, pendingProfit: 12000, releasedProfit: 63000, pendingEarlyGross: 0,
    settlementSettings: {
      earlyWithdrawalFeePct: 5,
      ...(floor === null ? {} : { earlyWithdrawalMinimumToman: Number(floor) }),
    },
  };
};
storeApi.getOwnedStores = async () => [];
storeApi.getStoreBySlug = async () => undefined;
storeApi.getStoreOrders = async () => [{ storeId: 'a', status: 'processing', dropshipperProfit: 900 }] as Awaited<ReturnType<typeof storeApi.getStoreOrders>>;
storeApi.getPayouts = async () => [];
storeApi.getEarlyWithdrawals = async () => [];

const nativeFetch = window.fetch.bind(window);
window.fetch = (input, options) => {
  const url = new URL(String(input), window.location.href);
  if (url.pathname === '/api/v1/stores/a/early-withdrawals' && options?.method === 'POST') {
    const request = JSON.parse(String(options.body));
    withdrawalRequests.push(request);
    const feeAmount = Math.trunc(request.amount * 0.05);
    return Promise.resolve(Response.json({
      id: 'fixture-withdrawal', storeId: 'a', grossAmount: request.amount,
      feeAmount, netAmount: request.amount - feeAmount, status: 'pending',
    }, { status: 201 }));
  }
  if (url.pathname !== '/api/v1/seller/analytics') return nativeFetch(input, options);
  return new Promise(resolve => analyticsRequests.push({ storeId: url.searchParams.get('storeId'), signal: options?.signal, resolve }));
};
tokenStore.setToken('fixture');

function Fixture() {
  const [currentStore, setStore] = useState({
    id: params.has('noStore') ? undefined : 'a', slug: 'a', name: 'Seller A',
  });
  const [orders, setOrders] = useState<unknown[]>(() => view === 'wallet' && isMockMode()
    ? [{ storeId: 'a', status: 'delivered', profitReleased: true, dropshipperProfit: availableAmount }]
    : []);
  (window as any).sellerMoney = {
    select: (id: string) => setStore({ id, slug: id, name: 'Seller ' + id }),
    refreshOrders: () => setOrders([{}]),
    balancePending: () => releaseBalance !== undefined,
    resolveBalance: () => releaseBalance?.(),
    breakdownPending: () => releaseBreakdown !== undefined,
    resolveBreakdown: () => releaseBreakdown?.(),
    withdrawalRequests: () => withdrawalRequests,
    invalidEvents: () => invalidEvents,
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
