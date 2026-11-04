const assert = require('assert');
const { request } = require('./test_helpers.cjs');

async function required(method, path, body, token, expected = 200, headers = {}) {
  const response = await request(method, path, body, token, headers);
  const allowed = Array.isArray(expected) ? expected : [expected];
  if (!allowed.includes(response.status)) {
    throw new Error(`${method} ${path}: expected ${allowed.join('/')}, got ${response.status}: ${JSON.stringify(response.body)}`);
  }
  return response;
}

async function tokenFor(mobile, role) {
  const sent = await required('POST', '/api/v1/auth/otp/send', { mobile }, null, 200);
  const verified = await required('POST', '/api/v1/auth/otp/verify', { mobile, code: sent.body.demoCode, role }, null, 200);
  assert.strictEqual(verified.body.user.role, role);
  return verified.body.token;
}

async function main() {
  const suffix = String(Date.now()).slice(-8);
  const evidence = { suffix, api: {}, ids: {}, blocked: [] };
  const warehouseToken = await tokenFor('09000000007', 'warehouse_staff');
  const logisticsToken = await tokenFor('09000000044', 'logistics_specialist');
  const adminToken = await tokenFor('09000000001', 'admin');

  const bins = await required('GET', '/api/v1/warehouse/bins', null, warehouseToken);
  evidence.api.bins = { status: bins.status, count: Array.isArray(bins.body) ? bins.body.length : 0 };
  const forbiddenCapacity = await required('PUT', '/api/v1/warehouse/bins/A-12-05/capacity', { capacityVolumeCm3: 1000000, maxWeightGrams: 100000, verify: true }, warehouseToken, 403);
  evidence.api.capacityForbidden = forbiddenCapacity.status;
  const capacity = await required('PUT', '/api/v1/warehouse/bins/A-12-05/capacity', { capacityVolumeCm3: 10000000, maxWeightGrams: 100000, verify: true }, logisticsToken, 200);
  evidence.api.capacityAllowed = capacity.status;

  const inbounds = await required('GET', '/api/v1/warehouse/inbounds', null, warehouseToken);
  const inboundList = Array.isArray(inbounds.body) ? inbounds.body : inbounds.body.inbounds;
  const inbound = inboundList.find((item) => item.status === 'in_transit' && Array.isArray(item.items) && item.items.length > 0);
  if (!inbound) throw new Error('No real in_transit inbound with manifest items available');
  evidence.ids.inboundId = inbound.id;
  const manifestItem = inbound.items[0];
  await required('POST', `/api/v1/warehouse/inbounds/${encodeURIComponent(inbound.id)}/start-receiving`, {}, warehouseToken);
  await required('POST', `/api/v1/warehouse/inbounds/${encodeURIComponent(inbound.id)}/scan-item`, { productId: manifestItem.productId || manifestItem.product_id, barcode: manifestItem.barcode || inbound.id, quantity: manifestItem.quantity }, warehouseToken);
  await required('POST', `/api/v1/warehouse/inbounds/${encodeURIComponent(inbound.id)}/complete`, {}, warehouseToken);
  evidence.api.inboundLifecycle = 'start/scan/complete:200';

  const qcQueue = await required('GET', '/api/v1/warehouse/qc/queue', null, warehouseToken);
  const qcItem = (qcQueue.body.queue || []).find((item) => item.inboundId === inbound.id);
  if (!qcItem) throw new Error(`No QC queue item for ${inbound.id} after receiving`);
  await required('POST', `/api/v1/warehouse/qc/${encodeURIComponent(qcItem.productId)}/complete`, { inboundId: qcItem.inboundId, passedQuantity: Math.max(1, qcItem.receivedQuantity), failedQuantity: 0, shortageQuantity: 0, defectReason: `controlled acceptance ${suffix}` }, warehouseToken);
  evidence.ids.qcProductId = qcItem.productId;
  evidence.api.qc = 'complete:200';
  await required('POST', `/api/v1/warehouse/products/${encodeURIComponent(qcItem.productId)}/measurements`, { lengthMm: 100, widthMm: 100, heightMm: 100, weightGrams: 500, reason: `controlled measurement ${suffix}` }, warehouseToken);
  const putawayQueue = await required('GET', '/api/v1/warehouse/putaway/queue', null, warehouseToken);
  const putawayTask = (putawayQueue.body.queue || []).find((item) => item.inboundId === inbound.id && item.productId === qcItem.productId);
  if (!putawayTask) throw new Error(`No putaway task for ${qcItem.productId}`);
  evidence.ids.putawayTaskId = putawayTask.id;
  await required('POST', `/api/v1/warehouse/putaway/tasks/${encodeURIComponent(putawayTask.id)}/assign`, { binLocation: 'A-12-05' }, logisticsToken, 200);
  await required('POST', `/api/v1/warehouse/putaway/tasks/${encodeURIComponent(putawayTask.id)}/confirm`, {}, warehouseToken, 200);
  evidence.api.putaway = 'measurement/assign/confirm:200';

  const registration = await required('POST', '/api/v1/auth/supplier/register', {
    company_name: `Final Acceptance Supplier ${suffix}`,
    contact_person: 'Final Acceptance Test',
    username: `final_acceptance_${suffix}`,
    password: 'TestPass123!',
    phone: `0915${suffix}`,
    email: `final-acceptance-${suffix}@example.test`,
    warehouse_city: 'Tehran',
  }, null, [200, 201]);
  const supplierToken = registration.body.token;
  const proposal = await required('POST', '/api/v1/supplier/products/propose', { title: `Final Acceptance Product ${suffix}`, slug: `final-acceptance-${suffix}`, categorySlug: 'digital', supplierBaseCost: 100000, stock: 10, fulfillmentMethod: 'warehouse' }, supplierToken, 201);
  const productId = proposal.body.product.id;
  const approval = await required('POST', `/api/v1/admin/products/approve/${proposal.body.proposal_id}`, { approved: true, notes: `final acceptance ${suffix}` }, adminToken, 200);
  const approvedProductId = approval.body.product_id || productId;
  const returnResponse = await required('POST', '/api/v1/supplier/returns', { productId: approvedProductId, quantity: 2, collectionMethod: 'supplier_collect', reason: `final acceptance return ${suffix}` }, supplierToken, 201, { 'Idempotency-Key': `final-return-${suffix}` });
  const returnId = returnResponse.body.id;
  evidence.ids.returnId = returnId;
  const review = await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/review`, { status: 'approved', reason: `approved ${suffix}` }, warehouseToken, 200, { 'Idempotency-Key': `final-review-${suffix}` });
  const charge = 125000;
  const parcel = await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/parcels`, { quantity: 1, carrierCode: 'tipax', paymentMethod: 'sender_pays', origin: 'Tehran', destination: 'Qom', lengthMm: 200, widthMm: 150, heightMm: 100, weightGrams: 1800, finalCharge: charge, finalChargeSource: 'manual', finalChargeReference: `manual-${suffix}`, notes: `internal tariff/manual acceptance ${suffix}` }, warehouseToken, 200, { 'Idempotency-Key': `final-parcel-${suffix}` });
  evidence.ids.parcelId = parcel.body.id;
  await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/parcels/${encodeURIComponent(parcel.body.id)}/confirm-charge`, {}, warehouseToken, 200, { 'Idempotency-Key': `final-charge-${suffix}` });
  await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/handover`, {}, warehouseToken, 200, { 'Idempotency-Key': `final-handover-${suffix}` });
  await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/receive`, {}, warehouseToken, 200, { 'Idempotency-Key': `final-receive-${suffix}` });
  await required('POST', `/api/v1/warehouse/returns/${encodeURIComponent(returnId)}/inspect`, { acceptedQuantity: 1, failedQuantity: 1, reason: `partial QC ${suffix}` }, warehouseToken, 200, { 'Idempotency-Key': `final-inspect-${suffix}` });
  const returns = await required('GET', '/api/v1/warehouse/returns', null, warehouseToken);
  const finalReturn = (returns.body.returns || []).find((item) => item.id === returnId);
  evidence.api.returnLifecycle = 'review/parcel/charge/handover/receive/partial-qc:200';
  evidence.finalStatus = finalReturn && { status: finalReturn.status, inspectionStatus: finalReturn.inspectionStatus, acceptedQuantity: finalReturn.acceptedQuantity, failedQuantity: finalReturn.failedQuantity };

  const carriers = await required('GET', '/api/v1/warehouse/carriers', null, warehouseToken);
  evidence.carriers = carriers.body.carriers;
  if (!evidence.carriers.some((carrier) => carrier.supportsReceiverPays)) evidence.blocked.push('receiver-paid: no enabled carrier supports receiver_pays');
  console.log(JSON.stringify(evidence, null, 2));
}

main().catch((error) => { console.error(error.stack || error.message); process.exit(1); });