import React from 'react';
import { createRoot } from 'react-dom/client';
import { AuthProvider, useAuth } from '../../../src/context/AuthContext';
import { OrderProvider, useOrders } from '../../../src/context/OrderContext';
import { CartProvider } from '../../../src/context/CartContext';
import { ProductProvider } from '../../../src/context/ProductContext';
import { FinanceProvider } from '../../../src/context/FinanceContext';
import { WarehouseProvider } from '../../../src/context/WarehouseContext';
import { StoreProvider, useStore } from '../../../src/context/StoreContext';
import { ToastProvider } from '../../../src/context/ToastContext';
import { CustomerOrdersView } from '../../../src/components/views/CustomerOrdersView';
import { CustomerProfileView } from '../../../src/components/views/CustomerProfileView';
import { authApi } from '../../../src/services/authApi';
import { orderApi } from '../../../src/services/orderApi';
import { storeApi } from '../../../src/services/storeApi';
import { tokenStore } from '../../../src/services/apiClient';
import { stageAddressSelection, takeStagedAddressSelection } from '../../../src/services/addressApi';
import { wishlistApi } from '../../../src/services/wishlistApi';
import { mockProducts } from '../../../src/data/mockData';
import { paymentApi } from '../../../src/services/paymentApi';
import { SellerAuthGuard } from '../../../src/components/seller/SellerAuthGuard';

