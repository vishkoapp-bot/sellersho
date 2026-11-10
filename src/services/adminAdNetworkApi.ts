import { apiRequest } from './apiClient';

export type AdReportWindow = 7 | 30 | 90;
export interface AdNetworkReport {
  windowDays: AdReportWindow;
  from: string;
  to: string;
  limit: number;
  summary: {
    acceptedClicks: number;
    acceptedImpressions: number;
    rejectedClicks: number;
    rejectedImpressions: number;
    rejectedChallenges: number;
    otherRejectedEvents: number;
    totalRejectedEvents: number;
  };
  reasons: { reason: string; eventType: string; count: number }[];
  incidents: { id: string; campaignId: string; campaignName: string | null; storeName: string | null; eventType: string; reason: string; occurredAt: string }[];
}
export const adminAdNetworkApi = {
  report: (days: AdReportWindow, signal?: AbortSignal) => apiRequest<AdNetworkReport>(`/admin/ad-network/report?days=${days}`, { auth: true, signal }),
};
