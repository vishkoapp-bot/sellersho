import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { apiRequest, ApiError, toUserFacingErrorMessage } from './apiClient';

function stubFetch(status: number, contentType: string, body: string) {
  return mock.method(globalThis, 'fetch', async () => new Response(body, {
    status,
    headers: { 'content-type': contentType },
  }));
}

test('PR-3 raw-JSON 403 becomes Persian, never the JSON literal', async (t) => {
  const body = '{"error":"Resource does not belong to your supplier"}';
  const fake = stubFetch(403, 'text/plain', body);
  t.after(() => fake.mock.restore());

  await assert.rejects(apiRequest('/supplier-test'), (error: unknown) => {
    assert.ok(error instanceof ApiError);
    assert.equal(error.status, 403);
    assert.equal(error.message, 'دسترسی به این مورد ممکن نیست.');
    assert.doesNotMatch(error.message, /\{|"error"|[A-Za-z]+/);
    assert.equal(error.body, body);
    return true;
  });
});

test('OR-9 raw-JSON 409 becomes Persian', async (t) => {
  const fake = stubFetch(409, 'text/plain', '{"error":"Shipment is not ready for shipping"}');
  t.after(() => fake.mock.restore());

  await assert.rejects(apiRequest('/supplier-test'), {
    message: 'این سفارش آماده ارسال نیست.',
  });
});

test('unknown English error string falls back to generic Persian', async (t) => {
  const fake = stubFetch(500, 'application/json', '{"error":"Something new broke"}');
  t.after(() => fake.mock.restore());

  await assert.rejects(apiRequest('/supplier-test'), {
    message: 'خطایی رخ داد؛ دوباره تلاش کنید.',
  });
});

test('unparseable plain-text body falls back to generic Persian', async (t) => {
  const fake = stubFetch(404, 'text/plain', '404 page not found');
  t.after(() => fake.mock.restore());

  await assert.rejects(apiRequest('/supplier-test'), {
    message: 'خطایی رخ داد؛ دوباره تلاش کنید.',
  });
});

test('already-Persian backend message passes through', async (t) => {
  const fake = stubFetch(409, 'application/json', '{"error":"موجودی کافی نیست."}');
  t.after(() => fake.mock.restore());

  await assert.rejects(apiRequest('/supplier-test'), {
    message: 'موجودی کافی نیست.',
  });
});

test('toUserFacingErrorMessage maps known errors and handles missing or invalid bodies', () => {
  const cases: Array<[unknown, number, string]> = [
    [{ error: 'Resource does not belong to your supplier' }, 403, 'دسترسی به این مورد ممکن نیست.'],
    [{ error: 'Shipment is not ready for shipping' }, 409, 'این سفارش آماده ارسال نیست.'],
    [{ error: 'Unauthorized' }, 401, 'نشست شما تمام شده؛ دوباره وارد شوید.'],
    [{ error: 'Supplier profile not found' }, 404, 'پروفایل تأمین‌کننده پیدا نشد.'],
    [{ error: 'Supplier scope required' }, 403, 'دسترسی تأمین‌کننده برقرار نیست.'],
    ['{"message":"Unauthorized"}', 401, 'نشست شما تمام شده؛ دوباره وارد شوید.'],
    ['404 page not found', 404, 'خطایی رخ داد؛ دوباره تلاش کنید.'],
    [{ error: 'shipment_source_stock_insufficient' }, 409, 'موجودی قابل رزرو در انبار مبدأ برای تعداد واردشده کافی نیست.'],
    [null, 500, 'خطایی رخ داد؛ دوباره تلاش کنید.'],
    [undefined, 500, 'خطایی رخ داد؛ دوباره تلاش کنید.'],
  ];

  for (const [raw, status, expected] of cases) {
    assert.equal(toUserFacingErrorMessage(raw, status), expected);
  }
});

test('toUserFacingErrorMessage maps a string message when error is an object', () => {
  assert.equal(
    toUserFacingErrorMessage({ error: {}, message: 'Shipment is not ready for shipping' }, 409),
    'این سفارش آماده ارسال نیست.',
  );
});
