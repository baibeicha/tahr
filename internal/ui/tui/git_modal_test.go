package tui

import (
	"testing"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/git"
	"tahr/internal/ui"
)

func TestGitModal_KeyIsolationAndTabSwitching(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.RepoStatus = &git.RepoStatus{
		Branch: "main",
		Staged: []git.FileItem{
			{Path: "main.go", Status: "M", Staged: true},
		},
		Unstaged: []git.FileItem{
			{Path: "app.go", Status: "M", Staged: false},
		},
	}

	// 1. Initial tab is 0
	if gm.CurrentTab != 0 {
		t.Fatalf("expected initial tab 0, got %d", gm.CurrentTab)
	}

	// 2. Press '2' -> switches to Tab 1
	handled := gm.HandleKey(input.Key{Type: input.KeyRune, Rune: '2'})
	if !handled || gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after pressing '2', got %d (handled=%v)", gm.CurrentTab, handled)
	}

	// 3. Press '1' -> switches to Tab 0
	handled = gm.HandleKey(input.Key{Type: input.KeyRune, Rune: '1'})
	if !handled || gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after pressing '1', got %d (handled=%v)", gm.CurrentTab, handled)
	}

	// 4. Tab key cycles
	gm.HandleKey(input.Key{Type: input.KeyTab})
	if gm.CurrentTab != 1 {
		t.Errorf("expected Tab 1 after Tab key, got %d", gm.CurrentTab)
	}
	gm.HandleKey(input.Key{Type: input.KeyTab})
	if gm.CurrentTab != 0 {
		t.Errorf("expected Tab 0 after second Tab key, got %d", gm.CurrentTab)
	}
}

func TestGitModal_Tab1_2DPanAndCommitNavigation(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.CommitGraph = []git.CommitNode{
		{Hash: "c0ffee1", GraphPrefix: "●", Refs: "HEAD -> main", Message: "feat: add metro graph", Author: "Dev", Date: "now"},
		{Hash: "c0ffee2", GraphPrefix: "● │", Refs: "", Message: "fix: align unicode rails", Author: "Dev", Date: "1h ago"},
		{Hash: "c0ffee3", GraphPrefix: "● ╰─╮", Refs: "dev", Message: "merge: dev branch", Author: "Dev", Date: "2h ago"},
	}
	gm.SelectedCommit = 0
	gm.GraphScrollX = 0
	gm.GraphScrollY = 0

	// 1. Horizontal 2D Pan with Right arrow and 'l'
	gm.HandleKey(input.Key{Type: input.KeyRight})
	if gm.GraphScrollX != 6 {
		t.Errorf("expected GraphScrollX 6 after Right key, got %d", gm.GraphScrollX)
	}
	gm.HandleKey(input.Key{Type: input.KeyRune, Rune: 'l'})
	if gm.GraphScrollX != 12 {
		t.Errorf("expected GraphScrollX 12 after 'l', got %d", gm.GraphScrollX)
	}

	// 2. Horizontal 2D Pan with Left arrow and 'h'
	gm.HandleKey(input.Key{Type: input.KeyLeft})
	if gm.GraphScrollX != 6 {
		t.Errorf("expected GraphScrollX 6 after Left key, got %d", gm.GraphScrollX)
	}
	gm.HandleKey(input.Key{Type: input.KeyRune, Rune: 'h'})
	if gm.GraphScrollX != 0 {
		t.Errorf("expected GraphScrollX 0 after 'h', got %d", gm.GraphScrollX)
	}

	// 3. Vertical navigation with Down arrow and 'j'
	gm.HandleKey(input.Key{Type: input.KeyDown})
	if gm.SelectedCommit != 1 {
		t.Errorf("expected SelectedCommit 1 after Down key, got %d", gm.SelectedCommit)
	}
	gm.HandleKey(input.Key{Type: input.KeyRune, Rune: 'j'})
	if gm.SelectedCommit != 2 {
		t.Errorf("expected SelectedCommit 2 after 'j', got %d", gm.SelectedCommit)
	}

	// 4. Up navigation
	gm.HandleKey(input.Key{Type: input.KeyUp})
	if gm.SelectedCommit != 1 {
		t.Errorf("expected SelectedCommit 1 after Up key, got %d", gm.SelectedCommit)
	}
}

