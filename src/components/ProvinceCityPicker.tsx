import React, { useCallback, useEffect, useRef, useState } from 'react';
import { apiRequest } from '../services/apiClient';

// Worker A — live parity (index-MERGED.js, dC picker + Lr geo client).
// Dataset SOURCE (proven, never invented): GET /geo/provinces (auth) ->
// {available, provinces: [{code, name}]}, GET /geo/cities?province=<JSON
// code> (auth) -> {available, cities: [{code, name}]}. Live public probe:
// GET /api/v1/geo/provinces without a token returns 401, matching the
// bundle's auth:true. When the dataset is unavailable the picker degrades
// to free-text inputs, exactly like live — no list is hard-coded here.

export const toLatinDigits = (value: string): string =>
  (value || '')
    .replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 1776))
    .replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 1632));

// Live eo(): Arabic Yeh/Kaf -> Persian, tabs stripped, trimmed.
export const normalizeGeoName = (value: string): string =>
  (value || '')
    .replace(/ي/g, 'ی')
    .replace(/ك/g, 'ک')
    .replace(/\t/g, '')
    .trim();

// Live td(): city keys drop any "-suffix" before matching.
const cityKey = (value: string): string => normalizeGeoName(value).split('-')[0].trim();

export interface GeoItem {
  code: unknown;
  name: string;
}

export const geoApi = {
  provinces: async (): Promise<{ available: boolean; items: GeoItem[] }> => {
    const data = await apiRequest<{ available?: boolean; provinces?: GeoItem[] }>('/geo/provinces', {
      method: 'GET',
      auth: true,
    });
    return {
      available: !!(data as any)?.available,
      items: Array.isArray((data as any)?.provinces) ? (data as any).provinces : [],
    };
  },
  cities: async (provinceCode: unknown): Promise<{ available: boolean; items: GeoItem[] }> => {
    const params = new URLSearchParams({ province: JSON.stringify(provinceCode) });
    const data = await apiRequest<{ available?: boolean; cities?: GeoItem[] }>(`/geo/cities?${params}`, {
      method: 'GET',
      auth: true,
    });
    return {
      available: !!(data as any)?.available,
      items: Array.isArray((data as any)?.cities) ? (data as any).cities : [],
    };
  },
};

interface ProvinceCityPickerProps {
  province: string;
  city: string;
  onProvinceChange: (value: string, fromList: boolean) => void;
  onCityChange: (value: string) => void;
  inputClassName: string;
  inputStyle?: React.CSSProperties;
  provinceId?: string;
  cityId?: string;
  labelClassName?: string;
  onProvinceNodeChange?: (node: GeoItem | null) => void;
  onCityNodeChange?: (node: GeoItem | null) => void;
}

