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
let sessionGeneration = 0;

export const tokenStore = {
  getGeneration: (): number => sessionGeneration,
  invalidateRequests: (): void => { sessionGeneration++; },
  getToken: (): string | null => {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(TOKEN_KEY);
  },
  setToken: (token: string): void => {
    if (typeof window === 'undefined') return;
    sessionGeneration++;
    localStorage.setItem(TOKEN_KEY, token);
    window.dispatchEvent(new Event('drup-session-changed'));
  },
  clearToken: (): void => {
    if (typeof window === 'undefined') return;
    sessionGeneration++;
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
const DEFAULT_TIMEOUT_MS = 15000;

export const getApiBase = (): string => API_BASE;

export interface RequestOptions extends RequestInit {
  auth?: boolean;
  /** حداکثر زمان انتظار پاسخ بر حسب میلی‌ثانیه (فقط وقتی signal دستی داده نشده باشد اعمال می‌شود) */
  timeout?: number;
}

export async function apiRequest<T>(
  endpoint: string,
  options: RequestOptions = {}
): Promise<T> {
  const { auth = false, headers = {}, timeout = DEFAULT_TIMEOUT_MS, ...rest } = options;

  const reqHeaders: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(headers as Record<string, string>),
  };
  const requestGeneration = tokenStore.getGeneration();

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
      signal: rest.signal ?? AbortSignal.timeout(timeout),
    });
  } catch (err: any) {
    if (err?.name === 'TimeoutError' || err?.name === 'AbortError') {
      throw new ApiError(0, 'پاسخی از سرور دریافت نشد؛ لطفاً دوباره تلاش کنید.');
    }
    throw new ApiError(0, `خطای ارتباط با سرور: ${err.message || 'شبکه در دسترس نیست'}`);
  }

  if (response.status === 401) {
    const requestToken = reqHeaders['Authorization']?.replace(/^Bearer\s+/, '');
    if (auth && requestToken && requestToken === tokenStore.getToken() && requestGeneration === tokenStore.getGeneration()) {
      tokenStore.clearToken();
      window.dispatchEvent(new Event('drup-session-expired'));
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
      // A committed mutation can lose/truncate its response body. This is not
      // an acknowledgement; callers must keep their operation identity for a
      // safe replay. HTTP 204 intentionally has no representation.
      if (response.ok && response.status !== 204) {
        throw new ApiError(0, 'پاسخ سرور کامل دریافت نشد؛ نتیجه درخواست هنوز مشخص نیست.');
      }
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
