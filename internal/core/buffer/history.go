// Package buffer provides a transactional Command-based Undo/Redo stack with
// a 5-state word-level delta batching engine and exact sequence invertibility algebra.
package buffer

import (
	"time"
	"unicode"
)

// DeltaKind represents the primitive mutation operation on the text buffer.
type DeltaKind int

const (
	// DeltaInsert inserts Text at Offset.
	DeltaInsert DeltaKind = iota
	// DeltaDelete deletes len(Text) bytes at Offset (storing deleted text for inverse).
	DeltaDelete
)

// TextDelta represents a single reversible atomic buffer mutation.
type TextDelta struct {
	Kind   DeltaKind
	Offset int    // 0-based absolute byte offset in buffer
	Text   string // Inserted text OR deleted text
}

// Invert returns the exact inverse mutation.
func (d TextDelta) Invert() TextDelta {
	if d.Kind == DeltaInsert {
		return TextDelta{
			Kind:   DeltaDelete,
			Offset: d.Offset,
			Text:   d.Text,
		}
	}
	return TextDelta{
		Kind:   DeltaInsert,
		Offset: d.Offset,
		Text:   d.Text,
	}
}

// CharCategory categorizes runes for word batching state machine transitions.
type CharCategory int

const (
	CatWord CharCategory = iota
	CatWhitespace
	CatNewline
	CatPunctuation
)

// ClassifyRune categorizes a rune for typing transaction boundaries.
func ClassifyRune(r rune) CharCategory {
	switch {
	case r == '\n':
		return CatNewline
	case unicode.IsSpace(r):
		return CatWhitespace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return CatWord
	default:
		return CatPunctuation
	}
}

// BatchState defines the 5 states of the keystroke batching engine.
type BatchState int

const (
	BatchIdle BatchState = iota
	BatchWord
	BatchWhitespace
	BatchPunctuation
	BatchDelete
)

// Transaction represents an atomic undoable unit containing deltas and selection snapshots.
type Transaction struct {
	ID          int64
	Deltas      []TextDelta
	BeforeState []Selection // Selections snapshot BEFORE edit
	AfterState  []Selection // Selections snapshot AFTER edit
	Timestamp   time.Time
}

// DeltaApplier is a callback that applies a single delta to the underlying text buffer.
type DeltaApplier func(d TextDelta) error

// SelectionRestorer is a callback that updates the buffer selections.
type SelectionRestorer func(sels []Selection)

// History manages the transactional Undo/Redo stacks and word-level batching.
type History struct {
	undoStack []*Transaction
	redoStack []*Transaction

	activeTxn     *Transaction
	batchState    BatchState
	lastActivity  time.Time
	inactivityDur time.Duration // default: 800ms
	maxUndoLimit  int           // default: 1000
	savedTxnID    int64         // Transaction ID when file was last saved
	nextTxnID     int64
}

// NewHistory constructs a new History manager.
func NewHistory() *History {
	return &History{
		undoStack:     make([]*Transaction, 0, 64),
		redoStack:     make([]*Transaction, 0, 16),
		inactivityDur: 800 * time.Millisecond,
		maxUndoLimit:  1000,
	}
}

// ShouldCommit checks if the currently active keystroke batch must commit.
func (h *History) ShouldCommit(cat CharCategory, isDelete bool, currentSels []Selection) bool {
	if h.activeTxn == nil {
		return false
	}

	// 1. Inactivity Timeout Trigger
	if time.Since(h.lastActivity) > h.inactivityDur {
		return true
	}

	// 2. Cursor Discontinuity / Jump Trigger
	if !selectionsEqual(currentSels, h.activeTxn.AfterState) {
		return true
	}

	// 3. Operation Kind Switch (Insert vs Delete)
	if isDelete && h.batchState != BatchDelete {
		return true
	}
	if !isDelete && h.batchState == BatchDelete {
		return true
	}

	// 4. Character Category Transition
	if !isDelete {
		if cat == CatNewline {
			return true // Newlines are always distinct transaction boundaries
		}
		switch h.batchState {
		case BatchWord:
			return cat != CatWord
		case BatchWhitespace:
			return cat != CatWhitespace
		case BatchPunctuation:
			return cat != CatPunctuation
		}
	}

	return false
}

// RecordKeystroke records a typing or backspace event, coalescing consecutive
// keystrokes according to the 5-state batching engine.
func (h *History) RecordKeystroke(
	cat CharCategory,
	isDelete bool,
	deltas []TextDelta,
	beforeSels []Selection,
	afterSels []Selection,
) {
	if len(deltas) == 0 {
		return
	}

	if h.ShouldCommit(cat, isDelete, beforeSels) {
		h.CommitActiveBatch()
	}

	// Any forward modification invalidates the redo stack
	h.redoStack = h.redoStack[:0]

	if h.activeTxn == nil {
		h.nextTxnID++
		h.activeTxn = &Transaction{
			ID:          h.nextTxnID,
			Deltas:      make([]TextDelta, 0, len(deltas)),
			BeforeState: cloneSelections(beforeSels),
			Timestamp:   time.Now(),
		}
		if isDelete {
			h.batchState = BatchDelete
		} else {
			switch cat {
			case CatWord:
				h.batchState = BatchWord
			case CatWhitespace:
				h.batchState = BatchWhitespace
			case CatPunctuation:
				h.batchState = BatchPunctuation
			case CatNewline:
				h.batchState = BatchIdle
			}
		}
	}

	h.activeTxn.Deltas = append(h.activeTxn.Deltas, deltas...)
	h.activeTxn.AfterState = cloneSelections(afterSels)
	h.lastActivity = time.Now()

	// Newlines commit immediately
	if cat == CatNewline {
		h.CommitActiveBatch()
	}
}

