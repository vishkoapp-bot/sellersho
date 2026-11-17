package testredis_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/ippanel"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testredis"
	"github.com/redis/go-redis/v9"
)

// The proxy points only at the marker-verified disposable target, never app config.
func TestOPS02StalledRedisReservationHonorsDeadlineAndNeverSends(t *testing.T) {
	guarded := testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var block atomic.Bool
	release := make(chan struct{})
	defer close(release)
	var wg sync.WaitGroup
	go func() {
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			upstream, err := net.DialTimeout("tcp", guarded.Options().Addr, time.Second)
			if err != nil {
				client.Close()
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				defer upstream.Close()
				go func() { io.Copy(upstream, client); upstream.Close() }()
				buffer := make([]byte, 16384)
				for {
					n, err := upstream.Read(buffer)
					if n > 0 {
						if block.Load() {
							<-release
						}
						if _, e := client.Write(buffer[:n]); e != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	opts := *guarded.Options()
	opts.Addr = listener.Addr().String()
	opts.MaxRetries = -1
	proxied := redis.NewClient(&opts)
	defer proxied.Close()
	if err := proxied.Ping(context.Background()).Err(); err != nil {
		t.Fatal("disposable proxy warmup failed")
	}
	var sends atomic.Int32
	sms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Write([]byte(`{"meta":{"status":true},"data":{"message_outbox_ids":["synthetic"]}}`))
	}))
	defer sms.Close()
	service := auth.NewOTPService(&repository.RedisClient{Client: proxied}, "production")
	service.ConfigureSMS(ippanel.New(sms.URL, "synthetic", "synthetic"), "synthetic", nil)
	block.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = service.GenerateAndStoreOTP(ctx, "09120000000")
	if err == nil {
		t.Fatal("unconfirmed Redis reservation accepted")
	}
	if elapsed := time.Since(started); elapsed > 600*time.Millisecond {
		t.Fatalf("Redis ignored caller deadline: %s", elapsed)
	}
	if sends.Load() != 0 {
		t.Fatal("SMS sent without confirmed reservation")
	}
}
