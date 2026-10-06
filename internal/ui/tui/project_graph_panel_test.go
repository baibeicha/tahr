package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/dag"
	"tahr/internal/ui"
)

func TestProjectGraphPanel_CycleModeAndRender(t *testing.T) {
	theme := &ui.Theme{
		Foreground:  0xFFFFFF,
		Background:  0x1E1E2E,
		Function:    0x89B4FA,
		SelectionBg: 0x313244,
	}

	panel := NewProjectGraphPanel(theme)
	if panel.Mode != ModeCallHierarchyDownstream {
		t.Fatalf("expected initial mode Downstream, got %v", panel.Mode)
	}

	// Cycle modes
	m1 := panel.CycleMode()
	if m1 != ModeCallHierarchyUpstream || panel.ModeTitle() != "Blast Radius (Upstream)" {
		t.Errorf("expected Upstream mode, got %v (%s)", m1, panel.ModeTitle())
	}
	m2 := panel.CycleMode()
	if m2 != ModeModuleImports {
		t.Errorf("expected ModuleImports, got %v", m2)
	}
	m3 := panel.CycleMode()
	if m3 != ModeRouteToCode {
		t.Errorf("expected RouteToCode, got %v", m3)
	}
	m0 := panel.CycleMode()
	if m0 != ModeCallHierarchyDownstream {
		t.Errorf("expected wrap around to Downstream, got %v", m0)
	}

	// Set model and render
	model := dag.NewGraphModel()
	model.AddNode(&dag.NodeCard{
		ID:    "main",
		Title: "main",
		Rows:  []dag.CardRow{{Name: "main.go:10", DataType: "entry"}},
	})
	panel.SetModel(model)

	buf := buffer.NewBuffer(80, 25)
	panel.Render(buf, buffer.NewRect(0, 0, 80, 25))

	// Verify header rendered
	cell := buf.Cell(1, 0)
	if cell.Rune != '[' {
		t.Errorf("expected header rune '[', got %c", cell.Rune)
	}
}
