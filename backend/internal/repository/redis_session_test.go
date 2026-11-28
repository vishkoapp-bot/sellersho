package repository

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRedisSessionRevocationExpiresWithSession(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	digest := strings.Repeat("ab", 32)
	key := revokedSessionPrefix + digest
	t.Cleanup(func() { client.Client.Del(ctx, key) })

	expiration := time.Now().Truncate(time.Second).Add(time.Minute)
	for range 2 {
		if err := client.RevokeSession(ctx, digest, expiration); err != nil {
			t.Fatal(err)
		}
	}
	if revoked, err := client.IsSessionRevoked(ctx, digest); err != nil || !revoked {
		t.Fatalf("session revocation missing: %v, %v", revoked, err)
	}
	if ttl, err := client.Client.TTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("revocation must expire with the credential: %v, %v", ttl, err)
	}
	if err := client.RevokeSession(ctx, digest, expiration.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if revoked, err := client.IsSessionRevoked(ctx, digest); err != nil || revoked {
		t.Fatalf("expired revocation remained active: %v, %v", revoked, err)
	}
}
