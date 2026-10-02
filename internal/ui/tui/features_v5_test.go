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
	"tahr/internal/core/plugin"
	"tahr/internal/ui"
)

// TestProjectExplorer_DockIconIsEXAndNotEmoji verifies that the left dock strip renders "EX" and not a broken emoji.
func TestProjectExplorer_DockIconIsEXAndNotEmoji(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30
	app.sidebarOpen = true
	app.sidebarMode = 0

	buf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: buf}
	app.View(frame)

	c0 := buf.Cell(0, 2)
	c1 := buf.Cell(1, 2)

	if c0 == nil || c1 == nil {
		t.Fatal("expected cells at (0, 2) and (1, 2) to exist")
	}

	if c0.Rune != 'E' || c1.Rune != 'X' {
		t.Fatalf("expected dock icon at row 2 to be 'EX', got c0=%c, c1=%c", c0.Rune, c1.Rune)
	}

	// Verify no emoji at (0, 2)
	if c0.Rune == '📁' {
		t.Fatal("dock icon must not be an emoji")
	}
}

// TestNewProjectModal_ButtonsHaveNoBrackets verifies that no buttons in NewProjectModal contain square brackets [ ].
func TestNewProjectModal_ButtonsHaveNoBrackets(t *testing.T) {
	modal := NewNewProjectModal("D:/dummy")
	modal.Open = true

	buf := buffer.NewBuffer(100, 30)
	theme := ui.DefaultTheme()

	modal.Render(buf, 100, 30, &theme)

	// Scan bottom row (30 - 2) for buttons
	bottomY := 30 - 2

	var bottomRowText strings.Builder
	for x := 0; x < 100; x++ {
		c := buf.Cell(x, bottomY)
		if c != nil && c.Rune != 0 {
			bottomRowText.WriteRune(c.Rune)
		} else {
			bottomRowText.WriteRune(' ')
		}
	}
	bottomStr := bottomRowText.String()

	// Verify "Create Project" and "Cancel" are present
	if !strings.Contains(bottomStr, "Create Project") {
		t.Fatalf("expected 'Create Project' button text in bottom row, got: %q", bottomStr)
	}
	if !strings.Contains(bottomStr, "Cancel") {
		t.Fatalf("expected 'Cancel' button text in bottom row, got: %q", bottomStr)
	}

	// Verify NO square brackets [ or ] in the entire bottom button row
	if strings.Contains(bottomStr, "[") || strings.Contains(bottomStr, "]") {
		t.Fatalf("buttons must NOT contain square brackets [ or ], found in bottom row: %q", bottomStr)
	}

	// Scan top row (0) for close button
	var topRowText strings.Builder
	for x := 0; x < 100; x++ {
		c := buf.Cell(x, 0)
		if c != nil && c.Rune != 0 {
			topRowText.WriteRune(c.Rune)
		}
	}
	topStr := topRowText.String()

	if strings.Contains(topStr, "[✕]") || strings.Contains(topStr, "[X]") {
		t.Fatalf("close button must NOT contain square brackets, got top row: %q", topStr)
	}
	if !strings.Contains(topStr, "✕") {
		t.Fatalf("expected close button ✕ in top row, got: %q", topStr)
	}
}

// TestNewProjectModal_NoEmojisAnywhere verifies that the modal contains zero smileys/emojis.
func TestNewProjectModal_NoEmojisAnywhere(t *testing.T) {
	modal := NewNewProjectModal("D:/dummy")
	modal.Open = true

	buf := buffer.NewBuffer(100, 30)
	theme := ui.DefaultTheme()
	modal.Render(buf, 100, 30, &theme)

	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			c := buf.Cell(x, y)
			if c == nil || c.Rune == 0 {
				continue
			}
			r := c.Rune
			// Check emoji ranges
			if (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF && r != '✕' && r != '│' && r != '─') {
				t.Fatalf("found emoji %c (U+%X) at (%d, %d); smileys are strictly forbidden in NewProjectModal", r, r, x, y)
			}
		}
	}
}

