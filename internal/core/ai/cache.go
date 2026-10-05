package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// CompletionCache is a thread-safe LRU cache for prompt completions.
type CompletionCache struct {
	mu       sync.RWMutex
	capacity int
	entries  map[string]string
	order    []string
}

// NewCompletionCache creates an LRU cache with the specified maximum capacity.
func NewCompletionCache(capacity int) *CompletionCache {
	if capacity <= 0 {
		capacity = 256
	}
	return &CompletionCache{
		capacity: capacity,
		entries:  make(map[string]string, capacity),
		order:    make([]string, 0, capacity),
	}
}

func hashPrompt(prefix, suffix string) string {
	h := sha256.New()
	h.Write([]byte(prefix))
	h.Write([]byte{0})
	h.Write([]byte(suffix))
	return hex.EncodeToString(h.Sum(nil))
}

// Get retrieves a cached completion for the given prefix and suffix.
func (c *CompletionCache) Get(prefix, suffix string) (string, bool) {
	if c == nil {
		return "", false
	}
	key := hashPrompt(prefix, suffix)
	c.mu.RLock()
	val, ok := c.entries[key]
	c.mu.RUnlock()
	return val, ok
}

// Put stores a completion into the cache, evicting the oldest if capacity is exceeded.
func (c *CompletionCache) Put(prefix, suffix, completion string) {
	if c == nil || completion == "" {
		return
	}
	key := hashPrompt(prefix, suffix)
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		c.entries[key] = completion
		return
	}

	if len(c.order) >= c.capacity {
		evictKey := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, evictKey)
	}

	c.entries[key] = completion
	c.order = append(c.order, key)
}

// Clear clears all cached entries.
func (c *CompletionCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]string, c.capacity)
	c.order = c.order[:0]
}

// Len returns the current count of cached items.
func (c *CompletionCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
