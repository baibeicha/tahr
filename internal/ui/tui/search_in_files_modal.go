package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// SearchMatch represents a single search match within a file.
type SearchMatch struct {
	FilePath string // Absolute or relative file path
	Line     int    // 0-based document line index
	Col      int    // 0-based character column index
	Length   int    // Match length in characters
	LineText string // Full text of the matched line
}

// SearchInFilesModal implements global project search across files (Ctrl+Shift+F).
type SearchInFilesModal struct {
	Open          bool
	Query         string
	Cursor        int
	CaseSensitive bool
	UseRegex      bool
	WholeWord     bool

	Matches       []SearchMatch
	SelectedIndex int
	ScrollOffset  int

	Searching     bool
	TotalSearched int
	statusMsg     string
	mu            sync.Mutex

	OnJump func(filePath string, line, col int)
}

// NewSearchInFilesModal initializes the global search modal.
func NewSearchInFilesModal() *SearchInFilesModal {
	return &SearchInFilesModal{
		Open:          false,
		Matches:       make([]SearchMatch, 0),
		SelectedIndex: 0,
	}
}

// OpenModal activates the search dialog with an optional initial query.
func (s *SearchInFilesModal) OpenModal(initialQuery string, workspaceDir string) {
	s.Open = true
	if initialQuery != "" {
		s.Query = initialQuery
		s.Cursor = len([]rune(s.Query))
	}
	s.SelectedIndex = 0
	s.ScrollOffset = 0
	if s.Query != "" && workspaceDir != "" {
		s.ExecuteSearch(workspaceDir)
	}
}

// Close dismisses the search dialog.
func (s *SearchInFilesModal) Close() {
	s.Open = false
}

// ExecuteSearch performs multi-threaded search across files in workspaceDir.
func (s *SearchInFilesModal) ExecuteSearch(workspaceDir string) {
	s.mu.Lock()
	q := strings.TrimSpace(s.Query)
	if q == "" {
		s.Matches = nil
		s.SelectedIndex = 0
		s.Searching = false
		s.statusMsg = i18n.T("searchfiles.type_to_search")
		s.mu.Unlock()
		return
	}
	s.Searching = true
	s.statusMsg = i18n.T("searchfiles.searching")
	s.Matches = nil
	s.mu.Unlock()

	caseSens := s.CaseSensitive
	useRegex := s.UseRegex
	wholeWord := s.WholeWord

	var compiledRegex *regexp.Regexp
	if useRegex {
		pattern := q
		if !caseSens {
			pattern = "(?i)" + pattern
		}
		if wholeWord {
			pattern = `\b` + pattern + `\b`
		}
		var err error
		compiledRegex, err = regexp.Compile(pattern)
		if err != nil {
			s.mu.Lock()
			s.Searching = false
			s.statusMsg = fmt.Sprintf(i18n.T("searchfiles.invalid_regex"), err)
			s.mu.Unlock()
			return
		}
	} else if wholeWord {
		pattern := regexp.QuoteMeta(q)
		if !caseSens {
			pattern = "(?i)" + pattern
		}
		pattern = `\b` + pattern + `\b`
		var err error
		compiledRegex, err = regexp.Compile(pattern)
		if err != nil {
			compiledRegex = nil
		}
	}

	var results []SearchMatch
	filesSearched := 0
	maxResults := 500

	ignoreDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		".gemini":      true,
		".idea":        true,
		".vscode":      true,
		"bin":          true,
		"obj":          true,
		"target":       true,
		"dist":         true,
		"build":        true,
	}

	_ = filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if ignoreDirs[base] || (strings.HasPrefix(base, ".") && base != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > 5*1024*1024 {
			// Skip special files and files larger than 5MB
			return nil
		}

		filesSearched++
		fileMatches := searchSingleFile(path, workspaceDir, q, caseSens, compiledRegex, maxResults-len(results))
		if len(fileMatches) > 0 {
			results = append(results, fileMatches...)
			if len(results) >= maxResults {
				return io.EOF // Stop walking once limit is reached
			}
		}
		return nil
	})

	s.mu.Lock()
	s.Matches = results
	s.TotalSearched = filesSearched
	s.Searching = false
	if len(results) >= maxResults {
		s.statusMsg = fmt.Sprintf(i18n.T("searchfiles.matches_overflow"), maxResults, filesSearched)
	} else {
		s.statusMsg = fmt.Sprintf(i18n.T("searchfiles.matches_found"), len(results), filesSearched)
	}
	if s.SelectedIndex >= len(s.Matches) {
		s.SelectedIndex = max(0, len(s.Matches)-1)
	}
	s.mu.Unlock()
}

