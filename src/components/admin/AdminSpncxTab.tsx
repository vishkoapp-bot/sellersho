import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useToast } from '../../context/ToastContext';
import { ApiError } from '../../services/apiClient';
import {
  extractSpncxErrorCode, isSpncxUnavailable, spncxAdminApi, SPNCX_UNAVAILABLE,
  type BackfillAssertion, type BackfillErrorBody, type BackfillExcessDecision, type BackfillPlan,
  type BackfillPlanDiff, type BackfillProductPlan, type BackfillRollbackProduct, type BackfillRollbackResult,
  type BackfillRunResult, type BackfillStatus, type BackfillStatusProduct, type ExcessDecision,
  type ReconciliationCheck, type ReconciliationReport, type ReconciliationTotals,
  type ReincludeReasons,
} from '../../services/spncxAdminApi';
import { UnitLookup } from '../common/UnitLookup';

const panel = 'space-y-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm';
const evidence = 'space-y-2 rounded-xl border border-slate-200 p-3';
const button = 'rounded-xl border border-slate-300 px-3 py-2 font-semibold hover:bg-slate-50 disabled:opacity-50';
const primary = 'rounded-xl bg-slate-900 px-3 py-2 font-semibold text-white disabled:opacity-50';
const input = 'rounded-xl border border-slate-300 px-3 py-2';
const table = 'w-full border-separate border-spacing-2 text-start';
const warning = 'rounded-xl bg-amber-50 p-3 text-amber-900';
const emptyInputs: Record<string, ReincludeInput> = {};

export function checkPlanTotals(plan: BackfillPlan, productIds?: readonly string[]) {
  const applicable = !productIds?.length && !plan.products.some(product => product.excluded);
  const zeroAllocCount = plan.products.flatMap(product => product.alloc ?? []).filter(row => row.alloc === 0).length;
  const { doubled_non_default, equal, other, variant_less } = plan.group_totals;
  const passed = !applicable || (plan.total_units === 4016 && doubled_non_default === 2324 &&
    equal === 260 && other === 407 && variant_less === 1025 && zeroAllocCount === 40);
  return { applicable, passed, zeroAllocCount };
}

const trimProductIds = new Set(['prod-vitamin-serum', 'prod-brush-set', 'demo-women-style-19']);

export function classifyV0Lists(plan: BackfillPlan) {
  const v0a: BackfillProductPlan[] = [];
  const v0b: Array<{ product: BackfillProductPlan; flagged: boolean; trimOrders: number; maxKeptOrders: number }> = [];
  const judgementCalls = new Set<BackfillProductPlan>();
  for (const product of plan.products) {
    const alloc = product.alloc ?? [];
    const history = product.order_history;
    const namedOrders = history.named_variant_orders ?? [];
    // Display grouping mirrors planVariantsTx. The V0a flag comes from order history.
    const namedStock = alloc.reduce((sum, row) => sum + (row.is_default ? 0 : row.variant_stock), 0);
    const doubled = product.P > 0 && alloc.some(row => row.is_default && row.variant_stock === product.P) &&
      namedStock === product.P;
    if (doubled) {
      if (history.default_ever_ordered || (history.orders > 0 && !history.named_ever_ordered)) {
        v0a.push(product);
      } else if (history.orders === 0 && !history.named_ever_ordered && namedOrders.every(row => row.orders === 0)) {
        judgementCalls.add(product);
      }
    }
    if (trimProductIds.has(product.product_id)) {
      const orderCounts = new Map(namedOrders.map(row => [row.variant_id, row.orders]));
      const trimOrders = orderCounts.get(product.trim_target ?? '') ?? 0;
      const kept = alloc.filter(row => row.alloc > 0 && row.variant_id !== product.trim_target);
      const maxKeptOrders = Math.max(0, ...kept.map(row => orderCounts.get(row.variant_id) ?? 0));
      v0b.push({ product, flagged: product.trim_target !== null && trimOrders > maxKeptOrders, trimOrders, maxKeptOrders });
      if (product.trim_target !== null && trimOrders === maxKeptOrders) judgementCalls.add(product);
    }
  }
  return { v0a, v0b, judgementCalls: [...judgementCalls] };
}

export function buildScopedProductIds(planProducts: readonly Pick<BackfillProductPlan, 'product_id'>[], vetoedIds: ReadonlySet<string>): string[] {
  return planProducts.map(product => product.product_id).filter(id => !vetoedIds.has(id)).sort();
}

export function isRunEnabled(viewedKey: string | null, runKey: string): boolean {
  return viewedKey !== null && viewedKey === runKey;
}

const errorGloss: ReadonlyMap<string, string> = new Map([
  ['backfill_already_running', 'یک اجرای دیگر در حال انجام است.'],
  ['no_excess_to_decide', 'اول یک dry-run اجرا کنید.'],
  ['dry_run_required', 'dry-run را دوباره اجرا کنید.'],
  ['plan_hash_mismatch', 'dry-run را دوباره اجرا کنید.'],
  ['plan_diff_requires_reapproval', 'برای ادامه، تفاوت طرح را بررسی و تأیید کنید.'],
  ['backfill_preflight_failed', 'پیش‌شرط‌های اجرا برقرار نیست.'],
  ['rollback_needs_printed_confirmation', 'برای ادامه، امحای برچسب‌های چاپ‌شده را تأیید کنید.'],
  ['rollback_not_completed', 'اجرای این محصول هنوز تکمیل نشده است.'],
  ['rollback_refused', 'سرور بازگشت را رد کرد.'],
  ['product_not_found', 'محصول پیدا نشد.'],
  ['order_not_found', 'سفارش پیدا نشد.'],
  ['capture_already_completed', 'ثبت مالی سفارش قبلاً تکمیل شده است.'],
  ['payment_not_retryable', 'تلاش مجدد برای این پرداخت ممکن نیست.'],
  ['product_not_excluded', 'محصول در فهرست کنارگذاشته‌شده‌ها نیست.'],
  ['reinclude_reason_required', 'دلیل ورود مجدد لازم است.'],
  ['invalid_excess_decision', 'تصمیم مازاد معتبر نیست.'],
  ['invalid_request', 'درخواست معتبر نیست.'],
]);

export function SpncxCode({ children, title }: { children: React.ReactNode; title?: string }) {
  return <code dir="ltr" className="break-all font-mono" title={title}>{children}</code>;
}

export function SpncxError({ code, fallback }: { code: string | null; fallback?: string }) {
  if (code === null && !fallback) return null;
  return <p role="alert" className="rounded-xl bg-red-50 p-3 text-red-800">
    {code !== null && <>خطا: <SpncxCode>{code}</SpncxCode> </>}
    {(code !== null && errorGloss.get(code)) || fallback || 'درخواست انجام نشد.'}
  </p>;
}

