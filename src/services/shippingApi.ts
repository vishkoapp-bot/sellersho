import { apiRequest } from './apiClient';

// FIX 5 (Team 03 G1/G2): live pre-checkout shipping quote.
// POST /api/v1/shipping/quote with {items, destination}; the response
// carries one leg per origin (legIndex, originLabel, fare,
// weightGrams, boxLabel), allocations linking each line to its leg,
// and totalShipping. This is distinct from the warehouse carrier
// tariff (/warehouse/carriers/quote), which is left untouched.

export interface ShippingQuoteItem {
  productId: string;
  variantId?: string;
  quantity: number;
}

export interface ShippingQuoteDestination {
  province: string;
  city: string;
  address: string;
  postalCode?: string;
}

export interface ShippingQuoteLeg {
  legIndex: number;
  index?: number;
  originId: string;
  originLabel: string;
  fare: number;
  weightGrams: number;
  boxLabel: string;
  source?: string;
  estimated?: boolean;
}

export interface ShippingQuoteAllocation {
  productId: string;
  variantId?: string;
  quantity: number;
  legIndex: number;
}

export interface ShippingQuote {
  success: boolean;
  quoteId: string;
  legs: ShippingQuoteLeg[];
  totalShipping: number;
  allocations: ShippingQuoteAllocation[];
}

export const shippingApi = {
  quote: async (
    items: ShippingQuoteItem[],
    destination: ShippingQuoteDestination,
    options?: { signal?: AbortSignal },
  ): Promise<ShippingQuote> => {
    return apiRequest<ShippingQuote>('/shipping/quote', {
      method: 'POST',
      auth: true,
      body: JSON.stringify({ items, destination }),
      ...(options?.signal ? { signal: options.signal } : {}),
    });
  },
};

// ---------------------------------------------------------------------------
// CHECKOUT-02 — quote generation / signature / sequencing (G-01..G-14).
//
// The drawer binds to QuoteSession and nothing else decides what is
// displayed or submittable, so the gates below are enforced in exactly
// one place:
//  - G-09: buildQuoteSignature covers province/city/address/postal
//    code, store, lines (product/variant/quantity/store) and addon
//    selection. Recipient name/phone are NOT inputs, so changing them
//    never issues a new request.
//  - G-01/G-02/G-10/G-11: every request gets a monotonically
//    increasing generation AND its own AbortController (Q9: both
//    layers). Only the latest generation may update success/error/
//    loading; older resolutions — success or failure — are ignored,
//    and the older fetch is aborted where possible.
//  - Q5: a previous good quote is RETAINED on failure / while a
//    refetch is in flight and is always exposed as visibly stale
//    (displayStale), never as the current charge; it is never
//    submittable while stale.
//  - Q1/Q7: beginMismatch() auto-refetches immediately and records
//    old vs new charge; submit stays blocked until
//    acknowledgeMismatch() is called.
// Addons are FEE-ONLY (Q3): the PDP addon model carries no product,
// weight or dimensions — ProductAddon (src/types/index.ts:66-72) is
// {id, title, description?, feePerUnit, ownerType} and the backend
// PDPAddon (backend/internal/repository/pdp_addons.go:12-19) is the
// same shape; addons linked to a catalog product are explicitly
// skipped as fee cards there (pdp_addons.go:91-94, "their fee is the
// linked product's resolved price, a separate complementary display,
// not a fee card"). So addon changes bump the signature and force a
// revalidation request, but add NO lines to the quote request.
// ---------------------------------------------------------------------------

export interface QuoteLineInput extends ShippingQuoteItem {
  storeId?: string;
  /** Fee-only addon ids selected on this line (Q3: signature only). */
  addonIds?: string[];
}

export interface QuoteSignatureInput {
  storeId?: string;
  items: QuoteLineInput[];
  destination: ShippingQuoteDestination;
}

export function buildQuoteSignature(input: QuoteSignatureInput): string {
  const lines = input.items
    .map((item) => ({
      p: item.productId,
      v: item.variantId ?? '',
      q: item.quantity,
      s: item.storeId ?? '',
      a: [...(item.addonIds ?? [])].sort(),
    }))
    .sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b), 'en'));
  return JSON.stringify({
    d: {
      p: input.destination.province ?? '',
      c: input.destination.city ?? '',
      a: input.destination.address ?? '',
      pc: input.destination.postalCode ?? '',
    },
    s: input.storeId ?? '',
    lines,
  });
}

export type QuoteStatus = 'idle' | 'loading' | 'quoted' | 'free' | 'warning' | 'error';

