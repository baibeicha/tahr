package buffer

import (
	"testing"
)

func TestPositionLess(t *testing.T) {
	p1 := Position{Line: 0, Column: 5, Byte: 10}
	p2 := Position{Line: 1, Column: 0, Byte: 15}
	p3 := Position{Line: 0, Column: 6, Byte: 10}

	if !p1.Less(p2) {
		t.Fatalf("expected p1 < p2")
	}
	if p2.Less(p1) {
		t.Fatalf("expected p2 not < p1")
	}
	if !p1.Less(p3) {
		t.Fatalf("expected p1 < p3 due to column difference")
	}
}

func TestSelectionProps(t *testing.T) {
	p0 := Position{Line: 0, Column: 0, Byte: 0}
	p10 := Position{Line: 0, Column: 10, Byte: 10}

	caret := NewCursor(p0)
	if !caret.IsEmpty() {
		t.Fatalf("expected caret.IsEmpty() == true")
	}
	if !caret.IsForward() {
		t.Fatalf("expected caret.IsForward() == true")
	}
	if caret.ByteLength() != 0 {
		t.Fatalf("expected caret.ByteLength() == 0")
	}

	forward := NewSelection(p0, p10)
	if forward.IsEmpty() {
		t.Fatalf("expected forward not empty")
	}
	if !forward.IsForward() {
		t.Fatalf("expected forward.IsForward() == true")
	}
	if forward.Start() != p0 || forward.End() != p10 {
		t.Fatalf("forward Start/End mismatch")
	}
	if forward.ByteLength() != 10 {
		t.Fatalf("expected forward.ByteLength() == 10, got %d", forward.ByteLength())
	}
	if !forward.ContainsByte(5) || !forward.ContainsByte(0) || !forward.ContainsByte(10) {
		t.Fatalf("ContainsByte failed")
	}
	if forward.ContainsByte(11) {
		t.Fatalf("ContainsByte(11) should be false")
	}

	backward := NewSelection(p10, p0)
	if backward.IsForward() {
		t.Fatalf("expected backward.IsForward() == false")
	}
	if backward.Start() != p0 || backward.End() != p10 {
		t.Fatalf("backward Start/End mismatch")
	}
	if backward.ByteLength() != 10 {
		t.Fatalf("expected backward.ByteLength() == 10")
	}
}

func TestNormalizeSelections(t *testing.T) {
	p := func(b int) Position {
		return Position{Line: 0, Column: b, Byte: b}
	}

	// 1. Single cursor
	s1 := []Selection{NewCursor(p(5))}
	norm, pIdx := NormalizeSelections(s1, 0)
	if len(norm) != 1 || norm[0].Start().Byte != 5 || pIdx != 0 {
		t.Fatalf("Single cursor normalize failed")
	}

	// 2. Out of order selections
	s2 := []Selection{
		NewSelection(p(30), p(40)),
		NewSelection(p(10), p(20)),
	}
	norm, pIdx = NormalizeSelections(s2, 0) // primary was index 0 ([30, 40))
	if len(norm) != 2 || norm[0].Start().Byte != 10 || norm[1].Start().Byte != 30 {
		t.Fatalf("Out-of-order sorting failed: %+v", norm)
	}
	if pIdx != 1 { // [30, 40) is now at index 1
		t.Fatalf("expected pIdx 1, got %d", pIdx)
	}

	// 3. Overlapping ranges: [5, 12) and [10, 18) -> [5, 18)
	s3 := []Selection{
		NewSelection(p(5), p(12)),
		NewSelection(p(10), p(18)),
	}
	norm, pIdx = NormalizeSelections(s3, 1)
	if len(norm) != 1 || norm[0].Start().Byte != 5 || norm[0].End().Byte != 18 {
		t.Fatalf("Overlapping ranges failed: %+v", norm)
	}
	if pIdx != 0 {
		t.Fatalf("expected pIdx 0, got %d", pIdx)
	}

	// 4. Abutting ranges: [5, 10) and [10, 15) -> [5, 15)
	s4 := []Selection{
		NewSelection(p(5), p(10)),
		NewSelection(p(10), p(15)),
	}
	norm, pIdx = NormalizeSelections(s4, 0)
	if len(norm) != 1 || norm[0].Start().Byte != 5 || norm[0].End().Byte != 15 {
		t.Fatalf("Abutting ranges failed: %+v", norm)
	}

	// 5. Boundary caret absorbed: [5, 10) and [10, 10) -> [5, 10)
	s5 := []Selection{
		NewSelection(p(5), p(10)),
		NewCursor(p(10)),
	}
	norm, _ = NormalizeSelections(s5, 0)
	if len(norm) != 1 || norm[0].Start().Byte != 5 || norm[0].End().Byte != 10 {
		t.Fatalf("Boundary caret absorb failed: %+v", norm)
	}

	// 6. Duplicate carets coalesced: [5, 5) and [5, 5) -> [5, 5)
	s6 := []Selection{
		NewCursor(p(5)),
		NewCursor(p(5)),
	}
	norm, _ = NormalizeSelections(s6, 1)
	if len(norm) != 1 || norm[0].Start().Byte != 5 || !norm[0].IsEmpty() {
		t.Fatalf("Duplicate carets coalesced failed: %+v", norm)
	}
}

