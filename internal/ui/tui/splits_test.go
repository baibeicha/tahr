package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core"
)

func TestSplitManager_AllLayouts(t *testing.T) {
	sm := NewSplitManager()
	if sm.TotalPanes() != 1 {
		t.Fatalf("expected 1 pane, got %d", sm.TotalPanes())
	}

	area := buffer.NewRect(5, 2, 100, 40)
	docs := []*core.Document{
		{ID: "doc1", FilePath: "main.go"},
		{ID: "doc2", FilePath: "tree.go"},
		{ID: "doc3", FilePath: "app.go"},
		{ID: "doc4", FilePath: "splits.go"},
		{ID: "doc5", FilePath: "settings.go"},
		{ID: "doc6", FilePath: "runner.go"},
	}

	modes := []struct {
		mode       SplitLayoutMode
		totalPanes int
		title      string
	}{
		{SplitSingle, 1, "Single Pane"},
		{Split2Cols, 2, "2 Columns"},
		{Split2Rows, 2, "2 Rows"},
		{Split3Cols, 3, "3 Columns"},
		{Split4Grid, 4, "4 Grid (2x2)"},
		{Split5Panes, 5, "5 Panes"},
		{Split6Grid, 6, "6 Grid (3x2)"},
	}

	for _, m := range modes {
		sm.SetLayout(m.mode)
		if sm.TotalPanes() != m.totalPanes {
			t.Errorf("mode %v: expected %d panes, got %d", m.mode, m.totalPanes, sm.TotalPanes())
		}
		if sm.ModeTitle() != m.title {
			t.Errorf("mode %v: expected title %q, got %q", m.mode, m.title, sm.ModeTitle())
		}

		sm.UpdateLayout(area, docs, "doc1")
		if len(sm.Panes) != m.totalPanes {
			t.Errorf("mode %v: expected %d panes slice, got %d", m.mode, m.totalPanes, len(sm.Panes))
		}

		// Ensure all panes have valid bounds inside area
		for i, p := range sm.Panes {
			if p.Bounds.Width <= 0 || p.Bounds.Height <= 0 {
				t.Errorf("mode %v pane %d invalid bounds: %+v", m.mode, i, p.Bounds)
			}
			if p.Bounds.X < area.X || p.Bounds.Y < area.Y {
				t.Errorf("mode %v pane %d outside area origin: %+v", m.mode, i, p.Bounds)
			}
		}
	}
}

func TestSplitManager_NavigationAndFind(t *testing.T) {
	sm := NewSplitManager()
	sm.SetLayout(Split4Grid)
	area := buffer.NewRect(0, 0, 80, 40)
	sm.UpdateLayout(area, nil, "doc_active")

	if sm.ActiveIndex != 0 {
		t.Fatalf("expected active index 0, got %d", sm.ActiveIndex)
	}

	// Next pane
	sm.NextPane()
	if sm.ActiveIndex != 1 {
		t.Fatalf("expected active index 1, got %d", sm.ActiveIndex)
	}
	sm.NextPane()
	if sm.ActiveIndex != 2 {
		t.Fatalf("expected active index 2, got %d", sm.ActiveIndex)
	}
	sm.NextPane()
	if sm.ActiveIndex != 3 {
		t.Fatalf("expected active index 3, got %d", sm.ActiveIndex)
	}
	sm.NextPane() // Wrap around
	if sm.ActiveIndex != 0 {
		t.Fatalf("expected wrap around to 0, got %d", sm.ActiveIndex)
	}

	// Prev pane
	sm.PrevPane()
	if sm.ActiveIndex != 3 {
		t.Fatalf("expected prev wrap around to 3, got %d", sm.ActiveIndex)
	}

	// Direct set
	sm.SetPane(2)
	if sm.ActiveIndex != 2 {
		t.Fatalf("expected direct set to 2, got %d", sm.ActiveIndex)
	}

	// Cycle layout
	nextMode := sm.CycleLayout()
	if nextMode != Split5Panes {
		t.Fatalf("expected next mode Split5Panes, got %v", nextMode)
	}

	// FindPaneAt
	paneAt := sm.FindPaneAt(10, 5)
	if paneAt < 0 {
		t.Fatalf("expected to find pane at (10, 5), got %d", paneAt)
	}
}
