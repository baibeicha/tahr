package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/git"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// GitModal provides an interactive Git interface with repository status,
// staging controls, a 2D DAG canvas diagram with arrows and bends, 2D panning, and animated commit detail cards.
type GitModal struct {
	Open         bool
	WorkspaceDir string
	ActiveFile   string
	CurrentTab   int // 0: Interactive Commit / Repo Status, 1: Branch Graph

	// Tab 0: Interactive Commit
	RepoStatus       *git.RepoStatus
	SelectedCategory int // 0: Staged, 1: Unstaged, 2: Untracked
	SelectedIndex    int // index within current category
	DiffContent      string
	DiffScrollY      int
	DiffScrollX      int
	IsCommitting     bool
	CommitMessage    string
	PromptMsg        string
	CommitError      string

	// Tab 1: Visual Branch Graph & Commit History
	Branches            []git.BranchInfo
	SelectedBranchIndex int
	Periods             []string
	SelectedPeriodIndex int
	SearchQuery         string
	SearchActive        bool
	CommitGraph         []git.CommitNode
	DAGCommits          []git.DAGCommit
	DAGCentered         bool
	SelectedCommit      int // Index in CommitGraph table
	GraphScrollX        int // 2D PAN: horizontal viewport offset
	GraphScrollY        int // 2D PAN: vertical viewport offset
	HistoryScrollY      int // Scroll in bottom commit table

	// Commit Details Card (Animated Popup)
	DetailOpen         bool
	DetailCommit       *git.CommitDetail
	DetailScrollY      int
	DetailSelectedFile int
	AnimProgress       float64 // 0.0 to 1.0

	// Commit File Diff Viewer inside Details Card
	DetailDiffOpen    bool
	DetailDiffFile    string
	DetailDiffLines   []string
	DetailDiffScrollY int
	DetailDiffScrollX int

	// UI State
	IsDragging          bool
	DragStartX          int
	DragStartY          int
	DragInitialScrollX  int
	DragInitialScrollY  int
	StatusMsg           string
	BorderRounded bool
}

// NewGitModal creates an initialized instance of GitModal.
func NewGitModal(workspaceDir string) *GitModal {
	return &GitModal{
		Open:                false,
		WorkspaceDir:        workspaceDir,
		CurrentTab:          0,
		Periods:             []string{"All time", "Today", "Last 7 days", "Last 30 days"},
		SelectedPeriodIndex: 0,
		SelectedBranchIndex: 0,
		BorderRounded:       true,
		CommitGraph:         make([]git.CommitNode, 0),
		DAGCommits:          make([]git.DAGCommit, 0),
	}
}

// OpenForFile opens the modal for the given workspace and active file.
func (gm *GitModal) OpenForFile(dir, filePath string) {
	gm.Open = true
	gm.WorkspaceDir = dir
	gm.ActiveFile = filePath
	gm.IsCommitting = false
	gm.PromptMsg = ""
	gm.CommitError = ""
	gm.DetailOpen = false
	gm.AnimProgress = 0.0
	gm.SearchActive = false
	gm.GraphScrollX = 0
	gm.GraphScrollY = 0
	gm.HistoryScrollY = 0
	gm.DiffScrollY = 0
	gm.DiffScrollX = 0
	gm.DAGCentered = false

	gm.Refresh()

	// If ActiveFile is specified and has modifications, select it in the file list
	if filePath != "" && gm.RepoStatus != nil {
		cleanTarget := filepath.Clean(filePath)
		relTarget, err := filepath.Rel(dir, cleanTarget)
		if err == nil {
			gm.selectFileByPath(relTarget)
		}
	}
}

// StepAnimation advances the animated commit details popup card.
func (gm *GitModal) StepAnimation() bool {
	if !gm.Open {
		return false
	}
	if gm.DetailOpen && gm.AnimProgress < 1.0 {
		gm.AnimProgress += 0.25
		if gm.AnimProgress > 1.0 {
			gm.AnimProgress = 1.0
		}
		return true
	}
	return false
}

// Refresh reloads workspace status, diff, branches, commit graph, and the 2D DAG.
func (gm *GitModal) Refresh() {
	// 1. Refresh Repo Status for Tab 0
	st, err := git.GetRepositoryStatus(gm.WorkspaceDir)
	if err == nil && st != nil {
		gm.RepoStatus = st
	} else {
		gm.RepoStatus = &git.RepoStatus{
			Branch:    "main",
			Staged:    make([]git.FileItem, 0),
			Unstaged:  make([]git.FileItem, 0),
			Untracked: make([]git.FileItem, 0),
		}
	}
	gm.clampSelection()
	gm.updateSelectedDiff()

	// 2. Refresh Branches & Commit Graph for Tab 1
	branches, err := git.GetBranches(gm.WorkspaceDir)
	if err == nil && len(branches) > 0 {
		gm.Branches = branches
	} else {
		gm.Branches = []git.BranchInfo{
			{Name: "All branches", IsCurrent: false},
			{Name: "main", IsCurrent: true},
		}
	}

	selectedBranch := ""
	if gm.SelectedBranchIndex >= 0 && gm.SelectedBranchIndex < len(gm.Branches) {
		selectedBranch = gm.Branches[gm.SelectedBranchIndex].Name
	}
	selectedPeriod := ""
	if gm.SelectedPeriodIndex >= 0 && gm.SelectedPeriodIndex < len(gm.Periods) {
		selectedPeriod = gm.Periods[gm.SelectedPeriodIndex]
	}

	nodes, err := git.GetFilteredCommitGraph(gm.WorkspaceDir, selectedBranch, selectedPeriod, gm.SearchQuery, 100)
	if err == nil && len(nodes) > 0 {
		gm.CommitGraph = nodes
	} else {
		gm.CommitGraph = []git.CommitNode{
			{Hash: "a1b2c3d", GraphPrefix: "●", Refs: "HEAD -> main, origin/main", Author: "Tahr Core", Date: "just now", Message: "feat: multi-selection rope & async lsp"},
			{Hash: "f4e5d6c", GraphPrefix: "●", Refs: "v0.1.0", Author: "Tahr Core", Date: "1 day ago", Message: "release: initial headless core release"},
		}
	}

	// 3. Refresh 2D DAG layout
	dag, err := git.GetCommitDAG(gm.WorkspaceDir, selectedBranch, selectedPeriod, gm.SearchQuery, 100)
	if err == nil && len(dag) > 0 {
		gm.DAGCommits = dag
	} else {
		gm.DAGCommits = []git.DAGCommit{
			{Hash: "f4e5d6c", AbbrevHash: "f4e5d6c", Author: "Tahr Core", Date: "1 day ago", Subject: "release: initial headless core release", Branch: "main", Lane: 0, Col: 0},
			{Hash: "a1b2c3d", AbbrevHash: "a1b2c3d", Author: "Tahr Core", Date: "just now", Subject: "feat: multi-selection rope & async lsp", Branch: "main", Lane: 0, Col: 1},
		}
	}

	if gm.SelectedCommit >= len(gm.CommitGraph) {
		gm.SelectedCommit = max(0, len(gm.CommitGraph)-1)
	}

	// Auto-center DAG camera on HEAD (the newest commit on the right)
	if !gm.DAGCentered && len(gm.DAGCommits) > 0 {
		gm.centerOnHEAD()
		gm.DAGCentered = true
	}
}

func (gm *GitModal) centerOnHEAD() {
	if len(gm.DAGCommits) == 0 {
		return
	}
	headCol := len(gm.DAGCommits) - 1
	headX := headCol * 26
	gm.GraphScrollX = max(0, headX-40)
	gm.GraphScrollY = 0
	gm.SelectedCommit = 0
}

func (gm *GitModal) findDAGNodeByHash(hash string) *git.DAGCommit {
	clean := strings.TrimSpace(hash)
	if clean == "" {
		return nil
	}
	for i := range gm.DAGCommits {
		c := &gm.DAGCommits[i]
		if strings.HasPrefix(c.Hash, clean) || strings.HasPrefix(clean, c.AbbrevHash) {
			return c
		}
	}
	return nil
}

func (gm *GitModal) findTableIndexByHash(hash string) int {
	clean := strings.TrimSpace(hash)
	if clean == "" {
		return -1
	}
	for i := range gm.CommitGraph {
		node := &gm.CommitGraph[i]
		if strings.HasPrefix(node.Hash, clean) || strings.HasPrefix(clean, node.Hash) {
			return i
		}
	}
	return -1
}