function Verdict({ passed }: { passed: boolean }) {
  return <span className={passed ? 'font-semibold text-emerald-700' : 'font-semibold text-red-800'}>
    {passed ? 'قبول شد' : 'رد شد'}
  </span>;
}

function IdList({ items }: { items: readonly string[] | null }) {
  return items?.length ? <ul className="list-inside list-disc">{items.map(id => <li key={id}><SpncxCode>{id}</SpncxCode></li>)}</ul>
    : <p>موردی ثبت نشده است.</p>;
}

function Readout({ fields }: { fields: Record<string, React.ReactNode> }) {
  return <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-2 lg:grid-cols-3">
    {Object.entries(fields).map(([label, value]) => <div key={label} className="flex flex-wrap gap-x-2">
      <dt className="text-slate-600">{label}</dt><dd>{value ?? '—'}</dd>
    </div>)}
  </dl>;
}

export function SpncxEvidenceRows({ rows }: { rows: readonly object[] | null }) {
  return rows?.length ? <ul className="space-y-2">{rows.map((row, index) =>
    <li key={index} className="rounded-lg bg-slate-50 p-2">
      <dl dir="ltr" className="grid gap-1 text-left sm:grid-cols-2">
        {Object.entries(row).map(([key, value]) => <div className="flex flex-wrap gap-x-2" key={key}>
          <dt><SpncxCode>{key}:</SpncxCode></dt><dd><SpncxCode>{typeof value === 'string' ? value : JSON.stringify(value)}</SpncxCode></dd>
        </div>)}
      </dl>
    </li>)}</ul> : <p>ردیفی ثبت نشده است.</p>;
}

const totalsLabels: Record<keyof ReconciliationTotals, string> = {
  total_units: 'کل واحدها', in_stock: 'در موجودی', removed: 'حذف‌شده',
  distinct_codes: 'کدهای یکتا', counters: 'شمارنده‌ها',
};

export function SpncxCheckResult({ check }: { check: ReconciliationCheck }) {
  const totals = 'totals' in check ? check : null;
  const vacuous = totals?.totals.total_units === 0 && totals.expected.total_units === 0;
  return <article className={evidence}>
    <h4 className="flex flex-wrap items-center gap-2">
      <SpncxCode>{check.id}</SpncxCode><SpncxCode>{check.name}</SpncxCode>
      {vacuous ? <span className={warning}>هنوز هیچ محصولی تکمیل نشده است.</span> : <Verdict passed={check.passed} />}
    </h4>
    {check.id === 'V5' && <p>این بررسی فقط تکمیل محصولات کنارگذاشته‌نشده را می‌سنجد.</p>}
    {'details' in check && check.details && <p dir="auto">{check.details}</p>}
    {'rows' in check && <details open={['V0', 'V1', 'V1b', 'V1c', 'V3', 'V6b', 'V12'].includes(check.id)}>
      <summary>ردیف‌های گزارش ({check.row_count})</summary><SpncxEvidenceRows rows={check.rows} />
    </details>}
    {'summary' in check && <StatusCounts summary={check.summary} />}
    {'sampled_products' in check && <>
      <p>محصولات نمونه شمارش</p><IdList items={check.sampled_products} />
      <p>اختلاف‌های شمارش</p><SpncxEvidenceRows rows={check.mismatches} />
    </>}
    {totals && <div className="overflow-x-auto">
      <table className={table}>
        <thead><tr><th scope="col">شاخص</th><th scope="col">ثبت‌شده</th><th scope="col">مورد انتظار</th></tr></thead>
        <tbody>{(Object.keys(totalsLabels) as Array<keyof ReconciliationTotals>).map(key => <tr key={key}>
          <th scope="row">{totalsLabels[key]}</th><td>{totals.totals[key]}</td><td>{totals.expected[key]}</td>
        </tr>)}</tbody>
      </table>
      <p>پوشش کل واحدها: {totals.expected.total_units === 0 ? '—' :
        <bdi>{(100 * totals.totals.total_units / totals.expected.total_units).toFixed(1)}٪</bdi>}</p>
    </div>}
  </article>;
}

export function SpncxCoverageSummary({ report, onRefresh, busy, errorCode, failed }: {
  report: ReconciliationReport | null; onRefresh?: () => void; busy?: boolean; errorCode?: string | null; failed?: boolean;
}) {
  const worstGaps = [...(report?.coverage_gaps ?? [])].sort((a, b) => b.shortfall - a.shortfall).slice(0, 10);
  return <section className={panel} dir="rtl">
    <header className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="font-bold">خلاصه پوشش سریال واحد</h2>
      {onRefresh && <button type="button" className={button} disabled={busy} onClick={onRefresh}>به‌روزرسانی پوشش</button>}
    </header>
    <p>حالت سخت‌گیرانه: خاموش</p>
    <p>فعال‌سازی با تغییر تنظیمات هنگام استقرار انجام می‌شود. تأیید همه بررسی‌ها و ۴۸ ساعت بدون اختلاف پیش‌شرط فعال‌سازی است.</p>
    {failed && <SpncxError code={errorCode ?? null} fallback="گزارش پوشش دریافت نشد." />}
    {busy && <p role="status">در حال دریافت گزارش پوشش…</p>}
    {report && <>
      <h3 className="font-semibold">پیش‌شرط‌های فعال‌سازی</h3>
      {report.checks.map(check => <SpncxCheckResult key={check.id} check={check} />)}
      <h3 className="font-semibold">۱۰ کسری پوشش بزرگ‌تر</h3>
      <SpncxEvidenceRows rows={worstGaps} />
    </>}
  </section>;
}

function Assertions({ assertions }: { assertions: BackfillAssertion[] }) {
  return <ul className="space-y-2">{assertions.map(assertion => <li key={assertion.name}>
    <SpncxCode>{assertion.name}</SpncxCode> <Verdict passed={assertion.passed} />
    {assertion.details && <p dir="auto">{assertion.details}</p>}
  </li>)}</ul>;
}

function PlanDiff({ diff }: { diff: BackfillPlanDiff | null }) {
  if (!diff?.changed_products.length) return <p>بدون تغییر نسبت به dry-run قبلی</p>;
  return <div className="space-y-2">
    {diff.previous_plan_hash && <p>هش طرح قبلی <SpncxCode>{diff.previous_plan_hash}</SpncxCode></p>}
    <ul className="space-y-2">{diff.changed_products.map(row => <li key={row.product_id} className={evidence}>
      <SpncxCode>{row.product_id}</SpncxCode>
      <Readout fields={{
        'تغییر تخصیص': row.alloc_changed ? 'بله' : 'خیر',
        'تغییر کسری قابل تأمین': row.top_up_changed ? 'بله' : 'خیر',
        'تغییر مازاد': row.excess_changed ? 'بله' : 'خیر',
      }} />
    </li>)}</ul>
  </div>;
}

