// Package buffer provides text buffer representation, UTF coordinate conversions,
// and safe atomic file persistence for the Tahr editor engine.
package buffer

import (
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// DefaultTabWidth is the default visual width allocated to a tab stop.
const DefaultTabWidth = 4

// LineProvider abstracts read access to the line-based text buffer.
// Both Rope and higher-level Buffer types implement this interface.
type LineProvider interface {
	TotalLines() int
	TotalBytes() int
	ByteOffsetForLine(lineIdx int) (int, error)
	LineForByteOffset(offset int) (int, error)
	GetLine(lineIdx int) ([]byte, error)
}

// UTFBridge defines the contract for 3-way coordinate translation between:
// 1. Buffer UTF-8 Byte Offset
// 2. Document Position (Line, Column, Byte)
// 3. LSP Protocol UTF-16 Code Units
// 4. Terminal Visual Display Columns (accounting for wide runes and tab stops)
type UTFBridge interface {
	ByteToPosition(offset int) Position
	PositionToByte(pos Position) int
	PositionToLSP(pos Position) (line int, characterUTF16 int)
	LSPToPosition(line int, characterUTF16 int) Position
	VisualColumn(line int, runeCol int) int
	RuneColFromVisual(line int, visualCol int) int
	TabWidth() int
	SetTabWidth(w int)
}

// UTFBridgeImpl is the concrete implementation of UTFBridge.
type UTFBridgeImpl struct {
	provider LineProvider
	tabWidth int
}

// NewUTFBridge constructs a new UTFBridge backed by a LineProvider.
func NewUTFBridge(provider LineProvider, tabWidth int) *UTFBridgeImpl {
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}
	return &UTFBridgeImpl{
		provider: provider,
		tabWidth: tabWidth,
	}
}

// TabWidth returns the current tab stop width.
func (b *UTFBridgeImpl) TabWidth() int {
	return b.tabWidth
}

// SetTabWidth updates the tab stop width.
func (b *UTFBridgeImpl) SetTabWidth(w int) {
	if w <= 0 {
		w = DefaultTabWidth
	}
	b.tabWidth = w
}

// ByteToPosition converts an absolute buffer byte offset into a Position{Line, Column, Byte}.
// Column is the 0-based rune index within the line.
func (b *UTFBridgeImpl) ByteToPosition(offset int) Position {
	if b.provider == nil || b.provider.TotalLines() == 0 {
		return Position{Line: 0, Column: 0, Byte: 0}
	}

	totalBytes := b.provider.TotalBytes()
	if offset < 0 {
		offset = 0
	}
	if offset > totalBytes {
		offset = totalBytes
	}

	lineIdx, err := b.provider.LineForByteOffset(offset)
	if err != nil {
		lineIdx = 0
	}

	lineStart, err := b.provider.ByteOffsetForLine(lineIdx)
	if err != nil {
		lineStart = 0
	}

	byteInLine := offset - lineStart
	if byteInLine <= 0 {
		return Position{Line: lineIdx, Column: 0, Byte: offset}
	}

	lineBytes, err := b.provider.GetLine(lineIdx)
	if err != nil || len(lineBytes) == 0 {
		return Position{Line: lineIdx, Column: 0, Byte: offset}
	}

	if byteInLine > len(lineBytes) {
		byteInLine = len(lineBytes)
	}

	// Count runes in the slice up to byteInLine
	runeCol := utf8.RuneCount(lineBytes[:byteInLine])
	return Position{
		Line:   lineIdx,
		Column: runeCol,
		Byte:   offset,
	}
}

// PositionToByte converts a Position{Line, Column, Byte} into an absolute buffer byte offset.
// If pos.Byte is already consistent with the line and column, it is returned.
// Otherwise, it computes the byte offset from Line and Column.
func (b *UTFBridgeImpl) PositionToByte(pos Position) int {
	if b.provider == nil || b.provider.TotalLines() == 0 {
		return 0
	}

	totalLines := b.provider.TotalLines()
	line := pos.Line
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = totalLines - 1
	}

	lineStart, err := b.provider.ByteOffsetForLine(line)
	if err != nil {
		return 0
	}

	if pos.Column <= 0 {
		return lineStart
	}

	lineBytes, err := b.provider.GetLine(line)
	if err != nil || len(lineBytes) == 0 {
		return lineStart
	}

	// Exclude trailing newline from column scanning
	content := lineBytes
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	byteOffsetInLine := LineRuneColToByte(content, pos.Column)
	return lineStart + byteOffsetInLine
}

