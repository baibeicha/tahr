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
	"runtime"
	"strconv"
	"strings"
	"time"

	"tahr/internal/core/sdk"
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

// resolveToolchainCommand finds the actual toolchain binary, virtualenv or SDK path.
func (r *Runner) resolveToolchainCommand(cfg *TestingConfig) (string, []string, []string) {
	cmdName := cfg.Command
	var extraArgs []string
	var env []string

	switch strings.ToLower(cfg.ID) {
	case "go":
		if mgr := sdk.GetManager(); mgr != nil {
			if goInfo := mgr.GoSDK(); goInfo != nil && goInfo.BinaryPath != "" {
				cmdName = goInfo.BinaryPath
				if goInfo.BinDir != "" {
					sysPath := os.Getenv("PATH")
					env = append(os.Environ(), "PATH="+goInfo.BinDir+string(os.PathListSeparator)+sysPath)
				}
			}
		}
		if cmdName == "go" {
			if path, err := exec.LookPath("go"); err == nil {
				cmdName = path
			}
		}

	case "python":
		venvCandidates := []string{
			filepath.Join(r.wsDir, ".venv"),
			filepath.Join(r.wsDir, "venv"),
			filepath.Join(r.wsDir, "env"),
		}
		for _, v := range venvCandidates {
			pytestBin := filepath.Join(v, "bin", "pytest")
			if runtime.GOOS == "windows" {
				pytestBin = filepath.Join(v, "Scripts", "pytest.exe")
			}
			if fi, err := os.Stat(pytestBin); err == nil && !fi.IsDir() {
				cmdName = pytestBin
				env = append(os.Environ(), "VIRTUAL_ENV="+v)
				break
			}

			pythonBin := filepath.Join(v, "bin", "python")
			if runtime.GOOS == "windows" {
				pythonBin = filepath.Join(v, "Scripts", "python.exe")
			}
			if fi, err := os.Stat(pythonBin); err == nil && !fi.IsDir() {
				cmdName = pythonBin
				extraArgs = []string{"-m", "pytest"}
				env = append(os.Environ(), "VIRTUAL_ENV="+v)
				break
			}
		}

	case "rust":
		if home, err := os.UserHomeDir(); err == nil {
			cargoBin := filepath.Join(home, ".cargo", "bin", "cargo")
			if runtime.GOOS == "windows" {
				cargoBin = filepath.Join(home, ".cargo", "bin", "cargo.exe")
			}
			if fi, err := os.Stat(cargoBin); err == nil && !fi.IsDir() {
				cmdName = cargoBin
			}
		}

	case "typescript", "javascript", "node":
		localVitest := filepath.Join(r.wsDir, "node_modules", ".bin", "vitest")
		localJest := filepath.Join(r.wsDir, "node_modules", ".bin", "jest")
		if runtime.GOOS == "windows" {
			localVitest += ".cmd"
			localJest += ".cmd"
		}
		if fi, err := os.Stat(localVitest); err == nil && !fi.IsDir() {
			cmdName = localVitest
		} else if fi, err := os.Stat(localJest); err == nil && !fi.IsDir() {
			cmdName = localJest
		}

	case "java", "kotlin":
		gradlew := filepath.Join(r.wsDir, "gradlew")
		mvnw := filepath.Join(r.wsDir, "mvnw")
		if runtime.GOOS == "windows" {
			gradlew += ".bat"
			mvnw += ".cmd"
		}
		if fi, err := os.Stat(gradlew); err == nil && !fi.IsDir() {
			cmdName = gradlew
		} else if fi, err := os.Stat(mvnw); err == nil && !fi.IsDir() {
			cmdName = mvnw
		}

	case "php":
		vendorPhpunit := filepath.Join(r.wsDir, "vendor", "bin", "phpunit")
		if runtime.GOOS == "windows" {
			vendorPhpunit += ".bat"
		}
		if fi, err := os.Stat(vendorPhpunit); err == nil && !fi.IsDir() {
			cmdName = vendorPhpunit
		}
	}

	return cmdName, extraArgs, env
}

