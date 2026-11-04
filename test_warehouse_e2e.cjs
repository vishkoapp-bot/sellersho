const fs = require('fs');
const path = require('path');
const { request } = require('./test_helpers.cjs');

async function runWarehouseE2ETests() {
  console.log('================================================================');
  console.log(' DRUP PLATFORM - WAREHOUSE STAFF LIFECYCLE INTEGRATION TEST');
  console.log('================================================================\n');

  const healthRes = await request('GET', '/api/health');
  if (healthRes.status !== 200) {
    console.error(`Backend unreachable. Status: ${healthRes.status}`);
    process.exit(1);
  }
  console.log(`Backend health: ${healthRes.body?.status || 'ok'} (${healthRes.latency}ms)\n`);

  const testResults = [];
  let warehouseToken = null;
  const targetInboundId = 'INB-TEST-PENDING-01';
  const targetOrderId = 'ord-ready-pick-001';
  let createdDiscrepancyId = null;
  let putawayTaskId = null;
  let trackingNumber = null;

  function record(step, title, method, endpoint, res, passed, details, error) {
    testResults.push({ step, title, endpoint, method, status: res.status, latency: res.latency, passed, details, error: error || null });
    const mark = passed ? '✅ PASS' : '❌ FAIL';
    console.log(`[Step ${String(step).padStart(2, '0')}/18] ${mark} [HTTP ${res.status}] (${res.latency}ms) - ${title}`);
    if (!passed && error) console.log(`   └─ Error: ${error}`);
  }

  // 1. Warehouse Staff Authentication
  const s1Res = await request('POST', '/api/v1/auth/password/login', {
    username: 'warehouse_ali',
    password: 'warehouse123'
  });
  const s1Pass = s1Res.status === 200 && s1Res.body?.token && s1Res.body?.user?.role === 'warehouse_staff';
  if (s1Pass) warehouseToken = s1Res.body.token;
  record(1, 'ورود پرسنل انبار با نام کاربری و رمز عبور', 'POST', '/api/v1/auth/password/login', s1Res, s1Pass,
    s1Pass ? `ورود موفق با شناسه ${s1Res.body?.user?.id} و نقش ${s1Res.body?.user?.role}` : `وضعیت HTTP ${s1Res.status}`,
    s1Pass ? null : JSON.stringify(s1Res.body));

  // 2. Profile Verification (RBAC & Claims)
  const s2Res = await request('GET', '/api/v1/auth/me', null, warehouseToken);
  const s2Pass = s2Res.status === 200 && s2Res.body?.role === 'warehouse_staff';
  record(2, 'تأیید هویت و دسترسی‌های پرسنل انبار (RBAC)', 'GET', '/api/v1/auth/me', s2Res, s2Pass,
    s2Pass ? `نام کاربر: ${s2Res.body?.name} - نقش: ${s2Res.body?.role}` : `وضعیت HTTP ${s2Res.status}`,
    s2Pass ? null : JSON.stringify(s2Res.body));

  // 3. Storage Bin Location Lookup
  const s3Res = await request('GET', '/api/v1/warehouse/bins', null, warehouseToken);
  const s3Pass = s3Res.status === 200 && (Array.isArray(s3Res.body?.bins) || Array.isArray(s3Res.body));
  const binCount = Array.isArray(s3Res.body?.bins) ? s3Res.body.bins.length : (Array.isArray(s3Res.body) ? s3Res.body.length : 0);
  record(3, 'استعلام و مشاهده قفسه‌ها و لوکیشن‌های انبار (Bins)', 'GET', '/api/v1/warehouse/bins', s3Res, s3Pass,
    s3Pass ? `تعداد قفسه‌ها و لوکیشن‌های فعال: ${binCount}` : `وضعیت HTTP ${s3Res.status}`,
    s3Pass ? null : JSON.stringify(s3Res.body));

  // 4. View Pending Inbound Shipments
  const s4Res = await request('GET', '/api/v1/warehouse/inbounds/pending', null, warehouseToken);
  const s4Pass = s4Res.status === 200 && Array.isArray(s4Res.body?.inbounds);
  record(4, 'مشاهده صف محموله‌های ورودی در انتظار تخلیه', 'GET', '/api/v1/warehouse/inbounds/pending', s4Res, s4Pass,
    s4Pass ? `تعداد محموله‌های در انتظار: ${s4Res.body?.inbounds?.length}` : `وضعیت HTTP ${s4Res.status}`,
    s4Pass ? null : JSON.stringify(s4Res.body));

  // 5. Start Inbound Receiving Workflow
  const s5Res = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/start-receiving`, {}, warehouseToken);
  const s5Pass = s5Res.status === 200 && s5Res.body?.status === 'receiving';
  record(5, 'شروع فرآیند تخلیه و دریافت محموله ورودی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/start-receiving`, s5Res, s5Pass,
    s5Pass ? `محموله ${targetInboundId} وارد وضعیت receiving شد` : `وضعیت HTTP ${s5Res.status}`,
    s5Pass ? null : JSON.stringify(s5Res.body));

  // 6. Barcode Scanning & Item Counting
  const s6Res = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/scan-item`, {
    barcode: '6260123456789',
    productId: 'prod-1',
    quantity: 45,
    cartonNumber: 'CTN-A01'
  }, warehouseToken);
  const s6Pass = s6Res.status === 200 && s6Res.body?.scannedCount >= 0;
  record(6, 'اسکن بارکد اقلام و ثبت کارتن‌های دریافتی', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/scan-item`, s6Res, s6Pass,
    s6Pass ? `اسکن با موفقیت ثبت شد (تعداد ردیف اسکن شده: ${s6Res.body?.scannedCount})` : `وضعیت HTTP ${s6Res.status}`,
    s6Pass ? null : JSON.stringify(s6Res.body));

  // 7. Inbound Receiving Completion
  const s7Res = await request('POST', `/api/v1/warehouse/inbounds/${targetInboundId}/complete`, {}, warehouseToken);
  const s7Pass = s7Res.status === 200 && s7Res.body?.status === 'received';
  record(7, 'تکمیل دریافت و ثبت محموله در وضعیت Received', 'POST', `/api/v1/warehouse/inbounds/${targetInboundId}/complete`, s7Res, s7Pass,
    s7Pass ? `محموله ورودی ${targetInboundId} تکمیل و دریافت شد` : `وضعیت HTTP ${s7Res.status}`,
    s7Pass ? null : JSON.stringify(s7Res.body));

  // 8. Quality Control (QC) Queue Lookup
  const s8Res = await request('GET', '/api/v1/warehouse/qc/queue', null, warehouseToken);
  const s8Pass = s8Res.status === 200 && Array.isArray(s8Res.body?.queue);
  record(8, 'مشاهده صف بازرسی کنترل کیفی اقلام (QC Queue)', 'GET', '/api/v1/warehouse/qc/queue', s8Res, s8Pass,
    s8Pass ? `تعداد اقلام در انتظار بررسی کیفی: ${s8Res.body?.queue?.length}` : `وضعیت HTTP ${s8Res.status}`,
    s8Pass ? null : JSON.stringify(s8Res.body));

  // 9. QC Pass Inspection Recording
  const s9Res = await request('POST', '/api/v1/warehouse/qc/prod-1/pass', {
    inboundId: targetInboundId,
    quantity: 45
  }, warehouseToken);
  const s9Pass = s9Res.status === 200 && s9Res.body?.status === 'passed';
  record(9, 'تأیید کنترل کیفی کالا و ارجاع به چیدمان (QC Pass)', 'POST', '/api/v1/warehouse/qc/prod-1/pass', s9Res, s9Pass,
    s9Pass ? 'کالای prod-1 تأیید کیفی شد و به صف چیدمان اضافه گردید' : `وضعیت HTTP ${s9Res.status}`,
    s9Pass ? null : JSON.stringify(s9Res.body));

  // 10. QC Defect / Rejection Recording
  const s10Res = await request('POST', '/api/v1/warehouse/qc/prod-1/fail', {
    inboundId: targetInboundId,
    quantity: 5,
    reason: 'آسیب‌دیدگی کارتن و پارگی بسته‌بندی حین حمل'
  }, warehouseToken);
  const s10Pass = s10Res.status === 200 && s10Res.body?.status === 'failed';
  record(10, 'ثبت عیوب ظاهری و عدم انطباق کیفی اقلام (QC Fail)', 'POST', '/api/v1/warehouse/qc/prod-1/fail', s10Res, s10Pass,
    s10Pass ? '۵ قلم کالای معیوب ثبت و به انبار ضایعات ارجاع داده شد' : `وضعیت HTTP ${s10Res.status}`,
    s10Pass ? null : JSON.stringify(s10Res.body));

  // 11. QC Discrepancy Logging & Ticket Generation
  const s11Res = await request('POST', '/api/v1/warehouse/qc/discrepancy/create', {
    inboundId: targetInboundId,
    supplierId: 'sup-technolife',
    productId: 'prod-1',
    expectedQuantity: 50,
    receivedQuantity: 45,
    defectCount: 5,
    reason: 'کسری ۵ قلم کالا به همراه عدم تطابق بارکد درج شده روی جعبه'
  }, warehouseToken);
  const s11Pass = (s11Res.status === 200 || s11Res.status === 201) && s11Res.body?.discrepancyId;
  if (s11Pass) createdDiscrepancyId = s11Res.body.discrepancyId;
  record(11, 'صدور صورت‌جلسه مغایرت کیفی و مقداری با تأمین‌کننده', 'POST', '/api/v1/warehouse/qc/discrepancy/create', s11Res, s11Pass,
    s11Pass ? `شناسه مغایرت ثبت شده: ${createdDiscrepancyId}` : `وضعیت HTTP ${s11Res.status}`,
    s11Pass ? null : JSON.stringify(s11Res.body));

  // 12. Putaway Task Queue Retrieval
  const s12Res = await request('GET', '/api/v1/warehouse/putaway/queue', null, warehouseToken);
  const s12Pass = s12Res.status === 200 && Array.isArray(s12Res.body?.queue);
  record(12, 'مشاهده صف وظایف چیدمان در قفسه (Putaway Queue)', 'GET', '/api/v1/warehouse/putaway/queue', s12Res, s12Pass,
    s12Pass ? `تعداد تسک‌های چیدمان در قفسه: ${s12Res.body?.queue?.length}` : `وضعیت HTTP ${s12Res.status}`,
    s12Pass ? null : JSON.stringify(s12Res.body));

  // 13. Shelf / Bin Location Assignment
  const s13Res = await request('POST', '/api/v1/warehouse/putaway/prod-1/assign', {
    binLocation: 'A-12-05',
    quantity: 45
  }, warehouseToken);
  const s13Pass = s13Res.status === 200 && s13Res.body?.binLocation === 'A-12-05';
  if (s13Pass) putawayTaskId = s13Res.body?.taskId;
  record(13, 'تخصیص لوکیشن انبار (قفسه A-12-05) به کالاهای ورودی', 'POST', '/api/v1/warehouse/putaway/prod-1/assign', s13Res, s13Pass,
    s13Pass ? `تسک ${putawayTaskId} به لوکیشن ${s13Res.body?.binLocation} منتسب شد` : `وضعیت HTTP ${s13Res.status}`,
    s13Pass ? null : JSON.stringify(s13Res.body));

  // 14. Putaway Confirmation & Inventory Stocking
  const s14Res = await request('POST', '/api/v1/warehouse/putaway/prod-1/confirm', {}, warehouseToken);
  const s14Pass = s14Res.status === 200 && s14Res.body?.status === 'completed';
  record(14, 'تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار', 'POST', '/api/v1/warehouse/putaway/prod-1/confirm', s14Res, s14Pass,
    s14Pass ? 'چیدمان کالا تأیید شد و موجودی در انبار فعال گردید' : `وضعیت HTTP ${s14Res.status}`,
    s14Pass ? null : JSON.stringify(s14Res.body));

  // 15. Outbound Ready-to-Pick Orders Lookup
  const s15Res = await request('GET', '/api/v1/warehouse/orders/ready-to-pick', null, warehouseToken);
  const s15Pass = s15Res.status === 200 && Array.isArray(s15Res.body?.orders);
  record(15, 'استعلام سفارشات پرداخت‌شده و آماده جمع‌آوری (Ready to Pick)', 'GET', '/api/v1/warehouse/orders/ready-to-pick', s15Res, s15Pass,
    s15Pass ? `تعداد سفارشات در صف جمع‌آوری: ${s15Res.body?.orders?.length}` : `وضعیت HTTP ${s15Res.status}`,
    s15Pass ? null : JSON.stringify(s15Res.body));

  // 16. Order Picking Initiation
  const s16Res = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/start-picking`, {}, warehouseToken);
  const s16Pass = s16Res.status === 200 && s16Res.body?.status === 'picking';
  record(16, 'آغاز فرآیند جمع‌آوری سفارش از قفسه‌ها (Start Picking)', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/start-picking`, s16Res, s16Pass,
    s16Pass ? `سفارش ${targetOrderId} در وضعیت picking قرار گرفت` : `وضعیت HTTP ${s16Res.status}`,
    s16Pass ? null : JSON.stringify(s16Res.body));

  // 17. Item Picking Scan & Verification
  const s17Res = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/pick-item`, {
    barcode: '6260123456789',
    productId: 'prod-1',
    quantity: 1,
    binLocation: 'A-12-05'
  }, warehouseToken);
  const s17Pass = s17Res.status === 200 && s17Res.body?.success === true;
  record(17, 'اسکن بارکد اقلام برداشته شده از قفسه و تطبیق سفارش', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/pick-item`, s17Res, s17Pass,
    s17Pass ? 'اسکن بارکد کالا در سبد جمع‌آوری با موفقیت ثبت شد' : `وضعیت HTTP ${s17Res.status}`,
    s17Pass ? null : JSON.stringify(s17Res.body));

  // 18. Picking Completion, Packaging & Dispatch Handover
  const s18aRes = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/complete-picking`, {}, warehouseToken);
  const s18bRes = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/pack`, {}, warehouseToken);
  if (s18bRes.body?.trackingNumber) trackingNumber = s18bRes.body.trackingNumber;
  const s18cRes = await request('POST', `/api/v1/warehouse/orders/${targetOrderId}/ship`, {
    carrier: 'پست پیشتاز',
    trackingNumber: trackingNumber || 'TRK-POST-991100'
  }, warehouseToken);

  const s18Pass = s18aRes.status === 200 && s18bRes.status === 200 && s18cRes.status === 200 && s18cRes.body?.status === 'shipped';
  record(18, 'تکمیل جمع‌آوری، صدور بارنامه، بسته‌بندی و تحویل به حامل پستی', 'POST', `/api/v1/warehouse/orders/${targetOrderId}/ship`, s18cRes, s18Pass,
    s18Pass ? `کد رهگیری: ${s18cRes.body?.trackingNumber} - شرکت پستی: ${s18cRes.body?.carrier} - وضعیت نهایی: shipped` : `وضعیت HTTP ${s18cRes.status}`,
    s18Pass ? null : JSON.stringify({ complete: s18aRes.body, pack: s18bRes.body, ship: s18cRes.body }));

  // Compile Summary & Write Report
  const totalSteps = testResults.length;
  const passedSteps = testResults.filter(t => t.passed).length;
  const failedSteps = totalSteps - passedSteps;
  const passRate = ((passedSteps / totalSteps) * 100).toFixed(1);

  console.log('\n================================================================');
  console.log(` WAREHOUSE INTEGRATION TEST COMPLETED: ${passedSteps}/${totalSteps} PASSED (${passRate}%)`);
  console.log('================================================================\n');

  const report = `# گزارش جامع تست یکپارچگی چرخه حیات انبارداری (Warehouse E2E Integration Test Report)

این آزمون رفتار سیستم را در چرخه کامل ۱۸ مرحله‌ای نقش **پرسنل انبارداری و لجستیک (Warehouse Staff)** روی سرور زنده پلتفرم (\`http://localhost:8080\`) ارزیابی کرده است.

---

## ۱. مشخصات عمومی آزمون
- **کاربر:** \`warehouse_ali\` (نقش: \`warehouse_staff\`)
- **شناسه محموله ورودی:** \`${targetInboundId}\`
- **شناسه سفارش خروجی:** \`${targetOrderId}\`
- **کد رهگیری صادر شده:** \`${trackingNumber || 'TRK-POST-991100'}\`
- **تعداد کل مراحل:** ${totalSteps}
- **تعداد مراحل موفق:** ${passedSteps}
- **درصد موفقیت (Pass Rate):** **${passRate}%**

---

## ۲. جدول جزئیات نتایج مراحل آزمون (18 گام چرخه حیات)

| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | زمان پاسخ | نتیجه | جزئیات عملکرد |
|:---:|:---|:---:|:---|:---:|:---:|:---:|:---|
${testResults.map(t => `| ${String(t.step).padStart(2, '0')} | ${t.title} | \`${t.method}\` | \`${t.endpoint}\` | \`${t.status}\` | ${t.latency}ms | ${t.passed ? '✅ موفق' : '❌ ناموفق'} | ${t.details} |`).join('\n')}

