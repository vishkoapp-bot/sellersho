import React, { useEffect, useState } from 'react';
import { UserPlus, UserCheck, UserX, ShieldCheck } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { supplierTeamApi, SupplierMember, SUPPLIER_ROLES } from '../../services/supplierTeamApi';
import { useStore } from '../../context/StoreContext';

const ROLE_OPTIONS = [
  ['products', 'مدیریت کالا'], ['operations', 'عملیات و موجودی'], ['orders', 'مدیریت سفارش‌ها'],
  ['finance', 'مالی و تسویه'], ['marketing', 'بازاریابی'], ['manager', 'مدیر کامل'],
];

export const SupplierTeamTab: React.FC = () => {
  const { supplierSession } = useStore();
  const supplierId = supplierSession?.supplierId || '';
  const { success, error } = useToast();
  const [members, setMembers] = useState<SupplierMember[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ fullName: '', username: '', mobile: '', password: '', roleKey: 'products' });
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [saving, setSaving] = useState(false);
  const [updatingId, setUpdatingId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError('');
    supplierTeamApi.list(supplierId).then((items) => { if (!cancelled) setMembers(items); }).catch((err: unknown) => { if (!cancelled) setLoadError(err instanceof Error ? err.message : 'همکاران نیامدند؛ دوباره تلاش کن.'); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [supplierId, loadAttempt]);
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    try { const created = await supplierTeamApi.create(supplierId, { ...form, fullName: form.fullName.trim(), username: form.username.trim(), mobile: form.mobile.trim() }); setMembers((prev) => [created, ...prev]); setShowForm(false); setForm({ fullName: '', username: '', mobile: '', password: '', roleKey: 'products' }); success('حساب همکار ساخته شد.'); } catch (err: unknown) { error(err instanceof Error ? err.message : 'ساختن حساب انجام نشد.'); } finally { setSaving(false); }
  };
  const update = async (member: SupplierMember, updates: { active?: boolean; roleKey?: string }) => {
    setUpdatingId(member.id);
    try { await supplierTeamApi.update(supplierId, member.id, updates); setMembers((prev) => prev.map((item) => item.id === member.id ? { ...item, isActive: updates.active ?? item.isActive, roleKey: updates.roleKey ?? item.roleKey } : item)); success('دسترسی همکار عوض شد.'); } catch (err: unknown) { error(err instanceof Error ? err.message : 'تغییر دسترسی انجام نشد.'); } finally { setUpdatingId(null); }
  };

  return <div className="space-y-3.5 pb-6">
    <section className="rounded-2xl border border-brand-border bg-brand-soft p-4 space-y-2">
      <h2 className="flex items-center gap-2 text-sm font-bold text-brand-deep"><ShieldCheck className="h-5 w-5"  strokeWidth={1.5}/> همکاران من</h2>
      <p className="text-xs leading-6 text-brand-deep">برای هر کارمندت یک حساب جدا بساز. هر همکار فقط بخش‌هایی را می‌بیند که تو مشخص می‌کنی و فقط اطلاعات همین شرکت را.</p>
      <p className="text-xs leading-6 text-brand-deep">همکار با نام کاربری و رمز خودش وارد پنل می‌شود. حسابش را غیرفعال کنی، دیگر نمی‌تواند وارد شود.</p>
      <button type="button" onClick={() => setShowForm(true)} className="flex items-center gap-2 rounded-xl bg-brand-deep px-3.5 py-2 min-h-[44px] text-xs font-bold text-white"><UserPlus className="h-4 w-4"  strokeWidth={1.5}/> افزودن همکار</button>
    </section>
    {showForm && <form onSubmit={submit} className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
      <h3 className="text-sm font-bold">حساب همکار جدید</h3>
      {([['fullName', 'نام و نام خانوادگی'], ['username', 'نام کاربری'], ['mobile', 'شماره موبایل'], ['password', 'رمز عبور (حداقل ۸ حرف)']] as const).map(([key, label]) => <label key={key} className="block text-xs">{label}<input required disabled={saving} minLength={key === 'password' ? 8 : undefined} maxLength={key === 'password' ? 72 : key === 'fullName' ? 100 : key === 'username' ? 50 : 11} pattern={key === 'mobile' ? '09[0-9]{9}' : key === 'username' ? '[a-zA-Z0-9_.\\-]{3,50}' : undefined} type={key === 'password' ? 'password' : key === 'mobile' ? 'tel' : 'text'} autoComplete={key === 'password' ? 'new-password' : 'off'} value={form[key]} onChange={(event) => setForm({ ...form, [key]: event.target.value })} className="mt-1 w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 min-h-[44px] text-xs" /></label>)}
      <label className="block text-xs">این همکار به چه بخش‌هایی دسترسی دارد؟<select disabled={saving} value={form.roleKey} onChange={(event) => setForm({ ...form, roleKey: event.target.value })} className="mt-1 w-full rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs">{ROLE_OPTIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
      <p className="rounded-xl bg-amber-50 p-3 text-xs leading-6 text-amber-900">{SUPPLIER_ROLES.find((role) => role.key === form.roleKey)?.description}</p>
      <div className="flex gap-2"><button disabled={saving} type="submit" className="rounded-xl bg-brand-deep px-4 py-2 min-h-[44px] text-xs font-bold text-white disabled:opacity-50">{saving ? 'در حال ساخت…' : 'ساخت حساب'}</button><button disabled={saving} type="button" onClick={() => { setShowForm(false); setForm({ ...form, password: '' }); }} className="rounded-xl bg-slate-100 px-4 py-2 min-h-[44px] text-xs font-bold">انصراف</button></div>
    </form>}
    <div className="space-y-2">{loading ? <p role="status" className="p-4 text-xs">همکاران می‌آیند…</p> : loadError ? <div className="space-y-2"><p role="alert" className="rounded-xl bg-rose-50 p-4 text-xs text-rose-700">{loadError}</p><button type="button" onClick={() => setLoadAttempt(attempt => attempt + 1)} className="min-h-11 rounded-lg bg-white px-3 font-bold text-rose-800">دریافت دوبارهٔ فهرست همکاران</button></div> : members.length === 0 ? <div className="rounded-2xl border bg-white p-6 text-center space-y-1.5"><p className="text-xs font-bold text-slate-700">هنوز همکاری نداری.</p><p className="text-[11px] leading-5 text-slate-400">اگر کارمند داری، با دکمه «افزودن همکار» برایش حساب بساز تا با نام و رمز خودش وارد شود و فقط بخش‌های لازم را ببیند.</p></div> : members.map((member) => <div key={member.id} className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-slate-200 bg-white p-3"><div><p className="text-xs font-bold">{member.fullName}</p><p className="mt-1 text-[10px] text-slate-500">@{member.username} · {member.mobile}</p><p className="mt-1 text-xs text-slate-600">{member.isActive ? 'حساب فعال' : 'حساب غیرفعال'}</p></div><select aria-label={`نقش ${member.fullName}`} disabled={updatingId !== null} value={member.roleKey} onChange={(event) => update(member, { roleKey: event.target.value })} className="rounded-xl border border-slate-200 p-2 min-h-[40px] text-xs">{ROLE_OPTIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select><button disabled={updatingId !== null} type="button" onClick={() => update(member, { active: !member.isActive })} className={`flex items-center gap-1 rounded-xl px-2.5 py-2 min-h-[36px] text-[10px] font-bold ${member.isActive ? 'bg-emerald-50 text-emerald-700' : 'bg-rose-50 text-rose-700'}`}>{member.isActive ? <><UserX className="h-3.5 w-3.5"  strokeWidth={1.5}/>غیرفعال‌کردن</> : <><UserCheck className="h-3.5 w-3.5"  strokeWidth={1.5}/>فعال‌کردن</>}</button></div>)}</div>
  </div>;
};