export interface QuoteMismatch {
  oldTotalShipping: number;
  oldPayable: number;
  newTotalShipping: number | null;
  newPayable: number | null;
  acknowledged: boolean;
}

export type QuoteFetch = (
  items: ShippingQuoteItem[],
  destination: ShippingQuoteDestination,
  opts: { signal: AbortSignal },
) => Promise<ShippingQuote>;

const INCOMPLETE_ADDRESS_MESSAGE =
  'برای محاسبه هزینه ارسال، نشانی کامل (استان، شهر و خیابان) را وارد کنید.';

export function isQuoteChangedError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : typeof err === 'string' ? err : '';
  return message.includes('shipping_quote_changed') || message.includes('هزینه ارسال تغییر کرده');
}

export class QuoteSession {
  status: QuoteStatus = 'idle';
  /** Last good quote, retained (marked stale) across refetch/failure — Q5. */
  quote: ShippingQuote | null = null;
  /** Signature the retained quote was issued for. */
  quoteSignature = '';
  /** Signature of the current inputs. */
  currentSignature = '';
  /** True when the displayed quote is not the current charge. */
  stale = false;
  message = '';
  errorCode = '';
  mismatch: QuoteMismatch | null = null;
  goodsPayable = 0;

  private fetchQuote: QuoteFetch;
  private generation = 0;
  private controller: AbortController | null = null;
  private currentInput: QuoteSignatureInput | null = null;
  /** Signature of the request currently in flight, if any. */
  private pendingSignature = '';
  private mismatchRefetchPending = false;
  private listeners = new Set<() => void>();

  constructor(fetchQuote: QuoteFetch) {
    this.fetchQuote = fetchQuote;
  }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  private notify(): void {
    this.listeners.forEach((listener) => listener());
  }

  /** The quote currently on screen (retained or fresh) and whether it is stale. */
  get displayQuote(): ShippingQuote | null {
    return this.quote;
  }

  get displayStale(): boolean {
    if (!this.quote) return false;
    return this.stale || this.quoteSignature !== this.currentSignature;
  }

  /** Submit is allowed only for the latest matching fresh quote (G-08). */
  canSubmit(): boolean {
    if (this.status !== 'quoted' && this.status !== 'free') return false;
    if (!this.quote || !this.currentInput || this.currentInput.items.length === 0) return false;
    if (this.quoteSignature !== this.currentSignature) return false;
    if (this.mismatch && !this.mismatch.acknowledged) return false;
    if (this.mismatch && this.mismatch.acknowledged && this.mismatch.newTotalShipping !== this.quote.totalShipping) return false;
    return true;
  }

  getSubmittableQuoteId(): string | null {
    return this.canSubmit() && this.quote ? this.quote.quoteId : null;
  }

  private abortInFlight(): void {
    if (this.controller) {
      this.controller.abort();
      this.controller = null;
    }
    this.pendingSignature = '';
    // Invalidating the generation makes any already-dispatched
    // resolution a no-op even where abort cannot stop the fetch (G-10).
    this.generation += 1;
  }