// RecordEdit records a standalone, atomic transaction (e.g. paste, replace, multi-line edit).
// Flushes any active typing batch and immediately commits.
func (h *History) RecordEdit(deltas []TextDelta, beforeSels []Selection, afterSels []Selection) {
	if len(deltas) == 0 {
		return
	}

	h.CommitActiveBatch()
	h.redoStack = h.redoStack[:0]

	h.nextTxnID++
	txn := &Transaction{
		ID:          h.nextTxnID,
		Deltas:      deltas,
		BeforeState: cloneSelections(beforeSels),
		AfterState:  cloneSelections(afterSels),
		Timestamp:   time.Now(),
	}

	h.undoStack = append(h.undoStack, txn)
	if len(h.undoStack) > h.maxUndoLimit {
		h.undoStack = h.undoStack[1:]
	}
}

// CommitActiveBatch finalizes the current typing batch into an undoable transaction.
func (h *History) CommitActiveBatch() {
	if h.activeTxn == nil {
		return
	}
	if len(h.activeTxn.Deltas) > 0 {
		h.undoStack = append(h.undoStack, h.activeTxn)
		if len(h.undoStack) > h.maxUndoLimit {
			h.undoStack = h.undoStack[1:]
		}
	}
	h.activeTxn = nil
	h.batchState = BatchIdle
}

// Undo inverts the most recent transaction and restores the BeforeState selections.
func (h *History) Undo(apply DeltaApplier, restore SelectionRestorer) bool {
	h.CommitActiveBatch()

	if len(h.undoStack) == 0 {
		return false
	}

	txn := h.undoStack[len(h.undoStack)-1]
	h.undoStack = h.undoStack[:len(h.undoStack)-1]

	// Sequence Inversion Law: apply inverse deltas in reverse order
	for i := len(txn.Deltas) - 1; i >= 0; i-- {
		inv := txn.Deltas[i].Invert()
		if err := apply(inv); err != nil {
			return false
		}
	}

	restore(cloneSelections(txn.BeforeState))
	h.redoStack = append(h.redoStack, txn)
	return true
}

// Redo re-applies the most recently undone transaction and restores the AfterState selections.
func (h *History) Redo(apply DeltaApplier, restore SelectionRestorer) bool {
	h.CommitActiveBatch()

	if len(h.redoStack) == 0 {
		return false
	}

	txn := h.redoStack[len(h.redoStack)-1]
	h.redoStack = h.redoStack[:len(h.redoStack)-1]

	// Apply forward deltas
	for i := 0; i < len(txn.Deltas); i++ {
		if err := apply(txn.Deltas[i]); err != nil {
			return false
		}
	}

	restore(cloneSelections(txn.AfterState))
	h.undoStack = append(h.undoStack, txn)
	return true
}

// MarkSaved marks the current history state as clean (saved to disk).
func (h *History) MarkSaved() {
	h.CommitActiveBatch()
	if len(h.undoStack) == 0 {
		h.savedTxnID = 0
	} else {
		h.savedTxnID = h.undoStack[len(h.undoStack)-1].ID
	}
}

// IsModified reports whether the buffer has uncommitted or unsaved edits.
func (h *History) IsModified() bool {
	if h.activeTxn != nil && len(h.activeTxn.Deltas) > 0 {
		return true
	}
	currentID := int64(0)
	if len(h.undoStack) > 0 {
		currentID = h.undoStack[len(h.undoStack)-1].ID
	}
	return currentID != h.savedTxnID
}

// CanUndo reports whether any transaction exists on the undo stack.
func (h *History) CanUndo() bool {
	return (h.activeTxn != nil && len(h.activeTxn.Deltas) > 0) || len(h.undoStack) > 0
}

// CanRedo reports whether any transaction exists on the redo stack.
func (h *History) CanRedo() bool {
	return len(h.redoStack) > 0
}

// UndoCount returns the count of completed transactions on the undo stack.
func (h *History) UndoCount() int {
	return len(h.undoStack)
}

// RedoCount returns the count of transactions on the redo stack.
func (h *History) RedoCount() int {
	return len(h.redoStack)
}

func cloneSelections(sels []Selection) []Selection {
	if len(sels) == 0 {
		return nil
	}
	out := make([]Selection, len(sels))
	copy(out, sels)
	return out
}

func selectionsEqual(a, b []Selection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Anchor != b[i].Anchor || a[i].Head != b[i].Head {
			return false
		}
	}
	return true
}
