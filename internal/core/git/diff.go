package git

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DiffKind represents the modification status of a line in git.
type DiffKind uint8

const (
	DiffNone DiffKind = iota
	DiffAdded
	DiffModified
	DiffDeleted
)

// FileGitDiff contains diff markers and branch metadata.
type FileGitDiff struct {
	Branch        string
	Lines         map[int]DiffKind // 0-based line number -> DiffKind
	AddedCount    int
	ModifiedCount int
	DeletedCount  int
}

// Tracker queries git status and diffs with throttled caching.
type Tracker struct {
	mu              sync.RWMutex
	cache           map[string]*FileGitDiff
	lastCheck       map[string]time.Time
	throttle        time.Duration
	statusCache     map[string]map[string]string
	statusLastCheck map[string]time.Time
	statusThrottle  time.Duration
}

// NewTracker creates a new GitTracker.
func NewTracker() *Tracker {
	return &Tracker{
		cache:           make(map[string]*FileGitDiff),
		lastCheck:       make(map[string]time.Time),
		throttle:        500 * time.Millisecond,
		statusCache:     make(map[string]map[string]string),
		statusLastCheck: make(map[string]time.Time),
		statusThrottle:  1500 * time.Millisecond,
	}
}

// GetBranch returns the current git branch for the specified directory.
func (t *Tracker) GetBranch(dir string) string {
	if dir == "" {
		dir = "."
	}
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetFileDiff returns line-by-line diff markers for the specified file.
func (t *Tracker) GetFileDiff(filePath string) *FileGitDiff {
	if filePath == "" {
		return &FileGitDiff{Lines: make(map[int]DiffKind)}
	}

	clean := filepath.Clean(filePath)
	dir := filepath.Dir(clean)

	t.mu.RLock()
	cached, ok := t.cache[clean]
	lastTime := t.lastCheck[clean]
	t.mu.RUnlock()

	if ok && time.Since(lastTime) < t.throttle {
		return cached
	}

	branch := t.GetBranch(dir)
	if branch == "" {
		res := &FileGitDiff{Lines: make(map[int]DiffKind)}
		t.mu.Lock()
		t.cache[clean] = res
		t.lastCheck[clean] = time.Now()
		t.mu.Unlock()
		return res
	}

	// Run git diff -U0 against HEAD
	cmd := exec.Command("git", "diff", "-U0", "HEAD", "--", clean)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Try without HEAD (for untracked or initial repo state)
		cmd = exec.Command("git", "diff", "-U0", "--", clean)
		cmd.Dir = dir
		out, _ = cmd.Output()
	}

	diff := ParseGitDiffUnified(string(out))
	diff.Branch = branch

	t.mu.Lock()
	t.cache[clean] = diff
	t.lastCheck[clean] = time.Now()
	t.mu.Unlock()

	return diff
}

// Invalidate clears cache for a file, prompting a refresh on next query.
func (t *Tracker) Invalidate(filePath string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cache, filepath.Clean(filePath))
	delete(t.lastCheck, filepath.Clean(filePath))
	t.statusCache = make(map[string]map[string]string)
	t.statusLastCheck = make(map[string]time.Time)
}

// GetFileStatuses returns git porcelain status mapping for the workspace directory.
// Returns a map where keys are both relative paths and absolute paths to status codes ("M", "U", "A", "D").
// Results are cached for 1.5 seconds.
func (t *Tracker) GetFileStatuses(dir string) map[string]string {
	if t == nil {
		return make(map[string]string)
	}

	if dir == "" {
		dir = "."
	}
	cleanDir, err := filepath.Abs(dir)
	if err != nil {
		cleanDir = filepath.Clean(dir)
	}

	t.mu.RLock()
	cached, ok := t.statusCache[cleanDir]
	lastTime := t.statusLastCheck[cleanDir]
	t.mu.RUnlock()

	if ok && time.Since(lastTime) < t.statusThrottle {
		return cached
	}

	cmd := exec.Command("git", "status", "--porcelain", "-uall")
	cmd.Dir = cleanDir
	out, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("git", "status", "--porcelain")
		cmd.Dir = cleanDir
		out, err = cmd.Output()
	}

	res := make(map[string]string)
	if err == nil {
		parsed := ParseGitStatusPorcelain(string(out))
		for relPath, status := range parsed {
			res[relPath] = status
			res[filepath.ToSlash(relPath)] = status

			absPath := filepath.Clean(filepath.Join(cleanDir, relPath))
			res[absPath] = status
			res[filepath.ToSlash(absPath)] = status
			res[strings.ToLower(absPath)] = status
			res[strings.ToLower(filepath.ToSlash(absPath))] = status
		}
	}

	t.mu.Lock()
	t.statusCache[cleanDir] = res
	t.statusLastCheck[cleanDir] = time.Now()
	t.mu.Unlock()

	return res
}

