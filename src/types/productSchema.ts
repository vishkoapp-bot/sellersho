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
// P1(b): the 500 cap applies to SELECTED rows only. The matrix itself
// may materialise up to this many rows for the picker (a UI guard, not
// a data rule — the backend cap is still 500 submitted SKUs).
export const MAX_MATRIX_ROWS = 5000;
