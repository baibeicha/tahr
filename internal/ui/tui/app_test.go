package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/baibeicha/goatui/pkg/ui"

	"tahr/internal/core"
	cbuf "tahr/internal/core/buffer"
	"tahr/internal/core/git"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tahr-test-*")
	if err == nil {
		defer os.RemoveAll(tmpDir)
		_ = os.Setenv("TAHR_CONFIG_DIR", tmpDir)
	}
	os.Exit(m.Run())
}

func TestAppModel_Lifecycle(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Simulate terminal resize
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)
	if m.width != 100 || m.height != 30 {
		t.Fatalf("expected 100x30, got %dx%d", m.width, m.height)
	}

	// Type text
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'h'}})
	m = updated.(*AppModel)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'i'}})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	l0, _ := doc.Buffer.GetLine(0)
	if string(l0) != "hi" {
		t.Fatalf("expected 'hi', got %q", string(l0))
	}

	// Omnibar file search
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Rune: 'p', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if !m.omnibarOpen {
		t.Fatal("expected Omnibar to be open after Ctrl+P")
	}

	// Filter in omnibar
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'r'}})
	m = updated.(*AppModel)
	if m.omnibarQuery != "r" {
		t.Fatalf("expected query 'r', got %q", m.omnibarQuery)
	}

	// Escape closes omnibar
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)
	if m.omnibarOpen {
		t.Fatal("expected Omnibar to be closed after Esc")
	}

	// Render into frame
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Verify header contains menu icon ≡
	cell0 := frameBuf.Cell(1, 0)
	if cell0 == nil || cell0.Rune != '≡' {
		t.Fatalf("expected '≡' at header position, got %v", cell0)
	}
}

func TestAppModel_BracketedPaste(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	pastedText := "func test() {\n    return 42\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: pastedText})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	l0, _ := doc.Buffer.GetLine(0)
	if string(l0) != "func test() {\n" {
		t.Fatalf("unexpected line 0: %q", string(l0))
	}
}

func TestAppModel_SyntaxHighlightingInView(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Paste Go code
	code := "package main\nfunc hello() {\n return 42\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: code})
	m := updated.(*AppModel)

	// Render into frame
	frameBuf := buffer.NewBuffer(80, 24)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Row 0 is Header, Row 1 is TabBar, Row 2 is Editor line 0.
	// Columns: 0..2 is Left Activity Strip, then gutter (width 5), then column 8 is first text character ('p' of "package")
	gutterW := m.gutterWidth()
	stripLeftW := 3
	cellP := frameBuf.Cell(stripLeftW+gutterW, 2)
	if cellP == nil || cellP.Rune != 'p' {
		t.Fatalf("expected 'p' at text start, got %v", cellP)
	}

	expectedKwColor := m.theme.Keyword
	if cellP.Fg != expectedKwColor {
		t.Errorf("expected syntax color for keyword (0x%x), got 0x%x", expectedKwColor, cellP.Fg)
	}
}

func TestAppModel_OmnibarCommandExecution(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Open omnibar in commands mode
	m := model
	m.openOmnibar("commands")
	if !m.omnibarOpen || m.omnibarMode != "commands" {
		t.Fatalf("expected omnibar open in commands mode")
	}

	// Filter for "Toggle Breakpoint"
	m.omnibarQuery = "Breakpoint"
	m.updateOmnibarCandidates()
	if len(m.omnibarItems) == 0 {
		t.Fatalf("expected to find Toggle Breakpoint command")
	}

	// Press Enter to execute
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	if len(m.breakpoints) != 1 || !m.breakpoints[0] {
		t.Errorf("expected breakpoint on line 0, got %v", m.breakpoints)
	}
}

func TestAppModel_ContextualCompletionPopup(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Write code with custom function name
	code := "func calculateQuantumEntropy() int {\n return 100\n}"
	updated, _ := model.Update(tea.PasteMsg{Text: code})
	m := updated.(*AppModel)

	// Trigger completion popup
	m.triggerCompletionPopup()
	if !m.popupVisible {
		t.Fatalf("expected completion popup to be visible")
	}

	found := false
	for _, item := range m.popupItems {
		if item == "calculateQuantumEntropy" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'calculateQuantumEntropy' in popup items, got: %v", m.popupItems)
	}
}

func TestAppModel_SpaceBarTyping(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// 1. Editor space bar typing
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'b'}})

	doc := m.eng.ActiveDocument()
	text, _ := doc.Buffer.GetText()
	if string(text) != "a b" {
		t.Fatalf("expected 'a b' in buffer, got %q", string(text))
	}

	// 2. Omnibar space bar typing
	m.openOmnibar("files")
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'm'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'g'}})
	if m.omnibarQuery != "m g" {
		t.Fatalf("expected omnibar query 'm g', got %q", m.omnibarQuery)
	}
}

func TestAppModel_CyrillicHotkeys(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Type initial text
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x'}})

	// Ctrl + 'ы' (Russian Ctrl+S -> Save)
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'ы',
		Mod:  input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if !strings.HasPrefix(m.statusMessage, "Saved") {
		t.Errorf("expected Ctrl+ы to save, got status %q", m.statusMessage)
	}

	// Ctrl + 'з' (Russian Ctrl+P -> Omnibar)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'з',
		Mod:  input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if !m.omnibarOpen {
		t.Fatalf("expected Ctrl+з to open Omnibar")
	}
	m.omnibarOpen = false

	// Ctrl + 'и' (Russian Ctrl+B -> Toggle Project Tree)
	origOpen := m.sidebarOpen
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'и',
		Mod:  input.ModCtrl,
	}})
	m = updated.(*AppModel)
	if m.sidebarOpen == origOpen {
		t.Errorf("expected Ctrl+и to toggle sidebar")
	}

	// Ctrl + 'ф' (Russian Ctrl+A -> Select All)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'ф',
		Mod:  input.ModCtrl,
	}})
	m = updated.(*AppModel)
	doc := m.eng.ActiveDocument()
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatalf("expected active selection after Ctrl+ф")
	}
}

