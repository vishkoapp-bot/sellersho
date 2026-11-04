// Warehouse E2E — OTP version (replaces test_warehouse_e2e.cjs step 1).
// TEST/STAGING ONLY. Never run against production: OTP send is a real SMS
// in production, and mutating steps change warehouse/inventory state.
//
// What changed vs the old file:
//  - Step 1 was POST /auth/password/login with a warehouse username/password.
//    Password login is now super-admin-only in backend/cmd/server/main.go,
//    so that step always failed (410 in the Oct 13 report, 401 now) and
//    cascaded to 0/18. Step 1 is now OTP: send + verify via
//    test_helpers_otp.cjs. See that file for how a code is obtained:
//    TEST_OTP_CODE env, or staging-Redis read. Never guess a code.
//  - Steps are fixture-aware: the old file assumed dev-seed fixtures
//    (prod-1, INB-TEST-PENDING-01, ord-ready-pick-001) that do NOT exist
//    in production (prod-1 returns 404 live). If a fixture is missing,
//    dependent steps are reported SKIP, not FAIL, so a staging run with
//    seeded dev data can still reach 18/18 PASS, while a run without
//    fixtures honestly reports PASS + SKIP instead of a false 0/18.
//  - Original file is left untouched. This file writes a NEW report:
//    warehouse-e2e-otp-test-report.md
//  - The warehouse test mobile must already be provisioned as
//    warehouse_staff (dev seed: 09000000007). OTP verify with a requested
//    role never grants that role; it only selects an existing capability,
//    otherwise it returns 403. Set TEST_WAREHOUSE_MOBILE to override.
const fs = require('fs');
const path = require('path');
const { request, getOTPToken, maskMobile } = require('./test_helpers_otp.cjs');