// PositionToLSP converts a Document Position into LSP 3.17 coordinates:
// line (0-based) and characterUTF16 (0-based UTF-16 code unit offset from line start).
func (b *UTFBridgeImpl) PositionToLSP(pos Position) (line int, characterUTF16 int) {
	if b.provider == nil || b.provider.TotalLines() == 0 {
		return 0, 0
	}

	totalLines := b.provider.TotalLines()
	line = pos.Line
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = totalLines - 1
	}

	if pos.Column <= 0 {
		return line, 0
	}

	lineBytes, err := b.provider.GetLine(line)
	if err != nil || len(lineBytes) == 0 {
		return line, 0
	}

	content := lineBytes
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	characterUTF16 = LineRuneColToLSP(content, pos.Column)
	return line, characterUTF16
}

// LSPToPosition converts LSP (line, characterUTF16) into a Document Position{Line, Column, Byte}.
// If characterUTF16 points in between a surrogate pair (code point >= 0x10000), it snaps
// to the rune boundary to ensure valid UTF-8 scalar values.
func (b *UTFBridgeImpl) LSPToPosition(line int, characterUTF16 int) Position {
	if b.provider == nil || b.provider.TotalLines() == 0 {
		return Position{Line: 0, Column: 0, Byte: 0}
	}

	totalLines := b.provider.TotalLines()
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = totalLines - 1
	}

	lineStart, err := b.provider.ByteOffsetForLine(line)
	if err != nil {
		lineStart = 0
	}

	if characterUTF16 <= 0 {
		return Position{Line: line, Column: 0, Byte: lineStart}
	}

	lineBytes, err := b.provider.GetLine(line)
	if err != nil || len(lineBytes) == 0 {
		return Position{Line: line, Column: 0, Byte: lineStart}
	}

	content := lineBytes
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	runeCol, byteInLine := LineLSPToRuneAndByte(content, characterUTF16)
	return Position{
		Line:   line,
		Column: runeCol,
		Byte:   lineStart + byteInLine,
	}
}

// VisualColumn computes the 0-based visual display column for a given line and rune column.
// Accounts for single-width characters, Cyrillic (width 1), CJK and emojis (width 2),
// combining accents (width 0), and tab stop expansion.
func (b *UTFBridgeImpl) VisualColumn(line int, runeCol int) int {
	if b.provider == nil || b.provider.TotalLines() == 0 || runeCol <= 0 {
		return 0
	}

	totalLines := b.provider.TotalLines()
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = totalLines - 1
	}

	lineBytes, err := b.provider.GetLine(line)
	if err != nil || len(lineBytes) == 0 {
		return 0
	}

	content := lineBytes
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	return LineRuneColToVisual(content, runeCol, b.tabWidth)
}

// RuneColFromVisual finds the 0-based rune column corresponding to a given visual display column.
// Employs a halfway rounding policy: if a mouse click lands in the right half of a wide rune
// or tab stop, it snaps to the next rune; otherwise, it snaps to the start of the current rune.
func (b *UTFBridgeImpl) RuneColFromVisual(line int, visualCol int) int {
	if b.provider == nil || b.provider.TotalLines() == 0 || visualCol <= 0 {
		return 0
	}

	totalLines := b.provider.TotalLines()
	if line < 0 {
		line = 0
	}
	if line >= totalLines {
		line = totalLines - 1
	}

	lineBytes, err := b.provider.GetLine(line)
	if err != nil || len(lineBytes) == 0 {
		return 0
	}

	content := lineBytes
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	return LineVisualToRuneCol(content, visualCol, b.tabWidth)
}

// ============================================================================
// Pure Line-Level Helper Functions (Zero-Allocation, Highly Testable)
// ============================================================================

// LineRuneColToByte converts a 0-based rune column into a byte offset within lineBytes.
func LineRuneColToByte(lineBytes []byte, runeCol int) int {
	if runeCol <= 0 || len(lineBytes) == 0 {
		return 0
	}

	byteIdx := 0
	curRune := 0
	for byteIdx < len(lineBytes) && curRune < runeCol {
		_, sz := utf8.DecodeRune(lineBytes[byteIdx:])
		byteIdx += sz
		curRune++
	}
	return byteIdx
}

// LineByteToRuneCol converts a byte offset within lineBytes into a 0-based rune column.
func LineByteToRuneCol(lineBytes []byte, byteOffset int) int {
	if byteOffset <= 0 || len(lineBytes) == 0 {
		return 0
	}
	if byteOffset > len(lineBytes) {
		byteOffset = len(lineBytes)
	}
	return utf8.RuneCount(lineBytes[:byteOffset])
}

