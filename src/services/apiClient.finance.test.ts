import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { apiRequest, ApiError } from './apiClient';

test('a declared JSON success with a truncated body is an ambiguous transport error', async () => {
  const fetch = mock.method(globalThis, 'fetch', async () => new Response('{"id":', { status: 200, headers: { 'Content-Type': 'application/json' } }));
  try {
    await assert.rejects(apiRequest('/financial-command', { method: 'POST' }), (error: unknown) => error instanceof ApiError && error.status === 0);
  } finally { fetch.mock.restore(); }
});

test('a valid no-content success keeps its existing null result', async () => {
  const fetch = mock.method(globalThis, 'fetch', async () => new Response(null, { status: 204, headers: { 'Content-Type': 'application/json' } }));
  try { assert.equal(await apiRequest('/no-content', { method: 'POST' }), null); }
  finally { fetch.mock.restore(); }
});
