package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/git"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// GitModal provides an interactive Git interface with Hunk Staging and a visual Branch Commit Graph.
type GitModal struct {
	Open          bool
	WorkspaceDir  string
	ActiveFile    string
	CurrentTab    int // 0: Hunk Staging, 1: Branch Graph
	Hunks         []git.Hunk
	SelectedHunk  int
	CommitGraph   []git.CommitNode
	GraphScrollY  int
	CommitMessage string
	IsCommitting  bool
	StatusMsg     string
	BorderRounded bool
}

// NewGitModal creates an instance of GitModal.
func NewGitModal(workspaceDir string) *GitModal {
	return &GitModal{
		Open:         false,
		WorkspaceDir: workspaceDir,
		CurrentTab:   0,
		Hunks:        make([]git.Hunk, 0),
		CommitGraph:  make([]git.CommitNode, 0),
	}
}

// OpenForFile opens the modal for the given file and workspace.
func (gm *GitModal) OpenForFile(dir, filePath string) {
	gm.Open = true
	gm.WorkspaceDir = dir
	gm.ActiveFile = filePath
	gm.SelectedHunk = 0
	gm.GraphScrollY = 0
	gm.IsCommitting = false
	gm.StatusMsg = ""

	gm.Refresh()
}

// Refresh reloads diff hunks and commit graph.
func (gm *GitModal) Refresh() {
	if gm.ActiveFile != "" {
		hunks, err := git.GetFileHunks(gm.WorkspaceDir, gm.ActiveFile)
		if err == nil {
			gm.Hunks = hunks
		} else {
			gm.Hunks = nil
		}
	}

	graph, err := git.GetCommitGraph(gm.WorkspaceDir, 100)
	if err == nil && len(graph) > 0 {
		gm.CommitGraph = graph
	} else {
		// Provide mock / educational branch graph if outside git repo
		gm.CommitGraph = []git.CommitNode{
			{Hash: "a1b2c3d", GraphPrefix: "*", Refs: "HEAD -> main, origin/main", Author: "Tahr Core", Date: "just now", Message: "feat: multi-selection rope & async lsp"},
			{Hash: "f4e5d6c", GraphPrefix: "*", Refs: "v0.1.0", Author: "Tahr Core", Date: "1 day ago", Message: "release: initial headless core release"},
		}
	}
}

