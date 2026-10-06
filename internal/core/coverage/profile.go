package coverage

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

// LineStatus indicates the test coverage state of a source line.
type LineStatus int

const (
	// LineNeutral indicates a line without executable statements (comments, blank lines, type declarations, etc.).
	LineNeutral LineStatus = iota
	// LineCovered indicates a line whose statements were executed at least once during testing.
	LineCovered
	// LineUncovered indicates an instrumented line whose statements were never executed during testing.
	LineUncovered
)

// String returns a human-readable representation of LineStatus.
func (s LineStatus) String() string {
	switch s {
	case LineCovered:
		return "covered"
	case LineUncovered:
		return "uncovered"
	default:
		return "neutral"
	}
}

// LineDetail contains the coverage status and execution hit count for a single 1-based source line.
type LineDetail struct {
	LineNumber int        `json:"line"`   // 1-based line number in source file
	Status     LineStatus `json:"status"` // LineCovered, LineUncovered, LineNeutral
	Count      int64      `json:"count"`  // Number of times the line was executed
}

// FileCoverage holds coverage data for an individual source file.
type FileCoverage struct {
	FilePath        string              `json:"file_path"`
	Lines           map[int]*LineDetail `json:"lines"` // 1-based line number -> LineDetail
	CoveredLines    int                 `json:"covered_lines"`
	UncoveredLines  int                 `json:"uncovered_lines"`
	TotalLines      int                 `json:"total_lines"`
	CoveragePercent float64             `json:"coverage_percent"`
	CoveredStmts    int                 `json:"covered_stmts,omitempty"`
	TotalStmts      int                 `json:"total_stmts,omitempty"`
	StmtPercent     float64             `json:"stmt_percent,omitempty"`
}

// NewFileCoverage creates an initialized FileCoverage container for the given file path.
func NewFileCoverage(filePath string) *FileCoverage {
	return &FileCoverage{
		FilePath: filePath,
		Lines:    make(map[int]*LineDetail),
	}
}

// RecordLine records execution hits for a 1-based line number.
func (fc *FileCoverage) RecordLine(lineNum int, count int64) {
	if lineNum <= 0 {
		return
	}

	detail, exists := fc.Lines[lineNum]
	if !exists {
		status := LineUncovered
		if count > 0 {
			status = LineCovered
		}
		fc.Lines[lineNum] = &LineDetail{
			LineNumber: lineNum,
			Status:     status,
			Count:      count,
		}
		return
	}

	detail.Count += count
	if detail.Count > 0 {
		detail.Status = LineCovered
	}
}

// Recalculate recalculates line and statement coverage aggregates and percentages for this file.
func (fc *FileCoverage) Recalculate() {
	fc.CoveredLines = 0
	fc.UncoveredLines = 0

	for _, detail := range fc.Lines {
		if detail.Status == LineCovered {
			fc.CoveredLines++
		} else if detail.Status == LineUncovered {
			fc.UncoveredLines++
		}
	}

	fc.TotalLines = fc.CoveredLines + fc.UncoveredLines
	if fc.TotalLines > 0 {
		fc.CoveragePercent = (float64(fc.CoveredLines) / float64(fc.TotalLines)) * 100.0
	} else {
		fc.CoveragePercent = 0.0
	}

	if fc.TotalStmts > 0 {
		fc.StmtPercent = (float64(fc.CoveredStmts) / float64(fc.TotalStmts)) * 100.0
	} else {
		fc.StmtPercent = 0.0
	}
}

// Summary returns the covered lines, total instrumented lines, and coverage percentage for this file.
func (fc *FileCoverage) Summary() (covered int, total int, percent float64) {
	return fc.CoveredLines, fc.TotalLines, fc.CoveragePercent
}

// FormattedPercent returns coverage percentage formatted to 1 decimal place, e.g. "84.5%".
func (fc *FileCoverage) FormattedPercent() string {
	return fmt.Sprintf("%.1f%%", fc.CoveragePercent)
}

// GetLineStatus returns the status of lineIdx (0-based editor line index).
// In editor buffers, lineIdx 0 corresponds to line 1 of the source file.
func (fc *FileCoverage) GetLineStatus(lineIdx int) LineStatus {
	if lineIdx < 0 {
		return LineNeutral
	}
	return fc.GetLineStatusByLine(lineIdx + 1)
}

// GetLineStatusByLine returns the status of lineNum (1-based source line number).
func (fc *FileCoverage) GetLineStatusByLine(lineNum int) LineStatus {
	if lineNum <= 0 {
		return LineNeutral
	}
	if detail, ok := fc.Lines[lineNum]; ok {
		return detail.Status
	}
	return LineNeutral
}

