package logviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRingBuffer_WrapAround verifies FIFO circular buffer eviction and indexed slicing.
func TestRingBuffer_WrapAround(t *testing.T) {
	buf := NewRingBuffer(5)
	if buf.Cap() != 5 {
		t.Fatalf("expected capacity 5, got %d", buf.Cap())
	}
	if buf.Len() != 0 {
		t.Fatalf("expected initial len 0, got %d", buf.Len())
	}

	// Append 10 items
	for i := 1; i <= 10; i++ {
		buf.Append(fmt.Sprintf("Line %d", i))
	}

	if buf.Len() != 5 {
		t.Fatalf("expected len 5 after wrap-around, got %d", buf.Len())
	}
	if buf.TotalAppended() != 10 {
		t.Fatalf("expected total appended 10, got %d", buf.TotalAppended())
	}
	if buf.EvictedCount() != 5 {
		t.Fatalf("expected evicted count 5, got %d", buf.EvictedCount())
	}

	// Oldest should be Line 6, newest should be Line 10
	first, ok := buf.Get(0)
	if !ok || first.Raw != "Line 6" {
		t.Errorf("expected Get(0) to be 'Line 6', got '%s'", first.Raw)
	}
	last, ok := buf.Get(4)
	if !ok || last.Raw != "Line 10" {
		t.Errorf("expected Get(4) to be 'Line 10', got '%s'", last.Raw)
	}

	// Slicing [1, 4) -> Lines 7, 8, 9
	slice := buf.Slice(1, 4)
	if len(slice) != 3 {
		t.Fatalf("expected slice length 3, got %d", len(slice))
	}
	if slice[0].Raw != "Line 7" || slice[1].Raw != "Line 8" || slice[2].Raw != "Line 9" {
		t.Errorf("unexpected slice content: %+v", slice)
	}

	// All() -> Lines 6, 7, 8, 9, 10
	all := buf.All()
	if len(all) != 5 {
		t.Fatalf("expected all length 5, got %d", len(all))
	}
	for i, l := range all {
		expected := fmt.Sprintf("Line %d", i+6)
		if l.Raw != expected {
			t.Errorf("all[%d] expected '%s', got '%s'", i, expected, l.Raw)
		}
	}

	// Tail(2) -> Lines 9, 10
	tail := buf.Tail(2)
	if len(tail) != 2 || tail[0].Raw != "Line 9" || tail[1].Raw != "Line 10" {
		t.Errorf("unexpected tail: %+v", tail)
	}

	// Clear
	buf.Clear()
	if buf.Len() != 0 {
		t.Errorf("expected len 0 after clear, got %d", buf.Len())
	}
}

// TestRingBuffer_SubMillisecondAppend verifies that appending 50,000 lines executes sub-millisecond per line.
func TestRingBuffer_SubMillisecondAppend(t *testing.T) {
	buf := NewRingBuffer(50000)
	start := time.Now()
	totalLines := 50000

	for i := 0; i < totalLines; i++ {
		buf.Append(fmt.Sprintf("2026-10-06 12:00:00 [INFO] Benchmark log message sequence %d", i))
	}

	elapsed := time.Since(start)
	avgPerLine := elapsed / time.Duration(totalLines)

	t.Logf("Appended %d lines in %v (avg %v per line)", totalLines, elapsed, avgPerLine)

	// Ensure sub-millisecond per append (1ms = 1,000,000 ns; normal Go mutex ring buffer does ~50-500ns)
	if avgPerLine >= time.Millisecond {
		t.Fatalf("append too slow: average %v per line (must be < 1ms)", avgPerLine)
	}

	if buf.Len() != 50000 {
		t.Errorf("expected 50,000 lines in buffer, got %d", buf.Len())
	}
}

