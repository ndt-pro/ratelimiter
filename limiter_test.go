package ratelimiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLimiter() *Limiter {
	return New(WithStore(NewMemoryStore(time.Minute)))
}

// --- Limit builder ---

func TestLimit_PerMinute(t *testing.T) {
	l := PerMinute(60)
	assert.Equal(t, 60, l.MaxAttempts)
	assert.Equal(t, time.Minute, l.DecayPeriod)
}

func TestLimit_PerHour(t *testing.T) {
	l := PerHour(1000)
	assert.Equal(t, 1000, l.MaxAttempts)
	assert.Equal(t, time.Hour, l.DecayPeriod)
}

func TestLimit_PerDay(t *testing.T) {
	l := PerDay(5000)
	assert.Equal(t, 5000, l.MaxAttempts)
	assert.Equal(t, 24*time.Hour, l.DecayPeriod)
}

func TestLimit_None(t *testing.T) {
	l := None()
	assert.True(t, l.isUnlimited())
}

func TestLimit_By(t *testing.T) {
	l := PerMinute(10).By("192.168.1.1")
	assert.Equal(t, "192.168.1.1", l.Key)
}

func TestLimit_PerMinutes(t *testing.T) {
	l := PerMinutes(5, 100)
	assert.Equal(t, 100, l.MaxAttempts)
	assert.Equal(t, 5*time.Minute, l.DecayPeriod)
}

// --- Limiter.Attempt ---

func TestLimiter_Attempt_Allowed(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	result, err := l.Attempt(ctx, "test", PerMinute(5).By("ip1"))
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, int64(1), result.Hits)
	assert.Equal(t, 4, result.Remaining)
}

func TestLimiter_Attempt_ExceedsLimit(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	limit := PerMinute(3).By("ip2")
	for i := 0; i < 3; i++ {
		result, err := l.Attempt(ctx, "test", limit)
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}

	// 4th attempt should be denied.
	result, err := l.Attempt(ctx, "test", limit)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, 0, result.Remaining)
	assert.Greater(t, result.RetryAfter, time.Duration(0))
}

func TestLimiter_Attempt_Unlimited(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	for i := 0; i < 1000; i++ {
		result, err := l.Attempt(ctx, "test", None())
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}
}

// --- Limiter.TooManyAttempts ---

func TestLimiter_TooManyAttempts(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	key := "test:ip3"
	_, _ = l.Hit(ctx, key, time.Minute)
	_, _ = l.Hit(ctx, key, time.Minute)

	too, err := l.TooManyAttempts(ctx, key, 3)
	require.NoError(t, err)
	assert.False(t, too)

	_, _ = l.Hit(ctx, key, time.Minute)

	too, err = l.TooManyAttempts(ctx, key, 3)
	require.NoError(t, err)
	assert.True(t, too)
}

// --- Limiter.Remaining ---

func TestLimiter_Remaining(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	key := "test:ip4"
	remaining, err := l.Remaining(ctx, key, 5)
	require.NoError(t, err)
	assert.Equal(t, 5, remaining)

	_, _ = l.Hit(ctx, key, time.Minute)
	_, _ = l.Hit(ctx, key, time.Minute)

	remaining, err = l.Remaining(ctx, key, 5)
	require.NoError(t, err)
	assert.Equal(t, 3, remaining)
}

func TestLimiter_Remaining_NeverNegative(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	key := "test:ip5"
	for i := 0; i < 10; i++ {
		_, _ = l.Hit(ctx, key, time.Minute)
	}

	remaining, err := l.Remaining(ctx, key, 3)
	require.NoError(t, err)
	assert.Equal(t, 0, remaining)
}

// --- Limiter.Clear ---

func TestLimiter_Clear(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	key := "test:ip6"
	_, _ = l.Hit(ctx, key, time.Minute)
	_, _ = l.Hit(ctx, key, time.Minute)

	err := l.Clear(ctx, key)
	require.NoError(t, err)

	hits, _ := l.Attempts(ctx, key)
	assert.Equal(t, int64(0), hits)
}

// --- Named limiters ---

func TestLimiter_For_And_CheckRequest(t *testing.T) {
	l := newTestLimiter()

	l.For("api", func(r *http.Request) []Limit {
		return []Limit{PerMinute(3).By(r.RemoteAddr)}
	})

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		return r
	}

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		result, err := l.CheckRequest(ctx, "api", makeReq())
		require.NoError(t, err)
		assert.True(t, result.Allowed, "attempt %d should be allowed", i+1)
	}

	result, err := l.CheckRequest(ctx, "api", makeReq())
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestLimiter_CheckRequest_MultipleLimits(t *testing.T) {
	l := newTestLimiter()

	// Restrict to 2 per minute AND 4 per hour.
	l.For("login", func(r *http.Request) []Limit {
		ip := r.RemoteAddr
		return []Limit{
			PerMinute(2).By(ip),
			PerHour(4).By(ip),
		}
	})

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.RemoteAddr = "10.0.0.2:9999"
		return r
	}

	ctx := context.Background()

	// First 2 allowed.
	for i := 0; i < 2; i++ {
		result, err := l.CheckRequest(ctx, "login", makeReq())
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}

	// 3rd blocked by per-minute limit.
	result, err := l.CheckRequest(ctx, "login", makeReq())
	require.NoError(t, err)
	assert.False(t, result.Allowed)
}

func TestLimiter_CheckRequest_UnknownLimiter(t *testing.T) {
	l := newTestLimiter()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := l.CheckRequest(context.Background(), "unknown", r)
	assert.Error(t, err)
}

func TestLimiter_CheckRequest_None(t *testing.T) {
	l := newTestLimiter()

	l.For("unlimited", func(r *http.Request) []Limit {
		return []Limit{None()}
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	result, err := l.CheckRequest(context.Background(), "unlimited", r)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

// --- Key prefix ---

func TestLimiter_KeyPrefix(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Stop()

	l := New(WithStore(store), WithKeyPrefix("myapp"))
	ctx := context.Background()

	limit := PerMinute(5).By("user1")
	result, err := l.Attempt(ctx, "api", limit)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Equal(t, "myapp:api:user1:60", result.Key)
}

// --- AvailableIn ---

func TestLimiter_AvailableIn(t *testing.T) {
	l := newTestLimiter()
	ctx := context.Background()

	key := "test:avail"
	_, _ = l.Hit(ctx, key, time.Minute)

	d, err := l.AvailableIn(ctx, key)
	require.NoError(t, err)
	assert.Greater(t, d, time.Duration(0))
}
