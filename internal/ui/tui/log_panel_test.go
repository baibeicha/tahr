package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/logviewer"
	"tahr/internal/ui"
)

// TestLogPanel_ZeroMockLines verifies that LogPanel starts completely empty with zero hardcoded mock lines.
func TestLogPanel_ZeroMockLines(t *testing.T) {
	p := NewLogPanel()
	if p.Open {
		t.Errorf("expected panel to start closed")
	}
	if p.Ring == nil {
		t.Fatalf("expected ring buffer to be initialized")
	}
	if p.Ring.Len() != 0 {
		t.Fatalf("expected 0 initial log lines (no mock lines allowed), got %d", p.Ring.Len())
	}
	filtered := p.getFilteredLines()
	if len(filtered) != 0 {
		t.Fatalf("expected 0 filtered lines, got %d", len(filtered))
	}
}

// TestLogPanel_PillButtonsAndFiltering verifies filter pills: All, Error, Warn, Info, Tail, Clear.
func TestLogPanel_PillButtonsAndFiltering(t *testing.T) {
	p := NewLogPanel()
	p.Open = true

	// Append test lines
	p.AppendLog("2026-10-06 10:00:00 [DEBUG] cache query")
	p.AppendLog("2026-10-06 10:00:01 [INFO] HTTP GET /ready 200")
	p.AppendLog("2026-10-06 10:00:02 [WARN] slow query took 450ms")
	p.AppendLog("2026-10-06 10:00:03 [ERROR] failed to connect to db")
	p.AppendLog("2026-10-06 10:00:04 [FATAL] panic: nil pointer")

	if p.Ring.Len() != 5 {
		t.Fatalf("expected 5 lines in ring buffer, got %d", p.Ring.Len())
	}

	// 1. All pill (default)
	all := p.getFilteredLines()
	if len(all) != 5 {
		t.Fatalf("expected 5 lines for 'All' filter, got %d", len(all))
	}

	// 2. Error pill (should match ERROR and FATAL)
	p.ActiveLevelFilter = logviewer.LevelError
	p.markDirty()
	errLines := p.getFilteredLines()
	if len(errLines) != 2 {
		t.Fatalf("expected 2 lines for 'Error' filter (Error+Fatal), got %d", len(errLines))
	}

	// 3. Warn pill
	p.ActiveLevelFilter = logviewer.LevelWarn
	p.markDirty()
	warnLines := p.getFilteredLines()
	if len(warnLines) != 1 || warnLines[0].Level != logviewer.LevelWarn {
		t.Fatalf("expected 1 line for 'Warn' filter, got %d", len(warnLines))
	}

	// 4. Info pill
	p.ActiveLevelFilter = logviewer.LevelInfo
	p.markDirty()
	infoLines := p.getFilteredLines()
	if len(infoLines) != 1 || infoLines[0].Level != logviewer.LevelInfo {
		t.Fatalf("expected 1 line for 'Info' filter, got %d", len(infoLines))
	}

	// 5. Clear pill
	p.Clear()
	if p.Ring.Len() != 0 {
		t.Fatalf("expected ring buffer to be empty after Clear, got %d", p.Ring.Len())
	}
	clearedLines := p.getFilteredLines()
	if len(clearedLines) != 0 {
		t.Fatalf("expected 0 filtered lines after Clear, got %d", len(clearedLines))
	}
}

// TestLogPanel_SearchFiltering verifies text and regex search filtering.
func TestLogPanel_SearchFiltering(t *testing.T) {
	p := NewLogPanel()
	p.Open = true

	p.AppendLog("2026-10-06 10:00:00 [INFO] Client user_123 authorized")
	p.AppendLog("2026-10-06 10:00:01 [INFO] Client user_456 disconnected")
	p.AppendLog("2026-10-06 10:00:02 [ERROR] Client user_789 invalid token")

	// Text search
	p.SearchText = "authorized"
	p.markDirty()
	res := p.getFilteredLines()
	if len(res) != 1 || !strings.Contains(res[0].Raw, "user_123") {
		t.Fatalf("expected 1 match for 'authorized', got %d", len(res))
	}

	// Regex search
	p.SearchIsRegex = true
	p.SearchText = `user_\d+`
	p.markDirty()
	resRegex := p.getFilteredLines()
	if len(resRegex) != 3 {
		t.Fatalf("expected 3 regex matches for 'user_\\d+', got %d", len(resRegex))
	}
}