// selectFileByPath selects a file matching the given path across staged, unstaged, and untracked lists.
func (gm *GitModal) selectFileByPath(relPath string) {
	if gm.RepoStatus == nil {
		return
	}
	clean := filepath.Clean(relPath)
	for i, f := range gm.RepoStatus.Staged {
		if filepath.Clean(f.Path) == clean {
			gm.SelectedCategory = 0
			gm.SelectedIndex = i
			gm.updateSelectedDiff()
			return
		}
	}
	for i, f := range gm.RepoStatus.Unstaged {
		if filepath.Clean(f.Path) == clean {
			gm.SelectedCategory = 1
			gm.SelectedIndex = i
			gm.updateSelectedDiff()
			return
		}
	}
	for i, f := range gm.RepoStatus.Untracked {
		if filepath.Clean(f.Path) == clean {
			gm.SelectedCategory = 2
			gm.SelectedIndex = i
			gm.updateSelectedDiff()
			return
		}
	}
}

func (gm *GitModal) clampSelection() {
	if gm.RepoStatus == nil {
		gm.SelectedCategory = 0
		gm.SelectedIndex = 0
		return
	}

	// Find first non-empty category if current is empty
	counts := []int{len(gm.RepoStatus.Staged), len(gm.RepoStatus.Unstaged), len(gm.RepoStatus.Untracked)}
	if counts[gm.SelectedCategory] == 0 {
		for i, c := range counts {
			if c > 0 {
				gm.SelectedCategory = i
				gm.SelectedIndex = 0
				break
			}
		}
	}

	if counts[gm.SelectedCategory] > 0 {
		if gm.SelectedIndex >= counts[gm.SelectedCategory] {
			gm.SelectedIndex = counts[gm.SelectedCategory] - 1
		}
		if gm.SelectedIndex < 0 {
			gm.SelectedIndex = 0
		}
	} else {
		gm.SelectedIndex = 0
	}
}

func (gm *GitModal) getSelectedItem() *git.FileItem {
	if gm.RepoStatus == nil {
		return nil
	}
	switch gm.SelectedCategory {
	case 0:
		if gm.SelectedIndex >= 0 && gm.SelectedIndex < len(gm.RepoStatus.Staged) {
			return &gm.RepoStatus.Staged[gm.SelectedIndex]
		}
	case 1:
		if gm.SelectedIndex >= 0 && gm.SelectedIndex < len(gm.RepoStatus.Unstaged) {
			return &gm.RepoStatus.Unstaged[gm.SelectedIndex]
		}
	case 2:
		if gm.SelectedIndex >= 0 && gm.SelectedIndex < len(gm.RepoStatus.Untracked) {
			return &gm.RepoStatus.Untracked[gm.SelectedIndex]
		}
	}
	return nil
}

func (gm *GitModal) updateSelectedDiff() {
	item := gm.getSelectedItem()
	if item == nil {
		gm.DiffContent = ""
		return
	}
	diff, err := git.GetFileUnifiedDiff(gm.WorkspaceDir, item.Path, item.Staged, item.Status == "?")
	if err == nil {
		gm.DiffContent = diff
	} else {
		gm.DiffContent = fmt.Sprintf("Error reading diff: %v", err)
	}
	gm.DiffScrollY = 0
	gm.DiffScrollX = 0
}

func (gm *GitModal) moveSelection(delta int) {
	if gm.RepoStatus == nil {
		return
	}
	counts := []int{len(gm.RepoStatus.Staged), len(gm.RepoStatus.Unstaged), len(gm.RepoStatus.Untracked)}
	if counts[0]+counts[1]+counts[2] == 0 {
		return
	}

	curCat := gm.SelectedCategory
	curIdx := gm.SelectedIndex + delta

	if delta > 0 {
		if curIdx < counts[curCat] {
			gm.SelectedIndex = curIdx
		} else {
			// Move to next non-empty category
			for nextCat := curCat + 1; nextCat < 3; nextCat++ {
				if counts[nextCat] > 0 {
					gm.SelectedCategory = nextCat
					gm.SelectedIndex = 0
					break
				}
			}
		}
	} else if delta < 0 {
		if curIdx >= 0 {
			gm.SelectedIndex = curIdx
		} else {
			// Move to previous non-empty category
			for prevCat := curCat - 1; prevCat >= 0; prevCat-- {
				if counts[prevCat] > 0 {
					gm.SelectedCategory = prevCat
					gm.SelectedIndex = counts[prevCat] - 1
					break
				}
			}
		}
	}
	gm.updateSelectedDiff()
}

// syncScrollToSelected keeps the selected commit in view in both top DAG diagram and bottom table.
func (gm *GitModal) syncScrollToSelected() {
	if gm.SelectedCommit >= 0 && gm.SelectedCommit < len(gm.CommitGraph) {
		selectedHash := gm.CommitGraph[gm.SelectedCommit].Hash
		dagNode := gm.findDAGNodeByHash(selectedHash)
		if dagNode != nil {
			cardX := dagNode.Col * 26
			if cardX < gm.GraphScrollX+6 {
				gm.GraphScrollX = max(0, cardX-10)
			} else if cardX+22 > gm.GraphScrollX+70 {
				gm.GraphScrollX = max(0, cardX-45)
			}
			cardY := dagNode.Lane * 5
			if cardY < gm.GraphScrollY {
				gm.GraphScrollY = cardY
			} else if cardY+4 > gm.GraphScrollY+12 {
				gm.GraphScrollY = max(0, cardY-8)
			}
		}
	}

	if gm.SelectedCommit < gm.HistoryScrollY {
		gm.HistoryScrollY = gm.SelectedCommit
	} else if gm.SelectedCommit >= gm.HistoryScrollY+10 {
		gm.HistoryScrollY = gm.SelectedCommit - 9
	}
}

