package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core/sdk"
	"tahr/internal/ui"
)

func TestRenderNode_Basic(t *testing.T) {
	theme := &ui.Theme{
		Foreground:  0xFFFFFF,
		Background:  0x1E1E2E,
		SelectionBg: 0x313244,
		Function:    0x89B4FA,
		LineNumber:  0x6C7086,
	}
	vr := NewViewRenderer(theme)
	vr.FocusedID = "btn_submit"

	buf := buffer.NewBuffer(80, 25)

	tree := sdk.NewVStack("root",
		sdk.NewHStack("hdr",
			sdk.NewText("title", "Services Overview"),
			sdk.NewButton("btn_submit", "Submit", "cmd.submit"),
		),
		sdk.NewDataGrid("grid",
			[]string{"ID", "Status"},
			[][]string{{"svc-1", "OK"}, {"svc-2", "FAIL"}},
		),
	)

	vr.RenderNode(buf, tree, buffer.NewRect(0, 0, 80, 25))

	// Verify buffer has non-empty cells
	foundText := false
	for x := 0; x < 20; x++ {
		cell := buf.Cell(x, 0)
		if cell.Rune == 'S' {
			foundText = true
			break
		}
	}
	if !foundText {
		t.Errorf("expected text 'Services Overview' in rendered buffer")
	}
}

func TestFocusCycle(t *testing.T) {
	tree := sdk.NewVStack("root",
		sdk.NewInput("in_filter", "filter...", "", "cmd.filter"),
		sdk.NewButton("btn_apply", "Apply", "cmd.apply"),
		sdk.NewButton("btn_reset", "Reset", "cmd.reset"),
	)

	ids := CollectFocusableIDs(tree)
	if len(ids) != 3 {
		t.Fatalf("expected 3 focusable elements, got %d", len(ids))
	}
	if ids[0] != "in_filter" || ids[1] != "btn_apply" || ids[2] != "btn_reset" {
		t.Errorf("unexpected focusable IDs order: %+v", ids)
	}

	// Tab next cycle
	next := NextFocusID(tree, "in_filter")
	if next != "btn_apply" {
		t.Errorf("expected next focus btn_apply, got %s", next)
	}
	next = NextFocusID(tree, "btn_reset")
	if next != "in_filter" {
		t.Errorf("expected wrap around to in_filter, got %s", next)
	}

	// Shift-Tab prev cycle
	prev := PrevFocusID(tree, "in_filter")
	if prev != "btn_reset" {
		t.Errorf("expected prev wrap around to btn_reset, got %s", prev)
	}
}

func TestErrorBoundaryRender(t *testing.T) {
	theme := &ui.Theme{
		Foreground: 0xFFFFFF,
		Background: 0x1E1E2E,
		Function:   0x89B4FA,
	}
	vr := NewViewRenderer(theme)
	buf := buffer.NewBuffer(80, 25)

	vr.RenderErrorBoundary(buf, buffer.NewRect(2, 2, 70, 15), "ext-kafka", "nil pointer dereference")

	// Verify error boundary text was rendered
	cell := buf.Cell(2, 2)
	if cell.Rune != '╭' {
		t.Errorf("expected top-left border '╭', got %c", cell.Rune)
	}
}
