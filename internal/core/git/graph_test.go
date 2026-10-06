package git

import (
	"testing"
)

func TestConvertGraphToUnicode(t *testing.T) {
	ascii := "* | / \\ _"
	unicode := ConvertGraphToUnicode(ascii)
	expected := "● │ ╭ ╰ ─"
	if unicode != expected {
		t.Fatalf("expected %q, got %q", expected, unicode)
	}
}

func TestGetBranches(t *testing.T) {
	branches, err := GetBranches(".")
	if err != nil {
		t.Fatalf("GetBranches failed: %v", err)
	}
	if len(branches) == 0 {
		t.Fatal("expected at least one branch entry ('All branches')")
	}
	if branches[0].Name != "All branches" {
		t.Errorf("expected first branch to be 'All branches', got %s", branches[0].Name)
	}
}

func TestGetFilteredCommitGraph(t *testing.T) {
	nodes, err := GetFilteredCommitGraph(".", "", "", "", 10)
	if err != nil {
		t.Fatalf("GetFilteredCommitGraph failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Log("no commits found in repo or shallow")
	}
}

func TestGetCommitDetails(t *testing.T) {
	// Query HEAD commit details
	nodes, err := GetFilteredCommitGraph(".", "", "", "", 1)
	if err != nil || len(nodes) == 0 {
		return
	}
	hash := nodes[0].Hash
	if hash == "" {
		return
	}
	detail, err := GetCommitDetails(".", hash)
	if err != nil {
		t.Fatalf("GetCommitDetails failed: %v", err)
	}
	if detail == nil || detail.Hash == "" {
		t.Fatal("expected valid commit detail")
	}
	if len(detail.Files) == 0 {
		t.Errorf("expected commit %s to have changed files, got 0", hash)
	} else {
		// Test GetCommitFileDiff
		firstFile := detail.Files[0].Path
		diffLines, err := GetCommitFileDiff(".", hash, firstFile)
		if err != nil {
			t.Fatalf("GetCommitFileDiff failed for %s: %v", firstFile, err)
		}
		if len(diffLines) == 0 {
			t.Errorf("expected diff lines for %s, got 0", firstFile)
		}
	}
}

func TestGetCommitDAG(t *testing.T) {
	dag, err := GetCommitDAG(".", "", "", "", 10)
	if err != nil {
		t.Fatalf("GetCommitDAG failed: %v", err)
	}
	if len(dag) == 0 {
		t.Fatal("expected at least one commit in DAG")
	}
	// Verify Col is ordered
	for i, c := range dag {
		if c.Col != i {
			t.Errorf("expected commit %d to have Col %d, got %d", i, i, c.Col)
		}
	}
}