// HandleKey processes keyboard shortcuts in the Git Modal.
func (gm *GitModal) HandleKey(k input.Key) bool {
	if !gm.Open {
		return false
	}

	// 1. Esc closes popup cards or dismisses active inputs
	if k.Type == input.KeyEsc {
		if gm.DetailDiffOpen {
			gm.DetailDiffOpen = false
			gm.DetailDiffLines = nil
			return true
		}
		if gm.DetailOpen {
			gm.DetailOpen = false
			return true
		}
		if gm.SearchActive {
			gm.SearchActive = false
			return true
		}
		if gm.IsCommitting {
			gm.IsCommitting = false
			gm.CommitError = ""
			return true
		}
		gm.Open = false
		return true
	}

	// 2. Commit Details Card / File Diff active navigation
	if gm.DetailDiffOpen {
		switch k.Type {
		case input.KeyUp:
			gm.DetailDiffScrollY = max(0, gm.DetailDiffScrollY-1)
			return true
		case input.KeyDown:
			gm.DetailDiffScrollY++
			return true
		case input.KeyLeft:
			gm.DetailDiffScrollX = max(0, gm.DetailDiffScrollX-4)
			return true
		case input.KeyRight:
			gm.DetailDiffScrollX += 4
			return true
		case input.KeyPgUp:
			gm.DetailDiffScrollY = max(0, gm.DetailDiffScrollY-10)
			return true
		case input.KeyPgDown:
			gm.DetailDiffScrollY += 10
			return true
		case input.KeyHome:
			gm.DetailDiffScrollY = 0
			gm.DetailDiffScrollX = 0
			return true
		}
		if k.Rune == 'k' {
			gm.DetailDiffScrollY = max(0, gm.DetailDiffScrollY-1)
			return true
		}
		if k.Rune == 'j' {
			gm.DetailDiffScrollY++
			return true
		}
		if k.Rune == 'h' {
			gm.DetailDiffScrollX = max(0, gm.DetailDiffScrollX-4)
			return true
		}
		if k.Rune == 'l' {
			gm.DetailDiffScrollX += 4
			return true
		}
		return true
	}

	if gm.DetailOpen {
		switch k.Type {
		case input.KeyUp:
			if gm.DetailCommit != nil && len(gm.DetailCommit.Files) > 0 {
				if gm.DetailSelectedFile > 0 {
					gm.DetailSelectedFile--
				} else {
					gm.DetailScrollY = max(0, gm.DetailScrollY-1)
				}
			} else {
				gm.DetailScrollY = max(0, gm.DetailScrollY-1)
			}
			return true
		case input.KeyDown:
			if gm.DetailCommit != nil && len(gm.DetailCommit.Files) > 0 {
				if gm.DetailSelectedFile < len(gm.DetailCommit.Files)-1 {
					gm.DetailSelectedFile++
				} else {
					gm.DetailScrollY++
				}
			} else {
				gm.DetailScrollY++
			}
			return true
		case input.KeyEnter:
			if gm.DetailCommit != nil && gm.DetailSelectedFile >= 0 && gm.DetailSelectedFile < len(gm.DetailCommit.Files) {
				gm.openDetailFileDiff(gm.DetailCommit.Files[gm.DetailSelectedFile].Path)
				return true
			}
		case input.KeyPgUp:
			gm.DetailScrollY = max(0, gm.DetailScrollY-8)
			return true
		case input.KeyPgDown:
			gm.DetailScrollY += 8
			return true
		}
		if k.Rune == 'k' {
			if gm.DetailCommit != nil && len(gm.DetailCommit.Files) > 0 && gm.DetailSelectedFile > 0 {
				gm.DetailSelectedFile--
			} else {
				gm.DetailScrollY = max(0, gm.DetailScrollY-1)
			}
			return true
		}
		if k.Rune == 'j' {
			if gm.DetailCommit != nil && len(gm.DetailCommit.Files) > 0 && gm.DetailSelectedFile < len(gm.DetailCommit.Files)-1 {
				gm.DetailSelectedFile++
			} else {
				gm.DetailScrollY++
			}
			return true
		}
		return true
	}

	// 3. Tab switching (when not typing in commit message or search bar)
	if !gm.IsCommitting && !gm.SearchActive {
		if k.Type == input.KeyTab || k.Type == input.KeyBacktab {
			gm.CurrentTab = (gm.CurrentTab + 1) % 2
			return true
		}
		if k.Rune == '1' {
			gm.CurrentTab = 0
			return true
		}
		if k.Rune == '2' {
			gm.CurrentTab = 1
			return true
		}
	}

	// 4. Tab 0: Interactive Commit mode
	if gm.CurrentTab == 0 {
		if gm.IsCommitting {
			switch k.Type {
			case input.KeyEnter:
				msg := strings.TrimSpace(gm.CommitMessage)
				if msg == "" {
					gm.CommitError = "Commit message cannot be empty"
					return true
				}
				err := git.Commit(gm.WorkspaceDir, msg)
				if err != nil {
					gm.CommitError = err.Error()
					return true
				}
				gm.IsCommitting = false
				gm.CommitMessage = ""
				gm.CommitError = ""
				gm.PromptMsg = ""
				gm.StatusMsg = "Changes committed successfully"
				gm.Refresh()
				return true
			case input.KeyBackspace:
				if len(gm.CommitMessage) > 0 {
					runes := []rune(gm.CommitMessage)
					gm.CommitMessage = string(runes[:len(runes)-1])
				}
				return true
			default:
				if k.Rune >= 32 {
					gm.CommitMessage += string(k.Rune)
					return true
				}
			}
			return true
		}

		// Normal Tab 0 keys
		switch k.Type {
		case input.KeyUp:
			gm.moveSelection(-1)
			return true
		case input.KeyDown:
			gm.moveSelection(1)
			return true
		case input.KeySpace:
			item := gm.getSelectedItem()
			if item != nil {
				if item.Staged {
					_ = git.UnstageFile(gm.WorkspaceDir, item.Path)
				} else {
					_ = git.StageFile(gm.WorkspaceDir, item.Path)
				}
				gm.PromptMsg = ""
				gm.Refresh()
			}
			return true
		case input.KeyPgUp:
			gm.DiffScrollY = max(0, gm.DiffScrollY-10)
			return true
		case input.KeyPgDown:
			gm.DiffScrollY += 10
			return true
		case input.KeyLeft:
			gm.DiffScrollX = max(0, gm.DiffScrollX-4)
			return true
		case input.KeyRight:
			gm.DiffScrollX += 4
			return true
		}

		switch k.Rune {
		case 'k':
			gm.moveSelection(-1)
			return true
		case 'j':
			gm.moveSelection(1)
			return true
		case 'h':
			gm.DiffScrollX = max(0, gm.DiffScrollX-4)
			return true
		case 'l':
			gm.DiffScrollX += 4
			return true
		case 'a':
			if gm.RepoStatus != nil {
				if len(gm.RepoStatus.Staged) > 0 && len(gm.RepoStatus.Unstaged) == 0 && len(gm.RepoStatus.Untracked) == 0 {
					_ = git.UnstageAll(gm.WorkspaceDir)
				} else {
					_ = git.StageAll(gm.WorkspaceDir)
				}
				gm.PromptMsg = ""
				gm.Refresh()
			}
			return true
		case 'c':
			if gm.RepoStatus == nil || len(gm.RepoStatus.Staged) == 0 {
				gm.PromptMsg = i18n.T("git.commit_empty_warning")
			} else {
				gm.IsCommitting = true
				gm.CommitMessage = ""
				gm.CommitError = ""
				gm.PromptMsg = ""
			}
			return true
		case 'r':
			gm.Refresh()
			gm.StatusMsg = "Refreshed repository status"
			return true
		}
		return false
	}

	// 5. Tab 1: 2D DAG Canvas & Commit History
	if gm.CurrentTab == 1 {
		if gm.SearchActive {
			switch k.Type {
			case input.KeyEnter:
				gm.SearchActive = false
				gm.Refresh()
				return true
			case input.KeyBackspace:
				if len(gm.SearchQuery) > 0 {
					runes := []rune(gm.SearchQuery)
					gm.SearchQuery = string(runes[:len(runes)-1])
				}
				return true
			default:
				if k.Rune >= 32 {
					gm.SearchQuery += string(k.Rune)
					return true
				}
			}
			return true
		}

		// 2D Pan and Navigation keys
		switch k.Type {
		case input.KeyLeft:
			gm.GraphScrollX = max(0, gm.GraphScrollX-6)
			return true
		case input.KeyRight:
			gm.GraphScrollX += 6
			return true
		case input.KeyUp:
			if gm.SelectedCommit > 0 {
				gm.SelectedCommit--
				gm.syncScrollToSelected()
			}
			return true
		case input.KeyDown:
			if gm.SelectedCommit+1 < len(gm.CommitGraph) {
				gm.SelectedCommit++
				gm.syncScrollToSelected()
			}
			return true
		case input.KeyPgUp:
			gm.SelectedCommit = max(0, gm.SelectedCommit-8)
			gm.syncScrollToSelected()
			return true
		case input.KeyPgDown:
			gm.SelectedCommit = min(len(gm.CommitGraph)-1, gm.SelectedCommit+8)
			gm.syncScrollToSelected()
			return true
		case input.KeyHome:
			gm.SelectedCommit = 0
			gm.centerOnHEAD()
			return true
		case input.KeyEnd:
			gm.SelectedCommit = max(0, len(gm.CommitGraph)-1)
			gm.syncScrollToSelected()
			return true
		case input.KeyEnter:
			if gm.SelectedCommit >= 0 && gm.SelectedCommit < len(gm.CommitGraph) {
				node := gm.CommitGraph[gm.SelectedCommit]
				if node.Hash != "" {
					detail, err := git.GetCommitDetails(gm.WorkspaceDir, node.Hash)
					if err == nil {
						gm.DetailCommit = detail
						gm.DetailOpen = true
						gm.AnimProgress = 0.0
						gm.DetailScrollY = 0
					}
				}
			}
			return true
		}

		switch k.Rune {
		case 'h':
			gm.GraphScrollX = max(0, gm.GraphScrollX-6)
			return true
		case 'l':
			gm.GraphScrollX += 6
			return true
		case 'k':
			if gm.SelectedCommit > 0 {
				gm.SelectedCommit--
				gm.syncScrollToSelected()
			}
			return true
		case 'j':
			if gm.SelectedCommit+1 < len(gm.CommitGraph) {
				gm.SelectedCommit++
				gm.syncScrollToSelected()
			}
			return true
		case 'b':
			if len(gm.Branches) > 0 {
				gm.SelectedBranchIndex = (gm.SelectedBranchIndex + 1) % len(gm.Branches)
				gm.Refresh()
			}
			return true
		case 'd':
			if len(gm.Periods) > 0 {
				gm.SelectedPeriodIndex = (gm.SelectedPeriodIndex + 1) % len(gm.Periods)
				gm.Refresh()
			}
			return true
		case '/':
			gm.SearchActive = true
			return true
		case 'r':
			gm.Refresh()
			gm.StatusMsg = "Refreshed commit graph"
			return true
		}
	}

	return false
}

