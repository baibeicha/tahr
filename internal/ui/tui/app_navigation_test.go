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

	"tahr/internal/core/git"
)

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
			X:	hit2.minX + 1,
			Y:	1,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
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
			X:	hit1.closeX,
			Y:	1,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
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
			X:	10,
			Y:	4,
			Button:	input.MouseRight,
			Action:	input.MousePress,
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
			X:	m.contextMenuX + 2,
			Y:	clickY,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
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
			X:	10,
			Y:	10,
			Button:	input.MouseRight,
			Action:	input.MousePress,
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
			X:	1,
			Y:	0,
			Action:	input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if !strings.Contains(m.tooltipText, "Main Menu") {
		t.Fatalf("expected tooltip to contain 'Main Menu', got %q", m.tooltipText)
	}

	// 2. Mouse motion over Activity Bar Project Explorer icon (1, 2)
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	1,
			Y:	2,
			Action:	input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if !strings.Contains(m.tooltipText, "Project Explorer") {
		t.Fatalf("expected tooltip to contain 'Project Explorer', got %q", m.tooltipText)
	}

	// 3. Mouse motion over non-interactive area clears tooltip
	updated, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	50,
			Y:	15,
			Action:	input.MouseMotion,
		},
	})
	m = updated.(*AppModel)
	if m.tooltipText != "" {
		t.Fatalf("expected tooltip to be cleared, got %q", m.tooltipText)
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
		Type:	input.KeyRune,
		Rune:	'e',
		Mod:	input.ModCtrl | input.ModAlt,
	}})
	if m.settings.Current.TreePosition != "right" {
		t.Fatalf("expected tree position right after Ctrl+Alt+E, got %s", m.settings.Current.TreePosition)
	}

	// Pressing again toggles back to left
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRune,
		Rune:	'e',
		Mod:	input.ModCtrl | input.ModAlt,
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
			X:	dockBtnX,
			Y:	2,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
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
			Type:	input.KeyRune,
			Rune:	'\\',
			Mod:	input.ModCtrl,
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
		Type:	input.KeyRune,
		Rune:	'3',
		Mod:	input.ModAlt,
	}})
	if m.splits.ActiveIndex != 2 {
		t.Fatalf("expected active index 2 after Alt+3, got %d", m.splits.ActiveIndex)
	}

	// Alt+Right advances to pane 4 (index 3)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyRight,
		Mod:	input.ModAlt,
	}})
	if m.splits.ActiveIndex != 3 {
		t.Fatalf("expected active index 3 after Alt+Right, got %d", m.splits.ActiveIndex)
	}

	// Alt+Left moves back to pane 3 (index 2)
	_, _ = m.Update(tea.KeyMsg{Key: input.Key{
		Type:	input.KeyLeft,
		Mod:	input.ModAlt,
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
			X:	7,
			Y:	0,
			Action:	input.MouseMotion,
		},
	})
	if m.tooltipText == "" {
		t.Fatalf("expected tooltip on hover over tree button, got empty")
	}

	// Test hover via MouseDrag + MouseNone
	_, _ = m.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:	15,
			Y:	0,
			Button:	input.MouseNone,
			Action:	input.MouseDrag,
		},
	})
	if m.tooltipText == "" {
		t.Fatalf("expected tooltip on hover via drag-none over split button, got empty")
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
			X:	3 + gw + 8,
			Y:	2,
			Button:	input.MouseLeft,
			Action:	input.MousePress,
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

func TestAppModel_TreeGitBadges(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Ensure sidebar is open
	m.sidebarAnimWidth = 30
	m.sidebarMode = 0	// project explorer

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
