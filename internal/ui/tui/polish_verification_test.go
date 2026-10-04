package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"
)

// TestDynamicKeybindingExecution verifies that changing keybindings dynamically changes hotkey routing.
func TestDynamicKeybindingExecution(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Insert text
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: "original_text"})
	txt, _ := doc.Buffer.GetText()
	if string(txt) != "original_text" {
		t.Fatalf("expected 'original_text', got %q", string(txt))
	}

	// Rebind "undo" to F3 in settings
	app.settings.Current.Keybindings["undo"] = "F3"
	app.applyCurrentSettings()

	// Pressing F3 should undo "original_text"
	f3Msg := tea.KeyMsg{Key: input.Key{Type: input.KeyF3}}
	updated, _ := app.Update(f3Msg)
	app = updated.(*AppModel)

	txtAfterF3, _ := doc.Buffer.GetText()
	if string(txtAfterF3) != "" {
		t.Fatalf("expected empty buffer after customized F3 undo, got %q", string(txtAfterF3))
	}
	if app.statusMessage != "Undo" {
		t.Fatalf("expected status 'Undo', got %q", app.statusMessage)
	}

	// Rebind "redo" to F4 in settings (replaces default terminal toggle for test)
	app.settings.Current.Keybindings["redo"] = "F4"
	app.applyCurrentSettings()

	f4Msg := tea.KeyMsg{Key: input.Key{Type: input.KeyF4}}
	updated, _ = app.Update(f4Msg)
	app = updated.(*AppModel)

	txtAfterF4, _ := doc.Buffer.GetText()
	if string(txtAfterF4) != "original_text" {
		t.Fatalf("expected 'original_text' restored after customized F4 redo, got %q", string(txtAfterF4))
	}
}

// TestRelativeAndNoneLineNumbers verifies gutter behavior under absolute, relative, and none modes.
func TestRelativeAndNoneLineNumbers(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Insert 10 lines
	lines := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10"
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: lines})

	// 1. None mode
	app.settings.Current.LineNumbers = "none"
	app.applyCurrentSettings()
	gwNone := app.gutterWidth()
	if gwNone != 3 {
		t.Errorf("expected gutter width 3 for 'none' line numbers, got %d", gwNone)
	}

	// 2. Absolute mode
	app.settings.Current.LineNumbers = "absolute"
	app.applyCurrentSettings()
	gwAbs := app.gutterWidth()
	if gwAbs <= 3 {
		t.Errorf("expected gutter width > 3 for 'absolute' line numbers, got %d", gwAbs)
	}

	// 3. Relative mode
	app.settings.Current.LineNumbers = "relative"
	app.applyCurrentSettings()
	gwRel := app.gutterWidth()
	if gwRel != gwAbs {
		t.Errorf("expected gutter width %d for 'relative', got %d", gwAbs, gwRel)
	}

	// Render frame to verify no panics and valid diff buffer
	app.width = 80
	app.height = 24
	frame := &tea.Frame{
		Buffer: buffer.NewBuffer(80, 24),
	}
	app.View(frame)
	if frame.Buffer == nil {
		t.Fatal("expected rendered frame buffer")
	}
}

// TestTabSizeDynamic verifies indentation spacing adapts dynamically to Settings.TabSize.
func TestTabSizeDynamic(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	doc := eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Set TabSize to 2 spaces with default settings
	app.settings.Current = DefaultSettings()
	app.settings.Current.TabSize = 2
	app.settings.Current.UseSpaces = true
	app.applyCurrentSettings()

	tabKey := tea.KeyMsg{Key: input.Key{Type: input.KeyTab}}
	updated, _ := app.Update(tabKey)
	app = updated.(*AppModel)

	txt, _ := doc.Buffer.GetText()
	if string(txt) != "  " {
		t.Fatalf("expected 2 spaces after Tab with TabSize=2, got %q", string(txt))
	}

	// Set TabSize to 4 spaces
	app.settings.Current.TabSize = 4
	app.applyCurrentSettings()

	updated, _ = app.Update(tabKey)
	app = updated.(*AppModel)

	txt2, _ := doc.Buffer.GetText()
	if string(txt2) != "      " { // 2 + 4 = 6 spaces
		t.Fatalf("expected 6 spaces after Tab with TabSize=4, got %q", string(txt2))
	}
}

// TestTerminalStabilityAndTickLoop verifies the terminal drawer tick loop stays alive and EnsureSession launches.
func TestTerminalStabilityAndTickLoop(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// Open terminal drawer
	app.terminal.Open = true
	app.terminalFocused = true

	// EnsureSession should start without error
	err := app.terminal.EnsureSession()
	if err != nil {
		t.Logf("EnsureSession warning (pty may not be supported on this host): %v", err)
	}

	// Send termTickMsg when terminal.Open is true
	updated, cmd := app.Update(termTickMsg{})
	if updated == nil {
		t.Fatal("expected updated model")
	}
	if cmd == nil {
		t.Fatal("expected tickTerm() cmd to keep terminal alive when Open is true")
	}

	// When terminal is closed, tick loop should gracefully stop
	app.terminal.Open = false
	_, cmdClosed := app.Update(termTickMsg{})
	if cmdClosed != nil {
		t.Fatal("expected tick loop to stop when terminal is closed")
	}
}

// TestMultiRepositoryLiveSettings verifies Settings Category 6 exposes live plugin.Manager repositories.
func TestMultiRepositoryLiveSettings(t *testing.T) {
	tempDir := t.TempDir()
	pm, err := plugin.NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer pm.Close()
	i18n.SetLocale("en")

	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.SetPluginManager(pm)

	// Add custom repository to manager
	customRepo := plugin.PluginRepository{
		ID:      "test-enterprise-repo",
		Name:    "Enterprise Internal Registry",
		URL:     "https://plugins.enterprise.internal/v1",
		Type:    "http",
		Enabled: true,
	}
	if err := pm.AddRepository(customRepo); err != nil {
		t.Fatalf("AddRepository failed: %v", err)
	}

	// Verify Settings Category 6 displays the new repository
	fields := app.settings.getCategoryFields(6)
	foundRepo := false
	for _, f := range fields {
		if strings.Contains(f.Label, "Enterprise Internal Registry") {
			foundRepo = true
			if !strings.Contains(f.Value, "Enabled") {
				t.Errorf("expected repo to be marked Enabled, got %q", f.Value)
			}
		}
	}
	if !foundRepo {
		t.Errorf("expected custom enterprise repo to appear in Settings Category 6")
	}

	// Toggle repository in settings
	app.settings.CategoryIdx = 6
	for idx, f := range fields {
		if f.Key == "repo_test-enterprise-repo" {
			app.settings.FieldIdx = idx
			app.settings.cycleCurrentField()
			break
		}
	}

	// Verify toggled
	repos := pm.GetRepositories()
	for _, r := range repos {
		if r.ID == "test-enterprise-repo" {
			if r.Enabled {
				t.Errorf("expected repo to be toggled to Disabled, but was Enabled")
			}
		}
	}
}
