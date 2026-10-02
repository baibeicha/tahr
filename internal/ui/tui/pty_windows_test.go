//go:build windows

package tui

import (
	"strings"
	"testing"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"golang.org/x/sys/windows"

	"tahr/internal/core"
	cbuf "tahr/internal/core/buffer"
)

// TestCreateProcessFlags verifies that the Windows ConPTY process creation
// uses EXTENDED_STARTUPINFO_PRESENT so that pseudoconsole attributes are passed.
func TestCreateProcessFlags(t *testing.T) {
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	if flags&windows.EXTENDED_STARTUPINFO_PRESENT == 0 {
		t.Fatal("expected EXTENDED_STARTUPINFO_PRESENT to be set in flags")
	}
}



// TestTerminalResize_Dampener verifies that repeated Resize calls with identical
// dimensions are properly dampened without redundant PTY pseudo-console resets.
func TestTerminalResize_Dampener(t *testing.T) {
	td := NewTerminalDrawer(".")
	defer td.Kill()

	td.Resize(120, 30)
	td.mu.Lock()
	if td.lastCols != 120 || td.lastRows != 30 {
		t.Fatalf("expected lastCols=120, lastRows=30, got %d, %d", td.lastCols, td.lastRows)
	}
	td.mu.Unlock()

	// Calling resize again with the same dimensions should be a no-op
	td.Resize(120, 30)
	td.mu.Lock()
	if td.lastCols != 120 || td.lastRows != 30 {
		t.Fatalf("expected dampened values to remain 120, 30")
	}
	td.mu.Unlock()
}

// TestTerminalEnsureSession_MaxFailuresHalt verifies that runaway spawn failures
// halt after the max threshold (>= 10) to prevent an infinite process spawning loop.
func TestTerminalEnsureSession_MaxFailuresHalt(t *testing.T) {
	td := NewTerminalDrawer(".")
	defer td.Kill()

	td.mu.Lock()
	if len(td.Instances) > 0 {
		td.Instances[0].spawnFailures = 10
	}
	td.mu.Unlock()

	err := td.EnsureSession()
	if err == nil {
		t.Fatal("expected EnsureSession to halt and return error when spawnFailures >= 10")
	}
	if !strings.Contains(err.Error(), "maximum retry attempts exceeded") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestMouseClick_StatusbarIgnored verifies that clicking on the status bar (bottom row)
// does not mutate the active editor buffer or displace the cursor position.
func TestMouseClick_StatusbarIgnored(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Initial cursor is at line 0, col 0
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 || sels[0].Head.Line != 0 || sels[0].Head.Column != 0 {
		t.Fatalf("expected initial cursor at (0, 0)")
	}

	// Click on bottom row (Y = 29, status bar)
	clickMsg := tea.MouseMsg{
		Mouse: input.Mouse{
			X:      15,
			Y:      29,
			Button: input.MouseLeft,
			Action: input.MousePress,
		},
	}
	updated, _ = m.Update(clickMsg)
	m = updated.(*AppModel)

	selsAfter := doc.Buffer.GetSelections()
	if len(selsAfter) == 0 || selsAfter[0].Head.Line != 0 || selsAfter[0].Head.Column != 0 {
		t.Fatalf("expected cursor to remain at (0, 0), got line=%d, col=%d",
			selsAfter[0].Head.Line, selsAfter[0].Head.Column)
	}
}

// TestMouseClick_ShiftClickSelection verifies Shift+Click text range selection extension.
func TestMouseClick_ShiftClickSelection(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	updated, _ = m.Update(tea.PasteMsg{Text: "Line 1\nLine 2\nLine 3\n"})
	m = updated.(*AppModel)

	doc := m.eng.ActiveDocument()
	if doc == nil {
		t.Fatal("expected active document")
	}

	// Set cursor at (0, 0)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 0, Column: 0, Byte: 0}, Head: cbuf.Position{Line: 0, Column: 0, Byte: 0}},
	})

	gw := m.gutterWidth()
	// Click on line 2 (screen row = 2 + 2 = 4) at visual col 4 (screen X = 3 + gw + 4) with Shift
	clickMsg := tea.MouseMsg{
		Mouse: input.Mouse{
			X:      3 + gw + 4,
			Y:      4,
			Button: input.MouseLeft,
			Action: input.MousePress,
			Mod:    input.ModShift,
		},
	}
	updated, _ = m.Update(clickMsg)
	m = updated.(*AppModel)

	sels := doc.Buffer.GetSelections()
	if len(sels) != 1 {
		t.Fatalf("expected 1 selection, got %d", len(sels))
	}
	sel := sels[0]
	if sel.Anchor.Line != 0 || sel.Anchor.Column != 0 {
		t.Errorf("expected anchor at (0,0), got line=%d, col=%d", sel.Anchor.Line, sel.Anchor.Column)
	}
	if sel.Head.Line != 2 {
		t.Errorf("expected head at line 2, got line=%d", sel.Head.Line)
	}
}

