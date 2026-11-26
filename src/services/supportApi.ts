import { apiRequest } from './apiClient';

export type SupportRole = 'seller' | 'supplier';
export type SupportCategory = 'general' | 'order' | 'shipment' | 'return' | 'financial';
export type SupportStatus = 'open' | 'assigned' | 'pending' | 'resolved' | 'closed';

export interface SupportReferenceInput {
  kind: SupportCategory;
  referenceId?: string;
  referenceType?: string;
  referenceCode?: string;
  storeId?: string;
}

export interface SupportReference {
  category: SupportCategory;
  referenceId?: string;
  referenceType?: string;
  referenceLabel?: string;
}

export interface SupportTicket extends SupportReference {
  id: string;
  subject: string;
  status: SupportStatus;
  createdAt: string;
  messages: string[];
  messageTotal?: number;
}

export interface StaffSupportTicket extends Omit<SupportTicket, 'messages' | 'messageTotal'> {
  requesterRole: string;
  requesterAccountId: string;
  relatedOrderNumber: string;
}

interface StaffSupportMessage {
  id: string;
  body: string;
  isInternal: boolean;
  isRequester: boolean;
  createdAt: string;
}

const staffTicketPath = (id: string) => `/admin/tickets/${encodeURIComponent(id)}`;

export const supportApi = {
  preview(role: SupportRole, input: SupportReferenceInput, signal: AbortSignal) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(input)) if (value) params.set(key, value);
    return apiRequest<SupportReference>(`/${role}/tickets/context?${params}`, { auth: true, signal });
  },
  create(role: SupportRole, input: SupportReferenceInput & { subject: string; message: string }, requestKey: string) {
    return apiRequest<{ id: string }>(`/${role}/tickets`, { auth: true, method: 'POST', headers: { 'Idempotency-Key': requestKey }, body: JSON.stringify(input) });
  },
  list(role: SupportRole, cursor = '', signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: '25' });
    if (cursor) query.set('cursor', cursor);
    return apiRequest<{ tickets: SupportTicket[]; nextCursor: string; hasMore: boolean }>(`/${role}/tickets?${query}`, { auth: true, signal });
  },
  staffList(signal: AbortSignal) {
    return apiRequest<{ tickets: StaffSupportTicket[] }>('/admin/support/tickets', { auth: true, signal });
  },
  staffThread(id: string, signal?: AbortSignal) {
    return apiRequest<{ messages: StaffSupportMessage[]; status: SupportStatus }>(staffTicketPath(id), { auth: true, signal });
  },
  staffReply(id: string, body: string, isInternal: boolean) {
    return apiRequest<{ status: SupportStatus }>(`${staffTicketPath(id)}/reply`, { auth: true, method: 'POST', body: JSON.stringify({ body, isInternal }) });
  },
  staffSetStatus(id: string, status: SupportStatus) {
    return apiRequest<{ status: SupportStatus }>(staffTicketPath(id), { auth: true, method: 'PATCH', body: JSON.stringify({ status }) });
  },
};
