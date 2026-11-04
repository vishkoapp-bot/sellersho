import React, { useState } from 'react';
import { supplierApi, SupplierAddonDto } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { Plus, X, CheckCircle2, XCircle, Clock, Edit, Link2 } from 'lucide-react';

// Live addons widget (Ql): supplier's complementary products/services,
// rendered at the top of the products tab. Edits to an approved add-on are
// staged for manager review (server returns staged:true); the widget shows
// live's staged toast in that case.
const ACCENT = '#5d44f8';

const formatSep = (raw: string): string => {
  const digits = raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
  if (digits === '') return '';
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬').replace(/\d/g, (ch) => '۰۱۲۳۴۵۶۷۸۹'[parseInt(ch, 10)]);
};

const parseSep = (raw: string): string =>
  raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');

interface FormState {
  title: string;
  description: string;
  kind: string;
  priceMode: string;
  priceValue: string;
  capToman: string;
  sortOrder: string;
  mode: string;
  linkedProductId: string;
}

const EMPTY_FORM: FormState = { title: '', description: '', kind: 'service', priceMode: 'fixed', priceValue: '', capToman: '', sortOrder: '0', mode: 'custom', linkedProductId: '' };

const formFromAddon = (addon: SupplierAddonDto): FormState => ({
  title: addon.title,
  description: addon.description || '',
  kind: addon.kind,
  priceMode: addon.priceMode,
  priceValue: String(addon.priceValue ?? ''),
  capToman: addon.capToman ? String(addon.capToman) : '',
  sortOrder: String(addon.sortOrder ?? 0),
  mode: addon.linkedProductId ? 'product' : 'custom',
  linkedProductId: addon.linkedProductId || '',
});

const addonStatus = (addon: SupplierAddonDto): string => {
  const s = addon.proposalStatus || addon.approvalStatus;
  return s === 'approved' || s === 'rejected' ? s : 'pending';
};

const StatusBadge: React.FC<{ addon: SupplierAddonDto }> = ({ addon }) => {
  const s = addonStatus(addon);
  if (s === 'approved') {
    return <span className="text-[9px] font-bold px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-700 border border-emerald-200 flex items-center gap-1 shrink-0"><CheckCircle2 className="w-3 h-3" strokeWidth={1.5} />تأییدشده</span>;
  }
  if (s === 'rejected') {
    return <span className="text-[9px] font-bold px-2 py-0.5 rounded-full bg-rose-50 text-rose-700 border border-rose-200 flex items-center gap-1 shrink-0"><XCircle className="w-3 h-3" strokeWidth={1.5} />ردشده</span>;
  }
  return <span className="text-[9px] font-bold px-2 py-0.5 rounded-full bg-amber-50 text-amber-700 border border-amber-200 flex items-center gap-1 shrink-0"><Clock className="w-3 h-3" strokeWidth={1.5} />در انتظار تأیید</span>;
};

