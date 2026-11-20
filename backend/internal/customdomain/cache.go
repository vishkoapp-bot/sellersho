// Package customdomain — EDGE-01 step 3: bounded hostname/ownership cache
// for the allow check (audit §3.2, owner decision D-1).
//
// Every HTTPS request pays one synchronous forward_auth call to
// /internal/domains/allow, and before this cache every custom-domain
// call ran one uncached DB query whose correlated subquery seq-scanned
// pending_approvals. The cache lives in the backend, behind the allow
// endpoint: Caddy still calls once per outer request (allow RPS stays
// 1.0x), but the handler answers from memory on a hit, so allow DB
// queries collapse from 1.0x per custom request to the cache-refresh
// rate (distinct domains / TTL + misses).
//
// Key: the normalized domain string (customdomain.Normalize —
// normalization-before-keying is load-bearing: EXAMPLE.com and
// example.com must hit one entry. A host:port form such as
// example.com:443 is rejected by Normalize — fail-closed, never
// cached, never an entry.) Malformed
// input never enters the cache: Normalize failure is a miss/no-op.
//
// Value: AllowDecision — {Allowed bool, Slug string} and NOTHING
// else. The slug is already public (it is the storefront URL path).
// No tenant data, no supplier-cost fields, no pricing, no margins,
// no user data ever enters this cache. The value is exactly what the
// two callers need: handleDomainPermission needs Allowed,
// handleDomainStore needs Slug, so the two can never disagree (today
// they can, in the window between two identical queries).
//
// Bounds: hard cap on entries (DefaultAllowCacheMaxEntries, a constant
// in code — not env — so it cannot be mis-set to unbounded in prod)
// with LRU eviction, plus singleflight for concurrent misses on the
// same domain (a miss storm for one new domain costs one DB query).
// Negative results are cached too (deny TTL), which bounds scanner
// storms: every random-subdomain TLS handshake still costs its ask
// call, but the PK-miss query behind it is bounded.
//
// TTL (owner decision D-1, Nov 20): allow 60s, deny 10s. TTL is a
// BACKSTOP, not the revocation mechanism: every domain
// approval/revocation write path invalidates the entry synchronously
// (step 4), so revocation is normally immediate and 60s worst-case.
// The cache is deliberately NOT persisted: after an API restart it
// is cold and the first request per domain hits the DB exactly as
// before (one query per distinct domain, then warm). Persisting it
// would risk serving stale revocations after a restore.
package customdomain

import (
	"container/list"
	"os"
	"sync"
	"time"
)

// AllowDecision is the entire cached value for one normalized domain.
// It is intentionally a two-field struct: any future field added here
// must pass the "already public?" test (the slug is public; tenant
// data, pricing, margins and user data are not). cache_test.go
// asserts the field set by reflection so a widening fails loudly.
type AllowDecision struct {
	Allowed bool
	Slug    string // storefront slug, only meaningful when Allowed
}

// EDGE-01 D-1 (owner-settled Nov 20): allow entries live 60s, deny
// entries 10s. Deny is shorter so a just-approved domain becomes
// reachable quickly; allow is bounded by the revocation SLA.
const (
	DefaultAllowCacheMaxEntries = 10000
	DefaultAllowCacheTTL        = 60 * time.Second
	DefaultDenyCacheTTL         = 10 * time.Second
)

// Env overrides for the TTLs (Go durations, e.g. "60s"). Invalid or
// missing values fall back to the D-1 defaults. The entry cap stays
// a code constant on purpose (see the package doc). NOTE (EDGE-01
// supervisor M-2): these two overrides are intentional and
// undocumented outside this file — nothing in prod sets them, so the
// shipped TTLs are exactly the D-1 defaults (allow 60s / deny 10s).
// They exist only for bench/harness tuning; do not set them in prod
// without a new owner decision, and they deliberately bypass
// config.go so a config typo can never silently change the
// revocation SLA.
const (
	envAllowTTL = "EDGE01_ALLOW_CACHE_TTL_ALLOW"
	envDenyTTL  = "EDGE01_ALLOW_CACHE_TTL_DENY"
)

