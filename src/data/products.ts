import { Product } from '../types';
import { productsPart1 } from './productsPart1';
import { productsPart2 } from './productsPart2';
import { cultureProducts, technolifeProducts } from './extraProducts';

export const mockProducts: Product[] = [
  ...productsPart1,
  ...productsPart2,
  ...cultureProducts,
  ...technolifeProducts,
];

