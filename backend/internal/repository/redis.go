package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/observability"
	"github.com/redis/go-redis/v9"
)

// RedisClient manages Redis cache and distributed lock connections
type RedisClient struct {
	Client *redis.Client
}

// NewRedisClient initializes Redis connection
func NewRedisClient(redisURL string) (*RedisClient, error) {
	return NewRedisClientContext(context.Background(), redisURL)
}

func NewRedisClientContext(parent context.Context, redisURL string) (*RedisClient, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL initialization failed; configuration value withheld")
	}

	dependencybudget.ConfigureRedis(opts)
	client := redis.NewClient(opts)
	client.AddHook(observability.RedisHook{Registry: observability.Default})

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, errors.New("Redis connection failed; dependency details withheld")
	}

	return &RedisClient{Client: client}, nil
}

// SetAdToken records ad click token to prevent duplicate CPC charge
func (r *RedisClient) SetAdToken(ctx context.Context, token string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("ad_token:%s", token)
	return r.Client.SetNX(ctx, key, 1, ttl).Result()
}

// GetJSON retrieves a cached JSON object from Redis
func (r *RedisClient) GetJSON(ctx context.Context, key string, dest interface{}) (bool, error) {
	data, err := r.Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return false, err
	}
	return true, nil
}

// SetJSON caches an object serialized as JSON in Redis with a TTL
func (r *RedisClient) SetJSON(ctx context.Context, key string, val interface{}, ttl time.Duration) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("failed to marshal cache value: %w", err)
	}
	return r.Client.Set(ctx, key, data, ttl).Err()
}

// InventoryHoldRef identifies one hold pool: a product, optionally one of its
// variants. VariantID "" addresses the legacy product-level pool, so holds
// placed before PERF-04 keep their exact key.
type InventoryHoldRef struct {
	ProductID string
	VariantID string
}

// holdPool renders the pool segment of the hold keys.
func holdPool(ref InventoryHoldRef) string {
	if ref.VariantID == "" {
		return ref.ProductID
	}
	return ref.ProductID + ":" + ref.VariantID
}

// holdKeys returns the ZSET (expiry index), the maintained aggregate of live
// held quantities, and the per-holder quantity-key prefix for one pool.
func holdKeys(ref InventoryHoldRef) (holds, aggregate, prefix string) {
	pool := holdPool(ref)
	return "inventory:holds:" + pool, "inventory:held_qty:" + pool, "inventory:hold_qty:" + pool + ":"
}

// holdCleanupBatch bounds how many expired members one hold call reaps
// (PERF-04 D-02: the old script reaped all of them in one call).
const holdCleanupBatch = 128

// holdQtyGraceMillis is how long a per-holder quantity key outlives the ZSET
// score that expires it. The cleanup path decrements the maintained aggregate
// by the quantity key's value, so the key must still be present when cleanup
// runs; without the grace, a key TTL-reaped before cleanup would leave its
// quantity counted in the aggregate forever (a safe over-count, healed only
// by the reject-path rescan). The hold itself still ends at the ZSET
// deadline: admission and HasInventoryHold read the score, never the key TTL.
const holdQtyGraceMillis = 60000

// observeRedis records one repository-level Redis operation as its own series
// (PERF-04 D-05: the hook can only see the "eval" command name, so hold,
// OTP and cache scripts were indistinguishable in metrics).
func observeRedis(ctx context.Context, op string, start time.Time, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
		if ctx.Err() == context.DeadlineExceeded {
			outcome = "timeout"
		}
	}
	observability.Default.Observe("redis", op, outcome, time.Since(start))
}

// HoldInventory atomically reserves inventory in Redis for the payment window.
// The sorted set is also the expiry index, so expired holds are removed before
// every new reservation instead of leaving a stale aggregate counter behind.
func (r *RedisClient) HoldInventory(ctx context.Context, orderID, productID, variantID string, qty, availableStock int, ttl time.Duration) error {
	return r.HoldInventoryUntil(ctx, orderID, productID, variantID, qty, availableStock, time.Now().Add(ttl))
}

