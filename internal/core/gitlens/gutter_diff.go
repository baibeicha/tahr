package gitlens

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LineStatus represents the modification state of a line.
type LineStatus string

const (
	StatusNone     LineStatus = "none"
	StatusAdded    LineStatus = "added"
	StatusModified LineStatus = "modified"
	StatusDeleted  LineStatus = "deleted"
)

const (
	MarkerNone     rune = ' '
	MarkerAdded    rune = '+'
	MarkerModified rune = '~'
	MarkerDeleted  rune = '-'
)

// GutterMarker contains the visual glyph and status classification for a line.
type GutterMarker struct {
	Rune   rune       `json:"rune"`
	Status LineStatus `json:"status"`
}

// FileGutterDiff stores computed line diff markers for both 0-based and 1-based queries.
type FileGutterDiff struct {
	FilePath      string
	Lines0        map[int]GutterMarker // 0-based line -> marker
	Lines1        map[int]GutterMarker // 1-based line -> marker
	AddedCount    int
	ModifiedCount int
	DeletedCount  int
	FetchedAt     time.Time
}

// NewFileGutterDiff creates an initialized FileGutterDiff container.
func NewFileGutterDiff(filePath string) *FileGutterDiff {
	return &FileGutterDiff{
		FilePath: filePath,
		Lines0:   make(map[int]GutterMarker),
		Lines1:   make(map[int]GutterMarker),
	}
}

// GetMarker retrieves the marker for a line, supporting both 0-based and 1-based indices.
func (d *FileGutterDiff) GetMarker(line int) (rune, string) {
	if d == nil || line < 0 {
		return MarkerNone, string(StatusNone)
	}

	if line == 0 {
		if m, ok := d.Lines0[0]; ok {
			return m.Rune, string(m.Status)
		}
		return MarkerNone, string(StatusNone)
	}

	// For line >= 1, check 0-based line first
	if m, ok := d.Lines0[line]; ok {
		return m.Rune, string(m.Status)
	}
	// Check 1-based line index
	if m, ok := d.Lines1[line]; ok {
		return m.Rune, string(m.Status)
	}

	return MarkerNone, string(StatusNone)
}

// ParseGutterDiffUnified parses unified diff output (-U0) against index/HEAD.
func ParseGutterDiffUnified(output string) *FileGutterDiff {
	diff := NewFileGutterDiff("")
	if strings.TrimSpace(output) == "" {
		return diff
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "@@") {
			continue
		}

		_, oldCount, newStart, newCount, ok := parseHunkHeader(line)
		if !ok {
			continue
		}

		if oldCount == 0 && newCount > 0 {
			// Pure addition
			for i := 0; i < newCount; i++ {
				l0 := (newStart - 1) + i
				l1 := newStart + i
				marker := GutterMarker{Rune: MarkerAdded, Status: StatusAdded}
				if l0 >= 0 {
					diff.Lines0[l0] = marker
				}
				if l1 >= 1 {
					diff.Lines1[l1] = marker
				}
				diff.AddedCount++
			}
		} else if oldCount > 0 && newCount == 0 {
			// Pure deletion
			l0 := newStart - 1
			if l0 < 0 {
				l0 = 0
			}
			l1 := newStart
			if l1 < 1 {
				l1 = 1
			}
			marker := GutterMarker{Rune: MarkerDeleted, Status: StatusDeleted}
			diff.Lines0[l0] = marker
			diff.Lines1[l1] = marker
			diff.DeletedCount += oldCount
		} else if oldCount > 0 && newCount > 0 {
			// Modification (or replacement)
			for i := 0; i < newCount; i++ {
				l0 := (newStart - 1) + i
				l1 := newStart + i
				marker := GutterMarker{Rune: MarkerModified, Status: StatusModified}
				if l0 >= 0 {
					diff.Lines0[l0] = marker
				}
				if l1 >= 1 {
					diff.Lines1[l1] = marker
				}
				diff.ModifiedCount++
			}
		}
	}

	return diff
}

func parseHunkHeader(line string) (oldStart, oldCount, newStart, newCount int, ok bool) {
	if !strings.HasPrefix(line, "@@") {
		return 0, 0, 0, 0, false
	}
	parts := strings.Split(line, "@@")
	if len(parts) < 3 {
		return 0, 0, 0, 0, false
	}
	hunkHeader := strings.TrimSpace(parts[1])
	fields := strings.Fields(hunkHeader)
	if len(fields) < 2 {
		return 0, 0, 0, 0, false
	}
	oldStart, oldCount = parseHunkSpec(fields[0])
	newStart, newCount = parseHunkSpec(fields[1])
	return oldStart, oldCount, newStart, newCount, true
}

