package buffer

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

func TestRopeEmpty(t *testing.T) {
	r := NewRope()
	if r.TotalBytes() != 0 {
		t.Fatalf("expected 0 bytes, got %d", r.TotalBytes())
	}
	if r.TotalLines() != 1 {
		t.Fatalf("expected 1 line, got %d", r.TotalLines())
	}

	offset, err := r.ByteOffsetForLine(0)
	if err != nil || offset != 0 {
		t.Fatalf("expected offset 0 for line 0, got offset=%d, err=%v", offset, err)
	}

	line, err := r.LineForByteOffset(0)
	if err != nil || line != 0 {
		t.Fatalf("expected line 0 for offset 0, got line=%d, err=%v", line, err)
	}

	lineBytes, err := r.GetLine(0)
	if err != nil || len(lineBytes) != 0 {
		t.Fatalf("expected empty line 0, got %q, err=%v", lineBytes, err)
	}

	// Out of bounds tests
	if _, err := r.ByteOffsetForLine(-1); err != ErrLineOutOfBounds {
		t.Fatalf("expected ErrLineOutOfBounds, got %v", err)
	}
	if _, err := r.ByteOffsetForLine(1); err != ErrLineOutOfBounds {
		t.Fatalf("expected ErrLineOutOfBounds, got %v", err)
	}
	if _, err := r.LineForByteOffset(-1); err != ErrOffsetOutOfBounds {
		t.Fatalf("expected ErrOffsetOutOfBounds, got %v", err)
	}
	if _, err := r.LineForByteOffset(1); err != ErrOffsetOutOfBounds {
		t.Fatalf("expected ErrOffsetOutOfBounds, got %v", err)
	}
}

func TestRopeInsertAndDelete(t *testing.T) {
	r := NewRope()

	// Insert "Hello, World!"
	err := r.Insert(0, []byte("Hello, World!"))
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if r.TotalBytes() != 13 {
		t.Fatalf("expected TotalBytes 13, got %d", r.TotalBytes())
	}

	sl, err := r.Slice(0, 13)
	if err != nil || string(sl) != "Hello, World!" {
		t.Fatalf("Slice(0, 13) = %q, err=%v", sl, err)
	}

	// Insert "Beautiful " at offset 7
	err = r.Insert(7, []byte("Beautiful "))
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	sl, err = r.Slice(0, r.TotalBytes())
	if err != nil || string(sl) != "Hello, Beautiful World!" {
		t.Fatalf("Slice = %q, err=%v", sl, err)
	}

	// Delete "Beautiful " (10 bytes at offset 7)
	err = r.Delete(7, 10)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	sl, err = r.Slice(0, r.TotalBytes())
	if err != nil || string(sl) != "Hello, World!" {
		t.Fatalf("Slice = %q, err=%v", sl, err)
	}

	// Delete all
	err = r.Delete(0, 13)
	if err != nil {
		t.Fatalf("Delete all failed: %v", err)
	}
	if r.TotalBytes() != 0 {
		t.Fatalf("expected 0 bytes, got %d", r.TotalBytes())
	}
}

func TestRopeLineSeeking(t *testing.T) {
	text := "Line 0\nLine 1: Hello\nLine 2: World\nLine 3: End"
	r := NewRope()
	err := r.Insert(0, []byte(text))
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	if r.TotalLines() != 4 {
		t.Fatalf("expected 4 lines, got %d", r.TotalLines())
	}

	expectedOffsets := []int{0, 7, 21, 35}
	for i, expected := range expectedOffsets {
		offset, err := r.ByteOffsetForLine(i)
		if err != nil {
			t.Fatalf("ByteOffsetForLine(%d) err: %v", i, err)
		}
		if offset != expected {
			t.Fatalf("ByteOffsetForLine(%d) = %d, expected %d", i, offset, expected)
		}

		lineIdx, err := r.LineForByteOffset(offset)
		if err != nil {
			t.Fatalf("LineForByteOffset(%d) err: %v", offset, err)
		}
		if lineIdx != i {
			t.Fatalf("LineForByteOffset(%d) = %d, expected %d", offset, lineIdx, i)
		}
	}

	// Test line contents
	l0, err := r.GetLine(0)
	if err != nil || string(l0) != "Line 0\n" {
		t.Fatalf("GetLine(0) = %q, expected 'Line 0\\n'", l0)
	}

	l3, err := r.GetLine(3)
	if err != nil || string(l3) != "Line 3: End" {
		t.Fatalf("GetLine(3) = %q, expected 'Line 3: End'", l3)
	}
}

