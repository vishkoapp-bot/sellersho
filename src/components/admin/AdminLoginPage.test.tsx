import assert from 'node:assert/strict';
import { test } from 'node:test';
import { normalizeOtpInput } from './AdminLoginPage';

test('normalizeOtpInput converts Persian and Arabic digits before stripping', () => {
  assert.equal(normalizeOtpInput('۱۲۳۴'), '1234');
  assert.equal(normalizeOtpInput('١٢٣٤'), '1234');
  assert.equal(normalizeOtpInput('۱2۳4'), '1234');
  assert.equal(normalizeOtpInput('۱۲۳۴۵'), '1234');
  assert.equal(normalizeOtpInput('۱۲a۳'), '123');
  assert.equal(normalizeOtpInput(''), '');
  assert.equal(normalizeOtpInput('1234'), '1234');
});
