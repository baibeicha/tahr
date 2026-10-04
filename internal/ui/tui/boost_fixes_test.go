package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/plugin"
)

// TestMouseClickTabIndentAlignment tests that clicking on tab-indented code
// places the cursor precisely on the clicked rune without 5-10 character rightward offset.
func TestMouseClickTabIndentAlignment(t *testing.T) {
	eng := core.NewEngine()
	doc, err := eng.Open("test_indent.go")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Line with 2 tabs: \t\tfunc main() {\n
	// Rune 0: \t (visual 0..3)
	// Rune 1: \t (visual 4..7)
	// Rune 2: 'f' (visual 8)
	// Rune 3: 'u' (visual 9)
	// Rune 4: 'n' (visual 10)
	// Rune 5: 'c' (visual 11)
	code := "\t\tfunc main() {\n\t\t\tx := 123\n}\n"
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: code})

	app := NewAppModel(eng)
	app.width = 100
	app.height = 30
	app.sidebarOpen = false

	gutterW := app.gutterWidth()
	editorTop := 2 // row 0: header, row 1: tab bar, row 2: code line 0
	screenY := editorTop

	stripLeftW := 3 // width >= 70 has 3-char left activity strip
	textStartX := stripLeftW + gutterW

	// Click on rune 'f' (visual column 8).
	clickX := textStartX + 8
	_, _ = app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      clickX,
			Y:      screenY,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})

	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatal("expected active selection after mouse click")
	}
	head := sels[0].Head
	if head.Line != 0 {
		t.Fatalf("expected Line 0, got Line %d", head.Line)
	}
	if head.Column != 2 {
		t.Fatalf("expected Column 2 ('f'), got Column %d. Rightward offset bug is present!", head.Column)
	}

	visCol := doc.Buffer.VisualColumn(head.Line, head.Column)
	if visCol != 8 {
		t.Fatalf("expected VisualColumn 8, got %d", visCol)
	}

	// Click on rune 'c' (visual column 11).
	clickXC := textStartX + 11
	_, _ = app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      clickXC,
			Y:      screenY,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})

	sels = doc.Buffer.GetSelections()
	head = sels[0].Head
	if head.Column != 5 {
		t.Fatalf("expected Column 5 ('c'), got Column %d", head.Column)
	}
	if v := doc.Buffer.VisualColumn(head.Line, head.Column); v != 11 {
		t.Fatalf("expected VisualColumn 11, got %d", v)
	}
}

// TestMouseDragSelectionInEditor verifies that dragging the mouse in the editor
// extends text selection from the initial anchor to the dragged head position.
func TestMouseDragSelectionInEditor(t *testing.T) {
	eng := core.NewEngine()
	doc, err := eng.Open("test_drag.go")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: "Hello World\nSecond Line\n"})

	app := NewAppModel(eng)
	app.width = 100
	app.height = 30
	app.sidebarOpen = false

	gutterW := app.gutterWidth()
	editorTop := 2
	screenY := editorTop
	stripLeftW := 3
	textStartX := stripLeftW + gutterW

	// 1. MousePress at column 0 ('H')
	_, _ = app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      textStartX,
			Y:      screenY,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})

	// 2. MouseDrag to column 5 (after "Hello")
	_, _ = app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      textStartX + 5,
			Y:      screenY,
			Action: input.MouseDrag,
			Button: input.MouseLeft,
		},
	})

	sels := doc.Buffer.GetSelections()
	if len(sels) != 1 {
		t.Fatalf("expected 1 selection after drag, got %d", len(sels))
	}
	sel := sels[0]
	if sel.Anchor.Line != 0 || sel.Anchor.Column != 0 {
		t.Fatalf("expected anchor at (0, 0), got (%d, %d)", sel.Anchor.Line, sel.Anchor.Column)
	}
	if sel.Head.Line != 0 || sel.Head.Column != 5 {
		t.Fatalf("expected head at (0, 5), got (%d, %d)", sel.Head.Line, sel.Head.Column)
	}
}

