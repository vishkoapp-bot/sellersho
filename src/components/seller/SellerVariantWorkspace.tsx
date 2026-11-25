import { useCallback } from 'react';
import { loadSellerVariantWorkspace, saveSellerVariantWork } from '../../services/variantWorkspaceApi';
import { VariantWorkspace, type WorkspaceRow } from '../VariantWorkspace';

export function SellerVariantWorkspace({
  store, onChanged, attentionOnly, onBack,
}: {
  store: { id: string; slug: string; name?: string };
  onChanged: () => void;
  attentionOnly?: boolean;
  onBack?: () => void;
}) {
  const load = useCallback(async (signal: AbortSignal): Promise<WorkspaceRow[]> => {
    const products = await loadSellerVariantWorkspace(store.id, signal);
    return products.sort((a, b) => a.title.localeCompare(b.title, 'fa')).flatMap<WorkspaceRow>(product => {
      if (!product.variants.length) return product.needsAttention ? [{
        id: 'product:' + product.productId, productId: product.productId, title: product.title,
        name: 'کالا بدون تنوع قابل نمایش', spnc: '', attention: true, available: false, active: true, summary: true,
      }] : [];
      return product.variants.map(row => ({
        ...row, productId: product.productId, title: product.title, canEditSeller: row.active,
        attention: product.needsAttention && (!row.active || !row.available || !row.visible),
      }));
    });
  }, [store.id]);
  return <VariantWorkspace role="seller" title={store.name ? 'تنوع‌های ' + store.name : 'تنوع‌های فروشگاه'} load={load} initiallyOpen={attentionOnly} attentionOnly={attentionOnly}
    save={(source, row, field, _attempt, signal) => saveSellerVariantWork(store, source.productId, row, field, signal)}
    onChanged={onChanged} onBack={onBack} />;
}
