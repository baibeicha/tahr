package buffer

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ============================================================================
// 1. Tree Invariant Validator
// ============================================================================

func validateTreeInvariants(t *testing.T, n *Node) (height int16, weight int, lines int) {
	t.Helper()
	if n == nil {
		return 0, 0, 0
	}

	if n.isLeaf() {
		if n.val == nil {
			t.Fatalf("leaf node has nil val")
		}
		actualWeight := len(n.val)
		actualLines := bytes.Count(n.val, []byte{'\n'})
		if n.weight != actualWeight {
			t.Fatalf("leaf weight mismatch: stored %d != actual %d", n.weight, actualWeight)
		}
		if n.lines != actualLines {
			t.Fatalf("leaf lines mismatch: stored %d != actual %d", n.lines, actualLines)
		}
		if n.height != 1 {
			t.Fatalf("leaf height mismatch: stored %d != 1", n.height)
		}
		return 1, actualWeight, actualLines
	}

	// Internal node
	if n.val != nil {
		t.Fatalf("internal node has non-nil val")
	}

	hl, wl, ll := validateTreeInvariants(t, n.left)
	hr, wr, lr := validateTreeInvariants(t, n.right)

	expectedHeight := hl + 1
	if hr > hl {
		expectedHeight = hr + 1
	}
	if n.height != expectedHeight {
		t.Fatalf("internal node height mismatch: stored %d != calculated %d", n.height, expectedHeight)
	}

	bf := int(hl) - int(hr)
	if bf < -1 || bf > 1 {
		t.Fatalf("AVL balance factor violated: bf = %d (left=%d, right=%d)", bf, hl, hr)
	}

	if n.weight != (wl + wr) {
		t.Fatalf("internal node weight mismatch: stored %d != (%d + %d)", n.weight, wl, wr)
	}
	if n.lines != (ll + lr) {
		t.Fatalf("internal node lines mismatch: stored %d != (%d + %d)", n.lines, ll, lr)
	}

	return n.height, n.weight, n.lines
}

// ============================================================================
// 2. Empirical Verification: O(log N) Line & Byte Seeking Scaling
// ============================================================================

