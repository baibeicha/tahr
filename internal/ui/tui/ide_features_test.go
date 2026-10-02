package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/lsp"
	"tahr/internal/core/plugin"
)

func TestIDE_ToolPromptModal(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// Ensure toolPrompt modal is initialized
	if app.toolPrompt == nil {
		t.Fatal("expected toolPrompt to be initialized")
	}

	// 1. Open toolPrompt for a missing tool
	var installed bool
	var customPathSet string
	app.toolPrompt.OpenForTool(
		"test-analyzer",
		"Test Language Support",
		"pkg install test-analyzer",
		".test",
		func() { installed = true },
		func(p string) { customPathSet = p },
	)

	if !app.toolPrompt.Open {
		t.Fatal("expected toolPrompt to be open")
	}

	// 2. Press 'P' to enter Specify Path input mode
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'p'}})
	if !app.toolPrompt.InputMode {
		t.Fatal("expected toolPrompt to be in InputMode after pressing 'p'")
	}

	// Type "/custom/path/bin"
	for _, r := range "/custom/path/bin" {
		app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
	}
	if app.toolPrompt.InputPath != "/custom/path/bin" {
		t.Fatalf("expected InputPath '/custom/path/bin', got %q", app.toolPrompt.InputPath)
	}

	// Press Enter to apply
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	if app.toolPrompt.Open {
		t.Fatal("expected toolPrompt to close after Enter")
	}
	if customPathSet != "/custom/path/bin" {
		t.Fatalf("expected custom path callback with '/custom/path/bin', got %q", customPathSet)
	}

	// 3. Open again and test 'D' (Auto-install)
	app.toolPrompt.OpenForTool(
		"test-analyzer",
		"Test Language Support",
		"pkg install test-analyzer",
		".test",
		func() { installed = true },
		func(p string) { customPathSet = p },
	)
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'd'}})
	if !installed {
		t.Fatal("expected OnInstall callback to be triggered after pressing 'd'")
	}
	if app.toolPrompt.Open {
		t.Fatal("expected toolPrompt to close after install")
	}

	// 4. Open again and test Esc (Ignore)
	app.toolPrompt.OpenForTool("test-analyzer", "Test", "cmd", ".test", nil, nil)
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEsc}})
	if app.toolPrompt.Open {
		t.Fatal("expected toolPrompt to close after Esc")
	}
}

func TestIDE_StructurePanelToggleAndJump(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30
	app.settings.Current.TreePosition = "left"

	// Create and open a sample Go document
	_, err := app.eng.Open("sample.go")
	if err != nil {
		t.Fatalf("failed to open sample.go: %v", err)
	}
	sampleCode := `package main

type Config struct {
	Host string
}

func (c *Config) Validate() bool {
	return true
}

func Run() {
}
`
	_ = app.eng.InsertText(0, 0, sampleCode)

	// 1. Click Left Activity Strip on Row 4 (ST: Structure)
	// stripLeftW is 3 when width >= 70
	app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      1,
			Y:      4, // Row 4 = ST
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})

	if !app.sidebarOpen || app.sidebarMode != 1 {
		t.Fatalf("expected sidebarOpen=true and sidebarMode=1 (Structure) after clicking ST, got open=%v mode=%d", app.sidebarOpen, app.sidebarMode)
	}
	app.sidebarAnimWidth = float64(app.sidebarWidth)

	// Check that symbols were extracted
	if len(app.structurePanel.Items) < 3 {
		t.Fatalf("expected at least 3 symbols extracted, got %d", len(app.structurePanel.Items))
	}

	// Find the "Validate" method item
	var validateItem *StructureItem
	var validateIdx int
	for idx, item := range app.structurePanel.Items {
		if item.Name == "Validate" {
			validateItem = &item
			validateIdx = idx
			break
		}
	}
	if validateItem == nil {
		t.Fatalf("expected 'Validate' method in structure items: %+v", app.structurePanel.Items)
	}
	if validateItem.Icon != "[M]" {
		t.Errorf("expected [M] icon for Validate method, got %s", validateItem.Icon)
	}

	// 2. Click on the symbol inside the sidebar
	// sidebar content starts at Y = editorTop + 1 = 3
	treeStartX := 3 // stripLeftW
	if app.settings.Current.TreePosition == "left" {
		treeStartX = 3
	}
	app.sidebarScrollY = 0
	clickY := 3 + validateIdx
	app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      treeStartX + 5,
			Y:      clickY,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})

	// Cursor should jump to the line of Validate
	doc := app.eng.ActiveDocument()
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		t.Fatal("expected selections after jump")
	}
	if sels[0].Head.Line != validateItem.Line {
		t.Errorf("expected cursor at line %d, got %d", validateItem.Line, sels[0].Head.Line)
	}

	// 3. Test keyboard navigation in Structure panel
	app.sidebarFocused = true
	app.structurePanel.Selected = 0
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyDown}})
	if app.structurePanel.Selected != 1 {
		t.Errorf("expected selected symbol index 1 after KeyDown, got %d", app.structurePanel.Selected)
	}

	// Enter jumps to selected symbol
	app.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyEnter}})
	targetLine := app.structurePanel.Items[1].Line
	sels = doc.Buffer.GetSelections()
	if sels[0].Head.Line != targetLine {
		t.Errorf("expected cursor at line %d after Enter, got %d", targetLine, sels[0].Head.Line)
	}
}

