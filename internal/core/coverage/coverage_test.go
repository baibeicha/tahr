package coverage

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestParseGoCoverage_SetMode(t *testing.T) {
	content := `mode: set
tahr/internal/core/math.go:10.2,12.5 3 1
tahr/internal/core/math.go:14.2,15.10 2 0
`
	prof, err := ParseGoCoverageString(content)
	if err != nil {
		t.Fatalf("ParseGoCoverageString failed: %v", err)
	}

	if prof.Mode != "set" {
		t.Fatalf("expected mode 'set', got %q", prof.Mode)
	}

	fc := prof.FindFile("tahr/internal/core/math.go")
	if fc == nil {
		t.Fatalf("expected to find file math.go")
	}

	// Line 10, 11, 12 should be covered
	for l := 10; l <= 12; l++ {
		// Test 0-based lineIdx
		status0 := fc.GetLineStatus(l - 1)
		if status0 != LineCovered {
			t.Errorf("expected line %d (lineIdx %d) to be LineCovered, got %v", l, l-1, status0)
		}
		// Test 1-based line number
		status1 := fc.GetLineStatusByLine(l)
		if status1 != LineCovered {
			t.Errorf("expected line %d to be LineCovered by line number, got %v", l, status1)
		}
	}

	// Line 13 has no statements -> LineNeutral
	if status := fc.GetLineStatus(12); status != LineNeutral {
		t.Errorf("expected line 13 (lineIdx 12) to be LineNeutral, got %v", status)
	}
	if status := fc.GetLineStatusByLine(13); status != LineNeutral {
		t.Errorf("expected line 13 to be LineNeutral, got %v", status)
	}

	// Line 14, 15 should be uncovered
	for l := 14; l <= 15; l++ {
		status0 := fc.GetLineStatus(l - 1)
		if status0 != LineUncovered {
			t.Errorf("expected line %d (lineIdx %d) to be LineUncovered, got %v", l, l-1, status0)
		}
		status1 := fc.GetLineStatusByLine(l)
		if status1 != LineUncovered {
			t.Errorf("expected line %d to be LineUncovered by line number, got %v", l, status1)
		}
	}

	// Check summary: 3 covered lines (10, 11, 12), 2 uncovered lines (14, 15) -> Total: 5 lines, 60.0%
	cov, tot, pct := fc.Summary()
	if cov != 3 || tot != 5 {
		t.Fatalf("expected 3 covered, 5 total, got cov=%d, tot=%d", cov, tot)
	}
	if math.Abs(pct-60.0) > 0.001 {
		t.Fatalf("expected 60.0%%, got %.2f%%", pct)
	}
	if fc.FormattedPercent() != "60.0%" {
		t.Fatalf("expected '60.0%%', got %q", fc.FormattedPercent())
	}
}

func TestParseGoCoverage_CountMode(t *testing.T) {
	content := `mode: count
pkg/calc/calc.go:5.1,7.15 3 25
pkg/calc/calc.go:8.1,9.15 2 0
`
	prof, err := ParseGoCoverageString(content)
	if err != nil {
		t.Fatalf("ParseGoCoverageString failed: %v", err)
	}

	if prof.Mode != "count" {
		t.Fatalf("expected mode 'count', got %q", prof.Mode)
	}

	fc := prof.FindFile("pkg/calc/calc.go")
	if fc == nil {
		t.Fatalf("file not found")
	}

	// Line 5 should have count 25 and status LineCovered
	detail5 := fc.Lines[5]
	if detail5 == nil || detail5.Status != LineCovered || detail5.Count != 25 {
		t.Fatalf("unexpected detail for line 5: %+v", detail5)
	}

	// Line 8 should have count 0 and status LineUncovered
	detail8 := fc.Lines[8]
	if detail8 == nil || detail8.Status != LineUncovered || detail8.Count != 0 {
		t.Fatalf("unexpected detail for line 8: %+v", detail8)
	}
}

func TestParseGoCoverage_AtomicMode(t *testing.T) {
	content := `mode: atomic
service/worker.go:20.1,22.10 2 100
`
	prof, err := ParseGoCoverageString(content)
	if err != nil {
		t.Fatalf("ParseGoCoverageString failed: %v", err)
	}
	if prof.Mode != "atomic" {
		t.Fatalf("expected mode 'atomic', got %q", prof.Mode)
	}
	if prof.GetLineStatus("service/worker.go", 19) != LineCovered {
		t.Fatalf("expected line 20 to be LineCovered")
	}
}