func TestAppModel_BuildAndRunHotkeys(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// F5 (Run)
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF5}})
	m = updated.(*AppModel)
	if !m.outputOpen && (m.terminal == nil || !m.terminal.Open) {
		t.Fatalf("expected F5 to open output drawer or terminal")
	}
	if !strings.Contains(m.statusMessage, "Starting") && !strings.Contains(m.statusMessage, "Running") {
		t.Errorf("expected Run status message, got %q", m.statusMessage)
	}
	m.outputOpen = false
	if m.terminal != nil {
		m.terminal.Open = false
	}

	// F7 (Build)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF7}})
	m = updated.(*AppModel)
	if !m.outputOpen {
		t.Fatalf("expected F7 to open output drawer")
	}
	if !strings.Contains(m.statusMessage, "Building") {
		t.Errorf("expected Build status message, got %q", m.statusMessage)
	}
}

func TestAppModel_HeaderToolbarClicks(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	frameBuf := buffer.NewBuffer(80, 24)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	var menuBtn, termBtn *toolbarBtn
	for i := range m.toolbarButtons {
		if m.toolbarButtons[i].id == "menu" {
			menuBtn = &m.toolbarButtons[i]
		}
		if m.toolbarButtons[i].id == "term" {
			termBtn = &m.toolbarButtons[i]
		}
	}
	if menuBtn == nil || termBtn == nil {
		t.Fatalf("expected toolbar buttons to be initialized")
	}

	// Click Menu button on top toolbar
	updated, _ := m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      menuBtn.minX + 1,
			Y:      0,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.mainMenuOpen {
		t.Fatalf("expected clicking Menu button to open main menu")
	}
	m.mainMenuOpen = false

	// Click Term button on top toolbar
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      termBtn.minX + 1,
			Y:      0,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if m.terminal == nil || !m.terminal.Open {
		t.Fatalf("expected clicking Term button to open terminal drawer")
	}

	// Click Tree icon on left Activity Bar (1, 2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      1,
			Y:      2,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.sidebarOpen {
		t.Fatalf("expected clicking Activity Bar tree to open sidebar")
	}

	// Click Settings icon on left Activity Bar (1, m.height-2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      1,
			Y:      m.height - 2,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if m.settings == nil || !m.settings.Open {
		t.Fatalf("expected clicking Activity Bar settings to open settings modal")
	}
}

func TestAppModel_FullIDEFeaturesVerification(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// 1. Enable Project Tree Sidebar
	m.SetSidebarOpen(true)
	if !m.sidebarOpen {
		t.Fatalf("expected sidebar to be open")
	}

	// 2. Open Output Drawer
	m.outputOpen = true
	m.runner.lines = []string{"Compiling project...", "Build completed in 12ms"}

	// 3. Type text with spaces
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'v'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'r'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '='}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeySpace}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '1'}})

	text, _ := doc.Buffer.GetText()
	if !strings.Contains(string(text), "var x = 1") {
		t.Fatalf("expected 'var x = 1' in buffer, got %q", string(text))
	}

	// 4. Render into GoatUI Frame Buffer
	frameBuf := buffer.NewBuffer(120, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Check Title Header at row 0 (Menu button " ≡ ")
	cellMenu := frameBuf.Cell(1, 0)
	if cellMenu == nil || cellMenu.Rune != '≡' {
		t.Fatalf("expected '≡' at header menu start, got %v", cellMenu)
	}

	// Check Explorer Title on left dock (stripLeft=3, sideW=26 -> treeStartX=3)
	cellExp := frameBuf.Cell(4, 2)
	if cellExp == nil || cellExp.Rune != 'P' {
		t.Fatalf("expected 'P' of PROJECT EXPLORER at left dock, got %v", cellExp)
	}

	// Check Tree divider at column stripLeft + sideW = 29
	cellDiv := frameBuf.Cell(29, 2)
	if cellDiv == nil || cellDiv.Rune != '│' {
		t.Fatalf("expected '│' at column 29, got %v", cellDiv)
	}

	// Check Status bar at row 29
	cellStatus := frameBuf.Cell(1, 29)
	if cellStatus == nil || cellStatus.Rune != 'N' {
		t.Fatalf("expected 'N' of NORMAL mode at status bar, got %v", cellStatus)
	}
}

func TestAppModel_FindInDocument(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Put multiple lines
	code := "first line\nsecond search_target line\nthird line\nfourth search_target line\n"
	_, _ = m.Update(tea.PasteMsg{Text: code})

	// Open Omnibar in find mode
	m.openOmnibar("find")
	if !m.omnibarOpen || m.omnibarMode != "find" {
		t.Fatalf("expected Omnibar to open in 'find' mode, got %v (%s)", m.omnibarOpen, m.omnibarMode)
	}

	// Type search query
	var updated tea.Model
	for _, r := range "search_target" {
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
		m = updated.(*AppModel)
	}

	if len(m.omnibarItems) != 2 {
		t.Fatalf("expected 2 matching lines, got %d: %v", len(m.omnibarItems), m.omnibarItems)
	}

	// Press Enter to jump to the first match
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 || sels[0].Head.Line != 1 {
		t.Errorf("expected cursor line 1 after selecting first match, got %v", sels)
	}
}

func TestAppModel_GotoLine(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Add 50 lines
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		sb.WriteString("line\n")
	}
	_, _ = m.Update(tea.PasteMsg{Text: sb.String()})

	// Press Ctrl+G
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'g', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if !m.omnibarOpen || m.omnibarMode != "goto" {
		t.Fatalf("expected Omnibar in 'goto' mode")
	}

	// Type line number '2', '5' -> line 25
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '2'}})
	m = updated.(*AppModel)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '5'}})
	m = updated.(*AppModel)

	// Press Enter
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	sels := doc.Buffer.GetSelections()
	// Line 25 (1-based) is index 24 (0-based)
	if len(sels) == 0 || sels[0].Head.Line != 24 {
		t.Errorf("expected cursor at line 24, got %v", sels)
	}
}

