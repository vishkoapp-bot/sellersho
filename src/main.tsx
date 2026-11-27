import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { ErrorBoundary } from './components/ErrorBoundary';
import { RouteLoading } from './components/common/RouteLoading';
import { pathForStoreDomain } from './utils/routing';
import './index.css';
import './utils/webVitals'; // OBS-01 (D-08): browser-local web-vitals measurement, no network send.

async function bootstrap() {
  const root = ReactDOM.createRoot(document.getElementById('root')!);
  root.render(<RouteLoading label="در حال بارگذاری فروشگاه…" screen />);
  try {
    const response = await fetch('/api/v1/domain-store', { signal: AbortSignal.timeout(8000) });
    const currentPath = window.location.pathname;
    let path = currentPath;
    if (response.ok) {
      const { slug } = await response.json() as { slug: string };
      path = pathForStoreDomain(currentPath, slug);
    } else if (response.status !== 404) {
      throw new Error('فروشگاه در حال حاضر در دسترس نیست.');
    } else if (currentPath.replace(/\/+$/, '') === '/store/central') {
      path = '/';
    }
    if (path !== currentPath) window.history.replaceState(null, '', path + window.location.search + window.location.hash);
    root.render(<React.StrictMode><ErrorBoundary><App /></ErrorBoundary></React.StrictMode>);
  } catch {
    root.render(<main dir="rtl" className="mx-auto max-w-md space-y-4 p-8 text-center"><p>ارتباط با فروشگاه برقرار نشد.</p><button onClick={() => window.location.reload()} className="rounded-xl bg-teal-700 px-4 py-2 text-white">تلاش مجدد</button></main>);
  }
}
void bootstrap();
