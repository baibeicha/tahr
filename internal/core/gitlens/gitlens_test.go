package gitlens

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- 1. Unit Tests for Git Blame Porcelain Parsing ---

func TestParseBlamePorcelain_StandardCommit(t *testing.T) {
	raw := `6795c1b0f2ff61dad80b46ceed39cc5399c00f12 1 1 1
author baibeicha
author-mail <myvvot@gmail.com>
author-time 1790751600
author-tz +0300
committer baibeicha
committer-mail <myvvot@gmail.com>
committer-time 1790751600
committer-tz +0300
summary chore: initial repository configuration and architecture design
boundary
filename go.mod
	module tahr
`

	info, err := ParseBlamePorcelain(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing blame porcelain: %v", err)
	}

	if info.CommitHash != "6795c1b0f2ff61dad80b46ceed39cc5399c00f12" {
		t.Errorf("expected hash %q, got %q", "6795c1b0f2ff61dad80b46ceed39cc5399c00f12", info.CommitHash)
	}
	if info.Author != "baibeicha" {
		t.Errorf("expected author %q, got %q", "baibeicha", info.Author)
	}
	if info.AuthorMail != "<myvvot@gmail.com>" {
		t.Errorf("expected author-mail %q, got %q", "<myvvot@gmail.com>", info.AuthorMail)
	}
	if info.AuthorTime.Unix() != 1790751600 {
		t.Errorf("expected author-time 1790751600, got %d", info.AuthorTime.Unix())
	}
	if info.AuthorTZ != "+0300" {
		t.Errorf("expected author-tz +0300, got %q", info.AuthorTZ)
	}
	if info.Summary != "chore: initial repository configuration and architecture design" {
		t.Errorf("expected summary %q, got %q", "chore: initial repository configuration and architecture design", info.Summary)
	}
	if info.Filename != "go.mod" {
		t.Errorf("expected filename %q, got %q", "go.mod", info.Filename)
	}
	if info.LineContent != "module tahr" {
		t.Errorf("expected line content %q, got %q", "module tahr", info.LineContent)
	}
	if info.IsUncommitted() {
		t.Errorf("expected commit to be committed, not uncommitted")
	}
}

func TestParseBlamePorcelain_Uncommitted(t *testing.T) {
	raw := `0000000000000000000000000000000000000000 81 81 1
author Not Committed Yet
author-mail <not.committed.yet>
author-time 1791274256
author-tz +0300
committer Not Committed Yet
committer-mail <not.committed.yet>
committer-time 1791274256
committer-tz +0300
summary Version of plugins.md from plugins.md
previous c4e715bcf89e7b31a1f885267b3c87bf2d9be5a2 plugins.md
filename plugins.md
	| **db-inspector** | Universal client
`

	info, err := ParseBlamePorcelain(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing uncommitted porcelain: %v", err)
	}

	if !info.IsUncommitted() {
		t.Errorf("expected IsUncommitted() to be true for all zeros hash and Not Committed Yet author")
	}

	formatted := FormatBlame(info)
	if !strings.Contains(formatted, "Not Committed Yet") {
		t.Errorf("expected formatted blame to mention Not Committed Yet, got %q", formatted)
	}
}

func TestParseBlamePorcelain_Invalid(t *testing.T) {
	_, err := ParseBlamePorcelain("")
	if err == nil {
		t.Errorf("expected error on empty porcelain input")
	}

	_, err = ParseBlamePorcelain("garbage content without headers\nanother line\n")
	if err == nil {
		t.Errorf("expected error on invalid porcelain structure")
	}
}

// --- 2. Unit Tests for Date Formatting and Human-Readable Blame ---

