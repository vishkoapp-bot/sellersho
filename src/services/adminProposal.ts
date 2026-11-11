import { ProposedProduct } from '../types';
export type AdminProposal = ProposedProduct & { variants: Record<string, unknown>[]; requestedAttributes: Record<string, unknown>; productType?: string };
export const mapAdminProposal = (raw: Record<string, any>): AdminProposal => ({
 ...raw,id:raw.id || raw.proposal_id,title:raw.title || '',proposedCost:Number(raw.supplierBaseCost ?? raw.proposedCost ?? 0),category:raw.categorySlug || raw.category || '',supplierId:raw.supplierId || raw.supplier_id || '',supplierName:raw.supplierName || raw.supplierId || raw.supplier_id || '',barcode:raw.barcode || '',warranty:raw.warranty || '',imageUrl:raw.imageUrl || raw.image || '',status:raw.status,variants:Array.isArray(raw.variants)?raw.variants:[],requestedAttributes:raw.requestedAttributes || {},
});
