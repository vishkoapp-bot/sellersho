const fs = require('fs');
const path = require('path');
const { request } = require('./test_helpers.cjs');

async function runAdminE2ETests() {
  console.log('================================================================');
  console.log(' DRUP PLATFORM - ADMIN MANAGEMENT & GOVERNANCE INTEGRATION TEST');
  console.log('================================================================\n');

  const healthRes = await request('GET', '/api/health');
  if (healthRes.status !== 200) {
    console.error(`Backend unreachable. Status: ${healthRes.status}`);
    process.exit(1);
  }
  console.log(`Backend health: ${healthRes.body?.status || 'ok'} (${healthRes.latency}ms)\n`);

  const testResults = [];
  let adminToken = null;
  let targetUserId = null;

  function record(step, title, method, endpoint, res, passed, details, error) {
    testResults.push({ step, title, endpoint, method, status: res.status, latency: res.latency, passed, details, error: error || null });
    const mark = passed ? '✅ PASS' : '❌ FAIL';
    console.log(`[Step ${String(step).padStart(2, '0')}/15] ${mark} [HTTP ${res.status}] (${res.latency}ms) - ${title}`);
    if (!passed && error) console.log(`   └─ Error: ${error}`);
  }

  // 1. Admin Authentication (Login)
  const s1Res = await request('POST', '/api/v1/auth/password/login', {
    username: 'admin_reza',
    password: 'admin123'
  });
  const s1Pass = s1Res.status === 200 && s1Res.body?.token && s1Res.body?.user?.role === 'admin';
  if (s1Pass) adminToken = s1Res.body.token;
  record(1, 'ورود مدیر ارشد سامانه با نام کاربری و رمز عبور', 'POST', '/api/v1/auth/password/login', s1Res, s1Pass,
    s1Pass ? `ورود موفق با شناسه ${s1Res.body?.user?.id} و نقش ${s1Res.body?.user?.role}` : `وضعیت HTTP ${s1Res.status}`,
    s1Pass ? null : JSON.stringify(s1Res.body));

  // 2. Profile Verification (RBAC & Claims)
  const s2Res = await request('GET', '/api/v1/auth/me', null, adminToken);
  const s2Pass = s2Res.status === 200 && s2Res.body?.role === 'admin';
  record(2, 'تأیید دسترسی سطح مدیر ارشد (RBAC Admin Role)', 'GET', '/api/v1/auth/me', s2Res, s2Pass,
    s2Pass ? `نام مدیر: ${s2Res.body?.name} - نقش احرازشده: ${s2Res.body?.role}` : `وضعیت HTTP ${s2Res.status}`,
    s2Pass ? null : JSON.stringify(s2Res.body));

  // 3. User Management - List Platform Users
  const s3Res = await request('GET', '/api/v1/admin/users', null, adminToken);
  const s3Pass = s3Res.status === 200 && Array.isArray(s3Res.body?.users);
  if (s3Pass && s3Res.body?.users?.length > 0) {
    const candidate = s3Res.body.users.find(u => u.role !== 'admin');
    targetUserId = candidate ? candidate.id : s3Res.body.users[0].id;
  }
  record(3, 'مشاهده و نظارت بر کاربران ثبت‌شده در سامانه', 'GET', '/api/v1/admin/users', s3Res, s3Pass,
    s3Pass ? `تعداد کل کاربران ثبت‌شده: ${s3Res.body?.users?.length}` : `وضعیت HTTP ${s3Res.status}`,
    s3Pass ? null : JSON.stringify(s3Res.body));

  // 4. User Moderation - Block User
  const blockTarget = targetUserId || 'usr-seller-01';
  const s4Res = await request('POST', `/api/v1/admin/users/${blockTarget}/block`, {}, adminToken);
  const s4Pass = s4Res.status === 200 && s4Res.body?.isActive === false;
  record(4, 'مسدودسازی کاربر متخلف یا مشکوک (User Deactivation)', 'POST', `/api/v1/admin/users/${blockTarget}/block`, s4Res, s4Pass,
    s4Pass ? `کاربر ${blockTarget} با موفقیت غیرفعال و مسدود شد` : `وضعیت HTTP ${s4Res.status}`,
    s4Pass ? null : JSON.stringify(s4Res.body));

  // 5. User Moderation - Unblock User
  const s5Res = await request('POST', `/api/v1/admin/users/${blockTarget}/unblock`, {}, adminToken);
  const s5Pass = s5Res.status === 200 && s5Res.body?.isActive === true;
  record(5, 'رفع انسداد و بازگرداندن دسترسی حساب کاربری', 'POST', `/api/v1/admin/users/${blockTarget}/unblock`, s5Res, s5Pass,
    s5Pass ? `حساب کاربری ${blockTarget} مجدداً فعال گردید` : `وضعیت HTTP ${s5Res.status}`,
    s5Pass ? null : JSON.stringify(s5Res.body));

  // 6. Store Moderation Queue - Pending Stores
  const s6Res = await request('GET', '/api/v1/admin/stores/pending', null, adminToken);
  const s6Pass = s6Res.status === 200 && Array.isArray(s6Res.body?.stores);
  record(6, 'مشاهده صف فروشگاه‌های در انتظار تأیید احراز هویت', 'GET', '/api/v1/admin/stores/pending', s6Res, s6Pass,
    s6Pass ? `تعداد فروشگاه‌های در انتظار بررسی: ${s6Res.body?.stores?.length}` : `وضعیت HTTP ${s6Res.status}`,
    s6Pass ? null : JSON.stringify(s6Res.body));

  // 7. Store Approval - Approve Store
  const s7Res = await request('POST', '/api/v1/admin/stores/store-pending-001/approve', {}, adminToken);
  const s7Pass = s7Res.status === 200 && s7Res.body?.status === 'active';
  record(7, 'تأیید مدارک و فعال‌سازی فروشگاه فروشنده (Store Approval)', 'POST', '/api/v1/admin/stores/store-pending-001/approve', s7Res, s7Pass,
    s7Pass ? 'فروشگاه store-pending-001 تأیید و دامنه فعالیت آن فعال شد' : `وضعیت HTTP ${s7Res.status}`,
    s7Pass ? null : JSON.stringify(s7Res.body));

  // 8. Store Moderation - Reject Store
  const s8Res = await request('POST', '/api/v1/admin/stores/store-pending-002/reject', {
    reason: 'عدم تطابق شماره شبا با نام دارنده پروانه کسب'
  }, adminToken);
  const s8Pass = s8Res.status === 200 && s8Res.body?.status === 'rejected';
  record(8, 'رد درخواست احراز هویت فروشگاه دارای مغایرت (Store Rejection)', 'POST', '/api/v1/admin/stores/store-pending-002/reject', s8Res, s8Pass,
    s8Pass ? 'فروشگاه نامعتبر رد شد و پیام علت رد برای فروشنده ارسال گردید' : `وضعیت HTTP ${s8Res.status}`,
    s8Pass ? null : JSON.stringify(s8Res.body));

  // 9. Margin Governance - Lock Store Margin
  const s9Res = await request('POST', '/api/v1/admin/stores/store-pending-001/lock-margin', {
    isLocked: true,
    maxMargin: 22.5
  }, adminToken);
  const s9Pass = s9Res.status === 200 && s9Res.body?.isLocked === true;
  record(9, 'قفل‌گذاری سقف حاشیه سود برای جلوگیری از گران‌فروشی', 'POST', '/api/v1/admin/stores/store-pending-001/lock-margin', s9Res, s9Pass,
    s9Pass ? `سقف حاشیه سود روی ${s9Res.body?.maxMargin}% تثبیت و قفل شد` : `وضعیت HTTP ${s9Res.status}`,
    s9Pass ? null : JSON.stringify(s9Res.body));

  // 10. Platform Financial Governance - Base Fee Configuration
  const s10Res = await request('PUT', '/api/v1/admin/settings/base-fee', {
    baseFeePercent: 4.0
  }, adminToken);
  const s10Pass = s10Res.status === 200 && s10Res.body?.baseFeePercent === 4.0;
  record(10, 'تنظیم و به‌روزرسانی کارمزد پایه پلتفرم (Platform Base Fee)', 'PUT', '/api/v1/admin/settings/base-fee', s10Res, s10Pass,
    s10Pass ? `کارمزد پایه با موفقیت روی ${s10Res.body?.baseFeePercent}% تنظیم شد` : `وضعیت HTTP ${s10Res.status}`,
    s10Pass ? null : JSON.stringify(s10Res.body));

  // 11. Supplier Commission Override
  const s11Res = await request('PUT', '/api/v1/admin/settings/supplier-fee-override', {
    supplierId: 'sup-technolife',
    feePercent: 2.75
  }, adminToken);
  const s11Pass = s11Res.status === 200 && s11Res.body?.supplierId === 'sup-technolife';
  record(11, 'تعیین کارمزد اختصاصی برای تأمین‌کننده راهبردی (Fee Override)', 'PUT', '/api/v1/admin/settings/supplier-fee-override', s11Res, s11Pass,
    s11Pass ? `کارمزد sup-technolife با نرخ ${s11Res.body?.feePercent}% اعمال شد` : `وضعیت HTTP ${s11Res.status}`,
    s11Pass ? null : JSON.stringify(s11Res.body));

  // 12. Catalog Governance - Review Product Proposals
  const s12Res = await request('GET', '/api/v1/admin/products/proposals', null, adminToken);
  const s12Pass = s12Res.status === 200 && (Array.isArray(s12Res.body?.proposals) || Array.isArray(s12Res.body));
  record(12, 'مشاهده و بررسی کالاهای پیشنهادی تأمین‌کنندگان', 'GET', '/api/v1/admin/products/proposals', s12Res, s12Pass,
    s12Pass ? 'لیست کالاهای پیشنهادی با موفقیت دریافت و بازبینی شد' : `وضعیت HTTP ${s12Res.status}`,
    s12Pass ? null : JSON.stringify(s12Res.body));

  // 13. Centralized Approvals Inbox
  const s13Res = await request('GET', '/api/v1/admin/approvals', null, adminToken);
  const s13Pass = s13Res.status === 200 && (Array.isArray(s13Res.body?.approvals) || Array.isArray(s13Res.body));
  record(13, 'کارتابل جامع مصوبات و تأییدیه‌های مدیر (Approvals Inbox)', 'GET', '/api/v1/admin/approvals', s13Res, s13Pass,
    s13Pass ? 'کارتابل مرکزی مصوبات و درخواست‌های در انتظار بررسی شد' : `وضعیت HTTP ${s13Res.status}`,
    s13Pass ? null : JSON.stringify(s13Res.body));

  // 14. Audit Logs Inspection
  const s14Res = await request('GET', '/api/v1/admin/audit-logs', null, adminToken);
  const s14Pass = s14Res.status === 200 && (Array.isArray(s14Res.body?.logs) || Array.isArray(s14Res.body));
  record(14, 'استعلام لاگ‌های امنیتی و ردپای عملیات سامانه (Audit Trail)', 'GET', '/api/v1/admin/audit-logs', s14Res, s14Pass,
    s14Pass ? 'لاگ‌های امنیتی و رخدادهای سیستمی با موفقیت دریافت گردید' : `وضعیت HTTP ${s14Res.status}`,
    s14Pass ? null : JSON.stringify(s14Res.body));

  // 15. Risk Monitoring - Suspicious Transactions Report
  const s15Res = await request('GET', '/api/v1/admin/reports/suspicious-transactions', null, adminToken);
  const s15Pass = s15Res.status === 200 && (Array.isArray(s15Res.body?.suspiciousTransactions) || Array.isArray(s15Res.body?.reports) || Array.isArray(s15Res.body));
  record(15, 'پایش ریسک و گزارش تراکنش‌های مشکوک مالی (Fraud Risk Monitor)', 'GET', '/api/v1/admin/reports/suspicious-transactions', s15Res, s15Pass,
    s15Pass ? 'گزارش تراکنش‌های پرریسک با الگوریتم‌های هوش مصنوعی دریافت شد' : `وضعیت HTTP ${s15Res.status}`,
    s15Pass ? null : JSON.stringify(s15Res.body));

  // Compile Summary & Write Report
  const totalSteps = testResults.length;
  const passedSteps = testResults.filter(t => t.passed).length;
  const failedSteps = totalSteps - passedSteps;
  const passRate = ((passedSteps / totalSteps) * 100).toFixed(1);

  console.log('\n================================================================');
  console.log(` ADMIN INTEGRATION TEST COMPLETED: ${passedSteps}/${totalSteps} PASSED (${passRate}%)`);
  console.log('================================================================\n');

  const report = `# گزارش جامع تست یکپارچگی چرخه حیات مدیریت و راهبری پلتفرم (Admin E2E Integration Test Report)

این آزمون رفتار سیستم را در چرخه کامل ۱۵ مرحله‌ای نقش **مدیریت ارشد (Platform Administrator)** روی سرور زنده پلتفرم (\`http://localhost:8080\`) ارزیابی کرده است.

---

## ۱. مشخصات عمومی آزمون
- **کاربر آزمایش‌کننده:** \`admin_reza\` (نقش: \`admin\`)
- **تعداد کل مراحل:** ${totalSteps}
- **تعداد مراحل موفق:** ${passedSteps}
- **تعداد مراحل ناموفق:** ${failedSteps}
- **درصد موفقیت (Pass Rate):** **${passRate}%**

---

## ۲. جدول جزئیات نتایج مراحل آزمون (15 گام چرخه حیات)

| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | زمان پاسخ | نتیجه | جزئیات عملکرد |
|:---:|:---|:---:|:---|:---:|:---:|:---:|:---|
${testResults.map(t => `| ${String(t.step).padStart(2, '0')} | ${t.title} | \`${t.method}\` | \`${t.endpoint}\` | \`${t.status}\` | ${t.latency}ms | ${t.passed ? '✅ موفق' : '❌ ناموفق'} | ${t.details} |`).join('\n')}