func TestIDE_URINormalizationAndDiagnostics(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30

	// Test URI normalization across different Windows / POSIX formats
	uri1 := "file:///d:/project/main.go"
	uri2 := "file:///D:/project/main.go"
	uri3 := "file:///d%3A/project/main.go"
	uri4 := "D:\\project\\main.go"

	norm1 := normalizeURI(uri1)
	norm2 := normalizeURI(uri2)
	norm3 := normalizeURI(uri3)
	norm4 := normalizeURI(uri4)

	if norm1 != norm2 {
		t.Errorf("expected norm1 == norm2, got %q vs %q", norm1, norm2)
	}
	if norm1 != norm3 {
		t.Errorf("expected norm1 == norm3, got %q vs %q", norm1, norm3)
	}
	if norm1 != norm4 {
		t.Errorf("expected norm1 == norm4, got %q vs %q", norm1, norm4)
	}

	// Open a file with lowercase drive letter
	_, err := app.eng.Open("D:/project/main.go")
	if err != nil {
		t.Fatalf("failed to open doc: %v", err)
	}
	_ = app.eng.InsertText(0, 0, "package main\n\nfunc main() {\n\tundefinedVar()\n}\n")

	// Deliver diagnostics from LSP using uppercase drive URI
	lspDiags := []lsp.Diagnostic{
		{
			Range: lsp.Range{
				Start: lsp.Position{Line: 3, Character: 1},
				End:   lsp.Position{Line: 3, Character: 13},
			},
			Severity: 1, // Error
			Message:  "undefined: undefinedVar",
		},
	}
	app.OnDiagnostics("file:///D:/project/main.go", lspDiags)

	// Verify diagnostics were stored
	diags, ok := app.getDiagnosticsForLine(3)
	if !ok || len(diags) == 0 {
		t.Fatalf("expected diagnostics on line 3")
	}
	if diags[0].Message != "undefined: undefinedVar" {
		t.Errorf("unexpected diagnostic message: %s", diags[0].Message)
	}

	// Set cursor on line 3 and verify status bar error message
	app.jumpToLine(3, 2)
	errMsg, hasErr := app.getDiagnosticAtCursor()
	if !hasErr || errMsg != "undefined: undefinedVar" {
		t.Errorf("expected cursor error 'undefined: undefinedVar', got %q (hasErr=%v)", errMsg, hasErr)
	}

	// Render frame and check that gutter marker and status bar include error
	buf := buffer.NewBuffer(100, 30)
	frame := &tea.Frame{Buffer: buf}
	app.View(frame)
	// Status bar (bottom line Y=29)
	var statusBarStr strings.Builder
	for x := 0; x < 100; x++ {
		statusBarStr.WriteRune(buf.Cell(x, 29).Rune)
	}
	if !strings.Contains(statusBarStr.String(), "[ERR] undefined: undefinedVar") {
		t.Errorf("expected status bar to contain '[ERR] undefined: undefinedVar', got: %s", statusBarStr.String())
	}
}

