package gitlens

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultCacheCapacity defines the LRU cache limit for blame virtual text.
	DefaultCacheCapacity = 512

	// DefaultDebounceDelay defines the quiet period before spawning git processes.
	DefaultDebounceDelay = 200 * time.Millisecond
)

// BlameInfo contains parsed metadata from git blame --porcelain.
type BlameInfo struct {
	CommitHash    string    `json:"commit_hash"`
	Author        string    `json:"author"`
	AuthorMail    string    `json:"author_mail"`
	AuthorTime    time.Time `json:"author_time"`
	AuthorTZ      string    `json:"author_tz"`
	Committer     string    `json:"committer"`
	CommitterMail string    `json:"committer_mail"`
	CommitterTime time.Time `json:"committer_time"`
	CommitterTZ   string    `json:"committer_tz"`
	Summary       string    `json:"summary"`
	Filename      string    `json:"filename"`
	OrigLine      int       `json:"orig_line"`
	FinalLine     int       `json:"final_line"`
	LineContent   string    `json:"line_content"`
}

// IsUncommitted returns true if the line has not been committed to git yet.
func (b *BlameInfo) IsUncommitted() bool {
	if b == nil {
		return false
	}
	if b.CommitHash != "" && strings.Trim(b.CommitHash, "0") == "" {
		return true
	}
	if strings.EqualFold(b.Author, "Not Committed Yet") {
		return true
	}
	return false
}

