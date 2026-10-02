package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchInFiles_Execution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-search-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	fileA := filepath.Join(tmpDir, "main.go")
	_ = os.WriteFile(fileA, []byte("package main\n\nfunc main() {\n\tprintln(\"Hello, Tahr Engine!\")\n}\n"), 0644)

	fileB := filepath.Join(tmpDir, "helper.go")
	_ = os.WriteFile(fileB, []byte("package main\n\n// Engine helper\nfunc InitEngine() {}\n"), 0644)

	modal := NewSearchInFilesModal()
	modal.OpenModal("Engine", tmpDir)

	if len(modal.Matches) != 3 {
		t.Fatalf("expected 3 matches for 'Engine', got %d: %+v", len(modal.Matches), modal.Matches)
	}

	// Test Case Sensitive
	modal.CaseSensitive = true
	modal.ExecuteSearch(tmpDir)
	if len(modal.Matches) != 3 {
		t.Errorf("expected 3 matches case-sensitive 'Engine', got %d", len(modal.Matches))
	}

	// Search for non-existent text
	modal.Query = "NonExistentSymbol123"
	modal.ExecuteSearch(tmpDir)
	if len(modal.Matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(modal.Matches))
	}
}
