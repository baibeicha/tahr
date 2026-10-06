package regextester

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Flags defines compilation options for Go RE2 regex.
type Flags struct {
	CaseInsensitive bool `json:"case_insensitive"` // 'i'
	Multiline       bool `json:"multiline"`        // 'm'
	DotAll          bool `json:"dot_all"`           // 's'
}

// String returns the canonical flag characters, e.g. "ims".
func (f Flags) String() string {
	var sb strings.Builder
	if f.CaseInsensitive {
		sb.WriteRune('i')
	}
	if f.Multiline {
		sb.WriteRune('m')
	}
	if f.DotAll {
		sb.WriteRune('s')
	}
	return sb.String()
}

// Prefix returns the RE2 flag prefix to prepend to the pattern, e.g. "(?ims)".
// Returns empty string if no flags are set.
func (f Flags) Prefix() string {
	s := f.String()
	if s == "" {
		return ""
	}
	return "(?" + s + ")"
}

// ParseFlags parses a string containing flag characters ('i', 'm', 's').
func ParseFlags(s string) Flags {
	var f Flags
	for _, r := range s {
		switch r {
		case 'i', 'I':
			f.CaseInsensitive = true
		case 'm', 'M':
			f.Multiline = true
		case 's', 'S':
			f.DotAll = true
		}
	}
	return f
}

// Span represents a start and end index (in runes) in the input text.
type Span struct {
	Start int `json:"start"` // 0-based rune index (inclusive)
	End   int `json:"end"`   // 0-based rune index (exclusive)
}

// Length returns the number of runes in the span.
func (s Span) Length() int {
	if s.End <= s.Start {
		return 0
	}
	return s.End - s.Start
}

// CaptureGroup represents a captured submatch or named group.
type CaptureGroup struct {
	Index   int    `json:"index"`   // Group # (0 = full match, 1..N = submatches)
	Name    string `json:"name"`    // Name if named group (?P<name>...), otherwise empty
	Value   string `json:"value"`   // Matched string content
	Span    Span   `json:"span"`    // Rune span in input text
	Matched bool   `json:"matched"` // True if this group was matched in the pattern
}

// Match represents a single regex match.
type Match struct {
	Index       int                     `json:"index"`       // 0-based match index
	Span        Span                    `json:"span"`        // Full match span in runes
	Text        string                  `json:"text"`        // Full matched text
	Groups      []CaptureGroup          `json:"groups"`      // All capture groups (Group 0 full match + submatches)
	Submatches  []CaptureGroup          `json:"submatches"`  // Submatches only (Group 1..N)
	NamedGroups map[string]CaptureGroup `json:"named_groups"`// Named groups by name
}

// GroupByName returns the first capture group with the specified name, or nil if not found.
func (m *Match) GroupByName(name string) *CaptureGroup {
	if m.NamedGroups != nil {
		if g, ok := m.NamedGroups[name]; ok {
			return &g
		}
	}
	for i := range m.Groups {
		if m.Groups[i].Name == name {
			return &m.Groups[i]
		}
	}
	return nil
}

// GroupByIndex returns the capture group at index i, or nil if out of bounds.
func (m *Match) GroupByIndex(i int) *CaptureGroup {
	if i >= 0 && i < len(m.Groups) {
		return &m.Groups[i]
	}
	return nil
}

// Result holds the complete evaluation result of a regex test.
type Result struct {
	Pattern    string   `json:"pattern"`
	Flags      Flags    `json:"flags"`
	Matches    []Match  `json:"matches"`
	MatchSpans []Span   `json:"match_spans"`
	TotalCount int      `json:"total_count"`
	IsValid    bool     `json:"is_valid"`
	Error      string   `json:"error,omitempty"`
}

// Engine manages regex compilation and evaluation.
type Engine struct {
	Pattern string
	Flags   Flags
	re      *regexp.Regexp
}

// Compile compiles a pattern with the specified flags.
// Returns an Engine instance or an error if the pattern is syntactically invalid.
func Compile(pattern string, flags Flags) (*Engine, error) {
	compiledPattern := flags.Prefix() + pattern
	re, err := regexp.Compile(compiledPattern)
	if err != nil {
		return nil, err
	}
	return &Engine{
		Pattern: pattern,
		Flags:   flags,
		re:      re,
	}, nil
}

// MustCompile compiles a pattern and panics if invalid.
func MustCompile(pattern string, flags Flags) *Engine {
	e, err := Compile(pattern, flags)
	if err != nil {
		panic(err)
	}
	return e
}

// Regexp returns the underlying standard library *regexp.Regexp.
func (e *Engine) Regexp() *regexp.Regexp {
	if e == nil {
		return nil
	}
	return e.re
}

