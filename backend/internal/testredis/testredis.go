// Package testredis verifies a separately provisioned disposable Redis marker
// before integration tests may write. It never reads application configuration.
package testredis

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/drup/platform/internal/dependencybudget"
	"github.com/redis/go-redis/v9"
)

const MarkerKey = "sellersho:test-guard"

var tokenPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)
var passwordPattern = regexp.MustCompile(`^[a-f0-9]{48}$`)

func Identity(dsn, token string) (*redis.Options, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "redis" || u.Hostname() != "127.0.0.1" || u.Path != "/15" || u.RawQuery != "" || u.Fragment != "" || u.User == nil || !tokenPattern.MatchString(token) {
		return nil, errors.New("test Redis requires explicit loopback DB15 URL and disposable marker identity")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 || port == 6379 {
		return nil, errors.New("test Redis requires an explicit non-application loopback port")
	}
	password, ok := u.User.Password()
	if !ok || !passwordPattern.MatchString(password) || u.User.Username() != "" {
		return nil, errors.New("test Redis requires unique disposable credentials")
	}
	opts, err := redis.ParseURL(dsn)
	if err != nil {
		return nil, errors.New("invalid disposable Redis URL")
	}
	dependencybudget.ConfigureRedis(opts)
	return opts, nil
}

// Only missing opt-in skips. Malformed, unreachable and unmarked explicit
// targets fail closed with redacted errors. Marker creation belongs to the runner.
func Open(t testing.TB, dsn, token string) *redis.Client {
	t.Helper()
	if dsn == "" && token == "" {
		t.Skip("Redis integration requires an explicit disposable test URL and marker")
	}
	opts, err := Identity(dsn, token)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	marker, err := client.Get(ctx, MarkerKey).Result()
	if err != nil || marker != token {
		t.Fatal("refusing Redis target: disposable identity/marker verification failed")
	}
	return client
}
