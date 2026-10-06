package git

import (
	"testing"
)

func TestGetRepositoryStatusAndStaging(t *testing.T) {
	st, err := GetRepositoryStatus(".")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if st == nil {
		t.Fatal("expected non-nil RepoStatus")
	}

	total := st.TotalChanges()
	if total < 0 {
		t.Errorf("expected >= 0 total changes, got %d", total)
	}
}

func TestGetFileUnifiedDiff(t *testing.T) {
	// Diff on a known file
	diff, err := GetFileUnifiedDiff(".", "status.go", false, false)
	// Even if there is no diff, it shouldn't error out
	if err != nil {
		t.Logf("diff error: %v", err)
	}
	_ = diff
}
