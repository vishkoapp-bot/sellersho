/**
 * Production API Client for Seler-Sho Platform
 * Direct integration with Go Backend Core on /api/v1 (Port 8080 via Vite Proxy)
 */

export class ApiError extends Error {
  public status: number;
  public body: any;

  constructor(status: number, message: string, body: any = null) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

// Token Storage and Session Management
const TOKEN_KEY = 'seler_sho_jwt_token';
let onUnauthorizedCallback: (() => void) | null = null;

export const tokenStore = {
  getToken: (): string | null => {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(TOKEN_KEY);
  },
  setToken: (token: string): void => {
    if (typeof window === 'undefined') return;
    localStorage.setItem(TOKEN_KEY, token);
    window.dispatchEvent(new Event('drup-session-changed'));
  },
  clearToken: (): void => {
    if (typeof window === 'undefined') return;
    localStorage.removeItem(TOKEN_KEY);
  },
  onUnauthorized: (callback: () => void): void => {
    onUnauthorizedCallback = callback;
  },
};

export const isMockMode = (): boolean => {
  return import.meta.env.VITE_USE_MOCK === 'true';
};

const API_BASE = '/api/v1';

export const getApiBase = (): string => API_BASE;

export interface RequestOptions extends RequestInit {
  auth?: boolean;
}

export async function apiRequest<T>(
  endpoint: string,
  options: RequestOptions = {}
): Promise<T> {
  const { auth = false, headers = {}, ...rest } = options;

  const reqHeaders: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(headers as Record<string, string>),
  };

  if (auth) {
    const token = tokenStore.getToken();
    if (token) {
      reqHeaders['Authorization'] = `Bearer ${token}`;
    }
  }

  const url = endpoint.startsWith('http')
    ? endpoint
    : `${API_BASE}${endpoint.startsWith('/') ? endpoint : `/${endpoint}`}`;

  let response: Response;
  try {
    response = await fetch(url, {
      ...rest,
      headers: reqHeaders,
    });
  } catch (err: any) {
    throw new ApiError(0, `خطای ارتباط با سرور: ${err.message || 'شبکه در دسترس نیست'}`);
  }

  if (response.status === 401) {
    const requestToken = reqHeaders['Authorization']?.replace(/^Bearer\s+/, '');
    if (auth && requestToken && requestToken === tokenStore.getToken()) {
      tokenStore.clearToken();
      if (onUnauthorizedCallback) {
        onUnauthorizedCallback();
      }
    }
  }

  let data: any = null;
  const contentType = response.headers.get('content-type');
  if (contentType && contentType.includes('application/json')) {
    try {
      data = await response.json();
    } catch {
      data = null;
    }
  } else {
    data = await response.text();
  }

  if (!response.ok) {
    const errorMsg =
      (typeof data === 'object' && (data?.error || data?.message)) ||
      (typeof data === 'string' && data) ||
      `خطای سرور با کد وضعیت ${response.status}`;
    throw new ApiError(response.status, errorMsg, data);
  }

  return data as T;
}
