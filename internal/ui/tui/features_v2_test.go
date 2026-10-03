package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/baibeicha/goatui/pkg/ui"
)

func TestSettingsLanguageSwitching(t *testing.T) {
	defer i18n.SetLocale("en")

	pm, err := plugin.NewManager("")
	if err != nil {
		t.Fatal(err)
	}
	defer pm.Close()

	locales := i18n.AvailableLocales()
	if len(locales) < 2 {
		t.Fatalf("expected at least 2 locales (en, ru), got %d: %+v", len(locales), locales)
	}

	st := NewSettingsState()
	st.SetPluginManager(pm)
	st.CategoryIdx = 1
	st.FocusRight = true
	st.FieldIdx = 0 // language

	// Initial cycle from en to ru
	st.cycleCurrentField()
	if st.Current.Language != "ru" {
		t.Errorf("expected Current.Language to be 'ru', got %q", st.Current.Language)
	}
	if i18n.GetLocale() != "ru" {
		t.Errorf("expected i18n.GetLocale() to be 'ru', got %q", i18n.GetLocale())
	}

	// Cycle back from ru to en
	st.cycleCurrentField()
	if st.Current.Language != "en" {
		t.Errorf("expected Current.Language to be 'en', got %q", st.Current.Language)
	}
	if i18n.GetLocale() != "en" {
		t.Errorf("expected i18n.GetLocale() to be 'en', got %q", i18n.GetLocale())
	}
}

func TestAppModel_DoubleClickWordSelection(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = false

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	content := "func mySpecialVariable(arg int) int {\n\treturn arg\n}\n"
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), content)

	// Editor top is row 2. Strip width is 3 (width >= 70).
	// Gutter width is app.gutterWidth().
	// Click on 'Special' in 'mySpecialVariable' at line 0, col 10 (targetX = 3 + gutterW + 10)
	gutterW := app.gutterWidth()
	stripLeftW := 3
	targetX := stripLeftW + gutterW + 10
	targetY := 2

	// First click
	click1 := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: targetX, Y: targetY}}
	_, _ = app.handleMouse(click1)

	// Second click within 100ms on same location
	click2 := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: targetX, Y: targetY}}
	_, _ = app.handleMouse(click2)

	selText := app.selectedText()
	if selText != "mySpecialVariable" {
		t.Fatalf("expected double click to select 'mySpecialVariable', got %q", selText)
	}
}

func TestAppModel_OccurrenceHighlighting(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = false

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	content := "func calculatesum() int {\n\tsum := 10\n\ttotalsum := sum + 20\n\treturn totalsum\n}\n"
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), content)

	// Place cursor at 'sum' in 'sum := 10' (line 1, col 2)
	offset, _ := doc.Buffer.ByteOffsetForLine(1)
	doc.Buffer.SetSelections([]corebuf.Selection{
		corebuf.NewCursor(corebuf.Position{Line: 1, Column: 2, Byte: offset + 2}),
	})

	word := app.wordUnderCursor()
	if word != "sum" {
		t.Fatalf("expected wordUnderCursor to be 'sum', got %q", word)
	}

	// Find matches in document (matching substrings, including calculatesum and totalsum)
	matches := app.findMatchesInDocument(word, true, false)
	if len(matches) < 4 {
		t.Fatalf("expected at least 4 occurrences of 'sum' across substrings, got %d", len(matches))
	}
}

