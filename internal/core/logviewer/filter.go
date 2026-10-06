package logviewer

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LogLevel indicates the severity of a log entry.
type LogLevel int

const (
	LevelUnknown LogLevel = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the canonical uppercase representation of the log level.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Badge returns a standardized 5-character badge for developer TUI rendering.
func (l LogLevel) Badge() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO "
	case LevelWarn:
		return "WARN "
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "LOG  "
	}
}

// ParseLogLevel converts a string to LogLevel case-insensitively.
func ParseLogLevel(s string) LogLevel {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case "DEBUG", "DBG", "TRACE", "VERBOSE":
		return LevelDebug
	case "INFO", "INF", "NOTICE", "INFORMATION":
		return LevelInfo
	case "WARN", "WARNING", "WRN":
		return LevelWarn
	case "ERROR", "ERR", "CRIT", "CRITICAL":
		return LevelError
	case "FATAL", "PANIC", "EMERGENCY", "ALERT":
		return LevelFatal
	default:
		return LevelUnknown
	}
}

// Precompiled regular expressions for fast detection.
var (
	jsonLevelRegex = regexp.MustCompile(`(?i)"(?:level|lvl|severity|log_level)"\s*:\s*"([a-zA-Z]+)"`)
	jsonTimeRegex  = regexp.MustCompile(`(?i)"(?:time|timestamp|ts|@timestamp)"\s*:\s*"?([^",}]+)"?`)

	bracketLevelRegex = regexp.MustCompile(`(?i)\[\s*(DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|DBG|INF|WRN|ERR)\s*\]`)
	kvLevelRegex      = regexp.MustCompile(`(?i)(?:level|lvl|severity)=(?:"|')?(DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|DBG|INF|WRN|ERR)(?:"|')?`)
	colonLevelRegex   = regexp.MustCompile(`(?i)\b(FATAL|ERROR|WARN(?:ING)?|INFO|DEBUG):\s+`)
	wordLevelRegex    = regexp.MustCompile(`(?i)\b(FATAL|ERROR|WARNING|WARN|INFO|DEBUG)\b`)

	// Timestamp formats at start of log line
	rfc3339PrefixRegex = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)`)
	dateSpaceRegex     = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
	dateSlashRegex     = regexp.MustCompile(`^\s*(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
	syslogPrefixRegex  = regexp.MustCompile(`^\s*([A-Za-z]{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})`)
	timeOnlyRegex      = regexp.MustCompile(`^\s*(\d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
)

var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05.999",
	"2006/01/02 15:04:05",
	"Jan _2 15:04:05",
	"15:04:05.999",
	"15:04:05",
}

// DetectLevel detects the log level from a raw log string or JSON object.
func DetectLevel(line string) LogLevel {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return LevelUnknown
	}

	// 1. If JSON structured log, try regex first for speed
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if m := jsonLevelRegex.FindStringSubmatch(trimmed); len(m) > 1 {
			lvl := ParseLogLevel(m[1])
			if lvl != LevelUnknown {
				return lvl
			}
		}
	}

	// 2. Bracketed level: [ERROR], [WARN], etc.
	if m := bracketLevelRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		lvl := ParseLogLevel(m[1])
		if lvl != LevelUnknown {
			return lvl
		}
	}

	// 3. Key-value level: level=info, severity=error
	if m := kvLevelRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		lvl := ParseLogLevel(m[1])
		if lvl != LevelUnknown {
			return lvl
		}
	}

	// 4. Colon level: ERROR: ...
	if m := colonLevelRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		lvl := ParseLogLevel(m[1])
		if lvl != LevelUnknown {
			return lvl
		}
	}

	// 5. Standalone keyword boundary
	if m := wordLevelRegex.FindStringSubmatch(trimmed); len(m) > 1 {
		lvl := ParseLogLevel(m[1])
		if lvl != LevelUnknown {
			return lvl
		}
	}

	return LevelUnknown
}

// DetectTimestamp parses and extracts a timestamp from a raw line or JSON log.
// Returns the parsed time.Time and the matched timestamp string (or empty if none).
func DetectTimestamp(line string) (time.Time, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return time.Time{}, ""
	}

	// 1. JSON field check
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if m := jsonTimeRegex.FindStringSubmatch(trimmed); len(m) > 1 {
			rawVal := strings.Trim(m[1], `"`)
			// Try parsing as numeric epoch
			if num, err := strconv.ParseInt(rawVal, 10, 64); err == nil {
				if num > 1e16 { // Nanoseconds
					return time.Unix(0, num), rawVal
				} else if num > 1e11 { // Milliseconds
					return time.UnixMilli(num), rawVal
				} else { // Seconds
					return time.Unix(num, 0), rawVal
				}
			}
			if fnum, err := strconv.ParseFloat(rawVal, 64); err == nil {
				sec := int64(fnum)
				nsec := int64((fnum - float64(sec)) * 1e9)
				return time.Unix(sec, nsec), rawVal
			}
			// String timestamp
			for _, layout := range timestampLayouts {
				if t, err := time.Parse(layout, rawVal); err == nil {
					return t, rawVal
				}
			}
		}
	}

	// 2. Line prefix patterns
	patterns := []*regexp.Regexp{
		rfc3339PrefixRegex,
		dateSpaceRegex,
		dateSlashRegex,
		syslogPrefixRegex,
		timeOnlyRegex,
	}

	for _, pat := range patterns {
		if m := pat.FindStringSubmatch(trimmed); len(m) > 1 {
			candidate := m[1]
			for _, layout := range timestampLayouts {
				if t, err := time.Parse(layout, candidate); err == nil {
					// If layout is without year (like syslog or time-only), fill current year
					if t.Year() == 0 {
						now := time.Now()
						t = t.AddDate(now.Year(), 0, 0)
					}
					return t, candidate
				}
			}
		}
	}

	return time.Time{}, ""
}