func TestIDE_LiveThemeSwitching(t *testing.T) {
	t.Setenv("TAHR_CONFIG_DIR", t.TempDir())
	eng := core.NewEngine()
	app := NewAppModel(eng)

	// Verify manager has builtin catalog themes (dracula, nord, monokai)
	pm, err := plugin.NewManager("")
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}
	app.SetPluginManager(pm)

	// Install a theme plugin (e.g. dracula)
	manifest := plugin.GetBuiltinManifest("tahr-theme-dracula")
	if manifest == nil {
		t.Fatal("expected tahr-theme-dracula in builtin catalog")
	}
	pm.InstallDeclarative(*manifest)

	// Apply dracula theme in settings
	app.settings.Current.Theme = "dracula"
	app.applyCurrentSettings()

	if app.theme.Name != "dracula" {
		t.Errorf("expected theme name 'dracula', got %s", app.theme.Name)
	}
	if app.theme.Background != 0x282a36 {
		t.Errorf("expected dracula bg 0x282a36, got 0x%06x", app.theme.Background)
	}

	// Test cycling themes in settings includes dracula
	app.settings.Current.Theme = "tokyo-night"
	app.settings.HandleKey(input.Key{Type: input.KeyEnter}) // triggers interaction if focused on theme
}

func TestIDE_DynamicToolchainsAndActionModal(t *testing.T) {
	t.Setenv("TAHR_CONFIG_DIR", t.TempDir())
	eng := core.NewEngine()
	app := NewAppModel(eng)

	pm, err := plugin.NewManager("")
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}
	app.SetPluginManager(pm)

	// Open Settings -> Category 5 (Toolchains & SDKs)
	app.settings.Open = true
	app.settings.CategoryIdx = 5
	app.settings.FocusRight = true

	fields := app.settings.getCategoryFields(5)
	if len(fields) < 4 {
		t.Fatalf("expected at least 4 dynamic toolchain fields, got %d", len(fields))
	}

	// Verify entries are generated from installed plugins
	hasGopls := false
	var goplsIdx int
	for idx, f := range fields {
		if strings.Contains(f.Label, "LSP: gopls") {
			hasGopls = true
			goplsIdx = idx
			break
		}
	}
	if !hasGopls {
		t.Fatalf("expected LSP: gopls field in toolchains category, fields: %+v", fields)
	}

	// Navigate to gopls field and press Enter
	app.settings.FieldIdx = goplsIdx
	handled, _ := app.settings.HandleKey(input.Key{Type: input.KeyEnter})
	if !handled || !app.settings.ToolAction.Open {
		t.Fatalf("expected ToolAction modal to open on Enter")
	}
	if app.settings.ToolAction.ToolName != "gopls" {
		t.Errorf("expected ToolName 'gopls', got %s", app.settings.ToolAction.ToolName)
	}

	// Press 'P' to specify path manually
	app.settings.HandleKey(input.Key{Type: input.KeyRune, Rune: 'p'})
	if !app.settings.ToolAction.InputMode {
		t.Fatalf("expected ToolAction to enter InputMode")
	}

	// Type custom path
	customGopls := "C:\\Users\\user\\go\\bin\\gopls.exe"
	app.settings.ToolAction.InputPath = customGopls

	// Commit path with Enter
	app.settings.HandleKey(input.Key{Type: input.KeyEnter})
	if app.settings.ToolAction.Open {
		t.Fatalf("expected ToolAction modal to close after Enter")
	}
	if app.settings.Current.CustomToolPaths["gopls"] != customGopls {
		t.Errorf("expected CustomToolPaths[gopls] = %q, got %q", customGopls, app.settings.Current.CustomToolPaths["gopls"])
	}
	if app.settings.Current.GoplsPath != customGopls {
		t.Errorf("expected GoplsPath = %q, got %q", customGopls, app.settings.Current.GoplsPath)
	}

	// Reopen and test Reset ('R')
	app.settings.FieldIdx = goplsIdx
	app.settings.HandleKey(input.Key{Type: input.KeyEnter})
	if !app.settings.ToolAction.Open {
		t.Fatalf("expected ToolAction modal to open")
	}
	app.settings.HandleKey(input.Key{Type: input.KeyRune, Rune: 'r'})
	if app.settings.Current.CustomToolPaths["gopls"] != "" {
		t.Errorf("expected CustomToolPaths[gopls] cleared after reset")
	}
	if app.settings.Current.GoplsPath != "" {
		t.Errorf("expected GoplsPath cleared after reset")
	}
}