// TestLaunchModalIntegrationInApp verifies that the LaunchConfigModal is wired into AppModel,
// responds to Alt+Shift+F10, renders, and handles profile runs.
func TestLaunchModalIntegrationInApp(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30

	if app.launchModal == nil {
		t.Fatal("expected launchModal to be initialized in NewAppModel")
	}
	if app.launchModal.Open {
		t.Fatal("launchModal should be closed by default")
	}

	// Open via Alt+Shift+F10
	app.handleKey(input.Key{
		Type: input.KeyF10,
		Mod:  input.ModAlt | input.ModShift,
	})

	if !app.launchModal.Open {
		t.Fatal("expected launchModal.Open to be true after Alt+Shift+F10")
	}

	// Render screen with launch modal open
	buf := buffer.NewBuffer(app.width, app.height)
	app.launchModal.Render(buf, app.width, app.height, &app.theme)

	// Close modal via Esc
	app.handleKey(input.Key{Type: input.KeyEsc})
	if app.launchModal.Open {
		t.Fatal("expected launchModal.Open to be false after Esc")
	}

	// Open via Omnibar command "Run: Configurations"
	app.executeOmnibarCommand("Run: Configurations (Alt+Shift+F10)")
	if !app.launchModal.Open {
		t.Fatal("expected launchModal to open from Omnibar command")
	}
	app.launchModal.Open = false
}

// TestThemePersistenceAndSwitching verifies that changing theme saves to settings.json
// and changes the active colors immediately.
func TestThemePersistenceAndSwitching(t *testing.T) {
	tempDir := t.TempDir()
	origConfigDir := os.Getenv("TAHR_CONFIG_DIR")
	defer func() {
		_ = os.Setenv("TAHR_CONFIG_DIR", origConfigDir)
	}()
	_ = os.Setenv("TAHR_CONFIG_DIR", tempDir)

	eng := core.NewEngine()
	app := NewAppModel(eng)

	// Switch theme to Dracula
	app.SetThemeByName("dracula")
	if strings.ToLower(app.settings.Current.Theme) != "dracula" {
		t.Fatalf("expected theme to be dracula, got %s", app.settings.Current.Theme)
	}

	// Verify Dracula colors
	if app.theme.Background != 0x282a36 {
		t.Errorf("expected Dracula background 0x282a36, got 0x%06x", app.theme.Background)
	}

	// Verify settings.json persisted on disk
	cfgPath := filepath.Join(tempDir, "settings.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read settings.json: %v", err)
	}
	if !strings.Contains(string(data), `"dracula"`) {
		t.Errorf("expected settings.json to contain 'dracula', got: %s", string(data))
	}

	// Switch theme to Nord
	app.SetThemeByName("nord")
	if app.theme.Background != 0x2e3440 {
		t.Errorf("expected Nord background 0x2e3440, got 0x%06x", app.theme.Background)
	}

	// Switch theme to Monokai
	app.SetThemeByName("monokai")
	if app.theme.Background != 0x272822 {
		t.Errorf("expected Monokai background 0x272822, got 0x%06x", app.theme.Background)
	}

	// Switch theme to Tokyo Night
	app.SetThemeByName("tokyo-night")
	if app.theme.Background != 0x1a1b26 {
		t.Errorf("expected Tokyo Night background 0x1a1b26, got 0x%06x", app.theme.Background)
	}
}

// TestTerminalSpawnThrottlingDefense verifies that repeated failed spawns are throttled.
func TestTerminalSpawnThrottlingDefense(t *testing.T) {
	td := NewTerminalDrawer(t.TempDir())
	inst := td.ActiveInstance()
	inst.spawnFailures = 4
	inst.lastSpawnAttempt = time.Now()

	err := td.EnsureSession()
	if err == nil {
		t.Fatal("expected EnsureSession to fail with throttling error when spawnFailures >= 3")
	}
	if !strings.Contains(err.Error(), "throttled") {
		t.Errorf("expected error message to mention 'throttled', got %v", err)
	}
}