func TestAppModel_ClipboardOperations(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Paste text
	_, _ = m.Update(tea.PasteMsg{Text: "clipboard_secret_token"})

	// Select all via Ctrl+A
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Copy via Ctrl+C
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'c', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if m.clipboardText != "clipboard_secret_token" {
		t.Fatalf("expected clipboard text 'clipboard_secret_token', got %q", m.clipboardText)
	}

	// Move cursor to end
	doc.Buffer.SetSelections([]cbuf.Selection{{
		Anchor: cbuf.Position{Line: 0, Column: 22, Byte: 22},
		Head:   cbuf.Position{Line: 0, Column: 22, Byte: 22},
	}})

	// Paste via Ctrl+V
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'v', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txt, _ := doc.Buffer.GetText()
	expected := "clipboard_secret_tokenclipboard_secret_token"
	if string(txt) != expected {
		t.Fatalf("expected %q, got %q", expected, string(txt))
	}

	// Select all and Cut via Ctrl+X
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a', Mod: input.ModCtrl}})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txtAfterCut, _ := doc.Buffer.GetText()
	if string(txtAfterCut) != "" {
		t.Fatalf("expected empty buffer after cut, got %q", string(txtAfterCut))
	}
	if m.clipboardText != expected {
		t.Fatalf("expected cut content in clipboard, got %q", m.clipboardText)
	}
}

func TestAppModel_DAPAndLSPControls(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// F9 toggles breakpoint
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF9}})
	m = updated.(*AppModel)
	if !m.breakpoints[0] {
		t.Fatalf("expected breakpoint on line 0 after F9")
	}

	// F10 step over
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF10}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "Step Over") {
		t.Errorf("expected Step Over status, got %q", m.statusMessage)
	}

	// F11 step in
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF11}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "Step In") {
		t.Errorf("expected Step In status, got %q", m.statusMessage)
	}

	// F12 goto definition
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF12}})
	m = updated.(*AppModel)
	if !strings.Contains(m.statusMessage, "definition") {
		t.Errorf("expected Definition status, got %q", m.statusMessage)
	}

	// Shift+K hover documentation
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'K', Mod: input.ModShift}})
	m = updated.(*AppModel)
	if !m.popupVisible {
		t.Fatalf("expected hover tooltip popup to be visible after Shift+K")
	}
}

func TestAppModel_ProjectPanelAndScaffolding(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Ctrl+O opens project modal
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'o', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if !m.omnibarOpen || m.omnibarMode != "project" {
		t.Fatalf("expected Ctrl+O to open project omnibar modal, got %v (%s)", m.omnibarOpen, m.omnibarMode)
	}

	// Verify project options exist
	if len(m.omnibarItems) < 5 {
		t.Fatalf("expected project options (open, new go, new py, new rust, new blank), got: %v", m.omnibarItems)
	}

	// Test scaffolding a new Go project in temp directory
	tmpDir, err := os.MkdirTemp("", "tahr-test-proj-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	projPath := filepath.Join(tmpDir, "my-go-app")
	err = m.CreateProject("go", tmpDir, "my-go-app")
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	mainGo := filepath.Join(projPath, "main.go")
	if _, err := os.Stat(mainGo); os.IsNotExist(err) {
		t.Fatalf("expected main.go to be created at %s", mainGo)
	}

	// Open the project
	err = m.OpenProject(projPath)
	if err != nil {
		t.Fatalf("OpenProject failed: %v", err)
	}
	if m.workspaceDir != projPath {
		t.Fatalf("expected workspaceDir to be %s, got %s", projPath, m.workspaceDir)
	}
	if !m.sidebarOpen {
		t.Fatalf("expected sidebar to be open after OpenProject")
	}
	if len(m.treeFlat) == 0 {
		t.Fatalf("expected non-empty file tree for opened project")
	}
}

func TestAppModel_TabsAndHitboxes(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Open two documents
	doc1, _ := eng.Open("file1.go")
	doc2, _ := eng.Open("file2.go")
	if doc1 == nil || doc2 == nil {
		t.Fatalf("failed to open test documents")
	}

	frameBuf := buffer.NewBuffer(80, 24)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	if len(m.tabHitboxes) < 2 {
		t.Fatalf("expected at least 2 tab hitboxes, got %d", len(m.tabHitboxes))
	}

	// Click on the second tab to switch to it
	hit2 := m.tabHitboxes[1]
	updated, _ := m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      hit2.minX + 1,
			Y:      1,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if eng.ActiveDocument().ID != hit2.docID {
		t.Errorf("expected active document to be %s, got %s", hit2.docID, eng.ActiveDocument().ID)
	}

	// Click close button on the first tab
	hit1 := m.tabHitboxes[0]
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      hit1.closeX,
			Y:      1,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	// doc1 should be closed
	docsAfter := eng.Documents()
	for _, d := range docsAfter {
		if d.ID == hit1.docID {
			t.Errorf("expected doc %s to be closed", hit1.docID)
		}
	}
}

func TestAppModel_SettingsModal(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Press Ctrl+,
	updated, _ := m.Update(tea.KeyMsg{
		Key: input.Key{
			Type: input.KeyRune,
			Rune: ',',
			Mod:  input.ModCtrl,
		},
	})
	m = updated.(*AppModel)
	if m.settings == nil || !m.settings.Open {
		t.Fatalf("expected settings modal to open on Ctrl+,")
	}

	// Navigate categories with Down arrow
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyDown}})
	m = updated.(*AppModel)
	if m.settings.CategoryIdx != 1 {
		t.Errorf("expected category idx 1, got %d", m.settings.CategoryIdx)
	}

	// Switch focus to right form pane
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyTab}})
	m = updated.(*AppModel)
	if !m.settings.FocusRight {
		t.Errorf("expected focus on right form pane after Tab")
	}

	// Close settings modal with Esc
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)
	if m.settings.Open {
		t.Errorf("expected settings modal to close on Esc")
	}
}

func TestAppModel_GhostTextAndTabAccept(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Type "fmt.Pr"
	for _, r := range "fmt.Pr" {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}

	m.updateGhostText()
	if m.ghostText == "" {
		t.Fatalf("expected ghost text prediction for prefix 'Pr', got empty")
	}
	if !strings.HasPrefix("Println", m.ghostPrefix+m.ghostText) && !strings.HasPrefix("Printf", m.ghostPrefix+m.ghostText) {
		t.Errorf("unexpected ghost text %q with prefix %q", m.ghostText, m.ghostPrefix)
	}

	// Press Tab to accept ghost text
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyTab}})
	m = updated.(*AppModel)

	text, _ := doc.Buffer.GetText()
	textStr := string(text)
	if !strings.Contains(textStr, "fmt.Print") {
		t.Fatalf("expected buffer to contain completed text, got %q", textStr)
	}
	if m.ghostText != "" {
		t.Errorf("expected ghost text to be cleared after Tab accept")
	}
}