func TestRopeSplitsAndMerges(t *testing.T) {
	// Build a larger text that spans multiple leaves (> MaxChunkSize = 2048)
	var buf bytes.Buffer
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&buf, "This is line number %04d in the test document.\n", i)
	}
	raw := buf.Bytes()

	node := BuildTreeFromBytes(raw)
	r := &Rope{root: node, scratch: make([]byte, 0, 1024)}

	if r.TotalBytes() != len(raw) {
		t.Fatalf("TotalBytes = %d, expected %d", r.TotalBytes(), len(raw))
	}
	if r.TotalLines() != 501 {
		t.Fatalf("TotalLines = %d, expected 501", r.TotalLines())
	}

	// Verify AVL height balance: h <= 1.44 * log2(leaves) + 2
	h := nodeHeight(r.root)
	maxAllowedHeight := int16(1.44*math.Log2(float64(len(raw)/MinChunkSize+1)) + 4)
	if h > maxAllowedHeight {
		t.Fatalf("tree height %d exceeds bound %d", h, maxAllowedHeight)
	}

	// Test split and concat at various split points
	splitPoints := []int{10, 500, 2048, 4096, len(raw) / 2, len(raw) - 50}
	for _, sp := range splitPoints {
		n1, n2 := Split(r.root, sp)
		if nodeWeight(n1) != sp {
			t.Fatalf("Split at %d: left weight %d != %d", sp, nodeWeight(n1), sp)
		}
		if nodeWeight(n2) != len(raw)-sp {
			t.Fatalf("Split at %d: right weight %d != %d", sp, nodeWeight(n2), len(raw)-sp)
		}

		joined := Concat(n1, n2)
		r.root = joined
		if r.TotalBytes() != len(raw) {
			t.Fatalf("Concat after split at %d: TotalBytes %d != %d", sp, r.TotalBytes(), len(raw))
		}

		sl, err := r.Slice(0, r.TotalBytes())
		if err != nil || !bytes.Equal(sl, raw) {
			t.Fatalf("data corrupted after split/concat at %d", sp)
		}
	}
}

func TestRopeChunkIterator(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&buf, "Line %03d: Testing chunk iterator streaming functionality.\n", i)
	}
	raw := buf.Bytes()
	r := &Rope{root: BuildTreeFromBytes(raw)}

	it := r.Iterator()
	var reconstructed bytes.Buffer
	chunkCount := 0

	for {
		chunk, ok := it.Next()
		if !ok {
			break
		}
		chunkCount++
		reconstructed.Write(chunk)
	}

	if chunkCount <= 1 {
		t.Fatalf("expected multiple chunks, got %d", chunkCount)
	}
	if !bytes.Equal(reconstructed.Bytes(), raw) {
		t.Fatalf("ChunkIterator reconstructed data does not match original")
	}
}

func TestRopeUTF8RuneBoundarySplitting(t *testing.T) {
	// 4-byte emoji repeated: 👋 (U+1F44B, 4 bytes: 0xF0 0x9F 0x91 0x8B)
	emoji := []byte("👋")
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		buf.Write(emoji)
	}
	raw := buf.Bytes()

	node := BuildTreeFromBytes(raw)
	r := &Rope{root: node}

	// Verify all leaves have weights that are multiples of 4 (no split emojis)
	it := r.Iterator()
	for {
		chunk, ok := it.Next()
		if !ok {
			break
		}
		if len(chunk)%4 != 0 {
			t.Fatalf("chunk split in middle of 4-byte UTF-8 emoji! chunk len = %d", len(chunk))
		}
	}
}

func TestRopeNormalizeCRLF(t *testing.T) {
	crlfData := []byte("Line 1\r\nLine 2\r\nLine 3\r\n")
	normalized, hasCRLF := NormalizeCRLF(crlfData)
	if !hasCRLF {
		t.Fatalf("expected hasCRLF = true")
	}
	expected := []byte("Line 1\nLine 2\nLine 3\n")
	if !bytes.Equal(normalized, expected) {
		t.Fatalf("NormalizeCRLF got %q, expected %q", normalized, expected)
	}

	// Standalone LF
	lfData := []byte("Line 1\nLine 2\n")
	normalizedLF, hasCRLF2 := NormalizeCRLF(lfData)
	if hasCRLF2 {
		t.Fatalf("expected hasCRLF = false")
	}
	if !bytes.Equal(normalizedLF, lfData) {
		t.Fatalf("NormalizeCRLF altered LF data")
	}
}