// Profile represents a complete test coverage profile across multiple source files.
type Profile struct {
	mu             sync.RWMutex
	Mode           string                   `json:"mode,omitempty"` // set, count, atomic, or lcov
	Files          map[string]*FileCoverage `json:"files"`
	CoveredLines   int                      `json:"covered_lines"`
	UncoveredLines int                      `json:"uncovered_lines"`
	TotalLines     int                      `json:"total_lines"`
	Percent        float64                  `json:"percent"`
	CoveredStmts   int                      `json:"covered_stmts,omitempty"`
	TotalStmts     int                      `json:"total_stmts,omitempty"`
	StmtPercent    float64                  `json:"stmt_percent,omitempty"`
}

// NewProfile creates an empty Profile ready for population.
func NewProfile() *Profile {
	return &Profile{
		Files: make(map[string]*FileCoverage),
	}
}

// normalizePath converts backslashes to forward slashes, cleans the path, and strips leading ./
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	return p
}

// GetOrCreateFile retrieves an existing FileCoverage entry or creates a new one.
func (p *Profile) GetOrCreateFile(filePath string) *FileCoverage {
	p.mu.Lock()
	defer p.mu.Unlock()

	filePath = strings.TrimSpace(filePath)
	fc, exists := p.Files[filePath]
	if !exists {
		fc = NewFileCoverage(filePath)
		p.Files[filePath] = fc
	}
	return fc
}

// FindFile locates the FileCoverage matching the given filePath.
// It handles exact matches, normalized paths, cross-platform separators, case-insensitivity,
// package prefix suffixes, and unique base names.
func (p *Profile) FindFile(filePath string) *FileCoverage {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Files) == 0 || filePath == "" {
		return nil
	}

	// 1. Direct exact map lookup
	if fc, ok := p.Files[filePath]; ok {
		return fc
	}

	normQuery := normalizePath(filePath)
	lowerQuery := strings.ToLower(normQuery)

	// 2. Normalized path lookup
	if fc, ok := p.Files[normQuery]; ok {
		return fc
	}

	// 3. Case-insensitive normalized match
	for k, fc := range p.Files {
		normKey := normalizePath(k)
		if strings.ToLower(normKey) == lowerQuery {
			return fc
		}
	}

	// 4. Suffix match (e.g. query has full disk path, profile has relative / package path, or vice-versa)
	for k, fc := range p.Files {
		normKey := normalizePath(k)
		lowerKey := strings.ToLower(normKey)

		if strings.HasSuffix(lowerQuery, "/"+lowerKey) || strings.HasSuffix(lowerKey, "/"+lowerQuery) {
			return fc
		}
	}

	// 5. Basename match if unique
	baseQuery := path.Base(normQuery)
	var matched *FileCoverage
	matchCount := 0
	for k, fc := range p.Files {
		normKey := normalizePath(k)
		if strings.EqualFold(path.Base(normKey), baseQuery) {
			matched = fc
			matchCount++
		}
	}
	if matchCount == 1 {
		return matched
	}

	return nil
}

// Recalculate recalculates overall workspace totals and percentages across all files.
func (p *Profile) Recalculate() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CoveredLines = 0
	p.UncoveredLines = 0
	p.CoveredStmts = 0
	p.TotalStmts = 0

	for _, fc := range p.Files {
		fc.Recalculate()
		p.CoveredLines += fc.CoveredLines
		p.UncoveredLines += fc.UncoveredLines
		p.CoveredStmts += fc.CoveredStmts
		p.TotalStmts += fc.TotalStmts
	}

	p.TotalLines = p.CoveredLines + p.UncoveredLines
	if p.TotalLines > 0 {
		p.Percent = (float64(p.CoveredLines) / float64(p.TotalLines)) * 100.0
	} else {
		p.Percent = 0.0
	}

	if p.TotalStmts > 0 {
		p.StmtPercent = (float64(p.CoveredStmts) / float64(p.TotalStmts)) * 100.0
	} else {
		p.StmtPercent = 0.0
	}
}

// Summary returns total covered lines, total lines, and workspace coverage percentage.
func (p *Profile) Summary() (covered int, total int, percent float64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.CoveredLines, p.TotalLines, p.Percent
}

// FormattedPercent returns overall workspace coverage formatted to 1 decimal place, e.g. "84.5%".
func (p *Profile) FormattedPercent() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return fmt.Sprintf("%.1f%%", p.Percent)
}

