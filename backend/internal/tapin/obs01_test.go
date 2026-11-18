package tapin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/observability"
)

// capturingServer records the X-Request-ID of the last request and
// answers with a Tapin success envelope.
func capturingServer(t *testing.T, got *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"returns":{"status":20,"message":"موفق"},"entries":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// OBS-01 D-09: the request correlation ID propagates to Tapin.
func TestCallPropagatesRequestID(t *testing.T) {
	var got string
	srv := capturingServer(t, &got)
	c := NewClient(srv.URL)
	ctx := observability.NewContext(context.Background())
	want := observability.RequestID(ctx)
	if len(want) != 32 {
		t.Fatalf("request id %q, want 32 hex chars", want)
	}
	if err := c.Options(ctx, "tok", "shop-1"); err != nil {
		t.Fatalf("options: %v", err)
	}
	if got != want {
		t.Fatalf("X-Request-ID sent %q, want %q", got, want)
	}
}

func TestCallSendsNoRequestIDWithoutOne(t *testing.T) {
	var got string
	srv := capturingServer(t, &got)
	c := NewClient(srv.URL)
	if err := c.Options(context.Background(), "tok", "shop-1"); err != nil {
		t.Fatalf("options: %v", err)
	}
	if got != "" {
		t.Fatalf("X-Request-ID sent %q without a request context, want absent", got)
	}
}

// OBS-01 D-09: the GET path (label proxy) propagates the ID too.
func TestGetRawPropagatesRequestID(t *testing.T) {
	var got string
	srv := capturingServer(t, &got)
	c := NewClient(srv.URL)
	ctx := observability.NewContext(context.Background())
	want := observability.RequestID(ctx)
	if _, _, _, err := c.GetRaw(ctx, "tok", "/api/v4/tipax/public/all/order/parcel/label/abc"); err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if got != want {
		t.Fatalf("X-Request-ID sent %q, want %q", got, want)
	}
}
