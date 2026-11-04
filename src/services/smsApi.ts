import { apiRequest } from './apiClient';

export interface SmsLog { id: string; mobile: string; type: string; providerMessageId: string; status: string; error?: string; createdAt: string }

export interface IPPanelSettings {
  enabled: boolean;
  baseUrl: string;
  apiKeyMasked: string;
  sender: string;
  patternCode: string;
}

export const smsApi = {
  getSettings: () => apiRequest<IPPanelSettings>('/admin/ippanel/settings', { method: 'GET', auth: true }),
  saveSettings: (payload: { enabled: boolean; baseUrl: string; apiKey?: string; sender: string; patternCode: string }) => apiRequest<{ success: boolean }>('/admin/ippanel/settings', { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  test: (recipient: string, code: string) => apiRequest<{ success: boolean; messageOutboxIds: string[] }>('/admin/ippanel/test', { method: 'POST', auth: true, body: JSON.stringify({ recipient, code }) }),
  credit: () => apiRequest<Record<string, unknown>>('/admin/sms/credit', { method: 'GET', auth: true }),
  logs: async (): Promise<SmsLog[]> => { const data = await apiRequest<SmsLog[] | unknown>('/admin/sms/logs', { method: 'GET', auth: true }); return Array.isArray(data) ? data : []; },
  recipients: (bulkId: string) => apiRequest<unknown>(`/admin/sms/report?bulk_id=${encodeURIComponent(bulkId)}`, { method: 'GET', auth: true }),
};