func TestAppModel_SidebarResizeAllThreeWays(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = true
	app.sidebarWidth = 26
	app.sidebarMode = 0
	if app.settings != nil {
		app.settings.Current.TreePosition = "left"
	}

	// 1. Mouse Drag on border
	stripLeftW := 3
	borderX := stripLeftW + app.sidebarWidth
	dragStart := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: borderX, Y: 10}}
	_, _ = app.handleMouse(dragStart)
	if !app.sidebarDragging {
		t.Fatal("expected sidebarDragging to be true after clicking sidebar border")
	}

	dragMove := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseDrag, Button: input.MouseLeft, X: borderX + 6, Y: 10}}
	_, _ = app.handleMouse(dragMove)
	if app.sidebarWidth != 32 {
		t.Fatalf("expected sidebarWidth 32 after dragging +6, got %d", app.sidebarWidth)
	}

	dragRelease := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseRelease, Button: input.MouseLeft, X: borderX + 6, Y: 10}}
	_, _ = app.handleMouse(dragRelease)
	if app.sidebarDragging {
		t.Fatal("expected sidebarDragging to be false after release")
	}

	// 2. Panel Header Buttons [◀] and [▶]
	headerY := 2
	treeStartX := stripLeftW
	sideW := app.sidebarWidth
	btnStart := treeStartX + sideW - 5 // dense dock buttons length is 5: ◀ ▶ ⇄

	// Click ◀ (btnStart) to shrink by 2
	btnLeft := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: btnStart, Y: headerY}}
	_, _ = app.handleMouse(btnLeft)
	if app.sidebarWidth != 30 {
		t.Fatalf("expected sidebarWidth 30 after clicking ◀, got %d", app.sidebarWidth)
	}

	// Click ▶ (btnStart + 2) to expand by 2
	sideW = app.sidebarWidth
	btnStart = treeStartX + sideW - 5
	btnRight := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: btnStart + 2, Y: headerY}}
	_, _ = app.handleMouse(btnRight)
	if app.sidebarWidth != 32 {
		t.Fatalf("expected sidebarWidth 32 after clicking ▶, got %d", app.sidebarWidth)
	}

	// 3. Hotkeys Alt+[ and Alt+]
	keyShrink := input.Key{Rune: '[', BaseKey: '[', Mod: input.ModAlt}
	_, _ = app.handleKey(keyShrink)
	if app.sidebarWidth != 30 {
		t.Fatalf("expected sidebarWidth 30 after Alt+[, got %d", app.sidebarWidth)
	}

	keyExpand := input.Key{Rune: ']', BaseKey: ']', Mod: input.ModAlt}
	_, _ = app.handleKey(keyExpand)
	if app.sidebarWidth != 32 {
		t.Fatalf("expected sidebarWidth 32 after Alt+], got %d", app.sidebarWidth)
	}

	// Mouse Wheel over sidebar header
	wheelDown := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseWheelDown, X: treeStartX + 5, Y: headerY}}
	_, _ = app.handleMouse(wheelDown)
	if app.sidebarWidth != 30 {
		t.Fatalf("expected sidebarWidth 30 after wheel down, got %d", app.sidebarWidth)
	}

	wheelUp := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseWheelUp, X: treeStartX + 5, Y: headerY}}
	_, _ = app.handleMouse(wheelUp)
	if app.sidebarWidth != 32 {
		t.Fatalf("expected sidebarWidth 32 after wheel up, got %d", app.sidebarWidth)
	}
}

func TestAppModel_TerminalResizeAllThreeWays(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 40
	app.terminal.Open = true
	app.terminalFocused = true
	app.terminal.Height = 10

	termTop := app.height - 1 - app.terminal.Height

	// 1. Mouse Drag on top border
	dragStart := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: 15, Y: termTop}}
	_, _ = app.handleMouse(dragStart)
	if !app.termDragging {
		t.Fatal("expected termDragging to be true")
	}

	dragMove := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseDrag, Button: input.MouseLeft, X: 15, Y: termTop - 4}}
	_, _ = app.handleMouse(dragMove)
	if app.terminal.Height != 14 {
		t.Fatalf("expected terminal height 14 after drag, got %d", app.terminal.Height)
	}

	dragRelease := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseRelease, Button: input.MouseLeft, X: 15, Y: termTop - 4}}
	_, _ = app.handleMouse(dragRelease)
	if app.termDragging {
		t.Fatal("expected termDragging to be false after release")
	}

	// 2. Header Buttons ▲ and ▼
	termTop = app.height - 1 - app.terminal.Height
	btnUp := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: app.width - 18, Y: termTop}}
	_, _ = app.handleMouse(btnUp)
	if app.terminal.Height != 17 {
		t.Fatalf("expected terminal height 17 after ▲ click, got %d", app.terminal.Height)
	}

	termTop = app.height - 1 - app.terminal.Height
	btnDown := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: app.width - 16, Y: termTop}}
	_, _ = app.handleMouse(btnDown)
	if app.terminal.Height != 14 {
		t.Fatalf("expected terminal height 14 after ▼ click, got %d", app.terminal.Height)
	}

	// 3. Hotkeys Alt+Up and Alt+Down & Wheel
	keyUp := input.Key{Type: input.KeyUp, Mod: input.ModAlt}
	_, _ = app.handleKey(keyUp)
	if app.terminal.Height != 16 {
		t.Fatalf("expected terminal height 16 after Alt+Up, got %d", app.terminal.Height)
	}

	keyDown := input.Key{Type: input.KeyDown, Mod: input.ModAlt}
	_, _ = app.handleKey(keyDown)
	if app.terminal.Height != 14 {
		t.Fatalf("expected terminal height 14 after Alt+Down, got %d", app.terminal.Height)
	}

	termTop = app.height - 1 - app.terminal.Height
	wheelUp := tea.MouseMsg{Mouse: input.Mouse{Button: input.MouseWheelUp, X: 50, Y: termTop}}
	_, _ = app.handleMouse(wheelUp)
	if app.terminal.Height != 16 {
		t.Fatalf("expected terminal height 16 after wheel up, got %d", app.terminal.Height)
	}
}

