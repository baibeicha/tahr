// Package buffer provides multi-selection management, position coordinates,
// and selection normalization algorithms with bottom-to-top delta ordering.
package buffer

import (
	"bytes"
	"sort"
	"unicode"
	"unicode/utf8"
)

// Position represents a 2D document coordinate and its corresponding byte offset.
// Line is 0-based document line index.
// Column is 0-based rune count from the start of Line (encoding-neutral).
// Byte is 0-based absolute byte offset in the buffer.
type Position struct {
	Line   int // 0-based line index
	Column int // 0-based rune column within line
	Byte   int // 0-based absolute byte offset in buffer
}

// Less reports whether p occurs strictly before other in the document.
func (p Position) Less(other Position) bool {
	if p.Byte != other.Byte {
		return p.Byte < other.Byte
	}
	if p.Line != other.Line {
		return p.Line < other.Line
	}
	return p.Column < other.Column
}

// Selection represents a directed text range from Anchor to Head.
// Anchor is the stationary anchor point of the selection.
// Head is the active cursor / moving caret.
// When Anchor.Byte == Head.Byte, the selection represents a single point cursor.
type Selection struct {
	Anchor Position
	Head   Position
}

// NewCursor creates an empty point selection (caret) at pos.
func NewCursor(pos Position) Selection {
	return Selection{Anchor: pos, Head: pos}
}

// NewSelection creates a directed selection from anchor to head.
func NewSelection(anchor, head Position) Selection {
	return Selection{Anchor: anchor, Head: head}
}

// IsEmpty reports whether the selection is an empty point cursor (caret).
func (s Selection) IsEmpty() bool {
	return s.Anchor.Byte == s.Head.Byte
}

// IsForward reports whether Head is at or after Anchor.
func (s Selection) IsForward() bool {
	return s.Head.Byte >= s.Anchor.Byte
}

// Start returns the lower Position (by byte offset) of Anchor and Head.
func (s Selection) Start() Position {
	if s.Anchor.Byte <= s.Head.Byte {
		return s.Anchor
	}
	return s.Head
}

// End returns the higher Position (by byte offset) of Anchor and Head.
func (s Selection) End() Position {
	if s.Anchor.Byte <= s.Head.Byte {
		return s.Head
	}
	return s.Anchor
}

// ByteLength returns the absolute number of bytes spanned by the selection.
func (s Selection) ByteLength() int {
	if s.Anchor.Byte <= s.Head.Byte {
		return s.Head.Byte - s.Anchor.Byte
	}
	return s.Anchor.Byte - s.Head.Byte
}

// ContainsByte reports whether offset falls within [Start().Byte, End().Byte].
func (s Selection) ContainsByte(offset int) bool {
	return offset >= s.Start().Byte && offset <= s.End().Byte
}

type rawTaggedSelection struct {
	sel       Selection
	isPrimary bool
}