func TestIDE_MultiDocumentDiagnostics(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	pathA := "d:/workspace/pkg/a/main.go"
	pathB := "d:/workspace/cmd/main.go"

	docA, err := app.eng.Open(pathA)
	if err != nil {
		t.Fatalf("failed to open doc A: %v", err)
	}
	_ = app.eng.InsertText(0, 0, "package a\nvar ErrA = 1\n")

	docB, err := app.eng.Open(pathB)
	if err != nil {
		t.Fatalf("failed to open doc B: %v", err)
	}
	_ = app.eng.InsertText(0, 0, "package main\nfunc main() {}\n")

	// Deliver diagnostics for doc A
	app.OnDiagnostics("file:///"+pathA, []lsp.Diagnostic{
		{
			Range:    lsp.Range{Start: lsp.Position{Line: 1, Character: 4}, End: lsp.Position{Line: 1, Character: 8}},
			Severity: 1,
			Message:  "error in package a",
		},
	})

	// Deliver diagnostics for doc B
	app.OnDiagnostics("file:///"+pathB, []lsp.Diagnostic{
		{
			Range:    lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 12}},
			Severity: 2,
			Message:  "warning in package main",
		},
	})

	// Set doc A active and verify diagnostics
	_ = app.eng.SwitchBuffer(docA.ID)
	app.syncActiveDiagnostics()

	diagsA, okA := app.getDiagnosticsForLine(1)
	if !okA || len(diagsA) == 0 || diagsA[0].Message != "error in package a" {
		t.Fatalf("expected error in package a for doc A, got ok=%v diags=%+v", okA, diagsA)
	}
	_, okA0 := app.getDiagnosticsForLine(0)
	if okA0 {
		t.Errorf("doc A should not have diagnostics on line 0")
	}

	// Set doc B active and verify diagnostics
	_ = app.eng.SwitchBuffer(docB.ID)
	app.syncActiveDiagnostics()

	diagsB, okB := app.getDiagnosticsForLine(0)
	if !okB || len(diagsB) == 0 || diagsB[0].Message != "warning in package main" {
		t.Fatalf("expected warning in package main for doc B, got ok=%v diags=%+v", okB, diagsB)
	}
	_, okB1 := app.getDiagnosticsForLine(1)
	if okB1 {
		t.Errorf("doc B should not have diagnostics on line 1")
	}
}

func TestIDE_URINormalizationEdgeCases(t *testing.T) {
	// 1. Windows UNC network paths
	unc1 := "\\\\server\\share\\repo\\file.go"
	unc2 := "file://server/share/repo/file.go"
	unc3 := "file:////server/share/repo/file.go"

	normUNC1 := normalizeURI(unc1)
	normUNC2 := normalizeURI(unc2)
	normUNC3 := normalizeURI(unc3)

	if normUNC1 != normUNC2 {
		t.Errorf("expected UNC normalization match: %q vs %q", normUNC1, normUNC2)
	}
	if normUNC1 != normUNC3 {
		t.Errorf("expected UNC normalization match: %q vs %q", normUNC1, normUNC3)
	}

	// 2. Spaces in Windows paths
	spaced1 := "D:\\My Projects\\App\\main.go"
	spaced2 := "file:///d:/My%20Projects/App/main.go"
	normSpaced1 := normalizeURI(spaced1)
	normSpaced2 := normalizeURI(spaced2)
	if normSpaced1 != normSpaced2 {
		t.Errorf("expected spaced URI match: %q vs %q", normSpaced1, normSpaced2)
	}

	// 3. POSIX paths
	posix1 := "/home/user/project/main.go"
	posix2 := "file:///home/user/project/main.go"
	normPosix1 := normalizeURI(posix1)
	normPosix2 := normalizeURI(posix2)
	if normPosix1 != normPosix2 {
		t.Errorf("expected POSIX match: %q vs %q", normPosix1, normPosix2)
	}
	if !strings.HasPrefix(normPosix1, "/") && !strings.HasPrefix(normPosix1, "\\") {
		t.Errorf("expected POSIX normalized path to remain absolute: %q", normPosix1)
	}
}

