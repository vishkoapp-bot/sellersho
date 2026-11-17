package dependencybudget

import (
	"github.com/redis/go-redis/v9"
	"time"
)

// Redis mutations may have committed before a reply is lost. Do not let the
// client replay them invisibly or ignore the caller's route/cleanup deadline.
func ConfigureRedis(opts *redis.Options) {
	opts.ContextTimeoutEnabled = true
	opts.MaxRetries = -1
	opts.DialTimeout = time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	opts.PoolTimeout = time.Second
	opts.PoolSize = 32
	opts.MaxActiveConns = 32
}
