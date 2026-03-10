package ratelimiter

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// KeyFunc is a function that accepts an HTTP request and returns a slice of
// Limit rules. It is used to define named rate limiters.
//
// Returning []Limit{ratelimiter.None()} bypasses rate limiting entirely.
type KeyFunc func(r *http.Request) []Limit

// Limiter is the central rate limiter. It manages named rate limit rules and
// delegates storage to a pluggable Store backend.
type Limiter struct {
	store      Store
	limiters   map[string]KeyFunc
	keyPrefix  string
	gcInterval time.Duration
}

// New creates a new Limiter with the provided options.
// By default it uses an in-memory store with a 1-minute GC interval.
func New(opts ...Option) *Limiter {
	l := &Limiter{
		limiters:   make(map[string]KeyFunc),
		gcInterval: time.Minute,
	}

	for _, opt := range opts {
		opt(l)
	}

	// Fall back to in-memory store if none was provided via options.
	if l.store == nil {
		l.store = NewMemoryStore(l.gcInterval)
	}

	return l
}

// For registers a named rate limiter with the given KeyFunc.
// The KeyFunc receives each HTTP request and returns the applicable Limit rules.
//
//	limiter.For("api", func(r *http.Request) []ratelimiter.Limit {
//	    return []ratelimiter.Limit{
//	        ratelimiter.PerMinute(60).By(r.RemoteAddr),
//	    }
//	})
func (l *Limiter) For(name string, fn KeyFunc) {
	l.limiters[name] = fn
}

// GetLimiter returns the KeyFunc registered under name, or nil if not found.
func (l *Limiter) GetLimiter(name string) KeyFunc {
	return l.limiters[name]
}

// buildKey constructs the store key from the limiter name, the Limit's Key segment,
// and the DecayPeriod. Including the period ensures that multiple limits registered
// under the same name with the same By-key (e.g. PerMinute and PerHour) each maintain
// an independent counter.
func (l *Limiter) buildKey(name string, limit Limit) string {
	key := name
	if limit.Key != "" {
		key = name + ":" + limit.Key
	}
	if !limit.isUnlimited() && limit.DecayPeriod > 0 {
		key = fmt.Sprintf("%s:%d", key, int64(limit.DecayPeriod.Seconds()))
	}
	if l.keyPrefix != "" {
		key = l.keyPrefix + ":" + key
	}
	return key
}

// Hit increments the counter for the given raw key and returns the new count.
// This is a low-level method; prefer Attempt for typical middleware use.
func (l *Limiter) Hit(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return l.store.Hit(ctx, key, ttl)
}

// Attempts returns the current hit count for the given raw key.
func (l *Limiter) Attempts(ctx context.Context, key string) (int64, error) {
	return l.store.Get(ctx, key)
}

// TooManyAttempts reports whether the given raw key has exceeded maxAttempts.
func (l *Limiter) TooManyAttempts(ctx context.Context, key string, maxAttempts int) (bool, error) {
	hits, err := l.store.Get(ctx, key)
	if err != nil {
		return false, err
	}
	return hits >= int64(maxAttempts), nil
}

// Remaining returns the number of attempts left for the given raw key.
// Returns 0 when the limit has already been exceeded.
func (l *Limiter) Remaining(ctx context.Context, key string, maxAttempts int) (int, error) {
	hits, err := l.store.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	remaining := maxAttempts - int(hits)
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

// AvailableIn returns the time until the rate limit window resets for the key.
func (l *Limiter) AvailableIn(ctx context.Context, key string) (time.Duration, error) {
	return l.store.AvailableIn(ctx, key)
}

// Clear removes the counter for the given raw key.
func (l *Limiter) Clear(ctx context.Context, key string) error {
	return l.store.Reset(ctx, key)
}

// AttemptResult contains the outcome of an Attempt call.
type AttemptResult struct {
	// Allowed indicates whether the request is within the rate limit.
	Allowed bool

	// Limit is the Limit rule that was evaluated.
	Limit Limit

	// Key is the store key that was used.
	Key string

	// Hits is the current hit count after this attempt.
	Hits int64

	// Remaining is the number of attempts left in the current window.
	Remaining int

	// RetryAfter is how long to wait before the next allowed attempt.
	// Only meaningful when Allowed is false.
	RetryAfter time.Duration
}

// Attempt checks whether a request is allowed under the given Limit and,
// if so, increments the counter. Returns an AttemptResult describing the outcome.
func (l *Limiter) Attempt(ctx context.Context, name string, limit Limit) (AttemptResult, error) {
	if limit.isUnlimited() {
		return AttemptResult{Allowed: true, Limit: limit}, nil
	}

	key := l.buildKey(name, limit)

	hits, err := l.store.Hit(ctx, key, limit.DecayPeriod)
	if err != nil {
		return AttemptResult{}, fmt.Errorf("ratelimiter: store hit error: %w", err)
	}

	remaining := limit.MaxAttempts - int(hits)
	if remaining < 0 {
		remaining = 0
	}

	if hits > int64(limit.MaxAttempts) {
		retryAfter, err := l.store.AvailableIn(ctx, key)
		if err != nil {
			retryAfter = limit.DecayPeriod
		}
		return AttemptResult{
			Allowed:    false,
			Limit:      limit,
			Key:        key,
			Hits:       hits,
			Remaining:  0,
			RetryAfter: retryAfter,
		}, nil
	}

	return AttemptResult{
		Allowed:   true,
		Limit:     limit,
		Key:       key,
		Hits:      hits,
		Remaining: remaining,
	}, nil
}

// CheckRequest evaluates all Limit rules returned by the named limiter for the
// given request. It returns the first AttemptResult that is not allowed, or
// the last AttemptResult if all limits pass.
//
// Returns an error if the named limiter is not registered.
func (l *Limiter) CheckRequest(ctx context.Context, name string, r *http.Request) (AttemptResult, error) {
	fn, ok := l.limiters[name]
	if !ok {
		return AttemptResult{}, fmt.Errorf("ratelimiter: limiter %q not registered", name)
	}

	limits := fn(r)
	var last AttemptResult
	for _, lim := range limits {
		result, err := l.Attempt(ctx, name, lim)
		if err != nil {
			return AttemptResult{}, err
		}
		last = result
		if !result.Allowed {
			return result, nil
		}
	}
	return last, nil
}
