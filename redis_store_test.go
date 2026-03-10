package ratelimiter

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewRedisStore(client), mr
}

func TestRedisStore_Hit(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()

	hits, err := s.Hit(ctx, "key1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), hits)

	hits, err = s.Hit(ctx, "key1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(2), hits)

	hits, err = s.Hit(ctx, "key1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(3), hits)
}

func TestRedisStore_Hit_TTLSetOnFirstHitOnly(t *testing.T) {
	s, mr := newTestRedisStore(t)
	ctx := context.Background()

	_, _ = s.Hit(ctx, "key1", time.Minute)
	ttlAfterFirst := mr.TTL("key1")
	assert.Greater(t, ttlAfterFirst, time.Duration(0))

	_, _ = s.Hit(ctx, "key1", time.Minute)
	ttlAfterSecond := mr.TTL("key1")

	// TTL should not be reset on the second hit; it should be <= first TTL.
	assert.LessOrEqual(t, ttlAfterSecond, ttlAfterFirst)
}

func TestRedisStore_Get(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()

	// Non-existent key returns 0.
	hits, err := s.Get(ctx, "missing")
	require.NoError(t, err)
	assert.Equal(t, int64(0), hits)

	_, _ = s.Hit(ctx, "key1", time.Minute)
	_, _ = s.Hit(ctx, "key1", time.Minute)

	hits, err = s.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), hits)
}

func TestRedisStore_Reset(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()

	_, _ = s.Hit(ctx, "key1", time.Minute)
	_, _ = s.Hit(ctx, "key1", time.Minute)

	err := s.Reset(ctx, "key1")
	require.NoError(t, err)

	hits, _ := s.Get(ctx, "key1")
	assert.Equal(t, int64(0), hits)
}

func TestRedisStore_AvailableIn(t *testing.T) {
	s, _ := newTestRedisStore(t)
	ctx := context.Background()

	// Non-existent key returns 0.
	d, err := s.AvailableIn(ctx, "missing")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)

	_, _ = s.Hit(ctx, "key1", time.Minute)

	d, err = s.AvailableIn(ctx, "key1")
	require.NoError(t, err)
	assert.Greater(t, d, time.Duration(0))
	assert.LessOrEqual(t, d, time.Minute)
}

func TestRedisStore_TTLExpiry(t *testing.T) {
	s, mr := newTestRedisStore(t)
	ctx := context.Background()

	_, _ = s.Hit(ctx, "key1", time.Second*5)

	hits, _ := s.Get(ctx, "key1")
	assert.Equal(t, int64(1), hits)

	// Fast-forward time in miniredis.
	mr.FastForward(time.Second * 6)

	hits, err := s.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), hits)
}
