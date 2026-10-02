package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/ui"
)

func TestMinimap_RenderAndHitTest(t *testing.T) {
	buf := buffer.NewBuffer(30, 20)
	th := ui.DefaultTheme()

	lines := []string{
		"package main",
		"",
		"func main() {",
		"    println(\"hello world\")",
		"}",
	}

	getLine := func(idx int) string {
		if idx >= 0 && idx < len(lines) {
			return lines[idx]
		}
		return ""
	}

	RenderMinimap(buf, th, 20, 2, 6, 10, len(lines), 0, 3, getLine)

	// Verify hit test
	lineTop := MinimapHitTest(2, 2, 10, 100)
	if lineTop != 0 {
		t.Errorf("expected line 0 for top click, got %d", lineTop)
	}

	lineMid := MinimapHitTest(7, 2, 10, 100)
	if lineMid != 50 {
		t.Errorf("expected line 50 for mid click, got %d", lineMid)
	}

	lineBottom := MinimapHitTest(11, 2, 10, 100)
	if lineBottom != 90 {
		t.Errorf("expected line 90 for bottom click, got %d", lineBottom)
	}
}
