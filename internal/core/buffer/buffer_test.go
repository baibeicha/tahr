package buffer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBufferCreationAndBasicProps(t *testing.T) {
	b := NewBuffer()
	if b.TotalBytes() != 0 {
		t.Fatalf("expected 0 bytes, got %d", b.TotalBytes())
	}
	if b.TotalLines() != 1 {
		t.Fatalf("expected 1 line, got %d", b.TotalLines())
	}
	if b.IsModified() {
		t.Fatalf("expected not modified initially")
	}

	sels := b.GetSelections()
	if len(sels) != 1 || !sels[0].IsEmpty() || sels[0].Head.Byte != 0 {
		t.Fatalf("expected single cursor at (0, 0, 0)")
	}

	// Buffer with text
	text := "Hello, Tahr!\nSecond line.\n"
	b2 := NewBufferWithText(text)
	if b2.TotalBytes() != len(text) {
		t.Fatalf("expected %d bytes, got %d", len(text), b2.TotalBytes())
	}
	if b2.TotalLines() != 3 {
		t.Fatalf("expected 3 lines, got %d", b2.TotalLines())
	}
	line0, err := b2.GetLine(0)
	if err != nil || string(line0) != "Hello, Tahr!\n" {
		t.Fatalf("GetLine(0) mismatch: %q, err=%v", line0, err)
	}
}

func TestBufferMultiCursorInsertBottomToTop(t *testing.T) {
	// Construct 10 lines of text
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(fmt.Sprintf("line %d: code statement;\n", i))
	}
	initialText := sb.String()
	b := NewBufferWithText(initialText)

	// Place 10 cursors, one at the start of each line
	cursors := make([]Selection, 10)
	for i := 0; i < 10; i++ {
		offset, err := b.ByteOffsetForLine(i)
		if err != nil {
			t.Fatalf("ByteOffsetForLine(%d) failed: %v", i, err)
		}
		cursors[i] = NewCursor(b.ByteToPosition(offset))
	}
	b.SetSelections(cursors)

	if len(b.GetSelections()) != 10 {
		t.Fatalf("expected 10 selections, got %d", len(b.GetSelections()))
	}

	// Insert comment prefix "// " across all 10 cursors simultaneously
	b.InsertAtSelections("// ")

	// Verify all 10 lines now begin with "// " and have zero coordinate drift
	for i := 0; i < 10; i++ {
		lineBytes, err := b.GetLine(i)
		if err != nil {
			t.Fatalf("GetLine(%d) failed: %v", i, err)
		}
		expectedPrefix := fmt.Sprintf("// line %d:", i)
		if !strings.HasPrefix(string(lineBytes), expectedPrefix) {
			t.Fatalf("line %d does not have expected prefix: %q", i, string(lineBytes))
		}
	}

	// Verify cursor positions: all should be at column 3 (after "// ")
	currentSels := b.GetSelections()
	if len(currentSels) != 10 {
		t.Fatalf("expected 10 cursors after insert, got %d", len(currentSels))
	}
	for i, s := range currentSels {
		if s.Head.Line != i || s.Head.Column != 3 {
			t.Fatalf("cursor %d drifted! Line=%d, Col=%d, Byte=%d", i, s.Head.Line, s.Head.Column, s.Head.Byte)
		}
	}

	// Now Undo: should restore exact original lines and 10 cursors at column 0!
	ok := b.Undo()
	if !ok {
		t.Fatalf("Undo failed")
	}

	for i := 0; i < 10; i++ {
		lineBytes, _ := b.GetLine(i)
		expectedPrefix := fmt.Sprintf("line %d:", i)
		if !strings.HasPrefix(string(lineBytes), expectedPrefix) {
			t.Fatalf("line %d after undo mismatch: %q", i, string(lineBytes))
		}
	}

	restoredSels := b.GetSelections()
	if len(restoredSels) != 10 {
		t.Fatalf("expected 10 restored cursors, got %d", len(restoredSels))
	}
	for i, s := range restoredSels {
		if s.Head.Line != i || s.Head.Column != 0 {
			t.Fatalf("restored cursor %d mismatch: Line=%d, Col=%d", i, s.Head.Line, s.Head.Column)
		}
	}

	// Redo: restores "// " on all 10 lines
	ok = b.Redo()
	if !ok {
		t.Fatalf("Redo failed")
	}
	for i := 0; i < 10; i++ {
		lineBytes, _ := b.GetLine(i)
		if !strings.HasPrefix(string(lineBytes), "// line ") {
			t.Fatalf("line %d after redo mismatch: %q", i, string(lineBytes))
		}
	}
}