func TestFormatRelativeTimeFrom(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		timestamp time.Time
		expected  string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-55 * time.Second), "just now"},
		{now.Add(-61 * time.Second), "1 minute ago"},
		{now.Add(-15 * time.Minute), "15 minutes ago"},
		{now.Add(-60 * time.Minute), "1 hour ago"},
		{now.Add(-5 * time.Hour), "5 hours ago"},
		{now.Add(-24 * time.Hour), "1 day ago"},
		{now.Add(-72 * time.Hour), "3 days ago"}, // Matches requirement: "Author, 3 days ago • commit summary"
		{now.Add(-20 * 24 * time.Hour), "20 days ago"},
		{now.Add(-35 * 24 * time.Hour), "1 month ago"},
		{now.Add(-90 * 24 * time.Hour), "3 months ago"},
		{now.Add(-400 * 24 * time.Hour), "1 year ago"},
		{now.Add(-800 * 24 * time.Hour), "2 years ago"},
	}

	for _, tc := range cases {
		actual := FormatRelativeTimeFrom(tc.timestamp, now)
		if actual != tc.expected {
			t.Errorf("for time offset %v: expected %q, got %q", now.Sub(tc.timestamp), tc.expected, actual)
		}
	}
}

func TestFormatBlame_HumanReadable(t *testing.T) {
	info := &BlameInfo{
		Author:     "Alex",
		AuthorTime: time.Now().Add(-72 * time.Hour),
		Summary:    "feat: high performance syntax highlighting engine",
	}

	line := FormatBlame(info)
	expectedPrefix := "Alex, 3 days ago • feat: high performance syntax highlighting engine"
	if line != expectedPrefix {
		t.Errorf("expected %q, got %q", expectedPrefix, line)
	}

	// Nil safety
	if FormatBlame(nil) != "" {
		t.Errorf("expected empty string for nil info")
	}
}

// --- 3. Unit Tests for Gutter Diff State Mapping ---

func TestParseGutterDiffUnified_StateMapping(t *testing.T) {
	unifiedDiff := `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -10,0 +11,3 @@
+line 11
+line 12
+line 13
@@ -20,2 +23,2 @@
-old line 20
-old line 21
+modified line 23
+modified line 24
@@ -30,2 +33,0 @@
-deleted line 30
-deleted line 31
`

	diff := ParseGutterDiffUnified(unifiedDiff)
	if diff == nil {
		t.Fatal("expected non-nil FileGutterDiff")
	}

	// 1. Added lines: 11, 12, 13 (1-based) / 10, 11, 12 (0-based)
	for _, l := range []int{10, 11, 12} {
		r, status := diff.GetMarker(l)
		if r != MarkerAdded || status != string(StatusAdded) {
			t.Errorf("0-based line %d: expected ('+', %q), got (%c, %q)", l, StatusAdded, r, status)
		}
	}
	for _, l := range []int{11, 12, 13} {
		r, status := diff.GetMarker(l)
		if r != MarkerAdded || status != string(StatusAdded) {
			t.Errorf("1-based line %d: expected ('+', %q), got (%c, %q)", l, StatusAdded, r, status)
		}
	}

	// 2. Modified lines: 23, 24 (1-based) / 22, 23 (0-based)
	for _, l := range []int{22, 23} {
		r, status := diff.GetMarker(l)
		if r != MarkerModified || status != string(StatusModified) {
			t.Errorf("0-based line %d: expected ('~', %q), got (%c, %q)", l, StatusModified, r, status)
		}
	}

	// 3. Deleted lines: line 32 (0-based) / 33 (1-based)
	r, status := diff.GetMarker(32)
	if r != MarkerDeleted || status != string(StatusDeleted) {
		t.Errorf("line 32: expected ('-', %q), got (%c, %q)", StatusDeleted, r, status)
	}

	// 4. Unchanged line: e.g. line 5
	rUnchanged, statusUnchanged := diff.GetMarker(5)
	if rUnchanged != MarkerNone || statusUnchanged != string(StatusNone) {
		t.Errorf("line 5: expected (' ', %q), got (%c, %q)", StatusNone, rUnchanged, statusUnchanged)
	}

	// Verify counts
	if diff.AddedCount != 3 {
		t.Errorf("expected 3 added, got %d", diff.AddedCount)
	}
	if diff.ModifiedCount != 2 {
		t.Errorf("expected 2 modified, got %d", diff.ModifiedCount)
	}
	if diff.DeletedCount != 2 {
		t.Errorf("expected 2 deleted, got %d", diff.DeletedCount)
	}
}

