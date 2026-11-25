import { apiRequest } from './apiClient';

export interface StatementRow {
  id: string;
  date: string;
  amount: number;
  label: string;
  reference: string;
  kind: string;
  status?: string;
}

export interface StatementPage {
  month: string;
  credits: number;
  debits: number;
  available: number;
  opening?: number;
  closing?: number;
  openingUnverified?: boolean;
  undatedCount?: number;
  totalEntries: number;
  entries: StatementRow[];
  nextCursor?: string;
}

const supplierLabels: Record<string, string> = {
  order: 'درآمد سفارش تحویل‌شده', payout: 'درخواست برداشت', storage: 'هزینهٔ انبارداری',
  adjustment: 'تعدیل حساب', return_shipping: 'هزینهٔ ارسال مرجوعی', return_clawback: 'برگشت مبلغ پایهٔ کالا',
  return_event: 'وجه عودت از انبار',
};

export async function getPanelStatement(source: { role: 'seller'; slug: string } | { role: 'supplier' }, month: string, cursor: string, signal?: AbortSignal, limit = 50): Promise<StatementPage> {
  const params = new URLSearchParams({ month, limit: String(limit) });
  if (cursor) params.set('cursor', cursor);
  if (source.role === 'seller') {
    const page = await apiRequest<{
      month: string; totalCredit: number; totalDebit: number; currentAvailable: number;
      openingAvailable: number; closingAvailable: number; openingVerified: boolean;
      totalEntries: number; nextCursor?: string;
      entries: { id: string; date: string; type: string; amount: number; reason: string; reasonKey: string; reference: string }[];
    }>(`/stores/${encodeURIComponent(source.slug)}/statement?${params}`, { auth: true, signal });
    return { month: page.month, credits: page.totalCredit, debits: page.totalDebit, available: page.currentAvailable,
      opening: page.openingAvailable, closing: page.closingAvailable, openingUnverified: page.openingAvailable !== 0 && !page.openingVerified,
      totalEntries: page.totalEntries, nextCursor: page.nextCursor,
      entries: page.entries.map(row => ({ id: row.id, date: row.date, amount: row.type === 'debit' ? -row.amount : row.amount, label: row.reason, kind: row.reasonKey, reference: row.reference })),
    };
  }
  const page = await apiRequest<{
    month: string; credits: number; debits: number; balance: { available: number }; undatedCount: number;
    totalEntries: number; entries: Omit<StatementRow, 'label'>[]; nextCursor?: string;
  }>(`/supplier/settlement/statement?${params}`, { auth: true, signal });
  return { ...page, available: page.balance.available,
    entries: page.entries.map(row => ({ ...row, label: supplierLabels[row.kind] ?? 'گردش حساب' })),
  };
}
