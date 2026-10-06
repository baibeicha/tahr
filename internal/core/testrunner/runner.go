package testrunner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TestStatus defines the execution state of a test case.
type TestStatus string

const (
	TestStatusPending TestStatus = "pending"
	TestStatusRunning TestStatus = "running"
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
	TestStatusSkipped TestStatus = "skipped"
)

// TestCase represents an individual test function or method.
type TestCase struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Package      string        `json:"package"`
	FilePath     string        `json:"file_path"`
	Line         int           `json:"line"`
	Status       TestStatus    `json:"status"`
	Duration     time.Duration `json:"duration"`
	ErrorMessage string        `json:"error_message,omitempty"`
	Traceback    []string      `json:"traceback,omitempty"`
	Output       string        `json:"output,omitempty"`
}

// TestSuite represents an aggregation of tests in a package or file.
type TestSuite struct {
	Name     string        `json:"name"`
	Package  string        `json:"package"`
	FilePath string        `json:"file_path,omitempty"`
	Tests    []*TestCase   `json:"tests"`
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Duration time.Duration `json:"duration"`
}

// TestRunSummary provides aggregate counts across all executed suites.
type TestRunSummary struct {
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Duration time.Duration `json:"duration"`
}

// TestRunResult holds the complete result of a test runner execution.
type TestRunResult struct {
	Suites    []*TestSuite   `json:"suites"`
	Summary   TestRunSummary `json:"summary"`
	RawOutput string         `json:"raw_output"`
}

// Runner coordinates discovery and execution of tests using declarative configs.
type Runner struct {
	registry *TestingRegistry
	wsDir    string
}

// NewRunner creates a Runner bound to a workspace directory and registry.
func NewRunner(wsDir string, reg *TestingRegistry) *Runner {
	if reg == nil {
		reg = DefaultRegistry(filepath.Join(wsDir, "plugins"))
	}
	return &Runner{
		registry: reg,
		wsDir:    wsDir,
	}
}

// DiscoverTests scans workspace files and extracts test cases.
func (r *Runner) DiscoverTests(ctx context.Context) ([]*TestSuite, error) {
	suitesMap := make(map[string]*TestSuite)

	err := filepath.WalkDir(r.wsDir, func(path string, d os.DirEntry, err error) error {
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
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		compiled := r.registry.FindByFile(path)
		if compiled == nil {
			return nil
		}

		// Extract tests from file
		tests, err := extractTestsFromFile(path, compiled)
		if err != nil || len(tests) == 0 {
			return nil
		}

		relPath, _ := filepath.Rel(r.wsDir, path)
		pkgName := filepath.Dir(relPath)
		if pkgName == "." {
			pkgName = filepath.Base(r.wsDir)
		}

		suite, exists := suitesMap[pkgName]
		if !exists {
			suite = &TestSuite{
				Name:     pkgName,
				Package:  pkgName,
				FilePath: relPath,
			}
			suitesMap[pkgName] = suite
		}

		suite.Tests = append(suite.Tests, tests...)
		suite.Total += len(tests)

		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, err
	}

	result := make([]*TestSuite, 0, len(suitesMap))
	for _, s := range suitesMap {
		result = append(result, s)
	}

	return result, nil
}

// extractTestsFromFile scans source lines matching ExtractRegexes.
func extractTestsFromFile(filePath string, compiled *CompiledTestingConfig) ([]*TestCase, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var tests []*TestCase
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		for _, re := range compiled.Discovery.ExtractRegexes {
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				testName := matches[1]
				tests = append(tests, &TestCase{
					ID:       fmt.Sprintf("%s:%s", filePath, testName),
					Name:     testName,
					FilePath: filePath,
					Line:     lineNum,
					Status:   TestStatusPending,
				})
				break
			}
		}
	}

	return tests, scanner.Err()
}

