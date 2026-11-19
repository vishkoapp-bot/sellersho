package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drup/platform/internal/ippanel"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testredis"
)

func TestOPS02LostSMSResponseRetainsUsableCodeAndCooldown(t *testing.T) {
	ctx := context.Background()
	client := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	var calls atomic.Int32
	var delivered string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Params map[string]string `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid SMS request")
		}
		delivered = body.Params["code"]
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer upstream.Close()
	svc := NewOTPService(client, "production")
	svc.ConfigureSMS(ippanel.New(upstream.URL, "synthetic-token", "synthetic-sender"), "synthetic-pattern", nil)
	mobile := "09120000000"
	if _, err := svc.GenerateAndStoreOTP(ctx, mobile); err == nil {
		t.Fatal("lost response acknowledged")
	}
	stored, err := client.Client.Get(ctx, "otp:"+mobile).Result()
	if err != nil || stored == "" || stored != delivered {
		t.Fatal("delivered OTP is not available after response loss")
	}
	if _, err := svc.GenerateAndStoreOTP(ctx, mobile); err == nil {
		t.Fatal("uncertain SMS was sent again")
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate SMS calls: %d", calls.Load())
	}
	if !svc.VerifyOTP(ctx, mobile, delivered) {
		t.Fatal("customer cannot use delivered code after response loss")
	}
}
func TestOPS02NoRedisDoesNotReportOTPDelivery(t *testing.T) {
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Write([]byte(`{"meta":{"status":true},"data":{"message_outbox_ids":["synthetic"]}}`))
	}))
	defer upstream.Close()
	svc := NewOTPService(nil, "production")
	svc.ConfigureSMS(ippanel.New(upstream.URL, "synthetic", "synthetic"), "synthetic", nil)
	if _, err := svc.GenerateAndStoreOTP(context.Background(), "09120000000"); err == nil {
		t.Fatal("OTP without durable reservation reported success")
	}
	if sends.Load() != 0 {
		t.Fatal("SMS sent without confirmed Redis reservation")
	}
}
func TestOPS02KnownSMSRejectionCannotDeleteNewGeneration(t *testing.T) {
	ctx := context.Background()
	client := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	mobile := "09120000000"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client.Client.Set(ctx, "otp:"+mobile, "new-code", time.Minute)
		client.Client.Set(ctx, "otp:cooldown:"+mobile, "new-generation", time.Minute)
		w.Write([]byte(`{"meta":{"status":false,"message":"synthetic rejection"},"data":{}}`))
	}))
	defer upstream.Close()
	svc := NewOTPService(client, "production")
	svc.ConfigureSMS(ippanel.New(upstream.URL, "synthetic", "synthetic"), "synthetic", nil)
	if _, err := svc.GenerateAndStoreOTP(ctx, mobile); err == nil {
		t.Fatal("rejected send reported success")
	}
	if code, _ := client.Client.Get(ctx, "otp:"+mobile).Result(); code != "new-code" {
		t.Fatal("old cleanup deleted newer code")
	}
	if gen, _ := client.Client.Get(ctx, "otp:cooldown:"+mobile).Result(); gen != "new-generation" {
		t.Fatal("old cleanup deleted newer cooldown")
	}
}
func TestOPS02KnownSMSRejectionClearsOnlyOwnReservation(t *testing.T) {
	ctx := context.Background()
	client := &repository.RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"meta":{"status":false,"message":"synthetic rejection"},"data":{}}`))
	}))
	defer upstream.Close()
	svc := NewOTPService(client, "production")
	svc.ConfigureSMS(ippanel.New(upstream.URL, "synthetic", "synthetic"), "synthetic", nil)
	// PERF-04: distinct mobile from TestOPS02KnownSMSRejectionCannotDeleteNewGeneration,
	// which deliberately leaves otp:/otp:cooldown: for 09120000000 behind on the
	// shared disposable Redis; reusing that mobile made this test fail on the
	// leftover cooldown (proven on pristine otp.go, pre-existing).
	if _, err := svc.GenerateAndStoreOTP(ctx, "09120000001"); err == nil {
		t.Fatal("rejection accepted")
	}
	if n, err := client.Client.Exists(ctx, "otp:09120000001", "otp:cooldown:09120000001").Result(); err != nil || n != 0 {
		t.Fatalf("definite rejection retained own reservation %d %v", n, err)
	}
}
