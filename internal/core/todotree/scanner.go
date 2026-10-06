package todotree

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// DefaultTags are the standard comment annotations searched across codebases.
var DefaultTags = []string{
	"TODO", "FIXME", "BUG", "HACK", "NOTE", "PERF", "XXX", "OPTIMIZE", "REVIEW",
}

// DefaultIgnoredDirs are common directories excluded from indexing.
var DefaultIgnoredDirs = map[string]bool{
	".git":         true,
	".svn":         true,
	".hg":          true,
	".idea":        true,
	".vscode":      true,
	"node_modules": true,
	"vendor":       true,
	"build":        true,
	"dist":         true,
	"target":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".cache":       true,
	".terraform":   true,
}

// ScanOptions configures the workspace scanner.
type ScanOptions struct {
	Tags           []string          // Custom tags to search for (nil = DefaultTags)
	IgnoredDirs    map[string]bool   // Custom directory exclusions
	MaxFileSize    int64             // Max file size in bytes to scan (default 2MB)
	IncludeGlobs   []string          // Optional glob filters (e.g. "*.go", "*.ts")
	ExcludeGlobs   []string          // Optional exclude glob filters
	CaseSensitive  bool              // Whether tag matching is case-sensitive
	Concurrency    int               // Worker count (default runtime.NumCPU())
}

// Scanner performs concurrent indexing of code annotations.
type Scanner struct {
	options ScanOptions
	regex   *regexp.Regexp
}

// NewScanner constructs a Scanner with the specified options.
func NewScanner(opts ...ScanOptions) *Scanner {
	var opt ScanOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	if len(opt.Tags) == 0 {
		opt.Tags = DefaultTags
	}
	if opt.IgnoredDirs == nil {
		opt.IgnoredDirs = DefaultIgnoredDirs
	}
	if opt.MaxFileSize <= 0 {
		opt.MaxFileSize = 2 * 1024 * 1024 // 2MB
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = runtime.NumCPU()
		if opt.Concurrency < 2 {
			opt.Concurrency = 2
		}
	}

	re := buildTagRegex(opt.Tags, opt.CaseSensitive)

	return &Scanner{
		options: opt,
		regex:   re,
	}
}

// buildTagRegex builds a regex to detect comment annotations.
func buildTagRegex(tags []string, caseSensitive bool) *regexp.Regexp {
	escapedTags := make([]string, len(tags))
	for i, t := range tags {
		escapedTags[i] = regexp.QuoteMeta(t)
	}
	tagGroup := strings.Join(escapedTags, "|")

	prefix := "(?i)"
	if caseSensitive {
		prefix = ""
	}

	// Pattern matches comments like:
	// // TODO: fix this
	// /* FIXME(alice): check overflow */
	// # BUG: division by zero
	// <!-- NOTE: HTML comment -->
	// -- HACK: workaround
	pattern := fmt.Sprintf(`%s(?:(?://|/\*|#|--|<!--|;|REM)\s*|^\s*)(%s)(?:\(([^)]+)\))?\s*[:\-]?\s*(.*)`, prefix, tagGroup)
	return regexp.MustCompile(pattern)
}

// ScanFile scans a single file from disk and returns detected TODOs.
func (s *Scanner) ScanFile(filePath string) ([]TodoItem, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() || fi.Size() > s.options.MaxFileSize {
		return nil, nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check if file is binary
	if isBinaryReader(f) {
		return nil, nil
	}

	// Rewind file
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	return s.ScanReader(f, filePath)
}

// ScanSource scans source code text in-memory and returns detected TODOs.
func (s *Scanner) ScanSource(src []byte, filePath string) []TodoItem {
	reader := bytes.NewReader(src)
	items, _ := s.ScanReader(reader, filePath)
	return items
}

// ScanReader parses lines from any reader and extracts matching items.
func (s *Scanner) ScanReader(r io.Reader, filePath string) ([]TodoItem, error) {
	var items []TodoItem
	scanner := bufio.NewScanner(r)
	// Support long lines (up to 256KB)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 256*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		matches := s.regex.FindStringSubmatch(line)
		if len(matches) > 0 {
			tag := strings.ToUpper(matches[1])
			author := strings.TrimSpace(matches[2])
			msg := strings.TrimSpace(matches[3])

			// Strip trailing comment terminators like "*/", "-->"
			msg = strings.TrimSuffix(msg, "*/")
			msg = strings.TrimSuffix(msg, "-->")
			msg = strings.TrimSpace(msg)

			// Find column position of tag
			col := strings.Index(strings.ToUpper(line), tag)
			if col < 0 {
				col = 0
			}

			items = append(items, TodoItem{
				Tag:      tag,
				Author:   author,
				Message:  msg,
				FilePath: filePath,
				Line:     lineNum,
				Column:   col + 1,
				Priority: TagPriority(tag),
				RawLine:  strings.TrimSpace(line),
			})
		}
	}

	return items, scanner.Err()
}