func TestFindReplaceModal_Workflow(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), "foo bar foo baz foo\n")

	// Open modal via Ctrl+F
	keyF := input.Key{Type: input.KeyRune, Rune: 'f', Mod: input.ModCtrl}
	_, _ = app.handleKey(keyF)
	if app.findReplaceModal == nil || !app.findReplaceModal.Open {
		t.Fatal("expected findReplaceModal to be open after Ctrl+F")
	}

	// Type query "foo"
	app.findReplaceModal.FindQuery = "foo"
	app.findReplaceModal.ReplaceQuery = "qux"
	app.updateFindMatches()

	if app.findReplaceModal.TotalMatches != 3 {
		t.Fatalf("expected 3 matches for 'foo', got %d", app.findReplaceModal.TotalMatches)
	}

	// Test Next / Prev: first Next selects match 1 (wrapped from end), second Next selects match 2
	_, _ = app.handleFindReplaceAction(FRActionNext)
	if app.findReplaceModal.CurrentMatch != 1 {
		t.Fatalf("expected currentMatch 1 after first Next, got %d", app.findReplaceModal.CurrentMatch)
	}

	_, _ = app.handleFindReplaceAction(FRActionNext)
	if app.findReplaceModal.CurrentMatch != 2 {
		t.Fatalf("expected currentMatch 2 after second Next, got %d", app.findReplaceModal.CurrentMatch)
	}

	_, _ = app.handleFindReplaceAction(FRActionPrev)
	if app.findReplaceModal.CurrentMatch != 1 {
		t.Fatalf("expected currentMatch 1 after Prev, got %d", app.findReplaceModal.CurrentMatch)
	}

	// Test Replace All
	_, _ = app.handleFindReplaceAction(FRActionReplaceAll)

	line0, _ := doc.Buffer.GetLine(0)
	expected := "qux bar qux baz qux\n"
	if string(line0) != expected {
		t.Fatalf("expected buffer to contain %q, got %q", expected, string(line0))
	}
}