func ttlFromEnv(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// DefaultAllowCache is the process-wide allow cache. The backend runs
// a single container, so an in-process cache invalidated at the write
// path (step 4) is exact; if a second replica ever appears,
// invalidation must be broadcast (or the TTL backstop applies) — a
// stated future constraint, not a v1 requirement.
var DefaultAllowCache = NewAllowCache(
	DefaultAllowCacheMaxEntries,
	ttlFromEnv(envAllowTTL, DefaultAllowCacheTTL),
	ttlFromEnv(envDenyTTL, DefaultDenyCacheTTL),
)

type cacheEntry struct {
	key       string
	decision  AllowDecision
	expiresAt time.Time
	elem      *list.Element // element in lru, value *cacheEntry
}

// allowCall is one in-flight load for a domain (singleflight): the
// first goroutine to miss runs the load, the rest wait on done.
type allowCall struct {
	done chan struct{}
	dec  AllowDecision
	err  error
}

// AllowCacheStats is a snapshot of the cache counters. Hits/misses
// are also mirrored into the observability registry by the repository
// call site (which owns the metrics); evictions are mirrored via the
// OnEvict callback.
type AllowCacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Entries   int
}

// AllowCache is a bounded LRU cache of allow decisions keyed by
// normalized domain, with per-decision TTLs and singleflight loads.
// The zero value is not usable; use NewAllowCache.
type AllowCache struct {
	mu       sync.Mutex
	max      int
	allowTTL time.Duration
	denyTTL  time.Duration
	entries  map[string]*cacheEntry
	lru      *list.List // front = most recently used
	inflight map[string]*allowCall
	hits     int64
	misses   int64
	evict    int64

	// OnEvict, when set, is called (without holding the cache mutex)
	// once per LRU eviction. The repository wires it to the
	// observability counter; the cache itself stays dependency-free.
	OnEvict func()
}

func NewAllowCache(maxEntries int, allowTTL, denyTTL time.Duration) *AllowCache {
	if maxEntries <= 0 {
		maxEntries = DefaultAllowCacheMaxEntries
	}
	if allowTTL <= 0 {
		allowTTL = DefaultAllowCacheTTL
	}
	if denyTTL <= 0 {
		denyTTL = DefaultDenyCacheTTL
	}
	return &AllowCache{
		max:      maxEntries,
		allowTTL: allowTTL,
		denyTTL:  denyTTL,
		entries:  make(map[string]*cacheEntry),
		lru:      list.New(),
		inflight: make(map[string]*allowCall),
	}
}

func (c *AllowCache) ttlFor(d AllowDecision) time.Duration {
	if d.Allowed {
		return c.allowTTL
	}
	return c.denyTTL
}

// lookupLocked returns the entry for a normalized key, or nil. Expired
// entries are dropped. Caller must hold c.mu.
func (c *AllowCache) lookupLocked(key string) *cacheEntry {
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		c.removeLocked(e)
		return nil
	}
	return e
}

func (c *AllowCache) removeLocked(e *cacheEntry) {
	c.lru.Remove(e.elem)
	delete(c.entries, e.key)
}