// RunSingle executes an individual test case.
func (r *Runner) RunSingle(ctx context.Context, tc *TestCase) (*TestCase, error) {
	if tc == nil {
		return nil, fmt.Errorf("test case is nil")
	}

	compiled := r.registry.FindByFile(tc.FilePath)
	if compiled == nil {
		return nil, fmt.Errorf("no testing toolchain found for %s", tc.FilePath)
	}

	cfg := compiled.Config
	args := substituteArgs(cfg.RunSingleArgs, map[string]string{
		"test":    tc.Name,
		"file":    tc.FilePath,
		"package": tc.Package,
	})
	if len(args) == 0 {
		args = append([]string{}, cfg.Args...)
	}

	tc.Status = TestStatusRunning
	startTime := time.Now()

	cmd := exec.CommandContext(ctx, cfg.Command, args...)
	cmd.Dir = r.wsDir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	_ = cmd.Run()
	duration := time.Since(startTime)

	output := strings.ReplaceAll(outBuf.String(), "\r\n", "\n")
	tc.Duration = duration
	tc.Output = output

	// Parse test status
	parsed := ParseOutput(output, compiled)
	for _, p := range parsed {
		if p.Name == tc.Name || strings.HasSuffix(p.Name, tc.Name) {
			tc.Status = p.Status
			tc.ErrorMessage = p.ErrorMessage
			tc.Traceback = p.Traceback
			if p.Duration > 0 {
				tc.Duration = p.Duration
			}
			return tc, nil
		}
	}

	// Fallback if not matched individually in stream
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		tc.Status = TestStatusPassed
	} else {
		tc.Status = TestStatusFailed
		tc.ErrorMessage = "Test failed with non-zero exit code"
	}

	return tc, nil
}

// RunAll executes the entire test suite across the workspace.
func (r *Runner) RunAll(ctx context.Context, langID string) (*TestRunResult, error) {
	var compiled *CompiledTestingConfig
	if langID != "" {
		compiled = r.registry.FindByLanguage(langID)
	}
	if compiled == nil {
		configs := r.registry.AllConfigs()
		if len(configs) > 0 {
			compiled = configs[0]
		}
	}
	if compiled == nil {
		return nil, fmt.Errorf("no testing configuration found")
	}

	cfg := compiled.Config
	args := cfg.RunAllArgs
	if len(args) == 0 {
		args = cfg.Args
	}

	startTime := time.Now()
	cmd := exec.CommandContext(ctx, cfg.Command, args...)
	cmd.Dir = r.wsDir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	_ = cmd.Run()
	duration := time.Since(startTime)

	output := strings.ReplaceAll(outBuf.String(), "\r\n", "\n")
	parsedCases := ParseOutput(output, compiled)

	// Build suites from parsed test cases
	suitesMap := make(map[string]*TestSuite)
	totalPassed := 0
	totalFailed := 0
	totalSkipped := 0

	for _, tc := range parsedCases {
		pkg := tc.Package
		if pkg == "" {
			pkg = "default"
		}
		suite, exists := suitesMap[pkg]
		if !exists {
			suite = &TestSuite{
				Name:    pkg,
				Package: pkg,
			}
			suitesMap[pkg] = suite
		}
		suite.Tests = append(suite.Tests, tc)
		suite.Total++
		switch tc.Status {
		case TestStatusPassed:
			suite.Passed++
			totalPassed++
		case TestStatusFailed:
			suite.Failed++
			totalFailed++
		case TestStatusSkipped:
			suite.Skipped++
			totalSkipped++
		}
	}

	suites := make([]*TestSuite, 0, len(suitesMap))
	for _, s := range suitesMap {
		suites = append(suites, s)
	}

	return &TestRunResult{
		Suites: suites,
		Summary: TestRunSummary{
			Total:    len(parsedCases),
			Passed:   totalPassed,
			Failed:   totalFailed,
			Skipped:  totalSkipped,
			Duration: duration,
		},
		RawOutput: output,
	}, nil
}

