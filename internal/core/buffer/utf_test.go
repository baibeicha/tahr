package buffer

import (
	"testing"
)

type mockLineProvider struct {
	lines []string
}

func newMockLineProvider(lines []string) *mockLineProvider {
	return &mockLineProvider{lines: lines}
}

func (m *mockLineProvider) TotalLines() int {
	return len(m.lines)
}

func (m *mockLineProvider) TotalBytes() int {
	total := 0
	for _, l := range m.lines {
		total += len(l)
	}
	return total
}

func (m *mockLineProvider) ByteOffsetForLine(lineIdx int) (int, error) {
	if lineIdx < 0 || lineIdx >= len(m.lines) {
		return 0, ErrLineOutOfBounds
	}
	offset := 0
	for i := 0; i < lineIdx; i++ {
		offset += len(m.lines[i])
	}
	return offset, nil
}

func (m *mockLineProvider) LineForByteOffset(offset int) (int, error) {
	if offset < 0 || offset > m.TotalBytes() {
		return 0, ErrOffsetOutOfBounds
	}
	accum := 0
	for i, l := range m.lines {
		accum += len(l)
		if offset < accum {
			return i, nil
		}
	}
	return len(m.lines) - 1, nil
}

func (m *mockLineProvider) GetLine(lineIdx int) ([]byte, error) {
	if lineIdx < 0 || lineIdx >= len(m.lines) {
		return nil, ErrLineOutOfBounds
	}
	return []byte(m.lines[lineIdx]), nil
}

func TestUTFCyrillicCoordinates(t *testing.T) {
	// "Привет" has 6 runes:
	// 'П' (2 bytes), 'р' (2 bytes), 'и' (2 bytes), 'в' (2 bytes), 'е' (2 bytes), 'т' (2 bytes)
	// Total 12 bytes.
	// All codepoints in BMP (U+041F, U+0440, etc.), so 1 UTF-16 code unit each -> 6 LSP units.
	// Visual width = 1 cell each -> 6 visual cols.
	text := "Привет\n"
	prov := newMockLineProvider([]string{text})
	bridge := NewUTFBridge(prov, 4)

	for runeCol := 0; runeCol <= 6; runeCol++ {
		expectedByte := runeCol * 2
		expectedLSP := runeCol
		expectedVisual := runeCol

		// Document position
		pos := Position{Line: 0, Column: runeCol, Byte: expectedByte}

		// 1. Position to Byte
		byteOffset := bridge.PositionToByte(pos)
		if byteOffset != expectedByte {
			t.Fatalf("runeCol %d: PositionToByte = %d, expected %d", runeCol, byteOffset, expectedByte)
		}

		// 2. Byte to Position
		pBack := bridge.ByteToPosition(expectedByte)
		if pBack.Line != 0 || pBack.Column != runeCol || pBack.Byte != expectedByte {
			t.Fatalf("runeCol %d: ByteToPosition = %+v, expected %+v", runeCol, pBack, pos)
		}

		// 3. Position to LSP
		line, lspChar := bridge.PositionToLSP(pos)
		if line != 0 || lspChar != expectedLSP {
			t.Fatalf("runeCol %d: PositionToLSP = (%d, %d), expected (0, %d)", runeCol, line, lspChar, expectedLSP)
		}

		// 4. LSP to Position
		pLSP := bridge.LSPToPosition(0, expectedLSP)
		if pLSP.Line != 0 || pLSP.Column != runeCol || pLSP.Byte != expectedByte {
			t.Fatalf("runeCol %d: LSPToPosition = %+v, expected %+v", runeCol, pLSP, pos)
		}

		// 5. Visual Column
		visCol := bridge.VisualColumn(0, runeCol)
		if visCol != expectedVisual {
			t.Fatalf("runeCol %d: VisualColumn = %d, expected %d", runeCol, visCol, expectedVisual)
		}

		// 6. Visual to Rune Column
		rCol := bridge.RuneColFromVisual(0, visCol)
		if rCol != runeCol {
			t.Fatalf("visCol %d: RuneColFromVisual = %d, expected %d", visCol, rCol, runeCol)
		}
	}
}

