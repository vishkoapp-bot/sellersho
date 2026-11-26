import React, { useEffect, useState } from 'react';
import { useStore } from '../context/StoreContext';
import { MapPin, ReceiptText, UserRound } from 'lucide-react';
import { isValidIranianMobile, isValidIranianPostalCode } from '../utils/validators';
import { AddressBook } from './AddressBook';
import { ProvinceCityPicker, toLatinDigits } from './ProvinceCityPicker';
import { addressApi, SavedAddress, stageSelectionForOrder } from '../services/addressApi';

interface CartCheckoutFormProps {
  hideSavedAddressSelector?: boolean;
  customerInfo: {
    fullName: string;
    phone: string;
    province: string;
    city: string;
    address: string;
    postalCode: string;
  };
  setCustomerInfo: React.Dispatch<React.SetStateAction<any>>;
}

// CHECKOUT-02 (Q8): an edit touches the saved-address selection
// exactly when it changes an address field (province, city, street
// address, postal code). Name/phone edits leave it alone.
export const ADDRESS_FIELDS = ['province', 'city', 'address', 'postalCode'] as const;
export function addressPatchClearsSavedSelection(patch: Record<string, unknown>): boolean {
  return ADDRESS_FIELDS.some((field) => field in patch);
}

export const CartCheckoutForm: React.FC<CartCheckoutFormProps> = ({
  customerInfo,
  setCustomerInfo,
  hideSavedAddressSelector,
}) => {
  const { customerUser } = useStore();
  const [addressBook, setAddressBook] = useState<SavedAddress[]>([]);
  // Team 08 (D63): load the saved-address book for logged-in customers.
  // The AddressBook component itself renders nothing when the book is
  // empty, matching live behaviour. Failures hide the picker, they do
  // not block checkout.
  useEffect(() => {
    setAddressBook([]);
    if (!customerUser.isLoggedIn || hideSavedAddressSelector) return;
    let cancelled = false;
    addressApi.getSavedAddresses()
      .then((list) => { if (!cancelled) setAddressBook(list); })
      .catch(() => { /* no saved addresses available: picker stays hidden */ });
    return () => { cancelled = true; };
  }, [customerUser.isLoggedIn, customerUser.mobile, hideSavedAddressSelector]);
  // ISO-B: live checkout tracks a SELECTED saved address, not just
  // prefilled fields (index-MERGED.js iC hook). saveAddress defaults
  // to true, like live. The selection is staged for OrderContext on
  // every relevant change; the direct-buy form
  // (hideSavedAddressSelector) never stages, so it can never
  // overwrite the modal's explicit address.
  // CHECKOUT-02 (Q8): editing any address field clears the saved
  // selection, so the quote and the order are always computed from
  // the same typed fields. This reverses the old deliberate "live
  // behaviour" (manual edits kept the selection, so the backend
  // loaded the saved address over the edited fields and the submit
  // deterministically failed with shipping_quote_changed - D-06).
  // Full name / phone edits are NOT address edits and keep the
  // selection.
  const [selectedAddressId, setSelectedAddressId] = useState<string | null>(null);
  const [saveAddress, setSaveAddress] = useState<boolean>(true);
  // CHECKOUT-02 fix B1: an explicit deselection that STICKS. Clearing
  // selectedAddressId alone was not enough - the staging effect
  // re-attached the same saved address from the unchanged fields.
  // Set by any address-field edit and by the explicit "new address"
  // choice; cleared only by explicitly picking a saved address.
  // The direct-buy form never stages, so this flag is cart-only.
  const [deselectedSaved, setDeselectedSaved] = useState<boolean>(false);
  const updateCustomerInfo = (patch: Record<string, any>) => {
    if (addressPatchClearsSavedSelection(patch)) { setSelectedAddressId(null); setDeselectedSaved(true); }
    setCustomerInfo((prev: any) => ({ ...prev, ...patch }));
  };
  const applySavedAddress = (saved: SavedAddress) => {
    setSelectedAddressId(saved.id);
    setDeselectedSaved(false);
    setCustomerInfo((prev: any) => ({
      ...prev,
      fullName: saved.fullName || prev.fullName,
      phone: saved.phone || prev.phone,
      province: saved.province || prev.province,
      city: saved.city || prev.city,
      postalCode: saved.postalCode || prev.postalCode,
      address: saved.address || prev.address,
    }));
  };
  const selectNewAddress = () => {
    setSelectedAddressId(null);
    setDeselectedSaved(true);
  };
  useEffect(() => {
    if (hideSavedAddressSelector) return;
    // CHECKOUT-02 fix B1: the staged selection carries the explicit
    // deselection, and postalCode is a staging dep (it was omitted
    // before, so a postal-code-only edit never re-staged at all).
    stageSelectionForOrder(customerInfo, addressBook, selectedAddressId, saveAddress, deselectedSaved);
  }, [hideSavedAddressSelector, customerInfo.province, customerInfo.city, customerInfo.address, customerInfo.postalCode, addressBook, selectedAddressId, saveAddress, deselectedSaved]);
  const isPhoneValid = !customerInfo.phone || isValidIranianMobile(customerInfo.phone);
  const isPostalValid = !customerInfo.postalCode || isValidIranianPostalCode(customerInfo.postalCode);

  const fieldClass = 'block min-h-12 w-full rounded-xl border border-slate-300 bg-slate-50 px-3 py-2.5 text-base text-[var(--checkout-ink)] placeholder:text-slate-500 focus:border-[var(--checkout-accent)] focus:bg-white focus:outline-2 focus:outline-[var(--checkout-accent)] aria-invalid:border-rose-600';
  const labelClass = 'mb-1.5 block text-sm font-bold text-slate-700';

  return (
    <div className="space-y-5 text-[var(--checkout-ink)]">
      <section aria-labelledby="checkout-delivery-title" className="space-y-3">
        <h4 id="checkout-delivery-title" className="flex items-center gap-2 text-base font-black">
          <UserRound className="size-4 text-[var(--checkout-accent)]" aria-hidden="true" />
          اطلاعات گیرنده
        </h4>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
          <div>
            <label htmlFor="cart-checkout-full-name" className={labelClass}>نام و نام خانوادگی</label>
            <input
              id="cart-checkout-full-name"
              name="receiver"
              autoComplete="shipping name"
              type="text"
              value={customerInfo.fullName}
              onChange={(e) => updateCustomerInfo({ fullName: e.target.value })}
              className={fieldClass}
              required
              minLength={3}
            />
          </div>
          <div>
            <label htmlFor="cart-checkout-phone" className={labelClass}>شماره موبایل</label>
            <input
              id="cart-checkout-phone"
              name="mobile"
              autoComplete="shipping tel-national"
              type="tel"
              placeholder="۰۹۱۲۳۴۵۶۷۸۹"
              value={customerInfo.phone}
              aria-invalid={!isPhoneValid}
              aria-describedby={!isPhoneValid ? 'checkout-phone-error' : undefined}
              onChange={(e) => updateCustomerInfo({ phone: toLatinDigits(e.target.value) })}
              className={fieldClass}
              dir="ltr"
              required
            />
            {!isPhoneValid && <p id="checkout-phone-error" className="mt-1.5 text-xs text-rose-700">شماره موبایل معتبر ۱۱ رقمی وارد کنید.</p>}
          </div>
        </div>
      </section>

      <section aria-labelledby="checkout-address-title" className="space-y-3 border-t border-slate-200 pt-5">
        <h4 id="checkout-address-title" className="flex items-center gap-2 text-base font-black">
          <MapPin className="size-4 text-[var(--checkout-accent)]" aria-hidden="true" />
          نشانی تحویل
        </h4>
        {!hideSavedAddressSelector && (
          <AddressBook
            book={addressBook}
            onApplyFields={applySavedAddress}
            selectedId={selectedAddressId}
            saveAddress={saveAddress}
            onSelectSaved={applySavedAddress}
            onSelectNew={selectNewAddress}
            onSaveAddressChange={setSaveAddress}
            showSaveControl={customerUser.isLoggedIn}
          />
        )}
        <div className="grid grid-cols-2 gap-3">
          <ProvinceCityPicker
            province={customerInfo.province}
            city={customerInfo.city}
            onProvinceChange={(value, fromList) => updateCustomerInfo(fromList ? { province: value, city: '' } : { province: value })}
            onCityChange={(value) => updateCustomerInfo({ city: value })}
            inputClassName={fieldClass}
            provinceId="cart-checkout-province"
            cityId="cart-checkout-city"
            labelClassName={labelClass}
          />
        </div>
        <div>
          <label htmlFor="cart-checkout-address" className={labelClass}>نشانی دقیق</label>
          <textarea
            id="cart-checkout-address"
            name="street-address"
            autoComplete="shipping street-address"
            placeholder="خیابان، کوچه، پلاک و واحد"
            rows={2}
            value={customerInfo.address}
            onChange={(e) => updateCustomerInfo({ address: e.target.value })}
            className={`${fieldClass} resize-y`}
            required
          />
        </div>
        <div>
          <label htmlFor="cart-checkout-postal-code" className={labelClass}>کد پستی</label>
          <input
            id="cart-checkout-postal-code"
            name="postal-code"
            type="text"
            inputMode="numeric"
            autoComplete="shipping postal-code"
            placeholder="۱۰ رقم بدون خط تیره"
            value={customerInfo.postalCode}
            aria-invalid={!isPostalValid}
            aria-describedby={!isPostalValid ? 'checkout-postal-error' : undefined}
            onChange={(e) => updateCustomerInfo({ postalCode: toLatinDigits(e.target.value).replace(/[^0-9]/g, '').slice(0, 10) })}
            className={fieldClass}
            dir="ltr"
            required
          />
          {!isPostalValid && <p id="checkout-postal-error" className="mt-1.5 text-xs text-rose-700">کد پستی ۱۰ رقمی معتبر وارد کنید.</p>}
        </div>
      </section>

      <section aria-labelledby="checkout-payment-title" className="rounded-xl bg-[var(--checkout-soft)] p-3">
        <h4 id="checkout-payment-title" className="flex items-center gap-2 text-sm font-black text-[var(--checkout-accent)]">
          <ReceiptText className="size-4" aria-hidden="true" />
          پرداخت پس از ثبت سفارش
        </h4>
        <p className="mt-1.5 text-xs leading-6 text-slate-700">با زدن «ثبت سفارش» مبلغی کسر نمی‌شود. وضعیت و نحوهٔ پرداخت را از «سفارش‌های من» پیگیری کنید.</p>
      </section>
    </div>
  );
};