// DetectWorkspaceLanguages scans workspace markers to find all active testable languages.
func (r *Runner) DetectWorkspaceLanguages(wsDir string) []*CompiledTestingConfig {
	var detected []*CompiledTestingConfig
	seen := make(map[string]bool)

	markerMap := map[string][]string{
		"go":         {"go.mod", "go.work"},
		"python":     {"pyproject.toml", "requirements.txt", "Pipfile", "setup.py"},
		"rust":       {"Cargo.toml", "Cargo.lock"},
		"typescript": {"package.json", "tsconfig.json"},
		"java":       {"pom.xml", "build.gradle"},
		"kotlin":     {"build.gradle.kts"},
		"php":        {"composer.json"},
		"zig":        {"build.zig"},
		"csharp":     {"*.csproj", "*.sln"},
		"ruby":       {"Gemfile", "Rakefile"},
		"c_cpp":      {"CMakeLists.txt", "Makefile"},
	}

	for langID, markers := range markerMap {
		for _, m := range markers {
			if strings.Contains(m, "*") {
				matches, _ := filepath.Glob(filepath.Join(wsDir, m))
				if len(matches) > 0 {
					if cfg := r.registry.FindByLanguage(langID); cfg != nil && !seen[cfg.Config.ID] {
						detected = append(detected, cfg)
						seen[cfg.Config.ID] = true
					}
					break
				}
			} else {
				if _, err := os.Stat(filepath.Join(wsDir, m)); err == nil {
					if cfg := r.registry.FindByLanguage(langID); cfg != nil && !seen[cfg.Config.ID] {
						detected = append(detected, cfg)
						seen[cfg.Config.ID] = true
					}
					break
				}
			}
		}
	}

	return detected
}

// RunSingle executes an individual test case with environment-aware command resolution.
func (r *Runner) RunSingle(ctx context.Context, tc *TestCase) (*TestCase, error) {
	if tc == nil {
		return nil, fmt.Errorf("test case is nil")
	}

	compiled := r.registry.FindByFile(tc.FilePath)
	if compiled == nil {
		return nil, fmt.Errorf("no testing toolchain found for %s", tc.FilePath)
	}

	cfg := compiled.Config
	cmdName, extraArgs, env := r.resolveToolchainCommand(cfg)

	rawArgs := cfg.RunSingleArgs
	if len(rawArgs) == 0 {
		rawArgs = cfg.Args
	}

	subArgs := substituteArgs(rawArgs, map[string]string{
		"test":    tc.Name,
		"file":    tc.FilePath,
		"package": tc.Package,
	})

	args := append([]string{}, extraArgs...)
	args = append(args, subArgs...)

	tc.Status = TestStatusRunning
	startTime := time.Now()

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Dir = r.wsDir
	if len(env) > 0 {
		cmd.Env = env
	}

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

	// Fallback if not matched individually in output stream
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		tc.Status = TestStatusPassed
	} else {
		tc.Status = TestStatusFailed
		tc.ErrorMessage = "Test failed with non-zero exit code"
	}

	return tc, nil
}

// RunAll executes test suites across all detected or specified languages in the workspace.
func (r *Runner) RunAll(ctx context.Context, langID string) (*TestRunResult, error) {
	var targetConfigs []*CompiledTestingConfig
	if langID != "" {
		if c := r.registry.FindByLanguage(langID); c != nil {
			targetConfigs = append(targetConfigs, c)
		}
	} else {
		targetConfigs = r.DetectWorkspaceLanguages(r.wsDir)
		if len(targetConfigs) == 0 {
			targetConfigs = r.registry.AllConfigs()
		}
	}

	if len(targetConfigs) == 0 {
		return nil, fmt.Errorf("no testing toolchain found for workspace")
	}

	aggResult := &TestRunResult{
		Suites: make([]*TestSuite, 0),
	}

	for _, compiled := range targetConfigs {
		cfg := compiled.Config
		cmdName, extraArgs, env := r.resolveToolchainCommand(cfg)

		args := append([]string{}, extraArgs...)
		if len(cfg.RunAllArgs) > 0 {
			args = append(args, cfg.RunAllArgs...)
		} else {
			args = append(args, cfg.Args...)
		}

		startTime := time.Now()
		cmd := exec.CommandContext(ctx, cmdName, args...)
		cmd.Dir = r.wsDir
		if len(env) > 0 {
			cmd.Env = env
		}

		var outBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		_ = cmd.Run()
		duration := time.Since(startTime)

		output := strings.ReplaceAll(outBuf.String(), "\r\n", "\n")
		aggResult.RawOutput += output + "\n"

		parsedCases := ParseOutput(output, compiled)
		suitesMap := make(map[string]*TestSuite)

		for _, tc := range parsedCases {
			pkg := tc.Package
			if pkg == "" {
				pkg = cfg.Name
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
			aggResult.Summary.Total++

			switch tc.Status {
			case TestStatusPassed:
				suite.Passed++
				aggResult.Summary.Passed++
			case TestStatusFailed:
				suite.Failed++
				aggResult.Summary.Failed++
			case TestStatusSkipped:
				suite.Skipped++
				aggResult.Summary.Skipped++
			}
		}

		for _, s := range suitesMap {
			aggResult.Suites = append(aggResult.Suites, s)
		}
		aggResult.Summary.Duration += duration
	}

	return aggResult, nil
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
