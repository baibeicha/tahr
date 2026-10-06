package gitlens

import "sync"

// lruNode represents an element in the LRU doubly linked list.
type lruNode[V any] struct {
	key  string
	val  V
	prev *lruNode[V]
	next *lruNode[V]
}

// LRUCache is a high-performance, thread-safe least-recently-used cache.
type LRUCache[V any] struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*lruNode[V]
	head     *lruNode[V] // Most recently used
	tail     *lruNode[V] // Least recently used
}

// NewLRUCache creates a new LRU cache with the specified capacity.
func NewLRUCache[V any](capacity int) *LRUCache[V] {
	if capacity <= 0 {
		capacity = 512
	}
	return &LRUCache[V]{
		capacity: capacity,
		items:    make(map[string]*lruNode[V]),
	}
}

// Get retrieves an item by key from the cache, updating its recency.
func (c *LRUCache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.moveToHead(node)
	return node.val, true
}

// Put adds or updates a key-value pair in the cache, evicting the least recently used if over capacity.
func (c *LRUCache[V]) Put(key string, val V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if node, ok := c.items[key]; ok {
		node.val = val
		c.moveToHead(node)
		return
	}

	node := &lruNode[V]{
		key: key,
		val: val,
	}
	c.items[key] = node
	c.attachHead(node)

	if len(c.items) > c.capacity {
		c.evictTail()
	}
}

// Len returns the current number of cached items.
func (c *LRUCache[V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear flushes all entries from the cache.
func (c *LRUCache[V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*lruNode[V])
	c.head = nil
	c.tail = nil
}

// Remove deletes an item by key.
func (c *LRUCache[V]) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node, ok := c.items[key]; ok {
		c.detach(node)
		delete(c.items, key)
	}
}

// RemovePrefix evicts all keys matching the given prefix (e.g. invalidating a file's lines).
func (c *LRUCache[V]) RemovePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, node := range c.items {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			c.detach(node)
			delete(c.items, k)
		}
	}
}

func (c *LRUCache[V]) attachHead(node *lruNode[V]) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *LRUCache[V]) detach(node *lruNode[V]) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}

func (c *LRUCache[V]) moveToHead(node *lruNode[V]) {
	if c.head == node {
		return
	}
	c.detach(node)
	c.attachHead(node)
}

func (c *LRUCache[V]) evictTail() {
	if c.tail == nil {
		return
	}
	oldTail := c.tail
	c.detach(oldTail)
	delete(c.items, oldTail.key)
}