// --- 4. Unit Tests for LRU Cache (Capacity 512 & Eviction) ---

func TestLRUCache_CapacityAndEviction(t *testing.T) {
	cache := NewLRUCache[string](DefaultCacheCapacity)
	if cache.capacity != 512 {
		t.Fatalf("expected capacity 512, got %d", cache.capacity)
	}

	// Fill exactly 512 entries
	for i := 0; i < 512; i++ {
		cache.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("val-%d", i))
	}
	if cache.Len() != 512 {
		t.Errorf("expected cache length 512, got %d", cache.Len())
	}

	// Access key-0 so it moves to MRU
	val, ok := cache.Get("key-0")
	if !ok || val != "val-0" {
		t.Fatalf("failed to retrieve key-0")
	}

	// Insert 513th item -> should evict the least recently used (key-1, since key-0 was refreshed)
	cache.Put("key-512", "val-512")
	if cache.Len() != 512 {
		t.Errorf("cache size should stay at 512 after eviction, got %d", cache.Len())
	}

	// key-0 should still exist
	if _, ok := cache.Get("key-0"); !ok {
		t.Errorf("expected key-0 to remain cached after MRU promotion")
	}

	// key-1 should have been evicted
	if _, ok := cache.Get("key-1"); ok {
		t.Errorf("expected key-1 to be evicted as LRU item")
	}

	// RemovePrefix test
	cache.RemovePrefix("key-5")
	if _, ok := cache.Get("key-512"); ok {
		t.Errorf("expected key-512 to be removed by prefix 'key-5'")
	}

	// Clear test
	cache.Clear()
	if cache.Len() != 0 {
		t.Errorf("expected empty cache after Clear(), got %d", cache.Len())
	}
}

func TestLRUCache_Concurrency(t *testing.T) {
	cache := NewLRUCache[int](512)
	var wg sync.WaitGroup

	// Concurrently read and write across 20 goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("con-key-%d", (id*10+j)%100)
				cache.Put(key, id*100+j)
				_, _ = cache.Get(key)
			}
		}(i)
	}

	wg.Wait()
	if cache.Len() > 512 {
		t.Errorf("cache exceeded capacity 512 under concurrent load: %d", cache.Len())
	}
}

// --- 5. Unit Tests for Debouncer ---

func TestDebouncer_ExecutionAndCancel(t *testing.T) {
	debouncer := NewDebouncer(50 * time.Millisecond)

	var mu sync.Mutex
	executionCount := 0

	// Rapidly fire 5 calls for the same key within 10ms
	for i := 0; i < 5; i++ {
		debouncer.Debounce("cursor_line", func() {
			mu.Lock()
			executionCount++
			mu.Unlock()
		})
		time.Sleep(2 * time.Millisecond)
	}

	// Wait for debounce delay to expire
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := executionCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("expected exactly 1 execution after debounce, got %d", count)
	}

	// Test cancellation
	debouncer.Debounce("cancel_test", func() {
		mu.Lock()
		executionCount++
		mu.Unlock()
	})
	debouncer.Cancel("cancel_test")
	time.Sleep(70 * time.Millisecond)

	mu.Lock()
	afterCancel := executionCount
	mu.Unlock()

	if afterCancel != 1 {
		t.Errorf("cancelled task should not have executed, count: %d", afterCancel)
	}
}

// --- 6. Helper Methods Integration Tests: GetBlame & GetGutterMarker ---