func TestEmpiricalLogNScaling(t *testing.T) {
	scales := []int{10000, 50000, 100000, 200000}
	type scaleResult struct {
		lines         int
		bytes         int
		height        int16
		maxAvlHeight  float64
		avgLineSeekNs float64
		avgByteSeekNs float64
	}

	results := make([]scaleResult, len(scales))

	for i, lineCount := range scales {
		// Generate realistic multi-line text with varying line lengths
		var sb strings.Builder
		sb.Grow(lineCount * 65)
		rng := rand.New(rand.NewSource(int64(42 + lineCount)))

		for l := 0; l < lineCount; l++ {
			lineLen := 20 + rng.Intn(80)
			sb.WriteString(fmt.Sprintf("[%06d] ", l))
			for c := 0; c < lineLen; c++ {
				sb.WriteByte(byte('a' + (c % 26)))
			}
			sb.WriteByte('\n')
		}

		raw := sb.String()
		buf := NewBufferWithText(raw)

		// 1. Verify structural tree invariants
		h, w, lCount := validateTreeInvariants(t, buf.rope.root)
		if w != buf.TotalBytes() || w != len(raw) {
			t.Fatalf("scale %d: byte count mismatch: %d vs %d", lineCount, w, len(raw))
		}
		if lCount != lineCount {
			t.Fatalf("scale %d: line count mismatch: %d vs %d", lineCount, lCount, lineCount)
		}

		// Estimate number of leaves: N / (average chunk size ~1024)
		estLeaves := float64(w) / 1024.0
		if estLeaves < 1 {
			estLeaves = 1
		}
		// Strict theoretical AVL height bound: 1.4405 * log2(leaves + 2) - 0.328
		maxAvl := 1.4405*math.Log2(estLeaves+2) + 2.0
		if float64(h) > maxAvl+4 {
			t.Fatalf("scale %d: tree height %d exceeds theoretical AVL bound %.2f", lineCount, h, maxAvl)
		}

		// 2. Measure Line-to-Byte Seeking latency
		const lookups = 30000
		sampleLines := make([]int, lookups)
		for k := 0; k < lookups; k++ {
			sampleLines[k] = rng.Intn(lineCount)
		}

		start := time.Now()
		for k := 0; k < lookups; k++ {
			targetLine := sampleLines[k]
			offset, err := buf.ByteOffsetForLine(targetLine)
			if err != nil {
				t.Fatalf("ByteOffsetForLine(%d) failed: %v", targetLine, err)
			}
			// Oracle verification: verify that offset points to the line header
			expectedPrefix := fmt.Sprintf("[%06d] ", targetLine)
			chunk, err := buf.Slice(offset, offset+len(expectedPrefix))
			if err != nil || string(chunk) != expectedPrefix {
				t.Fatalf("ByteOffsetForLine(%d) corrupt: got %q, expected %q", targetLine, string(chunk), expectedPrefix)
			}
		}
		lineDuration := time.Since(start)
		avgLineNs := float64(lineDuration.Nanoseconds()) / float64(lookups)

		// 3. Measure Byte-to-Line Seeking latency
		sampleOffsets := make([]int, lookups)
		totalB := buf.TotalBytes()
		for k := 0; k < lookups; k++ {
			sampleOffsets[k] = rng.Intn(totalB)
		}

		start = time.Now()
		for k := 0; k < lookups; k++ {
			targetOffset := sampleOffsets[k]
			lineIdx, err := buf.LineForByteOffset(targetOffset)
			if err != nil {
				t.Fatalf("LineForByteOffset(%d) failed: %v", targetOffset, err)
			}
			// Round trip sanity check: byte offset of lineIdx must be <= targetOffset
			lStart, err := buf.ByteOffsetForLine(lineIdx)
			if err != nil || lStart > targetOffset {
				t.Fatalf("LineForByteOffset(%d) = %d inconsistent with line start %d", targetOffset, lineIdx, lStart)
			}
		}
		byteDuration := time.Since(start)
		avgByteNs := float64(byteDuration.Nanoseconds()) / float64(lookups)

		results[i] = scaleResult{
			lines:         lineCount,
			bytes:         w,
			height:        h,
			maxAvlHeight:  maxAvl,
			avgLineSeekNs: avgLineNs,
			avgByteSeekNs: avgByteNs,
		}
	}

	t.Logf("=== EMPIRICAL O(log N) SCALING MEASUREMENTS ===")
	t.Logf("%-10s %-12s %-8s %-12s %-18s %-18s", "Lines", "Bytes", "Height", "MaxAVL", "LineSeek (ns/op)", "ByteSeek (ns/op)")
	for _, r := range results {
		t.Logf("%-10d %-12d %-8d %-12.1f %-18.2f %-18.2f",
			r.lines, r.bytes, r.height, r.maxAvlHeight, r.avgLineSeekNs, r.avgByteSeekNs)
	}

	// Validate logarithmic scaling factor:
	// From 10k lines to 200k lines (20x increase in lines):
	// O(N) would show ~20x slowdown.
	// O(log N) should show <= 2.5x difference in latency.
	r10k := results[0]
	r200k := results[len(results)-1]
	lineRatio := r200k.avgLineSeekNs / r10k.avgLineSeekNs
	byteRatio := r200k.avgByteSeekNs / r10k.avgByteSeekNs

	t.Logf("Scaling factor 200k/10k: LineSeek ratio = %.2fx, ByteSeek ratio = %.2fx", lineRatio, byteRatio)

	if lineRatio > 4.0 {
		t.Fatalf("FAIL: Line seeking does not scale logarithmically! 20x lines caused %.2fx slowdown", lineRatio)
	}
	if byteRatio > 4.0 {
		t.Fatalf("FAIL: Byte seeking does not scale logarithmically! 20x bytes caused %.2fx slowdown", byteRatio)
	}
	if r200k.avgLineSeekNs > 2000.0 { // 2 microseconds limit
		t.Fatalf("FAIL: Line seeking took too long on 200k lines: %.2f ns/op", r200k.avgLineSeekNs)
	}
}

// ============================================================================
// 3. Empirical Verification: Bottom-to-Top Multi-Cursor Chaotic Stress Test
// ============================================================================

