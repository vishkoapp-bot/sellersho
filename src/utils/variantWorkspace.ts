export type VariantWorkspaceRole = 'supplier' | 'seller';
export interface VariantEdit {
  stock?: number;
  cost?: number | null;
  visible?: boolean;
  margin?: number | null;
  reason?: string;
}
export type VariantField = Exclude<keyof VariantEdit, 'reason'>;
export interface VariantBaseline extends VariantEdit {
  id: string;
  productCost?: number;
  canEditStock?: boolean;
  canEditCost?: boolean;
  minimumMargin?: number;
  maximumMargin?: number;
  marginLocked?: boolean;
  canEditSeller?: boolean;
}
interface VariantEditInput {
  line: number;
  id: string;
  edit: VariantEdit;
  error?: string;
}
export interface VariantEditOutcome {
  state: 'saved' | 'pending' | 'error' | 'conflict';
  message: string;
  retired?: string[];
}
export class VariantEditConflict extends Error {}
export interface VariantEditPreview extends VariantEditInput {
  current?: VariantBaseline;
  outcomes?: Partial<Record<VariantField, VariantEditOutcome>>;
}

export const VARIANT_BATCH_LIMIT = 100;
const headers = {
  supplier: ['variant_id', 'stock', 'base_cost', 'reason'],
  seller: ['variant_id', 'visibility', 'margin_pct'],
};
const editFields: VariantField[] = ['stock', 'cost', 'visible', 'margin'];
type CellRow = { line: number; cells: string[] };

export function variantNumericInput(value: string): string {
  const normalized = value.replace(/[۰-۹]/g, digit => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(digit)))
    .replace(/[٠-٩]/g, digit => String('٠١٢٣٤٥٦٧٨٩'.indexOf(digit)))
    .replace(/٫/g, '.').trim();
  return /^\d{1,3}(?:[,٬]\d{3})+$/.test(normalized) ? normalized.replace(/[,٬]/g, '') : normalized;
}

function csvRecords(text: string) {
  const records: CellRow[] = [];
  const delimiter = text.split(/\r?\n/, 1)[0].includes('\t') ? '\t' : ',';
  let cells: string[] = [];
  let cell = '';
  let line = 1;
  let recordLine = 1;
  let quoted = false;
  let closed = false;
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (quoted) {
      if (char === '"' && text[i + 1] === '"') { cell += '"'; i++; continue; }
      if (char === '"') { quoted = false; closed = true; continue; }
      if (char === '\n' || (char === '\r' && text[i + 1] !== '\n')) line++;
      cell += char;
      continue;
    }
    if (char === delimiter || char === '\n' || char === '\r') {
      cells.push(cell);
      cell = '';
      closed = false;
      if (char === delimiter) continue;
      if (char === '\r' && text[i + 1] === '\n') i++;
      records.push({ line: recordLine, cells });
      cells = [];
      recordLine = ++line;
      continue;
    }
    if (closed || (char === '"' && cell)) throw new Error('ساختار فایل CSV در سطر ' + line + ' معتبر نیست.');
    if (char === '"') { quoted = true; continue; }
    cell += char;
  }
  if (quoted) throw new Error('علامت نقل‌قول در سطر ' + recordLine + ' بسته نشده است.');
  if (cell || cells.length || closed) records.push({ line: recordLine, cells: [...cells, cell] });
  return records.filter(row => row.cells.some(value => value.trim()));
}

export function parseVariantEdit(input: Partial<Record<'stock' | 'base_cost' | 'visibility' | 'margin_pct' | 'reason', string>>): VariantEdit {
  const edit: VariantEdit = {};
  for (const [header, field, label] of [
    ['stock', 'stock', 'موجودی'],
    ['base_cost', 'cost', 'قیمت عمده'],
    ['margin_pct', 'margin', 'درصد سود'],
  ] as const) {
    const raw = variantNumericInput(input[header] ?? '');
    if (!raw) continue;
    if (raw.toLowerCase() === 'inherit' && field !== 'stock') { edit[field] = null; continue; }
    const number = Number(raw);
    const valid = field === 'margin'
      ? /^\d+(\.\d{1,2})?$/.test(raw) && number <= 100
      : /^\d+$/.test(raw) && Number.isSafeInteger(number)
        && number >= (field === 'stock' ? 0 : 1) && (field !== 'stock' || number <= 2147483647);
    if (!valid) throw new Error(label + ' معتبر نیست.');
    edit[field] = number;
  }
  const visibility = input.visibility?.trim().toLowerCase();
  if (visibility) {
    if (visibility !== 'visible' && visibility !== 'hidden') throw new Error('نمایش باید visible یا hidden باشد.');
    edit.visible = visibility === 'visible';
  }
  const reason = input.reason?.trim();
  if (reason) edit.reason = reason;
  return edit;
}

export function parseVariantCSV(raw: string, role: VariantWorkspaceRole): VariantEditInput[] {
  if (raw.length > 512000) throw new Error('حجم فایل بیشتر از حد مجاز است.');
  return parseVariantRows(csvRecords(raw.replace(/^\uFEFF/, '')), role);
}

