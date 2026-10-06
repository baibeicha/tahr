package regextester

import (
	"strings"
	"testing"
)

func TestCompile_ValidAndInvalid(t *testing.T) {
	// Valid
	eng, err := Compile(`\w+`, Flags{})
	if err != nil {
		t.Fatalf("expected valid regex, got err: %v", err)
	}
	if eng == nil || eng.Regexp() == nil {
		t.Fatal("expected non-nil engine and regexp")
	}

	// Invalid syntax: unclosed parenthesis
	_, err = Compile(`(abc`, Flags{})
	if err == nil {
		t.Fatal("expected error on unclosed parenthesis, got nil")
	}
	if !strings.Contains(err.Error(), "missing closing )") {
		t.Fatalf("expected error description about missing closing ), got: %v", err)
	}

	// Invalid syntax: bad range
	_, err = Compile(`[z-a]`, Flags{})
	if err == nil {
		t.Fatal("expected error on bad character range, got nil")
	}
	if !strings.Contains(err.Error(), "invalid character class range") {
		t.Fatalf("expected error description about invalid class range, got: %v", err)
	}
}

func TestFlags_CaseInsensitive(t *testing.T) {
	flags := Flags{CaseInsensitive: true}
	res := Evaluate(`hello`, flags, "Hello HELLO hello hElLo world")
	if !res.IsValid {
		t.Fatalf("expected valid regex, got err: %s", res.Error)
	}
	if res.TotalCount != 4 {
		t.Fatalf("expected 4 case-insensitive matches, got %d", res.TotalCount)
	}

	// Without case-insensitivity
	resNoCase := Evaluate(`hello`, Flags{}, "Hello HELLO hello hElLo world")
	if resNoCase.TotalCount != 1 {
		t.Fatalf("expected 1 exact match without case-insensitive flag, got %d", resNoCase.TotalCount)
	}
}

func TestFlags_Multiline(t *testing.T) {
	text := "first line\nsecond line\nthird line"

	// Without multiline: ^ matches only start of text
	resNoMulti := Evaluate(`^second`, Flags{}, text)
	if resNoMulti.TotalCount != 0 {
		t.Fatalf("expected 0 matches for ^second without multiline, got %d", resNoMulti.TotalCount)
	}

	// With multiline: ^ matches line beginnings
	resMulti := Evaluate(`^second`, Flags{Multiline: true}, text)
	if resMulti.TotalCount != 1 {
		t.Fatalf("expected 1 match for ^second with multiline, got %d", resMulti.TotalCount)
	}
	if resMulti.Matches[0].Text != "second" {
		t.Fatalf("expected matched text 'second', got %q", resMulti.Matches[0].Text)
	}
}

func TestFlags_DotAll(t *testing.T) {
	text := "start\nmiddle\nend"

	// Without dot-all: . does not match \n
	resNoDotAll := Evaluate(`start.*end`, Flags{}, text)
	if resNoDotAll.TotalCount != 0 {
		t.Fatalf("expected 0 matches across newline without dot-all, got %d", resNoDotAll.TotalCount)
	}

	// With dot-all: . matches \n
	resDotAll := Evaluate(`start.*end`, Flags{DotAll: true}, text)
	if resDotAll.TotalCount != 1 {
		t.Fatalf("expected 1 match across newline with dot-all, got %d", resDotAll.TotalCount)
	}
	if resDotAll.Matches[0].Text != text {
		t.Fatalf("expected full match across newlines, got %q", resDotAll.Matches[0].Text)
	}
}

func TestFlags_AllCombined(t *testing.T) {
	flags := Flags{CaseInsensitive: true, Multiline: true, DotAll: true}
	if flags.String() != "ims" {
		t.Fatalf("expected flag string 'ims', got %q", flags.String())
	}
	if flags.Prefix() != "(?ims)" {
		t.Fatalf("expected prefix '(?ims)', got %q", flags.Prefix())
	}

	parsed := ParseFlags("smi")
	if !parsed.CaseInsensitive || !parsed.Multiline || !parsed.DotAll {
		t.Fatalf("expected all flags parsed, got %+v", parsed)
	}

	text := "ALPHA\nbravo\nCHARLIE"
	res := Evaluate(`^alpha.*charlie$`, flags, text)
	if !res.IsValid || res.TotalCount != 1 {
		t.Fatalf("expected 1 match with all flags combined, got %d (valid=%v, err=%s)", res.TotalCount, res.IsValid, res.Error)
	}
}

