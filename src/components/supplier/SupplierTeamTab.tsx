import { SupplierSectionHeading } from './SupplierSectionHeading';
import { SupplierDialog, SupplierDialogHeading } from './SupplierDialog';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import React, { useEffect, useState } from 'react';
import { UserPlus, UserCheck, UserX, ShieldCheck } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { supplierTeamApi, SupplierMember, SUPPLIER_ROLES } from '../../services/supplierTeamApi';
import { useStore } from '../../context/StoreContext';

const ROLE_OPTIONS = [
  ['products', 'مدیریت کالا'], ['operations', 'عملیات و موجودی'], ['orders', 'مدیریت سفارش‌ها'],
  ['finance', 'مالی و تسویه'], ['marketing', 'بازاریابی'], ['manager', 'مدیر کامل'],
];

const EMPTY_MEMBER_FORM = { fullName: '', username: '', mobile: '', password: '', roleKey: 'products' };
const MEMBER_FIELDS = [
  { key: 'fullName', label: 'نام و نام خانوادگی', type: 'text', maxLength: 100, autoComplete: 'name' },
  { key: 'username', label: 'نام کاربری', type: 'text', maxLength: 50, minLength: 3, pattern: '[a-zA-Z0-9_.\\-]{3,50}', dir: 'ltr' },
  { key: 'mobile', label: 'شماره موبایل', type: 'tel', maxLength: 11, pattern: '09[0-9]{9}', dir: 'ltr', autoComplete: 'tel-national' },
  { key: 'password', label: 'رمز عبور (حداقل ۸ نویسه)', type: 'password', maxLength: 72, minLength: 8, dir: 'ltr', autoComplete: 'new-password' },
] as const;
const fieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

