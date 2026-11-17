package tapin

import (
	"context"
	"github.com/drup/platform/internal/dependencybudget"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTapinSafeReadAllowlistAndRegistrationNeverReplays(t *testing.T) {
	old := sharedBudget
	defer func() { sharedBudget = old }()
	p := dependencybudget.DefaultPolicy(2, 100)
	p.BackoffMin = time.Millisecond
	p.BackoffMax = 2 * time.Millisecond
	for _, path := range []string{"/api/v4/tipax/public/user/order/options/", "/api/v4/tipax/public/user/order/check-price/", "/api/v4/tipax/public/user/order/filter/", "/api/v4/tipax/public/user/order/register/", "/unclassified-command"} {
		t.Run(path, func(t *testing.T) {
			sharedBudget = dependencybudget.New(p)
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					return
				}
				w.Write([]byte(`{"returns":{"status":20},"entries":{}}`))
			}))
			defer s.Close()
			_, err := NewClient(s.URL).Call(context.Background(), "synthetic", path, map[string]any{})
			read := path == "/api/v4/tipax/public/user/order/options/" || path == "/api/v4/tipax/public/user/order/check-price/" || path == "/api/v4/tipax/public/user/order/filter/"
			if read {
				if err != nil || calls.Load() != 2 {
					t.Fatalf("read retries %d %v", calls.Load(), err)
				}
			} else if !dependencybudget.IsAmbiguous(err) || calls.Load() != 1 {
				t.Fatalf("mutation replay/false certainty %d %v", calls.Load(), err)
			}
		})
	}
}

func TestTapinMissingRegistrationStatusStaysUnknown(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"returns":{}}`, `{"returns":{"status":null}}`, `{"returns":{"status":0}}`} {
		t.Run(body, func(t *testing.T) {
			old := sharedBudget
			sharedBudget = dependencybudget.New(dependencybudget.DefaultPolicy(2, 100))
			defer func() { sharedBudget = old }()
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Write([]byte(body))
			}))
			defer s.Close()
			_, err := NewClient(s.URL).Call(context.Background(), "synthetic", "/api/v4/tipax/public/user/order/register/", map[string]any{})
			if !dependencybudget.IsAmbiguous(err) || calls.Load() != 1 {
				t.Fatalf("missing status permits duplicate registration calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
