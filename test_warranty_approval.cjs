const { request, getAuthToken } = require('./test_helpers.cjs');

async function main() {
  const supplier = await request('POST', '/api/v1/auth/otp/send', { mobile: '09165605691' });
  const supplierAuth = await request('POST', '/api/v1/auth/otp/verify', { mobile: '09165605691', code: supplier.body.demoCode || '12345', role: 'supplier' });
  const supplierToken = supplierAuth.body.token;
  const before = await request('GET', '/api/v1/supplier/warranties', null, supplierToken);
  const adminToken = await getAuthToken('09000000001');
  const approvals = await request('GET', '/api/v1/admin/approvals?status=pending', null, adminToken);
  const item = (approvals.body || []).find((entry) => entry.entityType === 'warranty_property' && entry.status === 'pending');
  if (!item) throw new Error('pending warranty approval not found');
  const review = await request('POST', `/api/v1/admin/approvals/${item.id}/review`, { status: 'approved' }, adminToken);
  const after = await request('GET', '/api/v1/supplier/warranties', null, supplierToken);
  const approved = (after.body || []).some((entry) => entry.name === 'گارانتی کامل 5605691' && entry.status === 'active');
  console.log(JSON.stringify({ before: before.body?.length, approvalStatus: review.status, approvalBody: review.body, after: after.body?.length, approved }));
  if (review.status !== 200 || !approved) process.exitCode = 1;
}

main().catch((error) => { console.error(error.message); process.exitCode = 1; });