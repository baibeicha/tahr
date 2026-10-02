package gui

import (
	"testing"

	"tahr/internal/core"
	"tahr/internal/ui"
)

func TestGuiLayout_Splits(t *testing.T) {
	for splits := 1; splits <= 6; splits++ {
		l := ComputeLayout(1920, 1080, true, 250, true, 300, splits)
		if len(l.Panes) != splits {
			t.Fatalf("expected %d panes, got %d", splits, len(l.Panes))
		}
		for i, p := range l.Panes {
			if p.Width <= 0 || p.Height <= 0 {
				t.Errorf("pane %d has non-positive dimensions: %+v", i, p)
			}
		}
	}
}

func TestSmoothCaret_Lerp(t *testing.T) {
	c := NewSmoothCaret()
	c.SetTarget(100.0, 50.0)

	steps := 0
	for c.Update(0.25) {
		steps++
		if steps > 200 {
			t.Fatalf("caret failed to converge within 200 steps")
		}
	}

	if c.CurrentX != 100.0 || c.CurrentY != 50.0 {
		t.Fatalf("expected caret at (100, 50), got (%.2f, %.2f)", c.CurrentX, c.CurrentY)
	}
}

func TestInertialScroll_Damping(t *testing.T) {
	s := NewInertialScroll()
	s.AddDelta(120.0)

	ticks := 0
	for s.Step() {
		ticks++
		if ticks > 200 {
			t.Fatalf("scroll failed to settle within 200 ticks")
		}
	}

	if s.OffsetY <= 0 {
		t.Fatalf("expected positive scroll offset, got %.2f", s.OffsetY)
	}
}

func TestGuiApp_Creation(t *testing.T) {
	eng := core.NewEngine()
	app := NewGuiApp(eng, ui.CatppuccinMocha())

	if app.SplitCount != 1 {
		t.Fatalf("expected 1 split by default, got %d", app.SplitCount)
	}

	app.SetSplitCount(4)
	if app.SplitCount != 4 || len(app.Layout.Panes) != 4 {
		t.Fatalf("expected 4 split panes, got %d", len(app.Layout.Panes))
	}

	app.ToggleSidebar()
	if app.SidebarOpen {
		t.Fatalf("expected sidebar to be closed")
	}

	if err := app.Run(); err != nil {
		t.Fatalf("app.Run failed: %v", err)
	}
}
