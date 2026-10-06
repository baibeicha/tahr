package bookmarks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Bookmark represents a saved line marker across the workspace.
type Bookmark struct {
	ID          string    `json:"id"`
	FilePath    string    `json:"file_path"`
	LineNumber  int       `json:"line_number"`
	LinePreview string    `json:"line_preview"`
	Label       string    `json:"label,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// UnmarshalJSON supports both snake_case and camelCase field names.
func (b *Bookmark) UnmarshalJSON(data []byte) error {
	type Alias Bookmark
	aux := struct {
		AltFilePath    string `json:"filePath"`
		AltLineNumber  int    `json:"lineNumber"`
		AltLinePreview string `json:"linePreview"`
		*Alias
	}{
		Alias: (*Alias)(b),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if b.FilePath == "" && aux.AltFilePath != "" {
		b.FilePath = aux.AltFilePath
	}
	if b.LineNumber == 0 && aux.AltLineNumber != 0 {
		b.LineNumber = aux.AltLineNumber
	}
	if b.LinePreview == "" && aux.AltLinePreview != "" {
		b.LinePreview = aux.AltLinePreview
	}
	return nil
}

// bookmarksContainer wraps bookmarks for structured JSON serialization.
type bookmarksContainer struct {
	Version   int         `json:"version"`
	Bookmarks []*Bookmark `json:"bookmarks"`
}

// Store manages workspace line bookmarks and their persistence in <workspace>/.tahr/bookmarks.json.
type Store struct {
	mu           sync.RWMutex
	workspaceDir string
	filePath     string
	bookmarks    []*Bookmark
	idMap        map[string]*Bookmark
}

// NewStore initializes a new bookmark store for the specified workspace directory.
func NewStore(workspaceDir string) *Store {
	cleanWorkspace := ""
	if workspaceDir != "" {
		cleanWorkspace = filepath.Clean(workspaceDir)
	}
	s := &Store{
		workspaceDir: cleanWorkspace,
		idMap:        make(map[string]*Bookmark),
		bookmarks:    make([]*Bookmark, 0),
	}
	if cleanWorkspace != "" {
		s.filePath = filepath.Join(cleanWorkspace, ".tahr", "bookmarks.json")
	}
	return s
}

// SetWorkspaceDir updates the workspace directory and persistence path.
func (s *Store) SetWorkspaceDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleanDir := ""
	if dir != "" {
		cleanDir = filepath.Clean(dir)
	}
	s.workspaceDir = cleanDir
	if cleanDir != "" {
		s.filePath = filepath.Join(cleanDir, ".tahr", "bookmarks.json")
	} else {
		s.filePath = ""
	}
}

// WorkspaceDir returns the current workspace root directory.
func (s *Store) WorkspaceDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspaceDir
}

// SetFilePath overrides the destination file path for bookmarks persistence.
func (s *Store) SetFilePath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filePath = filepath.Clean(path)
}

// FilePath returns the target bookmarks file path.
func (s *Store) FilePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filePath
}

// normalizePath converts a file path to a canonical, workspace-relative slash-separated path.
func (s *Store) normalizePath(p string) string {
	if p == "" {
		return ""
	}
	cleaned := filepath.Clean(p)
	if s.workspaceDir != "" {
		absWorkspace, errW := filepath.Abs(s.workspaceDir)
		absPath, errP := filepath.Abs(cleaned)
		if errW == nil && errP == nil {
			if rel, err := filepath.Rel(absWorkspace, absPath); err == nil && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
	}
	return filepath.ToSlash(cleaned)
}

// generateBookmarkID creates a unique, collision-resistant identifier.
func generateBookmarkID() string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("bm_%d_%s", time.Now().UnixNano(), hex.EncodeToString(buf))
}

// AddBookmark adds or updates a bookmark for the given file and line number.
func (s *Store) AddBookmark(filePath string, lineNumber int, linePreview, label string) (*Bookmark, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("file path cannot be empty")
	}
	if lineNumber < 1 {
		return nil, errors.New("line number must be greater than or equal to 1")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	normPath := s.normalizePath(filePath)
	cleanPreview := strings.TrimRight(linePreview, "\r\n")

	// Check if bookmark on this line already exists
	for _, b := range s.bookmarks {
		if s.normalizePath(b.FilePath) == normPath && b.LineNumber == lineNumber {
			b.LinePreview = cleanPreview
			if label != "" {
				b.Label = label
			}
			return b, nil
		}
	}

	bm := &Bookmark{
		ID:          generateBookmarkID(),
		FilePath:    normPath,
		LineNumber:  lineNumber,
		LinePreview: cleanPreview,
		Label:       strings.TrimSpace(label),
		CreatedAt:   time.Now().UTC(),
	}

	s.bookmarks = append(s.bookmarks, bm)
	s.idMap[bm.ID] = bm
	return bm, nil
}

// Add inserts a pre-constructed Bookmark into the store.
func (s *Store) Add(bm *Bookmark) error {
	if bm == nil {
		return errors.New("bookmark cannot be nil")
	}
	if strings.TrimSpace(bm.FilePath) == "" {
		return errors.New("file path cannot be empty")
	}
	if bm.LineNumber < 1 {
		return errors.New("line number must be greater than or equal to 1")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	normPath := s.normalizePath(bm.FilePath)
	bm.FilePath = normPath
	bm.LinePreview = strings.TrimRight(bm.LinePreview, "\r\n")
	bm.Label = strings.TrimSpace(bm.Label)

	if bm.ID == "" {
		bm.ID = generateBookmarkID()
	}
	if bm.CreatedAt.IsZero() {
		bm.CreatedAt = time.Now().UTC()
	}

	for i, existing := range s.bookmarks {
		if s.normalizePath(existing.FilePath) == normPath && existing.LineNumber == bm.LineNumber {
			delete(s.idMap, existing.ID)
			s.bookmarks[i] = bm
			s.idMap[bm.ID] = bm
			return nil
		}
	}

	s.bookmarks = append(s.bookmarks, bm)
	s.idMap[bm.ID] = bm
	return nil
}

// ToggleBookmark toggles a bookmark on the given file and line.
// Returns the bookmark, whether it was added (true) or removed (false), and any error.
func (s *Store) ToggleBookmark(filePath string, lineNumber int, linePreview, label string) (*Bookmark, bool, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, false, errors.New("file path cannot be empty")
	}
	if lineNumber < 1 {
		return nil, false, errors.New("line number must be greater than or equal to 1")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	normPath := s.normalizePath(filePath)
	for i, b := range s.bookmarks {
		if s.normalizePath(b.FilePath) == normPath && b.LineNumber == lineNumber {
			delete(s.idMap, b.ID)
			s.bookmarks = append(s.bookmarks[:i], s.bookmarks[i+1:]...)
			return b, false, nil
		}
	}

	bm := &Bookmark{
		ID:          generateBookmarkID(),
		FilePath:    normPath,
		LineNumber:  lineNumber,
		LinePreview: strings.TrimRight(linePreview, "\r\n"),
		Label:       strings.TrimSpace(label),
		CreatedAt:   time.Now().UTC(),
	}

	s.bookmarks = append(s.bookmarks, bm)
	s.idMap[bm.ID] = bm
	return bm, true, nil
}

// RemoveBookmark removes a bookmark by its unique ID. Returns true if removed.
func (s *Store) RemoveBookmark(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, b := range s.bookmarks {
		if b.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false
	}

	delete(s.idMap, id)
	s.bookmarks = append(s.bookmarks[:idx], s.bookmarks[idx+1:]...)
	return true
}

// RemoveBookmarkAt removes a bookmark by file path and line number.
func (s *Store) RemoveBookmarkAt(filePath string, lineNumber int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	normPath := s.normalizePath(filePath)
	idx := -1
	for i, b := range s.bookmarks {
		if s.normalizePath(b.FilePath) == normPath && b.LineNumber == lineNumber {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false
	}

	delete(s.idMap, s.bookmarks[idx].ID)
	s.bookmarks = append(s.bookmarks[:idx], s.bookmarks[idx+1:]...)
	return true
}

// GetBookmark retrieves a bookmark by its unique ID.
func (s *Store) GetBookmark(id string) *Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idMap[id]
}

// GetBookmarkAt retrieves a bookmark at the specified file path and line number.
func (s *Store) GetBookmarkAt(filePath string, lineNumber int) *Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normPath := s.normalizePath(filePath)
	for _, b := range s.bookmarks {
		if s.normalizePath(b.FilePath) == normPath && b.LineNumber == lineNumber {
			return b
		}
	}
	return nil
}

// GetBookmarks returns a sorted copy of all bookmarks in the workspace.
func (s *Store) GetBookmarks() []*Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSortedBookmarksLocked()
}

// GetBookmarksForFile returns all bookmarks for a specific file, sorted by line number.
func (s *Store) GetBookmarksForFile(filePath string) []*Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normPath := s.normalizePath(filePath)
	var fileBookmarks []*Bookmark
	for _, b := range s.bookmarks {
		if s.normalizePath(b.FilePath) == normPath {
			fileBookmarks = append(fileBookmarks, b)
		}
	}

	sort.Slice(fileBookmarks, func(i, j int) bool {
		return fileBookmarks[i].LineNumber < fileBookmarks[j].LineNumber
	})
	return fileBookmarks
}

// Count returns the total number of bookmarks in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bookmarks)
}

// ClearAll removes all bookmarks from memory.
func (s *Store) ClearAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bookmarks = make([]*Bookmark, 0)
	s.idMap = make(map[string]*Bookmark)
	return nil
}

// Save writes all workspace bookmarks to <workspace>/.tahr/bookmarks.json.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.filePath == "" {
		return errors.New("bookmarks persistence file path not configured")
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create bookmarks directory: %w", err)
	}

	container := bookmarksContainer{
		Version:   1,
		Bookmarks: s.bookmarks,
	}

	data, err := json.MarshalIndent(container, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bookmarks: %w", err)
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// Load reads bookmarks from <workspace>/.tahr/bookmarks.json.
// Gracefully handles missing files, bare arrays, and wrapped JSON containers.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filePath == "" {
		return nil
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read bookmarks: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}

	var loaded []*Bookmark
	if strings.HasPrefix(trimmed, "[") {
		// Bare array format
		if err := json.Unmarshal(data, &loaded); err != nil {
			return fmt.Errorf("unmarshal bookmarks array: %w", err)
		}
	} else {
		// Container format
		var container bookmarksContainer
		if err := json.Unmarshal(data, &container); err != nil {
			if err2 := json.Unmarshal(data, &loaded); err2 != nil {
				return fmt.Errorf("unmarshal bookmarks: %w", err)
			}
		} else {
			loaded = container.Bookmarks
		}
	}

	s.bookmarks = make([]*Bookmark, 0, len(loaded))
	s.idMap = make(map[string]*Bookmark, len(loaded))
	for _, b := range loaded {
		if b == nil {
			continue
		}
		if b.ID == "" {
			b.ID = generateBookmarkID()
		}
		b.FilePath = s.normalizePath(b.FilePath)
		s.bookmarks = append(s.bookmarks, b)
		s.idMap[b.ID] = b
	}

	return nil
}

// getSortedBookmarksLocked returns a sorted copy of all bookmarks.
func (s *Store) getSortedBookmarksLocked() []*Bookmark {
	sorted := make([]*Bookmark, len(s.bookmarks))
	copy(sorted, s.bookmarks)
	sort.Slice(sorted, func(i, j int) bool {
		return s.comparePosition(sorted[i].FilePath, sorted[i].LineNumber, sorted[j].FilePath, sorted[j].LineNumber) < 0
	})
	return sorted
}

// comparePosition returns -1 if (f1, l1) < (f2, l2), 1 if > and 0 if equal.
func (s *Store) comparePosition(f1 string, l1 int, f2 string, l2 int) int {
	norm1 := s.normalizePath(f1)
	norm2 := s.normalizePath(f2)
	low1 := strings.ToLower(norm1)
	low2 := strings.ToLower(norm2)

	if low1 < low2 {
		return -1
	}
	if low1 > low2 {
		return 1
	}
	if norm1 < norm2 {
		return -1
	}
	if norm1 > norm2 {
		return 1
	}
	if l1 < l2 {
		return -1
	}
	if l1 > l2 {
		return 1
	}
	return 0
}
