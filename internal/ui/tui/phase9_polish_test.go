package tui

import (
	"os"
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	tea "github.com/baibeicha/goatui/pkg/tea"
	"tahr/internal/core"
	"tahr/internal/core/plugin"
)

func TestPhase9_ColorSwatchesAndEditorColorPicker(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)

	content := "package main\n\nconst BgColor = \"#1e1e2e\"\nconst FgColor = \"#cdd6f4\"\n"
	_ = eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: content})

	buf := buffer.NewBuffer(100, 30)
	app.width = 100
	app.height = 30
	pane := app.splits.ActivePane()
	pane.Bounds = buffer.NewRect(0, 1, 100, 28)
	doc := eng.ActiveDocument()
	app.renderPane(buf, pane, doc, true, false)

	// Verify editor color swatches were detected and recorded
	if len(app.editorColorSwatches) == 0 {
		t.Fatalf("expected editorColorSwatches to be detected and recorded, got 0")
	}

	foundBg := false
	for _, swatch := range app.editorColorSwatches {
		if swatch.Hex == "#1e1e2e" {
			foundBg = true
			// Test opening color picker for this swatch
			app.openEditorColorPicker(swatch)
			break
		}
	}
	if !foundBg {
		t.Fatalf("expected to find #1e1e2e swatch in editorColorSwatches")
	}

	if app.editorColorPicker == nil || !app.editorColorPicker.Open {
		t.Fatalf("expected editorColorPicker to be open after openEditorColorPicker")
	}

	// Change color to #ff0077 and apply (simulate Enter)
	app.editorColorPicker.CurHex = "#ff0077"
	app.editorColorPicker.HexInput = "ff0077"
	_, shouldClose := app.editorColorPicker.HandleKey(input.Key{Type: input.KeyEnter})
	if !shouldClose || app.editorColorPicker.Open {
		t.Fatalf("expected color picker to close on Enter")
	}

	// Verify buffer text was updated
	doc = eng.ActiveDocument()
	textBytes, _ := doc.Buffer.GetText()
	textStr := string(textBytes)
	if !foundString(textStr, "#ff0077") {
		t.Fatalf("expected buffer text to contain new hex #ff0077, got:\n%s", textStr)
	}

	// Verify Undo (Ctrl+Z) reverts the color change
	app.performUndo()
	textBytesAfterUndo, _ := doc.Buffer.GetText()
	if !foundString(string(textBytesAfterUndo), "#1e1e2e") {
		t.Fatalf("expected undo to restore original color #1e1e2e, got:\n%s", string(textBytesAfterUndo))
	}
}

func TestPhase9_MinimapNoTrailingDuplication(t *testing.T) {
	buf := buffer.NewBuffer(30, 20)
	eng := core.NewEngine()
	app := NewAppModel(eng)

	totalLines := 3
	lines := []string{"package main", "func main() {", "}"}
	getLine := func(idx int) string {
		if idx >= 0 && idx < len(lines) {
			return lines[idx]
		}
		return ""
	}

	// Render minimap with height 15 (much larger than 3 lines)
	RenderMinimap(buf, app.theme, 10, 0, 5, 15, totalLines, 0, 15, getLine)

	// Rows 0, 1, 2 should have line content / divider
	c0 := buf.Cell(10, 0)
	if c0 == nil || c0.Rune == ' ' {
		t.Errorf("expected line 0 in minimap to have divider or rune, got empty")
	}

	// Rows 5..14 should NOT duplicate the last line; left divider should be blank
	for r := 5; r < 15; r++ {
		cellDivider := buf.Cell(10, r)
		if cellDivider != nil && cellDivider.Rune == '▌' {
			t.Errorf("row %d beyond totalLines should not have active viewport block", r)
		}
	}
}

