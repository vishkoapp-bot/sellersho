import React, { useEffect, useId, useState } from 'react';
import { ChevronDown, Globe } from 'lucide-react';
import { apiRequest } from '../../services/apiClient';
interface Domain { domain: string; storeId: string; verificationToken: string; status: string; approved: boolean; }

// Batch 1 (C6, H1): the live /seller/domains endpoint returns a bare
// array, but the panel fed the response straight into setDomains and
// then called domains.filter(...) on render. A single non-array payload
// (envelope change, null body, error page parsed as JSON) would throw
// on first mount and — with no error boundary — blank the whole app.
// Normalise defensively: bare array wins, {domains: [...]} is accepted,
// anything else becomes [] (empty state, never a throw).
export const normalizeDomainRows = (data: unknown): Domain[] => {
  const rows: unknown = Array.isArray(data)
    ? data
    : data && typeof data === 'object' && 'domains' in data ? data.domains : undefined;
  if (!Array.isArray(rows)) return [];
  const domains: Domain[] = [];
  for (const row of rows) {
    if (!row || typeof row !== 'object') continue;
    const { domain, storeId, verificationToken, status, approved } = row as Partial<Record<keyof Domain, unknown>>;
    if (typeof domain !== 'string' || domain.trim() === '') continue;
    domains.push({
      domain,
      storeId: typeof storeId === 'string' ? storeId : '',
      verificationToken: typeof verificationToken === 'string' ? verificationToken : '',
      status: typeof status === 'string' ? status : '',
      approved: approved === true,
    });
  }
  return domains;
};
export const SellerDomainsPanel: React.FC<{ slug: string; storeId: string }> = ({ slug, storeId }) => {
  const domainInputId = useId();
  const [domains, setDomains] = useState<Domain[]>([]);
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void apiRequest('/seller/domains', { auth: true, signal: controller.signal })
      .then(rows => { if (!controller.signal.aborted) setDomains(normalizeDomainRows(rows)); })
      .catch(err => { if (!controller.signal.aborted) setError(err.message); });
    return () => controller.abort();
  }, [slug]);

  const run = async (action: () => Promise<unknown>, message: string) => {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await action();
      setDomains(normalizeDomainRows(await apiRequest('/seller/domains', { auth: true })));
      setNotice(message);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'درخواست انجام نشد.');
    } finally {
      setBusy(false);
    }
  };

  const registerDomain = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy || !name.trim()) return;
    void run(async () => {
      await apiRequest('/seller/domains', { method: 'POST', auth: true, body: JSON.stringify({ slug, domain: name.trim() }) });
      setName('');
    }, 'درخواست دامنه ثبت شد.');
  };

  return (
    <details className="group rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
      <summary className="flex min-h-11 cursor-pointer list-none items-center gap-3 rounded-lg text-base font-extrabold text-[#261b41] focus-visible:ring-2 focus-visible:ring-brand">
        <Globe aria-hidden="true" className="size-5 shrink-0 text-brand-deep" strokeWidth={1.75} />
        دامنهٔ اختصاصی و HTTPS
        <ChevronDown aria-hidden="true" className="ms-auto size-5 shrink-0 text-brand-deep group-open:rotate-180" />
      </summary>
      <div className="mt-3 space-y-4 border-t border-slate-100 pt-4">
        <p className="text-sm leading-6 text-slate-600">دامنه‌ای را که خریده‌ای وارد کن. بعد از تأیید مدیر، رکوردهای DNS را تنظیم کن و «بررسی DNS» را بزن.</p>
        <form onSubmit={registerDomain} className="max-w-xl space-y-1.5">
          <label htmlFor={domainInputId} className="block text-sm font-bold text-slate-800">نام دامنه</label>
          <div className="flex flex-wrap items-center gap-2">
            <input id={domainInputId} dir="ltr" placeholder="shop.example.com" value={name} onChange={e => setName(e.target.value)} required autoComplete="off" className="min-h-11 min-w-0 flex-1 rounded-xl border border-slate-200 bg-slate-50 px-3 text-sm focus-visible:ring-2 focus-visible:ring-brand" />
            <button type="submit" disabled={busy || !name.trim()} className="min-h-11 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">ثبت دامنه</button>
          </div>
        </form>
        {error && <p role="alert" className="text-sm text-rose-700">{error}</p>}
        {notice && <p role="status" className="text-sm text-emerald-700">{notice}</p>}
        {domains.filter(domain => domain.storeId === storeId).map(domain => (
          <div key={domain.domain} className="space-y-3 rounded-xl border border-slate-100 bg-slate-50 p-3 text-sm">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <strong><bdi dir="ltr" className="break-all">{domain.domain}</bdi></strong>
              <span className="rounded-full bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">
                {domain.status === 'disabled' ? 'غیرفعال' : domain.status === 'active' ? 'آمادهٔ HTTPS' : domain.approved ? 'منتظر تأیید DNS' : 'منتظر تأیید مدیر'}
              </span>
            </div>
            {domain.status !== 'disabled' && (
              <>
                <div className="space-y-1 text-xs leading-6 text-slate-600">
                  <p>در DNS دامنه، رکورد A یا AAAA را به سرور پلتفرم وصل کن و رکورد TXT زیر را اضافه کن.</p>
                  <code dir="ltr" className="block break-all">_sellersho-verification.{domain.domain}</code>
                  <code dir="ltr" className="block select-all break-all">{domain.verificationToken}</code>
                  <p>گواهی HTTPS با اولین بازدید امن صادر و خودکار تمدید می‌شود.</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button type="button" disabled={busy || !domain.approved} onClick={() => void run(() => apiRequest(`/seller/domains/${encodeURIComponent(domain.domain)}/verify`, { method: 'POST', auth: true }), 'DNS تأیید شد؛ دامنه آمادهٔ دریافت گواهی است.')} className="min-h-11 rounded-xl border border-brand-border px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-40">بررسی DNS</button>
                  <button type="button" disabled={busy} onClick={() => void run(() => apiRequest(`/seller/domains/${encodeURIComponent(domain.domain)}`, { method: 'DELETE', auth: true }), 'دامنه غیرفعال شد.')} className="min-h-11 rounded-xl px-3 text-xs font-bold text-rose-700 hover:bg-rose-50 focus-visible:ring-2 focus-visible:ring-rose-600 disabled:opacity-40">غیرفعال‌کردن</button>
                </div>
              </>
            )}
          </div>
        ))}
      </div>
    </details>
  );
};
