import { useRef, useState, type FormEvent } from 'react';
import { validateStoreSlug } from '../../utils/validators';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';

const digits = new Intl.NumberFormat('fa-IR', { useGrouping: false });
const fieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-brand aria-invalid:border-rose-700';
const primaryClass = 'min-h-11 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-60';

interface Contract {
  text: string;
  version: number;
}

function ContractApproval({ contract, checked, disabled, onChange, onRetry }: {
  contract: Contract;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
  onRetry: () => Promise<void>;
}) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const textAvailable = !!contract.text.trim();
  return (
    <fieldset className="space-y-2 rounded-xl border border-slate-200 bg-slate-50 p-3" disabled={disabled}>
      <legend className="px-1 text-sm font-bold text-slate-900">قرارداد همکاری، نسخهٔ {digits.format(contract.version)}</legend>
      {textAvailable ? (
        <div role="region" aria-label="متن قرارداد همکاری" tabIndex={0}
          className="max-h-44 overflow-y-auto overscroll-contain whitespace-pre-line rounded-lg border border-slate-200 bg-white p-3 text-sm leading-7 text-slate-800 focus-visible:outline-2 focus-visible:outline-brand">
          {contract.text}
        </div>
      ) : (
        <div className="rounded-lg bg-amber-50 p-2 text-sm leading-6 text-amber-950">
          <p role="status">{loading ? 'در حال دریافت متن قرارداد…' : 'متن قرارداد در دسترس نیست.'}</p>
          {error && <p role="alert">{error}</p>}
          <button type="button" disabled={loading} className="min-h-11 rounded-lg px-2 font-bold underline focus-visible:outline-2 focus-visible:outline-brand"
            onClick={async () => {
              setLoading(true);
              setError('');
              try { await onRetry(); }
              catch { setError('دریافت متن قرارداد انجام نشد. دوباره تلاش کن.'); }
              finally { setLoading(false); }
            }}>دریافت دوبارهٔ متن</button>
        </div>
      )}
      <label className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 text-sm font-bold text-slate-800 focus-within:outline-2 focus-within:outline-brand">
        <input type="checkbox" checked={checked} disabled={!textAvailable} onChange={event => onChange(event.target.checked)} className="size-4 shrink-0 accent-brand-deep" />
        متن قرارداد را خواندم و قبول دارم.
      </label>
    </fieldset>
  );
}

