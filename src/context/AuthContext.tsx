import React, { createContext, useContext, useState, useEffect, useRef, useCallback, ReactNode } from 'react';
import { 
  SupplierSession, 
  WarehouseSession 
} from '../types';
import { 
  AdminSession, 
  AdminRole,
  SellerSession 
} from './storeContextTypes';
import { authApi } from '../services/authApi';
import { tokenStore, isMockMode } from '../services/apiClient';
import { storeApi } from '../services/storeApi';

export type AuthenticatedPanel =
  | 'customer'
  | 'seller_dashboard'
  | 'supplier_dashboard'
  | 'warehouse_panel'
  | 'admin_panel';
type ProfilePanel = 'seller_dashboard' | 'supplier_dashboard';

export const panelForRole = (role: string): AuthenticatedPanel => {
  switch (role) {
    case 'seller':
      return 'seller_dashboard';
    case 'supplier':
      return 'supplier_dashboard';
    case 'warehouse_staff':
      return 'warehouse_panel';
    case 'logistics_specialist':
      return 'warehouse_panel';
    case 'admin':
    case 'super_admin':
    case 'finance':
    case 'finance_specialist':
    case 'support_specialist':
      return 'admin_panel';
    default:
      return 'customer';
  }
};

export const panelsForCapabilities = (capabilities: readonly string[] = []): AuthenticatedPanel[] => {
  const panels: AuthenticatedPanel[] = [];
  if (capabilities.includes('customer')) panels.push('customer');
  if (capabilities.includes('seller')) panels.push('seller_dashboard');
  if (capabilities.includes('supplier')) panels.push('supplier_dashboard');
  if (warehouseRoleFromCapabilities(capabilities)) panels.push('warehouse_panel');
  if (adminRolesForCapabilities(capabilities).length > 0) panels.push('admin_panel');
  return panels;
};

export function defaultPanelForUser(user: Pick<AuthUser, 'role' | 'capabilities'>): AuthenticatedPanel {
  const available = panelsForCapabilities(user.capabilities);
  const preferred = panelForRole(user.role);
  return available.includes(preferred) ? preferred : available[0] ?? 'customer';
}

export function adminRolesForCapabilities(capabilities: readonly string[] = []): AdminRole[] {
  const roles: AdminRole[] = [];
  if (capabilities.includes('super_admin')) roles.push('super_admin');
  if (capabilities.includes('admin')) roles.push('admin');
  if (capabilities.includes('finance') || capabilities.includes('finance_specialist')) roles.push('finance_specialist');
  if (capabilities.includes('support_specialist')) roles.push('support_specialist');
  if (roles.length > 0 && capabilities.includes('logistics_specialist')) roles.push('logistics_specialist');
  return roles;
}

export const adminRoleLabels: Record<AdminRole, string> = {
  admin: 'مدیر پلتفرم',
  super_admin: 'مدیر ارشد پلتفرم',
  finance_specialist: 'کارشناس مالی',
  support_specialist: 'کارشناس پشتیبانی',
  logistics_specialist: 'کارشناس لجستیک',
};

export function warehouseRoleFromCapabilities(capabilities: readonly string[] = []) {
  if (capabilities.includes('warehouse_staff')) return 'warehouse_staff';
  if (capabilities.includes('logistics_specialist')) return 'logistics_specialist';
  return undefined;
}

export interface AuthUser {
  id: string;
  mobile: string;
  fullName: string;
  role: 'customer' | 'seller' | 'supplier' | 'warehouse_staff' | 'admin' | 'super_admin' | 'finance' | string;
  username?: string;
  storeId?: string;
  capabilities?: string[];
}

export interface CustomerUser {
  fullName: string;
  mobile: string;
  nationalCode: string;
  isLoggedIn: boolean;
}

export interface AuthenticationResponse {
  token: string;
  user: { id: string; mobile: string; fullName: string; role: string; capabilities?: string[] };
}

export class AuthRequestSupersededError extends Error {
  constructor() { super('Session changed during authentication'); this.name = 'AuthRequestSupersededError'; }
}