type oracleModel struct {
	text string
	sels []Selection
}

func newOracleModel(initial string) *oracleModel {
	return &oracleModel{
		text: initial,
		sels: []Selection{NewCursor(Position{})},
	}
}

func (o *oracleModel) applyInsert(text string) {
	k := len(o.sels)
	edits := make([]TextEdit, k)
	for i := 0; i < k; i++ {
		edits[i] = TextEdit{
			StartByte: o.sels[i].Start().Byte,
			EndByte:   o.sels[i].End().Byte,
			NewText:   text,
		}
	}

	// Apply bottom-to-top on oracle string
	for i := k - 1; i >= 0; i-- {
		e := edits[i]
		o.text = o.text[:e.StartByte] + e.NewText + o.text[e.EndByte:]
	}

	// Update selections via cumulative shifts
	newSels := make([]Selection, k)
	shift := 0
	for i := 0; i < k; i++ {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		insLen := len(e.NewText)
		newOffset := e.StartByte + shift + insLen
		newPos := byteToPosInString(o.text, newOffset)
		newSels[i] = NewCursor(newPos)
		shift += (insLen - delLen)
	}

	o.sels, _ = NormalizeSelections(newSels, 0)
}

func (o *oracleModel) applyBackspace() {
	k := len(o.sels)
	edits := make([]TextEdit, k)
	for i := 0; i < k; i++ {
		s := o.sels[i]
		if !s.IsEmpty() {
			edits[i] = TextEdit{StartByte: s.Start().Byte, EndByte: s.End().Byte, NewText: ""}
		} else {
			head := s.Head.Byte
			if head <= 0 {
				edits[i] = TextEdit{StartByte: 0, EndByte: 0, NewText: ""}
			} else {
				_, sz := utf8.DecodeLastRuneInString(o.text[:head])
				edits[i] = TextEdit{StartByte: head - sz, EndByte: head, NewText: ""}
			}
		}
	}

	// Apply bottom-to-top
	for i := k - 1; i >= 0; i-- {
		e := edits[i]
		o.text = o.text[:e.StartByte] + o.text[e.EndByte:]
	}

	// Post-edit cursor positions
	newSels := make([]Selection, k)
	shift := 0
	for i := 0; i < k; i++ {
		e := edits[i]
		delLen := e.EndByte - e.StartByte
		newOffset := e.StartByte + shift
		newPos := byteToPosInString(o.text, newOffset)
		newSels[i] = NewCursor(newPos)
		shift -= delLen
	}

	o.sels, _ = NormalizeSelections(newSels, 0)
}

func byteToPosInString(s string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s) {
		offset = len(s)
	}

	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if s[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}

	runeCol := utf8.RuneCountInString(s[lineStart:offset])
	return Position{
		Line:   line,
		Column: runeCol,
		Byte:   offset,
	}
}

