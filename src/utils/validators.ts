// Iranian and Business Logic Validation Utilities

/**
 * Validates Iranian mobile numbers: 09xxxxxxxxx or 9xxxxxxxxx
 */
export const isValidIranianMobile = (mobile: string): boolean => {
  const cleaned = mobile.replace(/[^0-9]/g, '');
  const normalized = cleaned.startsWith('98') 
    ? '0' + cleaned.slice(2) 
    : cleaned.startsWith('9') 
      ? '0' + cleaned 
      : cleaned;
  return /^09[0-9]{9}$/.test(normalized);
};

/**
 * Standardizes mobile to 09xxxxxxxxx
 */
export const normalizeIranianMobile = (mobile: string): string => {
  const cleaned = mobile.replace(/[^0-9]/g, '');
  if (cleaned.startsWith('98')) return '0' + cleaned.slice(2);
  if (cleaned.startsWith('9') && cleaned.length === 10) return '0' + cleaned;
  return cleaned;
};

/**
 * Validates Iranian 10-digit national code with official checksum algorithm
 */
export const isValidIranianNationalCode = (code: string): boolean => {
  const cleaned = code.replace(/[^0-9]/g, '');
  if (cleaned.length !== 10) return false;
  if (/^(\d)\1{9}$/.test(cleaned)) return false; // Reject repeated digits like 0000000000

  const check = parseInt(cleaned[9], 10);
  let sum = 0;
  for (let i = 0; i < 9; i++) {
    sum += parseInt(cleaned[i], 10) * (10 - i);
  }
  const remainder = sum % 11;
  return (remainder < 2 && check === remainder) || (remainder >= 2 && check === 11 - remainder);
};

/**
 * Validates Iranian IBAN / Sheba (IR + 24 digits) with ISO 7064 Mod-97 check
 */
export const isValidIranianSheba = (sheba: string): boolean => {
  const cleaned = sheba.replace(/[\s-]/g, '').toUpperCase();
  if (!/^IR[0-9]{24}$/.test(cleaned)) return false;

  // Rearrange: Move "IR" (I=18, R=27) and two check digits to the end
  const numericString = cleaned.slice(4) + '1827' + cleaned.slice(2, 4);

  // Perform Mod 97 on large numeric string using chunked remainder
  let remainder = 0;
  for (let i = 0; i < numericString.length; i += 7) {
    const chunk = remainder.toString() + numericString.slice(i, i + 7);
    remainder = parseInt(chunk, 10) % 97;
  }
  return remainder === 1;
};

/**
 * Validates Iranian 10-digit postal code
 */
export const isValidIranianPostalCode = (postalCode: string): boolean => {
  const cleaned = postalCode.replace(/[^0-9]/g, '');
  if (cleaned.length !== 10) return false;
  // Iranian postal codes cannot contain '0' or '2' in the first 5 digits usually, and must be 10 digits
  return /^[1-9]{5}[0-9]{5}$/.test(cleaned);
};

/**
 * Reserved system slugs that cannot be used by dropshippers
 */
export const RESERVED_SLUGS = new Set([
  'admin', 'api', 'store', 'shop', 'main', 'central', 'root', 'login', 'auth', 
  'dashboard', 'supplier', 'warehouse', 'finance', 'support', 'tracking', 'checkout'
]);

/**
 * Validates a store slug
 */
export const validateStoreSlug = (slug: string): { valid: boolean; error?: string } => {
  const normalized = slug.trim().toLowerCase();
  if (!normalized) return { valid: false, error: 'نامک فروشگاه الزامی است.' };
  if (normalized.length < 3) return { valid: false, error: 'نامک باید حداقل ۳ نویسه باشد.' };
  if (normalized.length > 30) return { valid: false, error: 'نامک نمی‌تواند بیشتر از ۳۰ نویسه باشد.' };
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(normalized)) {
    return { valid: false, error: 'فقط حروف انگلیسی کوچک، اعداد و خط تیره (-) مجاز است.' };
  }
  if (RESERVED_SLUGS.has(normalized)) {
    return { valid: false, error: 'این نامک جزو نشانی‌های سیستمی و رزروشده پلتفرم است.' };
  }
  return { valid: true };
};