func searchSingleFile(path, root, query string, caseSens bool, re *regexp.Regexp, limit int) []SearchMatch {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Check if file is binary by inspecting first 512 bytes
	header := make([]byte, 512)
	n, _ := f.Read(header)
	if bytes.ContainsRune(header[:n], 0) {
		return nil
	}
	_, _ = f.Seek(0, io.SeekStart)

	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = path
	}

	var matches []SearchMatch
	scanner := bufio.NewScanner(f)
	lineIdx := 0

	lowerQ := strings.ToLower(query)

	for scanner.Scan() {
		lineStr := scanner.Text()
		if re != nil {
			locs := re.FindAllStringIndex(lineStr, -1)
			for _, loc := range locs {
				startRune := len([]rune(lineStr[:loc[0]]))
				matchLen := len([]rune(lineStr[loc[0]:loc[1]]))
				matches = append(matches, SearchMatch{
					FilePath: relPath,
					Line:     lineIdx,
					Col:      startRune,
					Length:   matchLen,
					LineText: strings.TrimSpace(lineStr),
				})
				if len(matches) >= limit {
					return matches
				}
			}
		} else {
			targetLine := lineStr
			if !caseSens {
				targetLine = strings.ToLower(lineStr)
			}
			searchTarget := query
			if !caseSens {
				searchTarget = lowerQ
			}

			startByte := 0
			for {
				pos := strings.Index(targetLine[startByte:], searchTarget)
				if pos == -1 {
					break
				}
				actualByte := startByte + pos
				startRune := len([]rune(lineStr[:actualByte]))
				matchLen := len([]rune(query))

				matches = append(matches, SearchMatch{
					FilePath: relPath,
					Line:     lineIdx,
					Col:      startRune,
					Length:   matchLen,
					LineText: strings.TrimSpace(lineStr),
				})
				if len(matches) >= limit {
					return matches
				}
				startByte = actualByte + len(searchTarget)
				if startByte >= len(targetLine) {
					break
				}
			}
		}
		lineIdx++
	}

	return matches
}

