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

export const API_ERROR_MESSAGE_FA: Record<string, string> = {
  'Resource does not belong to your supplier': 'دسترسی به این مورد ممکن نیست.',
  'Shipment is not ready for shipping': 'این سفارش آماده ارسال نیست.',
  'Unauthorized': 'نشست شما تمام شده؛ دوباره وارد شوید.',
  'Supplier profile not found': 'پروفایل تأمین‌کننده پیدا نشد.',
  'Supplier scope required': 'دسترسی تأمین‌کننده برقرار نیست.',
  shipment_source_stock_insufficient: 'موجودی قابل رزرو در انبار مبدأ برای تعداد واردشده کافی نیست.',
  shipment_unit_coverage_gap: 'تعداد واحدهای دارای کد در انبار مبدأ برای این انتقال کافی نیست.',
  shipment_source_conflict: 'اطلاعات مبدأ محموله با موجودی ثبت‌شده سازگار نیست.',
};

export function toUserFacingErrorMessage(raw: unknown, status: number): string {
  const fallback = 'خطایی رخ داد؛ دوباره تلاش کنید.';
  let data = raw;
  if (typeof raw === 'string') {
    try {
      data = JSON.parse(raw);
    } catch {
      return fallback;
    }
  }

  if (typeof data !== 'object' || data === null) return fallback;

  const { error, message } = data as { error?: unknown; message?: unknown };
  const extracted = typeof error === 'string' ? error : message;
  if (typeof extracted !== 'string') return fallback;

  if (Object.hasOwn(API_ERROR_MESSAGE_FA, extracted)) {
    return API_ERROR_MESSAGE_FA[extracted];
  }
  return /[\u0600-\u06FF]/.test(extracted) ? extracted : fallback;
}

// Token Storage and Session Management
const TOKEN_KEY = 'seler_sho_jwt_token';
let onUnauthorizedCallback: (() => void) | null = null;
let sessionGeneration = 0;

export const tokenStore = {
  key: TOKEN_KEY,
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
  /** حداکثر زمان انتظار پاسخ بر حسب میلی‌ثانیه؛ همراه با signal دستی نیز اعمال می‌شود */
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
      signal: rest.signal ? AbortSignal.any([rest.signal, AbortSignal.timeout(timeout)]) : AbortSignal.timeout(timeout),
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
    throw new ApiError(response.status, toUserFacingErrorMessage(data, response.status), data);
  }

  return data as T;
}
