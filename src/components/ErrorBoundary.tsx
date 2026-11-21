import React from 'react';

// Batch 1 (C1/C6): the app had NO error boundary anywhere — a single
// render throw in any tab unmounted the whole React root to a blank
// white page, with no message and no recovery except navigating away.
// This boundary contains the throw to an inline Persian error card
// with retry, so one broken tab can never blank the app again.
//
// Usage:
//   <ErrorBoundary>                        — App root (src/main.tsx)
//   <ErrorBoundary key={activeTab}        — seller-dashboard tab content,
//     onReset={() => setActiveTab('overview')}>  so "retry" also leaves a
//     tab that throws deterministically on mount (C6's store-setup tab
//     re-threw on every reload because the tab lives in the URL).
// The boundary REPORTS as well as displays: componentDidCatch logs the
// error + component stack to the console and records it on
// window.__sellershoLastError (browser-local, mirroring the webVitals
// convention — the app has no error/telemetry backend to send to).
// A silent boundary would mask future bugs.
declare global {
  interface Window {
    __sellershoLastError?: {
      message: string;
      stack: string | null;
      componentStack: string;
      route: string;
      at: string;
    };
  }
}

interface ErrorBoundaryProps {
  children: React.ReactNode;
  /** Optional label shown in the fallback card (e.g. the tab name). */
  title?: string;
  /** Called when the user presses "تلاش مجدد", after the error state resets. */
  onReset?: () => void;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    // REPORT, not just display (critic requirement): a silent boundary
    // would mask future bugs. There is no error/telemetry backend in
    // this app — no Sentry, no error endpoint; the only telemetry
    // (src/utils/webVitals.ts) is deliberately browser-local, with no
    // network send. So the caught error is reported two ways, both
    // local: the console, and a window record mirroring the webVitals
    // convention, inspectable in devtools as window.__sellershoLastError.
    // Do NOT invent a network send here — no endpoint exists to receive it.
    // eslint-disable-next-line no-console
    console.error('ErrorBoundary caught a render error:', error, info.componentStack);
    try {
      window.__sellershoLastError = {
        message: error.message,
        stack: error.stack ?? null,
        componentStack: info.componentStack ?? '',
        route: window.location.href,
        at: new Date().toISOString(),
      };
    } catch {
      // Recording must never throw.
    }
  }

  private handleRetry = (): void => {
    this.setState({ hasError: false });
    this.props.onReset?.();
  };

  render(): React.ReactNode {
    if (!this.state.hasError) return this.props.children;
    return (
      <div dir="rtl" className="mx-auto max-w-md space-y-3 rounded-2xl border border-rose-200 bg-rose-50 p-6 text-center">
        <p className="text-sm font-bold text-rose-800">
          {this.props.title ? `«${this.props.title}» درست بارگذاری نشد.` : 'این بخش درست بارگذاری نشد.'}
        </p>
        <p className="text-xs leading-6 text-slate-600">
          مشکلی در نمایش این بخش پیش آمد؛ بقیهٔ صفحه سالم است. دوباره تلاش کن یا صفحه را تازه کن.
        </p>
        <div className="flex items-center justify-center gap-2">
          <button
            type="button"
            onClick={this.handleRetry}
            className="rounded-xl bg-rose-700 px-4 py-2 text-xs font-bold text-white"
          >
            تلاش مجدد
          </button>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="rounded-xl border border-slate-300 bg-white px-4 py-2 text-xs font-bold text-slate-700"
          >
            بارگذاری دوبارهٔ صفحه
          </button>
        </div>
      </div>
    );
  }
}
