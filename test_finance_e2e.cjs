const fs = require('fs');
const path = require('path');
const { request } = require('./test_helpers.cjs');

async function runFinanceE2ETests() {
  console.log('================================================================');
  console.log(' DRUP PLATFORM - FINANCE SPECIALIST LIFECYCLE INTEGRATION TEST');
  console.log('================================================================\n');

  const healthRes = await request('GET', '/api/health');
  if (healthRes.status !== 200) {
    console.error(`Backend unreachable. Status: ${healthRes.status}`);
    process.exit(1);
  }
  console.log(`Backend health: ${healthRes.body?.status || 'ok'} (${healthRes.latency}ms)\n`);

  const testResults = [];
  let financeToken = null;
  const targetStoreId = 'store-pending-001';
  let createdDisputeId = null;

  function record(step, title, method, endpoint, res, passed, details, error) {
    testResults.push({ step, title, endpoint, method, status: res.status, latency: res.latency, passed, details, error: error || null });
    const mark = passed ? '✅ PASS' : '❌ FAIL';
    console.log(`[Step ${String(step).padStart(2, '0')}/12] ${mark} [HTTP ${res.status}] (${res.latency}ms) - ${title}`);
    if (!passed && error) console.log(`   └─ Error: ${error}`);
  }

  // 1. Finance Authentication (Login)
  const s1Res = await request('POST', '/api/v1/auth/password/login', {
    username: 'finance_sara',
    password: 'finance123'
  });
  const s1Pass = s1Res.status === 200 && s1Res.body?.token && (s1Res.body?.user?.role === 'finance' || s1Res.body?.user?.role === 'finance_specialist');
  if (s1Pass) financeToken = s1Res.body.token;
  record(1, 'ورود کارشناس مالی با نام کاربری و رمز عبور', 'POST', '/api/v1/auth/password/login', s1Res, s1Pass,
    s1Pass ? `ورود موفق با شناسه ${s1Res.body?.user?.id} و نقش ${s1Res.body?.user?.role}` : `وضعیت HTTP ${s1Res.status}`,
    s1Pass ? null : JSON.stringify(s1Res.body));

  // 2. Profile Verification (RBAC & Claims)
  const s2Res = await request('GET', '/api/v1/auth/me', null, financeToken);
  const s2Pass = s2Res.status === 200 && (s2Res.body?.role === 'finance' || s2Res.body?.role === 'finance_specialist');
  record(2, 'تأیید دسترسی حسابداری و مدیریت مالی (RBAC Finance Role)', 'GET', '/api/v1/auth/me', s2Res, s2Pass,
    s2Pass ? `نام کارشناس: ${s2Res.body?.name} - نقش: ${s2Res.body?.role}` : `وضعیت HTTP ${s2Res.status}`,
    s2Pass ? null : JSON.stringify(s2Res.body));

  // 3. General Ledger Inquiry (Full System Ledger)
  const s3Res = await request('GET', '/api/v1/finance/ledger', null, financeToken);
  const s3Pass = s3Res.status === 200 && (Array.isArray(s3Res.body?.ledger) || Array.isArray(s3Res.body));
  const ledgerCount = Array.isArray(s3Res.body?.ledger) ? s3Res.body.ledger.length : (Array.isArray(s3Res.body) ? s3Res.body.length : 0);
  record(3, 'استعلام و مشاهده دفتر کل مالی پلتفرم (General Ledger)', 'GET', '/api/v1/finance/ledger', s3Res, s3Pass,
    s3Pass ? `تعداد کل اسناد و تراکنش‌های دفتر کل: ${ledgerCount}` : `وضعیت HTTP ${s3Res.status}`,
    s3Pass ? null : JSON.stringify(s3Res.body));

  // 4. Date-Filtered Ledger Query
  const s4Res = await request('GET', '/api/v1/finance/ledger?dateFrom=2026-09-01&dateTo=2026-10-31', null, financeToken);
  const s4Pass = s4Res.status === 200 && (Array.isArray(s4Res.body?.ledger) || Array.isArray(s4Res.body));
  record(4, 'فیلتر زمانی اسناد دفتر کل بر اساس بازه تاریخ شمسی/میلادی', 'GET', '/api/v1/finance/ledger?dateFrom=...&dateTo=...', s4Res, s4Pass,
    s4Pass ? 'اسناد مالی در بازه زمانی تعیین‌شده با موفقیت واکشی شدند' : `وضعیت HTTP ${s4Res.status}`,
    s4Pass ? null : JSON.stringify(s4Res.body));

  // 5. Real-Time Store Balance Inspection
  const s5Res = await request('GET', `/api/v1/finance/stores/${targetStoreId}/balance`, null, financeToken);
  const s5Pass = s5Res.status === 200 && s5Res.body?.balance !== undefined;
  record(5, 'محاسبه برخط تراز و مانده حساب دراپ‌شیپر (Store Balance)', 'GET', `/api/v1/finance/stores/${targetStoreId}/balance`, s5Res, s5Pass,
    s5Pass ? `مانده قابل تسویه: ${Number(s5Res.body?.availableAmount || s5Res.body?.balance).toLocaleString()} ریال` : `وضعیت HTTP ${s5Res.status}`,
    s5Pass ? null : JSON.stringify(s5Res.body));

  // 6. Monthly Store Statement Inquiry
  const s6Res = await request('GET', `/api/v1/finance/stores/${targetStoreId}/statement?month=1405-07`, null, financeToken);
  const s6Pass = s6Res.status === 200 && (Array.isArray(s6Res.body?.entries) || s6Res.body?.storeId);
  record(6, 'صدور و دریافت صورت‌حساب ماهانه فروشگاه (Monthly Statement)', 'GET', `/api/v1/finance/stores/${targetStoreId}/statement`, s6Res, s6Pass,
    s6Pass ? `صورت‌حساب ماه جاری با تراز نهایی ${Number(s6Res.body?.netBalance || 0).toLocaleString()} ریال دریافت شد` : `وضعیت HTTP ${s6Res.status}`,
    s6Pass ? null : JSON.stringify(s6Res.body));


  // 7. Settlement Queue Inspection (Pending Payouts)
  const s7Res = await request('GET', '/api/v1/finance/settlements/pending', null, financeToken);
  const s7Pass = s7Res.status === 200 && Array.isArray(s7Res.body?.settlements);
  record(7, 'مشاهده صف درخواست‌های تسویه حساب معلق (Pending Settlements)', 'GET', '/api/v1/finance/settlements/pending', s7Res, s7Pass,
    s7Pass ? `تعداد درخواست‌های در انتظار تأیید تسویه: ${s7Res.body?.settlements?.length}` : `وضعیت HTTP ${s7Res.status}`,
    s7Pass ? null : JSON.stringify(s7Res.body));

  // 8. Settlement Request Detail Verification
  const s8Res = await request('GET', '/api/v1/finance/settlements/PAYOUT-PENDING-001', null, financeToken);
  const s8Pass = s8Res.status === 200 && (s8Res.body?.id === 'PAYOUT-PENDING-001' || s8Res.body?.amount > 0);
  record(8, 'بررسی اسناد و تطبیق شماره شبای درخواست تسویه پایا', 'GET', '/api/v1/finance/settlements/PAYOUT-PENDING-001', s8Res, s8Pass,
    s8Pass ? `مبلغ: ${Number(s8Res.body?.amount).toLocaleString()} ریال - شبا: ${s8Res.body?.shabaNumber}` : `وضعیت HTTP ${s8Res.status}`,
    s8Pass ? null : JSON.stringify(s8Res.body));

  // 9. Settlement Approval & Paya Transfer Approval
  const s9Res = await request('POST', '/api/v1/finance/settlements/PAYOUT-PENDING-001/approve', {}, financeToken);
  const s9Pass = s9Res.status === 200 && s9Res.body?.status === 'approved';
  record(9, 'تأیید حواله تسویه حساب و ارسال به کارتابل پایا (Approve Payout)', 'POST', '/api/v1/finance/settlements/PAYOUT-PENDING-001/approve', s9Res, s9Pass,
    s9Pass ? 'درخواست تسویه PAYOUT-PENDING-001 تأیید شد و سند پرداخت صادر گردید' : `وضعیت HTTP ${s9Res.status}`,
    s9Pass ? null : JSON.stringify(s9Res.body));

  // 10. Settlement Rejection & Audit Trail
  const s10Res = await request('POST', '/api/v1/finance/settlements/PAYOUT-PENDING-002/reject', {
    reason: 'عدم تطابق شماره شبا با نام دارنده پروانه کسب و اطلاعات ثبت احوال'
  }, financeToken);
  const s10Pass = s10Res.status === 200 && s10Res.body?.status === 'rejected';
  record(10, 'رد درخواست تسویه مشکوک با ذکر علت کارشناسی (Reject Payout)', 'POST', '/api/v1/finance/settlements/PAYOUT-PENDING-002/reject', s10Res, s10Pass,
    s10Pass ? 'درخواست تسویه رد شد و وجه به کیف پول فروشنده عودت یافت' : `وضعیت HTTP ${s10Res.status}`,
    s10Pass ? null : JSON.stringify(s10Res.body));

  // 11. Financial Dispute Creation & Investigation
  const s11Res = await request('POST', '/api/v1/finance/disputes/create', {
    orderId: 'ord-ready-pick-001',
    amount: 250000,
    reason: 'اختلاف در محاسبه سهم کارمزد تأمین‌کننده در فاکتور مرجع',
    type: 'commission_discrepancy'
  }, financeToken);
  const s11Pass = (s11Res.status === 200 || s11Res.status === 201) && s11Res.body?.disputeId;
  if (s11Pass) createdDisputeId = s11Res.body.disputeId;
  record(11, 'ثبت پرونده اختلاف مالی و داوری کارمزد (Financial Dispute)', 'POST', '/api/v1/finance/disputes/create', s11Res, s11Pass,
    s11Pass ? `پرونده داوری با شناسه ${createdDisputeId} تشکیل و ثبت گردید` : `وضعیت HTTP ${s11Res.status}`,
    s11Pass ? null : JSON.stringify(s11Res.body));

  // 12. Financial Analytics - Platform Revenue Report
  const s12Res = await request('GET', '/api/v1/finance/reports/revenue?period=monthly', null, financeToken);
  const s12Pass = s12Res.status === 200 && (s12Res.body?.totalRevenue !== undefined || s12Res.body?.platformFees !== undefined);
  record(12, 'تولید گزارش جامع درآمد پلتفرم و تفکیک جریان نقدی (Revenue Report)', 'GET', '/api/v1/finance/reports/revenue', s12Res, s12Pass,
    s12Pass ? `درآمد ناخالص: ${Number(s12Res.body?.totalRevenue).toLocaleString()} ریال - کارمزد ناخالص پلتفرم: ${Number(s12Res.body?.platformFees).toLocaleString()} ریال` : `وضعیت HTTP ${s12Res.status}`,
    s12Pass ? null : JSON.stringify(s12Res.body));



  // Compile Summary & Write Report
  const totalSteps = testResults.length;
  const passedSteps = testResults.filter(t => t.passed).length;
  const failedSteps = totalSteps - passedSteps;
  const passRate = ((passedSteps / totalSteps) * 100).toFixed(1);

  console.log('\n================================================================');
  console.log(` FINANCE INTEGRATION TEST COMPLETED: ${passedSteps}/${totalSteps} PASSED (${passRate}%)`);
  console.log('================================================================\n');

  const report = `# گزارش جامع تست یکپارچگی چرخه حیات حسابداری و مدیریت مالی (Finance E2E Integration Test Report)

این آزمون رفتار سیستم را در چرخه کامل ۱۲ مرحله‌ای نقش **کارشناس حسابداری و مالی (Finance Specialist)** روی سرور زنده پلتفرم (\`http://localhost:8080\`) ارزیابی کرده است.

---

## ۱. مشخصات عمومی آزمون
- **کاربر آزمایش‌کننده:** \`finance_sara\` (نقش: \`finance\`)
- **تعداد کل مراحل:** ${totalSteps}
- **تعداد مراحل موفق:** ${passedSteps}
- **تعداد مراحل ناموفق:** ${failedSteps}
- **درصد موفقیت (Pass Rate):** **${passRate}%**

---

## ۲. جدول جزئیات نتایج مراحل آزمون (12 گام چرخه حیات)

| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | زمان پاسخ | نتیجه | جزئیات عملکرد |
|:---:|:---|:---:|:---|:---:|:---:|:---:|:---|
${testResults.map(t => `| ${String(t.step).padStart(2, '0')} | ${t.title} | \`${t.method}\` | \`${t.endpoint}\` | \`${t.status}\` | ${t.latency}ms | ${t.passed ? '✅ موفق' : '❌ ناموفق'} | ${t.details} |`).join('\n')}

