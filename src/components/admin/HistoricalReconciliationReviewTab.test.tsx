import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  CLASSIFICATION_LABELS,
  MEASUREMENT_LABELS,
  HistoricalReconciliationRowCard,
  deriveVerdict,
  differenceGloss,
  formatSignedDifference,
  nextStepForRow,
} from './HistoricalReconciliationReviewTab';
import type { HistoricalInventoryReportRow } from '../../services/adminApi';

// The tab is read-only and its backend suggestedAction is one constant
// English string for every row ("manual review; no automatic mutation"),
// so the component must never render it — the verdict, the Persian labels
// and the derived next step replace it. These tests pin that, plus the
// verdict derivation and the label-map coverage for every code the
// backend can emit (classifyHistoricalRow + measurementClassification).

const row = (overrides: Partial<HistoricalInventoryReportRow> = {}): HistoricalInventoryReportRow => ({
  productId: 'prod-1',
  productTitle: 'تیشرت زنانه',
  supplierId: 'sup-1',
  legacyStock: 10,
  canonicalQuantity: 8,
  canonicalSellableQuantity: 7,
  physicalQuantity: 8,
  canonicalRows: 5,
  unreconciledRows: 2,
  movementQuantity: 3,
  measurementStatus: 'verified',
  hasMeasurement: true,
  hasInboundEvidence: true,
  hasQCEvidence: false,
  hasPutawayEvidence: true,
  hasLedgerEvidence: false,
  classifications: ['QUANTITY_MISMATCH', 'EVIDENCE_INSUFFICIENT'],
  classification: 'QUANTITY_MISMATCH',
  evidenceInsufficient: true,
  unresolved: true,
  suggestedAction: 'manual review; no automatic mutation',
  difference: 3,
  physicalInventory: true,
  measurementClassification: 'VERIFIED',
  locationBreakdown: [
    { locationKey: 'wh-1', locationType: 'warehouse', warehouseName: 'انبار مرکزی', status: 'sellable', quantity: 7, reconciled: true },
    { locationKey: 'wh-1', locationType: 'warehouse', warehouseName: 'انبار مرکزی', status: 'quarantine', quantity: 1, reconciled: false },
  ],
  activeReservations: [
    { id: 'res-1', locationKey: 'wh-1', quantity: 1, referenceId: 'ord-9', status: 'active', idempotencyKey: 'k-1' },
  ],
  movements: [
    { id: 'mov-1', quantity: 2, fromStatus: 'quarantine', toStatus: 'sellable', referenceType: 'qc_pass', referenceId: 'qc-1', idempotencyKey: 'k-2', createdAt: '2026-11-01T10:00:00Z' },
  ],
  measurementEvidence: [
    { id: 'me-1', newLengthMm: 700, newWidthMm: 500, newHeightMm: 20, newWeightGrams: 300, reason: 'ثبت اولیه', actorUserId: 'u-1', createdAt: '2026-11-01T09:00:00Z' },
  ],
  evidenceIds: ['mov-1', 'me-1'],
  reviewStatus: 'needs_review',
  ...overrides,
});

describe('deriveVerdict', () => {
  it('MATCHED row with zero difference reads مطابقت دارد', () => {
    assert.equal(deriveVerdict({ difference: 0, classifications: ['MATCHED'] }).label, 'مطابقت دارد');
    assert.equal(deriveVerdict({ difference: 0, classifications: ['MATCHED'] }).key, 'matched');
  });

  it('positive difference (old number higher) reads کسری', () => {
    const verdict = deriveVerdict({ difference: 3, classifications: ['QUANTITY_MISMATCH'] });
    assert.equal(verdict.label, 'کسری');
    assert.equal(verdict.key, 'shortage');
  });

  it('negative difference (ledger higher) reads مازاد', () => {
    const verdict = deriveVerdict({ difference: -2, classifications: ['QUANTITY_MISMATCH'] });
    assert.equal(verdict.label, 'مازاد');
    assert.equal(verdict.key, 'surplus');
  });
});

describe('differenceGloss', () => {
  it('explains a positive difference in plain Persian', () => {
    assert.equal(differenceGloss({ difference: 3 }), 'عدد قدیمی ۳ عدد بیشتر نشان می‌دهد');
  });

  it('explains a negative difference in plain Persian', () => {
    assert.equal(differenceGloss({ difference: -2 }), 'دفتر جدید ۲ عدد بیشتر نشان می‌دهد');
  });

  it('explains a zero difference', () => {
    assert.equal(differenceGloss({ difference: 0 }), 'عدد قدیمی و قابل‌فروشِ دفتر جدید می‌خوانند');
  });

  it('signs the difference with +/−', () => {
    assert.equal(formatSignedDifference(3), '+۳');
    assert.equal(formatSignedDifference(-2), '−۲');
    assert.equal(formatSignedDifference(0), '۰');
  });
});

