package dag

import (
	"fmt"
	"testing"
)

func TestSugiyamaLayout_DAG(t *testing.T) {
	model := NewGraphModel()

	model.AddNode(&NodeCard{
		ID:    "users",
		Title: "users",
		Rows: []CardRow{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "email", DataType: "varchar(255)"},
		},
	})

	model.AddNode(&NodeCard{
		ID:    "orders",
		Title: "orders",
		Rows: []CardRow{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "user_id", DataType: "uuid", IsFK: true},
		},
	})

	model.AddNode(&NodeCard{
		ID:    "order_items",
		Title: "order_items",
		Rows: []CardRow{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "order_id", DataType: "uuid", IsFK: true},
		},
	})

	model.AddEdge("users", "id", "orders", "user_id", MarkerCrowFootMany)
	model.AddEdge("orders", "id", "order_items", "order_id", MarkerCrowFootMany)

	LayoutSugiyama(model, DefaultSugiyamaConfig())

	users := model.Nodes["users"]
	orders := model.Nodes["orders"]
	items := model.Nodes["order_items"]

	if users.Layer >= orders.Layer || orders.Layer >= items.Layer {
		t.Fatalf("expected layer order users < orders < order_items, got %d, %d, %d", users.Layer, orders.Layer, items.Layer)
	}

	if users.X >= orders.X || orders.X >= items.X {
		t.Fatalf("expected X coordinates users < orders < items, got %d, %d, %d", users.X, orders.X, items.X)
	}
}

func TestChannelRouting_OrthogonalSegments(t *testing.T) {
	model := NewGraphModel()
	model.AddNode(&NodeCard{
		ID:    "users",
		Title: "users",
		X:     10,
		Y:     5,
		Width: 20,
		Height: 6,
		Ports: []Port{{ID: "pk", RowIndex: 0, Side: 'R'}},
	})
	model.AddNode(&NodeCard{
		ID:    "orders",
		Title: "orders",
		X:     50,
		Y:     15,
		Width: 20,
		Height: 6,
		Ports: []Port{{ID: "fk", RowIndex: 1, Side: 'L'}},
	})
	edge := model.AddEdge("users", "pk", "orders", "fk", MarkerCrowFootMany)

	router := NewChannelRouter()
	router.RouteAll(model)

	if len(edge.Points) < 2 {
		t.Fatalf("expected edge to have waypoints, got %d", len(edge.Points))
	}

	// Verify all segments are strictly 90-degree orthogonal
	for i := 0; i < len(edge.Points)-1; i++ {
		p1 := edge.Points[i]
		p2 := edge.Points[i+1]
		if p1[0] != p2[0] && p1[1] != p2[1] {
			t.Fatalf("segment %d -> %d is diagonal! (%v -> %v)", i, i+1, p1, p2)
		}
	}
}

func TestIncrementalRouting_ConnectedEdges(t *testing.T) {
	model := NewGraphModel()
	model.AddNode(&NodeCard{ID: "n1", Title: "n1", X: 10, Y: 10, Width: 15, Height: 5})
	model.AddNode(&NodeCard{ID: "n2", Title: "n2", X: 40, Y: 10, Width: 15, Height: 5})
	model.AddNode(&NodeCard{ID: "n3", Title: "n3", X: 70, Y: 10, Width: 15, Height: 5})

	e12 := model.AddEdge("n1", "", "n2", "", MarkerArrow)
	e23 := model.AddEdge("n2", "", "n3", "", MarkerArrow)

	router := NewChannelRouter()
	router.RouteAll(model)

	origE12Pts := append([][2]int(nil), e12.Points...)

	// Move node 3 (e12 should remain untouched)
	model.Nodes["n3"].X = 80
	router.RouteIncremental(model, "n3")

	if len(e12.Points) != len(origE12Pts) {
		t.Errorf("e12 points corrupted after moving n3")
	}
	if len(e23.Points) == 0 {
		t.Errorf("e23 points should be re-routed")
	}
}

func TestChannelRouting_ObstacleAvoidance(t *testing.T) {
	model := NewGraphModel()
	// Node 1 (Source)
	model.AddNode(&NodeCard{ID: "src", Title: "src", X: 10, Y: 10, Width: 15, Height: 5})
	// Node 2 (Obstacle directly in the middle path)
	model.AddNode(&NodeCard{ID: "obs", Title: "obs", X: 35, Y: 8, Width: 15, Height: 9})
	// Node 3 (Destination)
	model.AddNode(&NodeCard{ID: "dst", Title: "dst", X: 60, Y: 10, Width: 15, Height: 5})

	edge := model.AddEdge("src", "", "dst", "", MarkerArrow)

	router := NewChannelRouter()
	router.RouteAll(model)

	// Since "obs" is in the direct path, the edge should have been diverted (more than 4 waypoints)
	if len(edge.Points) <= 4 {
		t.Fatalf("expected edge to divert around obstacle, got %d waypoints", len(edge.Points))
	}

	// Verify all waypoints remain strictly orthogonal
	for i := 0; i < len(edge.Points)-1; i++ {
		p1 := edge.Points[i]
		p2 := edge.Points[i+1]
		if p1[0] != p2[0] && p1[1] != p2[1] {
			t.Fatalf("diversion segment %d -> %d is diagonal! (%v -> %v)", i, i+1, p1, p2)
		}
	}
}

func BenchmarkIncrementalRouting(b *testing.B) {
	model := NewGraphModel()
	for i := 0; i < 30; i++ {
		model.AddNode(&NodeCard{
			ID:     fmt.Sprintf("node_%d", i),
			Title:  fmt.Sprintf("table_%d", i),
			X:      (i % 5) * 30,
			Y:      (i / 5) * 12,
			Width:  20,
			Height: 8,
		})
	}
	for i := 0; i < 29; i++ {
		model.AddEdge(fmt.Sprintf("node_%d", i), "", fmt.Sprintf("node_%d", i+1), "", MarkerArrow)
		if i+5 < 30 {
			model.AddEdge(fmt.Sprintf("node_%d", i), "", fmt.Sprintf("node_%d", i+5), "", MarkerArrow)
		}
	}

	router := NewChannelRouter()
	router.RouteAll(model)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Simulate dragging node_10
		model.Nodes["node_10"].X += (i % 2)
		router.RouteIncremental(model, "node_10")
	}
}