func TestNamedCaptureGroups(t *testing.T) {
	pattern := `(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`
	text := "Event 1: 2026-10-06, Event 2: 1999-12-31"

	res := Evaluate(pattern, Flags{}, text)
	if !res.IsValid {
		t.Fatalf("expected valid regex, got err: %s", res.Error)
	}
	if res.TotalCount != 2 {
		t.Fatalf("expected 2 date matches, got %d", res.TotalCount)
	}

	// Check Match 0
	m0 := res.Matches[0]
	if m0.Text != "2026-10-06" {
		t.Fatalf("expected first match '2026-10-06', got %q", m0.Text)
	}
	if len(m0.Groups) != 4 {
		t.Fatalf("expected 4 groups (group 0 + 3 submatches), got %d", len(m0.Groups))
	}
	if len(m0.Submatches) != 3 {
		t.Fatalf("expected 3 submatches, got %d", len(m0.Submatches))
	}

	// Group 0: Full match
	if m0.Groups[0].Index != 0 || m0.Groups[0].Value != "2026-10-06" {
		t.Fatalf("unexpected group 0: %+v", m0.Groups[0])
	}

	// Named groups
	yearGrp := m0.GroupByName("year")
	if yearGrp == nil || yearGrp.Value != "2026" {
		t.Fatalf("expected named group 'year'='2026', got %+v", yearGrp)
	}

	monthGrp := m0.GroupByName("month")
	if monthGrp == nil || monthGrp.Value != "10" {
		t.Fatalf("expected named group 'month'='10', got %+v", monthGrp)
	}

	dayGrp := m0.GroupByName("day")
	if dayGrp == nil || dayGrp.Value != "06" {
		t.Fatalf("expected named group 'day'='06', got %+v", dayGrp)
	}

	// Non-existent group
	if m0.GroupByName("missing") != nil {
		t.Fatal("expected nil for missing group name")
	}

	// Check Match 1
	m1 := res.Matches[1]
	if m1.Text != "1999-12-31" {
		t.Fatalf("expected second match '1999-12-31', got %q", m1.Text)
	}
	if m1.GroupByName("year").Value != "1999" {
		t.Fatalf("expected year '1999', got %s", m1.GroupByName("year").Value)
	}
}

func TestUnnamedSubmatchesAndOptional(t *testing.T) {
	pattern := `(foo)(bar)?(baz)`
	text := "foobaz and foobarbaz"

	res := Evaluate(pattern, Flags{}, text)
	if !res.IsValid || res.TotalCount != 2 {
		t.Fatalf("expected 2 matches, got %d (err: %s)", res.TotalCount, res.Error)
	}

	// In first match "foobaz", group 2 (bar)? was NOT matched
	m0 := res.Matches[0]
	if len(m0.Submatches) != 3 {
		t.Fatalf("expected 3 submatches, got %d", len(m0.Submatches))
	}
	if m0.Submatches[0].Value != "foo" || !m0.Submatches[0].Matched {
		t.Fatalf("expected group 1 'foo' matched, got %+v", m0.Submatches[0])
	}
	if m0.Submatches[1].Matched || m0.Submatches[1].Value != "" {
		t.Fatalf("expected group 2 optional not matched, got %+v", m0.Submatches[1])
	}
	if m0.Submatches[2].Value != "baz" || !m0.Submatches[2].Matched {
		t.Fatalf("expected group 3 'baz' matched, got %+v", m0.Submatches[2])
	}

	// In second match "foobarbaz", group 2 WAS matched
	m1 := res.Matches[1]
	if !m1.Submatches[1].Matched || m1.Submatches[1].Value != "bar" {
		t.Fatalf("expected group 2 'bar' matched, got %+v", m1.Submatches[1])
	}
}

