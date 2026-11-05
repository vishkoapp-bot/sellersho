import { apiRequest } from './apiClient';

// Per-store wishlist API (backend: handleT01Wishlist, migration
// 202611050002). Every wishlist belongs to exactly one store, so every
// call names its store: the backend scopes rows by the resolved store
// and fails closed (404) on an unknown slug. Omitting the slug would
// mean the central store, so scoped callers always pass theirs.
export interface WishlistResponse {
  wishlist: string[];
}

export const wishlistApi = {
  getWishlist: (storeSlug?: string): Promise<WishlistResponse> => {
    const qs = storeSlug ? `?storeSlug=${encodeURIComponent(storeSlug)}` : '';
    return apiRequest<WishlistResponse>(`/wishlist${qs}`, { method: 'GET', auth: true });
  },

  add: (productId: string, storeSlug?: string): Promise<{ success: boolean }> =>
    apiRequest<{ success: boolean }>('/wishlist', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(storeSlug ? { productId, storeSlug } : { productId }),
    }),

  remove: (productId: string, storeSlug?: string): Promise<{ success: boolean }> =>
    apiRequest<{ success: boolean }>('/wishlist', {
      method: 'DELETE',
      auth: true,
      body: JSON.stringify(storeSlug ? { productId, storeSlug } : { productId }),
    }),
};