export const ProvinceCityPicker: React.FC<ProvinceCityPickerProps> = ({
  province,
  city,
  onProvinceChange,
  onCityChange,
  inputClassName,
  inputStyle,
  provinceId,
  cityId,
  labelClassName,
  onProvinceNodeChange,
  onCityNodeChange,
}) => {
  const [provinces, setProvinces] = useState<GeoItem[] | null>(null);
  const [cities, setCities] = useState<GeoItem[] | null>(null);
  const [citiesLoading, setCitiesLoading] = useState(false);
  const [provinceCode, setProvinceCode] = useState<unknown>(undefined);
  const requestCounter = useRef(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await geoApi.provinces();
        if (!cancelled) setProvinces(res.available ? res.items : []);
      } catch {
        if (!cancelled) setProvinces([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const loadCities = useCallback(async (code: unknown) => {
    const requestId = ++requestCounter.current;
    setCities(null);
    setCitiesLoading(true);
    try {
      const res = await geoApi.cities(code);
      if (requestCounter.current === requestId) setCities(res.available ? res.items : []);
    } catch {
      if (requestCounter.current === requestId) setCities([]);
    } finally {
      if (requestCounter.current === requestId) setCitiesLoading(false);
    }
  }, []);

  // Live dC: when the typed province matches a dataset node, resolve its
  // code and load its cities; a non-matching province clears the code.
  useEffect(() => {
    if (!provinces || provinces.length === 0) return;
    const matched = provinces.find((item) => normalizeGeoName(item.name) === normalizeGeoName(province));
    const code = matched?.code;
    if (code === undefined) {
      if (provinceCode !== undefined) setProvinceCode(undefined);
      return;
    }
    if (JSON.stringify(code) !== JSON.stringify(provinceCode)) {
      setProvinceCode(code);
      void loadCities(code);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [province, provinces]);

  const hasProvinces = provinces !== null && provinces.length > 0;
  const matchedProvince = hasProvinces
    ? provinces!.find((item) => normalizeGeoName(item.name) === normalizeGeoName(province))
    : undefined;
  const hasCities = cities !== null && cities.length > 0;
  const matchedCity = hasCities
    ? cities!.find((item) => cityKey(item.name) === cityKey(city))
    : undefined;

  const handleProvinceSelect = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value;
    if (value === '') {
      onProvinceChange('', true);
      onProvinceNodeChange?.(null);
      return;
    }
    const node = (provinces || []).find((item) => JSON.stringify(item.code) === value);
    if (node) {
      onProvinceChange(normalizeGeoName(node.name), true);
      onProvinceNodeChange?.(node);
    }
  };

  const handleCitySelect = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const value = e.target.value;
    if (value === '') {
      onCityChange('');
      onCityNodeChange?.(null);
      return;
    }
    const node = (cities || []).find((item) => JSON.stringify(item.code) === value);
    if (node) {
      onCityChange(cityKey(node.name));
      onCityNodeChange?.(node);
    }
  };

  // Live dC: the province is a free-text input when there is no dataset,
  // or when the current text matches no dataset node.
  const provinceControl =
    !hasProvinces || (province !== '' && !matchedProvince) ? (
      <input
        id={provinceId}
        type="text"
        aria-label="استان"
        placeholder="استان"
        value={province}
        onChange={(e) => onProvinceChange(e.target.value, false)}
        className={inputClassName}
        style={inputStyle}
      />
    ) : (
      <select
        id={provinceId}
        aria-label="استان"
        value={matchedProvince ? JSON.stringify(matchedProvince.code) : ''}
        onChange={handleProvinceSelect}
        className={inputClassName}
        style={inputStyle}
      >
        <option value="">استان</option>
        {(provinces || []).map((item) => (
          <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
            {normalizeGeoName(item.name)}
          </option>
        ))}
      </select>
    );

  let cityControl: React.ReactNode;
  if (hasProvinces) {
    if (matchedProvince) {
      if (citiesLoading || cities === null) {
        cityControl = (
          <select
            id={cityId}
            aria-label="شهر"
            value=""
            disabled
            onChange={() => {}}
            className={inputClassName}
            style={inputStyle}
          >
            <option value="">در حال بارگذاری شهرها…</option>
          </select>
        );
      } else if (!hasCities || (city !== '' && !matchedCity)) {
        cityControl = (
          <input
            id={cityId}
            type="text"
            aria-label="شهر"
            placeholder="شهر *"
            value={city}
            onChange={(e) => onCityChange(e.target.value)}
            className={inputClassName}
            style={inputStyle}
          />
        );
      } else {
        cityControl = (
          <select
            id={cityId}
            aria-label="شهر"
            value={matchedCity ? JSON.stringify(matchedCity.code) : ''}
            onChange={handleCitySelect}
            className={inputClassName}
            style={inputStyle}
          >
            <option value="">شهر *</option>
            {(cities || []).map((item) => (
              <option key={JSON.stringify(item.code)} value={JSON.stringify(item.code)}>
                {cityKey(item.name)}
              </option>
            ))}
          </select>
        );
      }
    } else if (province === '') {
      cityControl = (
        <select
          id={cityId}
          aria-label="شهر"
          value=""
          disabled
          onChange={() => {}}
          className={inputClassName}
          style={inputStyle}
        >
          <option value="">اول استان را انتخاب کنید</option>
        </select>
      );
    } else {
      cityControl = (
        <input
          id={cityId}
          type="text"
          aria-label="شهر"
          placeholder="شهر *"
          value={city}
          onChange={(e) => onCityChange(e.target.value)}
          className={inputClassName}
          style={inputStyle}
        />
      );
    }
  } else {
    cityControl = (
      <input
        id={cityId}
        type="text"
        aria-label="شهر"
        placeholder="شهر *"
        value={city}
        onChange={(e) => onCityChange(e.target.value)}
        className={inputClassName}
        style={inputStyle}
      />
    );
  }

  if (labelClassName) {
    return (
      <>
        <div className="min-w-0"><label className={labelClassName} htmlFor={provinceId}>استان</label>{provinceControl}</div>
        <div className="min-w-0"><label className={labelClassName} htmlFor={cityId}>شهر</label>{cityControl}</div>
      </>
    );
  }

  return (
    <>
      {provinceId ? (
        <label htmlFor={provinceId} className="sr-only">
          استان
        </label>
      ) : null}
      {provinceControl}
      {cityId ? (
        <label htmlFor={cityId} className="sr-only">
          شهر
        </label>
      ) : null}
      {cityControl}
    </>
  );
};
