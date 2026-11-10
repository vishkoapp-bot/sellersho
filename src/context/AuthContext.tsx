import React, { createContext, useContext, useState, useEffect, useRef, ReactNode } from 'react';
import { 
  SupplierSession, 
  WarehouseSession 
} from '../types';
import { 
  AdminSession, 
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

export const panelsForCapabilities = (capabilities: string[] = []): AuthenticatedPanel[] => {
  const panels: AuthenticatedPanel[] = [];
  if (capabilities.includes('customer')) panels.push('customer');
  if (capabilities.includes('seller')) panels.push('seller_dashboard');
  if (capabilities.includes('supplier')) panels.push('supplier_dashboard');
  if (capabilities.includes('warehouse_staff') || capabilities.includes('logistics_specialist')) panels.push('warehouse_panel');
  if (capabilities.some((capability) => ['admin', 'super_admin', 'finance', 'finance_specialist', 'support_specialist'].includes(capability))) panels.push('admin_panel');
  return panels;
};

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

export interface AuthContextType {
  currentUser: AuthUser | null;
  authToken: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  isSessionReady: boolean;
  panelSessionError: string | null;
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
  loginUser: (username: string, password: string) => Promise<AuthUser>;
  loginWithOTP: (mobile: string, otp: string, storeSlug?: string) => Promise<AuthUser>;
  logoutUser: () => void;
  registerUser: (userData: { mobile: string; fullName: string; role?: string }) => Promise<void>;
  updateProfile: (updates: Partial<CustomerUser>) => Promise<void>;
  refreshToken: () => Promise<void>;
  panelForRole: (role: string) => AuthenticatedPanel;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(() => {
    if (typeof window !== 'undefined') {
      const saved = localStorage.getItem('drup_current_user');
      if (saved) {
        try { return JSON.parse(saved); } catch {}
      }
    }
    return null;
  });

  const [authToken, setAuthToken] = useState<string | null>(() => {
    if (typeof window !== 'undefined') {
      return localStorage.getItem('drup_auth_token') || tokenStore.getToken();
    }
    return null;
  });

  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [isSessionReady, setIsSessionReady] = useState<boolean>(!tokenStore.getToken());

  useEffect(() => {
    let cancelled = false;
    const restore = async () => {
      const token = tokenStore.getToken();
      if (!token || isMockMode()) return;
      setIsSessionReady(false);
      try {
        const claims = await authApi.getMe();
        if (cancelled) return;
        setAuthToken(token);
        setCurrentUser({
          id: claims.userId,
          mobile: claims.mobile,
          fullName: claims.fullName,
          role: claims.role,
          capabilities: claims.capabilities || [],
        });
      } catch {
        if (!cancelled) {
          tokenStore.clearToken();
          localStorage.removeItem('drup_auth_token');
          localStorage.removeItem('drup_current_user');
          setAuthToken(null);
          setCurrentUser(null);
          // ISO-E: a failed restore (expired/revoked token) must not leave
          // the previous user panel sessions in memory for the next
          // render; the identity effect below also clears them, this is
          // the immediate path. Cart keys are NOT touched here (Fix72).
          setSupplierSession(null);
          setSellerSession(null);
          setAdminSession(null);
          setWarehouseSession(null);
          setCustomerUser({ fullName: '', mobile: '', nationalCode: '', isLoggedIn: false });
        }
      } finally {
        if (!cancelled) setIsSessionReady(true);
      }
    };
    restore();
    window.addEventListener('drup-session-changed', restore);
    return () => { cancelled = true; window.removeEventListener('drup-session-changed', restore); };
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
  const [panelSessionError, setPanelSessionError] = useState<string | null>(null);
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
      setPanelSessionError(null);
      return;
    }
    if (supplierSession && supplierSession.token !== authToken) setSupplierSession(null);
    if (sellerSession && sellerSession.token !== authToken) setSellerSession(null);
    if (adminSession && adminSession.token !== authToken) setAdminSession(null);
    if (warehouseSession && warehouseSession.token !== authToken) setWarehouseSession(null);
    setCustomerUser({ fullName: '', mobile: '', nationalCode: '', isLoggedIn: false });
    setPanelSessionError(null);
  }, [currentUser, authToken, supplierSession, sellerSession, adminSession, warehouseSession]);

  useEffect(() => {
    if (!isSessionReady || !currentUser || !authToken) return;
    setPanelSessionError(null);
    if (currentUser.capabilities?.includes('supplier') && !supplierSession) {
      authApi.getSupplierProfile().then((profile) => setSupplierSession({
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
      })).catch((error: Error) => setPanelSessionError(error.message || 'بارگذاری پروفایل تأمین‌کننده ناموفق بود.'));
    }
    if (currentUser.capabilities?.includes('seller') && !sellerSession) {
      storeApi.getOwnedStores().then((stores) => setSellerSession({ token: authToken, mobile: currentUser.mobile, fullName: currentUser.fullName, status: stores.length ? 'active' : 'onboarding', storeId: stores[0]?.id, loginAt: new Date().toISOString() })).catch((error: Error) => setPanelSessionError(error.message || 'بارگذاری فروشگاه ناموفق بود.'));
    }
    if (currentUser.capabilities?.some((capability) => ['warehouse_staff', 'logistics_specialist'].includes(capability)) && !warehouseSession) {
      setWarehouseSession({ token: authToken, staffId: currentUser.id, staffName: currentUser.fullName, warehouseId: 'WH-TEH-CENTRAL', warehouseName: 'انبار مرکزی', role: currentUser.role === 'logistics_specialist' ? 'logistics_specialist' : 'warehouse_staff', loginAt: new Date().toISOString() });
    }
    if (currentUser.capabilities?.some((capability) => ['admin', 'super_admin', 'finance', 'finance_specialist', 'support_specialist'].includes(capability)) && !adminSession) {
      // Live parity: preserve the real role (plain 'admin' stays 'admin');
      // only a super_admin capability/role upgrades, as in live's role resolution.
      const role = currentUser.capabilities.includes('super_admin') || currentUser.role === 'super_admin' ? 'super_admin' : currentUser.role === 'finance' ? 'finance_specialist' : currentUser.role as AdminSession['role'];
      setAdminSession({ token: authToken, username: currentUser.mobile, fullName: currentUser.fullName, role, roleLabel: role, loginAt: new Date().toISOString() });
    }
    if (currentUser.capabilities?.includes('customer')) {
      setCustomerUser((previous) => ({ ...previous, fullName: currentUser.fullName, mobile: currentUser.mobile, isLoggedIn: true }));
    }
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

  const logoutCustomer = () => {
    setCustomerUser({
      fullName: '',
      mobile: '',
      nationalCode: '',
      isLoggedIn: false,
    });
    // Clear the shared session regardless of which login path created it.
    setCurrentUser(null);
    setAuthToken(null);
    tokenStore.clearToken();
    // پاک کردن localStorage
    if (typeof window !== 'undefined') {
      localStorage.removeItem('drup_auth_token');
      localStorage.removeItem('drup_current_user');
      localStorage.removeItem('drup_customer_user');
      localStorage.removeItem('drup_cart');
      localStorage.removeItem('drup_carts');
      localStorage.removeItem('drup_wishlists');
      localStorage.removeItem('drup_orders');
    }
  };

  const logoutAdmin = () => {
    setAdminSession(null);
    if (currentUser?.role === 'admin' || currentUser?.role === 'super_admin' || currentUser?.role === 'finance_specialist') {
      setCurrentUser(null);
      setAuthToken(null);
    }
  };

  const logoutSeller = () => {
    setSellerSession(null);
    if (currentUser?.role === 'seller') {
      setCurrentUser(null);
      setAuthToken(null);
    }
  };

  const logoutSupplier = () => {
    setSupplierSession(null);
    if (currentUser?.role === 'supplier' || supplierSession) {
      setCurrentUser(null);
      setAuthToken(null);
      tokenStore.clearToken();
      localStorage.removeItem('drup_auth_token');
    }
  };

  const logoutWarehouse = () => {
    setWarehouseSession(null);
    if (currentUser?.role === 'warehouse_staff') {
      setCurrentUser(null);
      setAuthToken(null);
    }
  };

  const logoutUser = () => {
    setCurrentUser(null);
    setAuthToken(null);
    logoutCustomer();
    logoutAdmin();
    logoutSeller();
    logoutSupplier();
    logoutWarehouse();
  };

  const loginUser = async (username: string, password: string): Promise<AuthUser> => {
    setIsLoading(true);
    try {
      const res = await authApi.passwordLogin(username, password);
      const user: AuthUser = {
        id: res.user.id,
        mobile: res.user.mobile,
        fullName: res.user.fullName,
        role: res.user.role,
        capabilities: res.user.capabilities || [],
        username,
      };
      setAuthToken(res.token);
      setCurrentUser(user);
      tokenStore.setToken(res.token);
      localStorage.setItem('drup_auth_token', res.token);
      localStorage.setItem('drup_current_user', JSON.stringify(user));
      return user;
    } finally {
      setIsLoading(false);
    }
  };

  const loginWithOTP = async (mobile: string, otp: string, storeSlug?: string): Promise<AuthUser> => {
    setIsLoading(true);
    try {
      const res = await authApi.verifyOTP(mobile, otp, undefined, storeSlug);
      const user: AuthUser = {
        id: res.user.id,
        mobile: res.user.mobile,
        fullName: res.user.fullName,
        role: res.user.role,
        capabilities: res.user.capabilities || [],
      };
      setAuthToken(res.token);
      setCurrentUser(user);
      tokenStore.setToken(res.token);
      localStorage.setItem('drup_auth_token', res.token);
      localStorage.setItem('drup_current_user', JSON.stringify(user));
      if (user.role === 'customer') {
        setCustomerUser({
          fullName: user.fullName,
          mobile: user.mobile,
          nationalCode: '0012345678',
          isLoggedIn: true,
        });
      }
      return user;
    } finally {
      setIsLoading(false);
    }
  };

  const registerUser = async (userData: { mobile: string; fullName: string; role?: string }) => {
    setIsLoading(true);
    try {
      if (!isMockMode()) {
        await authApi.sendOTP(userData.mobile);
      }
    } finally {
      setIsLoading(false);
    }
  };

  const updateProfile = async (updates: Partial<CustomerUser>) => {
    setCustomerUser((prev) => ({ ...prev, ...updates }));
    if (currentUser) {
      setCurrentUser((prev) => prev ? {
        ...prev,
        fullName: updates.fullName || prev.fullName,
        mobile: updates.mobile || prev.mobile,
      } : null);
    }
  };

  const refreshToken = async () => {
    const token = tokenStore.getToken();
    if (!token || isMockMode()) return;

    const claims = await authApi.getMe();
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
  };

  const value: AuthContextType = {
    currentUser,
    authToken,
    isAuthenticated: !!authToken,
    isLoading,
    isSessionReady,
    panelSessionError,
    retryPanelSession: () => setPanelRetry((value) => value + 1),
    customerUser,
    setCustomerUser,
    logoutCustomer,
    adminSession,
    setAdminSession,
    logoutAdmin,
    sellerSession,
    setSellerSession,
    logoutSeller,
    supplierSession,
    setSupplierSession,
    logoutSupplier,
    warehouseSession,
    setWarehouseSession,
    logoutWarehouse,
    loginUser,
    loginWithOTP,
    logoutUser,
    registerUser,
    updateProfile,
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