// ParseOutput parses stdout/stderr into structured TestCases using compiled regex patterns.
func ParseOutput(output string, compiled *CompiledTestingConfig) []*TestCase {
	if compiled == nil {
		return nil
	}

	var results []*TestCase
	lines := strings.Split(output, "\n")

	var currentTraceback []string
	capturingTraceback := false
	var lastFailedCase *TestCase

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 1. Check Pass pattern
		if compiled.OutputParsing.PassRegex != nil {
			if m := compiled.OutputParsing.PassRegex.FindStringSubmatch(trimmed); len(m) > 1 {
				name := extractFirstNonEmpty(m[1:])
				dur := parseDuration(m)
				results = append(results, &TestCase{
					Name:     name,
					Status:   TestStatusPassed,
					Duration: dur,
				})
				capturingTraceback = false
				continue
			}
		}

		// 2. Check Fail pattern
		if compiled.OutputParsing.FailRegex != nil {
			if m := compiled.OutputParsing.FailRegex.FindStringSubmatch(trimmed); len(m) > 1 {
				name := extractFirstNonEmpty(m[1:])
				dur := parseDuration(m)
				failedCase := &TestCase{
					Name:     name,
					Status:   TestStatusFailed,
					Duration: dur,
				}
				results = append(results, failedCase)
				lastFailedCase = failedCase
				capturingTraceback = true
				continue
			}
		}

		// 3. Check Skip pattern
		if compiled.OutputParsing.SkipRegex != nil {
			if m := compiled.OutputParsing.SkipRegex.FindStringSubmatch(trimmed); len(m) > 1 {
				name := extractFirstNonEmpty(m[1:])
				results = append(results, &TestCase{
					Name:   name,
					Status: TestStatusSkipped,
				})
				capturingTraceback = false
				continue
			}
		}

		// 4. Capture traceback for failures
		if capturingTraceback && lastFailedCase != nil {
			if compiled.OutputParsing.TracebackEndRegex != nil && compiled.OutputParsing.TracebackEndRegex.MatchString(trimmed) {
				lastFailedCase.Traceback = currentTraceback
				currentTraceback = nil
				capturingTraceback = false
			} else {
				currentTraceback = append(currentTraceback, line)
				if lastFailedCase.ErrorMessage == "" && (strings.Contains(trimmed, "Error") || strings.Contains(trimmed, "fail") || strings.Contains(trimmed, ": ")) {
					lastFailedCase.ErrorMessage = trimmed
				}
			}
		}
	}

	if lastFailedCase != nil && len(currentTraceback) > 0 && len(lastFailedCase.Traceback) == 0 {
		lastFailedCase.Traceback = currentTraceback
	}

	return results
}

// substituteArgs replaces template placeholders {{test}}, {{file}}, {{package}} in CLI arguments.
func substituteArgs(templateArgs []string, vars map[string]string) []string {
	result := make([]string, len(templateArgs))
	for i, arg := range templateArgs {
		val := arg
		for k, v := range vars {
			placeholder := "{{" + k + "}}"
			val = strings.ReplaceAll(val, placeholder, v)
		}
		result[i] = val
	}
	return result
}

func extractFirstNonEmpty(matches []string) string {
	for _, m := range matches {
		if m != "" && !isDurationString(m) {
			return m
		}
	}
	return ""
}

var durationRegex = regexp.MustCompile(`^[0-9.]+(?:s|ms|ns|m|h)$`)

func isDurationString(s string) bool {
	return durationRegex.MatchString(s)
}

func parseDuration(matches []string) time.Duration {
	for _, m := range matches {
		if isDurationString(m) {
			d, err := time.ParseDuration(m)
			if err == nil {
				return d
			}
			// If number + s
			if strings.HasSuffix(m, "s") {
				secStr := strings.TrimSuffix(m, "s")
				sec, err := strconv.ParseFloat(secStr, 64)
				if err == nil {
					return time.Duration(sec * float64(time.Second))
				}
			}
		}
	}
	return 0
}