// HandleWheel processes mouse wheel events for scrolling and 2D panning.
func (gm *GitModal) HandleWheel(screenX, screenY, deltaY, deltaX int, w, h int) bool {
	if !gm.Open {
		return false
	}

	modalW := min(120, w-4)
	modalH := min(40, h-4)
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2

	if gm.DetailDiffOpen {
		if deltaY < 0 {
			gm.DetailDiffScrollY = max(0, gm.DetailDiffScrollY-1)
		} else if deltaY > 0 {
			gm.DetailDiffScrollY++
		}
		if deltaX != 0 {
			gm.DetailDiffScrollX = max(0, gm.DetailDiffScrollX+deltaX)
		}
		return true
	}

	if gm.DetailOpen {
		if deltaY < 0 {
			gm.DetailScrollY = max(0, gm.DetailScrollY-1)
		} else if deltaY > 0 {
			gm.DetailScrollY++
		}
		return true
	}

	if gm.CurrentTab == 0 {
		listW := 36
		if screenX < startX+listW {
			if deltaY < 0 {
				gm.moveSelection(-1)
			} else if deltaY > 0 {
				gm.moveSelection(1)
			}
		} else {
			if deltaY < 0 {
				gm.DiffScrollY = max(0, gm.DiffScrollY-3)
			} else if deltaY > 0 {
				gm.DiffScrollY += 3
			}
			if deltaX != 0 {
				gm.DiffScrollX = max(0, gm.DiffScrollX+deltaX)
			}
		}
		return true
	}

	if gm.CurrentTab == 1 {
		midY := startY + 3 + (modalH-6)/2
		if screenY < midY {
			// Top DAG Canvas: 2D Pan
			if deltaY < 0 {
				gm.GraphScrollY = max(0, gm.GraphScrollY-1)
			} else if deltaY > 0 {
				gm.GraphScrollY++
			}
			if deltaX != 0 {
				gm.GraphScrollX = max(0, gm.GraphScrollX+deltaX)
			} else {
				// Vertical wheel in canvas also nudges X if at edge
				if deltaY != 0 {
					gm.GraphScrollX = max(0, gm.GraphScrollX+deltaY*4)
				}
			}
		} else {
			// Bottom history table
			if deltaY < 0 {
				gm.HistoryScrollY = max(0, gm.HistoryScrollY-1)
			} else if deltaY > 0 {
				gm.HistoryScrollY++
			}
		}
		return true
	}

	return false
}

// HandleClick processes mouse clicks in the Git modal.
func (gm *GitModal) HandleClick(screenX, screenY int, w, h int) bool {
	if !gm.Open {
		return false
	}

	modalW := min(120, w-4)
	modalH := min(40, h-4)
	startX := (w - modalW) / 2
	startY := (h - modalH) / 2

	// 1. Commit Details Popup / File Diff dismiss and interaction
	if gm.DetailOpen {
		targetW := min(modalW-10, 84)
		targetH := min(modalH-6, 26)
		curW := max(20, int(float64(targetW)*gm.AnimProgress))
		curH := max(6, int(float64(targetH)*gm.AnimProgress))
		boxX := startX + (modalW-curW)/2
		boxY := startY + (modalH-curH)/2

		if screenX < boxX || screenX >= boxX+curW || screenY < boxY || screenY >= boxY+curH {
			if gm.DetailDiffOpen {
				gm.DetailDiffOpen = false
				gm.DetailDiffLines = nil
			} else {
				gm.DetailOpen = false
			}
			return true
		}

		if gm.DetailDiffOpen {
			// Click on title row: close diff and go back to commit details
			if screenY == boxY {
				gm.DetailDiffOpen = false
				gm.DetailDiffLines = nil
				return true
			}
			return true
		}

		// Click on title bar close button:
		if screenY == boxY && screenX >= boxX+curW-16 {
			gm.DetailOpen = false
			return true
		}

		// Click on a file line in the details modal:
		r := screenY - (boxY + 1)
		th := ui.DefaultTheme()
		contentLines := gm.getCommitDetailLines(&th)
		lineIdx := gm.DetailScrollY + r
		if lineIdx >= 0 && lineIdx < len(contentLines) {
			item := contentLines[lineIdx]
			if item.FileIndex >= 0 && gm.DetailCommit != nil && item.FileIndex < len(gm.DetailCommit.Files) {
				gm.DetailSelectedFile = item.FileIndex
				gm.openDetailFileDiff(gm.DetailCommit.Files[item.FileIndex].Path)
				return true
			}
		}
		return true
	}

	// 2. Click outside modal closes it
	if screenX < startX || screenX >= startX+modalW || screenY < startY || screenY >= startY+modalH {
		gm.Open = false
		return true
	}

	// 3. Tab header clicks
	if screenY == startY+1 {
		tab1W := len([]rune(i18n.T("git.tab_commit")))
		if screenX >= startX+2 && screenX < startX+2+tab1W {
			gm.CurrentTab = 0
			return true
		}
		tab2Start := startX + 4 + tab1W
		tab2W := len([]rune(i18n.T("git.tab_graph")))
		if screenX >= tab2Start && screenX < tab2Start+tab2W {
			gm.CurrentTab = 1
			return true
		}
	}

	// 4. Tab 1 Toolbar clicks
	if gm.CurrentTab == 1 {
		graphH := (modalH - 6) / 2
		toolY := startY + 3 + graphH + 1

		if screenY == toolY {
			if screenX >= startX+3 && screenX < startX+26 {
				gm.SelectedBranchIndex = (gm.SelectedBranchIndex + 1) % len(gm.Branches)
				gm.Refresh()
				return true
			}
			if screenX >= startX+28 && screenX < startX+50 {
				gm.SelectedPeriodIndex = (gm.SelectedPeriodIndex + 1) % len(gm.Periods)
				gm.Refresh()
				return true
			}
			if screenX >= startX+52 && screenX < startX+76 {
				gm.SearchActive = true
				return true
			}
			if screenX >= startX+78 && screenX < startX+92 {
				gm.Refresh()
				return true
			}
		}

		// Click / Drag on Top DAG Canvas cards
		topY := startY + 3
		if screenY >= topY+1 && screenY < topY+1+graphH {
			gm.IsDragging = true
			gm.DragStartX = screenX
			gm.DragStartY = screenY
			gm.DragInitialScrollX = gm.GraphScrollX
			gm.DragInitialScrollY = gm.GraphScrollY

			for _, c := range gm.DAGCommits {
				cardX := startX + 3 + c.Col*26 - gm.GraphScrollX
				cardY := topY + 1 + c.Lane*5 - gm.GraphScrollY
				if screenX >= cardX && screenX < cardX+20 && screenY >= cardY && screenY < cardY+4 {
					tableIdx := gm.findTableIndexByHash(c.Hash)
					if tableIdx != -1 {
						if gm.SelectedCommit == tableIdx {
							detail, err := git.GetCommitDetails(gm.WorkspaceDir, c.Hash)
							if err == nil {
								gm.DetailCommit = detail
								gm.DetailOpen = true
								gm.AnimProgress = 0.0
								gm.DetailScrollY = 0
							}
						} else {
							gm.SelectedCommit = tableIdx
							gm.syncScrollToSelected()
						}
					}
					return true
				}
			}
			return true
		}

		// Click on Bottom Table rows
		botY := toolY + 2
		botH := modalH - 5 - (toolY - startY)
		if screenY >= botY && screenY < botY+botH {
			idx := gm.HistoryScrollY + (screenY - botY)
			if idx >= 0 && idx < len(gm.CommitGraph) {
				if gm.SelectedCommit == idx && gm.CommitGraph[idx].Hash != "" {
					detail, err := git.GetCommitDetails(gm.WorkspaceDir, gm.CommitGraph[idx].Hash)
					if err == nil {
						gm.DetailCommit = detail
						gm.DetailOpen = true
						gm.AnimProgress = 0.0
						gm.DetailScrollY = 0
					}
				} else {
					gm.SelectedCommit = idx
					gm.syncScrollToSelected()
				}
				return true
			}
		}
	}

	return false
}

// HandleDrag handles mouse dragging to pan the 2D DAG canvas diagram.
func (gm *GitModal) HandleDrag(screenX, screenY, w, h int) bool {
	if !gm.Open || !gm.IsDragging || gm.CurrentTab != 1 {
		return false
	}
	deltaX := screenX - gm.DragStartX
	deltaY := screenY - gm.DragStartY
	if deltaX != 0 || deltaY != 0 {
		gm.GraphScrollX = max(0, gm.DragInitialScrollX-deltaX)
		gm.GraphScrollY = max(0, gm.DragInitialScrollY-deltaY)
		return true
	}
	return false
}

