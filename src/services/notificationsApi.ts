import { apiRequest } from './apiClient';

// Role-scoped outbox read clients (Phase 2, W2).
//
// One client per role, all speaking the same envelope the supplier
// client already speaks: GET returns
// {success, notifications: [{id, kind, title, body, payload, status,
// createdAt, sentAt?}], unreadCount}; POST .../read accepts {id},
// {ids}, or an empty body (= mark-all) and answers
// {success, updated}. The recipient is always the caller's JWT user
// id — the backend scopes by token, never by a client-supplied id.
//
// Endpoints (backend/cmd/server/main.go):
//   customer   GET/POST /api/v1/notifications[/read]
//   seller     GET/POST /api/v1/seller/notifications[/read]
//   admin      GET/POST /api/v1/admin/notifications[/read]
//   warehouse  GET/POST /api/v1/warehouse/notifications[/read]
//   supplier   GET/POST /api/v1/supplier/notifications[/read]
//              (supplierApi — kept as-is; same shape)

export interface OutboxNotification {
  id: string;
  kind: string;
  title: string;
  body: string;
  payload?: Record<string, unknown>;
  status: string;
  createdAt: string;
  sentAt?: string;
}

export interface NotificationsResponse {
  success?: boolean;
  notifications: OutboxNotification[];
  unreadCount: number;
}

// Defensive normalization, identical to supplierApi.getNotifications:
// a bare array is tolerated, and unreadCount is derived from the rows
// when the envelope omits it (the server now always returns the full
// envelope, including when the outbox table is missing — P1-15 — so
// the fallback is a safety net, not a live branch).
export const normalizeNotificationsResponse = (data: any): NotificationsResponse => {
  const notifications: OutboxNotification[] = Array.isArray(data)
    ? data
    : (Array.isArray(data?.notifications) ? data.notifications : []);
  const unreadCount: number = typeof data?.unreadCount === 'number'
    ? data.unreadCount
    : notifications.filter((n) => n.status !== 'read').length;
  return { success: data?.success, notifications, unreadCount };
};

export interface RoleNotificationsApi {
  getNotifications: () => Promise<NotificationsResponse>;
  markNotificationsRead: (ids?: string[]) => Promise<{ success?: boolean; updated?: number; unreadCount?: number }>;
}

const makeRoleNotificationsApi = (basePath: string): RoleNotificationsApi => ({
  getNotifications: async (): Promise<NotificationsResponse> => {
    const data = await apiRequest<any>(basePath, { method: 'GET', auth: true });
    return normalizeNotificationsResponse(data);
  },
  markNotificationsRead: (ids?: string[]) =>
    apiRequest<{ success?: boolean; updated?: number; unreadCount?: number }>(
      `${basePath}/read`,
      { method: 'POST', auth: true, body: JSON.stringify(ids?.length ? { ids } : {}) },
    ),
});

export const customerNotificationsApi: RoleNotificationsApi =
  makeRoleNotificationsApi('/notifications');
export const sellerNotificationsApi: RoleNotificationsApi =
  makeRoleNotificationsApi('/seller/notifications');
export const adminNotificationsApi: RoleNotificationsApi =
  makeRoleNotificationsApi('/admin/notifications');
export const warehouseNotificationsApi: RoleNotificationsApi =
  makeRoleNotificationsApi('/warehouse/notifications');
