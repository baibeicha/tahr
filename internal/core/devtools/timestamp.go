package devtools

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Supported date and time layouts for ISO 8601 parsing.
var iso8601Layouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
}

// TimestampInfo contains comprehensive parsed timestamp information.
type TimestampInfo struct {
	UnixSec      int64     `json:"unix_sec"`
	UnixMilli    int64     `json:"unix_milli"`
	UnixNano     int64     `json:"unix_nano"`
	UTC          time.Time `json:"utc"`
	Local        time.Time `json:"local"`
	ISO8601UTC   string    `json:"iso8601_utc"`
	ISO8601Local string    `json:"iso8601_local"`
	HumanUTC     string    `json:"human_utc"`
	HumanLocal   string    `json:"human_local"`
	Relative     string    `json:"relative"`
}

// UnixToISO8601UTC converts epoch seconds to an ISO 8601 (RFC 3339) UTC string.
func UnixToISO8601UTC(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// UnixToISO8601Local converts epoch seconds to an ISO 8601 (RFC 3339) local string.
func UnixToISO8601Local(sec int64) string {
	return time.Unix(sec, 0).Local().Format(time.RFC3339)
}

// UnixMilliToISO8601UTC converts epoch milliseconds to an ISO 8601 UTC string.
func UnixMilliToISO8601UTC(milli int64) string {
	return time.UnixMilli(milli).UTC().Format("2006-01-02T15:04:05.000Z")
}

// UnixMilliToISO8601Local converts epoch milliseconds to an ISO 8601 local string.
func UnixMilliToISO8601Local(milli int64) string {
	return time.UnixMilli(milli).Local().Format("2006-01-02T15:04:05.000Z07:00")
}

// FormatISO8601UTC formats a time.Time in UTC RFC 3339 format.
func FormatISO8601UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// FormatISO8601Local formats a time.Time in local RFC 3339 format.
func FormatISO8601Local(t time.Time) string {
	return t.Local().Format(time.RFC3339)
}

// ParseISO8601 parses a datetime string using supported ISO 8601 and RFC 3339 formats.
// Formats lacking timezone specifications are assumed to be in UTC.
func ParseISO8601(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp string")
	}

	for _, layout := range iso8601Layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse ISO 8601 timestamp: %q", s)
}

// ISO8601ToUnix parses an ISO 8601 string and returns Unix epoch seconds.
func ISO8601ToUnix(s string) (int64, error) {
	t, err := ParseISO8601(s)
	if err != nil {
		return 0, err
	}
	return t.Unix(), nil
}

// ISO8601ToUnixMilli parses an ISO 8601 string and returns Unix epoch milliseconds.
func ISO8601ToUnixMilli(s string) (int64, error) {
	t, err := ParseISO8601(s)
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

// FromTime builds TimestampInfo from a time.Time object relative to now.
func FromTime(t time.Time) *TimestampInfo {
	return FromTimeAt(t, time.Now())
}

// FromTimeAt builds TimestampInfo from a time.Time object relative to a reference time.
func FromTimeAt(t, ref time.Time) *TimestampInfo {
	tUTC := t.UTC()
	tLocal := t.Local()

	return &TimestampInfo{
		UnixSec:      t.Unix(),
		UnixMilli:    t.UnixMilli(),
		UnixNano:     t.UnixNano(),
		UTC:          tUTC,
		Local:        tLocal,
		ISO8601UTC:   tUTC.Format(time.RFC3339),
		ISO8601Local: tLocal.Format(time.RFC3339),
		HumanUTC:     tUTC.Format("2006-01-02 15:04:05 MST"),
		HumanLocal:   tLocal.Format("2006-01-02 15:04:05 MST"),
		Relative:     formatRelativeTime(t, ref),
	}
}

// FromUnixSec builds TimestampInfo from epoch seconds.
func FromUnixSec(sec int64) *TimestampInfo {
	return FromTime(time.Unix(sec, 0))
}

// FromUnixMilli builds TimestampInfo from epoch milliseconds.
func FromUnixMilli(milli int64) *TimestampInfo {
	return FromTime(time.UnixMilli(milli))
}

// ParseTimestamp parses an arbitrary timestamp string (epoch seconds/millis/nanos or ISO 8601).
func ParseTimestamp(input string) (*TimestampInfo, error) {
	return ParseTimestampAt(input, time.Now())
}

// ParseTimestampAt parses an arbitrary timestamp string relative to a reference time.
func ParseTimestampAt(input string, ref time.Time) (*TimestampInfo, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("empty timestamp input")
	}

	// Check if input is a pure integer number
	if val, err := strconv.ParseInt(s, 10, 64); err == nil {
		absVal := val
		if absVal < 0 {
			absVal = -absVal
		}

		var t time.Time
		switch {
		case absVal < 100000000000: // < 1e11: Unix seconds (up to Nov 5138)
			t = time.Unix(val, 0)
		case absVal < 100000000000000: // < 1e14: Unix milliseconds
			t = time.UnixMilli(val)
		case absVal < 100000000000000000: // < 1e17: Unix microseconds
			t = time.UnixMicro(val)
		default: // Unix nanoseconds
			t = time.Unix(0, val)
		}
		return FromTimeAt(t, ref), nil
	}

	// Try ISO 8601 formats
	t, err := ParseISO8601(s)
	if err != nil {
		return nil, err
	}

	return FromTimeAt(t, ref), nil
}

// formatRelativeTime generates human-friendly relative time strings (e.g. "5 minutes ago", "in 2 hours").
func formatRelativeTime(t, now time.Time) string {
	diff := now.Sub(t)
	isPast := diff >= 0
	if !isPast {
		diff = -diff
	}

	sec := int64(diff.Seconds())
	if sec < 2 {
		return "just now"
	}

	var durStr string
	switch {
	case sec < 60:
		durStr = fmt.Sprintf("%d seconds", sec)
	case sec < 120:
		durStr = "1 minute"
	case sec < 3600:
		durStr = fmt.Sprintf("%d minutes", sec/60)
	case sec < 7200:
		durStr = "1 hour"
	case sec < 86400:
		durStr = fmt.Sprintf("%d hours", sec/3600)
	case sec < 172800:
		if isPast {
			return "yesterday"
		}
		return "tomorrow"
	default:
		days := sec / 86400
		durStr = fmt.Sprintf("%d days", days)
	}

	if isPast {
		return durStr + " ago"
	}
	return "in " + durStr
}