// TestLogLevelDetection validates detection across unstructured text and JSON logs.
func TestLogLevelDetection(t *testing.T) {
	cases := []struct {
		input    string
		expected LogLevel
	}{
		{"2026-10-06 12:00:00 [DEBUG] cache hit for key 42", LevelDebug},
		{"2026-10-06 12:00:00 [INFO] HTTP GET /api/v1/health 200 OK", LevelInfo},
		{"2026-10-06 12:00:00 [WARN] High memory consumption detected", LevelWarn},
		{"2026-10-06 12:00:00 [WARNING] Disk space usage above 90%", LevelWarn},
		{"2026-10-06 12:00:00 [ERROR] Failed to query PostgreSQL database", LevelError},
		{"2026-10-06 12:00:00 [FATAL] Kernel memory segmentation fault", LevelFatal},
		{"ERROR: connection refused on port 5432", LevelError},
		{"WARN: slow query took 4200ms", LevelWarn},
		{"FATAL: panic occurred: nil pointer dereference", LevelFatal},
		{`{"level":"info","msg":"server listening on port 8080"}`, LevelInfo},
		{`{"level":"error","msg":"unexpected EOF while reading body"}`, LevelError},
		{`{"level":"warn","message":"request rate limit reached"}`, LevelWarn},
		{`{"severity":"DEBUG","component":"auth","msg":"token validated"}`, LevelDebug},
		{`{"level":"FATAL","msg":"out of memory"}`, LevelFatal},
		{`level=error msg="failed to parse config"`, LevelError},
		{`level=warn msg="retrying connection in 5s"`, LevelWarn},
		{"Plain unformatted message with no level indication", LevelUnknown},
	}

	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			detected := DetectLevel(c.input)
			if detected != c.expected {
				t.Errorf("expected level %s, got %s for: %s", c.expected, detected, c.input)
			}
		})
	}
}

// TestTimestampDetection validates timestamp extraction for ISO8601, RFC3339, standard date and JSON logs.
func TestTimestampDetection(t *testing.T) {
	cases := []struct {
		input         string
		expectedValid bool
	}{
		{"2026-10-06T11:08:14Z [INFO] app ready", true},
		{"2026-10-06T11:08:14.123456Z [INFO] detailed time", true},
		{"2026-10-06 11:08:14 [INFO] standard space date", true},
		{"2026/10/06 11:08:14 [WARN] slash date format", true},
		{`{"time":"2026-10-06T11:08:14Z","level":"info","msg":"hello"}`, true},
		{`{"ts":1760000000000,"level":"info","msg":"epoch millis"}`, true},
		{"11:08:14.500 [DEBUG] time only", true},
		{"Just a message with no time", false},
	}

	for _, c := range cases {
		ts, str := DetectTimestamp(c.input)
		isValid := !ts.IsZero()
		if isValid != c.expectedValid {
			t.Errorf("input '%s': expected valid=%v, got valid=%v (parsed: %v, str: '%s')",
				c.input, c.expectedValid, isValid, ts, str)
		}
	}
}

// TestSearchFiltering tests keyword filtering, regex matching, and level filtering.
func TestSearchFiltering(t *testing.T) {
	lines := []LogLine{
		ParseLine("2026-10-06 10:00:00 [DEBUG] Connection init for worker_1", 1),
		ParseLine("2026-10-06 10:00:01 [INFO] Worker worker_1 connected", 2),
		ParseLine("2026-10-06 10:00:02 [WARN] Worker worker_1 response delay > 200ms", 3),
		ParseLine("2026-10-06 10:00:03 [ERROR] Worker worker_2 failed to bind port 9090", 4),
		ParseLine("2026-10-06 10:00:04 [FATAL] Worker manager crashed", 5),
	}

	// 1. Level Filter: Error (should match ERROR and FATAL)
	errOpts := NewFilterOptions(LevelError, "", false)
	errMatches := FilterLines(lines, errOpts)
	if len(errMatches) != 2 {
		t.Fatalf("expected 2 error/fatal matches, got %d", len(errMatches))
	}

	// 2. Level Filter: Warn
	warnOpts := NewFilterOptions(LevelWarn, "", false)
	warnMatches := FilterLines(lines, warnOpts)
	if len(warnMatches) != 1 || warnMatches[0].Level != LevelWarn {
		t.Fatalf("expected 1 warn match, got %d", len(warnMatches))
	}

	// 3. Keyword Search: "worker_1"
	kwOpts := NewFilterOptions(LevelUnknown, "worker_1", false)
	kwMatches := FilterLines(lines, kwOpts)
	if len(kwMatches) != 3 {
		t.Fatalf("expected 3 keyword matches for 'worker_1', got %d", len(kwMatches))
	}

	// 4. Regex Search: worker_[0-9] failed
	regexOpts := NewFilterOptions(LevelUnknown, `worker_\d+\s+failed`, true)
	regexMatches := FilterLines(lines, regexOpts)
	if len(regexMatches) != 1 || regexMatches[0].Index != 4 {
		t.Fatalf("expected 1 regex match for line 4, got %d", len(regexMatches))
	}

	// 5. Combined Level + Keyword Search: LevelWarn + "delay"
	combOpts := NewFilterOptions(LevelWarn, "delay", false)
	combMatches := FilterLines(lines, combOpts)
	if len(combMatches) != 1 {
		t.Fatalf("expected 1 combined match, got %d", len(combMatches))
	}

	// 6. Invalid regex fallback shouldn't panic
	invRegexOpts := NewFilterOptions(LevelUnknown, `[unclosed-bracket`, true)
	if invRegexOpts.RegexError() == nil {
		t.Errorf("expected regex compilation error for invalid pattern")
	}
	// Fallback substring matching
	invMatches := FilterLines(lines, invRegexOpts)
	if len(invMatches) != 0 {
		t.Errorf("expected 0 matches for unclosed-bracket fallback, got %d", len(invMatches))
	}
}

