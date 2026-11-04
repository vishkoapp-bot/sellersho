import React from 'react';

// ISO-B live parity (index-MERGED.js oC @463700-465900): the checkout
// address book is a radio picker, not a prefill-only button list.
// "انتخاب از آدرس‌های ذخیره‌شده" radios select a saved address (the
// caller prefills the fields AND records the id); "آدرس جدید (ورود
// دستی)" clears the selection; while the manual option is selected a
// "ذخیره در آدرس‌های من" checkbox (live default: checked) stages
// saveAddress. With an empty book and a logged-in buyer only the save
// checkbox renders, like live; for guests it renders nothing.
import type { SavedAddress } from '../services/addressApi';
export type { SavedAddress };

export const AddressBook: React.FC<{
  book: SavedAddress[];
  onApplyFields: (address: SavedAddress) => void;
  selectedId?: string | null;
  saveAddress?: boolean;
  onSelectSaved?: (address: SavedAddress) => void;
  onSelectNew?: () => void;
  onSaveAddressChange?: (save: boolean) => void;
  showSaveControl?: boolean;
}> = ({ book, onApplyFields, selectedId = null, saveAddress = true, onSelectSaved, onSelectNew, onSaveAddressChange, showSaveControl = false }) => {
  if (!showSaveControl && (!book || book.length === 0)) return null;
  return (
    <div className="space-y-2.5">
      {book && book.length > 0 && (
        <div role="radiogroup" aria-label="آدرس‌های ذخیره‌شده" className="space-y-2">
          <span className="flex items-center gap-1.5 text-[11px] font-bold text-slate-500">انتخاب از آدرس‌های ذخیره‌شده</span>
          {book.map((item) => {
            const isSelected = selectedId === item.id;
            return (
              <button
                key={item.id}
                type="button"
                role="radio"
                aria-checked={isSelected}
                onClick={() => (onSelectSaved ? onSelectSaved(item) : onApplyFields(item))}
                className={`w-full min-h-[44px] text-right p-3 rounded-2xl border transition-colors ${isSelected ? 'border-brand-deep bg-brand-soft/60 ring-1 ring-brand-border' : 'border-slate-200 bg-white hover:bg-slate-50'}`}
              >
                <span className="flex items-center gap-1.5 text-xs font-bold text-slate-800">
                  {item.label || 'آدرس'}
                  {item.isDefault && (
                    <span className="text-[9px] bg-brand-soft text-brand-deep border border-brand/20 px-2 py-0.5 rounded-full font-bold">پیش‌فرض</span>
                  )}
                  <span className="mr-auto text-[10px] font-normal text-slate-400">
                    {item.province}، {item.city}
                  </span>
                </span>
                <span className="block text-[11px] text-slate-500 mt-1 leading-5">{item.address}</span>
                <span className="block text-[10px] text-slate-400 mt-0.5">گیرنده: {item.fullName}</span>
              </button>
            );
          })}
          <button
            type="button"
            role="radio"
            aria-checked={selectedId === null}
            onClick={() => onSelectNew?.()}
            className={`w-full min-h-[44px] text-right p-3 rounded-2xl border transition-colors flex items-center gap-1.5 text-xs font-bold ${selectedId === null ? 'border-brand-deep bg-brand-soft/60 text-brand-deep ring-1 ring-brand-border' : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'}`}
          >
            آدرس جدید (ورود دستی)
          </button>
        </div>
      )}
      {selectedId === null && showSaveControl && (
        <label className="flex min-h-[44px] items-center gap-2 text-xs font-bold text-slate-600 cursor-pointer px-1">
          <input
            type="checkbox"
            checked={saveAddress}
            onChange={(e) => onSaveAddressChange?.(e.target.checked)}
            className="h-5 w-5 shrink-0 accent-[#5d44f8]"
          />
          ذخیره در آدرس‌های من
        </label>
      )}
    </div>
  );
};