func TestMatchSpans_RuneIndicesUTF8(t *testing.T) {
	// String with multi-byte Russian runes: each Russian character is 2 UTF-8 bytes.
	// "Привет, мир! Привет, мир!"
	// "Привет" is 6 runes (12 bytes).
	// ", " is 2 runes (2 bytes).
	// "мир" is 3 runes (6 bytes).
	// "!" is 1 rune (1 byte).
	// Total line 1: 6+2+3+1 = 12 runes (21 bytes).
	// Space: 1 rune.
	// Total line 2: 12 runes.
	text := "Привет, мир! Привет, мир!"

	res := Evaluate(`мир`, Flags{}, text)
	if !res.IsValid {
		t.Fatalf("expected valid regex, got err: %s", res.Error)
	}
	if res.TotalCount != 2 {
		t.Fatalf("expected 2 matches for 'мир', got %d", res.TotalCount)
	}

	// First "мир":
	// Runes before "мир" are "Привет, " -> 6 runes ("Привет") + 2 runes (", ") = 8 runes.
	// "мир" is 3 runes -> rune span [8, 11].
	span0 := res.MatchSpans[0]
	if span0.Start != 8 || span0.End != 11 {
		t.Fatalf("expected first match span [8, 11], got [%d, %d]", span0.Start, span0.End)
	}

	// Verify slicing runes by span produces exact matched text
	runes := []rune(text)
	extracted0 := string(runes[span0.Start:span0.End])
	if extracted0 != "мир" {
		t.Fatalf("expected extracted runes to be 'мир', got %q", extracted0)
	}

	// Second "мир":
	// Offset: 12 runes + 1 space + 8 runes = 21 runes.
	// Span: [21, 24].
	span1 := res.MatchSpans[1]
	if span1.Start != 21 || span1.End != 24 {
		t.Fatalf("expected second match span [21, 24], got [%d, %d]", span1.Start, span1.End)
	}
	extracted1 := string(runes[span1.Start:span1.End])
	if extracted1 != "мир" {
		t.Fatalf("expected extracted runes to be 'мир', got %q", extracted1)
	}
}

func TestErrorParsing_InvalidSyntax(t *testing.T) {
	tests := []struct {
		pattern string
		errMsg  string
	}{
		{`[a-z`, "missing closing ]"},
		{`*abc`, "missing argument to repetition operator"},
		{`(?P<>abc)`, "invalid named capture"},
		{`a{5,2}`, "invalid repeat count"},
	}

	for _, tt := range tests {
		res := Evaluate(tt.pattern, Flags{}, "test text")
		if res.IsValid {
			t.Errorf("expected pattern %q to be invalid, but marked as valid", tt.pattern)
		}
		if res.Error == "" {
			t.Errorf("expected error message for %q, got empty", tt.pattern)
		}
		if !strings.Contains(res.Error, tt.errMsg) {
			t.Errorf("expected error for %q to contain %q, got: %s", tt.pattern, tt.errMsg, res.Error)
		}
	}
}

func TestEmptyPatternAndEmptyText(t *testing.T) {
	// Empty pattern should return valid result with 0 matches
	resEmptyPattern := Evaluate("", Flags{}, "some text")
	if !resEmptyPattern.IsValid {
		t.Fatalf("expected empty pattern to be valid, got err: %s", resEmptyPattern.Error)
	}
	if resEmptyPattern.TotalCount != 0 || len(resEmptyPattern.Matches) != 0 {
		t.Fatalf("expected 0 matches for empty pattern, got %d", resEmptyPattern.TotalCount)
	}

	// Empty text with valid pattern
	resEmptyText := Evaluate(`\d+`, Flags{}, "")
	if !resEmptyText.IsValid {
		t.Fatalf("expected valid regex, got err: %s", resEmptyText.Error)
	}
	if resEmptyText.TotalCount != 0 {
		t.Fatalf("expected 0 matches in empty text, got %d", resEmptyText.TotalCount)
	}
}

func TestReplaceAll(t *testing.T) {
	eng, err := Compile(`(?P<first>\w+)\s+(?P<last>\w+)`, Flags{})
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	substituted := eng.ReplaceAll("John Doe, Jane Smith", "${last}, ${first}")
	expected := "Doe, John, Smith, Jane"
	if substituted != expected {
		t.Fatalf("expected %q, got %q", expected, substituted)
	}
}
