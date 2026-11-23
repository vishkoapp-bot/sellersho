export const remainingCatalogCount = (total: number, loaded: number): number =>
  Math.max(0, total - loaded);