// GetLineStatus returns the coverage status of lineIdx (0-based editor line index) for filePath.
func (p *Profile) GetLineStatus(filePath string, lineIdx int) LineStatus {
	fc := p.FindFile(filePath)
	if fc == nil {
		return LineNeutral
	}
	return fc.GetLineStatus(lineIdx)
}

// GetLineStatusByLine returns the coverage status of lineNum (1-based source line number) for filePath.
func (p *Profile) GetLineStatusByLine(filePath string, lineNum int) LineStatus {
	fc := p.FindFile(filePath)
	if fc == nil {
		return LineNeutral
	}
	return fc.GetLineStatusByLine(lineNum)
}

// FileSummary returns covered lines, total lines, and percentage for a given file.
func (p *Profile) FileSummary(filePath string) (covered int, total int, percent float64, ok bool) {
	fc := p.FindFile(filePath)
	if fc == nil {
		return 0, 0, 0.0, false
	}
	c, t, pct := fc.Summary()
	return c, t, pct, true
}

// FilePaths returns a sorted list of all file paths in the profile.
func (p *Profile) FilePaths() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	paths := make([]string, 0, len(p.Files))
	for fp := range p.Files {
		paths = append(paths, fp)
	}
	sort.Strings(paths)
	return paths
}

// Engine manages the active coverage profile and provides thread-safe access.
type Engine struct {
	mu      sync.RWMutex
	profile *Profile
}

// NewEngine creates a new Engine instance.
func NewEngine() *Engine {
	return &Engine{
		profile: NewProfile(),
	}
}

// SetProfile sets the active coverage profile.
func (e *Engine) SetProfile(p *Profile) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if p == nil {
		p = NewProfile()
	}
	e.profile = p
}

// GetProfile returns the active coverage profile.
func (e *Engine) GetProfile() *Profile {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.profile
}

// Clear resets the active coverage profile to empty.
func (e *Engine) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.profile = NewProfile()
}

// GetLineStatus returns the coverage status for lineIdx (0-based editor line) in filePath.
func (e *Engine) GetLineStatus(filePath string, lineIdx int) LineStatus {
	e.mu.RLock()
	p := e.profile
	e.mu.RUnlock()
	if p == nil {
		return LineNeutral
	}
	return p.GetLineStatus(filePath, lineIdx)
}

// GetLineStatusByLine returns the coverage status for lineNum (1-based line) in filePath.
func (e *Engine) GetLineStatusByLine(filePath string, lineNum int) LineStatus {
	e.mu.RLock()
	p := e.profile
	e.mu.RUnlock()
	if p == nil {
		return LineNeutral
	}
	return p.GetLineStatusByLine(filePath, lineNum)
}

// Summary returns the active profile's total covered lines, total lines, and percentage.
func (e *Engine) Summary() (covered int, total int, percent float64) {
	e.mu.RLock()
	p := e.profile
	e.mu.RUnlock()
	if p == nil {
		return 0, 0, 0.0
	}
	return p.Summary()
}

// FileSummary returns the summary for a specific file in the active profile.
func (e *Engine) FileSummary(filePath string) (covered int, total int, percent float64, ok bool) {
	e.mu.RLock()
	p := e.profile
	e.mu.RUnlock()
	if p == nil {
		return 0, 0, 0.0, false
	}
	return p.FileSummary(filePath)
}

// Global default engine instance for editor integration
var defaultEngine = NewEngine()

// SetDefaultProfile sets the active profile in the default engine.
func SetDefaultProfile(p *Profile) {
	defaultEngine.SetProfile(p)
}

// GetDefaultProfile returns the active profile from the default engine.
func GetDefaultProfile() *Profile {
	return defaultEngine.GetProfile()
}

// Clear resets the default engine's profile.
func Clear() {
	defaultEngine.Clear()
}

// GetLineStatus returns the coverage status of lineIdx (0-based) for filePath using the active profile.
func GetLineStatus(filePath string, lineIdx int) LineStatus {
	return defaultEngine.GetLineStatus(filePath, lineIdx)
}

// GetLineStatusByLine returns the coverage status of lineNum (1-based) for filePath using the active profile.
func GetLineStatusByLine(filePath string, lineNum int) LineStatus {
	return defaultEngine.GetLineStatusByLine(filePath, lineNum)
}

// Summary returns total covered lines, total lines, and percentage across the active profile.
func Summary() (covered int, total int, percent float64) {
	return defaultEngine.Summary()
}

// FileSummary returns covered lines, total lines, and percentage for filePath in the active profile.
func FileSummary(filePath string) (covered int, total int, percent float64, ok bool) {
	return defaultEngine.FileSummary(filePath)
}
