import { useEffect, useRef, useState } from 'react';
import { mapBackendProduct } from '../../context/ProductContext';
import { ApiError } from '../../services/apiClient';
import { catalogApi, type ProductFilterParams } from '../../services/catalogApi';
import type { Product } from '../../types';

type Filters = Pick<ProductFilterParams, 'search' | 'category' | 'sort' | 'brands' | 'inStockOnly'>;
const defaultFilters: Filters = {};
const emptyItems: Product[] = [];

interface PageState {
  key: string;
  items: Product[];
  cursor: string | null;
  total: number;
  error: string;
  loadingMore: boolean;
}

export function useStorefrontProducts(storeSlug: string | undefined, filters: Filters = defaultFilters) {
  const signature = storeSlug ? JSON.stringify([storeSlug, filters]) : '';
  const active = useRef(signature);
  active.current = signature;
  const pending = useRef<AbortController | null>(null);
  const [state, setState] = useState<PageState | null>(null);
  const [revision, setRevision] = useState(0);
  const page = state?.key === signature ? state : null;
  const restart = () => {
    setState(null);
    setRevision(value => value + 1);
  };

  useEffect(() => {
    if (!storeSlug) return;
    const controller = new AbortController();
    pending.current = controller;
    const key = signature;
    catalogApi.getProductsPage({ ...filters, storeSlug, publicStorefront: true }, { signal: controller.signal })
      .then(result => {
        if (controller.signal.aborted || active.current !== key) return;
        setState({ key, items: result.items.map(mapBackendProduct), cursor: result.hasMore ? result.nextCursor : null,
          total: result.total, error: '', loadingMore: false });
      })
      .catch(() => {
        if (controller.signal.aborted || active.current !== key) return;
        setState({ key, items: [], cursor: null, total: 0, error: 'دریافت کالاهای ویترین انجام نشد.', loadingMore: false });
      })
      .finally(() => { if (pending.current === controller) pending.current = null; });
    return () => {
      pending.current?.abort();
      pending.current = null;
    };
  }, [filters, revision, signature, storeSlug]);

  const loadMore = () => {
    if (!storeSlug || !page?.cursor || pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setState({ ...page, loadingMore: true, error: '' });
    catalogApi.getProductsPage({ ...filters, storeSlug, publicStorefront: true }, { cursor: page.cursor, signal: controller.signal })
      .then(result => {
        if (controller.signal.aborted || active.current !== signature) return;
        const seen = new Set(page.items.map(product => product.id));
        setState({ ...page, items: [...page.items, ...result.items.filter(product => !seen.has(product.id)).map(mapBackendProduct)],
          cursor: result.hasMore ? result.nextCursor : null, total: result.total, error: '', loadingMore: false });
      })
      .catch(error => {
        if (controller.signal.aborted || active.current !== signature) return;
        if (error instanceof ApiError && error.status === 409) restart();
        else setState({ ...page, error: 'دریافت کالاهای بیشتر انجام نشد.', loadingMore: false });
      })
      .finally(() => { if (pending.current === controller) pending.current = null; });
  };

  return {
    items: page?.items ?? emptyItems,
    total: page?.total ?? 0,
    loading: Boolean(storeSlug && !page),
    hasMore: Boolean(page?.cursor),
    loadingMore: Boolean(page?.loadingMore),
    error: page?.error ?? '',
    loadMore,
    retry: page?.cursor ? loadMore : restart,
  };
}
