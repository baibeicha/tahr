package todotree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanSource_VariousCommentStyles(t *testing.T) {
	src := `
package main

// TODO: implement graceful shutdown
func main() {
	/* FIXME: avoid global variable */
	x := 10
	# BUG(alice): division by zero potential
	// NOTE: keep this for backward compatibility
	/* HACK(bob): workaround for upstream issue #42 */
	// PERF: optimize allocation in loop
	// XXX: clean this up before release
	// OPTIMIZE: vector operations
	<!-- NOTE: html style inside template -->
	-- HACK: sql style comment
}
`

	scanner := NewScanner()
	items := scanner.ScanSource([]byte(src), "main.go")

	if len(items) != 10 {
		t.Fatalf("Expected 10 TODO items, got %d", len(items))
	}

	expected := []struct {
		tag    string
		author string
		msg    string
		line   int
	}{
		{"TODO", "", "implement graceful shutdown", 4},
		{"FIXME", "", "avoid global variable", 6},
		{"BUG", "alice", "division by zero potential", 8},
		{"NOTE", "", "keep this for backward compatibility", 9},
		{"HACK", "bob", "workaround for upstream issue #42", 10},
		{"PERF", "", "optimize allocation in loop", 11},
		{"XXX", "", "clean this up before release", 12},
		{"OPTIMIZE", "", "vector operations", 13},
		{"NOTE", "", "html style inside template", 14},
		{"HACK", "", "sql style comment", 15},
	}

	for i, exp := range expected {
		it := items[i]
		if it.Tag != exp.tag {
			t.Errorf("[%d] expected tag %q, got %q", i, exp.tag, it.Tag)
		}
		if it.Author != exp.author {
			t.Errorf("[%d] expected author %q, got %q", i, exp.author, it.Author)
		}
		if it.Message != exp.msg {
			t.Errorf("[%d] expected message %q, got %q", i, exp.msg, it.Message)
		}
		if it.Line != exp.line {
			t.Errorf("[%d] expected line %d, got %d", i, exp.line, it.Line)
		}
		if it.FilePath != "main.go" {
			t.Errorf("[%d] expected filepath main.go, got %q", i, it.FilePath)
		}
	}
}

