package buffer

import (
	"strings"
	"testing"
	"time"
)

func TestDeltaInvertibility(t *testing.T) {
	dIns := TextDelta{Kind: DeltaInsert, Offset: 10, Text: "hello"}
	invIns := dIns.Invert()
	if invIns.Kind != DeltaDelete || invIns.Offset != 10 || invIns.Text != "hello" {
		t.Fatalf("Invert(Insert) failed: %+v", invIns)
	}

	dDel := TextDelta{Kind: DeltaDelete, Offset: 20, Text: "world"}
	invDel := dDel.Invert()
	if invDel.Kind != DeltaInsert || invDel.Offset != 20 || invDel.Text != "world" {
		t.Fatalf("Invert(Delete) failed: %+v", invDel)
	}

	// Double inversion identity
	if invIns.Invert() != dIns {
		t.Fatalf("Double inversion identity failed for insert")
	}
	if invDel.Invert() != dDel {
		t.Fatalf("Double inversion identity failed for delete")
	}
}

func TestSequenceInversionLaw(t *testing.T) {
	// Start with text
	text := "The quick brown fox jumps"
	r := NewRope()
	_ = r.Insert(0, []byte(text))

	// Apply 3 successive edits
	deltas := []TextDelta{
		{Kind: DeltaInsert, Offset: 4, Text: "very "}, // "The very quick brown fox jumps"
		{Kind: DeltaDelete, Offset: 15, Text: "brown "}, // "The very quick fox jumps"
		{Kind: DeltaInsert, Offset: 23, Text: " high"},  // "The very quick fox jumps high"
	}

	for _, d := range deltas {
		switch d.Kind {
		case DeltaInsert:
			_ = r.Insert(d.Offset, []byte(d.Text))
		case DeltaDelete:
			_ = r.Delete(d.Offset, len(d.Text))
		}
	}

	// Now apply Sequence Inversion Law: apply inverse deltas in REVERSE order
	for i := len(deltas) - 1; i >= 0; i-- {
		inv := deltas[i].Invert()
		switch inv.Kind {
		case DeltaInsert:
			_ = r.Insert(inv.Offset, []byte(inv.Text))
		case DeltaDelete:
			_ = r.Delete(inv.Offset, len(inv.Text))
		}
	}

	res, _ := r.Slice(0, r.TotalBytes())
	if string(res) != text {
		t.Fatalf("Sequence Inversion Law failed! Got %q, expected %q", string(res), text)
	}
}

func TestWordBatchingEngine(t *testing.T) {
	h := NewHistory()

	p0 := Position{Line: 0, Column: 0, Byte: 0}
	p1 := Position{Line: 0, Column: 1, Byte: 1}
	p2 := Position{Line: 0, Column: 2, Byte: 2}

	// 1. Typing alphanumeric letters 'c', 'a', 't'
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 0, Text: "c"}},
		[]Selection{NewCursor(p0)}, []Selection{NewCursor(p1)})
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 1, Text: "a"}},
		[]Selection{NewCursor(p1)}, []Selection{NewCursor(p2)})
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 2, Text: "t"}},
		[]Selection{NewCursor(p2)}, []Selection{NewCursor(Position{Byte: 3})})

	// Before commit, all 3 are in the active batch
	if h.UndoCount() != 0 {
		t.Fatalf("expected active batch not yet in undo stack, UndoCount = %d", h.UndoCount())
	}

	// 2. Typing space ' ' transitions from CatWord -> CatWhitespace
	pSpace := Position{Byte: 4}
	h.RecordKeystroke(CatWhitespace, false, []TextDelta{{Kind: DeltaInsert, Offset: 3, Text: " "}},
		[]Selection{NewCursor(Position{Byte: 3})}, []Selection{NewCursor(pSpace)})

	// Now "cat" should have committed to undo stack!
	if h.UndoCount() != 1 {
		t.Fatalf("expected 'cat' batch committed, UndoCount = %d", h.UndoCount())
	}

	// 3. Typing newline commits immediately
	pNL := Position{Byte: 5}
	h.RecordKeystroke(CatNewline, false, []TextDelta{{Kind: DeltaInsert, Offset: 4, Text: "\n"}},
		[]Selection{NewCursor(pSpace)}, []Selection{NewCursor(pNL)})

	// Both whitespace and newline committed
	if h.UndoCount() != 3 {
		t.Fatalf("expected 3 transactions in undo stack, got %d", h.UndoCount())
	}
}

func TestWordBatchingCursorJump(t *testing.T) {
	h := NewHistory()
	p0 := Position{Byte: 0}
	p1 := Position{Byte: 1}

	// Type 'a'
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 0, Text: "a"}},
		[]Selection{NewCursor(p0)}, []Selection{NewCursor(p1)})

	// Cursor jumps to byte 50 without editing
	pJump := Position{Byte: 50}
	// Type 'b' at byte 50
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 50, Text: "b"}},
		[]Selection{NewCursor(pJump)}, []Selection{NewCursor(Position{Byte: 51})})

	// Jump should have committed the 'a' transaction
	if h.UndoCount() != 1 {
		t.Fatalf("expected cursor jump to commit previous batch, UndoCount = %d", h.UndoCount())
	}
}