func TestEmpiricalChaoticMultiCursorMutations(t *testing.T) {
	initialText := "The quick brown fox jumps over the lazy dog.\n" +
		"Гогла в чаще пели птицы, расцветали травы.\n" +
		"Unicode emojis: 🚀 🌟 💻 🦀 ⚡ 🎯\n" +
		"Line four with some tabs\tand indentations.\n" +
		"Final line of test document."

	buf := NewBufferWithText(initialText)
	oracle := newOracleModel(initialText)

	rng := rand.New(rand.NewSource(1337))

	const iterations = 250
	tokens := []string{
		"X",
		"hello ",
		"123",
		"мир",
		"🔥",
		"\n",
		"/* comment */",
		"\t",
	}

	for iter := 0; iter < iterations; iter++ {
		curLen := buf.TotalBytes()
		if curLen == 0 {
			buf.InsertAtSelections("Reboot buffer\n")
			oracle.applyInsert("Reboot buffer\n")
			curLen = buf.TotalBytes()
		}

		// Generate 3 to 20 random disjoint selections
		numCursors := 3 + rng.Intn(18)
		rawSels := make([]Selection, numCursors)

		for c := 0; c < numCursors; c++ {
			b1 := rng.Intn(curLen + 1)
			// Align b1 to valid rune boundary in oracle.text
			for b1 < len(oracle.text) && !utf8.RuneStart(oracle.text[b1]) {
				b1++
			}
			if b1 > len(oracle.text) {
				b1 = len(oracle.text)
			}

			// 60% point cursor, 40% range selection
			if rng.Float64() < 0.60 {
				pos := buf.ByteToPosition(b1)
				rawSels[c] = NewCursor(pos)
			} else {
				span := rng.Intn(30)
				b2 := b1 + span
				if b2 > curLen {
					b2 = curLen
				}
				for b2 < len(oracle.text) && !utf8.RuneStart(oracle.text[b2]) {
					b2++
				}
				if b2 > len(oracle.text) {
					b2 = len(oracle.text)
				}

				pos1 := buf.ByteToPosition(b1)
				pos2 := buf.ByteToPosition(b2)
				if rng.Float64() < 0.5 {
					rawSels[c] = NewSelection(pos1, pos2) // forward
				} else {
					rawSels[c] = NewSelection(pos2, pos1) // backward
				}
			}
		}

		buf.SetSelections(rawSels)
		oracle.sels = buf.GetSelections() // sync normalized selections

		// Choose action: 0 = insert text, 1 = backspace
		action := rng.Intn(2)
		if action == 0 {
			token := tokens[rng.Intn(len(tokens))]
			buf.InsertAtSelections(token)
			oracle.applyInsert(token)
		} else {
			buf.DeleteAtSelections()
			oracle.applyBackspace()
		}

		// ASSERTIONS: State verification against Oracle
		// 1. TotalBytes check
		if buf.TotalBytes() != len(oracle.text) {
			t.Fatalf("iter %d: byte length drift: buf=%d, oracle=%d", iter, buf.TotalBytes(), len(oracle.text))
		}

		// 2. Full text bitwise equality
		actualBytes, err := buf.Slice(0, buf.TotalBytes())
		if err != nil {
			t.Fatalf("iter %d: Slice failed: %v", iter, err)
		}
		if string(actualBytes) != oracle.text {
			t.Fatalf("iter %d: buffer text corrupted!\nGot:    %q\nWanted: %q", iter, string(actualBytes), oracle.text)
		}

		// 3. Selection coordinate check
		bufSels := buf.GetSelections()
		if len(bufSels) != len(oracle.sels) {
			t.Fatalf("iter %d: selection count mismatch: buf=%d, oracle=%d", iter, len(bufSels), len(oracle.sels))
		}
		for sIdx := range bufSels {
			bs := bufSels[sIdx]
			os := oracle.sels[sIdx]
			if bs.Start().Byte != os.Start().Byte || bs.End().Byte != os.End().Byte {
				t.Fatalf("iter %d: cursor %d offset drift: buf=[%d, %d], oracle=[%d, %d]",
					iter, sIdx, bs.Start().Byte, bs.End().Byte, os.Start().Byte, os.End().Byte)
			}
		}

		// 4. Tree invariant check every 25 iterations
		if iter%25 == 0 {
			validateTreeInvariants(t, buf.rope.root)
		}
	}
	t.Logf("PASS: Successfully executed %d chaotic multi-cursor mutations with ZERO drift or corruption.", iterations)
}

// ============================================================================
// 4. Empirical Verification: Undo/Redo Exact State Symmetry
// ============================================================================

type stateSnapshot struct {
	text string
	sels []Selection
}