func TestParseGoCoverage_OverlappingBlocks(t *testing.T) {
	// Line 10 has both an uncovered branch and a covered branch
	content := `mode: set
foo/logic.go:10.1,10.15 1 0
foo/logic.go:10.16,10.30 1 1
bar/logic.go:20.1,20.15 1 1
bar/logic.go:20.16,20.30 1 0
`
	prof, err := ParseGoCoverageString(content)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Both line 10 in foo and line 20 in bar should be LineCovered because at least one block was executed
	if s := prof.GetLineStatus("foo/logic.go", 9); s != LineCovered {
		t.Fatalf("expected foo line 10 to be LineCovered, got %v", s)
	}
	if s := prof.GetLineStatus("bar/logic.go", 19); s != LineCovered {
		t.Fatalf("expected bar line 20 to be LineCovered, got %v", s)
	}
}

func TestParseLCOV_Standard(t *testing.T) {
	lcovData := `TN:unit_tests
SF:/project/src/parser.ts
FN:1,parse
FNDA:5,parse
DA:1,5
DA:2,5
DA:3,0
DA:5,1
LF:4
LH:3
end_of_record
`
	prof, err := ParseLCOVString(lcovData)
	if err != nil {
		t.Fatalf("ParseLCOVString failed: %v", err)
	}

	fc := prof.FindFile("/project/src/parser.ts")
	if fc == nil {
		t.Fatalf("expected to find parser.ts")
	}

	// Check line 1 (lineIdx 0) -> LineCovered
	if s := fc.GetLineStatus(0); s != LineCovered {
		t.Errorf("line 1 expected LineCovered, got %v", s)
	}
	// Check line 2 (lineIdx 1) -> LineCovered
	if s := fc.GetLineStatus(1); s != LineCovered {
		t.Errorf("line 2 expected LineCovered, got %v", s)
	}
	// Check line 3 (lineIdx 2) -> LineUncovered
	if s := fc.GetLineStatus(2); s != LineUncovered {
		t.Errorf("line 3 expected LineUncovered, got %v", s)
	}
	// Check line 4 (lineIdx 3) -> LineNeutral
	if s := fc.GetLineStatus(3); s != LineNeutral {
		t.Errorf("line 4 expected LineNeutral, got %v", s)
	}
	// Check line 5 (lineIdx 4) -> LineCovered
	if s := fc.GetLineStatus(4); s != LineCovered {
		t.Errorf("line 5 expected LineCovered, got %v", s)
	}

	// Summary: 3 covered, 4 total instrumented lines = 75.0%
	cov, tot, pct := fc.Summary()
	if cov != 3 || tot != 4 {
		t.Fatalf("expected 3 covered, 4 total, got %d, %d", cov, tot)
	}
	if math.Abs(pct-75.0) > 0.001 {
		t.Fatalf("expected 75.0%%, got %.2f%%", pct)
	}
	if fc.FormattedPercent() != "75.0%" {
		t.Fatalf("expected '75.0%%', got %q", fc.FormattedPercent())
	}
}

func TestParseLCOV_MultipleFiles(t *testing.T) {
	lcovData := `SF:src/a.ts
DA:1,1
DA:2,0
end_of_record
SF:src/b.ts
DA:10,2
DA:11,3
end_of_record
`
	prof, err := ParseLCOVString(lcovData)
	if err != nil {
		t.Fatalf("ParseLCOVString failed: %v", err)
	}

	if len(prof.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(prof.Files))
	}

	cov, tot, pct := prof.Summary()
	// src/a.ts: 1 covered, 2 total
	// src/b.ts: 2 covered, 2 total
	// Workspace: 3 covered, 4 total = 75.0%
	if cov != 3 || tot != 4 {
		t.Fatalf("expected 3 covered, 4 total in workspace, got cov=%d, tot=%d", cov, tot)
	}
	if math.Abs(pct-75.0) > 0.001 {
		t.Fatalf("expected 75.0%%, got %.2f%%", pct)
	}
}

