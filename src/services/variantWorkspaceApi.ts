import { ApiError, apiRequest } from './apiClient';
import { VariantEditConflict, type VariantEditPreview, type VariantField } from '../utils/variantWorkspace';

export interface SupplierVariantWorkRow {
  id: string;
  productId: string;
  title: string;
  name: string;
  spnc: string;
  active: boolean;
  sellable: number;
  reserved: number;
  cost: number | null;
  productCost: number;
  pendingCost?: number;
  pendingClear?: boolean;
  canEditStock: boolean;
  stockOriginRequired?: boolean;
  canEditCost: boolean;
}

export interface SellerVariantWorkRow {
  id: string;
  name: string;
  spnc: string;
  active: boolean;
  visible: boolean;
  available: boolean;
  price: number | null;
  effectiveMargin: number | null;
  margin: number | null;
  minimumMargin: number;
  maximumMargin: number;
  marginLocked: boolean;
}

export interface SellerVariantWorkProduct {
  productId: string;
  title: string;
  needsAttention: boolean;
  variants: SellerVariantWorkRow[];
}

interface SellerVariantWorkPage {
  workItems?: SellerVariantWorkProduct[];
  attentionCount?: number;
  nextCursor?: string;
}

async function* sellerVariantWorkPages(storeId: string, workspace: '1' | 'count', signal?: AbortSignal) {
  const query = new URLSearchParams({ storeId, workspace });
  let cursor: string | undefined;
  do {
    signal?.throwIfAborted();
    if (cursor) query.set('cursor', cursor);
    const page: SellerVariantWorkPage = await apiRequest(
      '/seller/variant-effective-margins?' + query, { auth: true, signal },
    );
    signal?.throwIfAborted();
    if (page.nextCursor && page.nextCursor === cursor) throw new Error('بارگیری تنوع‌های فروشگاه کامل نشد.');
    yield page;
    cursor = page.nextCursor;
  } while (cursor);
}

export async function loadSellerVariantWorkspace(storeId: string, signal?: AbortSignal): Promise<SellerVariantWorkProduct[]> {
  const result: SellerVariantWorkProduct[] = [];
  for await (const page of sellerVariantWorkPages(storeId, '1', signal)) result.push(...(page.workItems ?? []));
  return result;
}

export async function countSellerVariantAttention(storeId: string, signal?: AbortSignal): Promise<number> {
  let total = 0;
  for await (const page of sellerVariantWorkPages(storeId, 'count', signal)) {
    const count = page.attentionCount;
    if (typeof count !== 'number' || !Number.isSafeInteger(count) || count < 0) throw new Error('تعداد تنوع‌های نیازمند توجه دریافت نشد.');
    total += count;
  }
  return total;
}

export async function loadSupplierVariantWorkspace(signal?: AbortSignal): Promise<SupplierVariantWorkRow[]> {
  const data = await apiRequest<{ items: SupplierVariantWorkRow[] }>('/supplier/products/variant-workspace', { auth: true, signal });
  if (!Array.isArray(data?.items)) throw new Error('فهرست تنوع‌ها به‌طور کامل دریافت نشد.');
  return data.items;
}

function localizedVariantWriteError(cause: unknown): never {
  if (cause instanceof ApiError && cause.status === 409 && cause.body?.error === 'variant_workspace_conflict') {
    throw new VariantEditConflict('وضعیت تنوع تغییر کرده است. اطلاعات را تازه و پیش‌نمایش را دوباره تهیه کنید.');
  }
  if (cause instanceof ApiError && cause.status === 409 && cause.body?.error === 'variant_workspace_price_rejected') {
    throw new VariantEditConflict('درخواست تغییر قیمت رد شده است. وضعیت آن را در بخش قیمت‌ها بررسی کنید.');
  }
  if (cause instanceof ApiError && cause.status === 409 && cause.body?.error === 'stock_below_held') {
    throw new Error('موجودی واردشده کمتر از تعداد نگهداری‌شده در محل‌های دیگر است.');
  }
  if (cause instanceof ApiError && cause.status === 403) throw new Error('برای تغییر این تنوع دسترسی ندارید.');
  throw cause;
}