export interface AuthContextType {
  authenticate: <T extends AuthenticationResponse>(request: () => Promise<T>) => Promise<T & { isCurrent: () => boolean }>;
  currentUser: AuthUser | null;
  authToken: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  isSessionReady: boolean;
  panelSessionErrors: Partial<Record<ProfilePanel, string>>;
  retryPanelSession: () => void;
  customerUser: CustomerUser;
  setCustomerUser: React.Dispatch<React.SetStateAction<CustomerUser>>;
  logoutCustomer: () => void;
  adminSession: AdminSession | null;
  setAdminSession: React.Dispatch<React.SetStateAction<AdminSession | null>>;
  logoutAdmin: () => void;
  sellerSession: SellerSession | null;
  setSellerSession: React.Dispatch<React.SetStateAction<SellerSession | null>>;
  logoutSeller: () => void;
  supplierSession: SupplierSession | null;
  setSupplierSession: React.Dispatch<React.SetStateAction<SupplierSession | null>>;
  logoutSupplier: () => void;
  warehouseSession: WarehouseSession | null;
  setWarehouseSession: React.Dispatch<React.SetStateAction<WarehouseSession | null>>;
  logoutWarehouse: () => void;
  loginWithOTP: (mobile: string, otp: string, storeSlug?: string) => Promise<AuthUser>;
  logoutUser: () => void;
  registerUser: (userData: { mobile: string; fullName: string; role?: string }) => Promise<void>;
  updateProfileName: (fullName: string) => Promise<void>;
  refreshToken: () => Promise<void>;
  panelForRole: (role: string) => AuthenticatedPanel;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const authGeneration = useRef(0);
  const profileSequence = useRef(0);
  const mounted = useRef(true);
  const ownsRequest = (generation: number, credential: string | null, sequence?: number) =>
    mounted.current && generation === authGeneration.current && credential === tokenStore.getToken() &&
    (sequence === undefined || sequence === profileSequence.current);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; authGeneration.current += 1; };
  }, []);
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(() => {
    if (typeof window !== 'undefined' && tokenStore.getToken()) {
      const saved = localStorage.getItem('drup_current_user');
      if (saved) {
        try { return JSON.parse(saved); } catch {}
      }
    }
    return null;
  });

  const [authToken, setAuthToken] = useState(tokenStore.getToken);

  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isSessionReady, setIsSessionReady] = useState<boolean>(!tokenStore.getToken());

  useEffect(() => {
    let cancelled = false;
    const restore = async () => {
      const token = tokenStore.getToken();
      if (!token || isMockMode()) return;
      const generation = authGeneration.current;
      const sequence = ++profileSequence.current;
      setIsSessionReady(false);
      try {
        const claims = await authApi.getMe();
        if (cancelled || !ownsRequest(generation, token, sequence)) return;
        setAuthToken(token);
        setCurrentUser({
          id: claims.userId,
          mobile: claims.mobile,
          fullName: claims.fullName,
          role: claims.role,
          capabilities: claims.capabilities || [],
        });
      } catch {
        if (!cancelled && ownsRequest(generation, token, sequence)) logoutUser();
      } finally {
        if (!cancelled && ownsRequest(generation, token, sequence)) setIsSessionReady(true);
      }
    };
    restore();
    const restoreFromAnotherTab = (event: StorageEvent) => {
      if (event.storageArea !== localStorage || event.key !== tokenStore.key ||
        !event.newValue || event.newValue !== tokenStore.getToken()) return;
      authGeneration.current += 1;
      tokenStore.invalidateRequests();
      setIsLoading(false);
      void restore();
    };
    window.addEventListener('storage', restoreFromAnotherTab);
    window.addEventListener('drup-session-changed', restore);
    return () => {
      cancelled = true;
      window.removeEventListener('storage', restoreFromAnotherTab);
      window.removeEventListener('drup-session-changed', restore);
    };
  }, []);

  // Role Sessions
  const [customerUser, setCustomerUser] = useState<CustomerUser>(() => {
      if (isMockMode()) {
        return {
          fullName: 'علی مرادی',
          mobile: '09123456789',
          nationalCode: '0012345678',
          isLoggedIn: false,
        };
      }
      return {
        fullName: '',
        mobile: '',
        nationalCode: '',
        isLoggedIn: false,
      };
  });

  const [adminSession, setAdminSession] = useState<AdminSession | null>(null);
  const [sellerSession, setSellerSession] = useState<SellerSession | null>(null);
  const [supplierSession, setSupplierSession] = useState<SupplierSession | null>(null);
  const [warehouseSession, setWarehouseSession] = useState<WarehouseSession | null>(null);
  const [panelSessionErrors, setPanelSessionErrors] = useState<Partial<Record<ProfilePanel, string>>>({});
  const [panelRetry, setPanelRetry] = useState(0);

  // ISO-E: stale-session guard. The panel effect below only creates a
  // session when none exists (!supplierSession / !sellerSession / ...),
  // so without this, logging out and back in as a DIFFERENT user (or a
  // different role/token for the same account) kept the previous user
  // supplier/seller/admin/warehouse session - including the previous
  // seller storeId and the previous supplier company/supplierId -
  // visible in the panels, and the new user profile was never loaded.
  // On identity change, drop only sessions bound to a DIFFERENT token:
  // the panel guards (SellerAuthGuard / SupplierAuthGuard /
  // AdminLoginPage) set their session with the fresh token directly, and
  // those must survive; a session whose token matches the new token is
  // already the new user. When identity becomes null (logout / failed
  // restore) every panel session is dropped. Mock mode is untouched.
  const lastIdentityRef = useRef<string | null>(null);
  useEffect(() => {
    if (isMockMode()) return;
    const identity = currentUser && authToken ? `${currentUser.id}:${authToken}` : null;
    if (lastIdentityRef.current === identity) return;
    lastIdentityRef.current = identity;
    if (identity === null || authToken === null) {
      setSupplierSession(null);
      setSellerSession(null);
      setAdminSession(null);
      setWarehouseSession(null);
      setCustomerUser({ fullName: '', mobile: '', nationalCode: '', isLoggedIn: false });
      setPanelSessionErrors({});
      return;
    }
    if (supplierSession && supplierSession.token !== authToken) setSupplierSession(null);
    if (sellerSession && sellerSession.token !== authToken) setSellerSession(null);
    if (adminSession && adminSession.token !== authToken) setAdminSession(null);
    if (warehouseSession && warehouseSession.token !== authToken) setWarehouseSession(null);
    setCustomerUser({ fullName: '', mobile: '', nationalCode: '', isLoggedIn: false });
    setPanelSessionErrors({});
  }, [currentUser, authToken, supplierSession, sellerSession, adminSession, warehouseSession]);

  useEffect(() => {
    if (!isSessionReady || !currentUser || !authToken) return;
    let active = true;
    const generation = authGeneration.current;
    const canApply = () => active && ownsRequest(generation, authToken);
    setPanelSessionErrors({});
    if (currentUser.capabilities?.includes('supplier') && !supplierSession) {
      authApi.getSupplierProfile().then((profile) => { if (!canApply()) return; setSupplierSession({
        token: authToken,
        supplierId: profile.id,
        companyName: profile.company_name,
        managerName: currentUser.fullName,
        warehouseCity: profile.warehouse_city,
        // Live parity (index-MERGED.js AuthContext effect + SupplierDashboardView-MERGED.js login):
        // only an explicit false means unconfirmed; null/undefined/true count as confirmed.
        warehouseCityConfirmed: profile.warehouse_city_confirmed !== false,
        permissions: profile.permissions || [],
        loginAt: new Date().toISOString(),
        signedContractVersion: profile.signedContractVersion ?? 0,
      }); }).catch((error: Error) => {
        if (canApply()) setPanelSessionErrors(previous => ({ ...previous, supplier_dashboard: error.message || 'بارگذاری پروفایل تأمین‌کننده ناموفق بود.' }));
      });
    } else if (!currentUser.capabilities?.includes('supplier') && supplierSession && !isMockMode()) {
      setSupplierSession(null);
    }
    if (currentUser.capabilities?.includes('seller') && !sellerSession) {
      storeApi.getOwnedStores().then((stores) => { if (!canApply()) return; setSellerSession({ token: authToken, mobile: currentUser.mobile, fullName: currentUser.fullName, status: stores.length ? 'active' : 'onboarding', storeId: stores[0]?.id, loginAt: new Date().toISOString() }); }).catch((error: Error) => {
        if (canApply()) setPanelSessionErrors(previous => ({ ...previous, seller_dashboard: error.message || 'بارگذاری فروشگاه ناموفق بود.' }));
      });
    } else if (!currentUser.capabilities?.includes('seller') && sellerSession && !isMockMode()) {
      setSellerSession(null);
    }
    const warehouseRole = warehouseRoleFromCapabilities(currentUser.capabilities);
    if (warehouseRole && (warehouseSession?.token !== authToken || warehouseSession.staffId !== currentUser.id || warehouseSession.role !== warehouseRole)) {
      setWarehouseSession({ token: authToken, staffId: currentUser.id, staffName: currentUser.fullName, role: warehouseRole, loginAt: new Date().toISOString() });
    } else if (!warehouseRole && warehouseSession && !isMockMode()) {
      setWarehouseSession(null);
    }
    const roles = adminRolesForCapabilities(currentUser.capabilities);
    const role = roles[0];
    if (role && (adminSession?.token !== authToken || adminSession.roles?.join() !== roles.join())) {
      setAdminSession({ token: authToken, username: currentUser.mobile, fullName: currentUser.fullName, role, roles, roleLabel: adminRoleLabels[role], loginAt: new Date().toISOString() });
    } else if (!role && adminSession && !isMockMode()) {
      setAdminSession(null);
    }
    if (currentUser.capabilities?.includes('customer')) {
      setCustomerUser((previous) => ({ ...previous, fullName: currentUser.fullName, mobile: currentUser.mobile, isLoggedIn: true }));
    } else if (!isMockMode()) {
      setCustomerUser(previous => previous.isLoggedIn ? { fullName: '', mobile: '', nationalCode: '', isLoggedIn: false } : previous);
    }
    return () => { active = false; };
  }, [authToken, currentUser, isSessionReady, supplierSession, sellerSession, warehouseSession, adminSession, panelRetry]);

  useEffect(() => {
    if (authToken) {
      localStorage.setItem('drup_auth_token', authToken);
      if (tokenStore.getToken() !== authToken) tokenStore.setToken(authToken);
    } else {
      localStorage.removeItem('drup_auth_token');
      tokenStore.clearToken();
    }
  }, [authToken]);

  useEffect(() => {
    if (currentUser) {
      localStorage.setItem('drup_current_user', JSON.stringify(currentUser));
    } else {
      localStorage.removeItem('drup_current_user');
    }
  }, [currentUser]);

  // Every panel shares one credential. Logout is independent of base role
  // and invalidates pending work before clearing any identity-bound state.
  const logoutUser = useCallback(() => {
    authGeneration.current += 1;
    tokenStore.clearToken();
    for (const key of ['drup_auth_token', 'drup_current_user', 'drup_customer_user',
      'drup_wishlists', 'drup_orders']) {
      localStorage.removeItem(key);
    }
    setAuthToken(null);
    setCurrentUser(null);
    setCustomerUser({ fullName: '', mobile: '', nationalCode: '', isLoggedIn: false });
    setAdminSession(null);
    setSellerSession(null);
    setSupplierSession(null);
    setWarehouseSession(null);
    setPanelSessionErrors({});
    setIsLoading(false);
    setIsSessionReady(true);
  }, []);

  // Logout in one tab revokes the shared browser session in every tab.
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.storageArea === localStorage &&
          (event.key === null || event.key === tokenStore.key) && event.newValue === null && !tokenStore.getToken()) {
        logoutUser();
      }
    };
    window.addEventListener('storage', onStorage);
    window.addEventListener('drup-session-expired', logoutUser);
    return () => { window.removeEventListener('storage', onStorage); window.removeEventListener('drup-session-expired', logoutUser); };
  }, [logoutUser]);

  const logoutCustomer = logoutUser;
  const logoutAdmin = logoutUser;
  const logoutSeller = logoutUser;
  const logoutSupplier = logoutUser;
  const logoutWarehouse = logoutUser;

  const authenticate = async <T extends AuthenticationResponse,>(request: () => Promise<T>): Promise<T & { isCurrent: () => boolean }> => {
    const generation = ++authGeneration.current;
    const credential = tokenStore.getToken();
    ++profileSequence.current;
    tokenStore.invalidateRequests();
    setIsLoading(true);
    try {
      let result: T;
      try { result = await request(); }
      catch (error) {
        if (!ownsRequest(generation, credential)) throw new AuthRequestSupersededError();
        throw error;
      }
      if (!ownsRequest(generation, credential)) throw new AuthRequestSupersededError();
      const user: AuthUser = { ...result.user, capabilities: result.user.capabilities || [] };
      setAuthToken(result.token);
      setCurrentUser(user);
      tokenStore.setToken(result.token);
      localStorage.setItem('drup_auth_token', result.token);
      localStorage.setItem('drup_current_user', JSON.stringify(user));
      return { ...result, isCurrent: () => ownsRequest(generation, result.token) };
    } finally {
      if (mounted.current && generation === authGeneration.current) setIsLoading(false);
    }
  };

  const loginWithOTP = async (mobile: string, otp: string, storeSlug?: string): Promise<AuthUser> => {
    const result = await authenticate(() => authApi.verifyOTP(mobile, otp, undefined, storeSlug));
    return result.user;
  };

  const registerUser = async (userData: { mobile: string; fullName: string; role?: string }) => {
    const generation = authGeneration.current;
    const credential = tokenStore.getToken();
    setIsLoading(true);
    try {
      if (!isMockMode()) {
        await authApi.sendOTP(userData.mobile);
      }
    } finally {
      if (ownsRequest(generation, credential)) setIsLoading(false);
    }
  };

  const updateProfileName = async (fullName: string) => {
    const generation = authGeneration.current;
    const credential = tokenStore.getToken();
    const claims = isMockMode() ? null : await authApi.updateProfileName(fullName);
    if (!ownsRequest(generation, credential)) throw new AuthRequestSupersededError();
    profileSequence.current += 1;
    const savedName = claims?.fullName ?? fullName;
    setCustomerUser(previous => ({ ...previous, fullName: savedName }));
    if (claims) {
      const user: AuthUser = {
        id: claims.userId, mobile: claims.mobile, fullName: savedName,
        role: claims.role, capabilities: claims.capabilities ?? [],
      };
      setCurrentUser(user);
      localStorage.setItem('drup_current_user', JSON.stringify(user));
    }
  };

  const refreshToken = async () => {
    const token = tokenStore.getToken();
    if (!token || isMockMode()) return;

    const generation = authGeneration.current;
    const sequence = ++profileSequence.current;
    try {
    const claims = await authApi.getMe();
    if (!ownsRequest(generation, token, sequence)) return;
    const user: AuthUser = {
      id: claims.userId,
      mobile: claims.mobile,
      fullName: claims.fullName,
      role: claims.role,
      capabilities: claims.capabilities || [],
    };
    setAuthToken(token);
    setCurrentUser(user);
    localStorage.setItem('drup_auth_token', token);
    localStorage.setItem('drup_current_user', JSON.stringify(user));
    } catch (error) {
      if (ownsRequest(generation, token, sequence)) throw error;
    } finally {
      if (ownsRequest(generation, token, sequence)) setIsSessionReady(true);
    }
  };

  const guardedSessionSetter = <T extends { token: string },>(setter: React.Dispatch<React.SetStateAction<T | null>>): React.Dispatch<React.SetStateAction<T | null>> =>
    (next) => setter(previous => {
      const value = typeof next === 'function' ? next(previous) : next;
      return isMockMode() || value === null || value.token === tokenStore.getToken() ? value : previous;
    });

  const capabilities = currentUser?.capabilities ?? [];
  const activeAdminRoles = adminRolesForCapabilities(capabilities);
  const currentWarehouseRole = warehouseRoleFromCapabilities(capabilities);
  const verifiedSession = <T extends { token: string },>(session: T | null, allowed: boolean): T | null => {
    if (isMockMode()) return session;
    return allowed && isSessionReady && authToken && session?.token === authToken && authToken === tokenStore.getToken() ? session : null;
  };
  const value: AuthContextType = {
    authenticate,
    currentUser,
    authToken,
    isAuthenticated: !!authToken,
    isLoading,
    isSessionReady,
    panelSessionErrors,
    retryPanelSession: () => setPanelRetry((value) => value + 1),
    customerUser,
    setCustomerUser,
    logoutCustomer,
    adminSession: verifiedSession(adminSession, activeAdminRoles.length > 0 && activeAdminRoles.join() === adminSession?.roles?.join()),
    setAdminSession: guardedSessionSetter(setAdminSession),
    logoutAdmin,
    sellerSession: verifiedSession(sellerSession, capabilities.includes('seller')),
    setSellerSession: guardedSessionSetter(setSellerSession),
    logoutSeller,
    supplierSession: verifiedSession(supplierSession, capabilities.includes('supplier')),
    setSupplierSession: guardedSessionSetter(setSupplierSession),
    logoutSupplier,
    warehouseSession: verifiedSession(warehouseSession, !!currentWarehouseRole && warehouseSession?.role === currentWarehouseRole && warehouseSession?.staffId === currentUser?.id),
    setWarehouseSession: guardedSessionSetter(setWarehouseSession),
    logoutWarehouse,
    loginWithOTP,
    logoutUser,
    registerUser,
    updateProfileName,
    refreshToken,
    panelForRole,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};
