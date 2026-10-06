package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/dag"
	"tahr/internal/ui"
)

func TestDAGCanvasWidget_Render(t *testing.T) {
	theme := &ui.Theme{
		Foreground: 0xFFFFFF,
		Background: 0x1E1E2E,
		Function:   0x89B4FA,
		LineNumber: 0x6C7086,
	}

	model := dag.NewGraphModel()
	model.AddNode(&dag.NodeCard{
		ID:    "users",
		Title: "users",
		X:     2,
		Y:     2,
		Width: 20,
		Height: 6,
		Rows: []dag.CardRow{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "email", DataType: "varchar"},
		},
		Ports: []dag.Port{{ID: "pk", RowIndex: 0, Side: 'R'}},
	})
	model.AddNode(&dag.NodeCard{
		ID:    "orders",
		Title: "orders",
		X:     35,
		Y:     2,
		Width: 20,
		Height: 6,
		Rows: []dag.CardRow{
			{Name: "id", DataType: "uuid", IsPK: true},
			{Name: "user_id", DataType: "uuid", IsFK: true},
		},
		Ports: []dag.Port{{ID: "fk", RowIndex: 1, Side: 'L'}},
	})
	model.AddEdge("users", "pk", "orders", "fk", dag.MarkerCrowFootMany)

	widget := NewDAGCanvasWidget(model, theme)
	buf := buffer.NewBuffer(80, 25)

	widget.Render(buf, buffer.NewRect(0, 0, 80, 25))

	// Verify users card frame top-left corner
	c := buf.Cell(2, 2)
	if c.Rune != '╭' {
		t.Errorf("expected card top-left corner '╭', got %c", c.Rune)
	}

	// Verify orders card frame top-left corner
	cOrders := buf.Cell(35, 2)
	if cOrders.Rune != '╭' {
		t.Errorf("expected orders card top-left corner '╭', got %c", cOrders.Rune)
	}
}

func TestDAGCanvasWidget_PanAndSelection(t *testing.T) {
	model := dag.NewGraphModel()
	model.AddNode(&dag.NodeCard{ID: "n1", Title: "n1", X: 0, Y: 0, Width: 10, Height: 4})
	model.AddNode(&dag.NodeCard{ID: "n2", Title: "n2", X: 20, Y: 0, Width: 10, Height: 4})

	widget := NewDAGCanvasWidget(model, &ui.Theme{})

	// Pan
	widget.Pan(5, -3)
	if widget.PanX != 5 || widget.PanY != -3 {
		t.Errorf("expected PanX=5, PanY=-3, got %d, %d", widget.PanX, widget.PanY)
	}

	// Selection cycle
	initialSel := widget.SelectedNodeID
	widget.SelectNextNode()
	if widget.SelectedNodeID == initialSel {
		t.Errorf("expected selection to change after SelectNextNode")
	}

	// Move selected node
	origX := model.Nodes[widget.SelectedNodeID].X
	widget.MoveSelectedNode(10, 0)
	if model.Nodes[widget.SelectedNodeID].X != origX+10 {
		t.Errorf("expected card X to shift by 10")
	}
}

func TestDAGCanvasWidget_EmptyState(t *testing.T) {
	theme := &ui.Theme{
		Foreground: 0xFFFFFF,
		Background: 0x1E1E2E,
		LineNumber: 0x6C7086,
	}
	model := dag.NewGraphModel()
	widget := NewDAGCanvasWidget(model, theme)
	buf := buffer.NewBuffer(80, 25)

	widget.Render(buf, buffer.NewRect(0, 0, 80, 25))

	foundEmptyTitle := false
	for y := 0; y < 25; y++ {
		var lineRunes []rune
		for x := 0; x < 80; x++ {
			lineRunes = append(lineRunes, buf.Cell(x, y).Rune)
		}
		lineStr := string(lineRunes)
		if strings.Contains(lineStr, "Схема базы данных не найдена") {
			foundEmptyTitle = true
		}
		if strings.Contains(lineStr, "orders") || strings.Contains(lineStr, "users") {
			t.Fatalf("unexpected hardcoded mock table found in empty canvas: %s", lineStr)
		}
	}

	if !foundEmptyTitle {
		t.Errorf("expected empty state title in canvas rendering")
	}
}
