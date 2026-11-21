import React, { useEffect, useState } from 'react';
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
  if (Array.isArray(data)) return data as Domain[];
  if (data && typeof data === 'object' && Array.isArray((data as { domains?: unknown }).domains)) {
    return (data as { domains: Domain[] }).domains;
  }
  return [];
};
export const SellerDomainsPanel: React.FC<{ slug: string; storeId: string }> = ({ slug, storeId }) => {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [name, setName] = useState(''); const [error, setError] = useState(''); const [notice, setNotice] = useState(''); const [busy, setBusy] = useState(false);
  const load = async () => setDomains(normalizeDomainRows(await apiRequest<Domain[]>('/seller/domains', { auth: true })));
  useEffect(() => { let active = true; apiRequest<Domain[]>('/seller/domains', { auth: true }).then((rows) => { if (active) setDomains(normalizeDomainRows(rows)); }).catch((e) => { if (active) setError(e.message); }); return () => { active = false; }; }, [slug]);
  const run = async (action: () => Promise<unknown>, message: string) => { setBusy(true); setError(''); setNotice(''); try { await action(); await load(); setNotice(message); } catch (e) { setError(e instanceof Error ? e.message : 'درخواست انجام نشد.'); } finally { setBusy(false); } };
  return <section className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 text-xs">
    <h3 className="font-bold text-slate-900">دامنهٔ اختصاصی و HTTPS</h3>
    <p className="leading-6 text-slate-500">پس از تأیید مدیر، رکورد TXT مالکیت و رکورد A یا AAAA به سرور پلتفرم را تنظیم و تأیید کنید. گواهی HTTPS با اولین بازدید امن صادر و خودکار تمدید می‌شود.</p>
    <div className="flex gap-2"><input aria-label="دامنه اختصاصی" dir="ltr" placeholder="shop.example.com" value={name} onChange={(e) => setName(e.target.value)} className="min-w-0 flex-1 rounded-xl border px-3 py-2" /><button type="button" disabled={busy || !name.trim()} onClick={() => void run(async () => { await apiRequest('/seller/domains', { method: 'POST', auth: true, body: JSON.stringify({ slug, domain: name.trim() }) }); setName(''); }, 'درخواست دامنه ثبت شد.')} className="rounded-xl bg-brand-deep px-3 py-2 text-white disabled:opacity-50">ثبت دامنه</button></div>
    {error && <p role="alert" className="text-rose-700">{error}</p>}{notice && <p role="status" className="text-emerald-700">{notice}</p>}
    {domains.filter((d) => d.storeId === storeId).map((d) => <div key={d.domain} className="space-y-2 rounded-xl bg-slate-50 p-3">
      <div className="flex justify-between gap-2"><strong dir="ltr">{d.domain}</strong><span>{d.status === 'disabled' ? 'غیرفعال' : d.status === 'active' ? 'آمادهٔ HTTPS' : d.approved ? 'منتظر تأیید DNS' : 'منتظر تأیید مدیر'}</span></div>
      {d.status !== 'disabled' && <><p>رکورد TXT زیر را در DNS قرار دهید:</p><code dir="ltr" className="block break-all">_sellersho-verification.{d.domain}</code><code dir="ltr" className="block break-all select-all">{d.verificationToken}</code><div className="flex gap-3"><button type="button" disabled={busy || !d.approved} onClick={() => void run(() => apiRequest(`/seller/domains/${encodeURIComponent(d.domain)}/verify`, { method: 'POST', auth: true }), 'DNS تأیید شد؛ دامنه آمادهٔ دریافت گواهی است.')} className="rounded-lg border px-3 py-2 disabled:opacity-40">بررسی DNS</button><button type="button" disabled={busy} onClick={() => void run(() => apiRequest(`/seller/domains/${encodeURIComponent(d.domain)}`, { method: 'DELETE', auth: true }), 'دامنه غیرفعال شد.')} className="text-rose-700">غیرفعال‌کردن</button></div></>}
    </div>)}
  </section>;
};