// HoldInventoryUntil uses an absolute deadline, so SQL/network delay cannot renew
// an order's authoritative payment window. Existing Redis keys/scores remain compatible.
//
// PERF-04: admission no longer scans every holder. A maintained aggregate
// (inventory:held_qty:<pool>) holds the sum of live holders' quantities and is
// updated inside the same Lua script that mutates the ZSET/quantity keys, so it
// cannot drift within a script. The aggregate can only ever OVER-count: a
// quantity key TTL-reaped before cleanup, or a member removed without its
// decrement, leaves a phantom quantity behind. A rejection is therefore always
// re-checked against an exact full scan (which also heals the aggregate), so a
// phantom can delay a hold but never admit an oversell — and the scan runs only
// on the reject path, never on the hot accept path.
func (r *RedisClient) HoldInventoryUntil(ctx context.Context, orderID, productID, variantID string, qty, availableStock int, deadline time.Time) error {
	if qty <= 0 {
		return nil
	}

	ref := InventoryHoldRef{ProductID: productID, VariantID: variantID}
	holdsKey, aggregateKey, holdKeyPrefix := holdKeys(ref)
	member := orderID
	now := float64(time.Now().UnixMilli()) / 1000
	expiresAt := float64(deadline.UnixMilli()) / 1000
	if expiresAt <= now {
		return ErrPaymentExpired
	}

	const script = `
local holds = KEYS[1]
local agg = KEYS[2]
local member = ARGV[1]
local qty = tonumber(ARGV[2])
local stock = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local expires = tonumber(ARGV[5])
local holdKeyPrefix = ARGV[6]
local graceMillis = tonumber(ARGV[7])

-- Self-heal: a missing aggregate (first v2 hold on a pool, restart loss,
-- manual deletion) is rebuilt once from the live members inside this
-- atomic script, so a mixed-version deploy window cannot under-count.
if redis.call('EXISTS', agg) == 0 then
  local rebuilt = 0
  local members = redis.call('ZRANGE', holds, 0, -1)
  for _, m in ipairs(members) do
    rebuilt = rebuilt + tonumber(redis.call('GET', holdKeyPrefix .. m) or '0')
  end
  redis.call('SET', agg, rebuilt)
end

-- Bounded expiry cleanup: at most holdCleanupBatch stale members per call.
-- Each removed member decrements the aggregate by its exact quantity.
local expired = redis.call('ZRANGEBYSCORE', holds, '-inf', now, 'LIMIT', 0, 128)
for _, expiredMember in ipairs(expired) do
  local oldQty = tonumber(redis.call('GET', holdKeyPrefix .. expiredMember) or '0')
  redis.call('DEL', holdKeyPrefix .. expiredMember)
  redis.call('ZREM', holds, expiredMember)
  if oldQty > 0 then
    redis.call('DECRBY', agg, oldQty)
  end
end
if redis.call('ZCARD', holds) == 0 then
  redis.call('DEL', holds)
  redis.call('DEL', agg)
end

local heldQty = tonumber(redis.call('GET', agg) or '0')
local ownQty = tonumber(redis.call('GET', holdKeyPrefix .. member) or '0')
if heldQty - ownQty + qty > stock then
  -- The aggregate can only over-count, so a rejection is re-checked
  -- against an exact full scan, which also heals the aggregate. Only a
  -- scan-confirmed shortage rejects.
  local exact = 0
  local allMembers = redis.call('ZRANGE', holds, 0, -1)
  for _, holdMember in ipairs(allMembers) do
    exact = exact + tonumber(redis.call('GET', holdKeyPrefix .. holdMember) or '0')
  end
  redis.call('SET', agg, exact)
  if exact - ownQty + qty > stock then
    return 0
  end
end

if expires <= now then return -1 end
redis.call('SET', holdKeyPrefix .. member, qty, 'PXAT', math.floor(expires * 1000) + graceMillis)
redis.call('ZADD', holds, expires, member)
redis.call('INCRBY', agg, qty - ownQty)
return 1
`

	start := time.Now()
	result, err := r.Client.Eval(ctx, script, []string{holdsKey, aggregateKey}, member, qty, availableStock, now, expiresAt, holdKeyPrefix, holdQtyGraceMillis).Int()
	observeRedis(ctx, "hold", start, err)
	if err != nil {
		return err
	}
	if result != 1 {
		return ErrInsufficientInventory
	}
	return nil
}