// Lookup returns the cached decision for domain, or false on any miss
// (absent, expired, or malformed domain — malformed input never
// enters the cache, so it can never hit). The returned decision is a
// copy; callers cannot mutate cache state through it.
func (c *AllowCache) Lookup(domain string) (AllowDecision, bool) {
	key, err := Normalize(domain)
	if err != nil {
		return AllowDecision{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.lookupLocked(key)
	if e == nil {
		c.misses++
		return AllowDecision{}, false
	}
	c.lru.MoveToFront(e.elem)
	c.hits++
	return e.decision, true
}

// Store records a decision for domain. Malformed domains are dropped
// silently (fail-closed: the caller still answers from the DB).
// Storing evicts the least-recently-used entry when the cache is at
// its cap.
func (c *AllowCache) Store(domain string, d AllowDecision) {
	key, err := Normalize(domain)
	if err != nil {
		return
	}
	c.mu.Lock()
	evicted := c.storeLocked(key, d)
	onEvict := c.OnEvict
	c.mu.Unlock()
	if evicted && onEvict != nil {
		onEvict()
	}
}

func (c *AllowCache) storeLocked(key string, d AllowDecision) (evicted bool) {
	if e := c.lookupLocked(key); e != nil {
		e.decision = d
		e.expiresAt = time.Now().Add(c.ttlFor(d))
		c.lru.MoveToFront(e.elem)
		return false
	}
	for len(c.entries) >= c.max {
		back := c.lru.Back()
		if back == nil {
			break
		}
		c.removeLocked(back.Value.(*cacheEntry))
		c.evict++
		evicted = true
	}
	e := &cacheEntry{key: key, decision: d, expiresAt: time.Now().Add(c.ttlFor(d))}
	e.elem = c.lru.PushFront(e)
	c.entries[key] = e
	return evicted
}

// Load returns the cached decision for domain, or calls load exactly
// once per concurrent miss storm for that domain (singleflight) and
// caches its result. A load error is returned as-is and is NOT
// cached, so a transient DB failure never poisons the cache; a nil
// error with Allowed=false IS cached (deny, short TTL). The third
// return reports whether the value came from cache (a hit), for the
// call site's metrics. Callers pass an already-normalized domain;
// an unnormalizable domain skips the cache entirely and calls load
// directly (fail-closed, uncached).
func (c *AllowCache) Load(domain string, load func() (AllowDecision, error)) (AllowDecision, error, bool) {
	key, err := Normalize(domain)
	if err != nil {
		dec, err := load()
		return dec, err, false
	}
	for {
		c.mu.Lock()
		if e := c.lookupLocked(key); e != nil {
			c.lru.MoveToFront(e.elem)
			c.hits++
			dec := e.decision
			c.mu.Unlock()
			return dec, nil, true
		}
		if call, ok := c.inflight[key]; ok {
			c.mu.Unlock()
			<-call.done
			continue // re-check the cache: the leader stored (or failed)
		}
		// Only the goroutine that becomes leader counts a miss: a
		// waiter that finds the leader's entry on re-check is a hit.
		c.misses++
		call := &allowCall{done: make(chan struct{})}
		c.inflight[key] = call
		c.mu.Unlock()

		dec, err := load()
		c.mu.Lock()
		call.dec = dec
		call.err = err
		delete(c.inflight, key)
		evicted := false
		if err == nil {
			evicted = c.storeLocked(key, dec)
		}
		onEvict := c.OnEvict
		close(call.done)
		c.mu.Unlock()
		if evicted && onEvict != nil {
			onEvict()
		}
		return dec, err, false
	}
}

// Invalidate drops the cached decision for domain, if any. This is
// the step-4 write-path hook: every domain approval/revocation write
// path calls it after a successful commit, so revocation is normally
// immediate (the 60s allow TTL is only the backstop for a missed
// invalidation). Malformed input is a no-op. Best-effort by design:
// a cache miss here is not an error.
func (c *AllowCache) Invalidate(domain string) {
	key, err := Normalize(domain)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		c.removeLocked(e)
	}
}

// Stats returns a snapshot of the hit/miss/eviction counters and the
// current entry count.
func (c *AllowCache) Stats() AllowCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return AllowCacheStats{Hits: c.hits, Misses: c.misses, Evictions: c.evict, Entries: len(c.entries)}
}

// Evictions returns the LRU eviction count (convenience for the call
// site's metrics delta).
func (c *AllowCache) Evictions() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evict
}
