package tui

import (
	"os"

	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"

	"tahr/internal/core"
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

func TestAppModel_SettingsModal(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)

	// Press Ctrl+,
	updated, _ := m.Update(tea.KeyMsg{
		Key: input.Key{
			Type:	input.KeyRune,
			Rune:	',',
			Mod:	input.ModCtrl,
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

func TestAppModel_SettingsInlineEditing(t *testing.T) {
	eng := core.NewEngine()
	m := NewAppModel(eng)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open settings
	m.settings.Open = true
	m.settings.CategoryIdx = 5	// Toolchains & SDKs
	m.settings.FieldIdx = 0		// Go SDK Path
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
