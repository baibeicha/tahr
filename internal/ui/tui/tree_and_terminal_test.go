package tui

import (
	"os"
	"path/filepath"
	"testing"

	"tahr/internal/core"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
)

type mockProgramSender struct {
	sent []tea.Msg
}

func (m *mockProgramSender) Send(msg tea.Msg) {
	m.sent = append(m.sent, msg)
}

func TestAppModel_ClampSidebar(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr_tree_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("world"), 0644)
	_ = os.Mkdir(filepath.Join(tmpDir, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "sub", "child.txt"), []byte("child"), 0644)

	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.SetWorkspaceDir(tmpDir)
	app.height = 30
	app.width = 100

	// 1. Initial tree should be populated and valid
	app.clampSidebar()
	if len(app.treeFlat) == 0 {
		t.Fatal("expected treeFlat to be populated")
	}

	// 2. Artificial out of bounds scroll should be clamped to maxScroll
	app.sidebarScrollY = 9999
	app.clampSidebar()
	visH := app.visibleTreeHeight()
	maxScroll := max(0, len(app.treeFlat)-visH)
	if app.sidebarScrollY != maxScroll {
		t.Fatalf("expected sidebarScrollY to be clamped to %d, got %d", maxScroll, app.sidebarScrollY)
	}

	// 3. Negative scroll clamped to 0
	app.sidebarScrollY = -50
	app.clampSidebar()
	if app.sidebarScrollY != 0 {
		t.Fatalf("expected sidebarScrollY to be clamped to 0, got %d", app.sidebarScrollY)
	}

	// 4. treeSel out of bounds clamped to len - 1
	app.treeSel = 9999
	app.clampSidebar()
	if app.treeSel != len(app.treeFlat)-1 {
		t.Fatalf("expected treeSel to be clamped to %d, got %d", len(app.treeFlat)-1, app.treeSel)
	}

	// 5. If treeFlat is cleared, clampSidebar automatically restores it
	app.treeFlat = nil
	app.clampSidebar()
	if len(app.treeFlat) == 0 {
		t.Fatal("expected clampSidebar to restore treeFlat when cleared")
	}
}

func TestAppModel_TerminalHeightAdjustment(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.height = 40
	app.width = 100
	app.terminal.Open = true
	app.terminalFocused = true
	app.terminal.Height = 10

	// 1. Alt+Up increases height
	keyUp := input.Key{Type: input.KeyUp, Mod: input.ModAlt}
	_, _ = app.handleKey(keyUp)
	if app.terminal.Height != 12 {
		t.Fatalf("expected terminal height 12 after Alt+Up, got %d", app.terminal.Height)
	}

	// 2. Alt+Down decreases height
	keyDown := input.Key{Type: input.KeyDown, Mod: input.ModAlt}
	_, _ = app.handleKey(keyDown)
	if app.terminal.Height != 10 {
		t.Fatalf("expected terminal height 10 after Alt+Down, got %d", app.terminal.Height)
	}

	// 3. Mouse Wheel Up on terminal header increases height
	termTop := app.height - 1 - app.terminal.Height
	wheelUp := tea.MouseMsg{Mouse: input.Mouse{Button: input.MouseWheelUp, X: 50, Y: termTop}}
	_, _ = app.handleMouse(wheelUp)
	if app.terminal.Height != 12 {
		t.Fatalf("expected terminal height 12 after header wheel up, got %d", app.terminal.Height)
	}

	// 4. Mouse Wheel Down on terminal header decreases height
	termTop = app.height - 1 - app.terminal.Height
	wheelDown := tea.MouseMsg{Mouse: input.Mouse{Button: input.MouseWheelDown, X: 50, Y: termTop}}
	_, _ = app.handleMouse(wheelDown)
	if app.terminal.Height != 10 {
		t.Fatalf("expected terminal height 10 after header wheel down, got %d", app.terminal.Height)
	}

	// 5. Button clicks on ▲ (w-18) and ▼ (w-16)
	termTop = app.height - 1 - app.terminal.Height
	btnUpClick := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: app.width - 18, Y: termTop}}
	_, _ = app.handleMouse(btnUpClick)
	if app.terminal.Height != 13 {
		t.Fatalf("expected terminal height 13 after ▲ click, got %d", app.terminal.Height)
	}

	termTop = app.height - 1 - app.terminal.Height
	btnDownClick := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: app.width - 16, Y: termTop}}
	_, _ = app.handleMouse(btnDownClick)
	if app.terminal.Height != 10 {
		t.Fatalf("expected terminal height 10 after ▼ click, got %d", app.terminal.Height)
	}

	// 6. Border Drag
	termTop = app.height - 1 - app.terminal.Height
	dragStart := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: 10, Y: termTop}}
	_, _ = app.handleMouse(dragStart)
	if !app.termDragging {
		t.Fatal("expected termDragging to be true after clicking header border")
	}

	// Dragging UP by 5 rows increases height by 5
	dragMove := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseDrag, Button: input.MouseLeft, X: 10, Y: termTop - 5}}
	_, _ = app.handleMouse(dragMove)
	if app.terminal.Height != 15 {
		t.Fatalf("expected terminal height 15 after dragging up by 5, got %d", app.terminal.Height)
	}

	// Release drag
	dragRelease := tea.MouseMsg{Mouse: input.Mouse{Action: input.MouseRelease, Button: input.MouseLeft, X: 10, Y: termTop - 5}}
	_, _ = app.handleMouse(dragRelease)
	if app.termDragging {
		t.Fatal("expected termDragging to be false after mouse release")
	}
}

func TestAppModel_TerminalOnData(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	mockProg := &mockProgramSender{}
	app.SetProgram(mockProg)

	if app.terminal.OnData == nil {
		t.Fatal("expected app.terminal.OnData to be set after SetProgram")
	}

	// Calling OnData must send termTickMsg{} to program
	app.terminal.OnData()
	if len(mockProg.sent) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(mockProg.sent))
	}
	if _, ok := mockProg.sent[0].(termTickMsg); !ok {
		t.Fatalf("expected termTickMsg, got %T", mockProg.sent[0])
	}
}

func TestAppModel_SidebarSwitchingAndClamp(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.height = 30
	app.width = 100
	app.sidebarOpen = true
	app.sidebarMode = 0
	app.sidebarScrollY = 500 // simulate out-of-bounds scroll

	// Switching to structure mode (4)
	clickStructure := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: 1, Y: 4}}
	_, _ = app.handleMouse(clickStructure)
	if app.sidebarMode != 1 {
		t.Fatalf("expected sidebarMode 1, got %d", app.sidebarMode)
	}

	// Switching back to explorer mode (2)
	clickExplorer := tea.MouseMsg{Mouse: input.Mouse{Action: input.MousePress, Button: input.MouseLeft, X: 1, Y: 2}}
	_, _ = app.handleMouse(clickExplorer)
	if app.sidebarMode != 0 {
		t.Fatalf("expected sidebarMode 0, got %d", app.sidebarMode)
	}

	// Verify sidebarScrollY was clamped
	visH := app.visibleTreeHeight()
	maxScroll := max(0, len(app.treeFlat)-visH)
	if app.sidebarScrollY > maxScroll {
		t.Fatalf("expected sidebarScrollY <= %d, got %d", maxScroll, app.sidebarScrollY)
	}
	if len(app.treeFlat) == 0 {
		t.Fatal("expected treeFlat not to be empty")
	}
}