export function SpncxRunDiff({ diff, onApprove, disabled }: {
  diff: BackfillPlanDiff; onApprove?: () => void; disabled?: boolean;
}) {
  return <section className={warning}>
    <h3 className="mb-2 font-bold">تفاوت‌های طرح برای تأیید</h3>
    <PlanDiff diff={diff} />
    {onApprove && <button type="button" className={button} disabled={disabled} onClick={onApprove}>تأیید تفاوت و اجرای مجدد</button>}
  </section>;
}

function OrderHistory({ product }: { product: BackfillProductPlan }) {
  const history = product.order_history;
  return <>
    <Readout fields={{
      'تعداد سفارش': history.orders,
      'سفارش تنوع پیش‌فرض': history.default_ever_ordered ? 'دارد' : 'ندارد',
      'سفارش تنوع نام‌دار': history.named_ever_ordered ? 'دارد' : 'ندارد',
    }} />
    <SpncxEvidenceRows rows={history.named_variant_orders} />
  </>;
}

export function SpncxV0Lists({ plan }: { plan: BackfillPlan }) {
  const { v0a, v0b, judgementCalls } = classifyV0Lists(plan);
  const flaggedCopy = 'کنار گذاشته از اجرا تا تصمیم مالک';
  return <section className={evidence}>
    <h3 className="font-semibold">شواهد سابقه سفارش <SpncxCode>V0</SpncxCode></h3>
    <h4 className="font-semibold"><SpncxCode>V0a</SpncxCode> · محصولات دوبرابر با سابقه متعارض</h4>
    {v0a.length === 0 && <p>موردی ثبت نشده است.</p>}
    {v0a.map(product => <article key={product.product_id} className={evidence}>
      <p>{product.title} · <SpncxCode>{product.product_id}</SpncxCode> · {flaggedCopy}</p>
      <OrderHistory product={product} />
    </article>)}
    <h4 className="font-semibold"><SpncxCode>V0b</SpncxCode> · سابقه تنوع هدف کاهش</h4>
    {v0b.length === 0 && <p>موردی ثبت نشده است.</p>}
    {v0b.map(({ product, flagged, trimOrders, maxKeptOrders }) => <article key={product.product_id} className={evidence}>
      <p>{product.title} · <SpncxCode>{product.product_id}</SpncxCode>{flagged && <> · {flaggedCopy}</>}</p>
      <Readout fields={{
        'هدف کاهش': product.trim_target && <SpncxCode>{product.trim_target}</SpncxCode>,
        'سفارش‌های هدف کاهش': trimOrders, 'بیشترین سفارش میان تنوع‌های باقی‌مانده': maxKeptOrders,
      }} />
      <SpncxEvidenceRows rows={product.order_history.named_variant_orders} />
    </article>)}
    <h4 className="font-semibold">موارد نیازمند تصمیم مالک</h4>
    <IdList items={judgementCalls.map(product => product.product_id)} />
  </section>;
}

function ProductPlan({ product }: { product: BackfillProductPlan }) {
  return <details className={evidence}>
    <summary className="cursor-pointer font-semibold">
      {product.title} · <SpncxCode>{product.product_id}</SpncxCode> · موجودی مبنا {product.P}
      {product.excluded && <> · کنارگذاشته‌شده <SpncxCode>{product.excluded_reason}</SpncxCode></>}
    </summary>
    <Readout fields={{
      'موجودی مبنا (P)': product.P, 'کسری قابل تأمین': product.top_up, 'مازاد': product.excess,
      'تصمیم مازاد': product.excess_decision && <SpncxCode>{product.excess_decision}</SpncxCode>,
      'هدف کاهش': product.trim_target && <SpncxCode>{product.trim_target}</SpncxCode>,
    }} />
    <div className="overflow-x-auto">
      <table className={table}>
        <caption className="text-right font-semibold">تخصیص تنوع‌ها</caption>
        <thead><tr>{['شناسه', 'SPNC', 'موجودی', 'تخصیص', 'سریال صادرشده', 'پیش‌فرض'].map(label =>
          <th key={label} scope="col">{label}</th>)}</tr></thead>
        <tbody>{product.alloc?.map(row => <tr key={row.variant_id}>
          <td><SpncxCode>{row.variant_id}</SpncxCode></td><td><SpncxCode>{row.spnc}</SpncxCode></td>
          <td>{row.variant_stock}</td><td>{row.alloc}{row.alloc === 0 && ' · تخصیص صفر'}</td>
          <td>{row.already_minted}</td><td>{row.is_default ? 'بله' : 'خیر'}</td>
        </tr>)}</tbody>
      </table>
    </div>
    {([
      ['محموله‌های جدید', product.lots_to_create], ['کاهش محموله‌ها', product.lots_to_reduce],
      ['اصلاحات موجودی تنوع', product.variant_stock_corrections], ['تنوع‌های جدید', product.new_variants],
    ] as const).map(([heading, rows]) => <div key={heading}>
      <h4 className="font-semibold">{heading}</h4><SpncxEvidenceRows rows={rows} />
    </div>)}
    <h4 className="font-semibold">سابقه سفارش</h4><OrderHistory product={product} />
    <h4 className="font-semibold">تنوع‌های تغییرکرده به رهگیری سریال</h4><IdList items={product.flipped_variants} />
  </details>;
}