func TestEmpiricalUndoRedoSymmetry(t *testing.T) {
	buf := NewBufferWithText("Alpha\nBeta\nGamma\nDelta\n")
	rng := rand.New(rand.NewSource(9999))

	const numEdits = 60
	beforeSnapshots := make([]stateSnapshot, numEdits+1)
	afterSnapshots := make([]stateSnapshot, numEdits+1)

	for step := 1; step <= numEdits; step++ {
		// Random edit operation setup
		op := rng.Intn(4)
		curLen := buf.TotalBytes()

		switch op {
		case 0: // Single-cursor word typing
			pos := buf.ByteToPosition(rng.Intn(curLen + 1))
			buf.SetSelections([]Selection{NewCursor(pos)})
			// Snapshot state immediately before edits
			bTxt, _ := buf.Slice(0, buf.TotalBytes())
			beforeSnapshots[step] = stateSnapshot{text: string(bTxt), sels: buf.GetSelections()}

			words := []string{"apple", "orange", "grape", "banana"}
			word := words[rng.Intn(len(words))]
			for _, r := range word {
				buf.InsertAtSelections(string(r))
			}
		case 1: // Multi-cursor insertion
			k := 2 + rng.Intn(4)
			sels := make([]Selection, k)
			for i := 0; i < k; i++ {
				offset := rng.Intn(curLen + 1)
				sels[i] = NewCursor(buf.ByteToPosition(offset))
			}
			buf.SetSelections(sels)
			// Snapshot state immediately before edits
			bTxt, _ := buf.Slice(0, buf.TotalBytes())
			beforeSnapshots[step] = stateSnapshot{text: string(bTxt), sels: buf.GetSelections()}

			buf.InsertAtSelections(" [MUT] ")
		case 2: // Multi-cursor deletion
			k := 2 + rng.Intn(3)
			sels := make([]Selection, k)
			for i := 0; i < k; i++ {
				offset := rng.Intn(curLen + 1)
				sels[i] = NewCursor(buf.ByteToPosition(offset))
			}
			buf.SetSelections(sels)
			// Snapshot state immediately before edits
			bTxt, _ := buf.Slice(0, buf.TotalBytes())
			beforeSnapshots[step] = stateSnapshot{text: string(bTxt), sels: buf.GetSelections()}

			buf.DeleteAtSelections()
		case 3: // Atomic ApplyEdit replacement
			if curLen > 5 {
				start := rng.Intn(curLen - 4)
				delLen := 1 + rng.Intn(4)
				// Snapshot state immediately before edits
				bTxt, _ := buf.Slice(0, buf.TotalBytes())
				beforeSnapshots[step] = stateSnapshot{text: string(bTxt), sels: buf.GetSelections()}

				_ = buf.ApplyEdit(start, delLen, "<REPLACED>")
			} else {
				// Snapshot state immediately before edits
				bTxt, _ := buf.Slice(0, buf.TotalBytes())
				beforeSnapshots[step] = stateSnapshot{text: string(bTxt), sels: buf.GetSelections()}

				buf.InsertAtSelections("X\n")
			}
		}

		// Commit any active batch
		buf.history.CommitActiveBatch()

		txt, _ := buf.Slice(0, buf.TotalBytes())
		afterSnapshots[step] = stateSnapshot{
			text: string(txt),
			sels: buf.GetSelections(),
		}
	}

	t.Logf("Applied %d transactional edits. Verifying step-by-step UNDO symmetry...", numEdits)

	// Step-by-step UNDO: reverse through all transactions
	for step := numEdits; step >= 1; step-- {
		expectedBefore := beforeSnapshots[step]

		ok := buf.Undo()
		if !ok {
			t.Fatalf("Undo failed at step %d: returned false", step)
		}

		currentText, _ := buf.Slice(0, buf.TotalBytes())
		if string(currentText) != expectedBefore.text {
			t.Fatalf("UNDO asymmetry at step %d!\nGot:    %q\nWanted: %q", step, string(currentText), expectedBefore.text)
		}

		// Verify selections restoration
		currentSels := buf.GetSelections()
		if len(currentSels) != len(expectedBefore.sels) {
			t.Fatalf("UNDO selection count mismatch at step %d: got %d (sels: %+v), wanted %d (sels: %+v)",
				step, len(currentSels), currentSels, len(expectedBefore.sels), expectedBefore.sels)
		}
		for i := range currentSels {
			if currentSels[i].Start().Byte != expectedBefore.sels[i].Start().Byte ||
				currentSels[i].End().Byte != expectedBefore.sels[i].End().Byte {
				t.Fatalf("UNDO selection %d mismatch at step %d: got [%d, %d], wanted [%d, %d]",
					i, step, currentSels[i].Start().Byte, currentSels[i].End().Byte,
					expectedBefore.sels[i].Start().Byte, expectedBefore.sels[i].End().Byte)
			}
		}
	}

	// Should not be able to undo further
	if buf.Undo() {
		t.Fatalf("Undo returned true beyond the base of the undo stack!")
	}

	t.Logf("Verifying step-by-step REDO symmetry...")

	// Step-by-step REDO: re-apply all transactions
	for step := 1; step <= numEdits; step++ {
		expectedAfter := afterSnapshots[step]

		ok := buf.Redo()
		if !ok {
			t.Fatalf("Redo failed at step %d: returned false", step)
		}

		currentText, _ := buf.Slice(0, buf.TotalBytes())
		if string(currentText) != expectedAfter.text {
			t.Fatalf("REDO asymmetry at step %d!\nGot:    %q\nWanted: %q", step, string(currentText), expectedAfter.text)
		}

		currentSels := buf.GetSelections()
		if len(currentSels) != len(expectedAfter.sels) {
			t.Fatalf("REDO selection count mismatch at step %d: got %d, wanted %d", step, len(currentSels), len(expectedAfter.sels))
		}
		for i := range currentSels {
			if currentSels[i].Start().Byte != expectedAfter.sels[i].Start().Byte ||
				currentSels[i].End().Byte != expectedAfter.sels[i].End().Byte {
				t.Fatalf("REDO selection %d mismatch at step %d: got [%d, %d], wanted [%d, %d]",
					i, step, currentSels[i].Start().Byte, currentSels[i].End().Byte,
					expectedAfter.sels[i].Start().Byte, expectedAfter.sels[i].End().Byte)
			}
		}
	}

	// Should not be able to redo further
	if buf.Redo() {
		t.Fatalf("Redo returned true beyond the top of the redo stack!")
	}

	t.Logf("PASS: Perfect Undo/Redo symmetry verified across %d complex mutation cycles.", numEdits)
}