// ParseGitStatusPorcelain parses output of 'git status --porcelain'.
// Returns map of relative file path -> status code ("M", "U", "A", "D").
func ParseGitStatusPorcelain(output string) map[string]string {
	res := make(map[string]string)
	if strings.TrimSpace(output) == "" {
		return res
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			continue
		}

		x := line[0]
		y := line[1]
		rawPath := strings.TrimSpace(line[2:])

		// Handle renames: R  orig -> new
		if strings.Contains(rawPath, " -> ") {
			parts := strings.Split(rawPath, " -> ")
			if len(parts) >= 2 {
				rawPath = strings.TrimSpace(parts[len(parts)-1])
			}
		}

		rawPath = strings.Trim(rawPath, "\"")
		if rawPath == "" {
			continue
		}

		cleanRel := filepath.Clean(filepath.FromSlash(rawPath))

		var status string
		if x == '?' && y == '?' {
			status = "U"
		} else if x == 'A' || y == 'A' {
			status = "A"
		} else if x == 'M' || y == 'M' {
			status = "M"
		} else if x == 'D' || y == 'D' {
			status = "D"
		} else if x == 'R' || y == 'R' {
			status = "M"
		} else {
			status = "M"
		}

		res[cleanRel] = status
		res[filepath.ToSlash(cleanRel)] = status
	}

	return res
}

// ParseGitDiffUnified parses the unified diff output from git diff -U0.
// Expected hunk format:
// @@ -from_start,from_count +to_start,to_count @@
// or
// @@ -from_start +to_start @@
func ParseGitDiffUnified(output string) *FileGitDiff {
	res := &FileGitDiff{
		Lines: make(map[int]DiffKind),
	}

	if strings.TrimSpace(output) == "" {
		return res
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "@@") {
			continue
		}

		// Example: @@ -10,3 +12,5 @@ optional context
		parts := strings.Split(line, "@@")
		if len(parts) < 3 {
			continue
		}
		hunkHeader := strings.TrimSpace(parts[1])
		hunkParts := strings.Fields(hunkHeader)
		if len(hunkParts) < 2 {
			continue
		}

		oldSpec := strings.TrimPrefix(hunkParts[0], "-")
		newSpec := strings.TrimPrefix(hunkParts[1], "+")

		_, oldCount := parseHunkSpec(oldSpec)
		newStart, newCount := parseHunkSpec(newSpec)

		if oldCount == 0 && newCount > 0 {
			// Pure addition
			for i := 0; i < newCount; i++ {
				l := (newStart - 1) + i
				if l >= 0 {
					res.Lines[l] = DiffAdded
					res.AddedCount++
				}
			}
		} else if oldCount > 0 && newCount == 0 {
			// Pure deletion: mark the line before or at oldStart
			delLine := max(0, newStart-1)
			res.Lines[delLine] = DiffDeleted
			res.DeletedCount += oldCount
		} else if oldCount > 0 && newCount > 0 {
			// Modification (or replacement)
			for i := 0; i < newCount; i++ {
				l := (newStart - 1) + i
				if l >= 0 {
					res.Lines[l] = DiffModified
					res.ModifiedCount++
				}
			}
		}
	}

	return res
}

func parseHunkSpec(spec string) (start int, count int) {
	if !strings.Contains(spec, ",") {
		v, _ := strconv.Atoi(spec)
		return v, 1
	}
	parts := strings.Split(spec, ",")
	s, _ := strconv.Atoi(parts[0])
	c, _ := strconv.Atoi(parts[1])
	return s, c
}
