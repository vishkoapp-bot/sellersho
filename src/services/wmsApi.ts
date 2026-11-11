import { apiRequest } from './apiClient';

export interface WarehouseBinDto {
  positionX?: number;
  positionY?: number;
  positionZ?: number;
  binCode: string;
  hallName: string;
  aisle: string;
  shelf: string;
  capacityPercent: number;
  productId?: string;
  productTitle?: string;
  quantity: number;
  updatedAt: string;
  warehouseName?: string;
  capacityVolumeCm3?: number;
  occupiedVolumeCm3?: number;
  reservedVolumeCm3?: number;
  remainingVolumeCm3?: number;
  maxWeightGrams?: number;
  occupiedWeightGrams?: number;
  capacityStatus?: 'unverified' | 'verified' | 'disputed';
  unavailableReason?: string;
}

export interface InboundShipmentDto {
  id: string;
  supplierId: string;
  supplierName: string;
  warehouseName: string;
  itemsCount: number;
  cartonsCount: number;
  carrier?: string;
  truckPlate?: string;
  status: string;
  createdAt: string;
  receivedAt?: string;
  items?: Array<{ productId?: string; product_id?: string; barcode?: string; quantity?: number }>;
  scannedItems?: Array<{ productId?: string; product_id?: string; barcode?: string; quantity?: number }>;
}

export interface QCQueueItemDto {
  id: string;
  productId: string;
  productTitle: string;
  inboundId: string;
  supplierName: string;
  expectedQuantity: number;
  receivedQuantity: number;
  createdAt: string;
}

export interface WarehouseOverviewDto {
  warehouseName: string;
  capacityVolumeCm3: number;
  occupiedVolumeCm3: number;
  reservedVolumeCm3: number;
  capacityPercent: number;
  inboundPending: number;
  verifiedMovements: number;
}

export interface ShippingManifestDto {
  id: string;
  carrier: string;
  driverName: string;
  packagesCount: number;
  status: string;
  createdAt: string;
  dispatchedAt?: string;
}

export interface PutawayTaskDto {
  id: string;
  productId: string;
  title: string;
  inboundId: string;
  binLocation: string;
  quantity: number;
  status: string;
  createdAt: string;
  lengthMm: number;
  widthMm: number;
  heightMm: number;
  weightGrams: number;
  measurementStatus: string;
}

export const wmsApi = {
  getBins: async (): Promise<WarehouseBinDto[]> => {
    const data = await apiRequest<WarehouseBinDto[] | { bins?: WarehouseBinDto[]; data?: WarehouseBinDto[] }>('/warehouse/bins', {
      method: 'GET',
      auth: true,
    });
    return Array.isArray(data) ? data : (data?.bins ?? data?.data ?? []);
  },
  getOverview: () => apiRequest<WarehouseOverviewDto>('/warehouse/overview', { method: 'GET', auth: true }),

  getInbounds: async (): Promise<InboundShipmentDto[]> => {
    const response = await apiRequest<InboundShipmentDto[] | { inbounds?: InboundShipmentDto[] }>('/warehouse/inbounds', {
      method: 'GET',
      auth: true,
    });
    return Array.isArray(response) ? response : response.inbounds || [];
  },

  startReceiving: (inboundId: string) => apiRequest<{ success: boolean; status: string }>(`/warehouse/inbounds/${encodeURIComponent(inboundId)}/start-receiving`, { method: 'POST', auth: true }),
  scanInboundItem: (inboundId: string, payload: { productId?: string; barcode?: string; quantity: number }) => apiRequest<{ success: boolean; scannedCount: number }>(`/warehouse/inbounds/${encodeURIComponent(inboundId)}/scan-item`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  completeReceiving: (inboundId: string) => apiRequest<{ success: boolean; status: string }>(`/warehouse/inbounds/${encodeURIComponent(inboundId)}/complete`, { method: 'POST', auth: true }),
  getQCQueue: () => apiRequest<{ success: boolean; queue: QCQueueItemDto[] }>('/warehouse/qc/queue', { method: 'GET', auth: true }),
  completeQC: (productId: string, payload: { inboundId: string; passedQuantity: number; failedQuantity: number; shortageQuantity: number; defectReason: string }) => apiRequest<{ success: boolean; status: string }>(`/warehouse/qc/${encodeURIComponent(productId)}/complete`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),

  getManifests: async (): Promise<ShippingManifestDto[]> => {
    return apiRequest<ShippingManifestDto[]>('/warehouse/manifests', {
      method: 'GET',
      auth: true,
    });
  },

  dispatchManifest: async (
    manifestId: string
  ): Promise<{ success: boolean; manifestId: string; status: string }> => {
    return apiRequest<{ success: boolean; manifestId: string; status: string }>(
      `/warehouse/manifests/${encodeURIComponent(manifestId)}/dispatch`,
      {
        method: 'POST',
        auth: true,
      }
    );
  },

  updateBinCapacity: (binCode: string, payload: { capacityVolumeCm3: number; maxWeightGrams?: number; verify: boolean }) => apiRequest(`/warehouse/bins/${encodeURIComponent(binCode)}/capacity`, { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  recordMeasurement: (productId: string, payload: { lengthMm: number; widthMm: number; heightMm: number; weightGrams: number; reason: string }) => apiRequest(`/warehouse/products/${encodeURIComponent(productId)}/measurements`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  assignPutaway: (productId: string, binLocation: string, quantity: number) => apiRequest<{ taskId: string }>('/warehouse/putaway/' + encodeURIComponent(productId) + '/assign', { method: 'POST', auth: true, body: JSON.stringify({ binLocation, quantity }) }),
  confirmPutaway: (taskId: string) => apiRequest(`/warehouse/putaway/${encodeURIComponent(taskId)}/confirm`, { method: 'POST', auth: true }),
  getPutawayQueue: () => apiRequest<{ success: boolean; queue: PutawayTaskDto[] }>('/warehouse/putaway/queue', { method: 'GET', auth: true }),
  assignPutawayTask: (taskId: string, binLocation: string) => apiRequest(`/warehouse/putaway/tasks/${encodeURIComponent(taskId)}/assign`, { method: 'POST', auth: true, body: JSON.stringify({ binLocation }) }),
  // FIX 17: warehouse new tabs. /warehouses/returns serves
  // customer returns (handleWarehousesReturns);
  // /warehouse/return-backs is the Team 01 stub route.
  getCustomerReturns: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/warehouses/returns', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.returns ?? data?.data ?? []);
  },
  getReturnBacks: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/warehouse/return-backs', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.returnBacks ?? data?.returns ?? data?.data ?? []);
  },
};
