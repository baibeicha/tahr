package graphs

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tahr/internal/core/dag"
	"tahr/internal/core/lsp"
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

			cardFilePath := ""
			if curr.FileURI != "" {
				cleanURI := strings.TrimPrefix(curr.FileURI, "file:///")
				cardFilePath = filepath.FromSlash(cleanURI)
			}
			card := &dag.NodeCard{
				ID:       nodeID,
				Title:    curr.Name,
				Badge:    badge,
				FilePath: cardFilePath,
				Line:     curr.Line + 1,
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

// FetchCallHierarchyFromLSP queries the active LSP server (gopls, rust-analyzer, pyright, clangd, tsserver, etc.)
// for call hierarchy at the given position and builds a CallHierarchyNode tree.
func FetchCallHierarchyFromLSP(
	client *lsp.Client,
	uri string,
	line, col int,
	direction HierarchyDirection,
	maxDepth int,
) (*CallHierarchyNode, error) {
	if client == nil {
		return nil, fmt.Errorf("lsp client is nil")
	}

	// 1. First attempt: Standard textDocument/prepareCallHierarchy
	items, err := client.PrepareCallHierarchy(uri, line, col)
	if err == nil && len(items) > 0 {
		target := items[0]
		root := &CallHierarchyNode{
			Name:       target.Name,
			Detail:     target.Detail,
			FileURI:    target.URI,
			Line:       target.Range.Start.Line,
			IsStdLib:   IsStdLibSymbol(target.Name, target.Detail),
			RingRadius: 0,
		}

		visited := make(map[string]bool)
		visited[fmt.Sprintf("%s:%d", target.Name, target.Range.Start.Line)] = true

		var expand func(currItem lsp.CallHierarchyItem, currNode *CallHierarchyNode, depth int)
		expand = func(currItem lsp.CallHierarchyItem, currNode *CallHierarchyNode, depth int) {
			if depth >= maxDepth || currNode.IsStdLib {
				return
			}
			if direction == DirectionDownstream {
				outgoing, err := client.OutgoingCalls(currItem)
				if err == nil {
					for _, call := range outgoing {
						k := fmt.Sprintf("%s:%d", call.To.Name, call.To.Range.Start.Line)
						child := &CallHierarchyNode{
							Name:       call.To.Name,
							Detail:     call.To.Detail,
							FileURI:    call.To.URI,
							Line:       call.To.Range.Start.Line,
							IsStdLib:   IsStdLibSymbol(call.To.Name, call.To.Detail),
							RingRadius: depth + 1,
						}
						currNode.Children = append(currNode.Children, child)
						if !visited[k] {
							visited[k] = true
							expand(call.To, child, depth+1)
						}
					}
				}
			} else {
				incoming, err := client.IncomingCalls(currItem)
				if err == nil {
					for _, call := range incoming {
						k := fmt.Sprintf("%s:%d", call.From.Name, call.From.Range.Start.Line)
						child := &CallHierarchyNode{
							Name:       call.From.Name,
							Detail:     call.From.Detail,
							FileURI:    call.From.URI,
							Line:       call.From.Range.Start.Line,
							IsStdLib:   IsStdLibSymbol(call.From.Name, call.From.Detail),
							RingRadius: depth + 1,
						}
						currNode.Children = append(currNode.Children, child)
						if !visited[k] {
							visited[k] = true
							expand(call.From, child, depth+1)
						}
					}
				}
			}
		}

		expand(target, root, 0)
		return root, nil
	}

	// 2. Fallback for language servers without prepareCallHierarchy (e.g. older servers):
	// Query DocumentSymbols + References to construct cross-symbol relationships!
	symbols, sErr := client.DocumentSymbols(uri)
	if sErr != nil || len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols or call hierarchy available from lsp")
	}

	// Find the symbol nearest or enclosing (line, col)
	var targetSym *lsp.DocumentSymbol
	for i := range symbols {
		s := &symbols[i]
		if s.Kind == lsp.SymbolKindFunction || s.Kind == lsp.SymbolKindMethod || s.Kind == lsp.SymbolKindConstructor {
			if line >= s.Range.Start.Line && line <= s.Range.End.Line {
				targetSym = s
				break
			}
		}
	}
	if targetSym == nil {
		for i := range symbols {
			s := &symbols[i]
			if s.Kind == lsp.SymbolKindFunction || s.Kind == lsp.SymbolKindMethod {
				targetSym = s
				break
			}
		}
	}
	if targetSym == nil {
		return nil, fmt.Errorf("no function symbol found at target line")
	}

	root := &CallHierarchyNode{
		Name:       targetSym.Name,
		Detail:     targetSym.Detail,
		FileURI:    uri,
		Line:       targetSym.Range.Start.Line,
		IsStdLib:   IsStdLibSymbol(targetSym.Name, targetSym.Detail),
		RingRadius: 0,
	}

	// Query workspace references for callers
	refs, rErr := client.References(uri, targetSym.Range.Start.Line, targetSym.Range.Start.Character, false)
	if rErr == nil && len(refs) > 0 {
		seenRef := make(map[string]bool)
		for _, loc := range refs {
			refID := fmt.Sprintf("%s:%d", loc.URI, loc.Range.Start.Line)
			if seenRef[refID] {
				continue
			}
			seenRef[refID] = true
			callerName := filepath.Base(loc.URI)
			root.Children = append(root.Children, &CallHierarchyNode{
				Name:       fmt.Sprintf("%s:%d", callerName, loc.Range.Start.Line+1),
				Detail:     "reference",
				FileURI:    loc.URI,
				Line:       loc.Range.Start.Line,
				RingRadius: 1,
			})
		}
	}

	return root, nil
}