export function SellerCreateStoreDialog({ maxStores, contract, onRetry, onCreate, onClose }: {
  maxStores: number;
  contract: Contract;
  onRetry: () => Promise<void>;
  onCreate: (name: string, slug: string) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [accepted, setAccepted] = useState<Contract | null>(null);
  const [busy, setBusy] = useState(false);
  const [invalid, setInvalid] = useState<{ field?: 'name' | 'slug'; message: string } | null>(null);
  const submitting = useRef(false);
  const checked = !!contract.text.trim() && accepted?.version === contract.version && accepted?.text === contract.text;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!checked || submitting.current) return;
    const slugValidation = validateStoreSlug(slug);
    const failure = !name.trim() ? { field: 'name' as const, message: 'نام فروشگاه را وارد کن.' }
      : !slugValidation.valid ? { field: 'slug' as const, message: slugValidation.error || 'آدرس فروشگاه را با حروف انگلیسی وارد کن.' }
      : null;
    setInvalid(failure);
    if (failure) {
      (event.currentTarget.elements.namedItem(failure.field) as HTMLInputElement | null)?.focus();
      return;
    }
    submitting.current = true;
    setBusy(true);
    try {
      await onCreate(name.trim(), slug.trim().toLowerCase());
      onClose();
    } catch (cause) {
      setInvalid({ message: cause instanceof Error ? cause.message : 'ساخت فروشگاه انجام نشد. دوباره تلاش کن.' });
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  return (
    <SupplierDialog titleId="seller-create-store-title" onDismiss={busy ? undefined : onClose}>
      <SupplierDialogHeading id="seller-create-store-title" title="ساخت فروشگاه جدید"
        detail={`با اشتراک فعلی می‌توانی تا ${digits.format(maxStores)} فروشگاه داشته باشی.`} onClose={onClose} busy={busy} />
      <form onSubmit={submit} noValidate className="space-y-3">
        <label className="block text-sm font-bold text-slate-800">نام فروشگاه
          <input name="name" required value={name} disabled={busy} onChange={event => { setName(event.target.value); setInvalid(null); }}
            autoComplete="organization" aria-invalid={invalid?.field === 'name'} aria-describedby={invalid?.field === 'name' ? 'seller-store-create-error' : undefined}
            placeholder="مثلاً فروشگاه مد رها" className={fieldClass} />
        </label>
        <label className="block text-sm font-bold text-slate-800">آدرس انگلیسی فروشگاه
          <input name="slug" required dir="ltr" value={slug} disabled={busy} onChange={event => { setSlug(event.target.value); setInvalid(null); }}
            autoCapitalize="off" autoComplete="off" spellCheck={false} aria-invalid={invalid?.field === 'slug'}
            aria-describedby={invalid?.field === 'slug' ? 'seller-store-slug-hint seller-store-create-error' : 'seller-store-slug-hint'}
            placeholder="raha-shop" className={fieldClass} />
          <span id="seller-store-slug-hint" className="mt-1 block text-xs font-normal leading-6 text-slate-600">مشتری‌ها با این آدرس وارد فروشگاهت می‌شوند. از حرف و عدد انگلیسی و خط تیره استفاده کن.</span>
        </label>
        {invalid && <p id="seller-store-create-error" role="alert" className="rounded-lg bg-rose-50 p-2 text-sm text-rose-800">{invalid.message}</p>}
        <ContractApproval contract={contract} checked={checked} onChange={value => setAccepted(value ? contract : null)} disabled={busy} onRetry={onRetry} />
        <button type="submit" className={`${primaryClass} w-full`} disabled={busy || !checked}>{busy ? 'در حال ساخت…' : 'ساخت فروشگاه'}</button>
      </form>
    </SupplierDialog>
  );
}

export function SellerRenewContractDialog({ contract, onRetry, onAccept, onExit }: {
  contract: Contract;
  onRetry: () => Promise<void>;
  onAccept: (version: number) => Promise<void>;
  onExit: () => void;
}) {
  const [accepted, setAccepted] = useState<Contract | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const submitting = useRef(false);
  const checked = !!contract.text.trim() && accepted?.version === contract.version && accepted?.text === contract.text;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!checked || submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError('');
    try { await onAccept(contract.version); }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'تأیید قرارداد انجام نشد. دوباره تلاش کن.'); }
    finally { submitting.current = false; setBusy(false); }
  }

  return (
    <SupplierDialog titleId="seller-renew-contract-title" descriptionId="seller-renew-contract-description">
      <h2 id="seller-renew-contract-title" className="text-lg font-extrabold text-slate-900">قرارداد همکاری به‌روز شده است</h2>
      <p id="seller-renew-contract-description" className="mt-1 text-sm leading-7 text-slate-700">برای ادامهٔ کار فروشگاه، متن نسخهٔ جدید را بخوان و تأیید کن.</p>
      <form onSubmit={submit} className="mt-3 space-y-3">
        <ContractApproval contract={contract} checked={checked} onChange={value => setAccepted(value ? contract : null)} disabled={busy} onRetry={onRetry} />
        {error && <p role="alert" className="rounded-lg bg-rose-50 p-2 text-sm text-rose-800">{error}</p>}
        <div className="flex flex-wrap gap-2">
          <button type="submit" disabled={!checked || busy} className={`${primaryClass} flex-1`}>{busy ? 'در حال ثبت…' : 'تأیید نسخهٔ جدید'}</button>
          <button type="button" disabled={busy} onClick={onExit} className="min-h-11 rounded-xl px-3 text-sm font-bold text-slate-700 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand">خروج از حساب</button>
        </div>
      </form>
    </SupplierDialog>
  );
}
