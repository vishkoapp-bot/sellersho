package customdomain

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEDGE01AllowCacheValueShape (audit §3.5): the cached value type
// carries exactly {Allowed bool, Slug string} — nothing else. The
// slug is already public (the storefront URL path); tenant data,
// supplier-cost fields, pricing, margins and user data must never
// enter this cache. If a field is added here, this test fails on
// purpose: widening the value needs an explicit decision, not a
// drive-by.
func TestEDGE01AllowCacheValueShape(t *testing.T) {
	rt := reflect.TypeOf(AllowDecision{})
	if rt.NumField() != 2 {
		t.Fatalf("AllowDecision has %d fields, want exactly 2 (Allowed, Slug)", rt.NumField())
	}
	if f, ok := rt.FieldByName("Allowed"); !ok || f.Type.Kind() != reflect.Bool {
		t.Fatalf("AllowDecision.Allowed missing or not bool: %+v", rt)
	}
	if f, ok := rt.FieldByName("Slug"); !ok || f.Type.Kind() != reflect.String {
		t.Fatalf("AllowDecision.Slug missing or not string: %+v", rt)
	}
}

func TestEDGE01AllowCacheAllowDenyTTL(t *testing.T) {
	c := NewAllowCache(100, 60*time.Millisecond, 30*time.Millisecond)
	allowed := AllowDecision{Allowed: true, Slug: "shop-a"}
	denied := AllowDecision{Allowed: false}
	c.Store("shop-a.example.com", allowed)
	c.Store("unknown.example.com", denied)
	if got, ok := c.Lookup("shop-a.example.com"); !ok || got != allowed {
		t.Fatalf("allow lookup = %+v, %v; want %+v, true", got, ok, allowed)
	}
	if got, ok := c.Lookup("unknown.example.com"); !ok || got != denied {
		t.Fatalf("deny lookup = %+v, %v; want %+v, true", got, ok, denied)
	}
	// Deny TTL is shorter: the deny entry expires first.
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Lookup("unknown.example.com"); ok {
		t.Fatal("deny entry survived its TTL")
	}
	if _, ok := c.Lookup("shop-a.example.com"); !ok {
		t.Fatal("allow entry expired before its TTL")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Lookup("shop-a.example.com"); ok {
		t.Fatal("allow entry survived its TTL")
	}
}

// TestEDGE01AllowCacheTTLBackstop: revocation without invalidation is
// still bounded by the allow TTL. The cache is the backstop; step 4's
// write-path invalidation is the normal path (tested at the handler
// level in edge01_domain_cache_test.go).
func TestEDGE01AllowCacheTTLBackstop(t *testing.T) {
	c := NewAllowCache(100, 40*time.Millisecond, 10*time.Millisecond)
	// Load 1: approved, cached.
	dec, err, hit := c.Load("shop-a.example.com", func() (AllowDecision, error) {
		return AllowDecision{Allowed: true, Slug: "shop-a"}, nil
	})
	if err != nil || !dec.Allowed || hit {
		t.Fatalf("first load = %+v, %v, hit=%v; want allowed, nil, false", dec, err, hit)
	}
	// The DB now revokes the domain, but nobody invalidates (the
	// missed-invalidation case). Within the TTL the stale allow is
	// still served — that is the documented worst case.
	dec, err, hit = c.Load("shop-a.example.com", func() (AllowDecision, error) {
		t.Fatal("load called while a live allow entry exists")
		return AllowDecision{}, nil
	})
	if err != nil || !dec.Allowed || !hit {
		t.Fatalf("within-TTL load = %+v, %v, hit=%v; want stale allowed, nil, true", dec, err, hit)
	}
	// Past the TTL the next load re-reads the (now revoked) DB state.
	time.Sleep(50 * time.Millisecond)
	dec, err, hit = c.Load("shop-a.example.com", func() (AllowDecision, error) {
		return AllowDecision{Allowed: false}, nil
	})
	if err != nil || dec.Allowed || hit {
		t.Fatalf("post-TTL load = %+v, %v, hit=%v; want denied, nil, false", dec, err, hit)
	}
}

func TestEDGE01AllowCacheLRUEviction(t *testing.T) {
	evictions := 0
	c := NewAllowCache(3, time.Minute, time.Minute)
	c.OnEvict = func() { evictions++ }
	for _, d := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		c.Store(d, AllowDecision{Allowed: true, Slug: d})
	}
	if got := c.Stats().Entries; got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	// Touch a.example.com so it is most-recently used; b is LRU.
	if _, ok := c.Lookup("a.example.com"); !ok {
		t.Fatal("a.example.com missing before eviction")
	}
	c.Store("d.example.com", AllowDecision{Allowed: true, Slug: "d.example.com"})
	if _, ok := c.Lookup("b.example.com"); ok {
		t.Fatal("LRU entry b.example.com survived eviction at the cap")
	}
	for _, d := range []string{"a.example.com", "c.example.com", "d.example.com"} {
		if _, ok := c.Lookup(d); !ok {
			t.Fatalf("%s evicted, want it kept", d)
		}
	}
	if got := c.Stats().Evictions; got != 1 {
		t.Fatalf("evictions = %d, want 1", got)
	}
	if evictions != 1 {
		t.Fatalf("OnEvict calls = %d, want 1", evictions)
	}
}