func TestAppModel_AutocompletionPrefixReplacement(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Type "fmt.Prin"
	for _, r := range "fmt.Prin" {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}

	// Insert completion "fmt.Println"
	m.insertCompletion("fmt.Println")

	text, _ := doc.Buffer.GetText()
	textStr := string(text)
	// Must NOT contain duplicate "PrinPrintln"
	if strings.Contains(textStr, "PrinPrintln") {
		t.Fatalf("autocompletion caused duplicate text: %q", textStr)
	}
	if textStr != "fmt.Println" {
		t.Fatalf("expected 'fmt.Println', got %q", textStr)
	}
}

func TestAppModel_TreeFileCRUDAndPrompts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-crud-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial file
	initFile := filepath.Join(tmpDir, "init.txt")
	_ = os.WriteFile(initFile, []byte("hello"), 0644)

	eng := core.NewEngine()
	m := NewAppModel(eng)
	_ = m.OpenProject(tmpDir)

	m.sidebarFocused = true

	// 1. Create file via prompt: press 'a'
	updated, _ := m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a'}})
	m = updated.(*AppModel)
	if !m.treePromptOpen || m.treePromptMode != "new_file" {
		t.Fatalf("expected treePromptOpen for new_file, got open=%v mode=%s", m.treePromptOpen, m.treePromptMode)
	}
	// Type "newfile.go" and press Enter
	for _, r := range "newfile.go" {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	newFilePath := filepath.Join(tmpDir, "newfile.go")
	if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
		t.Fatalf("expected newfile.go to be created at %s", newFilePath)
	}

	// 2. Rename file via prompt: select new file, press 'r'
	m.refreshProjectTree()
	for i, node := range m.treeFlat {
		if node.Name == "newfile.go" {
			m.treeSel = i
			break
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'r'}})
	m = updated.(*AppModel)
	if !m.treePromptOpen || m.treePromptMode != "rename" {
		t.Fatalf("expected treePromptOpen for rename, got open=%v mode=%s", m.treePromptOpen, m.treePromptMode)
	}
	// Clear name and type "renamed.go"
	m.treePromptText = "renamed.go"
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	renamedPath := filepath.Join(tmpDir, "renamed.go")
	if _, err := os.Stat(renamedPath); os.IsNotExist(err) {
		t.Fatalf("expected renamed.go to exist at %s", renamedPath)
	}

	// 3. Delete file via prompt: select renamed file, press 'd'
	m.refreshProjectTree()
	for i, node := range m.treeFlat {
		if node.Name == "renamed.go" {
			m.treeSel = i
			break
		}
	}
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'd'}})
	m = updated.(*AppModel)
	if !m.treePromptOpen || m.treePromptMode != "delete" {
		t.Fatalf("expected treePromptOpen for delete, got open=%v mode=%s", m.treePromptOpen, m.treePromptMode)
	}
	// Press Enter to confirm deletion
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	m = updated.(*AppModel)

	if _, err := os.Stat(renamedPath); !os.IsNotExist(err) {
		t.Fatalf("expected renamed.go to be deleted")
	}
}

func TestAppModel_RightClickContextMenu(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr_ctx_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	file1 := filepath.Join(tmpDir, "file1.txt")
	_ = os.WriteFile(file1, []byte("content"), 0644)

	eng := core.NewEngine()
	m := NewAppModel(eng)
	m.SetWorkspaceDir(tmpDir)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetSidebarOpen(true)

	// 1. Right click on file1.txt in project tree
	// Left docked tree: stripLeft=3, sideW=26 -> treeStartX = 3
	// row 0 is header "PROJECT EXPLORER", row 1 is root dir, row 2 is file1.txt -> screenY = 2 + 1 + 1 = 4
	updated, _ := m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      10,
			Y:      4,
			Button: input.MouseRight,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.contextMenuOpen {
		t.Fatalf("expected context menu to be open on right click")
	}
	if len(m.contextMenuItems) != 6 {
		t.Fatalf("expected 6 actions for file node, got %d", len(m.contextMenuItems))
	}
	if m.contextMenuItems[4].action != "copy_path" {
		t.Fatalf("expected 5th action to be copy_path, got %s", m.contextMenuItems[4].action)
	}

	// 2. Click "Copy Relative Path" (row index 4 -> screen Y = contextMenuY + 1 + 4)
	clickY := m.contextMenuY + 1 + 4
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      m.contextMenuX + 2,
			Y:      clickY,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if m.contextMenuOpen {
		t.Fatalf("expected context menu to close after action click")
	}
	if m.clipboardText != "file1.txt" {
		t.Fatalf("expected clipboardText to be 'file1.txt', got %q", m.clipboardText)
	}

	// 3. Right click on empty tree space (below files) -> 3 actions
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      10,
			Y:      10,
			Button: input.MouseRight,
			Action: input.MousePress,
		},
	})
	m = updated.(*AppModel)
	if !m.contextMenuOpen {
		t.Fatalf("expected context menu to open on empty space right click")
	}
	if len(m.contextMenuItems) != 3 {
		t.Fatalf("expected 3 actions for empty space, got %d", len(m.contextMenuItems))
	}
	if m.contextMenuItems[2].action != "refresh" {
		t.Fatalf("expected 3rd action to be refresh, got %s", m.contextMenuItems[2].action)
	}

	// 4. Test keyboard Esc dismisses context menu
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	m = updated.(*AppModel)
	if m.contextMenuOpen {
		t.Fatalf("expected Esc to close context menu")
	}
}

func TestAppModel_HoverTooltips(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"

	// Render view to populate hitboxes and buttons
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// 1. Mouse motion over "menu" toolbar button
	updated, _ := m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      1,
			Y:      0,
			Action: input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if !strings.Contains(m.tooltipText, "Main Menu") {
		t.Fatalf("expected tooltip to contain 'Main Menu', got %q", m.tooltipText)
	}

	// 2. Mouse motion over Activity Bar Project Explorer icon (1, 2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      1,
			Y:      2,
			Action: input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if !strings.Contains(m.tooltipText, "Project Explorer") {
		t.Fatalf("expected tooltip to contain 'Project Explorer', got %q", m.tooltipText)
	}

	// 3. Mouse motion over non-interactive area clears tooltip
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      50,
			Y:      15,
			Action: input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if m.tooltipText != "" {
		t.Fatalf("expected tooltip to be cleared, got %q", m.tooltipText)
	}
}

