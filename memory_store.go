package ratelimiter

import (
	"context"
	"sync"
	"time"
)

// memoryEntry holds the hit count and expiration time for a single key.
type memoryEntry struct {
	hits      int64
	expiresAt time.Time
}

// expired reports whether this entry has passed its expiration time.
func (e *memoryEntry) expired() bool {
	return time.Now().After(e.expiresAt)
}

// MemoryStore is an in-memory implementation of Store.
// It is safe for concurrent use. A background goroutine periodically
// removes expired entries to prevent unbounded memory growth.
type MemoryStore struct {
	mu          sync.Mutex
	entries     map[string]*memoryEntry
	gcInterval  time.Duration
	stopCleanup chan struct{}
}

// NewMemoryStore creates a new MemoryStore with the given GC interval.
// If gcInterval is 0, it defaults to 1 minute.
func NewMemoryStore(gcInterval time.Duration) *MemoryStore {
	if gcInterval <= 0 {
		gcInterval = time.Minute
	}
	s := &MemoryStore{
		entries:     make(map[string]*memoryEntry),
		gcInterval:  gcInterval,
		stopCleanup: make(chan struct{}),
	}
	go s.runCleanup()
	return s
}

// runCleanup periodically removes expired entries.
func (s *MemoryStore) runCleanup() {
	ticker := time.NewTicker(s.gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.deleteExpired()
		case <-s.stopCleanup:
			return
		}
	}
}

// deleteExpired removes all expired entries from the store.
func (s *MemoryStore) deleteExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, e := range s.entries {
		if e.expired() {
			delete(s.entries, k)
		}
	}
}

// Stop shuts down the background cleanup goroutine.
// Call this when the store is no longer needed to free resources.
func (s *MemoryStore) Stop() {
	close(s.stopCleanup)
}

// Hit increments the counter for the given key.
// If the key does not exist or has expired, it is (re-)created with the given TTL.
// Returns the current hit count after incrementing.
func (s *MemoryStore) Hit(_ context.Context, key string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.expired() {
		s.entries[key] = &memoryEntry{
			hits:      1,
			expiresAt: time.Now().Add(ttl),
		}
		return 1, nil
	}
	entry.hits++
	return entry.hits, nil
}

// Get returns the current hit count for the given key.
// Returns 0 if the key does not exist or has expired.
func (s *MemoryStore) Get(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.expired() {
		return 0, nil
	}
	return entry.hits, nil
}

// Reset deletes the counter for the given key.
func (s *MemoryStore) Reset(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

// AvailableIn returns the time remaining until the key expires.
// Returns 0 if the key does not exist or has already expired.
func (s *MemoryStore) AvailableIn(_ context.Context, key string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.expired() {
		return 0, nil
	}
	remaining := time.Until(entry.expiresAt)
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}
