package tui

import (
	"fmt"
	"strings"
	"testing"

	cbuf "tahr/internal/core/buffer"
	"tahr/internal/core"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
)

func TestSmoothScroll_ExponentialDampingConvergence(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updated.(*AppModel)

	ap := m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}

	// Set initial target delta
	ap.SmoothScrollY = 0
	ap.TargetViewportY = 100
	ap.ViewportY = 0

	// Step smooth scroll repeatedly until convergence
	ticks := 0
	for {
		ticks++
		needsRedraw := m.stepSmoothScroll()
		if !needsRedraw {
			break
		}
		if ticks > 100 {
			t.Fatalf("stepSmoothScroll failed to converge within 100 ticks: SmoothScrollY=%f, Target=%f",
				ap.SmoothScrollY, ap.TargetViewportY)
		}
	}

	// Verify exact convergence
	if ap.SmoothScrollY != 100 {
		t.Fatalf("expected SmoothScrollY == 100, got %f", ap.SmoothScrollY)
	}
	if ap.ViewportY != 100 {
		t.Fatalf("expected ViewportY == 100, got %d", ap.ViewportY)
	}
	if m.viewportY != 100 {
		t.Fatalf("expected m.viewportY == 100, got %d", m.viewportY)
	}

	// Once converged, further ticks should not request redraw
	if m.stepSmoothScroll() {
		t.Fatal("expected stepSmoothScroll to return false after convergence")
	}
}

func TestSmoothScroll_SnapAndLockAfterUndo(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Populate document with 1000 lines
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf("line %04d: text payload", i))
	}
	updated, _ = m.Update(tea.PasteMsg{Text: strings.Join(lines, "\n")})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc.Buffer.TotalLines() < 1000 {
		t.Fatalf("expected >= 1000 lines, got %d", doc.Buffer.TotalLines())
	}

	// Place cursor at line 15 and type '#'
	off15, _ := doc.Buffer.ByteOffsetForLine(15)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 15, Column: 0, Byte: off15}, Head: cbuf.Position{Line: 15, Column: 0, Byte: off15}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '#'}})
	m = updated.(*AppModel)

	// Simulate user scrolling down to line 850
	m.viewportY = 850
	ap := m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}
	ap.ViewportY = 850
	ap.TargetViewportY = 850
	ap.SmoothScrollY = 850
	ap.ScrollVelocity = 10.0

	// Trigger Undo: cursor restored to line 15
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	ap = m.splits.ActivePane()
	initialViewportY := m.viewportY
	if initialViewportY > 15 {
		t.Fatalf("viewportY did not snap near restored cursor (line 15): got %d", initialViewportY)
	}

	// Offsets must be locked to the snapped viewport
	if ap.TargetViewportY != float64(initialViewportY) {
		t.Fatalf("expected TargetViewportY == %d, got %f", initialViewportY, ap.TargetViewportY)
	}
	if ap.SmoothScrollY != float64(initialViewportY) {
		t.Fatalf("expected SmoothScrollY == %d, got %f", initialViewportY, ap.SmoothScrollY)
	}
	if ap.ScrollVelocity != 0 {
		t.Fatalf("expected ScrollVelocity == 0, got %f", ap.ScrollVelocity)
	}

	// Animation ticks must not drift the viewport
	for tick := 0; tick < 10; tick++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)
		if m.viewportY != initialViewportY {
			t.Fatalf("tick %d: viewportY drifted! got %d, expected %d", tick, m.viewportY, initialViewportY)
		}
	}
}

func TestSmoothScroll_TerminalDrawerHeightConstraint(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updated.(*AppModel)

	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("content line %d", i))
	}
	updated, _ = m.Update(tea.PasteMsg{Text: strings.Join(lines, "\n")})
	m = updated.(*AppModel)

	// Open terminal drawer (takes 10 rows)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("expected terminal drawer to be open")
	}

	// Place cursor at line 18 and edit
	doc := m.eng.ActiveDocument()
	off18, _ := doc.Buffer.ByteOffsetForLine(18)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 18, Column: 0, Byte: off18}, Head: cbuf.Position{Line: 18, Column: 0, Byte: off18}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '$'}})
	m = updated.(*AppModel)

	// Usable height is reduced with terminal drawer open, so cursor at line 18 requires scrolling down
	if m.viewportY < 8 {
		t.Fatalf("expected viewportY >= 8 with terminal open, got %d", m.viewportY)
	}

	ap := m.splits.ActivePane()
	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("offsets desynchronized: viewportY=%d, target=%f, smooth=%f",
			m.viewportY, ap.TargetViewportY, ap.SmoothScrollY)
	}
}
