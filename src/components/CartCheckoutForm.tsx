import React, { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { useStore } from '../context/StoreContext';
import { CreditCard, Wallet, AlertCircle, Truck, Package } from 'lucide-react';
import { isValidIranianMobile, isValidIranianPostalCode } from '../utils/validators';
import { resolvePackagingAndShippingFee } from '../utils/pricingEngine';
import { AddressBook } from './AddressBook';
import { ProvinceCityPicker, toLatinDigits } from './ProvinceCityPicker';
import { addressApi, SavedAddress, selectionForOrder, stageAddressSelection } from '../services/addressApi';

interface CartCheckoutFormProps {
  paymentMethod: 'cash';
  setPaymentMethod: (m: 'cash') => void;
  shippingSlot?: ReactNode;
  walletSlot?: ReactNode;
  addonsAmount?: number;
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

export const CartCheckoutForm: React.FC<CartCheckoutFormProps> = ({
  paymentMethod,
  setPaymentMethod,
  customerInfo,
  setCustomerInfo,
  shippingSlot,
  walletSlot,
  addonsAmount,
  hideSavedAddressSelector,
}) => {
  const { cart, cartPayable, platformSettings, formatPrice, toPersianDigits, customerUser } = useStore();
  const [addressBook, setAddressBook] = useState<SavedAddress[]>([]);
  // Team 08 (D63): load the saved-address book for logged-in customers.
  // The AddressBook component itself renders nothing when the book is
  // empty, matching live behaviour. Failures hide the picker, they do
  // not block checkout.
  useEffect(() => {
    if (!customerUser.isLoggedIn) return;
    let cancelled = false;
    addressApi.getSavedAddresses()
      .then((list) => { if (!cancelled) setAddressBook(list); })
      .catch(() => { /* no saved addresses available: picker stays hidden */ });
    return () => { cancelled = true; };
  }, [customerUser.isLoggedIn]);
  // ISO-B: live checkout tracks a SELECTED saved address, not just
  // prefilled fields (index-MERGED.js iC hook). saveAddress defaults
  // to true, like live. Manual edits do not clear the selection: the
  // backend then uses the saved address over edited fields (live
  // behaviour, also true for direct-buy). The selection is staged for
  // OrderContext on every relevant change; the direct-buy form
  // (hideSavedAddressSelector) never stages, so it can never
  // overwrite the modal's explicit address.
  const [selectedAddressId, setSelectedAddressId] = useState<string | null>(null);
  const [saveAddress, setSaveAddress] = useState<boolean>(true);
  const applySavedAddress = (saved: SavedAddress) => {
    setSelectedAddressId(saved.id);
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
  };
  useEffect(() => {
    if (hideSavedAddressSelector) return;
    stageAddressSelection(selectionForOrder(customerInfo, addressBook, selectedAddressId, saveAddress));
  }, [hideSavedAddressSelector, customerInfo.province, customerInfo.city, customerInfo.address, addressBook, selectedAddressId, saveAddress]);
  const isPhoneValid = !customerInfo.phone || isValidIranianMobile(customerInfo.phone);
  const isPostalValid = !customerInfo.postalCode || isValidIranianPostalCode(customerInfo.postalCode);

  // Requirement 7: Packaging and Shipping fee computation by weight and destination
  // FIX 5 (Team 03 G12): live default weight for an item with no
  // recorded weight is 500g, not 380g.
  const estimatedWeight = cart.reduce((sum, item) => sum + (item.product.specs?.['وزن'] ? parseInt(item.product.specs['وزن'], 10) : 500) * item.quantity, 0);
  const packaging = resolvePackagingAndShippingFee(estimatedWeight, customerInfo.province, 'تهران', platformSettings);

  return (
    <div className="space-y-4 pt-2">
      {/* Worker A: live checkout form shows the shipping slot on mobile
          only (lg:hidden); on desktop it lives in the side column. */}
      {shippingSlot ? (
        <div className="p-3 bg-brand-soft/70 border border-brand-border/80 rounded-2xl lg:hidden">{shippingSlot}</div>
      ) : (
        <div className="p-3 bg-brand-soft/70 border border-brand-border/80 rounded-2xl text-xs lg:hidden">
          <div className="flex items-center gap-1.5 text-brand-deep font-bold">
            <Truck className="w-4 h-4" strokeWidth={1.5} />
            <span>هزینه بسته‌بندی و ارسال: محاسبه در مرحله بعد</span>
          </div>
          <p className="text-[10px] text-slate-500 mt-1 leading-5">مبلغ دقیق ارسال هنگام ثبت سفارش، بر اساس وزن مرسوله و مقصد محاسبه و به فاکتور اضافه می‌شود.</p>
        </div>
      )}
      {!!addonsAmount && addonsAmount > 0 && (
        <div className="p-3 bg-white border border-slate-200 rounded-2xl text-xs flex items-center justify-between">
          <span className="flex items-center gap-1.5 font-bold text-slate-700">
            <Package className="w-4 h-4" strokeWidth={1.5} />
            محصولات مکمل
          </span>
          <span className="font-bold text-slate-800">{formatPrice(addonsAmount)} تومان</span>
        </div>
      )}
      {/* Payment Method Selector */}
      <div>
        <label className="block text-xs lg:text-sm font-bold text-slate-700 mb-2">روش پرداخت:</label>
        <div className={`grid gap-2 ${walletSlot ? "grid-cols-1" : "grid-cols-2"}`}>
          <button
            type="button"
            onClick={() => setPaymentMethod('cash')}
            className={`p-3 rounded-2xl border text-right transition-all flex flex-col justify-between ${
              paymentMethod === 'cash'
                ? 'bg-brand-soft border-brand-deep text-brand-deep ring-2 ring-brand-border'
                : 'bg-white border-slate-200 text-slate-700 hover:bg-slate-50'
            }`}
          >
            <div className="flex items-center gap-1.5 font-bold text-xs">
              <CreditCard className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
              <span>پرداخت آنلاین</span>
            </div>
            <span className="text-[10px] lg:text-xs text-brand-deep mt-1 block">
              به‌زودی فعال می‌شود؛ پس از ثبت سفارش، نحوه پرداخت به شما اعلام می‌شود
            </span>
          </button>

          {!walletSlot && (
          <button
            type="button"
            disabled
            className="p-3 rounded-2xl border text-right transition-all flex flex-col justify-between bg-slate-50 border-slate-200 text-slate-400 cursor-not-allowed opacity-70"
          >
            <div className="flex items-center gap-1.5 font-bold text-xs">
              <Wallet className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/>
              <span>کیف پول و موجودی</span>
              <span className="mr-auto text-[9px] font-bold bg-slate-200 text-slate-500 rounded-full px-2 py-0.5">به‌زودی</span>
            </div>
            <span className="text-[10px] text-slate-400 mt-1 block">پرداخت با کیف پول در حال حاضر فعال نیست</span>
          </button>
          )}
        </div>
        {walletSlot && <div className="mt-2 lg:hidden">{walletSlot}</div>}
      </div>

      {/* Customer Address Details */}
      <div className="space-y-2.5">
        <label className="block text-xs lg:text-sm font-bold text-slate-700">اطلاعات ارسال گیرنده:</label>
        {/* Team 08 (D63): saved-address book (hidden when empty). */}
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
        <div className="grid grid-cols-2 gap-2">
          <div>
            <label htmlFor="cart-checkout-full-name" className="sr-only">نام و نام خانوادگی گیرنده</label>
            <input
              id="cart-checkout-full-name"
              type="text"
              placeholder="نام و نام خانوادگی"
              value={customerInfo.fullName}
              onChange={(e) => setCustomerInfo((prev: any) => ({ ...prev, fullName: e.target.value }))}
              className="w-full text-base p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="cart-checkout-phone" className="sr-only">شماره موبایل گیرنده</label>
            <input
              id="cart-checkout-phone"
              type="tel"
              placeholder="شماره موبایل (۰۹۱۲۳۴۵۶۷۸۹)"
              value={customerInfo.phone}
              onChange={(e) => setCustomerInfo((prev: any) => ({ ...prev, phone: toLatinDigits(e.target.value) }))}
              className={`w-full text-base p-2.5 rounded-xl bg-slate-50 border ${
                !isPhoneValid ? 'border-rose-400 bg-rose-50/40' : 'border-slate-200'
              } focus:bg-white focus:border-brand outline-hidden`}
              dir="ltr"
            />
            {!isPhoneValid && (
              <span className="text-[11px] text-rose-500 block">شماره موبایل معتبر ۱۱ رقمی وارد کنید.</span>
            )}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <ProvinceCityPicker
            province={customerInfo.province}
            city={customerInfo.city}
            onProvinceChange={(value, fromList) => setCustomerInfo((prev: any) => (fromList ? { ...prev, province: value, city: '' } : { ...prev, province: value }))}
            onCityChange={(value) => setCustomerInfo((prev: any) => ({ ...prev, city: value }))}
            inputClassName="w-full text-base p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
            provinceId="cart-checkout-province"
            cityId="cart-checkout-city"
          />
        </div>

        <div>
          <label htmlFor="cart-checkout-postal-code" className="sr-only">کد پستی</label>
          <input
            id="cart-checkout-postal-code"
            type="text"
            placeholder="کد پستی ۱۰ رقمی (بدون خط تیره)"
            value={customerInfo.postalCode}
            onChange={(e) => setCustomerInfo((prev: any) => ({ ...prev, postalCode: toLatinDigits(e.target.value) }))}
            className={`w-full text-base p-2.5 rounded-xl bg-slate-50 border ${
              !isPostalValid ? 'border-rose-400 bg-rose-50/40' : 'border-slate-200'
            } focus:bg-white focus:border-brand outline-hidden`}
            dir="ltr"
            maxLength={10}
          />
          {!isPostalValid && (
            <span className="text-[11px] text-rose-500 mt-0.5 block">کد پستی ۱۰ رقمی معتبر وارد کنید.</span>
          )}
        </div>

        <label htmlFor="cart-checkout-address" className="sr-only">آدرس دقیق پستی</label>
        <textarea
          id="cart-checkout-address"
          placeholder="آدرس دقیق پستی، پلاک و زنگ"
          rows={2}
          value={customerInfo.address}
          onChange={(e) => setCustomerInfo((prev: any) => ({ ...prev, address: e.target.value }))}
          className="w-full text-base p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden resize-none"
        />
      </div>
    </div>
  );
};
