package coverage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Format represents the format of the coverage report.
type Format int

const (
	FormatUnknown Format = iota
	FormatGoCoverage
	FormatLCOV
)

// goBlockRegex parses Go coverage profile block lines:
// filepath:startLine.startCol,endLine.endCol numStmt count
// The greedy (.+) captures file paths that may contain colons (e.g. C:\path\file.go).
var goBlockRegex = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+)\s+(\d+)\s+(\d+)$`)

// ParseGoCoverage parses a Go test coverage profile (coverage.out).
func ParseGoCoverage(r io.Reader) (*Profile, error) {
	scanner := bufio.NewScanner(r)
	// Allow scanning long lines if paths or comments are lengthy
	const maxScanCapacity = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScanCapacity)

	profile := NewProfile()
	profile.Mode = "set" // default fallback

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "mode:") {
			profile.Mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
			continue
		}

		m := goBlockRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		filePath := m[1]
		startLine, err1 := strconv.Atoi(m[2])
		_ = m[3] // startCol
		endLine, err2 := strconv.Atoi(m[4])
		_ = m[5] // endCol
		numStmt, err3 := strconv.Atoi(m[6])
		count, err4 := strconv.ParseInt(m[7], 10, 64)

		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}

		fc := profile.GetOrCreateFile(filePath)
		fc.TotalStmts += numStmt
		if count > 0 {
			fc.CoveredStmts += numStmt
		}

		if startLine > endLine {
			startLine, endLine = endLine, startLine
		}

		for l := startLine; l <= endLine; l++ {
			fc.RecordLine(l, count)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading Go coverage profile: %w", err)
	}

	profile.Recalculate()
	return profile, nil
}

// ParseGoCoverageString parses Go coverage data directly from a string.
func ParseGoCoverageString(content string) (*Profile, error) {
	return ParseGoCoverage(strings.NewReader(content))
}

// ParseLCOV parses standard LCOV tracefile format (lcov.info).
func ParseLCOV(r io.Reader) (*Profile, error) {
	scanner := bufio.NewScanner(r)
	const maxScanCapacity = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScanCapacity)

	profile := NewProfile()
	profile.Mode = "lcov"

	var currentFC *FileCoverage

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "SF:"):
			if currentFC != nil {
				currentFC.Recalculate()
			}
			filePath := strings.TrimSpace(strings.TrimPrefix(line, "SF:"))
			currentFC = profile.GetOrCreateFile(filePath)

		case strings.HasPrefix(line, "DA:"):
			if currentFC == nil {
				continue
			}
			daData := strings.TrimSpace(strings.TrimPrefix(line, "DA:"))
			parts := strings.Split(daData, ",")
			if len(parts) >= 2 {
				lineNum, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
				count, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err1 == nil && err2 == nil {
					currentFC.RecordLine(lineNum, count)
				}
			}

		case line == "end_of_record":
			if currentFC != nil {
				currentFC.Recalculate()
				currentFC = nil
			}
		}
	}

	if currentFC != nil {
		currentFC.Recalculate()
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading LCOV profile: %w", err)
	}

	profile.Recalculate()
	return profile, nil
}

// ParseLCOVString parses LCOV coverage data directly from a string.
func ParseLCOVString(content string) (*Profile, error) {
	return ParseLCOV(strings.NewReader(content))
}

// DetectFormat detects whether data is Go coverage, LCOV, or unknown.
func DetectFormat(data []byte) Format {
	s := string(data)
	if strings.Contains(s, "mode: set") || strings.Contains(s, "mode: count") || strings.Contains(s, "mode: atomic") {
		return FormatGoCoverage
	}
	if strings.Contains(s, "SF:") || strings.Contains(s, "end_of_record") || strings.Contains(s, "DA:") {
		return FormatLCOV
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if goBlockRegex.MatchString(line) {
			return FormatGoCoverage
		}
		if strings.HasPrefix(line, "SF:") || strings.HasPrefix(line, "DA:") {
			return FormatLCOV
		}
	}

	return FormatUnknown
}

// Parse automatically detects the format (Go coverage or LCOV) and parses the profile.
func Parse(r io.Reader) (*Profile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read coverage data: %w", err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return NewProfile(), nil
	}

	format := DetectFormat(data)
	switch format {
	case FormatGoCoverage:
		return ParseGoCoverage(bytes.NewReader(data))
	case FormatLCOV:
		return ParseLCOV(bytes.NewReader(data))
	default:
		// Attempt Go coverage first
		prof, err := ParseGoCoverage(bytes.NewReader(data))
		if err == nil && len(prof.Files) > 0 {
			return prof, nil
		}
		// Attempt LCOV fallback
		profLCOV, errLCOV := ParseLCOV(bytes.NewReader(data))
		if errLCOV == nil && len(profLCOV.Files) > 0 {
			return profLCOV, nil
		}
		return nil, errors.New("unrecognized coverage profile format")
	}
}

// ParseString parses coverage data from a string, auto-detecting the format.
func ParseString(content string) (*Profile, error) {
	return Parse(strings.NewReader(content))
}

// ParseFile reads and parses a coverage file from disk.
func ParseFile(filePath string) (*Profile, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open coverage file: %w", err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".out", ".cov":
		return ParseGoCoverage(f)
	case ".info", ".lcov":
		return ParseLCOV(f)
	default:
		return Parse(f)
	}
}

// LoadReader loads coverage data from an io.Reader into the Engine.
func (e *Engine) LoadReader(r io.Reader) error {
	p, err := Parse(r)
	if err != nil {
		return err
	}
	e.SetProfile(p)
	return nil
}

// LoadString loads coverage data from a string into the Engine.
func (e *Engine) LoadString(content string) error {
	return e.LoadReader(strings.NewReader(content))
}

// LoadFile loads a coverage report from disk into the Engine.
func (e *Engine) LoadFile(filePath string) error {
	p, err := ParseFile(filePath)
	if err != nil {
		return err
	}
	e.SetProfile(p)
	return nil
}

// LoadReader loads coverage data from an io.Reader into the default engine.
func LoadReader(r io.Reader) error {
	return defaultEngine.LoadReader(r)
}

// LoadString loads coverage data from a string into the default engine.
func LoadString(content string) error {
	return defaultEngine.LoadString(content)
}

// LoadFile loads a coverage report from disk into the default engine.
func LoadFile(filePath string) error {
	return defaultEngine.LoadFile(filePath)
}