func TestFindWordBoundsAt(t *testing.T) {
	data := []byte("func handle_request(id int) {")

	// Middle of "handle_request"
	start, end := FindWordBoundsAt(data, 7)
	if string(data[start:end]) != "handle_request" {
		t.Fatalf("expected 'handle_request', got %q", string(data[start:end]))
	}

	// At start of "func"
	start, end = FindWordBoundsAt(data, 0)
	if string(data[start:end]) != "func" {
		t.Fatalf("expected 'func', got %q", string(data[start:end]))
	}

	// On whitespace
	start, end = FindWordBoundsAt(data, 4)
	if start != end {
		t.Fatalf("expected empty bounds on space, got [%d, %d)", start, end)
	}

	// On punctuation '('
	start, end = FindWordBoundsAt(data, 19)
	if start != end {
		t.Fatalf("expected empty bounds on '(', got [%d, %d)", start, end)
	}
}

func TestSearchNextMatch(t *testing.T) {
	text := []byte("foo bar foo baz foo")
	query := []byte("foo")

	// Search from offset 0 -> matches [0, 3)
	s, e, found := SearchNextMatch(text, query, 0)
	if !found || s != 0 || e != 3 {
		t.Fatalf("search from 0: got [%d, %d), found=%v", s, e, found)
	}

	// Search from offset 3 -> matches [8, 11)
	s, e, found = SearchNextMatch(text, query, 3)
	if !found || s != 8 || e != 11 {
		t.Fatalf("search from 3: got [%d, %d), found=%v", s, e, found)
	}

	// Search from offset 12 -> matches [16, 19)
	s, e, found = SearchNextMatch(text, query, 12)
	if !found || s != 16 || e != 19 {
		t.Fatalf("search from 12: got [%d, %d), found=%v", s, e, found)
	}

	// Search from offset 19 -> wraps to [0, 3)
	s, e, found = SearchNextMatch(text, query, 19)
	if !found || s != 0 || e != 3 {
		t.Fatalf("wrap search from 19: got [%d, %d), found=%v", s, e, found)
	}
}

func TestSortEditsBottomToTop(t *testing.T) {
	edits := []TextEdit{
		{StartByte: 10, EndByte: 15, NewText: "A"},
		{StartByte: 50, EndByte: 60, NewText: "B"},
		{StartByte: 5, EndByte: 8, NewText: "C"},
	}

	SortEditsBottomToTop(edits)

	if edits[0].StartByte != 50 || edits[1].StartByte != 10 || edits[2].StartByte != 5 {
		t.Fatalf("SortEditsBottomToTop failed: %+v", edits)
	}
}