// ParseLine parses a raw line string into a structured LogLine.
func ParseLine(raw string, index int64) LogLine {
	clean := strings.TrimRight(raw, "\r\n")
	lvl := DetectLevel(clean)
	ts, _ := DetectTimestamp(clean)

	var fields map[string]any
	msg := clean

	trimmed := strings.TrimSpace(clean)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
			fields = obj
			if m, ok := obj["msg"].(string); ok && m != "" {
				msg = m
			} else if m, ok := obj["message"].(string); ok && m != "" {
				msg = m
			}
		}
	}

	return LogLine{
		Index:     index,
		Timestamp: ts,
		Level:     lvl,
		Raw:       clean,
		Message:   msg,
		Fields:    fields,
	}
}

// FilterOptions specifies search, level, and timestamp criteria for log filtering.
type FilterOptions struct {
	Level         LogLevel  // Filter by specific level (LevelUnknown = All)
	MinLevel      LogLevel  // Filter by minimum severity threshold
	SearchText    string    // Substring or regex query
	IsRegex       bool      // Whether SearchText should be treated as regex
	CaseSensitive bool      // Exact case matching
	StartTime     time.Time // Optional lower timestamp bound
	EndTime       time.Time // Optional upper timestamp bound

	compiledRegex *regexp.Regexp
	regexErr      error
}

// NewFilterOptions creates an initialized FilterOptions and compiles regex if needed.
func NewFilterOptions(level LogLevel, searchText string, isRegex bool) FilterOptions {
	opts := FilterOptions{
		Level:      level,
		SearchText: searchText,
		IsRegex:    isRegex,
	}
	opts.Compile()
	return opts
}

// Compile compiles the regular expression if IsRegex is enabled.
func (f *FilterOptions) Compile() {
	if f.IsRegex && f.SearchText != "" {
		pattern := f.SearchText
		if !f.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		f.compiledRegex = re
		f.regexErr = err
	} else {
		f.compiledRegex = nil
		f.regexErr = nil
	}
}

// RegexError returns any compilation error for the search pattern.
func (f *FilterOptions) RegexError() error {
	return f.regexErr
}

// Matches checks whether a given LogLine satisfies all active filter options.
func (f *FilterOptions) Matches(line LogLine) bool {
	// 1. Specific Level Filtering
	if f.Level != LevelUnknown {
		switch f.Level {
		case LevelError:
			// Error pill matches ERROR and FATAL
			if line.Level != LevelError && line.Level != LevelFatal {
				return false
			}
		case LevelWarn:
			if line.Level != LevelWarn {
				return false
			}
		case LevelInfo:
			if line.Level != LevelInfo {
				return false
			}
		case LevelDebug:
			if line.Level != LevelDebug {
				return false
			}
		default:
			if line.Level != f.Level {
				return false
			}
		}
	}

	// 2. Minimum severity threshold
	if f.MinLevel != LevelUnknown {
		if line.Level < f.MinLevel {
			return false
		}
	}

	// 3. Timestamp range
	if !f.StartTime.IsZero() && !line.Timestamp.IsZero() {
		if line.Timestamp.Before(f.StartTime) {
			return false
		}
	}
	if !f.EndTime.IsZero() && !line.Timestamp.IsZero() {
		if line.Timestamp.After(f.EndTime) {
			return false
		}
	}

	// 4. Text or Regex search
	if f.SearchText != "" {
		if f.IsRegex {
			if f.compiledRegex == nil {
				f.Compile()
			}
			if f.compiledRegex != nil {
				if !f.compiledRegex.MatchString(line.Raw) {
					return false
				}
			} else {
				// Fallback to substring matching if regex syntax was invalid
				return strings.Contains(strings.ToLower(line.Raw), strings.ToLower(f.SearchText))
			}
		} else {
			if f.CaseSensitive {
				if !strings.Contains(line.Raw, f.SearchText) {
					return false
				}
			} else {
				if !strings.Contains(strings.ToLower(line.Raw), strings.ToLower(f.SearchText)) {
					return false
				}
			}
		}
	}

	return true
}

// FilterLines applies FilterOptions to a slice of LogLine entries.
func FilterLines(lines []LogLine, opts FilterOptions) []LogLine {
	opts.Compile()
	var matched []LogLine
	for _, l := range lines {
		if opts.Matches(l) {
			matched = append(matched, l)
		}
	}
	return matched
}