func TestProjectSymbolRename_FallbackAndModal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr_rename_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "file1.go")
	file2 := filepath.Join(tmpDir, "file2.go")
	_ = os.WriteFile(file1, []byte("package main\n\nfunc myOriginalSymbol() int {\n\treturn 42\n}\n"), 0644)
	_ = os.WriteFile(file2, []byte("package main\n\nfunc callIt() int {\n\treturn myOriginalSymbol()\n}\n"), 0644)

	eng := core.NewEngine()
	doc, err := eng.Open(file1)
	if err != nil {
		t.Fatal(err)
	}

	app := NewAppModel(eng)
	app.SetWorkspaceDir(tmpDir)
	app.width = 120
	app.height = 40
	if app.settings != nil {
		app.settings.Current.Keybindings["rename"] = "F2"
		delete(app.settings.Current.Keybindings, "tree_toggle")
	}

	// Place cursor on myOriginalSymbol
	doc.Buffer.SetSelections([]corebuf.Selection{
		corebuf.NewCursor(corebuf.Position{Line: 2, Column: 8, Byte: 22}),
	})

	// Press F2 to open rename modal
	f2Key := input.Key{Type: input.KeyF2}
	_, _ = app.handleKey(f2Key)
	if app.renameModal == nil || !app.renameModal.Open {
		t.Fatal("expected renameModal to be open after F2")
	}
	if app.renameModal.OldName != "myOriginalSymbol" {
		t.Fatalf("expected oldName 'myOriginalSymbol', got %q", app.renameModal.OldName)
	}

	// Perform rename to newRenamedSymbol
	_, _ = app.executeSymbolRename("myOriginalSymbol", "newRenamedSymbol", 2, 8, file1)

	// Verify file1 content
	b1, _ := os.ReadFile(file1)
	if !strings.Contains(string(b1), "newRenamedSymbol") || strings.Contains(string(b1), "myOriginalSymbol") {
		t.Fatalf("file1 not properly renamed: %s", string(b1))
	}

	// Verify file2 content
	b2, _ := os.ReadFile(file2)
	if !strings.Contains(string(b2), "newRenamedSymbol") || strings.Contains(string(b2), "myOriginalSymbol") {
		t.Fatalf("file2 not properly renamed: %s", string(b2))
	}
}

func TestSettings_ToolActionDLV_EnterAndRussianLayout(t *testing.T) {
	st := NewSettingsState()
	st.ToolAction.Open = true
	st.ToolAction.ToolName = "dlv"
	st.ToolAction.InstallCmd = "go install github.com/go-delve/delve/cmd/dlv@latest"

	started := false
	st.OnToolInstallStarted = func(tool, cmd string) {
		started = true
	}

	// 1. Press Russian 'в' (equivalent to 'D')
	handled, closed := st.handleToolActionKey(input.Key{Type: input.KeyRune, Rune: 'в'})
	if !handled || !closed {
		t.Fatalf("expected 'в' to handle and close modal: handled=%v, closed=%v", handled, closed)
	}
	if !started {
		t.Fatal("expected install to start on 'в'")
	}

	// 2. Press KeyEnter
	started = false
	st.ToolAction.Open = true
	handled, closed = st.handleToolActionKey(input.Key{Type: input.KeyEnter})
	if !handled || !closed {
		t.Fatalf("expected Enter to handle and close modal: handled=%v, closed=%v", handled, closed)
	}
	if !started {
		t.Fatal("expected install to start on Enter")
	}
}

func TestTerminal_CtrlZ_NoHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		vt := KeyToVT(input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl})
		if vt != nil {
			t.Fatalf("expected Ctrl+Z on Windows to return nil (no 0x1A EOF sent), got %v", vt)
		}

		vtCyr := KeyToVT(input.Key{Type: input.KeyRune, Rune: 'я', Mod: input.ModCtrl})
		if vtCyr != nil {
			t.Fatalf("expected Ctrl+Я on Windows to return nil, got %v", vtCyr)
		}
	}
}

func TestToast_DismissAndTick(t *testing.T) {
	toasts := ui.NewToastManager(4)

	toasts.Info("TITLE", "Message body")
	if toasts.Count() != 1 {
		t.Fatalf("expected 1 toast, got %d", toasts.Count())
	}

	// Test Dismiss by click
	screenRect := buffer.Rect{X: 0, Y: 0, Width: 100, Height: 40}
	// Click inside toast area (top-right)
	clicked := toasts.HandleClick(95, 3, screenRect)
	if !clicked {
		t.Fatal("expected click to hit toast")
	}
	if toasts.Count() != 0 {
		t.Fatalf("expected toast to be dismissed after click, count=%d", toasts.Count())
	}

	// Test auto-expiration via Tick
	toasts.Info("AUTO", "Expires soon")
	if toasts.Count() != 1 {
		t.Fatalf("expected 1 toast, got %d", toasts.Count())
	}
	toasts.Tick(time.Now().Add(5 * time.Second))
	if toasts.Count() != 0 {
		t.Fatalf("expected toast to expire after 5s, count=%d", toasts.Count())
	}
}