func TestWordBatchingInactivityTimeout(t *testing.T) {
	h := NewHistory()
	h.inactivityDur = 10 * time.Millisecond // Short duration for test

	p0 := Position{Byte: 0}
	p1 := Position{Byte: 1}

	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 0, Text: "a"}},
		[]Selection{NewCursor(p0)}, []Selection{NewCursor(p1)})

	time.Sleep(20 * time.Millisecond)

	// Next keystroke should commit due to timeout
	h.RecordKeystroke(CatWord, false, []TextDelta{{Kind: DeltaInsert, Offset: 1, Text: "b"}},
		[]Selection{NewCursor(p1)}, []Selection{NewCursor(Position{Byte: 2})})

	if h.UndoCount() != 1 {
		t.Fatalf("expected inactivity timeout to commit batch, UndoCount = %d", h.UndoCount())
	}
}

func TestHistoryUndoRedoSelectionRestoration(t *testing.T) {
	h := NewHistory()
	buf := []byte("Initial Text")

	applier := func(d TextDelta) error {
		switch d.Kind {
		case DeltaInsert:
			buf = append(buf[:d.Offset], append([]byte(d.Text), buf[d.Offset:]...)...)
		case DeltaDelete:
			buf = append(buf[:d.Offset], buf[d.Offset+len(d.Text):]...)
		}
		return nil
	}

	var currentSelections []Selection
	restorer := func(sels []Selection) {
		currentSelections = sels
	}

	// 2 selections: [0, 7) and [8, 12)
	initialSels := []Selection{
		NewSelection(Position{Byte: 0}, Position{Byte: 7}),
		NewSelection(Position{Byte: 8}, Position{Byte: 12}),
	}
	currentSelections = initialSels

	// Replace with "ABC"
	afterSels := []Selection{
		NewCursor(Position{Byte: 3}),
		NewCursor(Position{Byte: 7}),
	}
	deltas := []TextDelta{
		{Kind: DeltaDelete, Offset: 8, Text: "Text"},
		{Kind: DeltaInsert, Offset: 8, Text: "ABC"},
		{Kind: DeltaDelete, Offset: 0, Text: "Initial"},
		{Kind: DeltaInsert, Offset: 0, Text: "ABC"},
	}

	h.RecordEdit(deltas, initialSels, afterSels)

	if !h.CanUndo() {
		t.Fatalf("expected CanUndo = true")
	}

	// Undo
	ok := h.Undo(applier, restorer)
	if !ok {
		t.Fatalf("Undo failed")
	}

	// Verify selections restored exactly
	if len(currentSelections) != 2 {
		t.Fatalf("expected 2 selections restored, got %d", len(currentSelections))
	}
	if currentSelections[0].Start().Byte != 0 || currentSelections[0].End().Byte != 7 {
		t.Fatalf("selection 0 not restored: %+v", currentSelections[0])
	}
	if currentSelections[1].Start().Byte != 8 || currentSelections[1].End().Byte != 12 {
		t.Fatalf("selection 1 not restored: %+v", currentSelections[1])
	}

	// Redo
	if !h.CanRedo() {
		t.Fatalf("expected CanRedo = true")
	}
	ok = h.Redo(applier, restorer)
	if !ok {
		t.Fatalf("Redo failed")
	}

	if len(currentSelections) != 2 || currentSelections[0].Head.Byte != 3 || currentSelections[1].Head.Byte != 7 {
		t.Fatalf("Redo selections not restored: %+v", currentSelections)
	}
}

func TestHistoryMaxUndoLimit(t *testing.T) {
	h := NewHistory()
	h.maxUndoLimit = 5

	for i := 0; i < 10; i++ {
		h.RecordEdit([]TextDelta{{Kind: DeltaInsert, Offset: i, Text: "x"}},
			[]Selection{NewCursor(Position{Byte: i})},
			[]Selection{NewCursor(Position{Byte: i + 1})})
	}

	if h.UndoCount() != 5 {
		t.Fatalf("expected 5 transactions due to maxUndoLimit, got %d", h.UndoCount())
	}
}

func TestHistoryIsModified(t *testing.T) {
	h := NewHistory()
	h.MarkSaved()
	if h.IsModified() {
		t.Fatalf("expected clean state initially")
	}

	// Perform edit
	h.RecordEdit([]TextDelta{{Kind: DeltaInsert, Offset: 0, Text: "a"}},
		[]Selection{NewCursor(Position{Byte: 0})},
		[]Selection{NewCursor(Position{Byte: 1})})

	if !h.IsModified() {
		t.Fatalf("expected modified state after edit")
	}

	// Undo back to saved state
	dummyApplier := func(d TextDelta) error { return nil }
	dummyRestorer := func(sels []Selection) {}

	h.Undo(dummyApplier, dummyRestorer)

	if h.IsModified() {
		t.Fatalf("expected clean state after undoing back to saved state")
	}
}

func init() {
	// Silence unused warnings
	_ = strings.Clone
}
