// Matches backend/internal/pricing/money.go. Only new price computations use
// this policy; historical order snapshots and exact refunds are not repriced.
export const MONEY_POLICY_VERSION = 'toman-bps-nearest100-floor-v1';
const denominator = 10000n;
const maxMoney = BigInt(Number.MAX_SAFE_INTEGER);

const money = (value: number): bigint => {
  if (!Number.isSafeInteger(value) || value < 0) throw new RangeError('Invalid integer toman amount');
  return BigInt(value);
};
const output = (value: bigint): number => {
  if (value < 0n || value > maxMoney) throw new RangeError('Toman amount exceeds supported bounds');
  return Number(value);
};
export const parsePercent = (value: number): bigint => {
  if (!Number.isFinite(value) || value < 0 || value > 1000) throw new RangeError('Invalid percentage');
  const text = String(value);
  if (!/^\d+(?:\.\d{1,2})?$/.test(text)) throw new RangeError('Percentage supports at most two decimal places');
  const [whole, fractional = ''] = text.split('.');
  return BigInt(whole) * 100n + BigInt(fractional.padEnd(2, '0'));
};
const roundedRatio = (numerator: bigint, divisor: bigint): bigint => {
  const step = divisor * 100n;
  return ((numerator + step / 2n) / step) * 100n;
};
export const roundMoneyTo100 = (amount: number): number => output(roundedRatio(money(amount), 1n));
export const markedUpPrice = (amount: number, percent: number): number => {
  const cost = money(amount);
  const rounded = roundedRatio(cost * (denominator + parsePercent(percent)), denominator);
  return output(rounded < cost ? cost : rounded);
};
export const truncatedPercent = (amount: number, percent: number): number =>
  output(money(amount) * parsePercent(percent) / denominator);
