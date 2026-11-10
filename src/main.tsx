import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './index.css';

async function bootstrap() {
  const root = ReactDOM.createRoot(document.getElementById('root')!);
  try {
    const response = await fetch('/api/v1/domain-store', { signal: AbortSignal.timeout(8000) });
    if (response.ok) {
      const { slug } = await response.json() as { slug: string };
      const base = `/store/${encodeURIComponent(slug)}`;
      const path = window.location.pathname;
      if (path === '/' || path.startsWith('/product/')) window.history.replaceState(null, '', `${base}${path === '/' ? '' : path}${window.location.search}${window.location.hash}`);
      else if (path.startsWith('/store/') && path !== base && !path.startsWith(`${base}/`)) window.history.replaceState(null, '', base);
    } else if (response.status !== 404) throw new Error('فروشگاه در حال حاضر در دسترس نیست.');
    root.render(<React.StrictMode><App /></React.StrictMode>);
  } catch {
    root.render(<main dir="rtl" className="mx-auto max-w-md space-y-4 p-8 text-center"><p>ارتباط با فروشگاه برقرار نشد.</p><button onClick={() => window.location.reload()} className="rounded-xl bg-teal-700 px-4 py-2 text-white">تلاش مجدد</button></main>);
  }
}
void bootstrap();