func TestGitModal_AnimatedDetailsCard(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.CommitGraph = []git.CommitNode{
		{Hash: "c0ffee1", GraphPrefix: "●", Message: "Initial commit", Author: "Tahr", Date: "now"},
	}
	gm.SelectedCommit = 0

	// Trigger commit detail card opening
	gm.DetailCommit = &git.CommitDetail{
		Hash:        "c0ffee111222333",
		AbbrevHash:  "c0ffee1",
		AuthorName:  "Tahr Dev",
		AuthorEmail: "dev@tahr.internal",
		Subject:     "feat: interactive git graph",
	}
	gm.DetailOpen = true
	gm.AnimProgress = 0.0

	// Step animation
	animating := gm.StepAnimation()
	if !animating || gm.AnimProgress != 0.25 {
		t.Fatalf("expected AnimProgress 0.25, got %f (animating=%v)", gm.AnimProgress, animating)
	}

	// Step remaining
	for gm.AnimProgress < 1.0 {
		gm.StepAnimation()
	}
	if gm.AnimProgress != 1.0 {
		t.Errorf("expected AnimProgress 1.0, got %f", gm.AnimProgress)
	}

	// Esc closes details popup
	handled := gm.HandleKey(input.Key{Type: input.KeyEsc})
	if !handled || gm.DetailOpen {
		t.Errorf("expected Esc to close DetailOpen, got DetailOpen=%v", gm.DetailOpen)
	}
}

func TestGitModal_MouseWheelAndPan(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.GraphScrollX = 0
	gm.GraphScrollY = 0

	screenW, screenH := 100, 30
	// Wheel in top graph area: pan X and Y
	gm.HandleWheel(50, 8, 1, 4, screenW, screenH)
	if gm.GraphScrollY != 1 || gm.GraphScrollX != 4 {
		t.Errorf("expected GraphScrollY=1 and GraphScrollX=4, got Y=%d X=%d", gm.GraphScrollY, gm.GraphScrollX)
	}
}

func TestGitModal_MouseDragPan(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.GraphScrollX = 20
	gm.GraphScrollY = 10

	screenW, screenH := 120, 40

	// 1. Mouse press in top DAG pane starts dragging
	handled := gm.HandleClick(50, 10, screenW, screenH)
	if !handled || !gm.IsDragging {
		t.Fatalf("expected IsDragging=true after clicking top pane, got %v", gm.IsDragging)
	}

	// 2. Drag to the left (screenX 50 -> 40, deltaX = -10): canvas pans to the right by 10 (GraphScrollX = 30)
	dragged := gm.HandleDrag(40, 15, screenW, screenH)
	if !dragged || gm.GraphScrollX != 30 || gm.GraphScrollY != 5 {
		t.Errorf("expected GraphScrollX=30 and GraphScrollY=5 after drag, got X=%d Y=%d", gm.GraphScrollX, gm.GraphScrollY)
	}

	// 3. Release mouse stops dragging
	gm.HandleRelease()
	if gm.IsDragging {
		t.Errorf("expected IsDragging=false after release")
	}

	// 4. Further drag when not dragging does nothing
	dragged = gm.HandleDrag(30, 20, screenW, screenH)
	if dragged {
		t.Errorf("expected HandleDrag to return false when not dragging")
	}
}

func TestGitModal_Render(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	buf := buffer.NewBuffer(120, 40)
	th := ui.CatppuccinMocha()

	// Render Tab 0
	gm.CurrentTab = 0
	gm.Render(buf, 120, 40, &th)

	// Render Tab 1
	gm.CurrentTab = 1
	gm.Render(buf, 120, 40, &th)

	// Render with Detail card open
	gm.DetailOpen = true
	gm.AnimProgress = 1.0
	gm.DetailCommit = &git.CommitDetail{
		Hash:       "deadbeef12345678",
		AbbrevHash: "deadbee",
		Subject:    "test commit",
	}
	gm.Render(buf, 120, 40, &th)

	if !gm.Open {
		t.Errorf("expected modal to remain open")
	}
}

