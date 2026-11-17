package payment

import (
	"context"
	"github.com/drup/platform/internal/observability"
	"testing"
	"time"
)

type observedProbe struct {
	t  *testing.T
	id string
}

func (p observedProbe) VerifyTransaction(ctx context.Context, token string, n int64) (*VerificationResult, error) {
	if observability.RequestID(ctx) != p.id || token != "SECRET" || n != 42 {
		p.t.Fatal("provider context/arguments changed")
	}
	return &VerificationResult{Success: true, VerifiedAmount: n, VerifiedAt: time.Now()}, nil
}
func TestObservedGatewayPreservesCorrelation(t *testing.T) {
	ctx := observability.NewContext(context.Background())
	v, e := ObserveGateway(observedProbe{t, observability.RequestID(ctx)}).VerifyTransaction(ctx, "SECRET", 42)
	if e != nil || !v.Success || v.VerifiedAmount != 42 {
		t.Fatal("provider result changed")
	}
}