func TestUTFEmojiCoordinates(t *testing.T) {
	// "👋🚀" has 2 runes:
	// '👋' (U+1F44B, 4 bytes: 0xF0 0x9F 0x91 0x8B), width = 2 cells, UTF-16 = 2 code units (surrogate pair)
	// '🚀' (U+1F680, 4 bytes: 0xF0 0x9F 0x9A 0x80), width = 2 cells, UTF-16 = 2 code units (surrogate pair)
	// Total 8 bytes, 4 LSP units, 4 visual cols.
	text := "👋🚀\n"
	prov := newMockLineProvider([]string{text})
	bridge := NewUTFBridge(prov, 4)

	// Rune col 0: start
	p0 := bridge.ByteToPosition(0)
	if p0.Column != 0 || p0.Byte != 0 {
		t.Fatalf("p0 mismatch: %+v", p0)
	}
	l0, lsp0 := bridge.PositionToLSP(p0)
	if l0 != 0 || lsp0 != 0 {
		t.Fatalf("lsp0 mismatch: %d, %d", l0, lsp0)
	}
	if v := bridge.VisualColumn(0, 0); v != 0 {
		t.Fatalf("v0 mismatch: %d", v)
	}

	// Rune col 1: after '👋'
	p1 := bridge.ByteToPosition(4)
	if p1.Column != 1 || p1.Byte != 4 {
		t.Fatalf("p1 mismatch: %+v", p1)
	}
	_, lsp1 := bridge.PositionToLSP(p1)
	if lsp1 != 2 { // Surrogate pair takes 2 code units
		t.Fatalf("lsp1 expected 2, got %d", lsp1)
	}
	if v := bridge.VisualColumn(0, 1); v != 2 { // Wide emoji takes 2 visual cells
		t.Fatalf("v1 expected 2, got %d", v)
	}

	// Rune col 2: after '🚀'
	p2 := bridge.ByteToPosition(8)
	if p2.Column != 2 || p2.Byte != 8 {
		t.Fatalf("p2 mismatch: %+v", p2)
	}
	_, lsp2 := bridge.PositionToLSP(p2)
	if lsp2 != 4 { // Two surrogate pairs = 4 code units
		t.Fatalf("lsp2 expected 4, got %d", lsp2)
	}
	if v := bridge.VisualColumn(0, 2); v != 4 { // Two wide emojis = 4 visual cells
		t.Fatalf("v2 expected 4, got %d", v)
	}
}

func TestUTFHalfwayRounding(t *testing.T) {
	// Line: "👋🚀" (each width 2 cells)
	text := []byte("👋🚀")

	// Clicking cell 0 (left half of '👋'): snaps to rune 0 (before '👋')
	if col := LineVisualToRuneCol(text, 0, 4); col != 0 {
		t.Fatalf("click col 0: expected rune 0, got %d", col)
	}

	// Clicking cell 1 (right half of '👋'): snaps to rune 1 (after '👋')
	if col := LineVisualToRuneCol(text, 1, 4); col != 1 {
		t.Fatalf("click col 1: expected rune 1, got %d", col)
	}

	// Clicking cell 2 (left half of '🚀'): snaps to rune 1 (before '🚀')
	if col := LineVisualToRuneCol(text, 2, 4); col != 1 {
		t.Fatalf("click col 2: expected rune 1, got %d", col)
	}

	// Clicking cell 3 (right half of '🚀'): snaps to rune 2 (after '🚀')
	if col := LineVisualToRuneCol(text, 3, 4); col != 2 {
		t.Fatalf("click col 3: expected rune 2, got %d", col)
	}

	// Clicking beyond line end: clamps to rune 2
	if col := LineVisualToRuneCol(text, 10, 4); col != 2 {
		t.Fatalf("click col 10: expected rune 2, got %d", col)
	}
}

