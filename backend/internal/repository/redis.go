package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL initialization failed; configuration value withheld")
	}

	dependencybudget.ConfigureRedis(opts)
	client := redis.NewClient(opts)
	client.AddHook(observability.RedisHook{Registry: observability.Default})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

// HoldInventory atomically reserves inventory in Redis for the payment window.
// The sorted set is also the expiry index, so expired holds are removed before
// every new reservation instead of leaving a stale aggregate counter behind.
func (r *RedisClient) HoldInventory(ctx context.Context, orderID, productID string, qty, availableStock int, ttl time.Duration) error {
	return r.HoldInventoryUntil(ctx, orderID, productID, qty, availableStock, time.Now().Add(ttl))
}

// HoldInventoryUntil uses an absolute deadline, so SQL/network delay cannot renew
// an order's authoritative payment window. Existing Redis keys/scores remain compatible.
func (r *RedisClient) HoldInventoryUntil(ctx context.Context, orderID, productID string, qty, availableStock int, deadline time.Time) error {
	if qty <= 0 {
		return nil
	}

	holdsKey := fmt.Sprintf("inventory:holds:%s", productID)
	holdKeyPrefix := fmt.Sprintf("inventory:hold_qty:%s:", productID)
	member := orderID
	now := float64(time.Now().UnixMilli()) / 1000
	expiresAt := float64(deadline.UnixMilli()) / 1000
	if expiresAt <= now {
		return ErrPaymentExpired
	}

	const script = `
local holds = KEYS[1]
local member = ARGV[1]
local qty = tonumber(ARGV[2])
local stock = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local expires = tonumber(ARGV[5])
local holdKeyPrefix = ARGV[6]

local expired = redis.call('ZRANGEBYSCORE', holds, '-inf', now)
for _, expiredMember in ipairs(expired) do
  redis.call('DEL', holdKeyPrefix .. expiredMember)
  redis.call('ZREM', holds, expiredMember)
end
if redis.call('ZCARD', holds) == 0 then
  redis.call('DEL', holds)
end

local heldQty = 0
local allMembers = redis.call('ZRANGE', holds, 0, -1)
for _, holdMember in ipairs(allMembers) do
  if holdMember ~= member then
    heldQty = heldQty + tonumber(redis.call('GET', holdKeyPrefix .. holdMember) or '0')
  end
end

if heldQty + qty > stock then
  return 0
end

if expires <= now then return -1 end
redis.call('SET', holdKeyPrefix .. member, qty, 'PXAT', math.floor(expires * 1000))
redis.call('ZADD', holds, expires, member)
return 1
`

	result, err := r.Client.Eval(ctx, script, []string{holdsKey}, member, qty, availableStock, now, expiresAt, holdKeyPrefix).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return ErrInsufficientInventory
	}
	return nil
}

// ReleaseInventory removes hold key after payment confirmation or timeout
func (r *RedisClient) ReleaseInventory(ctx context.Context, orderID, productID string) error {
	holdsKey := fmt.Sprintf("inventory:holds:%s", productID)
	holdKeyPrefix := fmt.Sprintf("inventory:hold_qty:%s:", productID)
	const script = `
local qty = tonumber(redis.call('GET', ARGV[2] .. ARGV[1]) or '0')
redis.call('DEL', ARGV[2] .. ARGV[1])
redis.call('ZREM', KEYS[1], ARGV[1])
if redis.call('ZCARD', KEYS[1]) == 0 then
  redis.call('DEL', KEYS[1])
end
return qty
`
	return r.Client.Eval(ctx, script, []string{holdsKey}, orderID, holdKeyPrefix).Err()
}

// HasInventoryHold verifies that an order still owns the requested quantity
// for a product and that the payment window has not expired.
func (r *RedisClient) HasInventoryHold(ctx context.Context, orderID, productID string, qty int) (bool, error) {
	holdsKey := fmt.Sprintf("inventory:holds:%s", productID)
	holdKeyPrefix := fmt.Sprintf("inventory:hold_qty:%s:", productID)
	const script = `
local expires = redis.call('ZSCORE', KEYS[1], ARGV[1])
if not expires then
  return 0
end
if tonumber(expires) <= tonumber(ARGV[2]) then
  redis.call('ZREM', KEYS[1], ARGV[1])
  redis.call('DEL', ARGV[4] .. ARGV[1])
  if redis.call('ZCARD', KEYS[1]) == 0 then
    redis.call('DEL', KEYS[1])
  end
  return 0
end
local heldQty = tonumber(redis.call('GET', ARGV[4] .. ARGV[1]) or '0')
if heldQty >= tonumber(ARGV[3]) then
  return 1
end
return 0
`
	result, err := r.Client.Eval(ctx, script, []string{holdsKey}, orderID, float64(time.Now().UnixMilli())/1000, qty, holdKeyPrefix).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// ReleaseOrderInventory removes every temporary hold belonging to an order.
func (r *RedisClient) ReleaseOrderInventory(ctx context.Context, orderID string, productIDs ...string) error {
	for _, productID := range productIDs {
		if err := r.ReleaseInventory(ctx, orderID, productID); err != nil {
			return err
		}
	}
	return nil
}