func TestScanDirectory_ExclusionsAndBinarySkipping(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Regular file
	file1 := filepath.Join(tmpDir, "service.go")
	if err := os.WriteFile(file1, []byte("// TODO: service logic\n// FIXME: service err"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Another nested file
	subDir := filepath.Join(tmpDir, "pkg", "handler")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	file2 := filepath.Join(subDir, "handler.go")
	if err := os.WriteFile(file2, []byte("// BUG(charlie): bad status code"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Ignored directory (.git)
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("// TODO: should be ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	// 4. Ignored directory (node_modules)
	nodeDir := filepath.Join(tmpDir, "node_modules", "pkg")
	if err := os.MkdirAll(nodeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeDir, "index.js"), []byte("// TODO: ignore node_modules"), 0644); err != nil {
		t.Fatal(err)
	}

	// 5. Binary file with null bytes
	binFile := filepath.Join(tmpDir, "data.bin")
	binData := []byte{0x00, 0x01, 0x02, 0x00}
	binData = append(binData, []byte("// TODO: binary should be ignored")...)
	if err := os.WriteFile(binFile, binData, 0644); err != nil {
		t.Fatal(err)
	}

	scanner := NewScanner()
	ctx := context.Background()
	items, err := scanner.ScanDirectory(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Should only find the 3 items in service.go and pkg/handler/handler.go
	if len(items) != 3 {
		t.Fatalf("Expected 3 items from workspace, got %d: %+v", len(items), items)
	}

	// Verify sorting: service.go then pkg/handler/handler.go
	var tagsFound []string
	for _, it := range items {
		tagsFound = append(tagsFound, it.Tag)
	}
	if tagsFound[0] != "BUG" && tagsFound[2] != "BUG" {
		t.Errorf("Unexpected tags found order: %v", tagsFound)
	}
}

func TestGroupingAndStats(t *testing.T) {
	items := []TodoItem{
		{Tag: "TODO", Message: "task 1", FilePath: "a.go", Line: 10, Priority: PriorityNormal},
		{Tag: "FIXME", Message: "fix 1", FilePath: "a.go", Line: 20, Priority: PriorityHigh},
		{Tag: "BUG", Message: "bug 1", FilePath: "b.go", Line: 5, Priority: PriorityCritical},
		{Tag: "TODO", Message: "task 2", FilePath: "b.go", Line: 15, Priority: PriorityNormal},
	}

	// Stats
	stats := CalculateStats(items)
	if stats.TotalCount != 4 {
		t.Errorf("Expected TotalCount 4, got %d", stats.TotalCount)
	}
	if stats.CountByTag["TODO"] != 2 || stats.CountByTag["FIXME"] != 1 || stats.CountByTag["BUG"] != 1 {
		t.Errorf("Unexpected CountByTag: %+v", stats.CountByTag)
	}
	if stats.CountByPriority[PriorityCritical] != 1 || stats.CountByPriority[PriorityNormal] != 2 {
		t.Errorf("Unexpected CountByPriority: %+v", stats.CountByPriority)
	}

	// Group by file
	byFile := GroupByFile(items)
	if len(byFile) != 2 {
		t.Fatalf("Expected 2 file groups, got %d", len(byFile))
	}
	if byFile[0].FilePath != "a.go" || len(byFile[0].Items) != 2 {
		t.Errorf("Unexpected file group 0: %+v", byFile[0])
	}
	if byFile[1].FilePath != "b.go" || len(byFile[1].Items) != 2 {
		t.Errorf("Unexpected file group 1: %+v", byFile[1])
	}

	// Group by tag (sorted by priority descending: BUG first, then FIXME, then TODO)
	byTag := GroupByTag(items)
	if len(byTag) != 3 {
		t.Fatalf("Expected 3 tag groups, got %d", len(byTag))
	}
	if byTag[0].Tag != "BUG" {
		t.Errorf("Expected first tag group to be BUG (Critical), got %s", byTag[0].Tag)
	}
	if byTag[1].Tag != "FIXME" {
		t.Errorf("Expected second tag group to be FIXME (High), got %s", byTag[1].Tag)
	}
	if byTag[2].Tag != "TODO" {
		t.Errorf("Expected third tag group to be TODO (Normal), got %s", byTag[2].Tag)
	}
}

func TestFilter(t *testing.T) {
	items := []TodoItem{
		{Tag: "TODO", Author: "alice", Message: "refactor parser", FilePath: "parser.go", Line: 1, Priority: PriorityNormal},
		{Tag: "FIXME", Author: "bob", Message: "fix race condition in queue", FilePath: "queue.go", Line: 5, Priority: PriorityHigh},
		{Tag: "BUG", Author: "alice", Message: "memory leak in cache", FilePath: "cache.go", Line: 10, Priority: PriorityCritical},
	}

	// Filter by query
	qRes := Filter(items, FilterOptions{Query: "race"})
	if len(qRes) != 1 || qRes[0].Author != "bob" {
		t.Errorf("Query filter failed: %+v", qRes)
	}

	// Filter by tag
	tagRes := Filter(items, FilterOptions{Tags: []string{"BUG"}})
	if len(tagRes) != 1 || tagRes[0].Tag != "BUG" {
		t.Errorf("Tag filter failed: %+v", tagRes)
	}

	// Filter by author
	authRes := Filter(items, FilterOptions{Author: "alice"})
	if len(authRes) != 2 {
		t.Errorf("Author filter failed, expected 2, got %d", len(authRes))
	}

	// Filter by minimum priority (High or Critical)
	priRes := Filter(items, FilterOptions{MinPriority: PriorityHigh})
	if len(priRes) != 2 {
		t.Errorf("Priority filter failed, expected 2, got %d", len(priRes))
	}
}

func TestTreeBuilders(t *testing.T) {
	items := []TodoItem{
		{Tag: "TODO", Author: "john", Message: "do this", FilePath: "a.go", Line: 10, RawLine: "// TODO(john): do this"},
		{Tag: "BUG", Message: "crash here", FilePath: "b.go", Line: 20, RawLine: "// BUG: crash here"},
	}

	fileTree := BuildFileTree(items)
	if len(fileTree) != 2 {
		t.Fatalf("BuildFileTree expected 2 root nodes, got %d", len(fileTree))
	}
	if len(fileTree[0].Children) != 1 || fileTree[0].Children[0].Item == nil {
		t.Errorf("BuildFileTree child missing or nil item")
	}

	tagTree := BuildTagTree(items)
	if len(tagTree) != 2 {
		t.Fatalf("BuildTagTree expected 2 root nodes, got %d", len(tagTree))
	}
}
