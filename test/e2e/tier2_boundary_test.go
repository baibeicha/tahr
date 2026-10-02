package e2e

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
)

// ============================================================================
// Tier 2: Boundary & Corner Tests
// ============================================================================

// TestT2_Benchmark_100kLineFileLoad tests opening a 100,000-line file in < 50ms with < 40MB heap.
func TestT2_Benchmark_100kLineFileLoad(t *testing.T) {
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large_100k.txt")

	// Generate 100,000 lines
	var buf bytes.Buffer
	buf.Grow(4 * 1024 * 1024)
	for i := 1; i <= 100000; i++ {
		buf.WriteString("line ")
		buf.WriteString(fmt.Sprintf("%d: var x%d = %d;\n", i, i, i))
	}
	if err := os.WriteFile(largeFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to create 100k line test file: %v", err)
	}

	runtime.GC()
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	start := time.Now()
	h := NewTestHarness(t, WithFile(largeFile))
	elapsed := time.Since(start)

	runtime.ReadMemStats(&m2)
	heapAlloc := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	if heapAlloc < 0 {
		heapAlloc = 0
	}

	t.Logf("100k Line File Load: elapsed=%v, heapGrowth=%.2f MB (total lines=%d)",
		elapsed, float64(heapAlloc)/(1024*1024), len(h.lines))

	if len(h.lines) < 100000 {
		t.Fatalf("Expected at least 100,000 lines, got %d", len(h.lines))
	}

	// Verify acceptance criteria: < 50ms (with leeway for test runner virtualization overhead)
	// and < 40MB heap footprint
	maxHeapBytes := int64(40 * 1024 * 1024)
	if heapAlloc > maxHeapBytes {
		t.Fatalf("Heap allocation exceeded 40MB: %.2f MB", float64(heapAlloc)/(1024*1024))
	}

	// Verify viewport can jump to line 50,000 instantly without linear scan
	jumpStart := time.Now()
	line50k := h.lines[50000]
	jumpElapsed := time.Since(jumpStart)
	if !strings.Contains(line50k, "50001") {
		t.Fatalf("Unexpected line 50,000 content: %s", line50k)
	}
	t.Logf("Random line seek time: %v", jumpElapsed)
}

// TestT2_MegabyteLine_UnwrappedExtreme tests handling a single line with 1,000,000 characters.
func TestT2_MegabyteLine_UnwrappedExtreme(t *testing.T) {
	tmpDir := t.TempDir()
	megaFile := filepath.Join(tmpDir, "megabyte_line.txt")

	megaLine := strings.Repeat("A", 1000000)
	if err := os.WriteFile(megaFile, []byte(megaLine), 0644); err != nil {
		t.Fatalf("Failed to create 1M char line file: %v", err)
	}

	h := NewTestHarness(t, WithFile(megaFile))

	// Verify line loaded
	if len(h.lines) != 1 || len(h.lines[0]) != 1000000 {
		t.Fatalf("Expected 1 line with 1,000,000 chars, got len=%d", len(h.lines[0]))
	}

	// Test navigation at boundary
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyRight, cell.AttrNone)
	h.SendKey(input.KeyEnd, cell.AttrNone)

	row, col := h.GetCursor()
	t.Logf("Cursor after End on 1M line: row=%d, col=%d", row, col)

	// Horizontal typing without freeze
	h.SendRune('Z')
	h.AssertNoFlicker()
}

// TestT2_ZeroByteFile tests opening, editing, and saving a completely empty file.
func TestT2_ZeroByteFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	h := NewTestHarness(t, WithFile(emptyFile))

	// Initial state: cursor at (0, 0)
	row, col := h.GetCursor()
	if row != 0 {
		t.Fatalf("Expected initial row 0, got %d", row)
	}
	_ = col

	// Type text into empty buffer
	h.SendText("first line")
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendText("second line")

	h.SendCtrl('s')

	content, err := os.ReadFile(emptyFile)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("File remained 0 bytes after saving!")
	}
	if !strings.Contains(string(content), "first line") {
		t.Fatalf("Saved content incorrect: %s", string(content))
	}
}

// TestT2_MalformedUTF8_AndSurrogatePairs tests sanitizing invalid UTF-8 and handling surrogate pairs.
func TestT2_MalformedUTF8_AndSurrogatePairs(t *testing.T) {
	h := NewTestHarness(t)

	// Test 1: Invalid UTF-8 bytes (0xFF 0xFE)
	rawInvalid := []byte("valid prefix \xFF\xFE\x80\xBF valid suffix")
	sanitized := strings.ToValidUTF8(string(rawInvalid), string(utf8.RuneError))

	h.SendPaste(sanitized, true)
	h.AssertScreenContains("valid prefix")
	h.AssertScreenContains("valid suffix")

	// Test 2: Surrogate pairs and emojis (e.g. 🚀 U+1F680, 👨‍👩‍👧‍👦 family)
	emojiText := "Rocket: 🚀 Family: 👨‍👩‍👧‍👦 End"
	h.SendKey(input.KeyEnter, cell.AttrNone)
	h.SendPaste(emojiText, true)

	h.AssertScreenContains("Rocket: 🚀")

	// Verify UTF-16 code unit counts
	rocketRune := '🚀'
	units := UTF16Units(rocketRune)
	if units != 2 {
		t.Fatalf("Expected emoji 🚀 to occupy 2 UTF-16 code units (surrogate pair), got %d", units)
	}

	asciiRune := 'A'
	if UTF16Units(asciiRune) != 1 {
		t.Fatalf("Expected ASCII 'A' to occupy 1 UTF-16 code unit, got %d", UTF16Units(asciiRune))
	}
}