// HandleKey handles keyboard navigation and typing within the search modal.
func (s *SearchInFilesModal) HandleKey(k input.Key, workspaceDir string) bool {
	if !s.Open {
		return false
	}

	if k.Type == input.KeyEsc {
		s.Close()
		return true
	}

	// Alt+C: Toggle case sensitivity
	if k.HasAlt() && (k.Rune == 'c' || k.Rune == 'C') {
		s.CaseSensitive = !s.CaseSensitive
		s.ExecuteSearch(workspaceDir)
		return true
	}

	// Alt+R: Toggle regex
	if k.HasAlt() && (k.Rune == 'r' || k.Rune == 'R') {
		s.UseRegex = !s.UseRegex
		s.ExecuteSearch(workspaceDir)
		return true
	}

	// Alt+W: Toggle whole word
	if k.HasAlt() && (k.Rune == 'w' || k.Rune == 'W') {
		s.WholeWord = !s.WholeWord
		s.ExecuteSearch(workspaceDir)
		return true
	}

	// Navigation in search results
	switch k.Type {
	case input.KeyUp:
		if s.SelectedIndex > 0 {
			s.SelectedIndex--
			if s.SelectedIndex < s.ScrollOffset {
				s.ScrollOffset = s.SelectedIndex
			}
		}
		return true
	case input.KeyDown:
		if s.SelectedIndex < len(s.Matches)-1 {
			s.SelectedIndex++
		}
		return true
	case input.KeyPgUp:
		s.SelectedIndex = max(0, s.SelectedIndex-10)
		s.ScrollOffset = max(0, s.ScrollOffset-10)
		return true
	case input.KeyPgDown:
		s.SelectedIndex = min(len(s.Matches)-1, s.SelectedIndex+10)
		return true
	case input.KeyEnter:
		if len(s.Matches) > 0 && s.SelectedIndex >= 0 && s.SelectedIndex < len(s.Matches) {
			m := s.Matches[s.SelectedIndex]
			s.Close()
			if s.OnJump != nil {
				fullPath := filepath.Join(workspaceDir, m.FilePath)
				s.OnJump(fullPath, m.Line, m.Col)
			}
		}
		return true
	case input.KeyBackspace:
		qRunes := []rune(s.Query)
		if s.Cursor > 0 && len(qRunes) > 0 {
			newRunes := append(qRunes[:s.Cursor-1], qRunes[s.Cursor:]...)
			s.Query = string(newRunes)
			s.Cursor--
			s.ExecuteSearch(workspaceDir)
		}
		return true
	case input.KeyLeft:
		if s.Cursor > 0 {
			s.Cursor--
		}
		return true
	case input.KeyRight:
		qRunes := []rune(s.Query)
		if s.Cursor < len(qRunes) {
			s.Cursor++
		}
		return true
	case input.KeyHome:
		s.Cursor = 0
		return true
	case input.KeyEnd:
		s.Cursor = len([]rune(s.Query))
		return true
	}

	// Text input
	if k.Rune != 0 && !k.HasCtrl() && !k.HasAlt() && unicode.IsPrint(k.Rune) {
		qRunes := []rune(s.Query)
		newRunes := make([]rune, 0, len(qRunes)+1)
		newRunes = append(newRunes, qRunes[:s.Cursor]...)
		newRunes = append(newRunes, k.Rune)
		newRunes = append(newRunes, qRunes[s.Cursor:]...)
		s.Query = string(newRunes)
		s.Cursor++
		s.ExecuteSearch(workspaceDir)
		return true
	}

	return false
}