func parseHunkSpec(spec string) (start int, count int) {
	spec = strings.TrimPrefix(spec, "-")
	spec = strings.TrimPrefix(spec, "+")
	if !strings.Contains(spec, ",") {
		v, _ := strconv.Atoi(spec)
		return v, 1
	}
	parts := strings.Split(spec, ",")
	s, _ := strconv.Atoi(parts[0])
	c, _ := strconv.Atoi(parts[1])
	return s, c
}

// GutterTracker manages and caches git diff state across files.
type GutterTracker struct {
	mu       sync.RWMutex
	cache    map[string]*FileGutterDiff
	throttle time.Duration
}

// NewGutterTracker creates an empty diff cache tracker.
func NewGutterTracker() *GutterTracker {
	return &GutterTracker{
		cache:    make(map[string]*FileGutterDiff),
		throttle: 500 * time.Millisecond,
	}
}

// GetFileDiff computes line diff status against git index/HEAD.
func (t *GutterTracker) GetFileDiff(filePath string) *FileGutterDiff {
	if filePath == "" {
		return NewFileGutterDiff("")
	}

	clean := filepath.Clean(filePath)
	t.mu.RLock()
	cached, ok := t.cache[clean]
	t.mu.RUnlock()

	if ok && time.Since(cached.FetchedAt) < t.throttle {
		return cached
	}

	dir := filepath.Dir(clean)
	if dir == "" {
		dir = "."
	}
	base := filepath.Base(clean)

	// Check if file is untracked
	statusCmd := exec.Command("git", "status", "--porcelain", base)
	statusCmd.Dir = dir
	if statusOut, err := statusCmd.Output(); err == nil && strings.HasPrefix(strings.TrimSpace(string(statusOut)), "??") {
		diff := t.buildUntrackedDiff(clean)
		t.mu.Lock()
		t.cache[clean] = diff
		t.mu.Unlock()
		return diff
	}

	// Run git diff -U0 HEAD to compare working copy against index/HEAD
	diffCmd := exec.Command("git", "diff", "-U0", "HEAD", "--", base)
	diffCmd.Dir = dir
	out, err := diffCmd.Output()
	if err != nil {
		// Fallback without HEAD (e.g. before initial commit)
		diffCmd = exec.Command("git", "diff", "-U0", "--", base)
		diffCmd.Dir = dir
		out, err = diffCmd.Output()
		if err != nil {
			// Fallback with clean path
			diffCmd = exec.Command("git", "diff", "-U0", "HEAD", "--", clean)
			diffCmd.Dir = dir
			out, _ = diffCmd.Output()
		}
	}

	diff := ParseGutterDiffUnified(string(out))
	diff.FilePath = clean
	diff.FetchedAt = time.Now()

	t.mu.Lock()
	t.cache[clean] = diff
	t.mu.Unlock()

	return diff
}

func (t *GutterTracker) buildUntrackedDiff(filePath string) *FileGutterDiff {
	diff := NewFileGutterDiff(filePath)
	diff.FetchedAt = time.Now()

	f, err := os.Open(filePath)
	if err != nil {
		return diff
	}
	defer f.Close()

	lineIdx := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		l0 := lineIdx
		l1 := lineIdx + 1
		marker := GutterMarker{Rune: MarkerAdded, Status: StatusAdded}
		diff.Lines0[l0] = marker
		diff.Lines1[l1] = marker
		diff.AddedCount++
		lineIdx++
	}

	return diff
}

// Invalidate clears cache for a file.
func (t *GutterTracker) Invalidate(filePath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cache, filepath.Clean(filePath))
}

// Clear flushes all cached diffs.
func (t *GutterTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cache = make(map[string]*FileGutterDiff)
}

// GetGutterMarker queries the diff status for a specific line in filePath.
func (e *Engine) GetGutterMarker(filePath string, line int) (rune, string) {
	if filePath == "" || e.diffTracker == nil {
		return MarkerNone, string(StatusNone)
	}
	diff := e.diffTracker.GetFileDiff(filePath)
	return diff.GetMarker(line)
}

// --- Package-level Helper Methods for Editor Integration ---

// GetGutterMarker returns the gutter glyph rune and diff status string for the given file and line.
// Statuses: Added ('+', "added"), Modified ('~', "modified"), Deleted ('-', "deleted"), None (' ', "none").
func GetGutterMarker(filePath string, line int) (rune, string) {
	return DefaultEngine().GetGutterMarker(filePath, line)
}