func TestAppModel_120FPSAnimationTicks(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.settings.Current.SmoothAnim = true

	m.sidebarOpen = false
	m.sidebarAnimWidth = 0

	// Toggle sidebar -> triggers animation cmd
	cmd := m.ToggleSidebar()
	if !m.sidebarOpen {
		t.Fatalf("expected sidebar to be open")
	}
	if cmd == nil {
		t.Fatalf("expected animation ticker cmd to be scheduled")
	}

	// Simulate receiving animation frames
	for i := 0; i < 10; i++ {
		msg := animTickMsg{}
		updated, nextCmd := m.Update(msg)
		m = updated.(*AppModel)
		if m.sidebarAnimWidth >= 26.0 {
			break
		}
		if nextCmd == nil {
			break
		}
	}

	if m.sidebarAnimWidth != 26.0 {
		t.Fatalf("expected sidebarAnimWidth to reach target 26.0, got %f", m.sidebarAnimWidth)
	}
}

func TestAppModel_TreeDockPositioning(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetSidebarOpen(true)

	// 1. Left Dock (default): treeStartX = 3; divider = 3 + 26 = 29
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	cellDivLeft := frameBuf.Cell(29, 2)
	if cellDivLeft == nil || cellDivLeft.Rune != '│' {
		t.Fatalf("expected '│' divider at col 29 for left dock, got %v", cellDivLeft)
	}

	// 2. Right Dock: treeStartX = 100 - 3 - 26 = 71; divider = 70
	m.toasts = ui.NewToastManager(4)
	m.settings.Current.TreePosition = "right"
	frameBufRight := buffer.NewBuffer(100, 30)
	frameRight := &tea.Frame{Buffer: frameBufRight}
	m.View(frameRight)

	cellDivRight := frameBufRight.Cell(70, 2)
	if cellDivRight == nil || cellDivRight.Rune != '│' {
		t.Fatalf("expected '│' divider at col 70 for right dock, got %v", cellDivRight)
	}
}

func TestAppModel_SettingsInlineEditing(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open settings
	m.settings.Open = true
	m.settings.CategoryIdx = 5 // Toolchains & SDKs
	m.settings.FieldIdx = 0    // Go SDK Path
	m.settings.FocusRight = true

	// Press Enter to start inline editing
	handled, _ := m.settings.HandleKey(input.Key{Type: input.KeyEnter})
	if !handled || !m.settings.EditingText {
		t.Fatalf("expected to enter editing text mode")
	}

	// Type path characters
	for _, r := range "/custom/go/sdk" {
		m.settings.HandleKey(input.Key{Type: input.KeyRune, Rune: r})
	}

	// Press Enter to commit
	m.settings.HandleKey(input.Key{Type: input.KeyEnter})
	if m.settings.EditingText {
		t.Fatalf("expected to exit editing text mode after Enter")
	}
	if m.settings.Current.GoSDKPath != "/custom/go/sdk" {
		t.Fatalf("expected GoSDKPath to be updated to '/custom/go/sdk', got %q", m.settings.Current.GoSDKPath)
	}
}

func TestAppModel_TreeDockToggle_OnTheFly(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetSidebarOpen(true)

	if m.settings.Current.TreePosition != "left" {
		t.Fatalf("expected default left tree position, got %s", m.settings.Current.TreePosition)
	}

	// Hotkey Ctrl+Alt+E toggles to right
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'e',
		Mod:  input.ModCtrl | input.ModAlt,
	}})
	if m.settings.Current.TreePosition != "right" {
		t.Fatalf("expected tree position right after Ctrl+Alt+E, got %s", m.settings.Current.TreePosition)
	}

	// Pressing again toggles back to left
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'e',
		Mod:  input.ModCtrl | input.ModAlt,
	}})
	if m.settings.Current.TreePosition != "left" {
		t.Fatalf("expected tree position left after second Ctrl+Alt+E, got %s", m.settings.Current.TreePosition)
	}

	// Click on [⇄] button in tree header (when left docked: treeStartX = 3; x=3+26-3=26, y=2)
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	treeStartX := 3
	dockBtnX := treeStartX + m.sidebarWidth - 1
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      dockBtnX,
			Y:      2,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	if m.settings.Current.TreePosition != "right" {
		t.Fatalf("expected tree position right after clicking dock button, got %s", m.settings.Current.TreePosition)
	}
}

func TestAppModel_MultiSplit_1to6Panes(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if m.splits.TotalPanes() != 1 {
		t.Fatalf("expected 1 pane initially, got %d", m.splits.TotalPanes())
	}

	// Cycle through all 1 to 6 split modes using Ctrl+\
	expectedPanes := []int{2, 2, 3, 4, 5, 6, 1}
	for _, expected := range expectedPanes {
		_, _ = m.Update(tea.KeyMsg{Key: input.Key{
			Type: input.KeyRune,
			Rune: '\\',
			Mod:  input.ModCtrl,
		}})
		if m.splits.TotalPanes() != expected {
			t.Fatalf("expected %d panes after cycle, got %d", expected, m.splits.TotalPanes())
		}
	}

	// Set to 4 Grid (2x2) and test Alt+1 .. Alt+4 navigation
	m.splits.SetLayout(Split4Grid)
	if m.splits.TotalPanes() != 4 {
		t.Fatalf("expected 4 panes, got %d", m.splits.TotalPanes())
	}

	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: '3',
		Mod:  input.ModAlt,
	}})
	if m.splits.ActiveIndex != 2 {
		t.Fatalf("expected active index 2 after Alt+3, got %d", m.splits.ActiveIndex)
	}

	// Alt+Right advances to pane 4 (index 3)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRight,
		Mod:  input.ModAlt,
	}})
	if m.splits.ActiveIndex != 3 {
		t.Fatalf("expected active index 3 after Alt+Right, got %d", m.splits.ActiveIndex)
	}

	// Alt+Left moves back to pane 3 (index 2)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyLeft,
		Mod:  input.ModAlt,
	}})
	if m.splits.ActiveIndex != 2 {
		t.Fatalf("expected active index 2 after Alt+Left, got %d", m.splits.ActiveIndex)
	}
}

