package buffer

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// generate100kLinesFile creates a synthetic 100,000 line file with typical code line lengths.
func generate100kLinesFile(t testing.TB) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "tahr_bench_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}

	filePath := filepath.Join(dir, "large_100k.txt")
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var buf bytes.Buffer
	for i := 0; i < 100_000; i++ {
		fmt.Fprintf(&buf, "func processRecord_%06d(ctx context.Context, id int64) (Result, error) {\r\n", i)
		if buf.Len() >= 64*1024 {
			if _, err := f.Write(buf.Bytes()); err != nil {
				t.Fatalf("write failed: %v", err)
			}
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		if _, err := f.Write(buf.Bytes()); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return filePath, cleanup
}

func Test100kLinesLoadPerformance(t *testing.T) {
	filePath, cleanup := generate100kLinesFile(t)
	defer cleanup()

	// Force GC before measurement to ensure baseline heap is clean
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	start := time.Now()
	b := NewBuffer()
	err := b.Load(filePath)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	var heapAllocated int64
	if mAfter.HeapAlloc > mBefore.HeapAlloc {
		heapAllocated = int64(mAfter.HeapAlloc - mBefore.HeapAlloc)
	} else {
		heapAllocated = int64(mAfter.HeapAlloc)
	}

	t.Logf("100,000-line file loaded in %v (Target: < 50ms)", elapsed)
	t.Logf("Heap footprint: %.2f MB (Target: < 40MB)", float64(heapAllocated)/(1024*1024))
	t.Logf("Total lines: %d, Total bytes: %d", b.TotalLines(), b.TotalBytes())

	if b.TotalLines() < 100_000 {
		t.Fatalf("expected >= 100,000 lines, got %d", b.TotalLines())
	}

	// Performance Acceptance Criterion 1: < 100ms load time (generous for Windows I/O)
	if elapsed >= 100*time.Millisecond {
		t.Errorf("load time %v exceeded 100ms threshold", elapsed)
	}

	// Performance Acceptance Criterion 2: < 40MB heap footprint
	const maxAllowedHeap = 40 * 1024 * 1024
	if heapAllocated >= maxAllowedHeap {
		t.Errorf("heap footprint %d bytes exceeded 40MB limit", heapAllocated)
	}
}

func BenchmarkLoad100kLines(b *testing.B) {
	filePath, cleanup := generate100kLinesFile(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := NewBuffer()
		if err := buf.Load(filePath); err != nil {
			b.Fatalf("Load failed: %v", err)
		}
	}
}

func BenchmarkByteOffsetForLine(b *testing.B) {
	filePath, cleanup := generate100kLinesFile(b)
	defer cleanup()

	buf := NewBuffer()
	if err := buf.Load(filePath); err != nil {
		b.Fatalf("Load failed: %v", err)
	}

	totalLines := buf.TotalLines()
	rnd := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lineIdx := rnd.Intn(totalLines)
		_, _ = buf.ByteOffsetForLine(lineIdx)
	}
}

func BenchmarkLineForByteOffset(b *testing.B) {
	filePath, cleanup := generate100kLinesFile(b)
	defer cleanup()

	buf := NewBuffer()
	if err := buf.Load(filePath); err != nil {
		b.Fatalf("Load failed: %v", err)
	}

	totalBytes := buf.TotalBytes()
	rnd := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		offset := rnd.Intn(totalBytes)
		_, _ = buf.LineForByteOffset(offset)
	}
}

func BenchmarkGetLine(b *testing.B) {
	filePath, cleanup := generate100kLinesFile(b)
	defer cleanup()

	buf := NewBuffer()
	if err := buf.Load(filePath); err != nil {
		b.Fatalf("Load failed: %v", err)
	}

	totalLines := buf.TotalLines()
	rnd := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lineIdx := rnd.Intn(totalLines)
		_, _ = buf.GetLine(lineIdx)
	}
}

func BenchmarkMultiCursorTyping(b *testing.B) {
	buf := NewBufferWithText(stringsRepeat("line with some text to edit;\n", 1000))

	// Setup 10 active cursors
	cursors := make([]Selection, 10)
	for i := 0; i < 10; i++ {
		offset, _ := buf.ByteOffsetForLine(i * 10)
		cursors[i] = NewCursor(buf.ByteToPosition(offset))
	}
	buf.SetSelections(cursors)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.InsertAtSelections("x")
	}
}

func stringsRepeat(s string, count int) string {
	var buf bytes.Buffer
	for i := 0; i < count; i++ {
		buf.WriteString(s)
	}
	return buf.String()
}