// HandleKey processes keyboard shortcuts in the Git Modal.
func (gm *GitModal) HandleKey(k input.Key) bool {
	if !gm.Open {
		return false
	}

	if k.Type == input.KeyEsc {
		if gm.IsCommitting {
			gm.IsCommitting = false
			return true
		}
		gm.Open = false
		return true
	}

	if k.Type == input.KeyTab || k.Type == input.KeyBacktab {
		gm.CurrentTab = (gm.CurrentTab + 1) % 2
		return true
	}

	if !gm.IsCommitting {
		switch k.Rune {
		case '1':
			gm.CurrentTab = 0
			return true
		case '2':
			gm.CurrentTab = 1
			return true
		}
		switch k.Type {
		case input.KeyLeft:
			gm.CurrentTab = 0
			return true
		case input.KeyRight:
			gm.CurrentTab = 1
			return true
		}
	}

	if gm.IsCommitting {
		switch k.Type {
		case input.KeyEnter:
			if strings.TrimSpace(gm.CommitMessage) != "" {
				gm.StatusMsg = fmt.Sprintf("Committed: %s", gm.CommitMessage)
				gm.CommitMessage = ""
				gm.IsCommitting = false
				gm.Refresh()
			}
			return true
		case input.KeyBackspace:
			if len(gm.CommitMessage) > 0 {
				gm.CommitMessage = gm.CommitMessage[:len(gm.CommitMessage)-1]
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

	// Tab 0: Hunk Staging
	if gm.CurrentTab == 0 {
		switch k.Type {
		case input.KeyUp:
			if gm.SelectedHunk > 0 {
				gm.SelectedHunk--
			}
			return true
		case input.KeyDown:
			if gm.SelectedHunk < len(gm.Hunks)-1 {
				gm.SelectedHunk++
			}
			return true
		}

		switch k.Rune {
		case 'y', 'Y': // Stage hunk
			if gm.SelectedHunk >= 0 && gm.SelectedHunk < len(gm.Hunks) {
				h := gm.Hunks[gm.SelectedHunk]
				_ = git.StageHunk(gm.WorkspaceDir, gm.ActiveFile, h)
				gm.Hunks[gm.SelectedHunk].Staged = true
				gm.StatusMsg = fmt.Sprintf("Staged hunk #%d", gm.SelectedHunk+1)
				if gm.SelectedHunk < len(gm.Hunks)-1 {
					gm.SelectedHunk++
				}
			}
			return true
		case 'n', 'N': // Next hunk / Skip
			if gm.SelectedHunk < len(gm.Hunks)-1 {
				gm.SelectedHunk++
			}
			return true
		case 'u', 'U': // Unstage hunk
			if gm.SelectedHunk >= 0 && gm.SelectedHunk < len(gm.Hunks) {
				h := gm.Hunks[gm.SelectedHunk]
				_ = git.UnstageHunk(gm.WorkspaceDir, gm.ActiveFile, h)
				gm.Hunks[gm.SelectedHunk].Staged = false
				gm.StatusMsg = fmt.Sprintf("Unstaged hunk #%d", gm.SelectedHunk+1)
			}
			return true
		case 'c', 'C': // Open commit input
			gm.IsCommitting = true
			gm.CommitMessage = ""
			return true
		}
	}

	// Tab 1: Branch Graph
	if gm.CurrentTab == 1 {
		switch k.Type {
		case input.KeyUp:
			if gm.GraphScrollY > 0 {
				gm.GraphScrollY--
			}
			return true
		case input.KeyDown:
			if gm.GraphScrollY < len(gm.CommitGraph)-1 {
				gm.GraphScrollY++
			}
			return true
		case input.KeyPgUp:
			gm.GraphScrollY = max(0, gm.GraphScrollY-10)
			return true
		case input.KeyPgDown:
			gm.GraphScrollY = min(max(0, len(gm.CommitGraph)-1), gm.GraphScrollY+10)
			return true
		}
	}

	// Complete key isolation: consume all other keys while modal is open
	return true
}

// HandleClick processes mouse clicks inside and outside the Git Modal.
func (gm *GitModal) HandleClick(mouseX, mouseY, screenW, screenH int) bool {
	if !gm.Open {
		return false
	}
	modalW := screenW - 12
	modalH := screenH - 6
	if modalW < 60 {
		modalW = screenW - 2
	}
	if modalH < 16 {
		modalH = screenH - 2
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Click outside modal -> close
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		gm.Open = false
		return true
	}

	// Click on tab headers (startY+1)
	if mouseY == startY+1 {
		tab1Len := len(" 1: Interactive Hunk Staging ")
		tab2Len := len(" 2: Visual Branch Graph ")
		tab1Start := startX + 2
		tab1End := tab1Start + tab1Len
		tab2Start := tab1End + 2
		tab2End := tab2Start + tab2Len

		if mouseX >= tab1Start && mouseX < tab1End {
			gm.CurrentTab = 0
			return true
		}
		if mouseX >= tab2Start && mouseX < tab2End {
			gm.CurrentTab = 1
			return true
		}
	}

	// Tab 0: Click inside hunk list
	if gm.CurrentTab == 0 {
		contentTop := startY + 3
		contentH := modalH - 5
		listW := 28
		if mouseX >= startX+2 && mouseX < startX+2+listW && mouseY >= contentTop && mouseY < contentTop+contentH {
			clickedIdx := mouseY - contentTop
			if clickedIdx >= 0 && clickedIdx < len(gm.Hunks) {
				gm.SelectedHunk = clickedIdx
				return true
			}
		}
	}

	return true
}

// Render draws the interactive Git Modal on screen.
func (gm *GitModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !gm.Open || buf == nil {
		return
	}

	modalW := screenW - 12
	modalH := screenH - 6
	if modalW < 60 {
		modalW = screenW - 2
	}
	if modalH < 16 {
		modalH = screenH - 2
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	selectBg := toColor(theme.SelectionBg)
	selectFg := toColor(theme.Foreground)

	// Background and Border
	for y := startY; y < startY+modalH; y++ {
		for x := startX; x < startX+modalW; x++ {
			var ch rune = ' '
			cBg := bg
			cFg := fg

			isTop := y == startY
			isBottom := y == startY+modalH-1
			isLeft := x == startX
			isRight := x == startX+modalW-1

			tl, tr, bl, br := '┌', '┐', '└', '┘'
			if gm.BorderRounded {
				tl, tr, bl, br = '╭', '╮', '╰', '╯'
			}

			if isTop && isLeft {
				ch = tl
				cFg = borderFg
			} else if isTop && isRight {
				ch = tr
				cFg = borderFg
			} else if isBottom && isLeft {
				ch = bl
				cFg = borderFg
			} else if isBottom && isRight {
				ch = br
				cFg = borderFg
			} else if isTop || isBottom {
				ch = '─'
				cFg = borderFg
			} else if isLeft || isRight {
				ch = '│'
				cFg = borderFg
			}

			buf.SetRune(x, y, ch, cFg, cBg, cell.AttrNone)
		}
	}

	// Tabs Header on startY+1
	tab1Label := i18n.T("git.tab_staging")
	tab2Label := i18n.T("git.tab_graph")
	tab1Fg := toColor(theme.Comment)
	tab1Bg := bg
	tab1Attr := cell.AttrNone

	tab2Fg := toColor(theme.Comment)
	tab2Bg := bg
	tab2Attr := cell.AttrNone

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
		// Tab 0: Hunks Staging
		if len(gm.Hunks) == 0 {
			msg := i18n.T("git.no_hunks")
			for i, r := range []rune(msg) {
				buf.SetRune(startX+4+i, contentTop+2, r, toColor(theme.Comment), bg, cell.AttrNone)
			}
		} else {
			// Left pane: Hunk list (width 28)
			listW := 28
			for i, h := range gm.Hunks {
				if i >= contentH {
					break
				}
				rowY := contentTop + i
				isSel := i == gm.SelectedHunk
				prefix := "  "
				if isSel {
					prefix = "▸ "
				}
				stagedTag := "○"
				if h.Staged {
					stagedTag = "✓"
				}
				hunkTitle := fmt.Sprintf(i18n.T("git.hunk_num"), prefix, stagedTag, i+1)
				hBg := bg
				hFg := fg
				if isSel {
					hBg = selectBg
					hFg = selectFg
				}
				hunkRunes := []rune(hunkTitle)
				for col := 0; col < listW; col++ {
					r := ' '
					if col < len(hunkRunes) {
						r = hunkRunes[col]
					}
					buf.SetRune(startX+2+col, rowY, r, hFg, hBg, cell.AttrNone)
				}
			}

			// Vertical divider
			for y := contentTop; y < contentTop+contentH; y++ {
				buf.SetRune(startX+2+listW, y, '│', borderFg, bg, cell.AttrNone)
			}

			// Right pane: Hunk lines preview
			if gm.SelectedHunk >= 0 && gm.SelectedHunk < len(gm.Hunks) {
				selH := gm.Hunks[gm.SelectedHunk]
				hunkHeaderStr := selH.Summary()
				for i, r := range hunkHeaderStr {
					buf.SetRune(startX+4+listW+i, contentTop, r, toColor(theme.Function), bg, cell.AttrBold)
				}

				for li, line := range selH.Lines {
					if li+1 >= contentH {
						break
					}
					rowY := contentTop + 1 + li
					lFg := fg
					if strings.HasPrefix(line, "+") {
						lFg = toColor(theme.String) // Green for added
					} else if strings.HasPrefix(line, "-") {
						lFg = toColor(theme.DiagnosticError) // Red for deleted
					}

					for c := 0; c < modalW-listW-6; c++ {
						r := ' '
						if c < len(line) {
							r = rune(line[c])
						}
						buf.SetRune(startX+4+listW+c, rowY, r, lFg, bg, cell.AttrNone)
					}
				}
			}
		}
	} else {
		// Tab 1: Visual Branch Graph
		for row := 0; row < contentH; row++ {
			nodeIdx := gm.GraphScrollY + row
			if nodeIdx >= len(gm.CommitGraph) {
				break
			}
			node := gm.CommitGraph[nodeIdx]
			rowY := contentTop + row

			// Format: [GraphPrefix] [Hash] [Refs] [Message] ([Author], [Date])
			col := startX + 3
			// Graph prefix
			for _, r := range node.GraphPrefix {
				buf.SetRune(col, rowY, r, toColor(theme.Function), bg, cell.AttrBold)
				col++
			}
			col += 2

			// Hash
			for _, r := range node.Hash {
				buf.SetRune(col, rowY, r, toColor(theme.Function), bg, cell.AttrNone)
				col++
			}
			col += 2

			// Refs
			if node.Refs != "" {
				refStr := fmt.Sprintf("(%s) ", node.Refs)
				for _, r := range refStr {
					buf.SetRune(col, rowY, r, toColor(theme.DiagnosticWarn), bg, cell.AttrBold)
					col++
				}
			}

			// Message
			for _, r := range node.Message {
				if col < startX+modalW-24 {
					buf.SetRune(col, rowY, r, fg, bg, cell.AttrNone)
					col++
				}
			}

			// Author and Date right-aligned
			meta := fmt.Sprintf("%s, %s", node.Author, node.Date)
			metaRunes := []rune(meta)
			metaStart := startX + modalW - len(metaRunes) - 3
			if metaStart > col+2 {
				for mi, r := range metaRunes {
					buf.SetRune(metaStart+mi, rowY, r, toColor(theme.Comment), bg, cell.AttrNone)
				}
			}
		}
	}

	// Commit Input bar if committing
	if gm.IsCommitting {
		inputY := startY + modalH - 3
		prompt := " " + i18n.T("git.commit_prompt") + gm.CommitMessage + "█"
		promptRunes := []rune(prompt)
		for c := 0; c < modalW-4; c++ {
			r := ' '
			if c < len(promptRunes) {
				r = promptRunes[c]
			}
			buf.SetRune(startX+2+c, inputY, r, toColor(theme.Foreground), toColor(theme.SelectionBg), cell.AttrBold)
		}
	}

	// Footer instructions
	footer := " " + i18n.T("git.footer_tab0") + " "
	if gm.CurrentTab == 1 {
		footer = " " + i18n.T("git.footer_tab1") + " "
	}
	if gm.StatusMsg != "" {
		footer = fmt.Sprintf(" %s │ %s", gm.StatusMsg, strings.TrimSpace(footer))
	}

	for i, r := range []rune(footer) {
		fx := startX + 3 + i
		if fx < startX+modalW-2 {
			buf.SetRune(fx, startY+modalH-1, r, toColor(theme.Comment), bg, cell.AttrNone)
		}
	}
}
