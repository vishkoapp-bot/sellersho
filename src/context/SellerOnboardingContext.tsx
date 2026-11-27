import React, { createContext, useCallback, useContext, useState } from 'react';

export const emptySellerOnboardingForm = {
  storeName: '',
  slug: '',
  ownerName: '',
  phone: '',
  referralCode: '',
  categories: ['digital', 'fashion'],
};

export function makeSellerSignatureCode() {
  return String(Math.floor(1000 + Math.random() * 9000));
}

function useSellerOnboardingState() {
  const [isOpen, setOpen] = useState(false);
  const [step, setStep] = useState<1 | 2 | 3 | 4>(1);
  const [formData, setFormData] = useState(() => {
    // Capture referral links before navigation or the first opening of the form.
    const referralCode = typeof window === 'undefined' ? '' : new URLSearchParams(window.location.search).get('ref')?.trim().toUpperCase();
    return referralCode ? { ...emptySellerOnboardingForm, referralCode } : emptySellerOnboardingForm;
  });
  const [contractAccepted, setContractAccepted] = useState(false);
  const [signatureCode, setSignatureCode] = useState('');
  const setIsOpen = useCallback((open: boolean) => {
    if (open) setSignatureCode(makeSellerSignatureCode());
    setOpen(open);
  }, []);
  return { isOpen, setIsOpen, step, setStep, formData, setFormData, contractAccepted, setContractAccepted, signatureCode };
}

const SellerOnboardingContext = createContext<ReturnType<typeof useSellerOnboardingState> | null>(null);

// The draft outlives the order and warehouse providers' identity-scoped remounts.
export const SellerOnboardingProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  const onboarding = useSellerOnboardingState();
  return <SellerOnboardingContext.Provider value={onboarding}>{children}</SellerOnboardingContext.Provider>;
};

export const useSellerOnboardingDraft = () => useContext(SellerOnboardingContext);
