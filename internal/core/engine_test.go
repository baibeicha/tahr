package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tahr/internal/core/buffer"
)

func TestEngine_DocumentLifecycle(t *testing.T) {
	eng := NewEngine()

	// Initially 1 untitled document
	if len(eng.Documents()) != 1 {
		t.Fatalf("expected 1 document initially, got %d", len(eng.Documents()))
	}
	active := eng.ActiveDocument()
	if active == nil {
		t.Fatal("expected active document to be non-nil")
	}

	// Create another untitled
	doc2 := eng.NewUntitled()
	if len(eng.Documents()) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(eng.Documents()))
	}
	if eng.ActiveDocument().ID != doc2.ID {
		t.Fatalf("expected active document to be doc2 (%s), got %s", doc2.ID, eng.ActiveDocument().ID)
	}

	// Switch buffer
	err := eng.SwitchBuffer(active.ID)
	if err != nil {
		t.Fatalf("failed to switch buffer: %v", err)
	}
	if eng.ActiveDocument().ID != active.ID {
		t.Fatalf("expected active document to be %s, got %s", active.ID, eng.ActiveDocument().ID)
	}

	// Next and Prev buffer
	eng.NextBuffer()
	if eng.ActiveDocument().ID != doc2.ID {
		t.Fatalf("expected next buffer to be %s, got %s", doc2.ID, eng.ActiveDocument().ID)
	}
	eng.PrevBuffer()
	if eng.ActiveDocument().ID != active.ID {
		t.Fatalf("expected prev buffer to be %s, got %s", active.ID, eng.ActiveDocument().ID)
	}

	// Close buffer
	eng.CloseBuffer(active.ID)
	if len(eng.Documents()) != 1 {
		t.Fatalf("expected 1 document after close, got %d", len(eng.Documents()))
	}
	if eng.ActiveDocument().ID != doc2.ID {
		t.Fatalf("expected active document to be %s, got %s", doc2.ID, eng.ActiveDocument().ID)
	}

	// Close the only remaining buffer - should auto-create untitled
	eng.CloseBuffer(doc2.ID)
	if len(eng.Documents()) != 1 {
		t.Fatalf("expected 1 document after closing last buffer, got %d", len(eng.Documents()))
	}
	if eng.ActiveDocument() == nil {
		t.Fatal("expected non-nil active document after closing last buffer")
	}
}

func TestEngine_OpenExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	content := "Hello world\nSecond line\nThird line\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine()
	doc, err := eng.Open(filePath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if doc.Buffer.TotalLines() < 3 {
		t.Fatalf("expected >= 3 lines, got %d", doc.Buffer.TotalLines())
	}

	// Re-opening same path returns existing document
	docAgain, err := eng.Open(filePath)
	if err != nil {
		t.Fatalf("Re-open failed: %v", err)
	}
	if docAgain.ID != doc.ID {
		t.Fatalf("expected same doc ID %s, got %s", doc.ID, docAgain.ID)
	}
}