// LineRuneColToLSP converts a 0-based rune column into UTF-16 code units (LSP character).
// Runes >= 0x10000 (such as emojis) contribute 2 code units; BMP runes contribute 1 unit.
func LineRuneColToLSP(lineBytes []byte, runeCol int) int {
	if runeCol <= 0 || len(lineBytes) == 0 {
		return 0
	}

	curUTF16 := 0
	curRune := 0
	byteIdx := 0
	for byteIdx < len(lineBytes) && curRune < runeCol {
		r, sz := utf8.DecodeRune(lineBytes[byteIdx:])
		if r >= 0x10000 {
			curUTF16 += 2
		} else {
			curUTF16 += 1
		}
		byteIdx += sz
		curRune++
	}
	return curUTF16
}

// LineLSPToRuneCol converts LSP UTF-16 code units into the 0-based rune column.
func LineLSPToRuneCol(lineBytes []byte, characterUTF16 int) int {
	col, _ := LineLSPToRuneAndByte(lineBytes, characterUTF16)
	return col
}

// LineLSPToRuneAndByte converts LSP UTF-16 code units into both the 0-based rune column
// and the byte offset within the line.
// Snaps to rune boundaries if characterUTF16 falls in the middle of a surrogate pair.
func LineLSPToRuneAndByte(lineBytes []byte, characterUTF16 int) (runeCol int, byteOffset int) {
	if characterUTF16 <= 0 || len(lineBytes) == 0 {
		return 0, 0
	}

	curUTF16 := 0
	curRune := 0
	byteIdx := 0
	for byteIdx < len(lineBytes) && curUTF16 < characterUTF16 {
		r, sz := utf8.DecodeRune(lineBytes[byteIdx:])
		units := 1
		if r >= 0x10000 {
			units = 2
		}
		// If the requested position is between the high and low surrogate,
		// snap forward to include this full rune rather than splitting it.
		curUTF16 += units
		byteIdx += sz
		curRune++
	}
	return curRune, byteIdx
}

// LineRuneColToVisual calculates the visual display column for a rune column in lineBytes.
// Supports tab stop expansion and runewidth detection.
func LineRuneColToVisual(lineBytes []byte, runeCol int, tabWidth int) int {
	if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\n' {
		lineBytes = lineBytes[:len(lineBytes)-1]
	}
	if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
		lineBytes = lineBytes[:len(lineBytes)-1]
	}
	if runeCol <= 0 || len(lineBytes) == 0 {
		return 0
	}
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}

	visualCol := 0
	curRune := 0
	byteIdx := 0
	for byteIdx < len(lineBytes) && curRune < runeCol {
		r, sz := utf8.DecodeRune(lineBytes[byteIdx:])
		if r == '\t' {
			tabStop := tabWidth - (visualCol % tabWidth)
			visualCol += tabStop
		} else {
			w := runewidth.RuneWidth(r)
			visualCol += w
		}
		byteIdx += sz
		curRune++
	}
	return visualCol
}

// LineVisualToRuneCol maps a visual display column to the closest rune column in lineBytes.
// Halfway rounding: if visualCol lands inside [curVisual, curVisual + w),
// clicking in the second half snaps to curRune + 1.
func LineVisualToRuneCol(lineBytes []byte, visualCol int, tabWidth int) int {
	if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\n' {
		lineBytes = lineBytes[:len(lineBytes)-1]
	}
	if len(lineBytes) > 0 && lineBytes[len(lineBytes)-1] == '\r' {
		lineBytes = lineBytes[:len(lineBytes)-1]
	}
	if visualCol <= 0 || len(lineBytes) == 0 {
		return 0
	}
	if tabWidth <= 0 {
		tabWidth = DefaultTabWidth
	}

	curVisual := 0
	curRune := 0
	byteIdx := 0
	for byteIdx < len(lineBytes) {
		r, sz := utf8.DecodeRune(lineBytes[byteIdx:])
		var w int
		if r == '\t' {
			w = tabWidth - (curVisual % tabWidth)
		} else {
			w = runewidth.RuneWidth(r)
		}

		if visualCol < curVisual+w {
			// Inside this rune's visual span: apply halfway rounding rule
			if visualCol-curVisual >= (w+1)/2 {
				return curRune + 1
			}
			return curRune
		}

		curVisual += w
		byteIdx += sz
		curRune++
	}

	// Target visual column is beyond the end of the line
	return curRune
}