describe('label-map coverage', () => {
  it('every classification code the backend can emit has a Persian label', () => {
    const backendCodes = [
      'MATCHED',
      'LEGACY_ONLY',
      'CANONICAL_ONLY',
      'QUANTITY_MISMATCH',
      'EVIDENCE_INSUFFICIENT',
      'MEASUREMENT_MISSING',
      'CATALOG_ONLY_MEASUREMENT_MISSING',
      'MEASUREMENT_UNVERIFIED',
      'PHYSICAL_PRESENCE_UNCONFIRMED',
    ];
    for (const code of backendCodes) {
      assert.ok(CLASSIFICATION_LABELS[code], `missing Persian label for ${code}`);
      assert.notEqual(CLASSIFICATION_LABELS[code], code);
    }
  });

  it('every measurement classification value has a Persian label', () => {
    const backendValues = [
      'VERIFIED',
      'PHYSICAL_MEASUREMENT_MISSING',
      'MEASUREMENT_UNVERIFIED',
      'PHYSICAL_PRESENCE_UNCONFIRMED',
      'CATALOG_ONLY_MEASUREMENT_MISSING',
    ];
    for (const value of backendValues) {
      assert.ok(MEASUREMENT_LABELS[value], `missing Persian label for ${value}`);
      assert.notEqual(MEASUREMENT_LABELS[value], value);
    }
  });

  it('VERIFIED is labelled as "all four filled in", not human-verified', () => {
    assert.match(MEASUREMENT_LABELS.VERIFIED, /هر چهار اندازه/);
    assert.doesNotMatch(MEASUREMENT_LABELS.VERIFIED, /تأیید/);
  });
});

describe('nextStepForRow', () => {
  it('derives a measurement next step from MEASUREMENT_MISSING', () => {
    assert.match(nextStepForRow({ classifications: ['MEASUREMENT_MISSING'] }), /اندازه و وزن/);
  });

  it('derives an unreconciled-rows next step from EVIDENCE_INSUFFICIENT', () => {
    assert.match(nextStepForRow({ classifications: ['EVIDENCE_INSUFFICIENT'] }), /تطبیق‌نیافته/);
  });

  it('a matched row needs no action beyond recording the decision', () => {
    assert.match(nextStepForRow({ classifications: ['MATCHED'] }), /نیازی به اقدام نیست/);
  });
});

describe('HistoricalReconciliationRowCard rendering', () => {
  it('never renders the raw English suggestedAction or any raw code', () => {
    const html = renderToStaticMarkup(
      <HistoricalReconciliationRowCard row={row()} onReview={() => undefined} />,
    );
    for (const banned of [
      'manual review',
      'legacy',
      'canonical',
      'mutation',
      'reconciled',
      'unreconciled',
      'evidenceIds',
      'LEGACY_ONLY',
      'CANONICAL_ONLY',
      'QUANTITY_MISMATCH',
      'EVIDENCE_INSUFFICIENT',
      'MEASUREMENT_MISSING',
      'VERIFIED',
      'MEASUREMENT_UNVERIFIED',
    ]) {
      assert.doesNotMatch(html, new RegExp(banned), `rendered output leaks raw string: ${banned}`);
    }
  });

  it('shows the Persian numbers, verdict, standing instruction and evidence flags', () => {
    const html = renderToStaticMarkup(
      <HistoricalReconciliationRowCard row={row()} onReview={() => undefined} />,
    );
    assert.match(html, /موجودی قدیمی/);
    assert.match(html, /موجودی جدید \(کل\)/);
    assert.match(html, /قابل‌فروش/);
    assert.match(html, /موجودی فیزیکی در انبار/);
    assert.match(html, /کسری/);
    assert.match(html, /عدد قدیمی ۳ عدد بیشتر نشان می‌دهد/);
    assert.match(html, /عدد قدیمی با قابل‌فروشِ دفتر جدید نمی‌خواند/);
    assert.match(html, /بعضی ردیف‌های دفتر تطبیق‌نیافته‌اند/);
    assert.match(html, /۲ ردیف از ۵ ردیف تطبیق‌نیافته‌اند/);
    assert.match(html, /بررسی دستی؛ هیچ اصلاح خودکاری انجام نمی‌شود/);
    assert.match(html, /مدرک ورود: دارد/);
    assert.match(html, /مدرک کنترل کیفیت: ندارد/);
    assert.match(html, /مدرک مالی: ندارد/);
  });

  it('a matched row renders مطابقت دارد with the matched explanation', () => {
    const html = renderToStaticMarkup(
      <HistoricalReconciliationRowCard
        row={row({
          legacyStock: 7,
          canonicalQuantity: 7,
          canonicalSellableQuantity: 7,
          physicalQuantity: 7,
          canonicalRows: 2,
          unreconciledRows: 0,
          classifications: ['MATCHED'],
          classification: 'MATCHED',
          evidenceInsufficient: false,
          unresolved: false,
          difference: 0,
        })}
        onReview={() => undefined}
      />,
    );
    assert.match(html, /مطابقت دارد/);
    assert.match(html, /همه‌چیز می‌خواند/);
    assert.doesNotMatch(html, /تطبیق‌نیافته‌اند/);
  });
});