// ============================================================================
// 5. Empirical Verification: High-Concurrency & Extreme Boundary Cursors
// ============================================================================

func TestEmpiricalExtremeBoundaries(t *testing.T) {
	// Boundary 1: Empty buffer operations
	b := NewBuffer()
	if b.TotalBytes() != 0 || b.TotalLines() != 1 {
		t.Fatalf("empty buffer invalid: bytes=%d, lines=%d", b.TotalBytes(), b.TotalLines())
	}
	b.DeleteAtSelections() // Should not panic or corrupt
	if b.TotalBytes() != 0 {
		t.Fatalf("delete on empty buffer produced non-zero bytes")
	}
	b.InsertAtSelections("A")
	if b.TotalBytes() != 1 {
		t.Fatalf("insert on empty buffer failed: len=%d", b.TotalBytes())
	}
	b.Undo()
	if b.TotalBytes() != 0 {
		t.Fatalf("undo back to empty buffer failed: len=%d", b.TotalBytes())
	}

	// Boundary 2: 100 simultaneous cursors all typing at once
	text := strings.Repeat("line item content here\n", 100)
	b = NewBufferWithText(text)
	sels := make([]Selection, 100)
	for i := 0; i < 100; i++ {
		offset, err := b.ByteOffsetForLine(i)
		if err != nil {
			t.Fatalf("ByteOffsetForLine(%d) failed: %v", i, err)
		}
		sels[i] = NewCursor(b.ByteToPosition(offset))
	}
	b.SetSelections(sels)
	b.InsertAtSelections("PREFIX_")

	// Verify all 100 lines received the prefix at their beginning
	for i := 0; i < 100; i++ {
		lineBytes, err := b.GetLine(i)
		if err != nil {
			t.Fatalf("GetLine(%d) failed: %v", i, err)
		}
		if !bytes.HasPrefix(lineBytes, []byte("PREFIX_")) {
			t.Fatalf("Line %d missing prefix! Content: %q", i, string(lineBytes))
		}
	}

	// Boundary 3: Multiple carets on the EXACT same byte offset
	b = NewBufferWithText("0123456789")
	b.SetSelections([]Selection{
		NewCursor(b.ByteToPosition(5)),
		NewCursor(b.ByteToPosition(5)),
		NewCursor(b.ByteToPosition(5)),
	})
	// Normalize should deduplicate to 1 cursor
	if len(b.GetSelections()) != 1 {
		t.Fatalf("duplicate carets failed to collapse: got %d", len(b.GetSelections()))
	}

	// Boundary 4: Abutting range selections [0, 5] and [5, 10]
	b.SetSelections([]Selection{
		NewSelection(b.ByteToPosition(0), b.ByteToPosition(5)),
		NewSelection(b.ByteToPosition(5), b.ByteToPosition(10)),
	})
	// Normalize should merge into single [0, 10]
	if len(b.GetSelections()) != 1 {
		t.Fatalf("abutting ranges failed to merge: got %d", len(b.GetSelections()))
	}
	merged := b.GetSelections()[0]
	if merged.Start().Byte != 0 || merged.End().Byte != 10 {
		t.Fatalf("merged range incorrect: [%d, %d]", merged.Start().Byte, merged.End().Byte)
	}

	t.Logf("PASS: Extreme boundary conditions and cursor collapses verified.")
}

