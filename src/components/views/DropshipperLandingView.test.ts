import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { openSellerOnboarding } from './DropshipperLandingView';

describe('seller onboarding entry point', () => {
  it('opens onboarding without changing the active workspace', () => {
    const calls: boolean[] = [];

    openSellerOnboarding((open) => calls.push(open));

    assert.deepEqual(calls, [true]);
  });
});