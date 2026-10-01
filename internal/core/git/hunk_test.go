package git

import (
	"strings"
	"testing"
)

func TestGit_ParseDiffHunks(t *testing.T) {
	diffOutput := `diff --git a/main.go b/main.go
index 83a21b..94bc21 100644
--- a/main.go
+++ b/main.go
@@ -10,3 +10,5 @@ func main() {
 	fmt.Println("hello")
+	fmt.Println("world")
+	fmt.Println("tahr")
 }
@@ -25,4 +27,3 @@ func helper() {
-	oldCall()
+	newCall()
 }
`

	hunks := ParseDiffHunks(diffOutput)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	h0 := hunks[0]
	if h0.OldStart != 10 || h0.OldCount != 3 || h0.NewStart != 10 || h0.NewCount != 5 {
		t.Errorf("hunk 0 coords mismatch: %+v", h0)
	}
	if len(h0.Lines) != 4 {
		t.Errorf("expected 4 lines in hunk 0, got %d", len(h0.Lines))
	}

	summary := h0.Summary()
	if !strings.Contains(summary, "+2, -0") {
		t.Errorf("expected summary with (+2, -0), got %q", summary)
	}

	patch := GeneratePatchForHunk("main.go", h0)
	if !strings.Contains(patch, "--- a/main.go") || !strings.Contains(patch, "+++ b/main.go") {
		t.Errorf("invalid patch generated:\n%s", patch)
	}
}

func TestGit_ParseCommitGraphLines(t *testing.T) {
	graphOutput := `*   a1b2c3d||| (HEAD -> main, origin/main)|||Alice|||2 hours ago|||Merge pull request #42
|\  
| * e4f5g6h||| (feature/multi-term)|||Bob|||5 hours ago|||feat: add multi-terminal split
* | 7i8j9k0||| (tag: v1.0.0)|||Charlie|||1 day ago|||release: v1.0.0
`

	nodes := ParseCommitGraphLines(graphOutput)
	if len(nodes) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(nodes))
	}

	if nodes[0].Hash != "a1b2c3d" || nodes[0].Author != "Alice" || !strings.Contains(nodes[0].Refs, "HEAD -> main") {
		t.Errorf("unexpected node 0: %+v", nodes[0])
	}
	if nodes[2].Hash != "e4f5g6h" || nodes[2].Author != "Bob" {
		t.Errorf("unexpected node 2: %+v", nodes[2])
	}
}