export function SpncxPlanView({ plan, productIds, showVetoLists = true }: {
  plan: BackfillPlan; productIds?: readonly string[]; showVetoLists?: boolean;
}) {
  const totals = checkPlanTotals(plan, productIds);
  return <section className={panel + ' printable-receipt'}>
    <h3 className="font-bold">طرح dry-run</h3>
    <Readout fields={{
      'هش طرح': <SpncxCode title={plan.plan_hash}>{plan.plan_hash.length > 16 ? plan.plan_hash.slice(0, 16) + '…' : plan.plan_hash}</SpncxCode>,
      'مبنای طرح': <SpncxCode>{plan.basis}</SpncxCode>, 'کل واحدها': plan.total_units, 'تعداد محصولات': plan.product_count,
      'گروه دوبرابر (تنوع‌های نام‌دار)': plan.group_totals.doubled_non_default,
      'گروه برابر': plan.group_totals.equal, 'سایر': plan.group_totals.other, 'بدون تنوع': plan.group_totals.variant_less,
    }} />
    {totals.applicable && <p>انتظار طرح کامل: ۴۰ تنوع با تخصیص صفر. مقدار گزارش: {totals.zeroAllocCount}. <Verdict passed={totals.passed} /></p>}
    {!totals.passed && <p role="alert" className="rounded-xl bg-red-50 p-3 font-bold text-red-800">
      خطای کنترل طرح: مجموع گروه‌ها، کل واحدها یا تعداد تخصیص صفر با انتظار کاتالوگ کامل برابر نیست.
    </p>}
    <h4 className="font-semibold">کنترل‌های سراسری طرح</h4>
    <Assertions assertions={plan.global_assertions} />
    <h4 className="font-semibold">سفارش‌های قبل از تغییر</h4>
    <Readout fields={{
      'سفارش‌ها': plan.pre_cutover_orders.count, 'اقلام': plan.pre_cutover_orders.items,
      'رزروهای باز': plan.pre_cutover_orders.open_reservations,
    }} />
    <h4 className="font-semibold">تفاوت با dry-run قبلی</h4><PlanDiff diff={plan.diff_vs_previous_dry_run} />
    {showVetoLists && <SpncxVetoLists plan={plan} />}
    <SpncxV0Lists plan={plan} />
    <h4 className="font-semibold">جزئیات طرح هر محصول</h4>
    {plan.products.map(product => <ProductPlan key={product.product_id} product={product} />)}
  </section>;
}

export function SpncxExcessDecisionRow({ product, decision, errorCode, failed, busy, onDecide }: {
  product: BackfillProductPlan; decision?: BackfillExcessDecision; errorCode?: string | null;
  failed?: boolean; busy?: boolean; onDecide?: (decision: ExcessDecision) => void;
}) {
  const currentDecision = decision?.excess_decision ?? product.excess_decision;
  return <article className={evidence}>
    <h4 className="font-semibold">{product.title} · <SpncxCode>{product.product_id}</SpncxCode></h4>
    <Readout fields={{
      'مقدار مازاد': decision?.excess_amount ?? product.excess,
      'تصمیم': currentDecision && <SpncxCode>{currentDecision}</SpncxCode>,
      'ثبت‌کننده تصمیم': decision && <SpncxCode>{decision.excess_decided_by}</SpncxCode>,
      'زمان تصمیم': decision && <SpncxCode>{decision.excess_decided_at}</SpncxCode>,
      'مهلت بررسی': decision?.review_by && <SpncxCode>{decision.review_by}</SpncxCode>,
    }} />
    {!currentDecision && <p className={warning}>اجرای این محصول تا ثبت تصمیم مازاد متوقف است.</p>}
    {(failed || errorCode != null) && <SpncxError code={errorCode ?? null} fallback="ثبت تصمیم مازاد انجام نشد." />}
    {onDecide && <div className="flex flex-wrap gap-2">
      <button type="button" className={button} disabled={busy} onClick={() => onDecide('write_off_accepted')}>پذیرش مازاد</button>
      <button type="button" className={button} disabled={busy} onClick={() => onDecide('physical_count_ordered')}>دستور شمارش فیزیکی</button>
    </div>}
  </article>;
}

function StatusCounts({ summary }: { summary: BackfillStatus['summary'] | Omit<BackfillStatus['summary'], 'total_units_minted'> }) {
  return <Readout fields={{
    'در انتظار': summary.pending, 'تکمیل‌شده': summary.completed, 'ناموفق': summary.failed,
    'کنارگذاشته‌شده': summary.excluded,
    ...('total_units_minted' in summary && { 'کل واحدهای صادرشده': summary.total_units_minted }),
  }} />;
}

function ExcludedEvidence({ product }: { product: BackfillStatusProduct }) {
  return <Readout fields={{
    'دلیل کنارگذاری': product.excluded_reason && <SpncxCode>{product.excluded_reason}</SpncxCode>,
    'ثبت‌کننده کنارگذاری': product.excluded_by && <SpncxCode>{product.excluded_by}</SpncxCode>,
    'زمان کنارگذاری': product.excluded_at && <SpncxCode>{product.excluded_at}</SpncxCode>,
  }} />;
}

export function SpncxStatusView({ status, runPending }: { status: BackfillStatus | null; runPending?: boolean }) {
  return <section className={panel}>
    <h3 className="font-bold">پیشرفت اجرا</h3>
    {(runPending || status?.running) && <p role="status">اجرا در جریان است.</p>}
    <p>توقف در مرز محصول بعدی. اجرای دوباره از محصول تکمیل‌نشده ادامه پیدا می‌کند.</p>
    {status && <>
      <StatusCounts summary={status.summary} />
      {status.products.map(product => <article key={product.product_id} className={evidence}>
        <h4 className="font-semibold"><SpncxCode>{product.product_id}</SpncxCode> · <SpncxCode>{product.status}</SpncxCode></h4>
        <Readout fields={{
          'صادرشده / هدف': <SpncxCode>{product.units_minted} / {product.units_target}</SpncxCode>,
          'آغاز': product.started_at && <SpncxCode>{product.started_at}</SpncxCode>,
          'پایان': product.completed_at && <SpncxCode>{product.completed_at}</SpncxCode>,
          'مازاد نگه‌داشته‌شده': product.excess_held, 'مقدار مازاد': product.excess_amount,
          'تصمیم مازاد': product.excess_decision && <SpncxCode>{product.excess_decision}</SpncxCode>,
          'مهلت بررسی مازاد': product.excess_review_by && <SpncxCode>{product.excess_review_by}</SpncxCode>,
          'ثبت‌کننده تصمیم': product.excess_decided_by && <SpncxCode>{product.excess_decided_by}</SpncxCode>,
          'زمان تصمیم': product.excess_decided_at && <SpncxCode>{product.excess_decided_at}</SpncxCode>,
        }} />
        {product.error !== null && <SpncxError code={product.error} fallback="اجرای محصول ناموفق بود." />}
        {product.status === 'excluded' && <ExcludedEvidence product={product} />}
        <p>تنوع‌های تغییرکرده به رهگیری سریال</p><IdList items={product.flipped_variants} />
      </article>)}
    </>}
  </section>;
}

type ReincludeInput = { selected: boolean; reason: string };

