package payment

import (
	"context"
	"github.com/drup/platform/internal/observability"
	"time"
)

type observedGateway struct{ next PaymentGatewayClient }

func ObserveGateway(next PaymentGatewayClient) PaymentGatewayClient {
	if next == nil {
		return nil
	}
	return observedGateway{next}
}
func (g observedGateway) VerifyTransaction(ctx context.Context, token string, amount int64) (v *VerificationResult, err error) {
	start := time.Now()
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		if ctx.Err() == context.DeadlineExceeded {
			outcome = "timeout"
		} else if ctx.Err() == context.Canceled {
			outcome = "canceled"
		}
		observability.LogOperation(ctx, "provider", "verify", outcome, time.Since(start))
	}()
	return g.next.VerifyTransaction(ctx, token, amount)
}