func TestParse_AutoDetect(t *testing.T) {
	goContent := `mode: count
pkg/test.go:1.1,2.1 1 5
`
	lcovContent := `SF:src/test.ts
DA:1,1
end_of_record
`

	profGo, err := ParseString(goContent)
	if err != nil || profGo.Mode != "count" {
		t.Fatalf("failed auto-detecting Go coverage: %v, mode=%s", err, profGo.Mode)
	}

	profLcov, err := ParseString(lcovContent)
	if err != nil || profLcov.Mode != "lcov" {
		t.Fatalf("failed auto-detecting LCOV: %v, mode=%s", err, profLcov.Mode)
	}

	// Empty string
	profEmpty, err := ParseString("")
	if err != nil {
		t.Fatalf("expected nil error for empty string, got %v", err)
	}
	c, tot, pct := profEmpty.Summary()
	if c != 0 || tot != 0 || pct != 0.0 {
		t.Fatalf("expected empty summary, got %d, %d, %f", c, tot, pct)
	}
}

func TestFilePathNormalization(t *testing.T) {
	content := `mode: set
github.com/baibeicha/tahr/internal/core/coverage/parser.go:10.1,12.1 2 1
`
	prof, err := ParseGoCoverageString(content)
	if err != nil {
		t.Fatalf("failed parsing: %v", err)
	}

	queries := []string{
		`github.com/baibeicha/tahr/internal/core/coverage/parser.go`,
		`internal/core/coverage/parser.go`,
		`D:\tahr\internal\core\coverage\parser.go`,
		`d:/tahr/internal/core/coverage/parser.go`,
		`parser.go`,
	}

	for _, q := range queries {
		fc := prof.FindFile(q)
		if fc == nil {
			t.Errorf("failed to resolve path %q", q)
			continue
		}
		status := prof.GetLineStatus(q, 9) // lineIdx 9 -> line 10
		if status != LineCovered {
			t.Errorf("expected LineCovered for path %q, got %v", q, status)
		}
	}

	// Nonexistent file
	if status := prof.GetLineStatus("nonexistent.go", 0); status != LineNeutral {
		t.Errorf("expected LineNeutral for nonexistent file, got %v", status)
	}
}

func TestSummaryCalculation_WorkspaceAndPerFile(t *testing.T) {
	// Construct a scenario where exactly 169 lines are covered out of 200 lines:
	// 169 / 200 = 84.5%
	prof := NewProfile()
	fc1 := prof.GetOrCreateFile("file1.go")
	for i := 1; i <= 100; i++ {
		if i <= 84 {
			fc1.RecordLine(i, 1) // 84 covered
		} else {
			fc1.RecordLine(i, 0) // 16 uncovered
		}
	}

	fc2 := prof.GetOrCreateFile("file2.go")
	for i := 1; i <= 100; i++ {
		if i <= 85 {
			fc2.RecordLine(i, 2) // 85 covered
		} else {
			fc2.RecordLine(i, 0) // 15 uncovered
		}
	}

	prof.Recalculate()

	// 84 + 85 = 169 covered out of 200 lines
	cov, tot, pct := prof.Summary()
	if cov != 169 {
		t.Errorf("expected 169 covered lines, got %d", cov)
	}
	if tot != 200 {
		t.Errorf("expected 200 total lines, got %d", tot)
	}
	if math.Abs(pct-84.5) > 0.0001 {
		t.Errorf("expected 84.5%%, got %.4f%%", pct)
	}
	if prof.FormattedPercent() != "84.5%" {
		t.Errorf("expected '84.5%%', got %q", prof.FormattedPercent())
	}

	// Per-file summary
	c1, t1, p1, ok1 := prof.FileSummary("file1.go")
	if !ok1 || c1 != 84 || t1 != 100 || math.Abs(p1-84.0) > 0.001 {
		t.Errorf("file1 summary mismatch: %d, %d, %.2f, %v", c1, t1, p1, ok1)
	}

	c2, t2, p2, ok2 := prof.FileSummary("file2.go")
	if !ok2 || c2 != 85 || t2 != 100 || math.Abs(p2-85.0) > 0.001 {
		t.Errorf("file2 summary mismatch: %d, %d, %.2f, %v", c2, t2, p2, ok2)
	}
}