// HandleRelease handles mouse button release to stop dragging.
func (gm *GitModal) HandleRelease() {
	gm.IsDragging = false
}

// Render draws the complete Git Modal onto the GoatUI terminal buffer.
func (gm *GitModal) Render(buf *buffer.Buffer, w, h int, theme *ui.Theme) {
	if !gm.Open {
		return
	}

	modalW := min(120, w-4)
	modalH := min(40, h-4)
	if modalW < 40 || modalH < 15 {
		return
	}

	startX := (w - modalW) / 2
	startY := (h - modalH) / 2

	borderFg := toColor(theme.BorderColor)
	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)

	// Draw background and outer rounded border
	for y := startY; y < startY+modalH; y++ {
		for x := startX; x < startX+modalW; x++ {
			buf.SetRune(x, y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// Rounded frame corners and lines
	buf.SetRune(startX, startY, '╭', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX+modalW-1, startY, '╮', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX, startY+modalH-1, '╰', borderFg, bg, cell.AttrNone)
	buf.SetRune(startX+modalW-1, startY+modalH-1, '╯', borderFg, bg, cell.AttrNone)

	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, startY, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(x, startY+modalH-1, '─', borderFg, bg, cell.AttrNone)
	}
	for y := startY + 1; y < startY+modalH-1; y++ {
		buf.SetRune(startX, y, '│', borderFg, bg, cell.AttrNone)
		buf.SetRune(startX+modalW-1, y, '│', borderFg, bg, cell.AttrNone)
	}

	// Modal Title
	title := " " + i18n.T("git.window_title") + " "
	if gm.RepoStatus != nil && gm.RepoStatus.Branch != "" {
		title = fmt.Sprintf(" %s (%s) ", i18n.T("git.window_title"), gm.RepoStatus.Branch)
	}
	for i, r := range []rune(title) {
		buf.SetRune(startX+2+i, startY, r, toColor(theme.Function), bg, cell.AttrBold)
	}

	// Tab header bar (Row startY + 1)
	tab1Label := []rune(i18n.T("git.tab_commit"))
	tab2Label := []rune(i18n.T("git.tab_graph"))

	tab1Fg, tab1Bg := fg, bg
	tab2Fg, tab2Bg := fg, bg
	tab1Attr, tab2Attr := cell.AttrNone, cell.AttrNone

	if gm.CurrentTab == 0 {
		tab1Fg = toColor(theme.Background)
		tab1Bg = toColor(theme.Function)
		tab1Attr = cell.AttrBold
	} else {
		tab2Fg = toColor(theme.Background)
		tab2Bg = toColor(theme.Function)
		tab2Attr = cell.AttrBold
	}

	curX := startX + 2
	for _, r := range tab1Label {
		buf.SetRune(curX, startY+1, r, tab1Fg, tab1Bg, tab1Attr)
		curX++
	}
	curX += 2
	for _, r := range tab2Label {
		buf.SetRune(curX, startY+1, r, tab2Fg, tab2Bg, tab2Attr)
		curX++
	}

	// Divider line under tabs
	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, startY+2, '─', borderFg, bg, cell.AttrNone)
	}

	contentTop := startY + 3
	contentH := modalH - 5

	if gm.CurrentTab == 0 {
		gm.renderTabCommit(buf, startX, contentTop, modalW, contentH, theme)
	} else {
		gm.renderTabGraph(buf, startX, contentTop, modalW, contentH, theme)
	}

	// Footer instructions (clear line to prevent any artifacts)
	footer := " " + i18n.T("git.footer_tab0") + " "
	if gm.CurrentTab == 1 {
		footer = " " + i18n.T("git.footer_tab1") + " "
	}
	if gm.StatusMsg != "" {
		footer = fmt.Sprintf(" %s │ %s", gm.StatusMsg, strings.TrimSpace(footer))
	}
	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, startY+modalH-1, '─', borderFg, bg, cell.AttrNone)
	}
	for i, r := range []rune(footer) {
		fx := startX + 3 + i
		if fx < startX+modalW-2 {
			buf.SetRune(fx, startY+modalH-1, r, toColor(theme.Comment), bg, cell.AttrNone)
		}
	}

	// Render floating Commit Details popup if active
	if gm.DetailOpen && gm.DetailCommit != nil {
		gm.renderDetailCard(buf, startX, startY, modalW, modalH, theme)
	}
}

// renderTabCommit renders the repository status file tree on the left and unified diff on the right.
func (gm *GitModal) renderTabCommit(buf *buffer.Buffer, startX, topY, modalW, contentH int, theme *ui.Theme) {
	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)

	listW := 36
	if listW > modalW/2 {
		listW = modalW / 2
	}

	// Vertical divider between file list and diff
	for y := topY; y < topY+contentH; y++ {
		buf.SetRune(startX+listW, y, '│', borderFg, bg, cell.AttrNone)
	}

	// Left Pane: Structured change categories
	stagedCount := 0
	unstagedCount := 0
	untrackedCount := 0
	if gm.RepoStatus != nil {
		stagedCount = len(gm.RepoStatus.Staged)
		unstagedCount = len(gm.RepoStatus.Unstaged)
		untrackedCount = len(gm.RepoStatus.Untracked)
	}

	totalItems := stagedCount + unstagedCount + untrackedCount
	if totalItems == 0 {
		cleanMsg := i18n.T("git.no_changes")
		for i, r := range []rune(cleanMsg) {
			buf.SetRune(startX+3+i, topY+2, r, toColor(theme.String), bg, cell.AttrNone)
		}
	} else {
		row := 0
		drawCategory := func(catIdx int, title string, items []git.FileItem, color uint32) {
			if len(items) == 0 {
				return
			}
			if row < contentH {
				// Section header
				for i, r := range []rune(title) {
					buf.SetRune(startX+2+i, topY+row, r, toColor(color), bg, cell.AttrBold)
				}
				row++
			}
			for i, item := range items {
				if row >= contentH {
					break
				}
				isSel := gm.SelectedCategory == catIdx && gm.SelectedIndex == i
				prefix := "  "
				rowBg := bg
				rowFg := fg
				if isSel {
					prefix = "▶ "
					rowBg = toColor(theme.SelectionBg)
					rowFg = toColor(theme.PopupSelFg)
				}

				badge := fmt.Sprintf("[%s]", item.Status)
				lineStr := fmt.Sprintf("%s%s %s", prefix, badge, filepath.Base(item.Path))
				col := startX + 2
				for _, r := range lineStr {
					if col < startX+listW-1 {
						buf.SetRune(col, topY+row, r, rowFg, rowBg, cell.AttrNone)
						col++
					}
				}
				// Fill remaining row width with rowBg
				for col < startX+listW {
					buf.SetRune(col, topY+row, ' ', rowFg, rowBg, cell.AttrNone)
					col++
				}
				row++
			}
			row++ // empty space between sections
		}

		drawCategory(0, fmt.Sprintf(i18n.T("git.staged_section"), stagedCount), gm.RepoStatus.Staged, theme.String)
		drawCategory(1, fmt.Sprintf(i18n.T("git.unstaged_section"), unstagedCount), gm.RepoStatus.Unstaged, theme.DiagnosticWarn)
		drawCategory(2, fmt.Sprintf(i18n.T("git.untracked_section"), untrackedCount), gm.RepoStatus.Untracked, theme.Function)
	}

	// Action buttons at bottom of left pane
	btnY := topY + contentH - 2
	actionStr := "Space: Stage  │  a: All  │  c: Commit"
	for i, r := range []rune(actionStr) {
		buf.SetRune(startX+2+i, btnY, r, toColor(theme.Comment), bg, cell.AttrNone)
	}

	// Right Pane: Unified Diff Viewer
	diffStartX := startX + listW + 2

	item := gm.getSelectedItem()
	diffHeader := "Diff Preview"
	if item != nil {
		diffHeader = fmt.Sprintf("Diff: %s  (%s)", item.Path, item.Status)
	}
	for i, r := range []rune(diffHeader) {
		if diffStartX+i < startX+modalW-1 {
			buf.SetRune(diffStartX+i, topY, r, toColor(theme.Function), bg, cell.AttrBold)
		}
	}

	// Unified Diff Rendering: Background color highlighting without leading '+' or '-'
	if gm.DiffContent != "" {
		diffLines := strings.Split(gm.DiffContent, "\n")
		for r := 0; r < contentH-2; r++ {
			lineIdx := gm.DiffScrollY + r
			if lineIdx >= len(diffLines) {
				break
			}
			line := diffLines[lineIdx]
			lineFg := fg
			lineBg := bg
			attr := cell.AttrNone

			isAdded := strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")
			isDeleted := strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")

			if isAdded {
				lineFg = toColor(theme.String)
				lineBg = toColor(0x1e3a2b) // Soft subtle green background tint
				line = " " + line[1:]     // Strip leading '+'
			} else if isDeleted {
				lineFg = toColor(theme.DiagnosticError)
				lineBg = toColor(0x3d1e24) // Soft subtle red background tint
				line = " " + line[1:]     // Strip leading '-'
			} else if strings.HasPrefix(line, "@@") {
				lineFg = toColor(theme.Keyword)
				attr = cell.AttrBold
			}

			// First fill the whole line width with lineBg
			for c := diffStartX; c < startX+modalW-2; c++ {
				buf.SetRune(c, topY+1+r, ' ', lineFg, lineBg, cell.AttrNone)
			}

			col := diffStartX
			lineRunes := []rune(line)
			for charIdx := gm.DiffScrollX; charIdx < len(lineRunes); charIdx++ {
				if col < startX+modalW-2 {
					buf.SetRune(col, topY+1+r, lineRunes[charIdx], lineFg, lineBg, attr)
					col++
				}
			}
		}
	}

	// Commit prompt / input bar
	if gm.IsCommitting {
		inputY := topY + contentH - 3
		inputW := modalW - 6
		prompt := " " + i18n.T("git.commit_prompt") + gm.CommitMessage + "█"
		promptRunes := []rune(prompt)
		for c := 0; c < inputW; c++ {
			r := ' '
			if c < len(promptRunes) {
				r = promptRunes[c]
			}
			buf.SetRune(startX+3+c, inputY, r, toColor(theme.Foreground), toColor(theme.SelectionBg), cell.AttrBold)
		}
		if gm.CommitError != "" {
			errRunes := []rune(" " + gm.CommitError)
			for c := 0; c < len(errRunes) && c < inputW; c++ {
				buf.SetRune(startX+3+c, inputY+1, errRunes[c], toColor(theme.DiagnosticError), bg, cell.AttrBold)
			}
		}
	} else if gm.PromptMsg != "" {
		warnY := topY + contentH - 3
		for i, r := range []rune(" ⚠ " + gm.PromptMsg) {
			buf.SetRune(startX+3+i, warnY, r, toColor(theme.DiagnosticWarn), bg, cell.AttrBold)
		}
	}
}

