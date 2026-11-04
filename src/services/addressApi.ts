import { apiRequest } from './apiClient';

// Team 08 live parity (D63): customer address book, wired to Team 01's
// GET /customer/addresses (also /addresses). Field mapping: backend
// receiverFullName/receiverPhone/provinceName/cityName/postalCode/
// addressDetail -> checkout customerInfo fields.
export interface SavedAddress {
  id: string;
  label?: string;
  fullName: string;
  phone: string;
  province: string;
  city: string;
  postalCode: string;
  address: string;
  isDefault?: boolean;
}

interface BackendAddress {
  id: string;
  label?: string | null;
  receiverFullName: string;
  receiverPhone?: string | null;
  provinceName: string;
  cityName: string;
  postalCode?: string | null;
  addressDetail: string;
  isDefault: boolean;
}

// ISO-B checkout staging (live parity, index-MERGED.js iC hook +
// placeOrder fS()/gm()): the cart checkout form stages the address
// selection on every relevant field change; placeOrder consumes it
// once (take-and-clear) so a stale selection can never leak into a
// later order. An explicit addressId (direct-buy modal) always wins
// over a staged value in OrderContext: the cart form never stages
// while its picker is hidden, so a staged value seen during a
// direct-buy can only be a leftover from an earlier cart checkout.
export interface StagedAddressSelection {
  addressId?: string;
  saveAddress?: boolean;
}

let stagedAddressSelection: StagedAddressSelection | null = null;

export const stageAddressSelection = (selection: StagedAddressSelection | null): void => {
  stagedAddressSelection = selection;
};

export const takeStagedAddressSelection = (): StagedAddressSelection | null => {
  const selection = stagedAddressSelection;
  stagedAddressSelection = null;
  return selection;
};

// Live selectionForOrder (index-MERGED.js @463099): when saving is on
// and the current fields match a saved address on province/city/detail
// (trimmed), that address wins with saveAddress; otherwise the selected
// saved id wins; otherwise saving alone stages saveAddress. Manual
// edits deliberately do NOT clear the selection (live's
// notifyManualEdit is an identity): a selected saved address wins over
// edited fields at the backend, exactly like live direct-buy.
export const selectionForOrder = (
  fields: { province: string; city: string; address: string },
  book: SavedAddress[],
  selectedId: string | null,
  saveAddress: boolean,
): StagedAddressSelection | null => {
  const trim = (v: string) => (v || '').trim();
  if (saveAddress && trim(fields.address) !== '') {
    const match = book.find(
      (a) => trim(a.province) === trim(fields.province) && trim(a.city) === trim(fields.city) && trim(a.address) === trim(fields.address),
    );
    if (match) return { addressId: match.id, saveAddress: true };
  }
  if (selectedId) return { addressId: selectedId };
  if (saveAddress) return { saveAddress: true };
  return null;
};

export const addressApi = {
  getSavedAddresses: async (): Promise<SavedAddress[]> => {
    const data = await apiRequest<{ addresses: BackendAddress[] }>('/customer/addresses', {
      method: 'GET',
      auth: true,
    });
    const list = Array.isArray((data as any)?.addresses) ? (data as any).addresses : [];
    return list.map((a: BackendAddress): SavedAddress => ({
      id: a.id,
      label: a.label || undefined,
      fullName: a.receiverFullName,
      phone: a.receiverPhone || '',
      province: a.provinceName,
      city: a.cityName,
      postalCode: a.postalCode || '',
      address: a.addressDetail,
      isDefault: a.isDefault,
    }));
  },
};
