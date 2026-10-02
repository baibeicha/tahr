package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/git"
	"tahr/internal/ui"
)

func TestGitModal_KeyIsolationAndTabSwitching(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.Hunks = []git.Hunk{
		{Staged: false, Lines: []string{"-old", "+new"}},
		{Staged: false, Lines: []string{"+added"}},
	}

	// 1. Initial tab is 0
	if gm.CurrentTab != 0 {
		t.Fatalf("expected initial tab 0, got %d", gm.CurrentTab)
	}

	// 2. Press '2' -> switches to Tab 1
	handled := gm.HandleKey(input.Key{Type: input.KeyRune, Rune: '2'})
	if !handled || gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after pressing '2', got %d (handled=%v)", gm.CurrentTab, handled)
	}

	// 3. Press '1' -> switches to Tab 0
	handled = gm.HandleKey(input.Key{Type: input.KeyRune, Rune: '1'})
	if !handled || gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after pressing '1', got %d (handled=%v)", gm.CurrentTab, handled)
	}

	// 4. Press Right -> switches to Tab 1
	handled = gm.HandleKey(input.Key{Type: input.KeyRight})
	if !handled || gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after pressing Right, got %d", gm.CurrentTab)
	}

	// 5. Press Left -> switches to Tab 0
	handled = gm.HandleKey(input.Key{Type: input.KeyLeft})
	if !handled || gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after pressing Left, got %d", gm.CurrentTab)
	}

	// 6. Complete Key Isolation: arbitrary key returns true without modifying document
	handled = gm.HandleKey(input.Key{Type: input.KeyRune, Rune: 'z'})
	if !handled {
		t.Errorf("expected key isolation (return true) for 'z'")
	}

	// 7. Tab key cycles
	gm.HandleKey(input.Key{Type: input.KeyTab})
	if gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after Tab key, got %d", gm.CurrentTab)
	}
	gm.HandleKey(input.Key{Type: input.KeyTab})
	if gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after second Tab key, got %d", gm.CurrentTab)
	}
}

func TestGitModal_HandleClick(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.Hunks = []git.Hunk{
		{Staged: false, Lines: []string{"+line 1"}},
		{Staged: false, Lines: []string{"+line 2"}},
	}

	screenW, screenH := 100, 30
	modalW := screenW - 12 // 88
	modalH := screenH - 6  // 24
	startX := (screenW - modalW) / 2 // 6
	startY := (screenH - modalH) / 2 // 3

	// Click Tab 2
	tab1Len := len(" [1] Interactive Hunk Staging ")
	tab2X := startX + 2 + tab1Len + 3
	gm.HandleClick(tab2X, startY+1, screenW, screenH)
	if gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after clicking Tab 2, got %d", gm.CurrentTab)
	}

	// Click Tab 1
	tab1X := startX + 5
	gm.HandleClick(tab1X, startY+1, screenW, screenH)
	if gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after clicking Tab 1, got %d", gm.CurrentTab)
	}

	// Click hunk 2 in list
	contentTop := startY + 3
	gm.HandleClick(startX+5, contentTop+1, screenW, screenH)
	if gm.SelectedHunk != 1 {
		t.Errorf("expected SelectedHunk 1, got %d", gm.SelectedHunk)
	}

	// Click outside modal -> closes
	gm.HandleClick(0, 0, screenW, screenH)
	if gm.Open {
		t.Errorf("expected modal to close when clicking outside")
	}
}

func TestGitModal_Render(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	buf := buffer.NewBuffer(100, 30)
	th := ui.DefaultTheme()

	gm.Render(buf, 100, 30, &th)
	if !gm.Open {
		t.Errorf("expected modal to remain open after render")
	}
}
