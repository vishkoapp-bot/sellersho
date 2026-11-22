import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storeSettingsSaveErrorMessage } from './SellerStoreSetupTab';

// G2 (Batch 4b): a failed save must toast Persian copy, never the
// backend's raw English text. The tab itself cannot render in this
// harness (useStore throws without the provider tree), so the exported
// mapping the save catch hinges on is pinned directly — the
// SellerDomainsPanel precedent.

test('a 400 validation failure maps to the Persian validation message, never raw backend text', () => {
  const msg = storeSettingsSaveErrorMessage({ status: 400, message: 'Invalid input' });
  assert.equal(msg, 'ذخیره انجام نشد؛ مقدار نامعتبر است.');
  assert.doesNotMatch(msg, /Invalid input/);
  assert.doesNotMatch(msg, /[{}"]/);
  // A bare 'Invalid input' message (no status) maps the same way.
  assert.equal(storeSettingsSaveErrorMessage({ message: 'Invalid input' }), 'ذخیره انجام نشد؛ مقدار نامعتبر است.');
  assert.equal(storeSettingsSaveErrorMessage({ status: 400, message: 'خطای سرور با کد وضعیت 400' }), 'ذخیره انجام نشد؛ مقدار نامعتبر است.');
});

test('non-validation failures keep the generic retry copy', () => {
  assert.equal(storeSettingsSaveErrorMessage({ status: 500, message: 'boom' }), 'ذخیره انجام نشد. دوباره تلاش کن.');
  assert.equal(storeSettingsSaveErrorMessage({ status: 0, message: 'پاسخی از سرور دریافت نشد؛ لطفاً دوباره تلاش کنید.' }), 'ذخیره انجام نشد. دوباره تلاش کن.');
  assert.equal(storeSettingsSaveErrorMessage(undefined), 'ذخیره انجام نشد. دوباره تلاش کن.');
  assert.equal(storeSettingsSaveErrorMessage(null), 'ذخیره انجام نشد. دوباره تلاش کن.');
  assert.equal(storeSettingsSaveErrorMessage(new Error('network down')), 'ذخیره انجام نشد. دوباره تلاش کن.');
});
