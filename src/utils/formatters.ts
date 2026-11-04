export const toPersianDigits = (strOrNum: string | number): string => {
  const farsiDigits = ['۰', '۱', '۲', '۳', '۴', '۵', '۶', '۷', '۸', '۹'];
  return String(strOrNum).replace(/\d/g, (x) => farsiDigits[parseInt(x, 10)]);
};

export const formatPrice = (price: number): string => {
  const formatted = Math.round(price).toLocaleString('en-US');
  // FIX 22: live renders the Persian thousands separator U+066C (٬),
  // not the Latin comma from en-US grouping (DOM-proven on PDP both
  // variants + homepage deals band). Digits stay Persian via
  // toPersianDigits; only the grouping separator changes. All surfaces
  // share this formatter (catalog, deals, cart, panels), matching live.
  return toPersianDigits(formatted).replace(/,/g, '٬');
};
