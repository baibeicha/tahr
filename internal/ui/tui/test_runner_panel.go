package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/testrunner"
	"tahr/internal/ui"
)

// TestRunnerButtonHit records coordinates of interactive buttons.
type TestRunnerButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// TestRunnerRowHit records coordinates of test items in table.
type TestRunnerRowHit struct {
	Index int
	Y     int
	X, W  int
}

// TestRunnerPanel provides a Test Explorer tool window.
type TestRunnerPanel struct {
	WorkspaceDir string
	Runner       *testrunner.Runner
	Position     string // "right" or "bottom"
	Open         bool

	Suites      []*testrunner.TestSuite
	FlatTests   []*testrunner.TestCase
	SelectedIdx int
	Offset      int
	Running     bool

	LastSummary testrunner.TestRunSummary
	DetailOpen  bool

	ButtonHits []TestRunnerButtonHit
	RowHits    []TestRunnerRowHit

	mu sync.RWMutex

	OnOpenFile func(path string, line int)
	OnToast    func(level, title, msg string)
}

// NewTestRunnerPanel creates a new test explorer tool window.
func NewTestRunnerPanel(workspaceDir string) *TestRunnerPanel {
	p := &TestRunnerPanel{
		WorkspaceDir: workspaceDir,
		Runner:       testrunner.NewRunner(workspaceDir, nil),
		Position:     "right",
		Open:         true,
		SelectedIdx:  0,
	}
	go p.Discover()
	return p
}

// Discover triggers test discovery across workspace.
func (p *TestRunnerPanel) Discover() {
	p.mu.Lock()
	if p.Running {
		p.mu.Unlock()
		return
	}
	runner := p.Runner
	p.mu.Unlock()

	go func() {
		suites, err := runner.DiscoverTests(context.Background())
		p.mu.Lock()
		if err == nil && len(suites) > 0 {
			p.Suites = suites
			p.rebuildFlatListLocked()
		}
		p.mu.Unlock()

		if err != nil && p.OnToast != nil {
			p.OnToast("error", "TEST EXPLORER", "Discovery error: "+err.Error())
		}
	}()
}

func (p *TestRunnerPanel) rebuildFlatListLocked() {
	p.FlatTests = p.FlatTests[:0]
	for _, s := range p.Suites {
		for _, tc := range s.Tests {
			p.FlatTests = append(p.FlatTests, tc)
		}
	}
	if p.SelectedIdx >= len(p.FlatTests) {
		p.SelectedIdx = 0
	}
}

// RunAll executes all tests without destroying discovered suites.
func (p *TestRunnerPanel) RunAll() {
	p.mu.Lock()
	if p.Running {
		p.mu.Unlock()
		return
	}
	p.Running = true

	hadExistingTests := len(p.FlatTests) > 0
	for _, tc := range p.FlatTests {
		tc.Status = testrunner.TestStatusRunning
	}
	runner := p.Runner
	p.mu.Unlock()

	go func() {
		if !hadExistingTests {
			suites, _ := runner.DiscoverTests(context.Background())
			if len(suites) > 0 {
				p.mu.Lock()
				p.Suites = suites
				p.rebuildFlatListLocked()
				for _, tc := range p.FlatTests {
					tc.Status = testrunner.TestStatusRunning
				}
				hadExistingTests = len(p.FlatTests) > 0
				p.mu.Unlock()
			}
		}

		res, err := runner.RunAll(context.Background(), "")
		p.mu.Lock()
		p.Running = false

		if err == nil && res != nil {
			p.LastSummary = res.Summary
			if len(res.Suites) > 0 {
				if hadExistingTests {
					matchedMap := make(map[string]*testrunner.TestCase)
					for _, s := range res.Suites {
						for _, tc := range s.Tests {
							matchedMap[tc.Name] = tc
							matchedMap[tc.ID] = tc
						}
					}
					for _, existing := range p.FlatTests {
						if match, ok := matchedMap[existing.Name]; ok {
							existing.Status = match.Status
							existing.Duration = match.Duration
							existing.ErrorMessage = match.ErrorMessage
							existing.Traceback = match.Traceback
						} else {
							if res.Summary.Failed == 0 {
								existing.Status = testrunner.TestStatusPassed
							} else {
								existing.Status = testrunner.TestStatusFailed
							}
						}
					}
				} else {
					p.Suites = res.Suites
					p.rebuildFlatListLocked()
				}
			} else if hadExistingTests {
				for _, existing := range p.FlatTests {
					if res.Summary.Failed == 0 {
						existing.Status = testrunner.TestStatusPassed
					} else {
						existing.Status = testrunner.TestStatusFailed
					}
				}
			}
		} else if err != nil && hadExistingTests {
			for _, existing := range p.FlatTests {
				existing.Status = testrunner.TestStatusFailed
				existing.ErrorMessage = err.Error()
			}
		}
		p.mu.Unlock()

		if err != nil && p.OnToast != nil {
			p.OnToast("error", "TEST EXPLORER", "Run error: "+err.Error())
		} else if err == nil && p.OnToast != nil && res != nil {
			p.OnToast("info", "TEST EXPLORER", fmt.Sprintf("Tests completed: %d passed, %d failed", res.Summary.Passed, res.Summary.Failed))
		}
	}()
}