export function parseVariantWorkbookRows(sheet: readonly (readonly unknown[])[], role: VariantWorkspaceRole): VariantEditInput[] {
  const records = sheet.map((values, index) => ({
    line: index + 1,
    cells: values.map(value => value == null ? '' : String(value)),
  })).filter(row => row.cells.some(value => value.trim()));
  const header = records[0]?.cells;
  if (header) {
    while (header.length && !header.at(-1)?.trim()) header.pop();
    for (const { cells } of records.slice(1)) {
      while (cells.length > header.length && !cells.at(-1)?.trim()) cells.pop();
      while (cells.length < header.length) cells.push('');
    }
  }
  return parseVariantRows(records, role);
}

function parseVariantRows(records: CellRow[], role: VariantWorkspaceRole): VariantEditInput[] {
  const [header, ...rows] = records;
  const names = header?.cells.map(value => value.trim().toLowerCase()) ?? [];
  if (!names.includes('variant_id') || new Set(names).size !== names.length || names.some(name => !headers[role].includes(name))) {
    throw new Error('عنوان ستون‌های فایل معتبر نیست. از فایل الگو استفاده کنید.');
  }
  if (!rows.length) throw new Error('فایل هیچ سطر تغییری ندارد.');
  if (rows.length > VARIANT_BATCH_LIMIT) throw new Error('حداکثر ۱۰۰ سطر در هر فایل پذیرفته می‌شود.');
  return rows.map(({ line, cells }) => {
    const input = Object.fromEntries(names.map((name, index) => [name, cells[index]?.trim()]));
    const id = input.variant_id ?? '';
    try {
      if (cells.length !== names.length) throw new Error('تعداد ستون‌های سطر با عنوان فایل یکسان نیست.');
      return { line, id, edit: parseVariantEdit(input) };
    } catch (cause) {
      return { line, id, edit: {}, error: (cause as Error).message };
    }
  });
}

export function writeVariantCSV(role: VariantWorkspaceRole, ids: string[]): string {
  const columns = headers[role];
  const lines = ids.map(id => {
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(id)) throw new Error('شناسه تنوع معتبر نیست.');
    return id + ','.repeat(columns.length - 1);
  });
  return '\uFEFF' + [columns.join(','), ...lines].join('\r\n') + '\r\n';
}

export function previewVariantEdits(inputs: VariantEditInput[], rows: VariantBaseline[], role: VariantWorkspaceRole): VariantEditPreview[] {
  const entries = new Map(rows.map(row => [row.id, row]));
  const frequencies = new Map<string, number>();
  for (const input of inputs) frequencies.set(input.id, (frequencies.get(input.id) ?? 0) + 1);
  return inputs.map(input => {
    const current = entries.get(input.id);
    const { edit } = input;
    let error = input.error;
    if (!error) {
      if (!current) error = 'تنوع در این پنل پیدا نشد.';
      else if ((frequencies.get(input.id) ?? 0) > 1) error = 'این شناسه در فایل تکراری است.';
      else if (!editFields.some(field => edit[field] !== undefined)) error = 'برای این تنوع تغییری وارد نشده است.';
      else if (role === 'supplier') {
        if ((edit.stock !== undefined && current.canEditStock === false) || (edit.cost !== undefined && current.canEditCost === false)) {
          error = 'مجوز یا شرایط ویرایش این تنوع فراهم نیست.';
        } else if (edit.stock !== undefined && (!edit.reason || edit.reason.length > 500)) error = 'علت اصلاح موجودی را در حداکثر ۵۰۰ نویسه وارد کنید.';
      } else if (current.canEditSeller === false) error = 'این تنوع در فروشگاه قابل ویرایش نیست.';
      else if (edit.margin !== undefined) {
        if (current.marginLocked) error = 'درصد سود این تنوع قفل است.';
        else if (edit.margin !== null && (edit.margin < (current.minimumMargin ?? 0) || edit.margin > (current.maximumMargin ?? 100))) {
          error = 'درصد سود خارج از بازه مجاز این کالا است.';
        }
      }
    }
    return { ...input, current, error };
  });
}

export function canRunVariantEdit(row: VariantEditPreview): boolean {
  return !row.error && editFields.some(field =>
    row.edit[field] !== undefined && (row.outcomes?.[field] == null || row.outcomes[field]?.state === 'error'));
}

export async function runVariantEdits(
  rows: VariantEditPreview[],
  batch: string,
  save: (row: VariantEditPreview, field: VariantField, attempt: string) => Promise<Omit<VariantEditOutcome, 'state'> & { state?: 'saved' | 'pending' }>,
): Promise<VariantEditPreview[]> {
  const result: VariantEditPreview[] = [];
  for (const row of rows) {
    if (!canRunVariantEdit(row)) { result.push(row); continue; }
    const outcomes = { ...row.outcomes };
    for (const field of editFields) {
      if (row.edit[field] === undefined || (outcomes[field] && outcomes[field]?.state !== 'error')) continue;
      if (!outcomes[field] && row.edit[field] === row.current?.[field]) {
        outcomes[field] = { state: 'saved', message: 'بدون تغییر' };
        continue;
      }
      try {
        const saved = await save(row, field, batch + '-' + row.line + '-' + field);
        outcomes[field] = { state: 'saved', ...saved };
      } catch (cause) {
        outcomes[field] = {
          state: cause instanceof VariantEditConflict ? 'conflict' : 'error',
          message: cause instanceof Error ? cause.message : 'ذخیره انجام نشد.',
        };
      }
    }
    result.push({ ...row, outcomes });
  }
  return result;
}
