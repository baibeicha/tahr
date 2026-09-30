package buffer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// TabstopRange records the position and default value of a tabstop.
type TabstopRange struct {
	Index       int    // 0 is the final exit point, 1..N are step tabstops
	StartOffset int    // 0-based character offset relative to snippet start
	EndOffset   int    // 0-based character offset
	DefaultText string
}

// ParsedSnippet contains the evaluated plain text and all extracted tabstops.
type ParsedSnippet struct {
	Raw       string
	PlainText string
	Tabstops  []TabstopRange
}

// ParseSnippet parses TextMate snippet syntax ($0, $1, ${1:default}, etc.).
func ParseSnippet(raw string) *ParsedSnippet {
	var sb strings.Builder
	var tabstops []TabstopRange

	runes := []rune(raw)
	n := len(runes)
	i := 0

	for i < n {
		r := runes[i]
		if r == '\\' && i+1 < n {
			next := runes[i+1]
			if next == '$' || next == '}' || next == '\\' {
				sb.WriteRune(next)
				i += 2
				continue
			}
			sb.WriteRune(r)
			i++
			continue
		}

		if r == '$' && i+1 < n {
			next := runes[i+1]
			if unicode.IsDigit(next) {
				// Simple tabstop $1, $2, $0
				numStr := string(next)
				idx, _ := strconv.Atoi(numStr)
				startOff := len([]rune(sb.String()))
				tabstops = append(tabstops, TabstopRange{
					Index:       idx,
					StartOffset: startOff,
					EndOffset:   startOff,
					DefaultText: "",
				})
				i += 2
				continue
			} else if next == '{' {
				// Complex tabstop ${1:placeholder} or ${1}
				closeBrace := -1
				colonIdx := -1
				depth := 1
				for j := i + 2; j < n; j++ {
					if runes[j] == '\\' && j+1 < n {
						j++
						continue
					}
					if runes[j] == '{' {
						depth++
					} else if runes[j] == '}' {
						depth--
						if depth == 0 {
							closeBrace = j
							break
						}
					} else if runes[j] == ':' && depth == 1 && colonIdx == -1 {
						colonIdx = j
					}
				}

				if closeBrace != -1 {
					var numStr string
					var defaultText string
					if colonIdx != -1 {
						numStr = string(runes[i+2 : colonIdx])
						defaultText = unescapeSnippetText(string(runes[colonIdx+1 : closeBrace]))
					} else {
						numStr = string(runes[i+2 : closeBrace])
					}

					idx, err := strconv.Atoi(strings.TrimSpace(numStr))
					if err == nil {
						startOff := len([]rune(sb.String()))
						sb.WriteString(defaultText)
						endOff := len([]rune(sb.String()))

						tabstops = append(tabstops, TabstopRange{
							Index:       idx,
							StartOffset: startOff,
							EndOffset:   endOff,
							DefaultText: defaultText,
						})

						i = closeBrace + 1
						continue
					}
				}
			}
		}

		sb.WriteRune(r)
		i++
	}

	// Sort tabstops: 1..N ascending, and 0 always at the very end
	sort.SliceStable(tabstops, func(a, b int) bool {
		idxA := tabstops[a].Index
		idxB := tabstops[b].Index
		if idxA == 0 {
			return false
		}
		if idxB == 0 {
			return true
		}
		return idxA < idxB
	})

	return &ParsedSnippet{
		Raw:       raw,
		PlainText: sb.String(),
		Tabstops:  tabstops,
	}
}

// SnippetSession tracks active tabstop stepping within a document.
type SnippetSession struct {
	Active     bool
	BaseLine   int // Starting line where snippet was inserted
	BaseCol    int // Starting col where snippet was inserted
	Tabstops   []TabstopRange
	CurrentIdx int // Pointer into Tabstops
}

// NewSnippetSession creates an active session for a parsed snippet inserted at (baseLine, baseCol).
func NewSnippetSession(snippet *ParsedSnippet, baseLine, baseCol int) *SnippetSession {
	if snippet == nil || len(snippet.Tabstops) == 0 {
		return &SnippetSession{Active: false}
	}
	return &SnippetSession{
		Active:     true,
		BaseLine:   baseLine,
		BaseCol:    baseCol,
		Tabstops:   snippet.Tabstops,
		CurrentIdx: 0,
	}
}

// Current returns the current tabstop range, or false if session is inactive.
func (s *SnippetSession) Current() (TabstopRange, bool) {
	if !s.Active || len(s.Tabstops) == 0 || s.CurrentIdx < 0 || s.CurrentIdx >= len(s.Tabstops) {
		return TabstopRange{}, false
	}
	return s.Tabstops[s.CurrentIdx], true
}

// Next moves to the next tabstop index. Returns false when the final tabstop is reached.
func (s *SnippetSession) Next() (TabstopRange, bool) {
	if !s.Active || len(s.Tabstops) == 0 {
		return TabstopRange{}, false
	}
	s.CurrentIdx++
	if s.CurrentIdx >= len(s.Tabstops) {
		s.Active = false
		return TabstopRange{}, false
	}
	cur := s.Tabstops[s.CurrentIdx]
	if cur.Index == 0 {
		// Reached the terminal $0 tabstop
		s.Active = false
	}
	return cur, true
}

// Prev moves to the previous tabstop index.
func (s *SnippetSession) Prev() (TabstopRange, bool) {
	if !s.Active || len(s.Tabstops) == 0 || s.CurrentIdx <= 0 {
		return TabstopRange{}, false
	}
	s.CurrentIdx--
	return s.Tabstops[s.CurrentIdx], true
}

// Cancel terminates the current snippet session.
func (s *SnippetSession) Cancel() {
	s.Active = false
}

// FormatSnippetPrompt formats a debug string of the session.
func (s *SnippetSession) FormatSnippetPrompt() string {
	if !s.Active {
		return ""
	}
	cur, ok := s.Current()
	if !ok {
		return ""
	}
	return fmt.Sprintf("SNIPPET [$%d: %s] (Tab: next, Shift+Tab: prev, Esc: exit)", cur.Index, cur.DefaultText)
}

func unescapeSnippetText(s string) string {
	var sb strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			if runes[i+1] == '}' || runes[i+1] == '$' || runes[i+1] == '\\' {
				sb.WriteRune(runes[i+1])
				i++
				continue
			}
		}
		sb.WriteRune(runes[i])
	}
	return sb.String()
}