export async function saveSupplierVariantWork(
  source: Pick<SupplierVariantWorkRow, 'id' | 'productId'>, row: VariantEditPreview, field: VariantField, attempt: string, signal?: AbortSignal,
) {
  const product = '/supplier/products/' + encodeURIComponent(source.productId);
  const variant = product + '/variants/' + encodeURIComponent(source.id);
  try {
    if (field === 'stock') {
      if (row.current?.stock === undefined || row.edit.stock === undefined) throw new VariantEditConflict('موجودی اولیه یا مقدار جدید مشخص نیست. پیش‌نمایش را دوباره تهیه کنید.');
      const result = await apiRequest<{ product_id?: string; variant_id?: string; stock?: number; retired_printed_codes?: string[] }>(product + '/stock', {
        method: 'PUT', auth: true, signal, headers: { 'Idempotency-Key': attempt },
        body: JSON.stringify({ variantId: source.id, expected_stock: row.current?.stock, new_stock: row.edit.stock, reason: row.edit.reason }),
      });
      if (result?.product_id !== source.productId || result.variant_id !== source.id || result.stock !== row.edit.stock ||
        !Array.isArray(result.retired_printed_codes) || result.retired_printed_codes.some(code => typeof code !== 'string')) {
        throw new Error('رسید ثبت موجودی کامل نیست. همین تغییر را دوباره بررسی کنید.');
      }
      return { message: 'موجودی ثبت شد.', retired: result.retired_printed_codes };
    }
    if (field !== 'cost') throw new Error('عملیات ویرایش معتبر نیست.');
    if (row.current?.productCost === undefined || row.edit.cost === undefined) throw new VariantEditConflict('قیمت اولیه یا مقدار جدید مشخص نیست. پیش‌نمایش را دوباره تهیه کنید.');
    const result = await apiRequest<{ status?: string; id?: string; applied?: boolean; requestId?: string }>(variant + '/pricing', {
      method: 'PUT', auth: true, signal, headers: { 'Idempotency-Key': attempt },
      body: JSON.stringify({
        supplierBaseCost: row.edit.cost ?? undefined, clear: row.edit.cost === null, reason: row.edit.reason,
        expected: { cost: row.current?.cost ?? null, productCost: row.current?.productCost },
      }),
    });
    if (result?.status === 'pending_review' && typeof result.requestId === 'string' && result.requestId) {
      return { state: 'pending' as const, message: 'افزایش قیمت برای بررسی ثبت شد.' };
    }
    if (result?.applied === true && result.id === source.id) return { message: 'قیمت عمده ثبت شد.' };
    throw new Error('رسید ثبت قیمت کامل نیست. همین تغییر را دوباره بررسی کنید.');
  } catch (cause) {
    return localizedVariantWriteError(cause);
  }
}

export async function saveSellerVariantWork(
  store: { id: string; slug: string }, productId: string, row: VariantEditPreview, field: VariantField, signal?: AbortSignal,
) {
  try {
    if (field === 'visible' && row.edit.visible !== undefined) {
      const path = '/seller/stores/' + encodeURIComponent(store.slug) + '/products/' + encodeURIComponent(productId)
        + '/variants/' + encodeURIComponent(row.id) + '/visibility';
      await apiRequest(path, {
        method: 'PUT', auth: true, signal, body: JSON.stringify({ isVisible: row.edit.visible }),
      });
      return { message: row.edit.visible ? 'تنوع در فروشگاه نمایش داده می‌شود.' : 'تنوع در فروشگاه پنهان شد.' };
    }
    if (field !== 'margin' || row.edit.margin === undefined) throw new Error('عملیات ویرایش معتبر نیست.');
    await apiRequest('/seller/variant-overrides?workspace=1', {
      method: 'PUT', auth: true, signal,
      body: JSON.stringify({
        storeId: store.id, productId, variantId: row.id, margin: row.edit.margin, expected: { value: row.current?.margin ?? null },
      }),
    });
    return { message: 'درصد سود ثبت شد.' };
  } catch (cause) {
    return localizedVariantWriteError(cause);
  }
}
