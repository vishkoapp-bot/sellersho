import { mockProducts } from '../data/mockData';
import { Product, Order, Store } from '../types';

const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

export const mockApiService = {
  /**
   * Fetches catalog products with simulated network latency
   */
  async getProducts(category?: string): Promise<Product[]> {
    await delay(200);
    if (!category || category === 'all') {
      return [...mockProducts];
    }
    return mockProducts.filter(p => p.category === category);
  },

  /**
   * Searches catalog by query string
   */
  async searchProducts(query: string): Promise<Product[]> {
    await delay(180);
    const q = query.trim().toLowerCase();
    if (!q) return [...mockProducts];
    return mockProducts.filter(p => 
      p.title.toLowerCase().includes(q) || 
      p.description.toLowerCase().includes(q) ||
      p.category.toLowerCase().includes(q)
    );
  },

  /**
   * Simulates tracking code query
   */
  async trackOrder(trackingCode: string, orders: Order[]): Promise<Order | null> {
    await delay(250);
    const normalized = trackingCode.trim().toLowerCase();
    const found = orders.find(o => o.trackingCode.toLowerCase() === normalized);
    return found || null;
  },

  /**
   * Simulates Shaparak online payment processing & settlement
   */
  async processPayment(amount: number, cardMask: string): Promise<{ success: boolean; rrn: string; trackingCode: string }> {
    await delay(400);
    const rrn = Math.floor(100000000000 + Math.random() * 900000000000).toString();
    const trackingCode = `TRK-${Math.floor(100000 + Math.random() * 900000)}`;
    return {
      success: true,
      rrn,
      trackingCode,
    };
  }
};
