export const UNIT_STATUS_LABELS = {
  defined: 'تعریف‌شده (در انتظار جانمایی)',
  in_stock: 'موجود در انبار',
  reserved: 'رزرو شده',
  sold: 'فروخته‌شده',
  returned: 'برگشتی (در انتظار QC)',
  quarantined: 'قرنطینه',
  damaged: 'آسیب‌دیده',
  removed: 'خارج‌شده از انبار',
} as const;

export type UnitStatus = keyof typeof UNIT_STATUS_LABELS;

export const UNIT_STATUS_TONES = {
  defined: 'slate',
  in_stock: 'green',
  reserved: 'amber',
  sold: 'blue',
  returned: 'orange',
  quarantined: 'red',
  damaged: 'red',
  removed: 'grey',
} as const satisfies Record<UnitStatus, string>;

export const UNIT_EVENT_LABELS: Readonly<Partial<Record<string, string>>> = {
  minted: 'صدور',
  printed: 'چاپ',
  reprinted: 'چاپ مجدد',
  sold: 'فروش',
  restored: 'بازگشت به انبار',
  retired_by_adjustment: 'حذف با اصلاح موجودی',
};

export function unitEventLabel(eventType: string): string {
  return Object.hasOwn(UNIT_EVENT_LABELS, eventType) ? UNIT_EVENT_LABELS[eventType] ?? eventType : eventType;
}
