import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveSubscriptionPlans, resolvePlanFeatures, resolveTrialQuota } from './SellerSubscriptionTab';
import type { PlatformSettings, SubscriptionPlan } from '../../types';

// G7 (Batch 3 fix, M-3): the hardening guards as exported pure helpers,
// pinned on the Batch 1 pattern (SellerDomainsPanel.test.tsx /
// SellerDiscountTab.test.ts — the tab itself cannot render in this
// harness, so the exported rule the render hinges on is pinned
// directly).
//
// NOTE — why there is no render test, with file:line evidence: the spec
// asked for "render SellerSubscriptionTab with currentStore = null,
// subscriptionPlans = undefined, and a plan with features missing;
// assert no throw". The currentStore = null case CANNOT be written at
// tab level: useStore() throws 'A store must be selected before
// consuming the store context' (src/context/StoreContext.tsx:1343-1345,
// `if (!store.currentStore) { throw … }`) before the tab body runs,
// and currentStore is typed non-null
// (src/context/storeContextTypes.ts:115, `currentStore: Store`). A
// render test would throw in the hook, not in the tab — it would pin
// the hook, not the guards. The subscriptionPlans-undefined and
// features-missing cases ARE pinned here, at the level that can
// actually throw (the resolvers the tab renders through). The live
// repro of the exact throwing line (window.__sellershoLastError) and
// the targeted fix remain the open parent item; this test does not
// claim to reproduce it.

const plan = (overrides: Partial<SubscriptionPlan> = {}): SubscriptionPlan => ({
  id: 'trial',
  name: 'آزمایشی',
  durationDays: 30,
  durationLabel: 'یک ماه',
  price: 0,
  productLimit: 25,
  allowedCustomDomains: 0,
  maxStores: 1,
  features: ['۱۰۰ قلم کالا'],
  ...overrides,
});

const settingsWith = (plans: unknown): PlatformSettings =>
  ({ subscriptionPlans: plans }) as unknown as PlatformSettings;

test('resolveSubscriptionPlans passes a real plans array through unchanged', () => {
  const plans = [plan(), plan({ id: 'vip', name: 'طلایی' })];
  assert.deepEqual(resolveSubscriptionPlans(settingsWith(plans)), plans);
});

test('resolveSubscriptionPlans returns [] for undefined/null settings, a missing field, or a non-array — never a throw', () => {
  assert.deepEqual(resolveSubscriptionPlans(undefined), []);
  assert.deepEqual(resolveSubscriptionPlans(null), []);
  assert.deepEqual(resolveSubscriptionPlans({} as unknown as PlatformSettings), []);
  assert.deepEqual(resolveSubscriptionPlans(settingsWith(undefined)), []);
  assert.deepEqual(resolveSubscriptionPlans(settingsWith(null)), []);
  assert.deepEqual(resolveSubscriptionPlans(settingsWith('error-page')), []);
  assert.deepEqual(resolveSubscriptionPlans(settingsWith({})), []);
});

test('resolvePlanFeatures passes a plan features array through; missing, null, or non-array features become []', () => {
  assert.deepEqual(resolvePlanFeatures(plan()), ['۱۰۰ قلم کالا']);
  assert.deepEqual(resolvePlanFeatures(undefined), []);
  assert.deepEqual(resolvePlanFeatures(null), []);
  assert.deepEqual(resolvePlanFeatures({ ...plan(), features: undefined } as unknown as SubscriptionPlan), []);
  assert.deepEqual(resolvePlanFeatures({ ...plan(), features: null as unknown as string[] }), []);
  assert.deepEqual(resolvePlanFeatures({ ...plan(), features: 'x' as unknown as string[] }), []);
});

test('resolveTrialQuota returns the configured trial quota, and 25 (the INITIAL_PLATFORM_SETTINGS default) for missing/non-numeric limits', () => {
  const withTrial = (trial: unknown): PlatformSettings =>
    ({ subscriptionPlanLimits: { trial } }) as unknown as PlatformSettings;
  assert.equal(resolveTrialQuota(withTrial(40)), 40);
  assert.equal(resolveTrialQuota(withTrial(25)), 25);
  assert.equal(resolveTrialQuota(undefined), 25);
  assert.equal(resolveTrialQuota(null), 25);
  assert.equal(resolveTrialQuota({} as unknown as PlatformSettings), 25);
  assert.equal(resolveTrialQuota({ subscriptionPlanLimits: undefined } as unknown as PlatformSettings), 25);
  assert.equal(resolveTrialQuota({ subscriptionPlanLimits: {} } as unknown as PlatformSettings), 25);
  assert.equal(resolveTrialQuota(withTrial('x')), 25);
  assert.equal(resolveTrialQuota(withTrial(NaN)), 25);
});