func TestUTFLSPSurrogateForwardSnapping(t *testing.T) {
	// '👋' is surrogate pair (units 0 and 1)
	// '🚀' is surrogate pair (units 2 and 3)
	text := []byte("👋🚀")

	// LSP unit 0 -> rune 0
	r0, b0 := LineLSPToRuneAndByte(text, 0)
	if r0 != 0 || b0 != 0 {
		t.Fatalf("LSP 0: expected (0, 0), got (%d, %d)", r0, b0)
	}

	// LSP unit 1 is in the MIDDLE of '👋' surrogate pair!
	// Must snap forward to include the full rune (rune 1, byte 4).
	r1, b1 := LineLSPToRuneAndByte(text, 1)
	if r1 != 1 || b1 != 4 {
		t.Fatalf("LSP 1 mid-surrogate: expected forward snap (1, 4), got (%d, %d)", r1, b1)
	}

	// LSP unit 2 -> rune 1, byte 4
	r2, b2 := LineLSPToRuneAndByte(text, 2)
	if r2 != 1 || b2 != 4 {
		t.Fatalf("LSP 2: expected (1, 4), got (%d, %d)", r2, b2)
	}

	// LSP unit 3 is in the MIDDLE of '🚀' surrogate pair!
	// Must snap forward to rune 2, byte 8.
	r3, b3 := LineLSPToRuneAndByte(text, 3)
	if r3 != 2 || b3 != 8 {
		t.Fatalf("LSP 3 mid-surrogate: expected forward snap (2, 8), got (%d, %d)", r3, b3)
	}

	// LSP unit 4 -> rune 2, byte 8
	r4, b4 := LineLSPToRuneAndByte(text, 4)
	if r4 != 2 || b4 != 8 {
		t.Fatalf("LSP 4: expected (2, 8), got (%d, %d)", r4, b4)
	}
}

func TestUTFTabExpansion(t *testing.T) {
	// "\ta\n" with tabWidth 4
	// Tab occupies visual cells [0, 4)
	// 'a' occupies visual cell 4
	line := []byte("\ta\n")

	// Rune col 0: before tab -> visual col 0
	if v := LineRuneColToVisual(line, 0, 4); v != 0 {
		t.Fatalf("col 0 visual: expected 0, got %d", v)
	}

	// Rune col 1: after tab -> visual col 4
	if v := LineRuneColToVisual(line, 1, 4); v != 4 {
		t.Fatalf("col 1 visual: expected 4, got %d", v)
	}

	// Rune col 2: after 'a' -> visual col 5
	if v := LineRuneColToVisual(line, 2, 4); v != 5 {
		t.Fatalf("col 2 visual: expected 5, got %d", v)
	}

	// Halfway rounding on tab: width is 4, threshold is (4+1)/2 = 2.
	// Visual col 0, 1 -> snaps to rune 0
	// Visual col 2, 3 -> snaps to rune 1
	if r := LineVisualToRuneCol(line, 0, 4); r != 0 {
		t.Fatalf("vis 0: expected rune 0, got %d", r)
	}
	if r := LineVisualToRuneCol(line, 1, 4); r != 0 {
		t.Fatalf("vis 1: expected rune 0, got %d", r)
	}
	if r := LineVisualToRuneCol(line, 2, 4); r != 1 {
		t.Fatalf("vis 2: expected rune 1, got %d", r)
	}
	if r := LineVisualToRuneCol(line, 3, 4); r != 1 {
		t.Fatalf("vis 3: expected rune 1, got %d", r)
	}
	if r := LineVisualToRuneCol(line, 4, 4); r != 1 {
		t.Fatalf("vis 4: expected rune 1 (before 'a'), got %d", r)
	}
	if r := LineVisualToRuneCol(line, 5, 4); r != 2 {
		t.Fatalf("vis 5: expected rune 2 (after 'a'), got %d", r)
	}
}
