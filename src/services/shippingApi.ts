import { apiRequest } from './apiClient';

// FIX 5 (Team 03 G1/G2): live pre-checkout shipping quote.
// POST /api/v1/shipping/quote with {items, destination}; the response
// carries one leg per origin (legIndex, originLabel, fare,
// weightGrams, boxLabel), allocations linking each line to its leg,
// and totalShipping. This is distinct from the warehouse carrier
// tariff (/warehouse/carriers/quote), which is left untouched.

export interface ShippingQuoteItem {
  productId: string;
  variantId?: string;
  quantity: number;
}

export interface ShippingQuoteDestination {
  province: string;
  city: string;
  address: string;
  postalCode?: string;
}

export interface ShippingQuoteLeg {
  legIndex: number;
  index?: number;
  originId: string;
  originLabel: string;
  fare: number;
  weightGrams: number;
  boxLabel: string;
  source?: string;
  estimated?: boolean;
}

export interface ShippingQuoteAllocation {
  productId: string;
  variantId?: string;
  quantity: number;
  legIndex: number;
}

export interface ShippingQuote {
  success: boolean;
  quoteId: string;
  legs: ShippingQuoteLeg[];
  totalShipping: number;
  allocations: ShippingQuoteAllocation[];
}

export const shippingApi = {
  quote: async (
    items: ShippingQuoteItem[],
    destination: ShippingQuoteDestination,
  ): Promise<ShippingQuote> => {
    return apiRequest<ShippingQuote>('/shipping/quote', {
      method: 'POST',
      auth: true,
      body: JSON.stringify({ items, destination }),
    });
  },
};
