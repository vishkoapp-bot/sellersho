// Clean inline SVG data URLs for reliable offline rendering

export const createSvgImage = (
  title: string,
  category: string,
  gradientStart: string,
  gradientEnd: string,
  iconSvg: string
): string => {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="400" viewBox="0 0 400 400">
    <defs>
      <linearGradient id="g" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="${gradientStart}"/>
        <stop offset="100%" stop-color="${gradientEnd}"/>
      </linearGradient>
      <pattern id="p" width="20" height="20" patternUnits="userSpaceOnUse">
        <circle cx="10" cy="10" r="1.5" fill="white" fill-opacity="0.12"/>
      </pattern>
    </defs>
    <rect width="100%" height="100%" rx="32" fill="url(#g)"/>
    <rect width="100%" height="100%" rx="32" fill="url(#p)"/>
    <g transform="translate(140, 110)" fill="white" stroke="white" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      ${iconSvg}
    </g>
    <rect x="20" y="270" width="360" height="85" rx="20" fill="black" fill-opacity="0.25"/>
    <text x="200" y="310" font-family="Vazirmatn, Tahoma, sans-serif" font-size="19" font-weight="bold" fill="white" text-anchor="middle" direction="rtl">${title}</text>
    <text x="200" y="338" font-family="Vazirmatn, Tahoma, sans-serif" font-size="14" fill="#f8fafc" fill-opacity="0.9" text-anchor="middle" direction="rtl">${category}</text>
  </svg>`;

  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
};

export const defaultImages = {
  goldTurtle: createSvgImage(
    'پلاک طلا ۱۸ عیار',
    'طرح لاکپشت میوگلد',
    '#b45309',
    '#f59e0b',
    '<circle cx="60" cy="60" r="45" fill="none" stroke-width="6"/><path d="M60 25 L60 95 M25 60 L95 60" stroke-width="4"/>'
  ),
  airpods: createSvgImage(
    'ایرپاد پرو ۲ نسل جدید',
    'نویز کنسلینگ فعال',
    '#1e40af',
    '#3b82f6',
    '<rect x="25" y="30" width="70" height="55" rx="25" fill="none" stroke-width="6"/><path d="M45 85 L45 105 M75 85 L75 105" stroke-width="6"/>'
  ),
  earrings: createSvgImage(
    'گوشواره طلا ۱۸ عیار',
    'دیبا گلد مدل گوی',
    '#d97706',
    '#fbbf24',
    '<circle cx="60" cy="40" r="25" fill="none" stroke-width="6"/><circle cx="60" cy="85" r="18" fill="white" fill-opacity="0.3"/>'
  ),
  borlaDevice: createSvgImage(
    'دستگاه ۷ کاره جوانساز',
    'بورلا بیوتی کلینیکال',
    '#9333ea',
    '#ec4899',
    '<path d="M40 30 Q60 15 80 30 L70 95 Q60 105 50 95 Z" fill="none" stroke-width="6"/><circle cx="60" cy="55" r="8"/>'
  ),
  bivaBag: createSvgImage(
    'کیف دوشی ضدآب بیوا',
    'مسترکالا سری Biva-03',
    '#0f766e',
    '#14b8a6',
    '<rect x="35" y="40" width="50" height="65" rx="8" fill="none" stroke-width="6"/><path d="M48 40 V28 A12 12 0 0 1 72 28 V40" stroke-width="5" fill="none"/>'
  ),
  smartWatch: createSvgImage(
    'ساعت هوشمند اولترا ۲',
    'AMOLED با بند آلپاین',
    '#0369a1',
    '#06b6d4',
    '<rect x="40" y="35" width="40" height="50" rx="10" fill="none" stroke-width="6"/><path d="M50 35 V20 H70 V35 M50 85 V100 H70 V85" stroke-width="5"/>'
  ),
  philipsAirfryer: createSvgImage(
    'سرخ‌کن بدون روغن فیلیپس',
    'مدل HD9270 ظرفیت ۶.۲L',
    '#374151',
    '#4b5563',
    '<rect x="30" y="30" width="60" height="65" rx="15" fill="none" stroke-width="6"/><circle cx="60" cy="50" r="10" stroke-width="4"/>'
  ),
  hoodie: createSvgImage(
    'هودی اورسایز کتان',
    'استایل خیابانی پاییزه',
    '#be123c',
    '#f43f5e',
    '<path d="M30 40 L50 25 L70 25 L90 40 L80 95 L40 95 Z" fill="none" stroke-width="6"/>'
  ),
  bannerInstallments: createSvgImage(
    'سرویس اعتباری ۴ قسطه',
    'خرید بدون چک و ضامن',
    '#1e3a8a',
    '#2563eb',
    '<rect x="25" y="35" width="70" height="45" rx="10" fill="none" stroke-width="6"/><line x1="25" y1="50" x2="95" y2="50" stroke-width="5"/>'
  ),
  bannerGold: createSvgImage(
    'جشنواره طلایی میوگلد',
    'طلای ۱۸ عیار با کمترین اجرت',
    '#92400e',
    '#d97706',
    '<circle cx="60" cy="55" r="35" fill="none" stroke-width="6"/><path d="M45 40 L75 70 M45 70 L75 40" stroke-width="5"/>'
  ),
  bannerDigital: createSvgImage(
    'تخفیف شگفت‌انگیز دیجیتال',
    'تا ۴۲٪ تخفیف همراه ضمانت',
    '#581c87',
    '#7c3aed',
    '<rect x="35" y="25" width="50" height="75" rx="12" fill="none" stroke-width="6"/><circle cx="60" cy="85" r="5"/>'
  ),
  supplierLogo: (name: string, color: string) => {
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
      <rect width="100%" height="100%" rx="24" fill="${color}"/>
      <text x="50" y="58" font-family="Vazirmatn, sans-serif" font-size="20" font-weight="bold" fill="white" text-anchor="middle" direction="rtl">${name.slice(0, 3)}</text>
    </svg>`;
    return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
  }
};
