// OBS-01 (D-08): frontend performance counts — browser-local measurement.
//
// Dependency-free PerformanceObserver measurement of the core web vitals
// (LCP, CLS, INP approximation) plus navigation timing (TTFB, DOMContentLoaded).
// Results are exposed ONLY locally, on `window.__sellershoWebVitals`, and
// mirrored to `console.debug` — there is NO network send, NO backend
// endpoint, NO user identifier, and NO aggregation anywhere. That is the
// deliberate scope of this change: counts exist in the browser, but they
// are not yet collected into Prometheus/Grafana.
//
// Follow-up (owning task, OBS-01 follow-up — frontend telemetry ingest):
// a backend endpoint (e.g. POST /api/v1/telemetry/web-vitals) that accepts
// these values with the page's X-Request-ID, so they can be aggregated per
// route class. Until it lands, the dashboard's nginx panel notes that RUM
// is browser-local, and G-09's "frontend performance counts exist" is met
// only in this local sense — recorded honestly in obs01-platform-worker.md.

export interface WebVitals {
  /** Largest Contentful Paint, ms. */
  lcpMs: number | null;
  /** Cumulative Layout Shift, session-windowed. */
  cls: number | null;
  /** Worst observed interaction latency, ms (INP approximation: max event duration, not p98). */
  inpMs: number | null;
  /** Time to first byte, ms. */
  ttfbMs: number | null;
  /** DOMContentLoaded, ms since navigation start. */
  dclMs: number | null;
  updatedAt: string | null;
}

declare global {
  interface Window {
    __sellershoWebVitals?: WebVitals;
  }
}

const vitals: WebVitals = {
  lcpMs: null,
  cls: null,
  inpMs: null,
  ttfbMs: null,
  dclMs: null,
  updatedAt: null,
};

function publish(): void {
  vitals.updatedAt = new Date().toISOString();
  window.__sellershoWebVitals = { ...vitals };
  // eslint-disable-next-line no-console
  console.debug('[web-vitals]', vitals);
}

function observe(
  type: string,
  onEntry: (entry: PerformanceEntry) => void,
  options?: PerformanceObserverInit,
): void {
  try {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) onEntry(entry);
      publish();
    });
    observer.observe({ type, buffered: true, ...(options ?? {}) });
  } catch {
    // PerformanceObserver type unsupported in this browser: that metric
    // simply stays null; measurement must never break the page.
  }
}

export function initWebVitals(): void {
  try {
    if (typeof PerformanceObserver === 'undefined' || typeof window === 'undefined') return;

    // LCP: the last largest-contentful-paint entry wins.
    observe('largest-contentful-paint', (entry) => {
      vitals.lcpMs = Math.round(entry.startTime);
    });

    // CLS: session window — accumulate shifts, reset after a 5 s gap.
    let sessionValue = 0;
    let sessionStart = 0;
    observe('layout-shift', (entry) => {
      const shift = entry as PerformanceEntry & { value?: number; hadRecentInput?: boolean };
      if (shift.hadRecentInput || typeof shift.value !== 'number') return;
      if (sessionStart === 0 || entry.startTime - sessionStart > 5000) {
        sessionStart = entry.startTime;
        sessionValue = 0;
      }
      sessionValue += shift.value;
      vitals.cls = Math.round(sessionValue * 1000) / 1000;
    });

    // INP approximation: worst event-timing duration observed.
    observe('event', (entry) => {
      const duration = (entry as PerformanceEntry & { duration?: number }).duration;
      if (typeof duration !== 'number') return;
      vitals.inpMs = vitals.inpMs === null ? Math.round(duration) : Math.max(vitals.inpMs, Math.round(duration));
    });

    // Navigation timing: TTFB + DOMContentLoaded (single-shot).
    const nav = performance.getEntriesByType('navigation')[0] as
      | (PerformanceNavigationTiming & { responseStart?: number; domContentLoadedEventEnd?: number })
      | undefined;
    if (nav) {
      if (typeof nav.responseStart === 'number') vitals.ttfbMs = Math.round(nav.responseStart);
      if (typeof nav.domContentLoadedEventEnd === 'number') {
        vitals.dclMs = Math.round(nav.domContentLoadedEventEnd);
      }
    }
    publish();
  } catch {
    // Measurement must never break the page.
  }
}

// Self-initialise on import (main.tsx imports this module for its side effect).
initWebVitals();
