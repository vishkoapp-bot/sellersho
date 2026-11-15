// Package requestbudget bounds synchronous HTTP work without detached handlers.
package requestbudget

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	API, Upload, Domain, Static, WriteMargin time.Duration
	APIConcurrent, UploadConcurrent          int
}

func DefaultConfig() Config {
	return Config{API: 8 * time.Second, Upload: 60 * time.Second, Domain: 2 * time.Second, Static: 20 * time.Second, WriteMargin: 2 * time.Second, APIConcurrent: 64, UploadConcurrent: 4}
}
func IsUpload(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path == "/api/v1/upload/product-image" {
		return true
	}
	p := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(p) < 6 || p[0] != "api" || p[1] != "v1" || p[2] != "supplier" || p[3] != "products" || p[4] == "" {
		return false
	}
	return len(p) == 6 && p[5] == "images" || len(p) == 8 && p[5] == "variants" && p[6] != "" && p[7] == "images"
}
func New(cfg Config) func(http.Handler) http.Handler {
	if cfg.API <= 0 || cfg.Upload <= 0 || cfg.Domain <= 0 || cfg.Static <= 0 || cfg.WriteMargin <= 0 || cfg.APIConcurrent < 1 || cfg.UploadConcurrent < 1 {
		panic("invalid request budget")
	}
	ordinary, uploads := make(chan struct{}, cfg.APIConcurrent), make(chan struct{}, cfg.UploadConcurrent)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			duration, margin, slots := cfg.API, cfg.WriteMargin, ordinary
			switch {
			case IsUpload(r):
				duration, margin, slots = cfg.Upload, 5*time.Second, uploads
			case r.URL.Path == "/internal/domains/allow":
				duration, margin = cfg.Domain, time.Second
			case strings.HasPrefix(r.URL.Path, "/static/"):
				duration = cfg.Static
			}
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			deadline, _ := ctx.Deadline()
			guard := &deadlineWriter{ResponseWriter: w, ctx: ctx, deadline: deadline, mutation: r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"}
			if guard.expired() {
				guard.timeout()
				return
			}
			if ctx.Err() != nil {
				panic(http.ErrAbortHandler)
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusServiceUnavailable)
				io.WriteString(w, `{"code":"request_overloaded","error":"سامانه موقتاً شلوغ است؛ کمی بعد دوباره تلاش کنید.","retryable":true}`)
				return
			}
			controller := http.NewResponseController(w)
			if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
				panic(http.ErrAbortHandler)
			}
			if err := controller.SetWriteDeadline(deadline.Add(margin)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				panic(http.ErrAbortHandler)
			}
			next.ServeHTTP(guard, r.WithContext(ctx))
			if guard.expired() {
				if guard.status == 0 {
					guard.timeout()
				} else if !guard.timedOut {
					panic(http.ErrAbortHandler)
				}
			} else if ctx.Err() != nil {
				panic(http.ErrAbortHandler)
			}
		})
	}
}

type deadlineWriter struct {
	http.ResponseWriter
	ctx      context.Context
	deadline time.Time
	mutation bool
	status   int
	timedOut bool
}

func (w *deadlineWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *deadlineWriter) expired() bool {
	return w.ctx.Err() == context.DeadlineExceeded || !time.Now().Before(w.deadline)
}
func (w *deadlineWriter) timeout() {
	if w.status != 0 {
		return
	}
	w.status = http.StatusGatewayTimeout
	w.timedOut = true
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(w.status)
	if w.mutation {
		io.WriteString(w.ResponseWriter, `{"code":"request_timeout","error":"نتیجه درخواست هنوز مشخص نیست؛ پیش از ثبت دوباره، نتیجه قبلی را بررسی کنید.","outcome":"unknown","retryable":false}`)
	} else {
		io.WriteString(w.ResponseWriter, `{"code":"request_timeout","error":"زمان پاسخ‌گویی تمام شد؛ لطفاً دوباره تلاش کنید.","retryable":true}`)
	}
}
func (w *deadlineWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if w.expired() {
		w.timeout()
		return
	}
	if w.ctx.Err() != nil {
		return
	}
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *deadlineWriter) Write(data []byte) (int, error) {
	if w.timedOut {
		return 0, context.DeadlineExceeded
	}
	if w.expired() {
		w.timeout()
		return 0, context.DeadlineExceeded
	}
	if w.ctx.Err() != nil {
		return 0, w.ctx.Err()
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(data)
}
func (w *deadlineWriter) Flush() {
	if w.expired() {
		if w.status == 0 {
			w.timeout()
			return
		}
		panic(http.ErrAbortHandler)
	}
	if w.status == 0 {
		w.WriteHeader(200)
	}
	http.NewResponseController(w.ResponseWriter).Flush()
}