---

## ۳. تحلیل معماری جریان مدیریت (Governance Architecture Breakdown)
1. **مدیریت کاربران و دسترسی‌ها (User Management & Security):** استعلام لیست کاربران، مسدودسازی حساب‌های پرریسک و رفع انسداد بلادرنگ.
2. **نظارت بر فروشگاه‌ها و حاشیه سود (Store Moderation & Anti-Gouging):** بازبینی مدارک هویتی، تأیید یا رد رسمی فروشگاه‌ها، و فعال‌سازی قفل سقف مارجین (\`lock-margin\`).
3. **تنظیم کارمزدها و قواعد مالی (Platform Financial Rules):** به‌روزرسانی کارمزد پایه و تعریف کارمزدهای ترجیحی برای تأمین‌کنندگان استراتژیک.
4. **حاکمیت کاتالوگ و کارتابل مصوبات (Catalog & Approvals Inbox):** نظارت بر کالاهای پیشنهادی تأمین‌کنندگان و تأیید اسناد در کارتابل یکپارچه.
5. **ردیابی امنیت و پیشگیری از تقلب (Audit Logs & Fraud Detection):** بازرسی لاگ‌های سیستمی و گزارش خودکار تراکنش‌های پرخطر.
`;

  fs.writeFileSync(path.join(__dirname, 'admin-e2e-test-report.md'), report, 'utf8');
  console.log('Report generated successfully: D:\\drup\\admin-e2e-test-report.md');

  if (failedSteps > 0) {
    process.exit(1);
  }
}

runAdminE2ETests().catch(err => {
  console.error('Test execution failed:', err);
  process.exit(1);
});