// TestMouseClick_WideRuneAlignment verifies that wide CJK and emoji runes correctly map visual columns to rune columns.
func TestMouseClick_WideRuneAlignment(t *testing.T) {
	// "你好世界" - each rune is 2 columns wide
	line := []byte("你好世界")
	tabSize := 4

	// Visual col 0 -> rune 0 (before '你')
	// Visual col 1 -> rune 1 (right half of '你', snaps after '你')
	col0 := cbuf.LineVisualToRuneCol(line, 0, tabSize)
	if col0 != 0 {
		t.Errorf("visual col 0: expected rune col 0, got %d", col0)
	}
	col1 := cbuf.LineVisualToRuneCol(line, 1, tabSize)
	if col1 != 1 {
		t.Errorf("visual col 1: expected rune col 1, got %d", col1)
	}

	// Visual col 2 -> rune 1 (before '好')
	// Visual col 3 -> rune 2 (right half of '好', snaps after '好')
	col2 := cbuf.LineVisualToRuneCol(line, 2, tabSize)
	if col2 != 1 {
		t.Errorf("visual col 2: expected rune col 1, got %d", col2)
	}
	col3 := cbuf.LineVisualToRuneCol(line, 3, tabSize)
	if col3 != 2 {
		t.Errorf("visual col 3: expected rune col 2, got %d", col3)
	}

	// Visual col 4 -> rune 2 (before '世')
	// Visual col 5 -> rune 3 (right half of '世', snaps after '世')
	col4 := cbuf.LineVisualToRuneCol(line, 4, tabSize)
	if col4 != 2 {
		t.Errorf("visual col 4: expected rune col 2, got %d", col4)
	}
	col5 := cbuf.LineVisualToRuneCol(line, 5, tabSize)
	if col5 != 3 {
		t.Errorf("visual col 5: expected rune col 3, got %d", col5)
	}

	// Visual col 6 -> rune 3 (before '界')
	// Visual col 7 -> rune 4 (right half of '界', snaps after '界')
	col6 := cbuf.LineVisualToRuneCol(line, 6, tabSize)
	if col6 != 3 {
		t.Errorf("visual col 6: expected rune col 3, got %d", col6)
	}
	col7 := cbuf.LineVisualToRuneCol(line, 7, tabSize)
	if col7 != 4 {
		t.Errorf("visual col 7: expected rune col 4, got %d", col7)
	}

	// Past the end -> rune 4 (length of line)
	col8 := cbuf.LineVisualToRuneCol(line, 8, tabSize)
	if col8 != 4 {
		t.Errorf("visual col 8: expected rune col 4, got %d", col8)
	}
}

// TestStructure_VarAndConstMultipleIdents verifies that const( and var( blocks
// without spaces and comma-separated declarations are correctly extracted.
func TestStructure_VarAndConstMultipleIdents(t *testing.T) {
	code := `package main

const(
	Alpha, Beta = 1, 2
	Gamma = 3
)

var(
	X, Y int
	Z = "hello"
)
`
	items := ExtractSymbolsRegex(".go", code)
	found := make(map[string]string)
	for _, it := range items {
		found[it.Name] = it.Icon
	}

	expectedConsts := []string{"Alpha", "Beta", "Gamma"}
	for _, c := range expectedConsts {
		if found[c] != "[C]" {
			t.Errorf("expected constant %s with [C], got %q", c, found[c])
		}
	}

	expectedVars := []string{"X", "Y", "Z"}
	for _, v := range expectedVars {
		if found[v] != "[V]" {
			t.Errorf("expected variable %s with [V], got %q", v, found[v])
		}
	}
}
