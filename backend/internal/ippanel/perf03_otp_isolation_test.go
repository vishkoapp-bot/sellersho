package ippanel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/drup/platform/internal/dependencybudget"
)

// PERF-03 G3: OTP sends run on their own executor. The token rate stays
// 8/s (unchanged); what changes is isolation — admin Credit reads must
// not consume OTP tokens, and OTP bursts must not consume admin tokens.

func perf03BudgetFixture(t *testing.T, inflight, rate int) {
	t.Helper()
	oldShared, oldOTP := sharedBudget, otpBudget
	p := dependencybudget.DefaultPolicy(inflight, rate)
	p.BackoffMin = time.Millisecond
	p.BackoffMax = 2 * time.Millisecond
	sharedBudget = dependencybudget.New(p)
	otpBudget = dependencybudget.New(p)
	t.Cleanup(func() { sharedBudget, otpBudget = oldShared, oldOTP })
}

func perf03IPPanelServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/api/payment/credit/mine":
			io.WriteString(w, `{"meta":{"status":true},"data":{"balance":10}}`)
		case "/api/send":
			io.WriteString(w, `{"meta":{"status":true},"data":{"message_outbox_ids":["synthetic"]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestPERF03AdminReadsDoNotStarveOTP(t *testing.T) {
	// Burst 2, rate 2/s on BOTH budgets: two calls drain one budget.
	perf03BudgetFixture(t, 8, 2)
	s := perf03IPPanelServer(t)
	defer s.Close()
	c := New(s.URL, "synthetic", "synthetic")
	ctx := context.Background()

	if _, err := c.Credit(ctx); err != nil {
		t.Fatalf("credit 1: %v", err)
	}
	if _, err := c.Credit(ctx); err != nil {
		t.Fatalf("credit 2: %v", err)
	}
	// Shared budget is now empty — a third admin read sheds.
	if _, err := c.Credit(ctx); !errors.Is(err, dependencybudget.ErrOverloaded) {
		t.Fatalf("credit 3: err=%v, want ErrOverloaded (shared budget drained)", err)
	}
	// ...but OTP still has its own full burst of 2.
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); err != nil {
		t.Fatalf("OTP send after drained admin budget: %v (isolation broken)", err)
	}
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); err != nil {
		t.Fatalf("OTP send 2: %v", err)
	}
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); !errors.Is(err, dependencybudget.ErrOverloaded) {
		t.Fatalf("OTP send 3: err=%v, want ErrOverloaded (OTP budget drained at its own rate)", err)
	}
}

func TestPERF03OTPBurstsDoNotStarveAdminReads(t *testing.T) {
	perf03BudgetFixture(t, 8, 2)
	s := perf03IPPanelServer(t)
	defer s.Close()
	c := New(s.URL, "synthetic", "synthetic")
	ctx := context.Background()

	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); err != nil {
		t.Fatalf("OTP send 1: %v", err)
	}
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); err != nil {
		t.Fatalf("OTP send 2: %v", err)
	}
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); !errors.Is(err, dependencybudget.ErrOverloaded) {
		t.Fatalf("OTP send 3: err=%v, want ErrOverloaded", err)
	}
	// Admin reads are untouched by the OTP burst.
	if _, err := c.Credit(ctx); err != nil {
		t.Fatalf("credit after drained OTP budget: %v (isolation broken)", err)
	}
}

func TestPERF03OTPRateUnchangedAtEightPerSecond(t *testing.T) {
	// The production OTP budget: 8/s, burst 8. Eight rapid sends pass;
	// the ninth sheds — the rate was NOT raised by the isolation work.
	perf03BudgetFixture(t, 8, 8)
	s := perf03IPPanelServer(t)
	defer s.Close()
	c := New(s.URL, "synthetic", "synthetic")
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); err != nil {
			t.Fatalf("OTP send %d: %v", i+1, err)
		}
	}
	if _, err := c.SendPattern(ctx, "09120000000", "synthetic", nil); !errors.Is(err, dependencybudget.ErrOverloaded) {
		t.Fatalf("OTP send 9: err=%v, want ErrOverloaded (8/s rate preserved)", err)
	}
}
