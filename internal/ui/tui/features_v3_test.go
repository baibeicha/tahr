package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/ui"
)

// TestEditor_CtrlClick_GotoDefinition tests that Ctrl + Left Click jumps to symbol definition.
func TestEditor_CtrlClick_GotoDefinition(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40
	app.sidebarOpen = false

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	code := "package main\n\nfunc targetFunction() {\n    println(\"hello\")\n}\n\nfunc main() {\n    targetFunction()\n}\n"
	_ = doc.Buffer.ApplyEdit(0, doc.Buffer.TotalBytes(), code)

	editorTop := 2
	gutterW := app.gutterWidth()
	stripLeftW := 3

	// Line 7 has `    targetFunction()`.
	// Column 6 is on `targetFunction`.
	// Click on line 7 (editorTop + 7) at column stripLeftW + gutterW + 6 with ModCtrl
	clickMsg := tea.MouseMsg{
		Mouse: input.Mouse{
			Action: input.MousePress,
			Button: input.MouseLeft,
			X:      stripLeftW + gutterW + 6,
			Y:      editorTop + 7,
			Mod:    input.ModCtrl,
		},
	}

	_, _ = app.handleMouse(clickMsg)

	// Verify that cursor jumped to definition line (line 2: `func targetFunction()`)
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatal("expected selections after Ctrl+Click")
	}

	if sels[0].Head.Line != 2 {
		t.Fatalf("expected cursor to jump to line 2 (func targetFunction), got line %d", sels[0].Head.Line)
	}
}

// TestOmnibar_Scrolling tests that Omnibar correctly scrolls through items exceeding viewport height.
func TestOmnibar_Scrolling(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30

	app.openOmnibar("commands")
	if len(app.omnibarItems) < 15 {
		t.Fatalf("expected at least 15 commands in omnibar, got %d", len(app.omnibarItems))
	}

	if app.omnibarScroll != 0 {
		t.Fatalf("expected initial omnibarScroll 0, got %d", app.omnibarScroll)
	}

	// Navigate down 10 times
	for i := 0; i < 10; i++ {
		_, _ = app.handleOmnibarKey(input.Key{Type: input.KeyDown})
	}

	if app.omnibarSel != 10 {
		t.Fatalf("expected omnibarSel to be 10, got %d", app.omnibarSel)
	}

	// Render omnibar to trigger viewport clamp
	buf := buffer.NewBuffer(app.width, app.height)
	app.renderOmnibar(buf, app.width, app.height)

	if app.omnibarScroll < 3 {
		t.Fatalf("expected omnibarScroll >= 3 after navigating down 10 items, got %d", app.omnibarScroll)
	}

	// Test Mouse Wheel scrolling in Omnibar
	startX := (app.width - 60) / 2
	startY := (app.height - 12) / 2
	wheelUp := tea.MouseMsg{
		Mouse: input.Mouse{
			Action: input.MousePress,
			Button: input.MouseWheelUp,
			X:      startX + 10,
			Y:      startY + 4,
		},
	}
	_, _ = app.handleMouse(wheelUp)
	if app.omnibarSel != 9 {
		t.Fatalf("expected omnibarSel 9 after wheel up, got %d", app.omnibarSel)
	}
}

// TestMainMenu_OpenFolderAndNewProject tests that menu actions trigger open_dir and newProjectModal.
func TestMainMenu_OpenFolderAndNewProject(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// 1. Open Folder / Project...
	app.executeMainMenuItem("open_folder")
	if !app.omnibarOpen || app.omnibarMode != "open_dir" {
		t.Fatalf("expected open_folder to open omnibar in open_dir mode, got open=%v, mode=%s", app.omnibarOpen, app.omnibarMode)
	}

	// 2. New Project...
	app.omnibarOpen = false
	app.executeMainMenuItem("new_project")
	if app.newProjectModal == nil || !app.newProjectModal.Open {
		t.Fatal("expected new_project to open newProjectModal")
	}
}

// TestNewProjectModal_AbsolutePath tests creating a new Go project by absolute path.
func TestNewProjectModal_AbsolutePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-newproj-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetProjPath := filepath.Join(tmpDir, "my-super-project")

	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 120
	app.height = 40

	modal := NewNewProjectModal(tmpDir)
	modal.Open = true
	modal.SelectedTemplate = 0 // "go"
	modal.PathInput = targetProjPath

	var createdKind, createdPath string
	modal.OnCreate = func(kind, path string) {
		createdKind = kind
		createdPath = path
		_ = app.CreateProject(kind, tmpDir, path)
	}

	// Press Enter to confirm
	consumed, confirmed := modal.HandleKey(input.Key{Type: input.KeyEnter})
	if !consumed || !confirmed {
		t.Fatal("expected Enter to confirm modal")
	}

	if createdKind != "go" || createdPath != targetProjPath {
		t.Fatalf("unexpected create params: kind=%s, path=%s", createdKind, createdPath)
	}

	// Check files created on disk
	if _, err := os.Stat(filepath.Join(targetProjPath, "main.go")); err != nil {
		t.Fatalf("expected main.go to exist in %s", targetProjPath)
	}
	if _, err := os.Stat(filepath.Join(targetProjPath, "go.mod")); err != nil {
		t.Fatalf("expected go.mod to exist in %s", targetProjPath)
	}

	// Check that workspace directory updated
	if app.workspaceDir != targetProjPath {
		t.Fatalf("expected app workspaceDir to be %s, got %s", targetProjPath, app.workspaceDir)
	}
}

// TestFindReplaceModal_GeometryAndNoClipping tests that modal width 68 renders hints without clipping.
func TestFindReplaceModal_GeometryAndNoClipping(t *testing.T) {
	modal := NewFindReplaceModal()
	modal.Open = true
	modal.FindQuery = "testSymbol"
	modal.TotalMatches = 5
	modal.CurrentMatch = 1

	buf := buffer.NewBuffer(120, 40)
	theme := ui.DefaultTheme()

	modal.Render(buf, 120, 40, &theme)

	// Check that row 5 (hint) contains "Alt+W: Word" and is not clipped
	// StartX is 120 - 68 - 3 = 49
	startX := 120 - 68 - 3
	startY := 2

	var row3Text string
	for x := startX; x < startX+68; x++ {
		c := buf.Cell(x, startY+3)
		row3Text += string(c.Rune)
	}

	if !strings.Contains(row3Text, "Alt+W: Word") {
		t.Fatalf("expected row 3 to contain full hint 'Alt+W: Word', got %q", row3Text)
	}
}