func TestEngine_ViewportMath(t *testing.T) {
	vp := Viewport{
		ViewportX:   0,
		ViewportY:   0,
		Width:       80,
		Height:      24,
		GutterWidth: 5,
		ScrolloffX:  2,
		ScrolloffY:  2,
	}

	if vp.ContentWidth() != 75 {
		t.Fatalf("expected ContentWidth 75, got %d", vp.ContentWidth())
	}

	// Test ComputeGutterWidth
	if w := ComputeGutterWidth(10); w != 5 {
		t.Fatalf("expected gutter width 5 for 10 lines, got %d", w)
	}
	if w := ComputeGutterWidth(1000); w != 6 { // 4 digits + 2 = 6
		t.Fatalf("expected gutter width 6 for 1000 lines, got %d", w)
	}
	if w := ComputeGutterWidth(100000); w != 8 { // 6 digits + 2 = 8
		t.Fatalf("expected gutter width 8 for 100k lines, got %d", w)
	}

	// Test ScrollToCursor
	buf := buffer.NewBufferWithText("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n21\n22\n23\n24\n25\n26\n27\n28\n29\n30\n")
	vp.Height = 10
	vp.GutterWidth = 5

	// Move cursor to line 20
	line20Byte, _ := buf.ByteOffsetForLine(20)
	pos20 := buf.ByteToPosition(line20Byte)
	buf.SetSelections([]buffer.Selection{buffer.NewCursor(pos20)})

	vp.ScrollToCursor(buf)
	if vp.ViewportY == 0 {
		t.Fatal("expected ViewportY to scroll down past 0")
	}
	// Cursor line 20 should be visible: vp.ViewportY <= 20 and 20 < vp.ViewportY + 10
	if 20 < vp.ViewportY || 20 >= vp.ViewportY+vp.Height {
		t.Fatalf("cursor line 20 not in visible range [%d, %d)", vp.ViewportY, vp.ViewportY+vp.Height)
	}
}

func TestEngine_CommandDispatch_Editing(t *testing.T) {
	eng := NewEngine()

	// Insert text
	if err := eng.Dispatch(Command{ID: CmdInsertText, Args: "line1\nline2"}); err != nil {
		t.Fatalf("CmdInsertText failed: %v", err)
	}
	doc := eng.ActiveDocument()
	if doc.Buffer.TotalLines() != 2 {
		t.Fatalf("expected 2 lines, got %d", doc.Buffer.TotalLines())
	}

	// Insert newline
	if err := eng.Dispatch(Command{ID: CmdInsertNewline}); err != nil {
		t.Fatalf("CmdInsertNewline failed: %v", err)
	}
	if doc.Buffer.TotalLines() != 3 {
		t.Fatalf("expected 3 lines, got %d", doc.Buffer.TotalLines())
	}

	// Delete backward
	if err := eng.Dispatch(Command{ID: CmdDeleteBackward}); err != nil {
		t.Fatalf("CmdDeleteBackward failed: %v", err)
	}
	if doc.Buffer.TotalLines() != 2 {
		t.Fatalf("expected 2 lines after backspace, got %d", doc.Buffer.TotalLines())
	}

	// Undo / Redo
	if err := eng.Dispatch(Command{ID: CmdUndo}); err != nil {
		t.Fatalf("CmdUndo failed: %v", err)
	}
	if err := eng.Dispatch(Command{ID: CmdRedo}); err != nil {
		t.Fatalf("CmdRedo failed: %v", err)
	}
}

func TestEngine_CommandDispatch_BlockIndentDedent(t *testing.T) {
	eng := NewEngine()
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "func main() {\nx := 1\ny := 2\n}"})

	doc := eng.ActiveDocument()
	// Select lines 1 to 2
	line1Byte, _ := doc.Buffer.ByteOffsetForLine(1)
	line2Byte, _ := doc.Buffer.ByteOffsetForLine(2)
	line2Bytes, _ := doc.Buffer.GetLine(2)
	sel := buffer.NewSelection(doc.Buffer.ByteToPosition(line1Byte), doc.Buffer.ByteToPosition(line2Byte+len(line2Bytes)))
	doc.Buffer.SetSelections([]buffer.Selection{sel})

	// Indent block
	if err := eng.Dispatch(Command{ID: CmdIndent}); err != nil {
		t.Fatalf("CmdIndent failed: %v", err)
	}

	l1, _ := doc.Buffer.GetLine(1)
	if strings.TrimRight(string(l1), "\r\n") != "    x := 1" {
		t.Fatalf("expected '    x := 1', got %q", string(l1))
	}

	// Dedent block
	if err := eng.Dispatch(Command{ID: CmdDedent}); err != nil {
		t.Fatalf("CmdDedent failed: %v", err)
	}

	l1After, _ := doc.Buffer.GetLine(1)
	if strings.TrimRight(string(l1After), "\r\n") != "x := 1" {
		t.Fatalf("expected 'x := 1', got %q", string(l1After))
	}
}