// Render draws the search modal overlay.
func (s *SearchInFilesModal) Render(buf *buffer.Buffer, w, h int, theme ui.Theme) {
	if !s.Open {
		return
	}

	mw := min(84, w-6)
	mh := min(26, h-4)
	if mw < 40 || mh < 10 {
		return
	}

	bx := (w - mw) / 2
	by := (h - mh) / 2

	bg := cell.Color{Type: cell.ColorRGB, Value: theme.PopupBg}
	borderFg := cell.Color{Type: cell.ColorRGB, Value: theme.BorderColor}
	textFg := cell.Color{Type: cell.ColorRGB, Value: theme.Foreground}
	dimFg := cell.Color{Type: cell.ColorRGB, Value: theme.Comment}
	selBg := cell.Color{Type: cell.ColorRGB, Value: theme.PopupSelBg}
	matchFg := cell.Color{Type: cell.ColorRGB, Value: theme.Function}

	// 1. Draw outer frame
	for y := by; y < by+mh; y++ {
		for x := bx; x < bx+mw; x++ {
			buf.SetRune(x, y, ' ', textFg, bg, cell.AttrNone)
		}
	}

	// Border box
	for x := bx; x < bx+mw; x++ {
		buf.SetRune(x, by, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(x, by+mh-1, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(x, by+3, '─', borderFg, bg, cell.AttrNone) // Separator below input
	}
	for y := by; y < by+mh; y++ {
		buf.SetRune(bx, y, '│', borderFg, bg, cell.AttrNone)
		buf.SetRune(bx+mw-1, y, '│', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(bx, by, '┌', borderFg, bg, cell.AttrNone)
	buf.SetRune(bx+mw-1, by, '┐', borderFg, bg, cell.AttrNone)
	buf.SetRune(bx, by+mh-1, '└', borderFg, bg, cell.AttrNone)
	buf.SetRune(bx+mw-1, by+mh-1, '┘', borderFg, bg, cell.AttrNone)
	buf.SetRune(bx, by+3, '├', borderFg, bg, cell.AttrNone)
	buf.SetRune(bx+mw-1, by+3, '┤', borderFg, bg, cell.AttrNone)

	// Title
	title := fmt.Sprintf(" %s ", i18n.T("searchfiles.title"))
	for i, r := range []rune(title) {
		if bx+2+i < bx+mw-2 {
			buf.SetRune(bx+2+i, by, r, matchFg, bg, cell.AttrBold)
		}
	}

	// Toggles
	toggles := fmt.Sprintf(i18n.T("searchfiles.toggles"), s.CaseSensitive, s.UseRegex, s.WholeWord)
	for i, r := range []rune(toggles) {
		col := bx + mw - 2 - len([]rune(toggles)) + i
		if col > bx+len([]rune(title))+4 && col < bx+mw-1 {
			buf.SetRune(col, by, r, dimFg, bg, cell.AttrNone)
		}
	}

	// Input box
	inputPrompt := i18n.T("searchfiles.prompt")
	for i, r := range []rune(inputPrompt) {
		buf.SetRune(bx+2+i, by+1, r, textFg, bg, cell.AttrBold)
	}

	qRunes := []rune(s.Query)
	inputX := bx + 2 + len([]rune(inputPrompt))
	for i, r := range qRunes {
		if inputX+i < bx+mw-2 {
			buf.SetRune(inputX+i, by+1, r, textFg, bg, cell.AttrNone)
		}
	}

	// Cursor
	curX := inputX + s.Cursor
	if curX < bx+mw-2 {
		buf.SetRune(curX, by+1, '█', cell.Color{Type: cell.ColorRGB, Value: theme.Foreground}, bg, cell.AttrNone)
	}

	// Status line
	status := s.statusMsg
	if status == "" {
		status = i18n.T("searchfiles.type_to_search")
	}
	for i, r := range []rune(status) {
		if bx+2+i < bx+mw-2 {
			buf.SetRune(bx+2+i, by+2, r, dimFg, bg, cell.AttrItalic)
		}
	}

	// Results list
	listTop := by + 4
	listHeight := mh - 5

	if s.SelectedIndex >= s.ScrollOffset+listHeight {
		s.ScrollOffset = s.SelectedIndex - listHeight + 1
	}
	if s.SelectedIndex < s.ScrollOffset {
		s.ScrollOffset = s.SelectedIndex
	}

	for row := 0; row < listHeight; row++ {
		idx := s.ScrollOffset + row
		screenY := listTop + row
		if idx >= len(s.Matches) {
			break
		}

		item := s.Matches[idx]
		isSel := idx == s.SelectedIndex
		rowBg := bg
		if isSel {
			rowBg = selBg
		}

		// Clear row background
		for x := bx + 1; x < bx+mw-1; x++ {
			buf.SetRune(x, screenY, ' ', textFg, rowBg, cell.AttrNone)
		}

		// File and Line tag: "path/to/file.go:42"
		locTag := fmt.Sprintf("%s:%d ", item.FilePath, item.Line+1)
		locRunes := []rune(locTag)
		locFg := cell.Color{Type: cell.ColorRGB, Value: theme.Type}
		if isSel {
			locFg = cell.Color{Type: cell.ColorRGB, Value: theme.Function}
		}

		currCol := bx + 2
		for _, r := range locRunes {
			if currCol < bx+mw-2 {
				buf.SetRune(currCol, screenY, r, locFg, rowBg, cell.AttrBold)
				currCol++
			}
		}

		// Line preview
		lineRunes := []rune(item.LineText)
		for _, r := range lineRunes {
			if currCol < bx+mw-2 {
				buf.SetRune(currCol, screenY, r, textFg, rowBg, cell.AttrNone)
				currCol++
			}
		}
	}
}