func TestBufferMultiCursorDeleteAtSelections(t *testing.T) {
	initialText := "cat\ndog\nfox\n"
	b := NewBufferWithText(initialText)

	// Place cursor at the end of each word (col 3 on lines 0, 1, 2)
	c0 := NewCursor(b.ByteToPosition(3)) // after "cat"
	c1 := NewCursor(b.ByteToPosition(7)) // after "dog"
	c2 := NewCursor(b.ByteToPosition(11)) // after "fox"
	b.SetSelections([]Selection{c0, c1, c2})

	// Delete 1 character backward across all 3 cursors (backspace)
	b.DeleteAtSelections()

	l0, _ := b.GetLine(0)
	l1, _ := b.GetLine(1)
	l2, _ := b.GetLine(2)

	if string(l0) != "ca\n" || string(l1) != "do\n" || string(l2) != "fo\n" {
		t.Fatalf("DeleteAtSelections failed: %q, %q, %q", l0, l1, l2)
	}

	// Undo restores "cat\ndog\nfox\n"
	b.Undo()
	l0, _ = b.GetLine(0)
	l1, _ = b.GetLine(1)
	l2, _ = b.GetLine(2)
	if string(l0) != "cat\n" || string(l1) != "dog\n" || string(l2) != "fox\n" {
		t.Fatalf("Undo after backspace failed: %q, %q, %q", l0, l1, l2)
	}
}

func TestBufferCtrlDSelectNextOccurrence(t *testing.T) {
	text := "foo bar foo baz foo"
	b := NewBufferWithText(text)

	// Position cursor inside the first "foo" (byte offset 1)
	b.SetSelections([]Selection{NewCursor(b.ByteToPosition(1))})

	// 1st Ctrl+D: selects the word "foo" [0, 3)
	ok := b.SelectNextOccurrence()
	if !ok {
		t.Fatalf("1st Ctrl+D failed")
	}
	sels := b.GetSelections()
	if len(sels) != 1 || sels[0].Start().Byte != 0 || sels[0].End().Byte != 3 {
		t.Fatalf("1st Ctrl+D selection mismatch: %+v", sels)
	}

	// 2nd Ctrl+D: selects second "foo" [8, 11)
	ok = b.SelectNextOccurrence()
	if !ok {
		t.Fatalf("2nd Ctrl+D failed")
	}
	sels = b.GetSelections()
	if len(sels) != 2 || sels[1].Start().Byte != 8 || sels[1].End().Byte != 11 {
		t.Fatalf("2nd Ctrl+D selection mismatch: %+v", sels)
	}

	// 3rd Ctrl+D: selects third "foo" [16, 19)
	ok = b.SelectNextOccurrence()
	if !ok {
		t.Fatalf("3rd Ctrl+D failed")
	}
	sels = b.GetSelections()
	if len(sels) != 3 || sels[2].Start().Byte != 16 || sels[2].End().Byte != 19 {
		t.Fatalf("3rd Ctrl+D selection mismatch: %+v", sels)
	}

	// 4th Ctrl+D: all occurrences already selected, returns false
	ok = b.SelectNextOccurrence()
	if ok {
		t.Fatalf("expected 4th Ctrl+D to return false (all selected)")
	}

	// Replace all "foo" with "qux"
	b.InsertAtSelections("qux")

	res, _ := b.Slice(0, b.TotalBytes())
	if string(res) != "qux bar qux baz qux" {
		t.Fatalf("multi-cursor replace after Ctrl+D failed: %q", string(res))
	}
}

