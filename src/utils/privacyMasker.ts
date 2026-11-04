/**
 * Privacy Masking Utility for Drup Dropship Platform
 * Protects customer Personally Identifiable Information (PII)
 * Enforces Principle 3 & Chapter 2 of Platform Specification
 */

export const maskName = (fullName?: string): string => {
  if (!fullName || !fullName.trim()) return 'کاربر محترم';
  const parts = fullName.trim().split(/\s+/);
  if (parts.length === 1) {
    const single = parts[0];
    return single.length > 2 ? `${single.slice(0, 1)}. ${single.slice(1, 2)}***` : `${single}***`;
  }
  const first = parts[0];
  const last = parts[parts.length - 1];
  return `${first.slice(0, 1)}. ${last.slice(0, 1)}***`;
};

export const maskPhone = (phone?: string): string => {
  if (!phone) return '***';
  const cleaned = phone.replace(/\D/g, '');
  if (cleaned.length >= 10) {
    const start = cleaned.slice(0, 4);
    const end = cleaned.slice(-3);
    return `${start}***${end}`;
  }
  return '۰۹***';
};

export const maskAddress = (address?: string, city?: string, province?: string): string => {
  const loc = [province, city].filter(Boolean).join('، ');
  return loc 
    ? `${loc} (آدرس دقیق و پلاک به دلایل حفظ محرمانگی خریدار نزد پلتفرم محفوظ است)`
    : 'نشانی دقیق گیرنده نزد انبار پلتفرم محفوظ است';
};

export const maskPostalCode = (postalCode?: string): string => {
  if (!postalCode) return '***';
  const cleaned = postalCode.replace(/\D/g, '');
  if (cleaned.length >= 6) {
    return `${cleaned.slice(0, 4)}******`;
  }
  return '******';
};

export interface CustomerInfoDTO {
  fullName: string;
  phone: string;
  province: string;
  city: string;
  address: string;
  postalCode: string;
  isMasked?: boolean;
}

export type ViewerRole = 'customer' | 'seller' | 'supplier' | 'warehouse' | 'admin' | 'public';

/**
 * Transforms CustomerInfo into a role-scoped DTO
 * Only customer (self), warehouse, supplier, and admin can see full shipping details.
 * Sellers and public viewers receive masked PII.
 */
export const maskCustomerForRole = (
  customerInfo: {
    fullName: string;
    phone: string;
    province: string;
    city: string;
    address: string;
    postalCode: string;
  },
  role: ViewerRole = 'seller'
): CustomerInfoDTO => {
  if (role === 'customer' || role === 'warehouse' || role === 'supplier' || role === 'admin') {
    return {
      ...customerInfo,
      isMasked: false,
    };
  }

  // Seller / Public: enforce strict privacy masking
  return {
    fullName: maskName(customerInfo.fullName),
    phone: maskPhone(customerInfo.phone),
    province: customerInfo.province,
    city: customerInfo.city,
    address: maskAddress(customerInfo.address, customerInfo.city, customerInfo.province),
    postalCode: maskPostalCode(customerInfo.postalCode),
    isMasked: true,
  };
};
