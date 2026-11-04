import { Product, Store } from '../types';
import { getProductNicheIds } from '../data/niches';

export const getCentralDealCandidates = (products: Product[]) =>
  products.filter((product) => product.stock > 0 && (product.specialDeal || product.discountPercent > 10));

// ISO-A (user-ordered storefront separation): a dropshipper storefront
// may only ever show deals for products this store itself publishes.
// Previously the niche/manual/'all' modes filtered the FULL central
// catalogue, so a central product in the same niche (or manually
// listed) but with zero store_products rows for this store appeared
// in the storefront with a working add-to-cart button - the same leak
// class as the preview buyable-central-product bug (order creation
// then 400s "is not published by this store"). The central store is
// unaffected (ownerType !== 'dropshipper' keeps the old behaviour),
// and a dropshipper with no recorded activeProductIds shows no deals
// at all instead of the whole central catalogue.
export function getCentralDeals(products: Product[], store: Store) {
  const candidates = getCentralDealCandidates(products);
  if (store.ownerType !== 'dropshipper') return candidates;
  const active = store.activeProductIds;
  if (!active || active.length === 0) return [];
  const own = candidates.filter((product) => active.includes(product.id));
  if (store.centralDealsFilter === 'all') return own;
  if (store.centralDealsFilter === 'manual') {
    return own.filter((product) => store.centralDealsProductIds?.includes(product.id));
  }
  const niches = store.selectedNicheIds?.length ? store.selectedNicheIds :
    products.filter((product) => active.includes(product.id)).flatMap(getProductNicheIds);
  return own.filter((product) => getProductNicheIds(product).some((id) => niches.includes(id)));
}