package ratelimiter

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// Option is a functional option for configuring a Limiter.
type Option func(*Limiter)

// WithStore sets the storage backend for the Limiter.
// Use this to provide a custom Store implementation.
func WithStore(store Store) Option {
	return func(l *Limiter) {
		l.store = store
	}
}

// WithRedis configures the Limiter to use a Redis-backed store.
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	limiter := ratelimiter.New(ratelimiter.WithRedis(rdb))
func WithRedis(client redis.Cmdable) Option {
	return func(l *Limiter) {
		l.store = NewRedisStore(client)
	}
}

// WithMemoryGCInterval configures the garbage collection interval for the
// in-memory store. This option is only effective when the default in-memory
// store is used (i.e. no WithStore or WithRedis option is provided).
func WithMemoryGCInterval(d time.Duration) Option {
	return func(l *Limiter) {
		l.gcInterval = d
	}
}

// WithKeyPrefix sets a prefix that is prepended to all store keys.
// This is useful when sharing a Redis instance across multiple applications.
func WithKeyPrefix(prefix string) Option {
	return func(l *Limiter) {
		l.keyPrefix = prefix
	}
}
