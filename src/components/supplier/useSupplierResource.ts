import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';

export function useSupplierResource<T>(load: () => Promise<T>) {
  const { supplierSession } = useStore();
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const generation = useRef(0);
  const reload = useCallback(async () => {
    const current = ++generation.current;
    setLoading(true);
    setError('');
    try {
      const result = await load();
      if (current === generation.current) setData(result);
    } catch (cause: unknown) {
      if (current === generation.current) setError(cause instanceof Error ? cause.message : 'دریافت اطلاعات انجام نشد.');
    } finally {
      if (current === generation.current) setLoading(false);
    }
  }, [load]);
  useEffect(() => {
    setData(null);
    void reload();
    return () => { ++generation.current; };
  }, [reload, supplierSession?.supplierId, supplierSession?.token]);
  return { data, loading, error, reload };
}