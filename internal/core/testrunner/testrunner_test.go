package testrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompileConfig_ValidAndInvalid(t *testing.T) {
	// Valid config
	cfg := &TestingConfig{
		ID:         "go",
		Framework:  "go test",
		Languages:  []string{"go"},
		Extensions: []string{".go"},
		Command:    "go",
		Discovery: DiscoveryConfig{
			ExtractRegexes: []string{`func\s+(Test\w+)\s*\(`},
		},
		OutputParsing: OutputParsingConfig{
			PassPattern: `--- PASS:\s+(\S+)`,
			FailPattern: `--- FAIL:\s+(\S+)`,
		},
	}

	compiled, err := CompileConfig(cfg)
	if err != nil {
		t.Fatalf("CompileConfig failed: %v", err)
	}
	if len(compiled.Discovery.ExtractRegexes) != 1 {
		t.Errorf("Expected 1 extract regex, got %d", len(compiled.Discovery.ExtractRegexes))
	}
	if compiled.OutputParsing.PassRegex == nil {
		t.Errorf("Expected compiled PassRegex, got nil")
	}

	// Invalid regex
	invalidCfg := &TestingConfig{
		ID: "invalid",
		Discovery: DiscoveryConfig{
			ExtractRegexes: []string{`(?P<incomplete`},
		},
	}
	if _, err := CompileConfig(invalidCfg); err == nil {
		t.Fatalf("Expected CompileConfig error on invalid regex, got nil")
	}
}

func TestRegistry_RegistrationAndLookup(t *testing.T) {
	reg := DefaultRegistry()

	// Verify default toolchains are registered
	goCfg := reg.FindByLanguage("go")
	if goCfg == nil {
		t.Fatalf("Expected 'go' toolchain to be registered")
	}
	if goCfg.Config.Command != "go" {
		t.Errorf("Expected command 'go', got %s", goCfg.Config.Command)
	}

	pyCfg := reg.FindByLanguage("python")
	if pyCfg == nil {
		t.Fatalf("Expected 'python' toolchain to be registered")
	}

	// Lookup by file extension
	byExt := reg.FindByExtension(".go")
	if byExt == nil || byExt.Config.ID != "go" {
		t.Errorf("FindByExtension('.go') failed: %v", byExt)
	}

	// Lookup by specific file path
	fileCfg := reg.FindByFile("internal/core/auth_test.go")
	if fileCfg == nil || fileCfg.Config.ID != "go" {
		t.Errorf("FindByFile('auth_test.go') failed: %v", fileCfg)
	}
}

func TestParseOutput_GoTestOutput(t *testing.T) {
	reg := DefaultRegistry()
	goCfg := reg.FindByLanguage("go")
	if goCfg == nil {
		t.Fatal("go toolchain missing")
	}

	sampleOutput := `
=== RUN   TestLogin_Success
--- PASS: TestLogin_Success (0.02s)
=== RUN   TestLogin_InvalidPassword
    auth_test.go:42: passwords do not match
--- FAIL: TestLogin_InvalidPassword (0.05s)
=== RUN   TestLogin_SkipLDAP
--- SKIP: TestLogin_SkipLDAP (0.00s)
PASS
FAIL
`

	cases := ParseOutput(sampleOutput, goCfg)
	if len(cases) != 3 {
		t.Fatalf("Expected 3 parsed cases, got %d", len(cases))
	}

	// 1. Pass
	if cases[0].Name != "TestLogin_Success" || cases[0].Status != TestStatusPassed {
		t.Errorf("Case 0 mismatch: %+v", cases[0])
	}
	if cases[0].Duration != 20*time.Millisecond {
		t.Errorf("Case 0 duration mismatch, expected 20ms, got %v", cases[0].Duration)
	}

	// 2. Fail
	if cases[1].Name != "TestLogin_InvalidPassword" || cases[1].Status != TestStatusFailed {
		t.Errorf("Case 1 mismatch: %+v", cases[1])
	}
	if len(cases[1].Traceback) == 0 {
		t.Errorf("Case 1 expected traceback lines, got none")
	}

	// 3. Skip
	if cases[2].Name != "TestLogin_SkipLDAP" || cases[2].Status != TestStatusSkipped {
		t.Errorf("Case 2 mismatch: %+v", cases[2])
	}
}

func TestParseOutput_PytestOutput(t *testing.T) {
	reg := DefaultRegistry()
	pyCfg := reg.FindByLanguage("python")
	if pyCfg == nil {
		t.Fatal("python toolchain missing")
	}

	sampleOutput := `
============================= test session starts =============================
tests/test_api.py::test_create_user PASSED                             [ 33%]
tests/test_api.py::test_delete_user FAILED                             [ 66%]
tests/test_api.py::test_billing_mock SKIPPED                           [100%]
=================================== FAILURES ===================================
_______________________________ test_delete_user _______________________________
tests/test_api.py:54: in test_delete_user
    assert response.status_code == 204
E   AssertionError: assert 404 == 204
`

	cases := ParseOutput(sampleOutput, pyCfg)
	if len(cases) != 3 {
		t.Fatalf("Expected 3 parsed cases, got %d", len(cases))
	}

	if cases[0].Status != TestStatusPassed {
		t.Errorf("Expected PASSED for case 0, got %s", cases[0].Status)
	}
	if cases[1].Status != TestStatusFailed {
		t.Errorf("Expected FAILED for case 1, got %s", cases[1].Status)
	}
	if cases[2].Status != TestStatusSkipped {
		t.Errorf("Expected SKIPPED for case 2, got %s", cases[2].Status)
	}
}

func TestDiscoverTests_MockDir(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "calculator_test.go")
	code := `
package calc

import "testing"

func TestAdd(t *testing.T) {
}

func TestSubtract(t *testing.T) {
}

func helperFunc() {}
`
	if err := os.WriteFile(testFile, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(tmpDir, DefaultRegistry())
	suites, err := runner.DiscoverTests(context.Background())
	if err != nil {
		t.Fatalf("DiscoverTests error: %v", err)
	}

	if len(suites) == 0 {
		t.Fatalf("Expected discovered suites, got 0")
	}

	totalTests := 0
	for _, s := range suites {
		totalTests += len(s.Tests)
	}
	if totalTests != 2 {
		t.Fatalf("Expected 2 discovered tests, got %d", totalTests)
	}
}

func TestSubstituteArgs(t *testing.T) {
	templates := []string{"test", "-run", "^{{test}}$", "{{package}}"}
	vars := map[string]string{
		"test":    "TestUserAuth",
		"package": "./pkg/auth/...",
	}

	res := substituteArgs(templates, vars)
	expected := []string{"test", "-run", "^TestUserAuth$", "./pkg/auth/..."}
	for i, arg := range res {
		if arg != expected[i] {
			t.Errorf("[%d] expected %q, got %q", i, expected[i], arg)
		}
	}
}