// TestNewProjectModal_PluginDiscoveryAndFullscreen tests dynamic discovery of templates and fullscreen layout.
func TestNewProjectModal_PluginDiscoveryAndFullscreen(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// Verify templates discovery from plugin manager
	if app.pluginMgr == nil {
		t.Fatal("expected pluginMgr to be initialized")
	}
	templates := app.pluginMgr.GetProjectTemplates()
	if len(templates) < 2 {
		t.Fatalf("expected at least 2 templates (Go and Blank), got %d", len(templates))
	}

	hasGo := false
	hasBlank := false
	for _, tpl := range templates {
		if tpl.ID == "go" || strings.Contains(tpl.Name, "Go") {
			hasGo = true
		}
		if tpl.ID == "blank" {
			hasBlank = true
		}
	}
	if !hasGo {
		t.Fatal("expected Go template to be discovered from plugins")
	}
	if !hasBlank {
		t.Fatal("expected Blank template fallback to be present")
	}

	// Open modal with discovered templates
	modal := NewNewProjectModal("D:/projects")
	modal.OpenWithTemplates(templates, "D:/projects", "Go 1.26.2 (D:/go/sdk/go1.26.2/bin/go.exe)")

	if !modal.Open {
		t.Fatal("expected modal to be open")
	}
	if modal.CurrentTemplate().ID != "go" {
		t.Fatalf("expected first template to be 'go', got %s", modal.CurrentTemplate().ID)
	}

	// Render in fullscreen buffer (110x32)
	buf := buffer.NewBuffer(110, 32)
	theme := ui.DefaultTheme()
	modal.Render(buf, 110, 32, &theme)

	// Check that ASCII file tree preview rendered
	var fullText strings.Builder
	for y := 0; y < 32; y++ {
		for x := 0; x < 110; x++ {
			c := buf.Cell(x, y)
			if c != nil && c.Rune != 0 {
				fullText.WriteRune(c.Rune)
			}
		}
		fullText.WriteString("\n")
	}
	screenContent := fullText.String()

	if !strings.Contains(screenContent, "PROJECT TEMPLATES") {
		t.Fatal("expected 'PROJECT TEMPLATES' column header in rendered modal")
	}
	if !strings.Contains(screenContent, "PROJECT STRUCTURE PREVIEW") {
		t.Fatal("expected 'PROJECT STRUCTURE PREVIEW' in right area")
	}
	if !strings.Contains(screenContent, "go.mod") {
		t.Fatal("expected 'go.mod' in preview tree")
	}
	if !strings.Contains(screenContent, "main.go") {
		t.Fatal("expected 'main.go' in preview tree")
	}
}

// TestNewProjectModal_NavigationAndSync verifies field focus cycling and name/location sync.
func TestNewProjectModal_NavigationAndSync(t *testing.T) {
	modal := NewNewProjectModal("D:/work")
	modal.Open = true

	// Initial field is Project Name
	if modal.ActiveField != FieldName {
		t.Fatalf("expected initial field to be FieldName (1), got %d", modal.ActiveField)
	}

	// Type in Project Name
	modal.HandleKey(input.Key{Rune: 'a'})
	modal.HandleKey(input.Key{Rune: 'p'})
	modal.HandleKey(input.Key{Rune: 'p'})

	// Verify Location was updated in sync
	if !strings.HasSuffix(modal.Location, "app") {
		t.Fatalf("expected location to end with 'app', got %s", modal.Location)
	}

	// Press Tab to cycle through fields
	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldLocation {
		t.Fatalf("expected Tab to switch to FieldLocation (2), got %d", modal.ActiveField)
	}

	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldModule {
		t.Fatalf("expected Tab to switch to FieldModule (3), got %d", modal.ActiveField)
	}

	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldSDK {
		t.Fatalf("expected Tab to switch to FieldSDK (4), got %d", modal.ActiveField)
	}

	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldCreateBtn {
		t.Fatalf("expected Tab to switch to FieldCreateBtn (5), got %d", modal.ActiveField)
	}

	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldCancelBtn {
		t.Fatalf("expected Tab to switch to FieldCancelBtn (6), got %d", modal.ActiveField)
	}

	modal.HandleKey(input.Key{Type: input.KeyTab})
	if modal.ActiveField != FieldTemplates {
		t.Fatalf("expected Tab to wrap around to FieldTemplates (0), got %d", modal.ActiveField)
	}

	// Up/Down in FieldTemplates
	modal.HandleKey(input.Key{Type: input.KeyDown})
	if modal.SelectedTemplate != 1 {
		t.Fatalf("expected KeyDown to select template 1, got %d", modal.SelectedTemplate)
	}

	modal.HandleKey(input.Key{Type: input.KeyUp})
	if modal.SelectedTemplate != 0 {
		t.Fatalf("expected KeyUp to select template 0, got %d", modal.SelectedTemplate)
	}
}

