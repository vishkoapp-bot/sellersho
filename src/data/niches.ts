import { Niche } from '../types/index';

export const OFFICIAL_NICHES: Niche[] = [
  {
    id: 'niche-smartwatch',
    title: 'ساعت هوشمند و گجت‌های پوشیدنی',
    slug: 'smartwatch',
    categorySlug: 'digital',
    badge: 'پرفروش‌ترین نیچ دیجیتال',
    description: 'انواع ساعت‌های هوشمند اولترا، بندهای متنوع و گجت‌های پایش سلامت',
  },
  {
    id: 'niche-powerbank',
    title: 'پاوربانک و تجهیزات شارژ سریع',
    slug: 'powerbank',
    categorySlug: 'digital',
    badge: 'تقاضای مداوم بازار',
    description: 'پاوربانک‌های ظرفیت بالا، کابل‌های فست و آداپتورهای استاندارد',
  },
  {
    id: 'niche-audio',
    title: 'هدفون، هندزفری و اسپیکر بلوتوثی',
    slug: 'audio',
    categorySlug: 'digital',
    badge: 'حاشیه سود بالا',
    description: 'انواع هندزفری‌های نویزکنسلینگ و اسپیکرهای پرتابل باکیفیت',
  },
  {
    id: 'niche-gold',
    title: 'طلا و زیورآلات ۱۸ عیار استاندارد',
    slug: 'gold-jewelry',
    categorySlug: 'gold',
    badge: 'ارزش سبد بالا (AOV)',
    description: 'پلاک‌های طلای رسمی ون‌کلیف، دستبند و آویزهای با فاکتور معتبر اتحادیه',
  },
  {
    id: 'niche-leather',
    title: 'کیف و اکسسوری چرم طبیعی',
    slug: 'leather-fashion',
    categorySlug: 'fashion',
    badge: 'لوکس و ترند',
    description: 'کیف‌های دوشی، کمربند و دستبندهای چرم طبیعی دست‌دوز',
  },
  {
    id: 'niche-appliances',
    title: 'لوازم برقی و پخت‌وپز خانه',
    slug: 'home-appliances',
    categorySlug: 'appliances',
    badge: 'مشتریان خانگی',
    description: 'سرخ‌کن‌های بدون روغن، خردکن و ملزومات مدرن آشپزخانه',
  },
  {
    id: 'niche-beauty',
    title: 'مراقبت پوست، مو و زیبایی',
    slug: 'beauty-care',
    categorySlug: 'beauty',
    badge: 'خرید تکرارشونده',
    description: 'سرم‌های تقویت‌کننده، ماسک و لوازم مراقبت شخصی با ضمانت اصالت',
  },
  {
    id: 'niche-culture',
    title: 'کتاب کنکور، رمان و نوشت‌افزار',
    slug: 'books-stationery',
    categorySlug: 'culture',
    badge: 'فصل تحصیلی و آموزشی',
    description: 'کتاب‌های جامع کنکور، رمان‌های پرفروش و ملزومات لوکس تحریر',
  },
  {
    id: 'niche-car',
    title: 'لوازم فنی، نظم‌دهنده و نگهداری خودرو',
    slug: 'car-accessories',
    categorySlug: 'car',
    badge: 'بازار تخصصی',
    description: 'پمپ باد دیجیتال، جامپ‌استارتر و گجت‌های سفر و خودرو',
  },
];

/**
 * Priority 3: Official Mapping between Central Categories and Official 9 Niches
 * Eliminates string-matching/Regex guessing in favor of explicit taxonomy mapping
 */
export const CATEGORY_TO_OFFICIAL_NICHES_MAP: Record<string, string[]> = {
  digital: ['niche-smartwatch', 'niche-powerbank', 'niche-audio'],
  electronics: ['niche-powerbank', 'niche-audio'],
  gadgets: ['niche-smartwatch'],
  fashion: ['niche-leather'],
  apparel: ['niche-leather'],
  gold: ['niche-gold'],
  'gold-silver': ['niche-gold'],
  jewelry: ['niche-gold'],
  appliances: ['niche-appliances'],
  home: ['niche-appliances'],
  beauty: ['niche-beauty'],
  cosmetics: ['niche-beauty'],
  culture: ['niche-culture'],
  books: ['niche-culture'],
  car: ['niche-car'],
  tools: ['niche-car'],
  supermarket: ['niche-culture'],
  travel: ['niche-car'],
};

export const getProductNicheIds = (product: { nicheIds?: string[]; category?: string }): string[] => {
  // 1. Explicit nicheIds take highest precedence
  if (product.nicheIds && Array.isArray(product.nicheIds) && product.nicheIds.length > 0) {
    return product.nicheIds;
  }

  // 2. Formal taxonomy mapping based on category slug
  const cat = (product.category || '').toLowerCase().trim();
  if (cat && CATEGORY_TO_OFFICIAL_NICHES_MAP[cat]) {
    return CATEGORY_TO_OFFICIAL_NICHES_MAP[cat];
  }

  return ['niche-smartwatch'];
};

