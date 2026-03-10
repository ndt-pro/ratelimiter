package ratelimiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_Hit(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	defer s.Stop()
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

func TestMemoryStore_Get(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	defer s.Stop()
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

func TestMemoryStore_Reset(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	defer s.Stop()
	ctx := context.Background()

	_, _ = s.Hit(ctx, "key1", time.Minute)
	_, _ = s.Hit(ctx, "key1", time.Minute)

	err := s.Reset(ctx, "key1")
	require.NoError(t, err)

	hits, _ := s.Get(ctx, "key1")
	assert.Equal(t, int64(0), hits)
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	s := NewMemoryStore(time.Millisecond * 50)
	defer s.Stop()
	ctx := context.Background()

	// Use a very short TTL.
	_, err := s.Hit(ctx, "key1", time.Millisecond*100)
	require.NoError(t, err)

	hits, _ := s.Get(ctx, "key1")
	assert.Equal(t, int64(1), hits)

	// Wait for the entry to expire.
	time.Sleep(time.Millisecond * 200)

	// After expiry, the key should behave as if it never existed.
	hits, err = s.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, int64(0), hits)

	// A new Hit after expiry should restart the counter.
	hits, err = s.Hit(ctx, "key1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), hits)
}

func TestMemoryStore_AvailableIn(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	defer s.Stop()
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

func TestMemoryStore_ConcurrentHits(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	defer s.Stop()
	ctx := context.Background()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.Hit(ctx, "concurrent", time.Minute)
		}()
	}
	wg.Wait()

	hits, err := s.Get(ctx, "concurrent")
	require.NoError(t, err)
	assert.Equal(t, int64(goroutines), hits)
}

func TestMemoryStore_GCCleansExpiredEntries(t *testing.T) {
	gcInterval := time.Millisecond * 50
	s := NewMemoryStore(gcInterval)
	defer s.Stop()
	ctx := context.Background()

	// Create an entry with a short TTL.
	_, _ = s.Hit(ctx, "short-lived", time.Millisecond*100)

	// Verify it's there.
	s.mu.Lock()
	_, exists := s.entries["short-lived"]
	s.mu.Unlock()
	assert.True(t, exists)

	// Wait for TTL to expire and GC to run.
	time.Sleep(time.Millisecond * 300)

	// After GC the entry should be gone from the map.
	s.mu.Lock()
	_, exists = s.entries["short-lived"]
	s.mu.Unlock()
	assert.False(t, exists)
}