func TestAppModel_CleanGlyphsAndHover(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Check tree file node icon does NOT have "go " prefix
	nodeGo := &FileNode{Name: "main.go", Path: "main.go", IsDir: false}
	iconGo := GetFileIcon(nodeGo)
	if strings.Contains(iconGo, "go") {
		t.Fatalf("expected clean glyph for Go file without 'go' prefix, got %q", iconGo)
	}

	// Render view to populate toolbar hitboxes
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	// Test hover without click
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      7,
			Y:      0,
			Action: input.MouseMotion,
		},
	})
	if m.tooltipText == "" {
		t.Fatalf("expected tooltip on hover over tree button, got empty")
	}

	// Test hover via MouseDrag + MouseNone
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      15,
			Y:      0,
			Button: input.MouseNone,
			Action: input.MouseDrag,
		},
	})
	if m.tooltipText == "" {
		t.Fatalf("expected tooltip on hover via drag-none over split button, got empty")
	}
}

func TestAppModel_Phase5_Terminal_Git_Minimap_Breadcrumbs_DAPHUD(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	doc := eng.ActiveDocument()
	doc.FilePath = "main.go"
	code := "package main\n\ntype Engine struct {}\n\nfunc (e *Engine) Start() {\n\tprintln(\"run\")\n}\n"
	_, _ = m.Update(tea.PasteMsg{Text: code})

	// 1. Test Terminal Drawer Toggle (F4 and click)
	if m.terminal.Open {
		t.Fatal("expected terminal to start closed")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if !m.terminal.Open {
		t.Fatal("expected terminal to open on F4")
	}
	// Type into terminal
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'l'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 's'}})
	if m.terminal.InputText != "ls" {
		t.Fatalf("expected terminal input 'ls', got %q", m.terminal.InputText)
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if m.terminal.Open {
		t.Fatal("expected terminal to close on second F4")
	}

	// 2. Test DAP Debugger HUD (F8 and Shift+F5)
	if m.dapHUD.Open {
		t.Fatal("expected DAP HUD to start closed")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF8}})
	if !m.dapHUD.Open {
		t.Fatal("expected DAP HUD to open on F8")
	}

	// Render view to populate buttons and minimap
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	if len(m.dapHUD.Buttons) == 0 {
		t.Fatal("expected DAP HUD buttons to be rendered")
	}

	// Test click on dap_stop button
	for _, b := range m.dapHUD.Buttons {
		if b.ID == "dap_stop" {
			_, _ = m.Update(tea.MouseMsg{
				Mouse: input.Mouse{
					X:      b.MinX + 1,
					Y:      b.Y,
					Button: input.MouseLeft,
					Action: input.MousePress,
				},
			})
			break
		}
	}
	if m.dapHUD.Open {
		t.Fatal("expected DAP HUD to close on stop button click")
	}

	// 3. Test Minimap hitboxes & click-to-scroll
	m.View(frame)
	if len(m.minimapHitboxes) == 0 {
		t.Fatal("expected minimap hitboxes when width >= 40")
	}
	mm := m.minimapHitboxes[0]
	// Click near bottom of minimap
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      mm.minX + 1,
			Y:      mm.maxY,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})

	// 4. Test Breadcrumbs on Row 1 (Tab Bar right side)
	m.View(frame)
	foundBreadcrumbs := false
	for x := 0; x < 100; x++ {
		cell := frameBuf.Cell(x, 1)
		if cell != nil && (cell.Rune == '›' || cell.Rune == '⚡' || cell.Rune == '🔷') {
			foundBreadcrumbs = true
			break
		}
	}
	if !foundBreadcrumbs {
		t.Errorf("expected breadcrumbs glyph on row 1")
	}

	// 5. Test Omnibar command execution for terminal and minimap
	m.executeOmnibarCommand("View: Toggle Integrated Terminal")
	if !m.terminal.Open {
		t.Errorf("expected terminal open via omnibar")
	}
	m.executeOmnibarCommand("View: Toggle Integrated Terminal")
	if m.terminal.Open {
		t.Errorf("expected terminal closed via omnibar")
	}

	m.executeOmnibarCommand("View: Toggle Code Minimap")
	if m.settings.Current.ShowMinimap {
		t.Errorf("expected minimap toggled off")
	}
}