// TestNewProjectModal_MouseClicks tests clicking left templates and action buttons.
func TestNewProjectModal_MouseClicks(t *testing.T) {
	modal := NewNewProjectModal("D:/work")
	modal.Open = true
	screenW := 100
	screenH := 30

	// 1. Click on template row 1 in left column (x = 5, y = 3 + 1 = 4)
	clicked, _ := modal.HandleClick(5, 4, screenW, screenH)
	if !clicked || modal.SelectedTemplate != 1 {
		t.Fatalf("expected click on template 1 to select it, clicked=%v, sel=%d", clicked, modal.SelectedTemplate)
	}

	// 2. Click on Cancel button
	bottomY := screenH - 2
	cancelX := screenW - 2 - len([]rune(" Cancel ")) - 1
	clicked, _ = modal.HandleClick(cancelX+2, bottomY, screenW, screenH)
	if !clicked || modal.Open {
		t.Fatalf("expected click on Cancel button to close modal, clicked=%v, open=%v", clicked, modal.Open)
	}

	// 3. Re-open and test Create button click
	modal.Open = true
	var createdTpl plugin.ProjectTemplate
	var createdName, createdLoc, createdMod string
	modal.OnCreateTemplate = func(tpl plugin.ProjectTemplate, name, location, module string) {
		createdTpl = tpl
		createdName = name
		createdLoc = location
		createdMod = module
	}

	btnCancelText := " Cancel "
	btnCreateText := " Create Project "
	cCancelX := screenW - 2 - len([]rune(btnCancelText)) - 1
	cCreateX := cCancelX - len([]rune(btnCreateText)) - 2

	clicked, confirmed := modal.HandleClick(cCreateX+3, bottomY, screenW, screenH)
	if !clicked || !confirmed || modal.Open {
		t.Fatalf("expected click on Create Project to confirm, clicked=%v, conf=%v, open=%v", clicked, confirmed, modal.Open)
	}
	if createdName == "" || createdLoc == "" {
		t.Fatalf("expected valid name and location on create, got name=%q, loc=%q", createdName, createdLoc)
	}
	_ = createdTpl
	_ = createdMod
}

// TestCreateProjectFromTemplate_Scaffolding tests multi-file directory and placeholder generation.
func TestCreateProjectFromTemplate_Scaffolding(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-cli-test-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetProj := filepath.Join(tmpDir, "my-cli-tool")

	eng := core.NewEngine()
	app := NewAppModel(eng)

	tpl := plugin.ProjectTemplate{
		ID:            "go-cli",
		Name:          "Go (CLI Application)",
		Description:   "CLI tool layout",
		Category:      "Go",
		Icon:          "",
		DefaultModule: "example.com/{{.ProjectName}}",
		Files: []plugin.ProjectTemplateFile{
			{
				Path:    "go.mod",
				Content: "module {{.ModulePath}}\n\ngo 1.22\n",
			},
			{
				Path:    "cmd/{{.ProjectName}}/main.go",
				Content: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"{{.ProjectName}} running\")\n}\n",
			},
			{
				Path:    "internal/app/app.go",
				Content: "package app\n\nfunc Exec() {}\n",
			},
			{
				Path:    ".gitignore",
				Content: "bin/\n",
			},
			{
				Path:    "README.md",
				Content: "# {{.ProjectName}}\n",
			},
		},
	}

	err = app.CreateProjectFromTemplate(tpl, "my-cli-tool", targetProj, "github.com/myuser/my-cli-tool")
	if err != nil {
		t.Fatalf("CreateProjectFromTemplate failed: %v", err)
	}

	// Verify all files on disk
	expectedFiles := []string{
		"go.mod",
		filepath.Join("cmd", "my-cli-tool", "main.go"),
		filepath.Join("internal", "app", "app.go"),
		".gitignore",
		"README.md",
	}

	for _, rel := range expectedFiles {
		fp := filepath.Join(targetProj, rel)
		data, err := os.ReadFile(fp)
		if err != nil {
			t.Fatalf("expected file %s to exist: %v", rel, err)
		}
		if rel == "go.mod" {
			if !strings.Contains(string(data), "module github.com/myuser/my-cli-tool") {
				t.Fatalf("expected module placeholder replacement in go.mod, got: %s", string(data))
			}
		}
		if rel == filepath.Join("cmd", "my-cli-tool", "main.go") {
			if !strings.Contains(string(data), "my-cli-tool running") {
				t.Fatalf("expected ProjectName placeholder replacement in main.go, got: %s", string(data))
			}
		}
	}

	// Verify workspace updated
	if app.workspaceDir != targetProj {
		t.Fatalf("expected workspaceDir to switch to %s, got %s", targetProj, app.workspaceDir)
	}
}

