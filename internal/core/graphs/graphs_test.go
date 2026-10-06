package graphs

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestLSPHierarchy_DebounceAndLRUCache(t *testing.T) {
	orchestrator := NewLSPHierarchyOrchestrator()
	orchestrator.debounceWait = 30 * time.Millisecond // fast test debounce

	key := HierarchyCacheKey{
		FileURI:   "file:///src/main.go",
		Symbol:    "HandleOrders",
		DocVer:    1,
		Direction: DirectionDownstream,
	}

	var fetchCount int32
	fetcher := func() (*CallHierarchyNode, error) {
		atomic.AddInt32(&fetchCount, 1)
		return &CallHierarchyNode{
			Name:    "HandleOrders",
			FileURI: "file:///src/main.go",
			Line:    42,
		}, nil
	}

	done := make(chan bool, 1)
	// Simulate rapid cursor movement (3 queries in quick succession)
	orchestrator.QueryWithDebounce(key, fetcher, func(node *CallHierarchyNode) {})
	orchestrator.QueryWithDebounce(key, fetcher, func(node *CallHierarchyNode) {})
	orchestrator.QueryWithDebounce(key, fetcher, func(node *CallHierarchyNode) {
		done <- true
	})

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("debounce query timed out")
	}

	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("expected exactly 1 fetch due to debounce, got %d", atomic.LoadInt32(&fetchCount))
	}

	// Immediate query from LRU cache
	instantCalled := false
	orchestrator.QueryWithDebounce(key, fetcher, func(node *CallHierarchyNode) {
		instantCalled = true
	})

	if !instantCalled {
		t.Fatalf("expected instant result from LRU cache")
	}
	if atomic.LoadInt32(&fetchCount) != 1 {
		t.Fatalf("fetch was called again! Expected cache hit")
	}
}

func TestTarjanSCC_CycleDetection(t *testing.T) {
	g := NewDependencyGraph()

	// Cyclic loop: auth -> session -> user -> auth
	g.AddDependency("auth", "session")
	g.AddDependency("session", "user")
	g.AddDependency("user", "auth")

	// Acyclic branch: user -> logger, logger -> db
	g.AddDependency("user", "logger")
	g.AddDependency("logger", "db")

	g.AnalyzeTarjanSCC()

	if len(g.Cycles) != 1 {
		t.Fatalf("expected 1 cyclic component, got %d: %+v", len(g.Cycles), g.Cycles)
	}

	cycle := g.Cycles[0]
	if len(cycle) != 3 {
		t.Fatalf("expected cycle with 3 modules, got %d: %+v", len(cycle), cycle)
	}

	// Verify cyclic flags
	for _, dep := range g.Deps {
		if (dep.From == "auth" && dep.To == "session") ||
			(dep.From == "session" && dep.To == "user") ||
			(dep.From == "user" && dep.To == "auth") {
			if !dep.IsCyclic {
				t.Errorf("expected edge %s -> %s to be flagged cyclic", dep.From, dep.To)
			}
		} else {
			if dep.IsCyclic {
				t.Errorf("edge %s -> %s should not be cyclic", dep.From, dep.To)
			}
		}
	}

	// Verify metrics
	if g.Metrics["user"] == nil || g.Metrics["user"].InDegree != 1 {
		t.Errorf("unexpected user module metrics: %+v", g.Metrics["user"])
	}
}

func TestRoutePipelineScanner(t *testing.T) {
	code := `
package main

func SetupRoutes(r *gin.Engine) {
    r.GET("/api/v1/orders", HandleGetOrders)
    r.POST("/api/v1/orders", HandleCreateOrder)
}
`
	scanner := NewRoutePipelineScanner()
	scanner.ScanFileContent("main.go", code)

	if len(scanner.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(scanner.Routes))
	}

	r0 := scanner.Routes[0]
	if r0.Method != "GET" || r0.Path != "/api/v1/orders" || r0.HandlerName != "HandleGetOrders" {
		t.Errorf("unexpected route 0: %+v", r0)
	}

	r1 := scanner.Routes[1]
	if r1.Method != "POST" || r1.Path != "/api/v1/orders" || r1.HandlerName != "HandleCreateOrder" {
		t.Errorf("unexpected route 1: %+v", r1)
	}

	// Test GraphModel projection
	gm := scanner.ToGraphModel()
	if len(gm.Nodes) != 4 { // 2 route nodes + 2 handler nodes
		t.Fatalf("expected 4 graph nodes, got %d", len(gm.Nodes))
	}
	if len(gm.Edges) != 2 {
		t.Fatalf("expected 2 graph edges, got %d", len(gm.Edges))
	}
}