  request(input: QuoteSignatureInput, goodsPayable: number, opts?: { force?: boolean; forMismatch?: boolean }): void {
    this.goodsPayable = goodsPayable;
    this.currentInput = input;
    this.currentSignature = buildQuoteSignature(input);
    if (!opts?.forMismatch) this.mismatch = null;

    if (input.items.length === 0) {
      // G-08 negative case: an emptied cart leaves no submittable quote.
      this.abortInFlight();
      this.quote = null;
      this.quoteSignature = '';
      this.stale = false;
      this.status = 'idle';
      this.message = '';
      this.errorCode = '';
      this.mismatch = null;
      this.notify();
      return;
    }
    if (!input.destination.province || !input.destination.city || !input.destination.address) {
      // Transient/incomplete destination (G-03): warn, never quote a
      // guessed zone; keep any previous quote only as visibly stale.
      this.abortInFlight();
      this.stale = this.quote !== null;
      this.status = 'warning';
      this.message = INCOMPLETE_ADDRESS_MESSAGE;
      this.errorCode = 'shipping_address_required';
      this.notify();
      return;
    }
    // Same inputs, already fresh or already in flight: no new request
    // (G-09 — a shipping-irrelevant change never reaches here, and an
    // identical re-request is a no-op instead of quote churn).
    if (!opts?.force && !this.mismatch &&
        ((this.quote && this.quoteSignature === this.currentSignature &&
          (this.status === 'quoted' || this.status === 'free')) ||
         (this.status === 'loading' && this.pendingSignature === this.currentSignature))) {
      return;
    }

    this.abortInFlight();
    if (opts?.forMismatch) this.mismatchRefetchPending = true;
    this.stale = this.quote !== null;
    this.status = 'loading';
    this.message = '';
    this.errorCode = '';
    const generation = this.generation;
    const signature = this.currentSignature;
    const controller = new AbortController();
    this.controller = controller;
    this.pendingSignature = signature;
    const items: ShippingQuoteItem[] = input.items.map((item) => ({
      productId: item.productId,
      variantId: item.variantId,
      quantity: item.quantity,
    }));
    const destination = { ...input.destination };
    this.notify();
    void this.fetchQuote(items, destination, { signal: controller.signal }).then(
      (q) => {
        if (generation !== this.generation || controller.signal.aborted) return;
        if (signature !== this.currentSignature) return;
        this.quote = q;
        this.quoteSignature = signature;
        this.stale = false;
        this.status = q.totalShipping > 0 ? 'quoted' : 'free';
        this.message = '';
        this.errorCode = '';
        if (this.mismatchRefetchPending) {
          this.mismatchRefetchPending = false;
          if (this.mismatch) {
            this.mismatch = {
              ...this.mismatch,
              newTotalShipping: q.totalShipping,
              newPayable: this.goodsPayable + q.totalShipping,
            };
          }
        }
        this.notify();
      },
      (e: any) => {
        if (generation !== this.generation || controller.signal.aborted) return;
        if (signature !== this.currentSignature) return;
        // The last good quote is RETAINED (Q5) — only its staleness
        // changes. A failed request never leaves a submittable quote
        // behind: status 'error' fails canSubmit() (G-07).
        this.mismatchRefetchPending = false;
        const code = e?.body?.error || (typeof e?.status === 'number' && e.status === 0 ? 'quote_timeout_or_network' : '');
        this.errorCode = code;
        if (code === 'shipping_address_required') {
          this.status = 'warning';
          this.message = INCOMPLETE_ADDRESS_MESSAGE;
        } else if (code === 'shipping_unavailable') {
          this.status = 'error';
          this.message = 'قیمت ارسال در دسترس نیست؛ لطفاً چند لحظه دیگر دوباره تلاش کنید.';
        } else if (code === 'quote_timeout_or_network') {
          this.status = 'error';
          this.message = 'زمان محاسبه هزینه ارسال به پایان رسید یا ارتباط برقرار نشد؛ لطفاً دوباره تلاش کنید.';
        } else {
          this.status = 'error';
          this.message = 'محاسبه هزینه ارسال انجام نشد. لطفاً دوباره تلاش کنید.';
        }
        this.stale = this.quote !== null;
        this.notify();
      },
    );
  }

  /** Retry issues exactly one new request for the current inputs (G-07). */
  retry(): void {
    if (!this.currentInput) return;
    // A retry during mismatch recovery is a retry of the mismatch
    // refetch: the old-vs-new record must survive it (Q1/Q7).
    this.request(this.currentInput, this.goodsPayable, { force: true, forMismatch: this.mismatch !== null });
  }

  /**
   * Drawer close / direct↔cart switch (G-05, G-06): abort the pending
   * request and discard its display state. The cart and the typed
   * address live outside this session and are untouched.
   */
  cancelAndClear(): void {
    this.abortInFlight();
    this.quote = null;
    this.quoteSignature = '';
    this.currentSignature = '';
    this.currentInput = null;
    this.stale = false;
    this.status = 'idle';
    this.message = '';
    this.errorCode = '';
    this.mismatch = null;
    this.mismatchRefetchPending = false;
    this.notify();
  }

  /**
   * Q1: on shipping_quote_changed, auto-refetch immediately and record
   * old vs new charge for explicit review. The resubmit itself stays
   * blocked until acknowledgeMismatch() (Q7).
   */
  beginMismatch(): void {
    if (!this.quote || !this.currentInput) return;
    if (this.status !== 'quoted' && this.status !== 'free') return;
    this.mismatch = {
      oldTotalShipping: this.quote.totalShipping,
      oldPayable: this.goodsPayable + this.quote.totalShipping,
      newTotalShipping: null,
      newPayable: null,
      acknowledged: false,
    };
    this.request(this.currentInput, this.goodsPayable, { force: true, forMismatch: true });
  }

  acknowledgeMismatch(): void {
    if (!this.mismatch || this.mismatch.newTotalShipping === null) return;
    this.mismatch = { ...this.mismatch, acknowledged: true };
    this.notify();
  }

  clearMismatch(): void {
    if (!this.mismatch) return;
    this.mismatch = null;
    this.notify();
  }
}
