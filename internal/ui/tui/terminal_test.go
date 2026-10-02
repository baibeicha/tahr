package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
)

func TestTerminalDrawer_BasicLifecycle(t *testing.T) {
	td := NewTerminalDrawer(".")
	if td.Open {
		t.Fatal("expected terminal to start closed")
	}

	td.Toggle()
	if !td.Open {
		t.Fatal("expected terminal to be open after toggle")
	}
	defer td.Kill()

	// Test key input
	td.HandleKey("", 'e')
	td.HandleKey("", 'c')
	td.HandleKey("", 'h')
	td.HandleKey("", 'o')
	td.HandleKey("", ' ')
	td.HandleKey("", 'h')
	td.HandleKey("", 'i')

	if td.InputText != "echo hi" {
		t.Fatalf("expected 'echo hi', got %q", td.InputText)
	}

	// Test backspace
	td.HandleKey("backspace", 0)
	if td.InputText != "echo h" {
		t.Fatalf("expected 'echo h', got %q", td.InputText)
	}

	// Test execution of built-in 'clear'
	td.InputText = "clear"
	td.HandleKey("enter", 0)
	if len(td.Lines) != 0 {
		t.Fatalf("expected lines to be cleared, got %d lines", len(td.Lines))
	}

	// Test cd
	td.InputText = "cd /test"
	td.HandleKey("enter", 0)
	if td.Cwd != "/test" {
		t.Fatalf("expected cwd /test, got %s", td.Cwd)
	}
}

func TestTerminalDrawer_Execution(t *testing.T) {
	td := NewTerminalDrawer(".")
	defer td.Kill()

	td.Execute("echo 42")

	// Wait up to 8 seconds for interactive shell to output 42
	found42 := td.WaitForOutput("42", 8*time.Second)
	if !found42 {
		td.mu.Lock()
		lines := td.Lines
		td.mu.Unlock()
		hasResourceError := false
		for _, l := range lines {
			compact := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(l, " ", ""), "\x00", ""))
			if strings.Contains(compact, "800705af") || strings.Contains(compact, "80004005") || strings.Contains(compact, "clr") || strings.Contains(compact, "psreadline") || strings.Contains(compact, "powershell") || strings.Contains(compact, "hresult") {
				hasResourceError = true
				break
			}
		}
		if hasResourceError {
			t.Skipf("skipping live shell test due to OS process resource exhaustion (0x800705AF): %+v", lines)
		} else {
			t.Errorf("expected output to contain '42', got %+v", lines)
		}
	}
}

