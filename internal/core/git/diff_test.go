package git

import (
	"testing"
)

func TestParseGitDiffUnified(t *testing.T) {
	sampleDiff := `diff --git a/main.go b/main.go
index 1234567..89abcdef 100644
--- a/main.go
+++ b/main.go
@@ -5,0 +6,2 @@
+	// new line 1
+	// new line 2
@@ -15,1 +17,1 @@
-	oldFunc()
+	newFunc()
@@ -25,2 +26,0 @@
-	deleted1
-	deleted2
`

	diff := ParseGitDiffUnified(sampleDiff)
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}

	// Line 6 and 7 (1-based) -> 0-based 5, 6 are Added
	if diff.Lines[5] != DiffAdded {
		t.Errorf("expected line 5 to be DiffAdded, got %v", diff.Lines[5])
	}
	if diff.Lines[6] != DiffAdded {
		t.Errorf("expected line 6 to be DiffAdded, got %v", diff.Lines[6])
	}

	// Line 17 (1-based) -> 0-based 16 is Modified
	if diff.Lines[16] != DiffModified {
		t.Errorf("expected line 16 to be DiffModified, got %v", diff.Lines[16])
	}

	// Line 26 (1-based) -> 0-based 25 has deletion
	if diff.Lines[25] != DiffDeleted {
		t.Errorf("expected line 25 to be DiffDeleted, got %v", diff.Lines[25])
	}

	if diff.AddedCount != 2 {
		t.Errorf("expected 2 added, got %d", diff.AddedCount)
	}
	if diff.ModifiedCount != 1 {
		t.Errorf("expected 1 modified, got %d", diff.ModifiedCount)
	}
	if diff.DeletedCount != 2 {
		t.Errorf("expected 2 deleted, got %d", diff.DeletedCount)
	}
}

func TestTracker_EmptyOrNonGit(t *testing.T) {
	tracker := NewTracker()
	diff := tracker.GetFileDiff("")
	if diff == nil || len(diff.Lines) != 0 {
		t.Errorf("expected empty diff for empty path")
	}

	// Invalidate test
	tracker.Invalidate("test.go")
}

func TestParseGitStatusPorcelain(t *testing.T) {
	sample := ` M modified_worktree.go
M  modified_staged.go
?? untracked_file.txt
A  added_file.go
AM added_and_modified.go
 D deleted_worktree.go
D  deleted_staged.go
R  old_name.go -> new_name.go
?? "quoted path/with spaces.go"
`

	statuses := ParseGitStatusPorcelain(sample)
	if statuses == nil {
		t.Fatal("expected non-nil statuses")
	}

	cases := map[string]string{
		"modified_worktree.go":        "M",
		"modified_staged.go":          "M",
		"untracked_file.txt":          "U",
		"added_file.go":               "A",
		"added_and_modified.go":       "A",
		"deleted_worktree.go":         "D",
		"deleted_staged.go":           "D",
		"new_name.go":                 "M",
		"quoted path/with spaces.go": "U",
	}

	for path, expected := range cases {
		actual, ok := statuses[path]
		if !ok {
			t.Errorf("expected status for %q, but key not found", path)
		} else if actual != expected {
			t.Errorf("for %q: expected %q, got %q", path, expected, actual)
		}
	}

	// Empty input
	empty := ParseGitStatusPorcelain("")
	if len(empty) != 0 {
		t.Errorf("expected empty map for empty output, got %d entries", len(empty))
	}
}

func TestTracker_GetFileStatuses(t *testing.T) {
	tracker := NewTracker()

	// Non-git directory should safely return empty map without panicking
	res := tracker.GetFileStatuses(t.TempDir())
	if res == nil {
		t.Fatal("expected non-nil map from GetFileStatuses")
	}

	// Nil tracker safety
	var nilTracker *Tracker
	nilRes := nilTracker.GetFileStatuses(".")
	if nilRes == nil {
		t.Fatal("expected non-nil map from nilTracker.GetFileStatuses")
	}

	// Cache test
	tracker.Invalidate("any_file.go")
}