// NormalizeSelections sorts selections by Start().Byte ascending and collapses
// overlapping, abutting, and duplicate selections into disjoint intervals while
// tracking the primary cursor index accurately.
func NormalizeSelections(selections []Selection, primaryIndex int) ([]Selection, int) {
	if len(selections) == 0 {
		return []Selection{NewCursor(Position{})}, 0
	}
	if primaryIndex < 0 || primaryIndex >= len(selections) {
		primaryIndex = len(selections) - 1
	}

	tagged := make([]rawTaggedSelection, len(selections))
	for i, s := range selections {
		tagged[i] = rawTaggedSelection{
			sel:       s,
			isPrimary: (i == primaryIndex),
		}
	}

	// 1. Stable Sort by Start().Byte ascending, then End().Byte ascending
	sort.SliceStable(tagged, func(i, j int) bool {
		si, sj := tagged[i].sel.Start(), tagged[j].sel.Start()
		if si.Byte != sj.Byte {
			return si.Byte < sj.Byte
		}
		return tagged[i].sel.End().Byte < tagged[j].sel.End().Byte
	})

	// 2. Linear Scan & Merge
	merged := make([]rawTaggedSelection, 0, len(tagged))
	for _, curr := range tagged {
		if len(merged) == 0 {
			merged = append(merged, curr)
			continue
		}

		lastIdx := len(merged) - 1
		last := &merged[lastIdx]

		// Check merge condition: curr.Start().Byte <= last.End().Byte
		if curr.sel.Start().Byte <= last.sel.End().Byte {
			newStart := last.sel.Start()
			newEnd := last.sel.End()
			if curr.sel.End().Byte > newEnd.Byte {
				newEnd = curr.sel.End()
			}

			wasPrimary := last.isPrimary || curr.isPrimary

			// Directionality: preserve curr if primary, else last
			var isForward bool
			if curr.isPrimary {
				isForward = curr.sel.IsForward()
			} else {
				isForward = last.sel.IsForward()
			}

			var mergedSel Selection
			if isForward {
				mergedSel = Selection{Anchor: newStart, Head: newEnd}
			} else {
				mergedSel = Selection{Anchor: newEnd, Head: newStart}
			}

			last.sel = mergedSel
			last.isPrimary = wasPrimary
		} else {
			merged = append(merged, curr)
		}
	}

	result := make([]Selection, len(merged))
	newPrimaryIndex := 0
	for i, m := range merged {
		result[i] = m.sel
		if m.isPrimary {
			newPrimaryIndex = i
		}
	}

	return result, newPrimaryIndex
}

// IsWordRune reports whether r is considered part of an alphanumeric word token.
func IsWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// FindWordBoundsAt returns the [startByte, endByte) offsets of the word surrounding byteOffset in data.
// If byteOffset does not point to a word rune, returns (byteOffset, byteOffset).
func FindWordBoundsAt(data []byte, byteOffset int) (int, int) {
	if len(data) == 0 {
		return 0, 0
	}
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(data) {
		byteOffset = len(data)
	}

	// Check if the rune AT byteOffset is a word rune
	if byteOffset >= len(data) {
		return byteOffset, byteOffset
	}
	r, _ := utf8.DecodeRune(data[byteOffset:])
	if !IsWordRune(r) {
		return byteOffset, byteOffset
	}

	// Scan left for start
	start := byteOffset
	for start > 0 {
		r, sz := utf8.DecodeLastRune(data[:start])
		if !IsWordRune(r) {
			break
		}
		start -= sz
	}

	// Scan right for end
	end := byteOffset
	for end < len(data) {
		r, sz := utf8.DecodeRune(data[end:])
		if !IsWordRune(r) {
			break
		}
		end += sz
	}

	return start, end
}

// TextEdit represents a text replacement operation on the buffer.
type TextEdit struct {
	StartByte int
	EndByte   int
	NewText   string
}

// BottomToTopDeltas verifies that a slice of TextEdit are applied strictly
// in descending StartByte order (bottom-to-top) to eliminate coordinate drift.
func SortEditsBottomToTop(edits []TextEdit) {
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].StartByte > edits[j].StartByte
	})
}

// SearchNextMatch searches for the next exact occurrence of query in fullText,
// beginning at fromOffset. If not found after fromOffset, wraps around from 0.
// Returns startByte, endByte, and true if found.
func SearchNextMatch(fullText []byte, query []byte, fromOffset int) (int, int, bool) {
	if len(query) == 0 || len(fullText) == 0 {
		return 0, 0, false
	}

	if fromOffset < 0 {
		fromOffset = 0
	}

	// Forward search from fromOffset to end
	if fromOffset < len(fullText) {
		idx := bytes.Index(fullText[fromOffset:], query)
		if idx != -1 {
			matchStart := fromOffset + idx
			return matchStart, matchStart + len(query), true
		}
	}

	// Wrap search from 0 to fromOffset
	if fromOffset > 0 {
		limit := fromOffset + len(query) - 1
		if limit > len(fullText) {
			limit = len(fullText)
		}
		idx := bytes.Index(fullText[:limit], query)
		if idx != -1 {
			return idx, idx + len(query), true
		}
	}

	return 0, 0, false
}