// renderTabGraph renders the 2D DAG canvas diagram, filter toolbar, and synchronized history table.
func (gm *GitModal) renderTabGraph(buf *buffer.Buffer, startX, topY, modalW, contentH int, theme *ui.Theme) {
	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)

	graphH := (contentH - 3) / 2
	toolbarY := topY + graphH + 1
	tableTop := toolbarY + 2
	tableH := contentH - graphH - 3

	// Palette for colorful metro lines and cards
	metroPalette := []uint32{
		theme.Function,
		theme.String,
		theme.Keyword,
		theme.Constant,
		theme.DiagnosticInfo,
		theme.DiagnosticWarn,
	}

	// 1. Top Pane: 2D Branch Diagram
	graphTitle := " " + i18n.T("git.graph_header") + " "
	for i, r := range []rune(graphTitle) {
		buf.SetRune(startX+2+i, topY, r, toColor(theme.Function), bg, cell.AttrBold)
	}

	// Clear top pane with background and subtle dot grid
	for r := 0; r < graphH; r++ {
		rowY := topY + 1 + r
		for x := startX + 1; x < startX+modalW-1; x++ {
			rChar := ' '
			rFg := fg
			if (x-startX+gm.GraphScrollX)%6 == 0 && (r+gm.GraphScrollY)%5 == 0 {
				rChar = '·'
				rFg = toColor(theme.Comment)
			}
			buf.SetRune(x, rowY, rChar, rFg, bg, cell.AttrNone)
		}
	}

	selectedHash := ""
	if gm.SelectedCommit >= 0 && gm.SelectedCommit < len(gm.CommitGraph) {
		selectedHash = gm.CommitGraph[gm.SelectedCommit].Hash
	}

	// Draw DAG connection lines between cards
	for _, c := range gm.DAGCommits {
		for _, parentHash := range c.Parents {
			parent := gm.findDAGNodeByHash(parentHash)
			if parent == nil {
				continue
			}
			// Connector from parent right edge to child left edge
			xStart := startX + 3 + parent.Col*26 + 20 - gm.GraphScrollX
			yStart := topY + 1 + parent.Lane*5 + 1 - gm.GraphScrollY
			xEnd := startX + 3 + c.Col*26 - gm.GraphScrollX
			yEnd := topY + 1 + c.Lane*5 + 1 - gm.GraphScrollY

			lineColor := toColor(metroPalette[c.Lane%len(metroPalette)])

			if parent.Lane == c.Lane {
				// Same horizontal lane: straight line with arrowhead
				if yStart >= topY+1 && yStart < topY+1+graphH {
					for x := xStart; x < xEnd; x++ {
						if x >= startX+2 && x < startX+modalW-2 {
							ch := '─'
							if x == xEnd-1 {
								ch = '▶'
							}
							buf.SetRune(x, yStart, ch, lineColor, bg, cell.AttrBold)
						}
					}
				}
			} else if c.Lane > parent.Lane {
				// Branching downward
				turnX := xStart + 2
				if yStart >= topY+1 && yStart < topY+1+graphH {
					for x := xStart; x < turnX; x++ {
						if x >= startX+2 && x < startX+modalW-2 {
							buf.SetRune(x, yStart, '─', lineColor, bg, cell.AttrBold)
						}
					}
					if turnX >= startX+2 && turnX < startX+modalW-2 {
						buf.SetRune(turnX, yStart, '╮', lineColor, bg, cell.AttrBold)
					}
				}
				minY := min(yStart+1, yEnd-1)
				maxY := max(yStart+1, yEnd-1)
				if turnX >= startX+2 && turnX < startX+modalW-2 {
					for y := minY; y <= maxY; y++ {
						if y >= topY+1 && y < topY+1+graphH {
							buf.SetRune(turnX, y, '│', lineColor, bg, cell.AttrBold)
						}
					}
				}
				if yEnd >= topY+1 && yEnd < topY+1+graphH {
					if turnX >= startX+2 && turnX < startX+modalW-2 {
						buf.SetRune(turnX, yEnd, '╰', lineColor, bg, cell.AttrBold)
					}
					for x := turnX + 1; x < xEnd; x++ {
						if x >= startX+2 && x < startX+modalW-2 {
							ch := '─'
							if x == xEnd-1 {
								ch = '▶'
							}
							buf.SetRune(x, yEnd, ch, lineColor, bg, cell.AttrBold)
						}
					}
				}
			} else {
				// Merging upward
				turnX := xStart + 2
				if yStart >= topY+1 && yStart < topY+1+graphH {
					for x := xStart; x < turnX; x++ {
						if x >= startX+2 && x < startX+modalW-2 {
							buf.SetRune(x, yStart, '─', lineColor, bg, cell.AttrBold)
						}
					}
					if turnX >= startX+2 && turnX < startX+modalW-2 {
						buf.SetRune(turnX, yStart, '╯', lineColor, bg, cell.AttrBold)
					}
				}
				minY := min(yEnd+1, yStart-1)
				maxY := max(yEnd+1, yStart-1)
				if turnX >= startX+2 && turnX < startX+modalW-2 {
					for y := minY; y <= maxY; y++ {
						if y >= topY+1 && y < topY+1+graphH {
							buf.SetRune(turnX, y, '│', lineColor, bg, cell.AttrBold)
						}
					}
				}
				if yEnd >= topY+1 && yEnd < topY+1+graphH {
					if turnX >= startX+2 && turnX < startX+modalW-2 {
						buf.SetRune(turnX, yEnd, '╭', lineColor, bg, cell.AttrBold)
					}
					for x := turnX + 1; x < xEnd; x++ {
						if x >= startX+2 && x < startX+modalW-2 {
							ch := '─'
							if x == xEnd-1 {
								ch = '▶'
							}
							buf.SetRune(x, yEnd, ch, lineColor, bg, cell.AttrBold)
						}
					}
				}
			}
		}
	}

	// Draw DAG Commit Cards (Width: 20, Height: 4)
	cardW := 20
	cardH := 4
	for _, c := range gm.DAGCommits {
		cardX := startX + 3 + c.Col*26 - gm.GraphScrollX
		cardY := topY + 1 + c.Lane*5 - gm.GraphScrollY

		// Skip if completely outside pane viewport
		if cardX+cardW < startX+2 || cardX >= startX+modalW-2 || cardY+cardH < topY+1 || cardY >= topY+1+graphH {
			continue
		}

		isSelected := (selectedHash != "" && (strings.HasPrefix(c.Hash, selectedHash) || strings.HasPrefix(selectedHash, c.AbbrevHash)))

		laneColor := toColor(metroPalette[c.Lane%len(metroPalette)])
		borderColor := laneColor
		cardBg := bg
		cardFg := fg
		borderAttr := cell.AttrBold

		if isSelected {
			cardBg = toColor(theme.SelectionBg)
			borderColor = toColor(theme.PopupSelFg)
			cardFg = toColor(theme.PopupSelFg)
		}

		// Clear card interior with cardBg
		for cy := cardY; cy < cardY+cardH; cy++ {
			if cy >= topY+1 && cy < topY+1+graphH {
				for cx := cardX; cx < cardX+cardW; cx++ {
					if cx >= startX+2 && cx < startX+modalW-2 {
						buf.SetRune(cx, cy, ' ', cardFg, cardBg, cell.AttrNone)
					}
				}
			}
		}

		drawRuneSafe := func(x, y int, r rune, color cell.Color, attr cell.Modifier) {
			if x >= startX+2 && x < startX+modalW-2 && y >= topY+1 && y < topY+1+graphH {
				buf.SetRune(x, y, r, color, cardBg, attr)
			}
		}

		// Top border with [branch] hash
		drawRuneSafe(cardX, cardY, '╭', borderColor, borderAttr)
		drawRuneSafe(cardX+cardW-1, cardY, '╮', borderColor, borderAttr)
		for cx := cardX + 1; cx < cardX+cardW-1; cx++ {
			drawRuneSafe(cx, cardY, '─', borderColor, borderAttr)
		}
		headerStr := fmt.Sprintf("%s: %s", c.Branch, c.AbbrevHash)
		if len([]rune(headerStr)) > cardW-4 {
			headerStr = string([]rune(headerStr)[:cardW-4])
		}
		for i, r := range []rune(headerStr) {
			drawRuneSafe(cardX+2+i, cardY, r, borderColor, borderAttr)
		}

		// Bottom border
		drawRuneSafe(cardX, cardY+cardH-1, '╰', borderColor, borderAttr)
		drawRuneSafe(cardX+cardW-1, cardY+cardH-1, '╯', borderColor, borderAttr)
		for cx := cardX + 1; cx < cardX+cardW-1; cx++ {
			drawRuneSafe(cx, cardY+cardH-1, '─', borderColor, borderAttr)
		}

		// Left & right border rails
		for cy := cardY + 1; cy < cardY+cardH-1; cy++ {
			drawRuneSafe(cardX, cy, '│', borderColor, borderAttr)
			drawRuneSafe(cardX+cardW-1, cy, '│', borderColor, borderAttr)
		}

		// Line 1: Author
		authorStr := c.Author
		if len([]rune(authorStr)) > cardW-4 {
			authorStr = string([]rune(authorStr)[:cardW-5]) + "…"
		}
		for i, r := range []rune(authorStr) {
			drawRuneSafe(cardX+2+i, cardY+1, r, cardFg, cell.AttrNone)
		}

		// Line 2: Date
		dateStr := c.Date
		if len([]rune(dateStr)) > cardW-4 {
			dateStr = string([]rune(dateStr)[:cardW-5]) + "…"
		}
		for i, r := range []rune(dateStr) {
			drawRuneSafe(cardX+2+i, cardY+2, r, toColor(theme.Comment), cell.AttrNone)
		}
	}

	// 2. Middle Toolbar
	for x := startX + 1; x < startX+modalW-1; x++ {
		buf.SetRune(x, toolbarY-1, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(x, toolbarY+1, '─', borderFg, bg, cell.AttrNone)
	}

	branchName := "All branches"
	if gm.SelectedBranchIndex >= 0 && gm.SelectedBranchIndex < len(gm.Branches) {
		branchName = gm.Branches[gm.SelectedBranchIndex].Name
	}
	periodName := "All time"
	if gm.SelectedPeriodIndex >= 0 && gm.SelectedPeriodIndex < len(gm.Periods) {
		periodName = gm.Periods[gm.SelectedPeriodIndex]
	}

	btnBranch := fmt.Sprintf("  b: %s  ", branchName)
	btnPeriod := fmt.Sprintf("  d: %s  ", periodName)
	searchLabel := "  /: Search  "
	if gm.SearchQuery != "" {
		searchLabel = fmt.Sprintf("  /: %s  ", gm.SearchQuery)
	}
	if gm.SearchActive {
		searchLabel = fmt.Sprintf("  /: %s█  ", gm.SearchQuery)
	}
	btnRefresh := "  r: Refresh  "

	toolCol := startX + 3
	drawToolbarBtn := func(label string, active bool) {
		btnFg := toColor(theme.Foreground)
		btnBg := toColor(theme.SelectionBg)
		if active {
			btnBg = toColor(theme.Function)
			btnFg = toColor(theme.Background)
		}
		for _, r := range label {
			if toolCol < startX+modalW-2 {
				buf.SetRune(toolCol, toolbarY, r, btnFg, btnBg, cell.AttrBold)
				toolCol++
			}
		}
		toolCol += 2
	}

	drawToolbarBtn(btnBranch, false)
	drawToolbarBtn(btnPeriod, false)
	drawToolbarBtn(searchLabel, gm.SearchActive)
	drawToolbarBtn(btnRefresh, false)

	// 3. Bottom Pane: Commit History Table
	tableHeader := fmt.Sprintf(" %-9s %-20s %-40s %-16s %s",
		i18n.T("git.col_hash"),
		i18n.T("git.col_branch"),
		i18n.T("git.col_message"),
		i18n.T("git.col_author"),
		i18n.T("git.col_date"),
	)
	for i, r := range []rune(tableHeader) {
		if startX+2+i < startX+modalW-2 {
			buf.SetRune(startX+2+i, tableTop, r, toColor(theme.Comment), bg, cell.AttrBold)
		}
	}

	for r := 0; r < tableH; r++ {
		idx := gm.HistoryScrollY + r
		if idx >= len(gm.CommitGraph) {
			break
		}
		node := gm.CommitGraph[idx]
		rowY := tableTop + 1 + r
		isSel := idx == gm.SelectedCommit

		rowBg := bg
		rowFg := fg
		if isSel {
			rowBg = toColor(theme.SelectionBg)
			rowFg = toColor(theme.PopupSelFg)
		}

		// Clear row background
		for x := startX + 1; x < startX+modalW-1; x++ {
			buf.SetRune(x, rowY, ' ', rowFg, rowBg, cell.AttrNone)
		}

		truncStr := func(s string, maxLen int) string {
			runes := []rune(s)
			if len(runes) > maxLen {
				return string(runes[:maxLen-1]) + "…"
			}
			return s
		}

		rowText := fmt.Sprintf(" %-9s %-20s %-40s %-16s %s",
			node.Hash,
			truncStr(node.Refs, 19),
			truncStr(node.Message, 39),
			truncStr(node.Author, 15),
			node.Date,
		)

		col := startX + 2
		for _, r := range rowText {
			if col < startX+modalW-2 {
				buf.SetRune(col, rowY, r, rowFg, rowBg, cell.AttrNone)
				col++
			}
		}
	}
}

