import test from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { readSheet } from 'read-excel-file/node';
import { canRunVariantEdit, parseVariantCSV, parseVariantWorkbookRows, previewVariantEdits, runVariantEdits, VariantEditConflict, writeVariantCSV } from './variantWorkspace';

const supplierRows = [
  { id: 'sku-1', stock: 7, cost: 1000 },
  { id: 'sku-2', stock: 4, cost: null },
];

test('reads Excel CSV with BOM, quotes, localized numbers and precise row failures', () => {
  const edits = parseVariantCSV('\uFEFFvariant_id,stock,base_cost,reason\r\nsku-1,۸,۱٬۵۰۰,"recount, shelf 2"\r\nsku-2,3,,\r\nforeign,2,,count\r\nsku-2,,2000,', 'supplier');
  const result = previewVariantEdits(edits, supplierRows, 'supplier');
  assert.deepEqual(result.map(row => [row.line, row.edit.stock, row.edit.cost, row.edit.reason, !!row.error]), [
    [2, 8, 1500, 'recount, shelf 2', false],
    [3, 3, undefined, undefined, true],
    [4, 2, undefined, 'count', true],
    [5, undefined, 2000, undefined, true],
  ]);
});

test('checks seller margin bounds and locked margins without blocking visibility', () => {
  const sellerRows = [{ id: 'sku-1', visible: true, margin: null, minimumMargin: 5, maximumMargin: 12, marginLocked: true }];
  const margin = previewVariantEdits(parseVariantCSV('variant_id,visibility,margin_pct\nsku-1,,10', 'seller'), sellerRows, 'seller');
  assert.ok(margin[0].error);
  const visibility = previewVariantEdits(parseVariantCSV('variant_id,visibility,margin_pct\nsku-1,hidden,', 'seller'), sellerRows, 'seller');
  assert.equal(visibility[0].error, undefined);
  assert.equal(visibility[0].edit.visible, false);
});

test('previews a real first-sheet Excel file with typed cells and preserves errors for foreign rows', async () => {
  const fixture = fileURLToPath(new URL('../../scripts/fixtures/catalog/variant-bulk-upload.xlsx', import.meta.url));
  const rows = await readSheet(fixture, 1);
  const edits = parseVariantWorkbookRows(rows, 'seller');
  const preview = previewVariantEdits(edits, [{ id: 'drp33-variant', visible: false, margin: null }], 'seller');
  assert.deepEqual(preview.map(row => [row.line, row.edit, !!row.error]), [
    [2, { visible: true, margin: 15 }, false],
    [3, { visible: false }, true],
  ]);
});

test('writes a spreadsheet template without a formula-capable first column', () => {
  assert.equal(writeVariantCSV('seller', ['sku-1']), '\uFEFFvariant_id,visibility,margin_pct\r\nsku-1,,\r\n');
  assert.throws(() => writeVariantCSV('seller', ['=1+1']));
});

test('leaves successful operations intact when another operation fails and reuses the retry key', async () => {
  const preview = previewVariantEdits(parseVariantCSV('variant_id,stock,base_cost,reason\nsku-1,8,1500,count', 'supplier'), supplierRows, 'supplier');
  const calls: string[] = [];
  const save = async (_row: typeof preview[number], field: string, attempt: string) => {
    calls.push(field + attempt);
    if (field === 'cost' && calls.length === 2) throw new Error('try again');
    return { message: field };
  };
  const once = await runVariantEdits(preview, 'batch-id', save);
  assert.equal(once[0].outcomes?.stock?.state, 'saved');
  assert.equal(once[0].outcomes?.cost?.state, 'error');
  const retry = await runVariantEdits(once, 'batch-id', save);
  assert.equal(retry[0].outcomes?.cost?.state, 'saved');
  assert.equal(calls.length, 3);
  assert.equal(calls[1], calls[2]);
});

test('marks stale rows as requiring a new preview and still applies later rows', async () => {
  const edits = parseVariantCSV('variant_id,stock,base_cost,reason\nsku-1,9,,count\nsku-2,6,,count', 'supplier');
  const result = await runVariantEdits(previewVariantEdits(edits, supplierRows, 'supplier'), 'batch', async row => {
    if (row.id === 'sku-1') throw new VariantEditConflict('refresh the preview');
    return { message: 'done' };
  });
  assert.equal(result[0].outcomes?.stock?.state, 'conflict');
  assert.equal(canRunVariantEdit(result[0]), false);
  assert.equal(result[1].outcomes?.stock?.state, 'saved');
});