func TestPhase9_FastScrollAndViewportSync(t *testing.T) {
	eng := core.NewEngine()
	app := NewAppModel(eng)
	doc := eng.ActiveDocument()

	// Insert 100 lines
	for i := 0; i < 100; i++ {
		_ = eng.Dispatch(core.Command{ID: core.CmdInsertNewline})
	}

	app.height = 40
	app.width = 100
	app.ensureCursorVisible()

	// Verify doc.Viewport.Height was synced
	if doc.Viewport.Height <= 1 {
		t.Fatalf("expected doc.Viewport.Height to be synced with editorHeight, got %d", doc.Viewport.Height)
	}

	// Move cursor to start of document so PageDown can scroll forward
	_ = eng.Dispatch(core.Command{ID: core.CmdCursorDocStart})
	app.ensureCursorVisible()

	// Test PageDown scrolls full page
	initialLine := doc.Buffer.PrimarySelection().Head.Line
	_ = eng.Dispatch(core.Command{ID: core.CmdPageDown})
	newLine := doc.Buffer.PrimarySelection().Head.Line
	if newLine <= initialLine+5 {
		t.Fatalf("expected PageDown to scroll full page (>= 20 lines), but moved from %d to %d", initialLine, newLine)
	}

	// Test MouseWheelUp & Down updates target viewport offsets
	app.viewportY = 20
	pane := app.splits.ActivePane()
	pane.ViewportY = 20

	_, _ = app.Update(tea.MouseMsg{
		Mouse: input.Mouse{
			Button: input.MouseWheelDown,
			X:      30,
			Y:      10,
		},
	})

	if pane.TargetViewportY != float64(app.viewportY) || pane.SmoothScrollY != float64(app.viewportY) {
		t.Errorf("expected pane.TargetViewportY and SmoothScrollY to sync on wheel down, got target=%.1f smooth=%.1f vpY=%d",
			pane.TargetViewportY, pane.SmoothScrollY, app.viewportY)
	}
}

func TestPhase9_MarketplaceEditRepository(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tahr-repo-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	mgr, err := plugin.NewManager(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Add initial repository
	initialRepo := plugin.PluginRepository{
		ID:      "test-repo-1",
		Name:    "Original Name",
		URL:     "https://repo.example.com/v1",
		Type:    "http",
		Enabled: true,
	}
	if err := mgr.AddRepository(initialRepo); err != nil {
		t.Fatal(err)
	}

	// Update repository URL and Name
	updatedRepo := plugin.PluginRepository{
		ID:      "test-repo-1",
		Name:    "Updated Name",
		URL:     "https://repo.example.com/v2-updated",
		Type:    "http",
		Enabled: true,
	}
	if err := mgr.UpdateRepository(updatedRepo); err != nil {
		t.Fatalf("failed to update repository: %v", err)
	}

	// Verify update was persisted
	repos := mgr.GetRepositories()
	found := false
	for _, r := range repos {
		if r.ID == "test-repo-1" {
			found = true
			if r.Name != "Updated Name" || r.URL != "https://repo.example.com/v2-updated" {
				t.Fatalf("expected updated repository attributes, got Name=%s URL=%s", r.Name, r.URL)
			}
		}
	}
	if !found {
		t.Fatal("expected to find test-repo-1 in repository list")
	}

	// Test MarketplaceModal 'e' key activates editing mode
	mm := NewMarketplaceModal(mgr)
	mm.Open = true
	mm.ActiveTab = 2 // Repositories tab
	mm.Refresh()

	// Select repo
	mm.SelectedRepo = 0
	handled, _ := mm.HandleKey(input.Key{Type: input.KeyRune, Rune: 'e'})
	if !handled || !mm.AddingRepo || mm.EditingRepoID == "" {
		t.Fatalf("expected 'e' key to enter editing mode with EditingRepoID set, got AddingRepo=%v EditingRepoID=%s",
			mm.AddingRepo, mm.EditingRepoID)
	}
	if mm.AddRepoField != 1 {
		t.Errorf("expected AddRepoField to focus URL field (1), got %d", mm.AddRepoField)
	}
}

func foundString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