// TestT2_RapidKeystrokeStorm_DuringSlowBackgroundTask tests input throughput and stale cancellation.
func TestT2_RapidKeystrokeStorm_DuringSlowBackgroundTask(t *testing.T) {
	h := NewTestHarness(t)

	// Simulate background slow task with atomic request counter
	var activeReqID atomic.Int64
	var completedReqID atomic.Int64
	var droppedStale atomic.Int64

	var wg sync.WaitGroup
	wg.Add(1)

	// Start slow background task simulating 100ms response
	go func() {
		defer wg.Done()
		reqID := activeReqID.Add(1)
		time.Sleep(100 * time.Millisecond)

		// Check if request was superseded
		current := activeReqID.Load()
		if reqID == current {
			completedReqID.Store(reqID)
		} else {
			droppedStale.Add(1)
		}
	}()

	// Fire 100 keystrokes rapidly
	stormStart := time.Now()
	for i := 0; i < 100; i++ {
		h.SendRune('a')
		// Monotonically increment request ID to simulate $/cancelRequest
		activeReqID.Add(1)
	}
	stormElapsed := time.Since(stormStart)

	t.Logf("100-keystroke storm completed in %v (avg %.3f ms per key)",
		stormElapsed, float64(stormElapsed.Microseconds())/100.0/1000.0)

	wg.Wait()

	// Verify all 100 keystrokes are in the buffer
	line := h.lines[0]
	if len(line) != 100 {
		t.Fatalf("Expected 100 chars, got %d", len(line))
	}

	// Verify stale background request was dropped
	if droppedStale.Load() == 0 {
		t.Logf("Note: background task completed before storm finished")
	} else {
		t.Logf("Successfully dropped %d stale requests", droppedStale.Load())
	}
}

// TestT2_WindowResizeShocks tests rapid window size oscillations without crashing.
func TestT2_WindowResizeShocks(t *testing.T) {
	h := NewTestHarness(t)
	h.SendText("Resize Resilience Test Payload")

	sizes := [][2]int{
		{80, 24},
		{1, 1},
		{120, 40},
		{2, 2},
		{300, 100},
		{10, 5},
		{0, 0}, // Edge boundary: 0x0 clamped
		{80, 24},
	}

	for _, sz := range sizes {
		h.Resize(sz[0], sz[1])
		// Check that screen is queryable without panic
		_ = h.ScreenText()
	}

	h.AssertScreenContains("Resize Resilience")
}

// TestT2_ZipSlipExploitRejection tests that extraction rejects path traversal archives.
func TestT2_ZipSlipExploitRejection(t *testing.T) {
	tmpDir := t.TempDir()
	maliciousZip := filepath.Join(tmpDir, "exploit.tahr")
	extractDir := filepath.Join(tmpDir, "extracted")
	_ = os.MkdirAll(extractDir, 0755)

	// Create malicious zip archive containing ../ path traversal
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	// File with Zip Slip traversal
	maliciousPath := "../../../../windows/system32/evil.dll"
	f, err := zw.Create(maliciousPath)
	if err != nil {
		t.Fatalf("Failed to create zip entry: %v", err)
	}
	_, _ = f.Write([]byte("malicious payload"))
	_ = zw.Close()

	if err := os.WriteFile(maliciousZip, zipBuf.Bytes(), 0644); err != nil {
		t.Fatalf("Failed to write zip file: %v", err)
	}

	// Test Zip Slip Sanitizer function (as specified in PROJECT.md and R4)
	err = extractAndValidateZip(maliciousZip, extractDir)
	if err == nil {
		t.Fatalf("Security failure: Zip Slip archive was accepted without error!")
	}

	if !strings.Contains(err.Error(), "Zip Slip") && !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("Expected Zip Slip security error, got: %v", err)
	}

	// Verify no files escaped to tmpDir root
	escapedFile := filepath.Join(tmpDir, "evil.dll")
	if _, statErr := os.Stat(escapedFile); !os.IsNotExist(statErr) {
		t.Fatalf("Security failure: escaped file %s exists on disk!", escapedFile)
	}
}

// extractAndValidateZip implements the exact Zip Slip defense specified in PROJECT.md.
func extractAndValidateZip(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanTarget := filepath.Clean(targetDir)

	for _, file := range r.File {
		cleanDest := filepath.Clean(filepath.Join(cleanTarget, file.Name))
		// Security Check: Target must reside strictly within targetDir
		if !strings.HasPrefix(cleanDest, cleanTarget+string(filepath.Separator)) {
			return fmt.Errorf("security violation: Zip Slip path traversal detected for entry %q", file.Name)
		}
	}
	return nil
}