const h: any = {};
(window as any).orderHarness = h;
// 1,000 separate canary identities, repeated display names, ten stores.
// This is a browser isolation fixture; capacity/load gates run separately.
const identities = Array.from({ length: 1000 }, (_, i) => ({ id: `account-${i}`, mobile: `09${String(i).padStart(9, '0')}`, fullName: 'Same Name', role: 'customer', capabilities: ['customer'] }));
identities[700] = { ...identities[700], mobile: '09121112233', role: 'seller', capabilities: ['customer', 'seller'] };
const identity = () => identities[Number(tokenStore.getToken()?.replace('fixture-', '') || 0)];
const dto = (i: number) => ({ id: `order-${i}`, orderNumber: `CANARY-${i}`, customerFullName: i === 20 ? 'Different recipient' : 'Same Name', customerPhone: i === 20 ? '09999999999' : identities[i].mobile, customerAddress: `PRIVATE-ADDRESS-${i}`, customerCity: 'City', customerProvince: 'Province', customerPostalCode: `POST-${i}`, items: [], status: 'processing', storeId: `store-${i % 10}`, trackingCode: `TRACK-${i}`, payableAmount: 100, totalAmount: 100 });
authApi.getMe = async () => ({ ...identity(), userId: identity().id });
storeApi.getAllStores = async () => Array.from({ length: 10 }, (_, i) => ({ id: `store-${i}`, name: `Store ${i}`, slug: `store-${i}` }) as any);
storeApi.getOwnedStores = async () => [{ id: 'store-0', name: 'Store 0', slug: 'store-0' }] as any;
authApi.sendOTP = async () => ({ success: true, mobile: '09121112233', expiresIn: 120, message: 'Synthetic OTP' });
authApi.verifyOTP = async (mobile) => {
  if (mobile === '09121112233') {
    return new Promise(resolve => {
      h.finishOtp = () => resolve({ success: true, token: 'fixture-700', user: identities[700] });
    });
  }
  const index = Number(mobile.slice(2));
  return { success: true, token: `fixture-${index}`, user: identities[index] };
};
// Explicitly synthetic fallback for unrelated mounted providers. No real API.
const nativeFetch = window.fetch.bind(window);
window.fetch = async (input, init) => String(input).includes('/api/') ? new Response('{}', { headers: { 'Content-Type': 'application/json' } }) : nativeFetch(input, init);
const pending: { resolve: (value: any) => void; reject: (error: Error) => void; value: any; onSuccess?: () => void }[] = [];
const serverOrders = new Map<number, any[]>();
let defer = false;
let fail = false;
orderApi.getCustomerOrders = async (...args: any[]) => {
  if (args.length) throw new Error('Client ownership must not use phone/name selectors');
  const owner = identities.indexOf(identity());
  const value = serverOrders.get(owner) ?? [dto(owner)];
  if (defer) { defer = false; return new Promise((resolve, reject) => pending.push({ resolve, reject, value })); }
  if (fail) throw new Error('Synthetic B failure');
  return value as any;
};
orderApi.getMyReturns = async () => [];
orderApi.trackOrder = async () => {
  const value = dto(identities.indexOf(identity()));
  return new Promise((resolve, reject) => pending.push({ resolve, reject, value })) as any;
};
orderApi.createOrder = async () => {
  const owner = identities.indexOf(identity());
  const value = { ...dto(owner), id: `order-${owner}-created`, orderNumber: `CANARY-${owner}-CREATED` };
  return new Promise((resolve, reject) => pending.push({ resolve, reject, value, onSuccess: () => serverOrders.set(owner, [value, ...(serverOrders.get(owner) ?? [dto(owner)])]) })) as any;
};
orderApi.confirmDelivery = async () => {
  const owner = identities.indexOf(identity());
  return new Promise((resolve, reject) => pending.push({ resolve, reject, value: {}, onSuccess: () => serverOrders.set(owner, [{ ...dto(owner), status: 'delivered_confirmed' }]) })) as any;
};
orderApi.disputeDelivery = async () => {
  const owner = identities.indexOf(identity());
  return new Promise((resolve, reject) => pending.push({ resolve, reject, value: { disputeId: `PRIVATE-DISPUTE-${owner}` }, onSuccess: () => serverOrders.set(owner, [{ ...dto(owner), status: 'delivery_disputed' }]) })) as any;
};
h.defer = () => { defer = true; };
h.fail = () => { fail = true; };
h.pending = () => pending.length;
h.finish = (index: number, error = false) => {
  if (error) pending[index].reject(new Error('Synthetic obsolete failure'));
  else { pending[index].onSuccess?.(); pending[index].resolve(pending[index].value); }
};
const paymentPending: ((value: any) => void)[] = [];
h.paymentPending = () => paymentPending.length;
h.finishPayment = () => paymentPending[0]({ token: 'synthetic-payment' });
h.paymentCallbacks = 0;
paymentApi.getShaparakToken = async () => new Promise(resolve => paymentPending.push(resolve));
paymentApi.processShaparakCallback = async () => { h.paymentCallbacks++; return {} as any; };
h.count = identities.length;
let holdWishlist = false;
let failWishlist = false;
const wishlistPending: { resolve: (value: any) => void; value: any }[] = [];
h.added = [];
wishlistApi.add = async (productId, slug) => { h.added.push({ owner: identity().id, productId, slug }); return { success: true }; };
wishlistApi.getWishlist = async () => {
  const value = { wishlist: [`PRIVATE-WISH-${identities.indexOf(identity())}`] };
  if (holdWishlist) { holdWishlist = false; return new Promise(resolve => wishlistPending.push({ resolve, value })); }
  if (failWishlist) throw new Error('Synthetic private wishlist failure');
  return value;
};
h.holdWishlist = () => { holdWishlist = true; };
h.failWishlist = () => { failWishlist = true; };
h.wishlistPending = () => wishlistPending.length;
h.finishWishlist = () => wishlistPending[0].resolve(wishlistPending[0].value);
h.stage = () => stageAddressSelection({ addressId: 'PRIVATE-ADDRESS-ID-A' });
h.takeStage = takeStagedAddressSelection;
h.addCart = () => h.store.addToCart(mockProducts[0], 1);
function Probe() {
  const auth = useAuth(); const orders = useOrders(); const store = useStore();
  h.auth = auth; h.orders = orders; h.store = store;
  h.state = () => ({ user: auth.currentUser?.id, view: store.activeView, orders: orders.orders, current: orders.currentOrder, tracking: orders.trackingOrder, modal: orders.isTrackingModalOpen, address: store.directBuyAddress, creating: orders.isCreatingOrder, busy: orders.busyOrderId, wishlist: store.wishlistIds, proposals: store.proposedProducts, products: store.products.map(p => p.title), cart: store.cart.map(item => ({ id: item.product.id, quantity: item.quantity })) });
  if (new URL(location.href).searchParams.has('seller-guard')) return <SellerAuthGuard onBackToCustomerSite={() => {}}><div id="seller-panel-ready">Seller panel ready</div></SellerAuthGuard>;
  return <><pre id="state">{JSON.stringify(h.state())}</pre><CustomerOrdersView onBack={() => {}} /><CustomerProfileView onBack={() => {}} /></>;
}
createRoot(document.getElementById('root')!).render(<AuthProvider><ProductProvider><CartProvider><OrderProvider><FinanceProvider><WarehouseProvider><StoreProvider><ToastProvider><Probe /></ToastProvider></StoreProvider></WarehouseProvider></FinanceProvider></OrderProvider></CartProvider></ProductProvider></AuthProvider>);