func TestGitModal_DiffColorHighlighting(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 0
	gm.DiffContent = "--- a/file.go\n+++ b/file.go\n@@ -1,2 +1,2 @@\n-oldLine\n+newLine\n contextLine"

	buf := buffer.NewBuffer(120, 40)
	th := ui.CatppuccinMocha()
	gm.Render(buf, 120, 40, &th)
}

func TestGitModal_2DDAGDiagramRendering(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.DAGCommits = []git.DAGCommit{
		{Hash: "1111111", AbbrevHash: "1111111", Col: 0, Lane: 0, Branch: "main", Author: "alice", Date: "yesterday"},
		{Hash: "2222222", AbbrevHash: "2222222", Col: 1, Lane: 1, Branch: "feat", Parents: []string{"1111111"}, Author: "bob", Date: "today"},
		{Hash: "3333333", AbbrevHash: "3333333", Col: 2, Lane: 0, Branch: "main", Parents: []string{"1111111", "2222222"}, Author: "charlie", Date: "now"},
	}
	gm.CommitGraph = []git.CommitNode{
		{Hash: "3333333", Message: "merge feat", Author: "charlie", Date: "now"},
		{Hash: "2222222", Message: "feat work", Author: "bob", Date: "today"},
		{Hash: "1111111", Message: "initial", Author: "alice", Date: "yesterday"},
	}

	buf := buffer.NewBuffer(120, 40)
	th := ui.CatppuccinMocha()
	gm.Render(buf, 120, 40, &th)

	// Click on DAG card (Col 1, Lane 1)
	modalW := min(120, 120-4)
	modalH := min(40, 40-4)
	startX := (120 - modalW) / 2
	startY := (40 - modalH) / 2
	topY := startY + 3
	cardX := startX + 3 + 1*26 - gm.GraphScrollX
	cardY := topY + 1 + 1*5 - gm.GraphScrollY
	handled := gm.HandleClick(cardX+2, cardY+1, 120, 40)
	if !handled {
		t.Errorf("expected click on DAG card to be handled")
	}
}

func TestGitModal_CommitDetails_FilesAndDiff(t *testing.T) {
	gm := NewGitModal(".")
	gm.Open = true
	gm.CurrentTab = 1
	gm.DetailOpen = true
	gm.AnimProgress = 1.0
	gm.DetailCommit = &git.CommitDetail{
		Hash:        "ffbc9a2824639ac9ab00cbbe20d2b570c68e3569",
		AbbrevHash:  "ffbc9a2",
		AuthorName:  "baibeicha",
		AuthorEmail: "myvvot@gmail.com",
		Subject:     "feat(ai): test commit",
		Files: []git.CommitFileStat{
			{Path: "cmd/tahr/app_tui.go", Additions: 1, Deletions: 0},
			{Path: ".gitignore", Additions: 6, Deletions: 0},
		},
	}

	th := ui.CatppuccinMocha()
	buf := buffer.NewBuffer(120, 40)
	gm.Render(buf, 120, 40, &th)

	// Test navigation in commit details
	gm.HandleKey(input.Key{Type: input.KeyDown})
	if gm.DetailSelectedFile != 1 {
		t.Errorf("expected DetailSelectedFile to be 1, got %d", gm.DetailSelectedFile)
	}

	// Test pressing Enter to open file diff
	gm.HandleKey(input.Key{Type: input.KeyEnter})
	if !gm.DetailDiffOpen {
		t.Errorf("expected DetailDiffOpen to be true after Enter")
	}
	if gm.DetailDiffFile != ".gitignore" {
		t.Errorf("expected DetailDiffFile to be .gitignore, got %s", gm.DetailDiffFile)
	}

	// Render diff popup
	gm.Render(buf, 120, 40, &th)

	// Test Esc to close diff (returns to details)
	gm.HandleKey(input.Key{Type: input.KeyEsc})
	if gm.DetailDiffOpen {
		t.Errorf("expected DetailDiffOpen to be false after Esc")
	}
	if !gm.DetailOpen {
		t.Errorf("expected DetailOpen to remain true after closing diff")
	}

	// Test Esc again to close details popup
	gm.HandleKey(input.Key{Type: input.KeyEsc})
	if gm.DetailOpen {
		t.Errorf("expected DetailOpen to be false after second Esc")
	}
}