func TestIDE_ColorPickerMouseAndHitboxes(t *testing.T) {
	cp := NewColorPickerModal()
	var appliedVal string
	cp.OpenForColor("keyword", "Keyword", "#ff0000", func(key, val string) {
		appliedVal = val
	})

	screenW := 80
	screenH := 30
	modalW := 58
	modalH := 24
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Click on [Enter: Apply] button
	cancelStartX := startX + modalW - 2 - 13
	applyStartX := cancelStartX - 14 - 2
	instY := startY + modalH - 1

	handled, shouldClose := cp.HandleClick(applyStartX+2, instY, screenW, screenH)
	if !handled || !shouldClose || cp.Open {
		t.Fatalf("expected click on Apply button to close modal")
	}
	if appliedVal == "" {
		t.Fatalf("expected OnApply callback to be triggered")
	}

	// Reopen and click [Esc: Cancel]
	appliedVal = ""
	cp.OpenForColor("keyword", "Keyword", "#ff0000", func(key, val string) {
		appliedVal = val
	})
	handled, shouldClose = cp.HandleClick(cancelStartX+2, instY, screenW, screenH)
	if !handled || !shouldClose || cp.Open {
		t.Fatalf("expected click on Cancel button to close modal")
	}
	if appliedVal != "" {
		t.Fatalf("expected OnApply callback NOT to be triggered on Cancel")
	}
}

func TestIDE_ToolPromptModal_MouseClick(t *testing.T) {
	tp := NewToolPromptModal()
	var installed bool
	tp.OpenForTool("gopls", "Go", "go install gopls", ".go", func() {
		installed = true
	}, nil)

	w := 80
	h := 30
	modalW := 68
	modalH := 11
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2

	// Click on [D: Install] at startY+4
	if !tp.HandleClick(startX+5, startY+4, w, h) {
		t.Fatalf("expected click on startY+4 to be handled")
	}
	if !installed || tp.Open {
		t.Fatalf("expected tool prompt to trigger install and close on startY+4 click")
	}

	// Reopen and click on [P: Specify Path] at startY+5
	tp.OpenForTool("gopls", "Go", "go install gopls", ".go", nil, nil)
	if !tp.HandleClick(startX+5, startY+5, w, h) {
		t.Fatalf("expected click on startY+5 to be handled")
	}
	if !tp.InputMode {
		t.Fatalf("expected click on startY+5 to enter InputMode")
	}

	// Click on [Esc: Ignore] at startY+6
	tp.InputMode = false
	if !tp.HandleClick(startX+5, startY+6, w, h) {
		t.Fatalf("expected click on startY+6 to be handled")
	}
	if tp.Open {
		t.Fatalf("expected click on startY+6 to close modal")
	}
}

func TestIDE_ModalMouseInterception(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	app.width = 100
	app.height = 30

	_, _ = app.eng.Open("sample.go")
	_ = app.eng.InsertText(0, 0, "line 1\nline 2\nline 3\n")
	_ = app.eng.Dispatch(core.Command{ID: core.CmdCursorDocStart})

	// 1. Marketplace modal open -> mouse click should be intercepted
	app.marketplace.Open = true
	app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      5,
			Y:      5,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})
	// Cursor in active document should not move to (5, 5)
	doc := app.eng.ActiveDocument()
	sels := doc.Buffer.GetSelections()
	if sels[0].Head.Line != 0 {
		t.Errorf("marketplace modal click leaked to editor buffer line %d", sels[0].Head.Line)
	}

	// 2. Settings modal open -> mouse click should be intercepted
	app.marketplace.Open = false
	app.settings.Open = true
	app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			X:      50,
			Y:      10,
			Action: input.MousePress,
			Button: input.MouseLeft,
		},
	})
	sels = doc.Buffer.GetSelections()
	if sels[0].Head.Line != 0 {
		t.Errorf("settings modal click leaked to editor buffer line %d", sels[0].Head.Line)
	}
}