// Evaluate evaluates the compiled engine against the input text.
func (e *Engine) Evaluate(text string) *Result {
	if e == nil || e.re == nil {
		return &Result{
			Pattern:    "",
			Flags:      Flags{},
			Matches:    []Match{},
			MatchSpans: []Span{},
			TotalCount: 0,
			IsValid:    false,
			Error:      "uninitialized regex engine",
		}
	}

	byteToRune := buildByteToRuneMap(text)
	subexpNames := e.re.SubexpNames()
	locs := e.re.FindAllStringSubmatchIndex(text, -1)

	var matches []Match
	var matchSpans []Span

	for mIdx, loc := range locs {
		if len(loc) < 2 {
			continue
		}
		fullStartByte := loc[0]
		fullEndByte := loc[1]
		if fullStartByte < 0 || fullEndByte < 0 || fullStartByte > len(text) || fullEndByte > len(text) {
			continue
		}

		fullSpan := Span{
			Start: byteToRune[fullStartByte],
			End:   byteToRune[fullEndByte],
		}
		fullText := text[fullStartByte:fullEndByte]

		var groups []CaptureGroup
		var submatches []CaptureGroup
		namedMap := make(map[string]CaptureGroup)

		for g := 0; g < len(subexpNames); g++ {
			name := subexpNames[g]
			if 2*g+1 >= len(loc) {
				continue
			}
			gStartByte := loc[2*g]
			gEndByte := loc[2*g+1]

			var cg CaptureGroup
			if gStartByte >= 0 && gEndByte >= 0 && gStartByte <= len(text) && gEndByte <= len(text) {
				cg = CaptureGroup{
					Index:   g,
					Name:    name,
					Value:   text[gStartByte:gEndByte],
					Span:    Span{Start: byteToRune[gStartByte], End: byteToRune[gEndByte]},
					Matched: true,
				}
			} else {
				cg = CaptureGroup{
					Index:   g,
					Name:    name,
					Value:   "",
					Span:    Span{Start: -1, End: -1},
					Matched: false,
				}
			}

			groups = append(groups, cg)
			if g > 0 {
				submatches = append(submatches, cg)
			}
			if name != "" {
				namedMap[name] = cg
			}
		}

		match := Match{
			Index:       mIdx,
			Span:        fullSpan,
			Text:        fullText,
			Groups:      groups,
			Submatches:  submatches,
			NamedGroups: namedMap,
		}

		matches = append(matches, match)
		matchSpans = append(matchSpans, fullSpan)
	}

	return &Result{
		Pattern:    e.Pattern,
		Flags:      e.Flags,
		Matches:    matches,
		MatchSpans: matchSpans,
		TotalCount: len(matches),
		IsValid:    true,
	}
}

// ReplaceAll substitutes all occurrences in text with replacement.
// Expansion variables $1, ${1}, $name, ${name} are evaluated according to Go RE2 rules.
func (e *Engine) ReplaceAll(text, replacement string) string {
	if e == nil || e.re == nil {
		return text
	}
	return e.re.ReplaceAllString(text, replacement)
}

// ReplaceAll substitutes all occurrences in text with replacement using pattern and flags.
func ReplaceAll(pattern string, flags Flags, text, replacement string) (string, error) {
	eng, err := Compile(pattern, flags)
	if err != nil {
		return text, err
	}
	return eng.ReplaceAll(text, replacement), nil
}

// Evaluate compiles pattern with flags and evaluates against text in a single step.
// If pattern is empty, returns an empty valid result with zero matches.
// If pattern is syntactically invalid, returns IsValid: false and the error description.
func Evaluate(pattern string, flags Flags, text string) *Result {
	if pattern == "" {
		return &Result{
			Pattern:    pattern,
			Flags:      flags,
			Matches:    []Match{},
			MatchSpans: []Span{},
			TotalCount: 0,
			IsValid:    true,
		}
	}

	eng, err := Compile(pattern, flags)
	if err != nil {
		return &Result{
			Pattern:    pattern,
			Flags:      flags,
			Matches:    []Match{},
			MatchSpans: []Span{},
			TotalCount: 0,
			IsValid:    false,
			Error:      err.Error(),
		}
	}

	return eng.Evaluate(text)
}

// buildByteToRuneMap creates an O(1) byte-offset to rune-index lookup table for UTF-8 strings.
func buildByteToRuneMap(s string) []int {
	byteToRune := make([]int, len(s)+1)
	rIdx := 0
	for bIdx, r := range s {
		rLen := utf8.RuneLen(r)
		for i := 0; i < rLen; i++ {
			byteToRune[bIdx+i] = rIdx
		}
		rIdx++
	}
	byteToRune[len(s)] = rIdx
	return byteToRune
}