func TestHelperMethods_GetBlame_RealRepo(t *testing.T) {
	target := "go.mod"
	if _, err := os.Stat(target); os.IsNotExist(err) {
		target = filepath.Join("..", "..", "..", "go.mod")
	}
	blameStr, ok := GetBlame(target, 1)
	if !ok {
		t.Fatalf("expected GetBlame for %s line 1 to succeed", target)
	}

	if !strings.Contains(blameStr, "•") {
		t.Errorf("expected blame line to contain bullet separator '•', got %q", blameStr)
	}

	// Cache verification: second query should return from LRU cache
	cachedStr, cachedOk := GetBlame(target, 1)
	if !cachedOk || cachedStr != blameStr {
		t.Errorf("expected cached blame to match original: %q vs %q", blameStr, cachedStr)
	}

	// Non-existent file safety
	nonExistent, nonExistentOk := GetBlame("non_existent_file_xyz123.go", 1)
	if nonExistentOk || nonExistent != "" {
		t.Errorf("expected false and empty string for non-existent file")
	}
}

func TestHelperMethods_GetGutterMarker_GitLifecycle(t *testing.T) {
	// Create an isolated temporary git repository to test full lifecycle
	tmpDir := t.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Tester",
			"GIT_AUTHOR_EMAIL=tester@example.com",
			"GIT_COMMITTER_NAME=Tester",
			"GIT_COMMITTER_EMAIL=tester@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v, out: %s", strings.Join(args, " "), err, string(out))
		}
	}

	runGit("init")
	runGit("config", "user.name", "Tester")
	runGit("config", "user.email", "tester@example.com")

	filePath := filepath.Join(tmpDir, "sample.txt")

	// Commit initial 5 lines
	initialContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}
	runGit("add", "sample.txt")
	runGit("commit", "-m", "initial commit")

	engine := NewEngine()

	// Initial clean state: all lines should have MarkerNone and StatusNone
	for l := 0; l < 5; l++ {
		marker, status := engine.GetGutterMarker(filePath, l)
		if marker != MarkerNone || status != string(StatusNone) {
			t.Errorf("clean state line %d: expected (' ', 'none'), got (%c, %s)", l, marker, status)
		}
	}

	// Now modify line 2, delete line 4, and add 2 lines at end
	modifiedContent := "line 1\nline 2 MODIFIED\nline 3\nline 5\nline 6 ADDED\nline 7 ADDED\n"
	if err := os.WriteFile(filePath, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("failed to write modified file: %v", err)
	}
	engine.Invalidate(filePath)

	// Verify Modified line (line 1 0-based / line 2 1-based)
	markerMod, statusMod := engine.GetGutterMarker(filePath, 1)
	if markerMod != MarkerModified || statusMod != string(StatusModified) {
		t.Errorf("line 1 modified: expected ('~', 'modified'), got (%c, %s)", markerMod, statusMod)
	}

	// Verify Added lines (line 4 and 5 0-based / line 5 and 6 1-based)
	markerAdd, statusAdd := engine.GetGutterMarker(filePath, 4)
	if markerAdd != MarkerAdded || statusAdd != string(StatusAdded) {
		t.Errorf("line 4 added: expected ('+', 'added'), got (%c, %s)", markerAdd, statusAdd)
	}
	markerAdd2, statusAdd2 := engine.GetGutterMarker(filePath, 5)
	if markerAdd2 != MarkerAdded || statusAdd2 != string(StatusAdded) {
		t.Errorf("line 5 added: expected ('+', 'added'), got (%c, %s)", markerAdd2, statusAdd2)
	}

	// Verify package-level helper method with engine singleton
	ResetDefaultEngine()
	m, s := GetGutterMarker(filePath, 1)
	if m != MarkerModified || s != string(StatusModified) {
		t.Errorf("package GetGutterMarker line 1: expected ('~', 'modified'), got (%c, %s)", m, s)
	}
}