export function SpncxVetoLists({ plan, status, vetoedIds, onVeto, reinclude = emptyInputs, onReinclude, disabled }: {
  plan: BackfillPlan | null; status?: BackfillStatus | null; vetoedIds?: ReadonlySet<string>;
  onVeto?: (id: string, checked: boolean) => void; reinclude?: Record<string, ReincludeInput>;
  onReinclude?: (id: string, value: ReincludeInput) => void; disabled?: boolean;
}) {
  const excluded = status?.products.filter(product => product.status === 'excluded') ?? [];
  return <section className={panel}>
    <h3 className="font-bold">کنارگذاری از دامنه اجرا</h3>
    {plan && <>
      {([
        ['محصولات آزمایشی و نمونه', plan.demo_test_products], ['محصولات غیرفعال', plan.inactive_products],
      ] as const).map(([label, ids]) => <div key={label} className={evidence}>
        <h4 className="font-semibold">{label} ({ids?.length ?? 0})</h4>
        {onVeto ? ids?.map(id => <label className="flex flex-wrap items-center gap-2" key={id}>
          <input type="checkbox" checked={vetoedIds?.has(id) ?? false} disabled={disabled}
            onChange={event => onVeto(id, event.target.checked)} />
          کنارگذاری <SpncxCode>{id}</SpncxCode>
        </label>) : <IdList items={ids} />}
        {onVeto && !ids?.length && <p>موردی ثبت نشده است.</p>}
      </div>)}
    </>}
    <h4 className="font-semibold">محصولات کنارگذاشته‌شده در وضعیت سرور</h4>
    {excluded.length === 0 && <p>موردی ثبت نشده است.</p>}
    {excluded.map(product => {
      const id = product.product_id;
      const current = Object.hasOwn(reinclude, id) ? reinclude[id] : { selected: false, reason: '' };
      return <article key={id} className={evidence}>
        <SpncxCode>{id}</SpncxCode><ExcludedEvidence product={product} />
        {onReinclude && <>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={current.selected} disabled={disabled}
              onChange={event => onReinclude(id, { ...current, selected: event.target.checked })} />
            ورود مجدد در اجرای بعدی
          </label>
          <label className="flex flex-wrap items-center gap-2">
            دلیل ورود مجدد
            <input className={input} value={current.reason} disabled={disabled || !current.selected}
              onChange={event => onReinclude(id, { ...current, reason: event.target.value })} />
          </label>
        </>}
      </article>;
    })}
  </section>;
}

type RollbackTarget = { productId: string } | { all: true };
const rollbackRows = (result?: BackfillRollbackResult | null): BackfillRollbackProduct[] =>
  result ? ('all' in result ? result.results : [result]) : [];

function DestructionList({ codes, onCopy }: { codes: string[]; onCopy?: (codes: string[]) => void }) {
  return codes.length ? <div className={warning}>
    <p className="font-semibold">برچسب‌های چاپ‌شده برای امحا</p>
    <IdList items={codes} />
    {onCopy && <button type="button" className={button} onClick={() => onCopy(codes)}>کپی همه</button>}
  </div> : null;
}

export function SpncxRollbackPanel({ productId = '', onProductIdChange, onRequest, target, onConfirm, onCancel,
  printedCodes = [], onConfirmPrinted, onCopy, result, errorCode, failed, busy }: {
  productId?: string; onProductIdChange?: (id: string) => void; onRequest?: (target: RollbackTarget) => void;
  target?: RollbackTarget | null; onConfirm?: () => void; onCancel?: () => void; printedCodes?: string[];
  onConfirmPrinted?: () => void; onCopy?: (codes: string[]) => void; result?: BackfillRollbackResult | null;
  errorCode?: string | null; failed?: boolean; busy?: boolean;
}) {
  return <section className={panel}>
    <h3 className="font-bold">ایمنی پیش از راه‌اندازی — نه عملیات روزمره</h3>
    <p>پس از فعال‌شدن حالت سخت‌گیرانه، عملیات فقط رو به جلو انجام می‌شود. بازگشت برای پیش از فعال‌سازی است.</p>
    {onRequest && <div className="flex flex-wrap items-end gap-2">
      <label className="grid gap-1">شناسه محصول
        <input dir="ltr" className={input + ' font-mono'} value={productId}
          disabled={busy} onChange={event => onProductIdChange?.(event.target.value)} />
      </label>
      <button type="button" className={button} disabled={busy || !productId.trim()}
        onClick={() => onRequest({ productId: productId.trim() })}>بازگشت محصول</button>
      <button type="button" className={button} disabled={busy} onClick={() => onRequest({ all: true })}>بازگشت همه محصولات</button>
    </div>}
    {target && <div className={warning}>
      <p>بازگشت {('all' in target) ? 'همه محصولات' : <SpncxCode>{target.productId}</SpncxCode>} را تأیید می‌کنید؟</p>
      <div className="mt-2 flex flex-wrap gap-2">
        {onConfirm && <button type="button" className={button} disabled={busy} onClick={onConfirm}>تأیید بازگشت</button>}
        {onCancel && <button type="button" className={button} disabled={busy} onClick={onCancel}>انصراف</button>}
      </div>
    </div>}
    {(failed || errorCode != null) && <SpncxError code={errorCode ?? null} fallback="بازگشت انجام نشد." />}
    <DestructionList codes={printedCodes} onCopy={onCopy} />
    {onConfirmPrinted && <div className="flex flex-wrap gap-2">
      <button type="button" className={button} disabled={busy} onClick={onConfirmPrinted}>تأیید امحا و بازگشت</button>
      {onCancel && <button type="button" className={button} disabled={busy} onClick={onCancel}>انصراف</button>}
    </div>}
    {result && rollbackRows(result).length === 0 && <p>محصول تکمیل‌شده‌ای برای بازگشت وجود نداشت.</p>}
    {rollbackRows(result).map(row => <article key={row.product_id} className={evidence}>
      <SpncxCode>{row.product_id}</SpncxCode>
      {row.error != null && <SpncxError code={row.error} />}
      {row.rolled_back && <>
        <p>بازگشت انجام شد. وضعیت ذخیره‌شده {row.restored_snapshot ? 'بازیابی شد' : 'بازیابی نشد'}.</p>
        <DestructionList codes={row.printed_codes_to_destroy} onCopy={onCopy} />
      </>}
    </article>)}
  </section>;
}

type SpncxFailure = { code: string | null; unavailable: boolean; body?: BackfillErrorBody };

function readFailure(cause: unknown): SpncxFailure {
  const code = extractSpncxErrorCode(cause);
  return { code, unavailable: isSpncxUnavailable(cause, code), body: cause instanceof ApiError ? cause.body : undefined };
}