async function run() {
  console.log('================================================================');
  console.log(' DRUP PLATFORM - WAREHOUSE STAFF LIFECYCLE INTEGRATION TEST (OTP)');
  console.log('================================================================\n');
  console.log('WARNING: test/staging only. Do not run against production.\n');

  const healthRes = await request('GET', '/api/health');
  if (healthRes.status !== 200) { console.error(`Backend unreachable. Status: ${healthRes.status}`); process.exit(1); }
  console.log(`Backend health: ${healthRes.body?.status || 'ok'} (${healthRes.latency}ms)\n`);

  const mobile = process.env.TEST_WAREHOUSE_MOBILE || '09000000007';
  const targetInboundId = 'INB-TEST-PENDING-01';
  const targetOrderId = 'ord-ready-pick-001';
  const results = [];
  let warehouseToken = null;
  let createdDiscrepancyId = null;
  let putawayTaskId = null;
  let trackingNumber = null;
  let prodExists = false;
  let inboundExists = false;

  function record(step, title, method, endpoint, res, outcome, details) {
    // outcome: 'PASS' | 'FAIL' | 'SKIP'
    results.push({ step, title, endpoint, method, status: res ? res.status : '-', latency: res ? res.latency : 0, outcome, details });
    const mark = outcome === 'PASS' ? '✅ PASS' : outcome === 'SKIP' ? '⏭️  SKIP' : '❌ FAIL';
    console.log(`[Step ${String(step).padStart(2, '0')}/18] ${mark} [HTTP ${res ? res.status : '-'}] - ${title} — ${details}`);
  }
  const skip = (step, title, method, endpoint, reason) => record(step, title, method, endpoint, { status: '-', latency: 0 }, 'SKIP', reason);

  // Fixture check (public, no token): does dev product prod-1 exist?
  const prodRes = await request('GET', '/api/v1/products/prod-1');
  prodExists = prodRes.status === 200;
  console.log(`Fixture check: prod-1 ${prodExists ? 'exists' : 'NOT FOUND (prod-dependent steps will SKIP)'}`);

  // 1. Warehouse Staff Authentication via OTP (replaces password login)
  let s1Pass = false;
  try {
    const auth = await getOTPToken(mobile, 'warehouse_staff');
    warehouseToken = auth.token;
    const roleOk = auth.user?.role === 'warehouse_staff' || (Array.isArray(auth.user?.capabilities) && (auth.user.capabilities.includes('warehouse_staff') || auth.user.capabilities.includes('logistics_specialist')));
    s1Pass = !!warehouseToken && roleOk;
    record(1, 'ورود پرسنل انبار با OTP موبایل', 'POST', '/api/v1/auth/otp/verify', { status: 200, latency: 0 }, s1Pass ? 'PASS' : 'FAIL', s1Pass ? `ورود موفق برای ${maskMobile(mobile)}، نقش ${auth.user?.role}` : `توکن دریافت شد ولی نقش انبار تایید نشد: ${JSON.stringify(auth.user)}`);
  } catch (e) {
    record(1, 'ورود پرسنل انبار با OTP موبایل', 'POST', '/api/v1/auth/otp/verify', { status: 0, latency: 0 }, 'FAIL', e.message);
  }
  if (!warehouseToken) {
    console.log('\nNo warehouse token — remaining steps cannot run. See step 1 error above (most likely: user not provisioned as warehouse_staff, OTP expired, or wrong code).');
    writeReport(results, mobile, targetInboundId, targetOrderId, trackingNumber);
    process.exit(1);
  }

  // 2. Profile / RBAC
  const s2 = await request('GET', '/api/v1/auth/me', null, warehouseToken);
  record(2, 'تأیید هویت و دسترسی‌های پرسنل انبار (RBAC)', 'GET', '/api/v1/auth/me', s2, (s2.status === 200 && (s2.body?.role === 'warehouse_staff' || (Array.isArray(s2.body?.capabilities) && s2.body.capabilities.length > 0))) ? 'PASS' : 'FAIL', s2.status === 200 ? `نقش: ${s2.body?.role}` : `HTTP ${s2.status}`);

  // 3. Bins (read-only)
  const s3 = await request('GET', '/api/v1/warehouse/bins', null, warehouseToken);
  record(3, 'استعلام و مشاهده قفسه‌ها و لوکیشن‌های انبار (Bins)', 'GET', '/api/v1/warehouse/bins', s3, (s3.status === 200 && (Array.isArray(s3.body?.bins) || Array.isArray(s3.body))) ? 'PASS' : 'FAIL', s3.status === 200 ? 'قفسه‌ها دریافت شد' : `HTTP ${s3.status}`);

  // 4. Pending inbounds — also determines inboundExists
  const s4 = await request('GET', '/api/v1/warehouse/inbounds/pending', null, warehouseToken);
  const inbounds = Array.isArray(s4.body?.inbounds) ? s4.body.inbounds : (Array.isArray(s4.body) ? s4.body : []);
  inboundExists = inbounds.some((x) => (x.id || x.inboundId) === targetInboundId);
  record(4, 'مشاهده صف محموله‌های ورودی در انتظار تخلیه', 'GET', '/api/v1/warehouse/inbounds/pending', s4, s4.status === 200 && Array.isArray(s4.body?.inbounds) ? 'PASS' : 'FAIL', s4.status === 200 ? `تعداد در انتظار: ${inbounds.length}، محموله تست ${inboundExists ? 'موجود است' : 'موجود نیست (مراحل ورودی SKIP می‌شوند)'}` : `HTTP ${s4.status}`);

  // 5-7. Inbound receiving (requires inbound fixture)
  if (!inboundExists) {
    skip(5, 'شروع فرآیند تخلیه و دریافت محموله ورودی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/start-receiving`, 'محموله تست در صف انتظار نیست');
    skip(6, 'اسکن بارکد اقلام و ثبت کارتن‌های دریافتی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/scan-item`, 'محموله تست در صف انتظار نیست');
    skip(7, 'تکمیل دریافت و ثبت محموله در وضعیت Received', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/complete`, 'محموله تست در صف انتظار نیست');
  } else {
    const s5 = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/start-receiving`, {}, warehouseToken);
    record(5, 'شروع فرآیند تخلیه و دریافت محموله ورودی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/start-receiving`, s5, (s5.status === 200 && s5.body?.status === 'receiving') ? 'PASS' : 'FAIL', `HTTP ${s5.status}`);
    const s6 = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/scan-item`, { barcode: '6260123456789', productId: 'prod-1', quantity: 45, cartonNumber: 'CTN-A01' }, warehouseToken);
    record(6, 'اسکن بارکد اقلام و ثبت کارتن‌های دریافتی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/scan-item`, s6, (s6.status === 200 && (s6.body?.scannedCount ?? 0) >= 0) ? 'PASS' : 'FAIL', `HTTP ${s6.status}`);
    const s7 = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/complete`, {}, warehouseToken);
    record(7, 'تکمیل دریافت و ثبت محموله در وضعیت Received', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/complete`, s7, (s7.status === 200 && s7.body?.status === 'received') ? 'PASS' : 'FAIL', `HTTP ${s7.status}`);
  }

  // 8. QC queue (read-only)
  const s8 = await request('GET', '/api/v1/warehouse/qc/queue', null, warehouseToken);
  record(8, 'مشاهده صف بازرسی کنترل کیفی اقلام (QC Queue)', 'GET', '/api/v1/warehouse/qc/queue', s8, (s8.status === 200 && Array.isArray(s8.body?.queue)) ? 'PASS' : 'FAIL', `HTTP ${s8.status}`);

  // 9-11. QC pass/fail/discrepancy (require prod-1)
  if (!prodExists) {
    skip(9, 'تأیید کنترل کیفی کالا و ارجاع به چیدمان (QC Pass)', 'POST', '/api/v1/warehouse/qc/prod-1/pass', 'محصول تست prod-1 در کاتالوگ نیست');
    skip(10, 'ثبت عیوب ظاهری و عدم انطباق کیفی اقلام (QC Fail)', 'POST', '/api/v1/warehouse/qc/prod-1/fail', 'محصول تست prod-1 در کاتالوگ نیست');
    skip(11, 'صدور صورت‌جلسه مغایرت کیفی و مقداری با تأمین‌کننده', 'POST', '/api/v1/warehouse/qc/discrepancy/create', 'محصول تست prod-1 در کاتالوگ نیست');
  } else {
    const s9 = await request('POST', '/api/v1/warehouse/qc/prod-1/pass', { inboundId: targetInboundId, quantity: 45 }, warehouseToken);
    record(9, 'تأیید کنترل کیفی کالا و ارجاع به چیدمان (QC Pass)', 'POST', '/api/v1/warehouse/qc/prod-1/pass', s9, (s9.status === 200 && s9.body?.status === 'passed') ? 'PASS' : 'FAIL', `HTTP ${s9.status}`);
    const s10 = await request('POST', '/api/v1/warehouse/qc/prod-1/fail', { inboundId: targetInboundId, quantity: 5, reason: 'آسیب‌دیدگی کارتن و پارگی بسته‌بندی حین حمل' }, warehouseToken);
    record(10, 'ثبت عیوب ظاهری و عدم انطباق کیفی اقلام (QC Fail)', 'POST', '/api/v1/warehouse/qc/prod-1/fail', s10, (s10.status === 200 && s10.body?.status === 'failed') ? 'PASS' : 'FAIL', `HTTP ${s10.status}`);
    const s11 = await request('POST', '/api/v1/warehouse/qc/discrepancy/create', { inboundId: targetInboundId, supplierId: 'sup-technolife', productId: 'prod-1', expectedQuantity: 50, receivedQuantity: 45, defectCount: 5, reason: 'کسری ۵ قلم کالا به همراه عدم تطابق بارکد' }, warehouseToken);
    if ((s11.status === 200 || s11.status === 201) && s11.body?.discrepancyId) createdDiscrepancyId = s11.body.discrepancyId;
    record(11, 'صدور صورت‌جلسه مغایرت کیفی و مقداری با تأمین‌کننده', 'POST', '/api/v1/warehouse/qc/discrepancy/create', s11, ((s11.status === 200 || s11.status === 201) && !!s11.body?.discrepancyId) ? 'PASS' : 'FAIL', `HTTP ${s11.status}`);
  }

  // 12. Putaway queue (read-only)
  const s12 = await request('GET', '/api/v1/warehouse/putaway/queue', null, warehouseToken);
  record(12, 'مشاهده صف وظایف چیدمان در قفسه (Putaway Queue)', 'GET', '/api/v1/warehouse/putaway/queue', s12, (s12.status === 200 && Array.isArray(s12.body?.queue)) ? 'PASS' : 'FAIL', `HTTP ${s12.status}`);

  // 13-14. Putaway (require prod-1)
  // Workflow fix vs the old test: in the current binary, putaway assign
  // is gated by TWO verifications the old test never did:
  //  (a) product measurements must be verified via
  //      POST /warehouse/products/<id>/measurements (warehouse_staff), and
  //  (b) bin capacity must be verified via
  //      PUT /warehouse/bins/<binCode>/capacity with verify:true, which
  //      requires logistics_specialist or admin — not warehouse_staff.
  //      So this step obtains an admin token (TEST_ADMIN_OTP_CODE, see
  //      below) for (b) only. Without (a)+(b), assign returns 400 with
  //      a Persian message, not a 500 — it is a workflow gate, not a crash.
  // Also fixed vs the old test: confirm takes a TASK id, not a product
  // id. The old test called /putaway/prod-1/confirm, which the handler
  // treats as taskId="prod-1" and therefore always 404s. Here confirm
  // uses the taskId returned by assign.
  if (!prodExists) {
    skip(13, 'تخصیص لوکیشن انبار (قفسه A-12-05) به کالاهای ورودی', 'POST', '/api/v1/warehouse/putaway/prod-1/assign', 'محصول تست prod-1 در کاتالوگ نیست');
    skip(14, 'تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار', 'POST', '/api/v1/warehouse/putaway/<taskId>/confirm', 'محصول تست prod-1 در کاتالوگ نیست');
  } else {
    const meas = await request('POST', '/api/v1/warehouse/products/prod-1/measurements', { lengthMm: 150, widthMm: 80, heightMm: 20, weightGrams: 250, reason: 'test measurement' }, warehouseToken);
    console.log(`  Putaway precondition: measurements ${meas.status} ${meas.body?.measurementStatus || ''}`);
    let adminToken = null;
    try {
      const adminMobile = process.env.TEST_ADMIN_MOBILE || '09000000001';
      const savedCode = process.env.TEST_OTP_CODE;
      if (process.env.TEST_ADMIN_OTP_CODE) process.env.TEST_OTP_CODE = process.env.TEST_ADMIN_OTP_CODE;
      const adminAuth = await getOTPToken(adminMobile, 'admin');
      adminToken = adminAuth.token;
      if (savedCode !== undefined) process.env.TEST_OTP_CODE = savedCode;
    } catch (e) { console.log(`  Putaway precondition: admin token failed (bin capacity will stay unverified): ${e.message}`); }
    if (adminToken) {
      const cap = await request('PUT', '/api/v1/warehouse/bins/A-12-05/capacity', { capacityVolumeCm3: 1000000, maxWeightGrams: 50000, verify: true }, adminToken);
      console.log(`  Putaway precondition: bin capacity ${cap.status} ${cap.body?.capacityStatus || JSON.stringify(cap.body)}`);
    }
    const s13 = await request('POST', '/api/v1/warehouse/putaway/prod-1/assign', { binLocation: 'A-12-05', quantity: 45 }, warehouseToken);
    if (s13.body?.taskId) putawayTaskId = s13.body.taskId;
    record(13, 'تخصیص لوکیشن انبار (قفسه A-12-05) به کالاهای ورودی', 'POST', '/api/v1/warehouse/putaway/prod-1/assign', s13, (s13.status === 200 && s13.body?.binLocation === 'A-12-05') ? 'PASS' : 'FAIL', `HTTP ${s13.status} ${s13.body && typeof s13.body === 'string' ? s13.body : ''}`);
    if (!putawayTaskId) {
      skip(14, 'تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار', 'POST', '/api/v1/warehouse/putaway/<taskId>/confirm', 'تخصیص موفق نبود، پس taskId برای تأیید وجود ندارد');
    } else {
      const s14 = await request('POST', `/api/v1/warehouse/putaway/${putawayTaskId}/confirm`, {}, warehouseToken);
      record(14, 'تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار', 'POST', `/api/v1/warehouse/putaway/${putawayTaskId}/confirm`, s14, (s14.status === 200 && s14.body?.status === 'completed') ? 'PASS' : 'FAIL', `HTTP ${s14.status}`);
    }
  }

  // 15. Ready-to-pick (read-only, also determines order fixture)
  const s15 = await request('GET', '/api/v1/warehouse/orders/ready-to-pick', null, warehouseToken);
  const readyOrders = Array.isArray(s15.body?.orders) ? s15.body.orders : [];
  const orderExists = readyOrders.some((x) => (x.id || x.orderId) === targetOrderId);
  record(15, 'استعلام سفارشات پرداخت‌شده و آماده جمع‌آوری (Ready to Pick)', 'GET', '/api/v1/warehouse/orders/ready-to-pick', s15, (s15.status === 200 && Array.isArray(s15.body?.orders)) ? 'PASS' : 'FAIL', s15.status === 200 ? `تعداد در صف: ${readyOrders.length}، سفارش تست ${orderExists ? 'موجود است' : 'موجود نیست (مراحل خروجی SKIP می‌شوند)'}` : `HTTP ${s15.status}`);

  // 16-18. Picking/packing/shipping (require order fixture)
  if (!orderExists) {
    skip(16, 'آغاز فرآیند جمع‌آوری سفارش از قفسه‌ها (Start Picking)', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/start-picking`, 'سفارش تست در صف آماده‌جمع‌آوری نیست');
    skip(17, 'اسکن بارکد اقلام برداشته شده از قفسه و تطبیق سفارش', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/pick-item`, 'سفارش تست در صف آماده‌جمع‌آوری نیست');
    skip(18, 'تکمیل جمع‌آوری، صدور بارنامه، بسته‌بندی و تحویل به حامل پستی', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/ship`, 'سفارش تست در صف آماده‌جمع‌آوری نیست');
  } else {
    const s16 = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/start-picking`, {}, warehouseToken);
    record(16, 'آغاز فرآیند جمع‌آوری سفارش از قفسه‌ها (Start Picking)', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/start-picking`, s16, (s16.status === 200 && s16.body?.status === 'picking') ? 'PASS' : 'FAIL', `HTTP ${s16.status}`);
    const s17 = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/pick-item`, { barcode: '6260123456789', productId: 'prod-1', quantity: 1, binLocation: 'A-12-05' }, warehouseToken);
    record(17, 'اسکن بارکد اقلام برداشته شده از قفسه و تطبیق سفارش', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/pick-item`, s17, (s17.status === 200 && s17.body?.success === true) ? 'PASS' : 'FAIL', `HTTP ${s17.status}`);
    const s18a = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/complete-picking`, {}, warehouseToken);
    const s18b = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/pack`, {}, warehouseToken);
    if (s18b.body?.trackingNumber) trackingNumber = s18b.body.trackingNumber;
    const s18c = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/ship`, { carrier: 'پست پیشتاز', trackingNumber: trackingNumber || 'TRK-POST-991100' }, warehouseToken);
    record(18, 'تکمیل جمع‌آوری، صدور بارنامه، بسته‌بندی و تحویل به حامل پستی', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/ship`, s18c, (s18a.status === 200 && s18b.status === 200 && s18c.status === 200 && s18c.body?.status === 'shipped') ? 'PASS' : 'FAIL', `HTTP complete=${s18a.status} pack=${s18b.status} ship=${s18c.status}`);
  }

  writeReport(results, mobile, targetInboundId, targetOrderId, trackingNumber);
  const failed = results.filter((t) => t.outcome === 'FAIL').length;
  process.exit(failed > 0 ? 1 : 0);

  function writeReport(res, mob, inboundId, orderId, tracking) {
    const passed = res.filter((t) => t.outcome === 'PASS').length;
    const failedN = res.filter((t) => t.outcome === 'FAIL').length;
    const skipped = res.filter((t) => t.outcome === 'SKIP').length;
    console.log('\n================================================================');
    console.log(` WAREHOUSE OTP TEST COMPLETED: ${passed} PASS, ${failedN} FAIL, ${skipped} SKIP / ${res.length}`);
    console.log('================================================================\n');
    const report = `# گزارش تست انبار با OTP (نسخه بازنویسی‌شده)\n\n- موبایل تست (ماسک‌شده): \`${maskMobile(mob)}\`\n- محموله ورودی: \`${inboundId}\` — سفارش خروجی: \`${orderId}\`\n- کد رهگیری صادر شده: \`${tracking || 'TRK-POST-991100'}\`\n- نتیجه: ${passed} موفق، ${failedN} ناموفق، ${skipped} ردشده (فیكسچر موجود نبود) از ${res.length} مرحله\n- نکته: این فایل جایگزین گزارش قدیمی نیست؛ گزارش قدیمی دست نخورده باقی مانده است.\n\n| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | نتیجه | جزئیات |\n|:---:|:---|:---:|:---|:---:|:---:|:---|\n${res.map((t) => `| ${String(t.step).padStart(2, '0')} | ${t.title} | \`${t.method}\` | \`${t.endpoint}\` | \`${t.status}\` | ${t.outcome} | ${t.details} |`).join('\n')}\n`;
    fs.writeFileSync(path.join(__dirname, 'warehouse-e2e-otp-test-report.md'), report, 'utf8');
    console.log('Report: warehouse-e2e-otp-test-report.md');
  }
}

run().catch((err) => { console.error('Test execution failed:', err.message); process.exit(1); });