func TestEngine_CommandDispatch_Navigation(t *testing.T) {
	eng := NewEngine()
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "Hello World\nSecond Line"})

	doc := eng.ActiveDocument()
	// Move to start of document
	_ = eng.Dispatch(Command{ID: CmdCursorDocStart})
	if doc.Buffer.PrimarySelection().Head.Byte != 0 {
		t.Fatalf("expected cursor at byte 0, got %d", doc.Buffer.PrimarySelection().Head.Byte)
	}

	// Move right 5 times
	for i := 0; i < 5; i++ {
		_ = eng.Dispatch(Command{ID: CmdCursorRight})
	}
	if doc.Buffer.PrimarySelection().Head.Column != 5 {
		t.Fatalf("expected col 5, got %d", doc.Buffer.PrimarySelection().Head.Column)
	}

	// Move down
	_ = eng.Dispatch(Command{ID: CmdCursorDown})
	if doc.Buffer.PrimarySelection().Head.Line != 1 {
		t.Fatalf("expected line 1, got %d", doc.Buffer.PrimarySelection().Head.Line)
	}

	// Move line end
	_ = eng.Dispatch(Command{ID: CmdCursorLineEnd})
	lineBytes, _ := doc.Buffer.GetLine(1)
	if doc.Buffer.PrimarySelection().Head.Column != len(lineBytes) {
		t.Fatalf("expected col %d at line end, got %d", len(lineBytes), doc.Buffer.PrimarySelection().Head.Column)
	}
}

func TestEngine_BreakpointsAndDiagnostics(t *testing.T) {
	eng := NewEngine()
	doc := eng.ActiveDocument()
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "line0\nline1\nline2\n"})

	// Toggle breakpoint on line 1
	if err := eng.Dispatch(Command{ID: CmdToggleBreakpoint, Args: 1}); err != nil {
		t.Fatalf("CmdToggleBreakpoint failed: %v", err)
	}
	if !doc.HasBreakpoint(1) {
		t.Fatal("expected breakpoint on line 1")
	}

	// Toggle off
	_ = eng.Dispatch(Command{ID: CmdToggleBreakpoint, Args: 1})
	if doc.HasBreakpoint(1) {
		t.Fatal("expected breakpoint removed from line 1")
	}

	// Diagnostics
	doc.SetDiagnostic(2, "syntax error")
	if doc.Diagnostics[2] != "syntax error" {
		t.Fatalf("expected diagnostic 'syntax error', got %q", doc.Diagnostics[2])
	}
	doc.ClearDiagnostics()
	if len(doc.Diagnostics) != 0 {
		t.Fatal("expected diagnostics cleared")
	}
}

func TestEngine_AtomicSaveCommand(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "saved.txt")

	eng := NewEngine()
	_ = eng.Dispatch(Command{ID: CmdInsertText, Args: "Persisted content"})

	if err := eng.Dispatch(Command{ID: CmdFileSave, Args: targetPath}); err != nil {
		t.Fatalf("CmdFileSave failed: %v", err)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(data) != "Persisted content" {
		t.Fatalf("expected 'Persisted content', got %q", string(data))
	}
}

func TestEngine_ConcurrentAccess(t *testing.T) {
	eng := NewEngine()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = eng.ActiveDocument()
			_ = eng.Documents()
			_ = eng.Width()
			_ = eng.Height()
			_ = eng.StatusMessage()
			_ = eng.Dispatch(Command{ID: CmdInsertChar, Args: 'a'})
		}(i)
	}
	wg.Wait()

	if eng.ActiveDocument().Buffer.TotalBytes() != 20 {
		t.Fatalf("expected 20 bytes from 20 concurrent inserts, got %d", eng.ActiveDocument().Buffer.TotalBytes())
	}
}