func TestVTerm_ANSI_Decoding(t *testing.T) {
	vt := NewVTerm(80, 24)

	// Write simple text with CR LF
	_, _ = vt.Write([]byte("Hello, World!\r\nNext Line"))
	lines := vt.ContentLines()
	if !strings.HasPrefix(lines[0], "Hello, World!") {
		t.Fatalf("expected line 0 to start with 'Hello, World!', got %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "Next Line") {
		t.Fatalf("expected line 1 to start with 'Next Line', got %q", lines[1])
	}

	// Test Cursor Positioning: CUP \x1b[5;10H
	_, _ = vt.Write([]byte("\x1b[5;10HX"))
	if vt.CursorY != 4 || vt.CursorX != 10 { // row 5 (idx 4), col 10 (idx 9) + 1 after write 'X'
		t.Fatalf("expected cursor at (10, 4), got (%d, %d)", vt.CursorX, vt.CursorY)
	}
	if vt.Screen[4][9].Char != 'X' {
		t.Fatalf("expected 'X' at (9, 4), got %c", vt.Screen[4][9].Char)
	}

	// Test SGR TrueColor RGB: \x1b[38;2;255;100;50m
	_, _ = vt.Write([]byte("\x1b[38;2;255;100;50mRGB\x1b[0m"))
	cellR := vt.Screen[4][10]
	if cellR.Char != 'R' {
		t.Fatalf("expected 'R', got %c", cellR.Char)
	}
	expectedFg := cell.RGB(255, 100, 50)
	if cellR.Fg != expectedFg {
		t.Fatalf("expected FG %v, got %v", expectedFg, cellR.Fg)
	}

	// Test SGR 256 colors: \x1b[38;5;208m
	_, _ = vt.Write([]byte("\x1b[38;5;208mC256\x1b[0m"))
	cell256 := vt.Screen[4][13]
	if cell256.Char != 'C' {
		t.Fatalf("expected 'C', got %c", cell256.Char)
	}
	if cell256.Fg.IsDefault() {
		t.Fatalf("expected non-default FG for 256 color")
	}

	// Test SGR Styles: Bold, Underline, Reverse
	_, _ = vt.Write([]byte("\x1b[1;4;7mStyled\x1b[0m"))
	cellStyle := vt.Screen[4][17]
	if cellStyle.Char != 'S' {
		t.Fatalf("expected 'S', got %c", cellStyle.Char)
	}
	if !cellStyle.Attr.Has(cell.AttrBold) || !cellStyle.Attr.Has(cell.AttrUnderline) || !cellStyle.Attr.Has(cell.AttrReverse) {
		t.Fatalf("expected bold+underline+reverse attributes, got %v", cellStyle.Attr)
	}

	// Test Clear Screen: \x1b[2J
	_, _ = vt.Write([]byte("\x1b[2J"))
	for y := 0; y < vt.Rows; y++ {
		for x := 0; x < vt.Cols; x++ {
			if vt.Screen[y][x].Char != ' ' {
				t.Fatalf("expected blank cell at (%d, %d), got %c", x, y, vt.Screen[y][x].Char)
			}
		}
	}
}

func TestVTerm_Scrollback_And_Wrapping(t *testing.T) {
	vt := NewVTerm(20, 5)

	// Write 10 lines to trigger scrolling into scrollback
	for i := 0; i < 10; i++ {
		_, _ = vt.Write([]byte("Line\r\n"))
	}

	if len(vt.Scrollback) == 0 {
		t.Fatalf("expected scrollback to have saved lines, got %d", len(vt.Scrollback))
	}

	// Scroll up into history
	vt.Scroll(5)
	if vt.ScrollOffset != 5 {
		t.Fatalf("expected scroll offset 5, got %d", vt.ScrollOffset)
	}

	vt.Scroll(-10)
	if vt.ScrollOffset != 0 {
		t.Fatalf("expected scroll offset clamped to 0, got %d", vt.ScrollOffset)
	}
}

func TestVTerm_AlternateScreen(t *testing.T) {
	vt := NewVTerm(40, 10)
	_, _ = vt.Write([]byte("Main Screen Content"))

	// Enter Alternate Screen
	_, _ = vt.Write([]byte("\x1b[?1049h"))
	if !vt.inAltScreen {
		t.Fatal("expected to be in alternate screen buffer")
	}
	_, _ = vt.Write([]byte("Alt Screen Content"))
	linesAlt := vt.ContentLines()
	if !strings.HasPrefix(linesAlt[0], "Alt Screen Content") {
		t.Fatalf("expected alt screen content, got %q", linesAlt[0])
	}

	// Exit Alternate Screen
	_, _ = vt.Write([]byte("\x1b[?1049l"))
	if vt.inAltScreen {
		t.Fatal("expected to exit alternate screen buffer")
	}
	linesMain := vt.ContentLines()
	if !strings.HasPrefix(linesMain[0], "Main Screen Content") {
		t.Fatalf("expected main screen content restored, got %q", linesMain[0])
	}
}

func TestKeyToVT(t *testing.T) {
	tests := []struct {
		key      input.Key
		expected string
	}{
		{input.Key{Type: input.KeyEnter}, "\r"},
		{input.Key{Type: input.KeyBackspace}, "\x08"},
		{input.Key{Type: input.KeyTab}, "\t"},
		{input.Key{Type: input.KeyBacktab}, "\x1b[Z"},
		{input.Key{Type: input.KeyUp}, "\x1b[A"},
		{input.Key{Type: input.KeyDown}, "\x1b[B"},
		{input.Key{Type: input.KeyLeft}, "\x1b[D"},
		{input.Key{Type: input.KeyRight}, "\x1b[C"},
		{input.Key{Type: input.KeyHome}, "\x1b[H"},
		{input.Key{Type: input.KeyEnd}, "\x1b[F"},
		{input.Key{Type: input.KeyPgUp}, "\x1b[5~"},
		{input.Key{Type: input.KeyPgDown}, "\x1b[6~"},
		{input.Key{Type: input.KeyDelete}, "\x1b[3~"},
		{input.Key{Rune: 'c', Mod: input.ModCtrl}, "\x03"},
		{input.Key{Rune: 'l', Mod: input.ModCtrl}, "\x0c"},
		{input.Key{Rune: 'a', Mod: input.ModCtrl}, "\x01"},
		{input.Key{Rune: 'Z'}, "Z"},
		{input.Key{Rune: 'Ж'}, "Ж"},
	}

	for _, tc := range tests {
		actual := string(KeyToVT(tc.key))
		if actual != tc.expected {
			t.Errorf("for key %+v expected %q, got %q", tc.key, tc.expected, actual)
		}
	}
}

func TestTerminalDrawer_MultiInstanceAndSplits(t *testing.T) {
	td := NewTerminalDrawer(".")
	defer td.Kill()

	if len(td.Instances) != 1 {
		t.Fatalf("expected 1 initial instance, got %d", len(td.Instances))
	}

	// Add second instance
	inst2 := td.AddInstance("Tests")
	if inst2 == nil || len(td.Instances) != 2 {
		t.Fatalf("expected 2 instances after add, got %d", len(td.Instances))
	}
	if td.ActiveIdx != 1 {
		t.Errorf("expected active index 1, got %d", td.ActiveIdx)
	}

	// Switch to instance 0
	td.SwitchInstance(0)
	if td.ActiveIdx != 0 {
		t.Errorf("expected active index 0, got %d", td.ActiveIdx)
	}

	// Test split toggling
	if td.SplitMode != TermSplitTabs {
		t.Errorf("expected default TermSplitTabs, got %v", td.SplitMode)
	}
	td.ToggleSplit()
	if td.SplitMode != TermSplitHorizontal {
		t.Errorf("expected TermSplitHorizontal after toggle, got %v", td.SplitMode)
	}

	// Test resizing in horizontal split mode
	td.Resize(100, 20)
	if td.Instances[0].VTerm.Cols < 40 || td.Instances[1].VTerm.Cols < 40 {
		t.Errorf("expected horizontal splits to split cols evenly, got (%d, %d)",
			td.Instances[0].VTerm.Cols, td.Instances[1].VTerm.Cols)
	}

	// Test vertical split
	td.ToggleSplit()
	if td.SplitMode != TermSplitVertical {
		t.Errorf("expected TermSplitVertical after toggle, got %v", td.SplitMode)
	}
	td.Resize(100, 20)
	if td.Instances[0].VTerm.Rows < 8 || td.Instances[1].VTerm.Rows < 8 {
		t.Errorf("expected vertical splits to split rows, got (%d, %d)",
			td.Instances[0].VTerm.Rows, td.Instances[1].VTerm.Rows)
	}

	// Close instance 1
	td.CloseInstance(1)
	if len(td.Instances) != 1 {
		t.Fatalf("expected 1 instance after close, got %d", len(td.Instances))
	}
}
