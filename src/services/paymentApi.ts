import { apiRequest } from './apiClient';

export interface ShaparakTokenRequest {
  orderId: string;
  orderNumber: string;
  amount: number;
  redirectUrl: string;
}

export interface ShaparakTokenResponse {
  success: boolean;
  token: string;
  paymentUrl: string;
  orderNumber: string;
  amount: number;
  message: string;
}

export interface ShaparakCallbackRequest {
  token: string;
  orderNumber: string;
  cardMask?: string;
}

export interface ShaparakCallbackResponse {
  success: boolean;
  status: string;
  rrn: string;
  cardMask: string;
  orderNumber: string;
  paymentDate: string;
  message: string;
}

export interface ShebaVerificationResponse {
  isValid: boolean;
  bankCode: string;
  bankName: string;
  iban: string;
  error?: string;
}

// Team 08 live parity (D75): fail-closed Shaparak callback simulator
// error mapping from live JS. An order that hits this error is
// "registered and awaiting payment", NEVER "paid successfully".
export function mapShaparakSimulatorError(message: string): string {
  if (message.includes('Shaparak callback simulator is strictly restricted')) {
    return 'درگاه پرداخت در این محیط در دسترس نیست. سفارش شما ثبت شده و در انتظار پرداخت است.';
  }
  if (message.includes('payment transaction was not persisted')) {
    return 'خطا در صدور توکن پرداخت. سفارش شما ثبت شده و در انتظار پرداخت است.';
  }
  return message;
}

// Team 08 live parity (D18): demo gateway API. DEMO/TEST ONLY, clearly
// labelled as such: this is never a real payment gateway and must never
// be presented to a customer as one. It is exposed here for testing
// tooling only; no checkout flow calls it.
export const demoGatewayApi = {
  getStatus: async (): Promise<unknown> => apiRequest('/payments/demo-gateway/status', { method: 'GET' }),
  getInfo: async (token: string): Promise<unknown> =>
    apiRequest(`/payments/demo-gateway/info?token=${encodeURIComponent(token)}`, { method: 'GET' }),
  postCallback: async (payload: Record<string, unknown>): Promise<unknown> =>
    apiRequest('/payments/demo-gateway/callback', { method: 'POST', body: JSON.stringify(payload) }),
};

export const paymentApi = {
  getShaparakToken: async (payload: ShaparakTokenRequest): Promise<ShaparakTokenResponse> => {
    return apiRequest<ShaparakTokenResponse>('/payments/shaparak/token', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  processShaparakCallback: async (
    payload: ShaparakCallbackRequest
  ): Promise<ShaparakCallbackResponse> => {
    return apiRequest<ShaparakCallbackResponse>('/payments/shaparak/callback', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },

  verifySheba: async (sheba: string): Promise<ShebaVerificationResponse> => {
    return apiRequest<ShebaVerificationResponse>('/bank/verify-sheba', {
      method: 'POST',
      body: JSON.stringify({ sheba }),
    });
  },
};
