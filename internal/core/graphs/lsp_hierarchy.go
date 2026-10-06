package graphs

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"tahr/internal/core/dag"
)

// HierarchyDirection defines the flow direction of call hierarchy.
type HierarchyDirection int

const (
	DirectionDownstream HierarchyDirection = 0 // Outgoing calls (execution flow)
	DirectionUpstream   HierarchyDirection = 1 // Incoming calls (blast radius)
)

// HierarchyCacheKey uniquely identifies a symbol in a document version.
type HierarchyCacheKey struct {
	FileURI   string
	Symbol    string
	DocVer    int32
	Direction HierarchyDirection
}

// CallHierarchyNode represents a function or method in the call tree.
type CallHierarchyNode struct {
	Name       string               `json:"name"`
	Detail     string               `json:"detail"`
	FileURI    string               `json:"file_uri"`
	Line       int                  `json:"line"`
	IsStdLib   bool                 `json:"is_stdlib"`
	Children   []*CallHierarchyNode `json:"children,omitempty"`
	RingRadius int                  `json:"ring_radius"` // Distance from center symbol
}

// RingLRUCache implements a thread-safe fixed-capacity LRU cache for hierarchy queries.
type RingLRUCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[HierarchyCacheKey]*CallHierarchyNode
	keys     []HierarchyCacheKey
}

// NewRingLRUCache creates an LRU cache with the specified capacity.
func NewRingLRUCache(capacity int) *RingLRUCache {
	if capacity <= 0 {
		capacity = 64
	}
	return &RingLRUCache{
		capacity: capacity,
		items:    make(map[HierarchyCacheKey]*CallHierarchyNode),
		keys:     make([]HierarchyCacheKey, 0, capacity),
	}
}

// Get returns the cached node or nil if not found.
func (c *RingLRUCache) Get(key HierarchyCacheKey) *CallHierarchyNode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[key]
}

// Put adds an item, evicting the oldest key if capacity is exceeded.
func (c *RingLRUCache) Put(key HierarchyCacheKey, node *CallHierarchyNode) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.items[key]; exists {
		c.items[key] = node
		return
	}

	if len(c.keys) >= c.capacity {
		// Evict oldest
		oldest := c.keys[0]
		c.keys = c.keys[1:]
		delete(c.items, oldest)
	}

	c.keys = append(c.keys, key)
	c.items[key] = node
}

// LSPHierarchyOrchestrator manages debounced LSP hierarchy queries with LRU caching.
type LSPHierarchyOrchestrator struct {
	mu           sync.Mutex
	cache        *RingLRUCache
	debounceWait time.Duration
	debounceTimer *time.Timer
}

// NewLSPHierarchyOrchestrator initializes an orchestrator with 150ms debounce and 128-entry cache.
func NewLSPHierarchyOrchestrator() *LSPHierarchyOrchestrator {
	return &LSPHierarchyOrchestrator{
		cache:        NewRingLRUCache(128),
		debounceWait: 150 * time.Millisecond,
	}
}

// IsStdLibSymbol filters noise from common standard library prefixes.
func IsStdLibSymbol(name, detail string) bool {
	lower := strings.ToLower(name + " " + detail)
	prefixes := []string{
		"fmt.", "runtime.", "os.", "sync.", "io.", "strings.", "time.",
		"std::", "console.", "builtin.", "math.",
	}
	for _, p := range prefixes {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ToGraphModel transforms a CallHierarchy tree into an interactive DAG Canvas model.
func (h *CallHierarchyNode) ToGraphModel() *dag.GraphModel {
	gm := dag.NewGraphModel()
	if h == nil {
		return gm
	}

	visited := make(map[string]bool)
	var addSubtree func(parent *CallHierarchyNode, current *CallHierarchyNode, depth int)

	addSubtree = func(parent *CallHierarchyNode, curr *CallHierarchyNode, depth int) {
		if curr == nil {
			return
		}
		nodeID := fmt.Sprintf("%s:%d", curr.Name, curr.Line)

		if !visited[nodeID] {
			visited[nodeID] = true

			badge := "Function"
			if curr.IsStdLib {
				badge = "StdLib"
			} else if depth == 0 {
				badge = "Target"
			}

			card := &dag.NodeCard{
				ID:    nodeID,
				Title: curr.Name,
				Badge: badge,
				Rows: []dag.CardRow{
					{Name: curr.Detail, DataType: fmt.Sprintf("line %d", curr.Line)},
				},
				Ports: []dag.Port{
					{ID: "in", RowIndex: 0, Side: 'L', Type: dag.PortInput},
					{ID: "out", RowIndex: 0, Side: 'R', Type: dag.PortOutput},
				},
			}
			gm.AddNode(card)
		}

		if parent != nil {
			parentID := fmt.Sprintf("%s:%d", parent.Name, parent.Line)
			gm.AddEdge(parentID, "out", nodeID, "in", dag.MarkerArrow)
		}

		// Don't expand stdlib nodes further to suppress noise
		if !curr.IsStdLib {
			for _, child := range curr.Children {
				addSubtree(curr, child, depth+1)
			}
		}
	}

	addSubtree(nil, h, 0)
	return gm
}

// QueryWithDebounce schedules a query after debounceWait or returns immediately from cache.
func (o *LSPHierarchyOrchestrator) QueryWithDebounce(
	key HierarchyCacheKey,
	fetcher func() (*CallHierarchyNode, error),
	onResult func(*CallHierarchyNode),
) {
	// 1. Instant return if in cache!
	if cached := o.cache.Get(key); cached != nil {
		onResult(cached)
		return
	}

	// 2. Debounce pending rapid cursor movements
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.debounceTimer != nil {
		o.debounceTimer.Stop()
	}

	o.debounceTimer = time.AfterFunc(o.debounceWait, func() {
		node, err := fetcher()
		if err == nil && node != nil {
			o.cache.Put(key, node)
			onResult(node)
		}
	})
}
