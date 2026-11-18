package ippanel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drup/platform/internal/observability"
)

// OBS-01 D-09: the request correlation ID propagates to IPPanel.
func TestRequestPropagatesRequestID(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{},"meta":{"status":true,"message":""}}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", "sender")
	ctx := observability.NewContext(context.Background())
	want := observability.RequestID(ctx)
	if len(want) != 32 {
		t.Fatalf("request id %q, want 32 hex chars", want)
	}
	if _, err := c.Credit(ctx); err != nil {
		t.Fatalf("credit: %v", err)
	}
	if got != want {
		t.Fatalf("X-Request-ID sent %q, want %q", got, want)
	}
}

func TestRequestSendsNoRequestIDWithoutOne(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{},"meta":{"status":true,"message":""}}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "tok", "sender")
	if _, err := c.Credit(context.Background()); err != nil {
		t.Fatalf("credit: %v", err)
	}
	if got != "" {
		t.Fatalf("X-Request-ID sent %q without a request context, want absent", got)
	}
}