func TestFindReplaceModal_NoBracketsAndNoHarshBackground(t *testing.T) {
	modal := NewFindReplaceModal()
	modal.OpenFind("test")
	modal.UpdateMatches(2, 14)

	buf := buffer.NewBuffer(80, 24)
	theme := ui.DefaultTheme()

	modal.Render(buf, 80, 24, &theme)

	// Scan row startY+1 for bracket occurrences around match count
	startY := 2
	lineText := ""
	for x := 0; x < 80; x++ {
		c := buf.Cell(x, startY+1)
		lineText += string(c.Rune)
	}

	if strings.Contains(lineText, "[2/14]") {
		t.Fatalf("expected match counter NOT to have brackets [2/14], got: %s", lineText)
	}
	if !strings.Contains(lineText, "2/14") {
		t.Fatalf("expected match counter to show '2/14', got: %s", lineText)
	}

	// Verify prev/next buttons exist and do not use PopupSelBg
	prevIdx := strings.Index(lineText, "◀")
	nextIdx := strings.Index(lineText, "▶")
	if prevIdx < 0 || nextIdx < 0 {
		t.Fatalf("expected ◀ and ▶ buttons on line, got: %s", lineText)
	}

	prevCell := buf.Cell(prevIdx, startY+1)
	nextCell := buf.Cell(nextIdx, startY+1)
	activeBg := toColor(theme.PopupSelBg)

	if prevCell.Bg == activeBg.Value {
		t.Fatalf("expected ◀ button to NOT have dark PopupSelBg background")
	}
	if nextCell.Bg == activeBg.Value {
		t.Fatalf("expected ▶ button to NOT have dark PopupSelBg background")
	}
}

func TestFindReplaceModal_CursorNavigationAndEditing(t *testing.T) {
	modal := NewFindReplaceModal()
	modal.OpenFind("foo")

	if modal.FindCursor != 3 {
		t.Fatalf("expected initial FindCursor 3, got %d", modal.FindCursor)
	}

	// Left arrow moves cursor
	modal.HandleKey(input.Key{Type: input.KeyLeft})
	if modal.FindCursor != 2 {
		t.Fatalf("expected FindCursor 2 after KeyLeft, got %d", modal.FindCursor)
	}

	// Insert character 'x' at cursor: should become "foxo"
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'x'})
	if modal.FindQuery != "foxo" {
		t.Fatalf("expected FindQuery 'foxo', got %q", modal.FindQuery)
	}
	if modal.FindCursor != 3 {
		t.Fatalf("expected FindCursor 3 after insert, got %d", modal.FindCursor)
	}

	// Home key moves cursor to start
	modal.HandleKey(input.Key{Type: input.KeyHome})
	if modal.FindCursor != 0 {
		t.Fatalf("expected FindCursor 0 after Home, got %d", modal.FindCursor)
	}

	// Delete key deletes forward
	modal.HandleKey(input.Key{Type: input.KeyDelete})
	if modal.FindQuery != "oxo" {
		t.Fatalf("expected FindQuery 'oxo' after Delete at start, got %q", modal.FindQuery)
	}

	// End key moves to end
	modal.HandleKey(input.Key{Type: input.KeyEnd})
	if modal.FindCursor != 3 {
		t.Fatalf("expected FindCursor 3 after End, got %d", modal.FindCursor)
	}

	// Ctrl+K clears field
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'k', Mod: input.ModCtrl})
	if modal.FindQuery != "" || modal.FindCursor != 0 {
		t.Fatalf("expected empty query after Ctrl+K, got %q (cursor %d)", modal.FindQuery, modal.FindCursor)
	}
}