---

## ۳. تحلیل معماری جریان عملیات مالی (Finance Architecture Breakdown)
1. **دفتر کل دوبل و اسناد حسابداری (General Ledger & Double-Entry):** استعلام تمام اسناد بدهکار/بستانکار و قابلیت فیلتر بر اساس بازه‌های زمانی.
2. **محاسبه برخط مانده و صورت‌حساب فروشگاه‌ها (Store Balance & Statements):** تفکیک مبالغ ناخالص فروش، سهم سود قطعی فروشنده، وجوه در انتظار تسویه و مانده قابل برداشت.
3. **فرآیند نظارت و تسویه حساب بانکی (Payout Settlements & Paya Pipeline):** بازبینی مدارک شبا، تأیید و ارسال دستور پرداخت به بانک، و ثبت رد با دلایل حقوقی و برگشت وجه.
4. **داوری و حل و فصل اختلافات مالی (Dispute Management):** مدیریت پرونده‌های مغایرت سهم کارمزد پلتفرم، فروشنده و تأمین‌کننده.
5. **گزارش‌گیری درآمد و کارمزد پلتفرم (Platform Revenue & Margin Reports):** تفکیک درآمد کل حاصل از گردش کالا، کارمزد اختصاصی پلتفرم و سهم واریزی هر ذینفع.
`;

  fs.writeFileSync(path.join(__dirname, 'finance-e2e-test-report.md'), report, 'utf8');
  console.log('Report generated successfully: D:\\drup\\finance-e2e-test-report.md');

  if (failedSteps > 0) {
    process.exit(1);
  }
}

runFinanceE2ETests().catch(err => {
  console.error('Test execution failed:', err);
  process.exit(1);
});