func TestEngine_ActiveProfileAndEditorHelpers(t *testing.T) {
	Clear()

	content := `mode: set
main.go:1.1,3.1 2 1
main.go:5.1,6.1 1 0
`
	if err := LoadString(content); err != nil {
		t.Fatalf("LoadString failed: %v", err)
	}

	// Editor helper GetLineStatus(filePath, lineIdx)
	// Line 1 (lineIdx 0) -> LineCovered
	if status := GetLineStatus("main.go", 0); status != LineCovered {
		t.Errorf("expected LineCovered at lineIdx 0, got %v", status)
	}
	// Line 4 (lineIdx 3) -> LineNeutral
	if status := GetLineStatus("main.go", 3); status != LineNeutral {
		t.Errorf("expected LineNeutral at lineIdx 3, got %v", status)
	}
	// Line 5 (lineIdx 4) -> LineUncovered
	if status := GetLineStatus("main.go", 4); status != LineUncovered {
		t.Errorf("expected LineUncovered at lineIdx 4, got %v", status)
	}

	// Editor helper Summary()
	cov, tot, pct := Summary()
	if cov != 3 || tot != 5 {
		t.Fatalf("expected cov=3, tot=5, got %d, %d", cov, tot)
	}
	if math.Abs(pct-60.0) > 0.001 {
		t.Fatalf("expected 60.0%%, got %.2f%%", pct)
	}

	// Clear profile
	Clear()
	cov, tot, pct = Summary()
	if cov != 0 || tot != 0 || pct != 0.0 {
		t.Fatalf("expected cleared summary, got %d, %d, %f", cov, tot, pct)
	}
	if status := GetLineStatus("main.go", 0); status != LineNeutral {
		t.Fatalf("expected LineNeutral after Clear, got %v", status)
	}
}

func TestParseFile_OnDisk(t *testing.T) {
	tmpDir := t.TempDir()

	goPath := filepath.Join(tmpDir, "coverage.out")
	goContent := "mode: set\napp.go:10.1,12.1 2 1\n"
	if err := os.WriteFile(goPath, []byte(goContent), 0644); err != nil {
		t.Fatalf("failed writing temp file: %v", err)
	}

	profGo, err := ParseFile(goPath)
	if err != nil {
		t.Fatalf("ParseFile Go failed: %v", err)
	}
	if profGo.GetLineStatus("app.go", 9) != LineCovered {
		t.Fatalf("expected LineCovered for app.go:10")
	}

	lcovPath := filepath.Join(tmpDir, "lcov.info")
	lcovContent := "SF:app.ts\nDA:20,3\nend_of_record\n"
	if err := os.WriteFile(lcovPath, []byte(lcovContent), 0644); err != nil {
		t.Fatalf("failed writing temp lcov: %v", err)
	}

	profLCOV, err := ParseFile(lcovPath)
	if err != nil {
		t.Fatalf("ParseFile LCOV failed: %v", err)
	}
	if profLCOV.GetLineStatus("app.ts", 19) != LineCovered {
		t.Fatalf("expected LineCovered for app.ts:20")
	}
}

func TestConcurrentAccess(t *testing.T) {
	eng := NewEngine()
	content := `mode: set
concurrent.go:1.1,50.1 50 1
`
	if err := eng.LoadString(content); err != nil {
		t.Fatalf("LoadString failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = eng.GetLineStatus("concurrent.go", j)
				_, _, _ = eng.Summary()
			}
		}(i)
	}
	wg.Wait()
}

func TestEdgeCases(t *testing.T) {
	prof := NewProfile()

	// Negative lineIdx
	if s := prof.GetLineStatus("any.go", -1); s != LineNeutral {
		t.Errorf("expected LineNeutral for negative lineIdx, got %v", s)
	}

	// Empty filePath
	if s := prof.GetLineStatus("", 0); s != LineNeutral {
		t.Errorf("expected LineNeutral for empty filePath, got %v", s)
	}

	// Zero lines
	cov, tot, pct := prof.Summary()
	if cov != 0 || tot != 0 || pct != 0.0 {
		t.Errorf("expected zeros, got %d, %d, %f", cov, tot, pct)
	}

	// Malformed lines are skipped gracefully
	malformed := `mode: set
not a valid block line
missing_numbers.go:invalid
pkg/good.go:1.1,1.10 1 1
`
	parsed, err := ParseGoCoverageString(malformed)
	if err != nil {
		t.Fatalf("unexpected error parsing with malformed lines: %v", err)
	}
	if parsed.GetLineStatus("pkg/good.go", 0) != LineCovered {
		t.Fatalf("expected valid line to still be parsed correctly")
	}

	// LineStatus stringer test
	if LineCovered.String() != "covered" {
		t.Errorf("expected 'covered', got %q", LineCovered.String())
	}
	if LineUncovered.String() != "uncovered" {
		t.Errorf("expected 'uncovered', got %q", LineUncovered.String())
	}
	if LineNeutral.String() != "neutral" {
		t.Errorf("expected 'neutral', got %q", LineNeutral.String())
	}
}

