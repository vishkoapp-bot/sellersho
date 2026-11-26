import React from 'react';
import { createRoot } from 'react-dom/client';
import { AuthProvider, useAuth } from '../../../src/context/AuthContext';
import { authApi } from '../../../src/services/authApi';
import { tokenStore, apiRequest } from '../../../src/services/apiClient';
import { storeApi } from '../../../src/services/storeApi';

const harness: any = {};
(window as any).authHarness = harness;
let claims: any;
let deferCount = new URL(location.href).searchParams.has('deferRestore') ? 1 : 0;
const pending: { resolve: (value: any) => void; reject: (error: Error) => void; snapshot: any }[] = [];
let holdLogin = false;
const logins: { resolve: (value: any) => void; value: any }[] = [];
harness.deferLogin = () => { holdLogin = true; };
harness.pendingLogin = () => logins.length;
harness.finishLogin = (index: number) => logins[index].resolve(logins[index].value);
authApi.verifyOTP = async (identity) => {
  const [role, capability = role] = identity.split(':');
  claims = { userId: identity, mobile: 'test-' + identity, fullName: 'Same Name', role, capabilities: [capability] };
  const value = { success: true, token: 'fixture-' + identity, user: { ...claims, id: identity } };
  if (holdLogin) { holdLogin = false; return new Promise(resolve => logins.push({ resolve, value })); }
  return value;
};
authApi.getMe = async () => {
  if (new URL(location.href).searchParams.has('failRestore')) {
    return apiRequest('/test-auth?unauthorized', { auth: true });
  }
  const identity = tokenStore.getToken()?.replace('fixture-', '') || 'customer';
  const [role, capability = role] = identity.split(':');
  const snapshot = claims?.userId === identity ? claims : { userId: identity, mobile: 'test-' + identity, fullName: 'Same Name', role, capabilities: [capability] };
  if (deferCount > 0) {
    deferCount--;
    return new Promise((resolve, reject) => pending.push({ resolve, reject, snapshot }));
  }
  return snapshot;
};
harness.deferMe = () => { deferCount++; };
harness.pendingMe = () => pending.length;
harness.finishMe = (index: number, failure = false, patch = {}) => {
  const request = pending[index];
  if (failure) request.reject(new Error('Synthetic obsolete auth failure'));
  else request.resolve({ ...request.snapshot, ...patch });
};
let holdProfiles = false;
const profiles: { resolve: (value: any) => void; reject: (error: Error) => void }[] = [];
harness.deferProfiles = () => { holdProfiles = true; };
harness.pendingProfiles = () => profiles.length;
harness.finishProfiles = (failure = false) => {
  holdProfiles = false;
  for (const request of profiles) {
    if (failure) request.reject(new Error('Synthetic obsolete profile failure'));
    else request.resolve({ id: 'obsolete-supplier', company_name: 'Private A' });
  }
};
authApi.getSupplierProfile = async () => holdProfiles
  ? new Promise((resolve, reject) => profiles.push({ resolve, reject }))
  : ({ id: 'supplier-fixture', company_name: 'Fixture' } as any);
storeApi.getOwnedStores = async () => [{ id: 'store-fixture' } as any];
function Probe() {
  const auth = useAuth();
  harness.auth = auth;
  harness.state = () => ({ user: auth.currentUser, token: auth.authToken, credential: tokenStore.getToken(), admin: auth.adminSession, seller: auth.sellerSession, supplier: auth.supplierSession, warehouse: auth.warehouseSession, customer: auth.customerUser, ready: auth.isSessionReady, error: auth.panelSessionErrors.supplier_dashboard ?? auth.panelSessionErrors.seller_dashboard ?? null });
  return <div data-authenticated={auth.isAuthenticated}>{auth.currentUser?.id || 'guest'}</div>;
}
const root = createRoot(document.getElementById('root')!);
root.render(<AuthProvider><Probe /></AuthProvider>);
harness.unmount = () => root.unmount();
harness.privilegedRequest = (path = '/test-auth') => apiRequest(path, { auth: true });