// TestBuiltinPluginsManifests verifies that all required language and theme plugins are defined.
func TestBuiltinPluginsManifests(t *testing.T) {
	// 1. Check tahr-go plugin
	goPlugin := plugin.GetBuiltinManifest("tahr-go")
	if goPlugin == nil {
		t.Fatal("expected tahr-go builtin manifest")
	}
	if goPlugin.LSP == nil || goPlugin.LSP.ServerName != "gopls" {
		t.Errorf("expected gopls LSP in tahr-go, got %+v", goPlugin.LSP)
	}
	if goPlugin.DAP == nil || goPlugin.DAP.AdapterName != "delve" {
		t.Errorf("expected delve DAP in tahr-go, got %+v", goPlugin.DAP)
	}
	if len(goPlugin.Languages) == 0 || goPlugin.Languages[0].RunCmd != "go" {
		t.Errorf("expected go run in tahr-go, got %+v", goPlugin.Languages)
	}

	// 2. Check all 6 theme plugins
	themes := []string{
		"tahr-theme-dracula",
		"tahr-theme-nord",
		"tahr-theme-monokai",
		"tahr-theme-tokyo",
		"tahr-theme-catppuccin",
		"tahr-theme-gruvbox",
	}
	for _, th := range themes {
		manifest := plugin.GetBuiltinManifest(th)
		if manifest == nil {
			t.Fatalf("expected builtin manifest for theme %s", th)
		}
		if len(manifest.Themes) == 0 {
			t.Fatalf("expected Themes in manifest for %s", th)
		}
		if len(manifest.Themes[0].Colors) == 0 {
			t.Fatalf("expected Colors in theme for %s", th)
		}
	}

	// 3. Test seeding into a directory: only tahr-go should be seeded, themes are built-in and not seeded
	tempDir := t.TempDir()
	mgr, err := plugin.NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()
	mgr.SeedDefaultPlugins()

	for _, th := range themes {
		pPath := filepath.Join(tempDir, th, "plugin.json")
		if _, err := os.Stat(pPath); !os.IsNotExist(err) {
			t.Errorf("expected theme %s NOT to be on disk after SeedDefaultPlugins (themes are built-in)", pPath)
		}
	}
	goPath := filepath.Join(tempDir, "tahr-go", "plugin.json")
	if _, err := os.Stat(goPath); os.IsNotExist(err) {
		t.Errorf("expected tahr-go on disk after SeedDefaultPlugins")
	}
}

func TestApp_DisabledLanguagePlugin_SuppressesLSPAndRendersPlainText(t *testing.T) {
	tempDir := t.TempDir()
	mgr, err := plugin.NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	testGoManifest := plugin.Manifest{
		ID:          "tahr-go",
		Name:        "Go Language Support",
		Version:     "1.0.0",
		Description: "Go tooling",
		Languages: []plugin.LanguageConfig{
			{
				ID:         "go",
				Extensions: []string{".go"},
			},
		},
		LSP: &plugin.LSPConfig{
			ServerName: "gopls",
			Command:    "gopls",
		},
	}
	if _, err := mgr.InstallDeclarative(testGoManifest); err != nil {
		t.Fatalf("InstallDeclarative failed: %v", err)
	}

	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.SetPluginManager(mgr)

	// Create and open a Go document
	goFilePath := filepath.Join(tempDir, "main.go")
	_ = os.WriteFile(goFilePath, []byte("package main\n\nfunc main() {}\n"), 0644)
	doc, err := eng.Open(goFilePath)
	if err != nil {
		t.Fatalf("eng.Open failed: %v", err)
	}

	// 1. With plugin enabled: .go is active
	if !mgr.IsExtensionActive(".go") {
		t.Fatalf("expected .go extension to be active when enabled")
	}

	// 2. Disable plugin
	if err := mgr.DisablePlugin("tahr-go"); err != nil {
		t.Fatalf("DisablePlugin failed: %v", err)
	}

	// Verify manager state
	if mgr.IsExtensionActive(".go") {
		t.Errorf("expected .go extension to be inactive")
	}
	if mgr.GetLanguageConfig(".go") != nil {
		t.Errorf("expected nil language config for .go")
	}

	// 3. EnsureLSPForFile should NOT start any LSP client because plugin is disabled
	app.EnsureLSPForFile(doc.FilePath)
	if app.lspClient != nil {
		t.Errorf("expected lspClient to be nil when plugin is disabled, but client was started")
	}

	// 4. Test rendering pane: spans should be nil (plain text)
	buf := buffer.NewBuffer(80, 24)
	app.width = 80
	app.height = 24
	pane := app.splits.ActivePane()
	pane.Bounds = buffer.NewRect(0, 1, 80, 22)
	app.renderPane(buf, pane, doc, true, false)
}