func TestAppModel_Phase6_Focus_Undo_Minimap_Marketplace_Colors(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Terminal Open vs Editor Undo (Ctrl+Z)
	// Type into active editor
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'h'}})
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'i'}})

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "hi" {
		t.Fatalf("expected 'hi' in buffer, got %q", string(txt))
	}

	// Open terminal drawer via F4
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	if !m.terminal.Open {
		t.Fatal("expected terminal drawer to be open")
	}
	if !m.terminalFocused {
		t.Fatal("expected terminal to be focused initially on F4")
	}

	// Click in editor area to switch focus to editor
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      20,
			Y:      5,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	})
	if m.terminalFocused {
		t.Fatal("expected terminalFocused to be false after clicking in editor")
	}
	if !m.terminal.Open {
		t.Fatal("expected terminal to remain visible at bottom")
	}

	// Press Ctrl+Z (Undo) in editor
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'z',
		Mod:  input.ModCtrl,
	}})
	txtAfterUndo, _ := doc.Buffer.GetText()
	if string(txtAfterUndo) == "hi" {
		t.Errorf("expected Ctrl+Z to undo editor text while terminal drawer is open, but text was %q", string(txtAfterUndo))
	}

	// 2. Test Alt+M Minimap Toggle
	initialMinimapState := m.settings.Current.ShowMinimap
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'm',
		Mod:  input.ModAlt,
	}})
	if m.settings.Current.ShowMinimap == initialMinimapState {
		t.Errorf("expected Alt+M to flip ShowMinimap state")
	}
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'm',
		Mod:  input.ModAlt,
	}})
	if m.settings.Current.ShowMinimap != initialMinimapState {
		t.Errorf("expected second Alt+M to restore ShowMinimap state")
	}

	// 3. Test Fast Scrolling (Mouse Wheel & Keyboard)
	// Add lines to document so scrolling is possible
	for i := 0; i < 50; i++ {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertNewline})
	}
	initialVpY := m.viewportY
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyDown,
		Mod:  input.ModCtrl,
	}})
	if m.viewportY != initialVpY+5 {
		t.Errorf("expected Ctrl+Down to scroll 5 lines, got vpY=%d", m.viewportY)
	}

	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyUp,
		Mod:  input.ModCtrl,
	}})
	if m.viewportY != initialVpY {
		t.Errorf("expected Ctrl+Up to scroll back 5 lines, got vpY=%d", m.viewportY)
	}

	// Mouse wheel down acceleration
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      20,
			Y:      5,
			Button: input.MouseWheelDown,
		},
	})
	if m.viewportY < 5 {
		t.Errorf("expected mouse wheel down to scroll at least 5 lines, got %d", m.viewportY)
	}

	// 4. Test Marketplace Modal (Ctrl+Shift+X)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: 'x',
		Mod:  input.ModCtrl | input.ModShift,
	}})
	if !m.marketplace.Open {
		t.Fatal("expected marketplace modal to open on Ctrl+Shift+X")
	}

	// Switch to Repositories tab in Marketplace (press '3')
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type: input.KeyRune,
		Rune: '3',
	}})
	if m.marketplace.ActiveTab != 2 {
		t.Errorf("expected active tab 2 (Repositories), got %d", m.marketplace.ActiveTab)
	}

	// Close marketplace on Esc
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	if m.marketplace.Open {
		t.Fatal("expected marketplace modal to close on Esc")
	}

	// 5. Test Settings Swatches & Color Picker
	m.settings.Open = true
	m.settings.CategoryIdx = 4 // Color Palette
	m.settings.FocusRight = true
	m.settings.FieldIdx = 0

	// Press Enter to open Color Picker
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	if m.settings.ColorPicker == nil || !m.settings.ColorPicker.Open {
		t.Fatal("expected ColorPicker modal to open on Enter in Color Palette category")
	}

	// Move wheel cursor right
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRight}})
	// Apply color on Enter
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	if m.settings.ColorPicker.Open {
		t.Fatal("expected ColorPicker to close on Enter after applying color")
	}
}

func TestAppModel_UndoRedo_English_CtrlZ_CtrlY(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Send 'a', 'b'
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'a'}})
	m := updated.(*AppModel)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'b'}})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "ab" {
		t.Fatalf("expected 'ab', got %q", string(txt))
	}

	// Send Ctrl+Z
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer after Ctrl+Z, got %q", string(txt))
	}
	if m.statusMessage != "Undo" {
		t.Fatalf("expected statusMessage 'Undo', got %q", m.statusMessage)
	}

	// Send Ctrl+Y
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "ab" {
		t.Fatalf("expected 'ab' after Ctrl+Y, got %q", string(txt))
	}
	if m.statusMessage != "Redo" {
		t.Fatalf("expected statusMessage 'Redo', got %q", m.statusMessage)
	}
}

func TestAppModel_UndoRedo_RussianLayout_CtrlЯ_CtrlShiftЯ_CtrlН(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Send 'x'
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'x'}})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "x" {
		t.Fatalf("expected 'x', got %q", string(txt))
	}

	// Send Ctrl+я (0x44F)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer after Ctrl+я, got %q", string(txt))
	}
	if m.statusMessage != "Undo" {
		t.Fatalf("expected statusMessage 'Undo', got %q", m.statusMessage)
	}

	// Send Ctrl+Shift+я (0x42F with ModCtrl | ModShift) -> Redo
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 0x42f, Mod: input.ModCtrl | input.ModShift}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "x" {
		t.Fatalf("expected 'x' after Ctrl+Shift+я, got %q", string(txt))
	}
	if m.statusMessage != "Redo" {
		t.Fatalf("expected statusMessage 'Redo', got %q", m.statusMessage)
	}

	// Send Ctrl+я again to undo
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer after second Ctrl+я, got %q", string(txt))
	}

	// Send Ctrl+н (0x43D) -> Redo
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 0x43d, Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "x" {
		t.Fatalf("expected 'x' after Ctrl+н, got %q", string(txt))
	}
	if m.statusMessage != "Redo" {
		t.Fatalf("expected statusMessage 'Redo', got %q", m.statusMessage)
	}
}

func TestAppModel_Undo_LegacyControlByte_0x1A(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Send text "hello"
	updated, _ := model.Update(tea.PasteMsg{Text: "hello"})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(txt))
	}

	// Send raw byte 0x1A (Rune 26)
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 26}})
	m = updated.(*AppModel)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected empty buffer after 0x1A control byte, got %q", string(txt))
	}
	if m.statusMessage != "Undo" {
		t.Fatalf("expected statusMessage 'Undo', got %q", m.statusMessage)
	}
}

func TestAppModel_TerminalDrawer_UndoPassthroughWhenNotActivelyTyping(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Insert text in editor buffer
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'w'}})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "w" {
		t.Fatalf("expected 'w', got %q", string(txt))
	}

	// Press F4 to open terminal drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	if !m.terminal.Open {
		t.Fatal("expected terminal drawer to be open")
	}
	if !m.terminalFocused {
		t.Fatal("expected terminal to be focused initially on F4")
	}
	if m.terminal.IsActivelyTyping() {
		t.Fatal("expected terminal not to be actively typing initially")
	}

	// Send Ctrl+Z without typing anything in terminal drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	txt, _ = doc.Buffer.GetText()
	if string(txt) != "" {
		t.Fatalf("expected Ctrl+Z to undo editor text when terminal is not actively typing, got %q", string(txt))
	}
	if m.statusMessage != "Undo" {
		t.Fatalf("expected statusMessage 'Undo', got %q", m.statusMessage)
	}
}