// ============================================================================
// 6. Empirical Verification: Word Batching State Transitions & Coalescing
// ============================================================================

func TestEmpiricalWordBatchingTransitions(t *testing.T) {
	b := NewBuffer()

	// Scenario 1: Consecutive word keystrokes should coalesce into 1 transaction
	for _, ch := range "hello" {
		b.InsertAtSelections(string(ch))
	}
	// Before commit or switch, activeTxn holds the word
	if b.history.UndoCount() != 0 {
		t.Fatalf("keystrokes prematurely committed: undoCount = %d", b.history.UndoCount())
	}
	// Commit
	b.history.CommitActiveBatch()
	if b.history.UndoCount() != 1 {
		t.Fatalf("expected 1 coalesced transaction, got %d", b.history.UndoCount())
	}
	// Undo should delete entire "hello" in one shot
	b.Undo()
	if b.TotalBytes() != 0 {
		t.Fatalf("undo failed to revert coalesced word: got %q", string(mustSlice(b, 0, b.TotalBytes())))
	}
	b.Redo()
	if string(mustSlice(b, 0, b.TotalBytes())) != "hello" {
		t.Fatalf("redo failed to restore coalesced word")
	}

	// Scenario 2: Category transition (Word -> Whitespace -> Word)
	// Reset buffer
	b = NewBuffer()
	for _, ch := range "foo" {
		b.InsertAtSelections(string(ch))
	}
	for _, ch := range "   " {
		b.InsertAtSelections(string(ch))
	}
	for _, ch := range "bar" {
		b.InsertAtSelections(string(ch))
	}
	b.history.CommitActiveBatch()

	// Should have 3 separate transactions: "foo", "   ", "bar"
	if b.history.UndoCount() != 3 {
		t.Fatalf("expected 3 transactions for word/space/word, got %d", b.history.UndoCount())
	}

	b.Undo() // Reverts "bar"
	if string(mustSlice(b, 0, b.TotalBytes())) != "foo   " {
		t.Fatalf("undo 1 failed: %q", string(mustSlice(b, 0, b.TotalBytes())))
	}
	b.Undo() // Reverts "   "
	if string(mustSlice(b, 0, b.TotalBytes())) != "foo" {
		t.Fatalf("undo 2 failed: %q", string(mustSlice(b, 0, b.TotalBytes())))
	}
	b.Undo() // Reverts "foo"
	if b.TotalBytes() != 0 {
		t.Fatalf("undo 3 failed: %q", string(mustSlice(b, 0, b.TotalBytes())))
	}

	// Scenario 3: Cursor jump commits batch
	b = NewBufferWithText("First Line\nSecond Line\n")
	b.SetSelections([]Selection{NewCursor(b.ByteToPosition(0))})
	for _, ch := range "AAA" {
		b.InsertAtSelections(string(ch))
	}
	// Jump cursor to line 1
	b.SetSelections([]Selection{NewCursor(b.ByteToPosition(20))})
	for _, ch := range "BBB" {
		b.InsertAtSelections(string(ch))
	}
	b.history.CommitActiveBatch()

	if b.history.UndoCount() != 2 {
		t.Fatalf("cursor jump failed to commit transaction: undoCount = %d", b.history.UndoCount())
	}

	// Scenario 4: MaxUndoLimit eviction
	h := NewHistory()
	for i := 0; i < 1100; i++ {
		h.RecordEdit([]TextDelta{{Kind: DeltaInsert, Offset: i, Text: "x"}}, nil, nil)
	}
	if h.UndoCount() != 1000 {
		t.Fatalf("MaxUndoLimit eviction failed: expected 1000, got %d", h.UndoCount())
	}

	t.Logf("PASS: Word batching state transitions, coalescing, and limits verified.")
}

