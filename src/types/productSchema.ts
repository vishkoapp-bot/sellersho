export interface ProductOption {
    id: string;
    label: string;
}
export interface ProductAttribute {
    id: string;
    name: string;
    role: 'fixed' | 'axis';
    source: 'color' | 'dk' | 'local' | 'requested';
    registryId?: number;
    values: ProductOption[];
}
export interface ProductSchema {
    version: 2;
    type: 'simple' | 'variable';
    attributes: ProductAttribute[];
}
export interface CatalogProductAttribute extends Omit<ProductAttribute, 'role'> {
}
export const MAX_PRODUCT_COMBINATIONS = 500;