export const SupplierAddonsWidget: React.FC<{ ownProducts: { id: string; title: string }[] }> = ({ ownProducts }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const resource = useSupplierResource(supplierApi.getAddons);
  const list = resource.error ? [] : resource.data || [];
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<SupplierAddonDto | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [saving, setSaving] = useState(false);
  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [attachId, setAttachId] = useState<string | null>(null);
  const [attached, setAttached] = useState<string[]>([]);
  const [savingAttach, setSavingAttach] = useState(false);

  const openNew = () => { setEditing(null); setForm(EMPTY_FORM); setFormOpen(true); };
  const openEdit = (addon: SupplierAddonDto) => { setEditing(addon); setForm(formFromAddon(addon)); setFormOpen(true); };

  const save = async () => {
    if (form.mode === 'product') {
      const product = ownProducts.find((item) => item.id === form.linkedProductId);
      if (!product) {
        error('کالای مکمل را از کالاهای خودت انتخاب کن.');
        return;
      }
      const payload = { title: product.title, description: form.description.trim(), kind: 'physical', priceMode: 'fixed', priceValue: 0, capToman: 0, linkedProductId: product.id, sortOrder: Math.floor(Number(form.sortOrder) || 0) };
      setSaving(true);
      try {
        if (editing) {
          const res = await supplierApi.updateAddon(editing.id, payload);
          if (res.staged) success('تغییرات ثبت شد؛ پس از تأیید مدیر اعمال می‌شود و تا آن زمان نسخه فعلی فعال می‌ماند.');
          else success('محصول مکمل ویرایش شد.');
        } else {
          await supplierApi.createAddon(payload);
          success('محصول مکمل ثبت شد و برای تأیید به مدیر ارسال شد.');
        }
        setFormOpen(false);
        setEditing(null);
        await resource.reload();
      } catch (cause: unknown) {
        error(cause instanceof Error ? cause.message : 'ذخیره محصول مکمل انجام نشد.');
      } finally {
        setSaving(false);
      }
      return;
    }
    if (!form.title.trim()) {
      error('عنوان محصول مکمل الزامی است.');
      return;
    }
    const price = Number(form.priceValue);
    if (!Number.isFinite(price) || price < 0) {
      error('مبلغ/درصد قیمت معتبر نیست.');
      return;
    }
    const payload = { title: form.title.trim(), description: form.description.trim(), kind: form.kind, priceMode: form.priceMode, priceValue: price, capToman: form.priceMode === 'percent' ? Math.max(0, Math.floor(Number(form.capToman) || 0)) : 0, sortOrder: Math.floor(Number(form.sortOrder) || 0) };
    setSaving(true);
    try {
      if (editing) {
        const res = await supplierApi.updateAddon(editing.id, payload);
        if (res.staged) success('تغییرات ثبت شد؛ پس از تأیید مدیر اعمال می‌شود و تا آن زمان نسخه فعلی فعال می‌ماند.');
        else success('محصول مکمل ویرایش شد.');
      } else {
        await supplierApi.createAddon(payload);
        success('محصول مکمل ثبت شد و برای تأیید به مدیر ارسال شد.');
      }
      setFormOpen(false);
      setEditing(null);
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ذخیره محصول مکمل انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const toggleActive = async (addon: SupplierAddonDto) => {
    setTogglingId(addon.id);
    try {
      await supplierApi.setAddonActive(addon.id, !addon.isActive);
      success(addon.isActive ? 'محصول مکمل غیرفعال شد.' : 'محصول مکمل فعال شد.');
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'تغییر وضعیت محصول مکمل انجام نشد.');
    } finally {
      setTogglingId(null);
    }
  };

  const toggleAttachPanel = (addon: SupplierAddonDto) => {
    if (attachId === addon.id) {
      setAttachId(null);
      return;
    }
    setAttachId(addon.id);
    setAttached((addon.attachments || []).filter((att) => att.scopeType === 'product' && att.productId).map((att) => att.productId));
  };

  const saveAttachments = async (addon: SupplierAddonDto) => {
    setSavingAttach(true);
    try {
      await supplierApi.setAddonAttachments(addon.id, attached);
      success('اتصال محصول مکمل به کالاها ذخیره شد.');
      setAttachId(null);
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ذخیره اتصال‌ها انجام نشد.');
    } finally {
      setSavingAttach(false);
    }
  };

  const priceDisplay = (addon: SupplierAddonDto): string => {
    if (addon.linkedProductId) return 'قیمت خود کالا';
    if (addon.priceMode === 'percent') return `درصدی: ${toPersianDigits(addon.priceValue)}٪${addon.capToman > 0 ? ` — سقف ${formatPrice(addon.capToman)} تومان` : ''}`;
    return `ثابت: ${formatPrice(addon.priceValue)} تومان`;
  };

  const inputClass = 'w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 text-slate-800 focus:outline-none';

  return (
    <div className="bg-white rounded-2xl p-3.5 border border-slate-200 shadow-sm space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-xs font-bold text-slate-800 flex items-center gap-1.5">
            <Plus className="w-4 h-4" strokeWidth={1.5} style={{ color: ACCENT }} />
            محصولات مکمل من
          </h3>
          <span className="text-[10px] text-slate-400 block mt-0.5">خدمات یا کالاهای اضافه‌ای که مشتری کنار کالای تو، اختیاری انتخاب می‌کند (مثل گارانتی اضافه)</span>
        </div>
        <button type="button" onClick={openNew} className="flex items-center gap-1 text-white text-[11px] font-bold min-h-[44px] px-3.5 rounded-xl shadow-xs shrink-0" style={{ backgroundColor: ACCENT }}>
          <Plus className="w-3.5 h-3.5" strokeWidth={1.5} />
          <span>محصول مکمل تازه</span>
        </button>
      </div>
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={() => void resource.reload()} />
      {formOpen && (
        <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-2.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-slate-800">{editing ? 'ویرایش محصول مکمل' : 'محصول مکمل تازه'}</span>
            <button type="button" onClick={() => setFormOpen(false)} aria-label="بستن فرم" className="p-1 text-slate-400"><X className="w-4 h-4" strokeWidth={1.5} /></button>
          </div>
          {editing && addonStatus(editing) === 'approved' && (
            <p className="text-[10px] leading-5 text-amber-700 bg-amber-50 border border-amber-200 rounded-lg px-2.5 py-2">این محصول مکمل تأییدشده و فعال است؛ تغییرات پس از تأیید مدیر اعمال می‌شود و تا آن زمان نسخه فعلی بدون تغییر کار می‌کند.</p>
          )}
          <div className="flex rounded-xl bg-white border border-slate-200 p-1 gap-1" role="tablist" aria-label="نوع محصول مکمل">
            <button type="button" onClick={() => setForm((prev) => ({ ...prev, mode: 'custom' }))} className={`flex-1 min-h-[36px] rounded-lg text-[11px] font-bold ${form.mode === 'custom' ? 'text-white' : 'text-slate-600'}`} style={form.mode === 'custom' ? { backgroundColor: ACCENT } : undefined}>عنوان دلخواه</button>
            <button type="button" onClick={() => setForm((prev) => ({ ...prev, mode: 'product' }))} className={`flex-1 min-h-[36px] rounded-lg text-[11px] font-bold ${form.mode === 'product' ? 'text-white' : 'text-slate-600'}`} style={form.mode === 'product' ? { backgroundColor: ACCENT } : undefined}>انتخاب از کالاهای من</button>
          </div>
          {form.mode === 'product' && (
            <div className="space-y-1.5">
              <select value={form.linkedProductId} onChange={(event) => { const id = event.target.value; const product = ownProducts.find((item) => item.id === id); setForm((prev) => ({ ...prev, linkedProductId: id, title: product ? product.title : prev.title })); }} className={inputClass}>
                <option value="">کالا را انتخاب کن…</option>
                {ownProducts.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}
              </select>
              <p className="text-[10px] leading-5 text-slate-500">هزینه این مکمل، قیمت خود همین کالا در هر فروشگاه است؛ عنوان و عکس هم از کالا گرفته می‌شود. تعریف‌شدن به‌تنهایی نمایش نمی‌دهد — دراپ‌شیپر باید آن را در فروشگاهش انتخاب کند.</p>
            </div>
          )}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <input type="text" value={form.title} disabled={form.mode === 'product'} onChange={(event) => setForm((prev) => ({ ...prev, title: event.target.value }))} placeholder={form.mode === 'product' ? 'عنوان از کالای انتخاب‌شده گرفته می‌شود' : 'عنوان (مثلاً: گارانتی اضافه یک‌ساله)'} className={inputClass} />
            {form.mode === 'custom' && (
              <>
                <select value={form.kind} onChange={(event) => setForm((prev) => ({ ...prev, kind: event.target.value }))} className={inputClass}>
                  <option value="service">خدمات</option>
                  <option value="physical">فیزیکی</option>
                </select>
                <select value={form.priceMode} onChange={(event) => setForm((prev) => ({ ...prev, priceMode: event.target.value }))} className={inputClass}>
                  <option value="fixed">قیمت ثابت (تومان)</option>
                  <option value="percent">درصدی از قیمت کالا</option>
                </select>
                <input type="text" inputMode="numeric" min="0" value={form.priceMode === 'percent' ? form.priceValue : formatSep(form.priceValue)} onChange={(event) => setForm((prev) => ({ ...prev, priceValue: parseSep(event.target.value) }))} placeholder={form.priceMode === 'percent' ? 'درصد (مثلاً ۵)' : 'مبلغ ثابت به تومان — به ازای هر عدد'} className={inputClass} dir="ltr" />
                {form.priceMode === 'percent' && (
                  <input type="text" inputMode="numeric" min="0" value={formatSep(form.capToman)} onChange={(event) => setForm((prev) => ({ ...prev, capToman: parseSep(event.target.value) }))} placeholder="سقف مبلغ (تومان — خالی = بدون سقف)" className={inputClass} dir="ltr" />
                )}
              </>
            )}
            {form.mode === 'product' && <p className="text-[10px] leading-5 text-slate-500 self-center">قیمت از خود کالا گرفته می‌شود؛ فیلد قیمت ندارد.</p>}
            <input type="text" inputMode="numeric" value={formatSep(form.sortOrder)} onChange={(event) => setForm((prev) => ({ ...prev, sortOrder: parseSep(event.target.value) }))} placeholder="ترتیب نمایش" className={inputClass} dir="ltr" />
          </div>
          <textarea value={form.description} onChange={(event) => setForm((prev) => ({ ...prev, description: event.target.value }))} placeholder="توضیحات — دقیقاً همین متن به مشتری نمایش داده می‌شود." rows={2} className={inputClass} />
          <div className="flex gap-2">
            <button type="button" disabled={saving} onClick={save} className="flex-1 min-h-[44px] rounded-xl text-white text-xs font-bold disabled:opacity-50" style={{ backgroundColor: ACCENT }}>{saving ? 'در حال ذخیره…' : editing ? 'ذخیره و ارسال برای تأیید' : 'ثبت و ارسال برای تأیید'}</button>
            <button type="button" onClick={() => setFormOpen(false)} className="min-h-[44px] px-4 rounded-xl bg-slate-100 text-slate-700 text-xs font-bold">انصراف</button>
          </div>
        </div>
      )}
      {!resource.loading && list.length === 0 && !resource.error && (
        <p className="text-[11px] text-slate-400 text-center py-3">هنوز محصول مکملی نساخته‌ای. با دکمه بالا اولی را بساز؛ بعد از تأیید مدیر می‌توانی آن را به کالاهایت وصل کنی.</p>
      )}
      <div className="space-y-2">
        {list.map((addon) => {
          const attachCount = (addon.attachments || []).filter((att) => att.scopeType === 'product').length;
          return (
            <div key={addon.id} className="p-3 bg-slate-50 rounded-xl border border-slate-200/80 space-y-2">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-slate-800 truncate">{addon.title}</h4>
                  <p className="text-[10px] text-slate-500 mt-0.5">{addon.kind === 'service' ? 'خدمات' : 'فیزیکی'} • {priceDisplay(addon)} • ترتیب {toPersianDigits(addon.sortOrder)}</p>
                  {addon.description && <p className="text-[10px] text-slate-400 mt-0.5 leading-4">{addon.description}</p>}
                  <p className="text-[10px] text-slate-400 mt-0.5">وصل به {toPersianDigits(attachCount)} کالا{addon.isActive ? '' : ' • غیرفعال'}</p>
                  {addonStatus(addon) === 'rejected' && addon.rejectionReason && <p className="text-[10px] text-rose-600 mt-0.5">دلیل رد: {addon.rejectionReason}</p>}
                </div>
                <StatusBadge addon={addon} />
              </div>
              <div className="flex flex-wrap gap-1.5">
                <button type="button" onClick={() => openEdit(addon)} className="min-h-[40px] px-3 bg-white border border-slate-200 text-slate-700 text-[11px] font-bold rounded-xl flex items-center gap-1"><Edit className="w-3.5 h-3.5" strokeWidth={1.5} />ویرایش</button>
                <button type="button" onClick={() => toggleAttachPanel(addon)} className="min-h-[40px] px-3 bg-white border border-slate-200 text-slate-700 text-[11px] font-bold rounded-xl flex items-center gap-1"><Link2 className="w-3.5 h-3.5" strokeWidth={1.5} />اتصال به کالاها</button>
                <button type="button" disabled={togglingId === addon.id} onClick={() => void toggleActive(addon)} className="min-h-[40px] px-3 bg-white border border-slate-200 text-slate-700 text-[11px] font-bold rounded-xl disabled:opacity-50">{addon.isActive ? 'غیرفعال‌کردن' : 'فعال‌کردن'}</button>
              </div>
              {attachId === addon.id && (
                <div className="p-2.5 bg-white border border-slate-200 rounded-xl space-y-2">
                  <p className="text-[10px] font-bold text-slate-700">این محصول مکمل کنار کدام کالاهای تو پیشنهاد شود؟</p>
                  {ownProducts.length === 0 ? (
                    <p className="text-[10px] text-slate-400">کالای تأییدشده‌ای نداری که به آن وصل شود.</p>
                  ) : (
                    <div className="space-y-1 max-h-52 overflow-y-auto no-scrollbar">
                      {ownProducts.map((item) => (
                        <label key={item.id} className="flex items-center gap-2 text-[11px] text-slate-700 py-1 cursor-pointer">
                          <input type="checkbox" checked={attached.includes(item.id)} onChange={(event) => setAttached((prev) => event.target.checked ? [...prev, item.id] : prev.filter((id) => id !== item.id))} className="w-4 h-4 shrink-0" style={{ accentColor: ACCENT }} />
                          <span className="truncate">{item.title}</span>
                        </label>
                      ))}
                    </div>
                  )}
                  <button type="button" disabled={savingAttach} onClick={() => void saveAttachments(addon)} className="w-full min-h-[40px] rounded-xl text-white text-[11px] font-bold disabled:opacity-50" style={{ backgroundColor: ACCENT }}>{savingAttach ? 'در حال ذخیره…' : 'ذخیره اتصال‌ها'}</button>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
};