func TestEngine_AdditionalHelpers(t *testing.T) {
	eng := NewEngine()
	if eng.GetProfile() == nil {
		t.Fatalf("expected non-nil initial profile")
	}

	// Test SetProfile and GetProfile
	p := NewProfile()
	fc := p.GetOrCreateFile("helper.go")
	fc.RecordLine(1, 10)
	fc.RecordLine(2, 0)
	p.Recalculate()

	eng.SetProfile(p)
	if eng.GetProfile() != p {
		t.Fatalf("expected profile to match")
	}

	// Test GetLineStatusByLine on engine and package
	if s := eng.GetLineStatusByLine("helper.go", 1); s != LineCovered {
		t.Errorf("expected LineCovered at line 1, got %v", s)
	}
	if s := eng.GetLineStatusByLine("helper.go", 2); s != LineUncovered {
		t.Errorf("expected LineUncovered at line 2, got %v", s)
	}
	if s := eng.GetLineStatusByLine("helper.go", 3); s != LineNeutral {
		t.Errorf("expected LineNeutral at line 3, got %v", s)
	}

	// Test FileSummary on engine
	cov, tot, pct, ok := eng.FileSummary("helper.go")
	if !ok || cov != 1 || tot != 2 || math.Abs(pct-50.0) > 0.001 {
		t.Errorf("FileSummary failed: cov=%d, tot=%d, pct=%.2f, ok=%v", cov, tot, pct, ok)
	}

	// Test FilePaths
	paths := p.FilePaths()
	if len(paths) != 1 || paths[0] != "helper.go" {
		t.Errorf("unexpected FilePaths: %v", paths)
	}

	// Test SetDefaultProfile, GetDefaultProfile, GetLineStatusByLine, FileSummary
	SetDefaultProfile(p)
	if GetDefaultProfile() != p {
		t.Fatalf("expected default profile to match")
	}
	if s := GetLineStatusByLine("helper.go", 1); s != LineCovered {
		t.Errorf("expected LineCovered from package helper, got %v", s)
	}
	c, to, pc, okPackage := FileSummary("helper.go")
	if !okPackage || c != 1 || to != 2 || math.Abs(pc-50.0) > 0.001 {
		t.Errorf("package FileSummary mismatch: %d, %d, %.2f, %v", c, to, pc, okPackage)
	}

	// Test FileSummary for nonexistent file
	if _, _, _, okNone := FileSummary("nonexistent.go"); okNone {
		t.Errorf("expected ok=false for nonexistent file")
	}
}

func TestLoadFileAndReader(t *testing.T) {
	tmpDir := t.TempDir()
	covFile := filepath.Join(tmpDir, "report.out")
	data := "mode: set\nservice.go:5.1,5.10 1 1\nservice.go:6.1,6.10 1 0\n"
	if err := os.WriteFile(covFile, []byte(data), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Test package-level LoadFile
	if err := LoadFile(covFile); err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if s := GetLineStatus("service.go", 4); s != LineCovered {
		t.Errorf("expected line 5 to be covered, got %v", s)
	}

	// Test Engine LoadFile
	eng := NewEngine()
	if err := eng.LoadFile(covFile); err != nil {
		t.Fatalf("engine LoadFile failed: %v", err)
	}
	if s := eng.GetLineStatus("service.go", 4); s != LineCovered {
		t.Errorf("engine: expected line 5 to be covered, got %v", s)
	}

	// Test non-existent file
	if err := eng.LoadFile(filepath.Join(tmpDir, "missing.out")); err == nil {
		t.Errorf("expected error for missing file")
	}
	if err := LoadFile(filepath.Join(tmpDir, "missing.out")); err == nil {
		t.Errorf("expected error for missing file in package helper")
	}
}