// TestStreamingTailer verifies that an asynchronous tailer streams newly appended lines in real-time.
func TestStreamingTailer(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "stream.log")

	// Create empty file
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	buf := NewRingBuffer(100)
	cfg := TailerConfig{
		FilePath:      logPath,
		PollInterval:  15 * time.Millisecond,
		FromBeginning: true,
	}
	tailer := NewTailerWithConfig(cfg, buf)

	received := make(chan LogLine, 20)
	tailer.OnLine = func(l LogLine) {
		received <- l
	}

	if err := tailer.Start(); err != nil {
		t.Fatalf("failed to start tailer: %v", err)
	}
	defer tailer.Stop()

	// Write 3 lines
	fmt.Fprintln(f, "2026-10-06 12:00:00 [INFO] Line 1")
	fmt.Fprintln(f, "2026-10-06 12:00:01 [WARN] Line 2")
	fmt.Fprintln(f, "2026-10-06 12:00:02 [ERROR] Line 3")
	_ = f.Sync()

	// Collect 3 lines
	for i := 1; i <= 3; i++ {
		select {
		case line := <-received:
			expectedSub := fmt.Sprintf("Line %d", i)
			if !containsSub(line.Raw, expectedSub) {
				t.Errorf("expected line %d to contain '%s', got '%s'", i, expectedSub, line.Raw)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("timed out waiting for line %d", i)
		}
	}

	// Test partial line buffering: write half, sleep, then write remainder + newline
	_, _ = f.WriteString("2026-10-06 12:00:03 [DEBUG] Half ")
	_ = f.Sync()
	time.Sleep(30 * time.Millisecond)

	// Should NOT be received yet
	select {
	case line := <-received:
		t.Fatalf("received incomplete line prematurely: %s", line.Raw)
	default:
	}

	// Now finish line
	_, _ = f.WriteString("finished line 4\n")
	_ = f.Sync()

	select {
	case line := <-received:
		if !containsSub(line.Raw, "Half finished line 4") {
			t.Errorf("expected combined line, got: %s", line.Raw)
		}
		if line.Level != LevelDebug {
			t.Errorf("expected LevelDebug, got %s", line.Level)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for finished line 4")
	}

	// Test Pause and Resume
	tailer.Pause()
	if !tailer.IsPaused() {
		t.Errorf("expected tailer to report paused")
	}
	fmt.Fprintln(f, "2026-10-06 12:00:04 [INFO] Line while paused")
	_ = f.Sync()
	time.Sleep(50 * time.Millisecond)

	select {
	case line := <-received:
		t.Fatalf("received line while paused: %s", line.Raw)
	default:
	}

	tailer.Resume()
	if tailer.IsPaused() {
		t.Errorf("expected tailer not paused after resume")
	}

	// Now write line 5
	fmt.Fprintln(f, "2026-10-06 12:00:05 [INFO] Line 5")
	_ = f.Sync()

	select {
	case line := <-received:
		if !containsSub(line.Raw, "Line 5") {
			t.Errorf("expected Line 5 after resume, got %s", line.Raw)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for Line 5 after resume")
	}

	// Verify buffer received lines
	if buf.Len() < 4 {
		t.Errorf("expected at least 4 lines in buffer, got %d", buf.Len())
	}

	// Stop tailer
	if err := tailer.Stop(); err != nil {
		t.Errorf("error stopping tailer: %v", err)
	}
	if tailer.IsRunning() {
		t.Errorf("expected tailer to not be running after Stop")
	}
}

func containsSub(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