// isBinaryReader reads first 512 bytes to test for null bytes.
func isBinaryReader(r io.Reader) bool {
	buf := make([]byte, 512)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(buf[:n], []byte{0})
}

// ScanDirectory concurrently traverses root directory and collects all TODO items.
func (s *Scanner) ScanDirectory(ctx context.Context, rootDir string) ([]TodoItem, error) {
	cleanRoot, err := filepath.Abs(rootDir)
	if err != nil {
		cleanRoot = rootDir
	}

	// Channel of files to scan
	fileChan := make(chan string, 256)
	var walkErr error

	// 1. Walk directory and feed files to worker channel
	go func() {
		defer close(fileChan)
		walkErr = filepath.WalkDir(cleanRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if d.IsDir() {
				base := d.Name()
				if s.options.IgnoredDirs[base] || (strings.HasPrefix(base, ".") && base != "." && base != "..") {
					return filepath.SkipDir
				}
				return nil
			}

			// Check include/exclude globs
			rel, _ := filepath.Rel(cleanRoot, path)
			if rel == "" {
				rel = path
			}
			if s.shouldSkipFile(rel, d.Name()) {
				return nil
			}

			fileChan <- path
			return nil
		})
	}()

	// 2. Concurrently process files with worker pool
	var mu sync.Mutex
	var allItems []TodoItem

	numWorkers := s.options.Concurrency
	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		go func() {
			defer wg.Done()
			for path := range fileChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				items, err := s.ScanFile(path)
				if err == nil && len(items) > 0 {
					relPath, relErr := filepath.Rel(cleanRoot, path)
					if relErr == nil {
						for i := range items {
							items[i].FilePath = filepath.ToSlash(relPath)
						}
					}
					mu.Lock()
					allItems = append(allItems, items...)
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	if walkErr != nil && walkErr != context.Canceled {
		return nil, walkErr
	}

	// 3. Sort results deterministically by FilePath then Line
	sort.Slice(allItems, func(i, j int) bool {
		if allItems[i].FilePath != allItems[j].FilePath {
			return allItems[i].FilePath < allItems[j].FilePath
		}
		if allItems[i].Line != allItems[j].Line {
			return allItems[i].Line < allItems[j].Line
		}
		return allItems[i].Column < allItems[j].Column
	})

	return allItems, nil
}

// shouldSkipFile determines if a file should be ignored based on extension or glob.
func (s *Scanner) shouldSkipFile(relPath, baseName string) bool {
	// Standard binary extensions
	ext := strings.ToLower(filepath.Ext(baseName))
	switch ext {
	case ".exe", ".bin", ".dll", ".so", ".dylib", ".o", ".a", ".obj",
		".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg", ".webp",
		".zip", ".tar", ".gz", ".tgz", ".bz2", ".7z",
		".pdf", ".woff", ".woff2", ".ttf", ".eot",
		".mp4", ".mp3", ".wav", ".lock":
		return true
	}

	// Exclude globs
	for _, pattern := range s.options.ExcludeGlobs {
		if matched, _ := filepath.Match(pattern, baseName); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, relPath); matched {
			return true
		}
	}

	// Include globs if specified
	if len(s.options.IncludeGlobs) > 0 {
		matchedAny := false
		for _, pattern := range s.options.IncludeGlobs {
			if m, _ := filepath.Match(pattern, baseName); m {
				matchedAny = true
				break
			}
			if m, _ := filepath.Match(pattern, relPath); m {
				matchedAny = true
				break
			}
		}
		if !matchedAny {
			return true
		}
	}

	return false
}