// ReleaseInventory removes hold key after payment confirmation or timeout.
// The release also decrements the maintained aggregate by the released
// quantity (PERF-04); a missing aggregate is left missing so the next hold
// rebuilds it from a scan instead of under-counting live holders.
func (r *RedisClient) ReleaseInventory(ctx context.Context, orderID, productID, variantID string) error {
	ref := InventoryHoldRef{ProductID: productID, VariantID: variantID}
	holdsKey, aggregateKey, holdKeyPrefix := holdKeys(ref)
	const script = `
local qty = tonumber(redis.call('GET', ARGV[2] .. ARGV[1]) or '0')
redis.call('DEL', ARGV[2] .. ARGV[1])
redis.call('ZREM', KEYS[1], ARGV[1])
if qty > 0 and redis.call('EXISTS', KEYS[2]) == 1 then
  local remaining = redis.call('DECRBY', KEYS[2], qty)
  if remaining < 0 then
    redis.call('SET', KEYS[2], 0)
  end
end
if redis.call('ZCARD', KEYS[1]) == 0 then
  redis.call('DEL', KEYS[1])
  redis.call('DEL', KEYS[2])
end
return qty
`
	start := time.Now()
	err := r.Client.Eval(ctx, script, []string{holdsKey, aggregateKey}, orderID, holdKeyPrefix).Err()
	observeRedis(ctx, "release", start, err)
	return err
}

// HasInventoryHold verifies that an order still owns the requested quantity
// for a product and that the payment window has not expired. An expired
// member also decrements the maintained aggregate (PERF-04).
func (r *RedisClient) HasInventoryHold(ctx context.Context, orderID, productID, variantID string, qty int) (bool, error) {
	ref := InventoryHoldRef{ProductID: productID, VariantID: variantID}
	holdsKey, aggregateKey, holdKeyPrefix := holdKeys(ref)
	const script = `
local expires = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not expires then
  return 0
end
if tonumber(expires) <= tonumber(ARGV[2]) then
  local heldQty = tonumber(redis.call('GET', ARGV[4] .. ARGV[1]) or '0')
  redis.call('ZREM', KEYS[1], ARGV[1])
  redis.call('DEL', ARGV[4] .. ARGV[1])
  if heldQty > 0 and redis.call('EXISTS', KEYS[2]) == 1 then
    local remaining = redis.call('DECRBY', KEYS[2], heldQty)
    if remaining < 0 then
      redis.call('SET', KEYS[2], 0)
    end
  end
  if redis.call('ZCARD', KEYS[1]) == 0 then
    redis.call('DEL', KEYS[1])
    redis.call('DEL', KEYS[2])
  end
  return 0
end
local heldQty = tonumber(redis.call('GET', ARGV[4] .. ARGV[1]) or '0')
if heldQty >= tonumber(ARGV[3]) then
  return 1
end
return 0
`
	start := time.Now()
	result, err := r.Client.Eval(ctx, script, []string{holdsKey, aggregateKey}, orderID, float64(time.Now().UnixMilli())/1000, qty, holdKeyPrefix).Int()
	observeRedis(ctx, "has_hold", start, err)
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// ReleaseOrderInventory removes every temporary hold belonging to an order.
// PERF-04 D-03: best-effort — every pool is attempted even when an earlier
// one fails, and all failures are joined into the returned error (each one
// is also logged without business identifiers). Callers that already treat
// release as best-effort keep working; the hold TTL (<= 15 min) and the
// 5 s unpaid-order-expiry job bound any residual leak.
func (r *RedisClient) ReleaseOrderInventory(ctx context.Context, orderID string, refs ...InventoryHoldRef) error {
	var failures []error
	for i, ref := range refs {
		if err := r.ReleaseInventory(ctx, orderID, ref.ProductID, ref.VariantID); err != nil {
			log.Printf("redis_release_failed index=%d error=%v", i, err)
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ReconcileHeldAggregate recomputes one pool's maintained aggregate from a
// full scan of its live quantity keys and stores the exact sum. It is the
// manual repair path (runbook docs/operations/redis-recovery.md) and the
// oracle for tests; the live hold path heals itself via rebuild-on-absent
// and the reject-path rescan, so this is never on the hot path. Run it when
// no holds are being placed on the pool, or accept that a concurrent hold
// may move the sum again right after the repair.
func (r *RedisClient) ReconcileHeldAggregate(ctx context.Context, ref InventoryHoldRef) (int, error) {
	holdsKey, aggregateKey, holdKeyPrefix := holdKeys(ref)
	members, err := r.Client.ZRange(ctx, holdsKey, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	if len(members) == 0 {
		if err := r.Client.Set(ctx, aggregateKey, 0, 0).Err(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	keys := make([]string, len(members))
	for i, m := range members {
		keys[i] = holdKeyPrefix + m
	}
	values, err := r.Client.MGet(ctx, keys...).Result()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, v := range values {
		switch n := v.(type) {
		case string:
			var q int
			if _, err := fmt.Sscanf(n, "%d", &q); err == nil {
				total += q
			}
		}
	}
	if err := r.Client.Set(ctx, aggregateKey, total, 0).Err(); err != nil {
		return 0, err
	}
	return total, nil
}
