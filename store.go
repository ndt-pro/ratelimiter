package ratelimiter

import (
	"context"
	"time"
)

// Store defines the interface for rate limiter storage backends.
// Implementations must be safe for concurrent use.
type Store interface {
	// Hit increments the counter for the given key. If the key does not exist,
	// it is created with TTL as the expiration duration.
	// Returns the current hit count after incrementing.
	Hit(ctx context.Context, key string, ttl time.Duration) (int64, error)

	// Get returns the current hit count for the given key.
	// Returns 0 if the key does not exist.
	Get(ctx context.Context, key string) (int64, error)

	// Reset deletes the counter for the given key.
	Reset(ctx context.Context, key string) error

	// AvailableIn returns the duration until the key expires (and the counter resets).
	// Returns 0 if the key does not exist.
	AvailableIn(ctx context.Context, key string) (time.Duration, error)
}
