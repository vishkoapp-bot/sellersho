import { apiRequest } from './apiClient';

export interface OTPSendResponse {
  success: boolean;
  mobile: string;
  expiresIn: number;
  message: string;
}

export interface UserSummary {
  id: string;
  mobile: string;
  fullName: string;
  role: string;
  capabilities?: string[];
}

export interface OTPVerifyResponse {
  success: boolean;
  token: string;
  user: UserSummary;
}

export interface UserClaims {
  userId: string;
  mobile: string;
  fullName: string;
  role: string;
  capabilities?: string[];
}

export const authApi = {
  sendOTP: async (mobile: string): Promise<OTPSendResponse> => {
    const response = await apiRequest<OTPSendResponse>('/auth/otp/send', {
      method: 'POST',
      body: JSON.stringify({ mobile }),
    });
    return response;
  },

  verifyOTP: async (
    mobile: string,
    code: string,
    role?: string,
    storeSlug?: string
  ): Promise<OTPVerifyResponse> => {
    return apiRequest<OTPVerifyResponse>('/auth/otp/verify', {
      method: 'POST',
      body: JSON.stringify({ mobile, code, role, storeSlug }),
    });
  },

  registerSupplier: async (payload: {
    company_name: string;
    contact_person: string;
    phone: string;
    email: string;
    warehouse_city: string;
  }): Promise<{ success: boolean; supplierId: string }> => {
    return apiRequest<{ success: boolean; supplierId: string }>('/auth/supplier/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  },

  getMe: async (): Promise<UserClaims> => {
    return apiRequest<UserClaims>('/auth/me', {
      method: 'GET',
      auth: true,
    });
  },

  updateProfileName: (fullName: string): Promise<UserClaims> =>
    apiRequest<UserClaims>('/auth/me', { method: 'PATCH', auth: true, body: JSON.stringify({ fullName }) }),

  getSupplierProfile: async (): Promise<{ id: string; company_name: string; warehouse_city: string; permissions: string[]; signedContractVersion?: number | null; warehouse_city_confirmed?: boolean }> => {
    return apiRequest<{ id: string; company_name: string; warehouse_city: string; permissions: string[]; signedContractVersion?: number | null; warehouse_city_confirmed?: boolean }>('/supplier/profile', { method: 'GET', auth: true });
  },
};