// RunSelected executes only the currently highlighted test case.
func (p *TestRunnerPanel) RunSelected() {
	p.mu.Lock()
	if p.Running || p.SelectedIdx < 0 || p.SelectedIdx >= len(p.FlatTests) {
		p.mu.Unlock()
		return
	}
	p.Running = true
	tc := p.FlatTests[p.SelectedIdx]
	runner := p.Runner
	p.mu.Unlock()

	go func() {
		res, err := runner.RunSingle(context.Background(), tc)
		p.mu.Lock()
		p.Running = false
		if err == nil && res != nil {
			tc.Status = res.Status
			tc.Duration = res.Duration
			tc.ErrorMessage = res.ErrorMessage
			tc.Traceback = res.Traceback
		}
		p.mu.Unlock()
	}()
}

// HandleKey handles keyboard navigation and actions.
func (p *TestRunnerPanel) HandleKey(k input.Key) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch k.Type {
	case input.KeyUp:
		if p.SelectedIdx > 0 {
			p.SelectedIdx--
		}
		return true

	case input.KeyDown:
		if p.SelectedIdx < len(p.FlatTests)-1 {
			p.SelectedIdx++
		}
		return true

	case input.KeyEnter:
		p.mu.Unlock()
		p.RunSelected()
		p.mu.Lock()
		return true
	}

	return false
}

// HandleClick handles mouse clicks on buttons and test rows.
func (p *TestRunnerPanel) HandleClick(x, y int) bool {
	p.mu.RLock()
	btnHits := append([]TestRunnerButtonHit{}, p.ButtonHits...)
	rowHits := append([]TestRunnerRowHit{}, p.RowHits...)
	p.mu.RUnlock()

	for _, hit := range btnHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "run_all":
				p.RunAll()
			case "run_selected":
				p.RunSelected()
			case "refresh":
				p.Discover()
			}
			return true
		}
	}

	for _, hit := range rowHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			p.mu.Lock()
			p.SelectedIdx = hit.Index
			p.mu.Unlock()
			return true
		}
	}

	return false
}

// RenderRect renders the test runner in bounds.
func (p *TestRunnerPanel) RenderRect(buf *buffer.Buffer, bounds buffer.Rect, theme *ui.Theme) {
	p.Render(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, theme)
}