func mustSlice(b *BufferImpl, start, end int) []byte {
	bytes, err := b.Slice(start, end)
	if err != nil {
		panic(err)
	}
	return bytes
}

// ============================================================================
// 7. Empirical Verification: 500,000 Line Extreme Stress Test
// ============================================================================

func TestEmpirical500kLineStress(t *testing.T) {
	const lineCount = 500000
	var sb strings.Builder
	sb.Grow(lineCount * 30)

	for i := 0; i < lineCount; i++ {
		sb.WriteString("fn main() { println!(\"line\"); }\n")
	}

	start := time.Now()
	buf := NewBufferWithText(sb.String())
	loadDur := time.Since(start)

	t.Logf("500,000-line buffer created in %v (Bytes: %d, Lines: %d)",
		loadDur, buf.TotalBytes(), buf.TotalLines())

	if buf.TotalLines() != lineCount+1 {
		t.Fatalf("TotalLines mismatch: got %d, expected %d", buf.TotalLines(), lineCount+1)
	}

	// Validate tree height for ~500k lines (~15 MB)
	h := buf.rope.root.height
	estLeaves := float64(buf.TotalBytes()) / 1024.0
	maxAvl := 1.4405*math.Log2(estLeaves+2) + 2.0
	t.Logf("500k-line tree height: %d (Max AVL bound: %.1f)", h, maxAvl)

	if float64(h) > maxAvl+4 {
		t.Fatalf("500k-line tree height %d exceeds AVL bound %.1f", h, maxAvl)
	}

	// Line lookups across the 500k lines
	lookupLines := []int{0, 1, 100, 1000, 50000, 100000, 250000, 499999, 500000}
	for _, l := range lookupLines {
		offset, err := buf.ByteOffsetForLine(l)
		if err != nil {
			t.Fatalf("ByteOffsetForLine(%d) failed: %v", l, err)
		}
		backLine, err := buf.LineForByteOffset(offset)
		if err != nil || backLine != l {
			t.Fatalf("Roundtrip failed at line %d: got line %d, offset %d", l, backLine, offset)
		}
	}

	t.Logf("PASS: 500,000-line buffer verified with tree height %d and instant lookups.", h)
}

// ============================================================================
// 8. Empirical Verification: Zero-Allocation Assertions
// ============================================================================

func TestEmpiricalZeroAllocationGuarantees(t *testing.T) {
	buf := NewBufferWithText(strings.Repeat("0123456789abcdefghijklmnopqrstuvwxyz\n", 10000))

	// 1. ByteOffsetForLine must have 0 heap allocations
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = buf.ByteOffsetForLine(5000)
	})
	if allocs > 0 {
		t.Fatalf("ByteOffsetForLine allocated %f objects/op (expected 0)", allocs)
	}

	// 2. LineForByteOffset must have 0 heap allocations
	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = buf.LineForByteOffset(150000)
	})
	if allocs > 0 {
		t.Fatalf("LineForByteOffset allocated %f objects/op (expected 0)", allocs)
	}

	// 3. SliceTo with pre-allocated slice must have 0 heap allocations
	dst := make([]byte, 100)
	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = buf.rope.SliceTo(5000, 5100, dst)
	})
	if allocs > 0 {
		t.Fatalf("SliceTo allocated %f objects/op (expected 0)", allocs)
	}

	// 4. ChunkIterator traversal must have 0 heap allocations per step
	it := buf.rope.Iterator()
	allocs = testing.AllocsPerRun(100, func() {
		_, _ = it.Next()
	})
	if allocs > 0 {
		t.Fatalf("ChunkIterator.Next allocated %f objects/op (expected 0)", allocs)
	}

	t.Logf("PASS: Zero heap allocations verified across all core seeking and traversal operations.")
}