type RunRequest = { ids?: string[]; batch: number; reinclude?: ReincludeReasons };
type ReviewedPlan = { plan: BackfillPlan; key: string; request: RunRequest };
type RollbackConfirmation = { target: RollbackTarget; stage: 'initial' } |
  { target: RollbackTarget; stage: 'printed'; codes: string[] };

function productSetKey(ids: readonly string[]): string {
  return ids.length ? [...ids].sort().join(',') : 'ALL';
}

function savedDecision(row?: BackfillStatusProduct): BackfillExcessDecision | undefined {
  if (!row?.excess_decision || !row.excess_decided_by || !row.excess_decided_at) return undefined;
  return {
    product_id: row.product_id, excess_amount: row.excess_amount, excess_decision: row.excess_decision,
    excess_decided_by: row.excess_decided_by, excess_decided_at: row.excess_decided_at, review_by: row.excess_review_by,
  };
}

export function AdminSpncxTab() {
  const toast = useToast();
  const [report, setReport] = useState<ReconciliationReport | null>(null);
  const [coverageBusy, setCoverageBusy] = useState(false);
  const [coverageFailure, setCoverageFailure] = useState<SpncxFailure | null>(null);
  const [status, setStatus] = useState<BackfillStatus | null>(null);
  const [statusFailure, setStatusFailure] = useState<SpncxFailure | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [productIdsText, setProductIdsText] = useState('');
  const [batchText, setBatchText] = useState('10');
  const [reviewedPlan, setReviewedPlan] = useState<ReviewedPlan | null>(null);
  const [viewedDryRunKey, setViewedDryRunKey] = useState<string | null>(null);
  const [vetoSource, setVetoSource] = useState<BackfillPlan | null>(null);
  const [vetoedIds, setVetoedIds] = useState<Set<string>>(() => new Set());
  const [reinclude, setReinclude] = useState<Record<string, ReincludeInput>>({});
  const [decisions, setDecisions] = useState<Record<string, BackfillExcessDecision>>({});
  const [excessFailures, setExcessFailures] = useState<Record<string, SpncxFailure>>({});
  const [operation, setOperation] = useState<'dry' | 'run' | 'rollback' | 'excess' | null>(null);
  const operationLock = useRef(false);
  const [dryFailure, setDryFailure] = useState<SpncxFailure | null>(null);
  const [runFailure, setRunFailure] = useState<SpncxFailure | null>(null);
  const [runInputError, setRunInputError] = useState<string | null>(null);
  const [runDiff, setRunDiff] = useState<{ diff: BackfillPlanDiff; request: RunRequest } | null>(null);
  const [runResult, setRunResult] = useState<BackfillRunResult | null>(null);
  const [monitorRun, setMonitorRun] = useState(false);
  const [rollbackId, setRollbackId] = useState('');
  const [rollbackConfirmation, setRollbackConfirmation] = useState<RollbackConfirmation | null>(null);
  const [rollbackResult, setRollbackResult] = useState<BackfillRollbackResult | null>(null);
  const [rollbackFailure, setRollbackFailure] = useState<SpncxFailure | null>(null);
  const [copyFailure, setCopyFailure] = useState<SpncxFailure | null>(null);
  const coverageRequest = useRef(0);
  const statusRequest = useRef<Promise<BackfillStatus | null> | null>(null);

  const refreshCoverage = useCallback(async () => {
    const requestId = ++coverageRequest.current;
    setCoverageBusy(true);
    try {
      const next = await spncxAdminApi.getReconciliation();
      if (requestId !== coverageRequest.current) return;
      setReport(next);
      setCoverageFailure(null);
    } catch (cause) {
      const failure = readFailure(cause);
      if (requestId !== coverageRequest.current) return;
      setCoverageFailure(failure);
      if (failure.unavailable) setUnavailable(true);
    } finally {
      if (requestId === coverageRequest.current) setCoverageBusy(false);
    }
  }, []);

  const refreshStatus = useCallback((): Promise<BackfillStatus | null> => {
    if (statusRequest.current) return statusRequest.current;
    const request = async () => {
      try {
        const next = await spncxAdminApi.getBackfillStatus();
        setStatus(next);
        setStatusFailure(null);
        if (!next.running && next.summary.pending === 0) setMonitorRun(false);
        return next;
      } catch (cause) {
        const failure = readFailure(cause);
        setStatusFailure(failure);
        if (failure.unavailable) setUnavailable(true);
        return null;
      } finally {
        statusRequest.current = null;
      }
    };
    statusRequest.current = request();
    return statusRequest.current;
  }, []);

  useEffect(() => {
    void refreshCoverage();
    void refreshStatus();
  }, [refreshCoverage, refreshStatus]);

  const polling = !unavailable && (operation === 'run' || monitorRun || status?.running === true);
  useEffect(() => {
    if (!polling) return;
    const timer = setInterval(() => { void refreshStatus(); }, 5000);
    return () => clearInterval(timer);
  }, [polling, refreshStatus]);

  const parseIds = (text: string) => [...new Set(text.split(',').map(id => id.trim()).filter(Boolean))].sort();
  const rawIds = parseIds(productIdsText);
  const ids = vetoedIds.size && vetoSource ? buildScopedProductIds(vetoSource.products, vetoedIds) : rawIds;
  const emptyVetoScope = vetoedIds.size > 0 && ids.length === 0;
  const runKey = productSetKey(ids);
  const batch = Number(batchText);
  const batchValid = Number.isInteger(batch) && batch > 0;
  const plan = reviewedPlan?.plan;
  const reviewMatches = isRunEnabled(viewedDryRunKey, runKey) && reviewedPlan?.request.batch === batch;
  const statusById = new Map(status?.products.map(product => [product.product_id, product]));
  const excessProducts = plan?.products.filter(product => product.excess > 0) ?? [];
  const decisionFor = (id: string) => {
    const local = Object.hasOwn(decisions, id) ? decisions[id] : undefined;
    const saved = savedDecision(statusById.get(id));
    if (!saved) return local;
    if (!local || Date.parse(saved.excess_decided_at) >= Date.parse(local.excess_decided_at)) return saved;
    return local;
  };
  const missingExcessDecision = excessProducts.some(product => {
    const id = product.product_id;
    const excluded = product.excluded || statusById.get(id)?.status === 'excluded';
    return (!excluded || (Object.hasOwn(reinclude, id) && reinclude[id].selected)) &&
      !decisionFor(id) && !product.excess_decision;
  });
  // Scoped runs can leave unrelated products pending in the global summary.
  const actionBusy = operation !== null || status?.running === true;
  const planningBusy = actionBusy || rollbackConfirmation !== null;
  const canRun = !unavailable && !planningBusy && !emptyVetoScope && reviewMatches && !missingExcessDecision;
  const actionsReady = !unavailable && !!(report || coverageFailure) && !!(status || statusFailure);

  const refreshStatusAfterMutation = async () => {
    // A request started before the POST returned may still reflect the old state.
    if (statusRequest.current) await statusRequest.current;
    await refreshStatus();
  };

  const clearRunReview = () => {
    setViewedDryRunKey(null);
    setRunDiff(null);
    setRunFailure(null);
    setRunInputError(null);
  };

  const updateScope = (next: string) => {
    setProductIdsText(next);
    const nextIds = parseIds(next);
    if (nextIds.length === rawIds.length && productSetKey(nextIds) === productSetKey(rawIds)) return;
    clearRunReview();
    setVetoSource(null);
    setVetoedIds(new Set());
  };

  const changeVeto = (id: string, checked: boolean) => {
    setVetoedIds(previous => {
      const next = new Set(previous);
      if (checked) next.add(id);
      else next.delete(id);
      return next;
    });
    clearRunReview();
  };

  const changeReinclude = (id: string, value: ReincludeInput) => {
    setReinclude(previous => ({ ...previous, [id]: value }));
    setRunDiff(null);
    setRunInputError(null);
  };

  const startOperation = (next: NonNullable<typeof operation>) => {
    if (operationLock.current || unavailable) return false;
    operationLock.current = true;
    setOperation(next);
    return true;
  };

  const finishOperation = () => {
    operationLock.current = false;
    setOperation(null);
  };

  const mutationFailure = (cause: unknown) => {
    const failure = readFailure(cause);
    if (failure.unavailable) setUnavailable(true);
    return failure;
  };

  const dryRun = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!actionsReady || planningBusy || !batchValid || emptyVetoScope || !startOperation('dry')) return;
    clearRunReview();
    setDryFailure(null);
    const request = { ids: ids.length ? ids : undefined, batch };
    try {
      const next = await spncxAdminApi.postBackfillDryRun(request.ids, request.batch);
      setReviewedPlan({ plan: next, key: runKey, request });
      setViewedDryRunKey(runKey);
      if (vetoedIds.size === 0) setVetoSource(next);
      setDecisions({});
      setExcessFailures({});
      toast.success('طرح dry-run آماده بررسی است.');
      await refreshStatusAfterMutation();
    } catch (cause) {
      setDryFailure(mutationFailure(cause));
    } finally {
      finishOperation();
    }
  };

  const run = async (approvedRequest?: RunRequest) => {
    if (!actionsReady || !canRun || !reviewedPlan || reviewedPlan.key !== runKey) return;
    const selected = Object.entries(reinclude).filter(([id, value]) =>
      value.selected && statusById.get(id)?.status === 'excluded');
    if (ids.length && selected.some(([id]) => !ids.includes(id))) {
      setRunInputError('محصول انتخاب‌شده برای ورود مجدد باید در دامنه همین dry-run باشد.');
      return;
    }
    if (selected.some(([, value]) => !value.reason.trim())) {
      setRunInputError('دلیل ورود مجدد برای همه محصولات انتخاب‌شده لازم است.');
      return;
    }
    if (!startOperation('run')) return;
    const request = approvedRequest ?? {
      ...reviewedPlan.request,
      reinclude: selected.length ? Object.fromEntries(selected.map(([id, value]) => [id, value.reason.trim()])) : undefined,
    };
    setRunInputError(null);
    setRunFailure(null);
    setRunDiff(null);
    setRunResult(null);
    try {
      const result = await spncxAdminApi.postBackfillRun(request.ids, request.batch, approvedRequest ? true : undefined, request.reinclude);
      setRunResult(result);
      setMonitorRun(true);
      toast.success('پاسخ اجرای سریال‌گذاری دریافت شد.');
      void refreshCoverage();
    } catch (cause) {
      const failure = mutationFailure(cause);
      setRunFailure(failure);
      if (failure.code === 'plan_diff_requires_reapproval' && failure.body?.diff) {
        setRunDiff({ diff: failure.body.diff, request });
      }
      if (failure.code === 'backfill_already_running') setMonitorRun(true);
      if (failure.code === 'plan_hash_mismatch' || failure.code === 'dry_run_required') setViewedDryRunKey(null);
    } finally {
      await refreshStatusAfterMutation();
      finishOperation();
    }
  };

  const decideExcess = async (id: string, decision: ExcessDecision) => {
    if (planningBusy || !startOperation('excess')) return;
    try {
      const saved = await spncxAdminApi.postExcessDecision(id, decision);
      setDecisions(previous => ({ ...previous, [id]: saved }));
      setExcessFailures(previous => {
        const next = { ...previous };
        delete next[id];
        return next;
      });
      setRunDiff(null);
      toast.success('تصمیم مازاد ثبت شد.');
      await refreshStatusAfterMutation();
    } catch (cause) {
      const failure = mutationFailure(cause);
      setExcessFailures(previous => ({ ...previous, [id]: failure }));
    } finally {
      finishOperation();
    }
  };

  const requestRollback = (target: RollbackTarget) => {
    setRollbackConfirmation({ target, stage: 'initial' });
    setRollbackResult(null);
    setRollbackFailure(null);
    setCopyFailure(null);
  };

  const doRollback = async (confirmation: RollbackConfirmation) => {
    if (actionBusy || !startOperation('rollback')) return;
    const { target, stage } = confirmation;
    const all = 'all' in target;
    setRollbackFailure(null);
    setRollbackConfirmation(null);
    try {
      const result = await spncxAdminApi.postBackfillRollback(all ? undefined : target.productId, all || undefined, stage === 'printed');
      const rows = rollbackRows(result);
      setRollbackResult(previous => {
        if (!('all' in result) || stage !== 'printed') return result;
        const updated = new Set(rows.map(row => row.product_id));
        return { all: true, results: [...rollbackRows(previous).filter(row => !updated.has(row.product_id)), ...rows] };
      });
      const needsConfirmation = rows.filter(row => row.error === 'rollback_needs_printed_confirmation');
      if (needsConfirmation.length) {
        setRollbackConfirmation({ target, stage: 'printed', codes: needsConfirmation.flatMap(row => row.printed_codes_to_destroy) });
      }
      if (rows.some(row => row.rolled_back)) {
        clearRunReview();
        toast.success('نتیجه بازگشت ثبت شد.');
      }
      void refreshCoverage();
      await refreshStatusAfterMutation();
    } catch (cause) {
      const failure = mutationFailure(cause);
      setRollbackFailure(failure);
      if (failure.code === 'rollback_needs_printed_confirmation') {
        setRollbackConfirmation({ target, stage: 'printed', codes: failure.body?.printed_codes_to_destroy ?? [] });
      }
    } finally {
      finishOperation();
    }
  };

  const copyPrinted = async (codes: string[]) => {
    setCopyFailure(null);
    try {
      await navigator.clipboard.writeText(codes.join('\n'));
      toast.success('کدها کپی شدند.');
    } catch (cause) {
      setCopyFailure(readFailure(cause));
    }
  };

  const unavailableFailures = [coverageFailure, statusFailure, dryFailure, runFailure, rollbackFailure, ...Object.values(excessFailures)];
  if (unavailable) return <div dir="rtl" className={panel}>
    <p role="status">{SPNCX_UNAVAILABLE}</p>
    {unavailableFailures.filter(failure => failure?.unavailable && failure.code !== null).map((failure, index) =>
      <SpncxError key={index} code={failure!.code} fallback={SPNCX_UNAVAILABLE} />)}
    <SpncxRollbackPanel />
  </div>;

  return <div dir="rtl" className="space-y-4 pb-6 text-sm text-slate-800">
    <section className={panel}>
      <h2 className="font-bold">جست‌وجوی واحد</h2>
      <UnitLookup compact />
    </section>
    <SpncxCoverageSummary report={report} onRefresh={() => void refreshCoverage()} busy={coverageBusy}
      failed={!!coverageFailure} errorCode={coverageFailure?.code} />
    <section className={panel}>
      <h2 className="font-bold">برنامه‌ریزی سریال‌گذاری</h2>
      <form onSubmit={dryRun} className="flex flex-wrap items-end gap-2">
        <label className="grid grow gap-1">شناسه محصولات (با ویرگول، خالی برای همه)
          <input dir="ltr" className={input + ' font-mono'} disabled={planningBusy} value={productIdsText}
            onChange={event => updateScope(event.target.value)} />
        </label>
        <label className="grid gap-1">اندازه دسته
          <input type="number" min="1" step="1" required dir="ltr" className={input + ' w-24'}
            disabled={planningBusy} value={batchText} onChange={event => {
              setBatchText(event.target.value);
              setRunDiff(null);
            }} />
        </label>
        <button className={primary} disabled={!actionsReady || planningBusy || !batchValid || emptyVetoScope}>
          {operation === 'dry' ? 'در حال دریافت طرح…' : 'اجرای dry-run'}
        </button>
      </form>
      {emptyVetoScope && <p role="alert" className={warning}>همه محصولات دامنه کنار گذاشته شده‌اند. برای پیش‌نمایش، دست‌کم یک محصول را نگه دارید.</p>}
      {vetoedIds.size > 0 && !emptyVetoScope && <details>
        <summary>دامنه پس از کنارگذاری ({ids.length} محصول)</summary><IdList items={ids} />
      </details>}
      {dryFailure && <SpncxError code={dryFailure.code} fallback="طرح dry-run دریافت نشد." />}
    </section>
    {reviewedPlan && <SpncxPlanView plan={reviewedPlan.plan} productIds={reviewedPlan.request.ids} showVetoLists={false} />}
    <SpncxVetoLists plan={vetoSource} status={status} vetoedIds={vetoedIds} onVeto={changeVeto}
      reinclude={reinclude} onReinclude={changeReinclude} disabled={planningBusy} />
    {!!excessProducts.length && <section className={panel}>
      <h3 className="font-bold">تصمیم‌گیری درباره مازاد</h3>
      {excessProducts.map(product => <SpncxExcessDecisionRow key={product.product_id} product={product}
        decision={decisionFor(product.product_id)} failed={Object.hasOwn(excessFailures, product.product_id)}
        errorCode={excessFailures[product.product_id]?.code} busy={planningBusy}
        onDecide={decision => void decideExcess(product.product_id, decision)} />)}
    </section>}
    <section className={panel}>
      <h2 className="font-bold">اجرای سریال‌گذاری</h2>
      {!reviewMatches && <p>ابتدا dry-run همین دامنه و اندازه دسته را اجرا و گزارش را بررسی کنید.</p>}
      {reviewMatches && missingExcessDecision && <p className={warning}>پیش از اجرا، تصمیم مازاد محصولات این دامنه را ثبت کنید.</p>}
      <button type="button" className={primary} disabled={!actionsReady || !canRun || !!runDiff} onClick={() => void run()}>
        {operation === 'run' ? 'در حال اجرا…' : 'اجرای سریال‌گذاری'}
      </button>
      {runInputError && <p role="alert" className={warning}>{runInputError}</p>}
      {runFailure && <SpncxError code={runFailure.code} fallback="درخواست اجرا انجام نشد." />}
      {runFailure?.body?.global_assertions && <Assertions assertions={runFailure.body.global_assertions} />}
      {runDiff && <SpncxRunDiff diff={runDiff.diff} disabled={!canRun} onApprove={() => void run(runDiff.request)} />}
      {runResult && <Readout fields={{
        'هش اجرا': <SpncxCode>{runResult.plan_hash}</SpncxCode>, 'تکمیل‌شده در این اجرا': runResult.completed,
        'قبلاً تکمیل‌شده': runResult.skipped_completed, 'ناموفق': runResult.failed,
        'عبور از کنارگذاشته‌شده‌ها': runResult.excluded_skipped, 'واحدهای صادرشده در این اجرا': runResult.total_minted,
      }} />}
    </section>
    {statusFailure && <SpncxError code={statusFailure.code} fallback="وضعیت اجرای سریال‌گذاری دریافت نشد." />}
    <SpncxStatusView status={status} runPending={operation === 'run'} />
    <SpncxRollbackPanel productId={rollbackId} onProductIdChange={setRollbackId}
      onRequest={actionsReady && !rollbackConfirmation ? requestRollback : undefined} busy={actionBusy}
      target={rollbackConfirmation?.stage === 'initial' ? rollbackConfirmation.target : null}
      onConfirm={rollbackConfirmation ? () => void doRollback(rollbackConfirmation) : undefined}
      onCancel={() => setRollbackConfirmation(null)}
      printedCodes={rollbackConfirmation?.stage === 'printed' ? rollbackConfirmation.codes : undefined}
      onConfirmPrinted={rollbackConfirmation?.stage === 'printed' ? () => void doRollback(rollbackConfirmation) : undefined}
      onCopy={codes => void copyPrinted(codes)} result={rollbackResult}
      failed={!!rollbackFailure} errorCode={rollbackFailure?.code} />
    {copyFailure && <SpncxError code={copyFailure.code} fallback="کپی کدها انجام نشد." />}
  </div>;
}