type detailContentLine struct {
	Text       string
	Color      uint32
	Bold       bool
	IsSelected bool
	FileIndex  int
}

func (gm *GitModal) getCommitDetailLines(theme *ui.Theme) []detailContentLine {
	if gm.DetailCommit == nil {
		return nil
	}
	var lines []detailContentLine
	lines = append(lines, detailContentLine{
		Text:      fmt.Sprintf("Author: %s <%s>", gm.DetailCommit.AuthorName, gm.DetailCommit.AuthorEmail),
		Color:     theme.Foreground,
		FileIndex: -1,
	})
	lines = append(lines, detailContentLine{
		Text:      fmt.Sprintf("Date:   %s (%s)", gm.DetailCommit.Date, gm.DetailCommit.RelativeDate),
		Color:     theme.Foreground,
		FileIndex: -1,
	})
	if gm.DetailCommit.Refs != "" {
		lines = append(lines, detailContentLine{
			Text:      fmt.Sprintf("Refs:   %s", gm.DetailCommit.Refs),
			Color:     theme.DiagnosticWarn,
			Bold:      true,
			FileIndex: -1,
		})
	}
	lines = append(lines, detailContentLine{Text: "─", Color: theme.BorderColor, FileIndex: -1})
	lines = append(lines, detailContentLine{
		Text:      fmt.Sprintf("Message: %s", gm.DetailCommit.Subject),
		Color:     theme.Function,
		Bold:      true,
		FileIndex: -1,
	})
	if gm.DetailCommit.Body != "" {
		for _, bline := range strings.Split(gm.DetailCommit.Body, "\n") {
			bline = strings.TrimSpace(bline)
			if bline != "" {
				lines = append(lines, detailContentLine{
					Text:      "  " + bline,
					Color:     theme.Foreground,
					FileIndex: -1,
				})
			}
		}
	}
	lines = append(lines, detailContentLine{Text: "─", Color: theme.BorderColor, FileIndex: -1})
	lines = append(lines, detailContentLine{
		Text:      fmt.Sprintf("%s (%d):", i18n.T("git.detail_files"), len(gm.DetailCommit.Files)),
		Color:     theme.Comment,
		Bold:      true,
		FileIndex: -1,
	})
	for idx, f := range gm.DetailCommit.Files {
		isSelected := (idx == gm.DetailSelectedFile)
		fileStr := fmt.Sprintf(" +%-3d -%-3d  %s", f.Additions, f.Deletions, f.Path)
		if isSelected {
			fileStr += "  ↵ Diff"
		}
		itemColor := theme.Foreground
		if isSelected {
			itemColor = theme.Function
		}
		lines = append(lines, detailContentLine{
			Text:       fileStr,
			Color:      itemColor,
			Bold:       isSelected,
			IsSelected: isSelected,
			FileIndex:  idx,
		})
	}
	return lines
}