---

## ۳. تحلیل معماری جریان کار انبار (Workflow Breakdown)
1. **جریان ورود کالا به انبار (Inbound & ASN):** استعلام محموله‌های دارای بارنامه در وضعیت Pending، شروع تخلیه محموله، اسکن اتمیک بارکد اقلام و تغییر وضعیت به Received.
2. **کنترل کیفیت و رفع تعارضات (Quality Control & Discrepancy):** انتقال اقلام به صف بازرسی کیفی، ثبت اقلام سالم در جدول qc_results و انتقال به صف چیدمان، ثبت اقلام معیوب و صدور صورت‌جلسه مغایرت (qc_discrepancies).
3. **چیدمان در قفسه و شارژ موجودی (Putaway & Bin Assignment):** تخصیص کالاها به لوکیشن‌های انبار و تأیید قرارگیری فیزیکی.
4. **فرآیند خروجی و ارسال سفارشات (Outbound Picking, Packing & Shipping):** جمع‌آوری سفارشات پرداخت‌شده، اسکن بارکد اقلام، بسته‌بندی، صدور لیبل و بارنامه و تحویل به مأمور پست.
`;

  fs.writeFileSync(path.join(__dirname, 'warehouse-e2e-test-report.md'), report, 'utf8');
  console.log('Report generated successfully: D:\\drup\\warehouse-e2e-test-report.md');
}

runWarehouseE2ETests().catch(err => {
  console.error('Test execution failed:', err);
  process.exit(1);
});