func TestAppModel_TerminalDrawer_UndoConsumedWhenActivelyTyping(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Insert text in editor buffer
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'q'}})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "q" {
		t.Fatalf("expected 'q', got %q", string(txt))
	}

	// Open terminal drawer
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
	m = updated.(*AppModel)

	// Type into terminal drawer
	m.terminal.mu.Lock()
	m.terminal.InputText = "ls -la"
	m.terminal.CursorPos = 6
	m.terminal.mu.Unlock()

	if !m.terminal.IsActivelyTyping() {
		t.Fatal("expected terminal to be actively typing")
	}

	// Send Ctrl+Z
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Terminal input must be cleared
	if m.terminal.IsActivelyTyping() {
		t.Fatalf("expected terminal input to be cleared, got %q", m.terminal.InputText)
	}

	// Editor buffer must be untouched ("q" must remain)
	txt, _ = doc.Buffer.GetText()
	if string(txt) != "q" {
		t.Fatalf("expected editor buffer to remain untouched at 'q', got %q", string(txt))
	}
}

func TestAppModel_UndoRedo_ViewportOffsetsSynchronized(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Resize to 80x24
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m := updated.(*AppModel)

	// Populate buffer with 200 lines
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line of content")
	}
	bigText := strings.Join(lines, "\n")
	updated, _ = m.Update(tea.PasteMsg{Text: bigText})
	m = updated.(*AppModel)

	// Move cursor to line 5 and insert edit
	doc := m.eng.ActiveDocument()
	offset, _ := doc.Buffer.ByteOffsetForLine(5)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 5, Column: 0, Byte: offset}, Head: cbuf.Position{Line: 5, Column: 0, Byte: offset}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '!'}})
	m = updated.(*AppModel)

	// Simulate viewport scrolled down to line 150
	m.viewportY = 150
	ap := m.splits.ActivePane()
	if ap != nil {
		ap.ViewportY = 150
		ap.TargetViewportY = 150
		ap.SmoothScrollY = 150
	}

	// Send Ctrl+Z
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Viewport must snap to cursor visibility (around line 5)
	if m.viewportY > 10 {
		t.Fatalf("expected viewportY to snap near cursor (<= 10), got %d", m.viewportY)
	}

	ap = m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}
	if ap.TargetViewportY != float64(m.viewportY) {
		t.Fatalf("expected TargetViewportY == %f, got %f", float64(m.viewportY), ap.TargetViewportY)
	}
	if ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("expected SmoothScrollY == %f, got %f", float64(m.viewportY), ap.SmoothScrollY)
	}
	if ap.ScrollVelocity != 0 {
		t.Fatalf("expected ScrollVelocity == 0, got %f", ap.ScrollVelocity)
	}

	// Trigger animation tick: stepSmoothScroll must not drift or fight cursor
	updated, _ = m.Update(animTickMsg{})
	m = updated.(*AppModel)
	if m.viewportY > 10 {
		t.Fatalf("expected viewportY to remain near cursor after animTick, got %d", m.viewportY)
	}
}

func TestAppModel_UndoRedo_BoundaryWarnings(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// On clean empty document, send Ctrl+Z
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m := updated.(*AppModel)
	if m.statusMessage != "Already at oldest change" {
		t.Fatalf("expected 'Already at oldest change', got %q", m.statusMessage)
	}

	// Send Ctrl+Shift+Z or Ctrl+Y
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)
	if m.statusMessage != "Already at newest change" {
		t.Fatalf("expected 'Already at newest change', got %q", m.statusMessage)
	}
}

func TestAppModel_MouseClickTabs(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Set window size
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Paste text with 2 tabs followed by 'hello'
	updated, _ = m.Update(tea.PasteMsg{Text: "\t\thello\n"})
	m = updated.(*AppModel)

	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)

	gw := m.gutterWidth()
	// StripLeftW is 3 when width >= 70, editorTop is 2 (row 0 menu, row 1 breadcrumbs).
	// Default tab size is 4, so 2 tabs take visual columns 0..7.
	// Visual col 8 is 'h' (rune index 2).
	// Click event at visual column 8 in text area: screen X = 3 + gw + 8, screen Y = 2
	clickMsg := tea.MouseMsg{
		Mouse: input.Mouse{
			X:      3 + gw + 8,
			Y:      2,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	}
	updated, _ = m.Update(clickMsg)
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatal("expected selection after click")
	}
	head := sels[0].Head
	if head.Line != 0 {
		t.Errorf("expected line 0, got %d", head.Line)
	}
	// Rune col should be 2 (the two tabs \t\t are runes 0 and 1, 'h' is rune 2)
	if head.Column != 2 {
		t.Errorf("expected rune column 2, got %d", head.Column)
	}
}

func TestAppModel_AutocompletePrefixStripping(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Case 1: Cursor right after "fmt."
	updated, _ := model.Update(tea.PasteMsg{Text: "fmt."})
	m := updated.(*AppModel)

	m.insertCompletion("fmt.Println - func(a ...any)")
	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}
	lineBytes, _ := doc.Buffer.GetLine(0)
	lineStr := string(lineBytes)
	if lineStr != "fmt.Println" {
		t.Errorf("expected 'fmt.Println', got %q", lineStr)
	}

	// Case 2: Cursor after "fmt.Pr"
	eng2 := core.NewEngine()
	model2 := NewAppModel(eng2)
	updated2, _ := model2.Update(tea.PasteMsg{Text: "fmt.Pr"})
	m2 := updated2.(*AppModel)

	m2.insertCompletion("fmt.Println - func(a ...any)")
	doc2 := m2.eng.ActiveDocument()
	if doc2 == nil {
		t.Fatal("expected active document")
	}
	lineBytes2, _ := doc2.Buffer.GetLine(0)
	lineStr2 := string(lineBytes2)
	if lineStr2 != "fmt.Println" {
		t.Errorf("expected 'fmt.Println', got %q", lineStr2)
	}
}

func TestAppModel_TreeGitBadges(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Ensure sidebar is open
	m.sidebarAnimWidth = 30
	m.sidebarMode = 0 // project explorer

	// Populate treeFlat with nodes
	m.treeFlat = []*FileNode{
		{Name: "modified.go", Path: filepath.Join(m.workspaceDir, "modified.go"), Depth: 0},
		{Name: "untracked.go", Path: filepath.Join(m.workspaceDir, "untracked.go"), Depth: 0},
		{Name: "added.go", Path: filepath.Join(m.workspaceDir, "added.go"), Depth: 0},
	}

	if m.gitTracker == nil {
		m.gitTracker = git.NewTracker()
	}

	// Render into frame
	frameBuf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: frameBuf}
	m.View(frame)
}