func TestFindReplaceModal_ToggleReplaceMode(t *testing.T) {
	modal := NewFindReplaceModal()
	modal.OpenFind("hello")

	if modal.ReplaceMode {
		t.Fatal("expected ReplaceMode false initially")
	}

	// Toggle via Ctrl+H
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'h', Mod: input.ModCtrl})
	if !modal.ReplaceMode {
		t.Fatal("expected ReplaceMode true after Ctrl+H")
	}
	if modal.ActiveField != 1 {
		t.Fatalf("expected ActiveField 1 (Replace) after toggle, got %d", modal.ActiveField)
	}

	// Toggle back via Ctrl+F
	modal.HandleKey(input.Key{Type: input.KeyRune, Rune: 'f', Mod: input.ModCtrl})
	if modal.ReplaceMode {
		t.Fatal("expected ReplaceMode false after Ctrl+F")
	}

	// Toggle via mouse click on ⇄ button
	screenW := 80
	screenH := 24
	modalW := 68
	startX := screenW - modalW - 3
	findFieldW := modalW - 37
	findFieldEnd := startX + 8 + findFieldW
	toggleBtnX := findFieldEnd + 16

	consumed, _ := modal.HandleClick(toggleBtnX, 3, screenW, screenH) // mouseY = startY+1 = 3
	if !consumed || !modal.ReplaceMode {
		t.Fatalf("expected clicking ⇄ to toggle ReplaceMode, got consumed=%v, replMode=%v, toggleBtnX=%d", consumed, modal.ReplaceMode, toggleBtnX)
	}
}

func TestUndoRedo_AltBackspaceAndCtrlU(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 80
	app.height = 24

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	_ = doc.Buffer.ApplyEdit(0, 0, "original")

	// Apply an edit
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: " extra"})
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "original extra" {
		t.Fatalf("expected 'original extra', got %q", string(txt))
	}

	// Test Undo via Alt+Backspace
	altBksp := input.Key{Type: input.KeyBackspace, Mod: input.ModAlt}
	if !app.MatchBinding(altBksp, "undo") {
		t.Fatal("expected MatchBinding to recognize Alt+Backspace as undo")
	}
	_, _ = app.handleKey(altBksp)

	txtAfterUndo, _ := doc.Buffer.GetText()
	if string(txtAfterUndo) != "original" {
		t.Fatalf("expected 'original' after Alt+Backspace undo, got %q", string(txtAfterUndo))
	}

	// Test Redo via Shift+Alt+Backspace
	shiftAltBksp := input.Key{Type: input.KeyBackspace, Mod: input.ModAlt | input.ModShift}
	if !app.MatchBinding(shiftAltBksp, "redo") {
		t.Fatal("expected MatchBinding to recognize Shift+Alt+Backspace as redo")
	}
	_, _ = app.handleKey(shiftAltBksp)

	txtAfterRedo, _ := doc.Buffer.GetText()
	if string(txtAfterRedo) != "original extra" {
		t.Fatalf("expected 'original extra' after Shift+Alt+Backspace redo, got %q", string(txtAfterRedo))
	}

	// Test Undo via Ctrl+U
	ctrlU := input.Key{Type: input.KeyRune, Rune: 'u', Mod: input.ModCtrl}
	if !app.MatchBinding(ctrlU, "undo") {
		t.Fatal("expected MatchBinding to recognize Ctrl+U as undo")
	}
	_, _ = app.handleKey(ctrlU)

	txtAfterCtrlU, _ := doc.Buffer.GetText()
	if string(txtAfterCtrlU) != "original" {
		t.Fatalf("expected 'original' after Ctrl+U undo, got %q", string(txtAfterCtrlU))
	}

	// Test Undo while FindReplaceModal is open
	app.openFindModal()
	if !app.findReplaceModal.Open {
		t.Fatal("expected findReplaceModal to be open")
	}

	// Re-do to "original extra" first
	app.performRedo()

	// In Find modal, pressing Alt+Backspace should return FRActionUndo and trigger performUndo()
	consumed, action := app.findReplaceModal.HandleKey(altBksp)
	if !consumed || action != FRActionUndo {
		t.Fatalf("expected FRActionUndo from modal, got consumed=%v action=%s", consumed, action)
	}

	_, _ = app.handleFindReplaceAction(action)
	txtModalUndo, _ := doc.Buffer.GetText()
	if string(txtModalUndo) != "original" {
		t.Fatalf("expected 'original' after modal undo, got %q", string(txtModalUndo))
	}
}

