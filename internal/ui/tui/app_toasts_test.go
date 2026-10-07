package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/ui"
	"tahr/internal/core"
)

func TestAppToasts_CompactDimensionsAndWrapping(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	m.width = 100
	m.height = 40
	m.toasts = ui.NewToastManager(4)

	// Add a long toast message to verify wrapping into 2 lines
	m.postToast("info", "BUILD", "Compilation finished successfully with zero warnings and zero errors reported")
	if m.toasts.Count() != 1 {
		t.Fatalf("expected 1 toast, got %d", m.toasts.Count())
	}

	buf := buffer.NewBuffer(100, 40)
	m.renderToasts(buf, buffer.NewRect(0, 1, 100, 39))

	// Verify toast width is 30 cols (cardX = 100 - 30 - 1 = 69)
	cardX := 100 - 30 - 1
	// Verify close button '✕' is present
	closeCell := buf.Cell(cardX+27, 2)
	if closeCell == nil || closeCell.Rune != '✕' {
		t.Errorf("expected close button '✕' at (%d, 2), got %v", cardX+27, closeCell)
	}

	// Test click dismissal within new 30-col width and 5-row height
	clicked := m.handleToastClick(cardX+5, 4)
	if !clicked {
		t.Fatalf("expected click to hit the toast card at (%d, 4)", cardX+5)
	}
	if m.toasts.Count() != 0 {
		t.Fatalf("expected toast to be dismissed after click, got count: %d", m.toasts.Count())
	}
}

func TestAppToasts_WrapToastText(t *testing.T) {
	longMsg := "The quick brown fox jumps over the lazy dog and runs away"
	lines := wrapToastText(longMsg, 20, 2)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if buffer.StringWidth(lines[0]) > 20 {
		t.Errorf("line 0 exceeded maxWidth: %s (%d cols)", lines[0], buffer.StringWidth(lines[0]))
	}
	if buffer.StringWidth(lines[1]) > 20 {
		t.Errorf("line 1 exceeded maxWidth: %s (%d cols)", lines[1], buffer.StringWidth(lines[1]))
	}
	// Verify ellipsis on truncated 2nd line
	if !strings.HasSuffix(lines[1], "…") {
		t.Errorf("expected ellipsis on truncated line 1, got: %s", lines[1])
	}
}

func TestAppToasts_AutoExpiration(t *testing.T) {
	toasts := ui.NewToastManager(4)
	toasts.Add(ui.ToastInfo, "TITLE", "Short message", DefaultToastDuration)

	if toasts.Count() != 1 {
		t.Fatalf("expected 1 toast, got %d", toasts.Count())
	}

	// Should not expire immediately (e.g. after 500ms)
	toasts.Tick(time.Now().Add(500 * time.Millisecond))
	if toasts.Count() != 1 {
		t.Fatalf("expected toast to remain after 500ms, count=%d", toasts.Count())
	}

	// Should expire after DefaultToastDuration (2s)
	toasts.Tick(time.Now().Add(DefaultToastDuration + 100*time.Millisecond))
	if toasts.Count() != 0 {
		t.Fatalf("expected toast to expire after duration, count=%d", toasts.Count())
	}
}