// Render draws the complete Test Explorer tool window.
func (p *TestRunnerPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme *ui.Theme) {
	if w <= 0 || h <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ButtonHits = p.ButtonHits[:0]
	p.RowHits = p.RowHits[:0]

	bg := toColor(theme.StatusBarBg)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	selBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	warnFg := toColor(theme.DiagnosticWarn)
	greenFg := toColor(theme.String)

	// Backdrop
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	curY := y

	// 1. Header Bar: Status
	sumText := "○ idle"
	sumColor := greenFg
	if p.Running {
		sumText = "● running..."
		sumColor = warnFg
	} else if p.LastSummary.Total > 0 {
		sumText = fmt.Sprintf("%d passed, %d failed", p.LastSummary.Passed, p.LastSummary.Failed)
		if p.LastSummary.Failed > 0 {
			sumColor = errFg
		}
	}
	drawText(buf, x+1, curY, fmt.Sprintf("Tests: %s", sumText), sumColor, bg, cell.AttrBold)
	curY++

	// 2. Action buttons (with wrapping for narrow sidebars)
	buttons := []struct {
		Label  string
		Action string
	}{
		{" Run All ", "run_all"},
		{" Run Selected ", "run_selected"},
		{" Refresh ", "refresh"},
	}
	btnX := x + 1
	for _, b := range buttons {
		bLen := len([]rune(b.Label))
		if btnX+bLen >= x+w-1 && btnX > x+1 {
			curY++
			btnX = x + 1
		}
		drawText(buf, btnX, curY, b.Label, textFg, toColor(theme.CursorLineBg), cell.AttrBold)
		p.ButtonHits = append(p.ButtonHits, TestRunnerButtonHit{
			Action: b.Action,
			X:      btnX,
			Y:      curY,
			Width:  bLen,
		})
		btnX += bLen + 1
	}
	curY++

	// 3. Tests List
	if len(p.FlatTests) == 0 {
		curY++
		emptyLines := []string{
			"Тесты не обнаружены",
			"───────────────────────────────",
			"Поддерживаемые фреймворки:",
			"• Go: go test",
			"• Python: pytest, unittest",
			"• Node.js: jest, vitest, mocha",
			"• Rust: cargo test",
			"",
			"Инструкция:",
			"• Нажмите ' Refresh ' для скана",
			"• Нажмите ' Run All ' для запуска",
		}
		for _, el := range emptyLines {
			if curY >= y+h-1 {
				break
			}
			c := dimFg
			attr := cell.AttrNone
			if el == "Тесты не обнаружены" {
				c = warnFg
				attr = cell.AttrBold
			} else if strings.HasPrefix(el, "Поддерживаемые") || strings.HasPrefix(el, "Инструкция") {
				c = accentFg
			} else if strings.HasPrefix(el, "•") {
				c = textFg
			}
			drawText(buf, x+1, curY, el, c, bg, attr)
			curY++
		}
		return
	}

	detailH := 0
	var selectedTc *testrunner.TestCase
	if p.SelectedIdx >= 0 && p.SelectedIdx < len(p.FlatTests) {
		selectedTc = p.FlatTests[p.SelectedIdx]
		if selectedTc.Status == testrunner.TestStatusFailed && len(selectedTc.Traceback) > 0 {
			detailH = 5
			if detailH > h/3 {
				detailH = h / 3
			}
		}
	}

	listH := y + h - curY - detailH
	for i := 0; i < listH && i+p.Offset < len(p.FlatTests); i++ {
		idx := i + p.Offset
		tc := p.FlatTests[idx]
		rowY := curY + i

		isSelected := idx == p.SelectedIdx
		rowBg := bg
		rowFg := textFg
		prefix := "  "
		if isSelected {
			rowBg = selBg
			prefix = "● "
		}

		statusBadge := "○ pending"
		badgeFg := dimFg
		switch tc.Status {
		case testrunner.TestStatusPassed:
			statusBadge = "● pass"
			badgeFg = greenFg
		case testrunner.TestStatusFailed:
			statusBadge = "● fail"
			badgeFg = errFg
		case testrunner.TestStatusSkipped:
			statusBadge = "○ skip"
			badgeFg = warnFg
		case testrunner.TestStatusRunning:
			statusBadge = "● run"
			badgeFg = accentFg
		}

		durPart := ""
		if tc.Duration > 0 {
			durPart = fmt.Sprintf(" (%v)", tc.Duration.Round(time.Millisecond))
		}

		dispText := fmt.Sprintf("%s%s %s%s", prefix, statusBadge, tc.Name, durPart)
		if len(dispText) > w-1 {
			dispText = dispText[:w-1]
		}

		drawText(buf, x+1, rowY, dispText, rowFg, rowBg, cell.AttrNone)
		if !isSelected {
			// Draw badge indicator in color
			drawText(buf, x+3, rowY, statusBadge, badgeFg, rowBg, cell.AttrBold)
		}

		p.RowHits = append(p.RowHits, TestRunnerRowHit{
			Index: idx,
			Y:     rowY,
			X:     x + 1,
			W:     w - 2,
		})
	}

	// 4. Traceback / failure details pane at bottom if selected test failed
	if detailH > 0 && selectedTc != nil {
		detailY := y + h - detailH
		// Separator line
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, detailY, '─', toColor(theme.BorderColor), bg, cell.AttrNone)
		}
		titleFail := " Failure Traceback: "
		drawText(buf, x+2, detailY, titleFail, errFg, bg, cell.AttrBold)
		detailY++

		for row := 0; row < detailH-1 && row < len(selectedTc.Traceback); row++ {
			tbLine := selectedTc.Traceback[row]
			if len(tbLine) > w-2 {
				tbLine = tbLine[:w-2]
			}
			drawText(buf, x+2, detailY+row, tbLine, textFg, toColor(theme.Background), cell.AttrNone)
		}
	}
}
