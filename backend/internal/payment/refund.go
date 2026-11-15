package payment

import "context"

// RefundProvider is an internal execution contract. FIN-010 supplies a real,
// source-bound adapter; this task uses only a deterministic local test provider.
// Lookup must query the same operation and must never issue a new transfer.
type RefundProvider interface {
	Refund(context.Context, RefundRequest) (*RefundOutcome, error)
	LookupRefund(context.Context, RefundRequest) (*RefundOutcome, error)
}

type RefundRequest struct {
	OperationKey      string
	PaymentID         string
	OriginalReference string
	AmountToman       int64
	Currency          string
	Destination       string
}

type RefundOutcome struct {
	OperationKey      string
	PaymentID         string
	AmountToman       int64
	Currency          string
	Destination       string
	Status            string
	ProviderReference string
}
