import './composed.provider';

const params = new URLSearchParams(location.search);
const failFirst = params.has('fail-first');
const fetchFixture = window.fetch.bind(window);
const attempts = new Map<string, number>();
const longReference = 'FIXTURE-REFERENCE-' + '7'.repeat(90);
const pendingTransfers: Array<{ id: string; reference: string }> = [];
const storageRuns: number[] = [];
const smsWrites: unknown[] = [];
let finishTransfer: (() => void) | undefined;
let storefrontApprovalPending = true;
Object.assign(window, { adminMobileFixture: { pendingTransfers, storageRuns, smsWrites, finishTransfer: () => finishTransfer?.() } });

window.fetch = async (input, options) => {
  const { pathname, searchParams } = new URL(String(input), location.href);
  const attempt = (attempts.get(pathname) ?? 0) + 1;
  attempts.set(pathname, attempt);
  const failure = (error: string) => Response.json({ error }, { status: 503 });

  if (pathname === '/api/v1/admin/audit-logs') {
    if (params.has('fail-audit') && attempt === 1) return failure('خطای نمونه هنگام دریافت سوابق');
    return Response.json(searchParams.get('category') === 'catalog' ? [] : [{
      id: longReference, operator: 'مدیر نمونه', timestamp: '2026-11-22T00:00:00Z',
      action: 'ثبت نمونه از سرور', category: 'finance', details: 'سابقهٔ مالی نمونه',
    }]);
  }
  if (pathname === '/api/v1/admin/ippanel/settings') {
    if (options?.method === 'PUT') {
      smsWrites.push(JSON.parse(String(options.body)));
      return Response.json({ success: true });
    }
    if (params.has('fail-sms') && attempt === 1) return failure('خطای نمونه هنگام دریافت تنظیمات پیامک');
    return Response.json({ enabled: true, baseUrl: 'https://provider.example', apiKeyMasked: 'configured', sender: '+983000505', patternCode: 'fixture-pattern' });
  }
  if (pathname === '/api/v1/admin/sms/logs') return Response.json([]);
  if (pathname === '/api/v1/admin/sms/credit') return failure('اعتبار در دسترس نیست');

  if (pathname === '/api/v1/admin/callback-retries') {
    if (failFirst && attempt === 1) return failure('خطای نمونه هنگام دریافت پرداخت‌های ناموفق');
    return Response.json({ retries: [{
      id: 'callback', orderNumber: longReference, status: 'waiting',
      attemptCount: 1, maxAttempts: 3, eventStatus: 'pending',
      lastError: 'https://gateway.example/' + longReference, createdAt: '2026-11-01',
    }] });
  }
  if (pathname === '/api/v1/warehouse/bins') {
    if (failFirst && attempt === 1) return failure('خطای نمونه هنگام دریافت جایگاه‌ها');
    return Response.json([{
      binCode: 'MOBILE-BIN-01', hallName: 'سالن نمونه', capacityStatus: 'verified',
      capacityVolumeCm3: 123456789, occupiedVolumeCm3: 1234567, reservedVolumeCm3: 20000,
      remainingVolumeCm3: 122202222, maxWeightGrams: 130000,
    }]);
  }
  if (pathname === '/api/v1/warehouse/bins/MOBILE-BIN-01/capacity') {
    return failure('خطای نمونه هنگام ذخیره ظرفیت');
  }
  if (pathname === '/api/v1/admin/settings/tapin-boxes') {
    if (params.has('fail-tapin') && attempt === 1) return failure('خطای نمونه هنگام دریافت کاتالوگ جعبه‌ها');
    return Response.json({
      success: true, defaultBoxId: '4',
      boxes: [{ id: '4', title: 'جعبه نمونه', lengthCm: 30, widthCm: 20, heightCm: 12, maxWeightGrams: 1500, isActive: true, sort: 0 }],
    });
  }
  if (pathname === '/api/v1/admin/settings/storage-fees') {
    if (failFirst && attempt === 1) return failure('خطای نمونه هنگام دریافت تعرفه‌ها');
    return Response.json({
      storageFeeEnabled: true, storageFeeFreeDays: 30, storageFeePerKgDay: 2500, storageFeePerM3Day: 8000,
    });
  }
  if (pathname === '/api/v1/admin/storage-fees/run') {
    storageRuns.push(attempt);
    return Response.json({
      positionsCharged: 1, chargesCreated: 1, positionsSkippedNoMeasurement: 0,
      totalCharged: 2500, moreWork: false, verifiedReceiptHistoryOnly: true,
    });
  }
  if (pathname === '/api/v1/finance/settlements/approved') {
    if (failFirst && attempt === 1) return failure('خطای نمونه هنگام دریافت تسویه‌ها');
    return Response.json({ settlements: [
      { id: 'first-settlement', recipientName: 'گیرنده یک', shaba: 'IR620170000001234567890123', bankName: 'بانک نمونه', amount: 100000 },
      { id: 'second-settlement', recipientName: 'گیرنده دو', shaba: 'IR880120000000987654321001', bankName: 'بانک نمونه', amount: 200000 },
    ] });
  }
  if (pathname.startsWith('/api/v1/admin/payouts/') && pathname.endsWith('/confirm-manual-transfer')) {
    const id = pathname.split('/')[5];
    const { transfer_reference: reference } = JSON.parse(String(options?.body));
    pendingTransfers.push({ id, reference });
    await new Promise<void>(resolve => { finishTransfer = resolve; });
    return failure('خطای نمونه هنگام ثبت واریز');
  }
  if (pathname === '/api/v1/admin/orders/pending-payment') {
    return Response.json({ success: true, orders: [{
      id: 'fixture-unpaid', orderNumber: longReference, customerName: 'مشتری نمونه',
      amountToman: 25000, createdAt: '2026-11-22', ageHours: 12, paymentStatus: 'pending', status: 'pending',
    }], hasMore: false });
  }
  if (pathname === '/api/v1/admin/support/tickets') return Response.json({ tickets: [] });
  if (pathname === '/api/v1/admin/approvals') {
    return Response.json(storefrontApprovalPending ? [{
      id: 'store-name-approval', entityType: 'store_name', targetId: 'fixture-store',
      title: 'درخواست تغییر نام ویترین نمونه', requestedBy: 'فروشنده نمونه',
      requestedData: { newName: 'ویترین نمونه' }, status: 'pending', createdAt: '2026-11-22T00:00:00Z',
    }] : []);
  }
  if (pathname === '/api/v1/admin/approvals/store-name-approval/review' && options?.method === 'POST') {
    const { status } = JSON.parse(String(options.body));
    if (status !== 'approved') return Response.json({ error: 'تأیید درخواست ثبت نشد.' }, { status: 400 });
    storefrontApprovalPending = false;
    return Response.json({ success: true, id: 'store-name-approval', status });
  }
  if (pathname === '/api/v1/admin/operations') {
    return Response.json({ jobs: [], overdueQC: [], supplierSLA: [], jobHealth: [] });
  }
  if (pathname === '/api/v1/admin/inventory/reconciliation') {
    return Response.json({ generated_at: '2026-11-22T00:00:00Z', checks: [], held_lots: [], coverage_gaps: [], failed_captures: [] });
  }
  if (pathname === '/api/v1/admin/spncx/backfill/status') {
    return Response.json({
      running: false, products: [],
      summary: { pending: 0, completed: 0, failed: 0, excluded: 0, total_units_minted: 0 },
    });
  }
  if (pathname === '/api/v1/admin/ad-network/report') {
    const date = '2026-11-22T00:00:00Z';
    return Response.json({
      windowDays: Number(searchParams.get('days')), from: date, to: date, limit: 50,
      summary: {
        acceptedClicks: 10, acceptedImpressions: 60, rejectedClicks: 1, rejectedImpressions: 0,
        rejectedChallenges: 0, otherRejectedEvents: 0, totalRejectedEvents: 1,
      },
      reasons: [{ reason: 'too_fast', eventType: 'click', count: 1 }],
      incidents: [{ id: 'incident-1', campaignId: 'fixture', campaignName: 'کمپین نمونه', storeName: 'فروشگاه نمونه',
        eventType: 'click', reason: 'too_fast', occurredAt: date }],
    });
  }
  return fetchFixture(input, options);
};
