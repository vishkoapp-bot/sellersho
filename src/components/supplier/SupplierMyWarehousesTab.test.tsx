import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { it } from 'node:test';
import assert from 'node:assert/strict';
import { ProvinceCityPicker, geoApi, normalizeGeoName } from '../ProvinceCityPicker';
import { ToastProvider } from '../../context/ToastContext';
import {
  SupplierMyWarehousesTab,
  applyWarehouseProvinceChange,
  warehouseFormFromItem,
  type WarehouseForm,
} from './SupplierNewTabs';

// ITEM 5: the warehouse form reuses the existing ProvinceCityPicker
// (GET /geo/provinces + /geo/cities, auth-protected, dataset-backed) instead
// of the old free-text province/city inputs. No public geo routes, no
// hard-coded list, payload stays Persian name strings.
//
// Harness limit (same as SupplierInventoryTab.test.tsx): tsx --test +
// renderToStaticMarkup has no DOM/jsdom, so effects never run — the picker
// cannot load its province list here and renders the free-text fallback,
// and no click can open the warehouse form or pick a province. What IS
// covered here: the geoApi fetch contract (stubbed fetch), the fallback
// render shape, the province-change rule (list pick clears a stale city),
// and the edit-mode prefill. The live click-through (pick province ->
// cities load -> save -> card shows the picked names) stays owed to a DOM
// harness or the user's device.

// --- geoApi contract (fetch stubbed; apiRequest builds /api/v1 + auth) ---

const stubFetch = (payload: unknown) => {
  const calls: Array<{ url: string; init: any }> = [];
  const prev = globalThis.fetch;
  (globalThis as any).fetch = async (url: string, init: any) => {
    calls.push({ url, init });
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => payload,
    };
  };
  return { calls, restore: () => { (globalThis as any).fetch = prev; } };
};

it('geoApi.provinces hits GET /api/v1/geo/provinces and maps available + items', async () => {
  const { calls, restore } = stubFetch({
    available: true,
    provinces: [
      { code: 1, name: 'تهران' },
      { code: 26, name: 'قم' },
    ],
  });
  try {
    const res = await geoApi.provinces();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/api/v1/geo/provinces');
    assert.equal(calls[0].init.method, 'GET');
    assert.equal(res.available, true);
    assert.deepEqual(res.items, [
      { code: 1, name: 'تهران' },
      { code: 26, name: 'قم' },
    ]);
  } finally {
    restore();
  }
});

it('geoApi.cities sends the province as a JSON-encoded Tapin code', async () => {
  const { calls, restore } = stubFetch({
    available: true,
    cities: [{ code: 101, name: 'کرج' }],
  });
  try {
    const res = await geoApi.cities(1);
    assert.equal(calls[0].url, '/api/v1/geo/cities?province=1');
    assert.deepEqual(res.items, [{ code: 101, name: 'کرج' }]);
  } finally {
    restore();
  }
  // A string code is JSON-encoded too (province="1"), per the handler contract.
  const second = stubFetch({ available: true, cities: [] });
  try {
    await geoApi.cities('1');
    assert.equal(second.calls[0].url, '/api/v1/geo/cities?province=%221%22');
  } finally {
    second.restore();
  }
});

it('geoApi passes available:false through so the picker degrades to free text', async () => {
  const { restore } = stubFetch({ available: false, provinces: [] });
  try {
    const res = await geoApi.provinces();
    assert.equal(res.available, false);
    assert.deepEqual(res.items, []);
  } finally {
    restore();
  }
});

// --- Picker fallback render (effects do not run under renderToStaticMarkup,
// so provinces stay null — the same branch as an unavailable dataset) ---

const renderPicker = (province: string, city: string) =>
  renderToStaticMarkup(
    <ProvinceCityPicker
      province={province}
      city={city}
      onProvinceChange={() => {}}
      onCityChange={() => {}}
      inputClassName="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs font-normal"
      provinceId="supplier-warehouse-province"
      cityId="supplier-warehouse-city"
    />,
  );

it('picker falls back to free-text inputs with the bound values when no dataset is loaded', () => {
  const html = renderPicker('تهران', 'کرج');
  assert.match(html, /id="supplier-warehouse-province"/);
  assert.match(html, /id="supplier-warehouse-city"/);
  assert.match(html, /aria-label="استان"/);
  assert.match(html, /aria-label="شهر"/);
  assert.match(html, /value="تهران"/);
  assert.match(html, /value="کرج"/);
  // No <select> while the dataset is unavailable — free text only.
  assert.doesNotMatch(html, /<select/);
});

it('picker keeps its sr-only labels and empty values for a fresh warehouse', () => {
  const html = renderPicker('', '');
  assert.match(html, /class="sr-only">استان</);
  assert.match(html, /class="sr-only">شهر</);
  assert.match(html, /value=""/);
});

// --- Wiring rules (pure helpers used by the tab's picker callbacks) ---

const filledForm: WarehouseForm = {
  name: 'انبار کرج',
  provinceName: 'تهران',
  cityName: 'کرج',
  address: '',
  postalCode: '',
  phone: '',
};

it('a province picked from the list clears a stale city; free text keeps it', () => {
  assert.deepEqual(applyWarehouseProvinceChange(filledForm, 'قم', true), {
    ...filledForm,
    provinceName: 'قم',
    cityName: '',
  });
  assert.deepEqual(applyWarehouseProvinceChange(filledForm, 'تهران', false), {
    ...filledForm,
    provinceName: 'تهران',
    cityName: 'کرج',
  });
});

it('edit mode copies the saved Persian province/city names into the form', () => {
  assert.deepEqual(
    warehouseFormFromItem({
      id: 'w1',
      name: 'انبار کرج',
      provinceName: 'تهران',
      cityName: 'کرج',
      address: 'جاده مخصوص',
      postalCode: '1234567890',
      phone: '09120000000',
    }),
    {
      name: 'انبار کرج',
      provinceName: 'تهران',
      cityName: 'کرج',
      address: 'جاده مخصوص',
      postalCode: '1234567890',
      phone: '09120000000',
    },
  );
  // Missing optional fields default to "" so the form stays controlled.
  assert.deepEqual(
    warehouseFormFromItem({ name: 'انبار قم', provinceName: 'قم', cityName: 'قم' }),
    { name: 'انبار قم', provinceName: 'قم', cityName: 'قم', address: '', postalCode: '', phone: '' },
  );
});

it('normalizeGeoName matches saved names despite Arabic Yeh/Kaf variants', () => {
  // The picker resolves edit-mode preselection by this name match.
  assert.equal(normalizeGeoName('تهران'), 'تهران');
  assert.equal(normalizeGeoName('تهران'), normalizeGeoName('تهران'));
  assert.equal(normalizeGeoName('يزد'), 'یزد');
  assert.equal(normalizeGeoName('اراك'), 'اراک');
});

// --- Tab mount (form starts closed; picker appears only after the form opens) ---

it('warehouse tab mounts with the new picker import and shows create + loading state', () => {
  const html = renderToStaticMarkup(
    <ToastProvider>
      <SupplierMyWarehousesTab />
    </ToastProvider>,
  );
  assert.match(html, /انبارهای من/);
  assert.match(html, /ثبت انبار جدید/);
  // loading starts true and the load() effect never runs under
  // renderToStaticMarkup, so the empty state is not reached here.
  assert.match(html, /role="status"[^>]*>در حال دریافت اطلاعات…/);
});