func (gm *GitModal) openDetailFileDiff(filePath string) {
	if gm.DetailCommit == nil || filePath == "" {
		return
	}
	lines, err := git.GetCommitFileDiff(gm.WorkspaceDir, gm.DetailCommit.Hash, filePath)
	if err != nil {
		lines = []string{fmt.Sprintf("Error loading diff: %v", err)}
	}
	if len(lines) == 0 {
		lines = []string{"(Binary file or no changes)"}
	}
	gm.DetailDiffOpen = true
	gm.DetailDiffFile = filePath
	gm.DetailDiffLines = lines
	gm.DetailDiffScrollY = 0
	gm.DetailDiffScrollX = 0
}

// renderDetailCard renders the smoothly animated commit detail popup card or file diff.
func (gm *GitModal) renderDetailCard(buf *buffer.Buffer, startX, startY, modalW, modalH int, theme *ui.Theme) {
	targetW := min(modalW-10, 84)
	targetH := min(modalH-6, 26)

	curW := max(24, int(float64(targetW)*gm.AnimProgress))
	curH := max(8, int(float64(targetH)*gm.AnimProgress))

	boxX := startX + (modalW-curW)/2
	boxY := startY + (modalH-curH)/2

	cardBg := toColor(theme.PopupBg)
	cardFg := toColor(theme.Foreground)
	cardBorder := toColor(theme.Function)

	// Draw animated popup background
	for y := boxY; y < boxY+curH; y++ {
		for x := boxX; x < boxX+curW; x++ {
			buf.SetRune(x, y, ' ', cardFg, cardBg, cell.AttrNone)
		}
	}

	// Border
	buf.SetRune(boxX, boxY, '╭', cardBorder, cardBg, cell.AttrNone)
	buf.SetRune(boxX+curW-1, boxY, '╮', cardBorder, cardBg, cell.AttrNone)
	buf.SetRune(boxX, boxY+curH-1, '╰', cardBorder, cardBg, cell.AttrNone)
	buf.SetRune(boxX+curW-1, boxY+curH-1, '╯', cardBorder, cardBg, cell.AttrNone)

	for x := boxX + 1; x < boxX+curW-1; x++ {
		buf.SetRune(x, boxY, '─', cardBorder, cardBg, cell.AttrNone)
		buf.SetRune(x, boxY+curH-1, '─', cardBorder, cardBg, cell.AttrNone)
	}
	for y := boxY + 1; y < boxY+curH-1; y++ {
		buf.SetRune(boxX, y, '│', cardBorder, cardBg, cell.AttrNone)
		buf.SetRune(boxX+curW-1, y, '│', cardBorder, cardBg, cell.AttrNone)
	}

	// Render details only once animation has expanded sufficiently
	if gm.AnimProgress < 0.6 {
		return
	}

	// If DetailDiffOpen: render the full unified diff of the selected file
	if gm.DetailDiffOpen {
		diffTitle := fmt.Sprintf(" %s: %s (%s)  %s ", i18n.T("git.detail_diff_title"), gm.DetailDiffFile, gm.DetailCommit.AbbrevHash, i18n.T("git.detail_diff_back"))
		for i, r := range []rune(diffTitle) {
			if boxX+2+i < boxX+curW-1 {
				buf.SetRune(boxX+2+i, boxY, r, toColor(theme.DiagnosticInfo), cardBg, cell.AttrBold)
			}
		}

		maxVisible := curH - 2
		for r := 0; r < maxVisible; r++ {
			lineIdx := gm.DetailDiffScrollY + r
			lineY := boxY + 1 + r

			if lineIdx >= len(gm.DetailDiffLines) {
				for c := boxX + 1; c < boxX+curW-1; c++ {
					buf.SetRune(c, lineY, ' ', cardFg, cardBg, cell.AttrNone)
				}
				continue
			}

			line := gm.DetailDiffLines[lineIdx]
			lineFg := toColor(theme.Foreground)
			lineBg := cardBg
			attr := cell.AttrNone

			if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
				lineFg = toColor(theme.Comment)
			} else if strings.HasPrefix(line, "@@") {
				lineFg = toColor(theme.Keyword)
				attr = cell.AttrBold
			} else if strings.HasPrefix(line, "+") {
				lineFg = toColor(0x4ade80)
				lineBg = toColor(0x1e3a2b) // Soft subtle green background tint
				line = " " + line[1:]      // Strip leading '+'
			} else if strings.HasPrefix(line, "-") {
				lineFg = toColor(0xf87171)
				lineBg = toColor(0x3d1e24) // Soft subtle red background tint
				line = " " + line[1:]      // Strip leading '-'
			}

			// Fill the whole row width with lineBg
			for c := boxX + 1; c < boxX+curW-1; c++ {
				buf.SetRune(c, lineY, ' ', lineFg, lineBg, cell.AttrNone)
			}

			col := boxX + 2
			lineRunes := []rune(line)
			for charIdx := gm.DetailDiffScrollX; charIdx < len(lineRunes); charIdx++ {
				if col < boxX+curW-2 {
					buf.SetRune(col, lineY, lineRunes[charIdx], lineFg, lineBg, attr)
					col++
				}
			}
		}
		return
	}

	// Title bar
	title := fmt.Sprintf(" %s: %s  Esc: Close ", i18n.T("git.detail_title"), gm.DetailCommit.AbbrevHash)
	for i, r := range []rune(title) {
		if boxX+2+i < boxX+curW-1 {
			buf.SetRune(boxX+2+i, boxY, r, toColor(theme.DiagnosticWarn), cardBg, cell.AttrBold)
		}
	}

	contentLines := gm.getCommitDetailLines(theme)
	maxVisible := curH - 2
	for r := 0; r < maxVisible; r++ {
		lineIdx := gm.DetailScrollY + r
		lineY := boxY + 1 + r
		if lineIdx >= len(contentLines) {
			for x := boxX + 1; x < boxX+curW-1; x++ {
				buf.SetRune(x, lineY, ' ', cardFg, cardBg, cell.AttrNone)
			}
			continue
		}
		item := contentLines[lineIdx]

		if item.Text == "─" {
			for x := boxX + 1; x < boxX+curW-1; x++ {
				buf.SetRune(x, lineY, '─', toColor(item.Color), cardBg, cell.AttrNone)
			}
			continue
		}

		lineBg := cardBg
		if item.IsSelected {
			lineBg = toColor(theme.SelectionBg)
		}
		for x := boxX + 1; x < boxX+curW-1; x++ {
			buf.SetRune(x, lineY, ' ', toColor(item.Color), lineBg, cell.AttrNone)
		}

		col := boxX + 2
		attr := cell.AttrNone
		if item.Bold {
			attr = cell.AttrBold
		}
		for _, r := range item.Text {
			if col < boxX+curW-2 {
				buf.SetRune(col, lineY, r, toColor(item.Color), lineBg, attr)
				col++
			}
		}
	}
}