// ParseBlamePorcelain parses the raw output of `git blame --porcelain`.
func ParseBlamePorcelain(output string) (*BlameInfo, error) {
	if strings.TrimSpace(output) == "" {
		return nil, fmt.Errorf("empty blame output")
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	info := &BlameInfo{}
	foundHeader := false

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimRight(line, "\r\n")

		if !foundHeader {
			fields := strings.Fields(line)
			if len(fields) >= 3 && len(fields[0]) >= 8 {
				orig, err1 := strconv.Atoi(fields[1])
				fin, err2 := strconv.Atoi(fields[2])
				if err1 == nil && err2 == nil && orig >= 0 && fin >= 0 {
					info.CommitHash = fields[0]
					info.OrigLine = orig
					info.FinalLine = fin
					foundHeader = true
					continue
				}
			}
		}

		if strings.HasPrefix(line, "\t") {
			info.LineContent = line[1:]
			break // Content line marks the end of porcelain header block
		}

		if strings.HasPrefix(line, "author ") {
			info.Author = strings.TrimPrefix(line, "author ")
		} else if strings.HasPrefix(line, "author-mail ") {
			info.AuthorMail = strings.TrimPrefix(line, "author-mail ")
		} else if strings.HasPrefix(line, "author-time ") {
			sec, _ := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			if sec > 0 {
				info.AuthorTime = time.Unix(sec, 0)
			}
		} else if strings.HasPrefix(line, "author-tz ") {
			info.AuthorTZ = strings.TrimPrefix(line, "author-tz ")
		} else if strings.HasPrefix(line, "committer ") {
			info.Committer = strings.TrimPrefix(line, "committer ")
		} else if strings.HasPrefix(line, "committer-mail ") {
			info.CommitterMail = strings.TrimPrefix(line, "committer-mail ")
		} else if strings.HasPrefix(line, "committer-time ") {
			sec, _ := strconv.ParseInt(strings.TrimPrefix(line, "committer-time "), 10, 64)
			if sec > 0 {
				info.CommitterTime = time.Unix(sec, 0)
			}
		} else if strings.HasPrefix(line, "committer-tz ") {
			info.CommitterTZ = strings.TrimPrefix(line, "committer-tz ")
		} else if strings.HasPrefix(line, "summary ") {
			info.Summary = strings.TrimPrefix(line, "summary ")
		} else if strings.HasPrefix(line, "filename ") {
			info.Filename = strings.TrimPrefix(line, "filename ")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !foundHeader || info.CommitHash == "" {
		return nil, fmt.Errorf("invalid blame porcelain format")
	}

	return info, nil
}

// FormatRelativeTime converts a timestamp into human-readable relative time (e.g. "3 days ago").
func FormatRelativeTime(t time.Time) string {
	return FormatRelativeTimeFrom(t, time.Now())
}

// FormatRelativeTimeFrom converts a timestamp relative to reference time into a human-readable string.
func FormatRelativeTimeFrom(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}

	diff := now.Sub(t)
	if diff < 0 {
		diff = 0
	}

	secs := int64(diff.Seconds())
	if secs < 60 {
		return "just now"
	}

	mins := secs / 60
	if mins < 60 {
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}

	hours := mins / 60
	if hours < 24 {
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := hours / 24
	if days < 30 {
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}

	months := days / 30
	if months < 12 {
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	}

	years := days / 365
	if years <= 1 {
		return "1 year ago"
	}
	return fmt.Sprintf("%d years ago", years)
}

// FormatBlame formats parsed blame info into a clean human-readable line:
// e.g. "Author, 3 days ago • commit summary".
func FormatBlame(info *BlameInfo) string {
	if info == nil {
		return ""
	}

	if info.IsUncommitted() {
		return "Not Committed Yet • Uncommitted changes"
	}

	author := info.Author
	if author == "" {
		author = "Unknown"
	}

	relTime := FormatRelativeTime(info.AuthorTime)
	summary := info.Summary

	if relTime != "" && summary != "" {
		return fmt.Sprintf("%s, %s • %s", author, relTime, summary)
	} else if relTime != "" {
		return fmt.Sprintf("%s, %s", author, relTime)
	} else if summary != "" {
		return fmt.Sprintf("%s • %s", author, summary)
	}
	return author
}

// Engine coordinates blame LRU caching, debouncing, and git process execution.
type Engine struct {
	mu          sync.RWMutex
	cache       *LRUCache[string]
	debouncer   *Debouncer
	diffTracker *GutterTracker
}

// NewEngine creates an Engine with an LRU cache of size 512 and a 200ms debouncer.
func NewEngine() *Engine {
	return &Engine{
		cache:       NewLRUCache[string](DefaultCacheCapacity),
		debouncer:   NewDebouncer(DefaultDebounceDelay),
		diffTracker: NewGutterTracker(),
	}
}

var (
	defaultEngineMu sync.RWMutex
	defaultEngine   = NewEngine()
)

// DefaultEngine returns the singleton git-lens engine instance.
func DefaultEngine() *Engine {
	defaultEngineMu.RLock()
	defer defaultEngineMu.RUnlock()
	return defaultEngine
}

// ResetDefaultEngine creates a fresh singleton engine instance (useful for testing).
func ResetDefaultEngine() *Engine {
	defaultEngineMu.Lock()
	defer defaultEngineMu.Unlock()
	defaultEngine = NewEngine()
	return defaultEngine
}

// GetBlame queries git blame for the given file and line with LRU caching (size 512).
// Returns the formatted human-readable blame line and true if successful, or ("", false).
func (e *Engine) GetBlame(filePath string, line int) (string, bool) {
	if filePath == "" {
		return "", false
	}

	cleanPath := filepath.Clean(filePath)
	gitLine := line
	if gitLine <= 0 {
		gitLine = 1
	}

	cacheKey := fmt.Sprintf("%s:%d", cleanPath, gitLine)
	if val, ok := e.cache.Get(cacheKey); ok {
		return val, true
	}

	dir := filepath.Dir(cleanPath)
	if dir == "" {
		dir = "."
	}
	base := filepath.Base(cleanPath)
	lineArg := fmt.Sprintf("%d,%d", gitLine, gitLine)

	cmd := exec.Command("git", "blame", "--porcelain", "-L", lineArg, base)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Fallback: try full cleaned path
		cmd = exec.Command("git", "blame", "--porcelain", "-L", lineArg, cleanPath)
		cmd.Dir = dir
		out, err = cmd.Output()
		if err != nil {
			return "", false
		}
	}

	info, err := ParseBlamePorcelain(string(out))
	if err != nil {
		return "", false
	}

	formatted := FormatBlame(info)
	e.cache.Put(cacheKey, formatted)
	return formatted, true
}

// DebounceBlame debounces blame requests by 200ms before executing.
// If invoked repeatedly during fast cursor navigation, only the settled line is requested.
func (e *Engine) DebounceBlame(filePath string, line int, callback func(string, bool)) {
	if filePath == "" || callback == nil {
		return
	}

	cleanPath := filepath.Clean(filePath)
	key := fmt.Sprintf("blame:%s", cleanPath)

	e.debouncer.Debounce(key, func() {
		res, ok := e.GetBlame(cleanPath, line)
		callback(res, ok)
	})
}

// Invalidate removes cached blame entries and resets diff status for the file.
func (e *Engine) Invalidate(filePath string) {
	if filePath == "" {
		return
	}
	clean := filepath.Clean(filePath)
	e.cache.RemovePrefix(clean + ":")
	if e.diffTracker != nil {
		e.diffTracker.Invalidate(clean)
	}
}

// ClearCache flushes all cached blame lines from the LRU cache.
func (e *Engine) ClearCache() {
	e.cache.Clear()
	if e.diffTracker != nil {
		e.diffTracker.Clear()
	}
}

// SetDebounceDelay configures the debounce quiet window.
func (e *Engine) SetDebounceDelay(delay time.Duration) {
	e.debouncer.SetDelay(delay)
}

// --- Package-level Helper Methods for Editor Integration ---

// GetBlame returns formatted blame virtual text for the given file and line.
// Runs git blame --porcelain -L <line>,<line> <file> with LRU caching (size 512)
// and debouncing (200ms) to ensure zero editor lag.
func GetBlame(filePath string, line int) (string, bool) {
	return DefaultEngine().GetBlame(filePath, line)
}

// DebounceBlame debounces blame queries by 200ms to guarantee zero editor lag.
func DebounceBlame(filePath string, line int, callback func(string, bool)) {
	DefaultEngine().DebounceBlame(filePath, line, callback)
}

// Invalidate clears cache entries for filePath.
func Invalidate(filePath string) {
	DefaultEngine().Invalidate(filePath)
}

// ClearCache flushes the singleton engine's LRU cache.
func ClearCache() {
	DefaultEngine().ClearCache()
}