func TestBufferAltClickAddAndToggleCursor(t *testing.T) {
	text := "0123456789\n"
	b := NewBufferWithText(text)

	// Add cursor at byte 2
	b.AddCursor(b.ByteToPosition(2))
	if len(b.GetSelections()) != 2 {
		t.Fatalf("expected 2 cursors, got %d", len(b.GetSelections()))
	}

	// Add cursor at byte 5
	b.AddCursor(b.ByteToPosition(5))
	if len(b.GetSelections()) != 3 {
		t.Fatalf("expected 3 cursors, got %d", len(b.GetSelections()))
	}

	// Toggle cursor at byte 2 -> should remove it
	b.ToggleCursor(b.ByteToPosition(2))
	if len(b.GetSelections()) != 2 {
		t.Fatalf("expected 2 cursors after toggle removal, got %d", len(b.GetSelections()))
	}

	// Verify remaining cursors are at 0 and 5
	sels := b.GetSelections()
	if sels[0].Head.Byte != 0 || sels[1].Head.Byte != 5 {
		t.Fatalf("remaining cursors mismatch: %+v", sels)
	}
}

func TestBufferUndoRedoWordBatching(t *testing.T) {
	b := NewBuffer()

	// Type "cat"
	b.InsertAtSelections("c")
	b.InsertAtSelections("a")
	b.InsertAtSelections("t")

	// Type space
	b.InsertAtSelections(" ")

	// Type "dog"
	b.InsertAtSelections("d")
	b.InsertAtSelections("o")
	b.InsertAtSelections("g")

	content, _ := b.Slice(0, b.TotalBytes())
	if string(content) != "cat dog" {
		t.Fatalf("expected 'cat dog', got %q", string(content))
	}

	// 1st Undo: removes "dog"
	b.Undo()
	c1, _ := b.Slice(0, b.TotalBytes())
	if string(c1) != "cat " {
		t.Fatalf("1st undo: expected 'cat ', got %q", string(c1))
	}

	// 2nd Undo: removes " "
	b.Undo()
	c2, _ := b.Slice(0, b.TotalBytes())
	if string(c2) != "cat" {
		t.Fatalf("2nd undo: expected 'cat', got %q", string(c2))
	}

	// 3rd Undo: removes "cat"
	b.Undo()
	c3, _ := b.Slice(0, b.TotalBytes())
	if string(c3) != "" {
		t.Fatalf("3rd undo: expected '', got %q", string(c3))
	}

	// Redo back to "cat dog"
	b.Redo()
	r1, _ := b.Slice(0, b.TotalBytes())
	if string(r1) != "cat" {
		t.Fatalf("1st redo: expected 'cat', got %q", string(r1))
	}

	b.Redo()
	r2, _ := b.Slice(0, b.TotalBytes())
	if string(r2) != "cat " {
		t.Fatalf("2nd redo: expected 'cat ', got %q", string(r2))
	}

	b.Redo()
	r3, _ := b.Slice(0, b.TotalBytes())
	if string(r3) != "cat dog" {
		t.Fatalf("3rd redo: expected 'cat dog', got %q", string(r3))
	}
}

func TestBufferSaveAndLoad(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_buffer_save_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	filePath := filepath.Join(dir, "source.go")
	code := "package main\n\nfunc main() {\n\tprintln(\"Hello\")\n}\n"

	b := NewBufferWithText(code)
	b.SetFilePath(filePath)

	err = b.Save()
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if b.IsModified() {
		t.Fatalf("expected buffer clean after Save")
	}

	// Load into a fresh buffer
	bLoaded := NewBuffer()
	err = bLoaded.Load(filePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if bLoaded.IsModified() {
		t.Fatalf("expected loaded buffer clean")
	}

	loadedBytes, err := bLoaded.Slice(0, bLoaded.TotalBytes())
	if err != nil || string(loadedBytes) != code {
		t.Fatalf("loaded text does not match: %q, err=%v", string(loadedBytes), err)
	}
}