export const SupplierTeamTab: React.FC = () => {
  const { supplierSession } = useStore();
  const supplierId = supplierSession?.supplierId || '';
  const { success, error } = useToast();
  const [members, setMembers] = useState<SupplierMember[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState(EMPTY_MEMBER_FORM);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [updatingId, setUpdatingId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError('');
    supplierTeamApi.list(supplierId)
      .then(items => { if (!cancelled) setMembers(items); })
      .catch((err: unknown) => { if (!cancelled) setLoadError(err instanceof Error ? err.message : 'فهرست همکاران دریافت نشد.'); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [supplierId, loadAttempt]);
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setSaveError('');
    try {
      const created = await supplierTeamApi.create(supplierId, {
        ...form,
        fullName: form.fullName.trim(),
        username: form.username.trim(),
        mobile: form.mobile.trim(),
      });
      setMembers(prev => [created, ...prev]);
      setShowForm(false);
      setForm(EMPTY_MEMBER_FORM);
      success('حساب همکار ساخته شد.');
    } catch (err: unknown) {
      setSaveError(err instanceof Error ? err.message : 'ساختن حساب انجام نشد.');
    } finally {
      setSaving(false);
    }
  };
  const update = async (member: SupplierMember, updates: { active?: boolean; roleKey?: string }) => {
    if (updatingId) return;
    setUpdatingId(member.id);
    try {
      await supplierTeamApi.update(supplierId, member.id, updates);
      setMembers(prev => prev.map(item => item.id === member.id
        ? { ...item, isActive: updates.active ?? item.isActive, roleKey: updates.roleKey ?? item.roleKey }
        : item));
      success('دسترسی همکار تغییر کرد.');
    } catch (err: unknown) {
      error(err instanceof Error ? err.message : 'تغییر دسترسی انجام نشد.');
    } finally {
      setUpdatingId(null);
    }
  };
  const closeForm = () => {
    setShowForm(false);
    setForm(current => ({ ...current, password: '' }));
    setSaveError('');
  };

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="همکاران من" description="برای هر همکار حساب جدا بساز و دسترسی‌اش را تعیین کن." icon={<ShieldCheck />}
        action={<button type="button" aria-haspopup="dialog" onClick={() => setShowForm(true)} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2"><UserPlus aria-hidden="true" className="size-4" /> افزودن همکار</button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={() => setLoadAttempt(attempt => attempt + 1)} />
      {!loading && !loadError && members.length === 0 && <SupplierEmptyState icon={<UserPlus />} title="هنوز حساب همکاری ثبت نشده است." description="با «افزودن همکار» برای اولین نفر حساب بساز." />}
      {!loading && !loadError && members.length > 0 && (
        <ul className="space-y-2.5">
          {members.map(member => (
            <li key={member.id} className="flex flex-wrap items-center justify-between gap-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="break-words text-base font-extrabold text-[#261b41]">{member.fullName}</h3>
                  <span className={`rounded-lg px-2 py-1 text-xs font-bold ${member.isActive ? 'bg-emerald-50 text-emerald-800' : 'bg-slate-100 text-slate-700'}`}>{member.isActive ? 'فعال' : 'غیرفعال'}</span>
                </div>
                <p className="mt-1 flex flex-wrap gap-x-2 break-all text-sm text-slate-600"><bdi dir="ltr">@{member.username}</bdi><bdi dir="ltr">{member.mobile}</bdi></p>
              </div>
              <div className="flex flex-wrap items-end gap-2">
                <label className="text-xs font-bold text-slate-700">نقش
                  <select aria-label={`نقش ${member.fullName}`} disabled={updatingId !== null} value={member.roleKey} onChange={event => void update(member, { roleKey: event.target.value })} className={fieldClass}>
                    {ROLE_OPTIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
                  </select>
                </label>
                <button disabled={updatingId !== null} type="button" onClick={() => void update(member, { active: !member.isActive })} className={`flex min-h-11 items-center gap-1 rounded-xl px-3 py-2 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60 ${member.isActive ? 'text-rose-700 hover:bg-rose-50' : 'bg-brand-soft text-brand-deep'}`}>
                  {member.isActive ? <UserX aria-hidden="true" className="size-4" /> : <UserCheck aria-hidden="true" className="size-4" />}
                  {member.isActive ? 'غیرفعال‌کردن' : 'فعال‌کردن'}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {showForm && (
        <SupplierDialog titleId="supplier-team-new-member" onDismiss={saving ? undefined : closeForm}>
          <SupplierDialogHeading id="supplier-team-new-member" title="افزودن همکار" onClose={closeForm} busy={saving} />
          <form onSubmit={submit} className="space-y-3">
            <fieldset disabled={saving} className="grid gap-3 sm:grid-cols-2">
              {MEMBER_FIELDS.map(({ key, label, ...inputProps }) => (
                <label key={key} className="block text-sm font-bold text-slate-700">{label}
                  <input {...inputProps} autoFocus={key === 'fullName'} required value={form[key]} onChange={event => setForm(current => ({ ...current, [key]: event.target.value }))} className={fieldClass} />
                </label>
              ))}
            </fieldset>
            <label className="block text-sm font-bold text-slate-700">نقش و دسترسی
              <select aria-describedby="supplier-team-role-help" disabled={saving} value={form.roleKey} onChange={event => setForm(current => ({ ...current, roleKey: event.target.value }))} className={fieldClass}>
                {ROLE_OPTIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
              </select>
            </label>
            <p id="supplier-team-role-help" className="rounded-xl bg-brand-bg p-3 text-sm leading-6 text-slate-700">{SUPPLIER_ROLES.find(role => role.key === form.roleKey)?.description}</p>
            {saveError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{saveError}</p>}
            <div className="flex gap-2">
              <button disabled={saving} type="submit" className="min-h-11 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">{saving ? 'در حال ساخت…' : 'ساخت حساب'}</button>
              <button disabled={saving} type="button" onClick={closeForm} className="min-h-11 rounded-xl border border-slate-300 px-4 py-2 text-sm font-bold text-slate-700 focus-visible:ring-2 focus-visible:ring-brand">انصراف</button>
            </div>
          </form>
        </SupplierDialog>
      )}
    </div>
  );
};
