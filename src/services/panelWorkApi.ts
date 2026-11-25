import { apiRequest } from './apiClient';

export interface SupplierWorkCounts {
  ready: number;
  overdue: number;
  openCartons: number;
  discrepancies: number;
}

export const getSupplierWorkCounts = (signal?: AbortSignal) =>
  apiRequest<SupplierWorkCounts>('/supplier/work-queue', { auth: true, signal });

export const getSellerWorkCounts = (slug: string, signal?: AbortSignal) =>
  apiRequest<{ orderFollowUp: number }>(`/stores/${encodeURIComponent(slug)}/work-queue`, { auth: true, signal });

export const getSellerOrderPage = (slug: string, work?: 'follow-up', cursor?: string, signal?: AbortSignal) => {
  const params = new URLSearchParams();
  if (work) params.set('work', work);
  if (cursor) params.set('cursor', cursor);
  const query = params.size ? `?${params}` : '';
  return apiRequest<{ orders: unknown[]; hasMore?: boolean; nextCursor?: string }>(`/stores/${encodeURIComponent(slug)}/orders${query}`, { auth: true, signal });
};