// TestLogPanel_AutoScrollAndHistoricalInspection tests auto-scrolling and pausing on historical inspection.
func TestLogPanel_AutoScrollAndHistoricalInspection(t *testing.T) {
	p := NewLogPanel()
	p.Open = true
	p.Height = 6 // contentH = 4

	for i := 1; i <= 20; i++ {
		p.AppendLog(fmt.Sprintf("2026-10-06 10:00:%02d [INFO] Line %d", i, i))
	}

	// AutoScroll should be true initially
	if !p.AutoScroll {
		t.Errorf("expected AutoScroll to be true initially")
	}

	// Simulate keyboard Up arrow to inspect historical lines
	p.HandleKey(input.Key{Type: input.KeyUp})
	if p.AutoScroll {
		t.Errorf("expected AutoScroll to pause (false) when inspecting historical lines with KeyUp")
	}

	// Scroll up multiple times
	p.ScrollUp(5)
	if p.AutoScroll {
		t.Errorf("expected AutoScroll to remain false while scrolling up")
	}

	// Scroll down back to the bottom
	contentH := p.Height - 2
	p.ScrollDown(30, contentH)
	if !p.AutoScroll {
		t.Errorf("expected AutoScroll to automatically resume (true) when reaching bottom")
	}

	// Test Tail toggle key 't'
	p.HandleKey(input.Key{Rune: 't'})
	if p.AutoScroll {
		t.Errorf("expected 't' to toggle AutoScroll off")
	}
	p.HandleKey(input.Key{Rune: 't'})
	if !p.AutoScroll {
		t.Errorf("expected 't' to toggle AutoScroll on")
	}
}

// TestLogPanel_MouseWheelAndPillClicks verifies mouse interaction and hitboxes.
func TestLogPanel_MouseWheelAndPillClicks(t *testing.T) {
	p := NewLogPanel()
	p.Open = true
	p.Height = 10
	theme := ui.CatppuccinMocha()

	for i := 1; i <= 15; i++ {
		p.AppendLog(fmt.Sprintf("2026-10-06 10:00:%02d [INFO] Event %d", i, i))
	}

	// Render to populate hitboxes
	buf := buffer.NewBuffer(80, 10)
	p.Render(buf, 0, 0, 80, 10, theme)

	if len(p.pillHitboxes) == 0 {
		t.Fatalf("expected pill hitboxes to be registered during Render")
	}

	// Test MouseWheelUp pauses auto-scroll
	p.HandleMouse(input.Mouse{Button: input.MouseWheelUp, X: 10, Y: 5}, 0, 0, 80, 10)
	if p.AutoScroll {
		t.Errorf("expected mouse wheel up to pause auto-scroll")
	}

	// Test MouseWheelDown to bottom resumes auto-scroll
	for i := 0; i < 10; i++ {
		p.HandleMouse(input.Mouse{Button: input.MouseWheelDown, X: 10, Y: 5}, 0, 0, 80, 10)
	}
	if !p.AutoScroll {
		t.Errorf("expected mouse wheel down to bottom to resume auto-scroll")
	}

	// Find 'Error' pill hitbox and click it
	var errorPill *logPillButton
	for i := range p.pillHitboxes {
		if p.pillHitboxes[i].ID == "error" {
			errorPill = &p.pillHitboxes[i]
			break
		}
	}
	if errorPill == nil {
		t.Fatalf("error pill hitbox not found")
	}

	// Click Error pill
	consumed := p.HandleMouse(input.Mouse{
		Button: input.MouseLeft,
		Action: input.MousePress,
		X:      errorPill.X + 1,
		Y:      errorPill.Y,
	}, 0, 0, 80, 10)

	if !consumed {
		t.Errorf("expected mouse click on Error pill to be consumed")
	}
	if p.ActiveLevelFilter != logviewer.LevelError {
		t.Errorf("expected active level filter to be LevelError after click, got %v", p.ActiveLevelFilter)
	}
}

// TestLogPanel_RenderBottomAndRightSidebar verifies rendering in both dock positions.
func TestLogPanel_RenderBottomAndRightSidebar(t *testing.T) {
	p := NewLogPanel()
	p.Open = true
	theme := ui.CatppuccinMocha()

	p.AppendLog("2026-10-06 12:00:00 [DEBUG] Debug trace event")
	p.AppendLog("2026-10-06 12:00:01 [INFO] Server started")
	p.AppendLog("2026-10-06 12:00:02 [WARN] High load")
	p.AppendLog("2026-10-06 12:00:03 [ERROR] Timeout")

	// 1. Render as Bottom Panel (wide: 120 cols x 12 lines)
	bufBottom := buffer.NewBuffer(120, 12)
	p.Dock = LogDockBottom
	p.Render(bufBottom, 0, 0, 120, 12, theme)

	// 2. Render as Right Sidebar (narrow: 45 cols x 30 lines)
	bufSidebar := buffer.NewBuffer(45, 30)
	p.Dock = LogDockRight
	p.Render(bufSidebar, 0, 0, 45, 30, theme)

	// Verify no panics and status summary formatting
	summary := p.StatusSummary()
	if summary == "" {
		t.Errorf("expected non-empty status summary")
	}
}
