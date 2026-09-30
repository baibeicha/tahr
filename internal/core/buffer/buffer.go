// Package buffer provides the central Buffer struct coordinating the Augmented AVL Rope,
// multi-selection manager, transactional history engine, UTF coordinate bridge,
// and safe atomic file persistence.
package buffer

import (
	"bytes"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Buffer defines the core contract for text storage, multi-selection editing,
// undo/redo history, and atomic persistence as specified in PROJECT.md.
type Buffer interface {
	TotalBytes() int
	TotalLines() int
	ByteOffsetForLine(lineIdx int) (int, error) // O(log N)
	LineForByteOffset(offset int) (int, error)   // O(log N)
	GetLine(lineIdx int) ([]byte, error)
	Slice(startByte, endByte int) ([]byte, error)

	// Multi-Selection and Edits (bottom-to-top execution)
	GetSelections() []Selection
	SetSelections(sels []Selection)
	InsertAtSelections(text string)
	DeleteAtSelections()
	ApplyEdit(offset int, deleteLen int, insertText string) error

	// Undo/Redo & Save
	Undo() bool
	Redo() bool
	SaveAtomic(filepath string) error
	Load(filepath string) error
}

// BufferImpl is the concrete implementation of Buffer coordinating all core sub-components.
type BufferImpl struct {
	rope         *Rope
	selections   []Selection
	primaryIndex int
	history      *History
	utfBridge    *UTFBridgeImpl
	filePath     string
}

// NewBuffer constructs an empty Buffer.
func NewBuffer() *BufferImpl {
	r := NewRope()
	b := &BufferImpl{
		rope:         r,
		selections:   []Selection{NewCursor(Position{Line: 0, Column: 0, Byte: 0})},
		primaryIndex: 0,
		history:      NewHistory(),
		filePath:     "",
	}
	b.utfBridge = NewUTFBridge(r, DefaultTabWidth)
	b.history.MarkSaved()
	return b
}

// NewBufferWithText constructs a Buffer initialized with initialText.
func NewBufferWithText(initialText string) *BufferImpl {
	b := NewBuffer()
	if len(initialText) > 0 {
		normalized, hasCRLF := NormalizeCRLF([]byte(initialText))
		b.rope.hasCRLF = hasCRLF
		b.rope.root = BuildTreeFromBytes(normalized)
		b.history.MarkSaved()
	}
	return b
}

// TotalBytes returns the buffer byte length.
func (b *BufferImpl) TotalBytes() int {
	return b.rope.TotalBytes()
}

// TotalLines returns the buffer line count.
func (b *BufferImpl) TotalLines() int {
	return b.rope.TotalLines()
}

// ByteOffsetForLine returns the byte offset where lineIdx begins in O(log N) time.
func (b *BufferImpl) ByteOffsetForLine(lineIdx int) (int, error) {
	return b.rope.ByteOffsetForLine(lineIdx)
}

// LineForByteOffset returns the line index containing offset in O(log N) time.
func (b *BufferImpl) LineForByteOffset(offset int) (int, error) {
	return b.rope.LineForByteOffset(offset)
}

// GetLine returns the contents of lineIdx.
func (b *BufferImpl) GetLine(lineIdx int) ([]byte, error) {
	return b.rope.GetLine(lineIdx)
}

// Slice returns a byte slice covering [startByte, endByte).
func (b *BufferImpl) Slice(startByte, endByte int) ([]byte, error) {
	return b.rope.Slice(startByte, endByte)
}

// GetText returns the complete text content of the buffer.
func (b *BufferImpl) GetText() ([]byte, error) {
	return b.rope.Slice(0, b.rope.TotalBytes())
}

// Rope returns the underlying Augmented AVL Rope.
func (b *BufferImpl) Rope() *Rope {
	return b.rope
}

// UTFBridge returns the coordinate conversion bridge.
func (b *BufferImpl) UTFBridge() UTFBridge {
	return b.utfBridge
}

// FilePath returns the associated file path.
func (b *BufferImpl) FilePath() string {
	return b.filePath
}

// SetFilePath updates the associated file path.
func (b *BufferImpl) SetFilePath(path string) {
	b.filePath = path
}

// HasCRLF reports whether the buffer uses CRLF line endings.
func (b *BufferImpl) HasCRLF() bool {
	return b.rope.HasCRLF()
}

// SetCRLF sets whether the buffer should use CRLF line endings.
func (b *BufferImpl) SetCRLF(crlf bool) {
	b.rope.SetCRLF(crlf)
}

// IsModified reports whether the buffer has unsaved changes.
func (b *BufferImpl) IsModified() bool {
	return b.history.IsModified()
}

// ============================================================================
// Coordinate Bridge Delegations
// ============================================================================

// ByteToPosition converts an absolute buffer byte offset into a Position.
func (b *BufferImpl) ByteToPosition(offset int) Position {
	return b.utfBridge.ByteToPosition(offset)
}

// PositionToByte converts a Position into an absolute buffer byte offset.
func (b *BufferImpl) PositionToByte(pos Position) int {
	return b.utfBridge.PositionToByte(pos)
}

// PositionToLSP converts a Document Position into LSP 3.17 coordinates.
func (b *BufferImpl) PositionToLSP(pos Position) (line int, characterUTF16 int) {
	return b.utfBridge.PositionToLSP(pos)
}

// LSPToPosition converts LSP (line, characterUTF16) into a Document Position.
func (b *BufferImpl) LSPToPosition(line int, characterUTF16 int) Position {
	return b.utfBridge.LSPToPosition(line, characterUTF16)
}

// VisualColumn returns the visual display column for a line and rune column.
func (b *BufferImpl) VisualColumn(line int, runeCol int) int {
	return b.utfBridge.VisualColumn(line, runeCol)
}

// RuneColFromVisual finds the rune column corresponding to a visual display column.
func (b *BufferImpl) RuneColFromVisual(line int, visualCol int) int {
	return b.utfBridge.RuneColFromVisual(line, visualCol)
}

// ============================================================================
// Multi-Selection Management
// ============================================================================

// GetSelections returns a copy of current selections.
func (b *BufferImpl) GetSelections() []Selection {
	out := make([]Selection, len(b.selections))
	copy(out, b.selections)
	return out
}

// SetSelections updates selections, sorting and normalizing them into disjoint ranges.
func (b *BufferImpl) SetSelections(sels []Selection) {
	if len(sels) == 0 {
		sels = []Selection{NewCursor(Position{Line: 0, Column: 0, Byte: 0})}
	}
	b.selections, b.primaryIndex = NormalizeSelections(sels, b.primaryIndex)
}

// PrimarySelection returns the active primary selection.
func (b *BufferImpl) PrimarySelection() Selection {
	if b.primaryIndex >= 0 && b.primaryIndex < len(b.selections) {
		return b.selections[b.primaryIndex]
	}
	if len(b.selections) > 0 {
		return b.selections[0]
	}
	return NewCursor(Position{Line: 0, Column: 0, Byte: 0})
}

// PrimaryIndex returns the index of the primary selection.
func (b *BufferImpl) PrimaryIndex() int {
	return b.primaryIndex
}

// SetPrimaryIndex updates the primary selection index.
func (b *BufferImpl) SetPrimaryIndex(idx int) {
	if idx >= 0 && idx < len(b.selections) {
		b.primaryIndex = idx
	}
}

// AddCursor adds a new cursor at pos and normalizes selections.
func (b *BufferImpl) AddCursor(pos Position) {
	b.selections = append(b.selections, NewCursor(pos))
	b.selections, b.primaryIndex = NormalizeSelections(b.selections, len(b.selections)-1)
}

// ToggleCursor toggles a cursor at pos: removes if existing and >1 selections, adds otherwise.
func (b *BufferImpl) ToggleCursor(pos Position) {
	for i, s := range b.selections {
		if s.ContainsByte(pos.Byte) {
			if len(b.selections) > 1 {
				b.selections = append(b.selections[:i], b.selections[i+1:]...)
				if b.primaryIndex >= len(b.selections) {
					b.primaryIndex = len(b.selections) - 1
				}
				return
			}
			// Collapse to single point cursor
			b.selections = []Selection{NewCursor(pos)}
			b.primaryIndex = 0
			return
		}
	}
	b.AddCursor(pos)
}

// ClearSelections collapses all selections into a single cursor at the primary selection's Head.
func (b *BufferImpl) ClearSelections() {
	primary := b.PrimarySelection()
	b.selections = []Selection{NewCursor(primary.Head)}
	b.primaryIndex = 0
}

// ============================================================================
// Multi-Selection Mutations (Bottom-to-Top Execution)
// ============================================================================

// InsertAtSelections replaces each selection with text.
// Mutates bottom-to-top (descending byte offset order) to prevent coordinate drift.
func (b *BufferImpl) InsertAtSelections(text string) {
	if len(b.selections) == 0 {
		return
	}

	beforeSelections := b.GetSelections()
	k := len(b.selections)

	// Collect edits per selection
	edits := make([]TextEdit, k)
	for i := 0; i < k; i++ {
		s := b.selections[i]
		edits[i] = TextEdit{
			StartByte: s.Start().Byte,
			EndByte:   s.End().Byte,
			NewText:   text,
		}
	}

	var recordedDeltas []TextDelta

	// 1. APPLY BOTTOM-TO-TOP (descending i: k-1 down to 0)
	for i := k - 1; i >= 0; i-- {
		e := edits[i]
		delLen := e.EndByte - e.StartByte

		if delLen > 0 {
			oldBytes, err := b.rope.Slice(e.StartByte, e.EndByte)
			if err == nil {
				_ = b.rope.Delete(e.StartByte, delLen)
				recordedDeltas = append(recordedDeltas, TextDelta{
					Kind:   DeltaDelete,
					Offset: e.StartByte,
					Text:   string(oldBytes),
				})
			}
		}

		if len(e.NewText) > 0 {
			_ = b.rope.Insert(e.StartByte, []byte(e.NewText))
			recordedDeltas = append(recordedDeltas, TextDelta{
				Kind:   DeltaInsert,
				Offset: e.StartByte,
				Text:   e.NewText,
			})
		}
	}

	// 2. COMPUTE POST-EDIT CURSOR POSITIONS VIA CUMULATIVE SHIFTS
	newSelections := make([]Selection, k)
	cumulativeShift := 0
	for i := 0; i < k; i++ {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		insLen := len(e.NewText)
		delta := insLen - delLen

		newOffset := e.StartByte + cumulativeShift + insLen
		newPos := b.ByteToPosition(newOffset)
		newSelections[i] = NewCursor(newPos)

		cumulativeShift += delta
	}

	// 3. NORMALIZE SELECTIONS
	b.selections, b.primaryIndex = NormalizeSelections(newSelections, b.primaryIndex)

	// 4. RECORD TO HISTORY
	if len(recordedDeltas) > 0 {
		// If single rune typed into empty carets, treat as keystroke for word batching
		isKeystroke := (len(text) > 0) && (utf8.RuneCountInString(text) == 1)
		for _, s := range beforeSelections {
			if !s.IsEmpty() {
				isKeystroke = false
				break
			}
		}

		if isKeystroke {
			r, _ := utf8.DecodeRuneInString(text)
			cat := ClassifyRune(r)
			b.history.RecordKeystroke(cat, false, recordedDeltas, beforeSelections, b.selections)
		} else {
			b.history.RecordEdit(recordedDeltas, beforeSelections, b.selections)
		}
	}
}

// DeleteAtSelections performs a Backspace operation at each selection.
// If a selection spans a range, it deletes the range.
// If a selection is an empty point, it deletes the preceding rune.
// Applied bottom-to-top.
func (b *BufferImpl) DeleteAtSelections() {
	if len(b.selections) == 0 {
		return
	}

	beforeSelections := b.GetSelections()
	k := len(b.selections)

	edits := make([]TextEdit, k)
	hasNonEmpty := false

	for i := 0; i < k; i++ {
		s := b.selections[i]
		if !s.IsEmpty() {
			edits[i] = TextEdit{
				StartByte: s.Start().Byte,
				EndByte:   s.End().Byte,
				NewText:   "",
			}
			hasNonEmpty = true
		} else {
			headByte := s.Head.Byte
			if headByte <= 0 {
				edits[i] = TextEdit{StartByte: 0, EndByte: 0, NewText: ""}
				continue
			}

			// Find previous rune boundary
			lineStart, _ := b.rope.ByteOffsetForLine(s.Head.Line)
			if headByte > lineStart {
				lineBytes, _ := b.rope.GetLine(s.Head.Line)
				colByte := headByte - lineStart
				if colByte > len(lineBytes) {
					colByte = len(lineBytes)
				}
				_, sz := utf8.DecodeLastRune(lineBytes[:colByte])
				prevByte := headByte - sz
				edits[i] = TextEdit{StartByte: prevByte, EndByte: headByte, NewText: ""}
			} else {
				// At start of line, delete preceding newline
				prevByte := headByte - 1
				edits[i] = TextEdit{StartByte: prevByte, EndByte: headByte, NewText: ""}
			}
		}
	}

	var recordedDeltas []TextDelta

	// 1. APPLY BOTTOM-TO-TOP (descending i: k-1 down to 0)
	for i := k - 1; i >= 0; i-- {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		if delLen > 0 {
			oldBytes, err := b.rope.Slice(e.StartByte, e.EndByte)
			if err == nil {
				_ = b.rope.Delete(e.StartByte, delLen)
				recordedDeltas = append(recordedDeltas, TextDelta{
					Kind:   DeltaDelete,
					Offset: e.StartByte,
					Text:   string(oldBytes),
				})
			}
		}
	}

	// 2. COMPUTE POST-EDIT POSITIONS
	newSelections := make([]Selection, k)
	cumulativeShift := 0
	for i := 0; i < k; i++ {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		delta := -delLen

		newOffset := e.StartByte + cumulativeShift
		newPos := b.ByteToPosition(newOffset)
		newSelections[i] = NewCursor(newPos)

		cumulativeShift += delta
	}

	// 3. NORMALIZE
	b.selections, b.primaryIndex = NormalizeSelections(newSelections, b.primaryIndex)

	// 4. RECORD TO HISTORY
	if len(recordedDeltas) > 0 {
		if hasNonEmpty {
			b.history.RecordEdit(recordedDeltas, beforeSelections, b.selections)
		} else {
			b.history.RecordKeystroke(CatWord, true, recordedDeltas, beforeSelections, b.selections)
		}
	}
}

// DeleteForwardAtSelections performs a Delete key operation (forward delete).
func (b *BufferImpl) DeleteForwardAtSelections() {
	if len(b.selections) == 0 {
		return
	}

	beforeSelections := b.GetSelections()
	k := len(b.selections)
	totalBytes := b.rope.TotalBytes()

	edits := make([]TextEdit, k)
	for i := 0; i < k; i++ {
		s := b.selections[i]
		if !s.IsEmpty() {
			edits[i] = TextEdit{StartByte: s.Start().Byte, EndByte: s.End().Byte, NewText: ""}
		} else {
			headByte := s.Head.Byte
			if headByte >= totalBytes {
				edits[i] = TextEdit{StartByte: headByte, EndByte: headByte, NewText: ""}
				continue
			}

			lineStart, _ := b.rope.ByteOffsetForLine(s.Head.Line)
			lineBytes, _ := b.rope.GetLine(s.Head.Line)
			colByte := headByte - lineStart

			if colByte < len(lineBytes) {
				_, sz := utf8.DecodeRune(lineBytes[colByte:])
				edits[i] = TextEdit{StartByte: headByte, EndByte: headByte + sz, NewText: ""}
			} else {
				// At end of line, delete newline
				edits[i] = TextEdit{StartByte: headByte, EndByte: headByte + 1, NewText: ""}
			}
		}
	}

	var recordedDeltas []TextDelta

	// 1. APPLY BOTTOM-TO-TOP
	for i := k - 1; i >= 0; i-- {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		if delLen > 0 {
			oldBytes, err := b.rope.Slice(e.StartByte, e.EndByte)
			if err == nil {
				_ = b.rope.Delete(e.StartByte, delLen)
				recordedDeltas = append(recordedDeltas, TextDelta{
					Kind:   DeltaDelete,
					Offset: e.StartByte,
					Text:   string(oldBytes),
				})
			}
		}
	}

	// 2. COMPUTE POST-EDIT POSITIONS
	newSelections := make([]Selection, k)
	cumulativeShift := 0
	for i := 0; i < k; i++ {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		newOffset := e.StartByte + cumulativeShift
		newPos := b.ByteToPosition(newOffset)
		newSelections[i] = NewCursor(newPos)
		cumulativeShift -= delLen
	}

	b.selections, b.primaryIndex = NormalizeSelections(newSelections, b.primaryIndex)

	if len(recordedDeltas) > 0 {
		b.history.RecordEdit(recordedDeltas, beforeSelections, b.selections)
	}
}

// SelectNextOccurrence implements the Ctrl+D command.
// If the primary selection is empty, it expands to the current word under cursor.
// If non-empty, it finds and selects the next occurrence of the selected text.
func (b *BufferImpl) SelectNextOccurrence() bool {
	primary := b.PrimarySelection()

	// Case 1: Empty selection -> expand to word
	if primary.IsEmpty() {
		headByte := primary.Head.Byte
		lineIdx := primary.Head.Line
		lineBytes, err := b.rope.GetLine(lineIdx)
		if err != nil || len(lineBytes) == 0 {
			return false
		}
		lineStart, err := b.rope.ByteOffsetForLine(lineIdx)
		if err != nil {
			return false
		}

		offsetInLine := headByte - lineStart
		wStart, wEnd := FindWordBoundsAt(lineBytes, offsetInLine)
		if wEnd > wStart {
			startPos := b.ByteToPosition(lineStart + wStart)
			endPos := b.ByteToPosition(lineStart + wEnd)
			b.selections[b.primaryIndex] = NewSelection(startPos, endPos)
			return true
		}

		// If not on a word, select 1 rune forward
		if offsetInLine < len(lineBytes) {
			_, sz := utf8.DecodeRune(lineBytes[offsetInLine:])
			endPos := b.ByteToPosition(headByte + sz)
			b.selections[b.primaryIndex] = NewSelection(primary.Head, endPos)
			return true
		}
		return false
	}

	// Case 2: Non-empty selection -> search next occurrence
	queryBytes, err := b.rope.Slice(primary.Start().Byte, primary.End().Byte)
	if err != nil || len(queryBytes) == 0 {
		return false
	}

	totalBytes := b.rope.TotalBytes()
	fromOffset := primary.End().Byte

	fullText, err := b.rope.Slice(0, totalBytes)
	if err != nil {
		return false
	}

	searchStart := fromOffset
	foundStart := -1
	foundEnd := -1

	// Loop to find an occurrence that is NOT already selected
	for attempts := 0; attempts < 2; attempts++ {
		for searchStart < len(fullText) {
			idx := bytes.Index(fullText[searchStart:], queryBytes)
			if idx == -1 {
				break
			}
			mStart := searchStart + idx
			mEnd := mStart + len(queryBytes)

			// Check if already selected
			alreadySelected := false
			for _, s := range b.selections {
				if s.Start().Byte == mStart && s.End().Byte == mEnd {
					alreadySelected = true
					break
				}
			}

			if !alreadySelected {
				foundStart = mStart
				foundEnd = mEnd
				break
			}
			searchStart = mEnd
		}

		if foundStart != -1 {
			break
		}
		// Wrap around from beginning
		searchStart = 0
	}

	if foundStart == -1 {
		return false // All occurrences already selected
	}

	newSel := NewSelection(b.ByteToPosition(foundStart), b.ByteToPosition(foundEnd))
	b.selections = append(b.selections, newSel)
	b.selections, b.primaryIndex = NormalizeSelections(b.selections, len(b.selections)-1)
	return true
}

// ApplyEdit performs an atomic replacement of deleteLen bytes at offset with insertText.
func (b *BufferImpl) ApplyEdit(offset int, deleteLen int, insertText string) error {
	if offset < 0 || offset+deleteLen > b.rope.TotalBytes() {
		return ErrOffsetOutOfBounds
	}

	beforeSels := b.GetSelections()
	var deltas []TextDelta

	if deleteLen > 0 {
		oldBytes, err := b.rope.Slice(offset, offset+deleteLen)
		if err != nil {
			return err
		}
		if err := b.rope.Delete(offset, deleteLen); err != nil {
			return err
		}
		deltas = append(deltas, TextDelta{
			Kind:   DeltaDelete,
			Offset: offset,
			Text:   string(oldBytes),
		})
	}

	if len(insertText) > 0 {
		if err := b.rope.Insert(offset, []byte(insertText)); err != nil {
			return err
		}
		deltas = append(deltas, TextDelta{
			Kind:   DeltaInsert,
			Offset: offset,
			Text:   insertText,
		})
	}

	// Update cursor to end of edit
	newPos := b.ByteToPosition(offset + len(insertText))
	b.selections = []Selection{NewCursor(newPos)}
	b.primaryIndex = 0

	if len(deltas) > 0 {
		b.history.RecordEdit(deltas, beforeSels, b.selections)
	}

	return nil
}

// Undo restores the previous state from history.
func (b *BufferImpl) Undo() bool {
	apply := func(d TextDelta) error {
		switch d.Kind {
		case DeltaInsert:
			return b.rope.Insert(d.Offset, []byte(d.Text))
		case DeltaDelete:
			return b.rope.Delete(d.Offset, len(d.Text))
		}
		return errors.New("unknown delta kind")
	}

	restore := func(sels []Selection) {
		b.selections = sels
		b.primaryIndex = len(sels) - 1
		if b.primaryIndex < 0 {
			b.primaryIndex = 0
		}
	}

	return b.history.Undo(apply, restore)
}

// Redo re-applies the next state from history.
func (b *BufferImpl) Redo() bool {
	apply := func(d TextDelta) error {
		switch d.Kind {
		case DeltaInsert:
			return b.rope.Insert(d.Offset, []byte(d.Text))
		case DeltaDelete:
			return b.rope.Delete(d.Offset, len(d.Text))
		}
		return errors.New("unknown delta kind")
	}

	restore := func(sels []Selection) {
		b.selections = sels
		b.primaryIndex = len(sels) - 1
		if b.primaryIndex < 0 {
			b.primaryIndex = 0
		}
	}

	return b.history.Redo(apply, restore)
}

// Save writes the buffer to filePath using atomic save protocol.
func (b *BufferImpl) Save() error {
	if b.filePath == "" {
		return fmt.Errorf("no file path set on buffer")
	}
	return b.SaveAtomic(b.filePath)
}

// SaveAtomic writes the buffer content to targetPath using the 10-step safe atomic save protocol.
func (b *BufferImpl) SaveAtomic(targetPath string) error {
	b.history.CommitActiveBatch()
	if err := SaveAtomicFile(targetPath, b.rope, b.rope.HasCRLF()); err != nil {
		return err
	}
	b.filePath = targetPath
	b.history.MarkSaved()
	return nil
}

// Load reads targetPath from disk, normalizes CRLF, and initializes buffer state.
func (b *BufferImpl) Load(targetPath string) error {
	if err := b.rope.Load(targetPath); err != nil {
		return err
	}
	b.filePath = targetPath
	b.selections = []Selection{NewCursor(Position{Line: 0, Column: 0, Byte: 0})}
	b.primaryIndex = 0
	b.history = NewHistory()
	b.history.MarkSaved()
	return nil
}
