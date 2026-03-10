package ratelimiter

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// hitScript atomically increments a key and sets its TTL only when the key
// is created for the first time. This prevents resetting the TTL on each hit,
// which would effectively make the window never expire.
//
// KEYS[1] = key
// ARGV[1] = ttl in milliseconds
var hitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return current
`)

// RedisStore is a Redis-backed implementation of Store.
// It uses atomic Lua scripts to ensure correctness under concurrent access.
type RedisStore struct {
	client redis.Cmdable
}

// NewRedisStore creates a new RedisStore using the provided Redis client.
// The client can be a *redis.Client, *redis.ClusterClient, or any type that
// implements redis.Cmdable.
func NewRedisStore(client redis.Cmdable) *RedisStore {
	return &RedisStore{client: client}
}

// Hit increments the counter for the given key.
// On first hit, the key's TTL is set to the given duration.
// Returns the current hit count after incrementing.
func (s *RedisStore) Hit(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	ttlMs := ttl.Milliseconds()
	result, err := hitScript.Run(ctx, s.client, []string{key}, ttlMs).Int64()
	if err != nil {
		return 0, err
	}
	return result, nil
}

// Get returns the current hit count for the given key.
// Returns 0 if the key does not exist.
func (s *RedisStore) Get(ctx context.Context, key string) (int64, error) {
	val, err := s.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// Reset deletes the counter for the given key.
func (s *RedisStore) Reset(ctx context.Context, key string) error {
	err := s.client.Del(ctx, key).Err()
	if err == redis.Nil {
		return nil
	}
	return err
}

// AvailableIn returns the time remaining until the key expires.
// Returns 0 if the key does not exist.
func (s *RedisStore) AvailableIn(ctx context.Context, key string) (time.Duration, error) {
	d, err := s.client.PTTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// PTTL returns -2 when key does not exist, -1 when no expiry is set.
	if d < 0 {
		return 0, nil
	}
	return d, nil
}