func TestEDGE01AllowCacheSingleflight(t *testing.T) {
	c := NewAllowCache(100, time.Minute, time.Minute)
	var loads int64
	load := func() (AllowDecision, error) {
		atomic.AddInt64(&loads, 1)
		time.Sleep(50 * time.Millisecond) // hold the miss open
		return AllowDecision{Allowed: true, Slug: "shop-a"}, nil
	}
	const n = 16
	var wg sync.WaitGroup
	results := make([]AllowDecision, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dec, err, _ := c.Load("shop-a.example.com", load)
			results[i], errs[i] = dec, err
		}(i)
	}
	wg.Wait()
	if got := atomic.LoadInt64(&loads); got != 1 {
		t.Fatalf("concurrent misses ran %d loads, want 1 (singleflight)", got)
	}
	for i, dec := range results {
		if errs[i] != nil || !dec.Allowed || dec.Slug != "shop-a" {
			t.Fatalf("goroutine %d: %+v, %v; want allowed shop-a, nil", i, dec, errs[i])
		}
	}
	// A load error is NOT cached: the next caller retries the load.
	failures := 0
	_, err, _ := c.Load("flaky.example.com", func() (AllowDecision, error) {
		failures++
		return AllowDecision{}, errors.New("db down")
	})
	if err == nil {
		t.Fatal("load error swallowed")
	}
	dec, err, hit := c.Load("flaky.example.com", func() (AllowDecision, error) {
		failures++
		return AllowDecision{Allowed: false}, nil
	})
	if err != nil || dec.Allowed || hit || failures != 2 {
		t.Fatalf("post-error load = %+v, %v, hit=%v, loads=%d; want denied, nil, false, 2", dec, err, hit, failures)
	}
}

func TestEDGE01AllowCacheNormalization(t *testing.T) {
	c := NewAllowCache(100, time.Minute, time.Minute)
	c.Store("Shop.Example.COM", AllowDecision{Allowed: true, Slug: "shop"})
	// Case-insensitive keying: every spelling hits the one entry.
	for _, spelling := range []string{"shop.example.com", "SHOP.EXAMPLE.COM", " Shop.Example.Com "} {
		if got, ok := c.Lookup(spelling); !ok || got.Slug != "shop" {
			t.Fatalf("Lookup(%q) = %+v, %v; want shop, true", spelling, got, ok)
		}
	}
	// Invalidate is normalization-aware too.
	c.Invalidate("SHOP.example.com")
	if _, ok := c.Lookup("shop.example.com"); ok {
		t.Fatal("entry survived a case-variant Invalidate")
	}
}

func TestEDGE01AllowCacheMalformedAndIDNNeverCached(t *testing.T) {
	c := NewAllowCache(100, time.Minute, time.Minute)
	malformed := []string{
		"", "not a domain", "https://shop.example.com", "shop.example.com/path",
		"shop.example.com.",   // trailing dot: Normalize rejects
		"127.0.0.1",           // IP: Normalize rejects
		"münchen.example.com", // unicode IDN: punycode form required
		"*.example.com", "a..com", "shop.localhost", "shop.local",
	}
	for _, d := range malformed {
		c.Store(d, AllowDecision{Allowed: true, Slug: "should-never-land"})
		if _, ok := c.Lookup(d); ok {
			t.Fatalf("malformed %q entered the cache", d)
		}
		// Load on a malformed domain skips the cache: the loader runs
		// every time (fail-closed, uncached) and nothing is stored.
		calls := 0
		dec, err, hit := c.Load(d, func() (AllowDecision, error) {
			calls++
			return AllowDecision{Allowed: false}, nil
		})
		if err != nil || dec.Allowed || hit || calls != 1 {
			t.Fatalf("Load(%q) = %+v, %v, hit=%v, calls=%d; want denied, nil, false, 1", d, dec, err, hit, calls)
		}
	}
	// The punycode form of an IDN IS a normalizable domain: it may be
	// cached (as a deny for an unregistered name), like any ASCII name.
	c.Store("xn--mnchen-3ya.example.com", AllowDecision{Allowed: false})
	if _, ok := c.Lookup("xn--mnchen-3ya.example.com"); !ok {
		t.Fatal("punycode IDN deny was not cached")
	}
	if got := c.Stats().Entries; got != 1 {
		t.Fatalf("entries = %d, want 1 (only the punycode deny)", got)
	}
}

func TestEDGE01AllowCacheDenyCaching(t *testing.T) {
	c := NewAllowCache(100, time.Minute, time.Minute)
	calls := 0
	load := func() (AllowDecision, error) {
		calls++
		return AllowDecision{Allowed: false}, nil
	}
	if _, _, hit := c.Load("unknown.example.com", load); hit {
		t.Fatal("first load reported a hit")
	}
	if _, _, hit := c.Load("unknown.example.com", load); !hit {
		t.Fatal("deny was not cached (second load missed)")
	}
	if calls != 1 {
		t.Fatalf("deny load ran %d times, want 1", calls)
	}
}
